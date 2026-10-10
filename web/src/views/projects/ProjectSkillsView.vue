<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router'
import { UiBadge, UiButton, UiCard, UiState } from '../../components/ui'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { useSkillOwner } from '../../composables/useSkillOwner'
import { projectRoute } from '../../router/auth'

const route = useRoute(), workspace = useProjectWorkspace()
const location = computed(() => {
  const address = projectRoute(route.fullPath)
  return {
    projectPath: address ? `/${address.username}/${address.project_name}` : '',
    skillID: typeof route.params.skill_id === 'string' ? route.params.skill_id : null,
  }
})
const owner = useSkillOwner(undefined, workspace, location), state = owner.state
const heading = ref<HTMLElement | null>(null)
const directoryPath = computed(() => workspace.paths.value.home + '/settings/skills')
watch(() => [location.value.skillID, state.phase] as const, async () => {
  await nextTick()
  if (owner.visible.value && state.phase === 'ready') heading.value?.focus()
})
onBeforeRouteLeave(() => owner.cancel())
onBeforeRouteUpdate((to, from) => { if (to.fullPath !== from.fullPath) owner.cancel() })
onBeforeUnmount(() => owner.dispose())
</script>

<template>
  <section class="skills-page" aria-label="项目技能库">
    <header class="skills-header">
      <h1 ref="heading" tabindex="-1">{{ location.skillID ? '技能详情' : '项目技能库' }}</h1>
      <div class="skills-actions">
        <RouterLink v-if="location.skillID" :to="directoryPath">返回技能库</RouterLink>
        <UiButton :disabled="owner.blocked.value" @click="owner.refresh()">重新读取</UiButton>
        <UiButton v-if="owner.reading.value" variant="ghost" @click="owner.cancel()">停止读取</UiButton>
      </div>
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认技能库访问身份" />
    <UiState v-else-if="state.phase === 'waiting' || state.phase === 'loading'" kind="loading" title="正在读取技能信息" />
    <UiState v-else-if="state.phase === 'error' || state.phase === 'unavailable'" kind="error"
      :title="state.phase === 'unavailable' ? '技能不可用' : '技能读取未完成'" :description="state.message" />
    <template v-else-if="!location.skillID">
      <ul class="skills-list" aria-label="技能目录">
        <li v-for="skill in state.items" :key="skill.id">
          <UiCard>
            <div class="skill-title">
              <h2><RouterLink :to="`${directoryPath}/${skill.id}`">{{ skill.name }}</RouterLink></h2>
              <UiBadge v-if="skill.protected">受保护</UiBadge>
            </div>
            <p class="skill-description">{{ skill.description }}</p>
            <p class="skill-revision">当前修订 {{ skill.current_revision }}</p>
          </UiCard>
        </li>
      </ul>
    </template>
    <UiCard v-else-if="state.detail">
      <div class="skill-title">
        <h2>{{ state.detail.name }}</h2>
        <UiBadge v-if="state.detail.protected">受保护</UiBadge>
      </div>
      <p class="skill-description">{{ state.detail.description }}</p>
      <dl class="skill-facts">
        <div><dt>当前修订</dt><dd>{{ state.detail.current_revision }}</dd></div>
        <div><dt>记录版本</dt><dd>{{ state.detail.version }}</dd></div>
        <div><dt>标识名</dt><dd>{{ state.detail.normalized_name }}</dd></div>
        <div><dt>技能 ID</dt><dd>{{ state.detail.id }}</dd></div>
      </dl>
    </UiCard>
  </section>
</template>

<style scoped>
.skills-page { min-width: 0; display: grid; gap: var(--content-gap); }
.skills-header, .skills-actions, .skill-title { display: flex; align-items: center; flex-wrap: wrap; gap: var(--content-gap); }
.skills-header { justify-content: space-between; }
.skills-list { display: grid; gap: var(--content-gap); padding: 0; margin: 0; list-style: none; }
.skill-title h2 { min-width: 0; overflow-wrap: anywhere; }
.skill-description { white-space: pre-wrap; overflow-wrap: anywhere; margin-block: var(--content-gap); }
.skill-revision, dt { color: var(--text-secondary); }
.skill-facts { display: grid; gap: var(--content-gap); margin: var(--content-gap) 0 0; }
.skill-facts > div { display: grid; grid-template-columns: minmax(6rem, 0.25fr) minmax(0, 1fr); gap: var(--content-gap); }
dd { margin: 0; overflow-wrap: anywhere; }
@media (max-width: 480px) { .skill-facts > div { grid-template-columns: minmax(0, 1fr); gap: 4px; } }
</style>
