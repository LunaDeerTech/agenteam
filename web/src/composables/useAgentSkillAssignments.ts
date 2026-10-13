import { computed, reactive, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureAgentSkillCommand,
  type AgentSkillAssignment,
  type AgentSkillCommand,
  type AgentSkillMutation,
} from '../api/agent-skill-assignments'
import type { AgentConfiguration } from '../api/agent-configuration'
import type { SkillMetadata } from '../api/skill-owner'
import type { SessionController } from './useSession'

type Phase = 'idle' | 'loading' | 'ready' | 'error'
type Context = {
  visible: boolean
  projectID: string | null
  agentID: string | null
  configuration: AgentConfiguration | null
  key: string
  editable: boolean
  blocked: boolean
}
function explain(error: unknown) {
  const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
  if ([401, 403, 404].includes(e.problem?.status ?? 0))
    return '当前项目、Agent 或技能不可用，或无权访问。'
  if (e.problem?.code === 'VERSION_CONFLICT')
    return 'Agent 配置已有新版本。请取消本次确认，重新读取后再选择。'
  if (e.problem?.code === 'CURSOR_STALE' || e.problem?.code === 'CURSOR_INVALID')
    return '分配目录已变化，请从第一页重新读取。'
  return '未取得完整结果，请等待当前请求结束后明确重试读取。'
}
export function createAgentSkillAssignments(
  auth: SessionController,
  context: Readonly<Ref<Context>>,
  reloadAgent: (id: string) => Promise<void>,
  denied: (error: unknown) => void,
) {
  const state = shallowReactive({
    open: false,
    phase: 'idle' as Phase,
    items: [] as readonly AgentSkillAssignment[],
    next: undefined as string | undefined,
    version: '',
    sequence: '',
    message: '',
    feedback: '',
    needsRead: false,
    candidates: [] as readonly SkillMetadata[],
    candidateNext: undefined as string | undefined,
    candidatePhase: 'idle' as Phase,
    candidateMessage: '',
  })
  const draft = reactive({
    selected: '',
    command: null as AgentSkillCommand | null,
    sequence: '',
    label: '',
    conflict: false,
  })
  const metadata = shallowReactive(new Map<string, SkillMetadata>())
  const io = reactive({ reading: false })
  let serial = 0,
    disposed = false
  const cursors = new Set<string>(),
    candidateCursors = new Set<string>()
  const progress = computed(() => {
    const p = auth.agentSkills?.progress
    return context.value.visible && p?.projectID === context.value.projectID ? p : null
  })
  const pending = computed(() => {
    const p = auth.agentSkills?.progress
    return p && ['submitting', 'uncertain'].includes(p.phase) ? p : null
  })
  const busy = computed(() => io.reading || auth.state.busy || context.value.blocked)
  const dirty = computed(() => !!draft.command || !!draft.selected)
  const current = computed(
    () =>
      state.phase === 'ready' &&
      !state.needsRead &&
      context.value.configuration?.id === context.value.agentID &&
      context.value.configuration.version === state.version,
  )
  const canManage = computed(
    () =>
      context.value.visible &&
      context.value.editable &&
      state.open &&
      current.value &&
      !pending.value,
  )
  const stale = computed(
    () =>
      !!draft.command &&
      (draft.conflict ||
        !current.value ||
        draft.command.input.expected_version !== state.version ||
        draft.sequence !== state.sequence),
  )
  const canConfirm = computed(
    () => canManage.value && !busy.value && !!draft.command && !stale.value,
  )
  const options = computed(() => [
    { value: '', label: '选择项目技能' },
    ...state.candidates.map((item) => ({
      value: item.id,
      label: item.name + (item.protected ? '（受保护）' : ''),
    })),
  ])
  const name = (id: string) => metadata.get(id)?.name ?? id
  function clearDraft() {
    Object.assign(draft, { selected: '', command: null, sequence: '', label: '', conflict: false })
  }
  function clearObservations() {
    Object.assign(state, {
      phase: 'idle',
      items: [],
      next: undefined,
      version: '',
      sequence: '',
      message: '',
      candidates: [],
      candidateNext: undefined,
      candidatePhase: 'idle',
      candidateMessage: '',
    })
    metadata.clear()
    cursors.clear()
    candidateCursors.clear()
  }
  function retire() {
    ++serial
    if (io.reading) auth.agentSkills?.abandonRead()
  }
  function reset(discardRejected = false) {
    const original = progress.value
    if (discardRejected && original?.phase === 'rejected' && original.contextValid)
      auth.agentSkills.editRejected()
    retire()
    clearObservations()
    clearDraft()
    state.open = false
    state.feedback = ''
    state.needsRead = false
  }
  const live = (own: number, key: string) =>
    !disposed && serial === own && context.value.visible && context.value.key === key
  function fail(error: unknown) {
    if (error instanceof AccountFailure && [401, 403, 404].includes(error.problem?.status ?? 0)) {
      const c = context.value
      if (c.projectID) auth.agentSkills?.forgetUnavailable(c.projectID, c.agentID ?? undefined)
      reset()
      denied(error)
    }
    return explain(error)
  }
  async function read<T>(
    call: (project: string, agent: string) => Promise<T>,
    accept: (value: T) => void,
    reject: (error: unknown) => void,
  ) {
    const c = context.value,
      own = serial,
      key = c.key
    if (busy.value || !c.visible || !c.projectID || !c.agentID) return
    io.reading = true
    try {
      const value = await call(c.projectID, c.agentID)
      if (live(own, key)) accept(value)
    } catch (error) {
      if (live(own, key)) reject(error)
    } finally {
      io.reading = false
    }
  }
  async function loadAssignments(more = false) {
    const cursor = more ? state.next : undefined
    if (more && !cursor) return
    await read(
      async (p, a) => {
        state.phase = 'loading'
        state.message = ''
        return auth.agentSkills.list(p, a, { limit: 25, ...(cursor ? { cursor } : {}) })
      },
      (page) => {
        if (
          page.agent_version !== context.value.configuration?.version ||
          (more &&
            (page.agent_version !== state.version || page.assignment_sequence !== state.sequence))
        ) {
          state.phase = 'error'
          state.next = undefined
          state.needsRead = true
          state.message = 'Agent 配置或分配版本已变化，请重新读取后核对；原确认不会自动采用新版本。'
          return
        }
        if (
          more &&
          (state.items.some((old) =>
            page.items.some((item) => item.id === old.id || item.skill_id === old.skill_id),
          ) ||
            (page.items[0] && state.items.at(-1)!.skill_id >= page.items[0].skill_id) ||
            (page.next_cursor && cursors.has(page.next_cursor)))
        )
          throw new AccountFailure('invalid-response')
        if (!more) cursors.clear()
        if (cursor) cursors.add(cursor)
        state.items = more ? [...state.items, ...page.items] : page.items
        state.next = page.next_cursor
        state.version = page.agent_version
        state.sequence = page.assignment_sequence
        state.phase = 'ready'
        state.needsRead = false
      },
      (error) => {
        const message = fail(error)
        state.phase = 'error'
        state.next = undefined
        state.message = message
      },
    )
  }
  async function loadCandidates(more = false) {
    const cursor = more ? state.candidateNext : undefined
    if (more && !cursor) return
    await read(
      async (p) => {
        state.candidatePhase = 'loading'
        state.candidateMessage = ''
        return auth.agentSkills.catalog(p, { limit: 25, ...(cursor ? { cursor } : {}) })
      },
      (page) => {
        if (
          more &&
          (state.candidates.some((old) => page.items.some((item) => old.id === item.id)) ||
            (page.items[0] && state.candidates.at(-1)!.id >= page.items[0].id) ||
            (page.next_cursor && candidateCursors.has(page.next_cursor)))
        )
          throw new AccountFailure('invalid-response')
        if (!more) candidateCursors.clear()
        if (cursor) candidateCursors.add(cursor)
        state.candidates = more ? [...state.candidates, ...page.items] : page.items
        page.items.forEach((item) => metadata.set(item.id, item))
        state.candidateNext = page.next_cursor
        state.candidatePhase = 'ready'
      },
      (error) => {
        const message = fail(error)
        state.candidatePhase = 'error'
        state.candidateMessage = message
        state.candidateNext = undefined
      },
    )
  }
  async function loadMetadata(id: string) {
    if (!state.items.some((item) => item.skill_id === id)) return
    await read(
      (p) => auth.agentSkills.metadata(p, id),
      (value) => {
        metadata.set(id, value)
      },
      (error) => {
        state.message = fail(error)
      },
    )
  }
  async function open() {
    const c = context.value
    if (!auth.agentSkills || busy.value || !c.visible || !c.configuration || pending.value) return
    state.open = true
    state.feedback = ''
    clearDraft()
    await loadAssignments()
    if (
      state.open &&
      context.value.agentID === c.agentID &&
      context.value.projectID === c.projectID &&
      state.phase === 'ready'
    )
      await loadCandidates()
  }
  function close() {
    if (busy.value || pending.value || dirty.value) return false
    reset()
    return true
  }
  function cancel() {
    if (busy.value || pending.value) return false
    if (auth.agentSkills?.progress?.phase === 'rejected' && auth.agentSkills.progress.contextValid)
      auth.agentSkills.editRejected()
    const selected = draft.command ? draft.selected : ''
    clearDraft()
    draft.selected = selected
    return true
  }
  function choose(kind: 'assign' | 'disable' | 'enable' | 'remove', id?: string) {
    if (!canManage.value || busy.value || draft.command) return
    const row = id ? state.items.find((item) => item.id === id) : null
    if (kind !== 'assign' && !row) return
    if (
      kind === 'assign' &&
      (state.candidatePhase !== 'ready' ||
        !state.candidates.some((item) => item.id === draft.selected))
    )
      return
    const skill = row?.skill_id ?? draft.selected
    draft.command = captureAgentSkillCommand(
      kind === 'remove'
        ? {
            kind: 'remove',
            input: { expected_version: state.version, request: { assignment_id: row!.id } },
          }
        : {
            kind: 'assign',
            input: {
              expected_version: state.version,
              request: { skill_id: skill, enabled: kind !== 'disable' },
            },
          },
    )
    draft.sequence = state.sequence
    draft.label = `${kind === 'remove' ? '撤销分配' : kind === 'disable' ? '禁用' : kind === 'enable' ? '启用' : '分配'}「${name(skill)}」`
    draft.conflict = false
    state.message = ''
    state.feedback = ''
  }
  async function refresh() {
    const c = context.value,
      own = serial,
      key = c.key
    if (busy.value || !c.visible || !c.agentID || pending.value) return
    state.needsRead = true
    await reloadAgent(c.agentID)
    if (!live(own, key) || context.value.configuration?.id !== c.agentID) return
    await loadAssignments()
  }
  async function accept(receipt: AgentSkillMutation, own: number, key: string) {
    if (!live(own, key) || context.value.agentID !== receipt.agent_id) return false
    clearDraft()
    state.open = true
    state.needsRead = true
    state.items = []
    state.next = undefined
    state.phase = 'idle'
    state.feedback = !receipt.changed
      ? '已确认原操作，当前分配没有变化。'
      : receipt.assignment === null
        ? '已撤销分配，技能包保留。'
        : receipt.assignment.enabled
          ? '技能分配已保存。'
          : '技能已禁用，分配记录保留。'
    if (receipt.runtime?.queued)
      state.feedback += ' 变更已进入原执行的待处理记录，不表示已用于某轮输入或恢复执行。'
    await reloadAgent(receipt.agent_id)
    if (!live(own, key) || context.value.configuration?.id !== receipt.agent_id) return false
    await loadAssignments()
    return live(own, key) && current.value
  }
  async function submit() {
    const c = context.value,
      command = draft.command,
      own = serial,
      key = c.key
    if (!canConfirm.value || !command || !c.projectID || !c.agentID) return false
    try {
      if (auth.agentSkills.progress?.phase === 'rejected') auth.agentSkills.editRejected()
      const value = await auth.agentSkills.start(c.projectID, c.agentID, command, draft.sequence)
      const receipt = 'state' in value ? value.receipt : value
      return receipt ? await accept(receipt, own, key) : false
    } catch (error) {
      if (live(own, key)) {
        state.message = fail(error)
        if (auth.agentSkills.progress?.phase === 'rejected') {
          draft.conflict = true
          state.needsRead = true
        }
      }
      return false
    }
  }
  async function lookup() {
    const p = progress.value,
      c = context.value,
      own = serial,
      key = c.key
    if (busy.value || !p?.contextValid || p.phase !== 'uncertain' || c.agentID !== p.agentID)
      return false
    try {
      const value = await auth.agentSkills.checkOriginal()
      if (!live(own, key)) return false
      if ('state' in value && value.state === 'not_observed') {
        state.message = '尚未观察到原操作结果，请保留原意图，稍后明确查询。'
        return false
      }
      return await accept('state' in value ? value.receipt! : value, own, key)
    } catch (error) {
      if (live(own, key)) state.message = fail(error)
      return false
    }
  }
  let lastIdentity = '',
    lastTarget = '',
    lastKey = ''
  const stop = watch(
    () =>
      [
        context.value.key,
        context.value.agentID,
        context.value.visible,
        auth.personalContext.identity,
      ] as const,
    () => {
      const identity = auth.personalContext.identity
      const identityKey = identity
        ? `${identity.userID}/${identity.sessionID}/${identity.epoch}`
        : ''
      const c = context.value,
        target = c.projectID && c.agentID ? `${c.projectID}/${c.agentID}` : ''
      if (
        identityKey !== lastIdentity ||
        (target && lastTarget && target !== lastTarget) ||
        (!c.agentID && lastTarget)
      )
        reset()
      else if (c.key !== lastKey) {
        retire()
        clearObservations()
      }
      lastIdentity = identityKey
      if (target) lastTarget = target
      lastKey = c.key
    },
    { flush: 'sync', immediate: true },
  )
  function dispose() {
    disposed = true
    stop()
    reset()
  }
  return {
    state,
    draft,
    metadata,
    options,
    name,
    progress,
    pending,
    busy,
    reading: computed(() => io.reading),
    dirty,
    current,
    canManage,
    stale,
    canConfirm,
    open,
    close,
    cancel,
    reset,
    clearDraft,
    loadAssignments,
    loadCandidates,
    loadMetadata,
    choose,
    refresh,
    submit,
    lookup,
    dispose,
  }
}
export type AgentSkillsOwner = ReturnType<typeof createAgentSkillAssignments>
