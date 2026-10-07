import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  createSystemProviderAPI,
  parseProviderModel,
  parseModelCapabilities,
  providerProtocols,
  type Provider,
  type ProviderModel,
  type ProviderProtocol,
  type ModelType,
  type ConfigurationPage,
  type ProviderQuery,
  type ProviderModelQuery,
} from './system-providers'

export type SystemModel = ProviderModel
export type ModelInput = SystemModel['input']
// Captured Provider context validates the response/configuration; it is never
// sent as extra query/body members or refreshed before historical replay.
export type ModelContext = Readonly<{ provider_id: string; protocol: ProviderProtocol }>
export type ModelTarget = ModelContext & Readonly<{ id: string }>
export type ModelCommand =
  | (ModelContext & Readonly<{ kind: 'model.create'; input: ModelInput }>)
  | (ModelTarget & Readonly<{ kind: 'model.update'; expected_version: string; input: ModelInput }>)
  | (ModelTarget &
      Readonly<{ kind: 'model.delete'; expected_version: string; replacement: string | null }>)
export type ModelReceipt = Readonly<{
  kind: ModelCommand['kind']
  resource_id: string
  version: string
  affected_references: string
}>
export type ModelObservation =
  Readonly<{ found: false; receipt: null }> | Readonly<{ found: true; receipt: ModelReceipt }>
const referenceKinds = [
  ['agent', 'agent_model', 'required'],
  ['agent', 'approval_model', 'required'],
  ['platform_selector', 'embedding', 'required'],
  ['platform_selector', 'image', 'optional'],
  ['platform_selector', 'meeting_summary', 'required'],
  ['platform_selector', 'memory', 'required'],
  ['platform_selector', 'reranker', 'optional'],
  ['project_summary', 'meeting_summary', 'required'],
] as const
export type ModelReferenceGroup = Readonly<{
  owner_kind: (typeof referenceKinds)[number][0]
  role: (typeof referenceKinds)[number][1]
  count: string
}>
export type ModelDeletionImpact = Readonly<{
  model_id: string
  version: string
  reference_count: string
  reference_groups: readonly ModelReferenceGroup[]
  replacement_requirement: 'none' | 'optional' | 'required'
  delete_blocker: 'reference_adapter_unbound' | null
}>
export interface SystemModelAPI {
  listProviders(query: ProviderQuery, signal: AbortSignal): Promise<ConfigurationPage<Provider>>
  getProvider(id: string, signal: AbortSignal): Promise<Provider>
  listModels(
    query: ProviderModelQuery,
    signal: AbortSignal,
  ): Promise<ConfigurationPage<SystemModel>>
  getModel(target: ModelTarget, signal: AbortSignal): Promise<SystemModel>
  createModel(
    context: ModelContext,
    input: ModelInput,
    options: WriteOptions,
  ): Promise<ModelReceipt>
  updateModel(
    target: ModelTarget,
    expectedVersion: string,
    input: ModelInput,
    options: WriteOptions,
  ): Promise<ModelReceipt>
  deleteModel(
    target: ModelTarget,
    expectedVersion: string,
    replacement: string | null,
    options: WriteOptions,
  ): Promise<ModelReceipt>
  getDeletionImpact(id: string, signal: AbortSignal): Promise<ModelDeletionImpact>
  lookupModelCommand(command: ModelCommand, options: WriteOptions): Promise<ModelObservation>
}
const maximumVersion = 9223372036854775807n
const encoder = new TextEncoder()
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function integer(value: unknown, minimum = 1n, maximum = maximumVersion) {
  const result = string(value, 1, 19)
  requireValue(/^(0|[1-9][0-9]*)$/.test(result))
  requireValue(BigInt(result) >= minimum && BigInt(result) <= maximum)
  return result
}
function text(value: unknown, characters: number, bytes: number) {
  const result = string(value, 1, characters)
  for (let i = 0; i < result.length; i++) {
    const unit = result.charCodeAt(i)
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = result.charCodeAt(++i)
      requireValue(next >= 0xdc00 && next <= 0xdfff)
    } else requireValue(unit < 0xdc00 || unit > 0xdfff)
  }
  requireValue(!result.includes('\0') && encoder.encode(result).byteLength <= bytes)
  return result
}
function context(value: Record<string, unknown>): ModelContext {
  requireValue(providerProtocols.includes(value.protocol as ProviderProtocol))
  return Object.freeze({
    provider_id: id(value.provider_id),
    protocol: value.protocol as ProviderProtocol,
  })
}
export function modelTypeForProtocol(protocol: ProviderProtocol): ModelType {
  const supported = {
    'openai-chat-completions': 'chat',
    'anthropic-messages': 'chat',
    'openai-embeddings': 'embedding',
    'jina-rerank': 'reranker',
    'openai-images-generations': 'image_generation',
  } as const
  return supported[protocol]
}
function modelInput(value: unknown, protocol: ProviderProtocol): ModelInput {
  const v = shape(value, [
    'name',
    'provider_model_id',
    'type',
    'enabled',
    'parameters',
    'request_overwrite',
    'header_overwrite',
    'capabilities',
  ])
  const type = modelTypeForProtocol(protocol)
  requireValue(v.type === type && typeof v.enabled === 'boolean')
  shape(v.parameters, [])
  shape(v.request_overwrite, [])
  shape(v.header_overwrite, [])
  return Object.freeze({
    name: text(v.name, 128, 512),
    provider_model_id: text(v.provider_model_id, 256, 256),
    type,
    enabled: v.enabled as boolean,
    parameters: Object.freeze({}),
    request_overwrite: Object.freeze({}),
    header_overwrite: Object.freeze({}),
    capabilities: parseModelCapabilities(v.capabilities, type, protocol),
  })
}
export function modelCommandBody(command: ModelCommand) {
  if (command.kind === 'model.create')
    return Object.freeze({ provider_id: command.provider_id, input: command.input })
  if (command.kind === 'model.update')
    return Object.freeze({ expected_version: command.expected_version, input: command.input })
  return Object.freeze({
    expected_version: command.expected_version,
    replacement: command.replacement,
  })
}
export function captureModelCommand(value: ModelCommand): ModelCommand {
  return input(() => {
    requireValue(
      ['model.create', 'model.update', 'model.delete'].includes(
        shape(
          value,
          ['kind', 'provider_id', 'protocol'],
          ['id', 'expected_version', 'input', 'replacement'],
        ).kind as string,
      ),
    )
    const kind = value.kind
    const v = shape(value, [
      'kind',
      'provider_id',
      'protocol',
      ...(kind === 'model.create' ? [] : ['id', 'expected_version']),
      ...(kind === 'model.delete' ? ['replacement'] : ['input']),
    ])
    const captured = context(v)
    let result: ModelCommand
    if (kind === 'model.create')
      result = Object.freeze({ kind, ...captured, input: modelInput(v.input, captured.protocol) })
    else {
      const target = id(v.id)
      const expected_version = integer(v.expected_version, 1n, maximumVersion - 1n)
      if (kind === 'model.update')
        result = Object.freeze({
          kind,
          ...captured,
          id: target,
          expected_version,
          input: modelInput(v.input, captured.protocol),
        })
      else {
        const replacement = v.replacement === null ? null : id(v.replacement)
        requireValue(replacement !== target)
        result = Object.freeze({ kind, ...captured, id: target, expected_version, replacement })
      }
    }
    requireValue(encoder.encode(JSON.stringify(modelCommandBody(result))).byteLength <= 16 * 1024)
    return result
  })
}
function writeOptions(value: WriteOptions) {
  return input(() => {
    const v = shape(value, ['csrfToken', 'key', 'signal'])
    return { csrf: v.csrfToken as string, key: v.key as string, signal: v.signal as AbortSignal }
  })
}
function receipt(value: unknown, command: ModelCommand): ModelReceipt {
  const v = shape(value, ['kind', 'resource_id', 'version', 'affected_references'])
  requireValue(v.kind === command.kind)
  const result = Object.freeze({
    kind: command.kind,
    resource_id: id(v.resource_id),
    version: integer(v.version),
    affected_references: integer(v.affected_references, 0n),
  })
  requireValue(command.kind === 'model.delete' || result.affected_references === '0')
  requireValue(
    command.kind === 'model.create'
      ? result.version === '1'
      : result.resource_id === command.id &&
          BigInt(result.version) === BigInt(command.expected_version) + 1n,
  )
  return result
}
function impact(value: unknown, target: string): ModelDeletionImpact {
  const v = shape(value, [
    'model_id',
    'version',
    'reference_count',
    'reference_groups',
    'replacement_requirement',
    'delete_blocker',
  ])
  requireValue(id(v.model_id) === target)
  const count = integer(v.reference_count, 0n, 10000n)
  requireValue(Array.isArray(v.reference_groups) && v.reference_groups.length <= 8)
  let previous = '',
    total = 0n,
    required = false,
    unbound = false
  const groups = (v.reference_groups as unknown[]).map((value) => {
    const group = shape(value, ['owner_kind', 'role', 'count'])
    const known = referenceKinds.find(
      ([owner, role]) => owner === group.owner_kind && role === group.role,
    )
    requireValue(!!known)
    const [owner_kind, role, requirement] = known!
    const key = owner_kind + '/' + role
    requireValue(key > previous)
    previous = key
    const count = integer(group.count, 1n, 10000n)
    total += BigInt(count)
    required ||= requirement === 'required'
    unbound ||= owner_kind !== 'platform_selector'
    return Object.freeze({ owner_kind, role, count })
  })
  const replacement_requirement = required ? 'required' : groups.length ? 'optional' : 'none'
  const delete_blocker = unbound ? 'reference_adapter_unbound' : null
  requireValue(
    total === BigInt(count) &&
      v.replacement_requirement === replacement_requirement &&
      v.delete_blocker === delete_blocker,
  )
  return Object.freeze({
    model_id: target,
    version: integer(v.version),
    reference_count: count,
    reference_groups: Object.freeze(groups),
    replacement_requirement,
    delete_blocker,
  })
}
export function createSystemModelAPI(fetcher?: Fetch): SystemModelAPI {
  const request = accountTransport(fetcher)
  const providers = createSystemProviderAPI(fetcher)
  return {
    listProviders: providers.listProviders,
    getProvider: providers.getProvider,
    listModels: providers.listModels,
    async getModel(value, signal) {
      const target = input(() => {
        const v = shape(value, ['id', 'provider_id', 'protocol'])
        return Object.freeze({ id: id(v.id), ...context(v) })
      })
      return request(
        'getModel',
        (value) => {
          const result = parseProviderModel(value, target.provider_id, target.protocol)
          requireValue(result.id === target.id)
          return result
        },
        { target: target.id, signal },
      )
    },
    async createModel(value, model, write) {
      input(() => shape(value, ['provider_id', 'protocol']))
      const command = captureModelCommand({ ...value, kind: 'model.create', input: model })
      return request('createModel', (v) => receipt(v, command), {
        ...writeOptions(write),
        body: modelCommandBody(command),
      })
    },
    async updateModel(value, expected_version, model, write) {
      input(() => shape(value, ['id', 'provider_id', 'protocol']))
      const command = captureModelCommand({
        ...value,
        kind: 'model.update',
        expected_version,
        input: model,
      })
      return request('updateModel', (v) => receipt(v, command), {
        ...writeOptions(write),
        target: value.id,
        body: modelCommandBody(command),
      })
    },
    async deleteModel(value, expected_version, replacement, write) {
      input(() => shape(value, ['id', 'provider_id', 'protocol']))
      const command = captureModelCommand({
        ...value,
        kind: 'model.delete',
        expected_version,
        replacement,
      })
      return request('deleteModel', (v) => receipt(v, command), {
        ...writeOptions(write),
        target: value.id,
        body: modelCommandBody(command),
      })
    },
    async getDeletionImpact(value, signal) {
      const target = input(() => id(value))
      return request('getModelDeletionImpact', (v) => impact(v, target), { target, signal })
    },
    async lookupModelCommand(value, write) {
      const command = captureModelCommand(value)
      return request(
        'lookupModelCommand',
        (raw): ModelObservation => {
          const v = shape(raw, ['found', 'receipt'])
          if (v.found === false) {
            requireValue(v.receipt === null)
            return Object.freeze({ found: false, receipt: null })
          }
          requireValue(v.found === true)
          return Object.freeze({ found: true, receipt: receipt(v.receipt, command) })
        },
        { ...writeOptions(write), body: { command: command.kind } },
      )
    },
  }
}
