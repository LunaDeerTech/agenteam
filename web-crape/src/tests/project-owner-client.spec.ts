import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { captureProjectUpdate, createProjectOwnerAPI } from '../api/project-owner'

const id = '01900000-0000-7000-8000-000000000001'
const other = '01900000-0000-7000-8000-000000000002'
const instant = '2026-10-08T10:00:00.000000Z'
const full = {
  id,
  owner_user_id: id,
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: instant,
  updated_at: instant,
  archived_at: null,
}
const row = { id, name: 'Demo', description: '', lifecycle: 'active', version: '1' }
const signal = () => new AbortController().signal
const write = { csrfToken: 'S'.repeat(43), key: 'owner-test-1' }
const response = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
function client(body: unknown) {
  const fetcher = vi.fn<Fetch>(async () => response(body))
  return { api: createProjectOwnerAPI(fetcher), fetcher }
}

describe('Project Owner closed client', () => {
  it('uses exactly five requests and captures only supplied update fields', async () => {
    const bodies = [
      { items: [row], next_cursor: null },
      full,
      full,
      full,
      { state: 'committed', result: { command: 'update', project: full } },
    ]
    const fetcher = vi.fn<Fetch>(async () => response(bodies.shift()))
    const api = createProjectOwnerAPI(fetcher)
    await api.list(
      { limit: 25, lifecycle: ['archived', 'active'], cursor: 'opaque+/=字' },
      signal(),
    )
    await api.resolve({ username: 'Admin', project_name: 'DeMo' }, id, signal())
    await api.get(id, id, signal())
    await api.update(id, id, { expected_version: '1', description: '' }, write)
    await api.lookup(id, id, { expected_version: '1', description: '' }, write)
    expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      [
        '/api/v1/projects?limit=25&lifecycle=active%2Carchived&cursor=opaque%2B%2F%3D%E5%AD%97',
        'GET',
      ],
      ['/api/v1/projects/resolve?username=admin&project_name=demo', 'GET'],
      [`/api/v1/projects/${id}`, 'GET'],
      [`/api/v1/projects/${id}`, 'PATCH'],
      [`/api/v1/projects/${id}/commands/lookup`, 'POST'],
    ])
    for (const [, init] of fetcher.mock.calls)
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
    for (const [, init] of fetcher.mock.calls.slice(0, 3)) {
      expect(init.body).toBeUndefined()
      expect(init.headers).not.toHaveProperty('X-CSRF-Token')
      expect(init.headers).not.toHaveProperty('Idempotency-Key')
    }
    expect(fetcher.mock.calls[3]![1].body).toBe('{"expected_version":"1","description":""}')
    expect(fetcher.mock.calls[4]![1].body).toBe('{"command":"update"}')
  })
  it.each([
    { id: other },
    { owner_user_id: other },
    { extra: 1 },
    { version: 1 },
    { version: '01' },
    { version: '9223372036854775808' },
    { normalized_name: 'Demo' },
    { name: '..' },
    { lifecycle: 'deleting' },
    { current_sprint_id: undefined },
    { archived_at: undefined },
    { lifecycle: 'archived' },
    { archived_at: instant },
    { created_at: '2026-02-30T00:00:00.000000Z' },
    { updated_at: '2025-01-01T00:00:00.000000Z' },
    { description: '\ud800' },
    { description: '\r' },
    { description: '字'.repeat(2731) },
  ])('rejects a complete invalid Get without publishing: %j', async (change) => {
    await expect(client({ ...full, ...change }).api.get(id, id, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('keeps Resolve deleting separate from Get and allows MaxInt64 no-op history', async () => {
    const { api } = client({ ...full, lifecycle: 'deleting' })
    expect(
      (await api.resolve({ username: 'admin', project_name: 'demo' }, id, signal())).lifecycle,
    ).toBe('deleting')
    const max = '9223372036854775807'
    expect(
      (
        await client({ ...full, version: max }).api.update(
          id,
          id,
          { expected_version: max, name: 'Demo' },
          write,
        )
      ).version,
    ).toBe(max)
  })
  it.each([
    { expected_version: '1' },
    { expected_version: '1', name: undefined },
    { expected_version: '1', description: null },
    { expected_version: '1', description: '\udfff' },
    { expected_version: '1', name: 'a/b' },
    { expected_version: '1', description: '\u007f' },
    { expected_version: '1', name: 'demo', extra: true },
  ])('rejects invalid input before network: %j', async (value) => {
    const { api, fetcher } = client(full)
    await expect(api.update(id, id, value as never, write)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetcher).not.toHaveBeenCalled()
  })
  it('preserves Unicode and whitespace and freezes captured material', () => {
    const value = captureProjectUpdate({ expected_version: '1', description: ' \t\n😀字  ' })
    expect(value.description).toBe(' \t\n😀字  ')
    expect(Object.isFrozen(value)).toBe(true)
  })
  it.each([
    { items: [row] },
    { items: [row], next_cursor: '' },
    { items: [row], next_cursor: 'next' },
    { items: [row, row], next_cursor: null },
    { items: [{ ...full }], next_cursor: null },
    { items: [{ ...row, lifecycle: 'archiving' }], next_cursor: null },
    { items: [{ ...row, lifecycle: 'deleting', operation_id: id }], next_cursor: null },
    { items: [{ ...row, operation_id: id }], next_cursor: null },
    { items: [{ ...row, version: '0' }], next_cursor: null },
  ])('rejects invalid page atomically: %j', async (body) => {
    await expect(client(body).api.list({ limit: 25 }, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('accepts exact transitional list variants in original service order', async () => {
    const items = [
      { id: other, name: 'z', lifecycle: 'deleting', version: '2', operation_id: id },
      { ...row, lifecycle: 'archiving', operation_id: other },
    ]
    const page = await client({ items, next_cursor: null }).api.list({ limit: 2 }, signal())
    expect(page.items).toEqual(items)
    expect(page.items[0]).not.toHaveProperty('description')
  })
  it.each([
    { limit: 0 },
    { limit: 101 },
    { limit: 25, lifecycle: [] },
    { limit: 25, lifecycle: ['active', 'active'] },
    { limit: 25, cursor: '\ud800' },
    { limit: 25, cursor: '字'.repeat(2731) },
    { limit: 25, extra: 'x' },
  ])('rejects invalid typed query: %j', async (query) => {
    const { api, fetcher } = client({ items: [], next_cursor: null })
    await expect(api.list(query as never, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetcher).not.toHaveBeenCalled()
  })
  it.each([
    { state: 'not_observed', result: null },
    { state: 'in_progress', extra: 1 },
    { state: 'committed', result: null },
    { state: 'committed', result: { command: 'create', project: full } },
    { state: 'committed', result: { command: 'update', project: { ...full, id: other } } },
    {
      state: 'committed',
      result: { command: 'update', project: { ...full, description: 'another intent' } },
    },
    { state: 'committed', result: { command: 'update', project: { ...full, version: '3' } } },
    {
      state: 'committed',
      result: {
        command: 'update',
        project: { ...full, lifecycle: 'archived', archived_at: instant },
      },
    },
  ])('rejects invalid lookup or mismatched historical receipt: %j', async (body) => {
    await expect(
      client(body).api.lookup(id, id, { expected_version: '1', description: '' }, write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each(['not_observed', 'in_progress'] as const)(
    'accepts exact lookup %s only',
    async (state) => {
      expect(
        await client({ state }).api.lookup(id, id, { expected_version: '1', name: 'Demo' }, write),
      ).toEqual({ state })
    },
  )
  it('accepts 100 full-size descriptions escaped as Go JSON beyond the old cap', async () => {
    const items = Array.from({ length: 100 }, (_, i) => ({
      ...row,
      id: `01900000-0000-7000-8000-${String(i + 1).padStart(12, '0')}`,
      description: '<'.repeat(8192),
    }))
    const body = JSON.stringify({ items, next_cursor: 'opaque' }).replaceAll('<', '\\u003c')
    expect(body.length).toBeGreaterThan(600_000)
    expect(body.length).toBeLessThan(5 * 1024 * 1024)
    const api = createProjectOwnerAPI(
      async () =>
        new Response(body, {
          headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
        }),
    )
    expect((await api.list({ limit: 100 }, signal())).items).toEqual(items)
    items[99]!.version = 'bad'
    await expect(
      client({ items, next_cursor: null }).api.list({ limit: 100 }, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([false, true])(
    'counts actual bytes and joins delayed cancellation (reject=%s)',
    async (rejectCancel) => {
      let finish!: () => void
      let cancelled = false,
        settled = false
      const wait = new Promise<void>((resolve, reject) => {
        finish = () => (rejectCancel ? reject(new Error('cancel failed')) : resolve())
      })
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new Uint8Array(5 * 1024 * 1024 + 1).fill(32))
        },
        cancel() {
          cancelled = true
          return wait
        },
      })
      const api = createProjectOwnerAPI(
        async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
      )
      const result = api.list({ limit: 100 }, signal()).catch((error: unknown) => {
        settled = true
        return error
      })
      for (let i = 0; i < 12; ++i) await Promise.resolve()
      expect(cancelled).toBe(true)
      expect(settled).toBe(false)
      finish()
      expect(await result).toMatchObject({ kind: 'invalid-response' })
    },
  )
  it('enforces precise response caps including the last byte', async () => {
    for (const cap of [64 * 1024, 5 * 1024 * 1024]) {
      const base = JSON.stringify(cap === 64 * 1024 ? full : { items: [], next_cursor: null })
      for (const extra of [0, 1]) {
        const api = createProjectOwnerAPI(
          async () =>
            new Response(base + ' '.repeat(cap - base.length + extra), {
              headers: { 'Content-Type': 'application/json' },
            }),
        )
        const result =
          cap === 64 * 1024 ? api.get(id, id, signal()) : api.list({ limit: 25 }, signal())
        if (extra) await expect(result).rejects.toMatchObject({ kind: 'invalid-response' })
        else await expect(result).resolves.toBeDefined()
      }
    }
  })
  it('rejects arbitrary transport options and malformed lookup before fetch', async () => {
    const fetcher = vi.fn<Fetch>()
    const request = accountTransport(fetcher)
    await expect(
      request('getOwnerProject', (v) => v, {
        signal: signal(),
        target: id,
        csrf: 'secret',
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('lookupOwnerProject', (v) => v, {
        signal: signal(),
        target: id,
        csrf: write.csrfToken,
        key: write.key,
        body: { command: 'create' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})
