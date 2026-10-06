import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureSelectionCommand,
  createSystemModelSelectionAPI,
  selectionCommandBody,
  type SelectionCommand,
} from '../api/system-model-selection'
import type { ModelType, ProviderProtocol } from '../api/system-providers'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const values = () => ({ embedding: id(10), memory: id(11), reranker: null, image: null })
const selection = () => ({ id: id(1), version: '9007199254740993', configured: values() })
const command = (): SelectionCommand => ({
  kind: 'model.selection.update',
  id: id(1),
  expected_version: '9007199254740993',
  ...values(),
})
const receipt = () => ({
  kind: 'model.selection.update',
  resource_id: id(1),
  version: '9007199254740994',
  affected_references: '0',
})
const provider = (protocol: ProviderProtocol = 'openai-chat-completions') => ({
  id: id(2),
  version: '1',
  created_at: time,
  updated_at: time,
  input: {
    name: 'Provider 🧩',
    protocol,
    base_url: 'https://example.invalid/v1',
    enabled: true,
    credential_ref: null,
    options: {},
  },
})
const model = (type: ModelType = 'chat') => ({
  id: id(11),
  provider_id: id(2),
  version: '9007199254740993',
  created_at: time,
  updated_at: time,
  input: {
    name: 'Model 🧩',
    provider_model_id: ' Native/模型 ',
    type,
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: type === 'chat',
      parallel_tool_calls: type === 'chat',
      streaming: type === 'chat',
      reasoning: type === 'chat',
      input_modalities: ['text'],
      output_modalities:
        type === 'reranker'
          ? []
          : [type === 'embedding' ? 'vector' : type === 'image_generation' ? 'image' : 'text'],
      reasoning_efforts: [],
      structured_output_modes: type === 'chat' ? ['json_schema'] : [],
      context_length: '9007199254740993',
      max_output: null,
    },
  },
})
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrfToken: 'S'.repeat(43), key: 'selection/original:1' })
function changed(value: unknown, path: string, replacement?: unknown, remove = false) {
  const result = JSON.parse(JSON.stringify(value)) as Record<string, unknown>
  const keys = path.split('.')
  let parent = result
  for (const key of keys.slice(0, -1)) parent = parent[key] as Record<string, unknown>
  if (remove) delete parent[keys.at(-1)!]
  else parent[keys.at(-1)!] = replacement
  return result
}
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}

describe('Selection closed API and original command', () => {
  it('uses exactly seven operations with a private two-GET read and unchanged query/header rules', async () => {
    const fetch = vi.fn<Fetch>(async (path, init) => {
      if (path.endsWith('/lookup')) return json({ found: true, receipt: receipt() })
      if (path.endsWith('/model-selection'))
        return json(init.method === 'GET' ? selection() : receipt())
      if (path.includes('/model-providers'))
        return json(path.includes('?') ? { items: [provider()], next_cursor: null } : provider())
      return json(path.includes('?') ? { items: [model()], next_cursor: null } : model())
    })
    const api = createSystemModelSelectionAPI(fetch),
      write = options(),
      cursor = 'next/+?&='
    const observed = await api.getSelection(write.signal)
    await api.updateSelection(command(), write)
    await api.lookupSelectionCommand(command(), write)
    await api.listProviders({ cursor }, write.signal)
    await api.getProvider(id(2), write.signal)
    await api.listModels(
      { provider_id: id(2), protocol: 'openai-chat-completions', cursor },
      write.signal,
    )
    const pair = await api.getSavedModel(id(11), write.signal)
    expect(fetch.mock.calls.map(([path, init]) => [init.method, path])).toEqual([
      ['GET', '/api/v1/system/model-selection'],
      ['PUT', '/api/v1/system/model-selection'],
      ['POST', '/api/v1/system/model-commands/lookup'],
      ['GET', '/api/v1/system/model-providers?limit=25&cursor=next%2F%2B%3F%26%3D'],
      ['GET', '/api/v1/system/model-providers/' + id(2)],
      [
        'GET',
        '/api/v1/system/models?provider_id=' + id(2) + '&limit=25&cursor=next%2F%2B%3F%26%3D',
      ],
      ['GET', '/api/v1/system/models/' + id(11)],
      ['GET', '/api/v1/system/model-providers/' + id(2)],
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        signal: write.signal,
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      if (init.method === 'GET') {
        expect(init.body).toBeUndefined()
        expect(init.headers).not.toHaveProperty('X-CSRF-Token')
        expect(init.headers).not.toHaveProperty('Idempotency-Key')
      } else
        expect(init.headers).toMatchObject({
          'X-CSRF-Token': write.csrfToken,
          'Idempotency-Key': write.key,
        })
    }
    expect(JSON.parse(fetch.mock.calls[1]![1].body as string)).toEqual({
      id: id(1),
      expected_version: '9007199254740993',
      ...values(),
    })
    expect(JSON.parse(fetch.mock.calls[2]![1].body as string)).toEqual({
      command: 'model.selection.update',
    })
    expect(observed).toEqual(selection())
    expect(Object.isFrozen(observed.configured)).toBe(true)
    expect(pair).toEqual({ model: model(), provider: provider() })
    expect(Object.isFrozen(pair)).toBe(true)
    expect(Object.isFrozen(pair.model.input.capabilities.structured_output_modes)).toBe(true)
    expect(Object.isFrozen(pair.provider.input)).toBe(true)
    expect(pair.model.input.provider_model_id).toBe(' Native/模型 ')
  })

  it('preserves the stable unconfigured singleton and all explicit optional nulls', async () => {
    const value = { id: id(1), version: '1', configured: null }
    const api = createSystemModelSelectionAPI(async () => json(value))
    expect(await api.getSelection(signal())).toEqual(value)
    const capture = captureSelectionCommand({ ...command(), reranker: id(12), image: id(13) })
    expect(capture.reranker).toBe(id(12))
    expect(capture.image).toBe(id(13))
    expect(Object.isFrozen(capture)).toBe(true)
    expect(Object.isFrozen(selectionCommandBody(capture))).toBe(true)
  })

  it.each(['id', 'version', 'configured', ...Object.keys(values()).map((k) => 'configured.' + k)])(
    'rejects a missing required Selection field %s',
    async (path) => {
      const api = createSystemModelSelectionAPI(async () =>
        json(changed(selection(), path, undefined, true)),
      )
      await expect(api.getSelection(signal())).rejects.toMatchObject({ kind: 'invalid-response' })
    },
  )
  it.each([
    ['id', id(1).toUpperCase().replace('01900000', 'ABC00000')],
    ['id', '01900000-0000-4000-8000-000000000001'],
    ['version', 1],
    ['version', '0'],
    ['version', '01'],
    ['version', '9223372036854775808'],
    ['configured', []],
    ['configured.embedding', null],
    ['configured.memory', null],
    ['configured.reranker', false],
    ['configured.image', ''],
    ['configured.extra', id(12)],
    ['extra', null],
  ])('rejects invalid Selection %s=%s', async (path, value) => {
    const api = createSystemModelSelectionAPI(async () =>
      json(changed(selection(), path as string, value)),
    )
    await expect(api.getSelection(signal())).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('captures six immutable PUT fields and rejects invalid input before any dispatch', async () => {
    const original = command(),
      captured = captureSelectionCommand(original)
    ;(original as { embedding: string }).embedding = id(99)
    expect(captured.embedding).toBe(id(10))
    const fetch = vi.fn<Fetch>(async () => json(receipt())),
      api = createSystemModelSelectionAPI(fetch)
    for (const value of [
      ...Object.keys(command()).map((key) => changed(command(), key, undefined, true)),
      ...[
        ['kind', 'model.update'],
        ['expected_version', '9223372036854775807'],
        ['expected_version', '01'],
        ['id', id(1) + '?'],
        ['memory', null],
        ['image', undefined],
        ['provider_id', id(2)],
        ['extra', '🧩'.repeat(9000)],
      ].map(([key, value]) => changed(command(), key as string, value)),
    ]) {
      await expect(api.updateSelection(value as SelectionCommand, options())).rejects.toMatchObject(
        { kind: 'invalid-input' },
      )
      await expect(
        api.lookupSelectionCommand(value as SelectionCommand, options()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    await expect(api.getSavedModel('../' + id(11), signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
    const last = captureSelectionCommand({ ...command(), expected_version: '9223372036854775806' })
    expect(last.expected_version).toBe('9223372036854775806')
  })

  it.each([
    ['kind', 'model.update'],
    ['resource_id', id(2)],
    ['version', '9007199254740993'],
    ['version', '9007199254740995'],
    ['version', 2],
    ['version', '02'],
    ['version', '9223372036854775808'],
    ['affected_references', '1'],
    ['affected_references', 0],
    ['affected_references', '00'],
    ['extra', true],
  ])('rejects mismatched Execute and lookup receipt %s=%s', async (path, value) => {
    const bad = changed(receipt(), path as string, value)
    const api = createSystemModelSelectionAPI(async (path) =>
      json(path.endsWith('/lookup') ? { found: true, receipt: bad } : bad),
    )
    await expect(api.updateSelection(command(), options())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    await expect(api.lookupSelectionCommand(command(), options())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it.each([
    { found: false },
    { found: false, receipt: receipt() },
    { found: true, receipt: null },
    { found: 1, receipt: receipt() },
    { found: false, receipt: null, extra: true },
  ])('rejects a noncanonical lookup observation %#', async (value) => {
    const api = createSystemModelSelectionAPI(async () => json(value))
    await expect(api.lookupSelectionCommand(command(), options())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('returns found=false only with explicit null and leaves the original command unchanged', async () => {
    const value = { found: false, receipt: null },
      original = captureSelectionCommand(command())
    const api = createSystemModelSelectionAPI(async () => json(value))
    expect(await api.lookupSelectionCommand(original, options())).toEqual(value)
    expect(original).toEqual(command())
  })
})

describe('private saved Model composition', () => {
  it.each([
    ['openai-chat-completions', 'chat'],
    ['openai-embeddings', 'embedding'],
    ['jina-rerank', 'reranker'],
    ['openai-images-generations', 'image_generation'],
  ] as const)(
    'validates complete %s/%s DTOs using the returned Provider',
    async (protocol, type) => {
      const fetch = vi.fn<Fetch>(async (path) =>
        json(path.includes('model-providers') ? provider(protocol) : model(type)),
      )
      const pair = await createSystemModelSelectionAPI(fetch).getSavedModel(id(11), signal())
      expect(pair.model.input.type).toBe(type)
      expect(pair.provider.input.protocol).toBe(protocol)
      expect(fetch).toHaveBeenCalledTimes(2)
    },
  )
  it.each([
    ['id', id(12)],
    ['provider_id', null],
    ['provider_id', id(2) + '/x'],
    ['extra', 1],
  ])('does not issue the second GET after invalid first-stage %s', async (path, value) => {
    const fetch = vi.fn<Fetch>(async () => json(changed(model(), path as string, value)))
    await expect(
      createSystemModelSelectionAPI(fetch).getSavedModel(id(11), signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it.each(['input', 'version', 'created_at', 'updated_at'])(
    'requires first-stage top-level %s before a Provider GET',
    async (field) => {
      const fetch = vi.fn<Fetch>(async () => json(changed(model(), field, undefined, true)))
      await expect(
        createSystemModelSelectionAPI(fetch).getSavedModel(id(11), signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(fetch).toHaveBeenCalledTimes(1)
    },
  )
  it.each([
    ['provider', 'id', id(3)],
    ['provider', 'input.protocol', 'openai-embeddings'],
    ['provider', 'created_at', '2026-02-29T12:34:56.123456Z'],
    ['model', 'input.capabilities.structured_output_modes', ['unknown']],
    ['model', 'input.capabilities.parallel_tool_calls', 'true'],
    ['model', 'input.parameters', { unknown: true }],
    ['model', 'input.name', '\ud800'],
    ['model', 'version', '01'],
    ['model', 'updated_at', '2026-10-05T12:34:56.123456Z'],
  ])('publishes no pair after full %s validation fails at %s', async (side, path, value) => {
    const fetch = vi.fn<Fetch>(async (url) =>
      json(
        url.includes('model-providers')
          ? side === 'provider'
            ? changed(provider(), path as string, value)
            : provider()
          : side === 'model'
            ? changed(model(), path as string, value)
            : model(),
      ),
    )
    const result = await createSystemModelSelectionAPI(fetch)
      .getSavedModel(id(11), signal())
      .then(
        (pair) => ({ pair, error: null }),
        (error) => ({ pair: null, error }),
      )
    expect(result.pair).toBeNull()
    expect(result.error).toMatchObject({ kind: 'invalid-response' })
    expect(fetch).toHaveBeenCalledTimes(2)
  })
  it('returns no half-result after a second-stage transport failure', async () => {
    const fetch = vi.fn<Fetch>(async (path) => {
      if (path.includes('model-providers')) throw new Error('private response text')
      return json(model())
    })
    await expect(
      createSystemModelSelectionAPI(fetch).getSavedModel(id(11), signal()),
    ).rejects.toMatchObject({ kind: 'transport', message: 'transport' })
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it.each([1, 2])(
    'joins the actual native body/cancel tail after cancellation in GET %i',
    async (stage) => {
      const abort = new AbortController(),
        cancelEntered = deferred(),
        tail = deferred()
      const fetch = vi.fn<Fetch>(async (path) => {
        const isProvider = path.includes('model-providers')
        if (Number(isProvider) + 1 !== stage) return json(model())
        return new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(
                new TextEncoder().encode(JSON.stringify(isProvider ? provider() : model())),
              )
            },
            cancel() {
              cancelEntered.resolve()
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        )
      })
      let ended = false
      const result = createSystemModelSelectionAPI(fetch)
        .getSavedModel(id(11), abort.signal)
        .then(
          () => 'unexpected',
          (e: { kind: string }) => e.kind,
        )
        .finally(() => {
          ended = true
        })
      try {
        await flushPromises()
        expect(fetch).toHaveBeenCalledTimes(stage)
        abort.abort()
        await cancelEntered.promise
        await flushPromises()
        expect(ended).toBe(false)
        expect(fetch).toHaveBeenCalledTimes(stage)
      } finally {
        tail.resolve()
        await result
      }
      expect(await result).toBe('cancelled')
      expect(ended).toBe(true)
      expect(fetch).toHaveBeenCalledTimes(stage)
    },
  )

  it('does not continue across the between-GET boundary when the first actual finalizer is cancelled', async () => {
    const abort = new AbortController(),
      entered = deferred(),
      tail = deferred()
    const response = json(model()),
      originalCancel = response.body!.cancel.bind(response.body)
    response.body!.cancel = async () => {
      entered.resolve()
      await tail.promise
      return originalCancel()
    }
    const fetch = vi.fn<Fetch>(async () => response)
    const result = createSystemModelSelectionAPI(fetch)
      .getSavedModel(id(11), abort.signal)
      .catch((e: { kind: string }) => e.kind)
    try {
      await entered.promise
      abort.abort()
      expect(fetch).toHaveBeenCalledTimes(1)
    } finally {
      tail.resolve()
      await result
    }
    expect(await result).toBe('cancelled')
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})

describe('selection fixed transport boundaries', () => {
  it('rejects extra GET/options, unsafe mutations and over-budget UTF-8 requests before dispatch', async () => {
    const fetch = vi.fn<Fetch>(async () => json(selection())),
      request = accountTransport(fetch)
    for (const extra of [
      { body: {} },
      { csrf: 'S'.repeat(43) },
      { key: 'K' },
      { target: id(1) },
      { limit: 100 },
      { headers: {} },
    ])
      await expect(
        request('getModelSelection', (v) => v, { signal: signal(), ...extra }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    for (const kind of ['updateModelSelection', 'lookupModelSelectionCommand'] as const) {
      for (const extra of [
        { target: id(1) },
        { csrf: '' },
        { key: '' },
        { body: { text: '🧩'.repeat(4096) } },
      ])
        await expect(
          request(kind, (v) => v, {
            signal: signal(),
            body: {},
            csrf: 'S'.repeat(43),
            key: 'K',
            ...extra,
          }),
        ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each(['getModelSelection', 'updateModelSelection', 'lookupModelSelectionCommand'] as const)(
    'keeps %s success at 600000 bytes and joins a streamed overflow cancel',
    async (endpoint) => {
      const entered = deferred(),
        tail = deferred()
      const response = new Response(
        new ReadableStream<Uint8Array>({
          start(c) {
            c.enqueue(new Uint8Array(600001).fill(32))
          },
          cancel() {
            entered.resolve()
            return tail.promise
          },
        }),
        { headers: { 'Content-Type': 'application/json' } },
      )
      const request = accountTransport(async () => response),
        parse = vi.fn((v: unknown) => v)
      const write = { signal: signal(), body: {}, csrf: 'S'.repeat(43), key: 'K' }
      let ended = false
      const result = (
        endpoint === 'getModelSelection'
          ? request(endpoint, parse, { signal: write.signal })
          : request(endpoint, parse, write)
      )
        .catch((e: { kind: string }) => e.kind)
        .finally(() => {
          ended = true
        })
      try {
        await entered.promise
        expect(ended).toBe(false)
        expect(parse).not.toHaveBeenCalled()
      } finally {
        tail.resolve()
        await result
      }
      expect(await result).toBe('invalid-response')
      expect(ended).toBe(true)
      expect(parse).not.toHaveBeenCalled()
    },
  )
  it('allows exactly 600000 success bytes without broadening Provider or Problem budgets', async () => {
    const payload = JSON.stringify(selection()),
      body = payload + ' '.repeat(600000 - new TextEncoder().encode(payload).length)
    const api = createSystemModelSelectionAPI(
      async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    )
    expect(await api.getSelection(signal())).toEqual(selection())
    for (const endpoint of ['getModelSelection', 'getProvider', 'listProviders'] as const) {
      const request = accountTransport(
        async () =>
          new Response(' '.repeat(600001), {
            status: 403,
            headers: { 'Content-Type': 'application/problem+json' },
          }),
      )
      const result =
        endpoint === 'getModelSelection'
          ? request(endpoint, (v) => v, { signal: signal() })
          : endpoint === 'getProvider'
            ? request(endpoint, (v) => v, { signal: signal(), target: id(2) })
            : request(endpoint, (v) => v, { signal: signal(), providers: {} })
      await expect(result).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
})
