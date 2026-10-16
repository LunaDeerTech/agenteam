<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiCheckbox, UiDialog } from '../ui'
import type { KnowledgeDelete } from '../../composables/useKnowledgeDelete'
const props = defineProps<{ deletion: KnowledgeDelete; fallbackFocus?: HTMLElement | null }>()
const open = computed({
  get: () => props.deletion.state.open,
  set: (value) => {
    if (!value) void props.deletion.close()
  },
})
const confirmation = computed({
  get: () => props.deletion.state.confirming,
  set: (value) => {
    if (!value) props.deletion.confirmDiscard(false)
  },
})
</script>
<template>
  <UiDialog
    v-model:open="open"
    title="删除文档及子文档"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <form aria-label="确认删除子树" @submit.prevent="deletion.remove()">
      <p v-if="deletion.state.loading" role="status">正在读取完整删除范围</p>
      <template v-if="deletion.state.nodes.length">
        <p>
          将删除以下 {{ deletion.state.nodes.length }} 篇文档，包含“{{
            deletion.state.title
          }}”及其全部子文档。此操作无法撤销。
        </p>
        <ul class="delete-scope" aria-label="完整删除范围">
          <li v-for="node in deletion.state.nodes" :key="node.id">{{ node.title }}</li>
        </ul>
        <UiCheckbox v-model="deletion.state.accepted" :disabled="deletion.blocked.value"
          >我已核对以上文档，确认删除整个子树</UiCheckbox
        >
      </template>
      <p v-if="deletion.state.message" role="status">{{ deletion.state.message }}</p>
      <div class="delete-actions">
        <UiButton
          v-if="deletion.state.nodes.length"
          type="submit"
          variant="danger"
          :disabled="!deletion.canDelete.value"
          >确认删除</UiButton
        >
        <UiButton
          v-if="deletion.canPreview.value"
          :disabled="!deletion.canPreview.value"
          @click="deletion.loadPreview()"
          >重新预览</UiButton
        >
        <UiButton
          v-if="deletion.progress.value?.phase === 'uncertain'"
          :disabled="!deletion.canLookup.value"
          @click="deletion.lookup()"
          >查询原删除结果</UiButton
        >
        <UiButton
          v-if="deletion.progress.value?.canRetryOriginal"
          :disabled="!deletion.canReplay.value"
          @click="deletion.replay()"
          >按原请求重放</UiButton
        >
        <UiButton
          v-if="deletion.progress.value?.phase === 'confirmed'"
          :disabled="deletion.blocked.value"
          @click="deletion.reread()"
          >重新读取当前文档和目录</UiButton
        >
      </div>
    </form>
    <template #footer><UiButton @click="deletion.close()">关闭</UiButton></template>
  </UiDialog>
  <UiDialog
    v-model:open="confirmation"
    title="离开尚未确认的删除？"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <p>原删除可能已经提交。离开会放弃本页的原结果查询入口，停止等待不表示撤销。</p>
    <template #footer
      ><UiButton @click="deletion.confirmDiscard(false)">继续处理</UiButton
      ><UiButton @click="deletion.confirmDiscard(true)">放弃并继续</UiButton></template
    >
  </UiDialog>
</template>
<style scoped>
.delete-scope {
  max-height: 16rem;
  overflow: auto;
  overflow-wrap: anywhere;
  padding-inline-start: 20px;
}
.delete-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: var(--content-gap);
}
</style>
