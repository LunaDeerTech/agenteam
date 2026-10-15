<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  UiButton,
  UiCheckbox,
  UiDialog,
  UiMenu,
  UiSelect,
  UiState,
  UiTextarea,
} from '../../components/ui'
import MeetingInfo from '../../components/meetings/MeetingInfo.vue'
import MeetingComposer from '../../components/meetings/MeetingComposer.vue'
import MeetingFilePreview from '../../components/meetings/MeetingFilePreview.vue'
import type { MeetingReference } from '../../api/meetings'
import { meetingLink } from '../../composables/meeting-composer'
import MeetingSessionList from '../../components/meetings/MeetingSessionList.vue'
import MeetingContributionExecution from '../../components/meetings/MeetingContributionExecution.vue'
import MeetingContributionVersions from '../../components/meetings/MeetingContributionVersions.vue'
import MeetingMetadataEditor from '../../components/meetings/MeetingMetadataEditor.vue'
import MeetingProposalReview from '../../components/meetings/MeetingProposalReview.vue'
import { agentLabel } from '../../api/agent-directory'
import { useProjectMeetings } from '../../composables/useProjectMeetings'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { installMeetingNavigation } from '../../router/auth'
const route = useRoute(),
  router = useRouter(),
  workspace = useProjectWorkspace()
const selected = computed(() =>
  typeof route.params.meeting_id === 'string' ? route.params.meeting_id : null,
)
const owner = useProjectMeetings(() => selected.value),
  { state, draft, busy, unsettled, canSend, canWrite, canControl, answers, pending, activeTurns } =
    owner
const listOpen = ref(false),
  infoOpen = ref(false),
  cancelTurn = ref<string | null>(null),
  cancelContribution = ref<{ turn: string; id: string; name: string } | null>(null),
  agentsOpen = ref(false),
  heading = ref<HTMLElement | null>(null),
  composer = ref<HTMLFormElement | null>(null),
  filePreview = ref<MeetingReference | null>(null),
  deleteTarget = ref<{ id: string; version: string; title: string } | null>(null)
const moreActions = computed(() => {
  const detail = state.detail
  if (!detail) return []
  return [
    ...(detail.session.status === 'proposed'
      ? []
      : [
          {
            id: 'archive',
            label: detail.session.status === 'archive' ? '恢复会议' : '归档会议',
            disabled: !canControl.value,
          },
        ]),
    { id: 'delete', label: '删除会议…', danger: true, disabled: !owner.canDelete.value },
  ]
})
function moreAction(action: string) {
  if (!state.detail) return
  if (action === 'delete' && owner.canDelete.value) {
    deleteTarget.value = {
      id: state.detail.session.id,
      version: state.detail.session.version,
      title: state.detail.session.title || '这场会议',
    }
  } else if (action === 'archive') {
    void owner.control(state.detail.session.status === 'archive' ? 'unarchive' : 'archive')
  }
}
async function confirmDelete() {
  const target = deleteTarget.value
  if (!target || target.id !== selected.value) return
  deleteTarget.value = null
  await owner.deleteMeeting(target.version)
}
watch(
  [owner.scope, selected],
  () => {
    deleteTarget.value = null
    filePreview.value = null
  },
  { flush: 'sync' },
)
const removeGuard = installMeetingNavigation(router, owner)
onScopeDispose(removeGuard)
const name = (id: string) => state.detail?.participants.find((p) => p.id === id)?.name || '参会成员'
const chosenName = (id: string) => {
  const a = state.agents.find((a) => a.id === id)
  return a ? agentLabel(a) : id
}
const labels: Record<string, string> = {
  pending: '等待发言',
  waiting_for_agent: 'Agent 正在处理其他工作，等待空闲',
  running: '正在发言',
  completed: '已完成',
  failed: '发言失败',
  cancelled: '已取消',
  skipped: '已跳过',
  queued: '排队中',
  finalizing: '正在整理摘要',
}
async function select(id: string | null) {
  const target = workspace.paths.value.home + '/meetings' + (id ? '/' + id : '')
  if (route.path === target) {
    listOpen.value = false
    return
  }
  const result = await router.push(target)
  if (!result) {
    listOpen.value = false
    await nextTick()
    heading.value?.focus()
  }
}
async function send(lookup = false) {
  const id = await owner.submit(lookup)
  if (id && id !== selected.value) await select(id)
}
async function reply(id: string) {
  if (!owner.replyToMessage(id)) return
  await nextTick()
  composer.value?.querySelector<HTMLElement>('[role="textbox"]')?.focus()
}
async function cancelReply() {
  if (!owner.cancelReply()) return
  await nextTick()
  composer.value?.querySelector<HTMLElement>('[role="textbox"]')?.focus()
}
function referenceActions(reference: MeetingReference) {
  return [
    {
      id: 'open',
      label:
        '打开' + { task: '任务', knowledge: '文档', link: '链接', file: '文件' }[reference.kind],
    },
    {
      id: 'pin',
      label: owner.referencePinned(reference) ? '已置顶到会议引用' : '置顶到会议引用',
      disabled: !canWrite.value || state.metadataDirty || owner.referencePinned(reference),
    },
  ]
}
function referenceAction(action: string, reference: MeetingReference) {
  if (action === 'pin') {
    void owner.pinReference(reference)
    return
  }
  if (action !== 'open') return
  if (reference.kind === 'link') {
    const safe = meetingLink(reference.id)
    if (safe) window.open(safe.id, '_blank', 'noopener,noreferrer')
  } else if (reference.kind === 'file') {
    filePreview.value = reference
  } else if (reference.kind === 'task' || reference.kind === 'knowledge') {
    void router.push(
      workspace.paths.value.home +
        (reference.kind === 'task' ? '/tasks/' : '/knowledge/') +
        reference.id,
    )
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (owner.dirty.value || unsettled.value) {
    event.preventDefault()
    event.returnValue = ''
  }
}
window.addEventListener('beforeunload', beforeUnload)
onScopeDispose(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
<template>
  <section class="meetings-page" aria-label="项目会议">
    <aside class="meetings-sidebar">
      <MeetingSessionList
        :items="state.items"
        :selected="selected"
        :disabled="busy || unsettled"
        @select="select"
      />
      <p v-if="state.message" class="error" role="status">{{ state.message }}</p>
      <UiButton v-if="state.phase === 'error'" :disabled="busy" @click="owner.refresh()"
        >重新读取</UiButton
      >
    </aside>
    <main class="meeting-main" :class="{ preparing: !selected }">
      <header class="meeting-heading">
        <UiButton class="mobile-list" variant="ghost" @click="listOpen = true">会议列表</UiButton>
        <h1 ref="heading" tabindex="-1">
          {{
            selected
              ? state.deletion?.phase === 'deleted'
                ? '会议已删除'
                : state.detail?.session.title || '新会议'
              : '开始一场会议'
          }}
        </h1>
        <div class="heading-actions">
          <MeetingMetadataEditor
            v-if="state.detail"
            :key="state.detail.session.id"
            :detail="state.detail"
            :agents="state.agents"
            :more-agents="!!state.agentNext"
            :agent-error="state.agentMessage"
            :mutable="canWrite"
            :busy="busy"
            :unsettled="unsettled"
            :can-lookup="
              pending?.phase === 'uncertain' &&
              !!pending.contextValid &&
              ['participants', 'references'].includes(pending.kind)
            "
            :feedback="state.feedback"
            :submit="owner.updateMetadata"
            :lookup="() => owner.submit(true)"
            @agents="owner.loadAgents"
            @dirty="state.metadataDirty = $event"
          />
          <span v-if="owner.realtime.value" class="muted" role="status">{{
            owner.realtime.value.phase === 'ready'
              ? '实时更新'
              : owner.realtime.value.phase === 'connecting'
                ? '正在连接更新'
                : owner.realtime.value.phase === 'loading'
                  ? '正在同步'
                  : '更新已暂停，显示最近确认内容'
          }}</span>
          <UiButton
            v-if="owner.realtime.value?.phase === 'stale'"
            variant="ghost"
            :disabled="busy"
            @click="owner.reconnect()"
            >重新连接</UiButton
          >
          <span v-if="state.polling" class="muted" role="status">正在更新进展</span>
          <UiButton v-if="selected" variant="ghost" :disabled="busy" @click="owner.loadDetail()"
            >刷新</UiButton
          ><UiMenu
            v-if="state.detail"
            label="更多操作"
            :actions="moreActions"
            @select="moreAction"
          />
          <UiButton v-if="state.detail" class="mobile-info" variant="ghost" @click="infoOpen = true"
            >详情</UiButton
          >
        </div>
      </header>
      <section
        v-if="
          owner.scope.value &&
          pending?.kind === 'delete' &&
          ['submitting', 'uncertain', 'processing'].includes(pending.phase)
        "
        class="deletion-progress"
        aria-label="会议删除进度"
        role="status"
      >
        <p>
          {{
            pending.phase === 'submitting'
              ? '正在提交删除请求…'
              : pending.phase === 'uncertain'
                ? '删除结果尚未确认，请查询原操作。'
                : '删除已接受，正在停止相关工作。'
          }}
        </p>
        <p v-if="state.deletion?.error_code">删除尚未完成，查询原操作可确认最新进度。</p>
        <UiButton
          v-if="pending.phase !== 'submitting'"
          :disabled="busy || !pending.contextValid"
          @click="owner.submit(true)"
          >查询原操作</UiButton
        >
      </section>
      <div v-if="!owner.scope.value" class="content-state">
        <UiState kind="loading" title="正在确认项目访问身份" />
      </div>
      <div
        v-else-if="selected && state.detailPhase === 'loading' && !state.detail"
        class="content-state"
      >
        <UiState kind="loading" title="正在读取会议" />
      </div>
      <div
        v-else-if="selected && !state.detail && state.detailPhase === 'error'"
        class="content-state"
      >
        <UiState kind="error" title="会议读取失败" :description="state.detailMessage"
          ><UiButton :disabled="busy" @click="owner.loadDetail()">重新读取</UiButton></UiState
        >
      </div>
      <div v-else-if="state.deletion?.phase === 'deleted'" class="content-state" role="status">
        <h2>会议已删除</h2>
        <UiButton @click="select(null)">返回会议列表</UiButton>
      </div>
      <template v-else>
        <div v-if="selected && state.detail" class="meeting-content">
          <div
            class="timeline"
            aria-label="会议消息"
            aria-live="polite"
            aria-relevant="additions text"
          >
            <p v-if="state.detailMessage" class="error" role="status">
              {{ state.detailMessage }} 以下保留上次完整读取的内容。
            </p>
            <p v-if="state.detail.session.deleting" class="notice">会议正在删除，当前只读。</p>
            <p v-else-if="state.detail.session.status === 'archive'" class="notice">会议已归档。</p>
            <MeetingProposalReview
              v-if="state.proposal"
              :proposal="state.proposal"
              :participants="state.detail.participants"
              :inbox-path="workspace.paths.value.home + '/inbox'"
              :current="owner.proposalCurrent.value"
              :busy="busy"
              :progress="pending"
              :message="state.feedback"
              @review="owner.reviewProposal"
              @lookup="owner.submit(true)"
              @reload="owner.loadDetail()"
            />
            <div v-else-if="state.proposalPhase === 'error'" role="status">
              <p>{{ state.proposalMessage }}</p>
              <UiButton :disabled="busy" @click="owner.loadDetail()">重新读取提案</UiButton>
            </div>
            <p v-else-if="state.proposalPhase === 'loading'" role="status">正在读取会议提案…</p>
            <section
              v-for="turn in state.detail.turns"
              :key="turn.id"
              class="turn"
              :aria-label="(turn.mode === 'parallel' ? '并行' : '串行') + '轮次'"
            >
              <div class="turn-heading">
                <span>{{ turn.mode === 'parallel' ? '并行' : '串行' }}</span
                ><span>{{ labels[turn.status] || turn.status }}</span
                ><time :datetime="turn.created_at">{{
                  new Date(turn.created_at).toLocaleString('zh-CN')
                }}</time>
                <UiButton
                  v-if="['queued', 'running', 'finalizing'].includes(turn.status)"
                  variant="ghost"
                  :disabled="!canControl"
                  @click="cancelTurn = turn.id"
                  >取消本轮</UiButton
                >
              </div>
              <article
                v-for="contribution in turn.contributions"
                :key="contribution.id"
                class="contribution"
                :class="contribution.kind"
              >
                <header>
                  <span class="avatar" aria-hidden="true">{{
                    contribution.kind === 'agent' ? 'A' : 'U'
                  }}</span
                  ><strong>{{ name(contribution.participant_id) }}</strong
                  ><span class="identity">{{
                    contribution.kind === 'agent' ? 'Agent' : '用户'
                  }}</span>
                </header>
                <div class="message-body">
                  <p v-if="contribution.message?.reply_to_message_id" class="reply-quote">
                    回复 {{ owner.messagePreview(contribution.message.reply_to_message_id) }}
                  </p>
                  <template v-if="contribution.message"
                    ><template v-for="(node, n) in contribution.message.content_nodes" :key="n"
                      ><span v-if="node.type === 'text'" class="message-text">{{ node.text }}</span
                      ><span v-else-if="node.type === 'mention'" class="inline-reference"
                        >@{{ name(node.participant_id || '') }}</span
                      ><UiMenu
                        v-else
                        class="message-reference"
                        :label="
                          node.reference.label ||
                          (node.type === 'link'
                            ? node.reference.id
                            : { task: '任务', knowledge: '知识文档', file: '文件' }[node.type])
                        "
                        :actions="referenceActions(node.reference)"
                        @select="referenceAction($event, node.reference)" /></template
                  ></template>
                  <p v-else class="muted">
                    {{
                      state.detail.decisions.some(
                        (decision) =>
                          decision.contribution_id === contribution.id &&
                          decision.status === 'pending',
                      )
                        ? '等待用户决定'
                        : labels[contribution.status]
                    }}
                  </p>
                  <UiButton
                    v-if="contribution.message"
                    variant="ghost"
                    :disabled="!canWrite"
                    :aria-label="'回复 ' + name(contribution.participant_id) + ' 的消息'"
                    @click="reply(contribution.message.id)"
                    >回复</UiButton
                  >
                  <UiButton
                    v-if="
                      turn.status === 'running' &&
                      contribution.status === 'waiting_for_agent' &&
                      !contribution.execution_id
                    "
                    variant="ghost"
                    :disabled="!canControl"
                    @click="owner.control('skip-contribution', turn.id, contribution.id)"
                    >跳过此 Agent</UiButton
                  >
                  <p
                    v-if="
                      contribution.message && ['failed', 'cancelled'].includes(contribution.status)
                    "
                    class="notice"
                  >
                    {{ labels[contribution.status] }}
                  </p>
                  <p
                    v-if="contribution.cancel_requested && contribution.status === 'running'"
                    class="notice"
                    role="status"
                  >
                    取消已请求，正在等待此 Agent 停止。
                  </p>
                  <UiButton
                    v-else-if="
                      ['running', 'completed'].includes(turn.status) &&
                      contribution.kind === 'agent' &&
                      contribution.status === 'running' &&
                      contribution.execution_id
                    "
                    variant="ghost"
                    :disabled="!canControl"
                    @click="
                      cancelContribution = {
                        turn: turn.id,
                        id: contribution.id,
                        name: name(contribution.participant_id),
                      }
                    "
                    >取消此 Agent 发言</UiButton
                  >
                  <MeetingContributionVersions
                    v-if="contribution.kind === 'agent'"
                    :contribution="contribution"
                    :mutable="
                      owner.canRestart.value && ['running', 'completed'].includes(turn.status)
                    "
                    :participant-name="name"
                    :can-reply="canWrite"
                    @restart="owner.restartContribution"
                    @reply="reply"
                  />
                  <MeetingContributionExecution
                    v-if="
                      contribution.kind === 'agent' &&
                      contribution.execution_id &&
                      owner.scope.value
                    "
                    :project-i-d="owner.scope.value.projectID"
                    :meeting-i-d="state.detail.session.id"
                    :contribution="contribution"
                    :mutable="canControl && !contribution.cancel_requested"
                    :disabled="busy"
                    @refresh="owner.loadDetail()"
                  />
                </div>
              </article>
              <p v-if="turn.summary_error" class="error" role="status">
                摘要更新失败，后续轮次等待收尾恢复。
                <UiButton
                  v-if="turn.status === 'finalizing'"
                  :disabled="!canControl"
                  @click="owner.control('retry-summary', turn.id)"
                  >重试摘要</UiButton
                >
              </p>
            </section>
            <section v-for="decision in state.detail.decisions" :key="decision.id" class="decision">
              <h2>{{ decision.status === 'pending' ? '等待用户决定' : '用户决定' }}</h2>
              <p>{{ decision.question }}</p>
              <ul v-if="decision.options.length">
                <li v-for="option in decision.options" :key="option">{{ option }}</li>
              </ul>
              <p v-if="decision.answer">{{ decision.answer }}</p>
              <form
                v-if="decision.status === 'pending'"
                class="decision-response"
                @submit.prevent="owner.respond(decision, 'answer')"
              >
                <label :for="'decision-answer-' + decision.id">你的决定</label>
                <UiTextarea
                  :id="'decision-answer-' + decision.id"
                  v-model="answers[decision.id]"
                  rows="3"
                  :disabled="!canControl"
                  placeholder="回答这个问题，也可以跳过让 Agent 继续判断。"
                />
                <div class="decision-actions">
                  <UiButton
                    type="submit"
                    variant="primary"
                    :disabled="!canControl || !(answers[decision.id] || '').trim()"
                    >提交回答</UiButton
                  ><UiButton :disabled="!canControl" @click="owner.respond(decision, 'skip')"
                    >跳过这个问题</UiButton
                  >
                </div>
              </form>
              <p v-else class="muted">
                {{
                  decision.status === 'answered'
                    ? '已回答'
                    : decision.status === 'skipped'
                      ? '已跳过，Agent 将继续处理'
                      : '问题已随本轮取消'
                }}
              </p>
            </section>
            <p v-if="!state.detail.turns.length" class="muted">尚无发言。</p>
          </div>
          <aside class="meeting-details">
            <MeetingInfo :detail="state.detail" @open="referenceAction('open', $event)" />
          </aside>
        </div>
        <div class="composer-area" :class="{ centered: !selected }">
          <p v-if="!selected" class="intro">选择参会 Agent，发出第一条消息，让讨论开始。</p>
          <form ref="composer" class="composer" @submit.prevent="send()">
            <div v-if="draft.replyTo" class="reply-preview" role="status">
              <p>回复 {{ owner.messagePreview(draft.replyTo) }}</p>
              <UiButton variant="ghost" :disabled="busy || unsettled" @click="cancelReply"
                >取消回复</UiButton
              >
            </div>
            <label class="sr-only" for="meeting-message">{{
              selected ? '发送到会议' : '会议首条消息'
            }}</label>
            <MeetingComposer
              :key="
                (owner.scope.value?.projectID || '') +
                ':' +
                (owner.scope.value?.generation || '') +
                ':' +
                (owner.scope.value?.readGeneration || '') +
                ':' +
                (owner.scope.value?.identity.epoch || '') +
                ':' +
                (selected || '')
              "
              v-model="draft.nodes"
              :meeting-id="selected"
              :upload-pending="owner.fileUploadPending.value"
              :mentions="owner.mentionChoices.value"
              :label="selected ? '发送到会议' : '会议首条消息'"
              :disabled="
                busy ||
                unsettled ||
                !owner.scope.value ||
                workspace.detail.project?.lifecycle !== 'active'
              "
              :placeholder="selected ? '继续讨论…' : '你想和 Agent 讨论什么？'"
            >
              <div class="composer-footer">
                <UiSelect
                  v-model="draft.mode"
                  label="发言模式"
                  :options="[
                    { value: 'parallel', label: '并行发言' },
                    { value: 'sequential', label: '串行发言' },
                  ]"
                  :disabled="busy || unsettled"
                />
                <div class="send-actions">
                  <UiButton
                    v-if="!selected"
                    variant="ghost"
                    :disabled="busy || unsettled"
                    @click="agentsOpen = true"
                    >{{
                      draft.agents.length ? draft.agents.length + ' 位 Agent' : '编排 Agent'
                    }}</UiButton
                  ><UiButton
                    type="submit"
                    variant="primary"
                    :disabled="!canSend"
                    :state="pending?.phase === 'submitting' ? 'loading' : 'idle'"
                    loading-label="正在提交"
                    >发送</UiButton
                  >
                </div>
              </div>
            </MeetingComposer>
          </form>
          <UiCheckbox v-if="selected && activeTurns" v-model="draft.interrupt" :disabled="!canWrite"
            >中断当前轮次并发送新消息</UiCheckbox
          >
          <p v-if="owner.fileUploadPending.value" class="notice" role="status">
            请先确认原上传的结果，再发送这条消息。
          </p>
          <p
            v-if="
              !selected && !state.agents.length && state.phase === 'ready' && !state.agentMessage
            "
            class="muted"
          >
            项目中还没有 Agent。<RouterLink :to="workspace.paths.value.home + '/settings/agents'"
              >前往创建</RouterLink
            >
          </p>
          <p v-if="state.agentMessage" class="error">
            {{ state.agentMessage }}
            <UiButton variant="ghost" :disabled="busy" @click="owner.loadAgents()">重试</UiButton>
          </p>
          <div
            v-if="
              pending?.phase === 'uncertain' &&
              pending.kind !== 'delete' &&
              !(pending.kind === 'proposal-review' && state.proposal)
            "
            class="uncertain"
            role="status"
          >
            <p>{{ state.feedback || '发送结果待确认，请查询原操作。' }}</p>
            <UiButton :disabled="busy || !pending.contextValid" @click="send(true)"
              >查询原操作</UiButton
            >
          </div>
          <p v-else-if="state.feedback" class="feedback" role="status">{{ state.feedback }}</p>
        </div>
      </template>
    </main>
    <UiDialog v-model:open="listOpen" title="会议" drawer
      ><MeetingSessionList
        :items="state.items"
        :selected="selected"
        :disabled="busy || unsettled"
        @select="select"
    /></UiDialog>
    <UiDialog v-model:open="infoOpen" title="会议详情" drawer
      ><MeetingInfo
        v-if="state.detail"
        :detail="state.detail"
        @open="referenceAction('open', $event)"
    /></UiDialog>
    <MeetingFilePreview
      v-if="selected && filePreview"
      :open="true"
      :meeting-id="selected"
      :artifact-id="filePreview.id"
      :label="filePreview.label || '文件引用'"
      @update:open="!$event && (filePreview = null)"
    />
    <UiDialog v-model:open="agentsOpen" title="编排参会 Agent"
      ><p class="muted">最多 32 位。串行发言将按下方顺序进行。</p>
      <div class="agent-options">
        <div v-for="a in state.agents" :key="a.id" class="agent-choice">
          <UiCheckbox
            :model-value="draft.agents.includes(a.id)"
            :disabled="
              busy || unsettled || (!draft.agents.includes(a.id) && draft.agents.length >= 32)
            "
            @update:model-value="owner.toggleAgent(a.id, $event)"
            ><span class="agent-label"
              ><span
                v-if="a.tag_color"
                class="agent-tag"
                :style="{ background: a.tag_color }"
              ></span
              >{{ agentLabel(a) }}<small>@{{ a.name }}</small></span
            ></UiCheckbox
          >
          <p v-if="a.description">{{ a.description }}</p>
        </div>
      </div>
      <UiButton v-if="state.agentNext" :disabled="busy" @click="owner.loadAgents(true)"
        >更多 Agent</UiButton
      >
      <ol v-if="draft.agents.length" class="agent-order">
        <li v-for="(id, index) in draft.agents" :key="id">
          <span>{{ chosenName(id) }}</span
          ><UiButton
            variant="ghost"
            :disabled="index === 0 || busy || unsettled"
            :aria-label="'上移 ' + chosenName(id)"
            @click="owner.moveAgent(index, -1)"
            >上移</UiButton
          ><UiButton
            variant="ghost"
            :disabled="index === draft.agents.length - 1 || busy || unsettled"
            :aria-label="'下移 ' + chosenName(id)"
            @click="owner.moveAgent(index, 1)"
            >下移</UiButton
          >
        </li>
      </ol>
      <template #footer
        ><UiButton variant="primary" @click="agentsOpen = false">完成</UiButton></template
      ></UiDialog
    >
    <UiDialog
      :open="cancelTurn !== null"
      title="取消本轮讨论？"
      @update:open="!$event && (cancelTurn = null)"
    >
      <p>停止本轮尚未完成的发言，并取消等待回答的问题。已发表的消息会保留。</p>
      <template #footer
        ><UiButton @click="cancelTurn = null">继续讨论</UiButton
        ><UiButton
          variant="danger"
          :disabled="!canControl"
          @click="
            async () => {
              const turn = cancelTurn
              cancelTurn = null
              if (turn) await owner.control('cancel-turn', turn)
            }
          "
          >取消本轮</UiButton
        ></template
      >
    </UiDialog>
    <UiDialog
      :open="cancelContribution !== null"
      title="取消此 Agent 发言？"
      @update:open="!$event && (cancelContribution = null)"
    >
      <p>
        停止 {{ cancelContribution?.name }} 当前这次发言，并取消它等待的审批或问题。其他 Agent
        会继续本轮讨论，已发表的消息会保留。
      </p>
      <template #footer>
        <UiButton @click="cancelContribution = null">继续发言</UiButton>
        <UiButton
          variant="danger"
          :disabled="!canControl"
          @click="
            async () => {
              const target = cancelContribution
              cancelContribution = null
              if (target) await owner.control('cancel-contribution', target.turn, target.id)
            }
          "
          >取消此 Agent</UiButton
        >
      </template>
    </UiDialog>
    <UiDialog
      :open="deleteTarget !== null"
      title="删除这场会议？"
      :fallback-focus="heading"
      @update:open="!$event && (deleteTarget = null)"
    >
      <p>删除“{{ deleteTarget?.title }}”会停止相关工作并移除会议内容。此操作无法撤销。</p>
      <p>删除请求被接受后仍需等待停止和清理完成。</p>
      <template #footer>
        <UiButton @click="deleteTarget = null">保留会议</UiButton>
        <UiButton variant="danger" :disabled="!owner.canDelete.value" @click="confirmDelete"
          >确认删除会议</UiButton
        >
      </template>
    </UiDialog>
    <UiDialog
      :open="state.leaveOpen"
      title="保留未提交的输入？"
      @update:open="!$event && owner.answerLeave(false)"
      ><p>当前消息或回答尚未提交。继续编辑，或放弃草稿后离开。</p>
      <template #footer
        ><UiButton @click="owner.answerLeave(false)">继续编辑</UiButton
        ><UiButton variant="danger" @click="owner.answerLeave(true)">放弃草稿</UiButton></template
      ></UiDialog
    >
  </section>
</template>
<style scoped>
.deletion-progress {
  padding: 16px 24px;
  border-bottom: 1px solid var(--border);
  display: grid;
  gap: 8px;
  justify-items: start;
}

.decision-response {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}
.decision-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.meetings-page {
  display: flex;
  flex: 1;
  min-height: 0;
  min-width: 0;
  background: var(--canvas);
}
.meetings-sidebar {
  flex: 0 0 236px;
  padding: 22px 16px;
  border-right: 1px solid var(--border);
  overflow-y: auto;
}
.meeting-main {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.meeting-heading {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 64px;
  padding: 14px 24px;
  border-bottom: 1px solid var(--border);
  flex: none;
}
h1 {
  font-size: 16px;
  line-height: 1.4;
  font-weight: 600;
  margin: 0;
  overflow-wrap: anywhere;
  flex: 1;
}
.heading-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}
.heading-actions span {
  font-size: 12px;
}
.meeting-content {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.timeline {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 24px;
}
.meeting-details {
  flex: 0 0 250px;
  padding: 24px 20px;
  overflow-y: auto;
  border-left: 1px solid var(--border);
}
.turn + .turn {
  margin-top: 32px;
}
.turn-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 11px;
  color: var(--muted);
  margin-bottom: 22px;
}
.turn-heading time {
  margin-left: auto;
}
.contribution {
  margin-block: 20px 28px;
}
.contribution header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.avatar {
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: 6px;
  background: var(--surface-alt);
  font-size: 11px;
  color: var(--muted);
}
.identity {
  color: var(--muted);
  font-size: 11px;
}
.message-body {
  margin: 8px 0 0 32px;
  font-size: 14px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.message-text {
  white-space: pre-wrap;
}
.reply-quote {
  margin: 0 0 8px;
  padding-left: 10px;
  border-left: 2px solid var(--border);
  color: var(--muted);
  font-size: 12px;
}
.reply-preview {
  display: flex;
  align-items: start;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  font-size: 12px;
}
.reply-preview p {
  flex: 1;
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.reply-preview :deep(button) {
  flex: none;
}
.inline-reference {
  border-radius: 4px;
  background: var(--surface-alt);
  padding: 2px 5px;
}
.message-body :deep(.message-reference) {
  display: inline-flex;
  min-height: 0;
  max-width: 100%;
  padding: 1px 5px;
  line-height: inherit;
  white-space: normal;
  text-align: left;
  vertical-align: baseline;
}
.decision {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  margin-top: 20px;
  overflow-wrap: anywhere;
}
.decision h2 {
  font-size: 13px;
}
.composer-area {
  padding: 16px 24px 20px;
  flex: none;
  display: grid;
  gap: 10px;
}
.centered {
  margin: auto;
  width: min(100%, 760px);
  padding-block: 48px;
}
.intro {
  margin: 0 0 14px;
  color: var(--muted);
  font-size: 14px;
}
.composer {
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
  overflow: hidden;
}
.composer:focus-within {
  border-color: var(--text-secondary, var(--muted));
}
.composer :deep(textarea) {
  display: block;
  width: 100%;
  resize: vertical;
  min-height: 112px;
  max-height: 300px;
  padding: 16px;
  border: 0;
  box-shadow: none;
  background: transparent;
  outline: none;
}
.composer-footer {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0;
}
.send-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.composer-footer :deep(.ui-select) {
  width: auto;
}
.muted {
  color: var(--muted);
}
.error {
  color: var(--danger);
}
.notice {
  color: var(--warning);
}
.feedback,
.error,
.notice,
.muted {
  font-size: 13px;
  overflow-wrap: anywhere;
}
.uncertain {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px;
  background: var(--warning-bg);
}
.uncertain p {
  margin: 0 0 10px;
  font-size: 13px;
}
.content-state {
  padding: 32px 24px;
}
.agent-options {
  display: grid;
  gap: 14px;
  margin-block: 16px;
}
.agent-choice p {
  color: var(--muted);
  font-size: 12px;
  padding-left: 25px;
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.agent-label {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.agent-label small {
  color: var(--muted);
}
.agent-tag {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}
.agent-order {
  padding-left: 20px;
  border-top: 1px solid var(--border);
}
.agent-order li {
  padding-top: 8px;
}
.agent-order span {
  display: inline-block;
  margin-right: 12px;
  overflow-wrap: anywhere;
}
.mobile-list,
.mobile-info {
  display: none;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
}
@media (max-width: 1100px) {
  .meeting-details {
    display: none;
  }
  .mobile-info {
    display: inline-flex;
  }
  .meetings-sidebar {
    flex-basis: 210px;
  }
}
@media (max-width: 720px) {
  .meetings-sidebar {
    display: none;
  }
  .mobile-list {
    display: inline-flex;
  }
  .meeting-heading {
    padding: 12px 16px;
    flex-wrap: wrap;
  }
  .heading-actions {
    margin-left: auto;
  }
  .heading-actions span {
    display: none;
  }
  .timeline {
    padding: 20px 16px;
  }
  .composer-area {
    padding: 12px 16px 16px;
  }
  .composer-footer {
    flex-wrap: wrap;
  }
  .send-actions {
    margin-left: auto;
  }
  .centered {
    padding-block: 32px;
  }
  .turn-heading time {
    margin-left: 0;
  }
}
</style>
