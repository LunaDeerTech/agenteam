import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { AccountFailure, type Fetch, type Problem } from '../api/client'
import {
  createWorkPlanningAPI,
  captureWorkPlanningCommand,
  type WorkPlanningCommand,
} from '../api/work-planning'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const project = id(10),
  target = id(11),
  at = '2026-10-09T10:00:00.000000Z'
const session = (): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: '',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
const milestone = {
  id: target,
  project_id: project,
  title: '原文',
  description: '',
  manual_rank: '8'.repeat(32),
  version: '1',
  created_at: at,
  updated_at: at,
}
const task = {
  ...milestone,
  milestone_id: id(12),
  sprint_id: id(13),
  type: 'task',
  priority: 'medium',
  state: 'backlog',
  assignee_agent_id: null,
  plan: '原计划',
  version: '2',
}
const blocker = {
  id: id(14),
  project_id: project,
  task_id: target,
  type: 'waiting_for_human',
  description: '',
  metadata: {},
  created_at: at,
  created_by: { type: 'human', user_id: id(1), source: 'task_domain' },
  resolved_at: null,
  resolved_by: null,
  resolution_comment: null,
}
const commands: WorkPlanningCommand[] = [
  {
    domain: 'structure',
    projectID: project,
    command: 'work.milestone.create',
    request: { milestone_id: target, title: '原文' },
  },
  {
    domain: 'task',
    projectID: project,
    command: 'work.task.update',
    targetID: target,
    expected_version: '1',
    request: { plan: '原计划' },
  },
  {
    domain: 'blocker',
    projectID: project,
    command: 'work.task.blocker.add',
    taskID: target,
    expected_version: '1',
    request: { blocker_id: id(14), type: 'waiting_for_human', description: '', metadata: {} },
  },
]
const wire = (c: WorkPlanningCommand) =>
  c.domain === 'structure'
    ? { command: c.command, changed: true, milestone, sprint: null, event_id: id(20) }
    : c.domain === 'task'
      ? { task, changed: true, task_event_id: id(21), event_ids: [id(22)] }
      : { task, blocker, task_event_id: id(21), event_ids: [id(22)] }
const response = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
    status,
    headers: {
      'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
const problem = (
  code: string,
  status = 409,
  commit_state: Problem['commit_state'] = 'not_committed',
) =>
  response(
    {
      type: 'urn:agenteam:problem:test',
      title: '未完成',
      detail: '',
      instance: '/api/v1/projects',
      request_id: id(99),
      code,
      status,
      commit_state,
    },
    status,
  )
const controllers: SessionController[] = [],
  releases: (() => void)[] = []
function deferred<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
async function fixture() {
  let view = session(),
    handler: Fetch = async () => {
      throw new Error('unexpected Work request')
    },
    sessionHandler: (() => Promise<Response>) | undefined
  const sent: { path: string; init: RequestInit }[] = []
  const fetcher: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return sessionHandler ? sessionHandler() : response(view)
    if (path === '/api/v1/sessions/logout') return new Response(null, { status: 204 })
    if (path === '/api/v1/auth/bootstrap')
      return response({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    sent.push({ path, init })
    return handler(path, init)
  }
  const auth = createSessionController(
    createAccountAPI(fetcher),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetcher),
    undefined,
    undefined,
    createWorkPlanningAPI(fetcher),
  )
  controllers.push(auth)
  await auth.restore()
  return {
    auth,
    sent,
    setHandler(value: Fetch) {
      handler = value
    },
    setSession(value: SessionView) {
      view = value
    },
    setSessionHandler(value?: () => Promise<Response>) {
      sessionHandler = value
    },
  }
}
afterEach(async () => {
  for (const c of controllers.splice(0)) c.leave()
  for (const r of releases.splice(0)) r()
  await flushPromises()
  vi.useRealTimers()
  vi.restoreAllMocks()
})
describe('Work Session Cookie ownership and original recovery', () => {
  it.each(commands)(
    'recovers the original $domain intent without replacing it with current state',
    async (original) => {
      const f = await fixture(),
        command = structuredClone(original)
      f.setHandler(async () => {
        throw new Error('lost response')
      })
      const pending = f.auth.workPlanning.start(command)
      ;(command.request as { description?: string }).description = 'later unsent edit'
      await expect(pending).rejects.toMatchObject({ kind: 'transport' })
      expect(f.auth.state.user?.role).toBe('user')
      expect(f.auth.workPlanning.progress).toMatchObject({
        phase: 'uncertain',
        canLookup: true,
        canReplay: true,
      })
      const first = f.sent[0]!,
        mutationCount = () =>
          f.sent.filter((v) => v.init.method !== 'GET' && !v.path.endsWith('/lookup')).length
      const safe = JSON.stringify(f.auth.workPlanning.progress)
      expect(safe).not.toContain('original-key')
      expect(safe).not.toContain('csrf')
      expect(safe).not.toContain('原计划')
      expect(safe).not.toContain('later unsent edit')
      f.setHandler(async () =>
        response(
          original.domain === 'structure'
            ? { state: 'committed', result: wire(original) }
            : { status: 'committed', receipt: wire(original) },
        ),
      )
      const observed = await f.auth.workPlanning.checkOriginal()
      expect(observed.domain).toBe(original.domain)
      expect(f.auth.workPlanning.progress).toMatchObject({
        phase: 'confirmed',
        observation: 'committed',
      })
      const lookup = f.sent[1]!
      expect(lookup.init.headers).toMatchObject({
        'Idempotency-Key': (first.init.headers as Record<string, string>)['Idempotency-Key'],
        'X-CSRF-Token': 'S'.repeat(43),
      })
      const expected = {
        command: original.command,
        ...('targetID' in original ? { target_id: original.targetID } : {}),
        ...JSON.parse(first.init.body as string),
      }
      expect(JSON.parse(lookup.init.body as string)).toEqual(expected)
      f.setHandler(async () => response({ ...milestone, title: '当前新标题', version: '9' }))
      expect((await f.auth.workPlanning.getMilestone(project, target)).title).toBe('当前新标题')
      expect(f.auth.workPlanning.progress?.receipt?.domain).toBe(original.domain)
      expect(mutationCount()).toBe(1)
      f.setHandler(async () => response(wire(original)))
      await f.auth.workPlanning.retryOriginal()
      const replay = f.sent.at(-1)!
      expect(replay.path).toBe(first.path)
      expect(replay.init.body).toBe(first.init.body)
      expect(replay.init.headers).toEqual(first.init.headers)
      expect(mutationCount()).toBe(2)
    },
  )
  it('keeps uncertainty after a later version rejection, current GET, not_observed and in_progress', async () => {
    const f = await fixture(),
      c = commands[1]!
    f.setHandler(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await expect(f.auth.workPlanning.start(c)).rejects.toMatchObject({ kind: 'problem' })
    f.setHandler(async () => problem('VERSION_CONFLICT'))
    await expect(f.auth.workPlanning.retryOriginal()).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.workPlanning.progress?.phase).toBe('uncertain')
    f.setHandler(async () => response(task))
    await f.auth.workPlanning.getTask(project, target)
    expect(f.auth.workPlanning.progress?.phase).toBe('uncertain')
    f.setHandler(async () => response({ status: 'not_observed', receipt: null }))
    await f.auth.workPlanning.checkOriginal()
    expect(f.auth.workPlanning.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
      canReplay: true,
    })
    f.setHandler(async () => response({ status: 'in_progress', receipt: null }))
    await f.auth.workPlanning.checkOriginal()
    expect(f.auth.workPlanning.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'in_progress',
      canLookup: true,
      canReplay: false,
    })
    const count = f.sent.length
    await expect(f.auth.workPlanning.retryOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.sent).toHaveLength(count)
  })
  it('only treats an explicit first rejection as rejected and never rotates a reused key', async () => {
    const f = await fixture()
    f.setHandler(async () => problem('RESOURCE_BUSY'))
    await expect(f.auth.workPlanning.start(commands[1]!)).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.workPlanning.progress).toMatchObject({ phase: 'rejected', canReplay: false })
    f.auth.workPlanning.abandon()
    f.setHandler(async () => problem('IDEMPOTENCY_KEY_REUSED'))
    await expect(f.auth.workPlanning.start(commands[1]!)).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.workPlanning.progress).toMatchObject({
      phase: 'uncertain',
      canLookup: false,
      canReplay: false,
    })
    const count = f.sent.length
    await expect(f.auth.workPlanning.start(commands[1]!)).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.workPlanning.retryOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.sent).toHaveLength(count)
  })
  it.each(['same', 'session', 'csrf', 'user'] as const)(
    'retains same identity checking and destroys actual %s changes',
    async (kind) => {
      const f = await fixture()
      f.setHandler(async () => {
        throw new Error('lost')
      })
      await expect(f.auth.workPlanning.start(commands[1]!)).rejects.toMatchObject({
        kind: 'transport',
      })
      const pending = deferred(response(session()))
      f.setSessionHandler(() => pending.promise)
      const restoring = f.auth.restore()
      await flushPromises()
      expect(f.auth.state.phase).toBe('checking')
      expect(f.auth.workPlanning.progress).toMatchObject({
        phase: 'uncertain',
        contextValid: false,
        canLookup: false,
      })
      const next = session()
      if (kind === 'session') next.session.id = id(98)
      if (kind === 'csrf') next.csrf_token = 'T'.repeat(43)
      if (kind === 'user') next.user.id = id(97)
      pending.resolve(response(next))
      await restoring
      if (kind === 'same') {
        expect(f.auth.workPlanning.progress).toMatchObject({
          phase: 'uncertain',
          contextValid: true,
          canLookup: true,
        })
        f.setHandler(async () => response({ status: 'not_observed', receipt: null }))
        await f.auth.workPlanning.checkOriginal()
      } else {
        expect(f.auth.workPlanning.progress).toBeNull()
        await expect(f.auth.workPlanning.checkOriginal()).rejects.toMatchObject({
          kind: 'invalid-input',
        })
      }
    },
  )
  it('does not release the unique owner when the 30s visible wait or local tracking ends', async () => {
    vi.useFakeTimers()
    const f = await fixture(),
      held = deferred(response(wire(commands[1]!)))
    f.setHandler(() => held.promise)
    const operation = f.auth.workPlanning.start(commands[1]!).catch((e) => e)
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(await operation).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.workPlanning.progress?.phase).toBe('uncertain')
    f.auth.workPlanning.abandon()
    await expect(f.auth.workPlanning.getTask(project, target)).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(f.auth.projects.get(project)).rejects.toMatchObject({ kind: 'busy' })
    expect(f.auth.state.busy).toBe(true)
    held.resolve(response(wire(commands[1]!)))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.workPlanning.progress).toBeNull()
    f.setHandler(async () => response(task))
    await f.auth.workPlanning.getTask(project, target)
  })
  it('joins a real ReadableStream cancel promise before allowing another Cookie operation', async () => {
    vi.useFakeTimers()
    const f = await fixture(),
      cancel = deferred<void>(undefined)
    let cancellations = 0
    f.setHandler(
      async () =>
        new Response(
          new ReadableStream<Uint8Array>({
            cancel() {
              cancellations++
              return cancel.promise
            },
          }),
          { headers: { 'Content-Type': 'text/plain' } },
        ),
    )
    const operation = f.auth.workPlanning.start(commands[1]!).catch((e) => e)
    await flushPromises()
    expect(cancellations).toBe(1)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(await operation).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.workPlanning.checkOriginal()).rejects.toMatchObject({ kind: 'busy' })
    cancel.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.workPlanning.progress?.phase).toBe('uncertain')
    expect(cancellations).toBe(1)
  })
  it('isolates a retired read and its late 401 from current identity and the next read', async () => {
    const f = await fixture(),
      held = deferred(problem('UNAUTHENTICATED', 401, 'not_started'))
    f.setHandler(() => held.promise)
    const first = f.auth.workPlanning.getTask(project, target).catch((e) => e)
    await flushPromises()
    f.auth.workPlanning.abandonRead()
    expect(await first).toMatchObject({ kind: 'cancelled' })
    await expect(f.auth.workPlanning.getTask(project, target)).rejects.toMatchObject({
      kind: 'busy',
    })
    held.resolve(problem('UNAUTHENTICATED', 401, 'not_started'))
    await flushPromises()
    expect(f.auth.state.phase).toBe('authenticated')
    f.setHandler(async () => response(task))
    expect((await f.auth.workPlanning.getTask(project, target)).id).toBe(target)
  })
  it('clears private recovery on current Session loss but not an ordinary Owner denial', async () => {
    const f = await fixture()
    f.setHandler(async () => {
      throw new Error('lost')
    })
    await expect(f.auth.workPlanning.start(commands[1]!)).rejects.toMatchObject({
      kind: 'transport',
    })
    f.setHandler(async () => problem('FORBIDDEN', 403, 'not_started'))
    await expect(f.auth.workPlanning.checkOriginal()).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.workPlanning.progress?.phase).toBe('uncertain')
    f.setHandler(async () => problem('CSRF_FAILED', 403, 'not_started'))
    await expect(f.auth.workPlanning.checkOriginal()).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.auth.workPlanning.progress).toBeNull()
  })
  it('never sends invalid originals or exposes raw input through progress', async () => {
    const f = await fixture()
    expect(() =>
      captureWorkPlanningCommand({
        ...commands[1]!,
        request: { plan: null },
      } as unknown as WorkPlanningCommand),
    ).toThrow(AccountFailure)
    await expect(
      f.auth.workPlanning.start({ ...commands[1]!, expected_version: '0' } as WorkPlanningCommand),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.sent).toHaveLength(0)
    expect(f.auth.workPlanning.progress).toBeNull()
  })
})

describe('Work confirmed history survives a failed explicit replay', () => {
  it.each(['rejection', 'disconnect'] as const)(
    'preserves previously confirmed receipt after %s',
    async (kind) => {
      const f = await fixture(),
        original = commands[1]!
      f.setHandler(async () => response(wire(original)))
      await f.auth.workPlanning.start(original)
      const confirmed = f.auth.workPlanning.progress!.receipt
      f.setHandler(async () => {
        if (kind === 'disconnect') throw new Error('response lost')
        return problem('PROJECT_NOT_ACTIVE', 409, 'not_started')
      })
      await expect(f.auth.workPlanning.retryOriginal()).rejects.toBeInstanceOf(AccountFailure)
      expect(f.auth.workPlanning.progress).toMatchObject({
        phase: 'confirmed',
        receipt: confirmed,
        contextValid: true,
      })
      expect(f.auth.workPlanning.progress?.failure).toBeInstanceOf(AccountFailure)
    },
  )
})
