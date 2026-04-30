<template>
  <div class="admin-orders">
    <h2>订单管理</h2>
    <el-table :data="orders" border style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="order_no" label="订单号" />
      <el-table-column prop="user_id" label="用户 ID" width="90" />
      <el-table-column prop="amount" label="金额" width="120">
        <template #default="{ row }">{{ row.amount }} {{ row.currency }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTag(row.status)">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
    </el-table>
    <div class="pagination" style="margin-top: 16px; text-align: right">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="fetchOrders"
      />
    </div>
    <el-empty v-if="orders.length === 0 && !loading" description="暂无订单" />
  </div>
</template>

<script setup>
// 管理后台 - 订单管理页。v1 阶段为骨架，展示订单列表。
import { ref, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage } from 'element-plus'

const orders = ref([])
const page = ref(1)
const size = ref(20)
const total = ref(0)
const loading = ref(false)

function statusTag(status) {
  switch (status) {
    case 'PAID': return 'success'
    case 'PENDING': return 'warning'
    case 'EXPIRED': return 'info'
    default: return 'info'
  }
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

async function fetchOrders() {
  loading.value = true
  try {
    const res = await adminApi.orders.list({ page: page.value, size: size.value })
    orders.value = res.data.orders || []
    total.value = res.data.total || 0
  } catch (err) {
    ElMessage.error('获取订单列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchOrders()
})
</script>

<style scoped>
.admin-orders {
  padding: 20px;
}
</style>
