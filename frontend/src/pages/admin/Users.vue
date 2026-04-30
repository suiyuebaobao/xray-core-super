<template>
  <div class="admin-users">
    <div class="header">
      <h2>用户管理</h2>
      <div class="header-actions">
        <el-input v-model="searchText" placeholder="搜索用户名" style="width: 220px" clearable @keyup.enter="fetchUsers" />
        <el-button type="primary" @click="showCreateDialog">新增用户</el-button>
      </div>
    </div>

    <div v-if="selectedUsers.length" class="batch-toolbar">
      <span>已选择 {{ selectedUsers.length }} 个用户</span>
      <div>
        <el-button size="small" @click="clearUserSelection">取消选择</el-button>
        <el-button size="small" type="danger" :loading="batchDeleting" @click="handleBatchDelete">批量删除</el-button>
      </div>
    </div>

    <el-table
      ref="usersTableRef"
      :data="users"
      border
      style="width: 100%"
      v-loading="loading"
      @selection-change="handleUserSelectionChange"
    >
      <el-table-column type="selection" width="48" :selectable="canSelectUser" />
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="username" label="用户名" min-width="120" />
      <el-table-column prop="email" label="邮箱" min-width="180" />
      <el-table-column label="套餐" min-width="190">
        <template #default="{ row }">
          <div class="user-plan-cell">
            <div class="user-plan-main">
              <span>{{ userPlanLabel(row) }}</span>
              <el-tag :type="userSubscriptionStatusType(row)" size="small">{{ userSubscriptionStatusLabel(row) }}</el-tag>
            </div>
            <small v-if="row.subscription_expire_date">到期 {{ formatDate(row.subscription_expire_date) }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="剩余流量" min-width="190">
        <template #default="{ row }">
          <div v-if="row.subscription" class="traffic-cell">
            <div class="traffic-line">
              <strong>{{ remainingTrafficLabel(row) }}</strong>
              <span>{{ trafficUsageLabel(row) }}</span>
            </div>
            <el-progress :percentage="trafficPercent(row)" :show-text="false" :status="trafficProgressStatus(row)" />
            <small>已用 {{ formatBytes(row.used_traffic) }} / {{ formatTrafficLimit(row.traffic_limit) }}</small>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="80">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'danger'">{{ row.status === 'active' ? '正常' : '禁用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="is_admin" label="角色" width="80">
        <template #default="{ row }">{{ row.is_admin ? '管理员' : '用户' }}</template>
      </el-table-column>
      <el-table-column prop="last_login_at" label="最后登录" width="180">
        <template #default="{ row }">{{ row.last_login_at ? formatDate(row.last_login_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="400">
        <template #default="{ row }">
          <el-button size="small" type="primary" @click="handleSubscription(row)">订阅</el-button>
          <el-button size="small" type="info" @click="handleUsage(row)">用量</el-button>
          <el-button size="small" :type="row.status === 'active' ? 'danger' : 'success'" @click="handleToggleStatus(row)">
            {{ row.status === 'active' ? '禁用' : '启用' }}
          </el-button>
          <el-button size="small" type="warning" @click="handleResetPassword(row)">重置密码</el-button>
          <el-button size="small" type="danger" :disabled="isCurrentUser(row)" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <div class="pagination" style="margin-top: 16px; text-align: right">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchUsers"
      />
    </div>

    <el-dialog v-model="createDialogVisible" title="新增用户" width="520px">
      <el-form :model="createForm" :rules="createRules" ref="createFormRef" label-width="100px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="createForm.username" />
        </el-form-item>
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="createForm.email" />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="createForm.password" type="password" show-password />
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-select v-model="createForm.status" style="width: 100%">
            <el-option label="正常" value="active" />
            <el-option label="禁用" value="disabled" />
          </el-select>
        </el-form-item>
        <el-form-item label="角色">
          <el-switch v-model="createForm.is_admin" active-text="管理员" inactive-text="用户" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreateUser" :loading="creatingUser">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="subDialogVisible" :title="`订阅管理 - ${currentUser?.username || ''}`" width="620px">
      <el-alert
        v-if="!currentSubscription"
        title="当前用户暂无订阅，保存后将创建新订阅"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      />
      <el-form :model="subForm" :rules="subRules" ref="subFormRef" label-width="110px" v-loading="subLoading">
        <el-form-item label="套餐" prop="plan_id">
          <el-select v-model="subForm.plan_id" placeholder="选择套餐" style="width: 100%">
            <el-option v-for="plan in plans" :key="plan.id" :label="formatPlanLabel(plan)" :value="plan.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-select v-model="subForm.status" style="width: 100%">
            <el-option label="正常" value="ACTIVE" />
            <el-option label="暂停" value="SUSPENDED" />
            <el-option label="过期" value="EXPIRED" />
          </el-select>
        </el-form-item>
        <el-form-item label="到期时间" prop="expire_date">
          <el-date-picker v-model="subForm.expire_date" type="datetime" style="width: 100%" />
        </el-form-item>
        <el-form-item label="总流量(GB)" prop="trafficLimitGB">
          <el-input-number v-model="subForm.trafficLimitGB" :min="0" :precision="0" />
        </el-form-item>
        <el-form-item label="已用(GB)">
          <el-input-number v-model="subForm.usedTrafficGB" :min="0" :precision="2" />
        </el-form-item>
      </el-form>

      <div class="token-list">
        <div class="token-list-header">
          <span>订阅 Token</span>
          <el-button size="small" type="warning" :loading="tokenResetting" @click="resetCurrentUserToken">重置 Token</el-button>
        </div>
        <div v-if="activeTokens.length">
          <div v-for="token in activeTokens" :key="token.id" class="token-row">
            <span>{{ tokenUrl(token.token, 'clash') }}</span>
            <div class="token-actions">
              <el-button size="small" type="primary" @click="copyTokenUrl(token.token, 'clash')">Clash</el-button>
              <el-button size="small" @click="copyTokenUrl(token.token, 'base64')">Base64</el-button>
              <el-button size="small" @click="copyTokenUrl(token.token, 'plain')">URI</el-button>
            </div>
          </div>
        </div>
        <el-empty v-else description="暂无可用 Token" :image-size="60" />
      </div>

      <template #footer>
        <el-button @click="subDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveSubscription" :loading="subSaving">保存订阅</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="usageDialogVisible" :title="`用量记录 - ${usageUser?.username || ''}`" width="920px">
      <div v-loading="usageLoading">
        <template v-if="usageData">
          <div class="usage-summary">
            <div class="usage-metric">
              <span>今日</span>
              <strong>{{ formatBytes(usageData.summary?.today?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.today?.upload) }} / 下行 {{ formatBytes(usageData.summary?.today?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>本周</span>
              <strong>{{ formatBytes(usageData.summary?.current_week?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.current_week?.upload) }} / 下行 {{ formatBytes(usageData.summary?.current_week?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>本月</span>
              <strong>{{ formatBytes(usageData.summary?.current_month?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.current_month?.upload) }} / 下行 {{ formatBytes(usageData.summary?.current_month?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>截止今日</span>
              <strong>{{ formatBytes(usageData.summary?.subscription_to_today?.total) }}</strong>
              <small>{{ usageData.plan_name || '当前范围' }}</small>
            </div>
          </div>

          <el-descriptions :column="3" border class="usage-subscription">
            <el-descriptions-item label="套餐">{{ usageData.plan_name || '无套餐' }}</el-descriptions-item>
            <el-descriptions-item label="订阅状态">{{ usageData.has_active_subscription ? '有效' : '无有效套餐' }}</el-descriptions-item>
            <el-descriptions-item label="套餐已用">{{ formatBytes(usageData.subscription?.used_traffic) }} / {{ formatTrafficLimit(usageData.subscription?.traffic_limit) }}</el-descriptions-item>
          </el-descriptions>

          <el-tabs v-model="usageTab">
            <el-tab-pane label="按天" name="daily">
              <el-table :data="usageData.daily || []" border height="320">
                <el-table-column prop="date" label="日期" width="130" />
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="按周" name="weekly">
              <el-table :data="usageData.weekly || []" border height="320">
                <el-table-column label="周期" width="220">
                  <template #default="{ row }">{{ row.start_at }} 至 {{ row.end_at }}</template>
                </el-table-column>
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="按月" name="monthly">
              <el-table :data="usageData.monthly || []" border height="320">
                <el-table-column prop="month" label="月份" width="130" />
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="最近明细" name="recent">
              <el-table :data="usageData.recent || []" border height="320">
                <el-table-column label="时间" width="180">
                  <template #default="{ row }">{{ formatDate(row.recorded_at) }}</template>
                </el-table-column>
                <el-table-column label="节点" min-width="140">
                  <template #default="{ row }">{{ row.node_name || `节点 #${row.node_id}` }}</template>
                </el-table-column>
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.delta_upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.delta_download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.delta_total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
          </el-tabs>
        </template>
        <el-empty v-else description="暂无用量记录" />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
// 管理后台 - 用户管理页。
import { ref, reactive, computed, onMounted } from 'vue'
import { adminApi } from '@/api'
import { useUserStore } from '@/stores/user'
import { ElMessage, ElMessageBox } from 'element-plus'

const userStore = useUserStore()
const users = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const searchText = ref('')
const plans = ref([])
const usersTableRef = ref(null)
const selectedUsers = ref([])
const batchDeleting = ref(false)
const createDialogVisible = ref(false)
const creatingUser = ref(false)
const createFormRef = ref(null)
const subDialogVisible = ref(false)
const subLoading = ref(false)
const subSaving = ref(false)
const subFormRef = ref(null)
const currentUser = ref(null)
const currentSubscription = ref(null)
const subTokens = ref([])
const tokenResetting = ref(false)
const usageDialogVisible = ref(false)
const usageLoading = ref(false)
const usageUser = ref(null)
const usageData = ref(null)
const usageTab = ref('daily')

const createForm = reactive({
  username: '',
  email: '',
  password: '',
  status: 'active',
  is_admin: false,
})

const subForm = reactive({
  plan_id: null,
  status: 'ACTIVE',
  expire_date: null,
  trafficLimitGB: 0,
  usedTrafficGB: 0,
})

const subRules = {
  plan_id: [{ required: true, message: '请选择套餐', trigger: 'change' }],
  status: [{ required: true, message: '请选择状态', trigger: 'change' }],
  expire_date: [{ required: true, message: '请选择到期时间', trigger: 'change' }],
  trafficLimitGB: [{ required: true, message: '请输入总流量', trigger: 'blur' }],
}

const createRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 32, message: '用户名长度为 3-32 位', trigger: 'blur' },
  ],
  email: [{ type: 'email', message: '邮箱格式不正确', trigger: 'blur' }],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, message: '密码至少 6 位', trigger: 'blur' },
  ],
  status: [{ required: true, message: '请选择状态', trigger: 'change' }],
}

const activeTokens = computed(() => subTokens.value.filter((t) => !t.is_revoked && (!t.expires_at || new Date(t.expires_at) > new Date())))

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

function formatPlanLabel(plan) {
  return `${plan.name} / ${formatBytes(plan.traffic_limit)} / ${plan.duration_days} 天`
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let val = bytes
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024
    i++
  }
  return val.toFixed(1) + ' ' + units[i]
}

function formatTrafficLimit(bytes) {
  if (!bytes) return '不限量'
  return formatBytes(bytes)
}

function userPlanLabel(row) {
  if (!row.subscription) return '无套餐'
  if (row.plan_name) return row.plan_name
  if (row.plan?.name) return row.plan.name
  return `套餐 #${row.subscription.plan_id}`
}

function userSubscriptionStatusLabel(row) {
  if (!row.subscription) return '未开通'
  if (row.has_active_subscription) return '有效'
  const labels = {
    ACTIVE: '已过期',
    EXPIRED: '已过期',
    SUSPENDED: '已暂停',
    PENDING: '待生效',
  }
  return labels[row.subscription_status || row.subscription?.status] || row.subscription_status || '未知'
}

function userSubscriptionStatusType(row) {
  if (!row.subscription) return 'info'
  if (row.has_active_subscription) return 'success'
  const status = row.subscription_status || row.subscription?.status
  if (status === 'SUSPENDED') return 'warning'
  return status === 'PENDING' ? 'info' : 'danger'
}

function remainingTrafficLabel(row) {
  if (row.traffic_unlimited || !row.traffic_limit) return '不限量'
  return formatBytes(row.remaining_traffic)
}

function trafficPercent(row) {
  if (!row.traffic_limit) return 0
  return Math.min(100, Math.round(Number(row.traffic_usage_percent || 0)))
}

function trafficUsageLabel(row) {
  if (!row.traffic_limit) return '不限量'
  return `${trafficPercent(row)}%`
}

function trafficProgressStatus(row) {
  const percent = trafficPercent(row)
  if (percent >= 100) return 'exception'
  if (percent >= 80) return 'warning'
  return undefined
}

function bytesToGB(bytes) {
  return Math.round((Number(bytes || 0) / 1024 / 1024 / 1024) * 100) / 100
}

function gbToBytes(gb) {
  return Math.round(Number(gb || 0) * 1024 * 1024 * 1024)
}

function defaultExpireDate() {
  const d = new Date()
  d.setDate(d.getDate() + 30)
  return d
}

function resetCreateForm() {
  createForm.username = ''
  createForm.email = ''
  createForm.password = ''
  createForm.status = 'active'
  createForm.is_admin = false
}

function showCreateDialog() {
  resetCreateForm()
  createDialogVisible.value = true
}

async function handleCreateUser() {
  const valid = await createFormRef.value.validate().catch(() => false)
  if (!valid) return

  creatingUser.value = true
  try {
    await adminApi.users.create({
      username: createForm.username,
      email: createForm.email,
      password: createForm.password,
      status: createForm.status,
      is_admin: createForm.is_admin,
    })
    ElMessage.success('用户已创建')
    createDialogVisible.value = false
    page.value = 1
    await fetchUsers()
  } catch (err) {
    ElMessage.error(err.message || '创建失败')
  } finally {
    creatingUser.value = false
  }
}

async function handleToggleStatus(row) {
  const newStatus = row.status === 'active' ? 'disabled' : 'active'
  try {
    await adminApi.users.updateStatus(row.id, newStatus)
    ElMessage.success('操作成功')
    await fetchUsers()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  }
}

async function handleResetPassword(row) {
  try {
    const { value: newPassword } = await ElMessageBox.prompt('请输入新密码（至少 6 位）', `重置 ${row.username} 的密码`, {
      inputType: 'password',
      inputValidator: (v) => v && v.length >= 6 ? true : '密码至少 6 位',
    })
    await adminApi.users.resetPassword(row.id, newPassword)
    ElMessage.success('密码已重置')
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '重置失败')
    }
  }
}

function isCurrentUser(row) {
  return String(row.id) === String(userStore.user?.id)
}

function canSelectUser(row) {
  return !isCurrentUser(row)
}

function handleUserSelectionChange(selection) {
  selectedUsers.value = selection
}

function clearUserSelection() {
  usersTableRef.value?.clearSelection()
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除用户"${row.username}"吗？该操作会删除用户账号、订阅、订阅 Token、订单、支付记录和用量记录。`,
      '确认删除',
      { type: 'warning' },
    )
    await adminApi.users.delete(row.id)
    removeUsersFromList([row.id])
    ElMessage.success('删除成功')
    fetchUsers().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

async function handleBatchDelete() {
  const rows = [...selectedUsers.value]
  if (!rows.length) {
    ElMessage.warning('请选择要删除的用户')
    return
  }

  try {
    await ElMessageBox.confirm(
      `确定批量删除选中的 ${rows.length} 个用户吗？该操作会删除对应账号、订阅、订阅 Token、订单、支付记录和用量记录。`,
      '批量删除',
      { type: 'warning' },
    )
  } catch {
    return
  }

  batchDeleting.value = true
  const deletedIds = []
  const failed = []
  try {
    for (const row of rows) {
      try {
        await adminApi.users.delete(row.id)
        deletedIds.push(row.id)
      } catch (err) {
        failed.push(`${row.username || row.id}：${err.message || '删除失败'}`)
      }
    }

    if (deletedIds.length) {
      removeUsersFromList(deletedIds)
    }
    if (failed.length) {
      ElMessage.warning(`成功删除 ${deletedIds.length} 个，失败 ${failed.length} 个：${failed.join('；')}`)
    } else {
      ElMessage.success('批量删除成功')
    }
    fetchUsers().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } finally {
    batchDeleting.value = false
  }
}

function removeUsersFromList(ids) {
  const idSet = new Set(ids.map((id) => String(id)))
  const beforeCount = users.value.length
  users.value = users.value.filter((user) => !idSet.has(String(user.id)))
  selectedUsers.value = selectedUsers.value.filter((user) => !idSet.has(String(user.id)))
  const removedCount = beforeCount - users.value.length
  total.value = Math.max(0, total.value - removedCount)
  if (users.value.length === 0 && page.value > 1 && total.value > 0) {
    page.value -= 1
  }
}

async function handleSubscription(row) {
  currentUser.value = row
  currentSubscription.value = null
  subTokens.value = []
  subForm.plan_id = plans.value[0]?.id || null
  subForm.status = 'ACTIVE'
  subForm.expire_date = defaultExpireDate()
  subForm.trafficLimitGB = bytesToGB(plans.value[0]?.traffic_limit || 0)
  subForm.usedTrafficGB = 0
  subDialogVisible.value = true
  subLoading.value = true
  try {
    const res = await adminApi.users.subscription(row.id)
    currentSubscription.value = res.data.subscription || null
    subTokens.value = res.data.tokens || []
    if (currentSubscription.value) {
      subForm.plan_id = currentSubscription.value.plan_id
      subForm.status = currentSubscription.value.status
      subForm.expire_date = new Date(currentSubscription.value.expire_date)
      subForm.trafficLimitGB = bytesToGB(currentSubscription.value.traffic_limit)
      subForm.usedTrafficGB = bytesToGB(currentSubscription.value.used_traffic)
    }
  } catch (err) {
    ElMessage.error(err.message || '获取订阅失败')
  } finally {
    subLoading.value = false
  }
}

async function handleUsage(row) {
  usageUser.value = row
  usageData.value = null
  usageTab.value = 'daily'
  usageDialogVisible.value = true
  usageLoading.value = true
  try {
    const res = await adminApi.users.usage(row.id, { days: 30, weeks: 8, months: 12, recent: 50 })
    usageData.value = res.data
  } catch (err) {
    ElMessage.error(err.message || '获取用量失败')
  } finally {
    usageLoading.value = false
  }
}

async function saveSubscription() {
  const valid = await subFormRef.value.validate().catch(() => false)
  if (!valid || !currentUser.value) return

  subSaving.value = true
  try {
    const res = await adminApi.users.upsertSubscription(currentUser.value.id, {
      plan_id: subForm.plan_id,
      status: subForm.status,
      expire_date: new Date(subForm.expire_date).toISOString(),
      traffic_limit: gbToBytes(subForm.trafficLimitGB),
      used_traffic: gbToBytes(subForm.usedTrafficGB),
    })
    currentSubscription.value = res.data.subscription
    subTokens.value = res.data.tokens || []
    ElMessage.success('订阅已保存')
    await fetchUsers()
  } catch (err) {
    ElMessage.error(err.message || '保存失败')
  } finally {
    subSaving.value = false
  }
}

async function resetCurrentUserToken() {
  if (!currentUser.value) return
  try {
    await ElMessageBox.confirm(`确定要重置 ${currentUser.value.username} 的订阅 Token 吗？`, '确认重置', {
      type: 'warning',
    })
  } catch {
    return
  }

  tokenResetting.value = true
  try {
    const token = subTokens.value[0]
    if (token?.id) {
      await adminApi.subscriptionTokens.reset(token.id)
    } else {
      await adminApi.subscriptionTokens.create({ user_id: currentUser.value.id })
    }
    const res = await adminApi.users.subscription(currentUser.value.id)
    subTokens.value = res.data.tokens || []
    ElMessage.success('Token 已重置')
  } catch (err) {
    ElMessage.error(err.message || '重置失败')
  } finally {
    tokenResetting.value = false
  }
}

function tokenUrl(token, format) {
  if (!token) return ''
  return `${window.location.origin}/sub/${token}/${format}`
}

function copyTokenUrl(token, format) {
  const url = tokenUrl(token, format)
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('已复制')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

async function fetchUsers() {
  loading.value = true
  try {
    const res = await adminApi.users.list({ page: page.value, size: size.value, keyword: searchText.value })
    users.value = res.data.users || []
    total.value = res.data.total || 0
  } catch (err) {
    ElMessage.error('获取用户列表失败')
  } finally {
    loading.value = false
  }
}

async function fetchPlans() {
  try {
    const res = await adminApi.plans.list()
    plans.value = res.data.plans || []
  } catch {
    ElMessage.error('获取套餐列表失败')
  }
}

onMounted(() => {
  fetchPlans()
  fetchUsers()
})
</script>

<style scoped>
.admin-users {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.batch-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  background: #f5f7fa;
  color: #606266;
  font-size: 14px;
}
.user-plan-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.user-plan-main,
.traffic-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.user-plan-main {
  justify-content: flex-start;
  flex-wrap: wrap;
}
.user-plan-cell small,
.traffic-cell small,
.muted {
  color: #909399;
}
.traffic-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.traffic-line strong {
  font-size: 13px;
  color: #303133;
}
.traffic-line span {
  color: #909399;
  font-size: 12px;
}
.token-list {
  border-top: 1px solid #ebeef5;
  margin-top: 8px;
  padding-top: 12px;
}
.token-list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 10px;
  font-weight: 500;
}
.token-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.token-row span {
  flex: 1;
  font-family: monospace;
  font-size: 12px;
  word-break: break-all;
}
.token-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.usage-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
}
.usage-metric {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 12px;
  min-width: 0;
}
.usage-metric span,
.usage-metric small {
  display: block;
  color: #909399;
}
.usage-metric strong {
  display: block;
  font-size: 20px;
  line-height: 28px;
  margin: 4px 0;
  color: #303133;
}
.usage-subscription {
  margin-bottom: 12px;
}
@media (max-width: 900px) {
  .usage-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
