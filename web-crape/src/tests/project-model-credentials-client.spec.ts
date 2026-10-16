import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import { createProjectModelSettingsAPI } from '../api/project-models'
import {
  captureProjectCredentialTarget,
  createProjectModelCredentialAPI,
  type ProjectCredentialLookupTarget,
} from '../api/project-model-credentials'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  credential = id(20),
  maximum = '9223372036854775807',
  expectedMaximum = '9223372036854775806'
const signal = () => new AbortController().signal
const write = () => ({ csrfToken: 'a'.repeat(43), key: 'original:credential', signal: signal() })
const metadata = (version = '1') => ({ credential_id: credential, purpose: 'model', version })
const mutation = (version = '1', deleted = false) => ({ ...metadata(version), deleted })
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('Project Credential five closed methods, no material read', () => {
  it('uses only the explicit Project paths, exact bodies and passive lookup targets', async () => {
    const replies: unknown[] = [
      metadata(maximum),
      mutation(),
      mutation('9007199254740994'),
      mutation(maximum, true),
      { observed: false, result: null },
      { observed: true, result: mutation('9007199254740994') },
      { observed: true, result: mutation(maximum, true) },
    ]
    const fetch = vi.fn<Fetch>(async () => json(replies.shift())),
      api = createProjectModelSettingsAPI(fetch),
      options = write()
    expect(await api.getCredentialMetadata(project, credential, signal())).toEqual(
      metadata(maximum),
    )
    expect(await api.createCredential(project, ' synthetic value\0 ', options)).toEqual(mutation())
    expect(
      await api.updateCredential(project, credential, '9007199254740993', 'replacement', options),
    ).toEqual(mutation('9007199254740994'))
    expect(await api.deleteCredential(project, credential, expectedMaximum, options)).toEqual(
      mutation(maximum, true),
    )
    expect(await api.lookupCredential(project, { kind: 'create' }, options)).toEqual({
      observed: false,
      result: null,
    })
    expect(
      await api.lookupCredential(
        project,
        { kind: 'update', credential_id: credential, expected_version: '9007199254740993' },
        options,
      ),
    ).toEqual({ observed: true, result: mutation('9007199254740994') })
    expect(
      await api.lookupCredential(
        project,
        { kind: 'delete', credential_id: credential, expected_version: expectedMaximum },
        options,
      ),
    ).toEqual({ observed: true, result: mutation(maximum, true) })
    expect(
      fetch.mock.calls.map(([path, init]) => [
        path.slice(`/api/v1/projects/${project}/`.length),
        init.method,
        init.body === undefined ? undefined : JSON.parse(String(init.body)),
      ]),
    ).toEqual([
      [`model-credentials/${credential}`, 'GET', undefined],
      ['model-credentials', 'POST', { value: ' synthetic value\0 ' }],
      [
        `model-credentials/${credential}`,
        'PUT',
        { expected_version: '9007199254740993', value: 'replacement' },
      ],
      [`model-credentials/${credential}`, 'DELETE', { expected_version: expectedMaximum }],
      ['model-credential-commands/lookup', 'POST', { kind: 'create' }],
      [
        'model-credential-commands/lookup',
        'POST',
        { kind: 'update', credential_id: credential, expected_version: '9007199254740993' },
      ],
      [
        'model-credential-commands/lookup',
        'POST',
        { kind: 'delete', credential_id: credential, expected_version: expectedMaximum },
      ],
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      const headers = new Headers(init.headers)
      expect(headers.get('X-CSRF-Token')).toBe(init.method === 'GET' ? null : options.csrfToken)
      expect(headers.get('Idempotency-Key')).toBe(init.method === 'GET' ? null : options.key)
    }
  })
  it('keeps decoded 65536-byte materials, including worst-case JSON escapes, within the distinct Project cap', async () => {
    const fetch = vi.fn<Fetch>(async (_path, init) =>
        json(mutation(init.method === 'POST' ? '1' : maximum)),
      ),
      api = createProjectModelCredentialAPI(fetch)
    await api.createCredential(project, '\0'.repeat(65536), write())
    await api.updateCredential(project, credential, expectedMaximum, '\0'.repeat(65536), write())
    for (const [, init] of fetch.mock.calls) {
      const body = String(init.body)
      expect(new TextEncoder().encode(body).byteLength).toBeGreaterThan(393216)
      expect(new TextEncoder().encode(body).byteLength).toBeLessThanOrEqual(409600)
      expect(JSON.parse(body).value).toHaveLength(65536)
    }
  })
  it.each(['', 'x'.repeat(65537), '😀'.repeat(16385), '\ud800', '\udfff'])(
    'rejects invalid material before a fetch and retains no material in the error',
    async (value) => {
      const fetch = vi.fn<Fetch>(),
        api = createProjectModelCredentialAPI(fetch)
      const failure = await api.createCredential(project, value, write()).catch((e: unknown) => e)
      expect(failure).toMatchObject({
        name: 'AccountFailure',
        message: 'invalid-input',
        kind: 'invalid-input',
      })
      expect(Object.keys(failure as object).sort()).toEqual(['kind', 'name', 'problem'])
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it.each(['0', '01', '1.0', 1, maximum, '9223372036854775808'])(
    'rejects expected version %s for rotate, delete and lookup',
    async (version) => {
      const fetch = vi.fn<Fetch>(),
        api = createProjectModelCredentialAPI(fetch)
      await expect(
        api.updateCredential(project, credential, version as string, 'value', write()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      await expect(
        api.deleteCredential(project, credential, version as string, write()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      await expect(
        api.lookupCredential(
          project,
          { kind: 'update', credential_id: credential, expected_version: version as string },
          write(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it('captures original lookup target, version and headers before await and freezes observations', async () => {
    const gate = deferred<Response>(),
      fetch = vi.fn<Fetch>(() => gate.promise),
      api = createProjectModelCredentialAPI(fetch)
    const target = {
        kind: 'update' as const,
        credential_id: credential,
        expected_version: '9007199254740993',
      },
      options = write()
    const pending = api.lookupCredential(project, target, options)
    target.credential_id = id(99)
    target.expected_version = '99'
    options.key = 'changed'
    expect(JSON.parse(String(fetch.mock.calls[0]![1].body))).toEqual({
      kind: 'update',
      credential_id: credential,
      expected_version: '9007199254740993',
    })
    gate.resolve(json({ observed: true, result: mutation('9007199254740994') }))
    const observation = await pending
    expect(observation).toEqual({ observed: true, result: mutation('9007199254740994') })
    expect(Object.isFrozen(observation)).toBe(true)
    if (observation.observed) expect(Object.isFrozen(observation.result)).toBe(true)
  })
  it.each([
    { kind: 'create', value: 'must-not-send' },
    { kind: 'create', credential_id: credential },
    { kind: 'update', credential_id: credential },
    { kind: 'delete', credential_id: credential, expected_version: '1', value: 'forbidden' },
    { kind: 'provider.create' },
    { kind: 'in_progress' },
    { command: 'create' },
  ])('rejects unknown or material-bearing lookup target', async (target) => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectModelCredentialAPI(fetch)
    await expect(
      api.lookupCredential(project, target as ProjectCredentialLookupTarget, write()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    id(0xab).toUpperCase(),
    '../escape',
    `${project}?`,
    '',
    '01970000-0000-6000-8000-000000000001',
  ])('rejects a noncanonical Project ID %s', async (value) => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectModelCredentialAPI(fetch)
    await expect(api.getCredentialMetadata(value, credential, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('Project Credential strict metadata/mutation/observation separation', () => {
  it.each([
    { ...metadata(), value: 'never metadata' },
    { ...metadata(), purpose: 'smtp' },
    { ...metadata(), credential_id: id(21) },
    { ...metadata(), version: '0' },
    { ...metadata(), version: '9223372036854775808' },
    { ...metadata(), deleted: false },
  ])('rejects invalid or material-bearing metadata', async (value) => {
    await expect(
      createProjectModelCredentialAPI(async () => json(value)).getCredentialMetadata(
        project,
        credential,
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    { ...mutation(), deleted: true },
    { ...mutation(), version: '2' },
    { ...mutation(), purpose: 'smtp' },
    { ...mutation(), value: 'forbidden' },
    { ...mutation(), deleted: 'false' },
    metadata(),
  ])('rejects a non-strict create receipt', async (value) => {
    await expect(
      createProjectModelCredentialAPI(async () => json(value)).createCredential(
        project,
        'value',
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    { ...mutation('2'), credential_id: id(21) },
    mutation('1'),
    mutation('3'),
    mutation('2', true),
  ])('rejects wrong target/version/deleted on rotation', async (value) => {
    await expect(
      createProjectModelCredentialAPI(async () => json(value)).updateCredential(
        project,
        credential,
        '1',
        'value',
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    { found: false, receipt: null },
    { observed: false },
    { observed: false, result: mutation() },
    { observed: true, result: null },
    { observed: true, result: mutation('2') },
    { observed: false, result: null, in_progress: true },
    { observed: true, result: { ...mutation(), value: 'forbidden' } },
  ])('does not accept configuration/Owner or malformed Credential lookup unions', async (value) => {
    await expect(
      createProjectModelCredentialAPI(async () => json(value)).lookupCredential(
        project,
        { kind: 'create' },
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('requires a matching delete observation, without treating it as a current metadata read', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ observed: true, result: mutation('2', true) })),
      api = createProjectModelCredentialAPI(fetch)
    expect(
      await api.lookupCredential(
        project,
        { kind: 'delete', credential_id: credential, expected_version: '1' },
        write(),
      ),
    ).toEqual({ observed: true, result: mutation('2', true) })
    expect(fetch).toHaveBeenCalledOnce()
    expect(fetch.mock.calls[0]![0].endsWith('/model-credential-commands/lookup')).toBe(true)
    expect(Object.isFrozen(captureProjectCredentialTarget({ kind: 'create' }))).toBe(true)
  })
})

describe('Project Credential small success and fixed raw request caps', () => {
  it.each(['metadata', 'create', 'update', 'delete', 'lookup'] as const)(
    'applies exactly the 1024-byte success cap to %s',
    async (method) => {
      const body =
          method === 'metadata'
            ? metadata()
            : method === 'lookup'
              ? { observed: false, result: null }
              : mutation(method === 'create' ? '1' : '2', method === 'delete'),
        raw = JSON.stringify(body)
      for (const extra of [0, 1]) {
        const api = createProjectModelCredentialAPI(
          async () =>
            new Response(raw + ' '.repeat(1024 - raw.length + extra), {
              headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
            }),
        )
        const pending =
          method === 'metadata'
            ? api.getCredentialMetadata(project, credential, signal())
            : method === 'create'
              ? api.createCredential(project, 'value', write())
              : method === 'update'
                ? api.updateCredential(project, credential, '1', 'value', write())
                : method === 'delete'
                  ? api.deleteCredential(project, credential, '1', write())
                  : api.lookupCredential(project, { kind: 'create' }, write())
        if (extra) await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
        else await expect(pending).resolves.toEqual(body)
      }
    },
  )
  it('applies Project 409600/1024 raw request limits, preserving the legacy System cap', async () => {
    const fetch = vi.fn<Fetch>(async () => json({})),
      request = accountTransport(fetch),
      options = { signal: signal(), projectID: project, csrf: 'a'.repeat(43), key: 'original' }
    const large = { padding: 'x'.repeat(409600 - 14) },
      small = { padding: 'x'.repeat(1024 - 14) }
    await request('createProjectModelCredential', (v) => v, { ...options, body: large })
    await request('updateProjectModelCredential', (v) => v, {
      ...options,
      target: credential,
      body: large,
    })
    await request('deleteProjectModelCredential', (v) => v, {
      ...options,
      target: credential,
      body: small,
    })
    await request('lookupProjectModelCredential', (v) => v, { ...options, body: small })
    await expect(
      request('createProjectModelCredential', (v) => v, {
        ...options,
        body: { padding: large.padding + 'x' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('updateProjectModelCredential', (v) => v, {
        ...options,
        target: credential,
        body: { padding: large.padding + 'x' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('deleteProjectModelCredential', (v) => v, {
        ...options,
        target: credential,
        body: { padding: small.padding + 'x' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      request('lookupProjectModelCredential', (v) => v, {
        ...options,
        body: { padding: small.padding + 'x' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await request('createModelCredential', (v) => v, {
      signal: signal(),
      csrf: options.csrf,
      key: options.key,
      body: { padding: 'x'.repeat(500000) },
    })
    expect(fetch).toHaveBeenCalledTimes(5)
  })
  it('does not publish a metadata prefix before EOF, and owns delayed reader cancellation', async () => {
    let controller!: ReadableStreamDefaultController<Uint8Array>
    const gate = deferred(),
      stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c
        },
        cancel: () => gate.promise,
      }),
      abort = new AbortController()
    const api = createProjectModelCredentialAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    )
    let settled = false
    const pending = api.getCredentialMetadata(project, credential, abort.signal).finally(() => {
        settled = true
      }),
      outcome = expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    controller.enqueue(new TextEncoder().encode(JSON.stringify(metadata())))
    await flushPromises()
    expect(settled).toBe(false)
    abort.abort()
    await flushPromises()
    expect(settled).toBe(false)
    gate.resolve()
    await outcome
    expect(stream.locked).toBe(false)
  })
})
