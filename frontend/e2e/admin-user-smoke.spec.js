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
  await loginAdmin(page)

  const adminPages = [
    ['/admin', '管理后台仪表盘'],
    ['/admin/plans', '套餐管理'],
    ['/admin/node-groups', '节点分组管理'],
    ['/admin/nodes', '节点管理'],
    ['/admin/relays', '中转节点管理'],
    ['/admin/users', '用户管理'],
    ['/admin/orders', '订单管理'],
    ['/admin/redeem-codes', '兑换码管理'],
    ['/admin/subscription-tokens', '订阅 Token 管理'],
  ]

  for (const [path, text] of adminPages) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page.getByText(text).first()).toBeVisible()
  }

  await page.goto('/admin/nodes')
  await page.waitForLoadState('networkidle')
  await page.getByRole('button', { name: '一键部署' }).click()
  const deployDialog = page.locator('.el-dialog').filter({ hasText: '一键部署节点' })
  await expect(deployDialog.getByText('多 IP 服务器')).toBeVisible()
  await expect(deployDialog.getByRole('button', { name: '扫描出口 IP' })).toHaveCount(0)
  await deployDialog.locator('.el-switch').click()
  await expect(deployDialog.getByRole('button', { name: '扫描出口 IP' })).toBeVisible()
  await deployDialog.getByRole('button', { name: '取消' }).click()

  await page.goto('/admin/relays')
  await page.waitForLoadState('networkidle')
  await expect(page.getByRole('button', { name: '一键部署中转' })).toBeVisible()
  await expect(page.getByText('管理后端').first()).toBeVisible()

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
  await expect(page.locator('.admin-users .el-table-column--selection .el-checkbox__inner').first()).toBeVisible()
  await expect(page.getByRole('button', { name: '删除' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '用量' }).first()).toBeVisible()
  await page.getByRole('button', { name: '用量' }).first().click()
  await expect(page.getByText('用量记录 -').first()).toBeVisible()
  await expect(page.getByText('截止今日').first()).toBeVisible()

  const userPages = [
    ['/', '欢迎使用 RayPilot'],
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

  assertNoRuntimeFailures()
})
