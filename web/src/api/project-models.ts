import type { WriteOptions } from './account'
import {
  AccountFailure,
  accountTransport,
  object,
  projectModelJSONBytes,
  shape,
  string,
  uuid7,
  type Fetch,
} from './client'
import { parseSystemInstant } from './system-account'
import {
  createProjectModelCredentialAPI,
  type ProjectModelCredentialsAPI,
} from './project-model-credentials'

export const projectChatProtocols = ['openai-chat-completions', 'anthropic-messages'] as const
export type ProjectChatProtocol = (typeof projectChatProtocols)[number]
export type ProjectModelPageQuery = Readonly<{ cursor?: string; limit?: number }>
export type ProjectModelPage<T> = Readonly<{ items: readonly T[]; next_cursor: string | null }>
export type EmptyObject = Readonly<Record<string, never>>
export type ProjectConfigurationJSON =
  | null
  | boolean
  | number
  | string
  | readonly ProjectConfigurationJSON[]
  | { readonly [key: string]: ProjectConfigurationJSON }
export type ProjectConfigurationObject = Readonly<Record<string, ProjectConfigurationJSON>>
export type ProjectModelScope = Readonly<{ kind: 'project'; project_id: string }>
export type ProjectModelModality = 'text' | 'image' | 'file' | 'vector'
export type ProjectModelStructuredOutput = 'text' | 'json_schema'
export type ProjectModelCapabilities = Readonly<{
  tool_calls: boolean
  parallel_tool_calls: boolean
  streaming: boolean
  reasoning: boolean
  input_modalities: readonly ProjectModelModality[]
  output_modalities: readonly ProjectModelModality[]
  reasoning_efforts: readonly string[]
  structured_output_modes: readonly ProjectModelStructuredOutput[]
  context_length: string | null
  max_output: string | null
}>
export type ProjectChatWriteCapabilities = Omit<ProjectModelCapabilities, 'reasoning_efforts'> &
  Readonly<{ reasoning_efforts: readonly [] }>
export type ProjectProviderInput = Readonly<{
  name: string
  protocol: ProjectChatProtocol
  base_url: string
  enabled: boolean
  credential_ref: string | null
  options: ProjectConfigurationObject
}>
export type ProjectProviderWriteInput = Omit<ProjectProviderInput, 'options'> &
  Readonly<{ options: EmptyObject }>
export type ProjectProvider = Readonly<{
  id: string
  scope: ProjectModelScope
  input: ProjectProviderInput
  version: string
  created_at: string
  updated_at: string
}>
export type ProjectModelInput = Readonly<{
  name: string
  provider_model_id: string
  type: 'chat'
  enabled: boolean
  parameters: ProjectConfigurationObject
  request_overwrite: ProjectConfigurationObject
  header_overwrite: Readonly<Record<string, string>>
  capabilities: ProjectModelCapabilities
}>
export type ProjectModelWriteInput = Omit<
  ProjectModelInput,
  'parameters' | 'request_overwrite' | 'header_overwrite' | 'capabilities'
> &
  Readonly<{
    parameters: EmptyObject
    request_overwrite: EmptyObject
    header_overwrite: EmptyObject
    capabilities: ProjectChatWriteCapabilities
  }>
export type ProjectModel = Readonly<{
  id: string
  provider_id: string
  scope: ProjectModelScope
  input: ProjectModelInput
  version: string
  created_at: string
  updated_at: string
}>
export type ProjectAvailableChatModel = Readonly<{
  id: string
  provider_id: string
  scope: ProjectModelScope | Readonly<{ kind: 'system' }>
  name: string
  provider_name: string
  version: string
  capabilities: ProjectModelCapabilities
}>
export type ProjectModelWriteContext = Readonly<{
  provider_id: string
  protocol: ProjectChatProtocol
}>
export type ProjectModelWriteTarget = ProjectModelWriteContext & Readonly<{ id: string }>
export type ProjectConfigurationKind =
  | 'provider.create'
  | 'provider.update'
  | 'provider.delete'
  | 'model.create'
  | 'model.update'
  | 'model.delete'
export type ProjectConfigurationReceipt<
  K extends ProjectConfigurationKind = ProjectConfigurationKind,
> = Readonly<{
  kind: K
  resource_id: string
  version: string
  affected_references: '0'
}>
export type ProjectConfigurationCommand =
  | Readonly<{ kind: 'provider.create'; input: ProjectProviderWriteInput }>
  | Readonly<{
      kind: 'provider.update'
      id: string
      expected_version: string
      input: ProjectProviderWriteInput
    }>
  | Readonly<{ kind: 'provider.delete'; id: string; expected_version: string }>
  | (ProjectModelWriteContext & Readonly<{ kind: 'model.create'; input: ProjectModelWriteInput }>)
  | (ProjectModelWriteTarget &
      Readonly<{ kind: 'model.update'; expected_version: string; input: ProjectModelWriteInput }>)
  | Readonly<{
      kind: 'model.delete'
      id: string
      expected_version: string
      replacement: string | null
    }>
export type ProjectConfigurationObservation =
  | Readonly<{ found: false; receipt: null }>
  | Readonly<{ found: true; receipt: ProjectConfigurationReceipt }>
export interface ProjectModelsAPI {
  listProviders(
    projectID: string,
    query: ProjectModelPageQuery,
    signal: AbortSignal,
  ): Promise<ProjectModelPage<ProjectProvider>>
  getProvider(projectID: string, providerID: string, signal: AbortSignal): Promise<ProjectProvider>
  listModels(
    projectID: string,
    query: ProjectModelPageQuery,
    signal: AbortSignal,
  ): Promise<ProjectModelPage<ProjectModel>>
  getModel(projectID: string, modelID: string, signal: AbortSignal): Promise<ProjectModel>
  listAvailableChatModels(
    projectID: string,
    query: ProjectModelPageQuery,
    signal: AbortSignal,
  ): Promise<ProjectModelPage<ProjectAvailableChatModel>>
  createProvider(
    projectID: string,
    input: ProjectProviderWriteInput,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'provider.create'>>
  updateProvider(
    projectID: string,
    providerID: string,
    expectedVersion: string,
    input: ProjectProviderWriteInput,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'provider.update'>>
  deleteProvider(
    projectID: string,
    providerID: string,
    expectedVersion: string,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'provider.delete'>>
  createModel(
    projectID: string,
    context: ProjectModelWriteContext,
    input: ProjectModelWriteInput,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'model.create'>>
  updateModel(
    projectID: string,
    target: ProjectModelWriteTarget,
    expectedVersion: string,
    input: ProjectModelWriteInput,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'model.update'>>
  deleteModel(
    projectID: string,
    modelID: string,
    expectedVersion: string,
    replacement: string | null,
    options: WriteOptions,
  ): Promise<ProjectConfigurationReceipt<'model.delete'>>
  lookupConfiguration(
    projectID: string,
    command: ProjectConfigurationCommand,
    options: WriteOptions,
  ): Promise<ProjectConfigurationObservation>
}
export type ProjectModelSettingsAPI = ProjectModelsAPI & ProjectModelCredentialsAPI

const encoder = new TextEncoder()
const maximumVersion = 9223372036854775807n
export function requireProjectModelValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
export function captureProjectModelInput<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function projectModelWellFormed(value: string) {
  return !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)
}
function text(value: unknown, bytes: number, characters = bytes): string {
  const result = string(value, 1, characters)
  requireProjectModelValue(
    projectModelWellFormed(result) &&
      !result.includes('\0') &&
      encoder.encode(result).byteLength <= bytes,
  )
  return result
}
export function parseProjectModelID(value: unknown): string {
  const result = string(value, 36, 36)
  requireProjectModelValue(uuid7.test(result))
  return result
}
export function captureProjectModelID(value: unknown): string {
  return captureProjectModelInput(() => parseProjectModelID(value))
}
export function parseProjectModelVersion(value: unknown): string {
  const result = string(value, 1, 19)
  requireProjectModelValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= maximumVersion)
  return result
}
function member<T extends string>(value: unknown, choices: readonly T[]): T {
  requireProjectModelValue(typeof value === 'string' && choices.includes(value as T))
  return value as T
}
function bool(value: unknown): boolean {
  requireProjectModelValue(typeof value === 'boolean')
  return value
}
export function projectModelByteBudget(value: unknown, maximum: number) {
  requireProjectModelValue(encoder.encode(JSON.stringify(value)).byteLength <= maximum)
}
function empty(value: unknown): EmptyObject {
  shape(value, [])
  return Object.freeze({})
}
function jsonValue(value: unknown, depth: number): ProjectConfigurationJSON {
  requireProjectModelValue(depth <= 32)
  if (value === null || typeof value === 'boolean') return value
  if (typeof value === 'number') {
    requireProjectModelValue(Number.isFinite(value))
    return value
  }
  if (typeof value === 'string') {
    requireProjectModelValue(projectModelWellFormed(value))
    return value
  }
  if (Array.isArray(value)) return Object.freeze(value.map((v) => jsonValue(v, depth + 1)))
  return Object.freeze(
    Object.fromEntries(
      Object.entries(object(value)).map(([key, v]) => {
        requireProjectModelValue(projectModelWellFormed(key))
        return [key, jsonValue(v, depth + 1)]
      }),
    ),
  )
}
const configurationBytes = new WeakMap<object, number>()
function configurationObject(value: unknown): ProjectConfigurationObject {
  object(value)
  const result = jsonValue(value, 1) as ProjectConfigurationObject
  const bytes = projectModelJSONBytes(value)
  requireProjectModelValue(bytes <= 65536)
  configurationBytes.set(result, bytes)
  return result
}
function headers(
  value: unknown,
  requestOverwrite: ProjectConfigurationObject,
): Readonly<Record<string, string>> {
  const seen = new Set<string>()
  let bytes = 0
  const result = Object.freeze(
    Object.fromEntries(
      Object.entries(object(value)).map(([name, v]) => {
        const lower = name.toLowerCase()
        requireProjectModelValue(
          /^[A-Za-z0-9-]+$/.test(name) &&
            !seen.has(lower) &&
            ![
              'authorization',
              'proxy-authorization',
              'x-api-key',
              'api-key',
              'host',
              'cookie',
              'set-cookie',
              'content-length',
              'transfer-encoding',
              'connection',
              'upgrade',
              'trailer',
              'te',
            ].includes(lower),
        )
        requireProjectModelValue(
          typeof v === 'string' && projectModelWellFormed(v) && !/[\r\n\0]/.test(v),
        )
        seen.add(lower)
        bytes += encoder.encode(name).byteLength + encoder.encode(v).byteLength
        return [name, v]
      }),
    ),
  )
  requireProjectModelValue(
    bytes <= 16384 && bytes + configurationBytes.get(requestOverwrite)! <= 65536,
  )
  return result
}

// Preserve Go net/url endpoint text; WHATWG URL normalizes valid host/path text.
function hostByte(value: number) {
  return (
    (value >= 65 && value <= 90) ||
    (value >= 97 && value <= 122) ||
    (value >= 48 && value <= 57) ||
    '!$&\'()*+,;=:[ ]<>"-_.~'.replace(' ', '').includes(String.fromCharCode(value))
  )
}
function hostUnescape(value: string, zone = false) {
  let decoded = ''
  for (let index = 0; index < value.length; index++) {
    const unit = value.charCodeAt(index)
    if (unit === 37) {
      const escaped = value.slice(index + 1, index + 3)
      requireProjectModelValue(/^[0-9a-f]{2}$/i.test(escaped))
      const byte = Number.parseInt(escaped, 16)
      requireProjectModelValue(
        zone ? byte === 37 || byte === 32 || hostByte(byte) : byte >= 128 || byte === 37,
      )
      decoded += String.fromCharCode(byte)
      index += 2
    } else {
      requireProjectModelValue(unit >= 128 || hostByte(unit))
      decoded += value[index]
    }
  }
  return decoded
}
function ipv6(value: string) {
  const zone = value.indexOf('%')
  if (zone >= 0) {
    if (zone === value.length - 1) return false
    value = value.slice(0, zone)
  }
  if (!value.includes(':')) return false
  const sides = value.split('::')
  if (sides.length > 2) return false
  const pieces = sides.flatMap((part) => (part ? part.split(':') : []))
  let count = 0
  for (let index = 0; index < pieces.length; index++) {
    const part = pieces[index]!
    if (part.includes('.')) {
      const bytes = part.split('.')
      if (
        index !== pieces.length - 1 ||
        !value.endsWith(part) ||
        bytes.length !== 4 ||
        !bytes.every((byte) => /^(0|[1-9][0-9]{0,2})$/.test(byte) && Number(byte) <= 255)
      )
        return false
      count += 2
    } else {
      if (!/^[0-9a-f]{1,4}$/i.test(part)) return false
      count++
    }
  }
  return sides.length === 2 ? count < 8 : count === 8
}
function endpoint(value: unknown) {
  const result = text(value, 8192)
  requireProjectModelValue(
    /^https?:\/\//i.test(result) &&
      !/[\x00-\x20\x7f?#]/.test(result) &&
      !/%(?![0-9a-f]{2})/i.test(result),
  )
  const authority = result.slice(result.indexOf('//') + 2).split('/')[0]!
  requireProjectModelValue(!authority.includes('@') && !authority.endsWith(':'))
  let host = authority,
    port = ''
  const bracket = authority.lastIndexOf('[')
  requireProjectModelValue(bracket <= 0)
  if (bracket === 0) {
    const close = authority.lastIndexOf(']')
    requireProjectModelValue(close > 0)
    port = authority.slice(close + 1)
    requireProjectModelValue(port === '' || /^:[0-9]+$/.test(port))
    const literal = authority.slice(1, close),
      zone = literal.indexOf('%25')
    host =
      zone < 0
        ? hostUnescape(literal)
        : hostUnescape(literal.slice(0, zone)) + hostUnescape(literal.slice(zone), true)
    requireProjectModelValue(ipv6(host))
  } else {
    const colon = authority.indexOf(':')
    if (colon >= 0) {
      host = authority.slice(0, colon)
      port = authority.slice(colon)
    }
    requireProjectModelValue(port === '' || /^:[0-9]+$/.test(port))
    host = hostUnescape(host)
  }
  requireProjectModelValue(
    host !== '' && (port === '' || (BigInt(port.slice(1)) > 0n && BigInt(port.slice(1)) <= 65535n)),
  )
  return result
}
function uniqueStrings<T extends string>(
  value: unknown,
  parse: (value: unknown) => T,
): readonly T[] {
  requireProjectModelValue(Array.isArray(value))
  const result = value.map(parse)
  requireProjectModelValue(new Set(result).size === result.length)
  return Object.freeze(result)
}
export function parseProjectModelCapabilities(value: unknown): ProjectModelCapabilities {
  const v = {
    ...shape(value, [
      'tool_calls',
      'parallel_tool_calls',
      'streaming',
      'reasoning',
      'input_modalities',
      'output_modalities',
      'reasoning_efforts',
      'structured_output_modes',
      'context_length',
      'max_output',
    ]),
  }
  const modality = (item: unknown) => member(item, ['text', 'image', 'file', 'vector'] as const)
  const result = Object.freeze({
    tool_calls: bool(v.tool_calls),
    parallel_tool_calls: bool(v.parallel_tool_calls),
    streaming: bool(v.streaming),
    reasoning: bool(v.reasoning),
    input_modalities: uniqueStrings(v.input_modalities, modality),
    output_modalities: uniqueStrings(v.output_modalities, modality),
    reasoning_efforts: uniqueStrings(v.reasoning_efforts, (item) => {
      const effort = text(item, 32)
      requireProjectModelValue(/^[A-Za-z0-9_.:-]+$/.test(effort))
      return effort
    }),
    structured_output_modes: uniqueStrings(v.structured_output_modes, (item) =>
      member(item, ['text', 'json_schema'] as const),
    ),
    context_length: v.context_length === null ? null : parseProjectModelVersion(v.context_length),
    max_output: v.max_output === null ? null : parseProjectModelVersion(v.max_output),
  })
  requireProjectModelValue(
    (!result.parallel_tool_calls || result.tool_calls) &&
      (result.reasoning || result.reasoning_efforts.length === 0),
  )
  requireProjectModelValue(
    result.context_length === null ||
      result.max_output === null ||
      BigInt(result.max_output) <= BigInt(result.context_length),
  )
  return result
}
function providerInput(value: unknown): ProjectProviderInput {
  const v = {
    ...shape(value, ['name', 'protocol', 'base_url', 'enabled', 'credential_ref', 'options']),
  }
  return Object.freeze({
    name: text(v.name, 512, 128),
    protocol: member(v.protocol, projectChatProtocols),
    base_url: endpoint(v.base_url),
    enabled: bool(v.enabled),
    credential_ref: v.credential_ref === null ? null : parseProjectModelID(v.credential_ref),
    options: configurationObject(v.options),
  })
}
function modelInput(value: unknown): ProjectModelInput {
  const v = {
    ...shape(value, [
      'name',
      'provider_model_id',
      'type',
      'enabled',
      'parameters',
      'request_overwrite',
      'header_overwrite',
      'capabilities',
    ]),
  }
  const request_overwrite = configurationObject(v.request_overwrite)
  requireProjectModelValue(
    !Object.keys(request_overwrite).some((key) =>
      [
        'model',
        'messages',
        'input',
        'prompt',
        'query',
        'documents',
        'tools',
        'tool_choice',
        'url',
        'base_url',
        'authorization',
        'api_key',
        'credential',
        'headers',
        'stream',
        'stream_options',
      ].includes(key.toLowerCase()),
    ),
  )
  return Object.freeze({
    name: text(v.name, 512, 128),
    provider_model_id: text(v.provider_model_id, 256),
    type: member(v.type, ['chat'] as const),
    enabled: bool(v.enabled),
    parameters: configurationObject(v.parameters),
    request_overwrite,
    header_overwrite: headers(v.header_overwrite, request_overwrite),
    capabilities: parseProjectModelCapabilities(v.capabilities),
  })
}
function scope(value: unknown, projectID: string): ProjectModelScope {
  const v = shape(value, ['kind', 'project_id'])
  requireProjectModelValue(v.kind === 'project' && parseProjectModelID(v.project_id) === projectID)
  return Object.freeze({ kind: 'project', project_id: projectID })
}
function times(v: Record<string, unknown>) {
  const created_at = parseSystemInstant(v.created_at),
    updated_at = parseSystemInstant(v.updated_at)
  requireProjectModelValue(created_at <= updated_at)
  return { created_at, updated_at }
}
export function parseProjectProvider(
  value: unknown,
  projectID: string,
  expectedID?: string,
): ProjectProvider {
  const v = shape(value, ['id', 'scope', 'input', 'version', 'created_at', 'updated_at'])
  const id = parseProjectModelID(v.id)
  requireProjectModelValue(expectedID === undefined || id === expectedID)
  return Object.freeze({
    id,
    scope: scope(v.scope, projectID),
    input: providerInput(v.input),
    version: parseProjectModelVersion(v.version),
    ...times(v),
  })
}
export function parseProjectModel(
  value: unknown,
  projectID: string,
  expectedID?: string,
): ProjectModel {
  const v = shape(value, [
    'id',
    'provider_id',
    'scope',
    'input',
    'version',
    'created_at',
    'updated_at',
  ])
  const id = parseProjectModelID(v.id)
  requireProjectModelValue(expectedID === undefined || id === expectedID)
  return Object.freeze({
    id,
    provider_id: parseProjectModelID(v.provider_id),
    scope: scope(v.scope, projectID),
    input: modelInput(v.input),
    version: parseProjectModelVersion(v.version),
    ...times(v),
  })
}
export function parseProjectAvailableChatModel(
  value: unknown,
  projectID: string,
): ProjectAvailableChatModel {
  const v = shape(value, [
    'id',
    'provider_id',
    'scope',
    'name',
    'provider_name',
    'version',
    'capabilities',
  ])
  let modelScope: ProjectAvailableChatModel['scope']
  if (object(v.scope).kind === 'system') {
    shape(v.scope, ['kind'])
    modelScope = Object.freeze({ kind: 'system' })
  } else modelScope = scope(v.scope, projectID)
  return Object.freeze({
    id: parseProjectModelID(v.id),
    provider_id: parseProjectModelID(v.provider_id),
    scope: modelScope,
    name: text(v.name, 512, 128),
    provider_name: text(v.provider_name, 512, 128),
    version: parseProjectModelVersion(v.version),
    capabilities: parseProjectModelCapabilities(v.capabilities),
  })
}
function cursor(value: unknown): string {
  const result = string(value, 1, 8192)
  requireProjectModelValue(
    projectModelWellFormed(result) &&
      !result.includes('\0') &&
      encoder.encode(result).byteLength <= 8192,
  )
  return result
}
export function captureProjectModelQuery(value: ProjectModelPageQuery): ProjectModelPageQuery {
  return captureProjectModelInput(() => {
    const v = shape(value, [], ['cursor', 'limit'])
    const result: { cursor?: string; limit?: number } = {}
    if (Object.hasOwn(v, 'cursor')) result.cursor = cursor(v.cursor)
    if (Object.hasOwn(v, 'limit')) {
      const limit = v.limit
      requireProjectModelValue(
        typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100,
      )
      result.limit = limit
    }
    const params = new URLSearchParams()
    if (result.cursor !== undefined) params.set('cursor', result.cursor)
    if (result.limit !== undefined) params.set('limit', String(result.limit))
    requireProjectModelValue(encoder.encode(params.toString()).byteLength <= 32768)
    return Object.freeze(result)
  })
}
export function parseProjectModelPage<T extends { id: string }>(
  value: unknown,
  query: ProjectModelPageQuery,
  parse: (value: unknown) => T,
  createdAt?: (item: T) => string,
): ProjectModelPage<T> {
  const v = shape(value, ['items', 'next_cursor']),
    limit = query.limit ?? 50
  requireProjectModelValue(Array.isArray(v.items) && v.items.length <= limit)
  const seen = new Set<string>()
  let previous: T | undefined
  const items = v.items.map((item) => {
    const result = parse(item)
    requireProjectModelValue(!seen.has(result.id))
    if (createdAt && previous)
      requireProjectModelValue(
        createdAt(result) < createdAt(previous) ||
          (createdAt(result) === createdAt(previous) && result.id < previous.id),
      )
    seen.add(result.id)
    previous = result
    return result
  })
  const next_cursor = v.next_cursor === null ? null : cursor(v.next_cursor)
  requireProjectModelValue(next_cursor === null || items.length === limit)
  return Object.freeze({ items: Object.freeze(items), next_cursor })
}
export function captureProjectModelWriteOptions(value: WriteOptions) {
  return captureProjectModelInput(() => {
    const v = shape(value, ['csrfToken', 'key'], ['signal'])
    const csrf = string(v.csrfToken, 43, 43),
      key = string(v.key, 1, 128),
      signal = v.signal
    requireProjectModelValue(
      /^[A-Za-z0-9_-]{43}$/.test(csrf) && /^[A-Za-z0-9._:/-]{1,128}$/.test(key),
    )
    requireProjectModelValue(signal === undefined || signal instanceof AbortSignal)
    return Object.freeze({ csrf, key, signal: signal ?? new AbortController().signal })
  })
}
function providerWriteInput(value: unknown): ProjectProviderWriteInput {
  const result = providerInput(value)
  return Object.freeze({ ...result, options: empty(result.options) })
}
function modelWriteInput(value: unknown, protocol: ProjectChatProtocol): ProjectModelWriteInput {
  const result = modelInput(value),
    c = result.capabilities
  requireProjectModelValue(
    c.reasoning_efforts.length === 0 &&
      c.output_modalities.every((v) => v === 'text') &&
      (protocol !== 'anthropic-messages' || !c.structured_output_modes.includes('json_schema')),
  )
  return Object.freeze({
    ...result,
    parameters: empty(result.parameters),
    request_overwrite: empty(result.request_overwrite),
    header_overwrite: empty(result.header_overwrite),
    capabilities: Object.freeze({ ...c, reasoning_efforts: Object.freeze([]) as readonly [] }),
  })
}
export function captureProjectModelContext(
  value: ProjectModelWriteContext,
): ProjectModelWriteContext {
  return captureProjectModelInput(() => {
    const v = shape(value, ['provider_id', 'protocol'])
    return Object.freeze({
      provider_id: parseProjectModelID(v.provider_id),
      protocol: member(v.protocol, projectChatProtocols),
    })
  })
}
export function projectConfigurationBody(command: ProjectConfigurationCommand) {
  switch (command.kind) {
    case 'provider.create':
      return { input: command.input }
    case 'provider.update':
    case 'model.update':
      return { expected_version: command.expected_version, input: command.input }
    case 'provider.delete':
      return { expected_version: command.expected_version }
    case 'model.create':
      return { provider_id: command.provider_id, input: command.input }
    case 'model.delete':
      return { expected_version: command.expected_version, replacement: command.replacement }
  }
}
export function captureProjectConfigurationCommand<C extends ProjectConfigurationCommand>(
  command: C,
): C {
  return captureProjectModelInput(() => {
    const raw = { ...object(command) }
    const kind = member(raw.kind, [
      'provider.create',
      'provider.update',
      'provider.delete',
      'model.create',
      'model.update',
      'model.delete',
    ] as const)
    const v = shape(raw, [
      'kind',
      ...(kind.endsWith('.create') ? [] : ['id', 'expected_version']),
      ...(kind.endsWith('.delete') ? [] : ['input']),
      ...(kind === 'model.create' || kind === 'model.update' ? ['provider_id', 'protocol'] : []),
      ...(kind === 'model.delete' ? ['replacement'] : []),
    ])
    let result: ProjectConfigurationCommand
    switch (kind) {
      case 'provider.create':
        result = { kind, input: providerWriteInput(v.input) }
        break
      case 'provider.update':
        result = {
          kind,
          id: parseProjectModelID(v.id),
          expected_version: parseProjectModelVersion(v.expected_version),
          input: providerWriteInput(v.input),
        }
        break
      case 'provider.delete':
        result = {
          kind,
          id: parseProjectModelID(v.id),
          expected_version: parseProjectModelVersion(v.expected_version),
        }
        break
      case 'model.create': {
        const protocol = member(v.protocol, projectChatProtocols)
        result = {
          kind,
          provider_id: parseProjectModelID(v.provider_id),
          protocol,
          input: modelWriteInput(v.input, protocol),
        }
        break
      }
      case 'model.update': {
        const protocol = member(v.protocol, projectChatProtocols)
        result = {
          kind,
          id: parseProjectModelID(v.id),
          provider_id: parseProjectModelID(v.provider_id),
          protocol,
          expected_version: parseProjectModelVersion(v.expected_version),
          input: modelWriteInput(v.input, protocol),
        }
        break
      }
      case 'model.delete': {
        const id = parseProjectModelID(v.id),
          replacement = v.replacement === null ? null : parseProjectModelID(v.replacement)
        requireProjectModelValue(replacement !== id)
        result = {
          kind,
          id,
          expected_version: parseProjectModelVersion(v.expected_version),
          replacement,
        }
        break
      }
    }
    projectModelByteBudget(projectConfigurationBody(result), 1048576)
    return Object.freeze(result) as C
  })
}
export function parseProjectConfigurationReceipt<C extends ProjectConfigurationCommand>(
  value: unknown,
  command: C,
): ProjectConfigurationReceipt<C['kind']> {
  const v = shape(value, ['kind', 'resource_id', 'version', 'affected_references'])
  const result = Object.freeze({
    kind: member(v.kind, [command.kind]),
    resource_id: parseProjectModelID(v.resource_id),
    version: parseProjectModelVersion(v.version),
    affected_references: member(v.affected_references, ['0'] as const),
  })
  requireProjectModelValue(
    'expected_version' in command
      ? result.resource_id === command.id &&
          BigInt(result.version) === BigInt(command.expected_version) + 1n
      : result.version === '1',
  )
  return result
}
export function parseProjectConfigurationObservation(
  value: unknown,
  command: ProjectConfigurationCommand,
): ProjectConfigurationObservation {
  const v = shape(value, ['found', 'receipt'])
  requireProjectModelValue(typeof v.found === 'boolean')
  if (!v.found) {
    requireProjectModelValue(v.receipt === null)
    return Object.freeze({ found: false, receipt: null })
  }
  return Object.freeze({
    found: true,
    receipt: parseProjectConfigurationReceipt(v.receipt, command),
  })
}

export function createProjectModelsAPI(fetcher?: Fetch): ProjectModelsAPI {
  const request = accountTransport(fetcher)
  return {
    async listProviders(projectID, query, signal) {
      const project = captureProjectModelID(projectID),
        captured = captureProjectModelQuery(query)
      return request(
        'listProjectModelProviders',
        (value) =>
          parseProjectModelPage(
            value,
            captured,
            (v) => parseProjectProvider(v, project),
            (v) => v.created_at,
          ),
        { signal, projectID: project, projectModels: captured },
      )
    },
    async getProvider(projectID, providerID, signal) {
      const project = captureProjectModelID(projectID),
        target = captureProjectModelID(providerID)
      return request(
        'getProjectModelProvider',
        (value) => parseProjectProvider(value, project, target),
        { signal, projectID: project, target },
      )
    },
    async listModels(projectID, query, signal) {
      const project = captureProjectModelID(projectID),
        captured = captureProjectModelQuery(query)
      return request(
        'listProjectModels',
        (value) =>
          parseProjectModelPage(
            value,
            captured,
            (v) => parseProjectModel(v, project),
            (v) => v.created_at,
          ),
        { signal, projectID: project, projectModels: captured },
      )
    },
    async getModel(projectID, modelID, signal) {
      const project = captureProjectModelID(projectID),
        target = captureProjectModelID(modelID)
      return request('getProjectModel', (value) => parseProjectModel(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    async listAvailableChatModels(projectID, query, signal) {
      const project = captureProjectModelID(projectID),
        captured = captureProjectModelQuery(query)
      return request(
        'listProjectAvailableChatModels',
        (value) =>
          parseProjectModelPage(value, captured, (v) => parseProjectAvailableChatModel(v, project)),
        { signal, projectID: project, projectModels: captured },
      )
    },
    async createProvider(projectID, input, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectConfigurationCommand({ kind: 'provider.create', input }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'createProjectModelProvider',
        (value) => parseProjectConfigurationReceipt(value, command),
        { ...write, projectID: project, body: projectConfigurationBody(command) },
      )
    },
    async updateProvider(projectID, providerID, expectedVersion, input, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectConfigurationCommand({
          kind: 'provider.update',
          id: providerID,
          expected_version: expectedVersion,
          input,
        }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'updateProjectModelProvider',
        (value) => parseProjectConfigurationReceipt(value, command),
        {
          ...write,
          projectID: project,
          target: command.id,
          body: projectConfigurationBody(command),
        },
      )
    },
    async deleteProvider(projectID, providerID, expectedVersion, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectConfigurationCommand({
          kind: 'provider.delete',
          id: providerID,
          expected_version: expectedVersion,
        }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'deleteProjectModelProvider',
        (value) => parseProjectConfigurationReceipt(value, command),
        {
          ...write,
          projectID: project,
          target: command.id,
          body: projectConfigurationBody(command),
        },
      )
    },
    async createModel(projectID, context, input, options) {
      const project = captureProjectModelID(projectID),
        captured = captureProjectModelContext(context),
        command = captureProjectConfigurationCommand({ kind: 'model.create', ...captured, input }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'createProjectModel',
        (value) => parseProjectConfigurationReceipt(value, command),
        { ...write, projectID: project, body: projectConfigurationBody(command) },
      )
    },
    async updateModel(projectID, target, expectedVersion, input, options) {
      const project = captureProjectModelID(projectID),
        captured = captureProjectModelInput(() => {
          const v = shape(target, ['id', 'provider_id', 'protocol'])
          return {
            id: parseProjectModelID(v.id),
            provider_id: parseProjectModelID(v.provider_id),
            protocol: member(v.protocol, projectChatProtocols),
          }
        }),
        command = captureProjectConfigurationCommand({
          kind: 'model.update',
          ...captured,
          expected_version: expectedVersion,
          input,
        }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'updateProjectModel',
        (value) => parseProjectConfigurationReceipt(value, command),
        {
          ...write,
          projectID: project,
          target: command.id,
          body: projectConfigurationBody(command),
        },
      )
    },
    async deleteModel(projectID, modelID, expectedVersion, replacement, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectConfigurationCommand({
          kind: 'model.delete',
          id: modelID,
          expected_version: expectedVersion,
          replacement,
        }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'deleteProjectModel',
        (value) => parseProjectConfigurationReceipt(value, command),
        {
          ...write,
          projectID: project,
          target: command.id,
          body: projectConfigurationBody(command),
        },
      )
    },
    async lookupConfiguration(projectID, original, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectConfigurationCommand(original),
        write = captureProjectModelWriteOptions(options)
      return request(
        'lookupProjectModelConfiguration',
        (value) => parseProjectConfigurationObservation(value, command),
        { ...write, projectID: project, body: { command: command.kind } },
      )
    },
  }
}
export function createProjectModelSettingsAPI(fetcher?: Fetch): ProjectModelSettingsAPI {
  return Object.freeze({
    ...createProjectModelsAPI(fetcher),
    ...createProjectModelCredentialAPI(fetcher),
  })
}
