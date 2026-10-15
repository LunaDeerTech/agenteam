import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import { compareInboxItems, inboxSourceKey } from '../api/project-inbox'
import type { CapturedInboxQuery, InboxItem, InboxStatus } from '../api/inbox-types'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

export function useProjectInbox(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<string>> = computed(() => workspace.paths.value.home + '/inbox'),
) {
  const page = shallowReactive({
    phase: 'waiting' as 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable',
    items: Object.freeze([]) as readonly InboxItem[],
    nextBefore: null as string | null,
    message: '',
  })
  const status = shallowRef<InboxStatus>('open'),
    scope = shallowRef<Context | null>(null)
  const queue = shallowReactive<{
    active: number | null
    pending: { query: CapturedInboxQuery; append: boolean } | null
  }>({ active: null, pending: null })
  let generation = 0,
    disposed = false,
    boundRoute = ''
  const cursors = new Set<string>()
  const authorized = () =>
    !disposed &&
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    !!auth.personalContext.identity &&
    auth.state.user?.id === auth.personalContext.identity.userID &&
    auth.state.session?.id === auth.personalContext.identity.sessionID
  const live = () =>
    authorized() &&
    sameContext(scope.value, workspace.currentReadContext.value) &&
    location.value !== '' &&
    location.value === workspace.paths.value.home + '/inbox' &&
    boundRoute === location.value
  const visible = computed(live),
    reading = computed(() => queue.active !== null)
  const blocked = computed(
    () => !live() || auth.state.busy || reading.value || queue.pending !== null,
  )
  function clear() {
    page.items = Object.freeze([])
    page.nextBefore = null
    page.message = ''
    cursors.clear()
  }
  function retire() {
    ++generation
    queue.pending = null
    const active = queue.active
    queue.active = null
    if (active !== null) auth.inbox.abandonRead()
  }
  function first() {
    retire()
    clear()
    page.phase = 'waiting'
    queue.pending = { query: Object.freeze({ status: status.value, limit: 50 }), append: false }
    pump()
  }
  function pump() {
    if (!live() || !scope.value || !queue.pending || queue.active !== null || auth.state.busy)
      return
    const task = queue.pending,
      context = scope.value,
      serial = ++generation,
      path = boundRoute
    queue.pending = null
    queue.active = serial
    page.phase = 'loading'
    page.message = ''
    page.nextBefore = null
    void execute(task, context, serial, path)
  }
  async function execute(
    task: NonNullable<typeof queue.pending>,
    context: Context,
    serial: number,
    path: string,
  ) {
    const current = () =>
      live() && generation === serial && sameContext(context, scope.value) && path === boundRoute
    try {
      const result = await auth.inbox.list(context.projectID, task.query)
      if (!current()) return
      const prefix = task.append ? page.items : [],
        previous = prefix.at(-1)
      const ids = new Set(prefix.map((item) => item.id)),
        sources = new Set(prefix.map(inboxSourceKey))
      if (
        result.items.some((item) => ids.has(item.id) || sources.has(inboxSourceKey(item))) ||
        (previous && result.items[0] && compareInboxItems(previous, result.items[0]) >= 0) ||
        (result.next_before !== null &&
          (result.next_before === task.query.before || cursors.has(result.next_before)))
      )
        throw new AccountFailure('invalid-response')
      if (task.query.before) cursors.add(task.query.before)
      page.items = Object.freeze([...prefix, ...result.items])
      page.nextBefore = result.next_before
      page.phase = 'ready'
    } catch (error) {
      if (!current()) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      clear()
      if ([401, 403, 404].includes(failure.problem?.status ?? 0)) {
        page.phase = 'unavailable'
        page.message = '当前项目收件箱不可访问，请重新确认项目权限。'
      } else {
        page.phase = 'error'
        page.message =
          task.query.before || failure.problem?.status === 400
            ? '列表已发生变化，请刷新收件箱并从头读取。'
            : failure.kind === 'invalid-response'
              ? '未取得完整有效的收件箱列表，请重试。'
              : '收件箱读取未完成，请重试。'
      }
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
      if (!authorized() || !context || !home || path !== home + '/inbox') {
        if (scope.value || queue.pending || queue.active !== null) {
          scope.value = null
          boundRoute = ''
          retire()
          clear()
          page.phase = 'waiting'
          status.value = 'open'
        }
        return
      }
      if (!sameContext(scope.value, context) || boundRoute !== path) {
        scope.value = context
        boundRoute = path
        status.value = 'open'
        first()
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    page: readonly(page),
    status: readonly(status),
    visible,
    reading,
    blocked,
    filter(value: InboxStatus) {
      if (blocked.value || value === status.value || (value !== 'open' && value !== 'resolved'))
        return false
      status.value = value
      first()
      return true
    },
    refresh() {
      if (!blocked.value) first()
    },
    more() {
      if (blocked.value || page.phase !== 'ready' || !page.nextBefore) return
      queue.pending = {
        query: Object.freeze({ status: status.value, limit: 50, before: page.nextBefore }),
        append: true,
      }
      pump()
    },
    meetingPath(item: InboxItem) {
      return live() && item.project_id === scope.value?.projectID && page.items.includes(item)
        ? workspace.paths.value.home +
            '/meetings/' +
            (item.source_type === 'decision_request'
              ? item.source_context.meeting_id
              : item.source_id)
        : ''
    },
    cancel() {
      retire()
      clear()
      page.phase = 'error'
      page.message = '读取已停止，可在请求结束后重试。'
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
export type ProjectInboxController = ReturnType<typeof useProjectInbox>
