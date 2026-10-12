import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { captureProjectUpdate, parseProjectRef, type Project } from './project-owner'
import { parseSystemInstant } from './system-account'
import type { WriteOptions } from './account'

export type ProjectCreateInput = Readonly<{
  project_id: string
  name: string
  description?: string
}>
export type ProjectCreationOperation = Readonly<{
  id: string
  project_id: string
  state: 'accepted' | 'initializing' | 'failed'
  version: string
  safe_reason?:
    | 'dependency_unbound'
    | 'dependency_unavailable'
    | 'work_pending'
    | 'outcome_unknown'
    | 'operation_failed'
  created_at: string
  updated_at: string
}>
export type ProjectCreationResult =
  | Readonly<{ state: 'ready'; project: Project }>
  | Readonly<{ state: 'accepted'; operation: ProjectCreationOperation; status_url: string }>
export interface ProjectCreationAPI {
  create(
    ownerID: string,
    input: ProjectCreateInput,
    write: WriteOptions,
  ): Promise<ProjectCreationResult>
  get(
    ownerID: string,
    projectID: string,
    creationID: string,
    signal: AbortSignal,
  ): Promise<ProjectCreationResult>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
export function newProjectID(): string {
  const now = Date.now()
  if (!Number.isSafeInteger(now) || now < 0 || now >= 2 ** 48)
    throw new AccountFailure('invalid-input')
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  let timestamp = BigInt(now)
  for (let i = 5; i >= 0; i--) {
    bytes[i] = Number(timestamp & 255n)
    timestamp >>= 8n
  }
  bytes[6] = (bytes[6]! & 15) | 112
  bytes[8] = (bytes[8]! & 63) | 128
  const hex = Array.from(bytes, (v) => v.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
export function captureProjectCreate(value: unknown): ProjectCreateInput {
  try {
    const input = shape(value, ['project_id', 'name'], ['description'])
    const fields = captureProjectUpdate({
      expected_version: '1',
      name: input.name,
      ...(Object.hasOwn(input, 'description') ? { description: input.description } : {}),
    })
    return Object.freeze({
      project_id: id(input.project_id),
      name: fields.name!,
      ...(fields.description === undefined ? {} : { description: fields.description }),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function result(
  value: unknown,
  ownerID: string,
  projectID: string,
  creationID?: string,
): ProjectCreationResult {
  const header = shape(value, ['state'], ['project', 'operation', 'status_url'])
  if (header.state === 'ready') {
    const ready = shape(value, ['state', 'project'])
    return Object.freeze({
      state: 'ready',
      project: parseProjectRef(ready.project, ownerID, projectID),
    })
  }
  const accepted = shape(value, ['state', 'operation', 'status_url'])
  requireValue(accepted.state === 'accepted')
  const operation = shape(
    accepted.operation,
    ['id', 'project_id', 'state', 'version', 'created_at', 'updated_at'],
    ['safe_reason'],
  )
  const operationID = id(operation.id)
  requireValue(!creationID || operationID === creationID)
  requireValue(id(operation.project_id) === projectID)
  requireValue(['accepted', 'initializing', 'failed'].includes(operation.state as string))
  const version = string(operation.version, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(version) && BigInt(version) <= 9223372036854775807n)
  const created = parseSystemInstant(operation.created_at),
    updated = parseSystemInstant(operation.updated_at)
  requireValue(created <= updated)
  const reason = operation.safe_reason
  requireValue(
    reason === undefined ||
      [
        'dependency_unbound',
        'dependency_unavailable',
        'work_pending',
        'outcome_unknown',
        'operation_failed',
      ].includes(reason as string),
  )
  requireValue(operation.state !== 'failed' || reason !== undefined)
  requireValue(accepted.status_url === `/api/v1/project-creations/${operationID}`)
  return Object.freeze({
    state: 'accepted',
    status_url: accepted.status_url as string,
    operation: Object.freeze({
      id: operationID,
      project_id: projectID,
      state: operation.state as ProjectCreationOperation['state'],
      version,
      created_at: created,
      updated_at: updated,
      ...(reason === undefined
        ? {}
        : { safe_reason: reason as ProjectCreationOperation['safe_reason'] }),
    }),
  })
}
export function createProjectCreationAPI(fetcher?: Fetch): ProjectCreationAPI {
  const request = accountTransport(fetcher)
  return {
    create(ownerID, input, write) {
      const captured = captureProjectCreate(input)
      return request('createOwnerProject', (value) => result(value, ownerID, captured.project_id), {
        signal: write.signal ?? new AbortController().signal,
        body: captured,
        csrf: write.csrfToken,
        key: write.key,
      })
    },
    get(ownerID, projectID, creationID, signal) {
      return request(
        'getProjectCreation',
        (value) => result(value, ownerID, projectID, creationID),
        {
          signal,
          target: creationID,
        },
      )
    },
  }
}
