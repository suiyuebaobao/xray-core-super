// 前端路由配置。
//
// 路由结构：
// /login      — 用户登录
// /register   — 用户注册
// /           — 用户首页（需登录）
// /subscription — 我的订阅（需登录）
// /orders     — 订单列表（需登录）
// /plans      — 套餐列表
// /redeem     — 兑换码
// /admin/*    — 管理后台（需管理员权限）

import { createRouter, createWebHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { authApi, userApi } from '@/api'

const routes = [
  // 用户侧
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/pages/user/Login.vue'),
    meta: { guest: true },
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('@/pages/user/Register.vue'),
    meta: { guest: true },
  },
  {
    path: '/',
    component: () => import('@/layouts/UserLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'Home', component: () => import('@/pages/user/Home.vue') },
      { path: 'subscription', name: 'Subscription', component: () => import('@/pages/user/Subscription.vue') },
      { path: 'orders', name: 'Orders', component: () => import('@/pages/user/Orders.vue') },
      { path: 'plans', name: 'Plans', component: () => import('@/pages/user/Plans.vue') },
      { path: 'redeem', name: 'Redeem', component: () => import('@/pages/user/Redeem.vue') },
      { path: 'profile', name: 'Profile', component: () => import('@/pages/user/Profile.vue') },
    ],
  },
  // 管理后台
  {
    path: '/admin/login',
    name: 'AdminLogin',
    component: () => import('@/pages/admin/Login.vue'),
    meta: { guest: true },
  },
  {
    path: '/admin',
    component: () => import('@/layouts/AdminLayout.vue'),
    meta: { requiresAuth: true, requiresAdmin: true },
    children: [
      { path: '', name: 'AdminDashboard', component: () => import('@/pages/admin/Dashboard.vue') },
      { path: 'plans', name: 'AdminPlans', component: () => import('@/pages/admin/Plans.vue') },
      { path: 'node-groups', name: 'AdminNodeGroups', component: () => import('@/pages/admin/NodeGroups.vue') },
      { path: 'nodes', name: 'AdminNodes', component: () => import('@/pages/admin/Nodes.vue') },
      { path: 'relays', name: 'AdminRelays', component: () => import('@/pages/admin/Relays.vue') },
      { path: 'users', name: 'AdminUsers', component: () => import('@/pages/admin/Users.vue') },
      { path: 'orders', name: 'AdminOrders', component: () => import('@/pages/admin/Orders.vue') },
      { path: 'redeem-codes', name: 'AdminRedeemCodes', component: () => import('@/pages/admin/RedeemCodes.vue') },
      { path: 'subscription-tokens', name: 'AdminSubscriptionTokens', component: () => import('@/pages/admin/SubscriptionTokens.vue') },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 全局导航守卫
let isRefreshing = false
router.beforeEach(async (to, from, next) => {
  const userStore = useUserStore()

  // 如果内存没有 token 但需要认证，尝试用 refresh cookie 恢复
  if (to.meta.requiresAuth && !userStore.isLoggedIn && !isRefreshing) {
    isRefreshing = true
    try {
      const res = await authApi.refresh()
      if (res.data && res.data.accessToken) {
        userStore.accessToken = res.data.accessToken
        // 同时获取用户信息
        try {
          const meRes = await userApi.me()
          if (meRes.data && meRes.data.user) {
            userStore.user = meRes.data.user
          }
        } catch {}
      }
    } catch {
      // 无有效 refresh token，确实未登录
    } finally {
      isRefreshing = false
    }
  }

  // 已登录用户访问登录/注册页时，跳转到首页
  if (to.meta.guest && userStore.isLoggedIn) {
    next({ name: 'Home' })
    return
  }

  // 需要登录的页面
  if (to.meta.requiresAuth && !userStore.isLoggedIn) {
    // 判断是否管理后台
    if (to.path.startsWith('/admin')) {
      next({ name: 'AdminLogin', query: { redirect: to.fullPath } })
    } else {
      next({ name: 'Login', query: { redirect: to.fullPath } })
    }
    return
  }

  // 需要管理员权限
  if (to.meta.requiresAdmin && !userStore.isAdmin) {
    next({ name: 'Home' })
    return
  }

  next()
})

export default router
