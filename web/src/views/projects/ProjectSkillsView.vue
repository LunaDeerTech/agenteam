<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { UiBadge, UiButton, UiCard, UiState } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import SkillImportForm from '../../components/projects/SkillImportForm.vue'
import { useSkillImport } from '../../composables/useSkillImport'
import { useSkillOwner } from '../../composables/useSkillOwner'
import { projectRoute, skillsNavigationReadReady } from '../../router/auth'

const route = useRoute(),
  workspace = useProjectWorkspace(),
  navigationReady = skillsNavigationReadReady(useRouter())
const location = computed(() => {
  const address = projectRoute(route.fullPath)
  return {
    projectPath:
      navigationReady.value && address ? `/${address.username}/${address.project_name}` : '',
    skillID:
      typeof route.params.skill_id === 'string' && route.params.skill_id !== ''
        ? route.params.skill_id
        : null,
  }
})
const owner = useSkillOwner(undefined, workspace, location),
  state = owner.state
const importer = useSkillImport(undefined, workspace, () => owner.refresh())
const importButton = ref<{ $el: HTMLButtonElement } | null>(null)
const importPanel = ref<HTMLElement | null>(null)
async function showImport() {
  importer.show()
  await nextTick()
  if (importer.open.value) importPanel.value?.querySelector<HTMLElement>('input, h2')?.focus()
}
async function closeImport() {
  importer.close()
  await nextTick()
  const button = importButton.value?.$el
  if (!importer.open.value && button?.isConnected && !button.disabled) button.focus()
}
const heading = ref<HTMLElement | null>(null)
const moreButton = ref<{ $el: HTMLButtonElement } | null>(null)
const directoryPath = computed(() => workspace.paths.value.home + '/settings/skills')
watch(
  () => [location.value.skillID, state.phase, state.loadingMore] as const,
  async (_next, previous) => {
    await nextTick()
    if (
      owner.visible.value &&
      state.phase === 'ready' &&
      !state.loadingMore &&
      !importer.open.value
    ) {
      if (previous?.[2] && state.nextCursor) moreButton.value?.$el.focus()
      else heading.value?.focus()
    }
  },
)
onBeforeRouteLeave(() => owner.cancel())
onBeforeRouteUpdate((to, from) => {
  if (to.fullPath !== from.fullPath) owner.cancel()
})
onBeforeUnmount(() => {
  importer.dispose()
  owner.dispose()
})
</script>

<template>
  <section class="skills-page" aria-label="项目技能库">
    <header class="skills-header">
      <h1 ref="heading" tabindex="-1">{{ location.skillID ? '技能详情' : '项目技能库' }}</h1>
      <div class="skills-actions">
        <RouterLink v-if="location.skillID" :to="directoryPath">返回技能库</RouterLink>
        <UiButton
          v-if="!location.skillID"
          ref="importButton"
          :disabled="owner.blocked.value"
          @click="showImport"
          >导入技能包</UiButton
        >
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">重新读取</UiButton>
        <UiButton v-if="owner.reading.value" variant="ghost" @click="owner.cancel()"
          >停止读取</UiButton
        >
      </div>
    </header>
    <div v-if="!location.skillID && owner.visible.value && importer.open.value" ref="importPanel">
      <SkillImportForm :owner="importer" @close="closeImport" />
    </div>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认技能库访问身份" />
    <UiState
      v-else-if="state.phase === 'waiting' || state.phase === 'loading'"
      kind="loading"
      title="正在读取技能信息"
    />
    <UiState
      v-else-if="state.phase === 'error' || state.phase === 'unavailable'"
      kind="error"
      :title="state.phase === 'unavailable' ? '技能不可用' : '技能读取未完成'"
      :description="state.message"
    />
    <template v-else-if="!location.skillID">
      <p v-if="state.items.length === 0">项目技能目录暂无条目。</p>
      <ul class="skills-list" aria-label="技能目录" :aria-busy="state.loadingMore">
        <li v-for="skill in state.items" :key="skill.id">
          <UiCard>
            <div class="skill-title">
              <h2>
                <RouterLink :to="`${directoryPath}/${skill.id}`">{{ skill.name }}</RouterLink>
              </h2>
              <UiBadge v-if="skill.protected">受保护</UiBadge>
            </div>
            <p class="skill-description">{{ skill.description }}</p>
            <p class="skill-revision">当前修订 {{ skill.current_revision }}</p>
          </UiCard>
        </li>
      </ul>
      <UiButton
        ref="moreButton"
        v-if="state.nextCursor"
        :disabled="owner.blocked.value"
        @click="owner.loadMore()"
        >{{ state.loadingMore ? '正在加载技能' : '加载更多技能' }}</UiButton
      >
    </template>
    <UiCard v-else-if="state.detail">
      <div class="skill-title">
        <h2>{{ state.detail.name }}</h2>
        <UiBadge v-if="state.detail.protected">受保护</UiBadge>
      </div>
      <p class="skill-description">{{ state.detail.description }}</p>
      <div class="skills-actions">
        <UiButton :disabled="owner.blocked.value" @click="owner.downloadPackage()">
          {{ owner.download.phase === 'loading' ? '正在下载技能包' : '下载当前修订' }}
        </UiButton>
        <UiButton
          v-if="owner.download.phase === 'loading'"
          variant="ghost"
          @click="owner.cancelDownload()"
          >取消下载</UiButton
        >
      </div>
      <div v-if="owner.download.phase === 'loading'" class="skill-download" role="status">
        <progress
          :value="owner.download.received"
          :max="owner.download.total || 1"
          aria-label="技能包下载进度"
        />
        <span>{{
          owner.download.total
            ? `${Math.floor((owner.download.received / owner.download.total) * 100)}%`
            : '正在读取技能包'
        }}</span>
      </div>
      <p
        v-if="owner.download.message"
        :role="owner.download.phase === 'error' ? 'alert' : 'status'"
      >
        {{ owner.download.message }}
      </p>
      <dl class="skill-facts">
        <div>
          <dt>当前修订</dt>
          <dd>{{ state.detail.current_revision }}</dd>
        </div>
        <div>
          <dt>记录版本</dt>
          <dd>{{ state.detail.version }}</dd>
        </div>
        <div>
          <dt>标识名</dt>
          <dd>{{ state.detail.normalized_name }}</dd>
        </div>
        <div>
          <dt>项目 ID</dt>
          <dd>{{ state.detail.project_id }}</dd>
        </div>
        <div>
          <dt>保护状态</dt>
          <dd>{{ state.detail.protected ? '受保护' : '普通技能' }}</dd>
        </div>
        <div>
          <dt>技能 ID</dt>
          <dd>{{ state.detail.id }}</dd>
        </div>
      </dl>
    </UiCard>
  </section>
</template>

<style scoped>
.skills-page {
  min-width: 0;
  display: grid;
  gap: var(--content-gap);
}
.skills-header,
.skills-actions,
.skill-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.skills-header {
  justify-content: space-between;
}
.skills-list {
  display: grid;
  gap: var(--content-gap);
  padding: 0;
  margin: 0;
  list-style: none;
}
.skill-title h2 {
  min-width: 0;
  overflow-wrap: anywhere;
}
.skill-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin-block: var(--content-gap);
}
.skill-revision,
dt {
  color: var(--muted);
}
.skill-download {
  display: flex;
  gap: var(--content-gap);
  align-items: center;
  margin-block: var(--content-gap);
}
.skill-download progress {
  max-width: 100%;
  min-width: 0;
}
.skill-facts {
  display: grid;
  gap: var(--content-gap);
  margin: var(--content-gap) 0 0;
}
.skill-facts > div {
  display: grid;
  grid-template-columns: minmax(6rem, 0.25fr) minmax(0, 1fr);
  gap: var(--content-gap);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
  .skill-facts > div {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
}
</style>
