import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import {
  decodeWorkBlocker,
  decodeWorkTask,
  type Blocker,
  type PageQuery,
  type Task,
  type TaskQuery,
  type Sprint,
} from '../api/work-review'
import type { HumanBlockerAddInput, HumanBlockerResolveInput } from '../api/work-task-blockers'
import type { TaskTimelineEvent, TaskTimelineQuery } from '../api/work-task-timeline'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController, WorkReviewProgress } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import TaskReviewDetail from '../components/work/TaskReviewDetail.vue'
import {
  addInput,
  at,
  draft,
  id,
  p,
  receipt,
  resolveInput,
  t,
  task,
} from './task-human-blocker-fixture'
const owners: ProjectTasks[] = [],
  wrappers: VueWrapper[] = [],
  releases: (() => void)[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
const failure = (code: string, status = 409) =>
  new AccountFailure('problem', {
    type: 'urn:agenteam:problem:test',
    title: 'Rejected',
    status,
    detail: '',
    instance: '',
    code,
    request_id: id(99),
    commit_state: code === 'COMMIT_UNKNOWN' ? 'unknown' : 'not_committed',
  })
async function fixture(withBlocker = false) {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    identity,
    projectID: p,
    generation: 1,
    readGeneration: 1,
  })
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const detail = shallowReactive<{ phase: string; project: Project | null }>({
    phase: 'ready',
    project: {
      id: p,
      owner_user_id: id(8),
      name: 'Demo',
      normalized_name: 'demo',
      description: '',
      lifecycle: 'active',
      version: '1',
      current_sprint_id: id(3),
      created_at: at,
      updated_at: at,
      archived_at: null,
    },
  })
  const progress = shallowRef<WorkReviewProgress | null>(null)
  const sessionState = shallowReactive({ phase: 'authenticated', busy: false })
  let current: Task = task()
  let blockers: readonly Blocker[] = withBlocker ? [decodeWorkBlocker(receipt().blocker, p, t)] : []
  let blockerCursor: string | undefined
  let currentSprint: Sprint = {
    id: id(3),
    project_id: p,
    milestone_id: id(2),
    title: '当前迭代',
    description: '',
    state: 'current',
    manual_rank: '8'.repeat(32),
    version: '1',
    created_at: at,
    updated_at: at,
    started_at: at,
    completed_at: null,
  }
  const milestone = { ...currentSprint, id: id(2), title: '目标' }
  const publish = (
    kind: 'blocker-add' | 'blocker-resolve',
    phase: WorkReviewProgress['phase'],
    result: WorkReviewProgress['receipt'] = null,
    observation: WorkReviewProgress['observation'] = 'none',
  ) => {
    progress.value = {
      kind,
      projectID: p,
      taskID: t,
      sprintID: null,
      phase,
      receipt: result,
      observation,
      contextValid: true,
    }
  }
  function commit(input: HumanBlockerAddInput | HumanBlockerResolveInput) {
    const base = receipt(input)
    const wire = { ...base, task: { ...current, version: base.task.version, updated_at: base.task.updated_at } }
    const result = {
      ...wire,
      task: decodeWorkTask(wire.task, p, t),
      blocker: decodeWorkBlocker(wire.blocker, p, t),
    }
    current = result.task
    blockers = [result.blocker]
    blockerCursor = undefined
    publish('description' in input.request ? 'blocker-add' : 'blocker-resolve', 'confirmed', result)
    return result
  }
  type Port = SessionController['workTaskBlockers']
  const commands = {
    add: vi.fn<Port['add']>(async (_p, _t, input) =>
      commit({ ...input, request: { blocker_id: id(5), ...input.request } }),
    ),
    resolve: vi.fn<Port['resolve']>(async (_p, _t, input) => commit(input)),
    lookup: vi.fn<Port['lookup']>(async () => ({ status: 'not_observed', receipt: null })),
    forgetUnavailable: vi.fn(() => {
      progress.value = null
    }),
    get progress() {
      return progress.value
    },
  } satisfies Port
  const reads = {
    get progress() {
      return progress.value
    },
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [currentSprint] })),
    sprint: vi.fn(async () => currentSprint),
    task: vi.fn(async (_p: string, target: string) => ({ ...current, id: target })),
    tasks: vi.fn(async (_p: string, query: TaskQuery) => ({
      items: query.state === current.state ? [current] : [],
      next_cursor: undefined as string | undefined,
    })),
    blockers: vi.fn(async (_p: string, _t: string, _query: PageQuery) => ({
      items: blockers,
      next_cursor: blockerCursor,
    })),
    events: vi.fn(async (_p: string, _t: string, _query: TaskTimelineQuery) => ({
      items: [] as readonly TaskTimelineEvent[],
      next_cursor: undefined as string | undefined,
    })),
    agents: vi.fn(async () => ({
      items: [
        {
          id: id(7),
          project_id: p,
          name: 'real-agent',
          display_name: '真实 Agent',
          description: '',
          tag_color: null,
          version: '1',
          created_at: at,
          updated_at: at,
        },
      ],
    })),
    abandonRead: vi.fn(),
    startReady: vi.fn(),
    startTransfer: vi.fn(),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
  }
  const auth = {
    state: sessionState,
    personalContext: personal,
    workReview: reads,
    workTaskBlockers: commands,
    workMilestonePlanning: { progress: null },
    workSprintLifecycle: { progress: null, createProgress: null },
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async (path) => {
    owner.afterNavigation(path)
  })
  owners.push(owner)
  owner.afterNavigation(`/owner/demo/tasks/${t}`)
  const wrapper = mount(TaskReviewDetail, { props: { owner }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return {
    owner,
    wrapper,
    commands,
    reads,
    context,
    personal,
    detail,
    sessionState,
    publish,
    progress,
    commit,
    setTask(value: Task) {
      current = value
    },
    setSprint(value: Sprint['state']) {
      currentSprint = { ...currentSprint, state: value }
    },
    setBlockers(value: readonly Blocker[], cursor?: string) {
      blockers = value
      blockerCursor = cursor
    },
  }
}
function label(node: DOMWrapper<Element>) {
  const copy = node.element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((child) => child.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim()
}
function button(f: Awaited<ReturnType<typeof fixture>>, name: string) {
  const found = f.wrapper.findAll<HTMLButtonElement>('button').find((node) => label(node) === name)
  expect(found, name).toBeDefined()
  return found!
}
async function click(f: Awaited<ReturnType<typeof fixture>>, name: string) {
  await button(f, name).trigger('click')
  await flushPromises()
}
async function save(f: Awaited<ReturnType<typeof fixture>>, name: string) {
  await f.wrapper.get(`form[aria-label="${name}"]`).trigger('submit')
  await flushPromises()
}
describe('Task human blocker visible workflow', () => {
  it('adds and resolves a todo Task blocker while preserving its Agent, state and rank', async () => {
    const f = await fixture()
    const before = { ...task(), state: 'todo' as const, assignee_agent_id: id(7) }
    f.setTask(before)
    await f.owner.refresh()
    expect(f.owner.canEdit.value).toBe(false)
    await click(f, '添加人工阻塞')
    await f.wrapper.get('textarea').setValue('等待确认需求')
    await save(f, '添加人工阻塞')
    expect(f.commands.add.mock.calls[0]?.[3]).toEqual(before)
    expect(f.owner.state.task).toMatchObject({ state: 'todo', assignee_agent_id: id(7), manual_rank: before.manual_rank, version: '9007199254740994' })
    await click(f, '解除人工阻塞')
    await f.wrapper.get('textarea').setValue('需求已确认')
    await save(f, '解除人工阻塞')
    expect(f.owner.state.task).toMatchObject({ state: 'todo', assignee_agent_id: id(7), manual_rank: before.manual_rank, version: '9007199254740995' })
    expect(f.wrapper.text()).toContain('已解除')
    expect(document.activeElement).toBe(button(f, '添加人工阻塞').element)
    expect(f.reads.startTransfer).not.toHaveBeenCalled()
  })
  it('explains the last-blocker refusal without inferring completeness or changing a blocked Task', async () => {
    const f = await fixture(true)
    const before = { ...task(), state: 'blocked' as const, assignee_agent_id: id(7) }
    f.setTask(before)
    f.setBlockers([decodeWorkBlocker(receipt().blocker, p, t)], 'more-blockers')
    await f.owner.refresh()
    await click(f, '解除人工阻塞')
    expect(f.wrapper.text()).toContain('最后一项阻塞需要与任务恢复一并处理')
    f.commands.resolve.mockImplementationOnce(async () => {
      f.publish('blocker-resolve', 'rejected')
      throw failure('LAST_BLOCKER_REQUIRES_TASK_TRANSITION')
    })
    await f.wrapper.get('textarea').setValue('原解除说明')
    await save(f, '解除人工阻塞')
    expect(f.owner.state.task).toEqual(before)
    expect(f.owner.humanBlocker.resolutionComment).toBe('原解除说明')
    expect(f.wrapper.text()).toContain('这是受阻任务的最后一项未解除阻塞')
    expect(f.commands.resolve).toHaveBeenCalledTimes(1)
    expect(f.reads.startTransfer).not.toHaveBeenCalled()
  })
  it('adds with original text, rereads actual current facts and blocks Ready without changing state', async () => {
    const f = await fixture()
    await click(f, '添加人工阻塞')
    expect(document.activeElement).toBe(f.wrapper.get('textarea').element)
    await f.wrapper.get('textarea').setValue(draft().request.description)
    f.commands.add.mockImplementationOnce(async (_p, _t, input) => {
      const result = f.commit({ ...input, request: { blocker_id: id(5), ...input.request } })
      f.setTask({ ...result.task, title: '较新的当前读取', version: '9007199254740998' })
      return result
    })
    await save(f, '添加人工阻塞')
    expect(f.commands.add).toHaveBeenCalledWith(p, t, draft(), task())
    expect(f.owner.state.task).toMatchObject({
      title: '较新的当前读取',
      version: '9007199254740998',
      state: 'backlog',
      assignee_agent_id: null,
    })
    expect(f.wrapper.text()).toContain('人工阻塞已添加')
    expect(f.wrapper.text()).toContain('未解除')
    expect(f.reads.events).toHaveBeenCalledTimes(2)
    expect(document.activeElement).toBe(button(f, '添加人工阻塞').element)
    await click(f, '指派并加入待执行')
    // The public choice is exercised in the native browser; isolate the blocker gate here.
    f.owner.draft.agentID = id(7)
    await flushPromises()
    expect(button(f, '确认指派').element.disabled).toBe(true)
    expect(f.reads.startReady).not.toHaveBeenCalled()
    expect(f.reads.startTransfer).not.toHaveBeenCalled()
  })
  it('cancels resolve with no command and restores focus, then sends explicit null and preserves resolved history', async () => {
    const f = await fixture(true)
    await click(f, '解除人工阻塞')
    await click(f, '取消人工阻塞操作')
    expect(f.commands.resolve).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(button(f, '解除人工阻塞').element)
    await click(f, '解除人工阻塞')
    await save(f, '解除人工阻塞')
    expect(f.commands.resolve).toHaveBeenCalledWith(p, t, {
      ...resolveInput(),
      expected_version: task().version,
    }, task())
    expect(f.owner.state.task).toMatchObject({
      state: 'backlog',
      assignee_agent_id: null,
      manual_rank: task().manual_rank,
    })
    expect(f.wrapper.text()).toContain('已解除')
    expect(f.wrapper.text()).toContain('人工阻塞已解除')
    expect(f.wrapper.findAll('button').some((node) => label(node) === '解除人工阻塞')).toBe(false)
    expect(document.activeElement).toBe(button(f, '添加人工阻塞').element)
    expect(f.reads.startReady).not.toHaveBeenCalled()
  })
  it('rejects nonempty whitespace without writes and preserves a later raw comment', async () => {
    const f = await fixture(true)
    await click(f, '解除人工阻塞')
    await f.wrapper.get('textarea').setValue(' \n\t ')
    await save(f, '解除人工阻塞')
    expect(f.commands.resolve).not.toHaveBeenCalled()
    expect(f.wrapper.get('[role="alert"]').text()).toContain('非空白字符')
    const comment = '  已确认\n原样  '
    await f.wrapper.get('textarea').setValue(comment)
    await save(f, '解除人工阻塞')
    expect(f.commands.resolve.mock.calls[0]![2].request.resolution_comment).toBe(comment)
  })
  it('permits nonterminal Tasks in planned/current active Projects and refuses terminal Tasks', async () => {
    const f = await fixture()
    f.setSprint('planned')
    await f.owner.refresh()
    expect(button(f, '添加人工阻塞').element.disabled).toBe(false)
    f.setSprint('completed')
    await f.owner.refresh()
    expect(button(f, '添加人工阻塞').element.disabled).toBe(true)
    f.setSprint('current')
    f.detail.project = { ...f.detail.project!, lifecycle: 'archived' }
    await f.owner.refresh()
    expect(button(f, '添加人工阻塞').element.disabled).toBe(true)
    f.detail.project = { ...f.detail.project!, lifecycle: 'active' }
    for (const state of ['backlog', 'todo', 'in_progress', 'in_review', 'blocked'] as const) {
      f.setTask({ ...task(), state, assignee_agent_id: id(7) })
      await f.owner.refresh()
      expect(button(f, '添加人工阻塞').element.disabled).toBe(false)
    }
    for (const current of [
      { ...task(), state: 'cancelled' as const },
      { ...task(), state: 'done' as const },
    ]) {
      f.setTask(current)
      await f.owner.refresh()
      expect(f.wrapper.findAll('button').some((node) => label(node) === '添加人工阻塞')).toBe(false)
    }
    expect(f.commands.add).not.toHaveBeenCalled()
  })
  it('only offers resolution for unresolved waiting_for_human records', async () => {
    const f = await fixture()
    const human = decodeWorkBlocker(receipt().blocker, p, t)
    f.setBlockers([
      { ...human, id: id(60), type: 'technical' },
      { ...human, id: id(61), type: 'rely_on', related_task_id: id(70) },
      { ...human, id: id(62), resolved_at: at },
    ])
    await f.owner.refresh()
    expect(f.wrapper.findAll('button').some((node) => label(node) === '解除人工阻塞')).toBe(false)
    f.owner.openHumanBlocker(id(60))
    expect(f.owner.humanBlocker.open).toBe(false)
    expect(f.commands.resolve).not.toHaveBeenCalled()
  })
  it('retains conflict input and old version until explicit cancellation instead of adopting refreshed versions', async () => {
    const f = await fixture()
    f.commands.add.mockImplementationOnce(async () => {
      f.publish('blocker-add', 'rejected')
      throw failure('VERSION_CONFLICT')
    })
    await click(f, '添加人工阻塞')
    await f.wrapper.get('textarea').setValue('keep draft')
    await save(f, '添加人工阻塞')
    expect(f.owner.humanBlocker.description).toBe('keep draft')
    f.setTask({ ...task(), version: '9007199254740999' })
    await click(f, '重新读取任务')
    expect(f.owner.humanBlocker.version).toBe(task().version)
    expect(button(f, '确认添加人工阻塞').element.disabled).toBe(true)
    expect(f.commands.add).toHaveBeenCalledTimes(1)
    const leaving = f.owner.cancelHumanBlocker()
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.confirm()
    await expect(leaving).resolves.toBe(true)
    expect(f.owner.confirmation.open).toBe(false)
    expect(f.owner.humanBlocker.open).toBe(false)
    await flushPromises()
    await click(f, '添加人工阻塞')
    await save(f, '添加人工阻塞')
    expect(f.commands.add.mock.calls[1]![2].expected_version).toBe('9007199254740999')
    expect(f.reads.editRejected).toHaveBeenCalledTimes(1)
  })
  it('shows only original Lookup during Unknown and re-reads after committed observation', async () => {
    const f = await fixture()
    f.commands.add.mockImplementationOnce(async () => {
      f.publish('blocker-add', 'uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await click(f, '添加人工阻塞')
    await save(f, '添加人工阻塞')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.wrapper.findAll('button').some((node) => label(node) === '添加人工阻塞')).toBe(false)
    await click(f, '查询原操作结果')
    expect(f.commands.lookup).toHaveBeenCalledTimes(1)
    expect(f.commands.add).toHaveBeenCalledTimes(1)
    f.commands.lookup.mockImplementationOnce(async () => ({
      status: 'committed',
      receipt: f.commit(addInput()),
    }))
    await click(f, '查询原操作结果')
    expect(f.wrapper.text()).toContain('已确认原操作结果')
    expect(f.wrapper.text()).toContain('未解除')
    expect(f.owner.humanBlocker.open).toBe(false)
    expect(document.activeElement).toBe(button(f, '添加人工阻塞').element)
  })
  it('discards old blocker, Task and timeline cursors before re-reading after commit', async () => {
    const f = await fixture(true)
    f.setBlockers(f.owner.state.blockers, 'old-blocker-cursor')
    // A continuation is valid only after a nonempty, scoped event page.
    const oldEvent: TaskTimelineEvent = {
      id: id(90),
      project_id: p,
      task_id: t,
      task_version: task().version,
      actor: { type: 'human', user_id: id(8), source: 'task_domain' },
      operation_id: id(91),
      correlation_id: id(92),
      created_at: at,
      type: 'comment',
      payload: { body: '既有历史记录' },
    }
    f.reads.events.mockResolvedValueOnce({ items: [oldEvent], next_cursor: 'old-event-cursor' })
    f.reads.tasks.mockResolvedValueOnce({ items: [task()], next_cursor: 'old-task-cursor' })
    await f.owner.refresh()
    expect(f.owner.state.blockerNext).toBe('old-blocker-cursor')
    expect(f.owner.timeline.next).toBe('old-event-cursor')
    expect(f.owner.timeline.phase).toBe('ready')
    expect(f.owner.timeline.items).toEqual([oldEvent])
    expect(f.owner.columns.backlog.next).toBe('old-task-cursor')
    f.reads.blockers.mockClear()
    f.reads.events.mockClear()
    f.reads.tasks.mockClear()
    await click(f, '解除人工阻塞')
    await save(f, '解除人工阻塞')
    expect(f.owner.state.blockerNext).toBeUndefined()
    expect(f.owner.timeline.next).toBeUndefined()
    expect(f.owner.timeline.items).toEqual([])
    expect(f.owner.columns.backlog.next).toBeUndefined()
    for (const [, , query] of f.reads.blockers.mock.calls)
      expect(query).not.toHaveProperty('cursor')
    for (const [, query] of f.reads.tasks.mock.calls) expect(query).not.toHaveProperty('cursor')
    expect(f.reads.blockers).toHaveBeenCalledTimes(1)
    expect(f.reads.tasks).toHaveBeenCalledTimes(7)
    expect(f.reads.events).toHaveBeenCalledExactlyOnceWith(p, t, { limit: 50, order: 'desc' })
  })
  it('isolates a late mutation after Task navigation and clears draft on identity/Project changes', async () => {
    const f = await fixture()
    let release!: () => void
    f.commands.add.mockImplementationOnce(async (_p, _t, input) => {
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return f.commit({ ...input, request: { ...input.request, blocker_id: id(5) } })
    })
    await click(f, '添加人工阻塞')
    const saving = f.owner.submitHumanBlocker()
    await flushPromises()
    releases.push(() => release())
    f.owner.afterNavigation(`/owner/demo/tasks/${id(80)}`)
    await flushPromises()
    release()
    await saving
    expect(f.owner.state.task?.id).toBe(id(80))
    expect(f.owner.state.feedback).toBe('')
    expect(f.owner.humanBlocker.open).toBe(false)
    f.owner.afterNavigation(`/owner/demo/tasks/${t}`)
    await flushPromises()
    await click(f, '添加人工阻塞')
    await f.wrapper.get('textarea').setValue('private draft')
    f.context.value = { ...f.context.value!, projectID: id(81), generation: 2 }
    await flushPromises()
    expect(f.owner.humanBlocker.description).toBe('')
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.state.task).toBeNull()
    expect(f.wrapper.text()).not.toContain('private draft')
  })
  it('clears sensitive form and original intent after a current Task or Project denial', async () => {
    const f = await fixture()
    await click(f, '添加人工阻塞')
    await f.wrapper.get('textarea').setValue('private draft')
    f.reads.task.mockRejectedValueOnce(failure('NOT_FOUND', 404))
    await click(f, '重新读取任务')
    expect(f.owner.state.task).toBeNull()
    expect(f.owner.humanBlocker.description).toBe('')
    expect(f.commands.forgetUnavailable).toHaveBeenCalledWith(p, t)
    await f.owner.refresh()
    await click(f, '添加人工阻塞')
    f.detail.phase = 'loading'
    f.detail.phase = 'unavailable'
    expect(f.commands.forgetUnavailable).toHaveBeenCalledWith(p)
    expect(f.owner.humanBlocker.open).toBe(false)
  })
})
