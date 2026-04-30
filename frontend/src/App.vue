<template>
  <router-view />
</template>

<script setup>
// App.vue — 根组件。
// 职责：作为路由出口，不持有业务逻辑。
</script>

<style>
/* 全局重置 — 最小化定制，保持 Element Plus 默认风格 */
html, body {
  margin: 0;
  padding: 0;
  height: 100%;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}
#app {
  height: 100%;
}
</style>
