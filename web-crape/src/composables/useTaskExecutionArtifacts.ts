import { computed, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import {
  compareArtifacts,
  sameArtifactRef,
  type ArtifactContent,
  type ArtifactMetadata,
  type ArtifactPage,
  type ArtifactQuery,
  type ArtifactReadQuery,
  type ArtifactRef,
} from '../api/project-artifacts'

export type TaskExecutionArtifactScope = Readonly<{
  projectID: string
  taskID: string
  executionID: string
  key: string
}>
export type TaskExecutionArtifactReads = {
  list(project: string, query: ArtifactQuery): Promise<ArtifactPage>
  read(ref: ArtifactRef, query: ArtifactReadQuery): Promise<ArtifactContent>
}
type Phase = 'idle' | 'loading' | 'ready' | 'error'

// Reads use the Task Session owner. Retiring a view never releases that owner's
// actual request tail, and a late result cannot repopulate another execution.
export function createTaskExecutionArtifacts(
  scope: Readonly<Ref<TaskExecutionArtifactScope | null>>,
  busy: Readonly<Ref<boolean>>,
  reads?: TaskExecutionArtifactReads,
) {
  const state = shallowReactive({
    phase: 'idle' as Phase,
    items: Object.freeze([]) as readonly ArtifactMetadata[],
    next: null as string | null,
    message: '',
    selected: null as ArtifactMetadata | null,
    content: null as ArtifactContent | null,
    contentPhase: 'idle' as Phase,
    contentMessage: '',
    active: 0,
    pageIndex: 0,
  })
  let generation = 0,
    disposed = false,
    offsets: string[] = [],
    cursors = new Set<string>()
  const visible = computed(() => !disposed && !!reads && scope.value !== null)
  const blocked = computed(() => !visible.value || busy.value || state.active !== 0)
  function clearContent() {
    state.selected = null
    state.content = null
    state.contentPhase = 'idle'
    state.contentMessage = ''
    state.pageIndex = 0
    offsets = []
  }
  function reset() {
    ++generation
    Object.assign(state, {
      phase: 'idle',
      items: Object.freeze([]),
      next: null,
      message: '',
      active: 0,
    })
    cursors = new Set()
    clearContent()
  }
  const stop = watch(() => scope.value?.key ?? '', reset, { flush: 'sync' })
  function current(captured: TaskExecutionArtifactScope, serial: number) {
    return !disposed && generation === serial && scope.value?.key === captured.key
  }
  function owns(item: ArtifactMetadata, captured: TaskExecutionArtifactScope) {
    return (
      item.artifact_ref.project_id === captured.projectID &&
      item.source_execution_id === captured.executionID
    )
  }
  function inaccessible(error: unknown) {
    return (
      error instanceof AccountFailure && [401, 403, 404, 410].includes(error.problem?.status ?? 0)
    )
  }
  async function load(more = false) {
    const captured = scope.value
    if (!captured || !reads || blocked.value || (more && !state.next)) return false
    const cursor = more ? state.next : null,
      previous = more ? state.items : Object.freeze([]),
      serial = ++generation
    state.active = serial
    state.phase = 'loading'
    state.message = ''
    clearContent()
    if (!more) {
      state.items = Object.freeze([])
      state.next = null
      cursors = new Set()
    }
    try {
      const result = await reads.list(captured.projectID, {
        execution_id: captured.executionID,
        limit: 50,
        ...(cursor ? { cursor } : {}),
      })
      if (!current(captured, serial)) return false
      const last = previous.at(-1)
      if (
        result.items.some(
          (item) =>
            !owns(item, captured) ||
            previous.some((row) => row.artifact_ref.artifact_id === item.artifact_ref.artifact_id),
        ) ||
        (last && result.items[0] && compareArtifacts(last, result.items[0]) >= 0) ||
        (result.next_cursor !== null &&
          (result.next_cursor === cursor || cursors.has(result.next_cursor)))
      )
        throw new AccountFailure('invalid-response')
      if (cursor) cursors.add(cursor)
      state.items = Object.freeze([...previous, ...result.items])
      state.next = result.next_cursor
      state.phase = 'ready'
      return true
    } catch (error) {
      if (!current(captured, serial)) return false
      if (inaccessible(error)) {
        state.items = Object.freeze([])
        state.next = null
      }
      state.phase = 'error'
      state.message = inaccessible(error)
        ? '成果不存在或不可访问，请重新确认访问权限。'
        : '未取得完整的执行成果列表，请重新读取。'
      return false
    } finally {
      if (current(captured, serial)) state.active = 0
    }
  }
  async function read(item: ArtifactMetadata, offset: string, index: number) {
    const captured = scope.value
    if (!captured || !reads || blocked.value || !owns(item, captured)) return false
    const serial = ++generation
    state.active = serial
    state.selected = item
    state.content = null
    state.contentPhase = 'loading'
    state.contentMessage = ''
    try {
      const result = await reads.read(item.artifact_ref, { offset, limit: 8192 })
      if (!current(captured, serial)) return false
      const artifact = result.artifact
      if (
        !owns(artifact, captured) ||
        !sameArtifactRef(artifact.artifact_ref, item.artifact_ref) ||
        artifact.version !== item.version ||
        artifact.object.sha256 !== item.object.sha256 ||
        artifact.object.byte_size !== item.object.byte_size ||
        artifact.source_operation_id !== item.source_operation_id ||
        (result.projection === 'text' &&
          (result.offset !== offset ||
            (result.truncated && BigInt(result.next_offset) <= BigInt(result.offset))))
      )
        throw new AccountFailure('invalid-response')
      offsets = [...offsets.slice(0, index), offset]
      state.content = result
      state.pageIndex = index
      state.contentPhase = 'ready'
      return true
    } catch (error) {
      if (!current(captured, serial)) return false
      if (inaccessible(error)) {
        clearContent()
        state.items = Object.freeze([])
        state.next = null
        state.phase = 'error'
        state.message = '成果不存在或不可访问，请重新读取成果列表。'
      } else {
        state.contentPhase = 'error'
        state.contentMessage = '未取得同一成果的完整有效内容，请重新读取。'
      }
      return false
    } finally {
      if (current(captured, serial)) state.active = 0
    }
  }
  function select(ref: ArtifactRef) {
    const item = state.items.find((row) => sameArtifactRef(row.artifact_ref, ref))
    if (!item || blocked.value) return Promise.resolve(false)
    offsets = []
    return read(item, '0', 0)
  }
  function page(direction: 'previous' | 'next') {
    const item = state.selected,
      content = state.content
    if (!item || !content || state.contentPhase !== 'ready') return Promise.resolve(false)
    if (direction === 'next')
      return content.truncated
        ? read(item, content.next_offset, state.pageIndex + 1)
        : Promise.resolve(false)
    const offset = offsets[state.pageIndex - 1]
    return offset !== undefined ? read(item, offset, state.pageIndex - 1) : Promise.resolve(false)
  }
  function dispose() {
    disposed = true
    stop()
    reset()
  }
  return { state, visible, blocked, load, select, page, dispose }
}
export type TaskExecutionArtifacts = ReturnType<typeof createTaskExecutionArtifacts>
