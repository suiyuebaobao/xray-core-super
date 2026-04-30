<template>
  <div class="admin-nodes">
    <div class="header">
      <h2>出口节点管理</h2>
      <div>
        <el-button type="success" @click="showDeployDialog">一键部署</el-button>
        <el-button type="primary" @click="showAddDialog">新增节点</el-button>
      </div>
    </div>

    <el-table :data="nodes" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="name" label="节点名称" />
      <el-table-column prop="host" label="地址" />
      <el-table-column prop="port" label="端口" width="70" />
      <el-table-column prop="line_mode" label="线路输出" width="130">
        <template #default="{ row }">
          <el-tag effect="plain">{{ lineModeLabel(row.line_mode) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="is_enabled" label="状态" width="80">
        <template #default="{ row }">
          <el-tag :type="row.is_enabled ? 'success' : 'info'">{{ row.is_enabled ? '启用' : '禁用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_heartbeat_at" label="最后心跳" width="180">
        <template #default="{ row }">{{ row.last_heartbeat_at ? formatDate(row.last_heartbeat_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button size="small" @click="showEditDialog(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑节点' : '新增节点'" width="600px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="节点名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="地址" prop="host">
          <el-input v-model="form.host" placeholder="例如：hk.example.com" />
        </el-form-item>
        <el-form-item label="端口" prop="port">
          <el-input-number v-model="form.port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="Server Name">
          <el-input v-model="form.server_name" placeholder="Reality SNI" />
        </el-form-item>
        <el-form-item label="Public Key">
          <el-input v-model="form.public_key" placeholder="Reality 公钥" />
        </el-form-item>
        <el-form-item label="Short ID">
          <el-input v-model="form.short_id" placeholder="Reality Short ID" />
        </el-form-item>
        <el-form-item label="线路输出">
          <el-select v-model="form.line_mode" style="width: 100%">
            <el-option label="直连 + 中转" value="direct_and_relay" />
            <el-option label="仅直连" value="direct_only" />
            <el-option label="仅中转" value="relay_only" />
          </el-select>
        </el-form-item>
        <el-form-item label="Agent 地址" prop="agent_base_url">
          <el-input v-model="form.agent_base_url" placeholder="http://node-ip:port" />
        </el-form-item>
        <el-form-item label="Agent Token" prop="agent_token">
          <el-input v-model="form.agent_token" type="password" show-password placeholder="节点鉴权 Token" />
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

    <!-- 一键部署弹窗 -->
    <el-dialog v-model="deployDialogVisible" title="一键部署节点" width="500px">
      <el-form :model="deployForm" :rules="deployRules" ref="deployFormRef" label-width="120px">
        <el-alert title="部署成功后会自动创建节点记录并刷新列表" type="info" :closable="false" style="margin-bottom: 16px" />
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
        <el-form-item label="节点名称" prop="node_name">
          <el-input v-model="deployForm.node_name" placeholder="可选，默认为 suiyue-node-IP" />
        </el-form-item>
        <el-form-item label="中心服务地址" prop="center_url">
          <el-input v-model="deployForm.center_url" placeholder="例如：http://156.238.231.216" />
        </el-form-item>
        <el-form-item label="节点 Token">
          <el-input v-model="deployForm.node_token" placeholder="留空自动生成" />
        </el-form-item>
      </el-form>

      <!-- 部署进度 -->
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
import { ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleCheck, CircleClose, Loading } from '@element-plus/icons-vue'

const nodes = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const saving = ref(false)
const formRef = ref(null)

const form = reactive({
  name: '',
  host: '',
  port: 443,
  server_name: '',
  public_key: '',
  short_id: '',
  line_mode: 'direct_and_relay',
  agent_base_url: '',
  agent_token: '',
  is_enabled: true,
})

const rules = {
  name: [{ required: true, message: '请输入节点名称', trigger: 'blur' }],
  host: [{ required: true, message: '请输入节点地址', trigger: 'blur' }],
  port: [{ required: true, message: '请输入端口', trigger: 'blur' }],
  agent_base_url: [{ required: true, message: '请输入 Agent 地址', trigger: 'blur' }],
  agent_token: [{
    required: true,
    message: '请输入 Agent Token',
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

// 一键部署
const deployDialogVisible = ref(false)
const deploying = ref(false)
const deployFormRef = ref(null)
const deploySteps = ref([])

const deployForm = reactive({
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  node_name: '',
  center_url: window.location.origin,
  node_token: '',
})

const deployRules = {
  ssh_host: [{ required: true, message: '请输入服务器 IP', trigger: 'blur' }],
  ssh_user: [{ required: true, message: '请输入 SSH 用户', trigger: 'blur' }],
  ssh_password: [{ required: true, message: '请输入 SSH 密码', trigger: 'blur' }],
  center_url: [{ required: true, message: '请输入中心服务地址', trigger: 'blur' }],
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
    const payload = {
      ssh_host: deployForm.ssh_host,
      ssh_port: deployForm.ssh_port,
      ssh_user: deployForm.ssh_user,
      ssh_password: deployForm.ssh_password,
      node_name: deployForm.node_name,
      center_url: deployForm.center_url,
      node_token: deployForm.node_token,
    }

    const res = await adminApi.nodes.deploy(payload)
    deploySteps.value = res.data.steps || []

    if (res.data.success) {
      ElMessage.success(`部署成功！节点 ID: ${res.data.node_id}`)
      if (res.data.node_token) {
        await ElMessageBox.alert(`节点 Token 已自动生成：${res.data.node_token}`, '部署成功', {
          confirmButtonText: '知道了',
        })
      }
      deployDialogVisible.value = false
      await fetchNodes()
    } else {
      ElMessage.error('部署失败，请查看部署步骤详情')
    }
  } catch (err) {
    ElMessage.error(err.message || '部署失败')
  } finally {
    deploying.value = false
  }
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

function lineModeLabel(mode) {
  const labels = {
    direct_only: '仅直连',
    relay_only: '仅中转',
    direct_and_relay: '直连 + 中转',
  }
  return labels[mode] || '直连 + 中转'
}

function resetForm() {
  form.name = ''
  form.host = ''
  form.port = 443
  form.server_name = ''
  form.public_key = ''
  form.short_id = ''
  form.line_mode = 'direct_and_relay'
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
  form.port = row.port
  form.server_name = row.server_name || ''
  form.public_key = row.public_key || ''
  form.short_id = row.short_id || ''
  form.line_mode = row.line_mode || 'direct_and_relay'
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
    const payload = {
      name: form.name,
      host: form.host,
      port: form.port,
      server_name: form.server_name,
      public_key: form.public_key,
      short_id: form.short_id,
      line_mode: form.line_mode,
      agent_base_url: form.agent_base_url,
      agent_token: form.agent_token,
      is_enabled: form.is_enabled,
    }

    if (isEdit.value) {
      await adminApi.nodes.update(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      await adminApi.nodes.create(payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await fetchNodes()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除节点"${row.name}"吗？`, '确认删除', { type: 'warning' })
    await adminApi.nodes.delete(row.id)
    ElMessage.success('删除成功')
    await fetchNodes()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

async function fetchNodes() {
  loading.value = true
  try {
    const res = await adminApi.nodes.list()
    nodes.value = res.data.nodes || []
  } catch (err) {
    ElMessage.error('获取节点列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchNodes()
})
</script>

<style scoped>
.admin-nodes {
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
