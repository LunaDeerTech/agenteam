import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureSMTPUpdate,
  captureSMTPUnconfigure,
  createSystemSMTPSettingsAPI,
  validateSMTPPassword,
  type SMTPSettings,
  type SMTPUpdateInput,
  type SMTPUnconfigureInput,
} from '../api/system-smtp-settings'

const id = '01900000-0000-7000-8000-000000000001'
const version = '9007199254740993'
const update = (): SMTPUpdateInput => ({
  version,
  host: 'Mail.Example.',
  port: 587,
  encryption: 'starttls',
  username: ' sender ',
  sender_email: 'Sender@Example.test',
  sender_name: ' 邮件 ',
  credential_action: 'keep',
  auto_retry_count: '3',
  retry_interval_seconds: '60',
})
const unconfigure = (): SMTPUnconfigureInput => ({
  version,
  auto_retry_count: '0',
  retry_interval_seconds: '3600',
})
const settings = (current = '9007199254740994', configured = true): SMTPSettings => ({
  id,
  version: current,
  configured,
  host: configured ? 'mail.example' : '',
  port: configured ? 587 : 0,
  encryption: configured ? 'starttls' : '',
  username: configured ? ' sender ' : '',
  sender_email: configured ? 'sender@example.test' : '',
  sender_name: configured ? ' 邮件 ' : '',
  credential_present: configured,
  auto_retry_count: '3',
  retry_interval_seconds: '60',
})
const result = (current = settings()) => ({
  applied_version: '9007199254740994',
  settings: current,
})
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrfToken: 'S'.repeat(43), key: 'smtp/original:1' })
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('SMTP configuration fixed API and historical applied/current Settings', () => {
  it('uses only GET, PUT and POST/unconfigure with complete immutable captured inputs', async () => {
    const fetch = vi.fn<Fetch>(async (_, init) =>
        json(init.method === 'GET' ? settings() : result()),
      ),
      api = createSystemSMTPSettingsAPI(fetch),
      write = options(),
      original = update(),
      captured = captureSMTPUpdate(original)
    ;(original as { host: string }).host = 'changed.test'
    const read = await api.getSettings(write.signal),
      saved = await api.updateSettings(captured, write)
    await api.unconfigure(unconfigure(), write)
    expect(
      Object.isFrozen(read) &&
        Object.isFrozen(saved) &&
        Object.isFrozen(saved.settings) &&
        Object.isFrozen(captured),
    ).toBe(true)
    expect(fetch.mock.calls.map(([p, i]) => [p, i.method])).toEqual([
      ['/api/v1/system/smtp', 'GET'],
      ['/api/v1/system/smtp', 'PUT'],
      ['/api/v1/system/smtp/unconfigure', 'POST'],
    ])
    for (const [, request] of fetch.mock.calls)
      expect(request).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
    const get = fetch.mock.calls[0]![1],
      put = fetch.mock.calls[1]![1],
      post = fetch.mock.calls[2]![1]
    expect(get.body).toBeUndefined()
    expect(get.headers).not.toHaveProperty('X-CSRF-Token')
    expect(get.headers).not.toHaveProperty('Idempotency-Key')
    expect(put.body === JSON.stringify(captured)).toBe(true)
    expect(post.body === JSON.stringify(unconfigure())).toBe(true)
    expect(put.headers).toMatchObject({
      'X-CSRF-Token': write.csrfToken,
      'Idempotency-Key': write.key,
    })
  })

  it('preserves legal text and material, omits empty password, and requires explicit remove without authentication', async () => {
    const password = ' \t' + '🧩'.repeat(500) + ' ',
      captured = captureSMTPUpdate({ ...update(), password })
    expect(captured.password === password).toBe(true)
    expect(
      captured.host === update().host &&
        captured.username === update().username &&
        captured.sender_email === update().sender_email &&
        captured.sender_name === update().sender_name,
    ).toBe(true)
    expect(captureSMTPUpdate({ ...update(), password: '' })).not.toHaveProperty('password')
    expect(
      captureSMTPUpdate({ ...update(), credential_action: 'remove', username: '', password: '' }),
    ).toMatchObject({ credential_action: 'remove', username: '' })
    const unauth = { ...settings(), username: '', credential_present: false }
    expect(
      await createSystemSMTPSettingsAPI(async () => json(unauth)).getSettings(signal()),
    ).toEqual(unauth)
    for (const password of [' ', 'x'.repeat(2048), '🧩'.repeat(512), '\u0001'.repeat(2048)])
      expect(() => validateSMTPPassword(password)).not.toThrow()
    for (const password of [
      '',
      'x'.repeat(2049),
      '🧩'.repeat(513),
      '\0',
      '\r',
      '\n',
      '\ud800',
      '\udc00',
    ])
      expect(() => validateSMTPPassword(password)).toThrow(
        expect.objectContaining({ kind: 'invalid-input' }),
      )
    for (const bad of [
      { credential_action: 'replace' },
      { credential_action: 'remove' },
      { credential_action: 'remove', username: '', password: 'new' },
      { username: '', password: 'new' },
      { password: null },
      { password: undefined },
    ])
      expect(() => captureSMTPUpdate({ ...update(), ...bad } as SMTPUpdateInput)).toThrow(
        expect.objectContaining({ kind: 'invalid-input' }),
      )
  })

  it.each([
    'mail.example',
    'MAIL.Example.',
    'localhost',
    'xn--bcher-kva.test',
    '192.0.2.1',
    '2001:db8::1',
    '::ffff:192.0.2.1',
  ])('accepts formal host syntax %s without changing the captured spelling', async (host) => {
    expect(captureSMTPUpdate({ ...update(), host }).host).toBe(host)
    expect(
      (
        await createSystemSMTPSettingsAPI(async () => json({ ...settings(), host })).getSettings(
          signal(),
        )
      ).host,
    ).toBe(host)
  })
  it.each([
    'a@EXAMPLE.test',
    "x+y!#$%&'*-/=?^_`{|}~@domain",
    'a@[192.0.2.1]',
    'a@[IPv6:2001:db8::1]',
    'A@[IPv6:2001:0DB8:0000:0000:0000:0000:0000:0007]',
    'a@[ipv6:2001:db8::7]',
    'A@[IPv6:192.0.2.7]',
    'a@[ipv6:192.0.2.7]',
    'A@[IPv6:::FFFF:192.0.2.7]',
    'a@[ipv6:::ffff:192.0.2.1]',
    'a@[::ffff:192.0.2.1]',
    'a@[::ffff:c000:201]',
    'a@[0:0:0:0:0:ffff:c000:201]',
  ])('accepts bare ASCII email production forms %s', async (sender_email) => {
    expect(captureSMTPUpdate({ ...update(), sender_email }).sender_email).toBe(sender_email)
    expect(
      (
        await createSystemSMTPSettingsAPI(async () =>
          json({ ...settings(), sender_email: sender_email.toLowerCase() }),
        ).getSettings(signal())
      ).sender_email,
    ).toBe(sender_email.toLowerCase())
  })
  it('preserves the original raw email, body and options when the canonical current value is returned', async () => {
    const sender_email = 'A@[IPv6:2001:0DB8::7]',
      captured = captureSMTPUpdate({ ...update(), sender_email }),
      write = options(),
      fetch = vi.fn<Fetch>(async () =>
        json(result({ ...settings(), sender_email: sender_email.toLowerCase() })),
      ),
      api = createSystemSMTPSettingsAPI(fetch)
    for (let i = 0; i < 2; i++) {
      const saved = await api.updateSettings(captured, write)
      expect(saved.settings.sender_email).toBe('a@[ipv6:2001:0db8::7]')
    }
    expect(fetch).toHaveBeenCalledTimes(2)
    for (const [, request] of fetch.mock.calls) {
      expect(request.body === JSON.stringify(captured)).toBe(true)
      expect(request.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': write.key,
      })
    }
    expect(captured.sender_email).toBe(sender_email)
  })
  it('rejects noncanonical email in current and receipt DTOs without silently lowercasing', async () => {
    for (const sender_email of [
      'A@example.test',
      'a@EXAMPLE.test',
      'a@[IPv6:2001:db8::7]',
      'a@[iPv6:2001:db8::7]',
      'A@[ipv6:2001:db8::7]',
      'a@[ipv6:2001:DB8::7]',
      'a@[::FFFF:c000:207]',
    ]) {
      const current = { ...settings(), sender_email }
      await expect(
        createSystemSMTPSettingsAPI(async () => json(current)).getSettings(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      await expect(
        createSystemSMTPSettingsAPI(async () => json(result(current))).updateSettings(
          update(),
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('rejects ambiguous hosts, invalid email forms, controls and malformed Unicode before any dispatch', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createSystemSMTPSettingsAPI(fetch)
    const bad: Record<string, unknown>[] = [
      ...[
        '',
        'https://mail.test',
        'name@host',
        'host/path',
        'host?x',
        'host#x',
        '[::1]',
        'fe80::1%lo',
        '127.1',
        '0177.0.0.1',
        '0x7f.0.0.1',
        'host.0x',
        'host.123',
        'bad_host',
        '-bad.test',
        'bad-.test',
        'a..test',
        'a'.repeat(64) + '.test',
        'é.test',
        'host ',
      ].map((host) => ({ host })),
      ...[
        ' a@b',
        'a@b ',
        'a b@c',
        'Name<a@b>',
        'a(comment)@b',
        '"a"@b',
        'a@@b',
        'a..b@c',
        'a@[::1]',
        'a@[::192.0.2.1]',
        'a@[::ffff:0:1:2]',
        'a@[01.2.3.4]',
        'a@[IPv6:fe80::1%lo]',
        'a@[iPv6:2001:db8::7]',
        'a@[IPV6:2001:db8::7]',
        'A@[ipv6:2001:db8::7]',
        'a@[ipv6:2001:DB8::7]',
        'a@[ipv6:::FFFF:192.0.2.7]',
      ].map((sender_email) => ({ sender_email })),
      ...['\0', '\n', '\t', '\u007f', '\u0085', '\ud800'].map((username) => ({ username })),
      { username: '🧩'.repeat(65) },
      { sender_name: 'x'.repeat(81) },
      { sender_name: '\udc00' },
      ...[0, 65536, '587', 1.5, NaN, null].map((port) => ({ port })),
      { encryption: '' },
      { encryption: 'ssl' },
      { extra: true },
    ]
    for (const change of bad)
      await expect(
        api.updateSettings({ ...update(), ...change } as SMTPUpdateInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('checks exact Settings members, configured consistency, strict numeric encodings and both retry boundaries', async () => {
    const missing = Object.keys(settings()).map((key) => {
      const v: Record<string, unknown> = { ...settings() }
      delete v[key]
      return v
    })
    const bad: unknown[] = [
      null,
      [],
      ...missing,
      { ...settings(), extra: null },
      { ...settings(), id: id.replace('-7000-', '-4000-') },
      { ...settings(), configured: 'true' },
      { ...settings(), credential_present: 1 },
      { ...settings(), username: '' },
      { ...settings(), credential_present: false },
      { ...settings('1', false), host: 'mail.test' },
      { ...settings('1', false), port: 25 },
      { ...settings('1', false), username: 'x' },
      { ...settings('1', false), encryption: 'tls' },
      ...['0', '01', '-1', '+1', '1.0', '1e1', ' 1', '9223372036854775808', 1].map((version) => ({
        ...settings(),
        version,
      })),
      ...['-1', '6', '00', '+1', '1.0', '1e0', ' 1', 1, null].map((auto_retry_count) => ({
        ...settings(),
        auto_retry_count,
      })),
      ...['9', '3601', '010', '10.0', 10].map((retry_interval_seconds) => ({
        ...settings(),
        retry_interval_seconds,
      })),
    ]
    for (const v of bad)
      await expect(
        createSystemSMTPSettingsAPI(async () => json(v)).getSettings(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const [auto_retry_count, retry_interval_seconds] of [
      ['0', '10'],
      ['5', '3600'],
    ]) {
      const v = {
        ...settings('9223372036854775807', false),
        auto_retry_count: auto_retry_count!,
        retry_interval_seconds: retry_interval_seconds!,
      }
      expect(await createSystemSMTPSettingsAPI(async () => json(v)).getSettings(signal())).toEqual(
        v,
      )
      expect(() =>
        captureSMTPUnconfigure({
          version: '9223372036854775806',
          auto_retry_count: auto_retry_count!,
          retry_interval_seconds: retry_interval_seconds!,
        }),
      ).not.toThrow()
    }
    const fetch = vi.fn<Fetch>(),
      api = createSystemSMTPSettingsAPI(fetch)
    for (const v of [
      null,
      [],
      { version },
      { ...unconfigure(), version: '9223372036854775807' },
      { ...unconfigure(), auto_retry_count: 3 },
      { ...unconfigure(), retry_interval_seconds: '09' },
      { ...unconfigure(), configured: false },
      { ...unconfigure(), password: '' },
    ])
      await expect(api.unconfigure(v as SMTPUnconfigureInput, options())).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('confirms original applied_version with newer and opposite current settings, never with an incomplete receipt', async () => {
    const api = createSystemSMTPSettingsAPI(async (_, init) =>
      json(result(settings('9007199254740998', init.method === 'POST'))),
    )
    expect((await api.updateSettings(update(), options())).settings.configured).toBe(false)
    expect((await api.unconfigure(unconfigure(), options())).settings.configured).toBe(true)
    for (const bad of [
      null,
      {},
      { applied_version: '9007199254740994' },
      { ...result(), extra: true },
      { ...result(), applied_version: '9007199254740993' },
      { ...result(), applied_version: '9007199254740995' },
      { ...result(), applied_version: 9007199254740994 },
      result(settings('9007199254740993')),
      { ...result(), settings: null },
    ])
      await expect(
        createSystemSMTPSettingsAPI(async () => json(bad)).updateSettings(update(), options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('closes all endpoint options and isolates the exact UTF-8 PUT 32 KiB / POST 16 KiB request limits', async () => {
    const fetch = vi.fn<Fetch>(async () => json({})),
      request = accountTransport(fetch),
      write = options()
    for (const extra of [
      { body: {} },
      { csrf: write.csrfToken },
      { key: write.key },
      { query: '' },
      { target: id },
      { headers: {} },
      { models: {} },
    ])
      await expect(
        request('getSMTPSettings', (v) => v, { signal: write.signal, ...extra }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const endpoint of ['updateSMTPSettings', 'unconfigureSMTP'] as const)
      for (const extra of [
        { target: id },
        { query: '' },
        { csrf: '' },
        { key: '' },
        { method: 'GET' },
      ])
        await expect(
          request(endpoint, (v) => v, {
            signal: write.signal,
            csrf: write.csrfToken,
            key: write.key,
            body: {},
            ...extra,
          }),
        ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    for (const [endpoint, max] of [
      ['updateSMTPSettings', 32768],
      ['unconfigureSMTP', 16384],
      ['updateAccountSecurity', 16384],
      ['updateModelSelection', 16384],
    ] as const) {
      const body = { value: '🧩' + 'x'.repeat(max - 16) }
      expect(new TextEncoder().encode(JSON.stringify(body)).byteLength).toBe(max)
      const invoke = (body: unknown) => {
        const options = { signal: write.signal, csrf: write.csrfToken, key: write.key, body }
        if (endpoint === 'updateSMTPSettings') return request(endpoint, () => true, options)
        if (endpoint === 'unconfigureSMTP') return request(endpoint, () => true, options)
        if (endpoint === 'updateAccountSecurity') return request(endpoint, () => true, options)
        return request(endpoint, () => true, options)
      }
      await invoke(body)
      const count = fetch.mock.calls.length
      await expect(invoke({ value: body.value + 'x' })).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      expect(fetch).toHaveBeenCalledTimes(count)
    }
  })

  it.each(['get', 'put', 'unconfigure'] as const)(
    'joins %s native EOF and accepts exactly 600000 response bytes',
    async (mode) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>
      const stream = new ReadableStream<Uint8Array>({
          start(c) {
            controller = c
          },
        }),
        value = mode === 'get' ? settings() : result(),
        encoded = new TextEncoder().encode(JSON.stringify(value)),
        bytes = new Uint8Array(600000)
      bytes.fill(32)
      bytes.set(encoded)
      const api = createSystemSMTPSettingsAPI(
        async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
      )
      let settled = false
      const read = (
        mode === 'get'
          ? api.getSettings(signal())
          : mode === 'put'
            ? api.updateSettings(update(), options())
            : api.unconfigure(unconfigure(), options())
      ).then((v) => {
        settled = true
        return v
      })
      controller.enqueue(bytes)
      await flushPromises()
      expect(settled).toBe(false)
      controller.close()
      expect(await read).toEqual(value)
    },
  )
  it.each(['overflow', 'abort', 'media'] as const)(
    'does not settle until actual native %s cancellation joins',
    async (mode) => {
      const tail = deferred(),
        cancel = vi.fn(() => tail.promise),
        abort = new AbortController()
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          if (mode === 'overflow') c.enqueue(new Uint8Array(600001))
        },
        cancel,
      })
      const api = createSystemSMTPSettingsAPI(
        async () =>
          new Response(stream, {
            headers: { 'Content-Type': mode === 'media' ? 'text/plain' : 'application/json' },
          }),
      )
      let settled = false
      const read = api
        .updateSettings(update(), { ...options(), signal: abort.signal })
        .catch((e) => {
          settled = true
          return e
        })
      await flushPromises()
      if (mode === 'abort') abort.abort()
      await flushPromises()
      expect(cancel).toHaveBeenCalledTimes(1)
      expect(settled).toBe(false)
      tail.resolve()
      expect(await read).toMatchObject({
        kind: mode === 'abort' ? 'cancelled' : 'invalid-response',
      })
    },
  )
  it('rejects status, malformed UTF-8, truncated JSON and unsafe write options without retaining input material in errors', async () => {
    for (const response of [
      new Response(null, { status: 204 }),
      new Response('{', { headers: { 'Content-Type': 'application/json' } }),
      new Response(new Uint8Array([255]), { headers: { 'Content-Type': 'application/json' } }),
    ])
      await expect(
        createSystemSMTPSettingsAPI(async () => response).updateSettings(
          { ...update(), password: 'synthetic-only' },
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const fetch = vi.fn<Fetch>(),
      api = createSystemSMTPSettingsAPI(fetch)
    const failure = await api
      .updateSettings({ ...update(), password: 'synthetic-only' }, { ...options(), key: '' })
      .catch((e) => e)
    expect(failure.kind).toBe('invalid-input')
    expect(JSON.stringify(failure).includes('synthetic-only')).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })
})
