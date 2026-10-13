import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureArtifactQuery,
  compareArtifacts,
  sameArtifactRef,
  type ArtifactMetadata,
  type ArtifactContent,
  type ArtifactRef,
  type CapturedArtifactQuery,
} from '../api/project-artifacts'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Phase = 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable'
type Read =
  | { type: 'list'; query: CapturedArtifactQuery; append: boolean; created?: ArtifactMetadata }
  | { type: 'content'; ref: ArtifactRef; offset: string; index: number }
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
export function useProjectArtifacts(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<string>> = computed(() => workspace.paths.value.home + '/artifacts'),
) {
  const page = shallowReactive({
    phase: 'waiting' as Phase,
    items: Object.freeze([]) as readonly ArtifactMetadata[],
    nextCursor: null as string | null,
    message: '',
  })
  const detail = shallowReactive({
    phase: 'waiting' as Phase,
    content: null as ArtifactContent | null,
    message: '',
    index: 0,
  })
  const creationRead = shallowReactive<{ phase: 'idle' | 'loading' | 'ready' | 'error'; ref: ArtifactRef | null }>({ phase: 'idle', ref: null })
  const draft = shallowReactive({ name: '', kind: '' })
  const applied = shallowRef<CapturedArtifactQuery>(Object.freeze({ limit: 50 }))
  const selected = shallowRef<ArtifactMetadata | null>(null),
    scope = shallowRef<Context | null>(null)
  const queue = shallowReactive<{ active: number | null; pending: Read | null }>({
    active: null,
    pending: null,
  })
  let generation = 0,
    disposed = false,
    boundRoute = '',
    cursors = new Set<string>(),
    offsets: string[] = []
  let contentIdentity: ArtifactMetadata | null = null
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
    location.value === workspace.paths.value.home + '/artifacts' &&
    boundRoute === location.value
  const visible = computed(live),
    reading = computed(() => queue.active !== null)
  const blocked = computed(
    () => !live() || auth.state.busy || reading.value || queue.pending !== null,
  )
  function clearDetail() {
    selected.value = null
    detail.content = null
    detail.message = ''
    detail.phase = 'waiting'
    detail.index = 0
    offsets = []
    contentIdentity = null
  }
  function clearList() {
    page.items = Object.freeze([])
    page.nextCursor = null
    page.message = ''
    cursors = new Set()
    clearDetail()
  }
  function retire() {
    creationRead.phase = 'idle'
    creationRead.ref = null
    ++generation
    queue.pending = null
    const active = queue.active
    queue.active = null
    if (active !== null) auth.projectArtifacts.abandon()
  }
  function firstPage() {
    retire()
    clearList()
    page.phase = 'waiting'
    queue.pending = { type: 'list', query: applied.value, append: false }
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
    if (task.type === 'list') {
      page.phase = 'loading'
      page.nextCursor = null
      page.message = ''
      clearDetail()
    } else {
      detail.phase = 'loading'
      detail.content = null
      detail.message = ''
    }
    void execute(task, context, serial, path)
  }
  async function execute(task: Read, context: Context, serial: number, path: string) {
    const current = () =>
      live() && serial === generation && sameContext(context, scope.value) && path === boundRoute
    try {
      if (task.type === 'list') {
        const result = await auth.projectArtifacts.list(context.projectID, task.query)
        if (!current()) return
        const prefix = task.append ? page.items : [],
          previous = prefix.at(-1),
          ids = new Set(prefix.map((item) => item.artifact_ref.artifact_id))
        if (
          result.items.some((item) => ids.has(item.artifact_ref.artifact_id)) ||
          (previous &&
            result.items.length > 0 &&
            compareArtifacts(previous, result.items[0]!) >= 0) ||
          (result.next_cursor !== null &&
            (result.next_cursor === task.query.cursor || cursors.has(result.next_cursor)))
        )
          throw new AccountFailure('invalid-response')
        if (task.query.cursor) cursors.add(task.query.cursor)
        page.items = Object.freeze([...prefix, ...result.items])
        page.nextCursor = result.next_cursor
        page.phase = 'ready'
        if (task.created) {
          // A receipt is historical metadata, never a fabricated list item. The
          // current content is authorized separately using its complete ref.
          selected.value = task.created
          queue.pending = { type: 'content', ref: task.created.artifact_ref, offset: '0', index: 0 }
        }
      } else {
        const result = await auth.projectArtifacts.read(task.ref, {
          offset: task.offset,
          limit: 8192,
        })
        if (
          !current() ||
          !selected.value ||
          !sameArtifactRef(selected.value.artifact_ref, task.ref)
        )
          return
        if (
          task.index > 0 &&
          contentIdentity &&
          (result.artifact.version !== contentIdentity.version ||
            result.artifact.object.sha256 !== contentIdentity.object.sha256 ||
            result.artifact.object.byte_size !== contentIdentity.object.byte_size)
        )
          throw new AccountFailure('invalid-response')
        contentIdentity = result.artifact
        offsets = [...offsets.slice(0, task.index), task.offset]
        selected.value = result.artifact
        detail.content = result
        detail.index = task.index
        detail.phase = 'ready'
        if (creationRead.ref && sameArtifactRef(creationRead.ref, task.ref)) creationRead.phase = 'ready'
        if (
          result.projection === 'text' &&
          result.truncated &&
          result.next_offset === result.offset
        )
          detail.message = '当前片段未能推进，请重新读取。'
      }
    } catch (error) {
      if (!current()) return
      if (creationRead.phase === 'loading') creationRead.phase = 'error'
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const status = failure.problem?.status ?? 0
      if (status === 401 || status === 403 || status === 404) {
        clearList()
        page.phase = 'unavailable'
        page.message = '当前产物不存在或不可访问，请重新确认访问权限。'
      } else if (task.type === 'content') {
        detail.content = null
        offsets = []
        contentIdentity = null
        detail.index = 0
        if (status === 410) {
          clearDetail()
          page.message = '所选产物已不存在，请刷新产物列表。'
        } else {
          detail.phase = 'error'
          detail.message =
            status === 416
              ? '当前内容范围已失效，请重新读取。'
              : failure.kind === 'invalid-response'
                ? '未取得完整有效的产物内容，请重新读取。'
                : '本次内容读取未完成，请重新读取。'
        }
      } else {
        clearList()
        page.phase = 'error'
        page.message = ['CURSOR_INVALID', 'CURSOR_STALE'].includes(failure.problem?.code ?? '')
          ? '分页已失效，请刷新产物并从第一页重新读取。'
          : failure.kind === 'invalid-response'
            ? '未取得完整有效的产物列表，请刷新产物。'
            : '本次读取未完成，请刷新产物重试。'
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
      if (!authorized() || !context || !home || path !== home + '/artifacts') {
        if (scope.value || queue.pending || queue.active !== null) {
          scope.value = null
          boundRoute = ''
          retire()
          clearList()
          page.phase = 'waiting'
          draft.name = ''
          draft.kind = ''
          applied.value = Object.freeze({ limit: 50 })
        }
        return
      }
      if (!sameContext(scope.value, context) || boundRoute !== path) {
        scope.value = context
        boundRoute = path
        draft.name = ''
        draft.kind = ''
        applied.value = Object.freeze({ limit: 50 })
        firstPage()
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  function read(offset: string, index: number) {
    if (blocked.value || !selected.value) return
    queue.pending = { type: 'content', ref: selected.value.artifact_ref, offset, index }
    pump()
  }
  function apply(clear: boolean) {
    if (blocked.value) return false
    try {
      const query = captureArtifactQuery({
        limit: 50,
        ...(!clear && draft.name !== '' ? { name_query: draft.name } : {}),
        ...(!clear && draft.kind !== '' ? { kind: draft.kind } : {}),
      })
      if (clear) {
        draft.name = ''
        draft.kind = ''
      }
      applied.value = query
      firstPage()
      return true
    } catch {
      page.message = '请输入不超过 256 字节的产物名称，并选择有效类型。'
      return false
    }
  }
  return {
    page: readonly(page),
    creationRead: readonly(creationRead),
    refreshCreated(artifact: ArtifactMetadata) {
      if (blocked.value || artifact.artifact_ref.project_id !== scope.value?.projectID) return false
      retire()
      clearList()
      draft.name = ''
      draft.kind = ''
      applied.value = Object.freeze({ limit: 50 })
      page.phase = 'waiting'
      creationRead.ref = artifact.artifact_ref
      creationRead.phase = 'loading'
      queue.pending = { type: 'list', query: applied.value, append: false, created: artifact }
      pump()
      return true
    },
    detail: readonly(detail),
    selected: readonly(selected),
    applied: readonly(applied),
    draft,
    visible,
    reading,
    blocked,
    canNext: computed(
      () =>
        !blocked.value &&
        detail.phase === 'ready' &&
        detail.content?.projection === 'text' &&
        detail.content.truncated &&
        detail.content.next_offset !== detail.content.offset,
    ),
    apply() {
      return apply(false)
    },
    clearFilter() {
      return apply(true)
    },
    refresh() {
      if (!blocked.value) firstPage()
    },
    more() {
      if (blocked.value || page.phase !== 'ready' || !page.nextCursor) return
      queue.pending = {
        type: 'list',
        append: true,
        query: Object.freeze({ ...applied.value, cursor: page.nextCursor }),
      }
      pump()
    },
    select(id: string) {
      if (blocked.value || page.phase !== 'ready') return false
      const item = page.items.find((item) => item.artifact_ref.artifact_id === id)
      if (!item) return false
      clearDetail()
      selected.value = item
      read('0', 0)
      return true
    },
    closeDetail() {
      retire()
      clearDetail()
    },
    reread() {
      read('0', 0)
    },
    next() {
      if (
        !blocked.value &&
        detail.content?.truncated &&
        detail.content.next_offset !== detail.content.offset
      )
        read(detail.content.next_offset, detail.index + 1)
    },
    previous() {
      if (detail.index > 0 && offsets[detail.index - 1] !== undefined)
        read(offsets[detail.index - 1]!, detail.index - 1)
    },
    cancel() {
      retire()
      clearList()
      page.phase = 'error'
      page.message = '读取已停止，请在原请求结束后刷新产物。'
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      retire()
      clearList()
      scope.value = null
    },
  }
}
export type ProjectArtifactsController = ReturnType<typeof useProjectArtifacts>
