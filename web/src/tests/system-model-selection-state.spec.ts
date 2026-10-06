import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { watch } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { AccountFailure, type CommitState, type Fetch } from '../api/client'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI, type ProviderInput } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI, type SelectionCommand } from '../api/system-model-selection'
import { createSessionController, type SessionController } from '../composables/useSession'
import { useTheme } from '../composables/useTheme'
import {
  createSystemModelSelection,
  selectionReason,
  type SystemModelSelectionController,
} from '../composables/useSystemModelSelection'
import type { SelectionPurpose, SelectionState } from '../api/system-model-selection'
import type { ModelType, Provider, ProviderModel, ProviderProtocol } from '../api/system-providers'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const view = (sessionID = id(2), role: 'admin' | 'user' = 'admin'): SessionView => ({
  user: {
    id: id(1),
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Admin',
    role,
    theme: 'system',
    version: role === 'admin' ? '1' : '2',
    initial_password_suggestion: false,
  },
  session: { id: sessionID, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const configured = () => ({ embedding: id(20), memory: id(21), reranker: null, image: null })
const selectionValue = () => ({ id: id(100), version: '1', configured: configured() })
const command = (): SelectionCommand => ({
  kind: 'model.selection.update',
  id: id(100),
  expected_version: '1',
  ...configured(),
})
const receipt = (version = '2') => ({
  kind: 'model.selection.update',
  resource_id: id(100),
  version,
  affected_references: '0',
})
const providerInput = (): ProviderInput => ({
  name: 'Provider',
  protocol: 'openai-chat-completions',
  base_url: 'https://example.invalid',
  enabled: true,
  credential_ref: null,
  options: {},
})
const provider = () => ({
  id: id(10),
  input: providerInput(),
  version: '1',
  created_at: time,
  updated_at: time,
})
const model = () => ({
  id: id(21),
  provider_id: id(10),
  version: '1',
  created_at: time,
  updated_at: time,
  input: {
    name: 'Memory',
    provider_model_id: ' native ',
    type: 'chat',
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      input_modalities: ['text'],
      output_modalities: ['text'],
      reasoning_efforts: [],
      structured_output_modes: ['json_schema'],
      context_length: null,
      max_output: null,
    },
  },
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problemValue = (code: string, status = 503, commit_state: CommitState = 'not_started') => ({
  type: 'urn:agenteam:problem:test',
  title: 'Failure',
  status,
  code,
  detail: '',
  instance: '/api/v1/system/model-selection',
  request_id: id(9),
  commit_state,
})
const problem = (code: string, status = 503, state: CommitState = 'not_started') =>
  json(problemValue(code, status, state), status)
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, failed) => {
    resolve = done
    reject = failed
  })
  return { promise, resolve, reject }
}
const owners: SessionController[] = []
const pages: SystemModelSelectionController[] = []
async function fixture() {
  let current = view()
  let session: Fetch = async () => json(current)
  let perform: Fetch = async (path, init) => {
    if (path.endsWith('/lookup')) return json({ found: true, receipt: receipt() })
    if (init.method === 'GET') return json(selectionValue())
    return json(receipt(String(BigInt(JSON.parse(init.body as string).expected_version) + 1n)))
  }
  let references: Fetch = async (path) =>
    json(path.includes('model-providers') ? provider() : model())
  let other: Fetch = async (path, init) => {
    if (path === '/api/v1/me') return json({ user: current.user, avatar: null })
    if (path.endsWith('/model-credentials'))
      return json({ credential_id: id(11), purpose: 'model', version: '1', deleted: false })
    if (path.endsWith('/model-providers') && init.method === 'POST')
      return json({
        kind: 'provider.create',
        resource_id: id(10),
        version: '1',
        affected_references: '0',
      })
    if (path.includes('/model-providers?')) return json({ items: [provider()], next_cursor: null })
    if (path.includes('/models?')) return json({ items: [model()], next_cursor: null })
    if (path.endsWith('/invitations') && init.method === 'POST')
      return json({ id: id(30), job_id: id(31), version: '1' }, 201)
    if (path === '/api/v1/password-resets/request') return json({ accepted: true }, 202)
    return json({ items: [] })
  }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    if (
      path === '/api/v1/system/model-selection' ||
      (path.endsWith('/model-commands/lookup') &&
        JSON.parse(init.body as string).command === 'model.selection.update')
    )
      return perform(path, init)
    if (
      (path.startsWith('/api/v1/system/models/') ||
        path.startsWith('/api/v1/system/model-providers/')) &&
      init.method === 'GET'
    )
      return references(path, init)
    return other(path, init)
  })
  const selectionAPI = createSystemModelSelectionAPI(fetch)
  const auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    selectionAPI,
  )
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    selectionAPI,
    setPerform(value: Fetch) {
      perform = value
    },
    setReferences(value: Fetch) {
      references = value
    },
    setOther(value: Fetch) {
      other = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    writes() {
      return fetch.mock.calls.filter(
        ([path, init]) => path === '/api/v1/system/model-selection' && init.method === 'PUT',
      )
    },
    references() {
      return fetch.mock.calls.filter(
        ([path]) =>
          path.startsWith('/api/v1/system/models/') ||
          path.startsWith('/api/v1/system/model-providers/'),
      )
    },
  }
}
afterEach(() => {
  for (const page of pages.splice(0)) page.dispose()
  for (const auth of owners.splice(0)) auth.leave()
  vi.useRealTimers()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})

describe('Selection on the existing Cookie owner', () => {
  it('captures the full immutable original before key allocation and exposes no key/CSRF/body', async () => {
    const f = await fixture(),
      key = vi.spyOn(crypto, 'randomUUID')
    for (const invalid of [
      { ...command(), memory: null },
      { ...command(), expected_version: '9223372036854775807' },
      { ...command(), extra: '🧩'.repeat(9000) },
    ])
      await expect(
        f.auth.system.selection.start(invalid as SelectionCommand),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(key).not.toHaveBeenCalled()
    expect(f.writes()).toHaveLength(0)
    expect(f.auth.system.selection.progress).toBeNull()
    for (const query of [null, undefined, [], 'cursor']) {
      await expect(f.auth.system.selection.listProviders(query as never)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      await expect(f.auth.system.selection.listModels(query as never)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    }
    const held = barrier<Response>(),
      original = command()
    f.setPerform(() => held.promise)
    const result = f.auth.system.selection.start(original)
    try {
      Object.assign(original, { expected_version: '9', memory: id(99) })
      await flushPromises()
      expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
        id: id(100),
        expected_version: '1',
        ...configured(),
      })
      expect(key).toHaveBeenCalledTimes(1)
      const publicState = JSON.stringify([f.auth.state, f.auth.system.selection.progress])
      expect(publicState.includes(view().csrf_token)).toBe(false)
      expect(
        publicState.includes(new Headers(f.writes()[0]![1].headers).get('Idempotency-Key')!),
      ).toBe(false)
      expect(publicState.includes(id(20))).toBe(false)
    } finally {
      held.resolve(json(receipt()))
      await result
    }
    expect(await result).toEqual(receipt())
  })

  it.each(
    (['read', 'write', 'lookup'] as const).flatMap((operation) =>
      (['fetch', 'read', 'reader-cancel', 'body-cancel'] as const).map((tail) => ({
        operation,
        tail,
      })),
    ),
  )(
    'keeps $operation/$tail owned after the visible 30s cutoff until its actual tail joins',
    async ({ operation, tail }) => {
      vi.useFakeTimers()
      const f = await fixture(),
        fetched = barrier<Response>(),
        read = barrier<{ done: boolean; value?: Uint8Array }>(),
        cancelled = barrier(),
        bodyCancelled = barrier()
      if (operation === 'lookup') {
        f.setPerform(async () => {
          throw new Error('lost')
        })
        await f.auth.system.selection.start(command()).catch(() => undefined)
      }
      const payload =
        operation === 'read'
          ? selectionValue()
          : operation === 'write'
            ? receipt()
            : { found: false, receipt: null }
      const response = json(payload)
      const reader = {
        read: vi.fn(() => read.promise),
        cancel: vi.fn(() => (tail === 'reader-cancel' ? cancelled.promise : Promise.resolve())),
        releaseLock: vi.fn(),
      }
      if (tail === 'body-cancel') {
        reader.read.mockImplementationOnce(async () => ({
          done: false,
          value: new TextEncoder().encode(JSON.stringify(payload)),
        }))
        read.resolve({ done: true })
      } else if (tail === 'reader-cancel') read.resolve({ done: true })
      if (tail !== 'fetch')
        Object.defineProperty(response, 'body', {
          value: {
            getReader: () => reader,
            cancel: () => (tail === 'body-cancel' ? bodyCancelled.promise : Promise.resolve()),
          },
        })
      f.setPerform(() => (tail === 'fetch' ? fetched.promise : Promise.resolve(response)))
      const result = (
        operation === 'read'
          ? f.auth.system.selection.get()
          : operation === 'write'
            ? f.auth.system.selection.start(command())
            : f.auth.system.selection.checkOriginal()
      ).catch((e: unknown) => e)
      try {
        await flushPromises()
        await vi.advanceTimersByTimeAsync(30000)
        expect(await result).toMatchObject({ kind: 'cancelled' })
        expect(f.auth.state.busy).toBe(true)
        const before = f.fetch.mock.calls.length,
          key = vi.spyOn(crypto, 'randomUUID')
        await f.auth.restore()
        await f.auth.logout()
        await f.auth.login('admin@example.com', 'generated password 123')
        await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
        await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'busy' })
        await expect(f.auth.system.listInvitations({})).rejects.toMatchObject({ kind: 'busy' })
        await expect(f.auth.system.providers.list({})).rejects.toMatchObject({ kind: 'busy' })
        await expect(f.auth.system.models.listProviders({})).rejects.toMatchObject({ kind: 'busy' })
        await expect(f.auth.system.selection.start(command())).rejects.toMatchObject({
          kind: 'busy',
        })
        await expect(
          f.auth.entry.requestPasswordReset({ email: 'generated@example.com' }),
        ).rejects.toMatchObject({ kind: 'busy' })
        expect(key).not.toHaveBeenCalled()
        expect(f.fetch.mock.calls).toHaveLength(before)
        f.auth.system.selection.abandon()
        expect(f.auth.state.busy).toBe(true)
      } finally {
        fetched.resolve(response)
        read.resolve({ done: true })
        cancelled.resolve()
        bodyCancelled.resolve()
        await result
        await flushPromises()
      }
      expect(f.auth.state.busy).toBe(false)
      expect(f.auth.system.selection.progress).toBeNull()
      await expect(f.auth.system.listUsers({})).resolves.toEqual({ items: [] })
    },
  )

  it('uses one signal and one 30s owner across two sequential native body reads, with no half-publication', async () => {
    vi.useFakeTimers()
    const f = await fixture(),
      first = barrier<ReadableStreamDefaultController<Uint8Array>>(),
      second = barrier<ReadableStreamDefaultController<Uint8Array>>(),
      tail = barrier(),
      cancelled = barrier()
    f.setReferences(
      async (path) =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(
                new TextEncoder().encode(
                  JSON.stringify(path.includes('model-providers') ? provider() : model()),
                ),
              )
              ;(path.includes('model-providers') ? second : first).resolve(controller)
            },
            cancel() {
              cancelled.resolve()
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    let published = false
    const result = f.auth.system.selection.getSavedModel(id(21)).then(
      (value) => {
        published = true
        return value
      },
      (e: unknown) => e,
    )
    try {
      const initial = await first.promise
      await vi.advanceTimersByTimeAsync(20000)
      expect(f.references()).toHaveLength(1)
      expect(published).toBe(false)
      initial.close()
      await second.promise
      const requests = f.references()
      expect(requests).toHaveLength(2)
      expect(requests[1]![1].signal).toBe(requests[0]![1].signal)
      await vi.advanceTimersByTimeAsync(9999)
      expect(requests[1]![1].signal!.aborted).toBe(false)
      expect(published).toBe(false)
      await vi.advanceTimersByTimeAsync(1)
      expect(await result).toMatchObject({ kind: 'cancelled' })
      await cancelled.promise
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.system.providers.list({})).rejects.toMatchObject({ kind: 'busy' })
      expect(published).toBe(false)
    } finally {
      f.auth.system.selection.abandon()
      tail.resolve()
      await result
      await flushPromises()
    }
    expect(f.auth.state.busy).toBe(false)
    expect(f.references()).toHaveLength(2)
    expect(published).toBe(false)
  })

  it.each([1, 2])(
    'abandons native GET %i without publishing or advancing until cancel is joined',
    async (stage) => {
      const f = await fixture(),
        started = barrier(),
        tail = barrier(),
        cancelled = barrier()
      f.setReferences(async (path) => {
        const index = path.includes('model-providers') ? 2 : 1
        if (index !== stage) return json(model())
        return new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(
                new TextEncoder().encode(JSON.stringify(index === 1 ? model() : provider())),
              )
              started.resolve()
            },
            cancel() {
              cancelled.resolve()
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        )
      })
      let published = false
      const result = f.auth.system.selection.getSavedModel(id(21)).then(
        (v) => {
          published = true
          return v
        },
        (e: unknown) => e,
      )
      try {
        await started.promise
        f.auth.system.selection.abandonRead('selection-reference')
        expect(await result).toMatchObject({ kind: 'cancelled' })
        await cancelled.promise
        expect(f.auth.state.busy).toBe(true)
        await expect(f.auth.system.models.listProviders({})).rejects.toMatchObject({ kind: 'busy' })
        expect(f.references()).toHaveLength(stage)
      } finally {
        f.auth.system.selection.abandon()
        tail.resolve()
        await result
        await flushPromises()
      }
      expect(f.auth.state.busy).toBe(false)
      expect(published).toBe(false)
      expect(f.references()).toHaveLength(stage)
    },
  )

  it('does not dispatch Provider GET after cancellation at the first actual finalizer', async () => {
    const f = await fixture(),
      entered = barrier(),
      tail = barrier(),
      response = json(model())
    const cancel = response.body!.cancel.bind(response.body)
    response.body!.cancel = async () => {
      entered.resolve()
      await tail.promise
      return cancel()
    }
    f.setReferences(async () => response)
    const result = f.auth.system.selection.getSavedModel(id(21)).catch((e: unknown) => e)
    try {
      await entered.promise
      f.auth.system.selection.abandonRead('selection-reference')
      expect(await result).toMatchObject({ kind: 'cancelled' })
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
    } finally {
      tail.resolve()
      await result
      await flushPromises()
    }
    expect(f.references()).toHaveLength(1)
    expect(f.auth.state.busy).toBe(false)
  })

  it('keeps a failed Provider response body owned until its native cancel joins', async () => {
    const f = await fixture(),
      entered = barrier(),
      tail = barrier()
    f.setReferences(async (path) =>
      path.includes('model-providers')
        ? new Response(
            new ReadableStream<Uint8Array>({
              cancel() {
                entered.resolve()
                return tail.promise
              },
            }),
            { headers: { 'Content-Type': 'text/plain' } },
          )
        : json(model()),
    )
    let ended = false
    const result = f.auth.system.selection
      .getSavedModel(id(21))
      .catch((e: unknown) => e)
      .finally(() => {
        ended = true
      })
    try {
      await entered.promise
      expect(ended).toBe(false)
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
      expect(f.references()).toHaveLength(2)
    } finally {
      tail.resolve()
      await result
    }
    expect(await result).toMatchObject({ kind: 'invalid-response' })
    expect(f.auth.state.busy).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('separates saved-reference cancellation from the active candidate page', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    let signal: AbortSignal | undefined
    f.setOther((_path, init) => {
      signal = init.signal!
      return held.promise
    })
    const result = f.auth.system.selection.listModels({
      provider_id: id(10),
      protocol: 'openai-chat-completions',
    })
    try {
      await flushPromises()
      f.auth.system.selection.abandonRead('selection-reference')
      expect(() => f.auth.system.selection.abandonRead('model-detail' as never)).toThrow(
        AccountFailure,
      )
      expect(signal?.aborted).toBe(false)
      expect(f.auth.state.busy).toBe(true)
    } finally {
      held.resolve(json({ items: [model()], next_cursor: null }))
      await result
    }
    expect((await result).items).toHaveLength(1)
  })

  it('keeps a real overflow cancel tail owned and returns no candidate', async () => {
    const f = await fixture(),
      entered = barrier(),
      tail = barrier()
    f.setPerform(
      async () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(new Uint8Array(600001).fill(32))
            },
            cancel() {
              entered.resolve()
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    let ended = false
    const result = f.auth.system.selection
      .get()
      .catch((e: unknown) => e)
      .finally(() => {
        ended = true
      })
    try {
      await entered.promise
      expect(ended).toBe(false)
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.system.listUsers({})).rejects.toMatchObject({ kind: 'busy' })
    } finally {
      tail.resolve()
      await result
    }
    expect(await result).toMatchObject({ kind: 'invalid-response' })
    expect(f.auth.state.busy).toBe(false)
  })

  it.each(['read', 'write', 'lookup'] as const)(
    'other domain abandonment does not cancel Selection %s',
    async (operation) => {
      const f = await fixture(),
        held = barrier<Response>()
      if (operation === 'lookup') {
        f.setPerform(async () => {
          throw new Error('lost')
        })
        await f.auth.system.selection.start(command()).catch(() => undefined)
      }
      let signal: AbortSignal | undefined
      f.setPerform((_path, init) => {
        signal = init.signal!
        return held.promise
      })
      const result = (
        operation === 'read'
          ? f.auth.system.selection.get()
          : operation === 'write'
            ? f.auth.system.selection.start(command())
            : f.auth.system.selection.checkOriginal()
      ).catch((e: unknown) => e)
      try {
        await flushPromises()
        f.auth.personal.abandon()
        f.auth.system.abandon()
        f.auth.system.abandonInvitations()
        f.auth.system.providers.abandon()
        f.auth.system.models.abandon()
        f.auth.entry.abandon()
        expect(signal?.aborted).toBe(false)
        expect(f.auth.state.busy).toBe(true)
      } finally {
        held.resolve(
          json(
            operation === 'read'
              ? selectionValue()
              : operation === 'write'
                ? receipt()
                : { found: false, receipt: null },
          ),
        )
        await result
      }
      expect(await result).not.toBeInstanceOf(AccountFailure)
      expect(f.auth.state.busy).toBe(false)
    },
  )

  it.each(['personal', 'users', 'invitations', 'provider', 'model', 'restore'] as const)(
    'Selection abandonment does not cancel %s or release its tail',
    async (operation) => {
      const f = await fixture(),
        held = barrier<Response>()
      let signal: AbortSignal | undefined
      const hold: Fetch = (_path, init) => {
        signal = init.signal!
        return held.promise
      }
      if (operation === 'restore') f.setSessionRequest(hold)
      else f.setOther(hold)
      const result = (
        operation === 'personal'
          ? f.auth.personal.getProfile()
          : operation === 'users'
            ? f.auth.system.listUsers({})
            : operation === 'invitations'
              ? f.auth.system.listInvitations({})
              : operation === 'provider'
                ? f.auth.system.providers.list({})
                : operation === 'model'
                  ? f.auth.system.models.listProviders({})
                  : f.auth.restore()
      ).catch((e: unknown) => e)
      try {
        await flushPromises()
        f.auth.system.selection.abandon()
        expect(signal?.aborted).toBe(false)
        expect(f.auth.state.busy).toBe(true)
        await expect(f.auth.system.selection.get()).rejects.toMatchObject({ kind: 'busy' })
      } finally {
        held.resolve(
          json(
            operation === 'personal'
              ? { user: view().user, avatar: null }
              : operation === 'restore'
                ? view()
                : ['provider', 'model'].includes(operation)
                  ? { items: [], next_cursor: null }
                  : { items: [] },
          ),
        )
        await result
      }
      expect(await result).not.toBeInstanceOf(AccountFailure)
      expect(f.auth.state.busy).toBe(false)
    },
  )

  it('preserves both Provider Credential→Provider stages during Selection cleanup', async () => {
    const f = await fixture(),
      credential = barrier<Response>(),
      saved = barrier<Response>()
    f.setOther((path) => (path.endsWith('/model-credentials') ? credential.promise : saved.promise))
    f.auth.system.providers.setMaterial('generated-only')
    const result = f.auth.system.providers.start({
      kind: 'provider.create',
      input: providerInput(),
    })
    try {
      await flushPromises()
      f.auth.system.selection.abandon()
      expect(f.auth.system.providers.progress?.stage).toBe('credential')
      credential.resolve(
        json({ credential_id: id(11), purpose: 'model', version: '1', deleted: false }),
      )
      await flushPromises()
      f.auth.system.selection.abandon()
      expect(f.auth.system.providers.progress?.stage).toBe('provider')
      expect(f.auth.state.busy).toBe(true)
      await expect(f.auth.system.selection.start(command())).rejects.toMatchObject({ kind: 'busy' })
    } finally {
      credential.resolve(
        json({ credential_id: id(11), purpose: 'model', version: '1', deleted: false }),
      )
      saved.resolve(
        json({
          kind: 'provider.create',
          resource_id: id(10),
          version: '1',
          affected_references: '0',
        }),
      )
      await result
    }
    expect(f.auth.system.providers.progress?.phase).toBe('confirmed')
  })
})

describe('Selection original Execute recovery and authority', () => {
  it.each(['found', 'missing', 'failed'] as const)(
    'treats %s lookup only as history and explicitly replays the exact original PUT',
    async (observation) => {
      const f = await fixture()
      f.setPerform(async () => {
        throw new Error('accepted but response lost')
      })
      await f.auth.system.selection.start(command()).catch(() => undefined)
      const first = f.writes()[0]!
      expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
      f.setPerform(async (path) =>
        path.endsWith('/lookup')
          ? observation === 'failed'
            ? problem('INTERNAL_ERROR')
            : json(
                observation === 'found'
                  ? { found: true, receipt: receipt() }
                  : { found: false, receipt: null },
              )
          : json(receipt()),
      )
      await f.auth.system.selection.checkOriginal().catch(() => undefined)
      expect(f.writes()).toHaveLength(1)
      expect(f.auth.system.selection.progress).toMatchObject({
        phase: 'uncertain',
        observation,
        receipt: null,
        canRetryOriginal: true,
      })
      f.setReferences(async () => problem('NOT_FOUND', 404))
      await expect(f.auth.system.selection.getSavedModel(id(21))).rejects.toMatchObject({
        kind: 'problem',
      })
      expect(await f.auth.system.selection.retryOriginal()).toEqual(receipt())
      expect(f.writes()).toHaveLength(2)
      const replay = f.writes()[1]!
      expect(replay[1].body).toBe(first[1].body)
      expect(new Headers(replay[1].headers).get('Idempotency-Key')).toBe(
        new Headers(first[1].headers).get('Idempotency-Key'),
      )
      expect(new Headers(replay[1].headers).get('X-CSRF-Token')).toBe(view().csrf_token)
      expect(f.auth.system.selection.progress).toMatchObject({
        phase: 'confirmed',
        receipt: receipt(),
        canRetryOriginal: false,
      })
      f.setPerform(async () => problem('INTERNAL_ERROR'))
      await expect(f.auth.system.selection.get()).rejects.toMatchObject({ kind: 'problem' })
      expect(f.auth.system.selection.progress?.phase).toBe('confirmed')
      await expect(f.auth.system.selection.retryOriginal()).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      expect(f.writes()).toHaveLength(2)
    },
  )

  it.each([
    ['VERSION_CONFLICT', 409, 'not_committed', 'rejected'],
    ['CAPABILITY_UNSUPPORTED', 422, 'not_started', 'rejected'],
    ['NOT_FOUND', 404, 'not_committed', 'rejected'],
    ['COMMIT_UNKNOWN', 503, 'unknown', 'uncertain'],
    ['INTERNAL_ERROR', 503, 'not_committed', 'uncertain'],
  ] as const)(
    'keeps %s distinct from a confirmed or guessed save',
    async (code, status, state, phase) => {
      const f = await fixture()
      f.setPerform(async () => problem(code, status, state))
      await f.auth.system.selection.start(command()).catch(() => undefined)
      expect(f.auth.system.selection.progress).toMatchObject({
        phase,
        receipt: null,
        canRetryOriginal: false,
      })
      expect(f.writes()).toHaveLength(1)
    },
  )
  it('does not erase prior uncertainty with a known refusal or permit reused-key recovery', async () => {
    const f = await fixture()
    f.setPerform(async () => {
      throw new Error('lost')
    })
    await f.auth.system.selection.start(command()).catch(() => undefined)
    f.setPerform(async (path) =>
      path.endsWith('/lookup')
        ? json({ found: false, receipt: null })
        : problem('VERSION_CONFLICT', 409, 'not_committed'),
    )
    await f.auth.system.selection.checkOriginal()
    await f.auth.system.selection.retryOriginal().catch(() => undefined)
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    await f.auth.system.selection.checkOriginal()
    f.setPerform(async () => problem('IDEMPOTENCY_KEY_REUSED', 409, 'not_started'))
    await f.auth.system.selection.retryOriginal().catch(() => undefined)
    expect(f.auth.system.selection.progress).toMatchObject({
      phase: 'uncertain',
      canRetryOriginal: false,
    })
    expect(f.writes()).toHaveLength(3)
  })

  it.each(['write', 'lookup'] as const)(
    'invalidates current identity on Selection %s CSRF_FAILED',
    async (operation) => {
      const f = await fixture()
      if (operation === 'lookup') {
        f.setPerform(async () => {
          throw new Error('lost')
        })
        await f.auth.system.selection.start(command()).catch(() => undefined)
      }
      f.setPerform(async () => problem('CSRF_FAILED', 403))
      await (
        operation === 'write'
          ? f.auth.system.selection.start(command())
          : f.auth.system.selection.checkOriginal()
      ).catch(() => undefined)
      expect(f.auth.personalContext.phase).toBe('invalid')
      expect(f.auth.personalContext.identity).toBeNull()
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.system.selection.progress).toBeNull()
    },
  )
  it('clears every system private state on current FORBIDDEN and restores access only after Session revalidation', async () => {
    const f = await fixture()
    f.auth.system.providers.setMaterial('generated-only')
    f.setPerform(async () => {
      throw new Error('lost')
    })
    await f.auth.system.selection.start(command()).catch(() => undefined)
    f.setPerform(async () => problem('FORBIDDEN', 403))
    await f.auth.system.selection.get().catch(() => undefined)
    expect(f.auth.system.denied).toBe(true)
    expect(f.auth.state.user?.role).toBe('admin')
    expect(f.auth.system.selection.progress).toBeNull()
    expect(f.auth.system.providers.material.present).toBe(false)
    expect(f.auth.system.models.progress).toBeNull()
    await expect(f.auth.system.selection.get()).rejects.toMatchObject({ kind: 'invalid-input' })
    await f.auth.restore()
    expect(f.auth.system.denied).toBe(false)
  })
  it.each(['read', 'write', 'lookup'] as const)(
    'invalidates exact identity on current Selection %s 401',
    async (operation) => {
      const f = await fixture()
      if (operation === 'lookup') {
        f.setPerform(async () => {
          throw new Error('lost')
        })
        await f.auth.system.selection.start(command()).catch(() => undefined)
      }
      f.setPerform(async () => problem('SESSION_REVOKED', 401))
      await (
        operation === 'read'
          ? f.auth.system.selection.get()
          : operation === 'write'
            ? f.auth.system.selection.start(command())
            : f.auth.system.selection.checkOriginal()
      ).catch(() => undefined)
      expect(f.auth.personalContext.identity).toBeNull()
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.system.selection.progress).toBeNull()
    },
  )
  it.each(['FORBIDDEN', 'SESSION_REVOKED', 'CSRF_FAILED'] as const)(
    'does not apply a late old-generation %s to a restored identity',
    async (code) => {
      const f = await fixture(),
        tail = barrier<ReturnType<typeof selectionValue>>()
      f.selectionAPI.getSelection = () => tail.promise
      const result = f.auth.system.selection.get().catch((e: unknown) => e)
      try {
        await flushPromises()
        f.auth.leave()
        expect(await result).toMatchObject({ kind: 'cancelled' })
        f.setSession(view(id(3)))
        const before = f.fetch.mock.calls.length
        await f.auth.restore()
        expect(f.fetch.mock.calls).toHaveLength(before)
        expect(f.auth.state.busy).toBe(true)
      } finally {
        tail.reject(
          new AccountFailure('problem', problemValue(code, code === 'SESSION_REVOKED' ? 401 : 403)),
        )
        await result
        await flushPromises()
      }
      await f.auth.restore()
      expect(f.auth.personalContext.identity?.sessionID).toBe(id(3))
      expect(f.auth.system.denied).toBe(false)
      expect(f.auth.system.selection.progress).toBeNull()
    },
  )
  it.each([
    ['read', 'SESSION_REVOKED', true],
    ['write', 'CSRF_FAILED', true],
    ['lookup', 'CSRF_FAILED', true],
    ['read', 'FORBIDDEN', false],
  ] as const)(
    'applies already parsed late %s/%s only under the original identity/generation rule',
    async (operation, code, invalidates) => {
      vi.useFakeTimers()
      const f = await fixture(),
        delayed = barrier<never>()
      if (operation === 'lookup') {
        f.setPerform(async () => {
          throw new Error('lost')
        })
        await f.auth.system.selection.start(command()).catch(() => undefined)
      }
      if (operation === 'read') f.selectionAPI.getSelection = () => delayed.promise
      else if (operation === 'write') f.selectionAPI.updateSelection = () => delayed.promise
      else f.selectionAPI.lookupSelectionCommand = () => delayed.promise
      const result = (
        operation === 'read'
          ? f.auth.system.selection.get()
          : operation === 'write'
            ? f.auth.system.selection.start(command())
            : f.auth.system.selection.checkOriginal()
      ).catch((e: unknown) => e)
      try {
        await flushPromises()
        await vi.advanceTimersByTimeAsync(30000)
        expect(await result).toMatchObject({ kind: 'cancelled' })
        expect(f.auth.state.busy).toBe(true)
      } finally {
        delayed.reject(
          new AccountFailure('problem', problemValue(code, code === 'SESSION_REVOKED' ? 401 : 403)),
        )
        await result
        await flushPromises()
      }
      expect(f.auth.state.busy).toBe(false)
      expect(f.auth.personalContext.identity === null).toBe(invalidates)
      expect(f.auth.system.denied).toBe(false)
    },
  )

  it.each(['role', 'session', 'csrf'] as const)(
    'clears an uncertain intent when the restored %s changes',
    async (mode) => {
      const f = await fixture()
      f.setPerform(async () => {
        throw new Error('lost')
      })
      await f.auth.system.selection.start(command()).catch(() => undefined)
      const next = view(mode === 'session' ? id(3) : id(2), mode === 'role' ? 'user' : 'admin')
      if (mode === 'csrf') next.csrf_token = 'N'.repeat(43)
      f.setSession(next)
      await f.auth.restore()
      expect(f.auth.system.selection.progress).toBeNull()
      await expect(f.auth.system.selection.retryOriginal()).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    },
  )

  it('does not dispatch or resurrect progress after a synchronous submitting consumer abandons', async () => {
    const f = await fixture()
    const stop = watch(
      () => f.auth.system.selection.progress?.phase,
      (phase) => {
        if (phase === 'submitting') f.auth.system.selection.abandon()
      },
      { flush: 'sync' },
    )
    try {
      await expect(f.auth.system.selection.start(command())).rejects.toMatchObject({
        kind: 'cancelled',
      })
    } finally {
      stop()
    }
    expect(f.writes()).toHaveLength(0)
    expect(f.auth.system.selection.progress).toBeNull()
    expect(f.auth.state.busy).toBe(false)
  })
  it('does not erase a successor started synchronously from confirmation', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    let successor: Promise<unknown> | undefined,
      once = false
    const stop = watch(
      () => f.auth.system.selection.progress?.phase,
      (phase) => {
        if (phase !== 'confirmed' || once) return
        once = true
        f.auth.system.selection.abandon()
        f.setPerform(() => held.promise)
        successor = f.auth.system.selection.start({
          ...command(),
          expected_version: '2',
          memory: id(31),
        })
      },
      { flush: 'sync' },
    )
    try {
      await expect(f.auth.system.selection.start(command())).rejects.toMatchObject({
        kind: 'cancelled',
      })
      await flushPromises()
      expect(f.auth.state.busy).toBe(true)
      expect(f.auth.system.selection.progress?.phase).toBe('submitting')
      expect(JSON.parse(f.writes()[1]![1].body as string)).toMatchObject({
        expected_version: '2',
        memory: id(31),
      })
      expect(new Headers(f.writes()[1]![1].headers).get('Idempotency-Key')).not.toBe(
        new Headers(f.writes()[0]![1].headers).get('Idempotency-Key'),
      )
    } finally {
      stop()
      held.resolve(json(receipt('3')))
      await successor
    }
    expect(f.auth.system.selection.progress).toMatchObject({
      phase: 'confirmed',
      receipt: receipt('3'),
    })
  })
})

function configuredModel(n: number, type: ModelType, providerID: number): ProviderModel {
  const value = model()
  return {
    ...value,
    id: id(n),
    provider_id: id(providerID),
    input: {
      ...value.input,
      name: `${type} ${n}`,
      type,
      capabilities: {
        ...value.input.capabilities,
        output_modalities:
          type === 'reranker'
            ? []
            : [type === 'embedding' ? 'vector' : type === 'image_generation' ? 'image' : 'text'],
        structured_output_modes: type === 'chat' ? ['json_schema'] : [],
      },
    },
  }
}
async function pageFixture(
  initial: SelectionState = {
    ...selectionValue(),
    configured: { ...configured(), reranker: id(22), image: id(23) },
  },
) {
  const f = await fixture()
  let observed = initial
  const sources = new Map<string, Provider>(
    [
      [10, 'openai-chat-completions'],
      [12, 'openai-embeddings'],
      [13, 'jina-rerank'],
      [14, 'openai-images-generations'],
    ].map(([n, protocol]) => [
      id(n as number),
      {
        ...provider(),
        id: id(n as number),
        input: {
          ...providerInput(),
          name: `Provider ${n}`,
          protocol: protocol as ProviderProtocol,
        },
      },
    ]),
  )
  const records = new Map<string, ProviderModel>(
    [
      configuredModel(20, 'embedding', 12),
      configuredModel(21, 'chat', 10),
      configuredModel(22, 'reranker', 13),
      configuredModel(23, 'image_generation', 14),
    ].map((value) => [value.id, value]),
  )
  const readReference: Fetch = async (path) => {
    const target = path.split('/').at(-1)!
    const value = path.includes('model-providers') ? sources.get(target) : records.get(target)
    return value ? json(value) : problem('NOT_FOUND', 404)
  }
  f.setReferences(readReference)
  f.setPerform(async (_path, init) => {
    if (init.method === 'GET') return json(observed)
    const body = JSON.parse(init.body as string)
    observed = {
      id: body.id,
      version: String(BigInt(body.expected_version) + 1n),
      configured: {
        embedding: body.embedding,
        memory: body.memory,
        reranker: body.reranker,
        image: body.image,
      },
    }
    return json(receipt(observed.version))
  })
  f.setOther(async (path) => {
    const url = new URL(path, 'http://test.invalid')
    if (url.pathname.endsWith('/model-providers'))
      return json({
        items: [...sources.values()].sort((a, b) => b.id.localeCompare(a.id)),
        next_cursor: null,
      })
    if (url.pathname.endsWith('/models'))
      return json({
        items: [...records.values()]
          .filter((row) => row.provider_id === url.searchParams.get('provider_id'))
          .sort((a, b) => b.id.localeCompare(a.id)),
        next_cursor: null,
      })
    return json({ items: [] })
  })
  const page = createSystemModelSelection(f.auth)
  pages.push(page)
  page.afterNavigation('/system/model-selection', '/')
  page.attach()
  await flushPromises()
  return {
    ...f,
    page,
    sources,
    records,
    readReference,
    setObserved(value: SelectionState) {
      observed = value
    },
  }
}
async function choose(
  f: Awaited<ReturnType<typeof pageFixture>>,
  purpose: SelectionPurpose,
  source: number,
  target: number,
) {
  f.page.choosePurpose(purpose)
  await flushPromises()
  const provider = f.page.choices[purpose].providers.rows.find((row) => row.id === id(source))
  expect(provider).toBeTruthy()
  f.page.selectProvider(provider!)
  await flushPromises()
  const model = f.page.choices[purpose].models.rows.find((row) => row.id === id(target))
  expect(model).toBeTruthy()
  f.page.selectModel(model!)
}

describe('Selection App-lifetime controller and explicit choices', () => {
  it('keeps null/v1 unconfigured, selects only the active purpose, and saves a complete group once', async () => {
    const f = await pageFixture({ id: id(100), version: '1', configured: null })
    expect(f.page.selection.value).toEqual({ id: id(100), version: '1', configured: null })
    expect(f.references()).toHaveLength(0)
    const before = f.fetch.mock.calls.length
    f.page.openEditor()
    expect(f.page.dirty.value).toBe(false)
    expect(f.page.canSave.value).toBe(false)
    await f.page.save()
    expect(f.fetch.mock.calls).toHaveLength(before)
    await choose(f, 'embedding', 12, 20)
    expect(f.page.draft).toEqual({ embedding: id(20), memory: null, reranker: null, image: null })
    expect(f.page.canSave.value).toBe(false)
    await choose(f, 'memory', 10, 21)
    expect(f.page.canSave.value).toBe(true)
    expect(f.fetch.mock.calls.filter(([p]) => p.includes('/models?'))).toHaveLength(2)
    await f.page.save()
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
      id: id(100),
      expected_version: '1',
      ...configured(),
    })
    expect(f.page.state.confirmed).toEqual(receipt())
    expect(f.page.editor.open).toBe(false)
    expect(f.page.selection.value?.configured).toEqual(configured())
    expect(f.page.feedback.value).toBe('success')
    f.page.openEditor()
    expect(f.page.feedback.value).toBe('idle')
    expect(f.page.state.confirmed).toBeNull()
    expect(f.page.canSave.value).toBe(false)
    await f.page.save()
    expect(f.writes()).toHaveLength(1)
  })

  it.each([false, true])(
    'bounds the saved reference round to distinct Model IDs, duplicate=%s',
    async (duplicate) => {
      const bindings = {
        embedding: id(20),
        memory: duplicate ? id(20) : id(21),
        reranker: duplicate ? null : id(22),
        image: duplicate ? null : id(23),
      }
      const f = await pageFixture({ ...selectionValue(), configured: bindings })
      const count = duplicate ? 1 : 4
      expect(f.references().filter(([p]) => p.includes('/models/'))).toHaveLength(count)
      expect(f.references().filter(([p]) => p.includes('/model-providers/'))).toHaveLength(count)
      for (const purpose of ['embedding', 'memory'] as const) {
        expect(f.page.references[purpose].phase).toBe('ready')
        expect(f.page.references[purpose].value?.model.id).toBe(bindings[purpose])
      }
      expect(f.fetch.mock.calls.some(([p]) => p.includes('?'))).toBe(false)
    },
  )

  it('preserves a saved ID after exact Provider GET failure with no half-details or new PUT until explicit re-read', async () => {
    const f = await pageFixture()
    f.setReferences((path, init) =>
      path.endsWith('/' + id(12))
        ? Promise.resolve(problem('INTERNAL_ERROR'))
        : f.readReference(path, init),
    )
    await f.page.refresh()
    expect(f.page.selection.value?.configured?.embedding).toBe(id(20))
    expect(f.page.references.embedding).toMatchObject({ id: id(20), phase: 'error', value: null })
    expect(f.page.references.memory.phase).toBe('ready')
    f.page.openEditor()
    f.page.clearOptional('reranker')
    expect(f.page.changed.value).toBe(true)
    expect(f.page.canSave.value).toBe(false)
    await f.page.save()
    expect(f.writes()).toHaveLength(0)
    f.setReferences(f.readReference)
    const before = f.references().length
    await f.page.retryReference('embedding')
    expect(f.references().length - before).toBe(2)
    expect(f.page.draft.embedding).toBe(id(20))
    expect(f.page.canSave.value).toBe(true)
  })

  it.each(['model-disabled', 'provider-disabled', 'memory-capability'] as const)(
    'retains a now-ineligible current binding after %s',
    async (kind) => {
      const f = await pageFixture()
      if (kind === 'provider-disabled') {
        const value = f.sources.get(id(10))!
        f.sources.set(value.id, { ...value, input: { ...value.input, enabled: false } })
      } else {
        const value = f.records.get(id(21))!
        f.records.set(value.id, {
          ...value,
          input: {
            ...value.input,
            enabled: kind !== 'model-disabled',
            capabilities: {
              ...value.input.capabilities,
              structured_output_modes: kind === 'memory-capability' ? ['text'] : ['json_schema'],
            },
          },
        })
      }
      await f.page.refresh()
      expect(f.page.references.memory.phase).toBe('ready')
      expect(f.page.references.memory.id).toBe(id(21))
      expect(selectionReason('memory', f.page.references.memory.value!)).not.toBe('')
      f.page.openEditor()
      f.page.clearOptional('image')
      expect(f.page.draft.memory).toBe(id(21))
      expect(f.page.canSave.value).toBe(false)
      await f.page.save()
      expect(f.writes()).toHaveLength(0)
    },
  )

  it('updates selected draft evidence from an exact disabled Provider, preserving IDs and version until an explicit valid reread', async () => {
    const f = await pageFixture()
    f.records.set(id(25), configuredModel(25, 'chat', 10))
    f.page.openEditor()
    await choose(f, 'memory', 10, 25)
    const draft = { ...f.page.draft }
    expect(f.page.canSave.value).toBe(true)
    const source = f.sources.get(id(10))!
    f.setReferences((path, init) =>
      path.endsWith('/' + source.id)
        ? Promise.resolve(
            json({ ...source, version: '2', input: { ...source.input, enabled: false } }),
          )
        : f.readReference(path, init),
    )
    f.page.choosePurpose('memory')
    await flushPromises()
    f.page.selectProvider(f.page.choices.memory.providers.rows.find((row) => row.id === source.id)!)
    await flushPromises()
    expect(f.page.draftDetails.memory?.provider).toMatchObject({
      version: '2',
      input: { enabled: false },
    })
    expect(f.page.references.memory.value?.provider.input.enabled).toBe(false)
    expect(f.page.canSave.value).toBe(false)
    await f.page.retryReference('reranker')
    expect(f.page.canSave.value).toBe(false)
    expect(f.page.draft).toEqual(draft)
    expect(f.page.editor.version).toBe('1')
    await f.page.save()
    expect(f.writes()).toHaveLength(0)
    f.setReferences(f.readReference)
    f.page.retryProvider()
    await flushPromises()
    expect(f.page.canSave.value).toBe(true)
    expect(f.page.draft).toEqual(draft)
    await f.page.save()
    expect(JSON.parse(f.writes()[0]![1].body as string)).toMatchObject({
      expected_version: '1',
      memory: id(25),
    })
  })

  it.each(['model-disabled', 'memory-capability'] as const)(
    'uses a completed same-ID Model page observation of %s to block a new command',
    async (kind) => {
      const f = await pageFixture()
      const original = configuredModel(25, 'chat', 10)
      f.records.set(original.id, original)
      f.page.openEditor()
      await choose(f, 'memory', 10, 25)
      const draft = { ...f.page.draft }
      f.records.set(original.id, {
        ...original,
        version: '2',
        input: {
          ...original.input,
          enabled: kind !== 'model-disabled',
          capabilities: {
            ...original.input.capabilities,
            structured_output_modes: kind === 'memory-capability' ? ['text'] : ['json_schema'],
          },
        },
      })
      f.page.choosePurpose('memory')
      await flushPromises()
      f.page.selectProvider(f.page.choices.memory.providers.rows.find((row) => row.id === id(10))!)
      await flushPromises()
      expect(f.page.draftDetails.memory?.model.version).toBe('2')
      expect(selectionReason('memory', f.page.draftDetails.memory!)).toBe(
        kind === 'model-disabled' ? 'Model 已停用' : 'Memory 需要声明 json_schema 能力',
      )
      expect(f.page.draft).toEqual(draft)
      expect(f.page.editor.version).toBe('1')
      expect(f.page.canSave.value).toBe(false)
      await f.page.save()
      expect(f.writes()).toHaveLength(0)
      f.records.set(original.id, original)
      await f.page.pageAction('models', 'refresh')
      expect(f.page.canSave.value).toBe(true)
    },
  )

  it('invalidates only exact Provider evidence on failed detail, not on page absence or an unrelated Provider', async () => {
    const f = await pageFixture()
    f.records.set(id(25), configuredModel(25, 'chat', 10))
    f.page.openEditor()
    await choose(f, 'memory', 10, 25)
    const draft = { ...f.page.draft }
    const source = f.sources.get(id(10))!
    const unrelated = { ...source, id: id(50), input: { ...source.input, enabled: false } }
    f.setOther(async (path) =>
      json({ items: path.includes('model-providers') ? [unrelated] : [], next_cursor: null }),
    )
    f.page.choosePurpose('memory')
    await flushPromises()
    expect(f.page.canSave.value).toBe(true)
    expect(f.page.draftDetails.memory?.provider.id).toBe(source.id)
    f.setOther(async (path) =>
      json({ items: path.includes('model-providers') ? [source] : [], next_cursor: null }),
    )
    await f.page.pageAction('providers', 'refresh')
    f.setReferences((path, init) =>
      path.endsWith('/' + source.id)
        ? Promise.resolve(problem('NOT_FOUND', 404))
        : f.readReference(path, init),
    )
    f.page.selectProvider(f.page.choices.memory.providers.rows[0]!)
    await flushPromises()
    expect(f.page.canSave.value).toBe(false)
    expect(f.page.draftDetails.memory).toBeNull()
    expect(f.page.references.memory).toMatchObject({ id: id(21), phase: 'error', value: null })
    expect(f.page.draft).toEqual(draft)
    expect(f.page.editor.version).toBe('1')
    await f.page.save()
    expect(f.writes()).toHaveLength(0)
  })

  it('applies only observed Provider rows and requires a fresh Model parse after protocol changes', async () => {
    const f = await pageFixture()
    f.records.set(id(25), configuredModel(25, 'chat', 10))
    f.page.openEditor()
    await choose(f, 'memory', 10, 25)
    const source = f.sources.get(id(10))!,
      draft = { ...f.page.draft }
    f.sources.set(source.id, {
      ...source,
      version: '2',
      input: { ...source.input, enabled: false },
    })
    f.page.choosePurpose('image')
    await flushPromises()
    expect(f.page.draftDetails.memory?.provider.input.enabled).toBe(false)
    expect(f.page.canSave.value).toBe(false)
    f.sources.set(source.id, {
      ...source,
      version: '3',
      input: { ...source.input, protocol: 'anthropic-messages' },
    })
    await f.page.pageAction('providers', 'refresh')
    expect(f.page.draftDetails.memory).toBeNull()
    expect(f.page.references.memory.value).toBeNull()
    expect(f.page.canSave.value).toBe(false)
    f.page.choosePurpose('memory')
    await flushPromises()
    f.page.selectProvider(f.page.choices.memory.providers.rows.find((row) => row.id === source.id)!)
    await flushPromises()
    // The unchanged Model declares json_schema, which the formal Anthropic
    // parser rejects. A changed Provider alone cannot restore a complete pair.
    expect(f.page.choices.memory.models.phase).toBe('error')
    expect(f.page.draftDetails.memory).toBeNull()
    expect(f.page.canSave.value).toBe(false)
    f.sources.set(source.id, { ...source, version: '4' })
    f.page.retryProvider()
    await flushPromises()
    expect(f.page.draftDetails.memory?.provider.input.protocol).toBe(source.input.protocol)
    expect(f.page.canSave.value).toBe(true)
    expect(f.page.draft).toEqual(draft)
    expect(f.page.editor.version).toBe('1')
    expect(f.writes()).toHaveLength(0)
  })

  it('allows declared Memory capabilities without imposing narrower Runtime restrictions and rejects wrong-purpose candidates', async () => {
    const f = await pageFixture()
    const value = f.records.get(id(21))!
    f.records.set(value.id, {
      ...value,
      input: {
        ...value.input,
        capabilities: {
          ...value.input.capabilities,
          input_modalities: ['text', 'image'],
          tool_calls: true,
          parallel_tool_calls: true,
        },
      },
    })
    await f.page.refresh()
    expect(selectionReason('memory', f.page.references.memory.value!)).toBe('')
    f.page.openEditor()
    f.page.choosePurpose('embedding')
    await flushPromises()
    f.page.selectProvider(f.page.choices.embedding.providers.rows.find((row) => row.id === id(10))!)
    await flushPromises()
    const wrong = f.page.choices.embedding.models.rows[0]!
    expect(f.page.candidateReason(wrong)).toContain('embedding')
    f.page.selectModel(wrong)
    expect(f.page.draft.embedding).toBe(id(20))
    expect(f.page.dirty.value).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('uses explicit cursor steps, hides failed rows, and restarts only on an explicit invalid-cursor action', async () => {
    const f = await pageFixture(),
      requests: string[] = []
    const first = Array.from({ length: 25 }, (_, index) => ({
      ...provider(),
      id: id(1000 - index),
    }))
    let invalid = false
    f.setOther(async (path) => {
      const cursor = new URL(path, 'http://test.invalid').searchParams.get('cursor')
      requests.push(cursor ?? 'first')
      return invalid && cursor
        ? problem('CURSOR_INVALID', 400)
        : json({ items: cursor ? [provider()] : first, next_cursor: cursor ? null : 'next-page' })
    })
    f.page.openEditor()
    f.page.choosePurpose('memory')
    await flushPromises()
    expect(requests).toEqual(['first'])
    expect(f.page.choices.memory.providers.rows).toHaveLength(25)
    await f.page.pageAction('providers', 'next')
    expect(requests).toEqual(['first', 'next-page'])
    await f.page.pageAction('providers', 'previous')
    expect(requests).toEqual(['first', 'next-page', 'first'])
    invalid = true
    await f.page.pageAction('providers', 'next')
    expect(f.page.choices.memory.providers).toMatchObject({
      phase: 'error',
      rows: [],
      cursorInvalid: true,
    })
    await flushPromises()
    expect(requests).toHaveLength(4)
    await f.page.pageAction('providers', 'retry')
    expect(requests.at(-1)).toBe('first')
    f.page.choosePurpose('embedding')
    await flushPromises()
    expect(requests.at(-1)).toBe('first')
    expect(f.page.activePurpose.value).toBe('embedding')
    expect(f.page.choices.embedding.providers.hasPrevious).toBe(false)
    expect(f.page.dirty.value).toBe(false)
  })

  it('partitions Model cursors by selected Provider and purpose without prefetching other purposes', async () => {
    const f = await pageFixture(),
      requests: string[] = []
    const first = Array.from({ length: 25 }, (_, index) => configuredModel(200 - index, 'chat', 10))
    f.setOther(async (path) => {
      const url = new URL(path, 'http://test.invalid')
      requests.push(url.pathname + url.search)
      if (url.pathname.endsWith('model-providers'))
        return json({
          items: [...f.sources.values()].sort((a, b) => b.id.localeCompare(a.id)),
          next_cursor: null,
        })
      const providerID = url.searchParams.get('provider_id'),
        cursor = url.searchParams.get('cursor')
      if (providerID === id(10))
        return json({
          items: cursor ? [f.records.get(id(21))!] : first,
          next_cursor: cursor ? null : 'models-next',
        })
      return json({ items: [f.records.get(id(20))!], next_cursor: null })
    })
    f.page.openEditor()
    f.page.choosePurpose('memory')
    await flushPromises()
    f.page.selectProvider(f.page.choices.memory.providers.rows.find((row) => row.id === id(10))!)
    await flushPromises()
    await f.page.pageAction('models', 'next')
    await f.page.pageAction('models', 'previous')
    expect(
      requests
        .filter((path) => path.includes('/models?'))
        .map((path) => new URL(path, 'http://test.invalid').searchParams.get('cursor')),
    ).toEqual([null, 'models-next', null])
    f.page.choosePurpose('embedding')
    await flushPromises()
    f.page.selectProvider(f.page.choices.embedding.providers.rows.find((row) => row.id === id(12))!)
    await flushPromises()
    expect(requests.at(-1)).toBe('/api/v1/system/models?provider_id=' + id(12) + '&limit=25')
    expect(f.page.choices.embedding.models.hasPrevious).toBe(false)
    expect(f.page.draft).toEqual({ ...configured(), reranker: id(22), image: id(23) })
  })

  it('cancels a stale Provider read on purpose switch and starts the one pending page only after actual join', async () => {
    const f = await pageFixture(),
      held = barrier<Response>()
    f.page.openEditor()
    f.page.choosePurpose('memory')
    await flushPromises()
    f.setReferences((path) =>
      path.endsWith('/' + id(10)) ? held.promise : Promise.resolve(json(provider())),
    )
    f.page.selectProvider(f.page.choices.memory.providers.rows.find((row) => row.id === id(10))!)
    try {
      await flushPromises()
      const before = f.fetch.mock.calls.length
      f.page.choosePurpose('embedding')
      await flushPromises()
      expect(f.auth.state.busy).toBe(true)
      expect(f.fetch.mock.calls).toHaveLength(before)
      expect(f.page.activePurpose.value).toBe('embedding')
      expect(f.page.choices.embedding.providers.phase).toBe('inactive')
    } finally {
      held.resolve(json(provider()))
      await flushPromises()
    }
    expect(f.auth.state.busy).toBe(false)
    expect(f.page.choices.memory.provider.value).toBeNull()
    expect(f.page.choices.embedding.providers.phase).toBe('ready')
    expect(f.writes()).toHaveLength(0)
  })

  it.each([false, true])(
    'defers the initial read once behind another owner and does not resume after detach=%s',
    async (detach) => {
      const f = await fixture(),
        held = barrier<Response>()
      f.setOther(() => held.promise)
      const pending = f.auth.personal.getProfile()
      await flushPromises()
      f.setPerform(async () => problem('INTERNAL_ERROR'))
      const page = createSystemModelSelection(f.auth)
      pages.push(page)
      page.afterNavigation('/system/model-selection', '/')
      page.attach()
      if (detach) page.detach()
      const before = f.fetch.mock.calls.length
      try {
        expect(f.fetch.mock.calls).toHaveLength(before)
      } finally {
        held.resolve(json({ user: view().user, avatar: null }))
        await pending
        await flushPromises()
      }
      expect(
        f.fetch.mock.calls.filter(([path]) => path === '/api/v1/system/model-selection'),
      ).toHaveLength(detach ? 0 : 1)
      if (!detach) {
        expect(page.selection.phase).toBe('error')
        await flushPromises()
        expect(
          f.fetch.mock.calls.filter(([path]) => path === '/api/v1/system/model-selection'),
        ).toHaveLength(1)
      }
    },
  )

  it('retains conflict draft IDs until explicit latest-version review, then requires new evidence for changed references', async () => {
    const f = await pageFixture()
    f.records.set(id(25), configuredModel(25, 'chat', 10))
    f.page.openEditor()
    await choose(f, 'memory', 10, 25)
    f.setPerform(async () => problem('VERSION_CONFLICT', 409, 'not_committed'))
    await f.page.save()
    expect(f.page.draft.memory).toBe(id(25))
    expect(f.page.editor.version).toBe('1')
    expect(f.page.progress.value?.phase).toBe('rejected')
    f.setPerform(async (_path, init) =>
      init.method === 'GET'
        ? json({
            ...selectionValue(),
            version: '2',
            configured: { ...configured(), reranker: id(22), image: id(23) },
          })
        : json(receipt('3')),
    )
    const review = f.page.review()
    await flushPromises()
    expect(f.page.confirmation.open).toBe(true)
    f.page.finishConfirmation(true)
    await review
    expect(f.page.draft.memory).toBe(id(25))
    expect(f.page.editor.version).toBe('2')
    expect(f.page.canSave.value).toBe(false)
    await choose(f, 'memory', 10, 25)
    expect(f.page.canSave.value).toBe(true)
    await f.page.save()
    expect(f.writes()).toHaveLength(2)
    expect(JSON.parse(f.writes()[0]![1].body as string).expected_version).toBe('1')
    expect(JSON.parse(f.writes()[1]![1].body as string).expected_version).toBe('2')
    expect(new Headers(f.writes()[1]![1].headers).get('Idempotency-Key')).not.toBe(
      new Headers(f.writes()[0]![1].headers).get('Idempotency-Key'),
    )
  })

  it('preserves strict save confirmation after GET failure and only re-reads on refresh', async () => {
    const f = await pageFixture()
    f.page.openEditor()
    f.page.clearOptional('reranker')
    f.setPerform(async (_path, init) =>
      init.method === 'PUT' ? json(receipt()) : problem('INTERNAL_ERROR'),
    )
    await f.page.save()
    expect(f.page.state.confirmed).toEqual(receipt())
    expect(f.page.state.writeMessage).toContain('已保存')
    expect(f.page.selection.phase).toBe('error')
    expect(f.page.editor.open).toBe(false)
    await f.page.refresh()
    expect(f.page.state.confirmed).toEqual(receipt())
    expect(f.writes()).toHaveLength(1)
    await f.page.retryOriginal()
    expect(f.writes()).toHaveLength(1)
  })
})
