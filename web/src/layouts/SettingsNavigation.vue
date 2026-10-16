<script setup lang="ts">
import { computed } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { UiIcon } from "../components/ui";
import type { SettingsPage, SettingsScope } from "../router/routes";

const props = defineProps<{
  pages: readonly SettingsPage[];
  scope: SettingsScope;
  label: string;
}>();
const emit = defineEmits<{ navigate: [] }>();
const route = useRoute();
const groups = computed(() =>
  [...new Set(props.pages.map((page) => page.group))].map((title) => ({
    title,
    pages: props.pages.filter((page) => page.group === title),
  })),
);
const destination = (key: string) => ({
  name: `${props.scope}-settings-${key}`,
  params:
    props.scope === "project"
      ? { owner: route.params.owner, project: route.params.project }
      : {},
});
</script>

<template>
  <nav class="settings-navigation" :aria-label="label">
    <section
      v-for="group in groups"
      :key="group.title"
      class="settings-nav-group"
    >
      <h3 v-if="group.title">{{ group.title }}</h3>
      <RouterLink
        v-for="page in group.pages"
        :key="page.key"
        :to="destination(page.key)"
        :aria-current="route.meta.settingKey === page.key ? 'page' : undefined"
        :data-setting-key="page.key"
        @click="emit('navigate')"
      >
        <UiIcon :name="page.icon" :size="15" />
        <span>{{ page.title }}</span>
      </RouterLink>
    </section>
  </nav>
</template>

<style scoped>
.settings-nav-group {
  display: grid;
  gap: 3px;
}
.settings-nav-group + .settings-nav-group {
  margin-top: 24px;
}
h3 {
  margin: 0 10px 8px;
  font-size: 11px;
  font-weight: 450;
  line-height: 1.6;
  color: var(--text-tertiary);
}
a {
  display: flex;
  align-items: center;
  gap: 11px;
  min-height: 33px;
  padding: 7px 11px;
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 12px;
  line-height: 1.5;
  border-radius: 6px;
  transition:
    background-color 120ms ease,
    color 120ms ease;
}
a svg {
  flex: none;
  color: var(--text-tertiary);
}
a span {
  min-width: 0;
  overflow-wrap: anywhere;
}
a:hover {
  background: var(--surface-hover);
  color: var(--text-primary);
}
a[aria-current="page"] {
  background: var(--surface-active);
  color: var(--text-primary);
  font-weight: 550;
}
a[aria-current="page"] svg {
  color: var(--text-secondary);
}
@media (max-width: 760px) {
  a {
    min-height: 44px;
  }
}
@media (prefers-reduced-motion: reduce) {
  a {
    transition: none;
  }
}
</style>
