import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type SecretMetadata = Readonly<{
  id: string
  project_id: string
  type: 'secret'
  name: string
  description: string
  version: string
  created_at: string
  updated_at: string
}>
export type SecretQuery = Readonly<{ limit: number; cursor?: string }>
export type SecretPage = Readonly<{ items: readonly SecretMetadata[]; next_cursor?: string }>
export type SecretIdentity =
  | Readonly<{ kind: 'create'; projectID: string; targetID: string }>
  | Readonly<{
      kind: 'update' | 'delete'
      projectID: string
      targetID: string
      expectedVersion: string
    }>
export type SecretCommand =
  | Readonly<{
      kind: 'create'
      projectID: string
      request: Readonly<{ variable_id: string; name: string; description: string; value: string }>
    }>
  | Readonly<{
      kind: 'update'
      projectID: string
      targetID: string
      expectedVersion: string
      request: Readonly<{ name?: string; description?: string; value?: string }>
    }>
  | Readonly<{ kind: 'delete'; projectID: string; targetID: string; expectedVersion: string }>
export type SecretReceipt =
  | Readonly<{
      command: 'project.secret_variable.create' | 'project.secret_variable.update'
      changed: boolean
      variable: SecretMetadata
      event_id: string | null
      audit_id: string | null
    }>
  | Readonly<{
      command: 'project.secret_variable.delete'
      changed: true
      deleted: Readonly<{
        id: string
        project_id: string
        type: 'secret'
        version: string
        deleted_at: string
      }>
      event_id: string
      audit_id: string
    }>
export type SecretLookup =
  | Readonly<{ status: 'committed'; receipt: SecretReceipt }>
  | Readonly<{ status: 'not_observed'; receipt: null }>
export type SecretWriteOptions = Readonly<{ csrf: string; key: string; signal: AbortSignal }>
export interface ProjectSecretsAPI {
  list(projectID: string, query: SecretQuery, signal: AbortSignal): Promise<SecretPage>
  get(projectID: string, targetID: string, signal: AbortSignal): Promise<SecretMetadata>
  execute(command: SecretCommand, options: SecretWriteOptions): Promise<SecretReceipt>
  lookup(identity: SecretIdentity, options: SecretWriteOptions): Promise<SecretLookup>
}
const maximumVersion = 9223372036854775807n
const encoder = new TextEncoder()
const metadataKeys = [
  'id',
  'project_id',
  'type',
  'name',
  'description',
  'version',
  'created_at',
  'updated_at',
]
function requireValue(ok: boolean): asserts ok {
  if (!ok) throw new AccountFailure('invalid-response')
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function id(value: unknown): string {
  const v = string(value, 36, 36)
  requireValue(uuid7.test(v))
  return v
}
function version(value: unknown): string {
  const v = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(v) && BigInt(v) <= maximumVersion)
  return v
}
function text(value: unknown, maximum: number): string {
  const v = string(value, 0, maximum)
  requireValue(!/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v))
  requireValue(!v.includes('\0') && encoder.encode(v).byteLength <= maximum)
  return v
}
function name(value: unknown): string {
  const v = string(value, 1, 128)
  requireValue(/^[A-Za-z_][A-Za-z0-9_]{0,127}$/.test(v) && !/^AGENTEAM(?:_|$)/i.test(v))
  return v
}
function description(value: unknown): string {
  const v = text(value, 4096)
  requireValue(!/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/u.test(v))
  return v
}
function material(value: unknown): string {
  const v = text(value, 65536)
  requireValue(v.length > 0)
  return v
}
function cursor(value: unknown): string {
  const v = text(value, 8192)
  requireValue(v.length > 0)
  return v
}
function instant(value: unknown): string {
  const v = parseSystemInstant(value)
  requireValue(v !== '0001-01-01T00:00:00.000000Z')
  return v
}
export const captureSecretID = (value: unknown): string => input(() => id(value))
export function newSecretID(): string {
  const now = Date.now()
  if (!Number.isSafeInteger(now) || now < 0 || now >= 2 ** 48)
    throw new AccountFailure('invalid-input')
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  let time = BigInt(now)
  for (let i = 5; i >= 0; i--) {
    bytes[i] = Number(time & 255n)
    time >>= 8n
  }
  bytes[6] = (bytes[6]! & 15) | 112
  bytes[8] = (bytes[8]! & 63) | 128
  const hex = Array.from(bytes, (v) => v.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
export function captureSecretQuery(value: unknown): SecretQuery {
  return input(() => {
    const v = shape(value, ['limit'], ['cursor'])
    requireValue(
      typeof v.limit === 'number' && Number.isInteger(v.limit) && v.limit >= 1 && v.limit <= 100,
    )
    return Object.freeze({
      limit: v.limit,
      ...(Object.hasOwn(v, 'cursor') ? { cursor: cursor(v.cursor) } : {}),
    })
  })
}
export function captureSecretIdentity(value: unknown): SecretIdentity {
  return input(() => {
    const v = shape(value, ['kind', 'projectID', 'targetID'], ['expectedVersion'])
    requireValue(v.kind === 'create' || v.kind === 'update' || v.kind === 'delete')
    const projectID = id(v.projectID),
      targetID = id(v.targetID)
    if (v.kind === 'create') {
      shape(v, ['kind', 'projectID', 'targetID'])
      return Object.freeze({ kind: v.kind, projectID, targetID })
    }
    const expectedVersion = version(v.expectedVersion)
    requireValue(BigInt(expectedVersion) < maximumVersion)
    return Object.freeze({ kind: v.kind, projectID, targetID, expectedVersion })
  })
}
// Never use this material-bearing object as observable state, a retry intent, or a log value.
export function captureSecretCommand(value: unknown): SecretCommand {
  return input(() => {
    const v = shape(value, ['kind', 'projectID'], ['targetID', 'expectedVersion', 'request'])
    if (v.kind === 'create') {
      shape(v, ['kind', 'projectID', 'request'])
      const r = shape(v.request, ['variable_id', 'name', 'description', 'value'])
      return Object.freeze({
        kind: 'create',
        projectID: id(v.projectID),
        request: Object.freeze({
          variable_id: id(r.variable_id),
          name: name(r.name),
          description: description(r.description),
          value: material(r.value),
        }),
      })
    }
    const identity = captureSecretIdentity({
      kind: v.kind,
      projectID: v.projectID,
      targetID: v.targetID,
      expectedVersion: v.expectedVersion,
    })
    requireValue(identity.kind !== 'create')
    if (identity.kind === 'delete') {
      shape(v, ['kind', 'projectID', 'targetID', 'expectedVersion'])
      return Object.freeze({ ...identity, kind: 'delete' as const })
    }
    const r = shape(v.request, [], ['name', 'description', 'value'])
    requireValue(Object.keys(r).length > 0)
    return Object.freeze({
      ...identity,
      kind: 'update',
      request: Object.freeze({
        ...(Object.hasOwn(r, 'name') ? { name: name(r.name) } : {}),
        ...(Object.hasOwn(r, 'description') ? { description: description(r.description) } : {}),
        ...(Object.hasOwn(r, 'value') ? { value: material(r.value) } : {}),
      }),
    })
  })
}
export function secretIdentity(command: SecretCommand): SecretIdentity {
  return command.kind === 'create'
    ? Object.freeze({
        kind: 'create',
        projectID: command.projectID,
        targetID: command.request.variable_id,
      })
    : captureSecretIdentity({
        kind: command.kind,
        projectID: command.projectID,
        targetID: command.targetID,
        expectedVersion: command.expectedVersion,
      })
}
export function parseSecretMetadata(
  value: unknown,
  projectID: string,
  targetID?: string,
): SecretMetadata {
  const v = shape(value, metadataKeys)
  const target = id(v.id),
    project = id(v.project_id),
    revision = version(v.version),
    created = instant(v.created_at),
    updated = instant(v.updated_at)
  requireValue(
    project === projectID &&
      (targetID === undefined || target === targetID) &&
      v.type === 'secret' &&
      created <= updated &&
      (revision !== '1' || created === updated),
  )
  return Object.freeze({
    id: target,
    project_id: project,
    type: 'secret',
    name: name(v.name),
    description: description(v.description),
    version: revision,
    created_at: created,
    updated_at: updated,
  })
}
export function parseSecretPage(value: unknown, projectID: string, query: SecretQuery): SecretPage {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= query.limit)
  const ids = new Set<string>(),
    names = new Set<string>()
  let previous: SecretMetadata | undefined
  const items = v.items.map((raw: unknown) => {
    const row = parseSecretMetadata(raw, projectID)
    requireValue(
      !ids.has(row.id) &&
        !names.has(row.name) &&
        (!previous ||
          previous.name < row.name ||
          (previous.name === row.name && previous.id < row.id)),
    )
    ids.add(row.id)
    names.add(row.name)
    previous = row
    return row
  })
  const next = Object.hasOwn(v, 'next_cursor') ? cursor(v.next_cursor) : undefined
  requireValue(next === undefined || (items.length === query.limit && next !== query.cursor))
  return Object.freeze({
    items: Object.freeze(items),
    ...(next === undefined ? {} : { next_cursor: next }),
  })
}
export function parseSecretReceipt(value: unknown, identity: SecretIdentity): SecretReceipt {
  const v = shape(value, [
    'command',
    'changed',
    'event_id',
    'audit_id',
    identity.kind === 'delete' ? 'deleted' : 'variable',
  ])
  requireValue(
    v.command === `project.secret_variable.${identity.kind}` && typeof v.changed === 'boolean',
  )
  const event = v.event_id === null ? null : id(v.event_id),
    audit = v.audit_id === null ? null : id(v.audit_id)
  requireValue(v.changed === (event !== null) && v.changed === (audit !== null))
  if (identity.kind === 'delete') {
    const d = shape(v.deleted, ['id', 'project_id', 'type', 'version', 'deleted_at'])
    requireValue(
      v.changed &&
        event !== null &&
        audit !== null &&
        id(d.id) === identity.targetID &&
        id(d.project_id) === identity.projectID &&
        d.type === 'secret',
    )
    const revision = version(d.version)
    requireValue(BigInt(revision) === BigInt(identity.expectedVersion) + 1n)
    return Object.freeze({
      command: 'project.secret_variable.delete',
      changed: true,
      event_id: event,
      audit_id: audit,
      deleted: Object.freeze({
        id: identity.targetID,
        project_id: identity.projectID,
        type: 'secret',
        version: revision,
        deleted_at: instant(d.deleted_at),
      }),
    })
  }
  const variable = parseSecretMetadata(v.variable, identity.projectID, identity.targetID)
  if (identity.kind === 'create') requireValue(v.changed && variable.version === '1')
  else
    requireValue(
      BigInt(variable.version) === BigInt(identity.expectedVersion) + (v.changed ? 1n : 0n),
    )
  return Object.freeze({
    command:
      identity.kind === 'create'
        ? 'project.secret_variable.create'
        : 'project.secret_variable.update',
    changed: v.changed,
    event_id: event,
    audit_id: audit,
    variable,
  })
}
export function createProjectSecretsAPI(fetcher?: Fetch): ProjectSecretsAPI {
  const request = accountTransport(fetcher)
  return {
    list(projectID, query, signal) {
      const project = captureSecretID(projectID),
        q = captureSecretQuery(query)
      return request('listProjectSecrets', (v) => parseSecretPage(v, project, q), {
        projectID: project,
        secrets: q,
        signal,
      })
    },
    get(projectID, targetID, signal) {
      const project = captureSecretID(projectID),
        target = captureSecretID(targetID)
      return request('getProjectSecret', (v) => parseSecretMetadata(v, project, target), {
        projectID: project,
        target,
        signal,
      })
    },
    async execute(command, options) {
      let captured: SecretCommand | null = captureSecretCommand(command)
      const identity = secretIdentity(captured)
      // Safe expected metadata is enough to validate a receipt; never capture value in the decoder.
      const fields =
        captured.kind === 'delete'
          ? {}
          : { name: captured.request.name, description: captured.request.description }
      const replaces = captured.kind !== 'delete' && Object.hasOwn(captured.request, 'value')
      const body =
        captured.kind === 'create'
          ? { request: captured.request }
          : captured.kind === 'update'
            ? { expected_version: captured.expectedVersion, request: captured.request }
            : { expected_version: captured.expectedVersion }
      const parse = (v: unknown) => {
        const receipt = parseSecretReceipt(v, identity)
        if ('variable' in receipt) {
          for (const key of ['name', 'description'] as const)
            if (fields[key] !== undefined) requireValue(receipt.variable[key] === fields[key])
          requireValue(!replaces || receipt.changed)
        }
        return receipt
      }
      captured = null
      return identity.kind === 'create'
        ? request('createProjectSecret', parse, { ...options, projectID: identity.projectID, body })
        : request(
            identity.kind === 'update' ? 'updateProjectSecret' : 'deleteProjectSecret',
            parse,
            { ...options, projectID: identity.projectID, target: identity.targetID, body },
          )
    },
    lookup(original, options) {
      const identity = captureSecretIdentity(original)
      const body = {
        command: `project.secret_variable.${identity.kind}`,
        target_id: identity.targetID,
        ...(identity.kind === 'create' ? {} : { expected_version: identity.expectedVersion }),
      }
      return request(
        'lookupProjectSecret',
        (raw) => {
          const v = shape(raw, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: parseSecretReceipt(v.receipt, identity),
            })
          requireValue(v.status === 'not_observed' && v.receipt === null)
          return Object.freeze({ status: 'not_observed', receipt: null })
        },
        { ...options, projectID: identity.projectID, body },
      )
    },
  }
}
