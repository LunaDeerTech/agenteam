<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import SettingsShell from '../../components/layout/SettingsShell.vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiState from '../../components/ui/UiState.vue'
import { useSession } from '../../composables/useSession'
const auth = useSession()
const allowed = computed(
  () =>
    auth.state.phase === 'authenticated' &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied,
)
const groups = [
  {
    key: 'users-invitations',
    label: '用户与邀请',
    children: [
      { label: '用户', path: '/system/users' },
      { label: '待注册邀请', path: '/system/invitations' },
    ],
  },
  {
    key: 'models-providers',
    label: '模型与提供商',
    children: [
      { label: 'Providers', path: '/system/providers' },
      { label: 'Models', path: '/system/models' },
      { label: '平台模型用途', path: '/system/model-selection' },
    ],
  },
]
const heading = ref<HTMLElement | null>(null)
watch(
  allowed,
  async (value) => {
    if (!value) {
      await nextTick()
      heading.value?.focus()
    }
  },
  { immediate: true },
)
</script>
<template>
  <SettingsShell v-if="allowed" title="系统设置" :groups="groups" :show-logout="false">
    <RouterView />
  </SettingsShell>
  <section v-else class="system-denied">
    <h1 ref="heading" tabindex="-1">无权访问系统设置</h1>
    <UiState kind="error" title="需要系统管理员权限" description="当前身份无法访问系统设置。">
      <div class="ui-row">
        <RouterLink to="/">返回首页</RouterLink>
        <UiButton :disabled="auth.state.busy" @click="auth.restore">重新检查权限</UiButton>
      </div>
    </UiState>
  </section>
</template>
<style scoped>
.system-denied {
  display: grid;
  gap: 20px;
  padding: var(--space);
  min-width: 0;
}
</style>
