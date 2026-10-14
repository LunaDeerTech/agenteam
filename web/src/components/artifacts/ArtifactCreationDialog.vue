<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ArtifactCreationEditor } from '../../composables/useArtifactCreation'
const props = defineProps<{ editor: ArtifactCreationEditor }>()
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
    props.editor.state.confirmed ||
    props.editor.state.cancelled ||
    props.editor.state.denied,
)
const media = [
  { value: 'text/plain', label: '纯文本' },
  { value: 'text/markdown', label: 'Markdown' },
]
const bytes = computed(() => new TextEncoder().encode(props.editor.state.content).byteLength)
</script>
<template>
  <UiDialog
    v-model:open="open"
    title="新建文本产物"
    :fallback-focus="editor.fallbackFocus.value"
    :close-on-outside="false"
  >
    <form
      class="artifact-creation-form"
      aria-label="创建文本产物"
      @submit.prevent="editor.create()"
    >
      <UiField label="产物名称" required hint="保留原文字，最多 255 字节，不含斜杠或控制字符。">
        <template #default="field"
          ><UiInput
            v-model="editor.state.name"
            :id="field.id"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            autocomplete="off"
            data-autofocus
        /></template>
      </UiField>
      <UiField label="描述" hint="可留空，最多 4 KiB。">
        <template #default="field"
          ><UiTextarea
            v-model="editor.state.description"
            :id="field.id"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            rows="2"
        /></template>
      </UiField>
      <UiSelect v-model="editor.state.media" label="文本格式" :options="media" :disabled="frozen" />
      <UiField
        label="正文"
        :error="editor.state.field"
        hint="可留空，按 UTF-8 计最多 1 MiB；保留空格与换行。"
      >
        <template #default="field"
          ><UiTextarea
            v-model="editor.state.content"
            :id="field.id"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            :disabled="frozen"
            rows="10"
            spellcheck="false"
        /></template>
      </UiField>
      <p class="meta">{{ bytes.toLocaleString() }} / 1,048,576 字节</p>
      <p v-if="editor.state.writing" role="status">正在处理原创建请求</p>
      <p v-if="editor.state.message" role="status">{{ editor.state.message }}</p>
      <div class="artifact-creation-actions">
        <UiButton
          v-if="!editor.state.confirmed && !editor.state.cancelled"
          type="submit"
          variant="primary"
          :disabled="!editor.canSave.value"
          >创建产物</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.phase === 'uncertain'"
          :disabled="!editor.canLookup.value"
          @click="editor.lookup()"
          >查询原创建结果</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canRetryOriginal"
          :disabled="!editor.canRetry.value"
          @click="editor.retry()"
          >重试原创建请求</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canCancel"
          :disabled="!editor.canCancel.value"
          @click="editor.cancel()"
          >取消原创建</UiButton
        >
        <UiButton
          v-if="editor.state.needsRead"
          :disabled="editor.blocked.value"
          @click="editor.rereadCreated()"
          >重读已创建产物</UiButton
        >
      </div>
    </form>
    <template #footer
      ><UiButton @click="editor.close()">{{
        editor.state.confirmed ? '返回产物' : '关闭'
      }}</UiButton></template
    >
  </UiDialog>
  <UiDialog
    v-model:open="confirmation"
    title="放弃本次产物编辑？"
    :fallback-focus="editor.fallbackFocus.value"
    :close-on-outside="false"
  >
    <p>
      离开会清除草稿和原请求查询入口。已发送的创建可能已经提交，放弃本地编辑不会取消服务器上的创建。
    </p>
    <template #footer
      ><UiButton @click="editor.confirmDiscard(false)">继续编辑</UiButton
      ><UiButton @click="editor.confirmDiscard(true)">放弃并继续</UiButton></template
    >
  </UiDialog>
</template>
<style scoped>
.artifact-creation-form {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.artifact-creation-form p {
  overflow-wrap: anywhere;
}
.artifact-creation-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
