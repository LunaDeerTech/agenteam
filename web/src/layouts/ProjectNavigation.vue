<script setup lang="ts">
import { computed } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { UiIcon } from "../components/ui";
import { projectNavigation } from "../router/routes";
const route = useRoute();
const identity = computed(() => ({
  owner: String(route.params.owner),
  project: String(route.params.project),
}));
const active = (name: string) =>
  name === "project-settings-general"
    ? route.meta.settingsScope === "project"
    : name === "project-tasks"
      ? ["project-tasks", "project-task-detail", "project-sprint"].includes(
          String(route.name),
        )
      : route.name === name;
</script>

<template>
  <header class="project-navigation">
    <div class="project-identity">
      <RouterLink to="/projects" class="project-back" aria-label="返回项目列表"
        ><UiIcon name="arrow-left" :size="14"
      /></RouterLink>
      <span class="project-owner">{{ identity.owner }}</span
      ><span aria-hidden="true" class="project-separator">/</span>
      <RouterLink
        :to="{ name: 'project-home', params: identity }"
        class="project-name"
        >{{ identity.project }}</RouterLink
      >
    </div>
    <nav aria-label="项目导航">
      <RouterLink
        v-for="item in projectNavigation"
        :key="item.name"
        :to="{ name: item.name, params: identity }"
        :aria-current="active(item.name) ? 'page' : undefined"
        >{{ item.title }}</RouterLink
      >
    </nav>
  </header>
</template>

<style scoped>
.project-navigation {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  flex-shrink: 0;
  gap: 18px;
  min-height: 48px;
  padding: 0 24px;
  border-bottom: 1px solid var(--border-subtle);
}
.project-identity {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
  font-size: 12px;
}
.project-back {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 28px;
  border-radius: 4px;
  color: var(--text-secondary);
}
.project-owner,
.project-separator {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.project-name {
  color: var(--text-primary);
  font-weight: 550;
  text-decoration: none;
  overflow-wrap: anywhere;
}
nav {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 22px;
}
nav a {
  display: inline-flex;
  align-items: center;
  min-height: 47px;
  padding: 0 1px;
  color: var(--text-secondary);
  font-size: 12px;
  text-decoration: none;
  border-bottom: 1px solid transparent;
}
nav a[aria-current="page"] {
  border-bottom-color: var(--text-primary);
  color: var(--text-primary);
}
@media (max-width: 760px) {
  .project-navigation {
    display: block;
    padding: 0 18px;
  }
  .project-identity {
    min-height: 46px;
  }
  nav {
    gap: 18px;
    flex-wrap: nowrap;
    overflow-x: auto;
  }
  nav a {
    min-height: 44px;
    flex: none;
  }
}
</style>
