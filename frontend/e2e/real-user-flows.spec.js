import { expect, request as apiRequest, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD
const subscriptionProfileName = process.env.E2E_SUBSCRIPTION_PROFILE_NAME || 'RayPilot'
const csrfHeader = { 'X-CSRF-Token': 'suiyue-web' }

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for real user flow e2e tests`)
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

async function newApiActor(baseURL, username, password) {
  const request = await apiRequest.newContext({ baseURL })
  const session = await login(request, username, password)
  return {
    request,
    token: session.token,
    user: session.user,
  }
}

async function installGuards(page) {
  const consoleErrors = []
  const failedRequests = []
  const badResponses = []

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      consoleErrors.push(msg.text())
    }
  })

  page.on('requestfailed', (request) => {
    const url = request.url()
    if (!url.includes('/favicon.ico')) {
      failedRequests.push(`${request.method()} ${url} ${request.failure()?.errorText || ''}`)
    }
  })

  page.on('response', (response) => {
    const url = response.url()
    const status = response.status()
    if (status >= 500 && !url.includes('/favicon.ico')) {
      badResponses.push(`${status} ${url}`)
    }
  })

  return () => {
    expect(consoleErrors, 'browser console errors').toEqual([])
    expect(failedRequests, 'failed browser requests').toEqual([])
    expect(badResponses, '5xx browser responses').toEqual([])
  }
}

function gb(value) {
  return value * 1024 * 1024 * 1024
}

function uniqueSuffix() {
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`
}

async function registerAndLogin(page, user) {
  await page.goto('/register')
  await expect(page.getByRole('heading', { name: '用户注册' })).toBeVisible()
  await page.getByPlaceholder('用户名').fill(user.username)
  await page.getByPlaceholder('邮箱（可选）').fill(user.email)
  await page.getByPlaceholder('密码', { exact: true }).fill(user.password)
  await page.getByPlaceholder('确认密码').fill(user.password)
  await page.getByRole('button', { name: '注册' }).click()
  await expect(page).toHaveURL(/\/login$/)

  await expect(page.getByRole('heading', { name: '用户登录' })).toBeVisible()
  await page.getByPlaceholder('用户名').fill(user.username)
  await page.getByPlaceholder('密码', { exact: true }).fill(user.password)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByText(user.username).first()).toBeVisible()
}

async function findUserByUsername(request, adminToken, username) {
  const data = await api(request, adminToken, 'get', '/api/admin/users', {
    params: { keyword: username, page: 1, size: 20 },
  })
  return data.users.find((item) => item.username === username) || null
}

async function cleanupUser(request, adminToken, username, knownId) {
  if (knownId) {
    await safeApi(request, adminToken, 'delete', `/api/admin/users/${knownId}`)
    return
  }
  const user = await findUserByUsername(request, adminToken, username).catch(() => null)
  if (user?.id) {
    await safeApi(request, adminToken, 'delete', `/api/admin/users/${user.id}`)
  }
}

async function expectSubscriptionDownload(request, url, format, nodeName, expectedTransport = 'tcp') {
  const response = await request.get(url)
  expect(response.ok(), `${format} subscription download`).toBeTruthy()
  const expectedFilename = `${subscriptionProfileName}.${format === 'clash' ? 'yaml' : 'txt'}`
  expect(response.headers()['content-disposition'] || '', `${format} content disposition`).toContain(expectedFilename)
  const text = await response.text()
  const content = format === 'base64' ? Buffer.from(text, 'base64').toString('utf8') : text
  expect(content, `${format} subscription contains the test node`).toContain(nodeName)
  expect(content, `${format} subscription contains vless data`).toContain(format === 'clash' ? 'vless' : 'vless://')
  if (expectedTransport === 'xhttp') {
    if (format === 'plain' || format === 'base64') {
      expect(content, `${format} subscription uses xhttp URI type`).toContain('type=xhttp')
      expect(content, `${format} subscription omits vision flow for xhttp`).not.toContain('flow=')
    } else {
      expect(content, `${format} subscription uses xhttp network`).toContain('network: xhttp')
      expect(content, `${format} subscription includes xhttp opts`).toContain('xhttp-opts:')
      expect(content, `${format} subscription omits vision flow for xhttp`).not.toContain('flow:')
    }
  }
}

async function getSelectedSubscriptionUrl(page) {
  return (await page.locator('.subscription-page .link-url').textContent()).trim()
}

test('real user flows cover signup, plans, orders, redeem, subscriptions, profile, and user isolation', async ({ browser, baseURL }) => {
  test.setTimeout(120_000)
  requireEnv('E2E_ADMIN_USERNAME', adminUsername)
  requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

  const actorBaseURL = baseURL || 'http://127.0.0.1:7000'
  const suffix = uniqueSuffix()
  const prefix = `real-user-flow-${suffix}`
  const compactSuffix = suffix.replace(/[^a-z0-9]/gi, '').slice(0, 18)
  const userA = {
    username: `rufa${compactSuffix}`.slice(0, 32),
    email: `rufa-${suffix}@example.test`,
    password: `PwA-${suffix}`,
  }
  const userB = {
    username: `rufb${compactSuffix}`.slice(0, 32),
    email: `rufb-${suffix}@example.test`,
    password: `PwB-${suffix}`,
  }
  const profileEmail = `profile-${suffix}@example.test`

  let adminActor
  let userAActor
  let userBActor
  let contextA
  let contextB
  let assertNoRuntimeFailuresA = () => {}
  let assertNoRuntimeFailuresB = () => {}

  const created = {
    nodeId: null,
    groupId: null,
    planAId: null,
    planBId: null,
    userAId: null,
    userBId: null,
  }

  try {
    adminActor = await newApiActor(actorBaseURL, adminUsername, adminPassword)

    const node = await api(adminActor.request, adminActor.token, 'post', '/api/admin/nodes', {
      data: {
        name: `${prefix}-node`,
        protocol: 'vless',
        host: '203.0.113.88',
        port: 24488,
        transport: 'xhttp',
        xhttp_path: '/real-user-xhttp',
        xhttp_mode: 'stream-up',
        xhttp_host: 'cdn.example.test',
        server_name: 'www.microsoft.com',
        public_key: 'E2ERealUserPublicKey123456789012345678901234',
        short_id: '8a8b8c8d',
        fingerprint: 'chrome',
        line_mode: 'direct_only',
        agent_base_url: 'http://203.0.113.88:18080',
        agent_token: `agent-token-${suffix}`,
        sort_weight: 960,
        is_enabled: true,
      },
    })
    created.nodeId = node.id

    const group = await api(adminActor.request, adminActor.token, 'post', '/api/admin/node-groups', {
      data: { name: `${prefix}-group`, description: 'playwright real user flow group' },
    })
    created.groupId = group.id
    await api(adminActor.request, adminActor.token, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
      data: { node_ids: [created.nodeId] },
    })

    const planA = await api(adminActor.request, adminActor.token, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan-a`,
        price: 1.11,
        currency: 'USDT',
        traffic_limit: gb(7),
        duration_days: 9,
        sort_weight: 961,
        is_active: true,
      },
    })
    created.planAId = planA.id
    await api(adminActor.request, adminActor.token, 'post', `/api/admin/plans/${created.planAId}/node-groups`, {
      data: { node_group_ids: [created.groupId] },
    })

    const planB = await api(adminActor.request, adminActor.token, 'post', '/api/admin/plans', {
      data: {
        name: `${prefix}-plan-b`,
        price: 2.22,
        currency: 'USDT',
        traffic_limit: gb(8),
        duration_days: 11,
        sort_weight: 962,
        is_active: true,
      },
    })
    created.planBId = planB.id
    await api(adminActor.request, adminActor.token, 'post', `/api/admin/plans/${created.planBId}/node-groups`, {
      data: { node_group_ids: [created.groupId] },
    })

    const redeemA = await api(adminActor.request, adminActor.token, 'post', '/api/admin/redeem-codes', {
      data: { plan_id: created.planAId, duration_days: 9, count: 1 },
    })
    const redeemB = await api(adminActor.request, adminActor.token, 'post', '/api/admin/redeem-codes', {
      data: { plan_id: created.planBId, duration_days: 11, count: 1 },
    })
    expect(redeemA.codes).toHaveLength(1)
    expect(redeemB.codes).toHaveLength(1)

    contextA = await browser.newContext()
    contextB = await browser.newContext()
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    assertNoRuntimeFailuresA = await installGuards(pageA)
    assertNoRuntimeFailuresB = await installGuards(pageB)

    await registerAndLogin(pageA, userA)
    await registerAndLogin(pageB, userB)

    const adminUserA = await findUserByUsername(adminActor.request, adminActor.token, userA.username)
    const adminUserB = await findUserByUsername(adminActor.request, adminActor.token, userB.username)
    expect(adminUserA?.id, 'registered user A is visible to admin').toBeTruthy()
    expect(adminUserB?.id, 'registered user B is visible to admin').toBeTruthy()
    created.userAId = adminUserA.id
    created.userBId = adminUserB.id

    userAActor = await newApiActor(actorBaseURL, userA.username, userA.password)
    userBActor = await newApiActor(actorBaseURL, userB.username, userB.password)

    await pageA.goto('/plans')
    await pageA.waitForLoadState('networkidle')
    await expect(pageA.getByRole('heading', { name: '套餐列表' })).toBeVisible()
    const planACard = pageA.locator('.plan-card').filter({ hasText: planA.name }).first()
    const planBCard = pageA.locator('.plan-card').filter({ hasText: planB.name }).first()
    await expect(planACard).toBeVisible()
    await expect(planBCard).toBeVisible()
    await planACard.getByRole('button', { name: '立即购买' }).click()
    await expect(pageA.getByText(`确定购买套餐"${planA.name}"吗？`).first()).toBeVisible()
    await pageA.getByRole('button', { name: '取消' }).click()

    const orderAData = await api(userAActor.request, userAActor.token, 'post', '/api/orders', {
      data: { plan_id: created.planAId },
    })
    const orderBData = await api(userBActor.request, userBActor.token, 'post', '/api/orders', {
      data: { plan_id: created.planBId },
    })
    const orderA = orderAData.order
    const orderB = orderBData.order
    expect(orderA.status).toBe('PENDING')
    expect(orderB.status).toBe('PENDING')
    expect(orderA.user_id).toBe(created.userAId)
    expect(orderB.user_id).toBe(created.userBId)

    await pageA.goto('/orders')
    await pageA.waitForLoadState('networkidle')
    await expect(pageA.getByRole('heading', { name: '我的订单' })).toBeVisible()
    await expect(pageA.getByText(orderA.order_no)).toBeVisible()
    await expect(pageA.getByText(orderB.order_no)).toHaveCount(0)

    await pageB.goto('/orders')
    await pageB.waitForLoadState('networkidle')
    await expect(pageB.getByRole('heading', { name: '我的订单' })).toBeVisible()
    await expect(pageB.getByText(orderB.order_no)).toBeVisible()
    await expect(pageB.getByText(orderA.order_no)).toHaveCount(0)

    await pageA.goto('/redeem')
    await pageA.waitForLoadState('networkidle')
    await pageA.getByPlaceholder('请输入兑换码').fill(redeemA.codes[0])
    await pageA.getByRole('button', { name: '立即兑换' }).click()
    await expect(pageA.getByText('兑换成功！订阅已开通。')).toBeVisible()

    await pageB.goto('/redeem')
    await pageB.waitForLoadState('networkidle')
    await pageB.getByPlaceholder('请输入兑换码').fill(redeemB.codes[0])
    await pageB.getByRole('button', { name: '立即兑换' }).click()
    await expect(pageB.getByText('兑换成功！订阅已开通。')).toBeVisible()

    const subscriptionAData = await api(userAActor.request, userAActor.token, 'get', '/api/user/subscription')
    const subscriptionBData = await api(userBActor.request, userBActor.token, 'get', '/api/user/subscription')
    const subscriptionA = subscriptionAData.subscription
    const subscriptionB = subscriptionBData.subscription
    expect(subscriptionA.status).toBe('ACTIVE')
    expect(subscriptionB.status).toBe('ACTIVE')
    expect(subscriptionA.plan_id).toBe(created.planAId)
    expect(subscriptionB.plan_id).toBe(created.planBId)
    expect(subscriptionA.tokens).toHaveLength(1)
    expect(subscriptionB.tokens).toHaveLength(1)
    expect(subscriptionA.tokens[0] === subscriptionB.tokens[0], 'users have isolated subscription tokens').toBe(false)

    await pageA.goto('/subscription')
    await pageA.waitForLoadState('networkidle')
    await expect(pageA.getByRole('heading', { name: '我的订阅' })).toBeVisible()
    await expect(pageA.getByText('订阅信息')).toBeVisible()
    await expect(pageA.getByText('ACTIVE').first()).toBeVisible()
    await expect(pageA.locator('.subscription-page .link-url')).toContainText('/clash')
    const clashUrl = await getSelectedSubscriptionUrl(pageA)

    await pageA.locator('.subscription-page .format-selector').getByText('Base64').click()
    await expect(pageA.locator('.subscription-page .link-url')).toContainText('/base64')
    const base64Url = await getSelectedSubscriptionUrl(pageA)

    await pageA.locator('.subscription-page .format-selector').getByText('URI').click()
    await expect(pageA.locator('.subscription-page .link-url')).toContainText('/plain')
    const plainUrl = await getSelectedSubscriptionUrl(pageA)

    const aLinkText = await pageA.locator('.subscription-page .link-url').textContent()
    expect(aLinkText.includes(subscriptionB.tokens[0]), 'user A page must not expose user B subscription token').toBe(false)
    await expectSubscriptionDownload(contextA.request, clashUrl, 'clash', node.name, 'xhttp')
    await expectSubscriptionDownload(contextA.request, base64Url, 'base64', node.name, 'xhttp')
    await expectSubscriptionDownload(contextA.request, plainUrl, 'plain', node.name, 'xhttp')

    await pageA.goto('/profile')
    await pageA.waitForLoadState('networkidle')
    await expect(pageA.getByRole('heading', { name: '个人资料' })).toBeVisible()
    await expect(pageA.locator('.user-profile input').first()).toHaveValue(userA.username)
    await pageA.getByPlaceholder('可选').fill(profileEmail)
    await pageA.getByRole('button', { name: '保存' }).click()
    await expect(pageA.getByText('资料已更新')).toBeVisible()
    await expect(pageA.locator('.user-profile input').first()).toHaveValue(userA.username)
    const profileData = await api(userAActor.request, userAActor.token, 'get', '/api/user/me')
    expect(profileData.user.email).toBe(profileEmail)

    const userAOrders = await api(userAActor.request, userAActor.token, 'get', '/api/user/orders', {
      params: { page: 1, size: 20 },
    })
    const userBOrders = await api(userBActor.request, userBActor.token, 'get', '/api/user/orders', {
      params: { page: 1, size: 20 },
    })
    expect(userAOrders.orders.map((item) => item.order_no)).toContain(orderA.order_no)
    expect(userAOrders.orders.map((item) => item.order_no)).not.toContain(orderB.order_no)
    expect(userBOrders.orders.map((item) => item.order_no)).toContain(orderB.order_no)
    expect(userBOrders.orders.map((item) => item.order_no)).not.toContain(orderA.order_no)

    assertNoRuntimeFailuresA()
    assertNoRuntimeFailuresB()
  } finally {
    if (adminActor?.token) {
      await cleanupUser(adminActor.request, adminActor.token, userA.username, created.userAId)
      await cleanupUser(adminActor.request, adminActor.token, userB.username, created.userBId)
      if (created.planAId) {
        await safeApi(adminActor.request, adminActor.token, 'post', `/api/admin/plans/${created.planAId}/node-groups`, {
          data: { node_group_ids: [] },
        })
        await safeApi(adminActor.request, adminActor.token, 'delete', `/api/admin/plans/${created.planAId}`)
      }
      if (created.planBId) {
        await safeApi(adminActor.request, adminActor.token, 'post', `/api/admin/plans/${created.planBId}/node-groups`, {
          data: { node_group_ids: [] },
        })
        await safeApi(adminActor.request, adminActor.token, 'delete', `/api/admin/plans/${created.planBId}`)
      }
      if (created.groupId) {
        await safeApi(adminActor.request, adminActor.token, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
          data: { node_ids: [] },
        })
        await safeApi(adminActor.request, adminActor.token, 'delete', `/api/admin/node-groups/${created.groupId}`)
      }
      if (created.nodeId) {
        await safeApi(adminActor.request, adminActor.token, 'delete', `/api/admin/nodes/${created.nodeId}`)
      }
    }
    await contextA?.close()
    await contextB?.close()
    await userAActor?.request.dispose()
    await userBActor?.request.dispose()
    await adminActor?.request.dispose()
  }
})
