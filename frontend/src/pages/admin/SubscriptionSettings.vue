<template>
  <div class="admin-subscription-settings">
    <div class="page-header">
      <div>
        <p class="eyebrow">Subscription Console</p>
        <h2>订阅配置</h2>
        <p>自定义客户端导入名称、订阅规则和用量展示头，保存后下一次订阅下载立即生效。</p>
      </div>
      <div class="header-actions">
        <el-button @click="fetchConfig" :loading="loading">刷新</el-button>
        <el-button @click="restoreDefaultRules">恢复默认规则</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存配置</el-button>
      </div>
    </div>

    <el-form v-loading="loading" class="settings-grid" label-position="top">
      <el-card class="control-card">
        <template #header>基础信息</template>
        <div class="form-grid two">
          <el-form-item label="订阅名称">
            <el-input v-model="form.profile_name" maxlength="64" placeholder="例如：雷云 VPN" />
            <p class="field-tip">Clash/mihomo 下载文件名会使用这个名称，例如 `雷云 VPN.yaml`。</p>
          </el-form-item>
          <el-form-item label="更新间隔（小时）">
            <el-input-number v-model="form.profile_update_interval" :min="0" :max="168" controls-position="right" />
            <p class="field-tip">写入 `profile-update-interval` 响应头，0 表示不输出。</p>
          </el-form-item>
        </div>
        <el-form-item label="订阅页面地址">
          <el-input v-model="form.profile_web_page_url" placeholder="/subscription 或 https://example.com" />
          <p class="field-tip">可选，写入 `profile-web-page-url`。只允许站内路径、锚点、HTTP 或 HTTPS。</p>
        </el-form-item>
        <el-form-item label="用量信息">
          <el-switch v-model="form.include_user_info" active-text="输出用量头" inactive-text="不输出" />
          <p class="field-tip">开启后订阅响应会输出 `subscription-userinfo`，客户端可展示已用、总量和到期时间。</p>
        </el-form-item>
      </el-card>

      <el-card class="preview-card">
        <template #header>响应预览</template>
        <div class="terminal-preview">
          <span>Content-Disposition: attachment; filename="{{ safeProfileName }}.yaml"</span>
          <span>profile-title: base64:{{ profileTitlePreview }}</span>
          <span v-if="form.include_user_info">subscription-userinfo: upload=0; download=&lt;已用字节&gt;; total=&lt;总量字节&gt;; expire=&lt;到期时间&gt;</span>
          <span v-if="form.profile_update_interval">profile-update-interval: {{ form.profile_update_interval }}</span>
          <span v-if="form.profile_web_page_url">profile-web-page-url: {{ form.profile_web_page_url }}</span>
        </div>
        <div class="rule-chips">
          <el-tag v-for="rule in normalizedRules" :key="rule">{{ rule }}</el-tag>
        </div>
      </el-card>

      <el-card class="rules-card">
        <template #header>
          <div class="card-head">
            <span>Clash / Mihomo 规则</span>
            <small>{{ normalizedRules.length }} 条有效规则</small>
          </div>
        </template>
        <el-form-item label="自定义规则">
          <el-input
            v-model="rulesText"
            type="textarea"
            :rows="14"
            placeholder="每行一条规则，例如：DOMAIN-SUFFIX,openai.com,PROXY"
          />
        </el-form-item>
        <div class="rule-help">
          <span>空行和注释行会被忽略。</span>
          <span>如果没有兜底规则，后端会自动追加 `MATCH,PROXY`。</span>
          <span>规则会写入 Clash YAML 的 `rules` 段，不影响 Base64 和 URI 格式。</span>
        </div>
      </el-card>
    </el-form>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { adminApi } from '@/api'

const defaultRules = ['GEOIP,CN,DIRECT', 'MATCH,PROXY']
const loading = ref(false)
const saving = ref(false)
const rulesText = ref(defaultRules.join('\n'))
const form = reactive({
  profile_name: 'RayPilot',
  custom_rules: [...defaultRules],
  include_user_info: true,
  profile_update_interval: 24,
  profile_web_page_url: '',
})

const normalizedRules = computed(() => {
  const rules = rulesText.value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#') && !line.startsWith('//'))
  return rules.length ? rules : defaultRules
})

const safeProfileName = computed(() => {
  const name = (form.profile_name || 'RayPilot').trim().replace(/[\\/:*?"<>|]/g, '-').replace(/^[.\-]+|[.\-]+$/g, '')
  return name || 'RayPilot'
})

const profileTitlePreview = computed(() => {
  try {
    return btoa(unescape(encodeURIComponent(safeProfileName.value)))
  } catch {
    return '...'
  }
})

function assignConfig(data = {}) {
  form.profile_name = data.profile_name || 'RayPilot'
  form.custom_rules = Array.isArray(data.custom_rules) && data.custom_rules.length ? data.custom_rules : [...defaultRules]
  form.include_user_info = data.include_user_info !== false
  form.profile_update_interval = Number(data.profile_update_interval || 0)
  form.profile_web_page_url = data.profile_web_page_url || ''
  rulesText.value = form.custom_rules.join('\n')
}

function payload() {
  return {
    profile_name: form.profile_name,
    custom_rules: normalizedRules.value,
    include_user_info: form.include_user_info,
    profile_update_interval: Number(form.profile_update_interval || 0),
    profile_web_page_url: form.profile_web_page_url,
  }
}

function restoreDefaultRules() {
  rulesText.value = defaultRules.join('\n')
}

async function fetchConfig() {
  loading.value = true
  try {
    const res = await adminApi.site.subscription()
    assignConfig(res.data)
  } catch (err) {
    ElMessage.error(err.message || '获取订阅配置失败')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const res = await adminApi.site.updateSubscription(payload())
    assignConfig(res.data)
    ElMessage.success('订阅配置已保存')
  } catch (err) {
    ElMessage.error(err.message || '保存订阅配置失败')
  } finally {
    saving.value = false
  }
}

onMounted(fetchConfig)
</script>

<style scoped>
.admin-subscription-settings {
  padding: 20px;
}
.page-header,
.header-actions,
.card-head {
  display: flex;
  align-items: center;
}
.page-header {
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}
.page-header h2 {
  margin: 2px 0 0;
}
.page-header p {
  margin: 8px 0 0;
  color: var(--rp-muted);
}
.eyebrow {
  margin: 0 !important;
  color: var(--rp-cyan) !important;
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.header-actions {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 10px;
}
.settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
  gap: 18px;
}
.rules-card {
  grid-column: 1 / -1;
}
.form-grid {
  display: grid;
  gap: 14px;
}
.form-grid.two {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.field-tip {
  margin: 8px 0 0;
  color: var(--rp-muted);
  font-size: 12px;
  line-height: 1.6;
}
.terminal-preview {
  display: grid;
  gap: 10px;
  padding: 16px;
  color: #bdf8ff;
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
  line-height: 1.7;
  border: 1px solid rgba(92, 241, 255, 0.18);
  border-radius: 8px;
  background:
    linear-gradient(rgba(66, 245, 255, 0.045) 1px, transparent 1px),
    rgba(4, 9, 18, 0.9);
  background-size: 100% 24px;
  overflow-x: auto;
}
.rule-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}
.card-head {
  justify-content: space-between;
  width: 100%;
}
.card-head small {
  color: var(--rp-muted);
}
.rule-help {
  display: grid;
  gap: 6px;
  color: var(--rp-muted);
  font-size: 13px;
}
@media (max-width: 1100px) {
  .settings-grid,
  .form-grid.two {
    grid-template-columns: 1fr;
  }
  .page-header {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
