<script setup lang="ts">
import { computed } from 'vue'
import SettingsShell from '../../components/layout/SettingsShell.vue'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const { visible, detail, paths } = useProjectWorkspace()
const groups = computed(() =>
  paths.value.settings
    ? [
        { label: '项目资料', path: paths.value.settings, leaf: '基本信息' },
        {
          key: 'project-skills',
          label: 'Skills',
          children: [{ path: paths.value.home + '/settings/skills', label: '项目技能库' }],
        },
        { label: '安全记录', path: paths.value.home + '/settings/audit', leaf: '项目审计' },
        {
          key: 'project-models-providers',
          label: '模型与 Provider',
          children: [
            { path: paths.value.home + '/settings/model-providers', label: 'Providers' },
            { path: paths.value.home + '/settings/available-models', label: '可用模型' },
          ],
        },
      ]
    : [],
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
