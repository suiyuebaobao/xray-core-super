import { expect, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD
const csrfHeader = { 'X-CSRF-Token': 'suiyue-web' }

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for residential socks5 e2e tests`)
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

function gb(value) {
  return value * 1024 * 1024 * 1024
}

function futureISO(days) {
  const date = new Date()
  date.setDate(date.getDate() + days)
  return date.toISOString()
}

function uniqueSuffix() {
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`
}

function countOccurrences(text, value) {
  return text.split(value).length - 1
}

async function findUserByUsername(request, token, username) {
  const data = await api(request, token, 'get', '/api/admin/users', {
    params: { keyword: username, page: 1, size: 20 },
  })
  return (data.users || []).find((item) => item.username === username) || null
}

async function cleanupUser(request, token, username, knownId) {
  if (knownId) {
    await safeApi(request, token, 'delete', `/api/admin/users/${knownId}`)
    return
  }
  const user = await findUserByUsername(request, token, username).catch(() => null)
  if (user?.id) {
    await safeApi(request, token, 'delete', `/api/admin/users/${user.id}`)
  }
}

test('multiple SOCKS5 upstreams create independent residential subscription lines', async ({ request }) => {
  test.setTimeout(120_000)
  requireEnv('E2E_ADMIN_USERNAME', adminUsername)
  requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

  const { token: adminToken } = await login(request, adminUsername, adminPassword)
  const suffix = uniqueSuffix()
  const prefix = `e2e-home-${suffix}`
  const username = `home${suffix.replace(/[^a-z0-9]/gi, '')}`.slice(0, 31)
  const userPassword = `Pw-${suffix}`
  const proxyA = 'socks5://user-a:pass-a@proxy-a.example.test:3010'
  const proxyB = 'socks5://user-b:pass-b@proxy-b.example.test:3011'
  const expectedNames = [
    `${prefix}-node-1`,
    `${prefix}-node-1-XHTTP`,
    `${prefix}-node-2`,
    `${prefix}-node-2-XHTTP`,
  ]

  const created = {
    nodeIds: [],
    groupId: null,
    planId: null,
    userId: null,
  }

  try {
    const createNodeData = await api(request, adminToken, 'post', '/api/admin/nodes', {
      data: {
        name: `${prefix}-node`,
        protocol: 'vless',
        host: '203.0.113.252',
        traffic_pool: 'residential',
        outbound_type: 'socks5',
        outbound_proxy_url: `${proxyA}\n${proxyB}`,
        transports: ['tcp', 'xhttp'],
        tcp_port: 25443,
        xhttp_port: 25445,
        xhttp_path: '/home-xhttp',
        xhttp_mode: 'stream-up',
        xhttp_host: 'cdn.example.test',
        server_name: 'www.microsoft.com',
        public_key: 'E2EResidentialPublicKey123456789012345678901234',
        short_id: 'a1b2c3d4',
        fingerprint: 'chrome',
        flow: 'xtls-rprx-vision',
        line_mode: 'direct_only',
        agent_base_url: `http://203.0.113.252:18${suffix.slice(-3).replace(/\D/g, '0') || '080'}`,
        agent_token: `home-agent-token-${suffix}`,
        sort_weight: 970,
        is_enabled: true,
      },
    })

    const nodes = createNodeData.nodes || []
    expect(nodes).toHaveLength(4)
    created.nodeIds = nodes.map((node) => node.id)
    expect(new Set(nodes.map((node) => node.node_host_id)).size).toBe(1)
    expect(new Set(nodes.map((node) => node.port)).size).toBe(4)
    expect(nodes.map((node) => node.name).sort()).toEqual([...expectedNames].sort())

    for (const node of nodes) {
      expect(node.traffic_pool).toBe('residential')
      expect(node.outbound_type).toBe('socks5')
      expect([proxyA, proxyB]).toContain(node.outbound_proxy_url)
      expect(node.line_mode).toBe('direct_only')
      expect(node.flow || '').toBe('')
      if (node.transport === 'xhttp') {
        expect(node.xhttp_path).toBe('/home-xhttp')
        expect(node.xhttp_mode).toBe('stream-up')
        expect(node.xhttp_host).toBe('cdn.example.test')
      }
    }

    const group = await api(request, adminToken, 'post', '/api/admin/node-groups', {
      data: { name: `${prefix}-group`, description: 'playwright residential socks5 group' },
    })
    created.groupId = group.id
    const binding = await api(request, adminToken, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
      data: { node_ids: created.nodeIds },
    })
    expect(binding.node_ids.map(String).sort()).toEqual(created.nodeIds.map(String).sort())

    const plan = await api(request, adminToken, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan`,
        price: 1,
        currency: 'USDT',
        traffic_limit: gb(1),
        residential_traffic_limit: gb(3),
        duration_days: 7,
        sort_weight: 971,
        is_active: true,
      },
    })
    created.planId = plan.id
    expect(plan.residential_traffic_limit).toBe(gb(3))
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

    const subscriptionData = await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
      data: {
        plan_id: created.planId,
        status: 'ACTIVE',
        expire_date: futureISO(7),
        traffic_limit: gb(1),
        used_traffic: 0,
        residential_traffic_limit: gb(3),
        residential_used_traffic: 0,
        generate_token: true,
      },
    })
    expect(subscriptionData.subscription?.residential_traffic_limit).toBe(gb(3))
    const token = subscriptionData.tokens?.[0]?.token
    expect(token).toBeTruthy()

    const plainResponse = await request.get(`/sub/${token}/plain`)
    expect(plainResponse.ok(), 'plain subscription download').toBeTruthy()
    const plain = await plainResponse.text()
    for (const name of expectedNames) {
      expect(plain, `plain subscription contains ${name}`).toContain(name)
    }
    expect(countOccurrences(plain, 'vless://')).toBe(4)
    expect(countOccurrences(plain, 'type=xhttp')).toBe(2)
    expect(plain).toContain('path=%2Fhome-xhttp')
    expect(plain).toContain('host=cdn.example.test')
    expect(countOccurrences(plain, 'flow=xtls-rprx-vision')).toBe(0)

    const clashResponse = await request.get(`/sub/${token}/clash`)
    expect(clashResponse.ok(), 'clash subscription download').toBeTruthy()
    const clash = await clashResponse.text()
    for (const name of expectedNames) {
      expect(clash, `clash subscription contains ${name}`).toContain(name)
    }
    expect(countOccurrences(clash, 'type: vless')).toBe(4)
    expect(countOccurrences(clash, 'network: xhttp')).toBe(2)
    expect(clash).toContain('xhttp-opts:')
    expect(clash).toContain('path: /home-xhttp')
    expect(clash).toContain('host: cdn.example.test')
    expect(countOccurrences(clash, 'flow: xtls-rprx-vision')).toBe(0)

    const exhausted = await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
      data: {
        plan_id: created.planId,
        status: 'ACTIVE',
        expire_date: futureISO(7),
        traffic_limit: gb(1),
        used_traffic: 0,
        residential_traffic_limit: gb(3),
        residential_used_traffic: gb(3),
        generate_token: true,
      },
    })
    expect(exhausted.subscription?.residential_used_traffic).toBe(gb(3))
    const emptyResidentialResponse = await request.get(`/sub/${token}/plain`)
    expect(emptyResidentialResponse.ok(), 'exhausted residential pool hides residential-only nodes').toBeFalsy()
  } finally {
    await cleanupUser(request, adminToken, username, created.userId)
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
    for (const nodeId of created.nodeIds) {
      await safeApi(request, adminToken, 'delete', `/api/admin/nodes/${nodeId}`)
    }
  }
})
