import { expect, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD
const xrayUserKeyDomain = process.env.E2E_XRAY_USER_KEY_DOMAIN || 'suiyue.local'
const csrfHeader = { 'X-CSRF-Token': 'suiyue-web' }

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for traffic flow e2e tests`)
  }
}

async function readJson(response) {
  const text = await response.text()
  try {
    return text ? JSON.parse(text) : {}
  } catch {
    throw new Error(`Expected JSON response from ${response.url()}, got: ${text.slice(0, 300)}`)
  }
}

async function login(request, username, password) {
  const response = await request.post('/api/auth/login', {
    data: { username, password },
    headers: csrfHeader,
  })
  const body = await readJson(response)
  expect(response.ok(), `login ${username}: ${body.message || response.status()}`).toBeTruthy()
  expect(body.success, `login ${username} success flag`).toBe(true)
  expect(body.data?.accessToken, `login ${username} access token`).toBeTruthy()
  return {
    token: body.data.accessToken,
    user: body.data.user,
  }
}

function authHeaders(token, extra = {}) {
  return {
    Authorization: `Bearer ${token}`,
    ...csrfHeader,
    ...extra,
  }
}

async function api(request, token, method, url, options = {}) {
  const response = await request[method](url, {
    ...options,
    headers: authHeaders(token, options.headers),
  })
  const body = await readJson(response)
  expect(response.ok(), `${method.toUpperCase()} ${url}: ${body.message || response.status()}`).toBeTruthy()
  expect(body.success, `${method.toUpperCase()} ${url} success flag`).toBe(true)
  return body.data
}

async function safeApi(request, token, method, url, options = {}) {
  try {
    return await api(request, token, method, url, options)
  } catch {
    return null
  }
}

async function agentApi(request, url, data) {
  const response = await request.post(url, {
    data,
    headers: csrfHeader,
  })
  const body = await readJson(response)
  expect(response.ok(), `POST ${url}: ${body.message || response.status()}`).toBeTruthy()
  expect(body.success, `POST ${url} success flag`).toBe(true)
  return body.data
}

function futureISO(days) {
  const date = new Date()
  date.setDate(date.getDate() + days)
  return date.toISOString()
}

function isoAt(baseTime, offsetSeconds) {
  return new Date(baseTime + offsetSeconds * 1000).toISOString()
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = 0
  let value = bytes
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(1)} ${units[index]}`
}

function multiTrafficConfig() {
  const hostId = Number(process.env.E2E_MULTI_NODE_HOST_ID || 0)
  const token = process.env.E2E_MULTI_NODE_HOST_TOKEN || ''
  const nodeIds = (process.env.E2E_MULTI_TRAFFIC_NODE_IDS || '')
    .split(',')
    .map((value) => Number(value.trim()))
    .filter(Boolean)
  const enabled = Boolean(hostId && token && nodeIds.length >= 2)
  if (process.env.E2E_REQUIRE_MULTI_TRAFFIC === '1') {
    expect(enabled, 'real multi_exit traffic env is configured').toBe(true)
  }
  return { enabled, hostId, token, nodeIds }
}

async function expectUsage(request, adminToken, userId, expected) {
  const usage = await api(request, adminToken, 'get', `/api/admin/users/${userId}/usage`, {
    params: { days: 7, weeks: 4, months: 3, recent: 20 },
  })
  expect(usage.summary.subscription_to_today.upload).toBe(expected.upload)
  expect(usage.summary.subscription_to_today.download).toBe(expected.download)
  expect(usage.summary.subscription_to_today.total).toBe(expected.total)
  expect(usage.summary.today.total).toBe(expected.total)
  expect((usage.recent || []).reduce((sum, item) => sum + item.delta_total, 0)).toBe(expected.total)
  return usage
}

async function reportAgentTask(request, nodeId, token, task, success = true) {
  await agentApi(request, '/api/agent/task-result', {
    node_id: nodeId,
    token,
    task_id: task.id,
    lock_token: task.lock_token,
    success,
  })
}

async function triggerNodeAccessSync(request, adminToken, planId, groupId, nodeId) {
  await api(request, adminToken, 'post', `/api/admin/plans/${planId}/node-groups`, {
    data: { node_group_ids: [groupId] },
  })
  await api(request, adminToken, 'put', `/api/admin/node-groups/${groupId}/nodes`, {
    data: { node_ids: [] },
  })
  await api(request, adminToken, 'put', `/api/admin/node-groups/${groupId}/nodes`, {
    data: { node_ids: [nodeId] },
  })
}

async function claimUpsertUserTask(request, adminToken, created, agentToken, xrayUserKey, uuid) {
  for (let attempt = 0; attempt < 6; attempt += 1) {
    if (attempt > 0) {
      await triggerNodeAccessSync(request, adminToken, created.planId, created.groupId, created.nodeId)
    }

    const heartbeat = await agentApi(request, '/api/agent/heartbeat', {
      node_id: created.nodeId,
      token: agentToken,
      version: 'playwright-e2e',
    })

    for (const task of heartbeat.tasks || []) {
      if (task.action === 'UPSERT_USER' && task.payload.includes(xrayUserKey)) {
        expect(task.payload).toContain(uuid)
        return task
      }
      await reportAgentTask(request, created.nodeId, agentToken, task)
    }

    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`UPSERT_USER task was not delivered for ${xrayUserKey}`)
}

test('traffic accounting data flow is billed, deduped, reported, and visible in UI', async ({ request, page }) => {
  test.setTimeout(120_000)
  requireEnv('E2E_ADMIN_USERNAME', adminUsername)
  requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

  const multi = multiTrafficConfig()
  const trafficLimit = multi.enabled ? 4000 : 2000
  const { token: adminToken } = await login(request, adminUsername, adminPassword)
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const prefix = `traffic-flow-${suffix}`
  const agentToken = `agent-token-${suffix}`
  const userPassword = `Pw-${suffix}`
  const username = `traffic${suffix.replace(/-/g, '')}`.slice(0, 31)
  const xrayUserKey = `${username}@${xrayUserKeyDomain}`
  const trafficBaseTime = Date.now() + 60_000

  const created = {
    nodeId: null,
    groupId: null,
    planId: null,
    userId: null,
  }

  try {
    const node = await api(request, adminToken, 'post', '/api/admin/nodes', {
      data: {
        name: `${prefix}-node`,
        protocol: 'vless',
        host: '203.0.113.77',
        port: 24477,
        server_name: 'www.microsoft.com',
        public_key: 'E2ETrafficPublicKey1234567890123456789012345',
        short_id: '7a7b7c7d',
        fingerprint: 'chrome',
        flow: 'xtls-rprx-vision',
        line_mode: 'direct_only',
        agent_base_url: 'http://203.0.113.77:18080',
        agent_token: agentToken,
        sort_weight: 950,
        is_enabled: true,
      },
    })
    created.nodeId = node.id

    const group = await api(request, adminToken, 'post', '/api/admin/node-groups', {
      data: { name: `${prefix}-group`, description: 'playwright traffic flow group' },
    })
    created.groupId = group.id
    await api(request, adminToken, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
      data: { node_ids: [created.nodeId] },
    })

    const plan = await api(request, adminToken, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan`,
        price: 1,
        currency: 'USDT',
        traffic_limit: trafficLimit,
        duration_days: 7,
        sort_weight: 950,
        is_active: true,
      },
    })
    created.planId = plan.id
    await api(request, adminToken, 'post', `/api/admin/plans/${created.planId}/node-groups`, {
      data: { node_group_ids: [created.groupId] },
    })

    const user = await api(request, adminToken, 'post', '/api/admin/users', {
      data: {
        username,
        email: `${username}@example.test`,
        password: userPassword,
        status: 'active',
        is_admin: false,
      },
    })
    created.userId = user.id
    expect(user.uuid).toBeTruthy()

    const upserted = await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
      data: {
        plan_id: created.planId,
        status: 'ACTIVE',
        expire_date: futureISO(7),
        traffic_limit: trafficLimit,
        used_traffic: 0,
        generate_token: true,
      },
    })
    expect(upserted.subscription.status).toBe('ACTIVE')
    expect(upserted.tokens.length).toBeGreaterThan(0)

    const upsertTask = await claimUpsertUserTask(request, adminToken, created, agentToken, xrayUserKey, user.uuid)
    await reportAgentTask(request, created.nodeId, agentToken, upsertTask)

    const plainSub = await request.get(`/sub/${upserted.tokens[0].token}/plain`)
    expect(plainSub.ok(), 'plain subscription download').toBeTruthy()
    expect(await plainSub.text()).toContain(`${prefix}-node`)

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 1),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 100, downlink_total: 200 }],
    })
    await expectUsage(request, adminToken, created.userId, { upload: 0, download: 0, total: 0 })

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 2),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 600, downlink_total: 900 }],
    })
    await expectUsage(request, adminToken, created.userId, { upload: 500, download: 700, total: 1200 })

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 1),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 5000, downlink_total: 5000 }],
    })
    await expectUsage(request, adminToken, created.userId, { upload: 500, download: 700, total: 1200 })

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 3),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 10, downlink_total: 20 }],
    })
    await expectUsage(request, adminToken, created.userId, { upload: 500, download: 700, total: 1200 })

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 4),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 110, downlink_total: 220 }],
    })

    const expected = { upload: 600, download: 900, total: 1500 }
    await expectUsage(request, adminToken, created.userId, expected)

    if (multi.enabled) {
      const [nodeA, nodeB] = multi.nodeIds
      await agentApi(request, '/api/agent/multi/traffic', {
        node_host_id: multi.hostId,
        token: multi.token,
        reports: [
          {
            node_id: nodeA,
            collected_at: isoAt(trafficBaseTime, 5),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 1000, downlink_total: 2000 }],
          },
          {
            node_id: nodeB,
            collected_at: isoAt(trafficBaseTime, 5),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 3000, downlink_total: 4000 }],
          },
        ],
      })
      await expectUsage(request, adminToken, created.userId, expected)

      await agentApi(request, '/api/agent/multi/traffic', {
        node_host_id: multi.hostId,
        token: multi.token,
        reports: [
          {
            node_id: nodeA,
            collected_at: isoAt(trafficBaseTime, 6),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 1300, downlink_total: 2500 }],
          },
          {
            node_id: nodeB,
            collected_at: isoAt(trafficBaseTime, 6),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 3400, downlink_total: 4600 }],
          },
        ],
      })
      expected.upload += 700
      expected.download += 1100
      expected.total += 1800
      const multiUsage = await expectUsage(request, adminToken, created.userId, expected)
      expect(multiUsage.recent.map((item) => Number(item.node_id))).toEqual(expect.arrayContaining([nodeA, nodeB]))

      await agentApi(request, '/api/agent/multi/traffic', {
        node_host_id: multi.hostId,
        token: multi.token,
        reports: [
          {
            node_id: nodeA,
            collected_at: isoAt(trafficBaseTime, 5),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 9999, downlink_total: 9999 }],
          },
          {
            node_id: nodeB,
            collected_at: isoAt(trafficBaseTime, 5),
            items: [{ xray_user_key: xrayUserKey, uplink_total: 9999, downlink_total: 9999 }],
          },
        ],
      })
      await expectUsage(request, adminToken, created.userId, expected)
    }

    await page.goto('/login')
    await expect(page.getByRole('heading', { name: '用户登录' })).toBeVisible()
    await page.getByPlaceholder('用户名').fill(username)
    await page.getByPlaceholder('密码').fill(userPassword)
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page).toHaveURL(/\/$/)
    await page.goto('/subscription')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(`${prefix}-plan`).first()).toBeVisible()
    await expect(page.getByText(formatBytes(expected.total)).first()).toBeVisible()

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 7),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 610, downlink_total: 720 }],
    })
    expected.upload += 500
    expected.download += 500
    expected.total += 1000
    expect(expected.total).toBeGreaterThanOrEqual(trafficLimit)
    await expectUsage(request, adminToken, created.userId, expected)

    const suspendedSub = await api(request, adminToken, 'get', `/api/admin/users/${created.userId}/subscription`)
    expect(suspendedSub.subscription.status).toBe('SUSPENDED')
    expect(suspendedSub.subscription.used_traffic).toBe(expected.total)

    await agentApi(request, '/api/agent/traffic', {
      node_id: created.nodeId,
      token: agentToken,
      collected_at: isoAt(trafficBaseTime, 8),
      items: [{ xray_user_key: xrayUserKey, uplink_total: 10000, downlink_total: 10000 }],
    })
    await expectUsage(request, adminToken, created.userId, expected)

    const nodes = await api(request, adminToken, 'get', '/api/admin/nodes')
    const testNode = nodes.nodes.find((item) => Number(item.id) === Number(created.nodeId))
    expect(testNode?.last_traffic_success_at).toBeTruthy()
    expect(testNode?.traffic_error_count || 0).toBe(0)

    await page.goto('/subscription')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText('暂无订阅')).toBeVisible()
  } finally {
    if (created.userId) {
      await safeApi(request, adminToken, 'delete', `/api/admin/users/${created.userId}`)
    }
    if (created.planId) {
      await safeApi(request, adminToken, 'post', `/api/admin/plans/${created.planId}/node-groups`, {
        data: { node_group_ids: [] },
      })
      await safeApi(request, adminToken, 'delete', `/api/admin/plans/${created.planId}`)
    }
    if (created.groupId) {
      await safeApi(request, adminToken, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
        data: { node_ids: [] },
      })
      await safeApi(request, adminToken, 'delete', `/api/admin/node-groups/${created.groupId}`)
    }
    if (created.nodeId) {
      await safeApi(request, adminToken, 'delete', `/api/admin/nodes/${created.nodeId}`)
    }
  }
})
