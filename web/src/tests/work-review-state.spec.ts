import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import {
  taskStates,
  type Task,
  type TaskQuery,
  type ReviewInput,
  type ReviewReceipt,
  type ReviewLookup,
  type Page,
  type TaskSummary,
} from '../api/work-review'
import type {
  TaskCreateDraft,
  TaskCreateLookup,
  TaskCreateReceipt,
} from '../api/work-task-planning'
import type { TaskUpdateInput, TaskUpdateReceipt, TaskUpdateLookup } from '../api/work-task-edit'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController, WorkReviewProgress } from '../composables/useSession'
import type { createProjectWorkspace } from '../composables/useProjectWorkspace'
const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const time = '2026-10-08T10:00:00.000000Z'
const base = {
  project_id: id(1),
  title: '真实任务标题',
  manual_rank: '8'.repeat(32),
  version: '1',
  created_at: time,
  updated_at: time,
}
const milestone = { ...base, id: id(2), title: '交付目标', description: '' }
const sprint = {
  ...base,
  id: id(3),
  milestone_id: id(2),
  title: '当前迭代',
  state: 'current' as const,
  description: '',
  started_at: time,
  completed_at: null,
}
const task: Task = {
  ...base,
  id: id(4),
  milestone_id: id(2),
  sprint_id: id(3),
  state: 'in_progress',
  type: 'task',
  priority: 'high',
  assignee_agent_id: id(5),
  description: '工作内容',
  plan: '',
}
const agent = {
  id: id(5),
  project_id: id(1),
  name: 'actual-agent',
  display_name: '真实 Agent',
  tag_color: null,
  description: '',
  version: '1',
  created_at: time,
  updated_at: time,
}
let owner: ProjectTasks | undefined
// These controlled reads test publication/selection, not backend authorization or real transactions.
function fixture(current: string | null = id(3)) {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<{
    identity: typeof identity
    projectID: string
    generation: number
    readGeneration: number
  } | null>({ identity, projectID: id(1), generation: 1, readGeneration: 1 })
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const progress = shallowRef<WorkReviewProgress | null>(null)
  let stored = { ...task }
  const api = {
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [sprint] })),
    sprint: vi.fn(async (_p: string, target: string) => ({ ...sprint, id: target })),
    tasks: vi.fn(async (_p: string, q: TaskQuery): Promise<Page<TaskSummary>> => ({
      items: q.state === stored.state ? [{ ...stored, sprint_id: q.sprint_id }] : [],
    })),
    task: vi.fn(async (_p: string, target: string) => ({ ...stored, id: target })),
    blockers: vi.fn(async () => ({ items: [] })),
    agents: vi.fn(async () => ({ items: [agent] })),
    agent: vi.fn(async () => agent),
    abandonRead: vi.fn(),
    startTransfer: vi.fn(
      async (_p: string, _t: string, input: ReviewInput): Promise<ReviewReceipt> => {
        stored = {
          ...stored,
          version: String(BigInt(stored.version) + 1n),
          state: input.request.target_state,
          assignee_agent_id: input.request.assignee_agent_id ?? stored.assignee_agent_id,
        }
        return { task: stored, task_event_ids: [id(21)], event_ids: [id(22)] }
      },
    ),
    startCreate: vi.fn(async (_p: string, input: TaskCreateDraft): Promise<TaskCreateReceipt> => ({
      task: {
        ...stored,
        ...input,
        id: id(40),
        state: 'backlog',
        assignee_agent_id: null,
        version: '1',
      },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    })),
    startEdit: vi.fn(
      async (_p: string, _t: string, input: TaskUpdateInput): Promise<TaskUpdateReceipt> => ({
        task: { ...stored, ...input.request },
        changed: false,
        task_event_id: null,
        event_ids: [],
      }),
    ),
    checkOriginal: vi.fn(async (): Promise<ReviewLookup | TaskCreateLookup | TaskUpdateLookup> => ({
      status: 'not_observed',
      receipt: null,
    })),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    get progress() {
      return progress.value
    },
  }
  const auth = {
    state: authState,
    personalContext: personal,
    workReview: api,
    workMilestonePlanning: {
      progress: null,
      create: vi.fn(async () => {
        throw new Error('Unexpected Milestone Create')
      }),
      checkOriginal: vi.fn(async () => {
        throw new Error('Unexpected Milestone Lookup')
      }),
      editRejected: vi.fn(),
      abandon: vi.fn(),
    },
    workSprintLifecycle: { progress: null },
  } as unknown as SessionController
  const project = shallowReactive({ id: id(1), lifecycle: 'active', current_sprint_id: current })
  const workspace = {
    currentReadContext: context,
    detail: { project },
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ReturnType<typeof createProjectWorkspace>
  const navigate = vi.fn(async (path: string) => {
    owner!.afterNavigation(path)
  })
  owner = createProjectTasks(auth, workspace, navigate)
  return {
    owner,
    api,
    progress,
    context,
    personal,
    authState,
    project,
    navigate,
    setStored: (value: Task) => {
      stored = value
    },
  }
}
afterEach(() => {
  owner?.dispose()
  owner = undefined
  vi.restoreAllMocks()
})
describe('Task keyword state', () => {
  it('searches all seven columns, resets pagination and keeps the applied query when opening details', async () => {
    const f = fixture()
    f.api.tasks.mockImplementation(async (_p, q) => ({
      items: q.state === 'in_progress' ? [{ ...task, id: q.cursor ? id(42) : task.id }] : [],
      ...(q.state === 'in_progress' && !q.cursor
        ? { next_cursor: q.text ? 'search-next' : 'all-next' }
        : {}),
    }))
    f.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    expect(f.owner.columns.in_progress.next).toBe('all-next')
    f.api.tasks.mockClear()
    f.owner.search.draft = 'literal %_'
    f.owner.search.priorityDraft = 'high'
    f.owner.search.typeDraft = 'task'
    await flushPromises()
    expect(f.api.tasks).not.toHaveBeenCalled()
    await f.owner.applySearch()
    expect(f.api.tasks.mock.calls.map(([, q]) => q)).toEqual(
      taskStates.map((state) => ({
        sprint_id: id(3),
        state,
        limit: 50,
        text: 'literal %_',
        priority: 'high',
        type: 'task',
      })),
    )
    expect(f.owner.columns.in_progress.next).toBe('search-next')
    f.owner.search.priorityDraft = 'low'
    f.owner.search.typeDraft = 'bug'
    await f.owner.loadColumn('in_progress', true)
    expect(f.api.tasks.mock.calls.at(-1)?.[1]).toMatchObject({
      text: 'literal %_',
      cursor: 'search-next',
      priority: 'high',
      type: 'task',
    })
    expect(f.owner.columns.in_progress.items.map((row) => row.id)).toEqual([id(4), id(42)])
    await f.owner.selectTask(id(4))
    await flushPromises()
    expect(f.owner.state.task?.id).toBe(id(4))
    expect(f.owner.search.text).toBe('literal %_')
    await f.owner.closeTask()
    await flushPromises()
    f.api.tasks.mockClear()
    await f.owner.clearSearch()
    expect(f.api.tasks.mock.calls.map(([, q]) => q)).toEqual(
      taskStates.map((state) => ({
        sprint_id: id(3),
        state,
        limit: 50,
      })),
    )
    expect(f.owner.columns.in_progress.items.map((row) => row.id)).toEqual([id(4)])
    expect(f.owner.search).toEqual({
      draft: '',
      text: '',
      message: '',
      priorityDraft: '',
      typeDraft: '',
      priority: '',
      type: '',
    })
  })

  it('clears a keyword on Sprint change and exposes empty results without dropping invalid input', async () => {
    const f = fixture()
    f.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    f.api.tasks.mockClear()
    f.owner.search.draft = '  '
    await f.owner.applySearch()
    expect(f.api.tasks).not.toHaveBeenCalled()
    expect(f.owner.search.draft).toBe('  ')
    expect(f.owner.search.message).toContain('纯空白')
    f.api.tasks.mockResolvedValue({ items: [] })
    f.owner.search.draft = 'not found'
    f.owner.search.priorityDraft = 'low'
    f.owner.search.typeDraft = 'bug'
    await f.owner.applySearch()
    expect(f.owner.searchEmpty.value).toBe(true)
    await f.owner.selectSprint(id(33))
    await flushPromises()
    expect(f.owner.search.text).toBe('')
    expect(f.owner.search.priority).toBe('')
    expect(f.owner.search.type).toBe('')
    expect(f.owner.search.priorityDraft).toBe('')
    expect(f.owner.search.typeDraft).toBe('')
    expect(f.owner.searchEmpty.value).toBe(false)
    const query = f.api.tasks.mock.calls.at(-1)?.[1]
    expect(query).not.toHaveProperty('text')
    expect(query).not.toHaveProperty('priority')
    expect(query).not.toHaveProperty('type')
  })

  it('does not publish an old search page after project retirement or retain its keyword', async () => {
    const f = fixture()
    f.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    let release!: (page: Page<TaskSummary>) => void
    const response = new Promise<Page<TaskSummary>>((resolve) => {
      release = resolve
    })
    f.api.tasks.mockImplementationOnce(() => response)
    f.owner.search.draft = 'old-project'
    f.owner.search.priorityDraft = 'high'
    f.owner.search.typeDraft = 'task'
    const searching = f.owner.applySearch()
    try {
      await flushPromises()
      expect(f.owner.busy.value).toBe(true)
      f.context.value = { ...f.context.value!, projectID: id(91) }
      await flushPromises()
      expect(f.owner.search.text).toBe('')
      release({
        items: [{ ...task, state: 'backlog', title: 'late old match' }],
        next_cursor: 'old-cursor',
      })
      await searching
      await flushPromises()
      expect(f.owner.columns.backlog.items).toEqual([])
      expect(f.owner.columns.backlog.next).toBeUndefined()
      expect(f.owner.search.draft).toBe('')
      expect(f.owner.search.priority).toBe('')
      expect(f.owner.search.type).toBe('')
      expect(f.owner.search.priorityDraft).toBe('')
      expect(f.owner.search.typeDraft).toBe('')
    } finally {
      release({ items: [] })
      await searching
    }
  })
})
describe('Task enum filters state', () => {
  it('supports enum-only empty results and keeps invalid selections out of the current query', async () => {
    const f = fixture()
    f.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    f.api.tasks.mockResolvedValue({ items: [] })
    f.api.tasks.mockClear()
    f.owner.search.priorityDraft = 'critical'
    f.owner.search.typeDraft = 'spike'
    await f.owner.applySearch()
    expect(f.api.tasks.mock.calls.map(([, q]) => q)).toEqual(
      taskStates.map((state) => ({
        sprint_id: id(3),
        state,
        limit: 50,
        priority: 'critical',
        type: 'spike',
      })),
    )
    expect(f.owner.filtersActive.value).toBe(true)
    expect(f.owner.searchEmpty.value).toBe(true)
    f.api.tasks.mockClear()
    Object.assign(f.owner.search, { priorityDraft: 'unknown' })
    await f.owner.applySearch()
    expect(f.api.tasks).not.toHaveBeenCalled()
    expect(f.owner.search.priority).toBe('critical')
    expect(f.owner.search.message).toContain('有效的优先级和类型')
    await f.owner.clearSearch()
    expect(f.owner.filtersActive.value).toBe(false)
    expect(f.owner.searchEmpty.value).toBe(false)
    expect(f.api.tasks.mock.calls.map(([, q]) => q)).toEqual(
      taskStates.map((state) => ({
        sprint_id: id(3),
        state,
        limit: 50,
      })),
    )
  })
})
describe('Work review selection and publication owner', () => {
  it('keeps edit presence, captured version and late results bound to the original unassigned backlog', async () => {
    const f = fixture()
    const backlog: Task = { ...task, state: 'backlog', assignee_agent_id: null }
    f.setStored(backlog)
    f.owner.afterNavigation(`/owner/demo/tasks/${id(4)}`)
    await flushPromises()
    f.owner.state.sprint = { ...sprint, state: 'planned', started_at: null }
    expect(f.owner.canEdit.value).toBe(true)
    f.owner.state.sprint = { ...sprint, state: 'completed', completed_at: time }
    expect(f.owner.canEdit.value).toBe(false)
    f.owner.state.sprint = sprint
    f.owner.state.task = { ...backlog, assignee_agent_id: id(5) }
    expect(f.owner.canEdit.value).toBe(false)
    f.owner.state.task = backlog
    f.owner.openEdit()
    f.owner.chooseAction('todo')
    expect(f.owner.draft.action).toBe('')
    f.owner.touchEdit('plan', backlog.plan)
    await f.owner.submitEdit()
    await flushPromises()
    expect(f.api.startEdit.mock.calls[0]).toEqual([
      id(1),
      id(4),
      { expected_version: '1', request: { plan: '' } },
    ])
    expect(f.owner.state.feedback).toContain('任务内容没有变化')
    expect(f.owner.edit.open).toBe(false)
    f.owner.openEdit()
    f.owner.touchEdit('description', '')
    f.setStored({ ...backlog, version: '2' })
    await f.owner.refresh()
    await flushPromises()
    expect(f.owner.edit.version).toBe('1')
    expect(f.owner.editStale.value).toBe(true)
    await f.owner.submitEdit()
    expect(f.api.startEdit).toHaveBeenCalledTimes(1)
    const cancelling = f.owner.cancelEdit()
    f.owner.confirm()
    await cancelling
    f.owner.openEdit()
    expect(f.owner.edit.version).toBe('2')
    f.owner.touchEdit('title', '原任务的未决编辑')
    let complete!: (value: TaskUpdateReceipt) => void
    const held = new Promise<TaskUpdateReceipt>((resolve) => {
      complete = resolve
    })
    f.api.startEdit.mockImplementationOnce(() => held)
    const lateReceipt: TaskUpdateReceipt = {
      task: { ...backlog, title: '原任务的未决编辑', version: '3' },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    }
    const saving = f.owner.submitEdit()
    try {
      await flushPromises()
      expect(f.api.startEdit).toHaveBeenCalledTimes(2)
      f.owner.afterNavigation(`/owner/demo/tasks/${id(41)}`)
      await flushPromises()
    } finally {
      complete(lateReceipt)
      await saving
    }
    await flushPromises()
    expect(f.owner.state.task?.id).toBe(id(41))
    expect(f.owner.state.feedback).toBe('')
    expect(f.owner.edit.open).toBe(false)
    f.owner.openEdit()
    f.owner.touchEdit('title', '旧身份草稿')
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.edit.title).toBe('')
    expect(f.owner.edit.open).toBe(false)
    expect(f.api.startTransfer).not.toHaveBeenCalled()
  })

  it('uses only current Sprint by default, retains manual selection and reads every column in domain order', async () => {
    const f = fixture()
    f.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    expect(f.owner.state.sprint?.id).toBe(id(3))
    expect(f.api.tasks.mock.calls.map(([, q]) => q.state)).toEqual(taskStates)
    expect(f.owner.columns.in_progress.items.map((t) => t.id)).toEqual([id(4)])
    expect(f.owner.name(id(5))).toBe('真实 Agent')
    await f.owner.selectSprint(id(30))
    await flushPromises()
    expect(f.owner.state.sprint?.id).toBe(id(30))
    f.project.current_sprint_id = id(31)
    await flushPromises()
    expect(f.owner.state.sprint?.id).toBe(id(30))
    f.owner.afterNavigation(`/owner/demo/tasks/${id(4)}`)
    await flushPromises()
    await f.owner.selectMilestone(id(2))
    await flushPromises()
    expect(f.navigate).toHaveBeenLastCalledWith('/owner/demo/tasks')
    expect(f.owner.state.task).toBe(null)
    expect(f.owner.state.sprint).toBe(null)
    expect(f.owner.state.milestone?.id).toBe(id(2))
    await f.owner.refresh()
    await flushPromises()
    expect(f.owner.state.sprint).toBe(null)
    f.owner.dispose()
    owner = undefined
    const empty = fixture(null)
    empty.owner.afterNavigation('/owner/demo/tasks')
    await flushPromises()
    expect(empty.owner.state.sprint).toBe(null)
    expect(empty.api.sprint).not.toHaveBeenCalled()
    expect(empty.api.tasks).not.toHaveBeenCalled()
    expect(empty.owner.state.milestones[0]?.title).toBe('交付目标')
  })
  it('ignores an original late Task read after route/identity retirement and never publishes foreign parent chains', async () => {
    const f = fixture()
    let resolve!: (v: Task) => void
    f.api.task.mockImplementationOnce(async () => {
      f.authState.busy = true
      try {
        return await new Promise<Task>((done) => {
          resolve = done
        })
      } finally {
        f.authState.busy = false
      }
    })
    f.owner.afterNavigation(`/owner/demo/tasks/${id(4)}`)
    await flushPromises()
    f.owner.afterNavigation(`/owner/demo/tasks/${id(40)}`)
    expect(f.api.task).toHaveBeenCalledTimes(1)
    resolve(task)
    await flushPromises()
    expect(f.owner.state.task?.id).toBe(id(40))
    expect(f.api.task).toHaveBeenCalledTimes(2)
    f.api.task.mockResolvedValueOnce({ ...task, id: id(41), milestone_id: id(99) })
    f.owner.afterNavigation(`/owner/demo/tasks/${id(41)}`)
    await flushPromises()
    expect(f.owner.state.phase).toBe('error')
    expect(f.owner.state.task).toBe(null)
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.state.milestones).toEqual([])
    expect(f.owner.columns.in_progress.items).toEqual([])
  })
  it('preserves review bytes/version and keeps uncertain changes locked while only original Lookup may recover', async () => {
    const f = fixture()
    f.owner.afterNavigation(`/owner/demo/tasks/${id(4)}`)
    await flushPromises()
    f.owner.chooseAction('in_review')
    f.owner.draft.agentID = id(5)
    f.owner.draft.comment = '  原始评审说明\n'
    f.api.startTransfer.mockImplementationOnce(async () => {
      f.progress.value = {
        kind: 'review',
        sprintID: null,
        projectID: id(1),
        taskID: id(4),
        phase: 'uncertain',
        receipt: null,
        observation: 'none',
        contextValid: true,
      }
      throw new AccountFailure('transport')
    })
    await f.owner.submit()
    await flushPromises()
    expect(f.api.startTransfer.mock.calls[0]?.[2]).toEqual({
      expected_version: '1',
      request: { target_state: 'in_review', assignee_agent_id: id(5), comment: '  原始评审说明\n' },
    })
    expect(f.owner.draft.comment).toBe('  原始评审说明\n')
    expect(f.owner.readOnly.value).toBe(true)
    f.owner.chooseAction('done')
    await f.owner.submit()
    expect(f.api.startTransfer).toHaveBeenCalledTimes(1)
    await f.owner.lookup()
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.owner.state.task?.state).toBe('in_progress')
    f.api.checkOriginal.mockImplementationOnce(async () => {
      const next = { ...task, state: 'in_review' as const, version: '2' }
      f.setStored(next)
      const receipt = { task: next, task_event_ids: [id(21)], event_ids: [id(22)] }
      f.progress.value = {
        kind: 'review',
        sprintID: null,
        projectID: id(1),
        taskID: id(4),
        phase: 'confirmed',
        receipt,
        observation: 'committed',
        contextValid: false,
      }
      return { status: 'committed', receipt }
    })
    await f.owner.lookup()
    await flushPromises()
    expect(f.owner.state.task?.state).toBe('in_review')
    expect(f.owner.state.task?.version).toBe('2')
    expect(f.owner.draft.comment).toBe('')
    f.owner.chooseAction('done')
    f.owner.draft.comment = '接受结果'
    await f.owner.submit()
    await flushPromises()
    expect(f.api.startTransfer.mock.calls[1]?.[2]).toEqual({
      expected_version: '2',
      request: { target_state: 'done', comment: '接受结果' },
    })
    expect(f.owner.state.task?.state).toBe('done')
  })
  it('recovers creation from its Sprint without reading a not-yet-confirmed Task and clears retired drafts', async () => {
    const f = fixture()
    f.owner.afterNavigation(`/owner/demo/tasks/sprints/${id(3)}`)
    await flushPromises()
    expect(f.owner.canCreate.value).toBe(true)
    f.owner.openCreation()
    Object.assign(f.owner.creation, { title: '原始新任务', type: 'task', priority: 'high' })
    f.api.startCreate.mockImplementationOnce(async () => {
      f.progress.value = {
        kind: 'create',
        projectID: id(1),
        taskID: id(40),
        sprintID: id(3),
        phase: 'uncertain',
        receipt: null,
        observation: 'none',
        contextValid: true,
      }
      throw new AccountFailure('transport')
    })
    await f.owner.submitCreation()
    expect(f.api.startCreate.mock.calls[0]).toEqual([
      id(1),
      {
        sprint_id: id(3),
        title: '原始新任务',
        type: 'task',
        priority: 'high',
        description: '',
        plan: '',
      },
    ])
    expect(f.owner.state.task).toBeNull()
    expect(f.api.task).not.toHaveBeenCalled()
    expect(f.owner.progress.value).toBeNull()
    expect(f.owner.creationProgress.value?.phase).toBe('uncertain')
    expect(f.owner.canCreate.value).toBe(false)
    await f.owner.submitCreation()
    expect(f.api.startCreate).toHaveBeenCalledTimes(1)
    await f.owner.selectSprint(id(30))
    await flushPromises()
    expect(f.owner.creationProgress.value).toBeNull()
    expect(f.owner.pending.value?.taskID).toBe(id(40))
    expect(f.owner.creationPending.value?.projectID).toBe(id(1))
    const originalContext = f.context.value!
    f.context.value = { ...originalContext, projectID: id(60), generation: 2 }
    f.owner.afterNavigation('/owner/other/tasks')
    await flushPromises()
    expect(f.owner.creationPending.value).toBeNull()
    expect(f.owner.pending.value?.projectID).toBe(id(1))
    expect(f.api.task).not.toHaveBeenCalled()
    f.context.value = { ...originalContext, generation: 3 }
    f.owner.afterNavigation(`/owner/demo/tasks/sprints/${id(30)}`)
    await flushPromises()
    await f.owner.selectSprint(id(3))
    await flushPromises()
    expect(f.owner.creationProgress.value?.taskID).toBe(id(40))
    await f.owner.lookupCreation()
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.api.task).not.toHaveBeenCalled()
    const created: TaskCreateReceipt = {
      task: { ...task, id: id(40), title: '原始新任务', state: 'backlog', assignee_agent_id: null },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    }
    f.api.checkOriginal.mockImplementationOnce(async () => {
      f.setStored(created.task)
      f.progress.value = {
        ...f.progress.value!,
        phase: 'confirmed',
        receipt: created,
        observation: 'committed',
        contextValid: false,
      }
      return { status: 'committed', receipt: created }
    })
    await f.owner.lookupCreation()
    await flushPromises()
    expect(f.navigate).toHaveBeenLastCalledWith(`/owner/demo/tasks/${id(40)}`)
    expect(f.owner.state.task?.state).toBe('backlog')
    expect(f.owner.creation.open).toBe(false)
    expect(f.api.startTransfer).not.toHaveBeenCalled()
    await f.owner.closeTask()
    await flushPromises()
    f.owner.openCreation()
    f.owner.creation.title = '不得跨身份保存的草稿'
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.creation.open).toBe(false)
    expect(f.owner.creation.title).toBe('')
    expect(f.owner.visible.value).toBe(false)
  })
})
