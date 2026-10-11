import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { MilestoneCreateDraft, MilestoneCreateReceipt } from '../api/work-milestone-planning'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
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
  releases: (() => void)[] = [],
  tails: Promise<unknown>[] = []
function held<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
function track<T>(promise: Promise<T>) {
  tails.push(promise)
  return promise
}
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  await Promise.allSettled(tails.splice(0))
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
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
  await flushPromises()
  return {
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

describe('Milestone creation page owner', () => {
  it('reads back an off-page created Milestone and retries only reads while preserving the current Sprint', async () => {
    const f = await fixture(true)
    f.omitCreated()
    f.owner.openMilestoneCreation()
    f.owner.milestoneCreation.title = 'New milestone'
    f.reads.milestone.mockRejectedValueOnce(new AccountFailure('transport'))
    await f.owner.submitMilestoneCreation()
    expect(f.owner.milestoneCreation.needsRead).toBe(true)
    expect(f.owner.state.milestone?.id).toBe(m)
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, { title: 'New milestone' })
    await f.owner.rereadConfirmedMilestone()
    await flushPromises()
    expect(f.reads.milestone).toHaveBeenCalledWith(p, createdID)
    expect(f.reads.milestones).toHaveBeenCalledWith(p, { limit: 50 })
    expect(f.reads.sprints).toHaveBeenCalledWith(p, createdID, { limit: 50 })
    expect(f.owner.state.milestones.some((row) => row.id === createdID)).toBe(false)
    expect(f.owner.sprintMilestoneOptions.value).toContainEqual({
      value: createdID,
      label: 'New milestone',
    })
    expect(f.owner.state.sprint).toBeNull()
    expect(f.detail.project?.current_sprint_id).toBe(currentID)
    expect(f.api.create).toHaveBeenCalledTimes(1)
    expect(f.sprintAPI.start).not.toHaveBeenCalled()
    expect(f.sprintAPI.create).not.toHaveBeenCalled()
  })
  it('keeps Unknown locked to the original lookup and never creates a Sprint automatically', async () => {
    const f = await fixture(),
      draft = { title: 'Original milestone', description: 'Original description' }
    f.api.create.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw new AccountFailure('transport')
    })
    f.owner.openMilestoneCreation()
    Object.assign(f.owner.milestoneCreation, draft)
    await f.owner.submitMilestoneCreation()
    expect(f.owner.canCreateMilestone.value).toBe(false)
    expect(f.owner.canCreateSprint.value).toBe(false)
    await f.owner.submitMilestoneCreation()
    await f.owner.lookupMilestoneCreation()
    expect(f.owner.state.milestone).toBeNull()
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      state: 'committed',
      result: f.commit(draft),
    }))
    await f.owner.lookupMilestoneCreation()
    expect(f.api.create).toHaveBeenCalledExactlyOnceWith(p, draft)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.owner.state.milestone?.description).toBe(draft.description)
    expect(f.owner.canCreateSprint.value).toBe(true)
    expect(f.sprintAPI.create).not.toHaveBeenCalled()
  })
  it('retires a held creation when the Project or Session changes instead of selecting its late result', async () => {
    const f = await fixture(),
      result = f.build({ title: 'Late' }),
      tail = held(result)
    f.api.create.mockImplementationOnce(() => tail.promise)
    f.owner.openMilestoneCreation()
    f.owner.milestoneCreation.title = 'Late'
    const creating = track(f.owner.submitMilestoneCreation())
    f.detail.project = { ...project, id: id(30), current_sprint_id: null }
    f.home.value = '/owner/other'
    f.context.value = {
      identity: f.personal.identity!,
      projectID: id(30),
      generation: 2,
      readGeneration: 2,
    }
    f.owner.afterNavigation('/owner/other/tasks')
    await flushPromises()
    tail.resolve(result)
    await creating
    expect(f.reads.milestone).not.toHaveBeenCalledWith(p, createdID)
    expect(f.owner.state.milestone?.id).not.toBe(createdID)
    expect(f.navigate).not.toHaveBeenCalled()
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.milestoneCreationFocus.value).toBeNull()
  })
})
