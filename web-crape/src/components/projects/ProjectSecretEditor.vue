<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiTextarea } from '../ui'
import { captureProjectSecretValue } from '../../api/project-secret-commands'
import type { ProjectSecretEditor } from '../../composables/useProjectSecretEditor'
const props = defineProps<{ owner: ProjectSecretEditor; fallbackFocus?: HTMLElement | null }>()
const emit = defineEmits<{ close: [] }>()
const form = ref<HTMLFormElement | null>(null),
  materialInput = ref<HTMLTextAreaElement | null>(null)
const materialInvalid = ref(false)
// Never make material reactive, emit it, or bind it to a component model.
let material = ''
let pendingEdit: { start: number; end: number; type: string } | null = null
const disabled = computed(
  () =>
    props.owner.blocked.value ||
    ['submitting', 'uncertain'].includes(props.owner.progress.value?.phase ?? ''),
)
const title = computed(() =>
  props.owner.state.mode === 'create'
    ? '创建 Secret'
    : props.owner.state.mode === 'metadata'
      ? '编辑名称与描述'
      : '替换 Secret 值',
)
const submitLabel = computed(() =>
  props.owner.state.mode === 'create'
    ? '创建 Secret'
    : props.owner.state.mode === 'metadata'
      ? '保存修改'
      : '确认替换',
)
const display = (value: string) => value.replace(/\r\n?/g, '\n')
function rawOffset(position: number): number {
  let at = 0,
    shown = 0
  while (at < material.length && shown < position) {
    at += material[at] === '\r' && material[at + 1] === '\n' ? 2 : 1
    ++shown
  }
  return at
}
function beforeInput(event: InputEvent) {
  const field = event.target as HTMLTextAreaElement
  pendingEdit = { start: field.selectionStart, end: field.selectionEnd, type: event.inputType }
}
function input(event: Event) {
  const next = (event.target as HTMLTextAreaElement).value,
    previous = display(material)
  const edit = pendingEdit
  pendingEdit = null
  if (edit) {
    let { start, end } = edit
    if (start === end && edit.type.startsWith('delete')) {
      const removed = previous.length - next.length
      if (removed >= 0 && edit.type.endsWith('Backward')) start = Math.max(0, start - removed)
      else if (removed >= 0 && edit.type.endsWith('Forward'))
        end = Math.min(previous.length, end + removed)
    }
    const insertedEnd = next.length - (previous.length - end)
    if (
      insertedEnd >= start &&
      next.slice(0, start) === previous.slice(0, start) &&
      next.slice(insertedEnd) === previous.slice(end)
    ) {
      material =
        material.slice(0, rawOffset(start)) +
        next.slice(start, insertedEnd) +
        material.slice(rawOffset(end))
      materialInvalid.value = false
      return
    }
  }
  let start = 0,
    end = previous.length,
    nextEnd = next.length
  while (start < end && start < nextEnd && previous[start] === next[start]) ++start
  while (end > start && nextEnd > start && previous[end - 1] === next[nextEnd - 1]) {
    --end
    --nextEnd
  }
  material =
    material.slice(0, rawOffset(start)) +
    next.slice(start, nextEnd) +
    material.slice(rawOffset(end))
  materialInvalid.value = false
}
function paste(event: ClipboardEvent) {
  if (disabled.value || !event.clipboardData || !materialInput.value) return
  event.preventDefault()
  pendingEdit = null
  const field = materialInput.value,
    pasted = event.clipboardData.getData('text/plain')
  const start = field.selectionStart,
    end = field.selectionEnd
  material = material.slice(0, rawOffset(start)) + pasted + material.slice(rawOffset(end))
  field.value = display(material)
  const caret = start + display(pasted).length
  field.setSelectionRange(caret, caret)
  materialInvalid.value = false
}
function clearMaterial() {
  material = ''
  pendingEdit = null
  if (materialInput.value) materialInput.value.value = ''
  materialInvalid.value = false
}
function takeMaterial() {
  const captured = captureProjectSecretValue(material)
  clearMaterial()
  return captured
}
async function submit() {
  if (!props.owner.canSave.value) return
  if (props.owner.state.mode !== 'metadata') {
    try {
      captureProjectSecretValue(material)
    } catch {
      materialInvalid.value = true
      return
    }
  }
  await props.owner.submit(takeMaterial)
}
function close() {
  clearMaterial()
  props.owner.close()
  emit('close')
}
watch(() => props.owner.state.materialRevision, clearMaterial, { flush: 'sync' })
watch(
  () => props.owner.state.formRevision,
  async () => {
    clearMaterial()
    const revision = props.owner.state.formRevision
    await nextTick()
    if (
      revision !== props.owner.state.formRevision ||
      !props.owner.state.open ||
      !props.owner.visible.value ||
      disabled.value
    )
      return
    const control = form.value?.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      'input,textarea',
    )
    if (control?.isConnected && !control.disabled) control.focus()
  },
  { immediate: true, flush: 'post' },
)
onBeforeUnmount(clearMaterial)
</script>

<template>
  <UiDialog
    :open="owner.visible.value && owner.state.open"
    :title="title"
    :fallback-focus="fallbackFocus"
    :close-on-outside="false"
    @update:open="!$event && close()"
  >
    <form
      id="project-secret-form"
      ref="form"
      :aria-label="title"
      class="secret-editor"
      @submit.prevent="submit"
    >
      <template v-if="owner.state.mode !== 'replace'">
        <UiField
          label="Secret 名称"
          required
          hint="最多 128 位英文字母、数字与下划线，首位不能是数字；AGENTEAM 名称保留。"
          v-slot="field"
        >
          <UiInput
            data-autofocus
            :id="field.id"
            v-model="owner.state.name"
            :disabled="disabled"
            :aria-describedby="field.describedby"
            autocomplete="off"
          />
        </UiField>
        <UiField label="描述" hint="可为空，最多 4096 UTF8 字节。" v-slot="field">
          <UiTextarea
            :id="field.id"
            v-model="owner.state.description"
            :disabled="disabled"
            rows="3"
          />
        </UiField>
      </template>
      <template v-else
        ><p>替换 {{ owner.state.name }} 的材料。已有值不会返回；替换会生成新版本。</p></template
      >
      <UiField
        v-if="owner.state.mode !== 'metadata'"
        label="Secret 值"
        required
        hint="1–65536 UTF8 字节，可多行。空白、制表符与粘贴的换行保持原样；提交后输入会清除。"
        :error="
          materialInvalid ? '请输入有效的非空文本（不含 NUL），并检查 UTF8 字节上限。' : undefined
        "
        v-slot="field"
      >
        <textarea
          ref="materialInput"
          data-autofocus
          :id="field.id"
          class="ui-input secret-material"
          :disabled="disabled"
          :aria-invalid="field.invalid || undefined"
          :aria-describedby="field.describedby"
          rows="4"
          autocomplete="off"
          autocapitalize="off"
          :spellcheck="false"
          @beforeinput="beforeInput"
          @input="input"
          @paste="paste"
        />
      </UiField>
      <p v-if="owner.state.mode === 'metadata'">此操作不会提交或修改 Secret 值。</p>
      <p v-if="owner.state.mode !== 'create'">当前编辑版本：{{ owner.state.version }}</p>
      <p v-if="owner.state.conflict" role="alert">请先重新读取当前版本，材料不会恢复。</p>
    </form>
    <p v-if="owner.state.message" role="status">{{ owner.state.message }}</p>
    <div v-if="owner.progress.value?.phase === 'uncertain'" class="secret-editor-recovery">
      <p>原请求尚未确认。材料已清除；检查原请求不会再次提交材料。</p>
      <UiButton :disabled="!owner.canLookup.value" @click="owner.lookup()">检查原请求</UiButton>
    </div>
    <div v-if="owner.progress.value?.phase === 'confirmed'" class="secret-editor-recovery">
      <p>原请求已确认。请独立读取当前记录。</p>
      <UiButton :disabled="owner.blocked.value" @click="owner.readConfirmed()"
        >读取已确认的记录</UiButton
      >
    </div>
    <template #footer>
      <UiButton
        v-if="owner.state.conflict"
        type="button"
        :disabled="disabled"
        @click="owner.readCurrent()"
        >重新读取当前版本</UiButton
      >
      <UiButton type="button" variant="ghost" @click="close">关闭编辑</UiButton>
      <UiButton
        type="submit"
        form="project-secret-form"
        variant="primary"
        :disabled="disabled || !owner.canSave.value"
        >{{ submitLabel }}</UiButton
      >
    </template>
  </UiDialog>
</template>
<style scoped>
.secret-editor {
  display: grid;
  gap: var(--content-gap);
  min-width: 0;
}
.secret-editor-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.secret-editor h2,
.secret-editor p {
  overflow-wrap: anywhere;
}
.secret-material {
  width: 100%;
  resize: vertical;
  white-space: pre-wrap;
}
</style>
