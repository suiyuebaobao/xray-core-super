import { expect, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD
const csrfHeader = { 'X-CSRF-Token': 'suiyue-web' }

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for admin ops e2e tests`)
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

async function loginAdminUI(page) {
  await page.goto('/admin/login')
  await expect(page.getByRole('heading', { name: '管理后台登录' })).toBeVisible()
  await page.getByPlaceholder('管理员用户名').fill(adminUsername)
  await page.getByPlaceholder('密码').fill(adminPassword)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/admin\/?$/)
  await expect(page.getByRole('heading', { name: '管理后台仪表盘' })).toBeVisible()
  await expect(page.getByText('用户总数')).toBeVisible()
}

async function gotoAdminPage(page, path, heading) {
  await page.goto(path)
  await page.waitForLoadState('networkidle')
  await expect(page.getByText(heading).first()).toBeVisible()
}

function activeDialog(page, title) {
  return page.locator('.el-dialog:visible').filter({ hasText: title }).first()
}

function formItem(scope, label) {
  return scope.locator('.el-form-item').filter({ hasText: label }).first()
}

async function fillFormField(scope, label, value) {
  const item = formItem(scope, label)
  await expect(item, `form item ${label}`).toBeVisible()
  await item.locator('input, textarea').first().fill(String(value))
}

async function selectFormOption(page, scope, label, optionText) {
  const item = formItem(scope, label)
  await expect(item, `select ${label}`).toBeVisible()
  await item.locator('.el-select').click()
  const option = page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: optionText }).first()
  await expect(option, `option ${optionText}`).toBeVisible()
  await option.click()
}

async function selectDropdownOption(page, optionText) {
  const option = page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: optionText }).first()
  await expect(option, `option ${optionText}`).toBeVisible()
  await option.click()
}

function tableRow(page, rootSelector, text) {
  return page.locator(`${rootSelector} .el-table__body-wrapper tbody tr`).filter({ hasText: text }).first()
}

async function findByName(request, token, url, collectionKey, name) {
  const data = await api(request, token, 'get', url)
  const item = (data[collectionKey] || []).find((entry) => entry.name === name)
  expect(item, `${name} should exist in ${url}`).toBeTruthy()
  return item
}

async function findUserByUsername(request, token, username) {
  const data = await api(request, token, 'get', '/api/admin/users', {
    params: { keyword: username, page: 1, size: 20 },
  })
  const user = (data.users || []).find((entry) => entry.username === username)
  expect(user, `${username} should exist in user list`).toBeTruthy()
  return user
}

async function findTokenPageForUser(request, token, userId) {
  let pageNo = 1
  for (;;) {
    const data = await api(request, token, 'get', '/api/admin/subscription-tokens', {
      params: { page: pageNo, size: 20 },
    })
    const found = (data.tokens || []).find((entry) => String(entry.user_id) === String(userId))
    if (found) {
      return { pageNo, token: found }
    }
    if (pageNo * 20 >= (data.total || 0)) {
      break
    }
    pageNo += 1
  }
  throw new Error(`subscription token for user ${userId} was not found`)
}

async function goToTokenPage(page, pageNo) {
  for (let current = 1; current < pageNo; current += 1) {
    const nextButton = page.locator('.admin-subscription-tokens .el-pagination button.btn-next')
    await expect(nextButton).toBeEnabled()
    await nextButton.click()
    await page.waitForLoadState('networkidle')
  }
}

async function waitForNewRedeemCode(request, token, beforeIds, planId) {
  for (let attempt = 0; attempt < 12; attempt += 1) {
    const data = await api(request, token, 'get', '/api/admin/redeem-codes', {
      params: { page: 1, size: 50 },
    })
    const code = (data.codes || []).find((item) => (
      !beforeIds.has(String(item.id))
      && String(item.plan_id) === String(planId)
      && Number(item.duration_days) === 3
    ))
    if (code) return code
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error('generated redeem code did not appear in admin list')
}

async function cleanupOpsData(request, token, created, planCleanupPayload) {
  if (created.tokenId) {
    await safeApi(request, token, 'post', `/api/admin/subscription-tokens/${created.tokenId}/revoke`, { data: {} })
  }
  if (created.userId) {
    await safeApi(request, token, 'put', `/api/admin/users/${created.userId}/status`, {
      data: { status: 'disabled' },
    })
    await safeApi(request, token, 'delete', `/api/admin/users/${created.userId}`)
  }
  if (created.relayId) {
    await safeApi(request, token, 'put', `/api/admin/relays/${created.relayId}/backends`, {
      data: { backends: [] },
    })
    await safeApi(request, token, 'delete', `/api/admin/relays/${created.relayId}`)
  }
  if (created.planId) {
    await safeApi(request, token, 'post', `/api/admin/plans/${created.planId}/node-groups`, {
      data: { node_group_ids: [] },
    })
    const deleted = await safeApi(request, token, 'delete', `/api/admin/plans/${created.planId}`)
    if (!deleted && planCleanupPayload) {
      await safeApi(request, token, 'put', `/api/admin/plans/${created.planId}`, {
        data: { ...planCleanupPayload, is_active: false },
      })
    }
  }
  if (created.groupId) {
    await safeApi(request, token, 'put', `/api/admin/node-groups/${created.groupId}/nodes`, {
      data: { node_ids: [] },
    })
    await safeApi(request, token, 'delete', `/api/admin/node-groups/${created.groupId}`)
  }
  if (created.nodeId) {
    await safeApi(request, token, 'delete', `/api/admin/nodes/${created.nodeId}`)
  }
}

test.describe('admin operations flow', () => {
  test.setTimeout(150_000)

  test('covers the core admin operations path without touching production nodes', async ({ page, request }) => {
    requireEnv('E2E_ADMIN_USERNAME', adminUsername)
    requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

    const assertNoRuntimeFailures = await installGuards(page)
    const { token: adminToken } = await login(request, adminUsername, adminPassword)
    const suffix = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`
    const prefix = `e2e-ops-${suffix}`
    const username = `e2eops${suffix}`.slice(0, 32)
    const userPassword = `Pw-${suffix}-1`
    const listenPort = 32000 + Math.floor(Math.random() * 2000)

    const created = {
      nodeId: null,
      groupId: null,
      planId: null,
      relayId: null,
      userId: null,
      tokenId: null,
    }
    let nodeName = `${prefix}-node`
    let groupName = `${prefix}-group`
    let planName = `${prefix}-plan`
    let relayName = `${prefix}-relay`
    const planCleanupPayload = {
      name: `${prefix}-plan-disabled`,
      price: 4.56,
      currency: 'USDT',
      traffic_limit: gb(11),
      duration_days: 31,
      sort_weight: 980,
    }

    try {
      await test.step('admin login and dashboard', async () => {
        await loginAdminUI(page)
        await expect(page.getByText('节点总数')).toBeVisible()
        await expect(page.getByText('套餐总数')).toBeVisible()
        await expect(page.getByText('有效订阅')).toBeVisible()
        await expect(page.getByText('快捷操作')).toBeVisible()
      })

      await test.step('node CRUD and deploy multi-IP visibility', async () => {
        await gotoAdminPage(page, '/admin/nodes', '出口节点管理')
        await page.getByRole('button', { name: '新增节点' }).click()
        let dialog = activeDialog(page, '新增节点')
        await fillFormField(dialog, '节点名称', nodeName)
        await fillFormField(dialog, '地址', '203.0.113.20')
        await fillFormField(dialog, '端口', 24430)
        await fillFormField(dialog, 'Server Name', 'www.microsoft.com')
        await fillFormField(dialog, 'Public Key', 'E2ETestPublicKey123456789012345678901234567')
        await fillFormField(dialog, 'Short ID', 'abcd1234')
        await selectFormOption(page, dialog, '线路输出', '直连 + 中转')
        await fillFormField(dialog, 'Agent 地址', 'http://203.0.113.20:18080')
        await fillFormField(dialog, 'Agent Token', `node-token-${suffix}`)
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-nodes', nodeName)).toBeVisible()
        created.nodeId = (await findByName(request, adminToken, '/api/admin/nodes', 'nodes', nodeName)).id

        await tableRow(page, '.admin-nodes', nodeName).getByRole('button', { name: '编辑' }).click()
        dialog = activeDialog(page, '编辑节点')
        nodeName = `${prefix}-node-updated`
        await fillFormField(dialog, '节点名称', nodeName)
        await fillFormField(dialog, '地址', '203.0.113.21')
        await fillFormField(dialog, '端口', 24431)
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-nodes', nodeName)).toBeVisible()

        await page.getByRole('button', { name: '一键部署' }).click()
        dialog = activeDialog(page, '一键部署节点')
        await expect(dialog.getByText('多 IP 服务器')).toBeVisible()
        await expect(dialog.getByRole('button', { name: '扫描出口 IP' })).toHaveCount(0)
        await formItem(dialog, '多 IP 服务器').locator('.el-switch').click()
        await expect(dialog.getByRole('button', { name: '扫描出口 IP' })).toBeVisible()
        await dialog.getByRole('button', { name: '取消' }).click()
      })

      await test.step('node group binding', async () => {
        await gotoAdminPage(page, '/admin/node-groups', '节点分组管理')
        await page.getByRole('button', { name: '新增分组' }).click()
        let dialog = activeDialog(page, '新增分组')
        await fillFormField(dialog, '分组名称', groupName)
        await fillFormField(dialog, '描述', 'playwright admin ops group')
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-node-groups', groupName)).toBeVisible()
        created.groupId = (await findByName(request, adminToken, '/api/admin/node-groups', 'groups', groupName)).id

        await tableRow(page, '.admin-node-groups', groupName).getByRole('button', { name: '编辑' }).click()
        dialog = activeDialog(page, '编辑分组')
        groupName = `${prefix}-group-updated`
        await fillFormField(dialog, '分组名称', groupName)
        await fillFormField(dialog, '描述', 'playwright admin ops group updated')
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-node-groups', groupName)).toBeVisible()

        await tableRow(page, '.admin-node-groups', groupName).getByRole('button', { name: '管理节点' }).click()
        dialog = activeDialog(page, '管理节点')
        await expect(dialog.getByText(nodeName)).toBeVisible()
        await dialog.locator('.el-table__body-wrapper tbody tr').filter({ hasText: nodeName }).first().locator('.el-checkbox__inner').click()
        await expect(dialog.getByText('已选择 1 个节点')).toBeVisible()
        await dialog.getByRole('button', { name: '保存绑定' }).click()
        await expect(tableRow(page, '.admin-node-groups', groupName)).toContainText(nodeName)
      })

      await test.step('plan CRUD and node group binding', async () => {
        await gotoAdminPage(page, '/admin/plans', '套餐管理')
        await page.getByRole('button', { name: '新增套餐' }).click()
        let dialog = activeDialog(page, '新增套餐')
        await fillFormField(dialog, '套餐名称', planName)
        await fillFormField(dialog, '价格', 3.45)
        await fillFormField(dialog, '流量（GB）', 10)
        await fillFormField(dialog, '时长（天）', 30)
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-plans', planName)).toBeVisible()
        created.planId = (await findByName(request, adminToken, '/api/admin/plans', 'plans', planName)).id

        await tableRow(page, '.admin-plans', planName).getByRole('button', { name: '编辑' }).click()
        dialog = activeDialog(page, '编辑套餐')
        planName = `${prefix}-plan-updated`
        await fillFormField(dialog, '套餐名称', planName)
        await fillFormField(dialog, '价格', 4.56)
        await fillFormField(dialog, '流量（GB）', 11)
        await fillFormField(dialog, '时长（天）', 31)
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-plans', planName)).toBeVisible()

        await tableRow(page, '.admin-plans', planName).getByRole('button', { name: '管理' }).click()
        dialog = activeDialog(page, '管理节点分组')
        await dialog.locator('.el-select').click()
        await selectDropdownOption(page, groupName)
        await dialog.locator('.el-dialog__header').click()
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-plans', planName)).toContainText(groupName)
      })

      await test.step('relay CRUD and backend binding', async () => {
        await gotoAdminPage(page, '/admin/relays', '中转节点管理')
        await page.getByRole('button', { name: '新增中转' }).click()
        let dialog = activeDialog(page, '新增中转')
        await fillFormField(dialog, '中转名称', relayName)
        await fillFormField(dialog, '入口地址', '198.51.100.20')
        await fillFormField(dialog, 'Agent 地址', 'http://198.51.100.20:18080')
        await fillFormField(dialog, 'Agent Token', `relay-token-${suffix}`)
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-relays', relayName)).toBeVisible()
        created.relayId = (await findByName(request, adminToken, '/api/admin/relays', 'relays', relayName)).id

        await tableRow(page, '.admin-relays', relayName).getByRole('button', { name: '编辑' }).click()
        dialog = activeDialog(page, '编辑中转')
        relayName = `${prefix}-relay-updated`
        await fillFormField(dialog, '中转名称', relayName)
        await fillFormField(dialog, '入口地址', '198.51.100.21')
        await dialog.getByRole('button', { name: '保存' }).click()
        await expect(tableRow(page, '.admin-relays', relayName)).toBeVisible()

        await tableRow(page, '.admin-relays', relayName).getByRole('button', { name: '管理后端' }).click()
        dialog = activeDialog(page, '管理后端')
        await page.getByRole('button', { name: '新增后端' }).click()
        const backendRow = dialog.locator('.el-table__body-wrapper tbody tr').last()
        await backendRow.locator('input').nth(0).fill(`${prefix}-backend`)
        await backendRow.locator('.el-select').click()
        await selectDropdownOption(page, nodeName)
        await backendRow.locator('input').nth(2).fill(String(listenPort))
        await backendRow.locator('input').nth(3).fill('24431')
        await backendRow.locator('input').nth(4).fill('1')
        await dialog.getByRole('button', { name: '保存绑定' }).click()
        await expect(tableRow(page, '.admin-relays', relayName)).toContainText(`${prefix}-backend`)
      })

      await test.step('user management and usage dialog', async () => {
        await gotoAdminPage(page, '/admin/users', '用户管理')
        await page.getByRole('button', { name: '新增用户' }).click()
        const dialog = activeDialog(page, '新增用户')
        await fillFormField(dialog, '用户名', username)
        await fillFormField(dialog, '邮箱', `${username}@example.test`)
        await fillFormField(dialog, '密码', userPassword)
        await dialog.getByRole('button', { name: '创建' }).click()
        await expect(dialog).toBeHidden()
        await page.getByPlaceholder('搜索用户名').fill(username)
        await page.getByPlaceholder('搜索用户名').press('Enter')
        await expect(tableRow(page, '.admin-users', username)).toBeVisible()
        const user = await findUserByUsername(request, adminToken, username)
        created.userId = user.id

        const subscriptionData = await api(request, adminToken, 'put', `/api/admin/users/${created.userId}/subscription`, {
          data: {
            plan_id: created.planId,
            status: 'ACTIVE',
            expire_date: futureISO(30),
            traffic_limit: gb(11),
            used_traffic: 0,
          },
        })
        created.tokenId = subscriptionData.tokens?.[0]?.id || null

        await tableRow(page, '.admin-users', username).getByRole('button', { name: '用量' }).click()
        const usageDialog = activeDialog(page, `用量记录 - ${username}`)
        await expect(usageDialog.getByText('截止今日')).toBeVisible()
        await expect(usageDialog.getByText('按天')).toBeVisible()
        await usageDialog.getByText('最近明细').click()
        await expect(usageDialog.getByText('节点').first()).toBeVisible()
        await usageDialog.locator('.el-dialog__headerbtn').click()
        await expect(usageDialog).toBeHidden()
      })

      await test.step('orders list', async () => {
        const { token: userToken } = await login(request, username, userPassword)
        const orderData = await api(request, userToken, 'post', '/api/orders', {
          data: { plan_id: created.planId },
        })
        await gotoAdminPage(page, '/admin/orders', '订单管理')
        await expect(page.getByText(orderData.order.order_no).first()).toBeVisible()
      })

      await test.step('redeem code generation and list', async () => {
        const beforeCodes = await api(request, adminToken, 'get', '/api/admin/redeem-codes', {
          params: { page: 1, size: 50 },
        })
        const beforeIds = new Set((beforeCodes.codes || []).map((code) => String(code.id)))

        await gotoAdminPage(page, '/admin/redeem-codes', '兑换码管理')
        await page.getByRole('button', { name: '生成兑换码' }).click()
        const dialog = activeDialog(page, '生成兑换码')
        await fillFormField(dialog, '套餐 ID', created.planId)
        await fillFormField(dialog, '时长（天）', 3)
        await fillFormField(dialog, '数量', 1)
        await dialog.getByRole('button', { name: '生成' }).click()
        const generated = await waitForNewRedeemCode(request, adminToken, beforeIds, created.planId)
        await page.reload()
        await page.waitForLoadState('networkidle')
        await expect(tableRow(page, '.admin-redeem-codes', generated.code)).toBeVisible()
      })

      await test.step('subscription token list and format switch', async () => {
        const { pageNo } = await findTokenPageForUser(request, adminToken, created.userId)
        await gotoAdminPage(page, '/admin/subscription-tokens', '订阅 Token 管理')
        await goToTokenPage(page, pageNo)
        const tokenRow = tableRow(page, '.admin-subscription-tokens', username)
        await expect(tokenRow).toBeVisible()
        await expect(tokenRow.locator('.link-preview')).toContainText('/clash')
        await tokenRow.getByText('Base64').click()
        await expect(tokenRow.locator('.link-preview')).toContainText('/base64')
        await tokenRow.getByText('URI').click()
        await expect(tokenRow.locator('.link-preview')).toContainText('/plain')
        await tokenRow.getByText('Clash').click()
        await expect(tokenRow.locator('.link-preview')).toContainText('/clash')
      })

      assertNoRuntimeFailures()
    } finally {
      await cleanupOpsData(request, adminToken, created, planCleanupPayload)
    }
  })
})
