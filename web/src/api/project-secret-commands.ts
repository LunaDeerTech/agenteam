import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseProjectSecretMetadata, type ProjectSecretMetadata } from './project-secrets'

// Transient transport inputs: never Session progress or retry storage.
export type ProjectSecretCreate = Readonly<{
  variable_id: string
  name: string
  description: string
  value: string
}>
export type ProjectSecretUpdate = Readonly<{ name?: string; description?: string; value?: string }>
export type ProjectSecretIdentity =
  | Readonly<{ kind: 'create'; projectID: string; targetID: string }>
  | Readonly<{ kind: 'update'; projectID: string; targetID: string; expectedVersion: string }>
export type ProjectSecretReceipt = Readonly<{
  command: 'project.secret_variable.create' | 'project.secret_variable.update'
  changed: boolean
  variable: ProjectSecretMetadata
  event_id: string | null
  audit_id: string | null
}>
export type ProjectSecretLookup =
  | Readonly<{ status: 'committed'; receipt: ProjectSecretReceipt }>
  | Readonly<{ status: 'not_observed'; receipt: null }>
export interface ProjectSecretCommandsAPI {
  create(
    projectID: string,
    request: ProjectSecretCreate,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectSecretReceipt>
  update(
    projectID: string,
    targetID: string,
    expectedVersion: string,
    request: ProjectSecretUpdate,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectSecretReceipt>
  lookup(
    identity: ProjectSecretIdentity,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectSecretLookup>
}
const encoder = new TextEncoder()
function requireValue(condition: boolean): asserts condition {
  if (!condition) throw new AccountFailure('invalid-response')
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
function version(value: unknown): string {
  const result = string(value, 1, 19)
  requireValue(
    /^[1-9]/.test(result) && !/[^0-9]/.test(result) && BigInt(result) <= 9223372036854775807n,
  )
  return result
}
function scalar(value: unknown, maximum: number): string {
  const result = string(value, 0, maximum)
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  requireValue(encoder.encode(result).byteLength <= maximum)
  return result
}
function name(value: unknown): string {
  const result = string(value, 1, 128)
  requireValue(
    /^[A-Za-z_]/.test(result) && !/[^A-Za-z0-9_]/.test(result) && !/^AGENTEAM(?:_|$)/i.test(result),
  )
  return result
}
function description(value: unknown): string {
  const result = scalar(value, 4096)
  requireValue(!/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/u.test(result))
  return result
}
export function captureProjectSecretValue(value: unknown): string {
  return input(() => {
    const result = scalar(value, 65536)
    requireValue(result.length > 0 && !result.includes('\0'))
    return result
  })
}
export function captureProjectSecretCreate(value: unknown): ProjectSecretCreate {
  return input(() => {
    const v = shape(value, ['variable_id', 'name', 'description', 'value'])
    return Object.freeze({
      variable_id: id(v.variable_id),
      name: name(v.name),
      description: description(v.description),
      value: captureProjectSecretValue(v.value),
    })
  })
}
export function captureProjectSecretUpdate(value: unknown): ProjectSecretUpdate {
  return input(() => {
    const v = shape(value, [], ['name', 'description', 'value'])
    requireValue(Object.keys(v).length > 0)
    return Object.freeze({
      ...(Object.hasOwn(v, 'name') ? { name: name(v.name) } : {}),
      ...(Object.hasOwn(v, 'description') ? { description: description(v.description) } : {}),
      ...(Object.hasOwn(v, 'value') ? { value: captureProjectSecretValue(v.value) } : {}),
    })
  })
}
export function captureProjectSecretIdentity(value: unknown): ProjectSecretIdentity {
  return input(() => {
    const v = shape(value, ['kind', 'projectID', 'targetID'], ['expectedVersion'])
    const projectID = id(v.projectID),
      targetID = id(v.targetID)
    if (v.kind === 'create') {
      shape(v, ['kind', 'projectID', 'targetID'])
      return Object.freeze({ kind: 'create', projectID, targetID })
    }
    requireValue(v.kind === 'update')
    return Object.freeze({
      kind: 'update',
      projectID,
      targetID,
      expectedVersion: version(v.expectedVersion),
    })
  })
}
export function projectSecretLookupBody(
  identity: ProjectSecretIdentity,
): Readonly<Record<string, string>> {
  const original = captureProjectSecretIdentity(identity)
  return Object.freeze({
    command: `project.secret_variable.${original.kind}`,
    target_id: original.targetID,
    ...(original.kind === 'update' ? { expected_version: original.expectedVersion } : {}),
  })
}
function receipt(
  value: unknown,
  original: ProjectSecretIdentity,
  supplied?: Readonly<{ name?: string; description?: string; replaces?: boolean }>,
): ProjectSecretReceipt {
  const v = shape(value, ['command', 'changed', 'variable', 'event_id', 'audit_id'])
  requireValue(
    v.command === `project.secret_variable.${original.kind}` && typeof v.changed === 'boolean',
  )
  const variable = parseProjectSecretMetadata(v.variable, original.projectID, original.targetID)
  const eventID = v.event_id === null ? null : id(v.event_id),
    auditID = v.audit_id === null ? null : id(v.audit_id)
  requireValue(v.changed === (eventID !== null) && v.changed === (auditID !== null))
  if (original.kind === 'create') requireValue(v.changed && variable.version === '1')
  else
    requireValue(
      BigInt(variable.version) === BigInt(original.expectedVersion) + (v.changed ? 1n : 0n),
    )
  if (supplied) {
    requireValue(!supplied.replaces || v.changed)
    requireValue(supplied.name === undefined || supplied.name === variable.name)
    requireValue(
      supplied.description === undefined || supplied.description === variable.description,
    )
  }
  return Object.freeze({
    command: v.command as ProjectSecretReceipt['command'],
    changed: v.changed,
    variable,
    event_id: eventID,
    audit_id: auditID,
  })
}
export function createProjectSecretCommandsAPI(fetcher?: Fetch): ProjectSecretCommandsAPI {
  const request = accountTransport(fetcher)
  return {
    create(projectID, raw, csrf, key, signal) {
      const captured = captureProjectSecretCreate(raw)
      const original = captureProjectSecretIdentity({
        kind: 'create',
        projectID,
        targetID: captured.variable_id,
      })
      // Receipt validation closes over safe metadata, never the material.
      const supplied = Object.freeze({ name: captured.name, description: captured.description })
      return request('createProjectSecret', (value) => receipt(value, original, supplied), {
        signal,
        projectID: original.projectID,
        csrf,
        key,
        body: { request: captured },
      })
    },
    update(projectID, targetID, expectedVersion, raw, csrf, key, signal) {
      const captured = captureProjectSecretUpdate(raw)
      const original = captureProjectSecretIdentity({
        kind: 'update',
        projectID,
        targetID,
        expectedVersion,
      })
      const supplied = Object.freeze({
        ...('name' in captured ? { name: captured.name } : {}),
        ...('description' in captured ? { description: captured.description } : {}),
        replaces: 'value' in captured,
      })
      return request('updateProjectSecret', (value) => receipt(value, original, supplied), {
        signal,
        projectID: original.projectID,
        target: original.targetID,
        csrf,
        key,
        body: { expected_version: expectedVersion, request: captured },
      })
    },
    lookup(identity, csrf, key, signal) {
      const original = captureProjectSecretIdentity(identity)
      return request<ProjectSecretLookup, 'lookupProjectSecret'>(
        'lookupProjectSecret',
        (value) => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'not_observed') {
            requireValue(v.receipt === null)
            return Object.freeze({ status: 'not_observed', receipt: null })
          }
          requireValue(v.status === 'committed')
          return Object.freeze({ status: 'committed', receipt: receipt(v.receipt, original) })
        },
        {
          signal,
          projectID: original.projectID,
          csrf,
          key,
          body: projectSecretLookupBody(original),
        },
      )
    },
  }
}
