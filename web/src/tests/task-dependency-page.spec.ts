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
import type {
  TaskDependencyAddInput,
  TaskDependencyResolveInput,
} from '../api/work-task-dependencies'
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
  related,
  resolveInput,
  t,
  task,
} from './task-dependency-fixture'
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
    kind: 'dependency-add' | 'dependency-resolve',
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
  function commit(input: TaskDependencyAddInput | TaskDependencyResolveInput) {
    const wire = receipt(input)
    const result = {
      ...wire,
      task: decodeWorkTask(wire.task, p, t),
      blocker: decodeWorkBlocker(wire.blocker, p, t),
    }
    current = result.task
    blockers = [result.blocker]
    blockerCursor = undefined
    publish(
      'description' in input.request ? 'dependency-add' : 'dependency-resolve',
      'confirmed',
      result,
    )
    return result
  }
  type Port = SessionController['workTaskDependencies']
  const commands = {
    candidates: vi.fn<Port['candidates']>(async () => ({
      items: [
        { ...task(), id: related, sprint_id: id(30), milestone_id: id(20), title: 'Target task B' },
      ],
    })),
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
    milestone: vi.fn(async (_p: string, target: string) => ({ ...milestone, id: target })),
    sprints: vi.fn(async () => ({ items: [currentSprint] })),
    sprint: vi.fn(async (_p: string, target: string) => ({
      ...currentSprint,
      id: target,
      milestone_id: target === id(30) ? id(20) : id(2),
    })),
    task: vi.fn(async (_p: string, target: string) => ({
      ...current,
      id: target,
      ...(target === related
        ? { title: 'Target task B', sprint_id: id(30), milestone_id: id(20) }
        : {}),
    })),
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
    workTaskDependencies: commands,
    workMilestonePlanning: { progress: null },
    workSprintLifecycle: { progress: null, createProgress: null },
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async (path) => {
    if (await owner.confirmLeave(path)) owner.afterNavigation(path)
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
describe('Task dependency visible workflow', () => {
  it('selects a real cross-Sprint candidate, commits original input, refreshes facts and restores focus', async () => {
    const f = await fixture()
    await click(f, '添加任务依赖')
    expect(f.commands.candidates).toHaveBeenCalledExactlyOnceWith(p, { limit: 50 })
    expect(document.activeElement).toBe(f.wrapper.get('input').element)
    await f.wrapper.get('button[aria-haspopup="listbox"]').trigger('click')
    const option = [...document.querySelectorAll<HTMLButtonElement>('[role="option"]')].find(
      (node) => node.textContent?.includes('Target task B'),
    )
    expect(option).toBeDefined()
    option!.click()
    await flushPromises()
    await f.wrapper.get('textarea').setValue(draft().request.description)
    await save(f, '添加任务依赖')
    expect(f.commands.add).toHaveBeenCalledExactlyOnceWith(p, t, draft())
    expect(f.owner.state.task).toMatchObject({
      version: '9007199254740994',
      state: 'backlog',
      assignee_agent_id: null,
      manual_rank: task().manual_rank,
    })
    expect(f.wrapper.text()).toContain('未解除')
    expect(f.reads.events).toHaveBeenCalledTimes(2)
    expect(document.activeElement).toBe(button(f, '添加任务依赖').element)
    expect(f.reads.startReady).not.toHaveBeenCalled()
  })
  it('uses applied text for continuation, preserves selection, resets on explicit search and rejects stale cursors', async () => {
    const f = await fixture()
    const first = Array.from({ length: 50 }, (_, n) => ({ ...task(), id: id(100 + n) }))
    f.commands.candidates.mockResolvedValueOnce({ items: first, next_cursor: 'first-cursor' })
    await click(f, '添加任务依赖')
    f.owner.dependency.relatedTaskID = id(100)
    f.owner.dependencyCandidates.text = 'draft search'
    f.commands.candidates.mockResolvedValueOnce({
      items: [{ ...task(), id: id(160), state: 'done' }],
      next_cursor: undefined,
    })
    await click(f, '更多候选任务')
    expect(f.commands.candidates.mock.calls[1]).toEqual([p, { limit: 50, cursor: 'first-cursor' }])
    expect(f.owner.dependency.relatedTaskID).toBe(id(100))
    expect(f.owner.dependencyCandidates.items).toHaveLength(51)
    f.commands.candidates.mockResolvedValueOnce({
      items: [{ ...task(), id: id(170), state: 'cancelled' }],
    })
    await click(f, '查找任务')
    expect(f.commands.candidates.mock.calls[2]).toEqual([p, { limit: 50, text: 'draft search' }])
    expect(f.owner.dependency.relatedTaskID).toBe('')
    expect(f.owner.dependencyOptions.value).toEqual([
      { value: id(170), label: `${task().title} · ${id(170)}` },
    ])
    f.owner.dependencyCandidates.next = 'expired'
    f.commands.candidates.mockRejectedValueOnce(failure('CURSOR_INVALID'))
    await f.owner.loadDependencyCandidates(true)
    expect(f.owner.dependencyCandidates).toMatchObject({
      phase: 'error',
      items: [],
      next: undefined,
    })
    expect(f.commands.add).not.toHaveBeenCalled()
  })
  it('cancels resolve without writing and confirms with the original related ID outside the request', async () => {
    const f = await fixture(true)
    await click(f, '解除任务依赖')
    await click(f, '取消')
    expect(f.commands.resolve).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(button(f, '解除任务依赖').element)
    await click(f, '解除任务依赖')
    await save(f, '解除任务依赖')
    expect(f.commands.resolve).toHaveBeenCalledExactlyOnceWith(
      p,
      t,
      { ...resolveInput(), expected_version: task().version },
      related,
    )
    expect(f.wrapper.text()).toContain('已解除')
    expect(document.activeElement).toBe(button(f, '添加任务依赖').element)
    expect(f.reads.startReady).not.toHaveBeenCalled()
  })
  it('preserves the cycle-rejected draft/version and requires explicit discard after a new current Get', async () => {
    const f = await fixture()
    f.commands.add.mockImplementationOnce(async () => {
      f.publish('dependency-add', 'rejected')
      throw failure('TASK_DEPENDENCY_CYCLE')
    })
    await click(f, '添加任务依赖')
    f.owner.dependency.relatedTaskID = related
    await f.wrapper.get('textarea').setValue('keep cycle reason')
    await save(f, '添加任务依赖')
    expect(f.wrapper.text()).toContain('该依赖会形成循环')
    expect(f.owner.dependency).toMatchObject({
      description: 'keep cycle reason',
      relatedTaskID: related,
      version: task().version,
    })
    f.setTask({ ...task(), version: '9007199254740999' })
    await click(f, '重新读取任务')
    expect(button(f, '添加依赖').element.disabled).toBe(true)
    expect(f.owner.dependency.version).toBe(task().version)
    const cancelling = f.owner.cancelDependency()
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.confirm()
    await expect(cancelling).resolves.toBe(true)
    expect(f.commands.add).toHaveBeenCalledTimes(1)
    expect(f.commands.lookup).not.toHaveBeenCalled()
  })
  it('shows only original Lookup for Unknown and clears all result cursors on committed observation', async () => {
    const f = await fixture()
    f.commands.add.mockImplementationOnce(async () => {
      f.publish('dependency-add', 'uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await click(f, '添加任务依赖')
    f.owner.dependency.relatedTaskID = related
    await f.wrapper.get('textarea').setValue(draft().request.description)
    await save(f, '添加任务依赖')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.wrapper.findAll('button').some((node) => label(node) === '添加任务依赖')).toBe(false)
    await click(f, '查询原操作结果')
    expect(f.commands.lookup).toHaveBeenCalledTimes(1)
    expect(f.commands.add).toHaveBeenCalledTimes(1)
    f.owner.columns.backlog.next = 'old-task-cursor'
    f.owner.state.blockerNext = 'old-blocker-cursor'
    f.owner.timeline.next = 'old-event-cursor'
    f.reads.tasks.mockClear()
    f.reads.blockers.mockClear()
    f.reads.events.mockClear()
    f.commands.lookup.mockImplementationOnce(async () => ({
      status: 'committed',
      receipt: f.commit(addInput()),
    }))
    await click(f, '查询原操作结果')
    expect(f.owner.dependency.open).toBe(false)
    expect(f.owner.columns.backlog.next).toBeUndefined()
    expect(f.owner.state.blockerNext).toBeUndefined()
    expect(f.owner.timeline.next).toBeUndefined()
    for (const [, query] of f.reads.tasks.mock.calls) expect(query).not.toHaveProperty('cursor')
    for (const [, , query] of f.reads.blockers.mock.calls)
      expect(query).not.toHaveProperty('cursor')
    expect(document.activeElement).toBe(button(f, '添加任务依赖').element)
  })
  it('loads the target real parent chain after dirty confirmation instead of reusing the source Sprint', async () => {
    const f = await fixture(true)
    await click(f, '解除任务依赖')
    await f.wrapper.get('textarea').setValue('draft comment')
    const navigating = f.owner.viewDependency(id(5))
    await flushPromises()
    expect(f.reads.task).toHaveBeenLastCalledWith(p, related)
    expect(f.owner.confirmation.open).toBe(true)
    expect(f.owner.state.task?.id).toBe(t)
    f.owner.confirm()
    await navigating
    await flushPromises()
    expect(f.owner.state.task).toMatchObject({
      id: related,
      sprint_id: id(30),
      milestone_id: id(20),
    })
    expect(f.owner.state.sprint?.id).toBe(id(30))
    expect(f.owner.state.milestone?.id).toBe(id(20))
    expect(f.commands.resolve).not.toHaveBeenCalled()
  })
  it('rejects stale candidate replies after navigation and clears candidate data on identity and access loss', async () => {
    const f = await fixture()
    let release!: () => void
    f.commands.candidates.mockImplementationOnce(async () => {
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return { items: [{ ...task(), id: related }] }
    })
    const opening = f.owner.openDependency()
    await flushPromises()
    releases.push(() => release())
    f.owner.afterNavigation(`/owner/demo/tasks/${id(80)}`)
    release()
    await opening
    await flushPromises()
    expect(f.owner.dependency.open).toBe(false)
    expect(f.owner.dependencyCandidates.items).toEqual([])
    expect(f.owner.state.task?.id).toBe(id(80))
    await f.owner.openDependency()
    expect(f.owner.dependencyCandidates.items).toHaveLength(1)
    f.personal.identity = { userID: id(88), sessionID: id(89), epoch: 2 }
    await flushPromises()
    expect(f.owner.dependencyCandidates.items).toEqual([])
    expect(f.owner.dependency.open).toBe(false)
    expect(f.commands.add).not.toHaveBeenCalled()
  })
  it('permits only unassigned backlog in planned/current active Projects and clears on candidate denial', async () => {
    const f = await fixture()
    f.setSprint('planned')
    await f.owner.refresh()
    expect(button(f, '添加任务依赖').element.disabled).toBe(false)
    f.setSprint('completed')
    await f.owner.refresh()
    expect(button(f, '添加任务依赖').element.disabled).toBe(true)
    f.setSprint('current')
    f.detail.project = { ...f.detail.project!, lifecycle: 'archived' }
    await f.owner.refresh()
    expect(button(f, '添加任务依赖').element.disabled).toBe(true)
    f.detail.project = { ...f.detail.project!, lifecycle: 'active' }
    f.setTask({ ...task(), assignee_agent_id: id(7) })
    await f.owner.refresh()
    expect(f.wrapper.findAll('button').some((node) => label(node) === '添加任务依赖')).toBe(false)
    f.setTask(task())
    await f.owner.refresh()
    f.commands.candidates.mockRejectedValueOnce(failure('NOT_FOUND', 404))
    await f.owner.openDependency()
    expect(f.owner.state.task).toBeNull()
    expect(f.owner.dependency.open).toBe(false)
    expect(f.owner.dependencyCandidates.items).toEqual([])
    expect(f.commands.forgetUnavailable).toHaveBeenCalledWith(p, t)
  })
})
