import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createSystemInvitationAPI } from '../api/system-invitations'
const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const delivery = () => ({
  job_id: id(400),
  accepted_at: time,
  phase: 'sent',
  attempts: '1',
  version: '4',
  channel: 'backend_log',
  attempt_result: 'sent',
  reason: 'sent',
})
const row = (n = 100) => ({
  id: id(n),
  email: `invite${n}@example.com`,
  version: '2',
  created_at: time,
  expires_at: time,
  latest_delivery: delivery(),
})
const full = () => Array.from({ length: 25 }, (_, n) => row(100 - n))
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const options = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'command/key:1',
  signal: new AbortController().signal,
})
const parse = (value: unknown) =>
  createSystemInvitationAPI(async () => json(value)).listInvitations(
    {},
    new AbortController().signal,
  )
describe('strict fixed System invitations transport', () => {
  it.each([
    'a@[ipv6:2001:db8::1]',
    'a@[ipv6:::ffff:192.0.2.1]',
    'a@[::ffff:192.0.2.1]',
    'a@[::ffff:c000:201]',
    'a@[0:0:0:0:0:ffff:192.0.2.1]',
    'a@[0:0:0:0:0:ffff:c000:0201]',
    'a@[0:0:0:0::ffff:c000:201]',
    'a@[::ffff:0:0]',
    'a@[::ffff:ffff:ffff]',
    'a@[::ffff:0.0.0.0]',
    'a@[192.0.2.1]',
    'a@[ipv6:192.0.2.1]',
    'a@[ipv6:::]',
    'a@[ipv6:::192.0.2.1]',
    'a@[ipv6:0:0:0:0:0:ffff:c000:201]',
    'a@999.999.999.999',
    "x+y!#$%&'*-/=?^_`{|}~@local",
    'a@local',
  ])('accepts canonical producer address %s without changing it', async (email) => {
    expect((await parse({ items: [{ ...row(), email }] })).items[0]!.email).toBe(email)
  })
  it.each([
    'a@[ipv6:bad]',
    'a@[ipv6:2001:::1]',
    'a@[999.0.0.1]',
    'a@[01.2.3.4]',
    'a@[2001:db8::1]',
    'a@[::]',
    'a@[::192.0.2.1]',
    'a@[::c000:201]',
    'a@[::ffff:0:1:2]',
    'a@[0:0:0:0:ffff:0:c000:201]',
    'a@[::ffff:192.00.2.1]',
    'a@[::ffff:192.0.2.256]',
    'a@[ipv6:::ffff:192.00.2.1]',
    'a@[ipv6:fe80::1%eth0]',
    'a@[127.1]',
    'a@[0x7f.0.0.1]',
    'a:b@example.com',
    'a..b@example.com',
    'a@[IPv6:2001:db8::1]',
    'a@example.com.',
  ])('still rejects noncanonical or malformed address %s', async (email) => {
    await expect(parse({ items: [{ ...row(), email }] })).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('encodes only an opaque cursor, uses one fixed GET and no command headers', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ items: [] }))
    const signal = new AbortController().signal,
      cursor = '../?limit=90&path=https://example.com/#空+'
    await createSystemInvitationAPI(fetch).listInvitations({ cursor }, signal)
    const [path, init] = fetch.mock.calls[0]!
    expect([...new URL(path, 'http://localhost').searchParams]).toEqual([
      ['limit', '25'],
      ['cursor', cursor],
    ])
    expect(path.split('?')[0]).toBe('/api/v1/system/invitations')
    expect(init).toEqual({
      method: 'GET',
      signal,
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      headers: { Accept: 'application/json, application/problem+json' },
    })
  })
  it.each([
    null,
    [],
    { cursor: '' },
    { cursor: undefined },
    { cursor: null },
    { cursor: 'x'.repeat(8193) },
    { limit: 25 },
    { sort: 'email' },
  ])('rejects unsupported query before dispatch %#', async (query) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      createSystemInvitationAPI(fetch).listInvitations(query as never, options().signal),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('preserves raw email and captures target versions in fixed POST bodies', async () => {
    const fetch = vi.fn<Fetch>(async (path) =>
      path.endsWith('/revoke')
        ? new Response(null, { status: 204 })
        : json({ id: id(3), job_id: id(4), version: '7' }, path.endsWith('/resend') ? 202 : 201),
    )
    const api = createSystemInvitationAPI(fetch),
      opts = options()
    await api.createInvitation({ email: ' Foo@Example.com ' }, opts)
    await api.resendInvitation({ id: id(3), version: '9223372036854775807' }, opts)
    await api.revokeInvitation({ id: id(3), version: '2' }, opts)
    expect(fetch.mock.calls.map(([p, init]) => [p, init.body])).toEqual([
      ['/api/v1/system/invitations', JSON.stringify({ email: ' Foo@Example.com ' })],
      [
        `/api/v1/system/invitations/${id(3)}/resend`,
        JSON.stringify({ version: '9223372036854775807' }),
      ],
      [`/api/v1/system/invitations/${id(3)}/revoke`, JSON.stringify({ version: '2' })],
    ])
    for (const [, init] of fetch.mock.calls)
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': opts.csrfToken,
        'Idempotency-Key': opts.key,
      })
  })
  it('accepts a new retry JobID and an already advanced receipt version', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ job_id: id(5), version: '8' }, 202))
    expect(
      await createSystemInvitationAPI(fetch).retryDelivery(
        { job_id: id(4), version: '4' },
        options(),
      ),
    ).toEqual({ job_id: id(5), version: '8' })
    expect(fetch.mock.calls[0]![0]).toBe(`/api/v1/system/mail-jobs/${id(4)}/retry`)
    expect(fetch.mock.calls[0]![1].body).toBe('{"version":"4"}')
  })
  it.each([{ email: '' }, { email: 'x'.repeat(255) }, { email: 'a@b', role: 'admin' }])(
    'rejects invalid create input before fetch %#',
    async (value) => {
      const fetch = vi.fn<Fetch>()
      await expect(
        Promise.resolve().then(() =>
          createSystemInvitationAPI(fetch).createInvitation(value as never, options()),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it.each([
    { id: '../session', version: '1' },
    { id: id(1), version: '01' },
    { id: id(1), version: 1 },
    { id: id(1), version: '9223372036854775808' },
    { id: id(1), version: '1', path: '/api/v1/session' },
  ])('validates every target before creating a path %#', async (value) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      Promise.resolve().then(() =>
        createSystemInvitationAPI(fetch).resendInvitation(value as never, options()),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('does not extend old endpoints with target/query or permit new endpoint authority maps', async () => {
    const fetch = vi.fn<Fetch>(),
      request = accountTransport(fetch),
      signal = options().signal
    for (const extra of [{ invitations: {} }, { target: id(1) }])
      await expect(
        request('session', (v) => v, { signal, ...extra } as never),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('systemInvitations', (v) => v, {
        signal,
        invitations: {},
        csrf: 'S'.repeat(43),
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('resendSystemInvitation', (v) => v, {
        signal,
        target: id(1),
        body: {},
        headers: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('publishes a frozen page only after validating all nested projections', async () => {
    const page = await parse({ items: full(), next_cursor: 'x'.repeat(8192) })
    expect(Object.isFrozen(page) && Object.isFrozen(page.items)).toBe(true)
    expect(page.items.every((r) => Object.isFrozen(r) && Object.isFrozen(r.latest_delivery))).toBe(
      true,
    )
    expect(page.items[0]).toEqual(row())
  })
  it.each([
    {
      phase: 'enqueue_pending',
      attempts: '0',
      version: '1',
      channel: null,
      attempt_result: null,
      reason: undefined,
    },
    { phase: 'pending', attempts: '0', channel: null, attempt_result: null },
    { phase: 'cancelled', attempts: '0', channel: null, attempt_result: null },
    { phase: 'unknown', attempts: '0', channel: null, attempt_result: null },
    { phase: 'unknown', channel: 'smtp', attempt_result: 'unknown' },
    { phase: 'sending', channel: 'smtp', attempt_result: null },
  ])('retains accepted null/unknown/current attempt distinctions %#', async (change) => {
    const value = { ...delivery(), ...change }
    if (value.reason === undefined) delete value.reason
    expect(
      (await parse({ items: [{ ...row(), latest_delivery: value }] })).items[0]!.latest_delivery,
    ).toEqual(value)
  })
  it.each([
    {},
    { items: null },
    { items: [], total: 0 },
    { items: [], next_cursor: 'x' },
    { items: full(), next_cursor: '' },
    { items: full(), next_cursor: null },
    { items: [...full(), row(1)] },
    { items: [row(), row()] },
    { items: [row(1), row(2)] },
    { items: [{ ...row(), latest_delivery: null }] },
    { items: [{ ...row(), extra: true }] },
    ...[
      { phase: 'processing' },
      { channel: 'configured_smtp' },
      { channel: null },
      { channel: null, attempt_result: null },
      { phase: 'failed', channel: null, attempt_result: null },
      { attempts: '7' },
      { attempts: 1 },
      { attempts: '01' },
      { attempt_result: 'retry_wait' },
      { reason: 'raw text' },
      { version: 1 },
      { accepted_at: '2026-02-29T00:00:00.000000Z' },
      { io_joined: true },
    ].map((change) => ({ items: [{ ...row(), latest_delivery: { ...delivery(), ...change } }] })),
    ...['a@b ', 'Name <a@b>', 'FOO@example.com', 'é@example.com'].map((email) => ({
      items: [{ ...row(), email }],
    })),
  ])('rejects malformed pages atomically %#', async (value) => {
    await expect(parse(value)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('compares full microseconds and shares the accepted Gregorian parser for all three Instants', async () => {
    const newer = { ...row(1), created_at: '2026-10-06T12:34:56.123457Z' }
    expect((await parse({ items: [newer, row()] })).items).toEqual([newer, row()])
    await expect(parse({ items: [row(), newer] })).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    for (const field of ['created_at', 'expires_at'] as const) {
      expect(
        (await parse({ items: [{ ...row(), [field]: '0000-02-29T00:00:00.000001Z' }] })).items[0]![
          field
        ],
      ).toBe('0000-02-29T00:00:00.000001Z')
      await expect(
        parse({ items: [{ ...row(), [field]: '1900-02-29T00:00:00.000001Z' }] }),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('rejects wrong status, target mismatch, original retry JobID and open receipt shapes', async () => {
    for (const [receipt, status] of [
      [{ id: id(2), job_id: id(4), version: '1' }, 202],
      [{ id: id(3), job_id: id(4), version: '1', email: 'a@b' }, 202],
      [{ id: id(3), job_id: id(4), version: '1' }, 200],
    ] as const)
      await expect(
        createSystemInvitationAPI(async () => json(receipt, status)).resendInvitation(
          { id: id(3), version: '1' },
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createSystemInvitationAPI(async () =>
        json({ job_id: id(4), version: '2' }, 202),
      ).retryDelivery({ job_id: id(4), version: '1' }, options()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects a nonempty 204 body only after the actual reader cancellation joins', async () => {
    let release!: () => void
    const cancelled = new Promise<void>((r) => {
      release = r
    })
    const response = new Response(null, { status: 204 })
    const reader = {
      read: vi.fn(async () => ({ done: false, value: new Uint8Array([1]) })),
      cancel: vi.fn(() => cancelled),
      releaseLock: vi.fn(),
    }
    Object.defineProperty(response, 'body', {
      value: { getReader: () => reader, cancel: async () => undefined },
    })
    let settled = false
    const result = createSystemInvitationAPI(async () => response)
      .revokeInvitation({ id: id(3), version: '1' }, options())
      .catch((e: unknown) => {
        settled = true
        return e
      })
    await new Promise((r) => setTimeout(r, 0))
    expect(reader.cancel).toHaveBeenCalledOnce()
    expect(settled).toBe(false)
    release()
    expect(await result).toMatchObject({ kind: 'invalid-response' })
    expect(reader.releaseLock).toHaveBeenCalledOnce()
  })
})
