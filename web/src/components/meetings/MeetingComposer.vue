<script setup lang="ts">
import { computed, nextTick, onMounted, onScopeDispose, ref, shallowRef, watch } from 'vue'
import type { MeetingNode, MeetingReference } from '../../api/meetings'
import {
  focusMeetingEditable,
  meetingDraftLength,
  meetingLink,
  meetingSelection,
  normalizeMeetingDraft,
  readMeetingEditable,
  replaceMeetingRange,
  sameMeetingMention,
  type MeetingMentionChoice,
} from '../../composables/meeting-composer'
import { UiButton, UiDialog, UiInput, UiMenu } from '../ui'
import MeetingResourcePicker from './MeetingResourcePicker.vue'
import MeetingFileUpload from './MeetingFileUpload.vue'
const props = defineProps<{
  modelValue: readonly MeetingNode[]
  meetingId: string | null
  mentions: readonly MeetingMentionChoice[]
  disabled: boolean
  label: string
  placeholder: string
  uploadPending?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [nodes: MeetingNode[]] }>()
const editor = ref<HTMLElement | null>(null)
const panel = ref<'mentions' | 'resources' | 'link' | 'upload' | null>(null)
const uploadFile = shallowRef<File | null>(null)
const initialKind = ref<'task' | 'knowledge' | 'file' | undefined>()
const onlyAgents = ref(false),
  url = ref(''),
  linkLabel = ref(''),
  error = ref('')
let chips = new WeakMap<Node, MeetingNode>(),
  current: readonly MeetingNode[] = props.modelValue
let fingerprint = JSON.stringify(current),
  composing = false,
  disposed = false
let selection = { start: meetingDraftLength(current), end: meetingDraftLength(current) },
  panelSelection = { ...selection },
  panelFingerprint = ''
const invalidMentions = computed(() =>
  props.modelValue.some(
    (node) =>
      node.type === 'mention' &&
      !props.mentions.some((choice) => sameMeetingMention(choice.node, node)),
  ),
)
const tooLarge = computed(
  () =>
    props.modelValue.length > 1024 ||
    new TextEncoder().encode(
      props.modelValue
        .filter((node) => node.type === 'text')
        .map((node) => node.text)
        .join(''),
    ).length > 131072,
)
const choices = computed(() =>
  props.mentions.filter((choice) => !onlyAgents.value || choice.kind === 'agent'),
)
const menu = [
  { id: 'upload', label: '添加 · 上传附件' },
  { id: 'knowledge', label: '知识库 · 选择文档' },
  { id: 'task', label: '任务 · 选择 Task' },
  { id: 'file', label: '文件 · 选择已有文件' },
  { id: 'mentions', label: 'Agent · 选择参会 Agent' },
  { id: 'link', label: '添加链接' },
]
function nodeLabel(node: MeetingNode) {
  if (node.type === 'text') return node.text
  if (node.type === 'mention')
    return (
      '@' +
      (props.mentions.find((choice) => sameMeetingMention(choice.node, node))?.label ||
        '不可用成员')
    )
  return (
    node.reference.label ||
    (node.type === 'link'
      ? node.reference.id
      : { task: '任务', knowledge: '知识文档', file: '文件' }[node.type])
  )
}
function render() {
  const root = editor.value
  if (!root) return
  chips = new WeakMap()
  const fragment = document.createDocumentFragment()
  let offset = 0
  for (const node of current) {
    if (node.type === 'text') {
      fragment.append(document.createTextNode(node.text))
      offset += node.text.length
      continue
    }
    fragment.append(document.createTextNode(''))
    const chip = document.createElement('span')
    chip.className = 'composer-chip'
    if (
      node.type === 'mention' &&
      !props.mentions.some((choice) => sameMeetingMention(choice.node, node))
    )
      chip.classList.add('unavailable')
    chip.contentEditable = 'false'
    const caption = document.createElement('span')
    caption.textContent = nodeLabel(node)
    const remove = document.createElement('button')
    remove.type = 'button'
    remove.textContent = '×'
    remove.setAttribute('aria-label', '移除 ' + nodeLabel(node))
    remove.disabled = props.disabled
    const at = offset
    remove.addEventListener('click', () => {
      if (!props.disabled) commit(replaceMeetingRange(current, at, at + 1, []), at)
    })
    chip.append(caption, remove)
    chips.set(chip, node)
    fragment.append(chip)
    offset++
  }
  fragment.append(document.createTextNode(''))
  root.replaceChildren(fragment)
}
function remember() {
  if (editor.value) selection = meetingSelection(editor.value, chips) ?? selection
}
function commit(nodes: readonly MeetingNode[], caret?: number) {
  current = normalizeMeetingDraft(nodes)
  fingerprint = JSON.stringify(current)
  emit('update:modelValue', [...current])
  if (!composing) {
    render()
    if (caret !== undefined && editor.value) {
      selection = { start: caret, end: caret }
      focusMeetingEditable(editor.value, chips, caret)
    }
  }
}
function input(event: Event) {
  if (props.disabled || !editor.value) return
  remember()
  commit(readMeetingEditable(editor.value, chips), selection.end)
  const previous = current
    .map((node) => (node.type === 'text' ? node.text : '\ufffc'))
    .join('')
    .at(selection.end - 2)
  if (
    !composing &&
    event instanceof InputEvent &&
    !event.isComposing &&
    ['@', '#'].includes(event.data ?? '') &&
    selection.end > 0 &&
    (selection.end === 1 || previous === '\ufffc' || /\s/.test(previous ?? ''))
  ) {
    selection = { start: selection.end - 1, end: selection.end }
    initialKind.value = undefined
    open(event.data === '@' ? 'mentions' : 'resources', false)
  }
}
function open(kind: 'mentions' | 'resources' | 'link', agents = false) {
  if (props.disabled) return
  // Keyup and blur can update the live caret after a trigger opens the panel.
  // The selected identity must replace the range captured by that opening.
  panelSelection = { ...selection }
  panelFingerprint = fingerprint
  onlyAgents.value = agents
  url.value = linkLabel.value = error.value = ''
  panel.value = kind
}
function action(id: string) {
  if (props.disabled) return
  if (id === 'mentions') open('mentions', true)
  else if (id === 'upload') openUpload()
  else if (id === 'link') open('link')
  else if (id === 'task' || id === 'knowledge' || id === 'file') {
    initialKind.value = id
    open('resources')
  }
}
function openUpload(file: File | null = null) {
  if (props.disabled && !props.uploadPending) return
  panelSelection = { ...selection }
  panelFingerprint = fingerprint
  uploadFile.value = file
  error.value = ''
  panel.value = 'upload'
}
function drop(event: DragEvent) {
  event.preventDefault()
  if (props.disabled || composing) return
  const files = event.dataTransfer?.files
  if (!files || files.length !== 1 || !(files[0] instanceof File)) {
    error.value = '请每次拖入一个文件。'
    return
  }
  remember()
  openUpload(files[0])
}
async function insert(nodes: readonly MeetingNode[]) {
  if (props.disabled || panelFingerprint !== fingerprint) {
    error.value = '正文已变化，请关闭后重新选择。'
    return
  }
  const next = replaceMeetingRange(current, panelSelection.start, panelSelection.end, nodes)
  const caret = panelSelection.start + meetingDraftLength(nodes)
  const captured = fingerprint
  panel.value = null
  await nextTick()
  if (!disposed && !props.disabled && fingerprint === captured) commit(next, caret)
}
function choose(reference: MeetingReference) {
  void insert([{ type: reference.kind, reference }])
}
function chooseMention(choice: MeetingMentionChoice) {
  if (props.mentions.some((value) => sameMeetingMention(value.node, choice.node)))
    void insert([choice.node])
}
function addLink() {
  const reference = meetingLink(url.value, linkLabel.value)
  if (!reference) {
    error.value = '请输入不含用户名或密码的 http/https 链接。'
    return
  }
  choose(reference)
}
function paste(event: ClipboardEvent) {
  event.preventDefault()
  if (props.disabled || composing) return
  const value = event.clipboardData?.getData('text/plain')
  if (!value) return
  remember()
  const reference = meetingLink(value)
  const nodes: MeetingNode[] = reference
    ? [{ type: 'link', reference }]
    : [{ type: 'text', text: value }]
  commit(
    replaceMeetingRange(current, selection.start, selection.end, nodes),
    selection.start + meetingDraftLength(nodes),
  )
}
function keydown(event: KeyboardEvent) {
  if (event.target !== editor.value || props.disabled || composing || event.isComposing) return
  if (event.key === 'Enter') {
    event.preventDefault()
    remember()
    commit(
      replaceMeetingRange(current, selection.start, selection.end, [{ type: 'text', text: '\n' }]),
      selection.start + 1,
    )
  }
}
function compositionEnd(event: CompositionEvent) {
  composing = false
  input(event)
}
function compositionStart() {
  composing = true
}
watch(
  () => props.modelValue,
  (value) => {
    if (JSON.stringify(value) === fingerprint) return
    current = value
    fingerprint = JSON.stringify(value)
    panel.value = null
    selection = { start: meetingDraftLength(value), end: meetingDraftLength(value) }
    render()
  },
  { deep: true },
)
watch(
  () => [props.mentions, props.disabled],
  () => {
    if (composing) return
    const focused = !!editor.value?.contains(document.activeElement)
    remember()
    render()
    if (focused && !props.disabled && editor.value)
      focusMeetingEditable(editor.value, chips, selection.end)
  },
  { deep: true },
)
watch(panel, async (value, before) => {
  if (!value) uploadFile.value = null
  if (!value && before) {
    await nextTick()
    if (!disposed && !props.disabled && editor.value)
      focusMeetingEditable(editor.value, chips, selection.end)
  }
})
onMounted(render)
onScopeDispose(() => {
  disposed = true
})
defineExpose({
  focus: () => editor.value && focusMeetingEditable(editor.value, chips, selection.end),
})
</script>
<template>
  <div class="meeting-composer-editor">
    <div
      ref="editor"
      id="meeting-message"
      role="textbox"
      aria-multiline="true"
      :aria-label="label"
      :aria-disabled="disabled"
      :aria-invalid="invalidMentions || tooLarge || undefined"
      :contenteditable="!disabled"
      :data-placeholder="placeholder"
      class="editable"
      @input="input"
      @paste="paste"
      @keydown="keydown"
      @keyup="remember"
      @mouseup="remember"
      @blur="remember"
      @drop="drop"
      @dragover.prevent
      @compositionstart="compositionStart"
      @compositionend="compositionEnd"
    />
    <p v-if="invalidMentions" class="notice" role="status">
      有成员已不在参会名单中，请移除对应提及或重新选择。
    </p>
    <p v-if="tooLarge" class="notice" role="status">消息过长，请缩短正文或移除部分引用。</p>
    <p v-if="error && !panel" class="notice" role="status">{{ error }}</p>
    <div class="composer-tools">
      <UiMenu
        label="添加到消息"
        aria-label="添加到消息"
        :actions="menu"
        :disabled="disabled"
        @select="action"
        >＋</UiMenu
      >
      <UiButton v-if="uploadPending" variant="ghost" @click="openUpload()">查看待确认上传</UiButton>
      <slot />
    </div>
    <UiDialog
      :open="panel !== null"
      :title="
        panel === 'mentions'
          ? '提及参会成员'
          : panel === 'resources'
            ? '插入项目资源'
            : panel === 'upload'
              ? '上传附件'
              : '添加链接'
      "
      @update:open="!$event && (panel = null)"
    >
      <template v-if="panel === 'mentions'">
        <p v-if="!choices.length" class="muted">请先编排参会 Agent。</p>
        <ul class="mention-choices">
          <li v-for="choice in choices" :key="choice.node.agent_id || choice.node.participant_id">
            <UiButton
              :aria-label="
                '提及 ' + choice.label + '（' + (choice.kind === 'agent' ? 'Agent' : '用户') + '）'
              "
              :disabled="disabled"
              @click="chooseMention(choice)"
              ><span>{{ choice.label }}</span
              ><span class="muted">{{ choice.kind === 'agent' ? 'Agent' : '用户' }}</span></UiButton
            >
          </li>
        </ul>
      </template>
      <MeetingResourcePicker
        v-else-if="panel === 'resources'"
        :meeting-id="meetingId"
        :initial-kind="initialKind"
        :disabled="disabled"
        :selected="[]"
        @choose="choose"
      />
      <div v-else-if="panel === 'link'" class="link-fields">
        <label>链接地址<UiInput v-model="url" type="url" :disabled="disabled" /></label
        ><label
          >显示文字（可选）<UiInput v-model="linkLabel" :disabled="disabled" maxlength="1024"
        /></label>
      </div>
      <MeetingFileUpload
        v-else-if="panel === 'upload'"
        :meeting-id="meetingId"
        :file="uploadFile"
        :disabled="disabled"
        @choose="choose"
      />
      <p v-if="error" class="notice" role="status">{{ error }}</p>
      <template #footer
        ><UiButton @click="panel = null">返回正文</UiButton
        ><UiButton
          v-if="panel === 'link'"
          variant="primary"
          :disabled="disabled || !url"
          @click="addLink"
          >插入链接</UiButton
        ></template
      >
    </UiDialog>
  </div>
</template>
<style scoped>
.editable {
  min-height: 112px;
  max-height: 300px;
  overflow-y: auto;
  padding: 16px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  outline: none;
  font-size: 14px;
  line-height: 1.7;
}
.editable:empty::before {
  content: attr(data-placeholder);
  color: var(--muted);
  pointer-events: none;
}
.editable[aria-disabled='true'] {
  opacity: 0.6;
}
.editable :deep(.composer-chip) {
  display: inline-flex;
  align-items: baseline;
  max-width: 100%;
  gap: 4px;
  background: var(--surface-alt);
  border-radius: 4px;
  padding: 0 5px;
  vertical-align: baseline;
  white-space: normal;
}
.editable :deep(.composer-chip button) {
  border: 0;
  padding: 0 2px;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
  font: inherit;
}
.editable :deep(.composer-chip button:focus-visible) {
  outline: 2px solid var(--focus);
  outline-offset: 1px;
}
.editable :deep(.unavailable) {
  color: var(--warning);
}
.composer-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
}
.mention-choices {
  display: grid;
  gap: 8px;
  padding: 0;
  list-style: none;
}
.mention-choices :deep(button) {
  display: flex;
  gap: 12px;
}
.link-fields,
.link-fields label {
  display: grid;
  gap: 8px;
}
.muted {
  color: var(--muted);
}
.notice {
  margin: 0;
  padding: 8px 16px;
  color: var(--warning);
  font-size: 12px;
}
</style>
