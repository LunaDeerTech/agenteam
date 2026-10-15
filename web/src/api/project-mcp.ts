import {
  AccountFailure,
  accountTransport,
  object,
  shape,
  string,
  uuid7,
  type Fetch,
} from './client'
import type { WriteOptions } from './account'

export type MCPAuthentication = Readonly<
  | { scheme: 'none' | 'bearer' | 'basic' | 'oauth' }
  | { scheme: 'custom_header'; header_name: string }
>
export type MCPConfigInput = Readonly<{
  name: string
  description: string
  endpoint: string
  authentication: MCPAuthentication
}>
export type MCPConfigWrite = Readonly<{ expected_version: string; input: MCPConfigInput }>
export type MCPConfig = MCPConfigInput &
  Readonly<{
    id: string
    scope: 'system' | 'project'
    project_id?: string
    version: string
    runtime_revision: string
  }>
export type MCPConnectionState =
  'unconnected' | 'connecting' | 'connected' | 'auth_required' | 'error' | 'needs_reconnect'
export type MCPDiscoveryState = 'never' | 'ready' | 'error'
export type MCPConnection = Readonly<{
  id: string
  project_id: string
  config_id: string
  version: string
  config_revision: string
  state: MCPConnectionState
  enabled: boolean
  credential_id?: string
  protocol_revision?: '2026-07-28' | '2025-11-25'
  tools_state: MCPDiscoveryState
  resources_state: MCPDiscoveryState
  tools_revision: string
  resources_revision: string
  last_error_code?: string
}>
export type MCPConnectInput = Readonly<{
  config_id: string
  credential_id?: string
  expected_version: string
}>
export type MCPResource = Readonly<{
  id: string
  connection_id: string
  kind: 'resource' | 'template'
  uri: string
  name: string
  title: string
  description: string
  mime_type: string
  byte_size?: string
  revision: string
}>
export type MCPTool = Readonly<{
  tool_id: string
  spec_revision: string
  definition: Readonly<{
    stable_key: string
    name: string
    description: string
    input_schema: Readonly<Record<string, unknown>>
    output_schema?: Readonly<Record<string, unknown>>
    annotations?: Readonly<{
      read_only: boolean
      destructive: boolean
      idempotency: 'none' | 'unknown' | 'inherent' | 'keyed'
    }>
  }>
  config_id: string
  connection_id: string
  remote_name: string
  default_enabled: boolean
  active: boolean
}>
export type MCPConnectionAction = 'refresh' | 'enable' | 'disable' | 'disconnect'
export type MCPCommand =
  | 'config.create'
  | 'config.update'
  | 'config.delete'
  | 'connection.connect'
  | `connection.${MCPConnectionAction}`
export type MCPCommandResult = Readonly<
  { config: MCPConfig } | { connection: MCPConnection } | { deleted: true }
>
export type MCPLookup = Readonly<
  { state: 'pending' | 'not_found' | 'stopped' } | { state: 'complete'; result: MCPCommandResult }
>
export interface ProjectMCPAPI {
  configs(project: string, signal: AbortSignal): Promise<readonly MCPConfig[]>
  connections(project: string, signal: AbortSignal): Promise<readonly MCPConnection[]>
  tools(project: string, connection: string, signal: AbortSignal): Promise<readonly MCPTool[]>
  resources(
    project: string,
    connection: string,
    signal: AbortSignal,
  ): Promise<readonly MCPResource[]>
  createConfig(
    project: string,
    input: MCPConfigWrite,
    write: WriteOptions,
  ): Promise<MCPCommandResult>
  updateConfig(
    project: string,
    config: string,
    input: MCPConfigWrite,
    write: WriteOptions,
  ): Promise<MCPCommandResult>
  deleteConfig(
    project: string,
    config: string,
    version: string,
    write: WriteOptions,
  ): Promise<MCPCommandResult>
  connect(project: string, input: MCPConnectInput, write: WriteOptions): Promise<MCPCommandResult>
  changeConnection(
    project: string,
    connection: string,
    action: MCPConnectionAction,
    version: string,
    write: WriteOptions,
  ): Promise<MCPCommandResult>
  lookup(project: string, command: MCPCommand, write: WriteOptions): Promise<MCPLookup>
}

function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function text(value: unknown, max: number, min = 0): string {
  const result = string(value, min, max)
  requireValue(!result.includes('\0') && new TextEncoder().encode(result).byteLength <= max)
  return result
}
function decimal(value: unknown, zero = false): string {
  const result = string(value, 1, 19)
  requireValue(
    /^(0|[1-9][0-9]{0,18})$/.test(result) &&
      (zero || result !== '0') &&
      BigInt(result) <= 9223372036854775807n,
  )
  return result
}
function boolean(value: unknown): boolean {
  requireValue(typeof value === 'boolean')
  return value
}
function oneOf<const T extends readonly string[]>(value: unknown, allowed: T): T[number] {
  requireValue(typeof value === 'string' && allowed.includes(value))
  return value
}
function authentication(value: unknown): MCPAuthentication {
  const a = shape(value, ['scheme'], ['header_name'])
  if (a.scheme === 'custom_header') {
    const name = text(a.header_name, 128, 1)
    requireValue(/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name))
    requireValue(
      ![
        'authorization',
        'cookie',
        'host',
        'connection',
        'proxy-authorization',
        'proxy-connection',
        'keep-alive',
        'upgrade',
        'transfer-encoding',
        'content-length',
        'content-type',
        'accept',
        'accept-encoding',
        'user-agent',
        'trailer',
        'te',
        'expect',
        'mcp-session-id',
        'mcp-protocol-version',
        'last-event-id',
        'origin',
      ].includes(name.toLowerCase()),
    )
    return Object.freeze({ scheme: 'custom_header', header_name: name })
  }
  shape(value, ['scheme'])
  return Object.freeze({ scheme: oneOf(a.scheme, ['none', 'bearer', 'basic', 'oauth']) })
}
function configInput(value: unknown): MCPConfigInput {
  const v = shape(value, ['name', 'description', 'endpoint', 'authentication'])
  const endpoint = text(v.endpoint, 8192, 1)
  // This validates the browser representation; only the server's controlled
  // outbound policy grants network access. URI metadata is never fetched here.
  const target = new URL(endpoint)
  requireValue(
    target.protocol === 'https:' &&
      !!target.hostname &&
      !target.username &&
      !target.password &&
      !target.hash &&
      !/[\u0000-\u0020\u007f]/.test(endpoint),
  )
  return Object.freeze({
    name: text(v.name, 256, 1),
    description: text(v.description, 8192),
    endpoint,
    authentication: authentication(v.authentication),
  })
}
function capture<T>(read: () => T): T {
  try {
    return read()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureMCPConfigWrite(value: unknown, create: boolean): MCPConfigWrite {
  return capture(() => {
    const v = shape(value, ['expected_version', 'input'])
    const version = decimal(v.expected_version, create)
    requireValue(!create || version === '0')
    return Object.freeze({ expected_version: version, input: configInput(v.input) })
  })
}
export function captureMCPConnect(value: unknown): MCPConnectInput {
  return capture(() => {
    const v = shape(value, ['config_id', 'expected_version'], ['credential_id'])
    return Object.freeze({
      config_id: id(v.config_id),
      expected_version: decimal(v.expected_version, true),
      ...(Object.hasOwn(v, 'credential_id') ? { credential_id: id(v.credential_id) } : {}),
    })
  })
}
export function parseMCPConfig(value: unknown, project: string): MCPConfig {
  const v = shape(
    value,
    [
      'id',
      'scope',
      'name',
      'description',
      'endpoint',
      'authentication',
      'version',
      'runtime_revision',
    ],
    ['project_id'],
  )
  const scope = oneOf(v.scope, ['system', 'project'])
  requireValue(scope === 'project' ? id(v.project_id) === project : !Object.hasOwn(v, 'project_id'))
  return Object.freeze({
    ...configInput({
      name: v.name,
      description: v.description,
      endpoint: v.endpoint,
      authentication: v.authentication,
    }),
    id: id(v.id),
    scope,
    ...(scope === 'project' ? { project_id: project } : {}),
    version: decimal(v.version),
    runtime_revision: decimal(v.runtime_revision),
  })
}
export function parseMCPConnection(value: unknown, project: string): MCPConnection {
  const v = shape(
    value,
    [
      'id',
      'project_id',
      'config_id',
      'version',
      'config_revision',
      'state',
      'enabled',
      'tools_state',
      'resources_state',
      'tools_revision',
      'resources_revision',
    ],
    ['credential_id', 'protocol_revision', 'last_error_code'],
  )
  requireValue(id(v.project_id) === project)
  const error = Object.hasOwn(v, 'last_error_code') ? text(v.last_error_code, 80, 1) : undefined
  requireValue(error === undefined || /^mcp_[a-z0-9_]+$/.test(error))
  return Object.freeze({
    id: id(v.id),
    project_id: project,
    config_id: id(v.config_id),
    version: decimal(v.version),
    config_revision: decimal(v.config_revision),
    state: oneOf(v.state, [
      'unconnected',
      'connecting',
      'connected',
      'auth_required',
      'error',
      'needs_reconnect',
    ]),
    enabled: boolean(v.enabled),
    tools_state: oneOf(v.tools_state, ['never', 'ready', 'error']),
    resources_state: oneOf(v.resources_state, ['never', 'ready', 'error']),
    tools_revision: decimal(v.tools_revision, true),
    resources_revision: decimal(v.resources_revision, true),
    ...(Object.hasOwn(v, 'credential_id') ? { credential_id: id(v.credential_id) } : {}),
    ...(Object.hasOwn(v, 'protocol_revision')
      ? { protocol_revision: oneOf(v.protocol_revision, ['2026-07-28', '2025-11-25']) }
      : {}),
    ...(error ? { last_error_code: error } : {}),
  })
}
export function parseMCPResource(value: unknown, connection: string): MCPResource {
  const v = shape(
    value,
    ['id', 'connection_id', 'kind', 'uri', 'name', 'title', 'description', 'mime_type', 'revision'],
    ['byte_size'],
  )
  requireValue(id(v.connection_id) === connection)
  return Object.freeze({
    id: id(v.id),
    connection_id: connection,
    kind: oneOf(v.kind, ['resource', 'template']),
    uri: text(v.uri, 8192, 1),
    name: text(v.name, 1024),
    title: text(v.title, 1024),
    description: text(v.description, 8192),
    mime_type: text(v.mime_type, 256),
    revision: decimal(v.revision),
    ...(Object.hasOwn(v, 'byte_size') ? { byte_size: decimal(v.byte_size, true) } : {}),
  })
}
function schema(value: unknown): Readonly<Record<string, unknown>> {
  const root = object(value)
  function copy(v: unknown, depth: number): unknown {
    requireValue(depth <= 128)
    if (v === null || typeof v !== 'object') {
      requireValue(
        v === null ||
          typeof v === 'string' ||
          typeof v === 'boolean' ||
          (typeof v === 'number' && Number.isFinite(v)),
      )
      return v
    }
    if (Array.isArray(v)) return Object.freeze(v.map((item) => copy(item, depth + 1)))
    return Object.freeze(
      Object.fromEntries(Object.entries(v).map(([key, item]) => [key, copy(item, depth + 1)])),
    )
  }
  return copy(root, 0) as Readonly<Record<string, unknown>>
}
export function parseMCPTool(value: unknown, connection: string): MCPTool {
  const v = shape(value, [
    'tool_id',
    'spec_revision',
    'definition',
    'config_id',
    'connection_id',
    'remote_name',
    'default_enabled',
    'active',
  ])
  requireValue(id(v.connection_id) === connection)
  const d = shape(
    v.definition,
    ['stable_key', 'name', 'description', 'input_schema'],
    ['output_schema', 'annotations'],
  )
  const key = text(d.stable_key, 512, 1)
  requireValue(key.startsWith('mcp:'))
  let annotations: MCPTool['definition']['annotations']
  if (Object.hasOwn(d, 'annotations')) {
    const a = shape(d.annotations, ['read_only', 'destructive', 'idempotency'])
    annotations = Object.freeze({
      read_only: boolean(a.read_only),
      destructive: boolean(a.destructive),
      idempotency: oneOf(a.idempotency, ['none', 'unknown', 'inherent', 'keyed']),
    })
  }
  return Object.freeze({
    tool_id: id(v.tool_id),
    spec_revision: decimal(v.spec_revision),
    config_id: id(v.config_id),
    connection_id: connection,
    remote_name: text(v.remote_name, 128, 1),
    default_enabled: boolean(v.default_enabled),
    active: boolean(v.active),
    definition: Object.freeze({
      stable_key: key,
      name: text(d.name, 128, 1),
      description: text(d.description, 8192),
      input_schema: schema(d.input_schema),
      ...(Object.hasOwn(d, 'output_schema') ? { output_schema: schema(d.output_schema) } : {}),
      ...(annotations ? { annotations } : {}),
    }),
  })
}
const commands = [
  'config.create',
  'config.update',
  'config.delete',
  'connection.connect',
  'connection.refresh',
  'connection.enable',
  'connection.disable',
  'connection.disconnect',
] as const
export function parseMCPCommandResult(
  value: unknown,
  project: string,
  command: MCPCommand,
): MCPCommandResult {
  if (command === 'config.delete') {
    const v = shape(value, ['deleted'])
    requireValue(v.deleted === true)
    return Object.freeze({ deleted: true as const })
  }
  if (command.startsWith('config.')) {
    const v = shape(value, ['config']),
      config = parseMCPConfig(v.config, project)
    requireValue(config.scope === 'project')
    return Object.freeze({ config })
  }
  const v = shape(value, ['connection'])
  return Object.freeze({ connection: parseMCPConnection(v.connection, project) })
}
export function parseMCPLookup(value: unknown, project: string, command: MCPCommand): MCPLookup {
  const v = shape(value, ['state'], ['result'])
  if (v.state === 'complete')
    return Object.freeze({
      state: 'complete',
      result: parseMCPCommandResult(v.result, project, command),
    })
  shape(value, ['state'])
  if (v.state === 'stopped') {
    requireValue(command === 'connection.refresh')
    return Object.freeze({ state: 'stopped' })
  }
  return Object.freeze({ state: oneOf(v.state, ['pending', 'not_found']) })
}
function collection<T>(
  value: unknown,
  maximum: number,
  parse: (item: unknown) => T,
  identity: (item: T) => string,
): readonly T[] {
  const v = shape(value, ['items'])
  requireValue(Array.isArray(v.items) && v.items.length <= maximum)
  const result = v.items.map(parse)
  requireValue(new Set(result.map(identity)).size === result.length)
  return Object.freeze(result)
}
export function createProjectMCPAPI(fetcher?: Fetch): ProjectMCPAPI {
  const request = accountTransport(fetcher)
  const read = (project: string, signal: AbortSignal) => ({
    projectID: capture(() => id(project)),
    signal,
  })
  const writeOptions = (project: string, body: unknown, write: WriteOptions) => ({
    ...read(project, write.signal ?? new AbortController().signal),
    body,
    csrf: write.csrfToken,
    key: write.key,
  })
  return {
    configs(project, signal) {
      return request(
        'listProjectMCPConfigs',
        (v) =>
          collection(
            v,
            512,
            (item) => parseMCPConfig(item, project),
            (item) => item.id,
          ),
        read(project, signal),
      )
    },
    connections(project, signal) {
      return request(
        'listProjectMCPConnections',
        (v) =>
          collection(
            v,
            512,
            (item) => parseMCPConnection(item, project),
            (item) => item.id,
          ),
        read(project, signal),
      )
    },
    tools(project, connection, signal) {
      const target = capture(() => id(connection))
      return request(
        'listProjectMCPTools',
        (v) =>
          collection(
            v,
            10000,
            (item) => parseMCPTool(item, target),
            (item) => item.tool_id,
          ),
        { ...read(project, signal), target },
      )
    },
    resources(project, connection, signal) {
      const target = capture(() => id(connection))
      return request(
        'listProjectMCPResources',
        (v) =>
          collection(
            v,
            1024,
            (item) => parseMCPResource(item, target),
            (item) => item.id,
          ),
        { ...read(project, signal), target },
      )
    },
    createConfig(project, input, write) {
      return request(
        'createProjectMCPConfig',
        (v) => parseMCPCommandResult(v, project, 'config.create'),
        writeOptions(project, captureMCPConfigWrite(input, true), write),
      )
    },
    updateConfig(project, config, input, write) {
      return request(
        'updateProjectMCPConfig',
        (v) => {
          const result = parseMCPCommandResult(v, project, 'config.update')
          requireValue('config' in result && result.config.id === config)
          return result
        },
        {
          ...writeOptions(project, captureMCPConfigWrite(input, false), write),
          target: capture(() => id(config)),
        },
      )
    },
    deleteConfig(project, config, version, write) {
      return request(
        'deleteProjectMCPConfig',
        (v) => parseMCPCommandResult(v, project, 'config.delete'),
        {
          ...writeOptions(project, { expected_version: capture(() => decimal(version)) }, write),
          target: capture(() => id(config)),
        },
      )
    },
    connect(project, input, write) {
      const captured = captureMCPConnect(input)
      return request(
        'connectProjectMCP',
        (v) => {
          const result = parseMCPCommandResult(v, project, 'connection.connect')
          requireValue('connection' in result && result.connection.config_id === captured.config_id)
          return result
        },
        writeOptions(project, captured, write),
      )
    },
    changeConnection(project, connection, action, version, write) {
      const selected = capture(() => oneOf(action, ['refresh', 'enable', 'disable', 'disconnect']))
      const endpoints = {
        refresh: 'refreshProjectMCP',
        enable: 'enableProjectMCP',
        disable: 'disableProjectMCP',
        disconnect: 'disconnectProjectMCP',
      } as const
      return request(
        endpoints[selected],
        (v) => {
          const result = parseMCPCommandResult(v, project, `connection.${selected}`)
          requireValue('connection' in result && result.connection.id === connection)
          return result
        },
        {
          ...writeOptions(project, { expected_version: capture(() => decimal(version)) }, write),
          target: capture(() => id(connection)),
        },
      )
    },
    lookup(project, command, write) {
      const captured = capture(() => oneOf(command, commands))
      return request(
        'lookupProjectMCP',
        (v) => parseMCPLookup(v, project, captured),
        writeOptions(project, { command: captured }, write),
      )
    },
  }
}
