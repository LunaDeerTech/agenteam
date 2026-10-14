import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Milestone, Sprint } from '../api/work-review'
import type { MilestoneUpdateInput, MilestoneUpdateReceipt } from '../api/work-milestone-planning'
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
type Port = SessionController['workMilestoneEditing']
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
    result: MilestoneUpdateReceipt | null = null,
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
  const commit = (input: MilestoneUpdateInput, changed = true) => {
    current = {
      ...current,
      ...input.request,
      version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
    }
    const result: MilestoneUpdateReceipt = {
      command: 'work.milestone.update',
      changed,
      milestone: { ...current },
      sprint: null,
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
    milestones: vi.fn(async () => ({ items: [{ ...current }] })),
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
    workMilestoneEditing: api,
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
function field(name: string) {
  const label = body()
    .findAll('label')
    .find((v) => v.text().includes(name))
  expect(label).toBeDefined()
  return body().get<HTMLInputElement | HTMLTextAreaElement>(`#${label!.attributes('for')}`)
}
async function save() {
  await body().get('form[aria-label="编辑 Milestone"]').trigger('submit')
  await flushPromises()
}
describe('Milestone editing visible behavior', () => {
  it('prefills, restores cancel focus, saves only the touched field and publishes actual current Get/List', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    expect(field('Milestone 标题').element.value).toBe(milestone.title)
    expect(field('Milestone 描述').element.value).toBe(milestone.description)
    expect(document.activeElement).toBe(field('Milestone 标题').element)
    await click('取消编辑 Milestone')
    expect(document.activeElement).toBe(button('编辑 Milestone').element)
    expect(f.api.update).not.toHaveBeenCalled()
    await click('编辑 Milestone')
    const title = '  e\u0301 delivery 😀  '
    await field('Milestone 标题').setValue(title)
    f.api.update.mockImplementationOnce(async (_p, _m, input) => {
      const result = f.commit(input)
      f.setCurrent({
        ...result.milestone,
        title: 'Newer current fact',
        version: '9007199254740998',
      })
      return result
    })
    const before = f.reads.milestone.mock.calls.length
    await save()
    expect(f.api.update).toHaveBeenCalledExactlyOnceWith(p, m, {
      expected_version: milestone.version,
      request: { title },
    })
    expect(f.reads.milestone).toHaveBeenCalledTimes(before + 1)
    expect(f.reads.milestones.mock.calls.length).toBeGreaterThan(1)
    expect(body().get('.selected-milestone h2').text()).toBe('Newer current fact')
    expect(document.activeElement).toBe(body().get('.selected-milestone h2').element)
    expect(f.progress.value?.result?.milestone.title).toBe(title)
    expect(f.createAPI.create).not.toHaveBeenCalled()
    expect(f.sprintAPI.start).not.toHaveBeenCalled()
  })
  it('sends explicit empty description and preserves the successful no-op result separately from rereads', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    await field('Milestone 描述').setValue('')
    await save()
    expect(f.api.update).toHaveBeenLastCalledWith(p, m, {
      expected_version: milestone.version,
      request: { description: '' },
    })
    expect(body().find('.milestone-description').exists()).toBe(false)
    await click('编辑 Milestone')
    await field('Milestone 标题').setValue('Temporary')
    await field('Milestone 标题').setValue(milestone.title)
    f.api.update.mockImplementationOnce(async (_p, _m, input) => f.commit(input, false))
    await save()
    expect(body().text()).toContain('原操作没有改变内容')
    expect(f.progress.value?.result?.milestone.version).toBe('9007199254740994')
  })
  it('retains the draft and original version on conflict, then requires a read and explicit version adoption before saving', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    await field('Milestone 标题').setValue('My draft')
    f.api.update.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw failure('VERSION_CONFLICT')
    })
    await save()
    expect(field('Milestone 标题').element.value).toBe('My draft')
    expect(button('保存 Milestone').element.disabled).toBe(true)
    f.setCurrent({
      ...milestone,
      title: 'Other title',
      description: 'New description',
      version: '9007199254740995',
    })
    await click('重新读取 Milestone')
    expect(f.owner.milestoneEdit.version).toBe(milestone.version)
    expect(field('Milestone 标题').element.value).toBe('My draft')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    await click('使用当前版本继续编辑')
    expect(field('Milestone 描述').element.value).toBe('New description')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    await save()
    expect(f.api.update).toHaveBeenLastCalledWith(p, m, {
      expected_version: '9007199254740995',
      request: { title: 'My draft' },
    })
  })
  it('keeps confirmed status when the current read fails and retries only the read', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    await field('Milestone 标题').setValue('Saved')
    f.reads.milestone.mockRejectedValueOnce(new AccountFailure('transport'))
    await save()
    expect(body().text()).toContain('保存已确认，当前内容读取失败')
    expect(body().get('.selected-milestone h2').text()).toBe(milestone.title)
    await click('读取已确认的 Milestone')
    expect(body().get('.selected-milestone h2').text()).toBe('Saved')
    expect(f.api.update).toHaveBeenCalledTimes(1)
  })
  it('hides the editor while Unknown and allows only original Lookup, including for an archived Project', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    await field('Milestone 描述').setValue('Unknown draft')
    f.api.update.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw failure('COMMIT_UNKNOWN')
    })
    await save()
    expect(body().find('form[aria-label="编辑 Milestone"]').exists()).toBe(false)
    f.detail.project = { ...project, lifecycle: 'archived', archived_at: at }
    await flushPromises()
    for (const observation of ['not_observed', 'in_progress'] as const) {
      f.api.checkOriginal.mockImplementationOnce(async () => {
        f.publish('uncertain', null, observation)
        return { state: observation, result: null }
      })
      await click('查询原 Milestone 保存结果')
    }
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      state: 'committed',
      result: f.commit({
        expected_version: milestone.version,
        request: { description: 'Unknown draft' },
      }),
    }))
    await click('查询原 Milestone 保存结果')
    expect(body().get('.milestone-description').text()).toBe('Unknown draft')
    expect(f.api.update).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(3)
    expect(button('编辑 Milestone').element.disabled).toBe(true)
  })
  it('preserves drafts through same-identity checking and clears material on an actual denied reread', async () => {
    const f = await fixture()
    await click('编辑 Milestone')
    await field('Milestone 标题').setValue('Private draft')
    const context = f.context.value
    f.context.value = null
    f.sessionState.phase = 'checking'
    await flushPromises()
    expect(body().text()).not.toContain('Private draft')
    f.sessionState.phase = 'authenticated'
    f.context.value = context
    await flushPromises()
    expect(field('Milestone 标题').element.value).toBe('Private draft')
    f.reads.milestone.mockRejectedValueOnce(failure('NOT_FOUND', 404))
    await save()
    expect(f.api.forgetUnavailable).toHaveBeenCalledExactlyOnceWith(p)
    expect(f.progress.value).toBeNull()
    expect(f.owner.milestoneEdit.title).toBe('')
    expect(f.owner.state.milestone).toBeNull()
    expect(body().text()).toContain('编辑内容已清除')
  })
})
