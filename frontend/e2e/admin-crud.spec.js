import { expect, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD
const csrfHeader = { 'X-CSRF-Token': 'suiyue-web' }

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for admin CRUD e2e tests`)
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

function byId(items, id) {
  return items.find((item) => String(item.id) === String(id))
}

test('admin CRUD APIs and subscription side effects work end to end', async ({ request }) => {
  requireEnv('E2E_ADMIN_USERNAME', adminUsername)
  requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

  const { token: adminToken } = await login(request, adminUsername, adminPassword)
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const prefix = `e2e-crud-${suffix}`
  const flowPassword = `Pw-${suffix}`
  const flowPasswordReset = `Pw2-${suffix}`

  const created = {
    nodeId: null,
    groupId: null,
    planCrudId: null,
    planFlowId: null,
    relayId: null,
    userId: null,
    tokenId: null,
  }

  try {
    const node = await api(request, adminToken, 'post', '/api/admin/nodes', {
      data: {
        name: `${prefix}-node`,
        protocol: 'vless',
        host: '203.0.113.10',
        port: 24430,
        server_name: 'www.microsoft.com',
        public_key: 'E2ETestPublicKey123456789012345678901234567',
        short_id: 'abcd1234',
        fingerprint: 'chrome',
        flow: 'xtls-rprx-vision',
        line_mode: 'direct_and_relay',
        agent_base_url: 'http://203.0.113.10:18080',
        agent_token: `node-token-${suffix}`,
        sort_weight: 900,
        is_enabled: true,
      },
    })
    created.nodeId = node.id

    const updatedNode = await api(request, adminToken, 'put', `/api/admin/nodes/${created.nodeId}`, {
      data: {
        name: `${prefix}-node-updated`,
        protocol: 'vless',
        host: '203.0.113.11',
        port: 24431,
        server_name: 'www.microsoft.com',
        public_key: 'E2ETestPublicKey123456789012345678901234567',
        short_id: 'dcba4321',
        fingerprint: 'chrome',
        flow: 'xtls-rprx-vision',
        line_mode: 'direct_and_relay',
        agent_base_url: 'http://203.0.113.11:18080',
        sort_weight: 901,
        is_enabled: true,
      },
    })
    expect(updatedNode.name).toBe(`${prefix}-node-updated`)

    const group = await api(request, adminToken, 'post', '/api/admin/node-groups', {
      data: { name: `${prefix}-group`, description: 'playwright crud group' },
    })
    created.groupId = group.id

    const updatedGroup = await api(request, adminToken, 'put', `/api/admin/node-groups/${created.groupId}`, {
      data: { name: `${prefix}-group-updated`, description: 'playwright crud group updated' },
    })
    expect(updatedGroup.name).toBe(`${prefix}-group-updated`)

    const boundNodes = await api(request, adminToken, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
      data: { node_ids: [created.nodeId] },
    })
    expect(boundNodes.node_ids.map(String)).toContain(String(created.nodeId))

    const groupNodes = await api(request, adminToken, 'get', `/api/admin/node-groups/${created.groupId}/nodes`)
    expect(groupNodes.nodes.map((item) => String(item.id))).toContain(String(created.nodeId))

    const planCrud = await api(request, adminToken, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan-crud`,
        price: 1.23,
        currency: 'USDT',
        traffic_limit: gb(5),
        duration_days: 7,
        sort_weight: 901,
        is_active: true,
      },
    })
    created.planCrudId = planCrud.id

    const updatedPlanCrud = await api(request, adminToken, 'put', `/api/admin/plans/${created.planCrudId}`, {
      data: {
        name: `${prefix}-plan-crud-updated`,
        price: 2.34,
        currency: 'USDT',
        traffic_limit: gb(6),
        duration_days: 8,
        sort_weight: 902,
        is_active: false,
      },
    })
    expect(updatedPlanCrud.name).toBe(`${prefix}-plan-crud-updated`)

    const planGroupBinding = await api(request, adminToken, 'post', `/api/admin/plans/${created.planCrudId}/node-groups`, {
      data: { node_group_ids: [created.groupId] },
    })
    expect(planGroupBinding.node_group_ids.map(String)).toContain(String(created.groupId))

    const planGroups = await api(request, adminToken, 'get', `/api/admin/plans/${created.planCrudId}/node-groups`)
    expect(planGroups.node_group_ids.map(String)).toContain(String(created.groupId))

    const relay = await api(request, adminToken, 'post', '/api/admin/relays', {
      data: {
        name: `${prefix}-relay`,
        host: '198.51.100.10',
        forwarder_type: 'haproxy',
        agent_base_url: 'http://198.51.100.10:18080',
        agent_token: `relay-token-${suffix}`,
        is_enabled: true,
      },
    })
    created.relayId = relay.id

    const updatedRelay = await api(request, adminToken, 'put', `/api/admin/relays/${created.relayId}`, {
      data: {
        name: `${prefix}-relay-updated`,
        host: '198.51.100.11',
        forwarder_type: 'haproxy',
        agent_base_url: 'http://198.51.100.11:18080',
        is_enabled: true,
      },
    })
    expect(updatedRelay.name).toBe(`${prefix}-relay-updated`)

    const listenPort = 30000 + Math.floor(Math.random() * 10000)
    const relayBackends = await api(request, adminToken, 'put', `/api/admin/relays/${created.relayId}/backends`, {
      data: {
        backends: [{
          name: `${prefix}-relay-backend`,
          exit_node_id: created.nodeId,
          listen_port: listenPort,
          target_host: '203.0.113.11',
          target_port: 24431,
          sort_weight: 1,
          is_enabled: true,
        }],
      },
    })
    expect(relayBackends.backends).toHaveLength(1)
    expect(relayBackends.backends[0].listen_port).toBe(listenPort)

    const relaysList = await api(request, adminToken, 'get', '/api/admin/relays')
    expect(byId(relaysList.relays, created.relayId)?.backends?.[0]?.listen_port).toBe(listenPort)

    const planFlow = await api(request, adminToken, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan-flow`,
        price: 3.45,
        currency: 'USDT',
        traffic_limit: gb(8),
        duration_days: 15,
        sort_weight: 903,
        is_active: true,
      },
    })
    created.planFlowId = planFlow.id
    await api(request, adminToken, 'post', `/api/admin/plans/${created.planFlowId}/node-groups`, {
      data: { node_group_ids: [created.groupId] },
    })

    const username = `e2euser${suffix.replace(/-/g, '')}`.slice(0, 31)
    const user = await api(request, adminToken, 'post', '/api/admin/users', {
      data: {
        username,
        email: `${username}@example.test`,
        password: flowPassword,
        status: 'active',
        is_admin: false,
      },
    })
    created.userId = user.id
    expect(user.username).toBe(username)

    const searchedUsers = await api(request, adminToken, 'get', '/api/admin/users', {
      params: { keyword: username, page: 1, size: 20 },
    })
    expect(searchedUsers.users.map((item) => item.username)).toContain(username)

    await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/status`, {
      data: { status: 'disabled' },
    })
    await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/status`, {
      data: { status: 'active' },
    })
    await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/password`, {
      data: { new_password: flowPasswordReset },
    })

    const { token: userToken } = await login(request, username, flowPasswordReset)

    const orderData = await api(request, userToken, 'post', '/api/orders', {
      data: { plan_id: created.planFlowId },
    })
    expect(orderData.order?.plan_id).toBe(created.planFlowId)

    const userOrders = await api(request, userToken, 'get', '/api/user/orders', {
      params: { page: 1, size: 20 },
    })
    expect(userOrders.orders.map((item) => String(item.id))).toContain(String(orderData.order.id))

    const adminOrders = await api(request, adminToken, 'get', '/api/admin/orders', {
      params: { page: 1, size: 50 },
    })
    expect(adminOrders.orders.map((item) => String(item.id))).toContain(String(orderData.order.id))

    const generatedCodes = await api(request, adminToken, 'post', '/api/admin/redeem-codes', {
      data: { plan_id: created.planFlowId, duration_days: 3, count: 1 },
    })
    expect(generatedCodes.codes).toHaveLength(1)

    const codesList = await api(request, adminToken, 'get', '/api/admin/redeem-codes', {
      params: { page: 1, size: 20 },
    })
    expect(codesList.codes.map((item) => item.code)).toContain(generatedCodes.codes[0])

    await api(request, userToken, 'post', '/api/redeem', {
      data: { code: generatedCodes.codes[0] },
    })

    const userSubscription = await api(request, userToken, 'get', '/api/user/subscription')
    expect(userSubscription.subscription?.status).toBe('ACTIVE')

    const updatedSubscription = await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
      data: {
        plan_id: created.planFlowId,
        status: 'ACTIVE',
        expire_date: futureISO(30),
        traffic_limit: gb(9),
        used_traffic: 12345,
      },
    })
    expect(updatedSubscription.subscription?.plan_id).toBe(created.planFlowId)
    expect(updatedSubscription.tokens?.length).toBeGreaterThan(0)

    const subscriptionData = await api(request, adminToken, 'get', `/api/admin/users/${created.userId}/subscription`)
    expect(subscriptionData.subscription?.status).toBe('ACTIVE')
    expect(subscriptionData.tokens?.length).toBeGreaterThan(0)
    created.tokenId = subscriptionData.tokens[0].id

    const usageData = await api(request, adminToken, 'get', `/api/admin/users/${created.userId}/usage`, {
      params: { days: 7, weeks: 4, months: 3, recent: 10 },
    })
    expect(usageData.has_active_subscription).toBe(true)
    expect(usageData.plan_name).toBe(`${prefix}-plan-flow`)
    expect(usageData.daily).toHaveLength(7)
    expect(usageData.weekly).toHaveLength(4)
    expect(usageData.monthly).toHaveLength(3)

    const userListWithPlan = await api(request, adminToken, 'get', '/api/admin/users', {
      params: { keyword: username, page: 1, size: 20 },
    })
    const userRow = userListWithPlan.users.find((item) => item.username === username)
    expect(userRow?.plan_name).toBe(`${prefix}-plan-flow`)
    expect(userRow?.has_active_subscription).toBe(true)
    expect(userRow?.remaining_traffic).toBe(gb(9) - 12345)

    const resetTokenData = await api(request, adminToken, 'post', `/api/admin/subscription-tokens/${created.tokenId}/reset`, {
      data: {},
    })
    expect(resetTokenData.id).toBe(created.tokenId)
    expect(resetTokenData.token).toBeTruthy()

    const tokenList = await api(request, adminToken, 'get', '/api/admin/subscription-tokens', {
      params: { page: 1, size: 100 },
    })
    const tokenRow = byId(tokenList.tokens, created.tokenId)
    expect(tokenRow?.username).toBe(username)
    expect(tokenRow?.has_active_subscription).toBe(true)
    expect(tokenRow?.plan_name).toBe(`${prefix}-plan-flow`)
    expect(tokenRow?.subscription_status).toBe('ACTIVE')
    expect(tokenRow?.token_status).toBe('ACTIVE')

    const subPlain = await request.get(`/sub/${resetTokenData.token}/plain`)
    expect(subPlain.ok(), 'download plain subscription after token reset').toBeTruthy()
    const subText = await subPlain.text()
    expect(subText).toContain(`${prefix}-node-updated`)
    expect(subText).toContain(`${prefix}-relay-backend`)

    await api(request, adminToken, 'post', `/api/admin/subscription-tokens/${created.tokenId}/revoke`, {
      data: {},
    })

    const revokedDownload = await request.get(`/sub/${resetTokenData.token}/plain`)
    expect(revokedDownload.ok(), 'revoked token should not download subscription').toBeFalsy()
  } finally {
    if (created.tokenId) {
      await safeApi(request, adminToken, 'post', `/api/admin/subscription-tokens/${created.tokenId}/revoke`, { data: {} })
    }
    if (created.userId && created.planFlowId) {
      await safeApi(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
        data: {
          plan_id: created.planFlowId,
          status: 'EXPIRED',
          expire_date: new Date(Date.now() - 60_000).toISOString(),
          traffic_limit: gb(1),
          used_traffic: 0,
        },
      })
    }
    if (created.userId) {
      await safeApi(request, adminToken, 'put', `/api/admin/users/${created.userId}/status`, {
        data: { status: 'disabled' },
      })
    }
    if (created.relayId) {
      await safeApi(request, adminToken, 'put', `/api/admin/relays/${created.relayId}/backends`, {
        data: { backends: [] },
      })
      await safeApi(request, adminToken, 'delete', `/api/admin/relays/${created.relayId}`)
    }
    if (created.planCrudId) {
      await safeApi(request, adminToken, 'post', `/api/admin/plans/${created.planCrudId}/node-groups`, {
        data: { node_group_ids: [] },
      })
      await safeApi(request, adminToken, 'delete', `/api/admin/plans/${created.planCrudId}`)
    }
    if (created.planFlowId) {
      await safeApi(request, adminToken, 'post', `/api/admin/plans/${created.planFlowId}/node-groups`, {
        data: { node_group_ids: [] },
      })
      await safeApi(request, adminToken, 'put', `/api/admin/plans/${created.planFlowId}`, {
        data: {
          name: `${prefix}-plan-flow-disabled`,
          price: 3.45,
          currency: 'USDT',
          traffic_limit: gb(8),
          duration_days: 15,
          sort_weight: 999,
          is_active: false,
        },
      })
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
