import { computed, readonly, shallowReactive, shallowRef, watch } from 'vue'
import { AccountFailure } from '../api/client'
import {
  AuditQueryFailure,
  auditFilterFields,
  captureAuditQuery,
  captureProjectAuditID,
  type AuditQuery,
  type AuditQueryField,
  type ProjectAuditRecord,
} from '../api/project-audit'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export type ProjectAuditFilterDraft = Record<(typeof auditFilterFields)[number] | 'limit', string>
export type ProjectAuditReadPhase =
  'waiting' | 'loading' | 'ready' | 'empty' | 'error' | 'unavailable' | 'inactive'
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type ListTarget = Readonly<{ index: number; query: AuditQuery }>
type ListObservation = Readonly<{
  phase: ProjectAuditReadPhase
  items: readonly ProjectAuditRecord[]
  message: string
  cursorInvalid: boolean
  hasPrevious: boolean
  hasNext: boolean
  page: number
}>
type DetailObservation = Readonly<{
  phase: ProjectAuditReadPhase | 'not-found'
  target: string | null
  record: ProjectAuditRecord | null
  message: string
}>
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
const keys = [...auditFilterFields, 'limit'] as const
const defaults = (): ProjectAuditFilterDraft =>
  Object.fromEntries(
    keys.map((key) => [key, key === 'limit' ? '50' : '']),
  ) as ProjectAuditFilterDraft
const emptyRows: readonly ProjectAuditRecord[] = Object.freeze([])
const initialList = (phase: ProjectAuditReadPhase): ListObservation =>
  Object.freeze({
    phase,
    items: emptyRows,
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
    page: 1,
  })
const initialDetail = (): DetailObservation =>
  Object.freeze({ phase: 'inactive', target: null, record: null, message: '' })

// Page-local observations only; the Session controller retains the single
// Cookie owner until actual I/O completion. Checking retires this instance.
export function useProjectAudit(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const draft = shallowReactive(defaults())
  const appliedDraft = shallowRef<Readonly<ProjectAuditFilterDraft>>(Object.freeze(defaults()))
  const state = shallowReactive({
    mode: 'list' as 'list' | 'detail',
    applied: captureAuditQuery({ limit: '50' }),
    list: initialList('waiting'),
    detail: initialDetail(),
    fieldErrors: Object.freeze({}) as Readonly<Partial<Record<AuditQueryField, string>>>,
    filterMessage: '',
  })
  const identity = auth.personalContext.identity
  let scope: Context | null = null,
    generation = 0,
    initial = true,
    retired = false,
    disposed = false
  let cancelledBeforeBinding = false
  let active: Readonly<{ generation: number; kind: 'list' | 'detail' }> | null = null
  let listTarget: ListTarget | null = null,
    history: (string | undefined)[] = [],
    index = 0,
    nextCursor: string | null = null
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    sameIdentity(identity, auth.personalContext.identity) &&
    auth.state.user?.id === identity?.userID &&
    auth.state.session?.id === identity?.sessionID
  const live = () =>
    !disposed && !retired && authorized() && sameContext(scope, workspace.currentReadContext.value)
  const blocked = computed(
    () =>
      !live() ||
      auth.state.busy ||
      state.list.phase === 'loading' ||
      state.detail.phase === 'loading',
  )
  const dirty = computed(() => keys.some((key) => draft[key] !== appliedDraft.value[key]))
  const reading = computed(
    () =>
      state.list.phase === 'loading' ||
      state.list.phase === 'waiting' ||
      state.detail.phase === 'loading',
  )
  function retireRead() {
    ++generation
    const owned = active !== null
    active = null
    if (owned) auth.projectAudit.abandon()
  }
  function clearHistory() {
    history = []
    index = 0
    nextCursor = null
  }
  function clearLocal(phase: ProjectAuditReadPhase) {
    retireRead()
    clearHistory()
    listTarget = null
    Object.assign(draft, defaults())
    appliedDraft.value = Object.freeze(defaults())
    state.applied = captureAuditQuery({ limit: '50' })
    state.mode = 'list'
    state.list = initialList(phase)
    state.detail = initialDetail()
    state.fieldErrors = Object.freeze({})
    state.filterMessage = ''
  }
  function readError(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    return {
      e,
      unavailable:
        e.kind === 'problem' &&
        (e.problem?.status === 403 ||
          (e.problem?.status === 409 &&
            ['PROJECT_NOT_ACTIVE', 'INVALID_STATE'].includes(e.problem.code))),
      message:
        e.kind === 'cancelled'
          ? '本次读取已取消或等待超时，请在请求结束后重新读取。'
          : e.kind === 'invalid-response'
            ? '审计响应无法确认，请重新读取。'
            : '暂时无法读取项目审计，请重新读取。',
    }
  }
  async function readList(target: ListTarget) {
    if (blocked.value || !scope) return
    initial = false
    const own = ++generation,
      captured = scope
    const current = () => live() && generation === own && sameContext(captured, scope)
    active = { generation: own, kind: 'list' }
    listTarget = target
    state.mode = 'list'
    state.detail = initialDetail()
    state.list = Object.freeze({ ...initialList('loading'), page: target.index + 1 })
    try {
      const result = await auth.projectAudit.list(captured.projectID, target.query)
      if (!current()) return
      history = [...history.slice(0, target.index), target.query.cursor]
      index = target.index
      nextCursor = result.next_cursor
      state.list = Object.freeze({
        phase: result.items.length ? 'ready' : 'empty',
        items: result.items,
        message: '',
        cursorInvalid: false,
        hasPrevious: index > 0,
        hasNext: nextCursor !== null,
        page: index + 1,
      })
    } catch (error) {
      if (!current()) return
      const { e, message, unavailable } = readError(error)
      const cursorInvalid =
        e.kind === 'problem' && e.problem?.status === 400 && e.problem.code === 'CURSOR_INVALID'
      const missing =
        e.kind === 'problem' && e.problem?.status === 404 && e.problem.code === 'NOT_FOUND'
      if (cursorInvalid || unavailable || missing) clearHistory()
      state.list = Object.freeze({
        ...initialList(unavailable || missing ? 'unavailable' : 'error'),
        page: target.index + 1,
        cursorInvalid,
        message: cursorInvalid
          ? '分页链接已失效，返回第一页。'
          : unavailable || missing
            ? '项目审计当前不可用，请核对当前项目或明确重新读取。'
            : message,
      })
    } finally {
      if (active?.generation === own) active = null
    }
  }
  async function readDetail(id: string) {
    if (blocked.value || !scope) return
    let target: string
    try {
      target = captureProjectAuditID(id)
    } catch {
      return
    }
    initial = false
    const own = ++generation,
      captured = scope
    const current = () => live() && generation === own && sameContext(captured, scope)
    active = { generation: own, kind: 'detail' }
    state.mode = 'detail'
    state.detail = Object.freeze({ phase: 'loading', target, record: null, message: '' })
    try {
      const record = await auth.projectAudit.get(captured.projectID, target)
      if (current()) state.detail = Object.freeze({ phase: 'ready', target, record, message: '' })
    } catch (error) {
      if (!current()) return
      const { e, message, unavailable } = readError(error)
      const missing =
        e.kind === 'problem' && e.problem?.status === 404 && e.problem.code === 'NOT_FOUND'
      if (unavailable) {
        clearHistory()
        state.list = initialList('unavailable')
      }
      state.detail = Object.freeze({
        phase: missing ? 'not-found' : unavailable ? 'unavailable' : 'error',
        target,
        record: null,
        message: missing
          ? '记录不存在或不可访问。'
          : unavailable
            ? '项目审计当前不可用，请核对当前项目或明确重新读取。'
            : message,
      })
    } finally {
      if (active?.generation === own) active = null
    }
  }
  function firstPage() {
    if (blocked.value) return Promise.resolve()
    clearHistory()
    return readList(Object.freeze({ index: 0, query: state.applied }))
  }
  function applyDraft() {
    if (blocked.value) return Promise.resolve()
    let captured: AuditQuery
    try {
      captured = captureAuditQuery(draft)
    } catch (error) {
      const invalid = error instanceof AuditQueryFailure ? error.fields : []
      state.fieldErrors = Object.freeze(
        Object.fromEntries(
          invalid.map((field) => [
            field,
            field === 'limit'
              ? '每页数量应为 1–200 的完整十进制整数。'
              : field === 'from' || field === 'to'
                ? '请检查完整时区、公历日期、微秒及起止顺序。'
                : field === 'actor_kind' || field === 'actor_id'
                  ? '请检查操作者类型与规范 UUIDv7；service 不能同时指定操作者 ID。'
                  : field.endsWith('_id')
                    ? '请输入规范的小写 UUIDv7。'
                    : '请选择有效选项。',
          ]),
        ),
      )
      state.filterMessage = '筛选尚未应用，请核对标记字段。'
      return Promise.resolve()
    }
    state.applied = captured
    appliedDraft.value = Object.freeze({ ...draft })
    state.fieldErrors = Object.freeze({})
    state.filterMessage = ''
    return firstPage()
  }
  const stop = watch(
    () =>
      [
        workspace.currentReadContext.value,
        auth.state.phase,
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.user?.id,
        auth.state.session?.id,
        auth.state.busy,
      ] as const,
    ([context]) => {
      if (disposed || retired) return
      if (!authorized()) {
        retired = true
        initial = false
        scope = null
        clearLocal('inactive')
        return
      }
      if (!context) {
        if (scope) {
          scope = null
          clearLocal('waiting')
        }
        return
      }
      if (!sameContext(scope, context)) {
        scope = context
        initial = !cancelledBeforeBinding
        clearLocal(initial ? 'waiting' : 'error')
        if (cancelledBeforeBinding) {
          listTarget = Object.freeze({ index: 0, query: state.applied })
          state.list = Object.freeze({ ...state.list, message: '本次读取已停止，请明确重新读取。' })
        }
        cancelledBeforeBinding = false
      }
      if (initial && !auth.state.busy) {
        initial = false
        void firstPage()
      }
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    draft: readonly(draft),
    blocked,
    dirty,
    reading,
    isCurrent: (stamp?: number) => live() && (stamp === undefined || stamp === generation),
    focusGeneration: () => generation,
    updateFilter(field: keyof ProjectAuditFilterDraft, value: string) {
      if (!live() || !keys.includes(field) || typeof value !== 'string') return
      draft[field] = value
      if (state.fieldErrors[field]) {
        const errors = { ...state.fieldErrors }
        delete errors[field]
        state.fieldErrors = Object.freeze(errors)
      }
    },
    apply: applyDraft,
    reset() {
      if (blocked.value) return Promise.resolve()
      Object.assign(draft, defaults())
      return applyDraft()
    },
    refresh: firstPage,
    previous() {
      return state.mode === 'list' && state.list.hasPrevious
        ? readList(
            Object.freeze({
              index: index - 1,
              query: captureAuditQuery({
                ...state.applied,
                ...(history[index - 1] === undefined ? {} : { cursor: history[index - 1] }),
              }),
            }),
          )
        : Promise.resolve()
    },
    next() {
      return state.mode === 'list' && state.list.hasNext && nextCursor !== null
        ? readList(
            Object.freeze({
              index: index + 1,
              query: captureAuditQuery({ ...state.applied, cursor: nextCursor }),
            }),
          )
        : Promise.resolve()
    },
    retry() {
      return state.mode === 'list' &&
        ['error', 'unavailable'].includes(state.list.phase) &&
        !state.list.cursorInvalid &&
        listTarget
        ? readList(listTarget)
        : Promise.resolve()
    },
    openDetail: readDetail,
    retryDetail() {
      return state.mode === 'detail' &&
        state.detail.target &&
        ['error', 'not-found', 'unavailable', 'ready'].includes(state.detail.phase)
        ? readDetail(state.detail.target)
        : Promise.resolve()
    },
    backToList() {
      if (!live() || state.mode !== 'detail') return
      retireRead()
      state.detail = initialDetail()
      state.mode = 'list'
    },
    cancel() {
      if (!authorized() || disposed || retired || !reading.value) return
      initial = false
      if (!scope) cancelledBeforeBinding = true
      retireRead()
      const message = '本次读取已取消，请在请求结束后重新读取。'
      if (state.mode === 'detail')
        state.detail = Object.freeze({ ...state.detail, phase: 'error', record: null, message })
      else {
        listTarget ??= Object.freeze({ index: 0, query: state.applied })
        state.list = Object.freeze({ ...initialList('error'), page: listTarget.index + 1, message })
      }
    },
    leave() {
      if (disposed || retired) return
      initial = false
      if (!scope) cancelledBeforeBinding = true
      clearLocal('error')
      listTarget = Object.freeze({ index: 0, query: state.applied })
      state.list = Object.freeze({ ...state.list, message: '本页读取已停止，可显式重新读取。' })
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      scope = null
      initial = false
      clearLocal('inactive')
    },
  }
}
export type ProjectAuditController = ReturnType<typeof useProjectAudit>
