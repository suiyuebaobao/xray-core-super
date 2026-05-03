<template>
  <el-container class="admin-layout">
    <el-aside width="220px">
      <div class="logo">RayPilot · 管理后台</div>
      <el-menu :default-active="activeMenu" router background-color="#304156" text-color="#bfcbd9" active-text-color="#409eff">
        <el-menu-item index="/admin">
          <el-icon><HomeFilled /></el-icon>
          <span>仪表盘</span>
        </el-menu-item>
        <el-menu-item index="/admin/plans">
          <el-icon><Goods /></el-icon>
          <span>套餐管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/node-groups">
          <el-icon><Connection /></el-icon>
          <span>节点分组</span>
        </el-menu-item>
        <el-menu-item index="/admin/nodes">
          <el-icon><Monitor /></el-icon>
          <span>出口节点</span>
        </el-menu-item>
        <el-menu-item index="/admin/relays">
          <el-icon><Connection /></el-icon>
          <span>中转节点</span>
        </el-menu-item>
        <el-menu-item index="/admin/users">
          <el-icon><UserFilled /></el-icon>
          <span>用户管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/orders">
          <el-icon><Document /></el-icon>
          <span>订单管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/redeem-codes">
          <el-icon><Ticket /></el-icon>
          <span>兑换码管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/subscription-tokens">
          <el-icon><Key /></el-icon>
          <span>订阅 Token</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="admin-header">
        <span>{{ userStore.user?.username || '管理员' }}</span>
        <el-button text @click="handleLogout">退出</el-button>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
// 管理后台布局。左侧菜单 + 右侧内容区。
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useRoute } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { ElMessage } from 'element-plus'
import { authApi } from '@/api'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const activeMenu = computed(() => route.path)

async function handleLogout() {
  try {
    await authApi.logout()
  } catch {
    // 忽略错误
  }
  userStore.logout()
  ElMessage.success('已退出登录')
  router.push('/admin/login')
}
</script>

<style scoped>
.admin-layout {
  min-height: 100vh;
}
.el-aside {
  background: #304156;
}
.logo {
  height: 60px;
  line-height: 60px;
  text-align: center;
  color: #fff;
  font-size: 16px;
  font-weight: bold;
  border-bottom: 1px solid #3d4f63;
}
.admin-header {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  border-bottom: 1px solid #ebeef5;
  background: #fff;
}
</style>
