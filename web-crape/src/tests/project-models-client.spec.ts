import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureProjectConfigurationCommand,
  createProjectModelsAPI,
  createProjectModelSettingsAPI,
  parseProjectModel,
  parseProjectProvider,
  parseProjectAvailableChatModel,
  type ProjectConfigurationCommand,
  type ProjectModelCapabilities,
  type ProjectModelWriteInput,
  type ProjectProviderWriteInput,
} from '../api/project-models'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  providerID = id(20),
  modelID = id(30),
  maximum = '9223372036854775807'
const signal = () => new AbortController().signal
const write = () => ({ csrfToken: 'a'.repeat(43), key: 'original:model-command', signal: signal() })
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
const page = (items: unknown[] = [], next_cursor: unknown = null) => ({ items, next_cursor })
const capabilities = (): ProjectModelCapabilities => ({
  tool_calls: true,
  parallel_tool_calls: true,
  streaming: true,
  reasoning: true,
  input_modalities: ['text', 'image'],
  output_modalities: ['text'],
  reasoning_efforts: [],
  structured_output_modes: ['text', 'json_schema'],
  context_length: maximum,
  max_output: '9007199254740993',
})
const providerInput = (): ProjectProviderWriteInput => ({
  name: '  Provider 名称  ',
  protocol: 'openai-chat-completions',
  base_url: 'HTTPS://Models.Example:443/v1/%2f',
  enabled: true,
  credential_ref: null,
  options: {},
})
const modelInput = (): ProjectModelWriteInput => ({
  name: '  Chat  ',
  provider_model_id: ' native-model ',
  type: 'chat',
  enabled: true,
  parameters: {},
  request_overwrite: {},
  header_overwrite: {},
  capabilities: { ...capabilities(), reasoning_efforts: [] },
})
const provider = (n = 20) => ({
  id: id(n),
  scope: { kind: 'project', project_id: project },
  input: providerInput(),
  version: '9007199254740993',
  created_at: '0000-02-29T01:02:03.123456Z',
  updated_at: '2026-10-08T01:02:03.123456Z',
})
const model = (n = 30) => ({ ...provider(n), provider_id: providerID, input: modelInput() })
const available = (n = 30, system = false) => ({
  id: id(n),
  provider_id: providerID,
  scope: system ? { kind: 'system' } : { kind: 'project', project_id: project },
  name: 'Chat',
  provider_name: 'Provider',
  version: maximum,
  capabilities: capabilities(),
})
const receipt = (
  kind: ProjectConfigurationCommand['kind'],
  resource_id = kind.startsWith('model.') ? modelID : providerID,
  version = kind.endsWith('.create') ? '1' : '9007199254740994',
) => ({ kind, resource_id, version, affected_references: '0' })
function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
function problem(status = 409, code = 'VERSION_CONFLICT') {
  return {
    type: 'urn:agenteam:problem:request-failed',
    title: 'Request failed',
    status,
    detail: 'Request failed.',
    instance: `/api/v1/projects/${project}/models`,
    code,
    request_id: id(99),
    commit_state: 'not_started',
  }
}

describe('Project model five reads and exact safe projections', () => {
  it('uses five ordinary Project endpoints and one combined 17-method default dependency', async () => {
    const replies = [page([provider()]), provider(), page([model()]), model(), page([available()])]
    const fetch = vi.fn<Fetch>(async () => json(replies.shift())),
      api = createProjectModelSettingsAPI(fetch)
    expect(Object.keys(api)).toHaveLength(17)
    const result = [
      await api.listProviders(project, {}, signal()),
      await api.getProvider(project, providerID, signal()),
      await api.listModels(project, {}, signal()),
      await api.getModel(project, modelID, signal()),
      await api.listAvailableChatModels(project, {}, signal()),
    ]
    expect(fetch.mock.calls.map(([path]) => path)).toEqual(
      [
        'model-providers',
        `model-providers/${providerID}`,
        'models',
        `models/${modelID}`,
        'available-chat-models',
      ].map((path) => `/api/v1/projects/${project}/${path}`),
    )
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.body).toBeUndefined()
      const headers = new Headers(init.headers)
      expect(headers.has('X-CSRF-Token') || headers.has('Idempotency-Key')).toBe(false)
    }
    expect(result.every(Object.isFrozen)).toBe(true)
    expect((result[1] as ReturnType<typeof provider>).input.base_url).toBe(providerInput().base_url)
  })
  it('accepts full read configurations without applying the narrower write policy', async () => {
    const p = {
      ...provider(),
      input: { ...providerInput(), options: { temperature: 0.75, nested: ['保留', null, true] } },
    }
    const m = {
      ...model(),
      input: {
        ...modelInput(),
        parameters: { temperature: 0.25 },
        request_overwrite: { vendor_option: ['a'] },
        header_overwrite: { 'X-Vendor-Option': 'safe value' },
        capabilities: {
          ...capabilities(),
          output_modalities: ['image'],
          reasoning_efforts: Array.from({ length: 1001 }, (_, n) => `effort.${n}`),
        },
      },
    }
    const fetch = vi.fn<Fetch>(async (path) => json(path.includes('/model-providers/') ? p : m)),
      api = createProjectModelsAPI(fetch)
    const gotProvider = await api.getProvider(project, providerID, signal()),
      gotModel = await api.getModel(project, modelID, signal())
    expect(gotProvider).toEqual(p)
    expect(gotModel).toEqual(m)
    expect(Object.isFrozen(gotProvider.input.options.nested)).toBe(true)
    expect(Object.isFrozen(gotModel.input.capabilities.reasoning_efforts)).toBe(true)
    expect(() =>
      captureProjectConfigurationCommand({
        kind: 'provider.create',
        input: gotProvider.input as ProjectProviderWriteInput,
      }),
    ).toThrow('invalid-input')
    expect(() =>
      captureProjectConfigurationCommand({
        kind: 'model.create',
        provider_id: providerID,
        protocol: 'openai-chat-completions',
        input: gotModel.input as ProjectModelWriteInput,
      }),
    ).toThrow('invalid-input')
  })
  it('allows both scopes in the seven-field directory without configuration fallback or N+1', async () => {
    const fetch = vi.fn<Fetch>(async () => json(page([available(31), available(30, true)]))),
      api = createProjectModelsAPI(fetch)
    const result = await api.listAvailableChatModels(project, { limit: 2 }, signal())
    expect(result.items.map((v) => Object.keys(v).length)).toEqual([7, 7])
    expect(result.items.map((v) => v.scope.kind)).toEqual(['project', 'system'])
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it.each([
    'base_url',
    'protocol',
    'credential_ref',
    'provider_model_id',
    'parameters',
    'created_at',
  ])('rejects an available-row private/configuration field %s', async (field) => {
    const fetch = vi.fn<Fetch>(async () =>
        json(page([{ ...available(), [field]: 'must not publish' }])),
      ),
      api = createProjectModelsAPI(fetch)
    await expect(api.listAvailableChatModels(project, {}, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it.each([
    (v: ReturnType<typeof provider>) => ({ ...v, scope: { kind: 'system' } }),
    (v: ReturnType<typeof provider>) => ({ ...v, scope: { ...v.scope, project_id: id(2) } }),
    (v: ReturnType<typeof provider>) => ({ ...v, version: 1 }),
    (v: ReturnType<typeof provider>) => ({ ...v, version: '9223372036854775808' }),
    (v: ReturnType<typeof provider>) => ({ ...v, created_at: '1900-02-29T01:02:03.123456Z' }),
    (v: ReturnType<typeof provider>) => ({ ...v, updated_at: '0000-01-01T01:02:03.123456Z' }),
    (v: ReturnType<typeof provider>) => ({ ...v, input: { ...v.input, name: '\ud800' } }),
    (v: ReturnType<typeof provider>) => ({ ...v, input: { ...v.input, options: null } }),
    (v: ReturnType<typeof provider>) => ({ ...v, extra: true }),
  ])('rejects the entire page for a malformed last row', async (change) => {
    const fetch = vi.fn<Fetch>(async () => json(page([provider(21), change(provider(20))]))),
      api = createProjectModelsAPI(fetch)
    await expect(api.listProviders(project, { limit: 2 }, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it.each([
    { ...capabilities(), parallel_tool_calls: true, tool_calls: false },
    { ...capabilities(), reasoning: false, reasoning_efforts: ['high'] },
    { ...capabilities(), reasoning_efforts: ['high', 'high'] },
    { ...capabilities(), reasoning_efforts: ['invalid space'] },
    { ...capabilities(), reasoning_efforts: ['x'.repeat(33)] },
    { ...capabilities(), input_modalities: null },
    { ...capabilities(), output_modalities: ['text', 'text'] },
    { ...capabilities(), structured_output_modes: ['xml'] },
    { ...capabilities(), context_length: '8', max_output: '9' },
    { ...capabilities(), max_output: 9007199254740993 },
    { ...capabilities(), context_length: undefined },
  ])('rejects capabilities outside the complete read contract', async (caps) => {
    const api = createProjectModelsAPI(async () =>
      json({ ...model(), input: { ...modelInput(), capabilities: caps } }),
    )
    await expect(api.getModel(project, modelID, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it.each([
    { parameters: [] },
    { request_overwrite: { Model: 'override' } },
    { header_overwrite: { Authorization: 'untrusted' } },
    { header_overwrite: { 'X-Vendor': 'a', 'x-vendor': 'b' } },
    { header_overwrite: { 'X-Vendor': 'a\r\nb' } },
    { parameters: { padding: 'x'.repeat(65536) } },
    { header_overwrite: { 'X-Vendor': 'x'.repeat(16384) } },
  ])('rejects malformed dynamic read configuration rather than discarding it', (change) => {
    expect(() =>
      parseProjectModel({ ...model(), input: { ...modelInput(), ...change } }, project),
    ).toThrow('invalid-response')
  })
  it('validates exact requested detail ID and full frozen projection', () => {
    expect(() => parseProjectProvider(provider(21), project, providerID)).toThrow(
      'invalid-response',
    )
    expect(() => parseProjectModel(model(31), project, modelID)).toThrow('invalid-response')
    expect(() =>
      parseProjectAvailableChatModel(
        { ...available(), scope: { kind: 'system', project_id: project } },
        project,
      ),
    ).toThrow('invalid-response')
  })
})

describe('Project model independent cursor pages', () => {
  it('captures and encodes a cursor once, preserves HTTP default 50 and all-provider models', async () => {
    const gate = deferred<Response>(),
      fetch = vi.fn<Fetch>(() => gate.promise),
      api = createProjectModelsAPI(fetch)
    const query = { cursor: 'opaque/中文? +', limit: 2 }
    const pending = api.listModels(project, query, signal())
    query.cursor = 'changed'
    query.limit = 1
    const url = new URL(fetch.mock.calls[0]![0], 'https://same.invalid')
    expect([...url.searchParams]).toEqual([
      ['cursor', 'opaque/中文? +'],
      ['limit', '2'],
    ])
    expect(url.searchParams.has('provider_id')).toBe(false)
    gate.resolve(json(page([model(31), { ...model(30), provider_id: id(21) }], 'next')))
    expect((await pending).items).toHaveLength(2)
    const all = Array.from({ length: 50 }, (_, n) => model(100 - n))
    await expect(
      createProjectModelsAPI(async () => json(page(all, 'next'))).listModels(project, {}, signal()),
    ).resolves.toMatchObject({ next_cursor: 'next' })
  })
  it.each([
    null,
    { provider_id: providerID },
    { limit: '25' },
    { limit: 0 },
    { limit: 101 },
    { limit: 1.5 },
    { cursor: '' },
    { cursor: '\udfff' },
    { cursor: '中'.repeat(2731) },
    { cursor: '/'.repeat(8192), limit: 100, extra: true },
  ])('rejects an invalid query before dispatch', async (query) => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectModelsAPI(fetch)
    // @ts-expect-error Deliberately exercising the untrusted input boundary.
    await expect(api.listModels(project, query, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    page(null as unknown as unknown[]),
    page([], ''),
    page([], 'next'),
    page([model(30)], 'next'),
    page([model(30), model(30)]),
    page([model(29), model(30)]),
    page([model(31), model(30), model(29)]),
    { items: [] },
  ])('rejects a malformed complete page or cursor watermark', async (body) => {
    const api = createProjectModelsAPI(async () => json(body))
    await expect(api.listModels(project, { limit: 2 }, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
})

describe('Project model six writes and configuration-only historical lookup', () => {
  it('sends precisely the six command bodies and lookup kind with the original key', async () => {
    const kinds = [
      'provider.create',
      'provider.update',
      'provider.delete',
      'model.create',
      'model.update',
      'model.delete',
    ] as const
    const replies: unknown[] = [
      ...kinds.map((kind) => receipt(kind)),
      { found: true, receipt: receipt('model.delete') },
    ]
    const fetch = vi.fn<Fetch>(async () => json(replies.shift())),
      api = createProjectModelsAPI(fetch),
      options = write(),
      input = modelInput(),
      context = { provider_id: providerID, protocol: 'openai-chat-completions' as const }
    await api.createProvider(project, providerInput(), options)
    await api.updateProvider(project, providerID, '9007199254740993', providerInput(), options)
    await api.deleteProvider(project, providerID, '9007199254740993', options)
    await api.createModel(project, context, input, options)
    await api.updateModel(project, { ...context, id: modelID }, '9007199254740993', input, options)
    await api.deleteModel(project, modelID, '9007199254740993', null, options)
    const observed = await api.lookupConfiguration(
      project,
      {
        kind: 'model.delete',
        id: modelID,
        expected_version: '9007199254740993',
        replacement: null,
      },
      options,
    )
    expect(observed).toEqual({ found: true, receipt: receipt('model.delete') })
    expect(Object.isFrozen(observed)).toBe(true)
    expect(
      fetch.mock.calls.map(([path, init]) => [
        path.slice(`/api/v1/projects/${project}/`.length),
        init.method,
        JSON.parse(String(init.body)),
      ]),
    ).toEqual([
      ['model-providers', 'POST', { input: providerInput() }],
      [
        `model-providers/${providerID}`,
        'PUT',
        { expected_version: '9007199254740993', input: providerInput() },
      ],
      [`model-providers/${providerID}`, 'DELETE', { expected_version: '9007199254740993' }],
      ['models', 'POST', { provider_id: providerID, input }],
      [`models/${modelID}`, 'PUT', { expected_version: '9007199254740993', input }],
      [`models/${modelID}`, 'DELETE', { expected_version: '9007199254740993', replacement: null }],
      ['model-commands/lookup', 'POST', { command: 'model.delete' }],
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init.credentials).toBe('same-origin')
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(options.csrfToken)
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(options.key)
    }
  })
  it('captures target, version, capabilities and options before awaiting any response', async () => {
    const gate = deferred<Response>(),
      fetch = vi.fn<Fetch>(() => gate.promise),
      api = createProjectModelsAPI(fetch)
    const input = modelInput(),
      target = {
        id: modelID,
        provider_id: providerID,
        protocol: 'openai-chat-completions' as const,
      },
      options = write()
    const pending = api.updateModel(project, target, '9007199254740993', input, options)
    target.id = id(99)
    target.provider_id = id(98)
    options.key = 'changed'
    options.csrfToken = 'z'.repeat(43)
    ;(input.capabilities.input_modalities as string[]).push('file')
    expect(
      JSON.parse(String(fetch.mock.calls[0]![1].body)).input.capabilities.input_modalities,
    ).toEqual(['text', 'image'])
    expect(fetch.mock.calls[0]![0].endsWith(modelID)).toBe(true)
    gate.resolve(json(receipt('model.update')))
    expect((await pending).resource_id).toBe(modelID)
  })
  it('keeps configuration MaxInt64 legal and leaves overflow to the typed server rejection', async () => {
    const p = problem(409, 'INVALID_STATE'),
      fetch = vi.fn<Fetch>(
        async () =>
          new Response(JSON.stringify(p), {
            status: 409,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': p.request_id },
          }),
      )
    await expect(
      createProjectModelsAPI(fetch).deleteModel(project, modelID, maximum, null, write()),
    ).rejects.toMatchObject({
      kind: 'problem',
      problem: { code: 'INVALID_STATE', commit_state: 'not_started' },
    })
    expect(JSON.parse(String(fetch.mock.calls[0]![1].body)).expected_version).toBe(maximum)
  })
  it.each([
    { kind: 'model.delete', id: modelID, expected_version: '1', replacement: modelID },
    { kind: 'model.delete', id: modelID, expected_version: '1' },
    { kind: 'provider.delete', id: providerID, expected_version: '01' },
    { kind: 'provider.delete', id: providerID, expected_version: '9223372036854775808' },
    { kind: 'provider.create', input: { ...providerInput(), protocol: 'openai-embeddings' } },
    { kind: 'provider.create', input: { ...providerInput(), options: { extra: true } } },
    {
      kind: 'model.create',
      provider_id: providerID,
      protocol: 'anthropic-messages',
      input: modelInput(),
    },
    {
      kind: 'model.create',
      provider_id: providerID,
      protocol: 'openai-chat-completions',
      input: { ...modelInput(), parameters: { x: 1 } },
    },
    {
      kind: 'model.create',
      provider_id: providerID,
      protocol: 'openai-chat-completions',
      input: { ...modelInput(), capabilities: { ...capabilities(), reasoning_efforts: ['high'] } },
    },
  ])('rejects an invalid command even before a passive lookup', async (command) => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectModelsAPI(fetch)
    await expect(
      api.lookupConfiguration(project, command as ProjectConfigurationCommand, write()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    'https://user@host/v1',
    'https://host?',
    'https://host#',
    'https://host:',
    'https://host:0',
    'https://host:65536',
    'https://host/%xy',
    'file://host/v1',
  ])('rejects invalid endpoint text %s without dispatch', async (base_url) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      createProjectModelsAPI(fetch).createProvider(
        project,
        { ...providerInput(), base_url },
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    { ...receipt('model.update'), kind: 'provider.update' },
    { ...receipt('model.update'), resource_id: id(99) },
    { ...receipt('model.update'), version: '9007199254740993' },
    { ...receipt('model.update'), affected_references: '1' },
    { ...receipt('model.update'), affected_references: 0 },
    { ...receipt('model.update'), extra: true },
  ])('rejects malformed or mismatched Execute receipts', async (body) => {
    const api = createProjectModelsAPI(async () => json(body))
    await expect(
      api.updateModel(
        project,
        { id: modelID, provider_id: providerID, protocol: 'openai-chat-completions' },
        '9007199254740993',
        modelInput(),
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    { observed: false, result: null },
    { found: false },
    { found: false, receipt: receipt('model.delete') },
    { found: true, receipt: null },
    { found: 'true', receipt: receipt('model.delete') },
    { found: true, receipt: receipt('provider.delete') },
    { found: false, receipt: null, in_progress: true },
  ])('does not accept Secret/Owner or malformed configuration lookup unions', async (body) => {
    const api = createProjectModelsAPI(async () => json(body))
    await expect(
      api.lookupConfiguration(
        project,
        {
          kind: 'model.delete',
          id: modelID,
          expected_version: '9007199254740993',
          replacement: null,
        },
        write(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})

describe('Project-specific native body and request budgets', () => {
  it.each([
    'listProviders',
    'getProvider',
    'listModels',
    'getModel',
    'listAvailableChatModels',
  ] as const)('applies the 8 MiB success cap, not Owner/System caps, to %s', async (method) => {
    const value = method === 'getProvider' ? provider() : method === 'getModel' ? model() : page()
    const raw = JSON.stringify(value),
      bytes = new TextEncoder().encode(raw).byteLength
    for (const extra of [0, 1]) {
      const fetch = vi.fn<Fetch>(
          async () =>
            new Response(raw + ' '.repeat(8388608 - bytes + extra), {
              headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
            }),
        ),
        api = createProjectModelsAPI(fetch)
      const result =
        method === 'getProvider'
          ? api.getProvider(project, providerID, signal())
          : method === 'getModel'
            ? api.getModel(project, modelID, signal())
            : api[method](project, {}, signal())
      if (extra) await expect(result).rejects.toMatchObject({ kind: 'invalid-response' })
      else await expect(result).resolves.toEqual(value)
    }
  })
  it('publishes no valid prefix before actual EOF and joins cancellation before settling', async () => {
    let controller!: ReadableStreamDefaultController<Uint8Array>
    const cancel = deferred(),
      stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c
        },
        cancel: () => cancel.promise,
      })
    const abort = new AbortController(),
      api = createProjectModelsAPI(
        async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
      )
    let settled = false
    const pending = api.listModels(project, {}, abort.signal).finally(() => {
      settled = true
    })
    const outcome = expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    controller.enqueue(new TextEncoder().encode(JSON.stringify(page())))
    await flushPromises()
    expect(settled).toBe(false)
    abort.abort()
    await flushPromises()
    expect(settled).toBe(false)
    cancel.resolve()
    await outcome
    expect(settled).toBe(true)
    expect(stream.locked).toBe(false)
  })
  it.each(['truncated', 'invalid-utf8', 'wrong-media'])(
    'rejects %s and waits for its actual body tail',
    async (kind) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>
      const cancelled = deferred(),
        cancel = vi.fn(() => cancelled.promise)
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c
        },
        cancel,
      })
      const api = createProjectModelsAPI(
        async () =>
          new Response(stream, {
            headers: { 'Content-Type': kind === 'wrong-media' ? 'text/html' : 'application/json' },
          }),
      )
      let settled = false
      const pending = api.listProviders(project, {}, signal()).finally(() => {
          settled = true
        }),
        outcome = expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
      if (kind === 'invalid-utf8') controller.enqueue(new Uint8Array([0xff]))
      if (kind === 'truncated') {
        controller.enqueue(new TextEncoder().encode('{"items":'))
        controller.close()
      }
      await flushPromises()
      if (kind !== 'truncated') {
        expect(cancel).toHaveBeenCalledOnce()
        expect(settled).toBe(false)
      }
      cancelled.resolve()
      await outcome
      expect(stream.locked).toBe(false)
    },
  )
  it('retains complete legal configuration pages larger than the old generic cap', async () => {
    const rows = Array.from({ length: 12 }, (_, n) => ({
      ...provider(100 - n),
      input: { ...providerInput(), options: { safe: 'x'.repeat(60000) } },
    }))
    const api = createProjectModelsAPI(async () => json(page(rows)))
    expect((await api.listProviders(project, { limit: 25 }, signal())).items).toEqual(rows)
  })
  it('keeps the 600000-byte Problem cap and validates a complete typed Problem', async () => {
    const p = problem(),
      raw = JSON.stringify(p)
    for (const size of [600000, 600001]) {
      const api = createProjectModelsAPI(
        async () =>
          new Response(raw + ' '.repeat(size - new TextEncoder().encode(raw).byteLength), {
            status: 409,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': p.request_id },
          }),
      )
      await expect(api.listProviders(project, {}, signal())).rejects.toMatchObject({
        kind: size === 600000 ? 'problem' : 'invalid-response',
      })
    }
  })
  it('uses 1024-byte configuration receipts and lookups', async () => {
    for (const lookup of [false, true])
      for (const extra of [0, 1]) {
        const value = lookup ? { found: false, receipt: null } : receipt('provider.create'),
          raw = JSON.stringify(value)
        const api = createProjectModelsAPI(
          async () =>
            new Response(raw + ' '.repeat(1024 - raw.length + extra), {
              headers: { 'Content-Type': 'application/json' },
            }),
        )
        const pending = lookup
          ? api.lookupConfiguration(
              project,
              { kind: 'provider.create', input: providerInput() },
              write(),
            )
          : api.createProvider(project, providerInput(), write())
        if (extra) await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
        else await expect(pending).resolves.toEqual(value)
      }
  })
  it('rejects unknown transport options and enforces fixed configuration request cap', async () => {
    const fetch = vi.fn<Fetch>(async () => json({})),
      request = accountTransport(fetch),
      options = { signal: signal(), projectID: project, csrf: 'a'.repeat(43), key: 'original' }
    const body = { padding: 'x'.repeat(1048576 - 14) }
    expect(new TextEncoder().encode(JSON.stringify(body))).toHaveLength(1048576)
    await request('createProjectModelProvider', (v) => v, { ...options, body })
    await expect(
      request('createProjectModelProvider', (v) => v, {
        ...options,
        body: { padding: body.padding + 'x' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      // @ts-expect-error A caller cannot choose a cap or smuggle GET write options.
      request('listProjectModels', (v) => v, { ...options, projectModels: {}, maximum: 999999999 }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).toHaveBeenCalledOnce()
  })
})

describe('regression: opaque cursors and original configuration bytes', () => {
  it.each([
    'listProjectModelProviders',
    'listProjectModels',
    'listProjectAvailableChatModels',
  ] as const)('also closes the NUL boundary on the %s transport overload', async (endpoint) => {
    const fetch = vi.fn<Fetch>()
    await expect(
      accountTransport(fetch)(endpoint, (value) => value, {
        signal: signal(),
        projectID: project,
        projectModels: { cursor: 'opaque\0tail' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each(['listProviders', 'listModels', 'listAvailableChatModels'] as const)(
    'rejects NUL in %s queries before any dispatch',
    async (method) => {
      const fetch = vi.fn<Fetch>(async () => json(page()))
      await expect(
        createProjectModelsAPI(fetch)[method](project, { cursor: 'opaque\0tail' }, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it.each(['listProviders', 'listModels', 'listAvailableChatModels'] as const)(
    'rejects the entire full %s page for a NUL next cursor',
    async (method) => {
      const item =
        method === 'listProviders' ? provider() : method === 'listModels' ? model() : available()
      const fetch = vi.fn<Fetch>(async () => json(page([item], 'opaque\0tail')))
      await expect(
        createProjectModelsAPI(fetch)[method](project, { limit: 1 }, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(fetch).toHaveBeenCalledOnce()
    },
  )
  function configurationResponse(
    field: 'options' | 'parameters' | 'request_overwrite',
    raw: string,
    overwriteHeaders?: Record<string, string>,
  ) {
    const value = field === 'options' ? provider() : model()
    const body = JSON.stringify({
      ...value,
      input: {
        ...value.input,
        [field]: 'RAW_CONFIG_MARKER',
        ...(overwriteHeaders ? { header_overwrite: overwriteHeaders } : {}),
      },
    }).replace('"RAW_CONFIG_MARKER"', raw)
    return new Response(body, { headers: { 'Content-Type': 'application/json' } })
  }
  it('keeps legal opaque Unicode cursors and encodes their literal escapes only once', async () => {
    const cursor = '游标 %2F/ +',
      fetch = vi.fn<Fetch>(async () => json(page()))
    const api = createProjectModelsAPI(fetch)
    for (const method of ['listProviders', 'listModels', 'listAvailableChatModels'] as const)
      await api[method](project, { cursor, limit: 25 }, signal())
    for (const [path] of fetch.mock.calls)
      expect(new URL(path, 'https://same.invalid').searchParams.get('cursor')).toBe(cursor)
    expect(fetch).toHaveBeenCalledTimes(3)
  })
  it.each(['options', 'parameters', 'request_overwrite'] as const)(
    'accepts legal raw exponent values in %s without counting JS number reserialization',
    async (field) => {
      const raw = '{"x":[' + Array(8192).fill('1e-6').join(',') + ']}'
      expect(raw).toHaveLength(40967)
      expect(JSON.stringify(JSON.parse(raw))).toHaveLength(73735)
      const api = createProjectModelsAPI(async () => configurationResponse(field, raw))
      const result =
        field === 'options'
          ? await api.getProvider(project, providerID, signal())
          : await api.getModel(project, modelID, signal())
      expect((result.input as unknown as Record<string, unknown>)[field]).toEqual(JSON.parse(raw))
      expect(Object.isFrozen((result.input as unknown as Record<string, unknown>)[field])).toBe(
        true,
      )
    },
  )
  it('does not misinterpret a literal backslash-u string as injected Go HTML escaping', async () => {
    const raw = JSON.stringify({ x: '\\u003c'.repeat(10000) })
    expect(raw.length).toBeGreaterThan(65536)
    const api = createProjectModelsAPI(async () => configurationResponse('options', raw))
    await expect(api.getProvider(project, providerID, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('accounts for Go escapes in JSON object keys and Unicode line-separator strings', async () => {
    for (const raw of [
      '{"' + '\\u003c'.repeat(60000) + '":1}',
      '{"x":"' + '\\u2028\\u2029'.repeat(10000) + '"}',
    ]) {
      expect(raw.length).toBeGreaterThan(65536)
      const value = await createProjectModelsAPI(async () =>
        configurationResponse('options', raw),
      ).getProvider(project, providerID, signal())
      expect(value.input.options).toEqual(JSON.parse(raw))
    }
  })
  it('retains structural depth and protected-header/overwrite rules alongside raw size accounting', async () => {
    for (const [levels, allowed] of [
      [31, true],
      [32, false],
    ] as const) {
      const raw = '{"x":'.repeat(levels) + '1e-6' + '}'.repeat(levels)
      const api = createProjectModelsAPI(async () => configurationResponse('options', raw)),
        pending = api.getProvider(project, providerID, signal())
      if (allowed) expect((await pending).input.options).toEqual(JSON.parse(raw))
      else await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(
      createProjectModelsAPI(async () =>
        configurationResponse('request_overwrite', '{"MODEL":"forbidden"}'),
      ).getModel(project, modelID, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createProjectModelsAPI(async () =>
        configurationResponse('request_overwrite', '{"x":[1e-6]}', { Cookie: 'forbidden' }),
      ).getModel(project, modelID, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('supports direct typed-parser values without treating JS exponential formatting as stored bytes', () => {
    const options = JSON.parse('{"x":[' + Array(8192).fill('1e-6').join(',') + ']}')
    expect(
      parseProjectProvider({ ...provider(), input: { ...providerInput(), options } }, project).input
        .options,
    ).toEqual(options)
  })
  it.each(['options', 'parameters', 'request_overwrite'] as const)(
    'allows Go RawMessage HTML expansion in %s without shrinking data',
    async (field) => {
      const raw = '{"x":"' + '\\u003c'.repeat(60000) + '"}'
      expect(raw.length).toBeGreaterThan(65536)
      const api = createProjectModelsAPI(async () => configurationResponse(field, raw))
      const result =
        field === 'options'
          ? await api.getProvider(project, providerID, signal())
          : await api.getModel(project, modelID, signal())
      expect((result.input as unknown as Record<string, { x: string }>)[field]!.x).toBe(
        '<'.repeat(60000),
      )
    },
  )
  it('preserves the request-overwrite plus decoded header byte limit at its exact boundary', async () => {
    const raw = '{"x":[' + Array(11000).fill('1e-6').join(',') + ']}'
    const name = 'X-Limit',
      remaining = 65536 - raw.length - name.length
    expect(remaining).toBeGreaterThan(0)
    expect(remaining + name.length).toBeLessThan(16384)
    for (const extra of [0, 1]) {
      const api = createProjectModelsAPI(async () =>
        configurationResponse('request_overwrite', raw, { [name]: 'x'.repeat(remaining + extra) }),
      )
      const pending = api.getModel(project, modelID, signal())
      if (extra) await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
      else expect((await pending).input.request_overwrite).toEqual(JSON.parse(raw))
    }
  })
  it.each(['options', 'parameters', 'request_overwrite'] as const)(
    'still rejects a numeric %s configuration whose original compact bytes exceed 64 KiB',
    async (field) => {
      const raw = '{"x":[' + Array(13106).fill('1e-6').join(',') + ']}'
      expect(raw.length).toBeGreaterThan(65536)
      const api = createProjectModelsAPI(async () => configurationResponse(field, raw))
      const pending =
        field === 'options'
          ? api.getProvider(project, providerID, signal())
          : api.getModel(project, modelID, signal())
      await expect(pending).rejects.toMatchObject({ kind: 'invalid-response' })
    },
  )
})
