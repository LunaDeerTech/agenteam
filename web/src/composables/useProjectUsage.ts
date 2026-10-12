import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureUsageWindow,
  compareProjectUsage,
  type CapturedUsageQuery,
  type ProjectUsageDay,
  type ProjectUsageRecord,
} from '../api/project-usage'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Phase = 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable'
type Mode = 'records' | 'daily'
type Read = { mode: Mode; index: number; query: CapturedUsageQuery }
type PageBoundary = { cursor?: string; ids: ReadonlySet<string>; last: ProjectUsageRecord | null }
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

export function useProjectUsage(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<string>> = computed(
    () => workspace.paths.value.home + '/settings/model-usage',
  ),
  now: () => number = Date.now,
) {
  const page = shallowReactive({
    phase: 'waiting' as Phase,
    items: Object.freeze([]) as readonly ProjectUsageRecord[],
    nextCursor: null as string | null,
    index: 0,
    message: '',
  })
  const window = shallowRef<CapturedUsageQuery | null>(null),
    mode = shallowRef<Mode>('records'),
    selected = shallowRef<ProjectUsageRecord | null>(null),
    scope = shallowRef<Context | null>(null)
  const daily = shallowReactive({
    items: Object.freeze([]) as readonly ProjectUsageDay[],
    asOf: '',
  })
  const queue = shallowReactive<{ active: number | null; pending: Read | null }>({
    active: null,
    pending: null,
  })
  let history: PageBoundary[] = [],
    generation = 0,
    disposed = false,
    boundRoute = ''
  const authorized = () => {
    const identity = auth.personalContext.identity
    return (
      !disposed &&
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      !!identity &&
      auth.state.user?.id === identity.userID &&
      auth.state.session?.id === identity.sessionID
    )
  }
  const live = () =>
    authorized() &&
    sameContext(scope.value, workspace.currentReadContext.value) &&
    location.value !== '' &&
    location.value === workspace.paths.value.home + '/settings/model-usage' &&
    boundRoute === location.value
  const visible = computed(live),
    reading = computed(() => queue.active !== null)
  const blocked = computed(
    () => !live() || auth.state.busy || reading.value || queue.pending !== null,
  )
  function clear() {
    daily.items = Object.freeze([])
    daily.asOf = ''
    page.items = Object.freeze([])
    page.nextCursor = null
    page.index = 0
    page.message = ''
    selected.value = null
    window.value = null
    history = []
  }
  function retire() {
    ++generation
    queue.pending = null
    const active = queue.active
    queue.active = null
    if (active !== null) auth.projectUsage.abandon()
  }
  function firstPage() {
    retire()
    clear()
    page.phase = 'waiting'
    try {
      window.value = captureUsageWindow(now())
      queue.pending = { mode: mode.value, index: 0, query: window.value }
      pump()
    } catch {
      page.phase = 'error'
      page.message = '未取得有效的查询时间，请检查设备时间后刷新。'
    }
  }
  function pump() {
    if (!live() || !scope.value || !queue.pending || queue.active !== null || auth.state.busy)
      return
    const task = queue.pending,
      captured = scope.value,
      serial = ++generation,
      path = boundRoute
    queue.pending = null
    queue.active = serial
    page.phase = 'loading'
    page.items = Object.freeze([])
    page.nextCursor = null
    page.message = ''
    daily.items = Object.freeze([])
    daily.asOf = ''
    selected.value = null
    void execute(task, captured, serial, path)
  }
  async function execute(task: Read, captured: Context, serial: number, path: string) {
    const current = () =>
      live() && serial === generation && sameContext(captured, scope.value) && path === boundRoute
    try {
      if (task.mode === 'daily') {
        const result = await auth.projectUsage.daily(captured.projectID, {
          from: task.query.from,
          to: task.query.to,
        })
        if (!current()) return
        daily.items = result.items
        daily.asOf = result.as_of
        page.phase = 'ready'
        return
      }
      const result = await auth.projectUsage.list(captured.projectID, task.query)
      if (!current()) return
      const prefix = history.slice(0, task.index)
      const previous = prefix.at(-1)?.last
      if (
        task.index > history.length ||
        result.items.some((item) => prefix.some((boundary) => boundary.ids.has(item.id))) ||
        (previous &&
          result.items.length > 0 &&
          compareProjectUsage(previous, result.items[0]!) >= 0) ||
        (result.next_cursor !== null &&
          (result.next_cursor === task.query.cursor ||
            prefix.some((boundary) => boundary.cursor === result.next_cursor)))
      )
        throw new AccountFailure('invalid-response')
      // A Previous read replaces its page and discards every old forward cursor.
      history = [
        ...prefix,
        {
          ...(task.query.cursor === undefined ? {} : { cursor: task.query.cursor }),
          ids: new Set(result.items.map((item) => item.id)),
          last: result.items.at(-1) ?? null,
        },
      ]
      page.items = result.items
      page.nextCursor = result.next_cursor
      page.index = task.index
      page.phase = 'ready'
    } catch (error) {
      if (!current()) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const unavailable = [401, 403, 404, 410].includes(failure.problem?.status ?? 0)
      const stale = ['CURSOR_INVALID', 'CURSOR_STALE'].includes(failure.problem?.code ?? '')
      clear()
      page.phase = unavailable ? 'unavailable' : 'error'
      page.message = unavailable
        ? '当前调用记录不存在或不可访问，请重新确认访问权限。'
        : stale
          ? '分页已失效，请刷新并重新读取第一页。'
          : failure.kind === 'invalid-response'
            ? '未取得完整有效的调用记录，请刷新后重新读取。'
            : failure.kind === 'cancelled'
              ? '读取已停止，请明确刷新。'
              : '本次读取未完成，请稍后刷新重试。'
    } finally {
      if (queue.active === serial) queue.active = null
      pump()
    }
  }
  const stop = watch(
    () =>
      [
        workspace.currentReadContext.value,
        workspace.paths.value.home,
        location.value,
        auth.state.phase,
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.busy,
      ] as const,
    ([context, home, path]) => {
      if (disposed) return
      if (!authorized() || !context || !home || path !== home + '/settings/model-usage') {
        if (scope.value || queue.pending || queue.active !== null) {
          scope.value = null
          boundRoute = ''
          retire()
          clear()
          page.phase = 'waiting'
        }
        return
      }
      if (!sameContext(scope.value, context) || boundRoute !== path) {
        scope.value = context
        boundRoute = path
        firstPage()
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  function move(index: number, cursor: string | undefined) {
    if (mode.value !== 'records' || blocked.value || page.phase !== 'ready' || !window.value) return
    queue.pending = {
      mode: 'records',
      index,
      query: Object.freeze({ ...window.value, ...(cursor === undefined ? {} : { cursor }) }),
    }
    pump()
  }
  return {
    page: readonly(page),
    mode: readonly(mode),
    daily: readonly(daily),
    window: readonly(window),
    selected: readonly(selected),
    visible,
    reading,
    blocked,
    showMode(value: Mode) {
      if (blocked.value || mode.value === value) return
      const captured = window.value
      mode.value = value
      if (!captured) {
        firstPage()
        return
      }
      retire()
      clear()
      window.value = captured
      page.phase = 'waiting'
      queue.pending = { mode: value, index: 0, query: captured }
      pump()
    },
    select(id: string) {
      if (mode.value === 'records' && !blocked.value && page.phase === 'ready')
        selected.value = page.items.find((item) => item.id === id) ?? null
    },
    closeDetail() {
      if (!blocked.value) selected.value = null
    },
    refresh() {
      if (!blocked.value) firstPage()
    },
    next() {
      if (page.nextCursor) move(page.index + 1, page.nextCursor)
    },
    previous() {
      if (page.index > 0) move(page.index - 1, history[page.index - 1]?.cursor)
    },
    cancel() {
      retire()
      clear()
      page.phase = 'error'
      page.message = '读取已停止，请在原请求结束后明确刷新。'
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      retire()
      clear()
      scope.value = null
    },
  }
}
export type ProjectUsageController = ReturnType<typeof useProjectUsage>
