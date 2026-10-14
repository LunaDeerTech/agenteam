<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSession } from '../composables/useSession'
const state = useSession().state
const heading = ref<HTMLElement | null>(null)
onMounted(() => heading.value?.focus())
</script>
<template>
  <div class="home-view page-layout page-layout--collection">
    <header class="page-header">
      <div class="page-heading"><h1 ref="heading" tabindex="-1">首页</h1></div>
    </header>
    <p v-if="state.user?.initial_password_suggestion" class="password-notice">
      你正在使用初始密码。建议之后更换为自己的密码。
      <RouterLink to="/settings/password">修改密码</RouterLink>
    </p>
    <section aria-label="Dashboard" class="dashboard" />
  </div>
</template>
<style scoped>
.home-view {
  min-width: 0;
}
.password-notice {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  margin-bottom: 24px;
}
.password-notice a {
  margin-left: 8px;
}
.dashboard {
  min-height: 200px;
}
</style>
