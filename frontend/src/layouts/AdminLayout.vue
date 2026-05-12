<template>
  <el-container class="admin-layout">
    <el-aside width="220px">
      <div class="logo">
        <span class="logo-mark">RP</span>
        <span>RayPilot</span>
        <small>控制中枢</small>
      </div>
      <el-menu :default-active="activeMenu" router>
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
        <el-menu-item index="/admin/node-operations">
          <el-icon><Monitor /></el-icon>
          <span>节点运营</span>
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
        <el-menu-item index="/admin/subscription-settings">
          <el-icon><Document /></el-icon>
          <span>订阅配置</span>
        </el-menu-item>
        <el-menu-item index="/admin/sales-landing">
          <el-icon><Document /></el-icon>
          <span>销售首页</span>
        </el-menu-item>
        <el-menu-item index="/admin/logs">
          <el-icon><Document /></el-icon>
          <span>日志中心</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="admin-header">
        <div class="header-title">
          <span>Command Console</span>
          <small>{{ route.path }}</small>
        </div>
        <div class="admin-profile">
          <span class="signal-dot"></span>
          <span>{{ userStore.user?.username || '管理员' }}</span>
          <el-button text @click="handleLogout">退出</el-button>
        </div>
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
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
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
  background:
    radial-gradient(circle at 78% 12%, rgba(255, 61, 242, 0.12), transparent 28rem),
    radial-gradient(circle at 12% 28%, rgba(66, 245, 255, 0.14), transparent 26rem),
    var(--rp-bg);
}
.el-aside {
  position: relative;
  overflow: hidden;
  background: linear-gradient(180deg, rgba(7, 12, 24, 0.98), rgba(10, 20, 38, 0.96));
  border-right: 1px solid rgba(66, 245, 255, 0.16);
  box-shadow: 10px 0 36px rgba(0, 0, 0, 0.28);
}
.el-aside::before {
  content: "";
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(rgba(66, 245, 255, 0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(66, 245, 255, 0.035) 1px, transparent 1px);
  background-size: 28px 28px;
  mask-image: linear-gradient(to bottom, black, transparent);
}
.logo {
  position: relative;
  z-index: 1;
  height: 76px;
  display: grid;
  grid-template-columns: 42px 1fr;
  grid-template-rows: 1fr 1fr;
  align-items: center;
  column-gap: 10px;
  padding: 14px 16px;
  color: var(--rp-text);
  border-bottom: 1px solid rgba(66, 245, 255, 0.16);
}
.logo-mark {
  grid-row: 1 / 3;
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  color: #061019;
  font-weight: 800;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--rp-cyan), var(--rp-pink));
  box-shadow: 0 0 24px rgba(66, 245, 255, 0.36);
}
.logo span:not(.logo-mark) {
  align-self: end;
  font-size: 17px;
  font-weight: 800;
}
.logo small {
  align-self: start;
  color: var(--rp-muted);
  font-size: 12px;
}
.el-menu {
  position: relative;
  z-index: 1;
  border-right: 0;
  background: transparent;
  padding: 10px 10px;
}
.el-menu-item {
  height: 42px;
  margin: 4px 0;
  color: #9fb6cb;
  border-radius: 7px;
}
.el-menu-item:hover {
  color: var(--rp-cyan);
  background: rgba(66, 245, 255, 0.08);
}
.el-menu-item.is-active {
  color: var(--rp-cyan);
  background: linear-gradient(90deg, rgba(66, 245, 255, 0.16), rgba(255, 61, 242, 0.08));
  box-shadow: inset 2px 0 0 var(--rp-cyan), 0 0 18px rgba(66, 245, 255, 0.12);
}
.admin-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  height: 64px;
  border-bottom: 1px solid rgba(66, 245, 255, 0.14);
  background: rgba(8, 15, 28, 0.72);
  backdrop-filter: blur(16px);
}
.header-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  color: var(--rp-text);
  font-weight: 700;
}
.header-title small {
  color: var(--rp-muted);
  font-size: 12px;
  font-weight: 500;
}
.admin-profile {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--rp-muted);
}
.signal-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--rp-success);
  box-shadow: 0 0 14px var(--rp-success);
}
.el-main {
  min-width: 0;
  padding: 20px;
  background:
    linear-gradient(rgba(66, 245, 255, 0.025) 1px, transparent 1px),
    linear-gradient(90deg, rgba(66, 245, 255, 0.022) 1px, transparent 1px);
  background-size: 36px 36px;
}
</style>
