import { expect, test } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME
const adminPassword = process.env.E2E_ADMIN_PASSWORD

function requireEnv(name, value) {
  if (!value) {
    throw new Error(`${name} is required for e2e smoke tests`)
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

async function loginAdmin(page) {
  requireEnv('E2E_ADMIN_USERNAME', adminUsername)
  requireEnv('E2E_ADMIN_PASSWORD', adminPassword)

  await page.goto('/admin/login')
  await expect(page.getByRole('heading', { name: '管理后台登录' })).toBeVisible()
  await page.getByPlaceholder('管理员用户名').fill(adminUsername)
  await page.getByPlaceholder('密码').fill(adminPassword)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/admin\/?$/)
  await expect(page.getByText('用户总数')).toBeVisible()
}

test('admin and user pages render against live API', async ({ page }) => {
  const assertNoRuntimeFailures = await installGuards(page)
  await page.goto('/')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('heading', { name: '高速 VPN 节点' })).toBeVisible()
  await expect(page.getByRole('link', { name: '立即开通' })).toBeVisible()
  await expect(page.getByRole('link', { name: '管理员入口' })).toHaveCount(0)
  const publicLinks = await page.locator('.sales-page a').evaluateAll((links) => links.map((link) => link.getAttribute('href') || ''))
  expect(publicLinks.filter((href) => href.toLowerCase().startsWith('javascript:'))).toEqual([])
  await page.goto('/platform')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('heading', { name: 'RayPilot' })).toBeVisible()
  await expect(page.getByRole('link', { name: '管理员入口' })).toHaveCount(0)

  await loginAdmin(page)

  const adminPages = [
    ['/admin', '管理后台仪表盘'],
    ['/admin/plans', '套餐管理'],
    ['/admin/node-groups', '节点分组管理'],
    ['/admin/nodes', '节点管理'],
    ['/admin/node-operations', '节点运营中心'],
    ['/admin/relays', '中转节点管理'],
    ['/admin/users', '用户管理'],
    ['/admin/orders', '订单管理'],
    ['/admin/redeem-codes', '兑换码管理'],
    ['/admin/subscription-tokens', '订阅 Token 管理'],
    ['/admin/subscription-settings', '订阅配置'],
    ['/admin/sales-landing', '销售首页'],
    ['/admin/logs', '日志中心'],
  ]

  for (const [path, text] of adminPages) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(text).first()).toBeVisible()
  }

  await page.goto('/admin/sales-landing')
  await page.waitForLoadState('networkidle')
  await expect(page.getByLabel('首页标题')).toHaveValue('高速 VPN 节点')
  await expect(page.getByRole('button', { name: '保存配置' })).toBeVisible()

  await page.goto('/admin/subscription-settings')
  await page.waitForLoadState('networkidle')
  await expect(page.getByLabel('订阅名称')).toBeVisible()
  await expect(page.locator('.terminal-preview').getByText(/subscription-userinfo:/)).toBeVisible()
  await expect(page.locator('.rule-chips').getByText('MATCH,PROXY', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '保存配置' })).toBeVisible()

  await page.goto('/admin/nodes')
  await page.waitForLoadState('networkidle')
  const nodeTableScroll = page.locator('.node-table-scroll').first()
  await expect(nodeTableScroll).toBeVisible()
  const nodeTableCanScroll = await nodeTableScroll.evaluate((el) => {
    const start = el.scrollLeft
    el.scrollLeft = el.scrollWidth
    const moved = el.scrollLeft > start
    el.scrollLeft = start
    return el.scrollWidth > el.clientWidth && moved
  })
  expect(nodeTableCanScroll).toBeTruthy()
  await expect(page.getByText('端口').first()).toBeVisible()
  await expect(page.getByText(/\d+\.\d+\.\d+\.\d+:\d+/).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '修复中心' }).first()).toBeVisible()
  await page.getByRole('button', { name: '一键部署' }).click()
  const deployDialog = page.locator('.el-dialog').filter({ hasText: '一键部署节点' })
  await expect(deployDialog.locator('.el-form-item').filter({ hasText: '备用中心地址' })).toBeVisible()
  await expect(deployDialog.getByText('多 IP 服务器')).toBeVisible()
  await expect(deployDialog.getByRole('button', { name: '扫描出口 IP' })).toHaveCount(0)
  await deployDialog.getByText('多 IP 服务器').locator('..').locator('.el-switch').click()
  await expect(deployDialog.getByRole('button', { name: '扫描出口 IP' })).toBeVisible()
  await deployDialog.getByRole('button', { name: '取消' }).click()

  await page.goto('/admin/node-operations')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('今日流量排行')).toBeVisible()
  await expect(page.getByText('本月流量排行')).toBeVisible()
  await expect(page.getByText('节点健康矩阵')).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新扫描结果' })).toBeVisible()
  await expect(page.getByText('总线路')).toBeVisible()

  await page.goto('/admin/relays')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('button', { name: '一键部署中转' })).toBeVisible()
  const relayRows = page.locator('.admin-relays .el-table__body-wrapper tbody tr')
  const relayRowCount = await relayRows.count()
  if (relayRowCount > 0) {
    await expect(page.getByRole('button', { name: '修复中心' }).first()).toBeVisible()
    await expect(page.getByText('管理后端').first()).toBeVisible()
  } else {
    await expect(page.getByText('暂无数据').first()).toBeVisible()
  }
  await page.getByRole('button', { name: '一键部署中转' }).click()
  const relayDeployDialog = page.locator('.el-dialog').filter({ hasText: '一键部署中转节点' })
  await expect(relayDeployDialog.locator('.el-form-item').filter({ hasText: '出口节点' })).toBeVisible()
  await expect(relayDeployDialog.locator('.el-form-item').filter({ hasText: '监听端口' })).toBeVisible()
  await expect(relayDeployDialog.locator('.el-form-item').filter({ hasText: '备用中心地址' })).toBeVisible()
  await expect(relayDeployDialog.locator('.el-form-item').filter({ hasText: '替换旧角色' })).toBeVisible()
  await relayDeployDialog.getByRole('button', { name: '取消' }).click()

  await page.goto('/admin/subscription-tokens')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('button', { name: '补齐 Token' })).toBeVisible()
  await expect(page.getByText('套餐状态').first()).toBeVisible()
  await expect(page.getByText('Token状态').first()).toBeVisible()
  await expect(page.getByText('/sub/').first()).toBeVisible()
  const firstTokenRow = page.locator('.admin-subscription-tokens .el-table__body-wrapper tbody tr').first()
  await firstTokenRow.getByText('Base64').click()
  await expect(firstTokenRow.locator('.link-preview')).toContainText('/base64')
  await firstTokenRow.getByText('URI').click()
  await expect(firstTokenRow.locator('.link-preview')).toContainText('/plain')
  await firstTokenRow.getByText('Clash').click()
  await expect(firstTokenRow.locator('.link-preview')).toContainText('/clash')

  await page.goto('/admin/users')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('剩余流量').first()).toBeVisible()
  const usersTableScroll = page.locator('.admin-users .admin-table-scroll').first()
  await expect(usersTableScroll).toBeVisible()
  const firstUsageButton = page.locator('.admin-users').getByRole('button', { name: '用量' }).first()
  await expect(firstUsageButton).toBeVisible()
  const usersActionButtonVisible = await firstUsageButton.evaluate((el) => {
    const rect = el.getBoundingClientRect()
    return rect.left >= 0 && rect.right <= window.innerWidth && rect.width > 0
  })
  expect(usersActionButtonVisible).toBeTruthy()
  await expect(page.locator('.admin-users .el-table-column--selection .el-checkbox__inner').first()).toBeVisible()
  await expect(page.getByRole('button', { name: '删除' }).first()).toBeVisible()
  await firstUsageButton.click()
  await expect(page.getByText('用量记录 -').first()).toBeVisible()
  await expect(page.getByText('截止今日').first()).toBeVisible()

  const userPages = [
    ['/dashboard', '欢迎使用 RayPilot'],
    ['/subscription', '我的订阅'],
    ['/orders', '我的订单'],
    ['/plans', '套餐列表'],
    ['/redeem', '兑换码'],
    ['/profile', '个人资料'],
  ]

  for (const [path, text] of userPages) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(text).first()).toBeVisible()
  }
  await page.goto('/dashboard')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('普通流量进度').first()).toBeVisible()
  await expect(page.getByText('家宽流量进度').first()).toBeVisible()
  await page.goto('/subscription')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('普通流量进度').first()).toBeVisible()
  await expect(page.getByText('家宽流量进度').first()).toBeVisible()

  assertNoRuntimeFailures()
})
