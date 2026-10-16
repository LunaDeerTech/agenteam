<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDialog, UiField, UiInput } from '../ui'
import type { KnowledgeFileEditor } from '../../composables/useKnowledgeFile'
const props = defineProps<{ editor: KnowledgeFileEditor; fallbackFocus?: HTMLElement | null }>()
const open = computed({
  get: () => props.editor.state.open,
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
  () => props.editor.busy.value || props.editor.pending.value || props.editor.state.conflict,
)
function choose(event: Event) {
  const input = event.target as HTMLInputElement
  void props.editor.choose(input.files?.[0])
  input.value = ''
}
</script>

<template>
  <UiDialog
    v-model:open="open"
    :title="editor.state.mode === 'create' ? '上传文档' : '替换原文件'"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <form class="knowledge-file-form" aria-label="知识库文件" @submit.prevent="editor.save()">
      <UiField
        v-if="editor.state.mode === 'create'"
        label="标题"
        required
        hint="最多 512 个字符；保存时保留原文字。"
      >
        <template #default="field"
          ><UiInput
            v-model="editor.state.title"
            :id="field.id"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            :maxlength="1024"
            autocomplete="off"
            data-autofocus
        /></template>
      </UiField>
      <p v-else>{{ editor.state.title }} · 替换基于版本 {{ editor.state.version }}</p>
      <p v-if="editor.state.mode === 'create'" class="meta">
        父文档：{{ editor.state.parentLabel }}
      </p>
      <UiField
        label="文件"
        required
        hint="支持 TXT、Markdown、PDF 和 DOCX，文件最大 1 MiB。PDF 需包含可提取文字，暂不识别扫描图片。"
      >
        <template #default="field"
          ><input
            :id="field.id"
            type="file"
            accept=".txt,.md,.markdown,.pdf,.docx"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            @change="choose"
        /></template>
      </UiField>
      <p v-if="editor.state.filename" class="selected-file">已选择：{{ editor.state.filename }}</p>
      <p v-if="editor.state.reading" role="status">正在读取所选文件</p>
      <p v-if="editor.state.message" role="status">{{ editor.state.message }}</p>
      <div class="file-actions">
        <UiButton type="submit" variant="primary" :disabled="!editor.canSave.value"
          >保存文件</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.phase === 'uncertain'"
          :disabled="!editor.canLookup.value"
          @click="editor.recover()"
          >查询原上传结果</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canRetryOriginal"
          :disabled="!editor.canReplay.value"
          @click="editor.recover(true)"
          >按原请求重放</UiButton
        >
      </div>
    </form>
    <template #footer><UiButton @click="editor.close()">关闭</UiButton></template>
  </UiDialog>
  <UiDialog v-model:open="confirmation" title="放弃本次文件操作？" :close-on-outside="false">
    <p>离开会清除所选文件和原上传查询入口。已发送的上传可能已保存，停止等待不会撤销保存。</p>
    <template #footer
      ><UiButton @click="editor.confirmDiscard(false)">继续处理</UiButton
      ><UiButton @click="editor.confirmDiscard(true)">放弃并继续</UiButton></template
    >
  </UiDialog>
</template>

<style scoped>
.knowledge-file-form {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.knowledge-file-form input[type='file'] {
  max-width: 100%;
  font: inherit;
  color: inherit;
}
.selected-file {
  overflow-wrap: anywhere;
}
.file-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
