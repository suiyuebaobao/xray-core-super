// admin-dashboard — 管理后台仪表盘。
//
// 功能：
// - 显示用户/节点/套餐/有效订阅统计
// - 提供快捷操作按钮跳转到对应管理页
// - 数据来自真实 API，每 30 秒自动刷新
<template>
  <div class="admin-dashboard">
    <h2>管理后台仪表盘</h2>
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-value">{{ stats.userCount }}</div>
            <div class="stat-label">用户总数</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-value">{{ stats.nodeCount }}</div>
            <div class="stat-label">节点总数</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-value">{{ stats.planCount }}</div>
            <div class="stat-label">套餐总数</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-value">{{ stats.activeSubCount }}</div>
            <div class="stat-label">有效订阅</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px">
      <template #header>
        <span>快捷操作</span>
      </template>
      <div class="quick-actions">
        <el-button @click="$router.push('/admin/plans')">套餐管理</el-button>
        <el-button @click="$router.push('/admin/nodes')">节点管理</el-button>
        <el-button @click="$router.push('/admin/users')">用户管理</el-button>
        <el-button @click="$router.push('/admin/redeem-codes')">兑换码管理</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage } from 'element-plus'

const stats = ref({
  userCount: 0,
  nodeCount: 0,
  planCount: 0,
  activeSubCount: 0,
})

let timer = null

async function fetchStats() {
  try {
    stats.value = await adminApi.dashboard()
  } catch (err) {
    ElMessage.error('获取统计数据失败：' + (err.message || '未知错误'))
  }
}

onMounted(() => {
  fetchStats()
  timer = setInterval(fetchStats, 30000) // 30 秒刷新
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.admin-dashboard {
  padding: 20px;
}
.stat-item {
  text-align: center;
}
.stat-value {
  font-size: 36px;
  font-weight: bold;
  color: #409eff;
}
.stat-label {
  font-size: 14px;
  color: #606266;
  margin-top: 8px;
}
.quick-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
</style>
