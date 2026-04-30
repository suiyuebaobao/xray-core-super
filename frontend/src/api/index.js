import { httpGet, httpPost, httpPut, httpDelete } from './request'

const compact = (params) => Object.fromEntries(
  Object.entries(params).filter(([, value]) => value !== undefined && value !== '')
)
const pageParams = ({ page = 1, size = 20, ...rest } = {}) => compact({ page, size, ...rest })

export const authApi = {
  register: (payload) => httpPost('/api/auth/register', payload),
  login: (payload) => httpPost('/api/auth/login', payload),
  refresh: () => httpPost('/api/auth/refresh', {}, { meta: { skipAuthRefresh: true } }),
  logout: () => httpPost('/api/auth/logout'),
}

export const userApi = {
  me: () => httpGet('/api/user/me'),
  subscription: () => httpGet('/api/user/subscription'),
  usage: (params) => httpGet('/api/user/usage', compact(params || {})),
  orders: (params) => httpGet('/api/user/orders', pageParams(params)),
  updateProfile: (payload) => httpPut('/api/user/profile', payload),
  changePassword: (payload) => httpPut('/api/user/password', payload),
}

export const planApi = {
  listActive: () => httpGet('/api/plans'),
}

export const redeemApi = {
  redeem: (payload) => httpPost('/api/redeem', payload),
}

export const adminApi = {
  dashboard: async () => {
    const res = await httpGet('/api/admin/dashboard/stats')
    return {
      userCount: res.data?.user_count ?? 0,
      nodeCount: res.data?.node_count ?? 0,
      planCount: res.data?.plan_count ?? 0,
      activeSubCount: res.data?.active_sub_count ?? 0,
    }
  },
  plans: {
    list: () => httpGet('/api/admin/plans'),
    create: (payload) => httpPost('/api/admin/plans', payload),
    update: (id, payload) => httpPut(`/api/admin/plans/${id}`, payload),
    delete: (id) => httpDelete(`/api/admin/plans/${id}`),
    bindNodeGroups: (id, nodeGroupIds) => httpPost(`/api/admin/plans/${id}/node-groups`, { node_group_ids: nodeGroupIds }),
    nodeGroups: (id) => httpGet(`/api/admin/plans/${id}/node-groups`),
  },
  nodeGroups: {
    list: () => httpGet('/api/admin/node-groups'),
    create: (payload) => httpPost('/api/admin/node-groups', payload),
    update: (id, payload) => httpPut(`/api/admin/node-groups/${id}`, payload),
    delete: (id) => httpDelete(`/api/admin/node-groups/${id}`),
    nodes: (id) => httpGet(`/api/admin/node-groups/${id}/nodes`),
    bindNodes: (id, nodeIds) => httpPut(`/api/admin/node-groups/${id}/nodes`, { node_ids: nodeIds }),
  },
  nodes: {
    list: () => httpGet('/api/admin/nodes'),
    create: (payload) => httpPost('/api/admin/nodes', payload),
    update: (id, payload) => httpPut(`/api/admin/nodes/${id}`, payload),
    delete: (id) => httpDelete(`/api/admin/nodes/${id}`),
    deploy: (payload) => httpPost('/api/admin/nodes/deploy', payload),
  },
  relays: {
    list: () => httpGet('/api/admin/relays'),
    create: (payload) => httpPost('/api/admin/relays', payload),
    update: (id, payload) => httpPut(`/api/admin/relays/${id}`, payload),
    delete: (id) => httpDelete(`/api/admin/relays/${id}`),
    deploy: (payload) => httpPost('/api/admin/relays/deploy', payload),
    backends: (id) => httpGet(`/api/admin/relays/${id}/backends`),
    bindBackends: (id, backends) => httpPut(`/api/admin/relays/${id}/backends`, { backends }),
  },
  users: {
    list: ({ keyword = '', ...params } = {}) => httpGet('/api/admin/users', pageParams({ ...params, keyword: keyword || undefined })),
    create: (payload) => httpPost('/api/admin/users', payload),
    updateStatus: (id, status) => httpPut(`/api/admin/users/${id}/status`, { status }),
    resetPassword: (id, newPassword) => httpPut(`/api/admin/users/${id}/password`, { new_password: newPassword }),
    subscription: (id) => httpGet(`/api/admin/users/${id}/subscription`),
    upsertSubscription: (id, payload) => httpPut(`/api/admin/users/${id}/subscription`, payload),
    usage: (id, params) => httpGet(`/api/admin/users/${id}/usage`, compact(params || {})),
  },
  orders: {
    list: (params) => httpGet('/api/admin/orders', pageParams(params)),
  },
  redeemCodes: {
    list: (params) => httpGet('/api/admin/redeem-codes', pageParams(params)),
    generate: (payload) => httpPost('/api/admin/redeem-codes', payload),
  },
  subscriptionTokens: {
    list: (params) => httpGet('/api/admin/subscription-tokens', pageParams(params)),
    create: (payload) => httpPost('/api/admin/subscription-tokens', payload),
    revoke: (id) => httpPost(`/api/admin/subscription-tokens/${id}/revoke`),
    reset: (id, payload = {}) => httpPost(`/api/admin/subscription-tokens/${id}/reset`, payload),
  },
}
