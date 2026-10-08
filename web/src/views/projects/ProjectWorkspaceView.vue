<script setup lang="ts">
import ProjectNav from '../../components/layout/ProjectNav.vue'
import { UiButton, UiSkeleton, UiState } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const s = useProjectWorkspace()
const { visible, detail, paths, blocked, progress } = s
</script>

<template>
  <div class="project-workspace">
    <UiState
      v-if="!visible || detail.phase === 'checking'"
      class="workspace-state"
      kind="loading"
      title="正在确认项目访问身份"
    />
    <template v-else>
      <ProjectNav
        v-if="detail.project"
        :name="detail.project.name"
        :home="paths.home"
        :settings="paths.settings"
      />
      <div
        v-else-if="detail.phase === 'loading' || detail.phase === 'inactive'"
        class="project-nav-loading"
        aria-label="正在读取项目名称"
      >
        <UiSkeleton :lines="1" />
      </div>
      <UiState
        v-if="detail.phase === 'loading' || detail.phase === 'inactive'"
        class="workspace-state"
        kind="loading"
        title="正在读取项目"
      />
      <UiState
        v-else-if="detail.phase === 'read-error' || detail.phase === 'unavailable'"
        class="workspace-state"
        kind="error"
        :title="detail.phase === 'unavailable' ? '项目不可用' : '项目信息读取失败'"
        :description="detail.message"
      >
        <div class="workspace-actions">
          <UiButton :disabled="blocked" @click="s.readCurrent()">重新读取项目</UiButton>
          <RouterLink to="/projects">返回项目列表</RouterLink>
        </div>
      </UiState>
      <p v-if="detail.project && detail.phase !== 'current'" class="workspace-notice" role="status">
        以下为上次完整读取的项目信息，当前读取尚未成功。
      </p>
      <p v-if="detail.project?.lifecycle === 'archiving'" class="workspace-notice" role="status">
        项目正在归档，当前内容只读。
      </p>
      <p
        v-else-if="detail.project?.lifecycle === 'archived'"
        class="workspace-notice"
        role="status"
      >
        项目已归档，当前内容只读。
      </p>
      <RouterView v-if="detail.project || progress" />
    </template>
  </div>
</template>

<style scoped>
.project-workspace {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 100%;
}
.project-nav-loading {
  width: min(100%, 360px);
  min-height: var(--nav-height);
  padding: 12px var(--space);
}
.workspace-state {
  margin: var(--space);
}
.workspace-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.workspace-notice {
  padding: var(--content-gap) var(--space) 0;
  overflow-wrap: anywhere;
}
</style>
