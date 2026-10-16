<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { UiButton, UiDialog } from '../ui'
import type { MeetingParticipant, MeetingProposal, MeetingProposalReview } from '../../api/meetings'
import type { MeetingProgress } from '../../composables/meeting-session'

const props = defineProps<{
  proposal: MeetingProposal
  participants: readonly MeetingParticipant[]
  inboxPath: string
  current: boolean
  busy: boolean
  progress: MeetingProgress | null
  message?: string
}>()
const emit = defineEmits<{
  review: [input: MeetingProposalReview]
  lookup: []
  reload: []
}>()
const open = ref(false)
const selected = ref<{ identity: string; input: MeetingProposalReview } | null>(null)
const submitted = ref(false)
const identity = computed(() =>
  JSON.stringify([
    props.proposal.session.project_id,
    props.proposal.session.id,
    props.proposal.session.version,
    props.proposal.source_execution_id,
    props.proposal.created_by_participant_id,
  ]),
)
const progress = computed(() => {
  const value = props.progress
  return value?.kind === 'proposal-review' &&
    value.project === props.proposal.session.project_id &&
    value.meeting === props.proposal.session.id
    ? value
    : null
})
const uncertain = computed(() => progress.value?.phase === 'uncertain')
const submitting = computed(() => progress.value?.phase === 'submitting')
const canReview = computed(
  () =>
    props.current &&
    !props.busy &&
    !uncertain.value &&
    !submitting.value &&
    props.proposal.session.status === 'proposed' &&
    !props.proposal.session.deleting,
)
const stale = computed(
  () =>
    selected.value !== null &&
    (selected.value.identity !== identity.value ||
      !props.current ||
      props.proposal.session.deleting ||
      props.proposal.session.status !== 'proposed'),
)
const creator = computed(
  () =>
    props.participants.find(
      (p) =>
        p.id === props.proposal.created_by_participant_id &&
        p.meeting_id === props.proposal.session.id &&
        p.kind === 'agent',
    )?.name ?? '原参会 Agent',
)
function choose(approve: boolean) {
  if (!canReview.value) return
  selected.value = {
    identity: identity.value,
    input: Object.freeze({ expected_version: props.proposal.session.version, approve }),
  }
  submitted.value = false
  open.value = true
}
function confirm() {
  if (!selected.value || stale.value || !canReview.value || submitted.value) return
  submitted.value = true
  open.value = false
  emit('review', selected.value.input)
}
</script>

<template>
  <section class="proposal-review" aria-label="会议提案" :aria-busy="submitting || undefined">
    <header>
      <h2>会议提案</h2>
      <RouterLink :to="inboxPath">返回收件箱</RouterLink>
    </header>
    <p class="proposal-origin">
      由 {{ creator }} 提议
      <span v-if="proposal.origin">
        · 来自{{ proposal.origin.kind === 'task' ? '任务' : '会议' }}</span
      >
    </p>
    <p class="proposal-reason">{{ proposal.session.proposal_content }}</p>
    <template v-if="uncertain">
      <p role="status">原决定的结果尚未确认，请查询原操作。</p>
      <UiButton :disabled="busy || !progress?.contextValid" @click="emit('lookup')"
        >查询原操作</UiButton
      >
    </template>
    <p v-else-if="submitting" role="status">正在提交提案决定…</p>
    <p v-else-if="proposal.session.deleting" role="status">会议正在删除，无法处理这份提案。</p>
    <p v-else-if="proposal.session.status === 'active'" role="status">
      会议已接受，可发送消息开始讨论。
    </p>
    <p v-else-if="proposal.session.status === 'archive'" role="status">
      提案已归档。恢复会议后可继续讨论。
    </p>
    <div v-else class="proposal-actions">
      <UiButton variant="primary" :disabled="!canReview" @click="choose(true)">接受会议</UiButton>
      <UiButton :disabled="!canReview" @click="choose(false)">拒绝并归档</UiButton>
    </div>
    <p v-if="message" role="status">{{ message }}</p>
    <UiButton v-if="!current && !uncertain" :disabled="busy" @click="emit('reload')"
      >重新读取提案</UiButton
    >
    <UiDialog
      v-model:open="open"
      :title="selected?.input.approve ? '接受这份会议提案？' : '拒绝并归档这份提案？'"
    >
      <p>
        {{
          selected?.input.approve
            ? '接受后可发送首条消息，开始会议。'
            : '提案会移入归档，之后仍可恢复会议。'
        }}
      </p>
      <p v-if="stale" role="status">提案已变化，请关闭确认并重新读取。</p>
      <template #footer>
        <UiButton :disabled="busy" @click="open = false">暂不决定</UiButton>
        <UiButton
          :variant="selected?.input.approve ? 'primary' : 'danger'"
          :disabled="stale || !canReview || submitted"
          @click="confirm"
        >
          {{ selected?.input.approve ? '确认接受' : '确认拒绝并归档' }}
        </UiButton>
      </template>
    </UiDialog>
  </section>
</template>

<style scoped>
.proposal-review {
  display: grid;
  gap: var(--content-gap);
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
header,
.proposal-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
header {
  justify-content: space-between;
}
h2,
p {
  margin: 0;
}
h2 {
  font-size: 1rem;
}
.proposal-origin {
  color: var(--muted);
}
.proposal-reason {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
