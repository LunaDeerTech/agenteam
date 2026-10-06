import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import { createSystemAccountAPI } from '../api/system-account'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const instant = '2026-10-06T12:34:56.123456Z'
const user = (n = 40) => ({
  id: id(n),
  email: `user${n}@example.com`,
  username: `user${n}`,
  display_name: '',
  role: 'user',
  theme: 'system',
  version: '9223372036854775807',
  initial_password_suggestion: false,
  created_at: instant,
})
const full = () => Array.from({ length: 25 }, (_, i) => user(40 - i))
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(1),
    },
  })
async function parse(value: unknown) {
  return createSystemAccountAPI(async () => json(value)).listUsers({}, new AbortController().signal)
}
describe('fixed System user directory client', () => {
  it('encodes only an opaque cursor with a fixed limit and the existing same-origin policy', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ items: [] }))
    const api = createSystemAccountAPI(fetch)
    const cursor = '../?path=https://example.com/&limit=100#fragment 空格+'
    const signal = new AbortController().signal
    await api.listUsers({ cursor }, signal)
    expect(fetch).toHaveBeenCalledTimes(1)
    const [path, init] = fetch.mock.calls[0]!
    const url = new URL(path, 'http://localhost')
    expect(url.pathname).toBe('/api/v1/system/users')
    expect([...url.searchParams]).toEqual([
      ['limit', '25'],
      ['cursor', cursor],
    ])
    expect(url.hash).toBe('')
    expect(init).toEqual({
      method: 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal,
      headers: { Accept: 'application/json, application/problem+json' },
    })
  })
  it.each([
    null,
    [],
    { cursor: '' },
    { cursor: null },
    { cursor: undefined },
    { cursor: 'x'.repeat(8193) },
    { limit: 100 },
    { path: '/api/v1/session' },
    { cursor: 'signed', sort: 'email' },
  ])('rejects unsupported query input before fetch: %j', async (query) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      createSystemAccountAPI(fetch).listUsers(query as never, new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('rejects system options on old endpoints and authentication headers on the system endpoint', async () => {
    const fetch = vi.fn<Fetch>()
    const request = accountTransport(fetch),
      signal = new AbortController().signal
    await expect(
      request('session', (v) => v, { signal, users: {} } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('systemUsers', (v) => v, { signal, users: {}, csrf: 'S'.repeat(43) } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('publishes only a completely parsed frozen page with exact microseconds and a bounded cursor', async () => {
    const cursor = 'x'.repeat(8192)
    const result = await parse({ items: full(), next_cursor: cursor })
    expect(result.next_cursor).toBe(cursor)
    expect(result.items[0]).toEqual(user())
    expect(Object.isFrozen(result)).toBe(true)
    expect(Object.isFrozen(result.items)).toBe(true)
    expect(result.items.every(Object.isFrozen)).toBe(true)
    expect(await parse({ items: [] })).toEqual({ items: [] })
  })
  it.each([
    '0000-02-29T00:00:00.000001Z',
    '2000-02-29T23:59:59.999999Z',
    '9999-12-31T23:59:59.999999Z',
  ])('accepts the foundation calendar boundary %s', async (created_at) => {
    expect((await parse({ items: [{ ...user(), created_at }] })).items[0]!.created_at).toBe(
      created_at,
    )
  })
  it.each([
    '2026-02-29T12:34:56.123456Z',
    '1900-02-29T12:34:56.123456Z',
    '2026-04-31T12:34:56.123456Z',
    '2026-00-01T12:34:56.123456Z',
    '2026-13-01T12:34:56.123456Z',
    '2026-01-00T12:34:56.123456Z',
    '2026-01-01T24:00:00.000000Z',
    '2026-01-01T12:60:00.000000Z',
    '2026-01-01T12:34:60.000000Z',
    '2026-01-01T12:34:56.123Z',
    '2026-01-01T12:34:56.123456+00:00',
    '10000-01-01T00:00:00.000000Z',
  ])('rejects a noncanonical or normalized date %s', async (created_at) => {
    await expect(parse({ items: [{ ...user(), created_at }] })).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it.each([
    {},
    { items: null },
    { items: [], total: 0 },
    { items: [], next_cursor: 'x' },
    { items: full(), next_cursor: '' },
    { items: full(), next_cursor: null },
    { items: full(), next_cursor: 'x'.repeat(8193) },
    { items: Array.from({ length: 26 }, (_, n) => user(40 - n)) },
    { items: [{ ...user(), created_at: null }] },
    { items: [{ ...user(), extra: true }] },
    { items: [{ ...user(), version: '9223372036854775808' }] },
    { items: [{ ...user(), version: 1 }] },
    { items: [{ ...user(), version: '01' }] },
    { items: [{ ...user(), id: id(1).toUpperCase().replace('7000', '4000') }] },
    { items: [{ ...user(), role: 'owner' }] },
    { items: [{ ...user(), theme: 'blue' }] },
    { items: [{ ...user(), initial_password_suggestion: 'false' }] },
    { items: [{ ...user(), display_name: 'bad\nname' }] },
    { items: [user(40), user(40)] },
    { items: [user(39), user(40)] },
    { items: [user(40), { ...user(39), created_at: '2026-10-06T12:34:56.123457Z' }] },
  ])('rejects a malformed page atomically: %#', async (value) => {
    await expect(parse(value)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('compares complete microseconds before canonical IDs, not Date milliseconds', async () => {
    const rows = [{ ...user(1), created_at: '2026-10-06T12:34:56.123457Z' }, user(40)]
    expect((await parse({ items: rows })).items).toEqual(rows)
    await expect(parse({ items: [rows[1], rows[0]] })).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    const { created_at: _, ...oldUser } = user()
    await expect(parse({ items: [oldUser] })).rejects.toMatchObject({ kind: 'invalid-response' })
    const session = {
      id: id(2),
      issued_at: instant,
      idle_expires_at: instant,
      absolute_expires_at: instant,
    }
    await expect(
      createAccountAPI(async () =>
        json({ user: user(), session, csrf_token: 'S'.repeat(43) }),
      ).getSession(new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('preserves only a validated safe Problem and never an underlying exception', async () => {
    const problem = {
      type: 'urn:agenteam:problem:forbidden',
      title: 'Denied',
      status: 403,
      detail: 'safe',
      instance: '/api/v1/system/users',
      code: 'FORBIDDEN',
      request_id: id(1),
      commit_state: 'not_started',
    }
    await expect(
      createSystemAccountAPI(async () => json(problem, 403)).listUsers(
        {},
        new AbortController().signal,
      ),
    ).rejects.toMatchObject({ kind: 'problem', problem })
    const raw = new Error('private request and response')
    const result = await createSystemAccountAPI(async () => {
      throw raw
    })
      .listUsers({}, new AbortController().signal)
      .catch((e: unknown) => e)
    expect(result).toBeInstanceOf(AccountFailure)
    expect(result).toMatchObject({ kind: 'transport', message: 'transport' })
    expect(JSON.stringify(result)).not.toContain('private')
  })
  it('keeps the existing 600,000 byte JSON boundary', async () => {
    await expect(
      createSystemAccountAPI(
        async () =>
          new Response(' '.repeat(600_001), {
            headers: { 'Content-Type': 'application/json' },
          }),
      ).listUsers({}, new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
