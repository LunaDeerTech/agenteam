import { computed, onScopeDispose, reactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import type { MemoryRecord, MemoryRevision } from '../api/agent-memory'
import { useProjectWorkspace } from './useProjectWorkspace'
import { useSession } from './useSession'

export function useAgentMemory(
  agent: () => string | null,
  workspace = useProjectWorkspace(),
  auth = useSession(),
) {
  const state = reactive({
    phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    records: [] as readonly MemoryRecord[],
    next: '',
    message: '',
    selected: null as MemoryRevision | null,
    revisions: [] as readonly MemoryRevision[],
    historyNext: '',
    historyMessage: '',
    historyLoading: false,
    confirmation: null as MemoryRevision | null,
    feedback: '',
  })
  let generation = 0,
    disposed = false
  const scope = computed(() => {
    const c = workspace.currentReadContext.value,
      a = agent()
    return c && a ? { ...c, agent: a } : null
  })
  const pending = computed(() => {
    const p = auth.agentMemory.progress,
      s = scope.value
    return p && s && p.project === s.projectID && p.agent === s.agent ? p : null
  })
  const busy = computed(() => auth.state.busy || state.phase === 'loading' || state.historyLoading)
  const unsettled = computed(
    () => pending.value?.phase === 'submitting' || pending.value?.phase === 'uncertain',
  )
  const canDelete = computed(
    () =>
      !!scope.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !busy.value &&
      !unsettled.value,
  )
  const current = (s: NonNullable<typeof scope.value>, own: number) =>
    !disposed &&
    generation === own &&
    scope.value?.projectID === s.projectID &&
    scope.value.agent === s.agent &&
    scope.value.identity.userID === s.identity.userID &&
    scope.value.identity.sessionID === s.identity.sessionID &&
    scope.value.identity.epoch === s.identity.epoch &&
    scope.value.generation === s.generation &&
    scope.value.readGeneration === s.readGeneration
  const failure = (error: unknown) =>
    error instanceof AccountFailure ? error : new AccountFailure('transport')
  async function load(more = false) {
    const s = scope.value
    if (!s || busy.value || (more && !state.next)) return
    const own = ++generation
    state.phase = 'loading'
    state.message = ''
    try {
      const result = await auth.agentMemory.list(s.projectID, s.agent, {
        limit: 50,
        ...(more ? { cursor: state.next } : {}),
      })
      if (!current(s, own)) return
      const combined = more ? [...state.records, ...result.memories] : result.memories
      if (new Set(combined.map((v) => v.current.memory_id)).size !== combined.length)
        throw new AccountFailure('invalid-response')
      state.records = combined
      state.next = result.next_cursor ?? ''
      state.phase = 'ready'
    } catch (error) {
      if (!current(s, own)) return
      const e = failure(error)
      if (e.problem?.status === 403 || e.problem?.status === 404) {
        state.records = []
        state.selected = null
        state.revisions = []
        state.confirmation = null
        state.message = '当前 Agent 的记忆不可访问。'
      } else state.message = '记忆读取未完成，请重新读取。'
      state.phase = 'error'
    }
  }
  async function history(value: MemoryRevision, more = false) {
    const s = scope.value
    if (!s || busy.value) return
    const own = ++generation
    state.selected = value
    state.historyMessage = ''
    state.historyLoading = true
    if (!more) {
      state.revisions = []
      state.historyNext = ''
    }
    try {
      const result = await auth.agentMemory.history(s.projectID, s.agent, value.memory_id, {
        limit: 50,
        ...(more ? { cursor: state.historyNext } : {}),
      })
      if (!current(s, own)) return
      state.revisions = more ? [...state.revisions, ...result.revisions] : result.revisions
      state.historyNext = result.next_cursor ?? ''
    } catch (error) {
      if (current(s, own)) {
        const e = failure(error)
        if (e.problem?.status === 403 || e.problem?.status === 404) {
          state.records = []
          state.selected = null
          state.revisions = []
          state.confirmation = null
          state.message = '当前 Agent 的记忆不可访问。'
        } else state.historyMessage = '历史读取未完成，请重新读取。'
      }
    } finally {
      if (current(s, own)) state.historyLoading = false
    }
  }
  async function remove(lookup = false) {
    const s = scope.value,
      target = state.confirmation
    if (
      !s ||
      busy.value ||
      (!lookup && (!target || !canDelete.value)) ||
      (lookup && !pending.value?.contextValid)
    )
      return false
    const own = generation
    state.message = ''
    state.feedback = ''
    try {
      const result = await (lookup
        ? auth.agentMemory.lookup()
        : auth.agentMemory.start(s.projectID, s.agent, {
            memory_id: target!.memory_id,
            expected_revision: target!.revision,
          }))
      if (!current(s, own)) return false
      if ('status' in result && result.status === 'not_found') {
        state.message = '原删除尚未查到提交结果，请稍后查询。'
        return false
      }
      const receipt = 'status' in result ? result.receipt : result
      state.records = state.records.filter((v) => v.current.memory_id !== receipt.memory_id)
      state.confirmation = null
      state.selected = null
      state.revisions = []
      state.feedback = '已删除'
      return true
    } catch (error) {
      if (!current(s, own)) return false
      const e = failure(error)
      if (e.problem?.status === 403 || e.problem?.status === 404) {
        state.records = []
        state.selected = null
        state.revisions = []
        state.confirmation = null
        state.message = '当前 Agent 的记忆不可访问。'
        return false
      }
      state.message =
        pending.value?.phase === 'uncertain'
          ? '删除结果待确认，请查询原操作。'
          : e.problem?.code === 'VERSION_CONFLICT'
            ? '此记忆已更新，请重新读取后确认。'
            : '删除未完成，请检查当前权限并重新读取。'
      if (pending.value?.phase === 'uncertain') state.confirmation = null
      return false
    }
  }
  watch(
    scope,
    () => {
      ++generation
      auth.agentMemory.abandonRead()
      Object.assign(state, {
        phase: 'idle',
        records: [],
        next: '',
        message: '',
        selected: null,
        revisions: [],
        historyNext: '',
        historyLoading: false,
        historyMessage: '',
        confirmation: null,
        feedback: '',
      })
    },
    { flush: 'sync' },
  )
  onScopeDispose(() => {
    disposed = true
    ++generation
    auth.agentMemory.abandonRead()
  })
  return { state, scope, pending, busy, canDelete, unsettled, load, history, remove }
}
export type AgentMemoryOwner = ReturnType<typeof useAgentMemory>
