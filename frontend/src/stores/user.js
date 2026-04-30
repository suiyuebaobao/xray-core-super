// 用户状态管理（Pinia）。
//
// 管理：
// - 用户登录状态
// - Access Token（内存存储，不持久化到 localStorage）
// - 用户基础信息
// - 管理员标识
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useUserStore = defineStore('user', () => {
  // 状态
  const accessToken = ref('')
  const user = ref(null)

  // 计算属性
  const isLoggedIn = computed(() => !!accessToken.value)
  const isAdmin = computed(() => user.value?.is_admin === true)

  // 设置登录信息
  function setLogin(token, userData) {
    accessToken.value = token
    user.value = userData
  }

  // 清除登录信息
  function logout() {
    accessToken.value = ''
    user.value = null
  }

  // 更新用户信息
  function updateUser(userData) {
    user.value = { ...user.value, ...userData }
  }

  return {
    accessToken,
    user,
    isLoggedIn,
    isAdmin,
    setLogin,
    logout,
    updateUser,
  }
})
