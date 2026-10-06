import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureAccountSecurityUpdate,
  createSystemAccountSecurityAPI,
  type AccountSecurityUpdateInput,
} from '../api/system-account-security'

const id = '01900000-0000-7000-8000-000000000001'
const values = () => ({
  session_idle_seconds: '604800',
  session_absolute_seconds: '2592000',
  password_reset_seconds: '1800',
  challenge_after_failures: '5',
})
const update = (): AccountSecurityUpdateInput => ({ version: '9007199254740993', ...values() })
const settings = (version = '9007199254740994') => ({
  id,
  version,
  ...values(),
  lifetime_changes_apply_to: 'newly_issued_sessions_and_tokens',
})
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrfToken: 'S'.repeat(43), key: 'security/original:1' })
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
const invalidValues: [string, unknown][] = [
  ['session_idle_seconds', '899'],
  ['session_idle_seconds', '2592001'],
  ['session_absolute_seconds', '3599'],
  ['session_absolute_seconds', '7776001'],
  ['password_reset_seconds', '299'],
  ['password_reset_seconds', '7201'],
  ['challenge_after_failures', '0'],
  ['challenge_after_failures', '21'],
  ...['', '01', '+900', '-900', '9e2', '900.0', ' 900', '900 ', '９００', 900, null, false].map(
    (value): [string, unknown] => ['session_idle_seconds', value],
  ),
]

describe('Account security strict Settings and historical success', () => {
  it('uses only the two fixed operations and preserves the immutable five-field original body', async () => {
    const fetch = vi.fn<Fetch>(async () => json(settings())),
      api = createSystemAccountSecurityAPI(fetch),
      write = options(),
      original = update(),
      captured = captureAccountSecurityUpdate(original)
    ;(original as { password_reset_seconds: string }).password_reset_seconds = '3600'
    const observed = await api.getSettings(write.signal)
    const confirmed = await api.updateSettings(captured, write)
    expect(observed).toEqual(settings())
    expect(confirmed).toEqual(settings())
    expect(
      Object.isFrozen(observed) && Object.isFrozen(confirmed) && Object.isFrozen(captured),
    ).toBe(true)
    expect(fetch.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      ['/api/v1/system/account-settings', 'GET'],
      ['/api/v1/system/account-settings', 'PUT'],
    ])
    const get = fetch.mock.calls[0]![1],
      put = fetch.mock.calls[1]![1]
    for (const request of [get, put])
      expect(request).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
    expect(get.body).toBeUndefined()
    expect(get.headers).not.toHaveProperty('X-CSRF-Token')
    expect(get.headers).not.toHaveProperty('Idempotency-Key')
    expect(put.headers).toMatchObject({
      'X-CSRF-Token': write.csrfToken,
      'Idempotency-Key': write.key,
    })
    expect(put.body).toBe(JSON.stringify(update()))
  })

  it.each(invalidValues)(
    'rejects invalid canonical/range field %s=%s in input and Settings',
    async (key, value) => {
      const fetch = vi.fn<Fetch>(async () => json({ ...settings(), [key]: value })),
        api = createSystemAccountSecurityAPI(fetch)
      await expect(api.getSettings(signal())).rejects.toMatchObject({ kind: 'invalid-response' })
      fetch.mockClear()
      await expect(
        api.updateSettings({ ...update(), [key]: value }, options()),
      ).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      expect(fetch).not.toHaveBeenCalled()
    },
  )

  it('accepts both legal boundaries, equality and the maximum read-only version without rounding', async () => {
    for (const fields of [
      {
        session_idle_seconds: '900',
        session_absolute_seconds: '3600',
        password_reset_seconds: '300',
        challenge_after_failures: '1',
      },
      {
        session_idle_seconds: '2592000',
        session_absolute_seconds: '7776000',
        password_reset_seconds: '7200',
        challenge_after_failures: '20',
      },
      { ...values(), session_idle_seconds: '2592000' },
    ]) {
      const api = createSystemAccountSecurityAPI(async () =>
        json({ ...settings('9223372036854775807'), ...fields }),
      )
      expect((await api.getSettings(signal())).version).toBe('9223372036854775807')
      expect(
        (await api.updateSettings({ version: '9223372036854775806', ...fields }, options()))
          .version,
      ).toBe('9223372036854775807')
    }
    const fetch = vi.fn<Fetch>(),
      api = createSystemAccountSecurityAPI(fetch)
    for (const version of [
      '0',
      '01',
      '-1',
      '1.0',
      '1e1',
      ' 1',
      '9223372036854775807',
      '9223372036854775808',
      1,
    ])
      await expect(
        api.updateSettings({ ...update(), version } as AccountSecurityUpdateInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('rejects incomplete, extra, inconsistent and malformed Settings without partial publication', async () => {
    const missing = Object.keys(settings()).map((key) => {
      const value: Record<string, unknown> = settings()
      delete value[key]
      return value
    })
    for (const value of [
      null,
      [],
      ...missing,
      { ...settings(), extra: null },
      { ...settings(), id: id.replace('-7000-', '-4000-') },
      { ...settings(), id: 'ABC00000-0000-7000-8000-000000000001' },
      { ...settings(), lifetime_changes_apply_to: 'all_sessions' },
      { ...settings(), version: '9223372036854775808' },
      { ...settings(), version: 1 },
      { ...settings(), session_idle_seconds: '3601', session_absolute_seconds: '3600' },
    ])
      await expect(
        createSystemAccountSecurityAPI(async () => json(value)).getSettings(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const fetch = vi.fn<Fetch>(),
      api = createSystemAccountSecurityAPI(fetch)
    for (const value of [
      null,
      [],
      { ...update(), id },
      { ...update(), version: undefined },
      { ...update(), session_idle_seconds: '3601', session_absolute_seconds: '3600' },
    ])
      await expect(
        api.updateSettings(value as AccountSecurityUpdateInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('confirms only the exact original successor and all four values, independently of a newer GET', async () => {
    const api = createSystemAccountSecurityAPI(async (_, init) =>
      json(settings(init.method === 'GET' ? '9007199254740998' : '9007199254740994')),
    )
    expect((await api.getSettings(signal())).version).toBe('9007199254740998')
    expect((await api.updateSettings(update(), options())).version).toBe('9007199254740994')
    for (const value of [
      settings('9007199254740993'),
      settings('9007199254740995'),
      { ...settings(), session_idle_seconds: '900' },
      { ...settings(), session_absolute_seconds: '7776000' },
      { ...settings(), password_reset_seconds: '7200' },
      { ...settings(), challenge_after_failures: '20' },
    ])
      await expect(
        createSystemAccountSecurityAPI(async () => json(value)).updateSettings(update(), options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('rejects options outside each closed endpoint before dispatch and retains the 16 KiB request limit', async () => {
    const fetch = vi.fn<Fetch>(),
      request = accountTransport(fetch),
      write = options()
    for (const extra of [
      { body: update() },
      { csrf: write.csrfToken },
      { key: write.key },
      { target: id },
      { users: {} },
      { query: '' },
      { headers: {} },
    ])
      await expect(
        request('getAccountSecurity', (v) => v, { signal: write.signal, ...extra }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const extra of [{ target: id }, { avatar: {} }, { query: '' }, { csrf: '' }, { key: '' }])
      await expect(
        request('updateAccountSecurity', (v) => v, {
          signal: write.signal,
          csrf: write.csrfToken,
          key: write.key,
          body: update(),
          ...extra,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('updateAccountSecurity', (v) => v, {
        signal: write.signal,
        csrf: write.csrfToken,
        key: write.key,
        body: { value: '🧩'.repeat(4096) },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('waits for native EOF, accepts exactly 600000 bytes and rejects overflow after actual cancel', async () => {
    let source!: ReadableStreamDefaultController<Uint8Array>
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        source = controller
      },
    })
    const text = JSON.stringify(settings()),
      bytes = new TextEncoder().encode(text + ' '.repeat(600000 - text.length))
    const api = createSystemAccountSecurityAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    )
    let completed = false
    const read = api.getSettings(signal()).then((value) => {
      completed = true
      return value
    })
    source.enqueue(bytes)
    await flushPromises()
    expect(completed).toBe(false)
    source.close()
    expect(await read).toEqual(settings())
    const cancel = deferred(),
      cancelled = vi.fn(() => cancel.promise)
    const overflow = createSystemAccountSecurityAPI(
      async () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(new Uint8Array(600001))
            },
            cancel: cancelled,
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    let settled = false
    const rejected = overflow.getSettings(signal()).catch((error) => {
      settled = true
      return error
    })
    await flushPromises()
    expect(cancelled).toHaveBeenCalledTimes(1)
    expect(settled).toBe(false)
    cancel.resolve()
    expect(await rejected).toMatchObject({ kind: 'invalid-response' })
  })

  it.each(['abort', 'media'] as const)(
    'joins the original native %s body cancellation',
    async (mode) => {
      const tail = deferred(),
        cancel = vi.fn(() => tail.promise),
        abort = new AbortController()
      const api = createSystemAccountSecurityAPI(
        async () =>
          new Response(new ReadableStream<Uint8Array>({ cancel }), {
            headers: { 'Content-Type': mode === 'media' ? 'text/plain' : 'application/json' },
          }),
      )
      let settled = false
      const task = api.getSettings(abort.signal).catch((error) => {
        settled = true
        return error
      })
      await flushPromises()
      if (mode === 'abort') abort.abort()
      await flushPromises()
      expect(cancel).toHaveBeenCalledTimes(1)
      expect(settled).toBe(false)
      tail.resolve()
      expect(await task).toMatchObject({
        kind: mode === 'abort' ? 'cancelled' : 'invalid-response',
      })
    },
  )
})
