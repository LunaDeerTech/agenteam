import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { SprintCreateDraft, SprintCreateReceipt } from '../api/work-sprint-lifecycle'
import { createProjectTasks, projectTasksKey, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import ProjectTasksView from '../views/projects/ProjectTasksView.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1), m = id(2), currentID = id(3), createdID = id(4), time = '2026-10-11T10:00:00.000000Z'
const base = { project_id: p, manual_rank: '8'.repeat(32), version: '1', created_at: time, updated_at: time }
const milestone = { ...base, id: m, title: '真实里程碑', description: '' }
const currentSprint: Sprint = { ...base, id: currentID, milestone_id: m, title: '当前迭代', description: '', state: 'current', started_at: time, completed_at: null }
const project: Project = { id: p, owner_user_id: id(8), name: 'Demo', normalized_name: 'demo', description: '', lifecycle: 'active', version: '3', current_sprint_id: currentID, created_at: time, updated_at: time, archived_at: null }
type Port = SessionController['workSprintLifecycle']
const owners: ProjectTasks[] = [], wrappers: VueWrapper[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(() => [{ width: 10, height: 10 }] as unknown as DOMRectList)
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
async function fixture() {
  // Real controller with controlled typed Session/read ports, not SQL or authorization proof.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({ identity, projectID: p, generation: 1, readGeneration: 1 })
  const detail = shallowReactive<{ project: Project | null }>({ project: { ...project } })
  const home = shallowRef('/owner/demo')
  const progress = shallowRef<Port['createProgress']>(null)
  let stored: SprintCreateReceipt | null = null
  const build = (draft: SprintCreateDraft): SprintCreateReceipt => ({ command: 'work.sprint.create', changed: true, milestone: null, event_id: id(20), sprint: { ...base, id: createdID, milestone_id: draft.milestone_id, title: draft.title, description: draft.description ?? '', state: 'planned', started_at: null, started_by: null, completed_at: null, completed_by: null } })
  const publish = (phase: NonNullable<Port['createProgress']>['phase'], value: SprintCreateReceipt | null = null) => {
    progress.value = { projectID: p, sprintID: createdID, milestoneID: m, phase, contextValid: true, result: value, observation: value ? 'committed' : 'none', error: null }
  }
  const commit = (draft: SprintCreateDraft) => { stored = build(draft); publish('confirmed', stored); return stored }
  const api = {
    progress: null,
    start: vi.fn<Port['start']>(async () => { throw new Error('Unexpected automatic Start') }),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => { throw new Error('Unexpected Start Lookup') }),
    editRejected: vi.fn(),
    create: vi.fn<Port['create']>(async (_p, draft) => commit(draft)),
    checkOriginalCreate: vi.fn<Port['checkOriginalCreate']>(async () => ({ state: 'not_observed', result: null })),
    editRejectedCreate: vi.fn(() => { progress.value = null }),
    get createProgress() { return progress.value },
  } satisfies Port
  const reads = {
    progress: null,
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: stored ? [currentSprint, stored.sprint] : [currentSprint] })),
    sprint: vi.fn(async (_p: string, sid: string): Promise<Sprint> => {
      if (sid === currentID) return currentSprint
      if (sid === createdID && stored) return stored.sprint
      throw new AccountFailure('invalid-response')
    }),
    agents: vi.fn(async () => ({ items: [] })),
    tasks: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
  }
  const auth = { state: shallowReactive({ phase: 'authenticated', busy: false }), personalContext: personal, workReview: reads, workSprintLifecycle: api } as unknown as SessionController
  const workspace = { currentReadContext: context, detail, paths: computed(() => ({ home: home.value })), readCurrent: vi.fn() } as unknown as ProjectWorkspace
  const navigate = vi.fn(async (path: string) => { owner.afterNavigation(path) })
  const owner = createProjectTasks(auth, workspace, navigate)
  owners.push(owner)
  owner.afterNavigation('/owner/demo/tasks')
  const wrapper = mount(ProjectTasksView, { attachTo: document.body, global: { provide: { [projectTasksKey as symbol]: owner } } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, owner, api, reads, navigate, progress, publish, commit, build, personal, context, detail, home }
}

const body = () => new DOMWrapper(document.body)
function label(element: Element) {
  const clone = element.cloneNode(true) as Element
  clone.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
  return clone.getAttribute('aria-label') ?? clone.textContent?.trim() ?? ''
}
function findButton(text: string) { return body().findAll<HTMLButtonElement>('button').find((b) => label(b.element) === text) }
function button(text: string) { const found = findButton(text); expect(found, text).toBeDefined(); return found! }
async function click(text: string) { await button(text).trigger('click'); await flushPromises() }
function control(text: string) {
  const field = body().findAll('label').find((v) => v.text().includes(text))
  expect(field, text).toBeDefined()
  return body().get<HTMLInputElement | HTMLTextAreaElement>(`#${field!.attributes('for')}`)
}
describe('Sprint Create real page controls', () => {
  it('preserves cancel focus and explicitly creates a planned Sprint while leaving the current Sprint unchanged', async () => {
    const f = await fixture()
    await click('新建 Sprint')
    expect(body().get('form[aria-label="新建 Sprint"]').element.contains(document.activeElement)).toBe(true)
    expect(body().get('form[aria-label="新建 Sprint"]').text()).toContain(milestone.title)
    await control('Sprint 标题').setValue('下个迭代')
    await click('取消创建 Sprint')
    expect(document.activeElement).toBe(button('新建 Sprint').element)
    expect(f.api.create).not.toHaveBeenCalled()
    await click('新建 Sprint')
    await control('Sprint 标题').setValue('下个迭代')
    await control('Sprint 描述').setValue('完整多行描述\n保留原文。')
    await body().get('form[aria-label="新建 Sprint"]').trigger('submit')
    await flushPromises()
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, { milestone_id: m, title: '下个迭代', description: '完整多行描述\n保留原文。' })
    expect(f.owner.state.sprint).toMatchObject({ id: createdID, state: 'planned', title: '下个迭代' })
    expect(f.detail.project?.current_sprint_id).toBe(currentID)
    expect(button('启动 Sprint').attributes('disabled')).toBeDefined()
    expect(f.api.start).not.toHaveBeenCalled()
    expect(body().find('form[aria-label="新建 Sprint"]').exists()).toBe(false)
  })
  it('hides the form on Unknown and only offers the original create Lookup before the real current reread', async () => {
    const f = await fixture(), draft = { milestone_id: m, title: '原创建' }
    f.api.create.mockImplementationOnce(async () => { f.publish('uncertain'); throw new AccountFailure('transport') })
    await click('新建 Sprint')
    await control('Sprint 标题').setValue(draft.title)
    await body().get('form[aria-label="新建 Sprint"]').trigger('submit')
    await flushPromises()
    expect(body().find('form[aria-label="新建 Sprint"]').exists()).toBe(false)
    expect(findButton('新建 Sprint')).toBeUndefined()
    expect(body().text()).toContain('Sprint 创建结果待确认')
    await click('查询原 Sprint 创建结果')
    expect(f.api.checkOriginalCreate).toHaveBeenCalledTimes(1)
    expect(f.navigate).not.toHaveBeenCalled()
    f.api.checkOriginalCreate.mockImplementationOnce(async () => ({ state: 'committed', result: f.commit(draft) }))
    await click('查询原 Sprint 创建结果')
    expect(f.api.checkOriginalCreate).toHaveBeenCalledTimes(2)
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, draft)
    expect(f.reads.sprint).toHaveBeenCalledWith(p, createdID)
    expect(f.owner.state.sprint?.id).toBe(createdID)
    expect(f.api.start).not.toHaveBeenCalled()
    expect(findButton('查询原 Sprint 创建结果')).toBeUndefined()
  })
})
