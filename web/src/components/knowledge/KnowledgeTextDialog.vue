<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { KnowledgeTextEditor } from '../../composables/useKnowledgeText'
const props = defineProps<{ editor: KnowledgeTextEditor }>()
const open = computed({
  get: () => props.editor.state.open && props.editor.visible.value,
  set: (value) => {
    if (!value) void props.editor.close()
  },
})
const confirmation = computed({
  get: () => props.editor.state.confirming,
  set: (value) => {
    if (!value) props.editor.confirmDiscard(false)
  },
})
const frozen = computed(
  () =>
    props.editor.blocked.value ||
    props.editor.pending.value ||
    props.editor.state.saved ||
    props.editor.state.denied,
)
const parent = computed({
  get: () => props.editor.state.parentID ?? '',
  set: (id) => {
    const value = props.editor.parents.value.find((doc) => doc.id === id)
    if (!id || value) props.editor.setParent(value ?? null)
  },
})
const parentOptions = computed(() => [
  { value: '', label: '根层' },
  ...props.editor.parents.value.map((doc) => ({ value: doc.id, label: doc.title })),
])
const media = [
  { value: 'text/plain', label: '纯文本' },
  { value: 'text/markdown', label: 'Markdown' },
]
</script>
<template>
  <UiDialog
    v-model:open="open"
    :title="editor.state.mode === 'create' ? '新建文本' : '编辑正文'"
    :close-on-outside="false"
  >
    <form class="knowledge-text-form" aria-label="知识库文本" @submit.prevent="editor.save()">
      <UiField
        v-if="editor.state.mode === 'create'"
        label="标题"
        required
        hint="保留原文字，最多 512 个字符。"
      >
        <template #default="field"
          ><UiInput
            v-model="editor.state.title"
            :id="field.id"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            autocomplete="off"
            data-autofocus
        /></template>
      </UiField>
      <p v-else class="editor-document">
        {{ editor.state.title
        }}<span v-if="editor.state.version"> · 编辑基于版本 {{ editor.state.version }}</span>
      </p>
      <UiSelect
        v-if="editor.state.mode === 'create'"
        v-model="parent"
        label="父文档"
        :options="parentOptions"
        :disabled="frozen"
      />
      <p v-if="editor.state.mode === 'create'" class="meta">
        父文档：{{ editor.state.parentLabel }}。可从当前已读取的目录选择；展开目录可读取其他文档。
      </p>
      <UiSelect v-model="editor.state.media" label="文本格式" :options="media" :disabled="frozen" />
      <UiField
        label="正文"
        :error="editor.state.field"
        hint="可留空；保留空格与换行。标题、格式和正文编码后的完整请求最多 1 MiB。"
      >
        <template #default="field"
          ><UiTextarea
            v-model="editor.state.text"
            :id="field.id"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            :disabled="frozen || (editor.state.mode === 'update' && !editor.state.version)"
            rows="12"
            spellcheck="false"
        /></template>
      </UiField>
      <p v-if="editor.state.reading" role="status">正在读取完整当前正文</p>
      <p v-if="editor.state.message" role="status">{{ editor.state.message }}</p>
      <div v-if="editor.current.value && editor.state.conflict" class="current-comparison">
        <h3>当前正文 · 版本 {{ editor.current.value.document.content_version }}</h3>
        <pre aria-label="冲突后的当前正文" tabindex="0">{{ editor.current.value.text.text }}</pre>
      </div>
      <div class="text-actions">
        <UiButton type="submit" :disabled="!editor.canSave.value">保存文本</UiButton>
        <UiButton
          v-if="editor.progress.value?.phase === 'uncertain'"
          :disabled="!editor.canLookup.value"
          @click="editor.lookup()"
          >查询原保存结果</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canRetryOriginal"
          :disabled="!editor.canReplay.value"
          @click="editor.replay()"
          >按原请求重放</UiButton
        >
        <template v-if="editor.state.conflict">
          <UiButton :disabled="editor.blocked.value" @click="editor.rereadConflict()"
            >重读当前正文</UiButton
          >
          <UiButton :disabled="!editor.canAdopt.value" @click="editor.adoptVersion()"
            >采用当前版本并保留输入</UiButton
          >
        </template>
        <UiButton
          v-if="editor.state.needsRead"
          :disabled="editor.blocked.value"
          @click="editor.rereadSaved()"
          >重读保存后的文档</UiButton
        >
      </div>
    </form>
    <template #footer><UiButton @click="editor.close()">关闭编辑器</UiButton></template>
  </UiDialog>
  <UiDialog v-model:open="confirmation" title="放弃本次文本编辑？" :close-on-outside="false">
    <p>离开会清除草稿和原保存查询入口。已发送的保存可能已提交，停止等待不会撤销保存。</p>
    <template #footer
      ><UiButton @click="editor.confirmDiscard(false)">继续编辑</UiButton
      ><UiButton @click="editor.confirmDiscard(true)">放弃并继续</UiButton></template
    >
  </UiDialog>
</template>
<style scoped>
.knowledge-text-form {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.editor-document {
  overflow-wrap: anywhere;
}
.text-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.current-comparison {
  min-width: 0;
}
.current-comparison pre {
  overflow: auto;
  max-height: 30vh;
  padding: var(--content-gap);
  background: var(--surface-alt);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
