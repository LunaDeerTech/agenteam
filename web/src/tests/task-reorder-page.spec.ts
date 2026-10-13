import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Page, Task, TaskQuery, TaskSummary, Sprint } from '../api/work-review'
import type { TaskReorderInput } from '../api/work-task-reorder'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController, WorkReviewProgress } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import TaskReviewDetail from '../components/work/TaskReviewDetail.vue'
import { id, p, t, at, task } from './task-human-blocker-fixture'
const owners: ProjectTasks[] = [],
  wrappers: VueWrapper[] = [],
  releases: (() => void)[] = []
beforeEach(() =>
  vi
    .spyOn(HTMLElement.prototype, 'getClientRects')
    .mockImplementation(() => [{ width: 10, height: 10 }] as unknown as DOMRectList),
)
afterEach(async () => {
  releases.splice(0).forEach((f) => f())
  wrappers.splice(0).forEach((w) => w.unmount())
  owners.splice(0).forEach((o) => o.dispose())
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
const candidate = (n = 6): TaskSummary => ({
  ...task(),
  id: id(n),
  title: `排序候选 ${n}`,
  manual_rank: n.toString(16).padStart(32, '0'),
})
async function fixture() {
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
  let current: Task = task()
  let sprint: Sprint = {
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
  const milestone = { ...sprint, id: id(2), title: '目标' }
  const progress = shallowRef<WorkReviewProgress | null>(null)
  const sessionState = shallowReactive({ phase: 'authenticated', busy: false })
  function publish(
    phase: WorkReviewProgress['phase'],
    result: WorkReviewProgress['receipt'] = null,
    observation: WorkReviewProgress['observation'] = 'none',
  ) {
    progress.value = {
      kind: 'reorder',
      projectID: p,
      taskID: t,
      sprintID: null,
      phase,
      receipt: result,
      observation,
      contextValid: true,
    }
  }
  function commit(input: TaskReorderInput, changed = true) {
    current = { ...current, version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)) }
    const result = changed
      ? { task: current, changed: true as const, task_event_id: id(10), event_ids: [id(11)] }
      : { task: current, changed: false as const, task_event_id: null, event_ids: [] }
    publish('confirmed', result)
    return result
  }
  const reads = {
    get progress() {
      return progress.value
    },
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [sprint] })),
    sprint: vi.fn(async () => sprint),
    task: vi.fn(async () => current),
    tasks: vi.fn(async (_p: string, query: TaskQuery): Promise<Page<TaskSummary>> => ({
      items: query.priority
        ? [current, candidate()]
        : query.state === current.state
          ? [current]
          : [],
    })),
    blockers: vi.fn(async () => ({ items: [] })),
    events: vi.fn(async () => ({ items: [] })),
    agents: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
    startReady: vi.fn(),
    startTransfer: vi.fn(),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    startReorder: vi.fn(async (_p: string, _t: string, input: TaskReorderInput) => commit(input)),
    checkOriginal: vi.fn<SessionController['workReview']['checkOriginal']>(async () => ({
      status: 'not_observed',
      receipt: null,
    })),
  }
  const auth = {
    state: sessionState,
    personalContext: personal,
    workReview: reads,
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
    reads,
    progress,
    publish,
    commit,
    detail,
    context,
    personal,
    setTask(value: Task) {
      current = value
    },
    setSprint(state: Sprint['state']) {
      sprint = { ...sprint, state }
    },
  }
}
function label(node: DOMWrapper<Element>) {
  const copy = node.element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim()
}
function button(f: Awaited<ReturnType<typeof fixture>>, name: string) {
  const node = f.wrapper.findAll<HTMLButtonElement>('button').find((n) => label(n) === name)
  expect(node, name).toBeDefined()
  return node!
}
async function click(f: Awaited<ReturnType<typeof fixture>>, name: string) {
  await button(f, name).trigger('click')
  await flushPromises()
}
async function choose(f: Awaited<ReturnType<typeof fixture>>, label: string, option: string) {
  await f.wrapper.get(`button[aria-label="${label}"]`).trigger('click')
  await flushPromises()
  const node = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find((n) =>
    n.textContent?.trim().includes(option),
  )
  expect(node).toBeDefined()
  node!.click()
  await flushPromises()
}
async function submit(f: Awaited<ReturnType<typeof fixture>>) {
  await f.wrapper.get('form[aria-label="调整任务顺序"]').trigger('submit')
  await flushPromises()
}

describe('Task backlog same-group sorting in the real detail component', () => {
  it('offers only a server-read same-group anchor, ignores board filters, commits and refreshes real facts', async () => {
    const f = await fixture()
    Object.assign(f.owner.search, { text: 'hidden', type: 'bug', assignee: id(20) })
    await click(f, '调整任务顺序')
    expect(f.reads.tasks).toHaveBeenCalledWith(p, {
      sprint_id: id(3),
      state: 'backlog',
      priority: 'high',
      limit: 50,
    })
    expect(f.wrapper.text()).toContain('候选不受看板搜索条件影响')
    expect(button(f, '确认调整顺序').element.disabled).toBe(true)
    await choose(f, '目标任务', '排序候选 6')
    const previousReads = f.reads.task.mock.calls.length
    await submit(f)
    expect(f.reads.startReorder).toHaveBeenCalledExactlyOnceWith(p, t, {
      expected_version: task().version,
      request: { before_id: id(6) },
    })
    expect(f.reads.task.mock.calls.length).toBeGreaterThan(previousReads)
    expect(f.wrapper.text()).toContain(String(BigInt(task().version) + 1n))
    expect(f.wrapper.find('form[aria-label="调整任务顺序"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button(f, '顺序已确认').element)
  })
  it('supports an explicit group-tail no-op without rank or cross-group input', async () => {
    const f = await fixture()
    f.reads.startReorder.mockImplementationOnce(async (_p, _t, input) => f.commit(input, false))
    await click(f, '调整任务顺序')
    await choose(f, '排序位置', '移到组尾')
    await submit(f)
    expect(f.reads.startReorder).toHaveBeenCalledExactlyOnceWith(p, t, {
      expected_version: task().version,
      request: {},
    })
    expect(f.wrapper.text()).toContain('任务顺序没有变化')
  })
  it('clears an anchor when changing pages and recovers stale cursors only from the first page', async () => {
    const f = await fixture()
    f.reads.tasks.mockImplementation(async (_p, q) => ({
      items: q.priority ? [candidate(q.cursor ? 7 : 6)] : [],
      next_cursor: q.priority && !q.cursor ? 'page-two' : undefined,
    }))
    await click(f, '调整任务顺序')
    await choose(f, '目标任务', '排序候选 6')
    await click(f, '下一页候选')
    expect(button(f, '确认调整顺序').element.disabled).toBe(true)
    expect(f.owner.taskReorder.beforeID).toBe('')
    f.reads.tasks.mockRejectedValueOnce(failure('CURSOR_INVALID'))
    await click(f, '上一页候选')
    expect(f.wrapper.text()).toContain('候选顺序已变化')
    expect(f.reads.startReorder).not.toHaveBeenCalled()
    await click(f, '从首页重新读取')
    expect(f.reads.tasks.mock.calls.at(-1)![1].cursor).toBeUndefined()
    expect(f.wrapper.text()).toContain('第 1 页')
  })
  it('preserves a rejected original version and requires explicit reread/adoption before a new command', async () => {
    const f = await fixture()
    f.reads.startReorder.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('TASK_VERSION_CONFLICT')
    })
    await click(f, '调整任务顺序')
    await choose(f, '排序位置', '移到组尾')
    await submit(f)
    expect(f.owner.taskReorder.version).toBe(task().version)
    expect(button(f, '确认调整顺序').element.disabled).toBe(true)
    f.setTask({ ...task('9007199254740995'), priority: 'low' })
    await click(f, '重新读取排序对象')
    expect(f.owner.taskReorder.version).toBe(task().version)
    expect(f.reads.startReorder).toHaveBeenCalledTimes(1)
    f.reads.tasks.mockImplementation(async (_p, q) => ({
      items: q.priority ? [{ ...candidate(), priority: 'low' }] : [],
    }))
    await click(f, '使用当前版本继续排序')
    expect(f.owner.taskReorder.version).toBe('9007199254740995')
    expect(f.reads.tasks.mock.calls.at(-1)![1]).toMatchObject({
      priority: 'low',
      state: 'backlog',
      sprint_id: id(3),
    })
    await submit(f)
    expect(f.reads.startReorder).toHaveBeenCalledTimes(2)
    expect(f.reads.startReorder.mock.calls[1]![2]).toEqual({
      expected_version: '9007199254740995',
      request: {},
    })
  })
  it('retains Unknown for explicit Lookup, never optimistically sorts or replays, and refreshes after commitment', async () => {
    const f = await fixture()
    f.reads.startReorder.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await click(f, '调整任务顺序')
    await choose(f, '排序位置', '移到组尾')
    await submit(f)
    expect(f.wrapper.text()).toContain('提交结果待确认')
    expect(f.owner.state.task?.version).toBe(task().version)
    expect(f.wrapper.find('form[aria-label="调整任务顺序"]').exists()).toBe(false)
    await click(f, '查询原操作结果')
    expect(f.reads.startReorder).toHaveBeenCalledTimes(1)
    f.reads.checkOriginal.mockImplementationOnce(async () => ({
      status: 'committed',
      receipt: f.commit({ expected_version: task().version, request: {} }),
    }))
    await click(f, '查询原操作结果')
    expect(f.wrapper.text()).toContain('已确认原排序结果')
    expect(f.owner.state.task?.version).toBe('9007199254740994')
    expect(f.reads.startReorder).toHaveBeenCalledTimes(1)
  })
  it('rejects cross-group candidates and discards a late page after a Project change', async () => {
    const f = await fixture()
    f.reads.tasks.mockResolvedValueOnce({ items: [{ ...candidate(), priority: 'low' }] })
    await click(f, '调整任务顺序')
    expect(f.wrapper.text()).toContain('未取得完整候选列表')
    expect(button(f, '确认调整顺序').element.disabled).toBe(true)
    let release!: () => void
    f.reads.tasks.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = () => resolve({ items: [candidate()] })
        }),
    )
    const reading = f.owner.loadTaskCandidates(0)
    await flushPromises()
    releases.push(() => release())
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    release()
    await reading
    await flushPromises()
    expect(f.owner.taskCandidates.items).toEqual([])
    expect(f.owner.taskReorder.open).toBe(false)
    expect(f.reads.startReorder).not.toHaveBeenCalled()
  })
  it('keeps a confirmed result if the following Get fails and only offers read recovery', async () => {
    const f = await fixture()
    await click(f, '调整任务顺序')
    await choose(f, '排序位置', '移到组尾')
    f.reads.task.mockRejectedValueOnce(new AccountFailure('transport'))
    await submit(f)
    expect(f.progress.value?.phase).toBe('confirmed')
    expect(f.owner.state.feedback).toContain('任务顺序已更新')
    expect(f.reads.startReorder).toHaveBeenCalledTimes(1)
    await f.owner.refresh()
    await flushPromises()
    expect(f.owner.state.detailCurrent).toBe(true)
    expect(f.reads.startReorder).toHaveBeenCalledTimes(1)
  })
  it('blocks a completed Sprint or archived Project and restores focus after cancellation', async () => {
    const f = await fixture()
    await click(f, '调整任务顺序')
    await click(f, '取消调整顺序')
    expect(document.activeElement).toBe(button(f, '调整任务顺序').element)
    f.setSprint('completed')
    await f.owner.refresh()
    await flushPromises()
    expect(button(f, '调整任务顺序').element.disabled).toBe(true)
    f.setSprint('current')
    await f.owner.refresh()
    f.detail.project = { ...f.detail.project!, lifecycle: 'archived', archived_at: at }
    await flushPromises()
    expect(button(f, '调整任务顺序').element.disabled).toBe(true)
    expect(f.reads.startReorder).not.toHaveBeenCalled()
  })
})
