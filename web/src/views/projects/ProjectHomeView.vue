<script setup lang="ts">
import { UiBadge, UiButton } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const s = useProjectWorkspace()
const { visible, detail, paths, blocked, writeMessage } = s
const lifecycleLabels = { active: '活跃', archiving: '正在归档', archived: '已归档' } as const
</script>

<template>
  <section
    v-if="visible"
    class="project-home page-layout page-layout--collection"
    aria-label="项目首页"
  >
    <template v-if="detail.project">
      <header class="page-header">
        <div class="project-heading">
          <h1 tabindex="-1">{{ detail.project.name }}</h1>
          <UiBadge
            :tone="
              detail.project.lifecycle === 'active'
                ? 'neutral'
                : detail.project.lifecycle === 'archiving'
                  ? 'warning'
                  : 'neutral'
            "
            >{{ lifecycleLabels[detail.project.lifecycle] }}</UiBadge
          >
        </div>
        <RouterLink :to="paths.settings">查看项目基本信息</RouterLink>
      </header>
      <p v-if="detail.project.description" class="project-description">
        {{ detail.project.description }}
      </p>
      <section class="dashboard" aria-label="Dashboard" />
    </template>
    <div v-if="writeMessage" class="project-message">
      <p role="status">{{ writeMessage }}</p>
      <UiButton :disabled="blocked" @click="s.readCurrent()">重读当前项目信息</UiButton>
    </div>
  </section>
</template>

<style scoped>
.project-home {
  min-width: 0;
}
.project-heading {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
  min-width: 0;
}
h1 {
  min-width: 0;
}
.project-description {
  max-width: 800px;
  color: var(--muted);
  white-space: pre-wrap;
}
h1,
p {
  overflow-wrap: anywhere;
}
.dashboard {
  min-height: 200px;
}
.project-message {
  display: grid;
  gap: var(--content-gap);
  justify-items: start;
}
</style>
