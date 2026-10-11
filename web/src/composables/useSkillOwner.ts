import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import { captureSkillID, type SkillMetadata } from '../api/skill-owner'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export type SkillLocation = Readonly<{ projectPath: string; skillID: string | null }>
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Phase = 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable'
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

export function useSkillOwner(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<SkillLocation>> = computed(() => ({
    projectPath: workspace.paths.value.home,
    skillID: null,
  })),
) {
  const state = shallowReactive({
    phase: 'waiting' as Phase,
    items: Object.freeze([]) as readonly SkillMetadata[],
    detail: null as SkillMetadata | null,
    message: '',
    nextCursor: undefined as string | undefined,
    loadingMore: false,
  })
  const identity = auth.personalContext.identity
  const download = shallowReactive({
    phase: 'idle' as 'idle' | 'loading' | 'saved' | 'error',
    received: 0,
    total: 0,
    message: '',
  })
  let downloadGeneration = 0,
    downloadActive = false,
    packageURL: string | null = null
  const scope = shallowRef<Context | null>(null)
  let route: SkillLocation | null = null,
    generation = 0,
    disposed = false
  const queue = shallowReactive<{
    active: number | null
    pending: (SkillLocation & { cursor?: string }) | null
  }>({
    active: null,
    pending: null,
  })
  const authorized = () =>
    !disposed &&
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    sameIdentity(identity, auth.personalContext.identity) &&
    auth.state.user?.id === identity?.userID &&
    auth.state.session?.id === identity?.sessionID
  const live = () =>
    authorized() &&
    sameContext(scope.value, workspace.currentReadContext.value) &&
    location.value.projectPath !== '' &&
    location.value.projectPath === workspace.paths.value.home
  const visible = computed(live)
  const reading = computed(() => queue.active !== null)
  const busy = computed(() => auth.state.busy || reading.value || queue.pending !== null)
  const blocked = computed(() => !live() || busy.value)
  function clear() {
    clearDownload()
    state.items = Object.freeze([])
    state.detail = null
    state.message = ''
    state.nextCursor = undefined
    state.loadingMore = false
  }
  function retire() {
    clearDownload()
    ++generation
    queue.pending = null
    const previous = queue.active
    queue.active = null
    if (previous !== null) auth.skills.abandon()
  }
  function clearDownload() {
    ++downloadGeneration
    if (downloadActive) auth.skills.abandon()
    downloadActive = false
    if (packageURL) URL.revokeObjectURL(packageURL)
    packageURL = null
    Object.assign(download, { phase: 'idle', received: 0, total: 0, message: '' })
  }
  async function downloadPackage() {
    if (blocked.value || state.phase !== 'ready' || !state.detail || !scope.value) return
    clearDownload()
    const document = state.detail,
      captured = scope.value,
      serial = downloadGeneration
    const current = () =>
      live() &&
      serial === downloadGeneration &&
      state.detail === document &&
      sameContext(captured, scope.value) &&
      location.value.skillID === document.id
    downloadActive = true
    download.phase = 'loading'
    try {
      const blob = await auth.skills.download(
        captured.projectID,
        document.id,
        document.current_revision,
        (received, total) => {
          if (current()) Object.assign(download, { received, total })
        },
      )
      if (!current()) return
      packageURL = URL.createObjectURL(blob)
      const link = window.document.createElement('a')
      link.href = packageURL
      link.download = 'skill-package.zip'
      link.hidden = true
      window.document.body.append(link)
      try {
        link.click()
      } finally {
        link.remove()
      }
      download.phase = 'saved'
      download.message = `修订 ${document.current_revision} 已交给浏览器保存。`
    } catch (error) {
      if (!current()) return
      if (packageURL) URL.revokeObjectURL(packageURL)
      packageURL = null
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([401, 403, 404].includes(failure.problem?.status ?? 0)) {
        clear()
        state.phase = 'unavailable'
        state.message = '当前技能包不存在或不可访问，请重新确认访问权限。'
      } else {
        download.phase = 'error'
        download.received = download.total = 0
        download.message =
          failure.kind === 'cancelled'
            ? '下载已取消。'
            : failure.kind === 'invalid-response'
              ? '未收到完整有效的技能包，未保存文件。请重新下载。'
              : '本次下载未完成，未保存文件。请稍后重新下载。'
      }
    } finally {
      if (serial === downloadGeneration) downloadActive = false
    }
  }
  function schedule(next: SkillLocation) {
    retire()
    clear()
    state.phase = 'waiting'
    queue.pending = next
    pump()
  }
  function pump() {
    if (!live() || !scope.value || queue.active !== null || !queue.pending || auth.state.busy)
      return
    const task = queue.pending,
      captured = scope.value,
      serial = ++generation
    queue.pending = null
    queue.active = serial
    state.loadingMore = task.cursor !== undefined
    if (!state.loadingMore) state.phase = 'loading'
    state.message = ''
    void execute(task, captured, serial)
  }
  async function execute(
    task: SkillLocation & { cursor?: string },
    captured: Context,
    serial: number,
  ) {
    const current = () =>
      live() &&
      serial === generation &&
      sameContext(captured, scope.value) &&
      task.projectPath === location.value.projectPath &&
      task.skillID === location.value.skillID
    try {
      if (task.skillID === null) {
        const result = await auth.skills.list(captured.projectID, {
          limit: 25,
          ...(task.cursor === undefined ? {} : { cursor: task.cursor }),
        })
        if (!current()) return
        if (task.cursor !== undefined) {
          if (
            state.nextCursor !== task.cursor ||
            (result.items.length > 0 &&
              state.items.length > 0 &&
              result.items[0]!.id <= state.items[state.items.length - 1]!.id)
          )
            throw new AccountFailure('invalid-response')
          state.items = Object.freeze([...state.items, ...result.items])
        } else state.items = result.items
        state.nextCursor = result.next_cursor
      } else {
        const result = await auth.skills.get(captured.projectID, captureSkillID(task.skillID))
        if (!current()) return
        state.detail = result
      }
      state.phase = 'ready'
    } catch (error) {
      if (!current()) return
      clear()
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const unavailable = [401, 403, 404].includes(e.problem?.status ?? 0)
      state.phase = unavailable ? 'unavailable' : 'error'
      state.message = unavailable
        ? '当前技能不存在或不可访问。'
        : e.problem?.code === 'CURSOR_INVALID'
          ? '目录游标已失效，请重新读取技能目录。'
          : e.problem?.status === 409
            ? '项目技能库尚未就绪，请稍后重新读取。'
            : e.kind === 'cancelled'
              ? '读取已停止，请明确重读。'
              : e.kind === 'invalid-response'
                ? '未取得有效的完整响应，请重新读取。'
                : '本次读取失败，请重新读取。'
    } finally {
      if (queue.active === serial) {
        queue.active = null
        state.loadingMore = false
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
    ([context, home, next]) => {
      if (disposed) return
      if (!authorized() || !context || !home || next.projectPath !== home) {
        if (scope.value || queue.pending || queue.active !== null) {
          scope.value = null
          route = null
          retire()
          clear()
          state.phase = 'waiting'
        }
        return
      }
      if (
        !sameContext(scope.value, context) ||
        route?.projectPath !== next.projectPath ||
        route?.skillID !== next.skillID
      ) {
        scope.value = context
        route = next
        schedule(next)
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    download: readonly(download),
    downloadPackage,
    cancelDownload() {
      clearDownload()
      download.message = '下载已取消，请在原请求结束后重新下载。'
    },
    visible,
    reading,
    busy,
    blocked,
    loadMore() {
      if (
        blocked.value ||
        location.value.skillID !== null ||
        state.phase !== 'ready' ||
        !state.nextCursor
      )
        return
      queue.pending = { ...location.value, cursor: state.nextCursor }
      pump()
    },
    refresh() {
      if (!blocked.value) schedule(location.value)
    },
    cancel() {
      retire()
      clear()
      state.phase = 'error'
      state.message = '读取已停止，请在原请求结束后明确重读。'
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
export type SkillOwnerController = ReturnType<typeof useSkillOwner>
