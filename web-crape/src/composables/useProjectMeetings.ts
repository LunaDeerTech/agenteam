import { computed, onScopeDispose, reactive, ref, shallowRef, watch } from 'vue'
import {
  meetingReferenceKey,
  sameMeetingMention,
  type MeetingMentionChoice,
} from './meeting-composer'
import type { MeetingSubscription } from './meeting-realtime'
import type { MeetingRealtimeSnapshot } from '../api/meeting-realtime'
import { AccountFailure } from '../api/client'
import type {
  MeetingDetail,
  MeetingMode,
  MeetingSession,
  MeetingCommand,
  MeetingControl,
  MeetingDecision,
  MeetingContribution,
  MeetingProposal,
  MeetingProposalReview,
  MeetingDeletionReceipt,
  MeetingMessage,
  MeetingNode,
  MeetingReference,
} from '../api/meetings'
import { agentLabel, type DirectoryAgent } from '../api/agent-directory'
import { useProjectWorkspace } from './useProjectWorkspace'
import { useSession } from './useSession'

export function useProjectMeetings(
  selected: () => string | null,
  workspace = useProjectWorkspace(),
  auth = useSession(),
) {
  const state = reactive({
    phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    items: [] as readonly MeetingSession[],
    message: '',
    detail: null as MeetingDetail | null,
    detailPhase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    detailMessage: '',
    proposal: null as MeetingProposal | null,
    proposalPhase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    proposalMessage: '',
    agents: [] as readonly DirectoryAgent[],
    agentNext: '',
    agentMessage: '',
    feedback: '',
    polling: false,
    leaveOpen: false,
    metadataDirty: false,
    deletion: null as MeetingDeletionReceipt | null,
  })
  const content = ref<MeetingNode[]>([{ type: 'text', text: '' }])
  const draft = reactive({
    get text() {
      return content.value
        .filter((node) => node.type === 'text')
        .map((node) => node.text)
        .join('')
    },
    set text(value: string) {
      content.value = [{ type: 'text', text: value }]
    },
    get nodes() {
      return content.value
    },
    set nodes(value: MeetingNode[]) {
      content.value = value
    },
    mode: 'parallel' as MeetingMode,
    agents: [] as string[],
    interrupt: false,
    replyTo: '',
  })
  const answers = reactive<Record<string, string>>({})
  const timeline = shallowRef<MeetingSubscription | null>(null)
  const realtime = computed(() => timeline.value?.state ?? null)
  let appliedSnapshot: MeetingRealtimeSnapshot | null = null
  const scope = computed(() => workspace.currentReadContext.value)
  const pending = computed(() => {
    const p = auth.meetings.progress
    return p?.project === scope.value?.projectID ? p : null
  })
  const fileUploadPending = computed(() => {
    const progress = auth.artifactUploads?.progress
    return (
      !!progress &&
      progress.projectID === scope.value?.projectID &&
      progress.meetingID === selected() &&
      !['confirmed', 'cancelled', 'rejected'].includes(progress.phase)
    )
  })
  const unsettled = computed(
    () =>
      pending.value?.phase === 'submitting' ||
      pending.value?.phase === 'uncertain' ||
      pending.value?.phase === 'processing' ||
      (!!auth.executionApproval?.meetingProgress &&
        auth.executionApproval.meetingProgress.projectID === scope.value?.projectID &&
        auth.executionApproval.meetingProgress.meetingID === selected() &&
        ['submitting', 'uncertain'].includes(auth.executionApproval.meetingProgress.phase)),
  )
  const busy = computed(
    () => auth.state.busy || state.phase === 'loading' || state.detailPhase === 'loading',
  )
  const dirty = computed(
    () =>
      draft.nodes.some((node) => node.type !== 'text' || node.text !== '') ||
      draft.replyTo !== '' ||
      state.metadataDirty ||
      fileUploadPending.value ||
      (!selected() && draft.agents.length > 0) ||
      Object.values(answers).some((v) => v !== ''),
  )
  const activeTurns = computed(
    () =>
      state.detail?.turns.some((t) => ['queued', 'running', 'finalizing'].includes(t.status)) ??
      false,
  )
  const activeWork = computed(
    () =>
      activeTurns.value ||
      !!state.detail?.turns.some(
        (t) =>
          t.status === 'completed' &&
          t.contributions.some(
            (c) =>
              c.kind === 'agent' && ['pending', 'waiting_for_agent', 'running'].includes(c.status),
          ),
      ),
  )
  const canRestart = computed(
    () => canWrite.value && !state.detail?.turns.some((t) => t.status === 'finalizing'),
  )
  const canWrite = computed(
    () =>
      !!scope.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      (!selected() ||
        (state.detailPhase === 'ready' &&
          state.detail?.session.status === 'active' &&
          !state.detail.session.deleting)) &&
      !busy.value &&
      !unsettled.value,
  )
  const canControl = computed(
    () =>
      !!scope.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.detailPhase === 'ready' &&
      !!state.detail &&
      state.detail.session.status !== 'proposed' &&
      !state.detail.session.deleting &&
      !busy.value &&
      !unsettled.value,
  )
  const canDelete = computed(
    () =>
      !!scope.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.detailPhase === 'ready' &&
      !!state.detail &&
      !state.detail.session.deleting &&
      !busy.value &&
      !unsettled.value,
  )
  const proposalCurrent = computed(
    () =>
      !!scope.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.detailPhase === 'ready' &&
      state.proposalPhase === 'ready' &&
      !!state.proposal &&
      !!state.detail &&
      state.proposal.session.project_id === scope.value.projectID &&
      state.proposal.session.id === selected() &&
      state.proposal.session.version === state.detail?.session.version &&
      state.proposal.session.status === state.detail?.session.status &&
      !state.detail.session.deleting,
  )
  const canReviewProposal = computed(
    () =>
      proposalCurrent.value &&
      state.proposal?.session.status === 'proposed' &&
      !busy.value &&
      !unsettled.value,
  )
  const messages = computed(() => {
    const result = new Map<string, MeetingMessage>()
    for (const turn of state.detail?.turns ?? []) {
      for (const contribution of turn.contributions) {
        for (const history of contribution.history) {
          if (history.message) result.set(history.message.id, history.message)
        }
        if (contribution.message) result.set(contribution.message.id, contribution.message)
      }
    }
    return result
  })
  function messagePreview(id: string) {
    const message = messages.value.get(id)
    if (!message) return '原消息暂不可用'
    const participantName = (id: string) =>
      state.detail?.participants.find((participant) => participant.id === id)?.name || '参会成员'
    const text = message.content_nodes
      .map((node) =>
        node.type === 'text'
          ? node.text
          : node.type === 'mention'
            ? '@' + participantName(node.participant_id || '')
            : node.reference.label || node.reference.id,
      )
      .join('')
      .replace(/\s+/g, ' ')
    return (
      participantName(message.participant_id) +
      '：' +
      (text.length > 180 ? text.slice(0, 180) + '…' : text)
    )
  }
  function replyToMessage(id: string) {
    if (!selected() || !canWrite.value || !messages.value.has(id)) return false
    draft.replyTo = id
    return true
  }
  function cancelReply() {
    if (busy.value || unsettled.value) return false
    draft.replyTo = ''
    return true
  }
  const mentionChoices = computed<readonly MeetingMentionChoice[]>(() =>
    selected()
      ? (state.detail?.participants
          .filter((p) => p.active)
          .map((p) => ({
            node: { type: 'mention' as const, participant_id: p.id },
            label: p.name,
            kind: p.kind,
          })) ?? [])
      : draft.agents.map((id) => ({
          node: { type: 'mention' as const, agent_id: id },
          label: state.agents.find((agent) => agent.id === id)
            ? agentLabel(state.agents.find((agent) => agent.id === id)!)
            : '参会 Agent',
          kind: 'agent' as const,
        })),
  )
  const validContent = computed(
    () =>
      draft.nodes.length <= 1024 &&
      draft.nodes.some((node) => node.type !== 'text' || node.text.trim() !== '') &&
      new TextEncoder().encode(draft.text).byteLength <= 131072 &&
      draft.nodes.every(
        (node) =>
          node.type !== 'mention' ||
          mentionChoices.value.some((choice) => sameMeetingMention(choice.node, node)),
      ),
  )
  const canSend = computed(
    () =>
      canWrite.value &&
      !fileUploadPending.value &&
      validContent.value &&
      (!draft.replyTo || (!!selected() && messages.value.has(draft.replyTo))) &&
      (selected()
        ? !!state.detail?.participants.some((p) => p.kind === 'agent' && p.active)
        : draft.agents.length > 0),
  )
  let generation = 0,
    detailRead = 0,
    disposed = false,
    timer: ReturnType<typeof setTimeout> | undefined,
    leave: ((yes: boolean) => void) | null = null
  const live = (own: number, captured: NonNullable<typeof scope.value>) =>
    !disposed &&
    own === generation &&
    scope.value?.projectID === captured.projectID &&
    scope.value.identity.userID === captured.identity.userID &&
    scope.value.identity.sessionID === captured.identity.sessionID &&
    scope.value.identity.epoch === captured.identity.epoch &&
    scope.value.generation === captured.generation &&
    scope.value.readGeneration === captured.readGeneration
  function stopTimer() {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    state.polling = false
  }
  function explain(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    return e.problem?.code === 'VERSION_CONFLICT'
      ? '会议已有新变化，请重新读取后再操作。输入已保留。'
      : e.problem?.status === 403 || e.problem?.status === 404
        ? '会议不可用或当前身份无权访问。'
        : e.problem?.code === 'PROJECT_NOT_ACTIVE'
          ? '项目当前只读，无法修改。'
          : '未取得完整结果，请重新读取。'
  }
  function denied(error: unknown) {
    return error instanceof AccountFailure && [403, 404].includes(error.problem?.status ?? 0)
  }
  async function loadList() {
    const captured = scope.value,
      own = generation
    if (!captured || busy.value) return
    state.phase = 'loading'
    state.message = ''
    try {
      const items = await auth.meetings.list(captured.projectID)
      if (!live(own, captured)) return
      state.items = items
      state.phase = 'ready'
    } catch (e) {
      if (!live(own, captured)) return
      if (denied(e)) {
        if (selected())
          auth.executionApproval?.forgetMeetingUnavailable(captured.projectID, selected()!)
        state.items = []
        state.detail = null
        state.agents = []
      }
      state.phase = 'error'
      state.message = explain(e)
    }
  }
  async function loadAgents(more = false) {
    const captured = scope.value,
      own = generation
    if (!captured || busy.value || (more && !state.agentNext)) return
    state.agentMessage = ''
    try {
      const page = await auth.agentConfiguration.list(captured.projectID, {
        limit: 100,
        ...(more ? { cursor: state.agentNext } : {}),
      })
      if (!live(own, captured)) return
      const agents = more ? [...state.agents, ...page.items] : page.items
      if (new Set(agents.map((a) => a.id)).size !== agents.length)
        throw new AccountFailure('invalid-response')
      state.agents = agents
      state.agentNext = page.next_cursor ?? ''
    } catch (e) {
      if (!live(own, captured)) return
      if (denied(e)) state.agents = []
      state.agentMessage = '参会 Agent 读取失败，请重试。'
    }
  }
  function schedule() {
    stopTimer()
    if (
      auth.meetingRealtime ||
      disposed ||
      !selected() ||
      !activeWork.value ||
      state.detailPhase !== 'ready' ||
      state.detail?.session.deleting
    )
      return
    state.polling = true
    timer = setTimeout(() => {
      timer = undefined
      if ((typeof document !== 'undefined' && document.hidden) || auth.state.busy) {
        schedule()
        return
      }
      void loadDetail(true)
    }, 2500)
  }
  function stopTimeline() {
    timeline.value?.dispose()
    timeline.value = null
    appliedSnapshot = null
  }
  function reconnect() {
    const captured = scope.value,
      meeting = selected()
    if (!captured || !meeting || !auth.meetingRealtime) return
    if (timeline.value) {
      timeline.value.refresh()
      return
    }
    try {
      timeline.value = auth.meetingRealtime.subscribe({
        kind: 'meeting.timeline',
        project_id: captured.projectID,
        meeting_id: meeting,
      })
    } catch {
      state.detailMessage = '更新连接暂不可用，请重新读取。'
    }
  }
  async function loadDetail(background = false, snapshot?: MeetingDetail) {
    const captured = scope.value,
      own = generation,
      meeting = selected()
    if (!captured || !meeting || busy.value) return
    const serial = ++detailRead
    stopTimer()
    if (!background) state.detailPhase = 'loading'
    state.detailMessage = ''
    try {
      const detail = snapshot ?? (await auth.meetings.detail(captured.projectID, meeting))
      if (!live(own, captured) || selected() !== meeting || serial !== detailRead) return
      if (state.detail && BigInt(detail.session.version) < BigInt(state.detail.session.version)) {
        state.detailPhase = 'ready'
        return
      }
      const initial = state.detail === null
      state.detail = detail
      state.proposal = null
      state.proposalMessage = ''
      state.proposalPhase = 'idle'
      if (detail.session.proposal_content !== undefined) {
        state.proposalPhase = 'loading'
        try {
          const proposal = await auth.meetings.proposal(captured.projectID, meeting)
          if (!live(own, captured) || selected() !== meeting || serial !== detailRead) return
          if (
            proposal.session.version !== detail.session.version ||
            proposal.session.status !== detail.session.status ||
            proposal.session.deleting !== detail.session.deleting ||
            !detail.participants.some(
              (p) => p.kind === 'agent' && p.id === proposal.created_by_participant_id,
            )
          ) {
            state.proposalPhase = 'error'
            state.proposalMessage = '提案已有新变化，请重新读取后再决定。'
          } else {
            state.proposal = proposal
            state.proposalPhase = 'ready'
          }
        } catch (error) {
          if (!live(own, captured) || selected() !== meeting || serial !== detailRead) return
          if (denied(error)) throw error
          state.proposalPhase = 'error'
          state.proposalMessage = '未取得完整提案，请重新读取。'
        }
      }
      for (const id of Object.keys(answers)) {
        if (!detail.decisions.some((d) => d.id === id && d.status === 'pending')) delete answers[id]
      }
      state.detailPhase = 'ready'
      state.items = [detail.session, ...state.items.filter((v) => v.id !== meeting)]
      if (initial && detail.turns.length) draft.mode = detail.turns.at(-1)!.mode
      if (!timeline.value && !detail.session.deleting) reconnect()
      schedule()
    } catch (e) {
      if (!live(own, captured) || selected() !== meeting || serial !== detailRead) return
      if (denied(e)) {
        auth.executionApproval?.forgetMeetingUnavailable(captured.projectID, meeting)
        state.detail = null
        state.proposal = null
        state.items = []
        state.agents = []
        stopTimeline()
      }
      state.detailPhase = 'error'
      state.detailMessage = explain(e)
    }
  }
  async function refresh() {
    await loadList()
    if (disposed || state.phase !== 'ready') return
    if (selected()) await loadDetail()
    else await loadAgents()
  }
  async function submit(lookup = false): Promise<string | null> {
    if (lookup && pending.value && !['start', 'send'].includes(pending.value.kind))
      return applyCommand(undefined, true)
    const captured = scope.value,
      own = generation,
      meeting = selected()
    if (
      !captured ||
      busy.value ||
      (!lookup && !canSend.value) ||
      (lookup && !pending.value?.contextValid)
    )
      return null
    stopTimer()
    state.feedback = ''
    try {
      const result = await (lookup
        ? auth.meetings.lookup()
        : auth.meetings.start(
            captured.projectID,
            meeting
              ? {
                  kind: 'send',
                  meeting,
                  input: {
                    expected_version: state.detail!.session.version,
                    mode: draft.mode,
                    content_nodes: draft.nodes,
                    interrupt: draft.interrupt,
                    ...(draft.replyTo ? { reply_to_message_id: draft.replyTo } : {}),
                  },
                }
              : {
                  kind: 'start',
                  input: {
                    agents: draft.agents,
                    mode: draft.mode,
                    content_nodes: draft.nodes,
                  },
                },
          ))
      if (!live(own, captured)) return null
      if ('status' in result && result.status === 'not_found') {
        state.feedback = '尚未查到原操作的提交结果，请稍后继续查询。'
        return null
      }
      const receipt = 'status' in result ? result.receipt : result
      draft.text = ''
      draft.interrupt = false
      draft.replyTo = ''
      if (!meeting) draft.agents = []
      state.feedback = '消息已提交'
      if (meeting) await loadDetail()
      return receipt.meeting_id
    } catch (e) {
      if (!live(own, captured)) return null
      if (denied(e)) {
        state.detail = null
        state.items = []
      }
      state.feedback =
        pending.value?.phase === 'uncertain'
          ? '发送结果待确认。请查询原操作，暂勿重新发送。'
          : explain(e)
      return null
    }
  }
  async function applyCommand(
    command?: Exclude<MeetingCommand, { kind: 'start' | 'send' }>,
    lookup = false,
  ): Promise<string | null> {
    const captured = scope.value,
      own = generation,
      meeting = selected()
    if (
      !captured ||
      !meeting ||
      busy.value ||
      (!lookup &&
        (!command ||
          command.meeting !== meeting ||
          (command.kind === 'proposal-review'
            ? !canReviewProposal.value
            : command.kind === 'delete'
              ? !canDelete.value
              : !canControl.value))) ||
      (lookup && !pending.value?.contextValid)
    )
      return null
    stopTimer()
    state.feedback = ''
    try {
      const result = await (lookup
        ? auth.meetings.lookup()
        : auth.meetings.start(captured.projectID, command!))
      if (!live(own, captured) || selected() !== meeting) return null
      if ('status' in result && result.status === 'not_found') {
        state.feedback = '尚未查到原操作的提交结果，请稍后继续查询。'
        return null
      }
      const receipt = 'status' in result ? result.receipt : result
      if ('phase' in receipt) {
        state.deletion = receipt
        if (receipt.phase === 'deleted') {
          stopTimeline()
          state.detail = null
          state.proposal = null
          state.items = state.items.filter((item) => item.id !== receipt.meeting_id)
          state.detailPhase = 'ready'
          state.feedback = '会议已删除。'
          draft.text = ''
          draft.replyTo = ''
          state.metadataDirty = false
          for (const id of Object.keys(answers)) delete answers[id]
        } else {
          state.feedback = receipt.error_code
            ? '删除尚未完成，请查询原操作确认进度。'
            : '删除已接受，正在停止相关工作。'
          await loadDetail()
        }
        return receipt.meeting_id
      }
      if (command?.kind === 'decision') delete answers[command.input.decision_id]
      state.feedback = '操作已提交'
      await loadDetail()
      return receipt.meeting_id
    } catch (e) {
      if (!live(own, captured) || selected() !== meeting) return null
      if (denied(e)) {
        state.detail = null
        state.items = []
        for (const id of Object.keys(answers)) delete answers[id]
      }
      state.feedback =
        pending.value?.phase === 'uncertain'
          ? '操作结果待确认。请查询原操作，暂勿重复提交。'
          : explain(e)
      return null
    }
  }
  async function reviewProposal(input: MeetingProposalReview) {
    if (
      !canReviewProposal.value ||
      input.expected_version !== state.proposal?.session.version ||
      !selected()
    )
      return false
    return !!(await applyCommand({ kind: 'proposal-review', meeting: selected()!, input }))
  }
  async function updateMetadata(
    command: Extract<MeetingCommand, { kind: 'participants' | 'references' }>,
  ) {
    if (
      !canWrite.value ||
      command.meeting !== selected() ||
      command.input.expected_version !== state.detail?.session.version
    )
      return false
    return !!(await applyCommand(command))
  }
  function referencePinned(reference: MeetingReference) {
    const key = meetingReferenceKey(reference)
    return state.detail?.references.some((value) => meetingReferenceKey(value) === key) ?? false
  }
  async function pinReference(reference: MeetingReference) {
    if (!canWrite.value || !selected() || !state.detail || state.metadataDirty) return false
    if (referencePinned(reference)) {
      state.feedback = '已在会议引用中。'
      return true
    }
    if (state.detail.references.length >= 256) {
      state.feedback = '会议引用已达上限，请先移除不再需要的引用。'
      return false
    }
    if (
      ![...messages.value.values()].some((message) =>
        message.content_nodes.some(
          (node) =>
            node.type !== 'text' &&
            node.type !== 'mention' &&
            meetingReferenceKey(node.reference) === meetingReferenceKey(reference),
        ),
      )
    )
      return false
    const saved = await updateMetadata({
      kind: 'references',
      meeting: selected()!,
      input: {
        expected_version: state.detail.session.version,
        references: [...state.detail.references, reference],
      },
    })
    if (saved) state.feedback = '已置顶到会议引用。'
    return saved
  }
  function deleteMeeting(expectedVersion: string) {
    if (!canDelete.value || !selected() || expectedVersion !== state.detail?.session.version) {
      state.feedback = '会议已有新变化，请重新读取后再确认删除。'
      return Promise.resolve(null)
    }
    return applyCommand({
      kind: 'delete',
      meeting: selected()!,
      input: { expected_version: expectedVersion },
    })
  }
  function control(action: MeetingControl['action'], turn?: string, contribution?: string) {
    const meeting = selected(),
      detail = state.detail
    if (!meeting || !detail) return Promise.resolve(null)
    return applyCommand({
      kind: 'control',
      meeting,
      input: {
        action,
        expected_version: detail.session.version,
        ...(turn ? { turn_id: turn } : {}),
        ...(contribution ? { contribution_id: contribution } : {}),
      },
    })
  }
  function restartContribution(
    contribution: MeetingContribution,
    action: 'retry-contribution' | 'regenerate-contribution',
  ) {
    const turn = state.detail?.turns.find((t) => t.id === contribution.turn_id)
    const current = turn?.contributions.find((c) => c.id === contribution.id)
    if (
      !canRestart.value ||
      !turn ||
      !['running', 'completed'].includes(turn.status) ||
      !current ||
      current.kind !== 'agent' ||
      current.cancel_requested ||
      !current.execution_id ||
      current.execution_id !== contribution.execution_id ||
      current.generation !== contribution.generation ||
      current.attempt !== contribution.attempt ||
      current.status !== (action === 'retry-contribution' ? 'failed' : 'completed')
    )
      return Promise.resolve(null)
    return control(action, turn.id, current.id)
  }
  function respond(decision: MeetingDecision, action: 'answer' | 'skip') {
    const meeting = selected()
    if (
      !meeting ||
      decision.status !== 'pending' ||
      !state.detail?.decisions.some(
        (d) => d.id === decision.id && d.version === decision.version && d.status === 'pending',
      )
    )
      return Promise.resolve(null)
    const answer = answers[decision.id] || ''
    if (
      action === 'answer' &&
      (!answer.trim() || new TextEncoder().encode(answer).byteLength > 32768)
    )
      return Promise.resolve(null)
    return applyCommand({
      kind: 'decision',
      meeting,
      input: {
        decision_id: decision.id,
        action,
        expected_version: decision.version,
        ...(action === 'answer' ? { answer } : {}),
      },
    })
  }
  function toggleAgent(id: string, value: boolean) {
    if (busy.value || unsettled.value) return
    if (value && draft.agents.length < 32 && !draft.agents.includes(id)) draft.agents.push(id)
    if (!value) draft.agents = draft.agents.filter((v) => v !== id)
  }
  function moveAgent(index: number, change: number) {
    const next = index + change
    if (next < 0 || next >= draft.agents.length || busy.value || unsettled.value) return
    const copy = [...draft.agents]
    ;[copy[index], copy[next]] = [copy[next]!, copy[index]!]
    draft.agents = copy
  }
  function answerLeave(yes: boolean) {
    state.leaveOpen = false
    if (yes) {
      draft.text = ''
      draft.agents = []
      draft.interrupt = false
      draft.replyTo = ''
      for (const id of Object.keys(answers)) delete answers[id]
    }
    const resolve = leave
    leave = null
    resolve?.(yes)
  }
  function confirmLeave() {
    if (unsettled.value) {
      state.feedback = '原操作结果尚未确认，请先查询原操作。'
      return Promise.resolve(false)
    }
    if (busy.value) return Promise.resolve(false)
    if (!dirty.value) return Promise.resolve(true)
    if (leave) return Promise.resolve(false)
    state.leaveOpen = true
    return new Promise<boolean>((resolve) => {
      leave = resolve
    })
  }
  watch(
    [() => timeline.value?.state.snapshot, busy],
    ([snapshot, busy]) => {
      if (
        !busy &&
        snapshot &&
        snapshot !== appliedSnapshot &&
        snapshot.schema === 'meeting_timeline_v1'
      ) {
        appliedSnapshot = snapshot
        void loadDetail(true, snapshot.content)
      }
    },
    { flush: 'post' },
  )
  watch(
    [scope, selected],
    () => {
      ++generation
      ++detailRead
      stopTimer()
      stopTimeline()
      auth.meetings.abandonRead()
      state.phase = 'idle'
      state.detailPhase = 'idle'
      state.items = []
      state.detail = null
      state.proposal = null
      state.proposalPhase = 'idle'
      state.proposalMessage = ''
      state.agents = []
      state.agentNext = ''
      state.message = ''
      state.detailMessage = ''
      state.feedback = ''
      state.metadataDirty = false
      state.deletion = null
      draft.text = ''
      draft.agents = []
      for (const id of Object.keys(answers)) delete answers[id]
      draft.mode = 'parallel'
      draft.interrupt = false
      draft.replyTo = ''
    },
    { flush: 'sync' },
  )
  watch(
    () => [scope.value, selected(), auth.state.busy, state.phase],
    () => {
      if (scope.value && !busy.value && state.phase === 'idle') void refresh()
    },
    { immediate: true, flush: 'post' },
  )
  onScopeDispose(() => {
    disposed = true
    ++generation
    ++detailRead
    stopTimer()
    stopTimeline()
    auth.meetings.abandonRead()
    auth.agentConfiguration.abandonRead()
    answerLeave(false)
  })
  return {
    state,
    realtime,
    reconnect,
    draft,
    messagePreview,
    replyToMessage,
    cancelReply,
    mentionChoices,
    referencePinned,
    pinReference,
    scope,
    pending,
    fileUploadPending,
    unsettled,
    busy,
    dirty,
    activeTurns,
    canWrite,
    canSend,
    canControl,
    canDelete,
    deleteMeeting,
    proposalCurrent,
    canReviewProposal,
    reviewProposal,
    canRestart,
    answers,
    control,
    updateMetadata,
    restartContribution,
    respond,
    refresh,
    loadList,
    loadDetail,
    loadAgents,
    submit,
    toggleAgent,
    moveAgent,
    confirmLeave,
    answerLeave,
  }
}
