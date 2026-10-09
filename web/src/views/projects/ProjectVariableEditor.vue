<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiTextarea from '../../components/ui/UiTextarea.vue'
import { useProjectVariables } from '../../composables/useProjectVariables'
const page = useProjectVariables()
const { editor, draft, blocked, canMutate, canSave, canDelete, feedback, progress } = page
const locked = computed(
  () =>
    blocked.value ||
    !canMutate.value ||
    !!progress.value ||
    editor.phase !== 'ready' ||
    editor.requiresRead ||
    editor.conflict,
)
const successVisible = ref(false)
let successTimer: ReturnType<typeof setTimeout> | undefined
watch(feedback, (value) => {
  clearTimeout(successTimer)
  successVisible.value = value === 'success'
  if (successVisible.value)
    successTimer = setTimeout(() => {
      successVisible.value = false
    }, 2400)
})
onBeforeUnmount(() => clearTimeout(successTimer))
const buttonState = computed(() =>
  feedback.value === 'loading' ? 'loading' : successVisible.value ? 'success' : 'idle',
)
const bytes = (value: string) => new TextEncoder().encode(value).byteLength
</script>
<template>
  <section v-if="editor.open" class="variable-editor" aria-labelledby="variable-editor-title">
    <div class="editor-heading">
      <h2 id="variable-editor-title">
        {{ editor.mode === 'create' ? '新建普通变量' : '变量详情' }}
      </h2>
      <UiButton variant="ghost" :disabled="blocked" @click="page.closeEditor()">关闭详情</UiButton>
    </div>
    <p class="meta">
      变量 ID：{{ editor.targetID
      }}<template v-if="editor.version"> · 已读版本 {{ editor.version }}</template>
    </p>
    <p v-if="editor.message" role="status" class="notice">{{ editor.message }}</p>
    <p v-if="editor.requiresRead" role="status" class="notice">
      尚未取得可用于新修改的当前信息。草稿与历史回执保留，请明确重新读取。
    </p>
    <UiButton v-if="editor.mode === 'edit'" :disabled="blocked" @click="page.readSelected()"
      >读取当前变量</UiButton
    >
    <p v-if="editor.phase === 'loading'" role="status">正在读取完整变量…</p>
    <p v-if="editor.missing" role="status">
      当前对象不存在或不可访问；不会将同名的新对象当作此变量。
    </p>
    <div v-if="editor.review" class="notice" aria-label="当前信息审阅">
      <h3>当前读取结果（版本 {{ editor.review.version }}）</h3>
      <dl>
        <dt>名称</dt>
        <dd>{{ editor.review.name }}</dd>
        <dt>描述</dt>
        <dd>{{ editor.review.description || '（空）' }}</dd>
        <dt>值</dt>
        <dd>
          <pre>{{ editor.review.value }}</pre>
        </dd>
      </dl>
      <p>请明确选择输入内容。版本只在此步骤采用，不自动合并。</p>
      <div class="actions">
        <UiButton
          :disabled="blocked || !!progress || editor.requiresRead"
          @click="page.adoptCurrent(false)"
          >采用当前内容</UiButton
        >
        <UiButton
          :disabled="blocked || !!progress || editor.requiresRead"
          @click="page.adoptCurrent(true)"
          >保留输入，采用当前版本</UiButton
        >
      </div>
    </div>
    <form
      v-if="editor.phase === 'ready' || editor.original || editor.mode === 'create'"
      class="variable-form"
      @submit.prevent="page.save()"
    >
      <UiField
        v-slot="field"
        label="名称"
        required
        :error="editor.fields.name"
        hint="字母或下划线开头，仅英文字母、数字和下划线，最多128字节。AGENTEAM及AGENTEAM_前缀保留。"
      >
        <UiInput
          :id="field.id"
          v-model="draft.name"
          :invalid="field.invalid"
          :aria-describedby="field.describedby"
          :disabled="locked"
          autocomplete="off"
          spellcheck="false"
        />
      </UiField>
      <UiField
        v-slot="field"
        label="描述"
        :error="editor.fields.description"
        :hint="`${bytes(draft.description)} / 4096 UTF-8 字节，可空，逐字保存。`"
      >
        <UiTextarea
          :id="field.id"
          v-model="draft.description"
          :invalid="field.invalid"
          :aria-describedby="field.describedby"
          :disabled="locked"
          rows="3"
        />
      </UiField>
      <UiField
        v-slot="field"
        label="值"
        :error="editor.fields.value"
        :hint="`${bytes(draft.value)} / 32768 UTF-8 字节，可空。普通变量不是 Secret，请勿作为敏感凭据保存。`"
      >
        <UiTextarea
          :id="field.id"
          v-model="draft.value"
          :invalid="field.invalid"
          :aria-describedby="field.describedby"
          :disabled="locked"
          rows="7"
          spellcheck="false"
        />
      </UiField>
      <div class="actions">
        <UiButton type="submit" variant="primary" :disabled="!canSave" :state="buttonState">{{
          editor.mode === 'create' ? '创建变量' : '保存修改'
        }}</UiButton>
        <UiButton
          v-if="editor.mode === 'edit'"
          :disabled="!canDelete"
          @click="page.deleteSelected()"
          >删除变量</UiButton
        >
      </div>
    </form>
  </section>
</template>
<style scoped>
.variable-editor {
  display: grid;
  gap: var(--space);
  min-width: 0;
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
.editor-heading,
.actions {
  display: flex;
  gap: var(--content-gap);
  align-items: center;
  flex-wrap: wrap;
}
.editor-heading {
  justify-content: space-between;
}
.variable-form {
  display: grid;
  gap: var(--space);
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 20rem;
  overflow: auto;
  margin: 0;
}
dd {
  margin: 0 0 var(--content-gap);
  overflow-wrap: anywhere;
}
.meta,
.notice {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
