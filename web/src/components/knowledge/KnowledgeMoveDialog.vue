<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiDialog, UiState } from '../ui'
import type { KnowledgeMove } from '../../composables/useKnowledgeMove'
const props = defineProps<{ editor: KnowledgeMove; fallbackFocus?: HTMLElement | null }>()
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
    title="移动文档"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <form class="move-form" aria-label="移动文档" @submit.prevent="editor.save()">
      <p v-if="editor.state.title">
        移动「{{ editor.state.title }}」及其子文档，正文内容保持不变。
      </p>
      <p>
        目标位置：<strong>{{ editor.state.targetLabel || '尚未选择' }}</strong>
      </p>
      <div class="move-actions">
        <UiButton :disabled="!editor.canChoose.value" @click="editor.choose(null)"
          >移到根层</UiButton
        >
        <UiButton :disabled="!editor.canChoose.value" @click="editor.browse(null)"
          >浏览根目录</UiButton
        >
      </div>
      <nav v-if="editor.state.path.length" aria-label="父文档选择路径" class="move-actions">
        <UiButton
          v-for="document in editor.state.path"
          :key="document.id"
          :disabled="!editor.canChoose.value"
          @click="editor.browse(document.id)"
          >{{ document.title }}</UiButton
        >
      </nav>
      <UiState
        v-if="editor.state.pickerPhase === 'loading'"
        kind="loading"
        title="正在读取可选父文档"
      />
      <UiState
        v-else-if="editor.state.pickerPhase === 'empty'"
        kind="empty"
        title="此目录没有子文档"
      />
      <div v-if="editor.state.pickerPhase === 'error'" role="status">
        <p>{{ editor.state.pickerMessage }}</p>
        <UiButton :disabled="!editor.canChoose.value" @click="editor.browse(editor.state.parent)"
          >重读当前目录</UiButton
        >
      </div>
      <ul class="parent-options" aria-label="可选父文档">
        <li v-for="document in editor.state.items" :key="document.id">
          <span>{{ document.title }}</span>
          <div class="move-actions">
            <UiButton
              :disabled="!editor.canChoose.value"
              :aria-label="`选择 ${document.title} 为父文档`"
              @click="editor.choose(document.id)"
              >选为父文档</UiButton
            >
            <UiButton
              :disabled="!editor.canChoose.value"
              :aria-label="`浏览 ${document.title} 的子文档`"
              @click="editor.browse(document.id)"
              >查看子文档</UiButton
            >
          </div>
        </li>
      </ul>
      <UiButton
        v-if="editor.state.nextCursor"
        :disabled="!editor.canChoose.value"
        @click="editor.browse(editor.state.parent, true)"
        >继续加载可选文档</UiButton
      >
      <p v-if="editor.state.field" role="alert">{{ editor.state.field }}</p>
      <p v-if="editor.state.message" role="status">{{ editor.state.message }}</p>
      <div class="move-actions">
        <UiButton type="submit" :disabled="!editor.canSave.value">确认移动</UiButton>
        <UiButton
          v-if="editor.progress.value?.phase === 'uncertain'"
          :disabled="!editor.canLookup.value"
          @click="editor.lookup()"
          >查询原移动结果</UiButton
        >
        <UiButton
          v-if="editor.progress.value?.canRetryOriginal"
          :disabled="!editor.canReplay.value"
          @click="editor.replay()"
          >按原请求重放</UiButton
        >
        <template v-if="editor.state.conflict || editor.state.needsCurrent">
          <UiButton
            :disabled="editor.blocked.value || editor.progress.value?.phase === 'uncertain'"
            @click="editor.reread()"
            >重新读取当前文档</UiButton
          >
          <UiButton :disabled="!editor.canAdopt.value" @click="editor.adoptCurrent()"
            >采用当前位置</UiButton
          >
        </template>
      </div>
    </form>
    <template #footer><UiButton @click="editor.close()">关闭</UiButton></template>
  </UiDialog>
  <UiDialog
    v-model:open="confirmation"
    title="放弃本次移动？"
    :close-on-outside="false"
    :fallback-focus="fallbackFocus"
  >
    <p>离开会丢弃当前选择和本页的原结果查询入口。已发送的移动可能已提交，停止等待不表示撤销。</p>
    <template #footer>
      <UiButton @click="editor.confirmDiscard(false)">继续处理</UiButton>
      <UiButton @click="editor.confirmDiscard(true)">放弃并继续</UiButton>
    </template>
  </UiDialog>
</template>

<style scoped>
.move-form {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
.move-form p {
  overflow-wrap: anywhere;
}
.move-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.parent-options {
  display: grid;
  gap: var(--space-3);
  list-style: none;
  padding: 0;
  max-height: 35vh;
  overflow: auto;
}
.parent-options li {
  display: grid;
  gap: var(--space-2);
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  overflow-wrap: anywhere;
}
</style>
