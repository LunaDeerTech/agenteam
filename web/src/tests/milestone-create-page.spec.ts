import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { MilestoneCreateDraft, MilestoneCreateReceipt } from '../api/work-milestone-planning'
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
  currentID = id(3),
  createdID = id(4),
  time = '2026-10-11T10:00:00.000000Z'
const base = {
  project_id: p,
  manual_rank: '8'.repeat(32),
  version: '1',
  created_at: time,
  updated_at: time,
}
const milestone = { ...base, id: m, title: '真实里程碑', description: '' }
const currentSprint: Sprint = {
  ...base,
  id: currentID,
  milestone_id: m,
  title: '当前迭代',
  description: '',
  state: 'current',
  started_at: time,
  completed_at: null,
}
const project: Project = {
  id: p,
  owner_user_id: id(8),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '3',
  current_sprint_id: currentID,
  created_at: time,
  updated_at: time,
  archived_at: null,
}
type Port = SessionController['workMilestonePlanning']
const owners: ProjectTasks[] = [],
  wrappers: VueWrapper[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  wrappers.splice(0).forEach((w) => w.unmount())
  owners.splice(0).forEach((o) => o.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function fixture(existing = false) {
  // Real controller with controlled typed Session/read ports, not SQL or authorization proof.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    identity,
    projectID: p,
    generation: 1,
    readGeneration: 1,
  })
  const detail = shallowReactive<{ project: Project | null }>({
    project: { ...project, current_sprint_id: existing ? currentID : null },
  })
  const home = shallowRef('/owner/demo')
  const progress = shallowRef<Port['progress']>(null)
  let stored: MilestoneCreateReceipt | null = null
  const build = (draft: MilestoneCreateDraft): MilestoneCreateReceipt => ({
    command: 'work.milestone.create',
    changed: true,
    sprint: null,
    event_id: id(20),
    milestone: { ...base, id: createdID, title: draft.title, description: draft.description ?? '' },
  })
  const publish = (
    phase: NonNullable<Port['progress']>['phase'],
    value: MilestoneCreateReceipt | null = null,
  ) => {
    progress.value = {
      projectID: p,
      milestoneID: createdID,
      phase,
      contextValid: true,
      result: value,
      observation: value ? 'committed' : 'none',
      error: null,
    }
  }
  const commit = (draft: MilestoneCreateDraft) => {
    stored = build(draft)
    publish('confirmed', stored)
    return stored
  }
  const api = {
    create: vi.fn<Port['create']>(async (_p, draft) => commit(draft)),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => ({
      state: 'not_observed',
      result: null,
    })),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    abandon: vi.fn(),
    get progress() {
      return progress.value
    },
  } satisfies Port
  let omitCreated = false
  const reads = {
    progress: null,
    milestones: vi.fn(async () => ({
      items: [
        ...(existing ? [milestone] : []),
        ...(stored && !omitCreated ? [stored.milestone] : []),
      ],
    })),
    milestone: vi.fn(async (_p: string, mid: string) => {
      if (stored && mid === createdID) return stored.milestone
      if (existing && mid === m) return milestone
      throw new AccountFailure('invalid-response')
    }),
    sprints: vi.fn(async (_p: string, mid: string) => ({
      items: existing && mid === m ? [currentSprint] : [],
    })),
    sprint: vi.fn(async () => currentSprint),
    agents: vi.fn(async () => ({ items: [] })),
    tasks: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
  }
  const sprintAPI = {
    progress: null,
    createProgress: null,
    create: vi.fn(async () => {
      throw new Error('Unexpected Sprint Create')
    }),
    start: vi.fn(async () => {
      throw new Error('Unexpected Start')
    }),
    checkOriginal: vi.fn(async () => {
      throw new Error('Unexpected Start Lookup')
    }),
    checkOriginalCreate: vi.fn(async () => {
      throw new Error('Unexpected Sprint Lookup')
    }),
    editRejected: vi.fn(),
    editRejectedCreate: vi.fn(),
  } satisfies SessionController['workSprintLifecycle']
  const auth = {
    state: shallowReactive({ phase: 'authenticated', busy: false }),
    personalContext: personal,
    workReview: reads,
    workMilestonePlanning: api,
    workSprintLifecycle: sprintAPI,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: home.value })),
    readCurrent: vi.fn(),
  } as unknown as ProjectWorkspace
  const navigate = vi.fn(async (path: string) => {
    owner.afterNavigation(path)
  })
  const owner = createProjectTasks(auth, workspace, navigate)
  owners.push(owner)
  owner.afterNavigation('/owner/demo/tasks')
  const wrapper = mount(ProjectTasksView, {
    attachTo: document.body,
    global: { provide: { [projectTasksKey as symbol]: owner } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return {
    wrapper,
    owner,
    api,
    reads,
    navigate,
    progress,
    publish,
    commit,
    build,
    personal,
    context,
    detail,
    home,
    sprintAPI,
    omitCreated: () => {
      omitCreated = true
    },
  }
}

const body = () => new DOMWrapper(document.body)
function button(text: string) {
  const found = body()
    .findAll<HTMLButtonElement>('button')
    .find((b) => {
      const copy = b.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((v) => v.remove())
      return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === text
    })
  expect(found, text).toBeDefined()
  return found!
}
async function click(text: string) {
  await button(text).trigger('click')
  await flushPromises()
}
function input(text: string) {
  const label = body()
    .findAll('label')
    .find((v) => v.text().includes(text))
  expect(label).toBeDefined()
  return body().get<HTMLInputElement | HTMLTextAreaElement>(`#${label!.attributes('for')}`)
}
describe('Milestone creation controls', () => {
  it('creates the first Milestone through real controls, restores focus and opens Sprint planning only explicitly', async () => {
    const f = await fixture()
    await click('新建 Milestone')
    expect(document.activeElement).toBe(input('Milestone 标题').element)
    await click('取消创建 Milestone')
    expect(document.activeElement).toBe(button('新建 Milestone').element)
    expect(f.api.create).not.toHaveBeenCalled()
    await click('新建 Milestone')
    await input('Milestone 标题').setValue('First milestone')
    await input('Milestone 描述').setValue('完整说明\n第二行。')
    await body().get('form[aria-label="新建 Milestone"]').trigger('submit')
    await flushPromises()
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, {
      title: 'First milestone',
      description: '完整说明\n第二行。',
    })
    expect(body().get('.selected-milestone h2').text()).toBe('First milestone')
    expect(document.activeElement).toBe(body().get('.selected-milestone h2').element)
    expect(f.detail.project?.current_sprint_id).toBeNull()
    expect(f.sprintAPI.create).not.toHaveBeenCalled()
    await click('新建 Sprint')
    expect(body().get('form[aria-label="新建 Sprint"]').text()).toContain('First milestone')
    await click('取消创建 Sprint')
    expect(document.activeElement).toBe(button('新建 Sprint').element)
    expect(f.sprintAPI.create).not.toHaveBeenCalled()
    expect(f.sprintAPI.start).not.toHaveBeenCalled()
  })
  it('hides an uncertain form and offers only the original Milestone lookup and confirmed reread', async () => {
    const f = await fixture(),
      draft = { title: 'Uncertain milestone' }
    f.api.create.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw new AccountFailure('transport')
    })
    await click('新建 Milestone')
    await input('Milestone 标题').setValue(draft.title)
    await body().get('form[aria-label="新建 Milestone"]').trigger('submit')
    await flushPromises()
    expect(body().find('form[aria-label="新建 Milestone"]').exists()).toBe(false)
    expect(body().text()).toContain('Milestone 创建结果待确认')
    await click('查询原 Milestone 创建结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      state: 'committed',
      result: f.commit(draft),
    }))
    f.reads.milestone.mockRejectedValueOnce(new AccountFailure('transport'))
    await click('查询原 Milestone 创建结果')
    expect(body().text()).toContain('当前 Milestone 信息读取失败')
    await click('读取已创建的 Milestone')
    expect(body().get('.selected-milestone h2').text()).toBe(draft.title)
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, draft)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.sprintAPI.create).not.toHaveBeenCalled()
  })
})
