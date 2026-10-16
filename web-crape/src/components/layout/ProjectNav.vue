<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { projectRoute } from '../../router/auth'

const props = defineProps<{ name: string; home: string; settings: string }>()
const route = useRoute()
const current = computed(() => projectRoute(route.fullPath))
const target = computed(() => projectRoute(props.home))
function sectionCurrent(suffix: string) {
  return (
    !!current.value &&
    !!target.value &&
    current.value.username === target.value.username &&
    current.value.project_name === target.value.project_name &&
    (current.value.suffix === suffix || current.value.suffix.startsWith(suffix + '/'))
  )
}
</script>

<template>
  <nav class="project-nav" aria-label="项目导航">
    <RouterLink
      class="project-name"
      :to="home"
      :title="name"
      :aria-current="route.path === home ? 'page' : undefined"
      >{{ name }}</RouterLink
    >
    <RouterLink :to="home + '/tasks'" :aria-current="sectionCurrent('/tasks') ? 'page' : undefined"
      >任务</RouterLink
    >
    <RouterLink
      :to="home + '/meetings'"
      :aria-current="sectionCurrent('/meetings') ? 'page' : undefined"
      >会议</RouterLink
    >
    <RouterLink
      :to="home + '/knowledge'"
      :aria-current="sectionCurrent('/knowledge') ? 'page' : undefined"
      >知识库</RouterLink
    >
    <RouterLink
      :to="home + '/artifacts'"
      :aria-current="sectionCurrent('/artifacts') ? 'page' : undefined"
      >产物</RouterLink
    >
    <RouterLink :to="home + '/inbox'" :aria-current="sectionCurrent('/inbox') ? 'page' : undefined"
      >收件箱</RouterLink
    >
    <RouterLink :to="settings" :aria-current="sectionCurrent('/settings') ? 'page' : undefined"
      >项目设置</RouterLink
    >
  </nav>
</template>

<style scoped>
.project-nav {
  display: flex;
  flex: none;
  align-items: center;
  gap: 8px;
  min-width: 0;
  min-height: var(--nav-height);
  padding: 8px var(--space);
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  background: var(--canvas);
}
.project-nav a {
  flex: 0 0 auto;
  padding: 4px 10px;
  border-radius: var(--radius);
  color: var(--muted);
  white-space: nowrap;
  transition:
    color var(--motion) var(--ease),
    background-color var(--motion) var(--ease);
}
.project-nav a:hover {
  color: var(--text);
  background: var(--surface-alt);
  text-decoration: none;
}
.project-nav a[aria-current='page'] {
  color: var(--text);
  background: var(--nav-selected);
  font-weight: 600;
}
.project-nav .project-name {
  max-width: min(52vw, 360px);
  overflow: hidden;
  text-overflow: ellipsis;
}
@media (max-width: 650px) {
  .project-nav {
    padding: 4px 12px;
    gap: 4px;
  }
  .project-nav a {
    min-height: 44px;
    display: inline-flex;
    align-items: center;
  }
}
@media (prefers-reduced-motion: reduce) {
  .project-nav a {
    transition: none;
  }
}
</style>
