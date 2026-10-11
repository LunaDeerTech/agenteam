import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Milestone, Sprint } from '../api/work-review'
import type { SprintUpdateInput, SprintUpdateReceipt } from '../api/work-sprint-planning'
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
  description: 'Original Sprint description',
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
type Port = SessionController['workSprintEditing']
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
async function fixture(initial: Sprint = sprint) {
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
  let current: Sprint = { ...initial }
  const publish = (
    phase: NonNullable<Port['progress']>['phase'],
    result: SprintUpdateReceipt | null = null,
    observation: NonNullable<Port['progress']>['observation'] = 'none',
  ) => {
    progress.value = {
      projectID: p,
      sprintID: s,
      phase,
      result,
      observation,
      error: null,
      contextValid: true,
    }
  }
  const commit = (input: SprintUpdateInput, changed = true) => {
    current = {
      ...current,
      ...input.request,
      version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
    }
    const result: SprintUpdateReceipt = {
      command: 'work.sprint.update',
      changed,
      sprint: {
        ...current,
        state: current.state === 'planned' ? 'planned' : 'current',
        started_by: current.state === 'planned' ? null : { kind: 'human', user_id: id(8) },
        completed_at: null,
        completed_by: null,
      },
      milestone: null,
      event_id: changed ? id(20) : null,
    }
    publish('confirmed', result)
    return result
  }
  const api = {
    update: vi.fn<Port['update']>(async (_p, _m, input) => commit(input)),
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
    sprints: vi.fn(async () => ({ items: [{ ...current }] })),
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
    workSprintEditing: api,
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
  owner.afterNavigation(`/owner/demo/tasks/sprints/${s}`)
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
function field(name: string) {
  const label = body()
    .findAll('label')
    .find((v) => v.text().includes(name))
  expect(label).toBeDefined()
  return body().get<HTMLInputElement | HTMLTextAreaElement>(`#${label!.attributes('for')}`)
}
async function save() {
  await body().get('form[aria-label="编辑 Sprint"]').trigger('submit')
  await flushPromises()
}
describe('Sprint editing visible behavior', () => {
  it('prefills, restores cancel focus, saves only the touched field and publishes actual current Get/List', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    expect(field('Sprint 标题').element.value).toBe(sprint.title)
    expect(field('Sprint 描述').element.value).toBe(sprint.description)
    expect(document.activeElement).toBe(field('Sprint 标题').element)
    await click('取消编辑 Sprint')
    expect(document.activeElement).toBe(button('编辑 Sprint').element)
    expect(f.api.update).not.toHaveBeenCalled()
    await click('编辑 Sprint')
    const title = '  e\u0301 delivery 😀  '
    await field('Sprint 标题').setValue(title)
    f.api.update.mockImplementationOnce(async (_p, _m, input) => {
      const result = f.commit(input)
      f.setCurrent({
        ...result.sprint,
        title: 'Newer current fact',
        version: '9007199254740998',
      })
      return result
    })
    const before = f.reads.sprint.mock.calls.length
    await save()
    expect(f.api.update).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: sprint.version,
      request: { title },
    })
    expect(f.reads.sprint).toHaveBeenCalledTimes(before + 1)
    expect(f.reads.sprints.mock.calls.length).toBeGreaterThan(1)
    expect(body().get('.sprint-heading h2').text()).toBe('Newer current fact')
    expect(document.activeElement).toBe(body().get('.sprint-heading h2').element)
    expect(f.progress.value?.result?.sprint.title).toBe(title)
    expect(f.createAPI.create).not.toHaveBeenCalled()
    expect(f.sprintAPI.start).not.toHaveBeenCalled()
  })
  it('allows a planned Sprint and blocks a completed Sprint even for an unchanged value', async () => {
    const f = await fixture({ ...sprint, state: 'planned', started_at: null })
    await click('编辑 Sprint')
    // FEFF is not Unicode White_Space; preserve it without trim/NFC.
    await field('Sprint 标题').setValue('\ufeff')
    expect(button('保存 Sprint').element.disabled).toBe(false)
    await save()
    expect(f.api.update).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: sprint.version,
      request: { title: '\ufeff' },
    })
    f.setCurrent({ ...sprint, state: 'completed', completed_at: at })
    await click('刷新任务看板')
    expect(button('编辑 Sprint').element.disabled).toBe(true)
    f.owner.openSprintEdit()
    f.owner.touchSprintEdit('title', sprint.title)
    await f.owner.submitSprintEdit()
    expect(f.api.update).toHaveBeenCalledTimes(1)
    expect(body().text()).toContain('已完成的 Sprint 不可修改')
    expect(f.sprintAPI.start).not.toHaveBeenCalled()
  })
  it('retains the draft when conflict reread is completed and never adopts its version for another write', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 标题').setValue('Keep my draft')
    f.api.update.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('INVALID_STATE')
    })
    await save()
    f.setCurrent({ ...sprint, state: 'completed', completed_at: at, version: '9007199254740998' })
    await click('重新读取 Sprint')
    expect(field('Sprint 标题').element.value).toBe('Keep my draft')
    expect(button('使用当前版本继续编辑').element.disabled).toBe(true)
    f.owner.adoptSprintVersion()
    await f.owner.submitSprintEdit()
    expect(f.owner.sprintEdit.version).toBe(sprint.version)
    expect(f.api.update).toHaveBeenCalledTimes(1)
    await click('取消编辑 Sprint')
    expect(f.owner.sprintEdit.open).toBe(false)
    expect(document.activeElement).toBe(body().get('.sprint-heading h2').element)
  })
  it('sends explicit empty description and preserves the successful no-op result separately from rereads', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 描述').setValue('')
    await save()
    expect(f.api.update).toHaveBeenLastCalledWith(p, s, {
      expected_version: sprint.version,
      request: { description: '' },
    })
    expect(body().get('.sprint-description').text()).toBe('暂无描述。')
    await click('编辑 Sprint')
    await field('Sprint 标题').setValue('Temporary')
    await field('Sprint 标题').setValue(sprint.title)
    f.api.update.mockImplementationOnce(async (_p, _m, input) => f.commit(input, false))
    await save()
    expect(body().text()).toContain('原操作没有改变内容')
    expect(f.progress.value?.result?.sprint.version).toBe('9007199254740994')
  })
  it('retains the draft and original version on conflict, then requires a read and explicit version adoption before saving', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 标题').setValue('My draft')
    f.api.update.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('VERSION_CONFLICT')
    })
    await save()
    expect(field('Sprint 标题').element.value).toBe('My draft')
    expect(button('保存 Sprint').element.disabled).toBe(true)
    f.setCurrent({
      ...sprint,
      title: 'Other title',
      description: 'New description',
      version: '9007199254740995',
    })
    await click('重新读取 Sprint')
    expect(f.owner.sprintEdit.version).toBe(sprint.version)
    expect(field('Sprint 标题').element.value).toBe('My draft')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    await click('使用当前版本继续编辑')
    expect(field('Sprint 描述').element.value).toBe('New description')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    await save()
    expect(f.api.update).toHaveBeenLastCalledWith(p, s, {
      expected_version: '9007199254740995',
      request: { title: 'My draft' },
    })
  })
  it('keeps confirmed status when the current read fails and retries only the read', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 标题').setValue('Saved')
    f.reads.sprint.mockRejectedValueOnce(new AccountFailure('transport'))
    await save()
    expect(body().text()).toContain('保存已确认，当前内容读取失败')
    expect(body().get('.sprint-heading h2').text()).toBe(sprint.title)
    await click('读取已确认的 Sprint')
    expect(body().get('.sprint-heading h2').text()).toBe('Saved')
    expect(f.api.update).toHaveBeenCalledTimes(1)
  })
  it('hides the editor while Unknown and allows only original Lookup, including for an archived Project', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 描述').setValue('Unknown draft')
    f.api.update.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await save()
    expect(body().find('form[aria-label="编辑 Sprint"]').exists()).toBe(false)
    f.detail.project = { ...project, lifecycle: 'archived', archived_at: at }
    await flushPromises()
    for (const observation of ['not_observed', 'in_progress'] as const) {
      f.api.checkOriginal.mockImplementationOnce(async () => {
        f.publish('uncertain', null, observation)
        return { state: observation, result: null }
      })
      await click('查询原 Sprint 保存结果')
    }
    f.api.checkOriginal.mockImplementationOnce(async () => {
      const result = f.commit({
        expected_version: sprint.version,
        request: { description: 'Unknown draft' },
      })
      f.setCurrent({
        ...result.sprint,
        state: 'completed',
        completed_at: at,
        version: '9007199254740999',
      })
      return { state: 'committed', result }
    })
    await click('查询原 Sprint 保存结果')
    expect(body().get('.sprint-description').text()).toBe('Unknown draft')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(3)
    expect(f.owner.state.sprint?.state).toBe('completed')
    expect(f.progress.value?.result?.sprint.state).toBe('current')
    expect(button('编辑 Sprint').element.disabled).toBe(true)
  })
  it('preserves drafts through same-identity checking and clears material on an actual denied reread', async () => {
    const f = await fixture()
    await click('编辑 Sprint')
    await field('Sprint 标题').setValue('Private draft')
    const context = f.context.value
    f.context.value = null
    f.sessionState.phase = 'checking'
    await flushPromises()
    expect(body().text()).not.toContain('Private draft')
    f.sessionState.phase = 'authenticated'
    f.context.value = context
    await flushPromises()
    expect(field('Sprint 标题').element.value).toBe('Private draft')
    f.reads.sprint.mockRejectedValueOnce(failure('NOT_FOUND', 404))
    await save()
    expect(f.api.forgetUnavailable).toHaveBeenCalledExactlyOnceWith(p)
    expect(f.progress.value).toBeNull()
    expect(f.owner.sprintEdit.title).toBe('')
    expect(f.owner.state.sprint).toBeNull()
    expect(body().text()).toContain('编辑内容已清除')
  })
})
