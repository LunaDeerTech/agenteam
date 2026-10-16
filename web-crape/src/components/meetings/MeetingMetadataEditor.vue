<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { agentLabel, type DirectoryAgent } from '../../api/agent-directory'
import {
  captureMeetingCommand,
  type MeetingCommand,
  type MeetingDetail,
  type MeetingReference,
} from '../../api/meetings'
import { UiButton, UiCheckbox, UiDialog, UiInput } from '../ui'
import MeetingResourcePicker from './MeetingResourcePicker.vue'

type EditCommand = Extract<MeetingCommand, { kind: 'participants' | 'references' }>
const props = defineProps<{
  detail: MeetingDetail
  agents: readonly DirectoryAgent[]
  moreAgents: boolean
  agentError: string
  mutable: boolean
  busy: boolean
  unsettled: boolean
  canLookup: boolean
  feedback: string
  submit: (command: EditCommand) => Promise<boolean>
  lookup: () => Promise<string | null>
}>()
const emit = defineEmits<{ agents: [more: boolean]; dirty: [dirty: boolean] }>()
const editing = ref<'participants' | 'references' | null>(null)
const returnFocus = ref<HTMLElement | null>(null)
const agents = ref<string[]>([]),
  references = ref<MeetingReference[]>([])
const version = ref(''),
  original = ref(''),
  meeting = ref('')
const url = ref(''),
  label = ref(''),
  error = ref(''),
  submitting = ref(false)
const discard = ref(false)
const payload = computed(() =>
  JSON.stringify(editing.value === 'participants' ? agents.value : references.value),
)
const dirty = computed(
  () =>
    editing.value !== null &&
    (payload.value !== original.value || url.value !== '' || label.value !== ''),
)
const current = computed(
  () => meeting.value === props.detail.session.id && version.value === props.detail.session.version,
)
const locked = computed(() => props.busy || props.unsettled || submitting.value)
let pendingFocus: {
  target: HTMLElement
  previous: Element | null
  project: string
  meeting: string
  version: string
} | null = null
function cancelFocusReturn() {
  pendingFocus = null
  document.removeEventListener('pointerdown', cancelFocusReturn, true)
  document.removeEventListener('keydown', cancelFocusReturn, true)
  document.removeEventListener('focusin', focusChanged, true)
  document.removeEventListener('visibilitychange', visibilityChanged)
  window.removeEventListener('blur', cancelFocusReturn)
}
function focusChanged(event: FocusEvent) {
  if (event.target !== document.body) cancelFocusReturn()
}
function visibilityChanged() {
  if (document.hidden) cancelFocusReturn()
}
watch(
  [
    editing,
    () => props.busy,
    () => props.mutable,
    () => props.unsettled,
    () => props.detail.session.project_id,
    () => props.detail.session.id,
    () => props.detail.session.version,
  ],
  () => {
    const original = pendingFocus
    if (!original) return
    if (
      editing.value !== null ||
      props.unsettled ||
      props.detail.session.project_id !== original.project ||
      props.detail.session.id !== original.meeting ||
      props.detail.session.version !== original.version ||
      returnFocus.value !== original.target ||
      !original.target.isConnected
    ) {
      cancelFocusReturn()
      return
    }
    if (props.busy) {
      // Only retain this close's original target while its read keeps it
      // disabled. A later user action or focus owner cancels the handoff.
      document.addEventListener('pointerdown', cancelFocusReturn, true)
      document.addEventListener('keydown', cancelFocusReturn, true)
      document.addEventListener('focusin', focusChanged, true)
      document.addEventListener('visibilitychange', visibilityChanged)
      window.addEventListener('blur', cancelFocusReturn)
      return
    }
    cancelFocusReturn()
    const target = original.target
    if (
      !props.mutable ||
      target.ownerDocument !== document ||
      (document.activeElement !== document.body && document.activeElement !== original.previous) ||
      target.matches(':disabled') ||
      target.closest('[inert], [aria-hidden="true"], [hidden]') ||
      !target.getClientRects().length ||
      getComputedStyle(target).visibility === 'hidden' ||
      document.hidden ||
      [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].some(
        (panel) => panel.getClientRects().length && !panel.closest('[inert], [aria-hidden="true"]'),
      )
    )
      return
    target.focus({ preventScroll: true })
  },
  { flush: 'post' },
)
const canSave = computed(
  () =>
    dirty.value &&
    current.value &&
    props.mutable &&
    !locked.value &&
    url.value === '' &&
    label.value === '',
)
const name = (id: string) => {
  const known = props.agents.find((a) => a.id === id)
  return known
    ? agentLabel(known)
    : props.detail.participants.find((p) => p.source_id === id)?.name || '不可用的 Agent'
}
function reset() {
  meeting.value = props.detail.session.id
  version.value = props.detail.session.version
  agents.value = props.detail.participants
    .filter((p) => p.kind === 'agent' && p.active)
    .sort((a, b) => a.order - b.order)
    .map((p) => p.source_id)
  references.value = props.detail.references.map((r) => ({ ...r }))
  url.value = label.value = error.value = ''
  discard.value = false
  original.value = payload.value
}
function open(kind: 'participants' | 'references', event: MouseEvent) {
  if (!props.mutable || locked.value) return
  cancelFocusReturn()
  // Reading the directory can disable this button before the dialog captures
  // activeElement. Keep its original local target for normal close restoration.
  returnFocus.value = event.currentTarget instanceof HTMLElement ? event.currentTarget : null
  editing.value = kind
  reset()
  if (kind === 'participants') emit('agents', false)
}
function close() {
  if (submitting.value) return
  if (dirty.value && !props.unsettled) {
    discard.value = true
    return
  }
  cancelFocusReturn()
  if (props.busy && !props.unsettled && current.value && returnFocus.value) {
    pendingFocus = {
      target: returnFocus.value,
      previous: document.activeElement,
      project: props.detail.session.project_id,
      meeting: props.detail.session.id,
      version: props.detail.session.version,
    }
  }
  editing.value = null
}
function toggle(id: string, selected: boolean) {
  if (locked.value) return
  if (!selected) agents.value = agents.value.filter((a) => a !== id)
  else if (!agents.value.includes(id) && agents.value.length < 32) agents.value.push(id)
}
function move(index: number, offset: number) {
  if (locked.value) return
  const values = editing.value === 'participants' ? agents.value : references.value
  const destination = index + offset
  if (destination < 0 || destination >= values.length) return
  if (editing.value === 'participants') {
    const copy = [...agents.value],
      item = copy.splice(index, 1)[0]
    if (item !== undefined) copy.splice(destination, 0, item)
    agents.value = copy
  } else {
    const copy = [...references.value],
      item = copy.splice(index, 1)[0]
    if (item !== undefined) copy.splice(destination, 0, item)
    references.value = copy
  }
}
function addLink() {
  if (locked.value || references.value.length >= 256) return
  try {
    const proposed = {
      kind: 'link' as const,
      id: url.value,
      ...(label.value ? { label: label.value } : {}),
    }
    captureMeetingCommand({
      kind: 'references',
      meeting: meeting.value,
      input: { expected_version: version.value, references: [...references.value, proposed] },
    })
    references.value.push(proposed)
    url.value = label.value = error.value = ''
  } catch {
    error.value = '请输入不含用户名或密码的 http/https 链接，且不要重复添加。'
  }
}
function addResource(reference: MeetingReference) {
  if (locked.value || !current.value || !props.mutable || references.value.length >= 256) return
  try {
    captureMeetingCommand({
      kind: 'references',
      meeting: meeting.value,
      input: { expected_version: version.value, references: [...references.value, reference] },
    })
    references.value.push({ ...reference })
    error.value = ''
  } catch {
    error.value = '该资源无法加入，请确认它尚未被固定。'
  }
}
async function save() {
  if (!canSave.value || !editing.value) return
  const command: EditCommand =
    editing.value === 'participants'
      ? {
          kind: 'participants',
          meeting: meeting.value,
          input: { expected_version: version.value, agents: [...agents.value] },
        }
      : {
          kind: 'references',
          meeting: meeting.value,
          input: {
            expected_version: version.value,
            references: references.value.map((r) => ({ ...r })),
          },
        }
  submitting.value = true
  try {
    if (await props.submit(command)) editing.value = null
  } finally {
    submitting.value = false
  }
}
async function lookup() {
  if (!props.canLookup || props.busy || submitting.value) return
  submitting.value = true
  try {
    if (await props.lookup()) editing.value = null
  } finally {
    submitting.value = false
  }
}
watch(dirty, (value) => emit('dirty', value), { flush: 'sync' })
onScopeDispose(() => {
  cancelFocusReturn()
  emit('dirty', false)
})
</script>
<template>
  <div class="metadata-actions">
    <UiButton variant="ghost" :disabled="!mutable || locked" @click="open('participants', $event)"
      >编辑成员</UiButton
    >
    <UiButton variant="ghost" :disabled="!mutable || locked" @click="open('references', $event)"
      >管理引用</UiButton
    >
    <UiDialog
      :open="editing !== null"
      :title="editing === 'participants' ? '编辑参会成员' : '管理会议引用'"
      :fallback-focus="returnFocus"
      @update:open="!$event && close()"
    >
      <p v-if="editing === 'participants'" class="muted">
        调整只影响后续轮次。移除成员会保留其历史发言；重新加入后，将从后续轮次继续参与。
      </p>
      <p v-else class="muted">固定引用不会自动读取链接内容。调整后，已开始的发言仍沿用原引用。</p>
      <p v-if="!current" class="notice" role="status">会议已有新变化，请重新载入后再编辑。</p>
      <UiButton v-if="!current" :disabled="locked" @click="reset">重新载入当前内容</UiButton>
      <template v-if="editing === 'participants'">
        <p v-if="agentError" class="notice" role="status">{{ agentError }}</p>
        <UiButton v-if="agentError" :disabled="locked" @click="emit('agents', false)"
          >重新读取 Agent</UiButton
        >
        <div class="agent-options">
          <UiCheckbox
            v-for="agent in props.agents"
            :key="agent.id"
            :model-value="agents.includes(agent.id)"
            :disabled="locked || (!agents.includes(agent.id) && agents.length >= 32)"
            @update:model-value="toggle(agent.id, $event)"
            >{{ agentLabel(agent) }}</UiCheckbox
          >
        </div>
        <UiButton v-if="moreAgents" :disabled="locked" @click="emit('agents', true)"
          >更多 Agent</UiButton
        >
        <p v-if="!agents.length" class="muted">当前没有参会 Agent。添加后才能发送新一轮消息。</p>
        <ol class="ordered-items">
          <li v-for="(id, index) in agents" :key="id">
            <span>{{ name(id) }}</span>
            <UiButton
              variant="ghost"
              :disabled="locked || index === 0"
              :aria-label="'上移 ' + name(id)"
              @click="move(index, -1)"
              >上移</UiButton
            >
            <UiButton
              variant="ghost"
              :disabled="locked || index === agents.length - 1"
              :aria-label="'下移 ' + name(id)"
              @click="move(index, 1)"
              >下移</UiButton
            >
            <UiButton
              variant="ghost"
              :disabled="locked"
              :aria-label="'移除 ' + name(id)"
              @click="toggle(id, false)"
              >移除</UiButton
            >
          </li>
        </ol>
      </template>
      <template v-else>
        <p v-if="!references.length" class="muted">暂无固定引用。</p>
        <ol class="ordered-items">
          <li v-for="(reference, index) in references" :key="reference.kind + ':' + reference.id">
            <span>{{ reference.label || reference.id }}</span>
            <UiButton
              variant="ghost"
              :disabled="locked || index === 0"
              :aria-label="'上移引用 ' + (reference.label || reference.id)"
              @click="move(index, -1)"
              >上移</UiButton
            >
            <UiButton
              variant="ghost"
              :disabled="locked || index === references.length - 1"
              :aria-label="'下移引用 ' + (reference.label || reference.id)"
              @click="move(index, 1)"
              >下移</UiButton
            >
            <UiButton
              variant="ghost"
              :disabled="locked"
              :aria-label="'取消固定 ' + (reference.label || reference.id)"
              @click="references.splice(index, 1)"
              >取消固定</UiButton
            >
          </li>
        </ol>
        <MeetingResourcePicker
          :meeting-id="meeting"
          :disabled="locked || !mutable || !current || references.length >= 256"
          :selected="references"
          @choose="addResource"
        />
        <label class="field"
          >链接<UiInput
            v-model="url"
            type="url"
            placeholder="https://example.com/"
            :disabled="locked"
        /></label>
        <label class="field"
          >显示名称（可选）<UiInput v-model="label" maxlength="1024" :disabled="locked"
        /></label>
        <p v-if="error" class="notice" role="status">{{ error }}</p>
        <UiButton :disabled="locked || !url || references.length >= 256" @click="addLink"
          >加入引用列表</UiButton
        >
      </template>
      <p v-if="feedback" class="notice" role="status">{{ feedback }}</p>
      <p v-if="discard" class="notice" role="alert">修改尚未保存。要放弃这些修改吗？</p>
      <template #footer>
        <template v-if="discard">
          <UiButton @click="discard = false">继续编辑</UiButton>
          <UiButton variant="primary" @click="editing = null">放弃修改</UiButton>
        </template>
        <template v-else>
          <UiButton :disabled="submitting" @click="close">返回会议</UiButton>
          <UiButton
            v-if="canLookup"
            variant="primary"
            :disabled="busy || submitting"
            @click="lookup"
            >核对原操作</UiButton
          >
          <UiButton v-else variant="primary" :disabled="!canSave" @click="save">保存修改</UiButton>
        </template>
      </template>
    </UiDialog>
  </div>
</template>
<style scoped>
.metadata-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.agent-options,
.ordered-items {
  display: grid;
  gap: 8px;
  margin: 16px 0;
}
.ordered-items {
  list-style: none;
  padding: 0;
}
.ordered-items li {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.ordered-items li > span {
  flex: 1 1 160px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.field {
  display: grid;
  gap: 6px;
  margin: 12px 0;
}
.muted {
  color: var(--muted);
}
.notice {
  color: var(--warning);
}
</style>
