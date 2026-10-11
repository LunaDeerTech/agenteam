import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { SprintCreateDraft, SprintCreateReceipt } from '../api/work-sprint-lifecycle'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1), m = id(2), currentID = id(3), createdID = id(4), time = '2026-10-11T10:00:00.000000Z'
const base = { project_id: p, manual_rank: '8'.repeat(32), version: '1', created_at: time, updated_at: time }
const milestone = { ...base, id: m, title: '真实里程碑', description: '' }
const currentSprint: Sprint = { ...base, id: currentID, milestone_id: m, title: '当前迭代', description: '', state: 'current', started_at: time, completed_at: null }
const project: Project = { id: p, owner_user_id: id(8), name: 'Demo', normalized_name: 'demo', description: '', lifecycle: 'active', version: '3', current_sprint_id: currentID, created_at: time, updated_at: time, archived_at: null }
type Port = SessionController['workSprintLifecycle']
const owners: ProjectTasks[] = [], releases: (() => void)[] = [], tails: Promise<unknown>[] = []
function held<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
function track<T>(promise: Promise<T>) { tails.push(promise); return promise }
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  await Promise.allSettled(tails.splice(0))
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
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
  await flushPromises()
  return { owner, api, reads, navigate, progress, publish, commit, build, personal, context, detail, home }
}

describe('Sprint Create page owner', () => {
  it('creates planned while another Sprint is current and retries only real reads after confirmed creation', async () => {
    const f = await fixture()
    expect(f.owner.canCreateSprint.value).toBe(true)
    f.owner.openSprintCreation()
    f.owner.sprintCreation.title = '下一迭代'
    const read = f.reads.sprint.getMockImplementation()!
    f.reads.sprint.mockImplementationOnce(async () => { throw new AccountFailure('transport') })
    await f.owner.submitSprintCreation()
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, { milestone_id: m, title: '下一迭代' })
    expect(f.owner.sprintCreation.needsRead).toBe(true)
    expect(f.navigate).not.toHaveBeenCalled()
    expect(f.owner.state.sprint?.id).toBe(currentID)
    f.reads.sprint.mockImplementation(read)
    await f.owner.rereadCreatedSprint()
    await flushPromises()
    expect(f.navigate).toHaveBeenCalledExactlyOnceWith(`/owner/demo/tasks/sprints/${createdID}`)
    expect(f.reads.sprint).toHaveBeenCalledWith(p, createdID)
    expect(f.reads.milestone).toHaveBeenCalledWith(p, m)
    expect(f.reads.sprints).toHaveBeenCalledWith(p, m, { limit: 50 })
    expect(f.owner.state.sprint).toMatchObject({ id: createdID, state: 'planned' })
    expect(f.detail.project?.current_sprint_id).toBe(currentID)
    expect(f.api.create).toHaveBeenCalledTimes(1)
    expect(f.api.start).not.toHaveBeenCalled()
    expect(f.api.checkOriginalCreate).not.toHaveBeenCalled()
  })
  it('exposes only original Lookup for Unknown and restores the original Milestone before a committed reread', async () => {
    const f = await fixture(), draft = { milestone_id: m, title: '未决迭代', description: '原始描述' }
    f.api.create.mockImplementationOnce(async () => { f.publish('uncertain'); throw new AccountFailure('transport') })
    f.owner.openSprintCreation()
    Object.assign(f.owner.sprintCreation, { title: draft.title, description: draft.description })
    await f.owner.submitSprintCreation()
    expect(f.owner.canCreateSprint.value).toBe(false)
    await f.owner.submitSprintCreation()
    await f.owner.lookupSprintCreation()
    expect(f.api.checkOriginalCreate).toHaveBeenCalledTimes(1)
    expect(f.navigate).not.toHaveBeenCalled()
    f.api.checkOriginalCreate.mockImplementationOnce(async () => ({ state: 'committed', result: f.commit(draft) }))
    await f.owner.lookupSprintCreation()
    await flushPromises()
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, draft)
    expect(f.api.checkOriginalCreate).toHaveBeenCalledTimes(2)
    expect(f.owner.state.sprint?.description).toBe(draft.description)
    expect(f.api.start).not.toHaveBeenCalled()
  })
  it('never navigates or publishes a late create into another Project or Session', async () => {
    const f = await fixture(), draft = { milestone_id: m, title: 'Late' }, result = f.build(draft), tail = held(result)
    f.api.create.mockImplementationOnce(async () => tail.promise)
    f.owner.openSprintCreation()
    f.owner.sprintCreation.title = draft.title
    const creating = track(f.owner.submitSprintCreation())
    f.detail.project = { ...project, id: id(30), name: 'Other', normalized_name: 'other', current_sprint_id: null }
    f.home.value = '/owner/other'
    f.context.value = { identity: f.personal.identity!, projectID: id(30), generation: 2, readGeneration: 2 }
    f.owner.afterNavigation('/owner/other/tasks')
    await flushPromises()
    tail.resolve(result)
    await creating
    expect(f.navigate).not.toHaveBeenCalled()
    expect(f.reads.sprint).not.toHaveBeenCalledWith(p, createdID)
    expect(f.owner.sprintCreateProgress.value).toBeNull()
    expect(f.owner.state.sprint?.id).not.toBe(createdID)
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
  })
})
