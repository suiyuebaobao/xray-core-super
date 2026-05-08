import { chromium } from '@playwright/test'
import { spawn } from 'node:child_process'
import fs from 'node:fs/promises'
import http from 'node:http'
import { once } from 'node:events'
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
  plan_name: 'Pro 双池 300G',
  status: 'ACTIVE',
  expire_date: iso('2026-06-03T23:59:59Z'),
  traffic_limit: gb(300),
  used_traffic: gb(86),
  residential_traffic_limit: gb(80),
  residential_used_traffic: gb(19),
  tokens: [token],
  created_at: iso('2026-04-20T02:35:00Z'),
  updated_at: now,
}

const nodeGroups = [
  {
    id: 1,
    name: '香港优选',
    description: '香港直连、中转与 XHTTP 线路',
    nodes: [],
  },
  {
    id: 2,
    name: '美国家宽',
    description: 'SOCKS5 上游家宽出口线路',
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
    name: '香港1-TCP',
    protocol: 'vless',
    transport: 'tcp',
    traffic_pool: 'normal',
    outbound_type: 'direct',
    outbound_proxy_url: '',
    host: '203.0.113.10',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-hk01',
    short_id: 'a1b2c3d4',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'direct_and_relay',
    node_host_id: 2,
    listen_ip: '203.0.113.10',
    outbound_ip: '203.0.113.10',
    agent_base_url: 'http://203.0.113.10:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:28:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:28:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:28:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 2,
    name: '香港1-XHTTP',
    protocol: 'vless',
    transport: 'xhttp',
    traffic_pool: 'normal',
    outbound_type: 'direct',
    outbound_proxy_url: '',
    host: '203.0.113.10',
    port: 8443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-hk01',
    short_id: 'a1b2c3d4',
    fingerprint: 'chrome',
    flow: '',
    line_mode: 'direct_and_relay',
    xhttp_path: '/raypilot',
    xhttp_mode: 'auto',
    xhttp_host: 'cdn.demo.raypilot.test',
    node_host_id: 2,
    listen_ip: '203.0.113.10',
    outbound_ip: '203.0.113.10',
    agent_base_url: 'http://203.0.113.10:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:29:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:29:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:29:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 3,
    name: '美国-TCP',
    protocol: 'vless',
    transport: 'tcp',
    traffic_pool: 'normal',
    outbound_type: 'direct',
    outbound_proxy_url: '',
    host: '198.51.100.30',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-us01',
    short_id: 'c3d4e5f6',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'relay_only',
    node_host_id: null,
    listen_ip: '198.51.100.30',
    outbound_ip: '198.51.100.30',
    agent_base_url: 'http://198.51.100.30:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:27:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:27:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:27:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 4,
    name: '美国家宽1-TCP',
    protocol: 'vless',
    transport: 'tcp',
    traffic_pool: 'residential',
    outbound_type: 'socks5',
    outbound_proxy_url: 'socks5://demo-user:demo-pass@res-us-1.demo.raypilot.test:3010',
    host: '198.51.100.40',
    port: 443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-home01',
    short_id: 'd4e5f6a7',
    fingerprint: 'chrome',
    flow: 'xtls-rprx-vision',
    line_mode: 'direct_only',
    node_host_id: 3,
    listen_ip: '0.0.0.0',
    outbound_ip: '198.51.100.40',
    agent_base_url: 'http://198.51.100.40:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:26:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:26:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:26:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
  {
    id: 5,
    name: '美国家宽2-XHTTP',
    protocol: 'vless',
    transport: 'xhttp',
    traffic_pool: 'residential',
    outbound_type: 'socks5',
    outbound_proxy_url: 'socks5://demo-user:demo-pass@res-us-2.demo.raypilot.test:3011',
    host: '198.51.100.40',
    port: 8443,
    server_name: 'www.microsoft.com',
    public_key: 'demo-public-key-home02',
    short_id: 'e5f6a7b8',
    fingerprint: 'chrome',
    flow: '',
    line_mode: 'direct_only',
    xhttp_path: '/raypilot',
    xhttp_mode: 'auto',
    xhttp_host: 'cdn-home.demo.raypilot.test',
    node_host_id: 3,
    listen_ip: '0.0.0.0',
    outbound_ip: '198.51.100.40',
    agent_base_url: 'http://198.51.100.40:8080',
    is_enabled: true,
    last_heartbeat_at: iso('2026-05-03T08:26:00Z'),
    last_traffic_report_at: iso('2026-05-03T08:26:00Z'),
    last_traffic_success_at: iso('2026-05-03T08:26:00Z'),
    traffic_error_count: 0,
    last_traffic_error: '',
  },
]

nodeGroups[0].nodes = [nodes[0], nodes[1], nodes[2]]
nodeGroups[1].nodes = [nodes[3], nodes[4]]
nodeGroups[2].nodes = [nodes[0]]

const plans = [
  {
    id: 1,
    name: '基础套餐',
    price: 0,
    currency: 'USDT',
    traffic_limit: gb(20),
    residential_traffic_limit: 0,
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
    name: 'Pro 双池 300G',
    price: 9.9,
    currency: 'USDT',
    traffic_limit: gb(300),
    residential_traffic_limit: gb(80),
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
    name: 'Team 双池 1T',
    price: 29.9,
    currency: 'USDT',
    traffic_limit: gb(1024),
    residential_traffic_limit: gb(300),
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
    name: '香港-转-美国',
    host: 'relay-hk.demo.raypilot.test',
    forwarder_type: 'haproxy',
    agent_base_url: 'http://relay-hk.demo.raypilot.test:8080',
    is_enabled: true,
    status: 'online',
    last_heartbeat_at: iso('2026-05-03T08:29:00Z'),
    backends: [
      {
        id: 1,
        relay_id: 1,
        name: '香港-转-美国-TCP',
        exit_node_id: 3,
        listen_port: 24443,
        target_host: nodes[2].host,
        target_port: 443,
        is_enabled: true,
        sort_weight: 1,
      },
      {
        id: 2,
        relay_id: 1,
        name: '香港-转-美国-XHTTP',
        exit_node_id: 2,
        listen_port: 28443,
        target_host: nodes[1].host,
        target_port: 8443,
        is_enabled: true,
        sort_weight: 2,
      },
    ],
  },
  {
    id: 2,
    name: '日本-转-美国家宽',
    host: 'relay-jp.demo.raypilot.test',
    forwarder_type: 'haproxy',
    agent_base_url: 'http://relay-jp.demo.raypilot.test:8080',
    is_enabled: true,
    status: 'offline',
    last_heartbeat_at: iso('2026-05-03T07:20:00Z'),
    backends: [
      {
        id: 3,
        relay_id: 2,
        name: '日本-转-美国家宽1',
        exit_node_id: 4,
        listen_port: 25443,
        target_host: nodes[3].host,
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
    plan_name: 'Pro 双池 300G',
    subscription_status: 'ACTIVE',
    has_active_subscription: true,
    remaining_traffic: gb(214),
    used_traffic: gb(86),
    traffic_limit: gb(300),
    residential_used_traffic: gb(19),
    residential_traffic_limit: gb(80),
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
      residential_used_traffic: gb(120),
      residential_traffic_limit: gb(300),
    },
    plan_name: 'Team 双池 1T',
    subscription_status: 'ACTIVE',
    has_active_subscription: true,
    remaining_traffic: gb(512),
    used_traffic: gb(512),
    traffic_limit: gb(1024),
    residential_used_traffic: gb(120),
    residential_traffic_limit: gb(300),
    traffic_usage_percent: 50,
    traffic_unlimited: false,
    subscription_expire_date: iso('2026-06-01T23:59:59Z'),
  },
]

const usageData = {
  has_active_subscription: true,
  plan_name: 'Pro 双池 300G',
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
      node_name: '香港1-TCP',
      delta_upload: gb(0.2),
      delta_download: gb(1.4),
      delta_total: gb(1.6),
      traffic_pool: 'normal',
    },
    {
      recorded_at: iso('2026-05-03T08:00:00Z'),
      node_id: 4,
      node_name: '美国家宽1-TCP',
      delta_upload: gb(0.1),
      delta_download: gb(0.9),
      delta_total: gb(1),
      traffic_pool: 'residential',
    },
  ],
}

const orders = [
  {
    id: 1,
    order_no: 'RP202605030001',
    user_id: 2,
    plan_id: 2,
    amount: 9.9,
    currency: 'USDT',
    status: 'PAID',
    created_at: iso('2026-05-01T10:00:00Z'),
  },
  {
    id: 2,
    order_no: 'RP202605030002',
    user_id: 3,
    plan_id: 3,
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
    plan_name: 'Pro 双池 300G',
    duration_days: 30,
    is_used: false,
    used_at: null,
  },
  {
    id: 2,
    code: 'DEMO-USED-0001',
    plan_id: 1,
    plan_name: '基础套餐',
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
    plan_name: 'Pro 双池 300G',
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
    plan_name: 'Team 双池 1T',
    has_active_subscription: true,
    subscription_status: 'ACTIVE',
    is_usable: true,
    is_revoked: false,
    is_expired: false,
    last_used_at: iso('2026-05-02T12:10:00Z'),
    expires_at: null,
  },
]

const runtimeLogLines = [
  { line_number: 1, level: 'info', message: '2026-05-03T08:28:00Z api started on :3000', raw: '2026-05-03T08:28:00Z api started on :3000' },
  { line_number: 2, level: 'info', message: '2026-05-03T08:29:10Z admin user demo_admin logged in from 198.51.100.10', raw: '2026-05-03T08:29:10Z admin user demo_admin logged in from 198.51.100.10' },
  { line_number: 3, level: 'warn', message: '2026-05-03T08:29:40Z relay heartbeat delayed relay-hk.demo.raypilot.test', raw: '2026-05-03T08:29:40Z relay heartbeat delayed relay-hk.demo.raypilot.test' },
  { line_number: 4, level: 'info', message: '2026-05-03T08:30:00Z deployment log center screenshot data loaded', raw: '2026-05-03T08:30:00Z deployment log center screenshot data loaded' },
]

const deploymentLogs = [
  {
    id: 1,
    operator_user_id: 1,
    operator_username: 'admin',
    operator_ip: '198.51.100.10',
    deploy_type: 'exit_deploy',
    target_server_ip: '203.0.113.10',
    target_role: 'exit',
    result: 'success',
    duration_ms: 42700,
    node_id: 4,
    node_ids: '[1,4]',
    node_host_id: 2,
    relay_id: null,
    backend_ids: null,
    error_detail: null,
    created_at: iso('2026-05-03T08:24:00Z'),
    steps: [
      { id: 1, deployment_log_id: 1, step_order: 0, name: 'SSH 连接', status: 'success', message: '连接到 root@203.0.113.10:22' },
      { id: 2, deployment_log_id: 1, step_order: 1, name: '创建节点', status: 'success', message: '已创建 TCP 与 XHTTP 逻辑节点' },
      { id: 3, deployment_log_id: 1, step_order: 2, name: '同步用户', status: 'success', message: '已触发现有活跃订阅同步' },
    ],
  },
  {
    id: 2,
    operator_user_id: 1,
    operator_username: 'admin',
    operator_ip: '198.51.100.10',
    deploy_type: 'relay_deploy',
    target_server_ip: '198.51.100.20',
    target_role: 'relay',
    result: 'success',
    duration_ms: 31800,
    node_id: null,
    node_ids: null,
    node_host_id: null,
    relay_id: 1,
    backend_ids: '[1,2]',
    error_detail: null,
    created_at: iso('2026-05-03T08:10:00Z'),
    steps: [
      { id: 4, deployment_log_id: 2, step_order: 0, name: '启动容器', status: 'success', message: 'relay agent 已启动' },
      { id: 5, deployment_log_id: 2, step_order: 1, name: '绑定中转后端', status: 'success', message: '已保存 2 条后端绑定' },
      { id: 6, deployment_log_id: 2, step_order: 2, name: '等待转发配置', status: 'success', message: 'HAProxy 配置已应用' },
    ],
  },
]

const operationLogs = [
  {
    id: 1,
    actor_type: 'admin',
    actor_user_id: 1,
    actor_username: 'admin',
    client_ip: '198.51.100.10',
    forwarded_for: '198.51.100.10',
    real_ip: '198.51.100.10',
    user_agent: 'RayPilot Demo Browser',
    action: 'admin_upsert_subscription',
    target_type: 'user',
    target_id: 2,
    result: 'success',
    summary: '管理员调整用户订阅',
    created_at: iso('2026-05-03T08:29:00Z'),
  },
  {
    id: 2,
    actor_type: 'user',
    actor_user_id: 2,
    actor_username: 'demo_user',
    client_ip: '203.0.113.55',
    forwarded_for: '203.0.113.55',
    real_ip: '203.0.113.55',
    user_agent: 'mihomo/demo',
    action: 'download_subscription',
    target_type: 'subscription_token',
    target_id: null,
    result: 'success',
    summary: '用户下载订阅',
    created_at: iso('2026-05-03T08:20:00Z'),
  },
  {
    id: 3,
    actor_type: 'user',
    actor_user_id: 2,
    actor_username: 'demo_user',
    client_ip: '203.0.113.55',
    forwarded_for: '203.0.113.55',
    real_ip: '203.0.113.55',
    user_agent: 'RayPilot Demo Browser',
    action: 'login',
    target_type: 'user',
    target_id: 2,
    result: 'success',
    summary: '用户登录成功',
    created_at: iso('2026-05-03T08:00:00Z'),
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
    detached: true,
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

async function stopFrontendServer(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) {
    return
  }

  try {
    process.kill(-child.pid, 'SIGTERM')
  } catch {
    child.kill('SIGTERM')
  }

  const timeout = new Promise((resolve) => {
    setTimeout(resolve, 5_000)
  })
  await Promise.race([once(child, 'exit'), timeout])

  if (child.exitCode === null && child.signalCode === null) {
    try {
      process.kill(-child.pid, 'SIGKILL')
    } catch {
      child.kill('SIGKILL')
    }
  }
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
    if (pathname === '/api/auth/register') {
      return fulfillJson(route, json({ user: { ...user, id: 4, username: 'new_demo_user' } }))
    }
    if (pathname === '/api/auth/refresh') {
      return fulfillJson(route, json({ accessToken: 'demo-access-token' }))
    }
    if (pathname === '/api/auth/logout') {
      return fulfillJson(route, json({}))
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
    if (pathname === '/api/user/profile') {
      const body = request.postDataJSON?.() || {}
      return fulfillJson(route, json({ user: { ...user, email: body.email || user.email } }))
    }
    if (pathname === '/api/user/password') {
      return fulfillJson(route, json({}))
    }
    if (pathname === '/api/plans') {
      return fulfillJson(route, json({ plans: plans.filter((item) => item.is_active && !item.is_deleted) }))
    }
    if (pathname === '/api/redeem') {
      return fulfillJson(route, json({ subscription: { ...subscription, expire_date: iso('2026-07-03T23:59:59Z') } }))
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
    if (pathname === '/api/admin/nodes/deploy/scan-ips') {
      return fulfillJson(route, json({
        ips: [
          {
            ip: '203.0.113.10',
            interface: 'eth0',
            status: 'usable',
            is_usable: true,
            message: '出口 IP 验证通过',
          },
          {
            ip: '203.0.113.11',
            interface: 'eth0:1',
            status: 'usable',
            is_usable: true,
            message: '出口 IP 验证通过',
          },
          {
            ip: '10.0.0.5',
            interface: 'eth1',
            status: 'skipped',
            is_usable: false,
            message: '私网地址已跳过',
          },
        ],
        steps: [
          { name: '读取服务器地址', status: 'success', message: '发现 3 个 IPv4 地址' },
          { name: '验证公网出口', status: 'success', message: '2 个公网出口 IP 可用' },
        ],
      }))
    }
    if (pathname === '/api/admin/nodes/deploy') {
      return fulfillJson(route, json({
        success: true,
        node_id: 6,
        node_ids: [6, 7, 8, 9],
        node_host_id: 3,
        node_host_token: 'demo-node-host-token',
        steps: [
          { name: 'SSH 连接', status: 'success', message: '连接到演示服务器' },
          { name: '安装 Xray', status: 'success', message: 'xray-core v26.3.27 已就绪' },
          { name: '创建逻辑节点', status: 'success', message: '已创建 TCP / XHTTP 多线路' },
          { name: '同步用户', status: 'success', message: '已触发现有活跃订阅同步' },
        ],
      }))
    }
    if (pathname === '/api/admin/relays') {
      return fulfillJson(route, json({ relays, total: relays.length }))
    }
    const relayBackendsMatch = pathname.match(/^\/api\/admin\/relays\/(\d+)\/backends$/)
    if (relayBackendsMatch) {
      const relay = relays.find((item) => String(item.id) === relayBackendsMatch[1])
      return fulfillJson(route, json({ backends: relay?.backends || [] }))
    }
    if (pathname === '/api/admin/relays/deploy') {
      return fulfillJson(route, json({
        success: true,
        relay_id: 3,
        backend_ids: [4],
        relay_token: 'demo-relay-token',
        steps: [
          { name: 'SSH 连接', status: 'success', message: '连接到演示中转服务器' },
          { name: '安装 HAProxy', status: 'success', message: '转发组件已就绪' },
          { name: '绑定后端', status: 'success', message: '已创建监听端口与出口节点绑定' },
          { name: '配置 reload', status: 'success', message: 'HAProxy 校验通过并已重载' },
        ],
      }))
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
      if (request.method() === 'POST') {
        return fulfillJson(route, json({
          codes: ['DEMO-PRO-8K2M', 'DEMO-PRO-9L3N', 'DEMO-PRO-4Q7X'],
          count: 3,
          plan_name: 'Pro 双池 300G',
        }))
      }
      return fulfillJson(route, paginated(redeemCodes, 'codes'))
    }
    if (pathname === '/api/admin/subscription-tokens') {
      return fulfillJson(route, paginated(tokens, 'tokens'))
    }
    if (pathname === '/api/admin/logs/runtime') {
      return fulfillJson(route, json({ source: url.searchParams.get('source') || 'api', lines: runtimeLogLines, count: runtimeLogLines.length }))
    }
    if (pathname === '/api/admin/logs/deployments') {
      return fulfillJson(route, paginated(deploymentLogs, 'logs'))
    }
    if (pathname === '/api/admin/logs/operations') {
      return fulfillJson(route, paginated(operationLogs, 'logs'))
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
    const guestPage = await context.newPage()
    await installDemoApi(guestPage, 'user')

    const guestPages = [
      ['user-login', '/login', '用户登录'],
      ['user-register', '/register', '用户注册'],
      ['admin-login', '/admin/login', '管理后台登录'],
    ]
    for (const [name, url, expectedText] of guestPages) {
      await guestPage.goto(url)
      await screenshot(guestPage, name, { expectedText })
    }
    await guestPage.close()

    const page = await context.newPage()
    await installDemoApi(page, 'admin')

    const adminPages = [
      ['admin-dashboard', '/admin', '管理后台仪表盘'],
      ['admin-plans', '/admin/plans', '套餐管理'],
      ['admin-node-groups', '/admin/node-groups', '节点分组管理'],
      ['admin-nodes', '/admin/nodes', '出口节点管理'],
      ['admin-relays', '/admin/relays', '中转节点管理'],
      ['admin-users', '/admin/users', '用户管理'],
      ['admin-orders', '/admin/orders', '订单管理'],
      ['admin-redeem-codes', '/admin/redeem-codes', '兑换码管理'],
      ['admin-subscription-tokens', '/admin/subscription-tokens', '订阅 Token 管理'],
      ['admin-logs', '/admin/logs', '日志中心'],
    ]

    for (const [name, url, expectedText] of adminPages) {
      await page.goto(url)
      await screenshot(page, name, { expectedText })
    }

    await page.goto('/admin/nodes')
    await page.getByRole('button', { name: '一键部署' }).click()
    await page.getByPlaceholder('例如：154.219.97.219').fill('203.0.113.10')
    await page.locator('.el-dialog input[type="password"]').fill('demo-password')
    await page.locator('.el-dialog .el-select').filter({ hasText: 'TCP + Reality' }).click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: 'XHTTP + Reality' }).click()
    await page.locator('.el-dialog .el-form-item').filter({ hasText: '多 IP 服务器' }).locator('.el-switch').click()
    await page.getByRole('button', { name: '扫描出口 IP' }).click()
    await page.getByText('203.0.113.11').waitFor({ state: 'visible', timeout: 30_000 })
    await page.locator('.scan-ip-table tbody .el-checkbox').nth(0).click()
    await page.locator('.scan-ip-table tbody .el-checkbox').nth(1).click()
    await screenshot(page, 'admin-node-multi-ip-deploy', { expectedText: '2 个公网出口 IP 可用' })

    await page.goto('/admin/nodes')
    await page.getByRole('button', { name: '新增节点' }).click()
    const nodeDialog = page.locator('.el-dialog').filter({ hasText: '新增节点' })
    await nodeDialog.locator('.el-form-item').filter({ hasText: '节点名称' }).getByRole('textbox').fill('美国家宽3')
    await nodeDialog.getByPlaceholder('例如：hk.example.com').fill('us-home-3.demo.raypilot.test')
    await nodeDialog.locator('.el-form-item').filter({ hasText: '流量池' }).locator('.el-select').click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: '家宽流量' }).click()
    await nodeDialog.locator('.el-form-item').filter({ hasText: '出站方式' }).locator('.el-select').click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: '上游 SOCKS5' }).click()
    await nodeDialog.locator('textarea').fill('socks5://demo-user:demo-pass@res-us-3.demo.raypilot.test:3012')
    await nodeDialog.locator('.el-form-item').filter({ hasText: '传输模式' }).locator('.el-select').click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: 'XHTTP + Reality' }).click()
    await page.keyboard.press('Escape')
    await screenshot(page, 'admin-node-residential-socks5', { expectedText: '上游代理 URL' })

    await page.goto('/admin/relays')
    await page.getByRole('button', { name: '一键部署中转' }).click()
    const relayDeployDialog = page.locator('.el-dialog').filter({ hasText: '一键部署中转节点' })
    await relayDeployDialog.getByPlaceholder('例如：154.219.97.219').fill('198.51.100.20')
    await relayDeployDialog.locator('input[type="password"]').fill('demo-password')
    await relayDeployDialog.locator('.el-form-item').filter({ hasText: '中转名称' }).getByRole('textbox').fill('香港-转-美国')
    await relayDeployDialog.locator('.el-form-item').filter({ hasText: '出口节点' }).locator('.el-select').click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: '美国-TCP' }).click()
    await screenshot(page, 'admin-relay-one-click-deploy', { expectedText: '替换旧角色' })

    await page.goto('/admin/redeem-codes')
    await page.getByRole('button', { name: '生成兑换码' }).click()
    const redeemDialog = page.locator('.el-dialog').filter({ hasText: '生成兑换码' })
    await redeemDialog.locator('.el-form-item').filter({ hasText: '套餐' }).locator('.el-select').click()
    await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: 'Pro 双池 300G' }).click()
    await redeemDialog.getByRole('button', { name: '生成' }).click()
    await page.getByText('本次生成的兑换码').waitFor({ state: 'visible', timeout: 30_000 })
    await screenshot(page, 'admin-redeem-code-generated', { expectedText: '套餐：Pro 双池 300G' })

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
      ['user-orders', '/orders', '我的订单'],
      ['user-redeem', '/redeem', '兑换码'],
      ['user-profile', '/profile', '个人资料'],
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
      await stopFrontendServer(server)
    }
  }
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
