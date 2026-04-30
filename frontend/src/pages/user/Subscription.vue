<template>
  <div class="subscription-page" v-loading="loading">
    <div class="header-row">
      <h2>我的订阅</h2>
      <el-button :loading="loading" @click="fetchSubscription" circle>
        <el-icon><Refresh /></el-icon>
      </el-button>
    </div>

    <el-alert v-if="!subscription" title="暂无订阅" description="请先购买套餐或使用兑换码开通订阅" type="info" :closable="false" style="margin-bottom: 20px">
      <template #footer>
        <el-button type="primary" @click="$router.push('/plans')">查看套餐</el-button>
        <el-button @click="$router.push('/redeem')">使用兑换码</el-button>
      </template>
    </el-alert>

    <el-alert v-if="subscription?.status === 'EXPIRED'" title="订阅已过期" description="请及时续费或购买新套餐以继续使用" type="warning" :closable="false" style="margin-bottom: 20px" />

    <el-alert v-if="subscription?.status === 'SUSPENDED'" title="订阅已暂停" description="您的订阅因流量超限已被暂停，请联系管理员处理" type="error" :closable="false" style="margin-bottom: 20px" />

    <template v-if="subscription">
      <el-card style="margin-bottom: 20px">
        <template #header>
          <span>订阅信息</span>
        </template>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="套餐 ID">{{ subscription.plan_id }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="statusTagType">{{ subscription.status }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="到期时间">{{ formatDate(subscription.expire_date) }}</el-descriptions-item>
          <el-descriptions-item label="已用流量">{{ formatBytes(subscription.used_traffic) }} / {{ formatBytes(subscription.traffic_limit) }}</el-descriptions-item>
        </el-descriptions>
        <div class="progress-wrap">
          <el-progress :percentage="trafficPercent" :color="trafficColor" :stroke-width="12" />
        </div>
      </el-card>

      <el-card style="margin-bottom: 20px" v-loading="usageLoading">
        <template #header>
          <span>用量记录</span>
        </template>
        <template v-if="usageData">
          <div class="usage-summary">
            <div class="usage-metric">
              <span>今日</span>
              <strong>{{ formatBytes(usageData.summary?.today?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.today?.upload) }} / 下行 {{ formatBytes(usageData.summary?.today?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>本周</span>
              <strong>{{ formatBytes(usageData.summary?.current_week?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.current_week?.upload) }} / 下行 {{ formatBytes(usageData.summary?.current_week?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>本月</span>
              <strong>{{ formatBytes(usageData.summary?.current_month?.total) }}</strong>
              <small>上行 {{ formatBytes(usageData.summary?.current_month?.upload) }} / 下行 {{ formatBytes(usageData.summary?.current_month?.download) }}</small>
            </div>
            <div class="usage-metric">
              <span>截止今日</span>
              <strong>{{ formatBytes(usageData.summary?.subscription_to_today?.total) }}</strong>
              <small>{{ usageData.plan_name || '当前套餐' }}</small>
            </div>
          </div>
          <el-tabs v-model="usageTab">
            <el-tab-pane label="按天" name="daily">
              <el-table :data="usageData.daily || []" border>
                <el-table-column prop="date" label="日期" width="130" />
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="按周" name="weekly">
              <el-table :data="usageData.weekly || []" border>
                <el-table-column label="周期" min-width="180">
                  <template #default="{ row }">{{ row.start_at }} 至 {{ row.end_at }}</template>
                </el-table-column>
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="按月" name="monthly">
              <el-table :data="usageData.monthly || []" border>
                <el-table-column prop="month" label="月份" width="130" />
                <el-table-column label="上行">
                  <template #default="{ row }">{{ formatBytes(row.upload) }}</template>
                </el-table-column>
                <el-table-column label="下行">
                  <template #default="{ row }">{{ formatBytes(row.download) }}</template>
                </el-table-column>
                <el-table-column label="合计">
                  <template #default="{ row }">{{ formatBytes(row.total) }}</template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
          </el-tabs>
        </template>
        <el-empty v-else description="暂无用量记录" :image-size="80" />
      </el-card>

      <el-card>
        <template #header>
          <span>订阅链接</span>
        </template>
        <p style="color: #606266; font-size: 14px; margin-bottom: 16px">点击下方链接在对应的代理客户端中导入订阅。</p>

        <div class="sub-links">
          <el-radio-group v-model="selectedFormat" size="large" class="format-selector">
            <el-radio-button v-for="format in subscriptionFormats" :key="format.value" :value="format.value">
              {{ format.label }}
            </el-radio-button>
          </el-radio-group>
          <div class="sub-link-item">
            <div class="link-label">{{ selectedFormatLabel }}</div>
            <div class="link-url">{{ selectedSubscriptionUrl }}</div>
            <el-button size="small" type="primary" @click="copyUrl(selectedSubscriptionUrl)">复制链接</el-button>
          </div>
        </div>

        <el-alert title="使用说明" type="info" :closable="false" style="margin-top: 16px">
          <template #default>
            <p style="margin: 4px 0">1. 复制链接后，在对应的代理客户端中导入订阅。</p>
            <p style="margin: 4px 0">2. <strong>Clash/mihomo</strong>：适用于 Clash Verge Rev、mihomo 等客户端。</p>
            <p style="margin: 4px 0">3. <strong>Base64</strong>：适用于 Shadowrocket、Surge、Quantumult 等通用客户端。</p>
            <p style="margin: 4px 0">4. <strong>纯文本 URI</strong>：VLESS 协议原始链接，兼容性最广。</p>
          </template>
        </el-alert>
      </el-card>
    </template>
  </div>
</template>

<script setup>
// 我的订阅页。展示订阅信息和三种格式的订阅链接。
import { ref, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { userApi } from '@/api'
import { ElMessage } from 'element-plus'

const subscription = ref(null)
const usageData = ref(null)
const loading = ref(false)
const usageLoading = ref(false)
const usageTab = ref('daily')
const selectedFormat = ref('clash')

const subscriptionFormats = [
  { value: 'clash', label: 'Clash / mihomo' },
  { value: 'base64', label: 'Base64' },
  { value: 'plain', label: 'URI' },
]

const selectedSubscriptionUrl = computed(() => subscriptionUrl(selectedFormat.value))
const selectedFormatLabel = computed(() => subscriptionFormats.find((item) => item.value === selectedFormat.value)?.label || 'Clash / mihomo')

const trafficPercent = computed(() => {
  if (!subscription.value || !subscription.value.traffic_limit) return 0
  return Math.min(100, Math.round((subscription.value.used_traffic / subscription.value.traffic_limit) * 100))
})

const trafficColor = computed(() => {
  if (trafficPercent.value > 90) return '#f56c6c'
  if (trafficPercent.value > 70) return '#e6a23c'
  return '#409eff'
})

const statusTagType = computed(() => {
  switch (subscription.value?.status) {
    case 'ACTIVE': return 'success'
    case 'EXPIRED': return 'info'
    case 'SUSPENDED': return 'warning'
    default: return 'info'
  }
})

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
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

function copyUrl(url) {
  if (!url) return
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动复制')
  })
}

function subscriptionUrl(format) {
  const token = subscription.value?.tokens?.[0]
  return token ? `${window.location.origin}/sub/${token}/${format}` : ''
}

async function fetchSubscription() {
  loading.value = true
  try {
    const res = await userApi.subscription()
    if (res.data.subscription) {
      subscription.value = res.data.subscription
      await fetchUsage()
    } else {
      subscription.value = null
      usageData.value = null
    }
  } catch (err) {
    // 500 等后端错误应显示提示，但 404/无订阅算正常
    if (err && err.message && err.message !== '订阅已过期或不可用') {
      ElMessage.error('获取订阅信息失败：' + (err.message || '未知错误'))
    }
    subscription.value = null
    usageData.value = null
  } finally {
    loading.value = false
  }
}

async function fetchUsage() {
  usageLoading.value = true
  usageTab.value = 'daily'
  try {
    const res = await userApi.usage({ days: 30, weeks: 8, months: 12 })
    usageData.value = res.data
  } catch {
    usageData.value = null
  } finally {
    usageLoading.value = false
  }
}

onMounted(() => {
  fetchSubscription()
})
</script>

<style scoped>
.subscription-page {
  padding: 20px;
}
.subscription-page h2 {
  margin-bottom: 20px;
}
.progress-wrap {
  margin-top: 16px;
}
.usage-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
}
.usage-metric {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 12px;
  min-width: 0;
}
.usage-metric span,
.usage-metric small {
  display: block;
  color: #909399;
}
.usage-metric strong {
  display: block;
  font-size: 20px;
  line-height: 28px;
  margin: 4px 0;
  color: #303133;
}
.sub-links {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.format-selector {
  align-self: flex-start;
}
.sub-link-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
}
.link-label {
  font-weight: bold;
  min-width: 140px;
  font-size: 14px;
}
.link-url {
  flex: 1;
  font-family: monospace;
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}
@media (max-width: 900px) {
  .usage-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
