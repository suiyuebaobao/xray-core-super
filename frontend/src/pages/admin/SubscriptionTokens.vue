<template>
  <div class="admin-subscription-tokens">
    <div class="header">
      <h2>订阅 Token 管理</h2>
      <el-button type="primary" @click="showCreateDialog">补齐 Token</el-button>
    </div>

    <el-table :data="tokens" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column label="用户" min-width="160">
        <template #default="{ row }">
          <div class="user-cell">
            <span>{{ userLabel(row) }}</span>
            <small>#{{ row.user_id }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="套餐状态" min-width="220">
        <template #default="{ row }">
          <div class="plan-cell">
            <div class="plan-main">
              <span>{{ planLabel(row) }}</span>
              <el-tag :type="subscriptionStatusType(row)" size="small">
                {{ subscriptionStatusLabel(row) }}
              </el-tag>
            </div>
            <small>{{ subscriptionMeta(row) }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="token" label="Token" width="260">
        <template #default="{ row }">
          <el-tag size="small" type="info">{{ row.token }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="订阅链接" min-width="360">
        <template #default="{ row }">
          <div class="link-cell">
            <div class="link-preview">{{ subscriptionUrl(row.token, 'clash') }}</div>
            <div class="link-actions">
              <el-button size="small" type="primary" :disabled="tokenActionDisabled(row)" @click="copySubscriptionUrl(row, 'clash')">Clash</el-button>
              <el-button size="small" :disabled="tokenActionDisabled(row)" @click="copySubscriptionUrl(row, 'base64')">Base64</el-button>
              <el-button size="small" :disabled="tokenActionDisabled(row)" @click="copySubscriptionUrl(row, 'plain')">URI</el-button>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="token_status" label="Token状态" width="110">
        <template #default="{ row }">
          <el-tag :type="tokenStatusType(row)" size="small">
            {{ tokenStatusLabel(row) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_used_at" label="最后使用" width="180">
        <template #default="{ row }">{{ row.last_used_at ? formatDate(row.last_used_at) : '-' }}</template>
      </el-table-column>
      <el-table-column prop="expires_at" label="Token过期" width="180">
        <template #default="{ row }">{{ row.expires_at ? formatDate(row.expires_at) : '永不过期' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <el-button
            size="small"
            type="warning"
            @click="handleReset(row)"
          >重置</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination" style="margin-top: 16px; text-align: right">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchTokens"
      />
    </div>

    <el-dialog v-model="dialogVisible" title="补齐订阅 Token" width="460px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="100px">
        <el-form-item label="用户" prop="user_id">
          <el-select v-model="form.user_id" filterable placeholder="选择用户" style="width: 100%">
            <el-option
              v-for="user in users"
              :key="user.id"
              :label="formatUserOption(user)"
              :value="user.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="过期时间" prop="expires_at">
          <el-date-picker
            v-model="form.expires_at"
            type="datetime"
            placeholder="可选，留空表示永不过期"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">补齐</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 管理后台 - 订阅 Token 管理页。
import { ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'

const tokens = ref([])
const users = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const dialogVisible = ref(false)
const creating = ref(false)
const formRef = ref(null)

const form = reactive({
  user_id: null,
  expires_at: '',
})

const rules = {
  user_id: [{ required: true, message: '请选择用户', trigger: 'change' }],
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

function formatUserOption(user) {
  return user.email ? `${user.username} / ${user.email}` : user.username
}

function userLabel(row) {
  if (row.username) return row.username
  const user = users.value.find((item) => item.id === row.user_id)
  return user ? user.username : `用户 #${row.user_id}`
}

function planLabel(row) {
  if (!row.subscription) return '无套餐'
  if (row.plan_name) return row.plan_name
  if (row.plan?.name) return row.plan.name
  return `套餐 #${row.subscription.plan_id}`
}

function subscriptionStatusLabel(row) {
  if (!row.subscription) return '未开通'
  if (row.has_active_subscription) return '有效套餐'
  const labels = {
    ACTIVE: '已过期',
    EXPIRED: '已过期',
    SUSPENDED: '已暂停',
    PENDING: '待生效',
  }
  return labels[row.subscription.status] || row.subscription.status || '未知'
}

function subscriptionStatusType(row) {
  if (!row.subscription) return 'info'
  if (row.has_active_subscription) return 'success'
  const types = {
    ACTIVE: 'danger',
    EXPIRED: 'danger',
    SUSPENDED: 'warning',
    PENDING: 'info',
  }
  return types[row.subscription.status] || 'info'
}

function subscriptionMeta(row) {
  if (!row.subscription) return '该用户当前没有套餐'
  const expire = row.subscription.expire_date ? formatDate(row.subscription.expire_date) : '-'
  return `到期 ${expire} / 流量 ${formatBytes(row.subscription.used_traffic)} / ${formatTrafficLimit(row.subscription.traffic_limit)}`
}

function formatBytes(value) {
  const size = Number(value || 0)
  if (size <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = size
  let index = 0
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024
    index += 1
  }
  const digits = current >= 10 || index === 0 ? 0 : 1
  return `${current.toFixed(digits)} ${units[index]}`
}

function formatTrafficLimit(value) {
  if (Number(value || 0) === 0) return '不限量'
  return formatBytes(value)
}

function tokenStatusLabel(row) {
  const labels = {
    ACTIVE: '可用',
    REVOKED: '已撤销',
    EXPIRED: '已过期',
  }
  if (row.token_status) return labels[row.token_status] || row.token_status
  if (row.is_revoked) return '已撤销'
  if (row.is_expired) return '已过期'
  return '可用'
}

function tokenStatusType(row) {
  const status = row.token_status || (row.is_revoked ? 'REVOKED' : row.is_expired ? 'EXPIRED' : 'ACTIVE')
  return status === 'ACTIVE' ? 'success' : 'danger'
}

function tokenActionDisabled(row) {
  return row.is_usable === false || row.is_revoked || row.is_expired
}

async function showCreateDialog() {
  form.user_id = null
  form.expires_at = ''
  if (!users.value.length) {
    await fetchUsers()
  }
  dialogVisible.value = true
}

async function handleCreate() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  creating.value = true
  try {
    const body = { user_id: form.user_id }
    if (form.expires_at) {
      body.expires_at = form.expires_at
    }
    await adminApi.subscriptionTokens.create(body)
    ElMessage.success('Token 已可用，可在列表复制订阅链接')
    dialogVisible.value = false
    page.value = 1
    await fetchTokens()
  } catch (err) {
    ElMessage.error(err.message || '生成失败')
  } finally {
    creating.value = false
  }
}

async function handleReset(row) {
  try {
    await ElMessageBox.confirm(`确定要重置 ${userLabel(row)} 的订阅 Token 吗？`, '确认重置', {
      type: 'warning',
    })
  } catch {
    return
  }

  try {
    await adminApi.subscriptionTokens.reset(row.id)
    ElMessage.success('Token 已重置')
    await fetchTokens()
  } catch (err) {
    ElMessage.error(err.message || '重置失败')
  }
}

function subscriptionUrl(token, format) {
  if (!token) return ''
  return `${window.location.origin}/sub/${token}/${format}`
}

function copySubscriptionUrl(row, format) {
  const url = subscriptionUrl(row.token, format)
  if (!url) return
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('订阅链接已复制')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

async function fetchTokens() {
  loading.value = true
  try {
    const res = await adminApi.subscriptionTokens.list({ page: page.value, size: size.value })
    tokens.value = res.data.tokens || []
    total.value = res.data.total || 0
  } catch (err) {
    ElMessage.error('获取 Token 列表失败')
  } finally {
    loading.value = false
  }
}

async function fetchUsers() {
  try {
    const res = await adminApi.users.list({ page: 1, size: 1000 })
    users.value = res.data.users || []
  } catch (err) {
    ElMessage.error('获取用户列表失败')
  }
}

onMounted(() => {
  fetchUsers()
  fetchTokens()
})
</script>

<style scoped>
.admin-subscription-tokens {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.user-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.user-cell small {
  color: #909399;
}
.plan-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.plan-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.plan-cell small {
  color: #909399;
}
.link-cell {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.link-preview {
  font-family: monospace;
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}
.link-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
