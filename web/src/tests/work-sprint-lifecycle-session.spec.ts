import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import type { Fetch, Problem } from '../api/client'
import { createWorkSprintLifecycleAPI, type SprintStartInput, type SprintCreateInput } from '../api/work-sprint-lifecycle'
import { createSessionController, type SessionController } from '../composables/useSession'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(10),
  s = id(11),
  at = '2026-10-11T10:00:00.000001Z'
const startPath = `/api/v1/projects/${p}/sprints/${s}/start`,
  lookupPath = `/api/v1/projects/${p}/sprint-lifecycle-commands/lookup`
const session = (n = 2): SessionView => ({
  user: {
    id: id(1),
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
function receipt(value: SprintStartInput) {
  return {
    sprint: {
      id: s,
      project_id: p,
      milestone_id: id(12),
      title: 'Sprint',
      description: '',
      manual_rank: '8'.repeat(32),
      version: String(BigInt(value.expected_version) + 1n),
      created_at: at,
      updated_at: at,
      state: 'current',
      started_at: at,
      started_by: { kind: 'human', user_id: id(1) },
      completed_at: null,
      completed_by: null,
    },
    project: {
      id: p,
      owner_user_id: id(1),
      name: 'Project',
      normalized_name: 'project',
      description: '',
      lifecycle: 'active',
      version: '2',
      current_sprint_id: s,
      created_at: at,
      updated_at: at,
    },
    event_id: id(13),
  }
}
const createPath = `/api/v1/projects/${p}/sprints`, createLookupPath = `/api/v1/projects/${p}/structure-commands/lookup`
function created(input: SprintCreateInput) {
  return { command: 'work.sprint.create', changed: true, milestone: null, event_id: id(14), sprint: {
    id: input.request.sprint_id, project_id: p, milestone_id: input.request.milestone_id,
    title: input.request.title, description: input.request.description ?? '', manual_rank: '8'.repeat(32), version: '1', created_at: at, updated_at: at,
    state: 'planned', started_at: null, started_by: null, completed_at: null, completed_by: null,
  } }
}
const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (code: string, commit_state: Problem['commit_state']) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status: 409,
      detail: '',
      instance: startPath,
      code,
      request_id: id(99),
      commit_state,
    },
    409,
  )
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((a) => a.leave())
  releases.splice(0).forEach((release) => release())
  await flushPromises()
  vi.useRealTimers()
})
async function fixture() {
  let current = session(),
    start: Fetch | null = null,
    create: Fetch | null = null
  let status: 'committed' | 'in_progress' | 'not_observed' = 'committed'
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return json(current)
    if (path === createPath) return create ? create(path, init) : json(created(JSON.parse(init.body as string)))
    if (path === createLookupPath) return json({ state: status, result: status === 'committed' ? created(JSON.parse(init.body as string)) : null })
    if (path === startPath)
      return start ? start(path, init) : json(receipt(JSON.parse(init.body as string)))
    if (path === lookupPath)
      return json({
        status,
        receipt: status === 'committed' ? receipt(JSON.parse(init.body as string)) : null,
      })
    throw new Error('Unexpected Sprint request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { sprintLifecycle: createWorkSprintLifecycleAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    creates: () => fetch.mock.calls.filter(([path]) => path === createPath),
    createLookups: () => fetch.mock.calls.filter(([path]) => path === createLookupPath),
    setCreate(v: Fetch | null) { create = v },
    setStart(v: Fetch | null) {
      start = v
    },
    setStatus(v: typeof status) {
      status = v
    },
    setSession(v: SessionView) {
      current = v
    },
    starts: () => fetch.mock.calls.filter(([path]) => path === startPath),
    lookups: () => fetch.mock.calls.filter(([path]) => path === lookupPath),
  }
}

describe('Sprint Start original Session ownership', () => {
  it('keeps the original Sprint version, empty request and key through Unknown and only returns the original Lookup result', async () => {
    const f = await fixture()
    f.setStart(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    const input = { expected_version: '9007199254740993', request: {} },
      expected = structuredClone(input)
    const starting = f.auth.workSprintLifecycle.start(p, s, input)
    input.expected_version = '2'
    await expect(starting).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    expect(f.auth.workSprintLifecycle.progress).toMatchObject({
      projectID: p,
      sprintID: s,
      phase: 'uncertain',
      contextValid: true,
    })
    const first = f.starts()[0]![1]
    expect(JSON.parse(first.body as string)).toEqual(expected)
    const key = (first.headers as Record<string, string>)['Idempotency-Key']
    expect(JSON.stringify(f.auth.workSprintLifecycle.progress)).not.toContain(key)
    await expect(f.auth.workSprintLifecycle.start(id(20), id(21), input)).rejects.toMatchObject({
      kind: 'busy',
    })
    await f.auth.restore()
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setStatus(status)
      expect(await f.auth.workSprintLifecycle.checkOriginal()).toMatchObject({ status })
      const lookup = f.lookups().at(-1)![1]
      expect(JSON.parse(lookup.body as string)).toEqual({
        command: 'work.sprint.start',
        target_id: s,
        ...expected,
      })
      expect(lookup.headers).toMatchObject({ 'Idempotency-Key': key })
    }
    expect(f.auth.workSprintLifecycle.progress).toMatchObject({
      phase: 'confirmed',
      contextValid: true,
      receipt: receipt(expected),
    })
    expect(f.starts()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    expect(f.auth.workReview.progress).toBeNull()
    expect(
      f.fetch.mock.calls.every(([path]) =>
        [startPath, lookupPath, '/api/v1/session'].includes(path),
      ),
    ).toBe(true)
  })

  it('requires explicit abandonment of a proven rejection and validates the current Owner before publishing a receipt', async () => {
    const f = await fixture(),
      input = { expected_version: '1', request: {} }
    f.setStart(async () => problem('VERSION_CONFLICT', 'not_started'))
    await expect(f.auth.workSprintLifecycle.start(p, s, input)).rejects.toMatchObject({
      kind: 'problem',
    })
    expect(f.auth.workSprintLifecycle.progress?.phase).toBe('rejected')
    await expect(f.auth.workSprintLifecycle.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    f.auth.workSprintLifecycle.editRejected()
    f.setStart(async () => {
      const other = receipt(input)
      other.project.owner_user_id = id(30)
      other.sprint.started_by.user_id = id(30)
      return json(other)
    })
    await expect(f.auth.workSprintLifecycle.start(p, s, input)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(f.auth.workSprintLifecycle.progress?.phase).toBe('uncertain')
    const keys = f
      .starts()
      .map(([, init]) => (init.headers as Record<string, string>)['Idempotency-Key'])
    expect(keys[0]).not.toBe(keys[1])
    expect(f.lookups()).toHaveLength(0)
  })

  it('holds the actual Cookie request after its visible timeout and isolates late completion from a new Session', async () => {
    const f = await fixture()
    vi.useFakeTimers()
    let release!: () => void
    f.setStart(
      async (_path, init) =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(receipt(JSON.parse(init.body as string))))
          releases.push(release)
        }),
    )
    const pending = f.auth.workSprintLifecycle.start(p, s, { expected_version: '1', request: {} })
    const rejected = expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    await vi.advanceTimersByTimeAsync(30_000)
    await rejected
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.workSprintLifecycle.progress?.phase).toBe('uncertain')
    await expect(f.auth.workSprintLifecycle.checkOriginal()).rejects.toMatchObject({ kind: 'busy' })
    f.auth.leave()
    expect(f.auth.workSprintLifecycle.progress).toBeNull()
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.workSprintLifecycle.progress).toBeNull()
    vi.useRealTimers()
    f.setSession(session(3))
    await f.auth.restore()
    await expect(f.auth.workSprintLifecycle.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.starts()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(0)
  })
})

describe('Sprint Create original Session ownership', () => {
  it('mints one original Sprint identity, preserves full presence and only looks up an uncertain create', async () => {
    const f = await fixture()
    f.setCreate(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    const draft = { milestone_id: id(12), title: 'Original planned Sprint' }
    const creating = f.auth.workSprintLifecycle.create(p, draft)
    draft.title = 'Later edit'
    await expect(creating).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const first = f.creates()[0]![1], body = JSON.parse(first.body as string)
    expect(body).toEqual({ request: { sprint_id: expect.stringMatching(/^[0-9a-f-]{14}7[0-9a-f-]{21}$/), milestone_id: id(12), title: 'Original planned Sprint' } })
    const key = (first.headers as Record<string, string>)['Idempotency-Key']
    expect(f.auth.workSprintLifecycle.createProgress).toMatchObject({ projectID: p, milestoneID: id(12), sprintID: body.request.sprint_id, phase: 'uncertain', contextValid: true })
    expect(JSON.stringify(f.auth.workSprintLifecycle.createProgress)).not.toContain(key)
    await expect(f.auth.workSprintLifecycle.create(p, draft)).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.workSprintLifecycle.start(p, s, { expected_version: '1', request: {} })).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.restore()
    for (const state of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setStatus(state)
      expect(await f.auth.workSprintLifecycle.checkOriginalCreate()).toMatchObject({ state })
      const lookup = f.createLookups().at(-1)![1]
      expect(JSON.parse(lookup.body as string)).toEqual({ command: 'work.sprint.create', ...body })
      expect(lookup.headers).toMatchObject({ 'Idempotency-Key': key })
    }
    expect(f.auth.workSprintLifecycle.createProgress).toMatchObject({ phase: 'confirmed', result: created(body) })
    expect(f.creates()).toHaveLength(1)
    expect(f.createLookups()).toHaveLength(3)
    expect(f.starts()).toHaveLength(0)
  })
  it('keeps Start and Create intents separate and requires explicit release of a proven create rejection', async () => {
    const f = await fixture(), draft = { milestone_id: id(12), title: 'Planned', description: '' }
    f.setCreate(async () => problem('INVALID_STATE', 'not_started'))
    await expect(f.auth.workSprintLifecycle.create(p, draft)).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.workSprintLifecycle.createProgress?.phase).toBe('rejected')
    await expect(f.auth.workSprintLifecycle.checkOriginalCreate()).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(f.auth.workSprintLifecycle.start(p, s, { expected_version: '1', request: {} })).rejects.toMatchObject({ kind: 'busy' })
    f.auth.workSprintLifecycle.editRejectedCreate()
    f.setCreate(null)
    expect(await f.auth.workSprintLifecycle.create(p, draft)).toMatchObject({ command: 'work.sprint.create', sprint: { description: '', state: 'planned' } })
    const [one, two] = f.creates().map(([, init]) => JSON.parse(init.body as string))
    expect(one.request.sprint_id).not.toBe(two.request.sprint_id)
    expect(one.request.description).toBe('')
    expect(f.createLookups()).toHaveLength(0)
    f.setStart(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    await expect(f.auth.workSprintLifecycle.start(p, s, { expected_version: '1', request: {} })).rejects.toMatchObject({ kind: 'problem' })
    await expect(f.auth.workSprintLifecycle.create(p, draft)).rejects.toMatchObject({ kind: 'busy' })
    expect(f.creates()).toHaveLength(2)
  })
  it('waits for the original timed-out create tail and never publishes it into a later identity', async () => {
    const f = await fixture()
    vi.useFakeTimers()
    let release!: () => void
    f.setCreate(async (_path, init) => new Promise<Response>((resolve) => {
      release = () => resolve(json(created(JSON.parse(init.body as string))))
      releases.push(release)
    }))
    const pending = f.auth.workSprintLifecycle.create(p, { milestone_id: id(12), title: 'Planned' })
    const rejected = expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    await vi.advanceTimersByTimeAsync(30_000)
    await rejected
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.workSprintLifecycle.createProgress?.phase).toBe('uncertain')
    await expect(f.auth.workSprintLifecycle.checkOriginalCreate()).rejects.toMatchObject({ kind: 'busy' })
    f.auth.leave()
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.workSprintLifecycle.createProgress).toBeNull()
    vi.useRealTimers()
    f.setSession(session(3))
    await f.auth.restore()
    await expect(f.auth.workSprintLifecycle.checkOriginalCreate()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.creates()).toHaveLength(1)
    expect(f.createLookups()).toHaveLength(0)
  })
})
