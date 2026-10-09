import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import {
  auditFilterActions,
  auditFilterFields,
  auditFilterResourceKinds,
  captureAuditInstant,
  captureAuditQuery,
  createProjectAuditAPI,
  parseProjectAuditPage,
  type AuditQuery,
} from '../api/project-audit'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1)
const signal = () => new AbortController().signal
const json = (value: unknown, status = 200, media = 'application/json') =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': media } })
function row(n = 10, created_at = '2026-10-08T12:00:00.123456Z') {
  return {
    audit_id: id(n),
    created_at,
    scope: 'project',
    project_id: project,
    actor: { kind: 'human', id: id(2) },
    action: 'secret.create',
    outcome: 'success',
    resource: { kind: 'secret', id: id(3) },
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
function problem(status = 400, code = 'CURSOR_INVALID') {
  return {
    type: 'urn:agenteam:problem:request-failed',
    title: 'Request failed',
    status,
    detail: 'Request failed.',
    instance: `/api/v1/projects/${project}/audit`,
    code,
    request_id: id(99),
    commit_state: 'not_started',
  }
}

describe('Project Audit two closed current-project GETs', () => {
  it('uses both captured IDs and same-origin GET options, with no bare query or write headers', async () => {
    const fetch = vi.fn<Fetch>(async (path) => json(path.endsWith('/audit') ? page() : row()))
    const api = createProjectAuditAPI(fetch)
    const list = await api.list(project, {}, signal()),
      detail = await api.get(project, id(10), signal())
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `/api/v1/projects/${project}/audit`,
      `/api/v1/projects/${project}/audit/${id(10)}`,
    ])
    expect([list, list.items, list.items[0], detail, detail.metadata].every(Object.isFrozen)).toBe(
      true,
    )
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
      })
      expect(init.body).toBeUndefined()
      const headers = new Headers(init.headers)
      expect(headers.has('X-CSRF-Token') || headers.has('Idempotency-Key')).toBe(false)
    }
  })
  it('captures all query getters exactly once before the first await and encodes only once', async () => {
    const values: Record<string, string> = {
      from: '0000-02-29T01:02:03.123456+01:02',
      to: '9999-12-31T23:59:59.999999Z',
      actor_kind: 'agent_run',
      actor_id: id(2),
      action: 'account.login',
      outcome: 'denied',
      resource_kind: 'session',
      resource_id: id(3),
      tool_id: id(4),
      execution_id: id(5),
      operation_id: id(6),
      approval_id: id(7),
      runner_id: id(8),
      agent_id: id(9),
      limit: '200',
      cursor: 'next.a_B-c.9',
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
      fetch = vi.fn<Fetch>(() => gate.promise)
    const pending = createProjectAuditAPI(fetch).list(project, query as AuditQuery, signal())
    try {
      expect(fetch).toHaveBeenCalledTimes(1)
      expect([...counts.values()]).toEqual(Array(16).fill(1))
      const path = fetch.mock.calls[0]![0],
        url = new URL(path, 'https://same.invalid')
      expect(url.pathname).toBe(`/api/v1/projects/${project}/audit`)
      expect([...url.searchParams.keys()]).toHaveLength(16)
      expect(url.searchParams.get('from')).toBe('0000-02-29T00:00:03.123456Z')
      expect(url.searchParams.get('cursor')).toBe(values.cursor)
      expect(path.includes('%253A')).toBe(false)
      expect(new TextEncoder().encode(url.search.slice(1)).byteLength).toBeLessThanOrEqual(32768)
    } finally {
      gate.resolve(json(page([])))
      await pending
    }
  })
  it('retains all 56 action and 26 resource filters including valid System-only empty results', async () => {
    expect(auditFilterActions).toHaveLength(56)
    expect(auditFilterResourceKinds).toHaveLength(26)
    const fetch = vi.fn<Fetch>(async () => json(page([]))),
      api = createProjectAuditAPI(fetch)
    for (const action of auditFilterActions)
      expect((await api.list(project, { action }, signal())).items).toEqual([])
    for (const resource_kind of auditFilterResourceKinds)
      expect((await api.list(project, { resource_kind }, signal())).items).toEqual([])
    expect(fetch).toHaveBeenCalledTimes(82)
  })
  it.each([
    null,
    '',
    id(1).replace('-7000-', '-4000-'),
    id(1) + '/audit',
    id(1) + '\n',
    id(15).toUpperCase(),
  ])('rejects invalid target %s before fetch', async (target) => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectAuditAPI(fetch)
    await expect(api.list(target as string, {}, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(api.get(project, target as string, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    { limit: '0' },
    { limit: '201' },
    { limit: '01' },
    { limit: 50 },
    { cursor: 'x'.repeat(8193) },
    { cursor: 'a/b' },
    { cursor: '中文' },
    { cursor: 'a%2Fb' },
    { action: 'invalid' },
    { action: null },
    { resource_kind: 'execution_text' },
    { actor_kind: 'service', actor_id: id(2) },
    { project_id: project },
    { scope: 'project' },
    { from: '2025-02-29T00:00:00Z' },
    { from: '2026-10-08T00:00:00.0000001Z' },
    { from: '2026-10-08T00:00:00Z', to: '2026-10-08T00:00:00Z' },
    { from: '0000-01-01T00:00:00+00:01' },
    { to: '9999-12-31T23:59:59-00:01' },
  ])('rejects invalid closed query %j with zero fetch', async (query) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      createProjectAuditAPI(fetch).list(project, query as AuditQuery, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('rejects transport options that introduce query, body, scope, CSRF or arbitrary response caps', async () => {
    const fetch = vi.fn<Fetch>(),
      request = accountTransport(fetch)
    for (const extra of [
      { audit: {} },
      { body: {} },
      { csrf: 'x' },
      { key: 'x' },
      { limit: 99999999 },
      { scope: 'system' },
    ]) {
      await expect(
        request('getProjectAudit', (value) => value, {
          signal: signal(),
          projectID: project,
          target: id(10),
          ...extra,
        } as never),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    await expect(
      request('listProjectAudit', (value) => value, {
        signal: signal(),
        projectID: project,
        audit: {},
        target: id(10),
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('keeps offset/year/microsecond boundaries and reports related query fields', () => {
    expect(captureAuditInstant('0099-12-31T23:59:59.1-00:00')).toBe('0099-12-31T23:59:59.100000Z')
    expect(captureAuditInstant('0000-02-29T00:00:00Z')).toBe('0000-02-29T00:00:00.000000Z')
    expect(() => captureAuditQuery({ actor_kind: 'service', actor_id: id(2) })).toThrowError(
      expect.objectContaining({ fields: ['actor_kind', 'actor_id'] }),
    )
    expect(auditFilterFields).toHaveLength(14)
    expect(
      captureAuditQuery(
        Object.fromEntries([...auditFilterFields, 'cursor'].map((key) => [key, ''])),
      ),
    ).toEqual({})
  })
})

describe('all-or-nothing immutable Project Audit pages', () => {
  it('accepts empty and full 200 pages with strict microsecond and equal-time UUID order', () => {
    expect(parseProjectAuditPage(page([]), project).items).toEqual([])
    const result = parseProjectAuditPage(
      page(
        Array.from({ length: 200 }, (_, n) => row(300 - n)),
        'x'.repeat(8192),
      ),
      project,
      { limit: '200' },
    )
    expect(result.items).toHaveLength(200)
    const times = [
      row(1, '0000-02-29T00:00:00.000002Z'),
      row(3, '0000-02-29T00:00:00.000001Z'),
      row(2, '0000-02-29T00:00:00.000001Z'),
    ]
    expect(parseProjectAuditPage(page(times), project).items).toHaveLength(3)
  })
  it('rejects malformed last records, cross-project records, duplicates, order and cursor mismatches as a whole', async () => {
    const rows = Array.from({ length: 200 }, (_, n) => row(300 - n))
    const bad = [
      null,
      {},
      { items: [] },
      page(null as never),
      page([null]),
      page([row(), row()]),
      page([row(1), row(2)]),
      page([...rows, row(1)]),
      page([row(1, '2026-10-08T00:00:00.000001Z'), row(2, '2026-10-08T00:00:00.000002Z')]),
      page([...rows.slice(0, 199), { ...row(1), project_id: id(99) }]),
      page([
        ...rows.slice(0, 199),
        { ...row(1), metadata: { version: '1', changed_fields: ['value'], private: 'secret' } },
      ]),
      { ...page(), extra: true },
      page([], 'next'),
      page([row()], 'next'),
      page([row()], ''),
      page([row()], null),
    ]
    for (const [index, body] of bad.entries()) {
      const api = createProjectAuditAPI(async () => json(body))
      if (index === bad.length - 1)
        expect((await api.list(project, { limit: '200' }, signal())).items).toHaveLength(1)
      else
        await expect(api.list(project, { limit: '200' }, signal())).rejects.toMatchObject({
          kind: 'invalid-response',
        })
    }
    for (const next_cursor of ['', 'a/b', 'a%2Fb', 'é', 'x'.repeat(8193), undefined, 1])
      expect(() =>
        parseProjectAuditPage({ items: [row()], next_cursor }, project, { limit: '1' }),
      ).toThrow('invalid-response')
  })
  it('checks every wire-verifiable filter, including AgentRun or agent-resource matches', async () => {
    const record = {
      ...row(),
      actor: { kind: 'agent_run', id: id(2), project_id: project, execution_id: id(5) },
      associations: {
        tool_id: id(4),
        execution_id: id(5),
        operation_id: id(6),
        approval_id: id(7),
        runner_id: id(8),
      },
    }
    const query: AuditQuery = {
      from: '2026-10-08T12:00:00.123456Z',
      to: '2026-10-08T12:00:00.123457Z',
      actor_kind: 'agent_run',
      actor_id: id(2),
      action: 'secret.create',
      outcome: 'success',
      resource_kind: 'secret',
      resource_id: id(3),
      tool_id: id(4),
      execution_id: id(5),
      operation_id: id(6),
      approval_id: id(7),
      runner_id: id(8),
      agent_id: id(2),
    }
    const api = createProjectAuditAPI(async () => json(page([record])))
    expect((await api.list(project, query, signal())).items).toHaveLength(1)
    const wrong: Record<string, string> = {
      from: '2026-10-08T12:00:00.123457Z',
      to: '2026-10-08T12:00:00.123456Z',
      actor_kind: 'human',
      action: 'secret.update',
      outcome: 'denied',
      resource_kind: 'agent',
    }
    for (const key of auditFilterFields) {
      const mismatch = { [key]: wrong[key] ?? id(99) }
      await expect(api.list(project, mismatch, signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    const denied = {
      ...row(),
      action: 'outbound.access.deny',
      outcome: 'denied',
      resource: { kind: 'agent', id: id(9) },
      metadata: { version: '1', consumer: 'model', reason: 'permission_denied' },
      summary: 'Outbound access denied',
    }
    expect(parseProjectAuditPage(page([denied]), project, { agent_id: id(9) }).items).toHaveLength(
      1,
    )
  })
  it('never substitutes an otherwise valid detail for the requested Project or audit ID', async () => {
    const api = createProjectAuditAPI(async () => json(row()))
    await expect(api.get(project, id(11), signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    await expect(api.get(id(99), id(10), signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
})

describe('native bounded transport EOF and actual cancellation', () => {
  it.each(['listProjectAudit', 'getProjectAudit'] as const)(
    'publishes %s at exactly 1MiB only after EOF, regardless of Content-Length',
    async (endpoint) => {
      const encoded = new TextEncoder().encode(JSON.stringify({ pad: 'x'.repeat(1048576 - 10) }))
      expect(encoded.byteLength).toBe(1048576)
      let control!: ReadableStreamDefaultController<Uint8Array>
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          control = c
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
        )
      let settled = false
      const options =
        endpoint === 'listProjectAudit'
          ? { projectID: project, audit: {}, signal: signal() }
          : { projectID: project, target: id(10), signal: signal() }
      const pending = request(endpoint as 'listProjectAudit', parser, options as never).finally(
        () => {
          settled = true
        },
      )
      try {
        await flushPromises()
        expect(settled).toBe(false)
        expect(parser).not.toHaveBeenCalled()
      } finally {
        control.close()
        await pending
      }
      expect(stream.locked).toBe(false)
      expect(parser).toHaveBeenCalledTimes(1)
    },
  )
  it.each(['oversize', 'abort', 'media'] as const)(
    'joins %s reader/body cancellation before exposing rejection',
    async (mode) => {
      const entered = deferred(),
        join = deferred(),
        abort = new AbortController()
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          if (mode === 'oversize') c.enqueue(new Uint8Array(1048577))
          else if (mode === 'abort') c.enqueue(new TextEncoder().encode('{'))
        },
        cancel() {
          entered.resolve()
          return join.promise
        },
      })
      const parser = vi.fn((value) => value),
        request = accountTransport(
          async () =>
            new Response(stream, {
              headers: { 'Content-Type': mode === 'media' ? 'text/plain' : 'application/json' },
            }),
        )
      let settled = false
      const pending = request('listProjectAudit', parser, {
        projectID: project,
        audit: {},
        signal: abort.signal,
      })
        .then(
          () => 'success',
          (error: { kind: string }) => error.kind,
        )
        .finally(() => {
          settled = true
        })
      let timer: ReturnType<typeof setTimeout> | undefined
      let enteredCancellation = false
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
        enteredCancellation = true
        await flushPromises()
        expect(settled).toBe(false)
        expect(parser).not.toHaveBeenCalled()
      } finally {
        if (timer) clearTimeout(timer)
        if (!enteredCancellation) abort.abort()
        join.resolve()
        expect(await pending).toBe(mode === 'abort' ? 'cancelled' : 'invalid-response')
        expect(stream.locked).toBe(false)
      }
    },
  )
  it.each([
    'truncated',
    'invalid-utf8',
    'wrong-status',
    'wrong-media',
    'wrong-request-id',
  ] as const)('rejects complete response %s without publishing a candidate', async (kind) => {
    const bytes =
      kind === 'invalid-utf8'
        ? new Uint8Array([123, 34, 120, 34, 58, 34, 0xc0, 0xaf, 34, 125])
        : new TextEncoder().encode(
            kind === 'truncated'
              ? '{"items":['
              : JSON.stringify(kind === 'wrong-request-id' ? problem() : page()),
          )
    const response = new Response(bytes, {
      status: kind === 'wrong-status' ? 201 : kind === 'wrong-request-id' ? 400 : 200,
      headers: {
        'Content-Type':
          kind === 'wrong-media'
            ? 'text/plain'
            : kind === 'wrong-request-id'
              ? 'application/problem+json'
              : 'application/json',
        ...(kind === 'wrong-request-id' ? { 'X-Request-ID': id(98) } : {}),
      },
    })
    await expect(
      createProjectAuditAPI(async () => response).list(project, {}, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('preserves all existing success cap families and the 600000-byte Problem ceiling', async () => {
    const cases = [
      ['profile', 600000, {}],
      ['listProviders', 2097152, { providers: {} }],
      ['getSystemRuntimeInformation', 16384, {}],
      ['listOwnerProjects', 5242880, { projects: { limit: 25 } }],
      ['getOwnerProject', 65536, { target: project }],
      ['listSystemAudit', 1048576, { audit: {} }],
    ] as const
    for (const [endpoint, maximum, options] of cases)
      for (const delta of [0, 1]) {
        const response = new Response(JSON.stringify({ pad: 'x'.repeat(maximum - 10 + delta) }), {
          headers: { 'Content-Type': 'application/json' },
        })
        const pending = accountTransport(async () => response)(
          endpoint as 'profile',
          (value) => value,
          { signal: signal(), ...options } as never,
        )
        if (delta === 0) await pending
        else await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
      }
    const encoded = JSON.stringify(problem())
    for (const maximum of [600000, 600001]) {
      const response = new Response(
        ' '.repeat(maximum - new TextEncoder().encode(encoded).byteLength) + encoded,
        {
          status: 400,
          headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(99) },
        },
      )
      await expect(
        createProjectAuditAPI(async () => response).list(project, {}, signal()),
      ).rejects.toMatchObject(
        maximum === 600000
          ? { kind: 'problem', problem: { status: 400, code: 'CURSOR_INVALID' } }
          : { kind: 'invalid-response' },
      )
    }
  })
})

describe('ordinary variable records through the actual Project Audit client', () => {
  const variableRow = (change: 'create' | 'update' | 'delete') => ({
    ...row(),
    action: `project.variable.${change}`,
    summary: 'Audit event',
    resource: { kind: 'project_variable', id: id(3) },
    metadata: {
      variable_id: id(3),
      version: change === 'create' ? '1' : '2',
      changed_fields: [change === 'create' ? 'created' : change === 'delete' ? 'deleted' : 'value'],
    },
  })
  it.each(['create', 'update', 'delete'] as const)(
    'accepts the exact %s record in list and detail',
    async (change) => {
      const record = variableRow(change),
        fetch = vi.fn<Fetch>(async (path) =>
          json(path.endsWith('/audit') ? page([record]) : record),
        )
      const api = createProjectAuditAPI(fetch)
      expect((await api.list(project, {}, signal())).items[0]?.metadata).toEqual(record.metadata)
      expect((await api.get(project, id(10), signal())).action).toBe(record.action)
    },
  )
  it.each([
    { resource: { kind: 'project', id: project } },
    { resource: { kind: 'project_variable', id: id(44) } },
    {
      actor: {
        kind: 'service',
        service: 'project-lifecycle',
        cause_ref: id(8),
        project_id: project,
      },
    },
    { outcome: 'denied' },
    { associations: { request_id: id(9) } },
    { metadata: { variable_id: id(3), version: '1', changed_fields: ['value'] } },
    {
      metadata: {
        variable_id: id(3),
        version: '2',
        changed_fields: ['value'],
        value: 'must-not-be-published',
      },
    },
  ])(
    'refuses an invalid variable record without publishing an earlier valid page item',
    async (change) => {
      const bad = { ...variableRow('update'), ...change }
      const api = createProjectAuditAPI(async () => json(page([row(11), bad])))
      await expect(api.list(project, {}, signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    },
  )
})
