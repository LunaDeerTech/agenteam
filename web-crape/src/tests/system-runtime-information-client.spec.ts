import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import { parseSystemInstant } from '../api/system-account'
import {
  createSystemRuntimeInformationAPI,
  parseSystemRuntimeInformation,
  type SystemRuntimeInformation,
} from '../api/system-runtime-information'

const endpoint = 'getSystemRuntimeInformation'
const path = '/api/v1/system/runtime-information'
const zero = '0001-01-01T00:00:00.000000Z'
const requestID = '01970000-0000-7000-8000-000000000001'
const signal = () => new AbortController().signal
const encode = (text: string) => new TextEncoder().encode(text)
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
function sample() {
  return {
    observed_at: '2026-10-07T01:02:03.123456Z',
    central: { version: null, safe_reason: 'build_version_not_recorded' },
    database: {
      status: 'available',
      safe_reason: null as string | null,
      last_success: {
        checked_at: '2026-10-07T01:01:03.123456Z',
        received_at: '2026-10-07T01:01:04.123456Z',
        postgresql_version: 'PostgreSQL 17.8',
        pgvector_version: '0.8.1',
      },
    },
    object_storage: {
      backend: 'minio',
      assessment: 'object_storage_aggregate',
      status: 'available',
      safe_reason: null as string | null,
      last_success_received_at: '2026-10-07T01:01:05.123456Z',
      details: 'not_reported',
    },
    readiness: { ready: false, safe_reason: 'DEPENDENCY_UNAVAILABLE' },
  }
}
function at(value: unknown, keys: readonly string[]): Record<string, unknown> {
  let current = value as Record<string, unknown>
  for (const key of keys) current = current[key] as Record<string, unknown>
  return current
}
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { resolve, promise }
}
function observed<T>(promise: Promise<T>) {
  let settled = false
  const result = promise.then(
    (value) => {
      settled = true
      return { value, error: undefined }
    },
    (error: unknown) => {
      settled = true
      return { value: undefined, error }
    },
  )
  return { result, settled: () => settled }
}
const turn = () => new Promise<void>((resolve) => setImmediate(resolve))
const rawTransport = (fetcher: Fetch) =>
  accountTransport(fetcher) as unknown as (
    name: string,
    parse: (value: unknown) => unknown,
    options: unknown,
  ) => Promise<unknown>
const identity = (value: unknown) => value
function problemValue(status = 503) {
  return {
    type: 'urn:agenteam:problem:request-failed',
    title: 'Request failed',
    status,
    detail: 'Request failed.',
    instance: path,
    code: 'COMMIT_UNKNOWN',
    request_id: requestID,
    commit_state: 'unknown',
    retry_hint: 'lookup_then_retry',
  }
}
function problemResponse(value: unknown = problemValue(), status = 503, padding = 0) {
  const text = JSON.stringify(value)
  return new Response(text + ' '.repeat(Math.max(0, padding - encode(text).byteLength)), {
    status,
    headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': requestID },
  })
}
const objectPaths = [
  [],
  ['central'],
  ['database'],
  ['database', 'last_success'],
  ['object_storage'],
  ['readiness'],
]
const timePaths = [
  ['observed_at'],
  ['database', 'last_success', 'checked_at'],
  ['database', 'last_success', 'received_at'],
  ['object_storage', 'last_success_received_at'],
]

describe('Runtime Information fixed read contract', () => {
  it('dispatches only the fixed GET with same-origin Cookie and no mutation or query material', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(sample()))
    const abort = new AbortController()
    const value = await createSystemRuntimeInformationAPI(fetcher).get(abort.signal)
    expect(fetcher).toHaveBeenCalledTimes(1)
    const [actualPath, init] = fetcher.mock.calls[0]!
    expect(actualPath).toBe(path)
    expect(init).toEqual({
      method: 'GET',
      headers: { Accept: 'application/json, application/problem+json' },
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal: abort.signal,
    })
    expect(value).toEqual(sample())
    expect(objectPaths.every((keys) => Object.isFrozen(at(value, keys)))).toBe(true)
  })

  it.each([
    'body',
    'query',
    'target',
    'cursor',
    'identity',
    'csrf',
    'key',
    'avatar',
    'users',
    'invitations',
    'providers',
    'models',
    'mailJobs',
    'audit',
    'url',
    'headers',
    'responseBudget',
  ])('rejects the own %s option, including undefined, before fetch', async (key) => {
    const fetcher = vi.fn<Fetch>(async () => json(sample()))
    for (const value of [undefined, null, 'unexpected']) {
      await expect(
        rawTransport(fetcher)(endpoint, identity, { signal: signal(), [key]: value }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('requires the sole options object and own signal before dispatch', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(sample()))
    for (const options of [undefined, null, [], {}, Object.create({ signal: signal() })])
      await expect(rawTransport(fetcher)(endpoint, identity, options)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('returns detached immutable values without trimming printable versions', () => {
    const source = sample()
    source.database.last_success.postgresql_version = '  <PostgreSQL>& "17.8"  '
    source.database.last_success.pgvector_version = ' '
    const value = parseSystemRuntimeInformation(source)
    source.database.last_success.postgresql_version = 'changed later'
    expect(value.database.last_success.postgresql_version).toBe('  <PostgreSQL>& "17.8"  ')
    expect(value.database.last_success.pgvector_version).toBe(' ')
    expect(objectPaths.every((keys) => Object.isFrozen(at(value, keys)))).toBe(true)
  })
})

describe('Runtime Information all-or-nothing projection', () => {
  for (const keys of objectPaths) {
    const label = keys.join('.') || 'snapshot'
    it(`rejects every missing required key and extra key at ${label}`, () => {
      for (const key of Object.keys(at(sample(), keys))) {
        const value = sample()
        delete at(value, keys)[key]
        expect(() => parseSystemRuntimeInformation(value), key).toThrow('invalid-response')
      }
      for (const extra of ['secret', 'version_detail']) {
        const value = sample()
        at(value, keys)[extra] = 'untrusted extra value'
        expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
      }
    })
    it(`rejects null, array and scalar instead of ${label}`, () => {
      for (const invalid of [null, [], '', 1, false]) {
        const value = sample()
        if (keys.length === 0)
          expect(() => parseSystemRuntimeInformation(invalid)).toThrow('invalid-response')
        else {
          at(value, keys.slice(0, -1))[keys.at(-1)!] = invalid
          expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
        }
      }
    })
  }
  it('enforces the fixed Central, Object Storage and readiness literals without coercion', () => {
    const values: [string[], unknown[]][] = [
      [
        ['central', 'version'],
        ['', 'v1', 0, false, {}],
      ],
      [
        ['central', 'safe_reason'],
        [null, '', 'unknown'],
      ],
      [
        ['object_storage', 'backend'],
        [null, '', 's3'],
      ],
      [
        ['object_storage', 'assessment'],
        [null, '', 'minio_server'],
      ],
      [
        ['object_storage', 'details'],
        [null, '', {}],
      ],
      [
        ['readiness', 'ready'],
        [null, true, 0, 'false'],
      ],
      [
        ['readiness', 'safe_reason'],
        [null, '', 'INTERNAL_ERROR'],
      ],
    ]
    for (const [keys, candidates] of values)
      for (const invalid of candidates) {
        const value = sample()
        at(value, keys.slice(0, -1))[keys.at(-1)!] = invalid
        expect(() => parseSystemRuntimeInformation(value), keys.join('.')).toThrow(
          'invalid-response',
        )
      }
  })
  it('accepts all nine aggregate status pairs with historical samples and broad unavailability', () => {
    const pairs = [
      ['available', null],
      ['unavailable', 'check_unavailable'],
      ['stale', 'sample_stale'],
    ] as const
    for (const [database, databaseReason] of pairs)
      for (const [storage, storageReason] of pairs) {
        const value = sample()
        Object.assign(value.database, { status: database, safe_reason: databaseReason })
        Object.assign(value.object_storage, { status: storage, safe_reason: storageReason })
        expect(parseSystemRuntimeInformation(value)).toEqual(value)
        value.readiness.safe_reason = 'DEPENDENCY_UNBOUND'
        if (database === 'available' && storage === 'available')
          expect(parseSystemRuntimeInformation(value)).toEqual(value)
        else expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
      }
  })
  it.each(['database', 'object_storage'] as const)(
    'rejects wrong status/reason pairs for %s',
    (key) => {
      for (const state of ['available', 'unavailable', 'stale', 'unknown', '', null, 0])
        for (const reason of [null, 'check_unavailable', 'sample_stale', 'unknown', false]) {
          if (
            (state === 'available' && reason === null) ||
            (state === 'unavailable' && reason === 'check_unavailable') ||
            (state === 'stale' && reason === 'sample_stale')
          )
            continue
          const value = sample()
          Object.assign(value[key], { status: state, safe_reason: reason })
          expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
        }
    },
  )

  for (const keys of timePaths) {
    it(`validates canonical nonzero Gregorian microseconds at ${keys.join('.')}`, () => {
      for (const valid of [
        '0000-02-29T23:59:59.999999Z',
        '0001-01-01T00:00:00.000001Z',
        '0099-01-01T00:00:00.000000Z',
        '2000-02-29T00:00:00.000000Z',
        '9999-12-31T23:59:59.999999Z',
      ]) {
        const value = sample()
        at(value, keys.slice(0, -1))[keys.at(-1)!] = valid
        expect(at(parseSystemRuntimeInformation(value), keys.slice(0, -1))[keys.at(-1)!]).toBe(
          valid,
        )
      }
      for (const invalid of [
        zero,
        null,
        '',
        '0000-02-30T00:00:00.000000Z',
        '0099-02-29T00:00:00.000000Z',
        '1900-02-29T00:00:00.000000Z',
        '2026-04-31T00:00:00.000000Z',
        '2026-00-01T00:00:00.000000Z',
        '2026-13-01T00:00:00.000000Z',
        '2026-01-00T00:00:00.000000Z',
        '2026-01-01T24:00:00.000000Z',
        '2026-01-01T00:60:00.000000Z',
        '2026-01-01T00:00:60.000000Z',
        '2026-01-01T00:00:00.00000Z',
        '2026-01-01T00:00:00.0000000Z',
        '2026-01-01T00:00:00.000000+00:00',
        '2026-01-01T00:00:00.000000z',
        '10000-01-01T00:00:00.000000Z',
      ]) {
        const value = sample()
        at(value, keys.slice(0, -1))[keys.at(-1)!] = invalid
        expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
      }
    })
  }
  it('leaves the accepted helper intact and does not impose time ordering or a client age', () => {
    expect(parseSystemInstant(zero)).toBe(zero)
    const value = sample()
    value.observed_at = '0000-01-01T00:00:00.000000Z'
    value.database.last_success.checked_at = '9999-12-31T23:59:59.999999Z'
    value.database.last_success.received_at = '0099-01-01T00:00:00.000001Z'
    value.object_storage.last_success_received_at = '0001-01-01T00:00:00.000001Z'
    expect(parseSystemRuntimeInformation(value)).toEqual(value)
  })
  it.each(['postgresql_version', 'pgvector_version'] as const)(
    'validates %s as 1–256 printable ASCII bytes',
    (key) => {
      for (const valid of [' ', 'A', 'x'.repeat(256), '<>&"\\'.repeat(51) + ' ']) {
        const value = sample()
        value.database.last_success[key] = valid
        expect(parseSystemRuntimeInformation(value).database.last_success[key]).toBe(valid)
      }
      for (const invalid of [
        '',
        'x'.repeat(257),
        '\t',
        '\n',
        '\x1f',
        '\x7f',
        '\x80',
        'é',
        '中',
        '\ud800',
        null,
        17,
      ]) {
        const value = sample()
        at(value, ['database', 'last_success'])[key] = invalid
        expect(() => parseSystemRuntimeInformation(value)).toThrow('invalid-response')
      }
    },
  )
})

describe('Runtime Information status, media and byte boundaries', () => {
  it('accepts maximal escaped legal versions without interpreting their text', async () => {
    const value = sample()
    value.database.last_success.postgresql_version = '<'.repeat(256)
    value.database.last_success.pgvector_version = '&'.repeat(256)
    const text = JSON.stringify(value).replace(
      /[<>&]/g,
      (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'),
    )
    expect(encode(text).byteLength).toBeLessThan(16384)
    expect(
      await createSystemRuntimeInformationAPI(
        async () => new Response(text, { headers: { 'Content-Type': 'application/json' } }),
      ).get(signal()),
    ).toEqual(value)
  })
  it('accepts the actual 16384-byte success including trailing whitespace, regardless of Content-Length', async () => {
    const value = sample()
    const text = JSON.stringify(value)
    const exact = text + ' '.repeat(16384 - encode(text).byteLength)
    expect(encode(exact).byteLength).toBe(16384)
    for (const length of ['1', '9999999'])
      expect(
        await createSystemRuntimeInformationAPI(
          async () =>
            new Response(exact, {
              headers: {
                'Content-Type': 'application/json; charset=utf-8',
                'Content-Length': length,
              },
            }),
        ).get(signal()),
      ).toEqual(value)
  })
  it('does not lower the new endpoint Problem budget or treat COMMIT_UNKNOWN as a successful observation', async () => {
    for (const size of [16385, 600000])
      await expect(
        createSystemRuntimeInformationAPI(async () => problemResponse(undefined, 503, size)).get(
          signal(),
        ),
      ).rejects.toMatchObject({
        kind: 'problem',
        problem: {
          code: 'COMMIT_UNKNOWN',
          commit_state: 'unknown',
          retry_hint: 'lookup_then_retry',
        },
      })
    await expect(
      createSystemRuntimeInformationAPI(async () => problemResponse(undefined, 503, 600001)).get(
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    ['bootstrap', 600000, {}],
    ['listSystemAudit', 1024 * 1024, { audit: {} }],
    ['getSystemAudit', 1024 * 1024, { target: requestID }],
    ['listProviders', 2 * 1024 * 1024, { providers: {} }],
  ] as const)('preserves the actual %s success budget', async (name, maximum, extra) => {
    for (const excess of [0, 1]) {
      const request = rawTransport(
        async () =>
          new Response('{}' + ' '.repeat(maximum + excess - 2), {
            headers: { 'Content-Type': 'application/json' },
          }),
      )
      const pending = request(name, identity, { signal: signal(), ...extra })
      if (excess) await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
      else expect(await pending).toEqual({})
    }
  })
  it('rejects malformed UTF-8, truncated JSON, trailing content, absent body and unexpected success statuses', async () => {
    const valid = JSON.stringify(sample())
    const inputs = [
      new Response(new Uint8Array([...encode(valid), 0xc3]), {
        headers: { 'Content-Type': 'application/json' },
      }),
      new Response(valid.slice(0, -1), { headers: { 'Content-Type': 'application/json' } }),
      new Response(valid + '{}', { headers: { 'Content-Type': 'application/json' } }),
      new Response(null, { headers: { 'Content-Type': 'application/json' } }),
      json(sample(), 201),
      new Response(null, { status: 204 }),
      json(sample(), 206),
      json(sample(), 503),
    ]
    for (const response of inputs) {
      await expect(
        createSystemRuntimeInformationAPI(async () => response).get(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(response.body?.locked ?? false).toBe(false)
    }
  })
  it('rejects redirect indicators and invalid Problem media, status, request ID or deep fields', async () => {
    for (const key of ['redirected', 'type']) {
      const response = json(sample())
      Object.defineProperty(response, key, { value: key === 'type' ? 'opaqueredirect' : true })
      await expect(
        createSystemRuntimeInformationAPI(async () => response).get(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const invalid = [
      problemResponse({ ...problemValue(), status: 401 }),
      problemResponse({ ...problemValue(), request_id: 'bad' }),
      problemResponse({ ...problemValue(), commit_state: 'probably_committed' }),
      problemResponse({ ...problemValue(), private_detail: 'not a public field' }),
      new Response(JSON.stringify(problemValue()), {
        status: 503,
        headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestID },
      }),
      new Response(JSON.stringify(problemValue()), {
        status: 503,
        headers: { 'Content-Type': 'application/problem+json' },
      }),
    ]
    for (const response of invalid)
      await expect(
        createSystemRuntimeInformationAPI(async () => response).get(signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('keeps valid Problem security facts without promoting status/code mismatches into identity decisions', async () => {
    for (const [status, code] of [
      [401, 'SESSION_REVOKED'],
      [403, 'FORBIDDEN'],
      [403, 'SESSION_REVOKED'],
      [403, 'CSRF_FAILED'],
    ] as const) {
      const value = { ...problemValue(status), code, commit_state: 'not_started' }
      await expect(
        createSystemRuntimeInformationAPI(async () => problemResponse(value, status)).get(signal()),
      ).rejects.toMatchObject({ kind: 'problem', problem: { status, code } })
    }
  })
})

describe('Runtime Information actual native I/O completion', () => {
  it('publishes nothing after a complete JSON prefix until the native read reaches actual EOF', async () => {
    let control!: ReadableStreamDefaultController<Uint8Array>
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        control = c
        c.enqueue(encode(JSON.stringify(sample())))
      },
    })
    const response = new Response(stream, { headers: { 'Content-Type': 'application/json' } })
    const abort = new AbortController()
    const job = observed(createSystemRuntimeInformationAPI(async () => response).get(abort.signal))
    try {
      await turn()
      expect(stream.locked).toBe(true)
      expect(job.settled()).toBe(false)
      control.close()
      const result = await job.result
      expect(result.error).toBeUndefined()
      expect(result.value).toEqual(sample())
      expect(stream.locked).toBe(false)
    } finally {
      abort.abort()
      await job.result
    }
  })
  it('rejects 16385 actual bytes but waits for the independent native cancel tail and releases its lock', async () => {
    const release = deferred()
    let entered = false
    const text = JSON.stringify(sample())
    const bytes = encode(text + ' '.repeat(16385 - encode(text).byteLength))
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(bytes)
      },
      cancel() {
        entered = true
        return release.promise
      },
    })
    const abort = new AbortController()
    const response = new Response(stream, {
      headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
    })
    const job = observed(createSystemRuntimeInformationAPI(async () => response).get(abort.signal))
    try {
      await vi.waitFor(() => expect(entered).toBe(true))
      expect(job.settled()).toBe(false)
      expect(stream.locked).toBe(true)
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'invalid-response' })
      expect(stream.locked).toBe(false)
    } finally {
      release.resolve()
      abort.abort()
      await job.result
    }
  })
  it('joins the cancelled native read and still waits for native reader.cancel completion', async () => {
    const release = deferred()
    let cancelEntered = false
    let readStarted = false
    let readEnded = false
    const stream = new ReadableStream<Uint8Array>({
      cancel() {
        cancelEntered = true
        return release.promise
      },
    })
    const getReader = stream.getReader.bind(stream)
    vi.spyOn(stream, 'getReader').mockImplementation(() => {
      const reader = getReader()
      const read = reader.read.bind(reader)
      vi.spyOn(reader, 'read').mockImplementation(() => {
        readStarted = true
        const original = read()
        void original.then(
          () => {
            readEnded = true
          },
          () => {
            readEnded = true
          },
        )
        return original
      })
      return reader
    })
    const abort = new AbortController()
    const job = observed(
      createSystemRuntimeInformationAPI(
        async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
      ).get(abort.signal),
    )
    try {
      await vi.waitFor(() => expect(readStarted).toBe(true))
      expect(readEnded).toBe(false)
      expect(job.settled()).toBe(false)
      abort.abort()
      await vi.waitFor(() => expect(cancelEntered && readEnded).toBe(true))
      expect(job.settled()).toBe(false)
      expect(stream.locked).toBe(true)
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'cancelled' })
      expect(stream.locked).toBe(false)
    } finally {
      release.resolve()
      abort.abort()
      await job.result
    }
  })
  it('joins wrong-media response.body.cancel without acquiring a reader', async () => {
    const release = deferred()
    let entered = false
    const stream = new ReadableStream<Uint8Array>({
      cancel() {
        entered = true
        return release.promise
      },
    })
    const getReader = vi.spyOn(stream, 'getReader')
    const job = observed(
      createSystemRuntimeInformationAPI(
        async () => new Response(stream, { headers: { 'Content-Type': 'text/html' } }),
      ).get(signal()),
    )
    try {
      await vi.waitFor(() => expect(entered).toBe(true))
      expect(getReader).not.toHaveBeenCalled()
      expect(job.settled()).toBe(false)
      expect(stream.locked).toBe(false)
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'invalid-response' })
    } finally {
      release.resolve()
      await job.result
    }
  })
  it('waits for a fetch that ignores abort and then its unread native response cancellation', async () => {
    const fetchEnd = deferred<Response>()
    const cancelEnd = deferred()
    let cancelEntered = false
    const stream = new ReadableStream<Uint8Array>({
      cancel() {
        cancelEntered = true
        return cancelEnd.promise
      },
    })
    const response = new Response(stream, { headers: { 'Content-Type': 'application/json' } })
    const abort = new AbortController()
    const job = observed(
      createSystemRuntimeInformationAPI(() => fetchEnd.promise).get(abort.signal),
    )
    try {
      abort.abort()
      await turn()
      expect(job.settled()).toBe(false)
      fetchEnd.resolve(response)
      await vi.waitFor(() => expect(cancelEntered).toBe(true))
      expect(job.settled()).toBe(false)
      cancelEnd.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'cancelled' })
      expect(stream.locked).toBe(false)
    } finally {
      fetchEnd.resolve(response)
      cancelEnd.resolve()
      abort.abort()
      await job.result
    }
  })
  it('publishes no partial projection on native read failure or a valid body with one bad deep field', async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.error(new Error('read failed'))
      },
    })
    const response = new Response(stream, { headers: { 'Content-Type': 'application/json' } })
    await expect(
      createSystemRuntimeInformationAPI(async () => response).get(signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(stream.locked).toBe(false)
    const value = sample()
    value.database.last_success.pgvector_version = '\n'
    const job = await observed(
      createSystemRuntimeInformationAPI(async () => json(value)).get(signal()),
    ).result
    expect(job.value).toBeUndefined()
    expect(job.error).toBeInstanceOf(AccountFailure)
    expect(job.error).toMatchObject({ kind: 'invalid-response' })
  })
})

// Compile-time checks for the public narrow overload; this is never dispatched.
function typeContract(signal: AbortSignal) {
  const request = accountTransport()
  // @ts-expect-error Runtime GET accepts no mutation material.
  void request(endpoint, parseSystemRuntimeInformation, { signal, body: {} })
  // @ts-expect-error Runtime GET has no target/query surface.
  void request(endpoint, parseSystemRuntimeInformation, { signal, target: requestID })
  // @ts-expect-error Runtime GET never supplies CSRF or a command key.
  void request(endpoint, parseSystemRuntimeInformation, { signal, csrf: 'token', key: 'key' })
  const api = createSystemRuntimeInformationAPI()
  const response: Promise<SystemRuntimeInformation> = api.get(signal)
  return response
}
void typeContract
