import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { type Fetch, type Problem } from '../api/client'
import { createWorkReviewAPI, type ReviewInput, type ReviewReceipt } from '../api/work-review'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import { createSessionController, type SessionController } from '../composables/useSession'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const instant = '2026-10-11T10:00:00.000000Z'
const projectID = id(10),
  taskID = id(20),
  transferPath = `/api/v1/projects/${projectID}/tasks/${taskID}/transfer`,
  lookupPath = `/api/v1/projects/${projectID}/task-transition-commands/lookup`
const session = (number = 2): SessionView => ({
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
  session: {
    id: id(number),
    issued_at: instant,
    absolute_expires_at: instant,
    idle_expires_at: instant,
  },
  csrf_token: 'S'.repeat(43),
})
const reviewInput = () => ({
  expected_version: '4',
  request: {
    target_state: 'in_review' as const,
    comment: '原始评审说明\n保留原字节。',
    assignee_agent_id: id(30),
  },
})
const receipt = (input: ReviewInput = reviewInput()): ReviewReceipt => ({
  task: {
    id: taskID,
    project_id: projectID,
    milestone_id: id(11),
    sprint_id: id(12),
    title: 'Original Task',
    description: 'Current task description',
    plan: 'Current task plan',
    type: 'task',
    priority: 'medium',
    state: input.request.target_state,
    assignee_agent_id: input.request.assignee_agent_id ?? id(30),
    manual_rank: '8' + '0'.repeat(31),
    version: String(BigInt(input.expected_version) + 1n),
    created_at: instant,
    updated_at: instant,
  },
  task_event_ids: [id(40), id(41)],
  event_ids: [id(42)],
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (code: string, status: number, commit: Problem['commit_state'] = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      instance: transferPath,
      code,
      status,
      request_id: id(99),
      commit_state: commit,
    },
    status,
  )
const owners: SessionController[] = [],
  releases: (() => void)[] = []
function deferred<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
afterEach(async () => {
  owners.splice(0).forEach((auth) => auth.leave())
  releases.splice(0).forEach((release) => release())
  await flushPromises()
  vi.useRealTimers()
  vi.restoreAllMocks()
})
async function fixture() {
  let current: SessionView | null = session()
  let intercept: Fetch | undefined
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return current ? json(current) : problem('UNAUTHENTICATED', 401)
    if (path === '/api/v1/sessions/logout') {
      current = null
      return new Response(null, { status: 204 })
    }
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path === transferPath) return json(receipt(JSON.parse(init.body as string) as ReviewInput))
    if (path === lookupPath) {
      const input = JSON.parse(init.body as string) as ReviewInput
      return json({ status: 'committed', receipt: receipt(input) })
    }
    throw new Error('Unexpected Session review endpoint')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[16] = createWorkReviewAPI(fetch)
  args[17] = createAgentDirectoryAPI(fetch)
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  expect(auth.state.phase).toBe('authenticated')
  return {
    auth,
    fetch,
    normal,
    intercept(value: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      current = value
    },
    transfers: () => fetch.mock.calls.filter(([path]) => path === transferPath),
    lookups: () => fetch.mock.calls.filter(([path]) => path === lookupPath),
  }
}

describe('Work review original Session ownership', () => {
  it('keeps one immutable intent through Unknown and all explicit lookup observations', async () => {
    const f = await fixture()
    let observation: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
    f.intercept(async (path, init) => {
      if (path === transferPath) return problem('COMMIT_UNKNOWN', 503, 'unknown')
      if (path === lookupPath)
        return json({
          status: observation,
          receipt: observation === 'committed' ? receipt() : null,
        })
      return f.normal(path, init)
    })
    const input = reviewInput(),
      expected = JSON.stringify(input)
    await expect(f.auth.workReview.startTransfer(projectID, taskID, input)).rejects.toMatchObject({
      problem: { code: 'COMMIT_UNKNOWN' },
    })
    expect(f.auth.workReview.progress).toMatchObject({
      phase: 'uncertain',
      receipt: null,
      contextValid: true,
    })
    input.expected_version = '8'
    input.request.comment = 'Changed caller draft'
    input.request.assignee_agent_id = id(31)
    expect(f.lookups()).toHaveLength(0)
    await expect(f.auth.workReview.startTransfer(projectID, taskID, input)).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(() => f.auth.workReview.editRejected()).toThrow()
    // A same-identity Session check keeps the original server command intact.
    await f.auth.restore()
    for (const status of ['not_observed', 'in_progress'] as const) {
      observation = status
      await f.auth.workReview.checkOriginal()
      expect(f.auth.workReview.progress).toMatchObject({
        phase: 'uncertain',
        observation: status,
        receipt: null,
      })
      await expect(f.auth.workReview.startTransfer(projectID, taskID, input)).rejects.toMatchObject(
        { kind: 'busy' },
      )
    }
    observation = 'committed'
    await f.auth.workReview.checkOriginal()
    expect(f.auth.workReview.progress).toMatchObject({
      phase: 'confirmed',
      observation: 'committed',
      receipt: receipt(),
    })
    const original = f.transfers()[0]![1],
      headers = new Headers(original.headers)
    expect(original.body).toBe(expected)
    expect(headers.get('Idempotency-Key')).toBeTruthy()
    expect(headers.get('X-CSRF-Token')).toBe(session().csrf_token)
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    for (const [, request] of f.lookups()) {
      expect(JSON.parse(request.body as string)).toEqual({
        command: 'work.task.transfer',
        target_id: taskID,
        ...JSON.parse(expected),
      })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        headers.get('Idempotency-Key'),
      )
      expect(new Headers(request.headers).get('X-CSRF-Token')).toBe(headers.get('X-CSRF-Token'))
    }
    expect('retryOriginal' in f.auth.workReview).toBe(false)
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })

  it('requires explicit editing after a known rejection and gives the new intent its own key', async () => {
    const f = await fixture()
    let rejected = true
    f.intercept(async (path, init) =>
      path === transferPath && rejected
        ? problem('VERSION_CONFLICT', 409, 'not_committed')
        : f.normal(path, init),
    )
    await expect(
      f.auth.workReview.startTransfer(projectID, taskID, reviewInput()),
    ).rejects.toMatchObject({ problem: { code: 'VERSION_CONFLICT' } })
    expect(f.auth.workReview.progress?.phase).toBe('rejected')
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(
      f.auth.workReview.startTransfer(projectID, taskID, reviewInput()),
    ).rejects.toMatchObject({ kind: 'busy' })
    f.auth.workReview.editRejected()
    expect(f.auth.workReview.progress).toBeNull()
    rejected = false
    const next = {
      expected_version: '7',
      request: { target_state: 'done' as const, comment: '确认完成' },
    }
    await f.auth.workReview.startTransfer(projectID, taskID, next)
    expect(f.auth.workReview.progress).toMatchObject({
      phase: 'confirmed',
      receipt: receipt(next),
    })
    expect(f.transfers()).toHaveLength(2)
    expect(new Headers(f.transfers()[0]![1].headers).get('Idempotency-Key')).not.toBe(
      new Headers(f.transfers()[1]![1].headers).get('Idempotency-Key'),
    )
    expect(JSON.parse(f.transfers()[1]![1].body as string)).toEqual(next)
    expect(f.lookups()).toHaveLength(0)
  })

  it('keeps the actual timed-out Cookie owner and discards its late result after leave', async () => {
    const f = await fixture()
    vi.useFakeTimers()
    const held = deferred(json(receipt()))
    f.intercept((path, init) => (path === transferPath ? held.promise : f.normal(path, init)))
    const saving = f.auth.workReview
      .startTransfer(projectID, taskID, reviewInput())
      .catch((error: unknown) => error)
    await flushPromises()
    expect(f.transfers()).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(30_000)
    await saving
    expect(f.auth.workReview.progress?.phase).toBe('uncertain')
    expect(f.auth.state.busy).toBe(true)
    expect(f.transfers()[0]![1].signal?.aborted).toBe(true)
    const count = f.fetch.mock.calls.length
    await f.auth.logout()
    await f.auth.restore()
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.fetch).toHaveBeenCalledTimes(count)
    f.auth.leave()
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.auth.state.busy).toBe(true)
    held.resolve(json(receipt()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.workReview.progress).toBeNull()
    f.session(session(3))
    await f.auth.restore()
    expect(f.auth.state.session?.id).toBe(id(3))
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await f.auth.logout()
    expect(f.auth.state.phase).toBe('anonymous')
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(0)
  })

  it('treats an invalid receipt as uncertain and drops it when lookup revokes the Session', async () => {
    const f = await fixture()
    f.intercept(async (path, init) => {
      if (path === transferPath)
        return json({ ...receipt(), task: { ...receipt().task, id: id(21) } })
      if (path === lookupPath) return problem('SESSION_REVOKED', 401)
      return f.normal(path, init)
    })
    await expect(
      f.auth.workReview.startTransfer(projectID, taskID, reviewInput()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(f.auth.workReview.progress).toMatchObject({
      phase: 'uncertain',
      receipt: null,
    })
    // Session invalidation may cancel the visible owner before its rejected
    // body returns. Neither result may publish a receipt or retain the intent.
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      name: 'AccountFailure',
    })
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.auth.workReview.progress).toBeNull()
    f.session(session(3))
    await f.auth.restore()
    expect(f.auth.state.session?.id).toBe(id(3))
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(1)
  })
})
