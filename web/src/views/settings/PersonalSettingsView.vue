<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import SettingsShell from '../../components/layout/SettingsShell.vue'
import UiButton from '../../components/ui/UiButton.vue'
import { usePersonalSettings, type SettingsSection } from '../../composables/usePersonalSettings'
const settings = usePersonalSettings(),
  route = useRoute()
const heading = ref<HTMLElement | null>(null)
const section = computed(() =>
  route.path.endsWith('/appearance')
    ? 'appearance'
    : route.path.endsWith('/password')
      ? 'password'
      : 'profile',
)
const title = computed(
  () => ({ profile: '基本资料', appearance: '主题', password: '修改密码' })[section.value],
)
let loaded = ''
watch(
  () => [route.path, settings.auth.state.busy] as const,
  ([path, busy]) => {
    if (!busy && settings.auth.state.phase === 'authenticated' && loaded !== path) {
      loaded = path
      void settings.load(section.value as SettingsSection)
    }
  },
  { immediate: true },
)
watch(
  () => route.path,
  async () => {
    await nextTick()
    heading.value?.focus()
  },
  { immediate: true },
)
async function logout() {
  if (await settings.confirmLeave()) await settings.auth.logout()
}
</script>
<template>
  <SettingsShell @logout="logout">
    <div class="settings-page">
      <h1 ref="heading" tabindex="-1">{{ title }}</h1>
      <div v-if="settings.requiresReload.value" role="status">
        <p>已放弃本次页面操作。此前提交仍可能生效，请重新加载当前资料后再编辑。</p>
        <UiButton :disabled="settings.auth.state.busy" @click="settings.load(section)"
          >重新加载当前资料</UiButton
        >
      </div>
      <RouterView />
    </div>
  </SettingsShell>
</template>
<style scoped>
.settings-page {
  display: grid;
  gap: 20px;
  min-width: 0;
}
</style>
