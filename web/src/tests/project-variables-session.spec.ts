import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { AccountFailure, type CommitState, type Fetch } from '../api/client'
import {
  createProjectVariablesAPI,
  type ProjectVariableCommand,
  type ProjectVariablesAPI,
} from '../api/project-variables'
import { createSessionController, type SessionController } from '../composables/useSession'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(10),
  target = id(20),
  at = '2026-10-09T12:00:00.000001Z'
const session = (): SessionView => ({
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
  session: { id: id(2), issued_at: at, idle_expires_at: at, absolute_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
const full = {
  id: target,
  project_id: project,
  type: 'variable',
  name: 'API_URL',
  description: '',
  value: 'original',
  version: '1',
  created_at: at,
  updated_at: at,
}
const create = (): ProjectVariableCommand => ({
  kind: 'create',
  projectID: project,
  request: { variable_id: target, name: full.name, description: '', value: full.value },
})
const update = (): ProjectVariableCommand => ({
  kind: 'update',
  projectID: project,
  targetID: target,
  expectedVersion: '1',
  request: { value: '' },
})
const deletion = (): ProjectVariableCommand => ({
  kind: 'delete',
  projectID: project,
  targetID: target,
  expectedVersion: '2',
})
function receipt(command: ProjectVariableCommand) {
  const common = {
    command: `project.variable.${command.kind}`,
    changed: true,
    event_id: id(30),
    audit_id: id(31),
  }
  return command.kind === 'delete'
    ? {
        ...common,
        deleted: {
          id: target,
          project_id: project,
          type: 'variable',
          version: '3',
          deleted_at: at,
        },
      }
    : {
        ...common,
        variable: command.kind === 'create' ? full : { ...full, value: '', version: '2' },
      }
}
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(90),
    },
  })
const problem = (code: string, status = 409, commit_state: CommitState = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Test',
      detail: 'Must not render raw detail',
      instance: '/api/v1/projects',
      code,
      status,
      commit_state,
      request_id: id(90),
    },
    status,
  )
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const active: SessionController[] = []
async function fixture(fetcher: Fetch, injected?: ProjectVariablesAPI) {
  let current = session()
  const account = { ...createAccountAPI(), getSession: vi.fn(async () => current) }
  const auth = createSessionController(
    account,
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
    undefined,
    undefined,
    undefined,
    injected ?? createProjectVariablesAPI(fetcher),
  )
  active.push(auth)
  await auth.restore()
  return {
    auth,
    account,
    setSession(value: SessionView) {
      current = value
    },
  }
}
afterEach(() => {
  for (const auth of active.splice(0)) auth.leave()
  vi.useRealTimers()
})

describe('Project Variables Session owns exact original intent and actual transport tail', () => {
  it.each([create, update, deletion])(
    'recovers original command then replays identical key/body (%#)',
    async (factory) => {
      const command = factory(),
        sent: { path: string; init: RequestInit }[] = []
      let mutation = 0
      const { auth } = await fixture(async (path, init) => {
        sent.push({ path, init })
        if (path.endsWith('/commands/lookup'))
          return json({ status: 'committed', receipt: receipt(command) })
        if (++mutation === 1) throw new TypeError('dropped response')
        return json(receipt(command))
      })
      const result = auth.projectVariables.start(command)
      if (command.kind !== 'delete')
        (command.request as { value: string }).value = 'changed after capture'
      await expect(result).rejects.toMatchObject({ kind: 'transport' })
      expect(auth.projectVariables.progress).toMatchObject({
        phase: 'uncertain',
        canReplayOriginal: true,
      })
      await auth.projectVariables.lookupOriginal()
      const historical = auth.projectVariables.progress!.receipt
      await auth.projectVariables.replayOriginal()
      expect(sent).toHaveLength(3)
      expect(sent[2]!.path).toBe(sent[0]!.path)
      expect(sent[2]!.init.body).toBe(sent[0]!.init.body)
      const headers = sent.map((x) => new Headers(x.init.headers))
      expect(new Set(headers.map((x) => x.get('Idempotency-Key'))).size).toBe(1)
      expect(headers.map((x) => x.get('X-CSRF-Token'))).toEqual(Array(3).fill('S'.repeat(43)))
      const original = JSON.parse(String(sent[0]!.init.body)),
        observed = JSON.parse(String(sent[1]!.init.body))
      expect(observed).toEqual({
        command: `project.variable.${command.kind}`,
        ...(command.kind === 'create' ? {} : { target_id: target }),
        ...original,
      })
      expect(auth.projectVariables.progress).toMatchObject({
        phase: 'confirmed',
        receipt: historical,
      })
      expect(JSON.stringify(auth.projectVariables.progress)).not.toContain(
        headers[0]!.get('Idempotency-Key'),
      )
    },
  )
  it.each(['not_observed', 'in_progress'] as const)(
    'does not infer noncommit from %s or current GET',
    async (status) => {
      const command = create()
      const { auth } = await fixture(async (path, init) => {
        if (path.endsWith('/commands/lookup')) return json({ status, receipt: null })
        if (init.method === 'GET') return json(full)
        throw new TypeError('lost')
      })
      await expect(auth.projectVariables.start(command)).rejects.toMatchObject({
        kind: 'transport',
      })
      await auth.projectVariables.get(project, target)
      await auth.projectVariables.lookupOriginal()
      expect(auth.projectVariables.progress).toMatchObject({
        phase: 'uncertain',
        receipt: null,
        observation: status,
      })
      expect(auth.projectVariables.editRejected()).toBe(false)
    },
  )
  it.each([create, update, deletion])(
    'retains confirmed history after rejected/disconnected replay and lookup (%#)',
    async (factory) => {
      const command = factory()
      let mode = 'success'
      const { auth } = await fixture(async (path) => {
        if (mode === 'rejected') return problem('PROJECT_NOT_ACTIVE')
        if (mode === 'lost') throw new TypeError('lost')
        return json(
          path.endsWith('/commands/lookup')
            ? { status: 'committed', receipt: receipt(command) }
            : receipt(command),
        )
      })
      await auth.projectVariables.start(command)
      const original = auth.projectVariables.progress!.receipt
      for (mode of ['rejected', 'lost']) {
        await expect(auth.projectVariables.replayOriginal()).rejects.toBeInstanceOf(AccountFailure)
        expect(auth.projectVariables.progress).toMatchObject({
          phase: 'confirmed',
          receipt: original,
        })
        await expect(auth.projectVariables.lookupOriginal()).rejects.toBeInstanceOf(AccountFailure)
        expect(auth.projectVariables.progress).toMatchObject({
          phase: 'confirmed',
          receipt: original,
          observation: 'failed',
        })
      }
      expect(auth.projectVariables.editRejected()).toBe(false)
    },
  )
  it('rejects a different otherwise valid historical receipt after confirmation', async () => {
    let changed = false
    const command = create()
    const { auth } = await fixture(async (path) => {
      const value = { ...receipt(command), event_id: changed ? id(80) : id(30) }
      return json(
        path.endsWith('/commands/lookup') ? { status: 'committed', receipt: value } : value,
      )
    })
    await auth.projectVariables.start(command)
    const original = auth.projectVariables.progress!.receipt
    changed = true
    await expect(auth.projectVariables.lookupOriginal()).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(auth.projectVariables.progress!.receipt).toEqual(original)
    await expect(auth.projectVariables.replayOriginal()).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(auth.projectVariables.progress!.receipt).toEqual(original)
  })
  it.each([
    ['INVALID_ARGUMENT', 400],
    ['FORBIDDEN', 403],
    ['ORIGIN_DENIED', 403],
    ['NOT_FOUND', 404],
    ['VERSION_CONFLICT', 409],
    ['PROJECT_NOT_ACTIVE', 409],
    ['RESOURCE_BUSY', 409],
    ['PAYLOAD_TOO_LARGE', 413],
    ['UNSUPPORTED_MEDIA_TYPE', 415],
    ['DEPENDENCY_UNBOUND', 503],
    ['DEPENDENCY_UNAVAILABLE', 503],
    ['SHUTTING_DOWN', 503],
  ] as const)(
    'recognizes first exact %s/%i refusal without guessing from status',
    async (code, status) => {
      const { auth } = await fixture(async () => problem(code, status, 'not_committed'))
      await expect(auth.projectVariables.start(create())).rejects.toMatchObject({ kind: 'problem' })
      expect(auth.projectVariables.progress?.phase).toBe('rejected')
      expect(auth.projectVariables.editRejected()).toBe(true)
      expect(auth.projectVariables.progress).toBeNull()
    },
  )
  it.each([
    ['VERSION_CONFLICT', 400, 'not_started'],
    ['SOMETHING_ELSE', 409, 'not_started'],
    ['VERSION_CONFLICT', 409, 'unknown'],
    ['COMMIT_UNKNOWN', 503, 'unknown'],
  ] as const)(
    'keeps uncertainty for mismatched or unknown problem %s/%i/%s',
    async (code, status, state) => {
      const { auth } = await fixture(async () => problem(code, status, state))
      await expect(auth.projectVariables.start(create())).rejects.toBeInstanceOf(AccountFailure)
      expect(auth.projectVariables.progress?.phase).toBe('uncertain')
      expect(auth.projectVariables.editRejected()).toBe(false)
    },
  )
  it('does not clear an older unknown after a new exact rejection; key conflict disables all replay', async () => {
    let step = 0
    const fetcher = vi.fn<Fetch>(async () =>
      ++step === 1
        ? problem('COMMIT_UNKNOWN', 503, 'unknown')
        : step === 2
          ? problem('VERSION_CONFLICT')
          : problem('IDEMPOTENCY_KEY_REUSED'),
    )
    const { auth } = await fixture(fetcher)
    await expect(auth.projectVariables.start(create())).rejects.toBeInstanceOf(AccountFailure)
    await expect(auth.projectVariables.replayOriginal()).rejects.toBeInstanceOf(AccountFailure)
    expect(auth.projectVariables.progress?.phase).toBe('uncertain')
    await expect(auth.projectVariables.lookupOriginal()).rejects.toBeInstanceOf(AccountFailure)
    expect(auth.projectVariables.progress).toMatchObject({
      phase: 'uncertain',
      keyConflict: true,
      canReplayOriginal: false,
    })
    await expect(auth.projectVariables.replayOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetcher).toHaveBeenCalledTimes(3)
  })
  it('retains private intent while same identity checking, but destroys it on actual CSRF/session/user change', async () => {
    const { auth, account, setSession } = await fixture(async () => {
      throw new TypeError('lost')
    })
    await expect(auth.projectVariables.start(create())).rejects.toBeInstanceOf(AccountFailure)
    const held = barrier<SessionView>()
    account.getSession.mockImplementationOnce(() => held.promise)
    const checking = auth.restore()
    await flushPromises()
    expect(auth.projectVariables.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
      canReplayOriginal: false,
    })
    held.resolve(session())
    await checking
    expect(auth.projectVariables.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
      canReplayOriginal: true,
    })
    for (const next of [
      { ...session(), csrf_token: 'N'.repeat(43) },
      { ...session(), session: { ...session().session, id: id(3) } },
      { ...session(), user: { ...session().user, id: id(4) } },
    ]) {
      setSession(next)
      await auth.restore()
      expect(auth.projectVariables.progress).toBeNull()
      await expect(auth.projectVariables.replayOriginal()).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      await expect(auth.projectVariables.start(create())).rejects.toMatchObject({
        kind: 'transport',
      })
    }
  })
  it.each([
    ['SESSION_REVOKED', 401],
    ['CSRF_FAILED', 403],
  ] as const)('retires intent on current %s', async (code, status) => {
    const { auth } = await fixture(async () => problem(code, status))
    await expect(auth.projectVariables.start(create())).rejects.toBeInstanceOf(AccountFailure)
    expect(auth.projectVariables.progress).toBeNull()
    expect(auth.state.phase).toBe('unavailable')
  })
  it('logical timeout and abandonment do not release the actual reader cancellation owner', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const cancelled = barrier(),
      release = barrier()
    let cancelCalls = 0
    const fetcher = vi.fn<Fetch>(
      async () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(new TextEncoder().encode(JSON.stringify(receipt(create()))))
            },
            async cancel() {
              ++cancelCalls
              cancelled.resolve()
              await release.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    const { auth, account } = await fixture(fetcher)
    const pending = auth.projectVariables.start(create()).catch((e) => e)
    await flushPromises()
    expect(auth.state.busy).toBe(true)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    await cancelled.promise
    expect(auth.projectVariables.progress?.phase).toBe('uncertain')
    expect(auth.state.busy).toBe(true)
    await expect(auth.projectVariables.get(project, target)).rejects.toMatchObject({ kind: 'busy' })
    await auth.restore()
    expect(account.getSession).toHaveBeenCalledTimes(1)
    auth.projectVariables.abandonPending()
    expect(auth.projectVariables.progress).toBeNull()
    expect(auth.state.busy).toBe(true)
    release.resolve()
    await flushPromises()
    expect(cancelCalls).toBe(1)
    expect(auth.state.busy).toBe(false)
    expect(auth.projectVariables.progress).toBeNull()
  })
  it('abandoning a read rejects visible result, joins its original signal and prevents late publication', async () => {
    const response = barrier<Response>(),
      invoked = barrier()
    let originalSignal: AbortSignal | undefined
    const { auth } = await fixture(async (_path, init) => {
      originalSignal = init.signal!
      invoked.resolve()
      return response.promise
    })
    const pending = auth.projectVariables.get(project, target).catch((e) => e)
    await invoked.promise
    auth.projectVariables.abandonReads()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(originalSignal!.aborted).toBe(true)
    expect(auth.state.busy).toBe(true)
    response.resolve(json(full))
    await flushPromises()
    expect(auth.state.busy).toBe(false)
    expect(auth.projectVariables.progress).toBeNull()
  })
  it('revalidates injected API receipts and pages rather than trusting an untyped success', async () => {
    const api = createProjectVariablesAPI(async () => json(receipt(create())))
    api.create = async () =>
      ({ ...receipt(create()), variable: { ...full, project_id: id(55) } }) as never
    api.list = async () => ({ items: [{ ...full }] }) as never
    const { auth } = await fixture(vi.fn<Fetch>(), api)
    await expect(auth.projectVariables.list(project, { limit: 50 })).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    await expect(auth.projectVariables.start(create())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(auth.projectVariables.progress?.phase).toBe('uncertain')
  })
})
