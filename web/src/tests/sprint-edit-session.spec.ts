import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { type Fetch, type Problem } from '../api/client'
import { createWorkSprintPlanningAPI, type SprintUpdateInput } from '../api/work-sprint-planning'
import { createWorkMilestonePlanningAPI } from '../api/work-milestone-planning'
import { createWorkTaskPlanningAPI } from '../api/work-task-planning'
import { createWorkSprintLifecycleAPI } from '../api/work-sprint-lifecycle'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  m = id(2),
  at = '2026-10-11T10:00:00.000001Z'
const path = `/api/v1/projects/${p}/sprints/${m}`,
  lookupPath = `/api/v1/projects/${p}/structure-commands/lookup`
const session = (n = 4): SessionView => ({
  user: {
    id: id(3),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(n), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (code: string, state: Problem['commit_state'], status = 409) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status,
      detail: '',
      instance: path,
      code,
      request_id: id(99),
      commit_state: state,
    },
    status,
  )
function receipt(input: SprintUpdateInput) {
  return {
    command: 'work.sprint.update',
    changed: true,
    event_id: id(6),
    milestone: null,
    sprint: {
      id: m,
      project_id: p,
      milestone_id: id(20),
      state: 'planned',
      started_at: null,
      started_by: null,
      completed_at: null,
      completed_by: null,
      title: 'Initial',
      description: 'Initial description',
      ...input.request,
      manual_rank: '8'.repeat(32),
      version: String(BigInt(input.expected_version) + 1n),
      created_at: at,
      updated_at: at,
    },
  }
}
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach((r) => r())
  owners.splice(0).forEach((a) => a.leave())
  await flushPromises()
  vi.restoreAllMocks()
})
async function fixture() {
  let current = session(),
    response: (() => Promise<Response>) | null = null,
    restore: (() => Promise<Response>) | null = null
  let observation: 'committed' | 'not_observed' | 'in_progress' = 'committed'
  const fetch = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') return restore ? restore() : json(current)
    if (url === path) return response ? response() : json(receipt(JSON.parse(init.body as string)))
    if (url === lookupPath)
      return json({
        state: observation,
        result: observation === 'committed' ? receipt(JSON.parse(init.body as string)) : null,
      })
    if (init.method === 'PATCH' || init.method === 'POST')
      return problem('COMMIT_UNKNOWN', 'unknown')
    throw new Error('Unexpected request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[18] = createWorkTaskPlanningAPI(fetch)
  args[19] = {
    milestonePlanning: createWorkMilestonePlanningAPI(fetch),
    sprintPlanning: createWorkSprintPlanningAPI(fetch),
    sprintLifecycle: createWorkSprintLifecycleAPI(fetch),
  }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    writes: () => fetch.mock.calls.filter(([url]) => url === path),
    lookups: () => fetch.mock.calls.filter(([url]) => url === lookupPath),
    setResponse(v: typeof response) {
      response = v
    },
    setObservation(v: typeof observation) {
      observation = v
    },
    setSession(v: SessionView) {
      current = v
    },
    holdRestore() {
      let resolve!: (r: Response) => void
      const held = new Promise<Response>((r) => {
        resolve = r
      })
      restore = () => held
      const release = () => {
        restore = null
        resolve(json(current))
      }
      releases.push(release)
      return release
    },
  }
}
const original = {
  expected_version: '9007199254740993',
  request: { title: '  e\u0301 Sprint  ', description: '' },
}
const taskDraft = {
  sprint_id: m,
  title: 'New task',
  description: '',
  type: 'task' as const,
  priority: 'medium' as const,
  plan: '',
}
describe('Sprint edit original Session ownership', () => {
  it('preserves complete original intent through all observations without a second PATCH or colliding create/start', async () => {
    const f = await fixture()
    f.setResponse(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    const draft = { expected_version: original.expected_version, request: { ...original.request } }
    const saving = f.auth.workSprintEditing.update(p, m, draft)
    draft.request.title = 'Later'
    draft.expected_version = '2'
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    expect(f.auth.workMilestonePlanning.progress).toBeNull()
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      projectID: p,
      sprintID: m,
      phase: 'uncertain',
    })
    await expect(
      f.auth.workMilestonePlanning.create(p, { title: 'Blocked' }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.workSprintLifecycle.start(p, id(8), { expected_version: '1', request: {} }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.workMilestoneEditing.update(p, id(20), original)).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.workSprintLifecycle.create(p, { milestone_id: id(20), title: 'Blocked' }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.workReview.startCreate(p, taskDraft)).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.auth.workSprintLifecycle.progress).toBeNull()
    await expect(f.auth.workSprintEditing.update(p, m, original)).rejects.toMatchObject({
      kind: 'busy',
    })
    for (const value of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setObservation(value)
      await f.auth.workSprintEditing.checkOriginal()
      expect(f.auth.workSprintEditing.progress?.phase).toBe(
        value === 'committed' ? 'confirmed' : 'uncertain',
      )
    }
    expect(f.writes()).toHaveLength(1)
    const init = f.writes()[0]![1],
      key = (init.headers as Record<string, string>)['Idempotency-Key']
    expect(JSON.parse(init.body as string)).toEqual(original)
    for (const [, lookup] of f.lookups()) {
      expect(JSON.parse(lookup.body as string)).toEqual({
        command: 'work.sprint.update',
        target_id: m,
        ...original,
      })
      expect(lookup.headers).toMatchObject({ 'Idempotency-Key': key })
    }
    expect(JSON.stringify(f.auth.workSprintEditing.progress)).not.toContain(key)
  })
  it('refuses a new Sprint update while another planning or Task original intent remains unresolved', async () => {
    const starters = [
      (a: SessionController) =>
        a.workMilestoneEditing.update(p, id(20), {
          expected_version: '1',
          request: { title: 'Other' },
        }),
      (a: SessionController) => a.workMilestonePlanning.create(p, { title: 'Other' }),
      (a: SessionController) =>
        a.workSprintLifecycle.create(p, { milestone_id: id(20), title: 'Other' }),
      (a: SessionController) =>
        a.workSprintLifecycle.start(p, id(8), { expected_version: '1', request: {} }),
      (a: SessionController) => a.workReview.startCreate(p, taskDraft),
    ]
    for (const start of starters) {
      const f = await fixture()
      await expect(start(f.auth)).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
      await expect(f.auth.workSprintEditing.update(p, m, original)).rejects.toMatchObject({
        kind: 'busy',
      })
      expect(f.writes()).toHaveLength(0)
      expect(f.auth.workSprintEditing.progress).toBeNull()
    }
  })
  it('keeps Unknown during same-identity checking and clears it on a real Session change', async () => {
    const f = await fixture()
    f.setResponse(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    await expect(f.auth.workSprintEditing.update(p, m, original)).rejects.toBeDefined()
    const release = f.holdRestore(),
      checking = f.auth.restore()
    await flushPromises()
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    release()
    await checking
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    f.setObservation('in_progress')
    await f.auth.workSprintEditing.checkOriginal()
    f.setSession(session(5))
    await f.auth.restore()
    expect(f.auth.workSprintEditing.progress).toBeNull()
    await expect(f.auth.workSprintEditing.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.writes()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(1)
  })
  it('retains a definite version rejection until explicit release, then accepts a separately confirmed version', async () => {
    const f = await fixture()
    f.setResponse(async () => problem('VERSION_CONFLICT', 'not_committed'))
    await expect(f.auth.workSprintEditing.update(p, m, original)).rejects.toBeDefined()
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'rejected',
      error: 'VERSION_CONFLICT',
    })
    await expect(f.auth.workSprintEditing.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    f.auth.workSprintEditing.editRejected()
    f.setResponse(null)
    await f.auth.workSprintEditing.update(p, m, {
      expected_version: '9007199254740998',
      request: { description: '' },
    })
    expect(f.auth.workSprintEditing.progress?.result?.sprint.version).toBe('9007199254740999')
    expect(f.writes()).toHaveLength(2)
    expect(f.lookups()).toHaveLength(0)
  })
  it('clears new command and confirmed material on an actual Owner denial', async () => {
    const f = await fixture()
    f.setResponse(async () => problem('NOT_FOUND', 'not_started', 404))
    await expect(f.auth.workSprintEditing.update(p, m, original)).rejects.toMatchObject({
      problem: { status: 404 },
    })
    expect(f.auth.workSprintEditing.progress).toBeNull()
    await expect(f.auth.workSprintEditing.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.lookups()).toHaveLength(0)
  })
})
