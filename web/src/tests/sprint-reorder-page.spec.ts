import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Milestone, Sprint } from '../api/work-review'
import type { SprintReorderInput, SprintReorderReceipt } from '../api/work-sprint-planning'
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
type Port = SessionController['workSprintReordering']
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
  let current: Sprint = { ...sprint }
  const publish = (
    phase: NonNullable<Port['progress']>['phase'],
    result: SprintReorderReceipt | null = null,
    observation: NonNullable<Port['progress']>['observation'] = 'none',
  ) => {
    progress.value = {
      projectID: p,
      milestoneID: m,
      sprintID: s,
      phase,
      result,
      observation,
      error: null,
      contextValid: true,
    }
  }
  const commit = (input: SprintReorderInput, changed = true) => {
    current = {
      ...current,
      version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
    }
    const result: SprintReorderReceipt = {
      command: 'work.sprint.reorder',
      changed,
      milestone: null,
      sprint: {
        ...current,
        state: current.state as 'planned' | 'current',
        started_by: current.state === 'current' ? { kind: 'human', user_id: id(8) } : null,
        completed_at: null,
        completed_by: null,
      },
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
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(
      async (_p: string, _m: string, _query?: { limit?: number; cursor?: string }) => ({
        items: [{ ...current }, { ...current, id: id(4), title: 'Anchor' }],
        next_cursor: undefined as string | undefined,
      }),
    ),
    sprint: vi.fn(async () => ({ ...current })),
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
    workSprintReordering: api,
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
    setCurrent(value: Sprint) {
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
  await body().get('form[aria-label="调整 Sprint 顺序"]').trigger('submit')
  await flushPromises()
}
describe('Sprint reorder visible behavior and paging', () => {
  it('offers real anchors, cancels with focus restoration and keeps current Get separate from the receipt', async () => {
    const f = await fixture()
    await click('调整 Sprint 顺序')
    expect(document.activeElement?.textContent).toBe('调整 Sprint 顺序')
    await click('取消调整顺序')
    expect(document.activeElement).toBe(button('调整 Sprint 顺序').element)
    expect(f.api.reorder).not.toHaveBeenCalled()
    await click('调整 Sprint 顺序')
    await select('目标 Sprint', 'Anchor')
    expect(f.owner.canEditSprint.value).toBe(false)
    f.api.reorder.mockImplementationOnce(async (_p, _m, input) => {
      const result = f.commit(input)
      f.setCurrent({
        ...result.sprint,
        title: 'Newer current fact',
        version: '9007199254740998',
      })
      return result
    })
    await save()
    expect(f.api.reorder).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: sprint.version,
      request: { milestone_id: m, before_id: id(4) },
    })
    expect(f.owner.state.sprint?.title).toBe('Newer current fact')
    expect(f.owner.sprintReorderProgress.value?.result?.sprint.title).toBe(sprint.title)
    expect(document.activeElement?.textContent).toBe('Newer current fact')
  })
  it('submits group tail with fixed parent and omitted anchor and confirms no-op without inventing a new version', async () => {
    const f = await fixture()
    await click('调整 Sprint 顺序')
    await select('排序位置', '移到组尾')
    f.api.reorder.mockImplementationOnce(async (_p, _m, input) => f.commit(input, false))
    await save()
    expect(f.api.reorder).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: sprint.version,
      request: { milestone_id: m },
    })
    expect(f.owner.state.sprint?.version).toBe(sprint.version)
    expect(body().text()).toContain('原操作无需调整')
  })
  it('reads real candidate pages with opaque cursors and explicitly restarts an invalidated chain', async () => {
    const f = await fixture()
    const first = Array.from({ length: 50 }, (_, i) => ({
      ...sprint,
      id: id(100 + i),
      title: `Candidate ${i}`,
    }))
    f.reads.sprints.mockResolvedValueOnce({ items: first, next_cursor: 'opaque-first' })
    await click('调整 Sprint 顺序')
    expect(f.owner.sprintCandidates.items).toHaveLength(50)
    await select('目标 Sprint', 'Candidate 0')
    f.reads.sprints.mockResolvedValueOnce({
      items: [{ ...sprint, id: id(200), title: 'Beyond first page' }],
      next_cursor: undefined,
    })
    await click('下一页候选')
    expect(f.reads.sprints).toHaveBeenLastCalledWith(p, m, { limit: 50, cursor: 'opaque-first' })
    expect(f.owner.sprintReorder.beforeID).toBe('')
    await select('目标 Sprint', 'Beyond first page')
    f.reads.sprints.mockRejectedValueOnce(failure('CURSOR_STALE'))
    await click('上一页候选')
    expect(f.owner.sprintCandidates.items).toHaveLength(0)
    expect(f.owner.sprintReorder.beforeID).toBe('')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    expect(f.api.reorder).not.toHaveBeenCalled()
    await click('从首页重新读取')
    expect(f.reads.sprints).toHaveBeenLastCalledWith(p, m, { limit: 50 })
    expect(f.owner.sprintCandidates.page).toBe(0)
  })
  it('keeps a rejected anchor and version until explicit read and adoption, then requires fresh selection', async () => {
    const f = await fixture()
    await click('调整 Sprint 顺序')
    await select('目标 Sprint', 'Anchor')
    f.api.reorder.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('VERSION_CONFLICT')
    })
    await save()
    expect(f.owner.sprintReorder.beforeID).toBe(id(4))
    expect(f.owner.sprintReorder.version).toBe(sprint.version)
    f.setCurrent({ ...sprint, version: '9007199254740998' })
    await click('重新读取排序对象')
    expect(f.owner.sprintReorder.version).toBe(sprint.version)
    await click('使用当前版本继续排序')
    expect(f.owner.sprintReorder.version).toBe('9007199254740998')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    await select('目标 Sprint', 'Anchor')
    await save()
    expect(f.api.reorder).toHaveBeenLastCalledWith(p, s, {
      expected_version: '9007199254740998',
      request: { milestone_id: m, before_id: id(4) },
    })
    expect(f.api.reorder).toHaveBeenCalledTimes(2)
  })
  it('keeps Unknown through observations and retries only current reads after Lookup confirms', async () => {
    const f = await fixture()
    await click('调整 Sprint 顺序')
    await select('排序位置', '移到组尾')
    f.api.reorder.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await save()
    await click('查询原 Sprint 排序结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.api.reorder).toHaveBeenCalledTimes(1)
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      state: 'committed',
      result: f.commit({ expected_version: sprint.version, request: { milestone_id: m } }),
    }))
    f.reads.sprint.mockRejectedValueOnce(new AccountFailure('transport'))
    await click('查询原 Sprint 排序结果')
    expect(body().text()).toContain('当前内容读取失败')
    expect(f.owner.sprintReorderProgress.value?.phase).toBe('confirmed')
    await click('读取已确认的排序')
    expect(f.owner.sprintReorder.needsRead).toBe(false)
    expect(f.api.reorder).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
  })
  it('rejects completed targets and another parent while keeping cancellation reachable', async () => {
    const f = await fixture()
    await click('调整 Sprint 顺序')
    await select('目标 Sprint', 'Anchor')
    f.setCurrent({ ...sprint, state: 'completed', completed_at: at })
    await f.owner.refresh()
    await flushPromises()
    expect(body().text()).toContain('Sprint 已完成，不能调整顺序')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    await save()
    expect(f.api.reorder).not.toHaveBeenCalled()
    await click('取消调整顺序')
    expect(document.activeElement?.textContent).toBe(sprint.title)
    f.owner.state.sprint = { ...sprint, milestone_id: id(77) }
    await flushPromises()
    expect(button('调整 Sprint 顺序').element.disabled).toBe(true)
  })
  it('keeps same-parent candidates separate and rejects late publication after identity loss', async () => {
    const f = await fixture()
    f.reads.sprints.mockResolvedValueOnce({
      items: [{ ...sprint, id: id(70), milestone_id: id(71), title: 'Foreign parent' }],
      next_cursor: undefined,
    })
    await click('调整 Sprint 顺序')
    expect(f.owner.sprintCandidates.items).toHaveLength(0)
    expect(f.owner.sprintCandidates.phase).toBe('error')
    expect(body().text()).not.toContain('Foreign parent')
    expect(f.owner.sprintReorder.beforeID).toBe('')
    expect(button('确认调整顺序').element.disabled).toBe(true)
    await click('取消调整顺序')
    let release!: () => void
    f.reads.sprints.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = () =>
            resolve({
              items: [{ ...sprint, id: id(4), title: 'Late anchor' }],
              next_cursor: undefined,
            })
        }),
    )
    const reading = click('调整 Sprint 顺序')
    await flushPromises()
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    release()
    await reading
    expect(f.owner.sprintCandidates.items).toHaveLength(0)
    expect(f.owner.sprintReorder.open).toBe(false)
    expect(f.api.reorder).not.toHaveBeenCalled()
  })
})
