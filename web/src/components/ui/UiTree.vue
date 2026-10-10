<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import UiIcon from './UiIcon.vue'
import type { TreeNode } from './types'
const props = defineProps<{ nodes: TreeNode[]; label: string }>()
const selected = defineModel<string>('selected', { default: '' })
const expanded = defineModel<string[]>('expanded', { default: () => [] })
const root = ref<HTMLElement | null>(null)
const focused = ref('')
const expandable = (node: TreeNode) => node.expandable ?? !!node.children?.length
const all = computed(() => {
  const out: { node: TreeNode; level: number; parent?: string; visible: boolean }[] = []
  function walk(nodes: TreeNode[], level: number, parent?: string, visible = true) {
    for (const node of nodes) {
      out.push({ node, level, parent, visible })
      if (node.children)
        walk(
          node.children,
          level + 1,
          node.id,
          visible && expandable(node) && expanded.value.includes(node.id),
        )
    }
  }
  walk(props.nodes, 1)
  return out
})
const visible = computed(() => all.value.filter((r) => r.visible && !r.node.disabled))
watch(
  visible,
  (value) => {
    if (!value.some((r) => r.node.id === focused.value)) focused.value = value[0]?.node.id || ''
  },
  { immediate: true },
)
function toggle(id: string) {
  expanded.value = expanded.value.includes(id)
    ? expanded.value.filter((v) => v !== id)
    : [...expanded.value, id]
}
async function focus(id?: string) {
  if (!id) return
  focused.value = id
  await nextTick()
  root.value?.querySelector<HTMLElement>(`[data-tree-id="${CSS.escape(id)}"]`)?.focus()
}
function keydown(event: KeyboardEvent, id: string) {
  const index = visible.value.findIndex((r) => r.node.id === id)
  const row = visible.value[index]
  if (!row) return
  let target: string | undefined
  if (event.key === 'ArrowDown') target = visible.value[index + 1]?.node.id
  else if (event.key === 'ArrowUp') target = visible.value[index - 1]?.node.id
  else if (event.key === 'Home') target = visible.value[0]?.node.id
  else if (event.key === 'End') target = visible.value.at(-1)?.node.id
  else if (event.key === 'ArrowRight') {
    if (expandable(row.node)) {
      if (!expanded.value.includes(id)) toggle(id)
      else target = row.node.children?.find((n) => !n.disabled)?.id
    }
  } else if (event.key === 'ArrowLeft') {
    if (expandable(row.node) && expanded.value.includes(id)) toggle(id)
    else target = row.parent
  } else if (event.key === 'Enter' || event.key === ' ') selected.value = id
  else return
  event.preventDefault()
  if (target) void focus(target)
}
</script>
<template>
  <div ref="root" class="ui-tree" role="tree" :aria-label="label">
    <template v-for="row in all" :key="row.node.id"
      ><div
        class="tree-visibility"
        :class="{ expanded: row.visible }"
        :inert="!row.visible"
        :aria-hidden="!row.visible"
      >
        <div class="collapse-inner">
          <div
            class="tree-row"
            role="treeitem"
            :aria-level="row.level"
            :aria-selected="selected === row.node.id"
            :aria-expanded="expandable(row.node) ? expanded.includes(row.node.id) : undefined"
            :aria-disabled="row.node.disabled || undefined"
            :tabindex="row.node.id === focused && !row.node.disabled ? 0 : -1"
            :data-tree-id="row.node.id"
            :style="{ '--tree-depth': row.level - 1 }"
            @focus="focused = row.node.id"
            @keydown="keydown($event, row.node.id)"
            @click="!row.node.disabled && (selected = row.node.id)"
          >
            <button
              v-if="expandable(row.node)"
              type="button"
              tabindex="-1"
              class="tree-disclosure"
              :disabled="row.node.disabled"
              :aria-label="(expanded.includes(row.node.id) ? '折叠' : '展开') + row.node.label"
              @click.stop="toggle(row.node.id)"
            >
              <UiIcon name="chevron" :class="{ rotated: expanded.includes(row.node.id) }" /></button
            ><span v-else class="tree-disclosure" /><UiIcon name="file" /><span
              class="truncate"
              :title="row.node.label"
              >{{ row.node.label }}</span
            ><slot name="suffix" :node="row.node" />
          </div>
        </div></div
    ></template>
  </div>
</template>
