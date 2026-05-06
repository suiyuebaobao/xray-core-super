<template>
  <div class="admin-nodes">
    <div class="header">
      <h2>出口节点管理</h2>
      <div>
        <el-button type="success" @click="showDeployDialog">一键部署</el-button>
        <el-button type="primary" @click="showAddDialog">新增节点</el-button>
      </div>
    </div>

    <div v-if="selectedNodes.length" class="batch-toolbar">
      <span>已选择 {{ selectedNodes.length }} 个出口节点</span>
      <div>
        <el-button size="small" @click="clearNodeSelection">取消选择</el-button>
        <el-button size="small" type="danger" :loading="batchDeleting" @click="handleBatchDelete">批量删除</el-button>
      </div>
    </div>

    <el-table
      ref="nodesTableRef"
      :data="nodes"
      border
      style="width: 100%"
      v-loading="loading"
      @selection-change="handleNodeSelectionChange"
    >
      <el-table-column type="selection" width="48" />
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="name" label="节点名称" />
      <el-table-column prop="host" label="地址" />
      <el-table-column prop="port" label="端口" width="70" />
      <el-table-column prop="transport" label="传输" width="120">
        <template #default="{ row }">
          <el-tag effect="plain" :type="row.transport === 'xhttp' ? 'warning' : 'info'">{{ transportLabel(row.transport) }}</el-tag>
        </template>
      </el-table-column>
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
      <el-table-column label="流量上报" min-width="220">
        <template #default="{ row }">
          <div class="traffic-sync-cell">
            <div>
              <el-tag size="small" :type="trafficSyncTag(row)">{{ trafficSyncLabel(row) }}</el-tag>
              <span class="traffic-sync-time">{{ row.last_traffic_success_at ? formatDate(row.last_traffic_success_at) : '未成功' }}</span>
            </div>
            <small v-if="row.last_traffic_error" class="traffic-sync-error">{{ row.last_traffic_error }}</small>
          </div>
        </template>
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
        <el-form-item label="传输模式">
          <el-select v-model="form.transports" multiple :multiple-limit="isEdit ? 1 : 2" style="width: 100%" @change="handleTransportSelectionChange(form)">
            <el-option label="TCP + Reality" value="tcp" />
            <el-option label="XHTTP + Reality" value="xhttp" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="hasTransport(form, 'tcp')" label="TCP 端口" prop="tcp_port">
          <el-input-number v-model="form.tcp_port" :min="1" :max="65535" />
        </el-form-item>
        <template v-if="hasTransport(form, 'xhttp')">
          <el-form-item label="XHTTP 端口" prop="xhttp_port">
            <el-input-number v-model="form.xhttp_port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="XHTTP Path">
            <el-input v-model="form.xhttp_path" placeholder="/raypilot" />
          </el-form-item>
          <el-form-item label="XHTTP Mode">
            <el-select v-model="form.xhttp_mode" style="width: 100%">
              <el-option label="auto" value="auto" />
              <el-option label="packet-up" value="packet-up" />
              <el-option label="stream-up" value="stream-up" />
              <el-option label="stream-one" value="stream-one" />
            </el-select>
          </el-form-item>
          <el-form-item label="XHTTP Host">
            <el-input v-model="form.xhttp_host" placeholder="可选" />
          </el-form-item>
        </template>
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
    <el-dialog v-model="deployDialogVisible" title="一键部署节点" width="760px">
      <el-form :model="deployForm" :rules="deployRules" ref="deployFormRef" label-width="120px">
        <el-alert :title="deployForm.multi_ip_enabled ? '多 IP 模式会先扫描出口 IP，勾选确认后才会创建多个逻辑节点' : '部署成功后会自动创建节点记录并刷新列表'" type="info" :closable="false" style="margin-bottom: 16px" />
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
          <el-input v-model="deployForm.node_name" placeholder="可选，默认为 raypilot-node-IP" />
        </el-form-item>
        <el-form-item label="中心服务地址" prop="center_url">
          <el-input v-model="deployForm.center_url" placeholder="例如：http://156.238.231.216" />
        </el-form-item>
        <el-form-item label="节点 Token">
          <el-input v-model="deployForm.node_token" placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item label="传输模式">
          <el-select v-model="deployForm.transports" multiple :multiple-limit="2" style="width: 100%" @change="handleTransportSelectionChange(deployForm)">
            <el-option label="TCP + Reality" value="tcp" />
            <el-option label="XHTTP + Reality" value="xhttp" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="hasTransport(deployForm, 'tcp')" label="TCP 端口">
          <el-input-number v-model="deployForm.tcp_port" :min="1" :max="65535" />
        </el-form-item>
        <template v-if="hasTransport(deployForm, 'xhttp')">
          <el-form-item label="XHTTP 端口">
            <el-input-number v-model="deployForm.xhttp_port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="XHTTP Path">
            <el-input v-model="deployForm.xhttp_path" placeholder="/raypilot" />
          </el-form-item>
          <el-form-item label="XHTTP Mode">
            <el-select v-model="deployForm.xhttp_mode" style="width: 100%">
              <el-option label="auto" value="auto" />
              <el-option label="packet-up" value="packet-up" />
              <el-option label="stream-up" value="stream-up" />
              <el-option label="stream-one" value="stream-one" />
            </el-select>
          </el-form-item>
          <el-form-item label="XHTTP Host">
            <el-input v-model="deployForm.xhttp_host" placeholder="可选" />
          </el-form-item>
        </template>
        <el-form-item label="多 IP 服务器">
          <el-switch
            v-model="deployForm.multi_ip_enabled"
            active-text="是"
            inactive-text="否"
            @change="handleMultiIpModeChange"
          />
        </el-form-item>
        <template v-if="deployForm.multi_ip_enabled">
          <el-form-item label="出口 IP 扫描">
            <el-button type="primary" plain :loading="scanningIps" @click="handleScanDeployIps">扫描出口 IP</el-button>
            <span class="scan-hint">扫描后手动勾选要创建为节点的公网出口 IP</span>
          </el-form-item>
          <el-table
            v-if="scannedIps.length"
            ref="scanIpsTableRef"
            :data="scannedIps"
            border
            size="small"
            class="scan-ip-table"
            @selection-change="handleScanIpSelectionChange"
          >
            <el-table-column type="selection" width="48" :selectable="isScannedIpSelectable" />
            <el-table-column prop="ip" label="IP" min-width="150" />
            <el-table-column prop="interface" label="网卡" width="110" />
            <el-table-column label="状态" width="120">
              <template #default="{ row }">
                <el-tag :type="row.is_usable ? 'success' : row.status === 'skipped' ? 'info' : 'danger'">
                  {{ row.is_usable ? '可用' : row.status === 'skipped' ? '跳过' : '不可用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="message" label="说明" min-width="220" />
          </el-table>
        </template>
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
const nodesTableRef = ref(null)
const selectedNodes = ref([])
const batchDeleting = ref(false)

const form = reactive({
  name: '',
  host: '',
  transports: ['tcp'],
  tcp_port: 443,
  xhttp_port: 443,
  xhttp_path: '/raypilot',
  xhttp_host: '',
  xhttp_mode: 'auto',
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
  tcp_port: [{ required: true, message: '请输入 TCP 端口', trigger: 'blur' }],
  xhttp_port: [{ required: true, message: '请输入 XHTTP 端口', trigger: 'blur' }],
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
const scannedIps = ref([])
const selectedDeployIps = ref([])
const scanningIps = ref(false)
const scanIpsTableRef = ref(null)

const deployForm = reactive({
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  node_name: '',
  center_url: window.location.origin,
  node_token: '',
  transports: ['tcp'],
  tcp_port: 443,
  xhttp_port: 443,
  xhttp_path: '/raypilot',
  xhttp_host: '',
  xhttp_mode: 'auto',
  multi_ip_enabled: false,
})

const deployRules = {
  ssh_host: [{ required: true, message: '请输入服务器 IP', trigger: 'blur' }],
  ssh_user: [{ required: true, message: '请输入 SSH 用户', trigger: 'blur' }],
  ssh_password: [{ required: true, message: '请输入 SSH 密码', trigger: 'blur' }],
  center_url: [{ required: true, message: '请输入中心服务地址', trigger: 'blur' }],
}

function showDeployDialog() {
  deploySteps.value = []
  scannedIps.value = []
  selectedDeployIps.value = []
  handleTransportSelectionChange(deployForm)
  deployDialogVisible.value = true
}

function handleMultiIpModeChange() {
  scannedIps.value = []
  selectedDeployIps.value = []
  scanIpsTableRef.value?.clearSelection()
}

function isScannedIpSelectable(row) {
  return !!row.is_usable
}

function handleScanIpSelectionChange(selection) {
  selectedDeployIps.value = selection
}

async function handleScanDeployIps() {
  const valid = await deployFormRef.value.validate().catch(() => false)
  if (!valid) return

  scanningIps.value = true
  scannedIps.value = []
  selectedDeployIps.value = []
  try {
    const res = await adminApi.nodes.scanDeployIps({
      ssh_host: deployForm.ssh_host,
      ssh_port: deployForm.ssh_port,
      ssh_user: deployForm.ssh_user,
      ssh_password: deployForm.ssh_password,
    })
    scannedIps.value = res.data.ips || []
    deploySteps.value = res.data.steps || []
    const usableCount = scannedIps.value.filter((item) => item.is_usable).length
    if (usableCount) {
      ElMessage.success(`扫描完成，发现 ${usableCount} 个可用出口 IP`)
    } else {
      ElMessage.warning('扫描完成，未发现可用公网出口 IP')
    }
  } catch (err) {
    ElMessage.error(err.message || '出口 IP 扫描失败')
  } finally {
    scanningIps.value = false
  }
}

async function handleDeploy() {
  const valid = await deployFormRef.value.validate().catch(() => false)
  if (!valid) return

  deploying.value = true
  deploySteps.value = []

  try {
    if (deployForm.multi_ip_enabled && selectedDeployIps.value.length === 0) {
      ElMessage.warning('请先扫描并勾选要部署的出口 IP')
      return
    }
    const transports = normalizedTransports(deployForm)
    if (transports.includes('tcp') && transports.includes('xhttp') && deployForm.tcp_port === deployForm.xhttp_port) {
      ElMessage.warning('TCP 和 XHTTP 端口不能相同')
      return
    }
    const payload = {
      ssh_host: deployForm.ssh_host,
      ssh_port: deployForm.ssh_port,
      ssh_user: deployForm.ssh_user,
      ssh_password: deployForm.ssh_password,
      node_name: deployForm.node_name,
      center_url: deployForm.center_url,
      node_token: deployForm.node_token,
      transports,
      transport: transports[0],
      tcp_port: deployForm.tcp_port,
      xhttp_port: deployForm.xhttp_port,
      xhttp_path: deployForm.xhttp_path,
      xhttp_host: deployForm.xhttp_host,
      xhttp_mode: deployForm.xhttp_mode,
      multi_ip_enabled: deployForm.multi_ip_enabled,
      selected_ips: selectedDeployIps.value.map((item) => item.ip),
    }

    const res = await adminApi.nodes.deploy(payload)
    deploySteps.value = res.data.steps || []

    if (res.data.success) {
      const ids = res.data.node_ids?.length ? res.data.node_ids.join(', ') : res.data.node_id
      ElMessage.success(`部署成功！节点 ID: ${ids}`)
      const generatedToken = res.data.node_host_token || res.data.node_token
      if (generatedToken) {
        await ElMessageBox.alert(`节点 Token 已自动生成：${generatedToken}`, '部署成功', {
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

function transportLabel(transport) {
  return transport === 'xhttp' ? 'XHTTP' : 'TCP'
}

function normalizedTransports(target) {
  const values = selectedTransports(target)
  return values.length ? values : ['tcp']
}

function selectedTransports(target) {
  const values = Array.isArray(target.transports) ? target.transports : []
  const set = new Set(values.filter((item) => item === 'tcp' || item === 'xhttp'))
  return ['tcp', 'xhttp'].filter((item) => set.has(item))
}

function hasTransport(target, transport) {
  return normalizedTransports(target).includes(transport)
}

function handleTransportSelectionChange(target) {
  const transports = selectedTransports(target)
  target.transports = transports
  if (!transports.length) return
  if (!target.tcp_port) target.tcp_port = 443
  if (!target.xhttp_port) target.xhttp_port = 443
  if (transports.includes('tcp') && transports.includes('xhttp') && target.tcp_port === target.xhttp_port) {
    target.xhttp_port = target.tcp_port === 443 ? 8443 : target.tcp_port + 1
  }
  if (transports.includes('xhttp')) {
    if (!target.xhttp_path) target.xhttp_path = '/raypilot'
    if (!target.xhttp_mode) target.xhttp_mode = 'auto'
  }
}

function trafficSyncTag(row) {
  if (row.last_traffic_error) return 'danger'
  if (row.last_traffic_success_at) return 'success'
  return 'info'
}

function trafficSyncLabel(row) {
  if (row.last_traffic_error) return `异常 ${row.traffic_error_count || 1} 次`
  if (row.last_traffic_success_at) return '正常'
  return '未上报'
}

function resetForm() {
  form.name = ''
  form.host = ''
  form.transports = ['tcp']
  form.tcp_port = 443
  form.xhttp_port = 443
  form.xhttp_path = '/raypilot'
  form.xhttp_host = ''
  form.xhttp_mode = 'auto'
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
  form.transports = [row.transport || 'tcp']
  form.tcp_port = (row.transport || 'tcp') === 'tcp' ? row.port : 443
  form.xhttp_port = row.transport === 'xhttp' ? row.port : 443
  form.xhttp_path = row.xhttp_path || '/raypilot'
  form.xhttp_host = row.xhttp_host || ''
  form.xhttp_mode = row.xhttp_mode || 'auto'
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
    const transports = normalizedTransports(form)
    if (isEdit.value && transports.length > 1) {
      ElMessage.warning('编辑单条节点只能选择一种传输模式')
      return
    }
    if (transports.includes('tcp') && transports.includes('xhttp') && form.tcp_port === form.xhttp_port) {
      ElMessage.warning('TCP 和 XHTTP 端口不能相同')
      return
    }
    const primaryTransport = transports[0]
    const payload = {
      name: form.name,
      host: form.host,
      port: primaryTransport === 'xhttp' ? form.xhttp_port : form.tcp_port,
      transports,
      transport: primaryTransport,
      tcp_port: form.tcp_port,
      xhttp_port: form.xhttp_port,
      xhttp_path: form.xhttp_path,
      xhttp_host: form.xhttp_host,
      xhttp_mode: form.xhttp_mode,
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
    removeNodesFromList([row.id])
    ElMessage.success('删除成功')
    fetchNodes().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

function handleNodeSelectionChange(selection) {
  selectedNodes.value = selection
}

function clearNodeSelection() {
  nodesTableRef.value?.clearSelection()
}

async function handleBatchDelete() {
  const rows = [...selectedNodes.value]
  if (!rows.length) {
    ElMessage.warning('请选择要删除的出口节点')
    return
  }

  try {
    await ElMessageBox.confirm(`确定批量删除选中的 ${rows.length} 个出口节点吗？`, '批量删除', { type: 'warning' })
  } catch {
    return
  }

  batchDeleting.value = true
  const deletedIds = []
  const failed = []
  try {
    for (const row of rows) {
      try {
        await adminApi.nodes.delete(row.id)
        deletedIds.push(row.id)
      } catch (err) {
        failed.push(`${row.name || row.id}：${err.message || '删除失败'}`)
      }
    }

    if (deletedIds.length) {
      removeNodesFromList(deletedIds)
    }
    if (failed.length) {
      ElMessage.warning(`成功删除 ${deletedIds.length} 个，失败 ${failed.length} 个：${failed.join('；')}`)
    } else {
      ElMessage.success('批量删除成功')
    }
    fetchNodes().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } finally {
    batchDeleting.value = false
  }
}

function removeNodesFromList(ids) {
  const idSet = new Set(ids.map((id) => String(id)))
  nodes.value = nodes.value.filter((node) => !idSet.has(String(node.id)))
  selectedNodes.value = selectedNodes.value.filter((node) => !idSet.has(String(node.id)))
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
.scan-hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}
.scan-ip-table {
  margin: 0 0 16px 120px;
  width: calc(100% - 120px);
}
.traffic-sync-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.traffic-sync-cell > div {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.traffic-sync-time {
  color: #606266;
  font-size: 12px;
}
.traffic-sync-error {
  color: #f56c6c;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
