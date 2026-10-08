<script setup lang="ts">
import { useRoute } from 'vue-router'

defineProps<{ name: string; home: string; settings: string }>()
const route = useRoute()
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
    <RouterLink :to="settings" :aria-current="route.path === settings ? 'page' : undefined"
      >项目设置</RouterLink
    >
  </nav>
</template>

<style scoped>
.project-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  min-height: var(--nav-height);
  padding: 8px var(--space);
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
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
@media (prefers-reduced-motion: reduce) {
  .project-nav a {
    transition: none;
  }
}
</style>
