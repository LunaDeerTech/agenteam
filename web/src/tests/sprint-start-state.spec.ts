import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { Sprint } from '../api/work-review'
import type { SprintStartReceipt } from '../api/work-sprint-lifecycle'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'

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
const tails: Promise<unknown>[] = []
const releases: (() => void)[] = []
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
  vi.restoreAllMocks()
})
async function fixture() {
  // Typed Session/Project responses isolate controller publication and navigation.
  // Real wire authorization, command ownership and SQL are covered independently.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    identity,
    projectID: p,
    generation: 1,
    readGeneration: 1,
  })
  const detail = shallowReactive<{ project: Project | null }>({ project: { ...originalProject } })
  const home = shallowRef('/owner/demo')
  let storedSprint: Sprint = planned
  let currentProject: Project = { ...originalProject }
  const progress = shallowRef<Port['progress']>(null)
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
    storedSprint = receipt.sprint
    currentProject = { ...receipt.project, archived_at: null }
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
    sprints: vi.fn(async () => ({ items: [storedSprint] })),
    sprint: vi.fn(async (_p: string, sid: string) => ({ ...storedSprint, id: sid })),
    agents: vi.fn(async () => ({ items: [] })),
    tasks: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
  }
  const readCurrent = vi.fn(async () => {
    detail.project = { ...currentProject }
  })
  const auth = {
    state: authState,
    personalContext: personal,
    workReview: reads,
    workMilestonePlanning: {
      progress: null,
      create: vi.fn(async () => {
        throw new Error('Unexpected Milestone Create')
      }),
      checkOriginal: vi.fn(async () => {
        throw new Error('Unexpected Milestone Lookup')
      }),
      editRejected: vi.fn(),
      abandon: vi.fn(),
    },
    workSprintLifecycle: api,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: home.value })),
    readCurrent,
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
    readCurrent,
    context,
    detail,
    personal,
    authState,
    home,
    progress,
    publish,
    commit,
    setProject: (value: Project) => {
      currentProject = value
    },
    setSprint: (value: Sprint) => {
      storedSprint = value
    },
  }
}

describe('Sprint Start page owner', () => {
  it('requires an explicit planned selection and waits for the real Project read before publishing current', async () => {
    const f = await fixture()
    expect(f.owner.state.sprint).toBeNull()
    expect(f.api.start).not.toHaveBeenCalled()
    await f.owner.selectSprint(s)
    await flushPromises()
    expect(f.owner.canStartSprint.value).toBe(true)
    f.detail.project = { ...originalProject, current_sprint_id: id(4) }
    expect(f.owner.canStartSprint.value).toBe(false)
    f.detail.project = { ...originalProject, lifecycle: 'archived', archived_at: time }
    expect(f.owner.canStartSprint.value).toBe(false)
    f.detail.project = originalProject
    f.owner.state.sprint = { ...planned, state: 'completed', started_at: time, completed_at: time }
    expect(f.owner.canStartSprint.value).toBe(false)
    f.owner.state.sprint = planned
    const read = held<void>(undefined)
    f.readCurrent.mockImplementationOnce(async () => {
      await read.promise
      f.detail.project = { ...receipt.project, archived_at: null }
    })
    f.owner.openSprintStart()
    const starting = track(f.owner.submitSprintStart())
    await flushPromises()
    expect(f.api.start).toHaveBeenCalledExactlyOnceWith(p, s, {
      expected_version: '7',
      request: {},
    })
    expect(f.readCurrent).toHaveBeenCalledTimes(1)
    expect(f.detail.project?.current_sprint_id).toBeNull()
    expect(f.owner.state.sprint?.state).toBe('planned')
    expect(f.owner.canCreate.value).toBe(false)
    read.resolve()
    await starting
    await flushPromises()
    expect(f.detail.project?.current_sprint_id).toBe(s)
    expect(f.owner.state.sprint?.state).toBe('current')
    expect(f.owner.state.sprint?.version).toBe('8')
    expect(f.owner.canCreate.value).toBe(true)
    expect(f.owner.sprintStart.message).toBe('Sprint 已启动，已确认当前 Sprint。')
  })

  it('keeps Unknown on the original Sprint and resolves only with Lookup and a fresh current read', async () => {
    const f = await fixture()
    await f.owner.selectSprint(s)
    await flushPromises()
    f.api.start.mockImplementationOnce(async () => {
      f.publish('uncertain')
      throw new AccountFailure('transport')
    })
    f.owner.openSprintStart()
    await f.owner.submitSprintStart()
    expect(f.owner.sprintProgress.value?.phase).toBe('uncertain')
    expect(f.owner.canStartSprint.value).toBe(false)
    expect(f.readCurrent).not.toHaveBeenCalled()
    const leaving = f.owner.confirmLeave('/owner/demo/tasks/sprints/' + id(4))
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.confirm()
    expect(await leaving).toBe(true)
    await f.owner.selectSprint(id(4))
    await flushPromises()
    expect(f.owner.sprintProgress.value).toBeNull()
    expect(f.owner.sprintPendingHere.value?.sprintID).toBe(s)
    await f.owner.lookupSprintStart()
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
    f.home.value = '/owner/other'
    f.detail.project = { ...originalProject, id: id(5) }
    f.context.value = { ...f.context.value!, projectID: id(5) }
    f.owner.afterNavigation('/owner/other/tasks/sprints/' + id(4))
    await flushPromises()
    expect(f.owner.sprintPendingHere.value).toBeNull()
    f.home.value = '/owner/demo'
    f.detail.project = originalProject
    f.context.value = { ...f.context.value!, projectID: p }
    f.owner.afterNavigation('/owner/demo/tasks/sprints/' + s)
    await flushPromises()
    await f.owner.lookupSprintStart()
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.readCurrent).not.toHaveBeenCalled()
    f.api.checkOriginal.mockImplementationOnce(async () => ({
      status: 'committed',
      receipt: f.commit(),
    }))
    // The old command receipt is valid even if a later lifecycle operation changed current.
    f.readCurrent.mockImplementationOnce(async () => {
      f.detail.project = { ...originalProject, version: '6', current_sprint_id: id(4) }
      f.setSprint({
        ...planned,
        state: 'completed',
        version: '9',
        started_at: time,
        completed_at: time,
      })
    })
    await f.owner.lookupSprintStart()
    expect(f.api.start).toHaveBeenCalledTimes(1)
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.detail.project?.current_sprint_id).toBe(id(4))
    expect(f.owner.state.sprint?.state).toBe('completed')
    expect(f.owner.sprintStart.message).toContain('当前 Sprint 已变化')
    expect(f.owner.canCreate.value).toBe(false)
  })

  it('requires rereading a rejected version and ignores a held receipt after identity retirement', async () => {
    const f = await fixture()
    await f.owner.selectSprint(s)
    await flushPromises()
    f.api.start.mockImplementationOnce(async () => {
      f.publish('rejected')
      throw new AccountFailure('problem', {
        type: 'urn:agenteam:problem:version',
        title: 'Conflict',
        status: 409,
        code: 'VERSION_CONFLICT',
        commit_state: 'not_started',
        detail: '',
        instance: '/api/v1/projects/' + p + '/sprints/' + s + '/start',
        request_id: id(30),
      })
    })
    f.owner.openSprintStart()
    await f.owner.submitSprintStart()
    expect(f.owner.canStartSprint.value).toBe(false)
    expect(f.owner.sprintStart.message).toContain('Sprint 已发生变化')
    f.setSprint({ ...planned, version: '8' })
    f.readCurrent.mockImplementationOnce(async () => {
      throw new AccountFailure('transport')
    })
    await f.owner.rereadSprintStart()
    expect(f.owner.sprintStart.message).toContain('读取失败')
    expect(f.api.start).toHaveBeenCalledTimes(1)
    await f.owner.rereadSprintStart()
    expect(f.owner.state.sprint?.version).toBe('8')
    expect(f.owner.canStartSprint.value).toBe(true)
    const late = held<SprintStartReceipt>(receipt)
    f.api.start.mockImplementationOnce(async () => {
      f.publish('submitting')
      return late.promise
    })
    f.owner.openSprintStart()
    const starting = track(f.owner.submitSprintStart())
    await flushPromises()
    const priorReads = f.readCurrent.mock.calls.length
    f.personal.identity = null
    f.authState.phase = 'anonymous'
    f.context.value = null
    await flushPromises()
    late.resolve(receipt)
    await starting
    expect(f.readCurrent).toHaveBeenCalledTimes(priorReads)
    expect(f.owner.state.sprint).toBeNull()
    expect(f.owner.sprintStart.message).toBe('')
  })
})
