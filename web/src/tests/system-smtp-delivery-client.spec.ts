import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import { captureSMTPEmail } from '../api/system-smtp-settings'
import {
  captureSMTPDeliveryCommand,
  createSystemSMTPDeliveryAPI,
  type SMTPDeliveryCommand,
  type SMTPDeliveryJob,
  type SMTPDeliveryQuery,
  type SMTPRetryInput,
  type SMTPTestInput,
  type SystemSMTPDeliveryAPI,
} from '../api/system-smtp-delivery'

const id = '01900000-0000-7000-8000-000000000001'
const nextID = '01900000-0000-7000-8000-000000000002'
const requestID = '01900000-0000-7000-8000-000000000003'
const maximum = '9223372036854775807'
const recipient = 'Sender@[IPv6:2001:DB8::7]'
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrfToken: 'S'.repeat(43), key: 'smtp/test:original' })
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
const job = (change: Partial<SMTPDeliveryJob> = {}): SMTPDeliveryJob => ({
  job_id: id,
  kind: 'test',
  phase: 'sent',
  attempts: '1',
  version: '9007199254740993',
  channel: 'smtp',
  created_at: '2026-10-06T01:02:03.123456Z',
  attempt_channel: 'smtp',
  attempt_result: 'sent',
  reason: 'sent',
  ...change,
})
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}
function fullPage() {
  return Array.from({ length: 25 }, (_, index) =>
    job({ job_id: `01900000-0000-7000-8000-${(30 - index).toString(16).padStart(12, '0')}` }),
  )
}
type Mode = 'test' | 'list' | 'detail' | 'retry'
const invoke = (api: SystemSMTPDeliveryAPI, mode: Mode, abort = signal()) => {
  if (mode === 'test') return api.testSMTP({ recipient }, { ...options(), signal: abort })
  if (mode === 'list') return api.listJobs({}, abort)
  if (mode === 'detail') return api.getJob(id, abort)
  return api.retryJob({ job_id: id, version: '9007199254740993' }, { ...options(), signal: abort })
}
const successful = (mode: Mode) =>
  mode === 'test'
    ? { job_id: nextID }
    : mode === 'retry'
      ? { job_id: nextID, version: maximum }
      : mode === 'list'
        ? { items: [job()] }
        : job()

describe('SMTP delivery four fixed APIs and current-attempt facts', () => {
  it('uses exact routes/options and preserves captured input bytes without inventing receipt fields', async () => {
    const write = options(),
      fetch = vi.fn<Fetch>(async (path, init) => {
        if (init.method === 'POST')
          return json(
            path.endsWith('/test') ? { job_id: nextID } : { job_id: nextID, version: '7' },
            202,
          )
        return json(path.includes('?') ? { items: [job()] } : job())
      }),
      api = createSystemSMTPDeliveryAPI(fetch),
      original = { recipient },
      captured = captureSMTPDeliveryCommand({ kind: 'test', input: original })
    original.recipient = 'later@example.test'
    expect(captured.kind).toBe('test')
    if (captured.kind !== 'test') throw new Error('test capture required')
    const accepted = await api.testSMTP(captured.input, write),
      page = await api.listJobs({ cursor: 'opaque+/=' }, write.signal),
      detail = await api.getJob(id, write.signal),
      retried = await api.retryJob({ job_id: id, version: '9007199254740993' }, write)
    expect(fetch.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      ['/api/v1/system/smtp/test', 'POST'],
      ['/api/v1/system/mail-jobs/management?limit=25&cursor=opaque%2B%2F%3D', 'GET'],
      [`/api/v1/system/mail-jobs/${id}/management`, 'GET'],
      [`/api/v1/system/mail-jobs/${id}/retry`, 'POST'],
    ])
    expect(accepted).toEqual({ job_id: nextID })
    expect(retried).toEqual({ job_id: nextID, version: '7' })
    expect(
      [captured, captured.input, accepted, page, page.items, page.items[0], detail, retried].every(
        Object.isFrozen,
      ),
    ).toBe(true)
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
      if (init.method === 'GET') {
        expect(init.body).toBeUndefined()
        expect(init.headers).not.toHaveProperty('Idempotency-Key')
        expect(init.headers).not.toHaveProperty('X-CSRF-Token')
      } else
        expect(init.headers).toMatchObject({
          'Idempotency-Key': write.key,
          'X-CSRF-Token': write.csrfToken,
        })
    }
    expect(fetch.mock.calls[0]![1].body === JSON.stringify({ recipient })).toBe(true)
    expect(fetch.mock.calls[3]![1].body === JSON.stringify({ version: '9007199254740993' })).toBe(
      true,
    )
  })

  it.each([
    'A@EXAMPLE.test',
    'a@[192.0.2.1]',
    'A@[IPv6:2001:DB8::7]',
    'a@[ipv6:2001:db8::7]',
    'A@[IPv6:192.0.2.1]',
    'a@[ipv6:192.0.2.1]',
    'a@[::ffff:192.0.2.1]',
    'a@[0:0:0:0:0:ffff:c000:201]',
    "a+b!#$%&'*-/=?^_`{|}~@domain",
  ])('reuses the accepted mailbox qualification and preserves %s', async (value) => {
    const fetch = vi.fn<Fetch>(async () => json({ job_id: nextID }, 202)),
      api = createSystemSMTPDeliveryAPI(fetch)
    expect(captureSMTPEmail(value)).toBe(value)
    await api.testSMTP({ recipient: value }, options())
    expect(fetch.mock.calls[0]![1].body === JSON.stringify({ recipient: value })).toBe(true)
  })

  it('rejects invalid inputs before dispatch and does not retain their material in errors', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createSystemSMTPDeliveryAPI(fetch)
    for (const recipient of [
      '',
      ' a@example.test',
      'a@example.test ',
      'A@[ipv6:2001:db8::7]',
      'a@[ipv6:2001:DB8::7]',
      'a@[iPv6:2001:db8::7]',
      'a@[2001:db8::7]',
      'a@[::ffff:0:1:2]',
      'a@[192.000.2.1]',
      'a@例子.test',
      'a\n@example.test',
      'a\0@example.test',
      'a'.repeat(250) + '@test',
    ]) {
      const error = await api.testSMTP({ recipient }, options()).catch((e) => e)
      expect(error.kind).toBe('invalid-input')
      expect(Object.keys(error).sort()).toEqual(['kind', 'name', 'problem'])
    }
    for (const input of [{}, { recipient: null }, { recipient, version: '1' }])
      await expect(api.testSMTP(input as SMTPTestInput, options())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    for (const change of [
      { job_id: id.toUpperCase().replace('01900000', '0190000A') },
      { job_id: id.replace('-7000-', '-4000-') },
      { job_id: `../${id}` },
      { version: '0' },
      { version: '01' },
      { version: maximum },
      { version: '9223372036854775808' },
      { version: 1 },
      { version: null },
      { recipient },
    ])
      await expect(
        api.retryJob({ job_id: id, version: '1', ...change } as SMTPRetryInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const bad of [
      { kind: 'resend', input: {} },
      { kind: 'test', input: { recipient }, extra: true },
    ])
      expect(() => captureSMTPDeliveryCommand(bad as SMTPDeliveryCommand)).toThrow(
        expect.objectContaining({ kind: 'invalid-input' }),
      )
    for (const query of [
      { cursor: '' },
      { cursor: null },
      { cursor: '🧩'.repeat(2049) },
      { limit: 25 },
    ])
      await expect(api.listJobs(query as SMTPDeliveryQuery, signal())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    await expect(api.getJob(id + '/management', signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    for (const change of [{ csrfToken: '' }, { key: '' }, { key: '\n' }, { headers: {} }])
      await expect(api.testSMTP({ recipient }, { ...options(), ...change })).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('requires exact 202 receipts and treats retry version as the new current cycle version', async () => {
    for (const value of [
      null,
      {},
      { job_id: id, version: '1' },
      { job_id: 'bad' },
      { job_id: id, phase: 'sent' },
    ])
      await expect(
        createSystemSMTPDeliveryAPI(async () => json(value, 202)).testSMTP(
          { recipient },
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const value of [
      { job_id: id, version: '2' },
      { job_id: nextID },
      { job_id: nextID, version: '0' },
      { job_id: nextID, version: '01' },
      { job_id: nextID, version: 1 },
      { job_id: nextID, version: '9223372036854775808' },
      { job_id: nextID, version: '3', recipient },
    ])
      await expect(
        createSystemSMTPDeliveryAPI(async () => json(value, 202)).retryJob(
          { job_id: id, version: '5' },
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const version of ['1', '7', maximum])
      expect(
        await createSystemSMTPDeliveryAPI(async () =>
          json({ job_id: nextID, version }, 202),
        ).retryJob({ job_id: id, version: '5' }, options()),
      ).toEqual({ job_id: nextID, version })
    await expect(
      createSystemSMTPDeliveryAPI(async () => json({ job_id: nextID }, 200)).testSMTP(
        { recipient },
        options(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('accepts only projected current-attempt facts without equating job phase and attempt result', async () => {
    const cases: SMTPDeliveryJob[] = []
    for (const kind of ['invitation', 'password_reset', 'test'] as const)
      cases.push(job({ kind, channel: 'backend_log', attempt_channel: 'backend_log' }))
    for (const phase of ['pending', 'unknown', 'cancelled'] as const)
      cases.push(
        job({ phase, attempts: '0', channel: 'smtp', attempt_channel: null, attempt_result: null }),
      )
    for (const phase of [
      'claimed',
      'sending',
      'retry_wait',
      'sent',
      'failed',
      'unknown',
      'cancelled',
    ] as const)
      cases.push(job({ phase, attempt_result: null }))
    cases.push(job({ phase: 'cancelled', attempt_result: 'sent' }))
    cases.push(job({ phase: 'retry_wait', attempt_result: 'failed' }))
    cases.push(
      job({ phase: 'unknown', channel: null, attempt_channel: null, attempt_result: null }),
    )
    const queued = job({
      phase: 'enqueue_pending',
      attempts: '0',
      version: '1',
      attempt_channel: null,
      attempt_result: null,
    })
    const { reason: _reason, ...enqueue } = queued
    cases.push(enqueue)
    for (const value of cases)
      expect(
        await createSystemSMTPDeliveryAPI(async () => json(value)).getJob(id, signal()),
      ).toEqual(value)
  })

  it('rejects missing, unknown and inconsistent row facts as a whole response', async () => {
    const invalid: unknown[] = [null, [], { ...job(), recipient }, { ...job(), root_id: id }]
    for (const key of Object.keys(job()).filter((key) => key !== 'reason')) {
      const incomplete = { ...job() } as Record<string, unknown>
      delete incomplete[key]
      invalid.push(incomplete)
    }
    for (const change of [
      { kind: 'reset' },
      { phase: 'processing' },
      { attempts: '7' },
      { attempts: '01' },
      { attempts: 1 },
      { version: '0' },
      { version: maximum + '0' },
      { channel: 'log' },
      { channel: null },
      { attempt_channel: 'log' },
      { attempt_channel: 'backend_log' },
      { attempts: '0' },
      { attempt_result: 'retrying' },
      { attempt_channel: null },
      { phase: 'failed', attempt_channel: null, attempt_result: null },
      {
        phase: 'enqueue_pending',
        attempt_channel: null,
        attempt_result: null,
        attempts: '0',
        version: '1',
      },
      { reason: '' },
      { reason: null },
      { reason: 'server_exception' },
      { created_at: '2026-02-29T01:02:03.123456Z' },
      { created_at: '2026-10-06T01:02:03.123Z' },
      { created_at: '2026-10-06T01:02:03.123456+00:00' },
    ])
      invalid.push({ ...job(), ...change })
    for (const value of invalid) {
      await expect(
        createSystemSMTPDeliveryAPI(async () => json(value)).getJob(id, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      await expect(
        createSystemSMTPDeliveryAPI(async () => json({ items: [value] })).listJobs({}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(
      createSystemSMTPDeliveryAPI(async () => json(job({ job_id: nextID }))).getJob(id, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('keeps complete microsecond ordering, Gregorian years and opaque byte-bounded cursors', async () => {
    for (const created_at of ['0000-02-29T00:00:00.000001Z', '0099-12-31T23:59:59.999999Z'])
      expect(
        (
          await createSystemSMTPDeliveryAPI(async () => json(job({ created_at }))).getJob(
            id,
            signal(),
          )
        ).created_at,
      ).toBe(created_at)
    const items = [
      job({ job_id: id, created_at: '2026-10-06T01:02:03.123456Z' }),
      job({ job_id: nextID, created_at: '2026-10-06T01:02:03.123455Z' }),
    ]
    expect(
      (await createSystemSMTPDeliveryAPI(async () => json({ items })).listJobs({}, signal())).items,
    ).toEqual(items)
    const cursor = '🧩'.repeat(2048)
    expect(
      (
        await createSystemSMTPDeliveryAPI(async () =>
          json({ items: fullPage(), next_cursor: cursor }),
        ).listJobs({ cursor }, signal())
      ).next_cursor,
    ).toBe(cursor)
    expect(
      await createSystemSMTPDeliveryAPI(async () => json({ items: [] })).listJobs({}, signal()),
    ).toEqual({ items: [] })
    for (const value of [
      { items: [...items].reverse() },
      { items: [job(), job()] },
      { items: [job(), job({ job_id: nextID })] },
      { items: fullPage().concat(job()) },
      { items: [], next_cursor: 'opaque' },
      { items: fullPage(), next_cursor: null },
      { items: fullPage(), next_cursor: '' },
      { items: fullPage(), next_cursor: cursor + 'a' },
      { items: [], total: 0 },
      { items: null },
    ])
      await expect(
        createSystemSMTPDeliveryAPI(async () => json(value)).listJobs({}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('closes the three added transport option shapes and retains the 16 KiB request boundary', async () => {
    const fetch = vi.fn<Fetch>(async () => json({}, 202)),
      request = accountTransport(fetch),
      write = options()
    for (const extra of [
      { body: {} },
      { csrf: write.csrfToken },
      { key: write.key },
      { target: id },
      { models: {} },
      { query: '' },
    ])
      await expect(
        request('listMailJobManagement', (v) => v, {
          signal: write.signal,
          mailJobs: {},
          ...extra,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const extra of [
      { body: {} },
      { mailJobs: {} },
      { key: write.key },
      { csrf: write.csrfToken },
    ])
      await expect(
        request('getMailJobManagement', (v) => v, { signal: write.signal, target: id, ...extra }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const extra of [{ target: id }, { mailJobs: {} }, { csrf: '' }, { key: '' }])
      await expect(
        request('testSMTP', (v) => v, {
          signal: write.signal,
          body: {},
          csrf: write.csrfToken,
          key: write.key,
          ...extra,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('listMailJobManagement', (v) => v, {
        signal: write.signal,
        mailJobs: { cursor: '🧩'.repeat(2049) },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    const body = { value: '🧩' + 'x'.repeat(16384 - 16) },
      wire = { signal: write.signal, csrf: write.csrfToken, key: write.key, body }
    expect(new TextEncoder().encode(JSON.stringify(body)).byteLength).toBe(16384)
    await request('testSMTP', () => true, wire)
    await expect(
      request('testSMTP', () => true, { ...wire, body: { value: body.value + 'x' } }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it.each(['test', 'list', 'detail', 'retry'] as const)(
    'joins %s native EOF at exactly 600000 bytes',
    async (mode) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>
      const stream = new ReadableStream<Uint8Array>({
          start(c) {
            controller = c
          },
        }),
        value = successful(mode),
        bytes = new Uint8Array(600000)
      bytes.fill(32)
      bytes.set(new TextEncoder().encode(JSON.stringify(value)))
      const api = createSystemSMTPDeliveryAPI(
        async () =>
          new Response(stream, {
            status: mode === 'test' || mode === 'retry' ? 202 : 200,
            headers: { 'Content-Type': 'application/json' },
          }),
      )
      let settled = false
      const result = invoke(api, mode).then((value) => {
        settled = true
        return value
      })
      controller.enqueue(bytes)
      try {
        await flushPromises()
        expect(settled).toBe(false)
      } finally {
        controller.close()
      }
      expect(await result).toEqual(value)
    },
  )

  it.each(['test', 'list', 'detail', 'retry'] as const)(
    'holds %s through real native overflow, abort and rejected-media cancellation',
    async (mode) => {
      for (const failure of ['overflow', 'abort', 'media'] as const) {
        const tail = deferred(),
          abort = new AbortController(),
          cancel = vi.fn(() => tail.promise)
        const stream = new ReadableStream<Uint8Array>({
          start(c) {
            if (failure === 'overflow') c.enqueue(new Uint8Array(600001))
          },
          cancel,
        })
        const api = createSystemSMTPDeliveryAPI(
          async () =>
            new Response(stream, {
              status: mode === 'test' || mode === 'retry' ? 202 : 200,
              headers: { 'Content-Type': failure === 'media' ? 'text/plain' : 'application/json' },
            }),
        )
        let settled = false
        const result = invoke(api, mode, abort.signal).catch((e) => {
          settled = true
          return e
        })
        try {
          await flushPromises()
          if (failure === 'abort') abort.abort()
          await flushPromises()
          expect(cancel).toHaveBeenCalledTimes(1)
          expect(settled).toBe(false)
        } finally {
          tail.resolve()
        }
        expect(await result).toMatchObject({
          kind: failure === 'abort' ? 'cancelled' : 'invalid-response',
        })
      }
    },
  )

  it('preserves a strict post-read 404 Problem as an error and rejects malformed success bodies', async () => {
    const value = {
      type: 'urn:agenteam:problem:not-found',
      title: 'Not found',
      status: 404,
      detail: '',
      instance: '/api/v1/system/smtp/test',
      code: 'NOT_FOUND',
      request_id: requestID,
      commit_state: 'not_started',
    }
    await expect(
      createSystemSMTPDeliveryAPI(
        async () =>
          new Response(JSON.stringify(value), {
            status: 404,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': requestID },
          }),
      ).testSMTP({ recipient }, options()),
    ).rejects.toMatchObject({
      kind: 'problem',
      problem: { code: 'NOT_FOUND', commit_state: 'not_started' },
    })
    for (const response of [
      new Response(null, { status: 204 }),
      new Response('{', { status: 202, headers: { 'Content-Type': 'application/json' } }),
      new Response(new Uint8Array([255]), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      }),
    ])
      await expect(
        createSystemSMTPDeliveryAPI(async () => response).testSMTP({ recipient }, options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
