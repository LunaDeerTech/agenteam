<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, useId, watch } from 'vue'
import { useRoute } from 'vue-router'
import UiButton from '../ui/UiButton.vue'
import UiDrawer from '../ui/UiDrawer.vue'
const emit = defineEmits<{ logout: [] }>()
const route = useRoute()
type SettingsGroup =
  | Readonly<{ label: string; path: string; leaf: string; key?: never; children?: never }>
  | Readonly<{
      key: string
      label: string
      children: readonly Readonly<{ label: string; path: string }>[]
      path?: never
      leaf?: never
    }>
const props = withDefaults(
  defineProps<{
    title?: string
    groups?: readonly SettingsGroup[]
    showLogout?: boolean
  }>(),
  {
    title: '个人设置',
    groups: () => [
      { label: '个人资料', path: '/settings/profile', leaf: '基本资料' },
      { label: '界面偏好', path: '/settings/appearance', leaf: '主题' },
      { label: '账号安全', path: '/settings/password', leaf: '修改密码' },
    ],
    showLogout: true,
  },
)
const instance = useId()
let sequence = 0
const ids = new Map<string, string>()
const expanded = reactive(new Set<string>())
const groups = computed(() =>
  props.groups.map((group) => ({
    key: group.children ? `group:${group.key}` : `path:${group.path}`,
    label: group.label,
    children: group.children ?? [{ label: group.leaf!, path: group.path! }],
  })),
)
watch(
  groups,
  (value) => {
    const present = new Set(value.map((group) => group.key))
    for (const key of ids.keys())
      if (!present.has(key)) {
        ids.delete(key)
        expanded.delete(key)
      }
    for (const group of value)
      if (!ids.has(group.key)) {
        ids.set(group.key, `settings-${instance}-${++sequence}`)
        expanded.add(group.key)
      }
  },
  { immediate: true, flush: 'sync' },
)
const narrow = ref(false),
  open = ref(false)
let media: MediaQueryList | undefined
function resize() {
  narrow.value = !!media?.matches
  if (!narrow.value) open.value = false
}
function toggle(path: string) {
  expanded.has(path) ? expanded.delete(path) : expanded.add(path)
}
onMounted(() => {
  media = window.matchMedia('(max-width: 760px)')
  resize()
  media.addEventListener('change', resize)
})
onUnmounted(() => media?.removeEventListener('change', resize))
</script>
<template>
  <div class="settings-shell">
    <UiButton v-if="narrow" class="settings-menu-button" aria-haspopup="dialog" @click="open = true"
      >{{ title }}栏目</UiButton
    >
    <component
      :is="narrow ? UiDrawer : 'aside'"
      :open="open"
      :title="title + '栏目'"
      v-bind="narrow ? {} : { class: 'settings-sidebar' }"
      @update:open="open = $event"
    >
      <nav class="settings-menu" :aria-label="title">
        <h2>{{ title }}</h2>
        <div
          v-for="group in groups"
          :key="group.key"
          class="settings-group"
          :class="{ selected: group.children.some((leaf) => route.path === leaf.path) }"
        >
          <button
            type="button"
            class="settings-group-toggle"
            :aria-expanded="expanded.has(group.key)"
            :aria-controls="ids.get(group.key)"
            @click="toggle(group.key)"
          >
            {{ group.label }}
          </button>
          <ul v-if="expanded.has(group.key)" :id="ids.get(group.key)">
            <li v-for="leaf in group.children" :key="leaf.path">
              <RouterLink
                :to="leaf.path"
                :aria-current="route.path === leaf.path ? 'page' : undefined"
                @click="open = false"
                >{{ leaf.label }}</RouterLink
              >
            </li>
          </ul>
        </div>
        <UiButton v-if="showLogout" variant="ghost" @click="emit('logout')">退出登录</UiButton>
      </nav>
    </component>
    <section class="settings-content"><slot /></section>
  </div>
</template>
<style scoped>
.settings-shell {
  display: grid;
  grid-template-columns: 216px minmax(0, 1fr);
  gap: 24px;
  padding: var(--space);
  min-width: 0;
  align-items: start;
}
.settings-sidebar {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 14px;
  position: sticky;
  top: 0;
  max-height: calc(100dvh - 96px);
  overflow-y: auto;
  min-width: 0;
}
.settings-menu {
  display: grid;
  gap: 12px;
}
.settings-menu h2 {
  font-size: 15px;
}
.settings-group-toggle {
  width: 100%;
  background: none;
  color: var(--text);
  border: 0;
  padding: 8px;
  text-align: start;
  font: inherit;
  cursor: pointer;
}
.settings-group.selected > button {
  font-weight: 600;
}
.settings-group ul {
  margin: 0;
  padding: 4px 0 4px 16px;
  list-style: none;
}
.settings-group a {
  display: block;
  padding: 8px;
  border-radius: var(--radius);
}
.settings-group a[aria-current='page'] {
  background: var(--accent-soft);
  color: var(--accent);
}
.settings-content {
  min-width: 0;
  max-width: 800px;
  width: 100%;
  overflow-wrap: anywhere;
}
@media (max-width: 760px) {
  .settings-shell {
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
  }
  .settings-menu-button {
    justify-self: start;
  }
}
</style>
