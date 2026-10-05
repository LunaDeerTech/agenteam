<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSession } from '../composables/useSession'
const state = useSession().state
const heading = ref<HTMLElement | null>(null)
onMounted(() => heading.value?.focus())
</script>
<template>
  <div class="home-view">
    <h1 ref="heading" tabindex="-1">首页</h1>
    <p v-if="state.user?.initial_password_suggestion" class="meta">
      你正在使用初始密码。建议之后更换为自己的密码。
      <RouterLink to="/settings/password">修改密码</RouterLink>
    </p>
    <section aria-label="Dashboard" class="dashboard" />
  </div>
</template>
<style scoped>
.home-view {
  padding: var(--space);
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.dashboard {
  min-height: 200px;
}
</style>
