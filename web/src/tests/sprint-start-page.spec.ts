import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, nextTick, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { SprintStartReceipt } from '../api/work-sprint-lifecycle'
import {
  createProjectTasks,
  projectTasksKey,
  type ProjectTasks,
} from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import ProjectTasksView from '../views/projects/ProjectTasksView.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const p = id(1),
  m = id(2),
  s = id(3)
const base = {
  project_id: p,
  manual_rank: '8'.repeat(32),
  version: '7',
  created_at: time,
  updated_at: time,
}
const milestone = { ...base, id: m, title: '里程碑', description: '' }
const planned: Sprint = {
  ...base,
  id: s,
  milestone_id: m,
  title: '待启动迭代',
  description: '',
  state: 'planned',
  started_at: null,
  completed_at: null,
}
const originalProject: Project = {
  id: p,
  owner_user_id: id(8),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '3',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: null,
}
const receipt: SprintStartReceipt = {
  sprint: {
    ...planned,
    version: '8',
    state: 'current',
    started_at: time,
    started_by: { kind: 'human', user_id: id(8) },
    completed_at: null,
    completed_by: null,
  },
  project: {
    id: p,
    owner_user_id: id(8),
    name: 'Demo',
    normalized_name: 'demo',
    description: '',
    lifecycle: 'active',
    version: '4',
    current_sprint_id: s,
    created_at: time,
    updated_at: time,
  },
  event_id: id(20),
}
type Port = SessionController['workSprintLifecycle']
const owners: ProjectTasks[] = []
const wrappers: VueWrapper[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function page() {
  // Typed Session and current Project fixtures test real controller/View/Ui controls.
  // They do not stand in for HTTP or backend authorization acceptance.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef({ identity, projectID: p, generation: 1, readGeneration: 1 })
  const detail = shallowReactive<{ project: Project }>({ project: { ...originalProject } })
  const progress = shallowRef<Port['progress']>(null)
  let stored: Sprint = planned
  const publish = (
    phase: NonNullable<Port['progress']>['phase'],
    value: SprintStartReceipt | null = null,
  ) => {
    progress.value = {
      projectID: p,
      sprintID: s,
      phase,
      contextValid: true,
      observation: value ? 'committed' : 'none',
      receipt: value,
      error: null,
    }
  }
  const commit = () => {
    stored = receipt.sprint
    publish('confirmed', receipt)
    return receipt
  }
  const api = {
    createProgress: null,
    create: vi.fn<Port['create']>(async () => {
      throw new Error('Unexpected Sprint Create')
    }),
    checkOriginalCreate: vi.fn<Port['checkOriginalCreate']>(async () => {
      throw new Error('Unexpected Sprint Create Lookup')
    }),
    editRejectedCreate: vi.fn(() => {
      throw new Error('Unexpected Sprint Create rejection')
    }),
    start: vi.fn<Port['start']>(async () => commit()),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => ({
      status: 'not_observed',
      receipt: null,
    })),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    get progress() {
      return progress.value
    },
  } satisfies Port
  const reads = {
    progress: null,
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [stored] })),
    sprint: vi.fn(async () => stored),
    agents: vi.fn(async () => ({ items: [] })),
    tasks: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
  }
  const auth = {
    state: shallowReactive({ phase: 'authenticated', busy: false }),
    personalContext: shallowReactive({ identity }),
    workReview: reads,
    workMilestonePlanning: {
      progress: null,
      create: vi.fn(async () => { throw new Error('Unexpected Milestone Create') }),
      checkOriginal: vi.fn(async () => { throw new Error('Unexpected Milestone Lookup') }),
      editRejected: vi.fn(), abandon: vi.fn(),
    },
    workSprintLifecycle: api,
  } as unknown as SessionController
  const readCurrent = vi.fn(async () => {
    detail.project = { ...receipt.project, archived_at: null }
  })
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: '/owner/demo' })),
    readCurrent,
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async (path) => {
    owner.afterNavigation(path)
  })
  owners.push(owner)
  owner.afterNavigation('/owner/demo/tasks/sprints/' + s)
  const wrapper = mount(ProjectTasksView, {
    attachTo: document.body,
    global: { provide: { [projectTasksKey as symbol]: owner } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { owner, api, readCurrent, detail, progress, publish, commit }
}
const body = () => new DOMWrapper(document.body)
function label(element: Element) {
  const copy = element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim() ?? ''
}
function findButton(text: string) {
  return body()
    .findAll<HTMLButtonElement>('button')
    .find((button) => label(button.element) === text)
}
function button(text: string) {
  const found = findButton(text)
  expect(found, text).toBeDefined()
  return found!
}
async function click(text: string) {
  await button(text).trigger('click')
  await flushPromises()
}

describe('Sprint Start visible workflow', () => {
  it('confirms explicitly, restores cancellation focus, then shows the reread current board without enabling scheduling', async () => {
    const f = await page()
    expect(f.api.start).not.toHaveBeenCalled()
    expect(button('新建任务').element.disabled).toBe(true)
    expect(body().text()).toContain('已暂停的调度保持暂停')
    await click('启动 Sprint')
    expect(document.activeElement).toBe(button('确认启动 Sprint').element)
    await click('取消启动')
    expect(body().find('form[aria-label="启动 Sprint 确认"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button('启动 Sprint').element)
    expect(f.api.start).not.toHaveBeenCalled()
    await click('启动 Sprint')
    await body().get('form[aria-label="启动 Sprint 确认"]').trigger('submit')
    await flushPromises()
    expect(f.api.start).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: '7',
      request: {},
    })
    expect(f.readCurrent).toHaveBeenCalledTimes(1)
    expect(f.detail.project.current_sprint_id).toBe(s)
    expect(body().get('.sprint-heading').text()).toContain('当前')
    expect(body().text()).toContain('Sprint 已启动，已确认当前 Sprint。')
    expect(button('新建任务').element.disabled).toBe(false)
    expect(document.activeElement).toBe(body().get('.sprint-heading h2').element)
    expect(body().findAll('.task-column')).toHaveLength(7)
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
  })

  it('shows only original Lookup after Unknown and retries a failed Project read without another Start', async () => {
    const f = await page()
    f.api.start.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw new AccountFailure('transport')
    })
    await click('启动 Sprint')
    await body().get('form[aria-label="启动 Sprint 确认"]').trigger('submit')
    await flushPromises()
    expect(body().get('section[aria-label="启动结果恢复"]').text()).toContain(
      '启动结果待确认。请查询原启动结果，不要重复启动。',
    )
    expect(findButton('启动 Sprint')).toBeUndefined()
    expect(body().find('form[aria-label="启动 Sprint 确认"]').exists()).toBe(false)
    f.progress.value = { ...f.progress.value!, contextValid: false }
    await flushPromises()
    expect(button('查询原启动结果').element.disabled).toBe(true)
    f.progress.value = { ...f.progress.value!, contextValid: true }
    await nextTick()
    expect(button('查询原启动结果').element.disabled).toBe(false)
    f.api.checkOriginal.mockImplementationOnce(async () => {
      f.progress.value = { ...f.progress.value!, observation: 'not_observed' }
      return { status: 'not_observed', receipt: null }
    })
    await click('查询原启动结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(body().text()).toContain('尚未观察到原启动结果，可再次查询。')
    expect(f.readCurrent).not.toHaveBeenCalled()
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      status: 'committed',
      receipt: f.commit(),
    }))
    f.readCurrent.mockImplementationOnce(async () => {
      throw new AccountFailure('transport')
    })
    await click('查询原启动结果')
    expect(body().text()).toContain('启动已确认，当前信息读取失败。请重新读取，不要再次启动。')
    expect(button('新建任务').element.disabled).toBe(true)
    await click('重新读取启动条件与当前状态')
    expect(body().get('.sprint-heading').text()).toContain('当前')
    expect(document.activeElement).toBe(body().get('.sprint-heading h2').element)
    expect(f.api.start).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.readCurrent).toHaveBeenCalledTimes(2)
  })
})
