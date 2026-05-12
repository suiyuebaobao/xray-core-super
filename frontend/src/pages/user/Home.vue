<template>
  <div class="home-page" v-loading="loading">
    <h2>欢迎使用 RayPilot</h2>
    <el-row :gutter="20">
      <!-- 用户信息卡片 -->
      <el-col :xs="24" :sm="12" :md="8">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>账户信息</span>
            </div>
          </template>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="用户名">{{ userStore.user?.username }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="userStore.user?.status === 'active' ? 'success' : 'danger'">
                {{ userStore.user?.status }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="注册时间">
              {{ formatDate(userStore.user?.created_at) }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>

      <!-- 订阅信息卡片 -->
      <el-col :xs="24" :sm="12" :md="8">
        <el-card>
          <template #header>
            <span>订阅状态</span>
          </template>
          <div v-if="subscription" class="sub-info">
            <el-descriptions :column="1" border>
              <el-descriptions-item label="状态">
                <el-tag :type="subTagType">{{ subscription.status }}</el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="到期时间">{{ formatDate(subscription.expire_date) }}</el-descriptions-item>
              <el-descriptions-item label="普通流量">{{ formatBytes(subscription.used_traffic) }} / {{ formatBytes(subscription.traffic_limit) }}</el-descriptions-item>
              <el-descriptions-item label="家宽流量">{{ formatBytes(subscription.residential_used_traffic) }} / {{ formatBytes(subscription.residential_traffic_limit) }}</el-descriptions-item>
            </el-descriptions>
            <div class="progress-wrap">
              <div v-for="pool in trafficPools" :key="pool.key" class="traffic-progress">
                <div class="traffic-progress-head">
                  <span>{{ pool.label }}进度</span>
                  <small>{{ formatBytes(pool.used) }} / {{ formatBytes(pool.limit) }}</small>
                </div>
                <el-progress :percentage="pool.percent" :color="trafficColor(pool.percent)" />
              </div>
            </div>
            <!-- 订阅链接快速复制 -->
            <div v-if="subscription.tokens?.length > 0" class="sub-quick-link">
              <div class="link-label">Clash / mihomo 订阅链接</div>
              <el-input :model-value="getSubscriptionUrl()" readonly size="small">
                <template #append>
                  <el-button @click="copyUrl(getSubscriptionUrl())">复制</el-button>
                </template>
              </el-input>
            </div>
          </div>
          <el-empty v-else description="暂无订阅，请先购买套餐" :image-size="80">
            <el-button type="primary" @click="$router.push('/plans')">查看套餐</el-button>
          </el-empty>
          <el-button v-if="subscription" type="primary" @click="$router.push('/subscription')" style="margin-top: 12px">
            查看订阅详情
          </el-button>
        </el-card>
      </el-col>

      <!-- 快捷操作 -->
      <el-col :xs="24" :sm="12" :md="8">
        <el-card>
          <template #header>
            <span>快捷操作</span>
          </template>
          <div class="quick-actions">
            <el-button @click="$router.push('/plans')" style="margin: 4px">购买套餐</el-button>
            <el-button @click="$router.push('/redeem')" style="margin: 4px">兑换码</el-button>
            <el-button @click="$router.push('/subscription')" style="margin: 4px">我的订阅</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
// 用户首页。展示用户信息、订阅状态、快捷操作。
// 进入页面时获取最新用户和订阅信息。
import { ref, computed, onMounted } from 'vue'
import { useUserStore } from '@/stores/user'
import { userApi } from '@/api'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'

const userStore = useUserStore()
const subscription = ref(null)
const loading = ref(false)

const trafficPools = computed(() => {
  if (!subscription.value) return []
  return [
    {
      key: 'normal',
      label: '普通流量',
      used: subscription.value.used_traffic || 0,
      limit: subscription.value.traffic_limit || 0,
    },
    {
      key: 'residential',
      label: '家宽流量',
      used: subscription.value.residential_used_traffic || 0,
      limit: subscription.value.residential_traffic_limit || 0,
    },
  ].map((pool) => ({
    ...pool,
    percent: trafficPercent(pool.used, pool.limit),
  }))
})

function trafficPercent(used, limit) {
  const total = Number(limit || 0)
  if (!total) return 0
  return Math.min(100, Math.round((Number(used || 0) / total) * 100))
}

function trafficColor(percent) {
  if (percent > 90) return '#f56c6c'
  if (percent > 70) return '#e6a23c'
  return '#42f5ff'
}

const subTagType = computed(() => {
  switch (subscription.value?.status) {
    case 'ACTIVE': return 'success'
    case 'EXPIRED': return 'info'
    case 'SUSPENDED': return 'warning'
    default: return 'info'
  }
})

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleDateString('zh-CN')
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

async function fetchMe() {
  loading.value = true
  try {
    // 获取用户信息
    const userRes = await userApi.me()
    if (userRes.data.subscription) {
      subscription.value = userRes.data.subscription
    }

    // 获取订阅 Token 用于生成订阅链接
    if (subscription.value) {
      try {
        const subRes = await userApi.subscription()
        if (subRes.data.subscription) {
          subscription.value.tokens = subRes.data.subscription.tokens || []
        }
      } catch (err) {
        // 订阅 Token 获取失败不影响主流程
      }
    }
  } catch (err) {
    ElMessage.error('获取用户信息失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchMe()
})

function copyUrl(url) {
  if (!url) return
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动复制')
  })
}

function getSubscriptionUrl() {
  if (!subscription.value || !subscription.value.tokens?.[0]) return ''
  return `${window.location.origin}/sub/${subscription.value.tokens[0]}`
}
</script>

<style scoped>
.home-page {
  padding: 20px;
  min-height: calc(100vh - 60px);
}
.home-page h2 {
  margin-bottom: 20px;
  color: var(--rp-text);
}
.progress-wrap {
  margin-top: 12px;
}
.traffic-progress + .traffic-progress {
  margin-top: 10px;
}
.traffic-progress-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
  color: var(--rp-muted);
  font-size: 12px;
}
.sub-quick-link {
  margin-top: 12px;
}
.quick-subscription-format {
  margin-bottom: 8px;
}
.sub-quick-link .link-label {
  font-size: 13px;
  color: var(--rp-muted);
  margin-bottom: 6px;
}
.quick-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
