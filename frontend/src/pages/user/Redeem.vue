<template>
  <div class="redeem-page">
    <el-card style="max-width: 500px; margin: 40px auto">
      <template #header>
        <div class="card-header">
          <span>兑换码</span>
        </div>
      </template>
      <el-form :model="form" :rules="rules" ref="formRef" @submit.prevent="handleRedeem">
        <el-form-item prop="code">
          <el-input v-model="form.code" placeholder="请输入兑换码" size="large" clearable @keyup.enter="handleRedeem" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleRedeem" :loading="loading" size="large" style="width: 100%">
            立即兑换
          </el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="successMessage" :title="successMessage" type="success" :closable="true" style="margin-top: 16px" @close="successMessage = ''" />
      <el-divider />
      <el-alert title="兑换码说明" type="info" :closable="false">
        <template #default>
          <p style="margin: 4px 0">1. 兑换码由管理员生成，每个码对应一个套餐和时长。</p>
          <p style="margin: 4px 0">2. 每个兑换码只能使用一次，使用后即失效。</p>
          <p style="margin: 4px 0">3. 如果您已有有效订阅，兑换将延长订阅到期时间。</p>
        </template>
      </el-alert>
    </el-card>
  </div>
</template>

<script setup>
// 兑换码页面。用户输入兑换码开通订阅。
import { ref, reactive } from 'vue'
import { redeemApi } from '@/api'
import { ElMessage } from 'element-plus'

const formRef = ref(null)
const form = reactive({ code: '' })
const loading = ref(false)
const successMessage = ref('')

const rules = {
  code: [{ required: true, message: '请输入兑换码', trigger: 'blur' }],
}

async function handleRedeem() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  successMessage.value = ''
  try {
    await redeemApi.redeem({ code: form.code })
    successMessage.value = '兑换成功！订阅已开通。'
    form.code = ''
    ElMessage.success('兑换成功')
  } catch (err) {
    ElMessage.error(err.message || '兑换失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.redeem-page {
  padding: 20px;
}
</style>
