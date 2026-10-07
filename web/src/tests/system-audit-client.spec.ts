import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, captureAuditWireQuery, type Fetch } from '../api/client'
import {
  auditFilterActions,
  auditFilterFields,
  auditFilterResourceKinds,
  captureAuditInstant,
  captureAuditQuery,
  createSystemAuditAPI,
  parseSystemAuditPage,
  type AuditQuery,
} from '../api/system-audit'
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const signal = () => new AbortController().signal
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
function row(n = 10, time = '2026-10-06T01:02:03.123456Z') {
  return {
    audit_id: id(n),
    created_at: time,
    scope: 'system',
    actor: { kind: 'human', id: id(1) },
    action: 'secret.create',
    outcome: 'success',
    resource: { kind: 'secret', id: id(2) },
    metadata: { version: '1', changed_fields: ['value'] },
    associations: {},
    summary: 'Secret created',
  }
}
const page = (items: unknown[] = [row()], next_cursor: unknown = null) => ({ items, next_cursor })
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}
function problem(status: number, code: string, commit_state = 'not_started') {
  return new Response(
    JSON.stringify({
      type: 'urn:agenteam:problem:request-failed',
      title: 'Request failed',
      status,
      detail: 'Request failed.',
      instance: '/api/v1/system/audit',
      code,
      request_id: id(99),
      commit_state,
    }),
    { status, headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(99) } },
  )
}
function invalidQuery(query: unknown) {
  try {
    captureAuditQuery(query)
    return false
  } catch (error) {
    return error instanceof Error && error.message === 'invalid-input'
  }
}

describe('System Audit two fixed GET operations', () => {
  it('sends empty filters without a bare query, and a single validated detail target', async () => {
    const fetch = vi.fn<Fetch>(async (path) => json(path.endsWith('/audit') ? page() : row())),
      api = createSystemAuditAPI(fetch)
    const current = await api.list({}, signal()),
      detail = await api.get(id(10), signal())
    expect(fetch.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      ['/api/v1/system/audit', 'GET'],
      [`/api/v1/system/audit/${id(10)}`, 'GET'],
    ])
    expect([current, current.items, current.items[0], detail].every(Object.isFrozen)).toBe(true)
    for (const [, init] of fetch.mock.calls) {
      const headers = new Headers(init.headers)
      expect(
        init.body === undefined &&
          init.credentials === 'same-origin' &&
          init.cache === 'no-store' &&
          init.redirect === 'error',
      ).toBe(true)
      expect(!headers.has('X-CSRF-Token') && !headers.has('Idempotency-Key')).toBe(true)
    }
  })
  it('captures all fourteen fields once, normalizes offsets without losing microseconds, and preserves the cursor', async () => {
    const values: Record<string, string> = {
      from: '0000-02-29T01:02:03.123456+01:02',
      to: '0099-12-31T23:59:59.999999-00:00',
      actor_kind: 'agent_run',
      actor_id: id(1),
      action: 'knowledge.delete_subtree',
      outcome: 'unknown',
      resource_kind: 'knowledge_document',
      resource_id: id(2),
      tool_id: id(3),
      execution_id: id(4),
      operation_id: id(5),
      approval_id: id(6),
      runner_id: id(7),
      agent_id: id(8),
      limit: '200',
      cursor: 'v1.a_B-c.9',
    }
    const counts = new Map<string, number>(),
      query: Record<string, unknown> = {}
    for (const [key, value] of Object.entries(values))
      Object.defineProperty(query, key, {
        enumerable: true,
        get() {
          counts.set(key, (counts.get(key) ?? 0) + 1)
          return value
        },
      })
    const gate = deferred<Response>(),
      fetch = vi.fn<Fetch>(() => gate.promise),
      pending = createSystemAuditAPI(fetch).list(query as AuditQuery, signal())
    try {
      expect(fetch).toHaveBeenCalledTimes(1)
      expect([...counts.values()].every((count) => count === 1) && counts.size === 16).toBe(true)
      const path = fetch.mock.calls[0]![0],
        parsed = new URL(path, 'https://same.invalid'),
        params = parsed.searchParams
      expect([...params.keys()].length).toBe(16)
      expect(
        params.get('from') === '0000-02-29T00:00:03.123456Z' &&
          params.get('to') === values.to.replace('-00:00', 'Z'),
      ).toBe(true)
      expect(
        auditFilterFields.every((key) => params.has(key)) && params.get('cursor') === values.cursor,
      ).toBe(true)
      expect(
        path.includes('%3A') &&
          !path.includes('%253A') &&
          new TextEncoder().encode(parsed.search.slice(1)).byteLength <= 32768,
      ).toBe(true)
    } finally {
      gate.resolve(json(page([])))
      await pending
    }
  })
  it('keeps the 53 action and 25 resource Filter domains, including project-only empty observations', async () => {
    expect(auditFilterActions.length).toBe(53)
    expect(auditFilterResourceKinds.length).toBe(25)
    const formalActions = [
      'secret.create',
      'secret.update',
      'secret.delete',
      'secret.resolve',
      'secret.master.register',
      'secret.master.rotation.start',
      'secret.master.rotation.complete',
      'secret.master.rotation.failed',
      'outbound.policy.update',
      'outbound.access.deny',
      'object.upload.complete',
      'object.upload.failed',
      'object.delete',
      'object.transfer.issue',
      'object.transfer.complete',
      'object.transfer.revoke',
      'artifact.create',
      'artifact.list',
      'artifact.read',
      'artifact.download',
      'outbox.delivery.requeue',
      'account.bootstrap',
      'account.login',
      'account.logout',
      'account.invite.create',
      'account.invite.revoke',
      'account.invite.redeem',
      'account.password.change',
      'account.password.reset.request',
      'account.password.reset.complete',
      'account.profile.update',
      'account.avatar.update',
      'account.settings.update',
      'smtp.settings.update',
      'smtp.test.request',
      'smtp.delivery',
      'smtp.delivery.retry',
      'project.create.accepted',
      'project.create.completed',
      'project.update',
      'project.archive.accepted',
      'project.archive.completed',
      'project.restore',
      'project.delete.accepted',
      'project.lifecycle.retry',
      'provider.create',
      'provider.update',
      'provider.delete',
      'model.create',
      'model.update',
      'model.delete',
      'model.selection.update',
      'knowledge.delete_subtree',
    ]
    const formalResources = [
      'secret',
      'secret_master',
      'secret_rotation',
      'outbound_policy',
      'agent',
      'stored_object',
      'object_transfer',
      'artifact',
      'artifact_collection',
      'outbox_delivery',
      'user',
      'session',
      'account_attempt',
      'invitation',
      'password_reset',
      'account_settings',
      'smtp_settings',
      'mail_job',
      'project',
      'project_operation',
      'project_creation',
      'model_provider',
      'model_config',
      'model_selection',
      'knowledge_document',
    ]
    expect(
      [...auditFilterActions].sort().join('|') === formalActions.sort().join('|') &&
        [...auditFilterResourceKinds].sort().join('|') === formalResources.sort().join('|'),
    ).toBe(true)
    for (const action of auditFilterActions)
      expect(captureAuditQuery({ action }).action === action).toBe(true)
    for (const resource_kind of auditFilterResourceKinds)
      expect(captureAuditQuery({ resource_kind }).resource_kind === resource_kind).toBe(true)
    const fetch = vi.fn<Fetch>(async () => json(page([])))
    expect(
      (
        await createSystemAuditAPI(fetch).list(
          {
            actor_kind: 'agent_run',
            action: 'project.create.accepted',
            resource_kind: 'agent',
            resource_id: id(2),
          },
          signal(),
        )
      ).items.length,
    ).toBe(0)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it('rejects invalid fields and forbidden runtime options before any request', async () => {
    const fetch = vi.fn<Fetch>(async () => json(page())),
      api = createSystemAuditAPI(fetch),
      request = accountTransport(fetch)
    const bad = [
      null,
      [],
      { scope: 'system' },
      { project_id: id(1) },
      { headers: {} },
      { limit: '' },
      { limit: '0' },
      { limit: '01' },
      { limit: '+1' },
      { limit: '1e2' },
      { limit: '1.1' },
      { limit: '201' },
      { limit: 1 },
      { cursor: 'x'.repeat(8193) },
      { cursor: 'a%2Fb' },
      { cursor: 'é' },
      { action: 'unknown' },
      { outcome: 'SUCCESS' },
      { resource_kind: 'url' },
      { actor_id: id(0xab).toUpperCase() },
      { tool_id: ' ' + id(1) },
      { actor_kind: 'service', actor_id: id(1) },
      { from: '2026-01-01T00:00:00Z', to: '2026-01-01T00:00:00Z' },
      { from: null },
    ]
    for (const query of bad)
      await expect(api.list(query as AuditQuery, signal())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    for (const target of [
      id(10) + '/x',
      '../' + id(10),
      id(10) + '?a=1',
      id(10).replace('-7000-', '-4000-'),
      '',
    ])
      await expect(api.get(target, signal())).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const extra of [
      { target: id(1) },
      { body: {} },
      { csrf: 'x' },
      { key: 'x' },
      { maximum: 2000000 },
      { audit: { scope: 'system' } },
    ])
      await expect(
        request('listSystemAudit', (v) => v, { signal: signal(), audit: {}, ...extra } as never),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('getSystemAudit', (v) => v, { signal: signal(), target: id(10), audit: {} } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('profile', (v) => v, { signal: signal(), audit: {} } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('omits only empty filters, freezes captures, accepts both page-size endpoints and validates canonical transport time independently', () => {
    expect(
      Object.keys(
        captureAuditQuery(
          Object.fromEntries([...auditFilterFields, 'cursor'].map((key) => [key, ''])),
        ),
      ).length,
    ).toBe(0)
    expect(captureAuditQuery({ limit: '1' }).limit).toBe('1')
    expect(captureAuditQuery({ limit: '200' }).limit).toBe('200')
    expect(Object.isFrozen(captureAuditQuery({ cursor: 'a'.repeat(8192) }))).toBe(true)
    for (const from of [
      '2026-01-01T00:00:00Z',
      '2026-01-01T00:00:00.000000+00:00',
      '2026-02-30T00:00:00.000000Z',
    ])
      expect(() => captureAuditWireQuery({ from })).toThrow('invalid-input')
    expect(
      invalidQuery({ from: '2026-01-01T00:00:00.000002Z', to: '2026-01-01T00:00:00.000001Z' }),
    ).toBe(true)
  })
  it.each([
    [400, 'CURSOR_INVALID'],
    [401, 'SESSION_REVOKED'],
    [403, 'PERMISSION_DENIED'],
    [404, 'NOT_FOUND'],
    [503, 'DEPENDENCY_UNAVAILABLE'],
  ] as const)(
    'preserves legal Problem status %s/code for owner classification',
    async (status, code) => {
      const fetch = vi.fn<Fetch>(async () => problem(status, code)),
        api = createSystemAuditAPI(fetch)
      await expect(api.get(id(10), signal())).rejects.toMatchObject({
        kind: 'problem',
        problem: { status, code, commit_state: 'not_started' },
      })
      expect(fetch).toHaveBeenCalledTimes(1)
    },
  )
  it('rejects wrong status, media, invalid UTF8 and wrong detail identity with no follow-up request', async () => {
    const cases = [
      () => json(row(), 201),
      () => new Response(JSON.stringify(row()), { headers: { 'Content-Type': 'text/plain' } }),
      () =>
        new Response(new Uint8Array([0xc3, 0x28]), {
          headers: { 'Content-Type': 'application/json' },
        }),
      () => new Response('{', { headers: { 'Content-Type': 'application/json' } }),
      () => json(row(11)),
    ]
    for (const make of cases) {
      const fetch = vi.fn<Fetch>(async () => make())
      await expect(createSystemAuditAPI(fetch).get(id(10), signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
      expect(fetch).toHaveBeenCalledTimes(1)
    }
  })
})

describe('complete page validation', () => {
  it('accepts 200 ordered records and checks the full microsecond then UUID tuple', () => {
    const rows = Array.from({ length: 200 }, (_, n) => row(300 - n))
    const result = parseSystemAuditPage(page(rows, 'one.next-token'), 200)
    expect(
      result.items.length === 200 &&
        result.next_cursor === 'one.next-token' &&
        Object.isFrozen(result.items),
    ).toBe(true)
    const times = [
      row(1, '0000-02-29T00:00:00.000002Z'),
      row(5, '0000-02-29T00:00:00.000001Z'),
      row(4, '0000-02-29T00:00:00.000001Z'),
    ]
    expect(parseSystemAuditPage(page(times)).items.length).toBe(3)
  })
  it('rejects the whole page for duplicate, reversed, missing/null, oversized or malformed-last records', async () => {
    const rows = Array.from({ length: 200 }, (_, n) => row(300 - n)),
      bad = [
        null,
        {},
        { items: [] },
        { items: [], next_cursor: undefined },
        page(null as never),
        page([null]),
        page([row(), row()]),
        page([row(1), row(2)]),
        page([row(2, '2026-10-06T01:02:03.000001Z'), row(1, '2026-10-06T01:02:03.000002Z')]),
        page([...rows, row(1)]),
        page([
          ...rows.slice(0, 199),
          { ...row(1), metadata: { version: '1', changed_fields: ['value'], token: 'forbidden' } },
        ]),
        { ...page(), extra: true },
      ]
    for (const value of bad)
      await expect(
        createSystemAuditAPI(async () => json(value)).list({ limit: '200' }, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('requires a full page for a bounded safe cursor and distinguishes null from missing', () => {
    for (const cursor of ['', 'x'.repeat(8193), 'é', 'x%2F', 'x/y', 42, undefined])
      expect(() => parseSystemAuditPage({ items: [row()], next_cursor: cursor }, 1)).toThrow(
        'invalid-response',
      )
    expect(() => parseSystemAuditPage(page([], 'next'), 1)).toThrow('invalid-response')
    expect(() => parseSystemAuditPage(page([row()], 'next'), 2)).toThrow('invalid-response')
    expect(parseSystemAuditPage(page([row()], 'x'.repeat(8192)), 1).next_cursor?.length).toBe(8192)
    expect(parseSystemAuditPage(page([])).items.length).toBe(0)
  })
})

// Actual ReadableStreams exercise EOF, reader.cancel and body.cancel joins;
// no Response.json mock or early resolve stands in for the native tail.
describe('Audit-only 1MiB native response budget', () => {
  it.each(['listSystemAudit', 'getSystemAudit'] as const)(
    'publishes %s only after the complete 1048576-byte EOF and releases its lock',
    async (endpoint) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>
      const encoded = new TextEncoder().encode(JSON.stringify({ pad: 'x'.repeat(1048576 - 10) }))
      expect(encoded.byteLength).toBe(1048576)
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c
          c.enqueue(encoded.subarray(0, 600000))
          c.enqueue(encoded.subarray(600000))
        },
      })
      const parser = vi.fn((value) => value),
        request = accountTransport(
          async () =>
            new Response(stream, {
              headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
            }),
        ),
        options =
          endpoint === 'listSystemAudit'
            ? { signal: signal(), audit: {} }
            : { signal: signal(), target: id(10) }
      let settled = false
      const pending = request(endpoint as 'listSystemAudit', parser, options as never).then(
        () => {
          settled = true
          return true
        },
        () => {
          settled = true
          return false
        },
      )
      try {
        await flushPromises()
        expect(settled).toBe(false)
        expect(parser).not.toHaveBeenCalled()
      } finally {
        controller.close()
        expect(await pending).toBe(true)
        expect(stream.locked).toBe(false)
      }
      expect(parser).toHaveBeenCalledTimes(1)
    },
  )
  it.each(['oversize', 'abort', 'media'] as const)(
    'joins actual %s cancellation before settling and releasing the native lock',
    async (mode) => {
      const entered = deferred(),
        joined = deferred(),
        abort = new AbortController()
      let settled = false
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          if (mode === 'oversize') c.enqueue(new Uint8Array(1048577))
          else if (mode === 'abort') c.enqueue(new TextEncoder().encode('{'))
        },
        cancel() {
          entered.resolve()
          return joined.promise
        },
      })
      const parser = vi.fn((v) => v),
        request = accountTransport(
          async () =>
            new Response(stream, {
              headers: { 'Content-Type': mode === 'media' ? 'text/plain' : 'application/json' },
            }),
        )
      const pending = request('listSystemAudit', parser, { signal: abort.signal, audit: {} }).then(
        () => {
          settled = true
          return 'success'
        },
        (error: unknown) => {
          settled = true
          return (error as { kind: string }).kind
        },
      )
      let timer: ReturnType<typeof setTimeout> | undefined,
        barrierReached = false
      try {
        if (mode === 'abort') {
          await flushPromises()
          abort.abort()
        }
        await Promise.race([
          entered.promise,
          new Promise<never>((_, reject) => {
            timer = setTimeout(() => reject(new Error('cancel barrier not reached')), 1000)
          }),
        ])
        barrierReached = true
        await flushPromises()
        expect(settled).toBe(false)
        expect(parser).not.toHaveBeenCalled()
      } finally {
        if (timer) clearTimeout(timer)
        if (!barrierReached) abort.abort()
        joined.resolve()
        expect(await pending).toBe(mode === 'abort' ? 'cancelled' : 'invalid-response')
        expect(stream.locked).toBe(false)
      }
    },
  )
  it('leaves Problem and ordinary GET at 600000 while Provider list remains 2MiB', async () => {
    for (const [endpoint, maximum, options] of [
      ['profile', 600000, {}],
      ['listProviders', 2097152, { providers: {} }],
    ] as const) {
      for (const delta of [0, 1]) {
        const response = new Response(JSON.stringify({ pad: 'x'.repeat(maximum - 10 + delta) }), {
            headers: { 'Content-Type': 'application/json' },
          }),
          request = accountTransport(async () => response)
        const pending = request(endpoint as 'profile', (v) => v, {
          signal: signal(),
          ...options,
        } as never)
        if (delta === 0) await pending
        else await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
      }
    }
    const encoded = JSON.stringify({
      type: 'urn:agenteam:problem:request-failed',
      title: 'Request failed',
      status: 400,
      detail: 'Request failed.',
      instance: '/api/v1/system/audit',
      code: 'CURSOR_INVALID',
      request_id: id(99),
      commit_state: 'not_started',
    })
    for (const bytes of [600000, 600001]) {
      const body = ' '.repeat(bytes - new TextEncoder().encode(encoded).byteLength) + encoded,
        parser = vi.fn((v) => v)
      const request = accountTransport(
        async () =>
          new Response(body, {
            status: 400,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(99) },
          }),
      )
      await expect(
        request('listSystemAudit', parser, { signal: signal(), audit: {} }),
      ).rejects.toMatchObject(
        bytes === 600000
          ? { kind: 'problem', problem: { status: 400, code: 'CURSOR_INVALID' } }
          : { kind: 'invalid-response' },
      )
      expect(parser).not.toHaveBeenCalled()
    }
  })
})

const instantVectors = [
  { input: '0000-01-01T00:00:00Z', accepted: true, canonical: '0000-01-01T00:00:00.000000Z' },
  {
    input: '0000-02-29T00:00:00.000001Z',
    accepted: true,
    canonical: '0000-02-29T00:00:00.000001Z',
  },
  {
    input: '0099-12-31T23:59:59.999999-00:00',
    accepted: true,
    canonical: '0099-12-31T23:59:59.999999Z',
  },
  {
    input: '2000-02-29T23:59:59.123456+23:59',
    accepted: true,
    canonical: '2000-02-29T00:00:59.123456Z',
  },
  {
    input: '2026-10-06T01:02:03.123456+08:30',
    accepted: true,
    canonical: '2026-10-05T16:32:03.123456Z',
  },
  {
    input: '2026-10-06T01:02:03.1-23:59',
    accepted: true,
    canonical: '2026-10-07T01:01:03.100000Z',
  },
  {
    input: '9999-12-31T23:59:59.999999Z',
    accepted: true,
    canonical: '9999-12-31T23:59:59.999999Z',
  },
  { input: '0000-01-01T00:00:00+00:01', accepted: false, canonical: '' },
  { input: '9999-12-31T23:59:59-00:01', accepted: false, canonical: '' },
  { input: '1900-02-29T00:00:00Z', accepted: false, canonical: '' },
  { input: '2026-02-30T00:00:00Z', accepted: false, canonical: '' },
  { input: '2026-01-01T24:00:00Z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:60:00Z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:60Z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00.1234567Z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00+24:00', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00+00:60', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00', accepted: false, canonical: '' },
  { input: '2026-01-01t00:00:00z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00.Z', accepted: false, canonical: '' },
  { input: '2026-01-01T00:00:00,1Z', accepted: false, canonical: '' },
  { input: '+2026-01-01T00:00:00Z', accepted: false, canonical: '' },
]
describe('fixed foundation.Instant oracle', () => {
  it.each(instantVectors.map((vector, index) => ({ ...vector, index })))(
    'matches Go Gregorian/offset/microsecond vector $index',
    ({ input, accepted, canonical }) => {
      if (accepted) expect(captureAuditInstant(input) === canonical).toBe(true)
      else expect(() => captureAuditInstant(input)).toThrow('invalid-input')
    },
  )
})
