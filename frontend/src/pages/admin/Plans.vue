<template>
  <div class="admin-plans">
    <div class="header">
      <h2>套餐管理</h2>
      <el-button type="primary" @click="showAddDialog">新增套餐</el-button>
    </div>

    <el-table :data="plans" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="name" label="套餐名称" min-width="150" />
      <el-table-column prop="price" label="价格" width="100">
        <template #default="{ row }">{{ row.price }} {{ row.currency }}</template>
      </el-table-column>
      <el-table-column prop="traffic_limit" label="流量" width="120">
        <template #default="{ row }">{{ formatBytes(row.traffic_limit) }}</template>
      </el-table-column>
      <el-table-column prop="duration_days" label="时长（天）" width="100" />
      <el-table-column prop="is_active" label="状态" width="80">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'info'">{{ row.is_active ? '上架' : '下架' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="节点分组" min-width="240">
        <template #default="{ row }">
          <div class="relation-cell">
            <template v-if="getPlanNodeGroups(row).length">
              <el-tag
                v-for="group in getPlanNodeGroups(row)"
                :key="group.id"
                size="small"
                effect="plain"
              >
                {{ group.name }}
              </el-tag>
            </template>
            <span v-else class="empty-text">未绑定</span>
            <el-button size="small" text type="primary" @click="showNodeGroupDialog(row)">管理</el-button>
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

    <div class="pagination" style="margin-top: 16px; text-align: right">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchPlans"
      />
    </div>

    <!-- 新增/编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑套餐' : '新增套餐'" width="600px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="100px">
        <el-form-item label="套餐名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="价格" prop="price">
          <el-input-number v-model="form.price" :min="0" :precision="2" />
        </el-form-item>
        <el-form-item label="流量（GB）" prop="trafficLimitGB">
          <el-input-number v-model="form.trafficLimitGB" :min="0" />
        </el-form-item>
        <el-form-item label="时长（天）" prop="duration_days">
          <el-input-number v-model="form.duration_days" :min="1" />
        </el-form-item>
        <el-form-item label="节点分组">
          <el-select v-model="form.nodeGroupIds" multiple placeholder="选择节点分组" style="width: 100%">
            <el-option v-for="g in nodeGroups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.is_active" active-text="上架" inactive-text="下架" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 节点分组管理对话框 -->
    <el-dialog v-model="ngDialogVisible" title="管理节点分组" width="500px">
      <el-select v-model="selectedNodeGroupIds" multiple placeholder="选择节点分组" style="width: 100%">
        <el-option v-for="g in nodeGroups" :key="g.id" :label="g.name" :value="g.id" />
      </el-select>
      <template #footer>
        <el-button @click="ngDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveNodeGroups" :loading="ngSaving">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 管理后台 - 套餐管理页。
// 新增功能：套餐关联节点分组管理。
import { computed, ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'

const plans = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const saving = ref(false)
const formRef = ref(null)

// 节点分组相关
const nodeGroups = ref([])
const ngDialogVisible = ref(false)
const ngSaving = ref(false)
const managingPlanId = ref(null)
const selectedNodeGroupIds = ref([])
const nodeGroupNameMap = computed(() => {
  const map = new Map()
  nodeGroups.value.forEach((group) => {
    map.set(String(group.id), group.name)
  })
  return map
})

const form = reactive({
  name: '',
  price: 0,
  trafficLimitGB: 0,
  duration_days: 30,
  nodeGroupIds: [],
  is_active: true,
})

const rules = {
  name: [{ required: true, message: '请输入套餐名称', trigger: 'blur' }],
  price: [{ required: true, message: '请输入价格', trigger: 'blur' }],
  duration_days: [{ required: true, message: '请输入时长', trigger: 'blur' }],
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

function getPlanNodeGroups(row) {
  if (Array.isArray(row.node_groups) && row.node_groups.length) {
    return row.node_groups
  }
  const ids = row.node_group_ids || []
  return ids.map((id) => ({
    id,
    name: nodeGroupNameMap.value.get(String(id)) || `分组 ${id}`,
  }))
}

function resetForm() {
  form.name = ''
  form.price = 0
  form.trafficLimitGB = 0
  form.duration_days = 30
  form.nodeGroupIds = []
  form.is_active = true
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
  form.price = row.price
  form.trafficLimitGB = Math.round(row.traffic_limit / 1024 / 1024 / 1024)
  form.duration_days = row.duration_days
  form.nodeGroupIds = row.node_group_ids || []
  form.is_active = row.is_active
  dialogVisible.value = true
}

async function handleSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const payload = {
      name: form.name,
      price: form.price,
      traffic_limit: form.trafficLimitGB * 1024 * 1024 * 1024,
      duration_days: form.duration_days,
      is_active: form.is_active,
    }

    let planId = editingId.value
    if (isEdit.value) {
      await adminApi.plans.update(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      const res = await adminApi.plans.create(payload)
      ElMessage.success('创建成功')
      planId = res.data?.id
    }

    // 保存节点分组绑定
    if (planId) {
      await adminApi.plans.bindNodeGroups(planId, form.nodeGroupIds)
    }

    dialogVisible.value = false
    page.value = 1
    await fetchPlans()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除套餐"${row.name}"吗？`, '确认删除', { type: 'warning' })
    await adminApi.plans.delete(row.id)
    ElMessage.success('删除成功')
    await fetchPlans()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

async function fetchPlans() {
  loading.value = true
  try {
    const res = await adminApi.plans.list()
    plans.value = res.data.plans || []
    total.value = plans.value.length
  } catch (err) {
    ElMessage.error('获取套餐列表失败')
  } finally {
    loading.value = false
  }
}

async function fetchNodeGroups() {
  try {
    const res = await adminApi.nodeGroups.list()
    nodeGroups.value = res.data?.groups || []
  } catch (err) {
    // 节点分组列表获取失败不影响主流程
  }
}

function showNodeGroupDialog(row) {
  managingPlanId.value = row.id
  selectedNodeGroupIds.value = row.node_group_ids || []
  ngDialogVisible.value = true
}

async function handleSaveNodeGroups() {
  ngSaving.value = true
  try {
    await adminApi.plans.bindNodeGroups(managingPlanId.value, selectedNodeGroupIds.value)
    ElMessage.success('节点分组绑定成功')
    ngDialogVisible.value = false
    await fetchPlans()
  } catch (err) {
    ElMessage.error(err.message || '绑定失败')
  } finally {
    ngSaving.value = false
  }
}

onMounted(async () => {
  await fetchNodeGroups()
  fetchPlans()
})
</script>

<style scoped>
.admin-plans {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
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
</style>
