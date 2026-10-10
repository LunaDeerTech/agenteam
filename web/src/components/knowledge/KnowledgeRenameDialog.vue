<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDialog, UiField, UiInput } from '../ui'
import type { KnowledgeRename } from '../../composables/useKnowledgeRename'
const props = defineProps<{ editor: KnowledgeRename; fallbackFocus?: HTMLElement | null }>()
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
</script>

<template>
  <UiDialog
    v-model:open="open"
    title="文档改名"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <form aria-label="文档改名" @submit.prevent="editor.save()">
      <UiField
        label="文档标题"
        :error="editor.state.field"
        hint="保留原文字；最多 512 个字符。"
        required
      >
        <template #default="field">
          <UiInput
            v-model="editor.state.title"
            :id="field.id"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            :disabled="
              editor.blocked.value ||
              editor.state.denied ||
              ['submitting', 'uncertain'].includes(editor.progress.value?.phase ?? '')
            "
            autocomplete="off"
          />
        </template>
      </UiField>
      <p v-if="editor.state.message" role="status">{{ editor.state.message }}</p>
      <div class="rename-actions">
        <UiButton type="submit" :disabled="!editor.canSave.value">保存标题</UiButton>
        <UiButton
          v-if="editor.progress.value?.phase === 'uncertain'"
          :disabled="!editor.canLookup.value"
          @click="editor.lookup()"
          >查询原改名结果</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canRetryOriginal"
          :disabled="!editor.canReplay.value"
          @click="editor.replay()"
          >按原请求重放</UiButton
        >
        <template v-if="editor.state.conflict || editor.state.needsCurrent">
          <UiButton :disabled="editor.blocked.value" @click="editor.reread()"
            >重新读取当前文档</UiButton
          >
          <UiButton :disabled="!editor.canAdopt.value" @click="editor.adoptCurrent()"
            >采用当前版本</UiButton
          >
        </template>
      </div>
    </form>
    <template #footer><UiButton @click="editor.close()">关闭</UiButton></template>
  </UiDialog>
  <UiDialog
    v-model:open="confirmation"
    title="放弃本次改名？"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <p>离开会丢弃当前草稿和本页的原结果查询入口。已发送的改名可能已提交，停止等待不表示撤销。</p>
    <template #footer>
      <UiButton @click="editor.confirmDiscard(false)">继续处理</UiButton>
      <UiButton @click="editor.confirmDiscard(true)">放弃并继续</UiButton>
    </template>
  </UiDialog>
</template>

<style scoped>
.rename-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-3);
}
</style>
