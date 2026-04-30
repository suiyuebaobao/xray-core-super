// Alova 请求实例封装。
//
// 功能：
// - 创建 Alova 实例，设置 baseURL 和超时
// - 请求拦截器：自动注入 Access Token
// - Token 过期时自动刷新（通过统一 Alova 实例调用 /api/auth/refresh）
// - 刷新失败才清除登录状态
//
// 使用方式：
//   import { httpGet, httpPost } from '@/api/request'
//   const res = await httpGet('/api/user/me')
//   const { data } = await res.data
//     — data 是后端的 JSON 响应：{ success, message, code, data }

import { createAlova } from 'alova'
import adapterFetch from 'alova/fetch'
import { useUserStore } from '@/stores/user'

let isRefreshing = false
let refreshPromise = null

const CSRF_HEADER_NAME = 'X-CSRF-Token'
const CSRF_HEADER_VALUE = 'suiyue-web'
const UNSAFE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

// 创建 Alova 实例
const alovaInstance = createAlova({
  baseURL: '', // 同域，Vite proxy 处理转发
  timeout: 15000,
  requestAdapter: adapterFetch(),
  beforeRequest(method) {
    // 注入 Access Token 与浏览器写操作 CSRF Header
    const userStore = useUserStore()
    const headers = {
      ...method.config.headers,
    }
    if (userStore.accessToken) {
      headers.Authorization = `Bearer ${userStore.accessToken}`
    }
    if (UNSAFE_METHODS.has(method.type)) {
      headers[CSRF_HEADER_NAME] = CSRF_HEADER_VALUE
    }
    method.config.headers = headers
  },
  responded: {
    // 成功响应
    onSuccess(response, method) {
      return response.json().then(async (data) => {
        if (!data.success) {
          // Token 无效或过期时尝试刷新
          if (!method.meta?.skipAuthRefresh && (data.code === 40101 || data.code === 40102 || data.code === 40103)) {
            const userStore = useUserStore()
            if (userStore.accessToken) {
              // 尝试刷新 Token
              const refreshed = await tryRefreshToken()
              if (refreshed) {
                return method.send(true)
              } else {
                userStore.logout()
              }
            } else {
              userStore.logout()
            }
          }
          const error = new Error(data.message || '请求失败')
          error.code = data.code
          throw error
        }
        return data
      })
    },
    // 失败响应
    onError(error) {
      // HTTP 401 也尝试刷新
      if (error.response && error.response.status === 401) {
        const userStore = useUserStore()
        if (userStore.accessToken) {
          tryRefreshToken()
        } else {
          userStore.logout()
        }
      }
      console.error('[api] request error:', error)
      throw error
    },
  },
})

// 尝试刷新 Token
async function tryRefreshToken() {
  if (isRefreshing) {
    return refreshPromise
  }

  isRefreshing = true
  refreshPromise = doRefreshToken()

  try {
    return await refreshPromise
  } finally {
    isRefreshing = false
    refreshPromise = null
  }
}

async function doRefreshToken() {
  try {
    const data = await alovaInstance.Post('/api/auth/refresh', {}, {
      meta: { skipAuthRefresh: true },
    }).send(true)
    if (data.success && data.data && data.data.accessToken) {
      const userStore = useUserStore()
      // 更新内存中的 Token
      userStore.accessToken = data.data.accessToken
      return true
    }
    return false
  } catch {
    return false
  }
}

// 封装常用请求方法
export function httpGet(url, params = {}) {
  return alovaInstance.Get(url, { params })
}

export function httpPost(url, data = {}, config = {}) {
  return alovaInstance.Post(url, data, config)
}

export function httpPut(url, data = {}, config = {}) {
  return alovaInstance.Put(url, data, config)
}

export function httpDelete(url, config = {}) {
  return alovaInstance.Delete(url, config)
}

export default alovaInstance
