import { onScopeDispose, reactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import type { MeetingReference } from '../api/meetings'
import { taskStates, type TaskState } from '../api/work-review'
import { useSession, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export type MeetingResourceKind = 'task' | 'knowledge' | 'file'
export type MeetingReferenceChoice = Readonly<{
  id: string
  title: string
  kind: MeetingResourceKind | 'milestone' | 'sprint'
}>
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const same = (a: Context, b: Context | null) =>
  !!b &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration &&
  a.identity.userID === b.identity.userID &&
  a.identity.sessionID === b.identity.sessionID &&
  a.identity.epoch === b.identity.epoch

// Selection reads reuse the Session's original domain read owner. This local
// controller only owns observations and drops late results after scope changes.
export function useMeetingReferenceChoices(
  meeting: () => string | null,
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const state = reactive({
    kind: null as MeetingResourceKind | null,
    phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    items: [] as MeetingReferenceChoice[],
    next: '',
    message: '',
    milestone: null as MeetingReferenceChoice | null,
    sprint: null as MeetingReferenceChoice | null,
    parents: [] as MeetingReferenceChoice[],
    taskState: 'todo' as TaskState,
    fileName: '',
  })
  let revision = 0,
    disposed = false,
    reading: MeetingResourceKind | null = null
  const cursors = new Set<string>()
  function retire() {
    ++revision
    const original = reading
    reading = null
    if (original === 'task') auth.workReview.abandonRead()
    else if (original === 'knowledge') auth.knowledge.abandon()
    else if (original === 'file') auth.projectArtifacts.abandon()
  }
  function clear() {
    state.items = []
    state.next = state.message = ''
    state.phase = 'idle'
    cursors.clear()
  }
  async function load(more = false) {
    const scope = workspace.currentReadContext.value,
      kind = state.kind
    if (
      disposed ||
      !scope ||
      meeting() === '' ||
      !kind ||
      reading ||
      auth.state.busy ||
      (more && !state.next)
    )
      return
    const captured: Context = { ...scope, identity: { ...scope.identity } },
      target = meeting(),
      own = ++revision
    const cursor = more ? state.next : ''
    if (!more) clear()
    state.phase = 'loading'
    state.message = ''
    reading = kind
    const live = () =>
      !disposed &&
      revision === own &&
      meeting() === target &&
      same(captured, workspace.currentReadContext.value)
    try {
      const query = { limit: 50, ...(cursor ? { cursor } : {}) }
      let items: MeetingReferenceChoice[], next: string
      if (kind === 'task') {
        if (!state.milestone) {
          const page = await auth.workReview.milestones(captured.projectID, query)
          items = page.items.map((x) => ({ id: x.id, title: x.title, kind: 'milestone' }))
          next = page.next_cursor ?? ''
        } else if (!state.sprint) {
          const page = await auth.workReview.sprints(captured.projectID, state.milestone.id, query)
          items = page.items.map((x) => ({ id: x.id, title: x.title, kind: 'sprint' }))
          next = page.next_cursor ?? ''
        } else {
          const page = await auth.workReview.tasks(captured.projectID, {
            ...query,
            sprint_id: state.sprint.id,
            state: state.taskState,
          })
          items = page.items.map((x) => ({ id: x.id, title: x.title, kind: 'task' }))
          next = page.next_cursor ?? ''
        }
      } else if (kind === 'knowledge') {
        const page = await auth.knowledge.children(
          captured.projectID,
          state.parents.at(-1)?.id ?? null,
          query,
        )
        items = page.items.map((x) => ({ id: x.id, title: x.title, kind: 'knowledge' }))
        next = page.next_cursor ?? ''
      } else {
        const page = await auth.projectArtifacts.list(captured.projectID, {
          ...query,
          ...(state.fileName ? { name_query: state.fileName } : {}),
        })
        items = page.items.map((x) => ({
          id: x.artifact_ref.artifact_id,
          title: x.name,
          kind: 'file',
        }))
        next = page.next_cursor ?? ''
      }
      if (!live()) return
      const combined = more ? [...state.items, ...items] : items
      if (
        new Set(combined.map((x) => x.id)).size !== combined.length ||
        (next && (next === cursor || cursors.has(next)))
      )
        throw new AccountFailure('invalid-response')
      if (cursor) cursors.add(cursor)
      state.items = combined
      state.next = next
      state.phase = 'ready'
    } catch (error) {
      if (!live()) return
      const fault = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([401, 403, 404, 410].includes(fault.problem?.status ?? 0)) state.items = []
      state.next = ''
      state.phase = 'error'
      state.message = [401, 403, 404, 410].includes(fault.problem?.status ?? 0)
        ? '资源不存在或当前无权访问。'
        : '资源列表未能完整读取，请重试。'
    } finally {
      if (revision === own) reading = null
    }
  }
  async function changeKind(kind: MeetingResourceKind) {
    if (reading || auth.state.busy) return
    state.kind = kind
    state.milestone = state.sprint = null
    state.parents = []
    state.taskState = 'todo'
    state.fileName = ''
    await load()
  }
  async function browse(item: MeetingReferenceChoice) {
    if (
      state.phase !== 'ready' ||
      reading ||
      auth.state.busy ||
      !state.items.some((x) => x.id === item.id && x.kind === item.kind)
    )
      return
    if (item.kind === 'milestone') state.milestone = item
    else if (item.kind === 'sprint') state.sprint = item
    else if (item.kind === 'knowledge') state.parents.push(item)
    else return
    await load()
  }
  async function back() {
    if (reading || auth.state.busy) return
    if (state.kind === 'knowledge') state.parents.pop()
    else if (state.sprint) state.sprint = null
    else state.milestone = null
    await load()
  }
  async function changeTaskState(value: string) {
    const next = taskStates.find((candidate) => candidate === value)
    if (!next || reading || auth.state.busy) return
    state.taskState = next
    await load()
  }
  function reference(item: MeetingReferenceChoice): MeetingReference | null {
    if (item.kind === 'milestone' || item.kind === 'sprint') return null
    if (
      state.phase !== 'ready' ||
      reading ||
      auth.state.busy ||
      item.kind !== state.kind ||
      !state.items.some((x) => x.id === item.id && x.kind === item.kind && x.title === item.title)
    )
      return null
    return { kind: item.kind, id: item.id, label: item.title }
  }
  watch(
    () => [workspace.currentReadContext.value, meeting()],
    () => {
      retire()
      clear()
      state.kind = null
      state.milestone = state.sprint = null
      state.parents = []
    },
    { flush: 'sync' },
  )
  onScopeDispose(() => {
    disposed = true
    retire()
  })
  return { state, load, changeKind, browse, back, changeTaskState, reference }
}
