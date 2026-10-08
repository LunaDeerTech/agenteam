<script setup lang="ts">
import { computed } from 'vue'
import SettingsShell from '../../components/layout/SettingsShell.vue'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const { visible, detail, paths } = useProjectWorkspace()
const groups = computed(() =>
  paths.value.settings ? [{ label: '项目资料', path: paths.value.settings, leaf: '基本信息' }] : [],
)
</script>

<template>
  <SettingsShell
    v-if="visible && detail.project"
    title="项目设置"
    :groups="groups"
    :show-logout="false"
  >
    <RouterView />
  </SettingsShell>
  <div v-else-if="visible" class="project-settings-recovery"><RouterView /></div>
</template>

<style scoped>
.project-settings-recovery {
  min-width: 0;
  padding: var(--space);
}
</style>
