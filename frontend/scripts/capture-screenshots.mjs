import { chromium } from '@playwright/test'
import { spawn } from 'node:child_process'
import fs from 'node:fs/promises'
import http from 'node:http'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(__dirname, '../..')
const frontendRoot = path.resolve(projectRoot, 'frontend')
const outputDir = path.resolve(projectRoot, 'assets/screenshots')
const explicitBaseURL = process.env.SCREENSHOT_BASE_URL || process.env.E2E_BASE_URL || ''
const defaultBaseURL = 'http://127.0.0.1:4174'
const demoPublicOrigin = 'https://demo.raypilot.dev'

const gb = (value) => value * 1024 * 1024 * 1024
const iso = (value) => new Date(value).toISOString()

const now = iso('2026-05-03T08:30:00Z')
const token = 'demo-subscription-token'

const user = {
  id: 2,
  uuid: 'demo-user-uuid',
  username: 'demo_user',
  email: 'demo_user@example.test',
  status: 'active',
  is_admin: false,
  created_at: iso('2026-04-20T02:30:00Z'),
  updated_at: now,
  last_login_at: iso('2026-05-03T08:00:00Z'),
}

const adminUser = {
  id: 1,
  uuid: 'demo-admin-uuid',
  username: 'admin',
  email: 'admin@example.test',
  status: 'active',
  is_admin: true,
  created_at: iso('2026-04-18T01:00:00Z'),
  updated_at: now,
  last_login_at: now,
}

const subscription = {
  id: 7,
  user_id: user.id,
  plan_id: 2,
  status: 'ACTIVE',
  expire_date: iso('2026-06-03T23:59:59Z'),
  traffic_limit: gb(300),
  used_traffic: gb(86),
  tokens: [token],
  created_at: iso('2026-04-20T02:35:00Z'),
  updated_at: now,
}

const nodeGroups = [
  {
    id: 1,
    name: '香港优选',
    description: '香港直连和中转线路',
    nodes: [],
  },
  {
    id: 2,
    name: '日本高速',
    description: '东京出口节点',
    nodes: [],
  },
  {
    id: 3,
    name: '基础可用区',
    description: '基础套餐默认线路',
    nodes: [],
  },
]

const nodes = [
  {
    id: 1,
    name: 'HK-01 Reality',
    protocol: 'vless',
    host: 'hk01.example.net',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-hk01',
    short_id: 'a1b2c3d4',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'direct_and_relay',
    agent_base_url: 'http://hk01.example.net:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:28:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:28:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:28:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 2,
    name: 'JP-01 Reality',
    protocol: 'vless',
    host: 'jp01.example.net',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-jp01',
    short_id: 'b2c3d4e5',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'direct_only',
    agent_base_url: 'http://jp01.example.net:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:25:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:25:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:25:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 3,
    name: 'US-01 Relay Only',
    protocol: 'vless',
    host: 'us01.example.net',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-us01',
    short_id: 'c3d4e5f6',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'relay_only',
    agent_base_url: 'http://us01.example.net:8080',
    is_enabled: false,
    last_heartbeat_at: null,
    last_traffic_report_at: null,
    last_traffic_success_at: null,
    traffic_error_count: 2,
    last_traffic_error: 'xray stats api timeout',
  },
]

nodeGroups[0].nodes = [nodes[0], nodes[2]]
nodeGroups[1].nodes = [nodes[1]]
nodeGroups[2].nodes = [nodes[0]]

const plans = [
  {
    id: 1,
    name: '基础套餐',
    price: 0,
    currency: 'USDT',
    traffic_limit: gb(20),
    duration_days: 30,
    is_active: true,
    is_default: true,
    is_deleted: false,
    sort_weight: 0,
    node_group_ids: [3],
    node_groups: [nodeGroups[2]],
  },
  {
    id: 2,
    name: 'Pro 300G',
    price: 9.9,
    currency: 'USDT',
    traffic_limit: gb(300),
    duration_days: 30,
    is_active: true,
    is_default: false,
    is_deleted: false,
    sort_weight: 10,
    node_group_ids: [1, 2],
    node_groups: [nodeGroups[0], nodeGroups[1]],
  },
  {
    id: 3,
    name: 'Team 1T',
    price: 29.9,
    currency: 'USDT',
    traffic_limit: gb(1024),
    duration_days: 30,
    is_active: true,
    is_default: false,
    is_deleted: false,
    sort_weight: 20,
    node_group_ids: [1, 2, 3],
    node_groups: nodeGroups,
  },
]

const relays = [
  {
    id: 1,
    name: 'HK Relay A',
    host: 'relay-hk.example.net',
    forwarder_type: 'haproxy',
    agent_base_url: 'http://relay-hk.example.net:8080',
    is_enabled: true,
    status: 'online',
    last_heartbeat_at: iso('2026-05-03T08:29:00Z'),
    backends: [
      {
        id: 1,
        relay_id: 1,
        name: '香港中转 -> HK-01',
        exit_node_id: 1,
        listen_port: 24443,
        target_host: nodes[0].host,
        target_port: 443,
        is_enabled: true,
        sort_weight: 1,
      },
      {
        id: 2,
        relay_id: 1,
        name: '香港中转 -> US-01',
        exit_node_id: 3,
        listen_port: 24444,
        target_host: nodes[2].host,
        target_port: 443,
        is_enabled: true,
        sort_weight: 2,
      },
    ],
  },
  {
    id: 2,
    name: 'JP Relay B',
    host: 'relay-jp.example.net',
    forwarder_type: 'haproxy',
    agent_base_url: 'http://relay-jp.example.net:8080',
    is_enabled: true,
    status: 'offline',
    last_heartbeat_at: iso('2026-05-03T07:20:00Z'),
    backends: [
      {
        id: 3,
        relay_id: 2,
        name: '日本中转 -> JP-01',
        exit_node_id: 2,
        listen_port: 25443,
        target_host: nodes[1].host,
        target_port: 443,
        is_enabled: false,
        sort_weight: 1,
      },
    ],
  },
]

const demoUsers = [
  {
    ...adminUser,
    subscription: null,
    plan_name: '',
    has_active_subscription: false,
    remaining_traffic: 0,
    used_traffic: 0,
    traffic_limit: 0,
    traffic_usage_percent: 0,
  },
  {
    ...user,
    subscription,
    plan_name: 'Pro 300G',
    subscription_status: 'ACTIVE',
    has_active_subscription: true,
    remaining_traffic: gb(214),
    used_traffic: gb(86),
    traffic_limit: gb(300),
    traffic_usage_percent: 29,
    traffic_unlimited: false,
    subscription_expire_date: subscription.expire_date,
  },
  {
    id: 3,
    username: 'relay_user',
    email: 'relay_user@example.test',
    status: 'active',
    is_admin: false,
    created_at: iso('2026-04-28T04:00:00Z'),
    last_login_at: iso('2026-05-02T11:00:00Z'),
    subscription: {
      ...subscription,
      id: 8,
      user_id: 3,
      plan_id: 3,
      used_traffic: gb(512),
      traffic_limit: gb(1024),
    },
    plan_name: 'Team 1T',
    subscription_status: 'ACTIVE',
    has_active_subscription: true,
    remaining_traffic: gb(512),
    used_traffic: gb(512),
    traffic_limit: gb(1024),
    traffic_usage_percent: 50,
    traffic_unlimited: false,
    subscription_expire_date: iso('2026-06-01T23:59:59Z'),
  },
]

const usageData = {
  has_active_subscription: true,
  plan_name: 'Pro 300G',
  subscription,
  summary: {
    today: { upload: gb(1.8), download: gb(8.4), total: gb(10.2) },
    current_week: { upload: gb(8.6), download: gb(43.1), total: gb(51.7) },
    current_month: { upload: gb(14.8), download: gb(71.2), total: gb(86) },
    subscription_to_today: { upload: gb(14.8), download: gb(71.2), total: gb(86) },
  },
  daily: [
    { date: '2026-05-03', upload: gb(1.8), download: gb(8.4), total: gb(10.2) },
    { date: '2026-05-02', upload: gb(2.2), download: gb(12.9), total: gb(15.1) },
    { date: '2026-05-01', upload: gb(1.1), download: gb(6.7), total: gb(7.8) },
    { date: '2026-04-30', upload: gb(1.5), download: gb(9.2), total: gb(10.7) },
  ],
  weekly: [
    { start_at: '2026-04-27', end_at: '2026-05-03', upload: gb(8.6), download: gb(43.1), total: gb(51.7) },
    { start_at: '2026-04-20', end_at: '2026-04-26', upload: gb(6.2), download: gb(28.1), total: gb(34.3) },
  ],
  monthly: [
    { month: '2026-05', upload: gb(5.1), download: gb(28), total: gb(33.1) },
    { month: '2026-04', upload: gb(9.7), download: gb(43.2), total: gb(52.9) },
  ],
  recent: [
    {
      recorded_at: iso('2026-05-03T08:20:00Z'),
      node_id: 1,
      node_name: 'HK-01 Reality',
      delta_upload: gb(0.2),
      delta_download: gb(1.4),
      delta_total: gb(1.6),
    },
    {
      recorded_at: iso('2026-05-03T08:00:00Z'),
      node_id: 2,
      node_name: 'JP-01 Reality',
      delta_upload: gb(0.1),
      delta_download: gb(0.9),
      delta_total: gb(1),
    },
  ],
}

const orders = [
  {
    id: 1,
    order_no: 'RP202605030001',
    user_id: 2,
    amount: 9.9,
    currency: 'USDT',
    status: 'PAID',
    created_at: iso('2026-05-01T10:00:00Z'),
  },
  {
    id: 2,
    order_no: 'RP202605030002',
    user_id: 3,
    amount: 29.9,
    currency: 'USDT',
    status: 'PENDING',
    created_at: iso('2026-05-03T07:40:00Z'),
  },
]

const redeemCodes = [
  {
    id: 1,
    code: 'RAYPILOT20260503',
    plan_id: 2,
    duration_days: 30,
    is_used: false,
    used_at: null,
  },
  {
    id: 2,
    code: 'DEMO-USED-0001',
    plan_id: 1,
    duration_days: 7,
    is_used: true,
    used_at: iso('2026-05-01T09:00:00Z'),
  },
]

const tokens = [
  {
    id: 1,
    user_id: 2,
    username: 'demo_user',
    token,
    token_status: 'ACTIVE',
    subscription,
    plan: plans[1],
    plan_name: 'Pro 300G',
    has_active_subscription: true,
    subscription_status: 'ACTIVE',
    is_usable: true,
    is_revoked: false,
    is_expired: false,
    last_used_at: iso('2026-05-03T08:20:00Z'),
    expires_at: null,
  },
  {
    id: 2,
    user_id: 3,
    username: 'relay_user',
    token: 'demo-relay-user-token',
    token_status: 'ACTIVE',
    subscription: demoUsers[2].subscription,
    plan: plans[2],
    plan_name: 'Team 1T',
    has_active_subscription: true,
    subscription_status: 'ACTIVE',
    is_usable: true,
    is_revoked: false,
    is_expired: false,
    last_used_at: iso('2026-05-02T12:10:00Z'),
    expires_at: null,
  },
]

const json = (data) => ({
  success: true,
  message: 'ok',
  code: 0,
  data,
})

function paginated(items, key) {
  return json({ [key]: items, total: items.length, page: 1, size: 20 })
}

async function fulfillJson(route, data) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

function waitForServer(url, timeoutMs = 30_000) {
  const startedAt = Date.now()

  return new Promise((resolve, reject) => {
    const attempt = () => {
      const req = http.get(url, (res) => {
        res.resume()
        resolve()
      })
      req.on('error', () => {
        if (Date.now() - startedAt > timeoutMs) {
          reject(new Error(`Timed out waiting for screenshot server: ${url}`))
          return
        }
        setTimeout(attempt, 300)
      })
      req.setTimeout(1_000, () => {
        req.destroy()
      })
    }
    attempt()
  })
}

async function startFrontendServer() {
  const child = spawn('npm', ['run', 'dev', '--', '--host', '127.0.0.1', '--port', '4174'], {
    cwd: frontendRoot,
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env },
  })

  let output = ''
  child.stdout.on('data', (chunk) => {
    output += chunk.toString()
  })
  child.stderr.on('data', (chunk) => {
    output += chunk.toString()
  })

  child.on('exit', (code) => {
    if (code !== 0 && code !== null) {
      output += `\nVite exited with code ${code}`
    }
  })

  try {
    await waitForServer(defaultBaseURL)
  } catch (error) {
    child.kill('SIGTERM')
    throw new Error(`${error.message}\n${output}`)
  }

  return child
}

async function installDemoApi(page, persona = 'user') {
  const currentUser = () => persona === 'admin' ? adminUser : user
  const currentSubscription = () => persona === 'admin' ? null : subscription

  await page.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const pathname = url.pathname

    if (!pathname.startsWith('/api/')) {
      return route.continue()
    }

    if (pathname === '/api/auth/login') {
      const body = request.postDataJSON?.() || {}
      const loginUser = body.username === 'admin' ? adminUser : user
      return fulfillJson(route, json({ accessToken: 'demo-access-token', user: loginUser }))
    }
    if (pathname === '/api/auth/refresh') {
      return fulfillJson(route, json({ accessToken: 'demo-access-token' }))
    }
    if (pathname === '/api/user/me') {
      return fulfillJson(route, json({ user: currentUser(), subscription: currentSubscription() }))
    }
    if (pathname === '/api/user/subscription') {
      return fulfillJson(route, json({ subscription }))
    }
    if (pathname === '/api/user/usage') {
      return fulfillJson(route, json(usageData))
    }
    if (pathname === '/api/user/orders') {
      return fulfillJson(route, paginated(orders, 'orders'))
    }
    if (pathname === '/api/plans') {
      return fulfillJson(route, json({ plans: plans.filter((item) => item.is_active && !item.is_deleted) }))
    }
    if (pathname === '/api/admin/dashboard/stats') {
      return fulfillJson(route, json({
        user_count: 128,
        node_count: nodes.length,
        plan_count: plans.length,
        active_sub_count: 93,
      }))
    }
    if (pathname === '/api/admin/plans') {
      return fulfillJson(route, json({ plans, total: plans.length }))
    }
    if (pathname === '/api/admin/node-groups') {
      return fulfillJson(route, json({ groups: nodeGroups, total: nodeGroups.length }))
    }
    const groupNodesMatch = pathname.match(/^\/api\/admin\/node-groups\/(\d+)\/nodes$/)
    if (groupNodesMatch) {
      const group = nodeGroups.find((item) => String(item.id) === groupNodesMatch[1])
      return fulfillJson(route, json({ nodes: group?.nodes || [], node_ids: (group?.nodes || []).map((item) => item.id) }))
    }
    if (pathname === '/api/admin/nodes') {
      return fulfillJson(route, json({ nodes, total: nodes.length }))
    }
    if (pathname === '/api/admin/relays') {
      return fulfillJson(route, json({ relays, total: relays.length }))
    }
    const relayBackendsMatch = pathname.match(/^\/api\/admin\/relays\/(\d+)\/backends$/)
    if (relayBackendsMatch) {
      const relay = relays.find((item) => String(item.id) === relayBackendsMatch[1])
      return fulfillJson(route, json({ backends: relay?.backends || [] }))
    }
    if (pathname === '/api/admin/users') {
      return fulfillJson(route, paginated(demoUsers, 'users'))
    }
    const userSubscriptionMatch = pathname.match(/^\/api\/admin\/users\/(\d+)\/subscription$/)
    if (userSubscriptionMatch) {
      const targetUser = demoUsers.find((item) => String(item.id) === userSubscriptionMatch[1])
      return fulfillJson(route, json({ subscription: targetUser?.subscription || null, tokens: tokens.filter((item) => String(item.user_id) === userSubscriptionMatch[1]) }))
    }
    const userUsageMatch = pathname.match(/^\/api\/admin\/users\/(\d+)\/usage$/)
    if (userUsageMatch) {
      return fulfillJson(route, json(usageData))
    }
    if (pathname === '/api/admin/orders') {
      return fulfillJson(route, paginated(orders, 'orders'))
    }
    if (pathname === '/api/admin/redeem-codes') {
      return fulfillJson(route, paginated(redeemCodes, 'codes'))
    }
    if (pathname === '/api/admin/subscription-tokens') {
      return fulfillJson(route, paginated(tokens, 'tokens'))
    }

    return fulfillJson(route, json({}))
  })
}

async function settle(page) {
  await page.waitForLoadState('networkidle')
  await page.evaluate((origin) => {
    const replaceValue = (value) => value.replace(/https?:\/\/[^/\s]+\/sub\//g, `${origin}/sub/`)
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
    while (walker.nextNode()) {
      const node = walker.currentNode
      if (node.nodeValue && node.nodeValue.includes('/sub/')) {
        node.nodeValue = replaceValue(node.nodeValue)
      }
    }
    document.querySelectorAll('input').forEach((input) => {
      if (input.value.includes('/sub/')) {
        input.value = replaceValue(input.value)
        input.setAttribute('value', input.value)
      }
    })
  }, demoPublicOrigin)
  await page.waitForTimeout(450)
}

async function screenshot(page, name, { expectedText, fullPage = true } = {}) {
  if (expectedText) {
    await page.getByText(expectedText).first().waitFor({ state: 'visible', timeout: 30_000 })
  }
  await settle(page)
  await page.screenshot({
    path: path.join(outputDir, `${name}.png`),
    fullPage,
    animations: 'disabled',
  })
}

async function main() {
  await fs.mkdir(outputDir, { recursive: true })
  const server = explicitBaseURL ? null : await startFrontendServer()
  const baseURL = explicitBaseURL || defaultBaseURL

  const browser = await chromium.launch()
  const context = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 1000 },
    deviceScaleFactor: 1,
    locale: 'zh-CN',
  })

  try {
    const page = await context.newPage()
    await installDemoApi(page, 'admin')

    const adminPages = [
      ['admin-dashboard', '/admin', '管理后台仪表盘'],
      ['admin-plans', '/admin/plans', '套餐管理'],
      ['admin-node-groups', '/admin/node-groups', '节点分组管理'],
      ['admin-nodes', '/admin/nodes', '出口节点管理'],
      ['admin-relays', '/admin/relays', '中转节点管理'],
      ['admin-users', '/admin/users', '用户管理'],
      ['admin-subscription-tokens', '/admin/subscription-tokens', '订阅 Token 管理'],
    ]

    for (const [name, url, expectedText] of adminPages) {
      await page.goto(url)
      await screenshot(page, name, { expectedText })
    }

    const userContext = await browser.newContext({
      baseURL,
      viewport: { width: 1440, height: 1000 },
      deviceScaleFactor: 1,
      locale: 'zh-CN',
    })
    const userPage = await userContext.newPage()
    await installDemoApi(userPage, 'user')

    const userPages = [
      ['user-home', '/', '欢迎使用 RayPilot'],
      ['user-subscription', '/subscription', '我的订阅'],
      ['user-plans', '/plans', '套餐列表'],
    ]
    for (const [name, url, expectedText] of userPages) {
      await userPage.goto(url)
      await screenshot(userPage, name, { expectedText })
    }
    await userContext.close()

    const mobile = await browser.newPage({
      baseURL,
      viewport: { width: 390, height: 844 },
      deviceScaleFactor: 1,
      isMobile: true,
      locale: 'zh-CN',
    })
    await installDemoApi(mobile, 'user')
    await mobile.goto('/subscription')
    await screenshot(mobile, 'mobile-user-subscription', { expectedText: '我的订阅' })
    await mobile.close()
  } finally {
    await context.close()
    await browser.close()
    if (server) {
      server.kill('SIGTERM')
    }
  }
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
