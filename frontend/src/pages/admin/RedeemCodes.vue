<template>
  <div class="admin-redeem-codes">
    <div class="header">
      <h2>兑换码管理</h2>
      <el-button type="primary" @click="showGenerateDialog">生成兑换码</el-button>
    </div>

    <el-alert
      v-if="generatedCodes.length"
      title="本次生成的兑换码"
      type="success"
      show-icon
      closable
      class="generated-codes-alert"
      @close="generatedCodes = []"
    >
      <template #default>
        <div class="generated-code-list">
          <div v-for="code in generatedCodes" :key="code" class="generated-code-row">
            <span class="generated-code-text">{{ code }}</span>
            <el-button size="small" type="primary" plain @click="copyGeneratedCode(code)">复制</el-button>
          </div>
        </div>
      </template>
    </el-alert>

    <el-table :data="codes" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="code" label="兑换码" width="180">
        <template #default="{ row }">
          <el-tag size="small" type="info">{{ row.code }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="plan_id" label="套餐 ID" width="90" />
      <el-table-column prop="duration_days" label="时长（天）" width="100" />
      <el-table-column prop="is_used" label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.is_used ? 'info' : 'success'" size="small">
            {{ row.is_used ? '已使用' : '未使用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="used_at" label="使用时间" width="180">
        <template #default="{ row }">{{ row.used_at ? formatDate(row.used_at) : '-' }}</template>
      </el-table-column>
    </el-table>

    <div class="pagination" style="margin-top: 16px; text-align: right">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchCodes"
      />
    </div>

    <el-dialog v-model="dialogVisible" title="生成兑换码" width="400px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="100px">
        <el-form-item label="套餐 ID" prop="plan_id">
          <el-input-number v-model="form.plan_id" :min="1" />
        </el-form-item>
        <el-form-item label="时长（天）" prop="duration_days">
          <el-input-number v-model="form.duration_days" :min="1" />
        </el-form-item>
        <el-form-item label="数量" prop="count">
          <el-input-number v-model="form.count" :min="1" :max="100" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleGenerate" :loading="generating">生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
// 管理后台 - 兑换码管理页。
import { ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage } from 'element-plus'

const codes = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const dialogVisible = ref(false)
const generating = ref(false)
const formRef = ref(null)
const generatedCodes = ref([])

const form = reactive({
  plan_id: 1,
  duration_days: 30,
  count: 10,
})

const rules = {
  plan_id: [{ required: true, message: '请输入套餐 ID', trigger: 'blur' }],
  duration_days: [{ required: true, message: '请输入时长', trigger: 'blur' }],
  count: [{ required: true, message: '请输入数量', trigger: 'blur' }],
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

async function copyGeneratedCode(code) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(code)
    } else if (!fallbackCopy(code)) {
      throw new Error('fallback copy failed')
    }
    ElMessage.success('兑换码已复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}

function fallbackCopy(text) {
  const input = document.createElement('textarea')
  input.value = text
  input.setAttribute('readonly', 'readonly')
  input.style.position = 'fixed'
  input.style.left = '-9999px'
  document.body.appendChild(input)
  input.select()
  const ok = document.execCommand('copy')
  document.body.removeChild(input)
  return ok
}

async function showGenerateDialog() {
  dialogVisible.value = true
}

async function handleGenerate() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  generating.value = true
  try {
    const res = await adminApi.redeemCodes.generate({
      plan_id: form.plan_id,
      duration_days: form.duration_days,
      count: form.count,
    })
    generatedCodes.value = res.data.codes || []
    ElMessage.success(`成功生成 ${res.data.count} 个兑换码`)
    dialogVisible.value = false
    page.value = 1
    await fetchCodes()
  } catch (err) {
    ElMessage.error(err.message || '生成失败')
  } finally {
    generating.value = false
  }
}

async function fetchCodes() {
  loading.value = true
  try {
    const res = await adminApi.redeemCodes.list({ page: page.value, size: size.value })
    codes.value = res.data.codes || []
    total.value = res.data.total || 0
  } catch (err) {
    ElMessage.error('获取兑换码列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchCodes()
})
</script>

<style scoped>
.admin-redeem-codes {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.generated-codes-alert {
  margin-bottom: 16px;
}
.generated-code-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}
.generated-code-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.generated-code-text {
  min-width: 180px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 13px;
}
</style>
