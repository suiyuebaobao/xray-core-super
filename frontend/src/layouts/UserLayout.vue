<template>
  <el-container class="user-layout">
    <el-header class="header">
      <div class="header-left">
        <router-link to="/" class="logo">岁月订阅</router-link>
      </div>
      <div class="header-right">
        <router-link to="/">首页</router-link>
        <router-link to="/plans">套餐</router-link>
        <router-link to="/subscription">我的订阅</router-link>
        <router-link to="/redeem">兑换码</router-link>
        <router-link to="/profile">个人中心</router-link>
        <el-button text @click="handleLogout">退出</el-button>
      </div>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>

<script setup>
// 用户侧布局。顶部导航栏 + 主内容区。
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { ElMessage } from 'element-plus'
import { authApi } from '@/api'

const router = useRouter()
const userStore = useUserStore()

async function handleLogout() {
  try {
    await authApi.logout()
  } catch {
    // 忽略错误
  }
  userStore.logout()
  ElMessage.success('已退出登录')
  router.push('/login')
}
</script>

<style scoped>
.user-layout {
  min-height: 100vh;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #ebeef5;
  background: #fff;
}
.header-left .logo {
  font-size: 20px;
  font-weight: bold;
  color: #409eff;
  text-decoration: none;
}
.header-right a, .header-right .el-button {
  margin-left: 16px;
  color: #606266;
  text-decoration: none;
  font-size: 14px;
}
</style>
