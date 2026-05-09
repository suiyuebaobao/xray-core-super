<template>
  <div class="admin-logs">
    <div class="header">
      <h2>日志中心</h2>
      <div class="actions">
        <el-button @click="fetchCurrentTab" :loading="loading">刷新</el-button>
      </div>
    </div>

    <el-tabs v-model="activeTab" @tab-change="fetchCurrentTab">
      <el-tab-pane label="运行日志" name="runtime">
        <div class="toolbar">
          <el-select v-model="runtimeFilters.source" style="width: 140px">
            <el-option label="API" value="api" />
            <el-option label="Worker" value="worker" />
          </el-select>
          <el-select v-model="runtimeFilters.lines" style="width: 140px">
            <el-option label="100 行" :value="100" />
            <el-option label="500 行" :value="500" />
            <el-option label="1000 行" :value="1000" />
          </el-select>
          <el-date-picker
            v-model="runtimeFilters.date"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="日志日期"
            clearable
            style="width: 160px"
          />
          <el-select v-model="runtimeFilters.hour" clearable placeholder="小时" style="width: 110px">
            <el-option v-for="hour in runtimeHourOptions" :key="hour" :label="`${hour}:00`" :value="hour" />
          </el-select>
          <el-input v-model="runtimeFilters.keyword" placeholder="关键字过滤" clearable style="width: 260px" />
          <el-button type="primary" @click="fetchRuntimeLogs" :loading="loading">查询</el-button>
        </div>

        <div class="runtime-log-panel">
          <div v-for="line in runtimeLogs" :key="`${line.line_number}-${line.raw}`" class="runtime-line" :data-level="line.level">
            <span class="runtime-level">{{ line.level.toUpperCase() }}</span>
            <span v-if="line.file" class="runtime-file">{{ line.file }}</span>
            <span class="runtime-message">{{ line.message }}</span>
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane label="部署日志" name="deployments">
        <div class="toolbar">
          <el-select v-model="deploymentFilters.deploy_type" clearable placeholder="部署类型" style="width: 180px">
            <el-option label="出口部署" value="exit_deploy" />
            <el-option label="中转部署" value="relay_deploy" />
          </el-select>
          <el-select v-model="deploymentFilters.result" clearable placeholder="结果" style="width: 140px">
            <el-option label="成功" value="success" />
            <el-option label="失败" value="failed" />
          </el-select>
          <el-input v-model="deploymentFilters.keyword" placeholder="目标服务器 / 操作人 / 错误关键字" clearable style="width: 320px" />
          <el-button type="primary" @click="fetchDeploymentLogs" :loading="loading">查询</el-button>
        </div>

        <el-table :data="deploymentLogs" border v-loading="loading">
          <el-table-column prop="created_at" label="时间" width="180">
            <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="deploy_type" label="类型" width="120" />
          <el-table-column prop="target_server_ip" label="目标服务器" width="160" />
          <el-table-column prop="operator_username" label="操作人" width="120" />
          <el-table-column prop="operator_ip" label="操作 IP" width="150" />
          <el-table-column prop="result" label="结果" width="100">
            <template #default="{ row }">
              <el-tag :type="row.result === 'success' ? 'success' : 'danger'">{{ row.result === 'success' ? '成功' : '失败' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="步骤" min-width="380">
            <template #default="{ row }">
              <div class="step-list">
                <div v-for="step in row.steps || []" :key="step.id || `${step.step_order}-${step.name}`" class="step-item">
                  <span class="step-name">{{ step.name }}</span>
                  <span class="step-status" :data-status="step.status">{{ step.status }}</span>
                  <span class="step-message">{{ step.message }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="操作日志" name="operations">
        <div class="toolbar">
          <el-select v-model="operationFilters.actor_type" clearable placeholder="操作者类型" style="width: 160px">
            <el-option label="用户" value="user" />
            <el-option label="管理员" value="admin" />
          </el-select>
          <el-input v-model="operationFilters.action" placeholder="动作类型，如 login / redeem" clearable style="width: 220px" />
          <el-input v-model="operationFilters.keyword" placeholder="用户名 / 摘要 / IP" clearable style="width: 280px" />
          <el-button type="primary" @click="fetchOperationLogs" :loading="loading">查询</el-button>
        </div>

        <el-table :data="operationLogs" border v-loading="loading">
          <el-table-column prop="created_at" label="时间" width="180">
            <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="actor_type" label="类型" width="100" />
          <el-table-column prop="actor_username" label="用户" width="140" />
          <el-table-column prop="action" label="动作" width="180" />
          <el-table-column prop="client_ip" label="客户端 IP" width="150" />
          <el-table-column prop="summary" label="摘要" min-width="320" />
          <el-table-column prop="result" label="结果" width="100">
            <template #default="{ row }">
              <el-tag :type="row.result === 'success' ? 'success' : row.result === 'failed' ? 'danger' : 'info'">{{ row.result }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { adminApi } from '@/api'

const activeTab = ref('runtime')
const loading = ref(false)

const runtimeFilters = reactive({
  source: 'api',
  lines: 200,
  date: '',
  hour: '',
  keyword: '',
})

const deploymentFilters = reactive({
  deploy_type: '',
  result: '',
  keyword: '',
})

const operationFilters = reactive({
  actor_type: '',
  action: '',
  keyword: '',
})

const runtimeLogs = ref([])
const deploymentLogs = ref([])
const operationLogs = ref([])
const runtimeHourOptions = Array.from({ length: 24 }, (_, index) => String(index).padStart(2, '0'))

function formatDate(value) {
  if (!value) return '-'
  return new Date(value).toLocaleString('zh-CN')
}

async function fetchRuntimeLogs() {
  loading.value = true
  try {
    const res = await adminApi.logs.runtime(runtimeFilters)
    runtimeLogs.value = res.data.lines || []
  } catch (err) {
    ElMessage.error(err.message || '获取运行日志失败')
  } finally {
    loading.value = false
  }
}

async function fetchDeploymentLogs() {
  loading.value = true
  try {
    const res = await adminApi.logs.deployments(deploymentFilters)
    deploymentLogs.value = res.data.logs || []
  } catch (err) {
    ElMessage.error(err.message || '获取部署日志失败')
  } finally {
    loading.value = false
  }
}

async function fetchOperationLogs() {
  loading.value = true
  try {
    const res = await adminApi.logs.operations(operationFilters)
    operationLogs.value = res.data.logs || []
  } catch (err) {
    ElMessage.error(err.message || '获取操作日志失败')
  } finally {
    loading.value = false
  }
}

function fetchCurrentTab() {
  if (activeTab.value === 'runtime') return fetchRuntimeLogs()
  if (activeTab.value === 'deployments') return fetchDeploymentLogs()
  return fetchOperationLogs()
}

onMounted(() => {
  fetchCurrentTab()
})
</script>

<style scoped>
.admin-logs {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.runtime-log-panel {
  background: #111827;
  color: #e5e7eb;
  border-radius: 6px;
  padding: 12px;
  min-height: 520px;
  max-height: 70vh;
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
  line-height: 1.6;
}

.runtime-line {
  display: flex;
  gap: 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

.runtime-line[data-level='error'],
.runtime-line[data-level='fatal'],
.runtime-line[data-level='panic'] {
  color: #fca5a5;
}

.runtime-line[data-level='warn'] {
  color: #fde68a;
}

.runtime-level {
  width: 48px;
  flex: 0 0 auto;
  color: #93c5fd;
}

.runtime-file {
  width: 170px;
  flex: 0 0 auto;
  color: #9ca3af;
}

.runtime-message {
  flex: 1;
}

.step-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.step-item {
  display: grid;
  grid-template-columns: 110px 80px 1fr;
  gap: 8px;
  align-items: start;
  font-size: 12px;
}

.step-name {
  color: #111827;
}

.step-status {
  color: #4b5563;
}

.step-status[data-status='success'] {
  color: #16a34a;
}

.step-status[data-status='failed'] {
  color: #dc2626;
}

.step-message {
  color: #4b5563;
}
</style>
