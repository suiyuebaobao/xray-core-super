<template>
  <div class="admin-node-groups">
    <div class="header">
      <h2>节点分组管理</h2>
      <el-button type="primary" @click="showAddDialog">新增分组</el-button>
    </div>

    <el-table :data="groups" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="name" label="分组名称" min-width="140" />
      <el-table-column prop="description" label="描述" min-width="180">
        <template #default="{ row }">{{ row.description || '-' }}</template>
      </el-table-column>
      <el-table-column label="绑定节点" min-width="320">
        <template #default="{ row }">
          <div class="relation-cell">
            <template v-if="getGroupNodes(row.id).length">
              <el-tag
                v-for="node in getGroupNodes(row.id)"
                :key="node.id"
                size="small"
                :type="node.is_enabled ? 'success' : 'info'"
                effect="plain"
              >
                {{ node.name || formatNodeAddress(node) }}
              </el-tag>
            </template>
            <span v-else class="empty-text">未绑定</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="节点数" width="80">
        <template #default="{ row }">{{ getGroupNodes(row.id).length }}</template>
      </el-table-column>
      <el-table-column label="操作" width="260">
        <template #default="{ row }">
          <el-button size="small" type="primary" text @click="showManageNodesDialog(row)">管理节点</el-button>
          <el-button size="small" @click="showEditDialog(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑分组' : '新增分组'" width="500px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="100px">
        <el-form-item label="分组名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="nodesDialogVisible" :title="nodesDialogTitle" width="760px">
      <div class="nodes-toolbar">
        <span class="nodes-count">已选择 {{ selectedNodeIds.length }} 个节点</span>
      </div>
      <el-table
        ref="nodesTableRef"
        :data="allNodes"
        border
        row-key="id"
        max-height="420"
        empty-text="暂无可绑定节点"
        v-loading="nodesLoading"
        @selection-change="handleNodeSelectionChange"
      >
        <el-table-column type="selection" width="48" />
        <el-table-column prop="name" label="节点名称" min-width="160">
          <template #default="{ row }">
            <span>{{ row.name || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="地址" min-width="220">
          <template #default="{ row }">
            <span>{{ formatNodeAddress(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.is_enabled ? 'success' : 'info'">
              {{ row.is_enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="nodesDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveNodes" :loading="nodesSaving" :disabled="nodesLoading">保存绑定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 管理后台 - 节点分组管理页。
import { computed, nextTick, ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'

const groups = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const saving = ref(false)
const formRef = ref(null)
const nodesDialogVisible = ref(false)
const nodesLoading = ref(false)
const nodesSaving = ref(false)
const nodesTableRef = ref(null)
const managingGroup = ref(null)
const allNodes = ref([])
const selectedNodeIds = ref([])
const groupNodesMap = ref({})

const form = reactive({
  name: '',
  description: '',
})

const nodesDialogTitle = computed(() => {
  return managingGroup.value ? `管理节点 - ${managingGroup.value.name}` : '管理节点'
})

const rules = {
  name: [{ required: true, message: '请输入分组名称', trigger: 'blur' }],
}

function resetForm() {
  form.name = ''
  form.description = ''
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
  form.description = row.description || ''
  dialogVisible.value = true
}

async function handleSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const payload = { name: form.name, description: form.description }
    if (isEdit.value) {
      await adminApi.nodeGroups.update(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      await adminApi.nodeGroups.create(payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await fetchGroups()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除分组"${row.name}"吗？`, '确认删除', { type: 'warning' })
    await adminApi.nodeGroups.delete(row.id)
    ElMessage.success('删除成功')
    await fetchGroups()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

function normalizeNodes(data) {
  if (Array.isArray(data?.nodes)) return data.nodes
  if (Array.isArray(data)) return data
  return []
}

function normalizeNodeIds(data) {
  if (Array.isArray(data?.node_ids)) return data.node_ids
  return normalizeNodes(data).map((node) => node.id).filter((id) => id !== undefined && id !== null)
}

function formatNodeAddress(row) {
  if (!row.host) return '-'
  return row.port ? `${row.host}:${row.port}` : row.host
}

function getGroupNodes(groupId) {
  return groupNodesMap.value[String(groupId)] || []
}

async function showManageNodesDialog(row) {
  managingGroup.value = row
  allNodes.value = []
  selectedNodeIds.value = []
  nodesDialogVisible.value = true
  await fetchManageNodes(row.id)
}

async function fetchManageNodes(groupId) {
  nodesLoading.value = true
  try {
    const [nodesRes, groupNodesRes] = await Promise.all([
      adminApi.nodes.list(),
      adminApi.nodeGroups.nodes(groupId),
    ])
    allNodes.value = normalizeNodes(nodesRes.data)
    selectedNodeIds.value = normalizeNodeIds(groupNodesRes.data)
    await syncNodeSelection()
  } catch (err) {
    ElMessage.error(err.message || '获取节点绑定失败')
  } finally {
    nodesLoading.value = false
  }
}

async function syncNodeSelection() {
  await nextTick()
  const table = nodesTableRef.value
  if (!table) return

  const selectedSet = new Set(selectedNodeIds.value.map((id) => String(id)))
  table.clearSelection()
  allNodes.value.forEach((node) => {
    if (selectedSet.has(String(node.id))) {
      table.toggleRowSelection(node, true)
    }
  })
}

function handleNodeSelectionChange(selection) {
  selectedNodeIds.value = selection.map((node) => node.id).filter((id) => id !== undefined && id !== null)
}

async function handleSaveNodes() {
  if (!managingGroup.value) return

  nodesSaving.value = true
  try {
    await adminApi.nodeGroups.bindNodes(managingGroup.value.id, selectedNodeIds.value)
    const selectedSet = new Set(selectedNodeIds.value.map((id) => String(id)))
    groupNodesMap.value = {
      ...groupNodesMap.value,
      [String(managingGroup.value.id)]: allNodes.value.filter((node) => selectedSet.has(String(node.id))),
    }
    ElMessage.success('节点绑定已保存')
    nodesDialogVisible.value = false
  } catch (err) {
    ElMessage.error(err.message || '保存节点绑定失败')
  } finally {
    nodesSaving.value = false
  }
}

async function fetchGroups() {
  loading.value = true
  try {
    const res = await adminApi.nodeGroups.list()
    groups.value = res.data.groups || []
    if (groups.value.every((group) => Array.isArray(group.nodes))) {
      groupNodesMap.value = Object.fromEntries(
        groups.value.map((group) => [String(group.id), normalizeNodes(group)])
      )
    } else {
      await fetchGroupNodes(groups.value)
    }
  } catch (err) {
    ElMessage.error('获取分组列表失败')
  } finally {
    loading.value = false
  }
}

async function fetchGroupNodes(groupList) {
  const entries = await Promise.all(groupList.map(async (group) => {
    try {
      const res = await adminApi.nodeGroups.nodes(group.id)
      return [String(group.id), normalizeNodes(res.data)]
    } catch (err) {
      return [String(group.id), []]
    }
  }))
  groupNodesMap.value = Object.fromEntries(entries)
}

onMounted(() => {
  fetchGroups()
})
</script>

<style scoped>
.admin-node-groups {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.nodes-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}
.nodes-count {
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
</style>
