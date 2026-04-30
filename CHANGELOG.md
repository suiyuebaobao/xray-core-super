# 更新日志

## 2026-04-30

- 修复订阅 Token 页面、管理员用户订阅弹窗、用户首页和用户订阅页只能使用 Clash 链接的问题，支持在 Clash、Base64、URI 三种订阅格式间切换。
- 修复出口节点流量统计未入账的问题：node-agent 默认启用 `XRAY_API_SERVER=127.0.0.1:10085`，启动时自动补齐 Xray Stats API 配置，并兼容 statsquery 返回的数字或字符串计数。
- 一键部署出口节点时自动传入 Xray Stats API 地址，确保新部署节点能上报用户级流量。
- 加固 node-agent 镜像构建流程，下载 Xray release 时启用失败检测、重试和 zip 校验。
- 增加 node-agent Stats API 配置单元测试，并在 Playwright smoke 中覆盖订阅格式切换。

注意：已有出口节点需要更新 node-agent 镜像或二进制后，流量统计才会从节点侧开始上报。
