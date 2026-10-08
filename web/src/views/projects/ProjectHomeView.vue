<script setup lang="ts">
import { UiBadge, UiButton } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'

const s = useProjectWorkspace()
const { visible, detail, ownerName, paths, blocked, writeMessage } = s
const lifecycleLabels = { active: '活跃', archiving: '正在归档', archived: '已归档' } as const
</script>

<template>
  <section v-if="visible" class="project-home" aria-label="项目首页">
    <template v-if="detail.project">
      <div class="project-heading">
        <h1 tabindex="-1">{{ detail.project.name }}</h1>
        <UiBadge
          :tone="
            detail.project.lifecycle === 'active'
              ? 'success'
              : detail.project.lifecycle === 'archiving'
                ? 'warning'
                : 'neutral'
          "
          >{{ lifecycleLabels[detail.project.lifecycle] }}</UiBadge
        >
      </div>
      <p class="project-description">{{ detail.project.description || '未填写描述。' }}</p>
      <dl class="project-facts">
        <div>
          <dt>Owner</dt>
          <dd>{{ ownerName }}</dd>
        </div>
        <div>
          <dt>版本</dt>
          <dd>
            <code>{{ detail.project.version }}</code>
          </dd>
        </div>
        <div>
          <dt>更新时间</dt>
          <dd>
            <time :datetime="detail.project.updated_at">{{ detail.project.updated_at }}</time>
          </dd>
        </div>
      </dl>
      <p><RouterLink :to="paths.settings">查看项目基本信息</RouterLink></p>
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
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
  padding: var(--space);
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
  white-space: pre-wrap;
}
.project-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 32px;
  margin: 0;
}
.project-facts > div {
  min-width: 0;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
}
h1,
p,
dd {
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
