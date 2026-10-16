import type { WriteOptions } from './account'
import {
  AccountFailure,
  accountTransport,
  object,
  shape,
  string,
  uuid7,
  type Fetch,
} from './client'

const encoder = new TextEncoder()
function requireMCPValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function captureMCPInput<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function mcpWellFormed(value: string) {
  return new TextDecoder().decode(encoder.encode(value)) === value
}
function parseMCPID(value: unknown) {
  const result = string(value, 36, 36)
  requireMCPValue(uuid7.test(result))
  return result
}
function captureMCPID(value: unknown) {
  return captureMCPInput(() => parseMCPID(value))
}
function parseMCPVersion(value: unknown) {
  const result = string(value, 1, 19)
  requireMCPValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= 9223372036854775807n)
  return result
}
function mcpByteBudget(value: unknown, maximum: number) {
  requireMCPValue(encoder.encode(JSON.stringify(value)).byteLength <= maximum)
}
function captureMCPWriteOptions(value: WriteOptions) {
  return captureMCPInput(() => {
    const v = shape(value, ['csrfToken', 'key'], ['signal'])
    const csrf = string(v.csrfToken, 43, 43),
      key = string(v.key, 1, 128),
      signal = v.signal
    requireMCPValue(/^[A-Za-z0-9_-]{43}$/.test(csrf) && /^[A-Za-z0-9._:/-]{1,128}$/.test(key))
    requireMCPValue(signal === undefined || signal instanceof AbortSignal)
    return Object.freeze({ csrf, key, signal: signal ?? new AbortController().signal })
  })
}

export type MCPCredentialLookupTarget =
  | Readonly<{ kind: 'create' }>
  | Readonly<{
      kind: 'update' | 'delete'
      credential_id: string
      expected_version: string
    }>
export type MCPCredentialMetadata = Readonly<{
  credential_id: string
  purpose: 'mcp'
  version: string
}>
export type MCPCredentialCreated = MCPCredentialMetadata &
  Readonly<{ version: '1'; deleted: false }>
export type MCPCredentialUpdated = MCPCredentialMetadata & Readonly<{ deleted: false }>
export type MCPCredentialDeleted = MCPCredentialMetadata & Readonly<{ deleted: true }>
export type MCPCredentialMutation =
  MCPCredentialCreated | MCPCredentialUpdated | MCPCredentialDeleted
export type MCPCredentialObservation =
  | Readonly<{ observed: false; result: null }>
  | Readonly<{ observed: true; result: MCPCredentialMutation }>
export interface MCPCredentialsAPI {
  getCredentialMetadata(
    projectID: string,
    credentialID: string,
    signal: AbortSignal,
  ): Promise<MCPCredentialMetadata>
  createCredential(
    projectID: string,
    value: string,
    options: WriteOptions,
  ): Promise<MCPCredentialCreated>
  updateCredential(
    projectID: string,
    credentialID: string,
    expectedVersion: string,
    value: string,
    options: WriteOptions,
  ): Promise<MCPCredentialUpdated>
  deleteCredential(
    projectID: string,
    credentialID: string,
    expectedVersion: string,
    options: WriteOptions,
  ): Promise<MCPCredentialDeleted>
  lookupCredential(
    projectID: string,
    command: MCPCredentialLookupTarget,
    options: WriteOptions,
  ): Promise<MCPCredentialObservation>
}

export function captureMCPCredentialTarget<C extends MCPCredentialLookupTarget>(value: C): C {
  return captureMCPInput(() => {
    const raw = { ...object(value) },
      kind = raw.kind
    requireMCPValue(kind === 'create' || kind === 'update' || kind === 'delete')
    const v = shape(
      raw,
      kind === 'create' ? ['kind'] : ['kind', 'credential_id', 'expected_version'],
    )
    if (kind === 'create') return Object.freeze({ kind }) as C
    const expected_version = parseMCPVersion(v.expected_version)
    requireMCPValue(BigInt(expected_version) < 9223372036854775807n)
    return Object.freeze({
      kind,
      credential_id: parseMCPID(v.credential_id),
      expected_version,
    }) as C
  })
}
// Material is returned only to the private caller for its original Execute.
// Neither errors nor observations retain it or synthesize a material digest.
export function captureMCPCredentialValue(value: string): string {
  return captureMCPInput(() => {
    requireMCPValue(
      typeof value === 'string' &&
        value.length > 0 &&
        mcpWellFormed(value) &&
        new TextEncoder().encode(value).byteLength <= 65536,
    )
    mcpByteBudget({ value }, 409600)
    return value
  })
}
export function parseMCPCredentialMetadata(
  value: unknown,
  credentialID?: string,
): MCPCredentialMetadata {
  const v = shape(value, ['credential_id', 'purpose', 'version'])
  const credential_id = parseMCPID(v.credential_id)
  requireMCPValue(
    v.purpose === 'mcp' && (credentialID === undefined || credential_id === credentialID),
  )
  return Object.freeze({
    credential_id,
    purpose: 'mcp',
    version: parseMCPVersion(v.version),
  })
}
export function parseMCPCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'create' }>,
): MCPCredentialCreated
export function parseMCPCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'update'; credential_id: string; expected_version: string }>,
): MCPCredentialUpdated
export function parseMCPCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'delete'; credential_id: string; expected_version: string }>,
): MCPCredentialDeleted
export function parseMCPCredentialMutation(
  value: unknown,
  command: MCPCredentialLookupTarget,
): MCPCredentialMutation
export function parseMCPCredentialMutation(
  value: unknown,
  command: MCPCredentialLookupTarget,
): MCPCredentialMutation {
  const v = shape(value, ['credential_id', 'purpose', 'version', 'deleted'])
  const metadata = parseMCPCredentialMetadata(
    { credential_id: v.credential_id, purpose: v.purpose, version: v.version },
    command.kind === 'create' ? undefined : command.credential_id,
  )
  requireMCPValue(v.deleted === (command.kind === 'delete'))
  if (command.kind === 'create') {
    requireMCPValue(metadata.version === '1')
    return Object.freeze({ ...metadata, version: '1', deleted: false })
  }
  requireMCPValue(BigInt(metadata.version) === BigInt(command.expected_version) + 1n)
  return Object.freeze({ ...metadata, deleted: command.kind === 'delete' })
}
export function parseMCPCredentialObservation(
  value: unknown,
  command: MCPCredentialLookupTarget,
): MCPCredentialObservation {
  const v = shape(value, ['observed', 'result'])
  requireMCPValue(typeof v.observed === 'boolean')
  if (!v.observed) {
    requireMCPValue(v.result === null)
    return Object.freeze({ observed: false, result: null })
  }
  return Object.freeze({
    observed: true,
    result: parseMCPCredentialMutation(v.result, command),
  })
}
export function createMCPCredentialAPI(fetcher?: Fetch): MCPCredentialsAPI {
  const request = accountTransport(fetcher)
  return {
    async getCredentialMetadata(projectID, credentialID, signal) {
      const project = captureMCPID(projectID),
        target = captureMCPID(credentialID)
      return request(
        'getMCPCredentialMetadata',
        (value) => parseMCPCredentialMetadata(value, target),
        { signal, projectID: project, target },
      )
    },
    async createCredential(projectID, value, options) {
      const project = captureMCPID(projectID),
        material = captureMCPCredentialValue(value),
        write = captureMCPWriteOptions(options)
      return request(
        'createMCPCredential',
        (value) => parseMCPCredentialMutation(value, { kind: 'create' }),
        { ...write, projectID: project, body: { value: material } },
      )
    },
    async updateCredential(projectID, credentialID, expectedVersion, value, options) {
      const project = captureMCPID(projectID),
        command = captureMCPCredentialTarget({
          kind: 'update',
          credential_id: credentialID,
          expected_version: expectedVersion,
        }),
        material = captureMCPCredentialValue(value),
        write = captureMCPWriteOptions(options)
      const body = { expected_version: command.expected_version, value: material }
      captureMCPInput(() => mcpByteBudget(body, 409600))
      return request('updateMCPCredential', (value) => parseMCPCredentialMutation(value, command), {
        ...write,
        projectID: project,
        target: command.credential_id,
        body,
      })
    },
    async deleteCredential(projectID, credentialID, expectedVersion, options) {
      const project = captureMCPID(projectID),
        command = captureMCPCredentialTarget({
          kind: 'delete',
          credential_id: credentialID,
          expected_version: expectedVersion,
        }),
        write = captureMCPWriteOptions(options)
      return request('deleteMCPCredential', (value) => parseMCPCredentialMutation(value, command), {
        ...write,
        projectID: project,
        target: command.credential_id,
        body: { expected_version: command.expected_version },
      })
    },
    async lookupCredential(projectID, original, options) {
      const project = captureMCPID(projectID),
        command = captureMCPCredentialTarget(original),
        write = captureMCPWriteOptions(options)
      return request(
        'lookupMCPCredential',
        (value) => parseMCPCredentialObservation(value, command),
        { ...write, projectID: project, body: command },
      )
    },
  }
}
