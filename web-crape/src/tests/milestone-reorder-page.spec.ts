import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Milestone, Sprint } from '../api/work-review'
import type { MilestoneReorderInput, MilestoneReorderReceipt } from '../api/work-milestone-planning'
import {
  createProjectTasks,
  projectTasksKey,
  type ProjectTasks,
} from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import ProjectTasksView from '../views/projects/ProjectTasksView.vue'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  m = id(2),
  s = id(3),
  at = '2026-10-11T10:00:00.000001Z'
const base = {
  project_id: p,
  manual_rank: '8'.repeat(32),
  version: '9007199254740993',
  created_at: at,
  updated_at: at,
}
const milestone: Milestone = {
  ...base,
  id: m,
  title: 'Original milestone',
  description: 'Original description',
}
const sprint: Sprint = {
  ...base,
  id: s,
  milestone_id: m,
  title: 'Current Sprint',
  description: '',
  state: 'current',
  started_at: at,
  completed_at: null,
}
const project: Project = {
  id: p,
  owner_user_id: id(8),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: s,
  created_at: at,
  updated_at: at,
  archived_at: null,
}
type Port = SessionController['workMilestoneReordering']
const owners: ProjectTasks[] = [],
  wrappers: VueWrapper[] = [],
  releases: (() => void)[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (v: string) => v })
})
afterEach(async () => {
  releases.splice(0).forEach((r) => r())
  wrappers.splice(0).forEach((w) => w.unmount())
  owners.splice(0).forEach((o) => o.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
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
async function fixture() {
  // Actual controller/shared controls; Session ports are controlled, not a SQL or browser oracle.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    identity,
    projectID: p,
    generation: 1,
    readGeneration: 1,
  })
  const detail = shallowReactive<{ project: Project | null }>({ project: { ...project } })
  const sessionState = shallowReactive({ phase: 'authenticated', busy: false })
  const progress = shallowRef<Port['progress']>(null)
  let current = { ...milestone }
  const publish = (
    phase: NonNullable<Port['progress']>['phase'],
    result: MilestoneReorderReceipt | null = null,
    observation: NonNullable<Port['progress']>['observation'] = 'none',
  ) => {
    progress.value = {
      projectID: p,
      milestoneID: m,
      phase,
      result,
      observation,
      error: null,
      contextValid: true,
    }
  }
  const commit = (input: MilestoneReorderInput, changed = true) => {
    current = {
      ...current,
      version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
    }
    const result: MilestoneReorderReceipt = {
      command: 'work.milestone.reorder',
      changed,
      milestone: { ...current },
      sprint: null,
      event_id: changed ? id(20) : null,
    }
    publish('confirmed', result)
    return result
  }
  const api = {
    reorder: vi.fn<Port['reorder']>(async (_p, _m, input) => commit(input)),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => ({
      state: 'not_observed',
      result: null,
    })),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    forgetUnavailable: vi.fn(() => {
      progress.value = null
    }),
    abandon: vi.fn(),
    get progress() {
      return progress.value
    },
  } satisfies Port
  const reads = {
    progress: null,
    milestones: vi.fn(async (_p: string, _query?: { limit?: number; cursor?: string }) => ({
      items: [{ ...current }, { ...current, id: id(4), title: 'Anchor' }],
      next_cursor: undefined as string | undefined,
    })),
    milestone: vi.fn(async () => ({ ...current })),
    sprints: vi.fn(async () => ({ items: [sprint] })),
    sprint: vi.fn(async () => sprint),
    agents: vi.fn(async () => ({ items: [] })),
    tasks: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
  }
  const createAPI = {
    progress: null,
    create: vi.fn(),
    checkOriginal: vi.fn(),
    editRejected: vi.fn(),
    abandon: vi.fn(),
  }
  const sprintAPI = {
    progress: null,
    createProgress: null,
    create: vi.fn(),
    start: vi.fn(),
    checkOriginal: vi.fn(),
    checkOriginalCreate: vi.fn(),
    editRejected: vi.fn(),
    editRejectedCreate: vi.fn(),
  }
  const auth = {
    state: sessionState,
    personalContext: personal,
    workReview: reads,
    workMilestoneReordering: api,
    workMilestonePlanning: createAPI,
    workSprintLifecycle: sprintAPI,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: '/owner/demo' })),
    readCurrent: vi.fn(),
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async (path) => {
    owner.afterNavigation(path)
  })
  owners.push(owner)
  owner.afterNavigation('/owner/demo/tasks')
  const wrapper = mount(ProjectTasksView, {
    attachTo: document.body,
    global: { provide: { [projectTasksKey as symbol]: owner } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return {
    owner,
    wrapper,
    api,
    reads,
    createAPI,
    sprintAPI,
    publish,
    commit,
    progress,
    context,
    personal,
    detail,
    sessionState,
    setCurrent(value: Milestone) {
      current = value
    },
  }
}
const body = () => new DOMWrapper(document.body)
function button(name: string) {
  const b = body()
    .findAll<HTMLButtonElement>('button')
    .find((v) => {
      const copy = v.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove())
      return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === name
    })
  expect(b, name).toBeDefined()
  return b!
}
async function click(name: string) {
  await button(name).trigger('click')
  await flushPromises()
}
async function select(label: string, value: string) {
  const control = body()
    .findAll('button[aria-label]')
    .find((node) => node.attributes('aria-label') === label)
  expect(control, label).toBeDefined()
  await control!.trigger('click')
  await flushPromises()
  const option = body()
    .findAll('[role="option"]')
    .find((node) => node.text() === value)
  expect(option, value).toBeDefined()
  await option!.trigger('click')
  await flushPromises()
}
async function save() {
  await body().get('form[aria-label="调整 Milestone 顺序"]').trigger('submit')
  await flushPromises()
}
describe('Milestone reorder visible behavior and paging', () => {
  it('offers real anchors, cancels with focus restoration and keeps current Get separate from the receipt', async () => {
    const f = await fixture()
    await click('调整 Milestone 顺序')
    expect(document.activeElement?.textContent).toBe('调整 Milestone 顺序')
    await click('取消调整顺序')
    expect(document.activeElement).toBe(button('调整 Milestone 顺序').element)
    expect(f.api.reorder).not.toHaveBeenCalled()
    await click('调整 Milestone 顺序')
    await select('目标 Milestone', 'Anchor')
    expect(f.owner.canEditMilestone.value).toBe(false)
    f.api.reorder.mockImplementationOnce(async (_p, _m, input) => {
      const result = f.commit(input)
      f.setCurrent({
        ...result.milestone,
        title: 'Newer current fact',
        version: '9007199254740998',
      })
      return result
    })
    await save()
    expect(f.api.reorder).toHaveBeenCalledExactlyOnceWith(p, m, {
      expected_version: milestone.version,
      request: { before_id: id(4) },
    })
    expect(f.owner.state.milestone?.title).toBe('Newer current fact')
    expect(f.owner.milestoneReorderProgress.value?.result?.milestone.title).toBe(milestone.title)
    expect(document.activeElement?.textContent).toBe('Newer current fact')
  })
  it('submits group tail as an empty request and confirms no-op without inventing a new version', async () => {
    const f = await fixture()
    await click('调整 Milestone 顺序')
    await select('排序位置', '移到组尾')
    f.api.reorder.mockImplementationOnce(async (_p, _m, input) => f.commit(input, false))
    await save()
    expect(f.api.reorder).toHaveBeenCalledExactlyOnceWith(p, m, {
      expected_version: milestone.version,
      request: {},
    })
    expect(f.owner.state.milestone?.version).toBe(milestone.version)
    expect(body().text()).toContain('原操作无需调整')
  })
  it('reads real candidate pages with opaque cursors and explicitly restarts an invalidated chain', async () => {
    const f = await fixture()
    const first = Array.from({ length: 50 }, (_, i) => ({
      ...milestone,
      id: id(100 + i),
      title: `Candidate ${i}`,
    }))
    f.reads.milestones.mockResolvedValueOnce({ items: first, next_cursor: 'opaque-first' })
    await click('调整 Milestone 顺序')
    expect(f.owner.milestoneCandidates.items).toHaveLength(50)
    await select('目标 Milestone', 'Candidate 0')
    f.reads.milestones.mockResolvedValueOnce({
      items: [{ ...milestone, id: id(200), title: 'Beyond first page' }],
      next_cursor: undefined,
    })
    await click('下一页候选')
    expect(f.reads.milestones).toHaveBeenLastCalledWith(p, { limit: 50, cursor: 'opaque-first' })
    expect(f.owner.milestoneReorder.beforeID).toBe('')
    await select('目标 Milestone', 'Beyond first page')
    f.reads.milestones.mockRejectedValueOnce(failure('CURSOR_STALE'))
    await click('上一页候选')
    expect(f.owner.milestoneCandidates.items).toHaveLength(0)
    expect(f.owner.milestoneReorder.beforeID).toBe('')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    expect(f.api.reorder).not.toHaveBeenCalled()
    await click('从首页重新读取')
    expect(f.reads.milestones).toHaveBeenLastCalledWith(p, { limit: 50 })
    expect(f.owner.milestoneCandidates.page).toBe(0)
  })
  it('keeps a rejected anchor and version until explicit read and adoption, then requires fresh selection', async () => {
    const f = await fixture()
    await click('调整 Milestone 顺序')
    await select('目标 Milestone', 'Anchor')
    f.api.reorder.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('VERSION_CONFLICT')
    })
    await save()
    expect(f.owner.milestoneReorder.beforeID).toBe(id(4))
    expect(f.owner.milestoneReorder.version).toBe(milestone.version)
    f.setCurrent({ ...milestone, version: '9007199254740998' })
    await click('重新读取排序对象')
    expect(f.owner.milestoneReorder.version).toBe(milestone.version)
    await click('使用当前版本继续排序')
    expect(f.owner.milestoneReorder.version).toBe('9007199254740998')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    await select('目标 Milestone', 'Anchor')
    await save()
    expect(f.api.reorder).toHaveBeenLastCalledWith(p, m, {
      expected_version: '9007199254740998',
      request: { before_id: id(4) },
    })
    expect(f.api.reorder).toHaveBeenCalledTimes(2)
  })
  it('keeps Unknown through observations and retries only current reads after Lookup confirms', async () => {
    const f = await fixture()
    await click('调整 Milestone 顺序')
    await select('排序位置', '移到组尾')
    f.api.reorder.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await save()
    await click('查询原 Milestone 排序结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.api.reorder).toHaveBeenCalledTimes(1)
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      state: 'committed',
      result: f.commit({ expected_version: milestone.version, request: {} }),
    }))
    f.reads.milestone.mockRejectedValueOnce(new AccountFailure('transport'))
    await click('查询原 Milestone 排序结果')
    expect(body().text()).toContain('当前内容读取失败')
    expect(f.owner.milestoneReorderProgress.value?.phase).toBe('confirmed')
    await click('读取已确认的排序')
    expect(f.owner.milestoneReorder.needsRead).toBe(false)
    expect(f.api.reorder).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
  })
})
