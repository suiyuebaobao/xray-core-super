<template>
  <div class="admin-relays">
    <div class="header">
      <h2>中转节点管理</h2>
      <div>
        <el-button type="success" @click="showDeployDialog">一键部署中转</el-button>
        <el-button type="primary" @click="showAddDialog">新增中转</el-button>
      </div>
    </div>

    <div v-if="selectedRelays.length" class="batch-toolbar">
      <span>已选择 {{ selectedRelays.length }} 个中转节点</span>
      <div>
        <el-button size="small" @click="clearRelaySelection">取消选择</el-button>
        <el-button size="small" type="danger" :loading="batchDeleting" @click="handleBatchDelete">批量删除</el-button>
      </div>
    </div>

    <el-table
      ref="relaysTableRef"
      :data="relays"
      border
      style="width: 100%"
      v-loading="loading"
      @selection-change="handleRelaySelectionChange"
    >
      <el-table-column type="selection" width="48" />
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="name" label="中转名称" min-width="140" />
      <el-table-column prop="host" label="入口地址" min-width="170" />
      <el-table-column prop="forwarder_type" label="转发组件" width="110">
        <template #default="{ row }">
          <el-tag effect="plain">{{ row.forwarder_type || 'haproxy' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="后端绑定" min-width="300">
        <template #default="{ row }">
          <div class="relation-cell">
            <template v-if="row.backends?.length">
              <el-tag
                v-for="backend in row.backends"
                :key="backend.id"
                size="small"
                :type="backend.is_enabled ? 'success' : 'info'"
                effect="plain"
              >
                {{ backend.name || formatBackend(backend) }}
              </el-tag>
            </template>
            <span v-else class="empty-text">未绑定</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="row.is_enabled ? statusType(row.status) : 'info'">
            {{ row.is_enabled ? statusLabel(row.status) : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_heartbeat_at" label="最后心跳" width="180">
        <template #default="{ row }">{{ row.last_heartbeat_at ? formatDate(row.last_heartbeat_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="280">
        <template #default="{ row }">
          <el-button size="small" type="primary" text @click="showBackendsDialog(row)">管理后端</el-button>
          <el-button size="small" @click="showEditDialog(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑中转' : '新增中转'" width="600px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="中转名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="入口地址" prop="host">
          <el-input v-model="form.host" placeholder="relay.example.com" />
        </el-form-item>
        <el-form-item label="转发组件">
          <el-select v-model="form.forwarder_type" disabled style="width: 100%">
            <el-option label="HAProxy" value="haproxy" />
          </el-select>
        </el-form-item>
        <el-form-item label="Agent 地址" prop="agent_base_url">
          <el-input v-model="form.agent_base_url" placeholder="http://relay-ip:8080" />
        </el-form-item>
        <el-form-item label="Agent Token" prop="agent_token">
          <el-input v-model="form.agent_token" type="password" show-password placeholder="创建必填，编辑留空表示不重置" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.is_enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="backendsDialogVisible" :title="backendsDialogTitle" width="1100px">
      <div class="backend-toolbar">
        <span class="backend-count">已配置 {{ backendRows.length }} 条后端</span>
        <el-button type="primary" @click="addBackendRow">新增后端</el-button>
      </div>
      <el-table :data="backendRows" border max-height="460" empty-text="暂无后端绑定">
        <el-table-column label="名称" min-width="150">
          <template #default="{ row }">
            <el-input v-model="row.name" placeholder="香港中转" />
          </template>
        </el-table-column>
        <el-table-column label="出口节点" min-width="180">
          <template #default="{ row }">
            <el-select v-model="row.exit_node_id" filterable placeholder="选择出口节点" style="width: 100%" @change="handleExitNodeChange(row)">
              <el-option v-for="node in allNodes" :key="node.id" :label="formatNodeOption(node)" :value="node.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="监听端口" width="130">
          <template #default="{ row }">
            <el-input-number v-model="row.listen_port" :min="1" :max="65535" controls-position="right" />
          </template>
        </el-table-column>
        <el-table-column label="目标地址" min-width="170">
          <template #default="{ row }">
            <div class="target-host-cell">
              <span>{{ targetHostLabel(row) }}</span>
              <small v-if="findExitNode(row)">由出口节点自动带出</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="目标端口" width="130">
          <template #default="{ row }">
            <el-input-number v-model="row.target_port" :min="1" :max="65535" controls-position="right" />
          </template>
        </el-table-column>
        <el-table-column label="排序" width="100">
          <template #default="{ row }">
            <el-input-number v-model="row.sort_weight" :min="0" controls-position="right" />
          </template>
        </el-table-column>
        <el-table-column label="启用" width="90">
          <template #default="{ row }">
            <el-switch v-model="row.is_enabled" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ $index }">
            <el-button size="small" type="danger" text @click="removeBackendRow($index)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="backendsDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveBackends" :loading="backendsSaving">保存绑定</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="deployDialogVisible" title="一键部署中转节点" width="520px">
      <el-form :model="deployForm" :rules="deployRules" ref="deployFormRef" label-width="120px">
        <el-alert title="部署成功后会创建中转记录，启动 node-agent relay 模式和 HAProxy" type="info" :closable="false" style="margin-bottom: 16px" />
        <el-form-item label="服务器 IP" prop="ssh_host">
          <el-input v-model="deployForm.ssh_host" placeholder="例如：154.219.97.219" />
        </el-form-item>
        <el-form-item label="SSH 端口" prop="ssh_port">
          <el-input-number v-model="deployForm.ssh_port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="SSH 用户" prop="ssh_user">
          <el-input v-model="deployForm.ssh_user" placeholder="例如：root" />
        </el-form-item>
        <el-form-item label="SSH 密码" prop="ssh_password">
          <el-input v-model="deployForm.ssh_password" type="password" show-password />
        </el-form-item>
        <el-form-item label="中转名称" prop="relay_name">
          <el-input v-model="deployForm.relay_name" placeholder="可选，默认为 raypilot-relay-IP" />
        </el-form-item>
        <el-form-item label="中心服务地址" prop="center_url">
          <el-input v-model="deployForm.center_url" placeholder="例如：http://156.238.231.216" />
        </el-form-item>
        <el-form-item label="中转 Token">
          <el-input v-model="deployForm.relay_token" placeholder="留空自动生成" />
        </el-form-item>
      </el-form>

      <div v-if="deploySteps.length > 0" class="deploy-steps">
        <div v-for="(step, index) in deploySteps" :key="index" class="deploy-step">
          <el-icon :class="step.status">
            <CircleCheck v-if="step.status === 'success'" />
            <CircleClose v-else-if="step.status === 'failed'" />
            <Loading v-else />
          </el-icon>
          <span>{{ step.name }}</span>
          <span class="step-msg">{{ step.message }}</span>
        </div>
      </div>

      <template #footer>
        <el-button @click="deployDialogVisible = false" :disabled="deploying">取消</el-button>
        <el-button type="success" @click="handleDeploy" :loading="deploying" :disabled="deploying">开始部署</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleCheck, CircleClose, Loading } from '@element-plus/icons-vue'

const relays = ref([])
const allNodes = ref([])
const loading = ref(false)
const relaysTableRef = ref(null)
const selectedRelays = ref([])
const batchDeleting = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const saving = ref(false)
const formRef = ref(null)
const backendsDialogVisible = ref(false)
const selectedRelay = ref(null)
const backendRows = ref([])
const backendsSaving = ref(false)
const deployDialogVisible = ref(false)
const deployFormRef = ref(null)
const deploying = ref(false)
const deploySteps = ref([])

const form = reactive({
  name: '',
  host: '',
  forwarder_type: 'haproxy',
  agent_base_url: '',
  agent_token: '',
  is_enabled: true,
})

const deployForm = reactive({
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  relay_name: '',
  center_url: window.location.origin,
  relay_token: '',
  forwarder_type: 'haproxy',
})

const rules = {
  name: [{ required: true, message: '请输入中转名称', trigger: 'blur' }],
  host: [{ required: true, message: '请输入入口地址', trigger: 'blur' }],
  agent_base_url: [{ required: true, message: '请输入 Agent 地址', trigger: 'blur' }],
  agent_token: [{
    required: true,
    trigger: 'blur',
    validator: (rule, value, callback) => {
      if (!isEdit.value && !value) {
        callback(new Error('请输入 Agent Token'))
      } else {
        callback()
      }
    },
  }],
}

const deployRules = {
  ssh_host: [{ required: true, message: '请输入服务器 IP', trigger: 'blur' }],
  ssh_user: [{ required: true, message: '请输入 SSH 用户', trigger: 'blur' }],
  ssh_password: [{ required: true, message: '请输入 SSH 密码', trigger: 'blur' }],
  center_url: [{ required: true, message: '请输入中心服务地址', trigger: 'blur' }],
}

const backendsDialogTitle = computed(() => {
  return selectedRelay.value ? `管理后端 - ${selectedRelay.value.name}` : '管理后端'
})

function statusLabel(status) {
  return status === 'online' ? '在线' : '离线'
}

function statusType(status) {
  return status === 'online' ? 'success' : 'warning'
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

function formatNodeAddress(row) {
  if (!row.host) return '-'
  return row.port ? `${row.host}:${row.port}` : row.host
}

function formatNodeOption(row) {
  const address = formatNodeAddress(row)
  return row.name ? `${row.name} / ${address}` : address
}

function formatBackend(backend) {
  return `${backend.listen_port} -> ${backend.target_host}:${backend.target_port}`
}

function normalizeNodes(data) {
  if (Array.isArray(data?.nodes)) return data.nodes
  if (Array.isArray(data)) return data
  return []
}

function normalizeBackends(data) {
  if (Array.isArray(data?.backends)) return data.backends
  if (Array.isArray(data)) return data
  return []
}

function resetForm() {
  form.name = ''
  form.host = ''
  form.forwarder_type = 'haproxy'
  form.agent_base_url = ''
  form.agent_token = ''
  form.is_enabled = true
}

function showAddDialog() {
  isEdit.value = false
  editingId.value = null
  resetForm()
  dialogVisible.value = true
}

function showEditDialog(row) {
  isEdit.value = true
  editingId.value = row.id
  form.name = row.name
  form.host = row.host
  form.forwarder_type = row.forwarder_type || 'haproxy'
  form.agent_base_url = row.agent_base_url
  form.agent_token = ''
  form.is_enabled = row.is_enabled
  dialogVisible.value = true
}

async function handleSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const payload = { ...form }
    if (isEdit.value) {
      await adminApi.relays.update(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      await adminApi.relays.create(payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await fetchRelays()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除中转节点"${row.name}"吗？启用中的后端绑定需要先删除或禁用。`, '确认删除', { type: 'warning' })
    await adminApi.relays.delete(row.id)
    removeRelaysFromList([row.id])
    ElMessage.success('删除成功')
    fetchRelays().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

function handleRelaySelectionChange(selection) {
  selectedRelays.value = selection
}

function clearRelaySelection() {
  relaysTableRef.value?.clearSelection()
}

async function handleBatchDelete() {
  const rows = [...selectedRelays.value]
  if (!rows.length) {
    ElMessage.warning('请选择要删除的中转节点')
    return
  }

  try {
    await ElMessageBox.confirm(`确定批量删除选中的 ${rows.length} 个中转节点吗？启用中的后端绑定需要先删除或禁用。`, '批量删除', { type: 'warning' })
  } catch {
    return
  }

  batchDeleting.value = true
  const deletedIds = []
  const failed = []
  try {
    for (const row of rows) {
      try {
        await adminApi.relays.delete(row.id)
        deletedIds.push(row.id)
      } catch (err) {
        failed.push(`${row.name || row.id}：${err.message || '删除失败'}`)
      }
    }

    if (deletedIds.length) {
      removeRelaysFromList(deletedIds)
    }
    if (failed.length) {
      ElMessage.warning(`成功删除 ${deletedIds.length} 个，失败 ${failed.length} 个：${failed.join('；')}`)
    } else {
      ElMessage.success('批量删除成功')
    }
    fetchRelays().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } finally {
    batchDeleting.value = false
  }
}

function removeRelaysFromList(ids) {
  const idSet = new Set(ids.map((id) => String(id)))
  relays.value = relays.value.filter((relay) => !idSet.has(String(relay.id)))
  selectedRelays.value = selectedRelays.value.filter((relay) => !idSet.has(String(relay.id)))
}

async function showBackendsDialog(row) {
  selectedRelay.value = row
  backendRows.value = []
  backendsDialogVisible.value = true
  await fetchBackendData(row.id)
}

async function fetchBackendData(relayId) {
  try {
    const [nodesRes, backendsRes] = await Promise.all([
      adminApi.nodes.list(),
      adminApi.relays.backends(relayId),
    ])
    allNodes.value = normalizeNodes(nodesRes.data)
    backendRows.value = normalizeBackends(backendsRes.data).map((backend) => ({
      _key: backend.id || Date.now() + Math.random(),
      id: backend.id,
      name: backend.name || '',
      exit_node_id: backend.exit_node_id,
      listen_port: backend.listen_port,
      target_host: backend.target_host,
      target_port: backend.target_port,
      is_enabled: backend.is_enabled,
      sort_weight: backend.sort_weight || 0,
    }))
  } catch (err) {
    ElMessage.error(err.message || '获取后端绑定失败')
  }
}

function addBackendRow() {
  backendRows.value.push({
    _key: Date.now() + Math.random(),
    name: '',
    exit_node_id: null,
    listen_port: 24443,
    target_host: '',
    target_port: 443,
    is_enabled: true,
    sort_weight: 0,
  })
}

function removeBackendRow(index) {
  backendRows.value.splice(index, 1)
}

function handleExitNodeChange(row) {
  const node = findExitNode(row)
  if (!node) return
  row.target_host = node.host || ''
  row.target_port = node.port || row.target_port || 443
  if (!row.name && selectedRelay.value) {
    row.name = `${selectedRelay.value.name} -> ${node.name || formatNodeAddress(node)}`
  }
}

function findExitNode(row) {
  return allNodes.value.find((item) => String(item.id) === String(row.exit_node_id))
}

function targetHostLabel(row) {
  const node = findExitNode(row)
  return node?.host || row.target_host || '选择出口节点后自动带出'
}

function resolveTargetHost(row) {
  return findExitNode(row)?.host || row.target_host || ''
}

function validateBackendRows() {
  const ports = new Set()
  for (const row of backendRows.value) {
    if (!row.exit_node_id) return '请选择出口节点'
    if (!row.listen_port || row.listen_port < 1 || row.listen_port > 65535) return '监听端口不合法'
    if (ports.has(row.listen_port)) return `监听端口 ${row.listen_port} 重复`
    ports.add(row.listen_port)
    if (!resolveTargetHost(row)) return '出口节点缺少目标地址'
    if (!row.target_port || row.target_port < 1 || row.target_port > 65535) return '目标端口不合法'
  }
  return ''
}

async function handleSaveBackends() {
  if (!selectedRelay.value) return
  const validationError = validateBackendRows()
  if (validationError) {
    ElMessage.error(validationError)
    return
  }

  backendsSaving.value = true
  try {
    const payload = backendRows.value.map((row) => ({
      id: row.id || 0,
      name: row.name,
      exit_node_id: row.exit_node_id,
      listen_port: row.listen_port,
      target_host: resolveTargetHost(row),
      target_port: row.target_port,
      is_enabled: row.is_enabled,
      sort_weight: row.sort_weight || 0,
    }))
    await adminApi.relays.bindBackends(selectedRelay.value.id, payload)
    ElMessage.success('后端绑定已保存')
    backendsDialogVisible.value = false
    await fetchRelays()
  } catch (err) {
    ElMessage.error(err.message || '保存后端绑定失败')
  } finally {
    backendsSaving.value = false
  }
}

function showDeployDialog() {
  deploySteps.value = []
  deployDialogVisible.value = true
}

async function handleDeploy() {
  const valid = await deployFormRef.value.validate().catch(() => false)
  if (!valid) return

  deploying.value = true
  deploySteps.value = []
  try {
    const res = await adminApi.relays.deploy({ ...deployForm })
    deploySteps.value = res.data.steps || []
    if (res.data.success) {
      ElMessage.success(`部署成功！中转 ID: ${res.data.relay_id}`)
      if (res.data.relay_token) {
        await ElMessageBox.alert(`中转 Token 已自动生成：${res.data.relay_token}`, '部署成功', {
          confirmButtonText: '知道了',
        })
      }
      deployDialogVisible.value = false
      await fetchRelays()
    } else {
      ElMessage.error('部署失败，请查看部署步骤详情')
    }
  } catch (err) {
    ElMessage.error(err.message || '部署失败')
  } finally {
    deploying.value = false
  }
}

async function fetchRelays() {
  loading.value = true
  try {
    const res = await adminApi.relays.list()
    relays.value = res.data.relays || []
  } catch (err) {
    ElMessage.error(err.message || '获取中转列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchRelays()
})
</script>

<style scoped>
.admin-relays {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.header > div {
  display: flex;
  gap: 8px;
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
.relation-cell {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  min-height: 28px;
}
.empty-text {
  color: #909399;
  font-size: 13px;
}
.backend-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.backend-count {
  color: #606266;
  font-size: 14px;
}
.target-host-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-height: 32px;
  justify-content: center;
}
.target-host-cell span {
  color: #303133;
  font-size: 13px;
  word-break: break-all;
}
.target-host-cell small {
  color: #909399;
  font-size: 12px;
}
.deploy-steps {
  margin-top: 16px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
  max-height: 300px;
  overflow-y: auto;
}
.deploy-step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  font-size: 14px;
}
.deploy-step .el-icon.success {
  color: #67c23a;
}
.deploy-step .el-icon.failed {
  color: #f56c6c;
}
.deploy-step .el-icon {
  color: #e6a23c;
}
.step-msg {
  margin-left: auto;
  color: #909399;
  font-size: 12px;
}
</style>
