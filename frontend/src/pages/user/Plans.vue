<template>
  <div class="plans-page">
    <h2>套餐列表</h2>
    <div v-loading="loading" class="plans-grid">
      <el-row :gutter="20">
        <el-col v-for="plan in plans" :key="plan.id" :xs="24" :sm="12" :md="8" :lg="6">
          <el-card class="plan-card" shadow="hover">
            <div class="plan-name">{{ plan.name }}</div>
            <div class="plan-price">
              <span class="amount">{{ plan.price }}</span>
              <span class="currency">{{ plan.currency || 'USDT' }}</span>
            </div>
            <ul class="plan-features">
              <li>流量：{{ formatBytes(plan.traffic_limit) }}</li>
              <li>时长：{{ plan.duration_days }} 天</li>
            </ul>
            <el-button type="primary" @click="handleBuy(plan)" style="width: 100%; margin-top: 12px">
              立即购买
            </el-button>
          </el-card>
        </el-col>
      </el-row>
      <el-empty v-if="plans.length === 0 && !loading" description="暂无可用套餐，请联系管理员" />
    </div>
  </div>
</template>

<script setup>
// 套餐列表页。展示上架中的套餐，用户可选择购买。
import { ref, onMounted } from 'vue'
import { planApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const userStore = useUserStore()
const plans = ref([])
const loading = ref(false)

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

function handleBuy(plan) {
  if (!userStore.isLoggedIn) {
    ElMessage.warning('请先登录')
    router.push({ name: 'Login', query: { redirect: '/plans' } })
    return
  }
  
  ElMessageBox.confirm(
    `确定购买套餐"${plan.name}"吗？价格：${plan.price} ${plan.currency || 'USDT'}`,
    '确认购买',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'info',
    }
  ).then(() => {
    // TODO: v1 阶段暂不实现支付流程
    ElMessage.info('支付功能开发中，敬请期待')
  }).catch(() => {})
}

async function fetchPlans() {
  loading.value = true
  try {
    const res = await planApi.listActive()
    plans.value = res.data.plans || []
  } catch (err) {
    ElMessage.error('获取套餐列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchPlans()
})
</script>

<style scoped>
.plans-page {
  padding: 20px;
}
.plans-page h2 {
  margin-bottom: 20px;
}
.plans-grid {
  min-height: 200px;
}
.plan-card {
  text-align: center;
  margin-bottom: 20px;
}
.plan-name {
  font-size: 18px;
  font-weight: bold;
  margin-bottom: 8px;
}
.plan-price {
  margin-bottom: 16px;
}
.plan-price .amount {
  font-size: 28px;
  font-weight: bold;
  color: #409eff;
}
.plan-price .currency {
  font-size: 14px;
  color: #606266;
  margin-left: 4px;
}
.plan-features {
  list-style: none;
  padding: 0;
  margin: 0 0 12px;
  text-align: left;
  font-size: 14px;
  color: #606266;
}
.plan-features li {
  padding: 4px 0;
}
</style>
