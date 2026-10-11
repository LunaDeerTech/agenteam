import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProjectSecretID,
  compareProjectSecrets,
  type ProjectSecretMetadata,
} from '../api/project-secrets'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Phase = 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable'
type Read = { kind: 'page'; cursor?: string } | { kind: 'detail'; id: string }
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

export function useProjectSecrets(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<string>> = computed(
    () => workspace.paths.value.home + '/settings/secrets',
  ),
) {
  const page = shallowReactive({
    phase: 'waiting' as Phase,
    items: Object.freeze([]) as readonly ProjectSecretMetadata[],
    nextCursor: undefined as string | undefined,
    message: '',
    loadingMore: false,
  })
  const detail = shallowReactive({
    phase: 'waiting' as Phase,
    value: null as ProjectSecretMetadata | null,
    message: '',
  })
  const selected = shallowRef<string | null>(null),
    scope = shallowRef<Context | null>(null)
  const queue = shallowReactive<{ active: number | null; pending: Read | null }>({
    active: null,
    pending: null,
  })
  const cursors = new Set<string>()
  let generation = 0,
    disposed = false,
    boundRoute = ''
  let pendingDetail: string | null = null
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
    location.value === workspace.paths.value.home + '/settings/secrets' &&
    boundRoute === location.value
  const visible = computed(live),
    reading = computed(() => queue.active !== null)
  const blocked = computed(
    () => !live() || auth.state.busy || reading.value || queue.pending !== null,
  )
  function clear() {
    page.items = Object.freeze([])
    page.nextCursor = undefined
    page.message = ''
    page.loadingMore = false
    cursors.clear()
    selected.value = null
    Object.assign(detail, { phase: 'waiting', value: null, message: '' })
  }
  function retire() {
    ++generation
    queue.pending = null
    pendingDetail = null
    const active = queue.active
    queue.active = null
    if (active !== null) auth.projectSecrets.abandon()
  }
  function firstPage(target?: string) {
    retire()
    clear()
    page.phase = 'waiting'
    pendingDetail = target ?? null
    queue.pending = { kind: 'page' }
    pump()
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
    if (task.kind === 'page') {
      page.loadingMore = task.cursor !== undefined
      if (!page.loadingMore) page.phase = 'loading'
      page.message = ''
    } else Object.assign(detail, { phase: 'loading', value: null, message: '' })
    void execute(task, captured, serial, path)
  }
  async function execute(task: Read, captured: Context, serial: number, path: string) {
    const current = () =>
      live() &&
      serial === generation &&
      sameContext(captured, scope.value) &&
      path === boundRoute &&
      (task.kind === 'page' || selected.value === task.id)
    try {
      if (task.kind === 'page') {
        const result = await auth.projectSecrets.list(captured.projectID, {
          limit: 50,
          ...(task.cursor === undefined ? {} : { cursor: task.cursor }),
        })
        if (!current()) return
        if (task.cursor !== undefined) {
          const previousIDs = new Set(page.items.map((item) => item.id))
          if (
            page.nextCursor !== task.cursor ||
            result.items.some((item) => previousIDs.has(item.id)) ||
            (result.items.length > 0 &&
              page.items.length > 0 &&
              compareProjectSecrets(page.items[page.items.length - 1]!, result.items[0]!) >= 0)
          )
            throw new AccountFailure('invalid-response')
        }
        if (
          result.next_cursor &&
          (cursors.has(result.next_cursor) || result.next_cursor === task.cursor)
        )
          throw new AccountFailure('invalid-response')
        const items =
          task.cursor === undefined ? result.items : Object.freeze([...page.items, ...result.items])
        if (task.cursor) cursors.add(task.cursor)
        page.items = items
        page.nextCursor = result.next_cursor
        page.phase = 'ready'
        if (task.cursor === undefined && pendingDetail) {
          selected.value = pendingDetail
          queue.pending = { kind: 'detail', id: pendingDetail }
          pendingDetail = null
        }
      } else {
        const result = await auth.projectSecrets.get(captured.projectID, task.id)
        if (!current()) return
        detail.value = result
        detail.phase = 'ready'
      }
    } catch (error) {
      if (!current()) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const unavailable = [401, 403, 404, 410].includes(failure.problem?.status ?? 0)
      const stale =
        task.kind === 'page' &&
        ['CURSOR_INVALID', 'CURSOR_STALE'].includes(failure.problem?.code ?? '')
      if (unavailable || stale) {
        clear()
        page.phase = unavailable ? 'unavailable' : 'error'
        page.message = unavailable
          ? '当前 Secrets 不存在或不可访问，请重新确认访问权限。'
          : '目录已变化或游标已失效，请重新读取第一页。'
      } else {
        const target = task.kind === 'page' ? page : detail
        target.phase = task.kind === 'page' && task.cursor !== undefined ? 'ready' : 'error'
        target.message =
          failure.kind === 'invalid-response'
            ? '未取得完整有效的安全信息，请重新读取。'
            : failure.kind === 'cancelled'
              ? '读取已停止，请明确重读。'
              : '本次读取未完成，请稍后重新读取。'
      }
    } finally {
      if (queue.active === serial) {
        queue.active = null
        page.loadingMore = false
      }
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
      if (!authorized() || !context || !home || path !== home + '/settings/secrets') {
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
        // Publish the route before the reactive scope: synchronous consumers
        // must never cache a new scope against the retired route.
        boundRoute = path
        scope.value = context
        firstPage()
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    page: readonly(page),
    detail: readonly(detail),
    selected: readonly(selected),
    visible,
    reading,
    blocked,
    select(value: string) {
      if (blocked.value || !page.items.some((item) => item.id === value)) return
      const id = captureProjectSecretID(value)
      selected.value = id
      queue.pending = { kind: 'detail', id }
      pump()
    },
    refresh() {
      if (!blocked.value) firstPage()
    },
    refreshAfterWrite(target: string) {
      if (!live()) return
      firstPage(captureProjectSecretID(target))
    },
    deny() {
      retire()
      clear()
      page.phase = 'unavailable'
      page.message = '当前 Secrets 不存在或不可访问，请重新确认访问权限。'
    },
    refreshDetail() {
      if (blocked.value || !selected.value) return
      queue.pending = { kind: 'detail', id: selected.value }
      pump()
    },
    loadMore() {
      if (blocked.value || page.phase !== 'ready' || !page.nextCursor) return
      queue.pending = { kind: 'page', cursor: page.nextCursor }
      pump()
    },
    closeDetail() {
      if (blocked.value) return
      selected.value = null
      Object.assign(detail, { phase: 'waiting', value: null, message: '' })
    },
    cancel() {
      retire()
      clear()
      page.phase = 'error'
      page.message = '读取已停止，请在原请求结束后明确重读。'
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
export type ProjectSecretsController = ReturnType<typeof useProjectSecrets>
