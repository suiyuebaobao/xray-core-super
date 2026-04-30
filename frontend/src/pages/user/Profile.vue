<template>
  <div class="user-profile">
    <h2>个人资料</h2>

    <el-form :model="form" :rules="rules" ref="formRef" label-width="100px" style="max-width: 500px">
      <el-form-item label="用户名">
        <el-input :value="userStore.user?.username" disabled />
      </el-form-item>
      <el-form-item label="邮箱" prop="email">
        <el-input v-model="form.email" placeholder="可选" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="handleUpdateProfile" :loading="updating">保存</el-button>
      </el-form-item>
    </el-form>

    <el-divider />

    <h3>修改密码</h3>
    <el-form :model="pwdForm" :rules="pwdRules" ref="pwdFormRef" label-width="120px" style="max-width: 500px">
      <el-form-item label="原密码" prop="old_password">
        <el-input v-model="pwdForm.old_password" type="password" show-password />
      </el-form-item>
      <el-form-item label="新密码" prop="new_password">
        <el-input v-model="pwdForm.new_password" type="password" show-password />
      </el-form-item>
      <el-form-item label="确认新密码" prop="confirm_password">
        <el-input v-model="pwdForm.confirm_password" type="password" show-password />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="handleChangePassword" :loading="changingPwd">修改密码</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
// 用户个人资料页 — 修改资料和密码。
import { ref, reactive, onMounted } from 'vue'
import { userApi } from '@/api'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const formRef = ref(null)
const pwdFormRef = ref(null)
const updating = ref(false)
const changingPwd = ref(false)

const form = reactive({
  email: '',
})

const rules = {
  email: [{ type: 'email', message: '请输入有效的邮箱地址', trigger: 'blur' }],
}

const pwdForm = reactive({
  old_password: '',
  new_password: '',
  confirm_password: '',
})

const pwdRules = {
  old_password: [{ required: true, message: '请输入原密码', trigger: 'blur' }],
  new_password: [{ required: true, message: '请输入新密码', trigger: 'blur', min: 6 }],
  confirm_password: [{ required: true, message: '请再次输入新密码', trigger: 'blur' }],
}

async function handleUpdateProfile() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  updating.value = true
  try {
    const body = {}
    if (form.email) body.email = form.email
    const res = await userApi.updateProfile(body)
    ElMessage.success('资料已更新')
    // 更新本地用户信息
    if (res.data.user) {
      userStore.updateUser(res.data.user)
    }
  } catch (err) {
    ElMessage.error(err.message || '更新失败')
  } finally {
    updating.value = false
  }
}

async function handleChangePassword() {
  const valid = await pwdFormRef.value.validate().catch(() => false)
  if (!valid) return
  if (pwdForm.new_password !== pwdForm.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }

  changingPwd.value = true
  try {
    await userApi.changePassword({
      old_password: pwdForm.old_password,
      new_password: pwdForm.new_password,
    })
    ElMessage.success('密码已修改，请重新登录')
    // 修改密码后登出
    userStore.logout()
    location.href = '/login'
  } catch (err) {
    ElMessage.error(err.message || '修改失败')
  } finally {
    changingPwd.value = false
  }
}

onMounted(async () => {
  try {
    const res = await userApi.me()
    if (res.data.user && res.data.user.email) {
      form.email = res.data.user.email
    }
  } catch {
    // ignore
  }
})
</script>

<style scoped>
.user-profile {
  padding: 24px;
  max-width: 600px;
}
h2 {
  margin-bottom: 24px;
}
h3 {
  margin-bottom: 16px;
}
</style>
