import { computed, readonly, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureKnowledgeID,
  compareKnowledgeDocuments,
  knowledgeDefaultReadBytes,
  type KnowledgeContent,
  type KnowledgeDocument,
} from '../api/knowledge-owner'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export type KnowledgePhase =
  | 'waiting'
  | 'idle'
  | 'loading'
  | 'ready'
  | 'empty'
  | 'error'
  | 'unavailable'
  | 'deleted'
  | 'changed'
export type KnowledgeLevel = Readonly<{
  parentID: string | null
  phase: KnowledgePhase
  items: readonly KnowledgeDocument[]
  nextCursor?: string
  message: string
}>
export type KnowledgeLocation = Readonly<{ projectPath: string; documentID: string | null }>
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Task =
  | Readonly<{ kind: 'children'; parent: string | null; cursor?: string; initial?: boolean }>
  | Readonly<{ kind: 'selection'; target: string }>
  | Readonly<{ kind: 'ancestors'; target: string }>
  | Readonly<{ kind: 'content'; target: string; offset: string; index: number; version: string }>
const rootKey = 'root'
const key = (parent: string | null) => parent ?? rootKey
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
const emptyLevel = (parentID: string | null, phase: KnowledgePhase = 'idle'): KnowledgeLevel =>
  Object.freeze({ parentID, phase, items: Object.freeze([]), message: '' })
const emptyAncestors = (): Readonly<{
  phase: KnowledgePhase
  items: readonly KnowledgeDocument[]
  message: string
}> =>
  Object.freeze({
    phase: 'idle' as KnowledgePhase,
    items: Object.freeze([]) as readonly KnowledgeDocument[],
    message: '',
  })
const emptyContent = (
  phase: KnowledgePhase = 'idle',
  message = '',
): Readonly<{
  phase: KnowledgePhase
  value: KnowledgeContent | null
  offset: string
  message: string
}> => Object.freeze({ phase, value: null as KnowledgeContent | null, offset: '0', message })
function failure(error: unknown) {
  const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
  const status = e.problem?.status
  const unavailable = status === 401 || status === 403 || status === 404
  return {
    phase:
      status === 410
        ? ('deleted' as const)
        : unavailable
          ? ('unavailable' as const)
          : ('error' as const),
    message:
      status === 410
        ? '文档已删除。'
        : unavailable
          ? '当前文档不存在或不可访问。'
          : e.kind === 'cancelled'
            ? '读取已停止，请在原请求结束后明确重读。'
            : e.kind === 'invalid-response'
              ? '未取得有效的完整响应，请重新读取。'
              : status === 409
                ? '当前状态已变化，请重新读取文档。'
                : '本次读取失败，请重新读取。',
  }
}

// This page owns observations, never another Cookie queue. Session retains the
// original fetch/body/cancel owner even after a visible read has been retired.
export function useKnowledgeOwner(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<KnowledgeLocation>> = computed(() => ({
    projectPath: workspace.paths.value.home,
    documentID: null,
  })),
) {
  const state = shallowReactive({
    levels: Object.freeze({ [rootKey]: emptyLevel(null, 'waiting') }) as Readonly<
      Record<string, KnowledgeLevel>
    >,
    expanded: Object.freeze([]) as readonly string[],
    selected: null as string | null,
    document: null as KnowledgeDocument | null,
    metadataPhase: 'idle' as KnowledgePhase,
    metadataMessage: '',
    ancestors: emptyAncestors(),
    content: emptyContent(),
  })
  const identity = auth.personalContext.identity
  let scope: Context | null = null,
    generation = 0,
    active: Readonly<{ serial: number; task: Task }> | null = null,
    pending: Task | null = null
  let disposed = false,
    retired = false,
    route: KnowledgeLocation | null = null
  let offsets: string[] = [],
    offsetIndex = 0
  const authorized = () =>
    !disposed &&
    !retired &&
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    sameIdentity(identity, auth.personalContext.identity) &&
    auth.state.user?.id === identity?.userID &&
    auth.state.session?.id === identity?.sessionID
  const live = () =>
    authorized() &&
    sameContext(scope, workspace.currentReadContext.value) &&
    location.value.projectPath !== '' &&
    location.value.projectPath === workspace.paths.value.home
  const busy = computed(() => auth.state.busy || active !== null || pending !== null)
  const blocked = computed(() => !live() || busy.value)
  const visible = computed(live)
  const root = computed(() => state.levels[rootKey]!)
  const canNext = computed(
    () =>
      !blocked.value &&
      state.content.phase === 'ready' &&
      state.content.value !== null &&
      'text' in state.content.value &&
      state.content.value.text.truncated &&
      state.content.value.text.next_byte_offset !== state.content.offset,
  )
  const canPrevious = computed(
    () => !blocked.value && state.content.phase === 'ready' && offsetIndex > 0,
  )
  function level(parent: string | null) {
    return state.levels[key(parent)] ?? emptyLevel(parent)
  }
  function setLevel(value: KnowledgeLevel) {
    state.levels = Object.freeze({ ...state.levels, [key(value.parentID)]: Object.freeze(value) })
  }
  function clearSelection(target: string | null) {
    state.selected = target
    state.document = null
    state.metadataPhase = target ? 'waiting' : 'idle'
    state.metadataMessage = ''
    state.ancestors = emptyAncestors()
    state.content = emptyContent()
    offsets = []
    offsetIndex = 0
  }
  function retireRead() {
    ++generation
    pending = null
    const previous = active
    active = null
    if (previous) auth.knowledge.abandon()
    const message = '读取已停止，请明确重读。'
    for (const value of Object.values(state.levels))
      if (value.phase === 'loading' || value.phase === 'waiting')
        setLevel({ ...value, phase: 'error', message })
    if (state.metadataPhase === 'loading' || state.metadataPhase === 'waiting') {
      state.metadataPhase = 'error'
      state.metadataMessage = message
    }
    if (state.ancestors.phase === 'loading')
      state.ancestors = Object.freeze({ ...state.ancestors, phase: 'error', message })
    if (state.content.phase === 'loading') state.content = emptyContent('error', message)
  }
  function reset() {
    retireRead()
    state.levels = Object.freeze({ [rootKey]: emptyLevel(null, 'waiting') })
    state.expanded = Object.freeze([])
    clearSelection(null)
  }
  function enqueue(task: Task) {
    if (!live()) return
    retireRead()
    if (task.kind === 'selection') clearSelection(task.target)
    pending = task
    pump()
  }
  function pump() {
    if (!pending || active || auth.state.busy || !scope || !live()) return
    const task = pending,
      captured = scope,
      serial = ++generation
    pending = null
    active = { serial, task }
    void execute(task, captured, serial)
  }
  async function readAncestors(target: string, captured: Context, current: () => boolean) {
    state.ancestors = Object.freeze({ ...emptyAncestors(), phase: 'loading' })
    try {
      const items = await auth.knowledge.ancestors(captured.projectID, target)
      if (!current()) return
      if ((items.at(-1)?.id ?? null) !== state.document?.parent_document_id) {
        state.ancestors = Object.freeze({
          ...emptyAncestors(),
          phase: 'changed',
          message: '文档位置已变化，请重新读取路径。',
        })
        return
      }
      state.ancestors = Object.freeze({ phase: 'ready', items, message: '' })
      // These IDs control expansion only. They are never inserted as children.
      state.expanded = Object.freeze([
        ...new Set([...state.expanded, ...items.map((item) => item.id)]),
      ])
    } catch (error) {
      if (current()) state.ancestors = Object.freeze({ ...emptyAncestors(), ...failure(error) })
    }
  }
  async function readContent(
    task: Extract<Task, { kind: 'content' }>,
    captured: Context,
    current: () => boolean,
  ) {
    state.content = emptyContent('loading')
    try {
      const value = await auth.knowledge.readContent(captured.projectID, task.target, {
        byte_offset: task.offset,
        max_bytes: knowledgeDefaultReadBytes,
      })
      if (!current()) return
      if (value.document.content_version !== task.version) {
        state.document = value.document
        offsets = []
        offsetIndex = 0
        state.content = emptyContent('changed', '文档版本已变化，请从开头重新读取；未拼接旧正文。')
        state.ancestors = emptyAncestors()
        return
      }
      state.document = value.document
      if (
        state.ancestors.phase === 'ready' &&
        (state.ancestors.items.at(-1)?.id ?? null) !== value.document.parent_document_id
      )
        state.ancestors = Object.freeze({
          ...emptyAncestors(),
          phase: 'changed',
          message: '文档位置已变化，请重新读取路径。',
        })
      offsets = [...offsets.slice(0, task.index), task.offset]
      offsetIndex = task.index
      const stalled =
        'text' in value && value.text.truncated && value.text.next_byte_offset === task.offset
      state.content = Object.freeze({
        phase: stalled ? 'error' : 'ready',
        value: stalled ? null : value,
        offset: task.offset,
        message: stalled ? '本次正文未向前推进，请从开头重新读取。' : '',
      })
    } catch (error) {
      if (!current()) return
      const result = failure(error)
      state.content = emptyContent(result.phase, result.message)
      if (result.phase === 'deleted' || result.phase === 'unavailable') {
        state.document = null
        state.metadataPhase = result.phase
        state.metadataMessage = result.message
        state.ancestors = emptyAncestors()
      }
    }
  }
  async function execute(task: Task, captured: Context, serial: number) {
    const current = () => live() && generation === serial && sameContext(scope, captured)
    try {
      if (task.kind === 'children') {
        const previous = level(task.parent)
        setLevel({ ...(task.cursor ? previous : emptyLevel(task.parent)), phase: 'loading' })
        try {
          const page = await auth.knowledge.children(captured.projectID, task.parent, {
            limit: 50,
            ...(task.cursor ? { cursor: task.cursor } : {}),
          })
          if (!current()) return
          const items = [...(task.cursor ? previous.items : []), ...page.items]
          const unique = new Map(items.map((item) => [item.id, item]))
          const moved = new Set(page.items.map((item) => item.id))
          for (const other of Object.values(state.levels))
            if (other.parentID !== task.parent && other.items.some((item) => moved.has(item.id)))
              setLevel({
                ...other,
                items: Object.freeze(other.items.filter((item) => !moved.has(item.id))),
              })
          setLevel({
            parentID: task.parent,
            phase: unique.size ? 'ready' : 'empty',
            items: Object.freeze([...unique.values()].sort(compareKnowledgeDocuments)),
            ...(page.next_cursor ? { nextCursor: page.next_cursor } : {}),
            message: '',
          })
        } catch (error) {
          if (current()) setLevel({ ...level(task.parent), ...failure(error) })
        }
      } else if (task.kind === 'selection') {
        state.metadataPhase = 'loading'
        try {
          const head = await auth.knowledge.get(captured.projectID, task.target)
          if (!current()) return
          if ('deleted' in head) {
            state.metadataPhase = 'deleted'
            state.metadataMessage = '文档已删除。'
            state.content = emptyContent('deleted', '文档已删除。')
            return
          }
          state.document = head.active
          state.metadataPhase = 'ready'
          await readAncestors(task.target, captured, current)
          if (current())
            await readContent(
              {
                kind: 'content',
                target: task.target,
                offset: '0',
                index: 0,
                version: head.active.content_version,
              },
              captured,
              current,
            )
        } catch (error) {
          if (current()) {
            const result = failure(error)
            state.metadataPhase = result.phase
            state.metadataMessage = result.message
          }
        }
      } else if (task.kind === 'ancestors') await readAncestors(task.target, captured, current)
      else await readContent(task, captured, current)
    } finally {
      if (active?.serial === serial) active = null
      if (current() && task.kind === 'children' && task.initial && location.value.documentID)
        enqueue({ kind: 'selection', target: location.value.documentID })
      else pump()
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
    ([context, home, nextRoute]) => {
      if (disposed || retired) return
      if (!authorized()) {
        retired = true
        scope = null
        reset()
        return
      }
      if (!context || !home || home !== nextRoute.projectPath) {
        if (scope) {
          scope = null
          reset()
        }
        return
      }
      if (!sameContext(scope, context) || route?.projectPath !== nextRoute.projectPath) {
        reset()
        scope = context
        route = nextRoute
        pending = { kind: 'children', parent: null, initial: true }
      } else if (route?.documentID !== nextRoute.documentID) {
        route = nextRoute
        if (nextRoute.documentID) enqueue({ kind: 'selection', target: nextRoute.documentID })
        else {
          retireRead()
          clearSelection(null)
        }
      }
      pump()
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    root,
    visible,
    busy,
    blocked,
    canNext,
    canPrevious,
    level,
    select(value: string) {
      try {
        enqueue({ kind: 'selection', target: captureKnowledgeID(value) })
      } catch {
        /* Untrusted UI IDs never issue a request. */
      }
    },
    setExpanded(values: readonly string[]) {
      if (!live()) return
      const before = state.expanded
      const next = [...new Set(values)].filter((value) =>
        Object.values(state.levels).some((valueLevel) =>
          valueLevel.items.some((item) => item.id === value),
        ),
      )
      state.expanded = Object.freeze(next)
      const added = next.find((value) => !before.includes(value))
      if (added && level(added).phase === 'idle') enqueue({ kind: 'children', parent: added })
    },
    refreshTree() {
      if (live()) {
        retireRead()
        state.expanded = Object.freeze([])
        state.levels = Object.freeze({ [rootKey]: emptyLevel(null) })
        enqueue({ kind: 'children', parent: null })
      }
    },
    readLevel(parent: string | null, more = false) {
      if (!blocked.value) {
        const value = level(parent)
        if (!more || value.nextCursor)
          enqueue({ kind: 'children', parent, ...(more ? { cursor: value.nextCursor } : {}) })
      }
    },
    retryDocument() {
      if (state.selected && !blocked.value) enqueue({ kind: 'selection', target: state.selected })
    },
    retryAncestors() {
      if (state.selected && state.document && !blocked.value)
        enqueue({ kind: 'ancestors', target: state.selected })
    },
    next() {
      const value = state.content.value
      if (canNext.value && state.selected && state.document && value && 'text' in value)
        enqueue({
          kind: 'content',
          target: state.selected,
          offset: value.text.next_byte_offset,
          index: offsetIndex + 1,
          version: state.document.content_version,
        })
    },
    previous() {
      if (canPrevious.value && state.selected && state.document)
        enqueue({
          kind: 'content',
          target: state.selected,
          offset: offsets[offsetIndex - 1]!,
          index: offsetIndex - 1,
          version: state.document.content_version,
        })
    },
    cancel: retireRead,
    dispose() {
      disposed = true
      stop()
      reset()
      scope = null
    },
  }
}
export type KnowledgeOwnerController = ReturnType<typeof useKnowledgeOwner>
