<script setup lang="ts">
import { computed } from 'vue'
import { UiBadge, UiButton, UiState, UiTree } from '../ui'
import type { TreeNode } from '../ui/types'
import type { KnowledgeDocument } from '../../api/knowledge-owner'
import type { KnowledgeLevel } from '../../composables/useKnowledgeOwner'

const props = defineProps<{
  levels: Readonly<Record<string, KnowledgeLevel>>
  expanded: readonly string[]
  selected: string | null
  busy: boolean
}>()
const emit = defineEmits<{
  select: [id: string]
  expand: [ids: string[]]
  read: [parent: string | null, more: boolean]
}>()
const root = computed(() => props.levels.root!)
const documents = computed(
  () =>
    new Map(
      Object.values(props.levels).flatMap((level) =>
        level.items.map((item) => [item.id, item] as const),
      ),
    ),
)
const nodes = computed(() => {
  const seen = new Set<string>()
  function branch(items: readonly KnowledgeDocument[]): TreeNode[] {
    return items
      .filter((document) => {
        if (seen.has(document.id)) return false
        seen.add(document.id)
        return true
      })
      .map((document) => {
        const level = props.levels[document.id]
        return {
          id: document.id,
          label: document.title,
          expandable: !level || level.phase !== 'empty',
          children: branch(level?.items ?? []),
        }
      })
  }
  return branch(root.value.items)
})
const indexLabels = {
  pending: '索引中',
  processing: '索引中',
  ready: '就绪',
  failed: '索引失败',
} as const
const media = (id: string) => {
  const value = documents.value.get(id)?.media_type
  return value === 'text/markdown'
    ? 'Markdown'
    : value === 'text/plain'
      ? '文本'
      : value === 'application/pdf'
        ? 'PDF'
        : 'DOCX'
}
</script>

<template>
  <div class="knowledge-tree">
    <UiState
      v-if="root.phase === 'waiting' || (root.phase === 'loading' && !root.items.length)"
      kind="loading"
      title="正在读取文档树"
    />
    <UiState v-else-if="root.phase === 'empty'" kind="empty" title="暂无文档" />
    <UiState
      v-if="root.phase === 'error' || root.phase === 'unavailable'"
      kind="error"
      title="文档树读取失败"
      :description="root.message"
    >
      <UiButton :disabled="busy" @click="emit('read', null, false)">重读根目录</UiButton>
    </UiState>
    <UiTree
      v-if="nodes.length"
      :nodes="nodes"
      label="知识库文档"
      :selected="selected ?? ''"
      :expanded="[...expanded]"
      @update:selected="emit('select', $event)"
      @update:expanded="emit('expand', $event)"
    >
      <template #suffix="{ node }">
        <span class="node-facts"
          ><span class="media">{{ media(node.id) }}</span
          ><UiBadge
            :tone="documents.get(node.id)?.indexing_status === 'failed' ? 'warning' : 'neutral'"
            >{{ indexLabels[documents.get(node.id)!.indexing_status] }}</UiBadge
          ></span
        >
        <span v-if="expanded.includes(node.id)" class="branch-actions" @click.stop @keydown.stop>
          <span v-if="levels[node.id]?.phase === 'loading'" role="status">读取中</span>
          <UiButton
            v-else-if="
              !levels[node.id] || ['idle', 'error', 'unavailable'].includes(levels[node.id]!.phase)
            "
            :disabled="busy"
            @click="emit('read', node.id, false)"
            >{{ levels[node.id]?.message ? '重读子文档' : '读取子文档' }}</UiButton
          >
          <UiButton
            v-else-if="levels[node.id]?.nextCursor"
            :disabled="busy"
            @click="emit('read', node.id, true)"
            >继续加载子文档</UiButton
          >
        </span>
      </template>
    </UiTree>
    <UiButton v-if="root.nextCursor" :disabled="busy" @click="emit('read', null, true)"
      >继续加载根文档</UiButton
    >
    <p v-if="root.items.length && root.phase === 'error'" class="meta">
      以上为此前完整读取的目录，本次读取未完成。
    </p>
  </div>
</template>

<style scoped>
.knowledge-tree {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.node-facts {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-inline-start: auto;
}
.media {
  color: var(--muted);
  font-size: 12px;
}
.branch-actions {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.knowledge-tree :deep(.tree-row) {
  flex-wrap: wrap;
}
</style>
