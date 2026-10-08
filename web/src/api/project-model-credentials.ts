import type { WriteOptions } from './account'
import { accountTransport, object, shape, type Fetch } from './client'
import {
  captureProjectModelID,
  captureProjectModelInput,
  captureProjectModelWriteOptions,
  parseProjectModelID,
  parseProjectModelVersion,
  projectModelByteBudget,
  projectModelWellFormed,
  requireProjectModelValue,
} from './project-models'

export type ProjectCredentialLookupTarget =
  | Readonly<{ kind: 'create' }>
  | Readonly<{
      kind: 'update' | 'delete'
      credential_id: string
      expected_version: string
    }>
export type ProjectCredentialMetadata = Readonly<{
  credential_id: string
  purpose: 'model'
  version: string
}>
export type ProjectCredentialCreated = ProjectCredentialMetadata &
  Readonly<{ version: '1'; deleted: false }>
export type ProjectCredentialUpdated = ProjectCredentialMetadata & Readonly<{ deleted: false }>
export type ProjectCredentialDeleted = ProjectCredentialMetadata & Readonly<{ deleted: true }>
export type ProjectCredentialMutation =
  ProjectCredentialCreated | ProjectCredentialUpdated | ProjectCredentialDeleted
export type ProjectCredentialObservation =
  | Readonly<{ observed: false; result: null }>
  | Readonly<{ observed: true; result: ProjectCredentialMutation }>
export interface ProjectModelCredentialsAPI {
  getCredentialMetadata(
    projectID: string,
    credentialID: string,
    signal: AbortSignal,
  ): Promise<ProjectCredentialMetadata>
  createCredential(
    projectID: string,
    value: string,
    options: WriteOptions,
  ): Promise<ProjectCredentialCreated>
  updateCredential(
    projectID: string,
    credentialID: string,
    expectedVersion: string,
    value: string,
    options: WriteOptions,
  ): Promise<ProjectCredentialUpdated>
  deleteCredential(
    projectID: string,
    credentialID: string,
    expectedVersion: string,
    options: WriteOptions,
  ): Promise<ProjectCredentialDeleted>
  lookupCredential(
    projectID: string,
    command: ProjectCredentialLookupTarget,
    options: WriteOptions,
  ): Promise<ProjectCredentialObservation>
}

export function captureProjectCredentialTarget<C extends ProjectCredentialLookupTarget>(
  value: C,
): C {
  return captureProjectModelInput(() => {
    const raw = { ...object(value) },
      kind = raw.kind
    requireProjectModelValue(kind === 'create' || kind === 'update' || kind === 'delete')
    const v = shape(
      raw,
      kind === 'create' ? ['kind'] : ['kind', 'credential_id', 'expected_version'],
    )
    if (kind === 'create') return Object.freeze({ kind }) as C
    const expected_version = parseProjectModelVersion(v.expected_version)
    requireProjectModelValue(BigInt(expected_version) < 9223372036854775807n)
    return Object.freeze({
      kind,
      credential_id: parseProjectModelID(v.credential_id),
      expected_version,
    }) as C
  })
}
// Material is returned only to the private caller for its original Execute.
// Neither errors nor observations retain it or synthesize a material digest.
export function captureProjectCredentialValue(value: string): string {
  return captureProjectModelInput(() => {
    requireProjectModelValue(
      typeof value === 'string' &&
        value.length > 0 &&
        projectModelWellFormed(value) &&
        new TextEncoder().encode(value).byteLength <= 65536,
    )
    projectModelByteBudget({ value }, 409600)
    return value
  })
}
export function parseProjectCredentialMetadata(
  value: unknown,
  credentialID?: string,
): ProjectCredentialMetadata {
  const v = shape(value, ['credential_id', 'purpose', 'version'])
  const credential_id = parseProjectModelID(v.credential_id)
  requireProjectModelValue(
    v.purpose === 'model' && (credentialID === undefined || credential_id === credentialID),
  )
  return Object.freeze({
    credential_id,
    purpose: 'model',
    version: parseProjectModelVersion(v.version),
  })
}
export function parseProjectCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'create' }>,
): ProjectCredentialCreated
export function parseProjectCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'update'; credential_id: string; expected_version: string }>,
): ProjectCredentialUpdated
export function parseProjectCredentialMutation(
  value: unknown,
  command: Readonly<{ kind: 'delete'; credential_id: string; expected_version: string }>,
): ProjectCredentialDeleted
export function parseProjectCredentialMutation(
  value: unknown,
  command: ProjectCredentialLookupTarget,
): ProjectCredentialMutation
export function parseProjectCredentialMutation(
  value: unknown,
  command: ProjectCredentialLookupTarget,
): ProjectCredentialMutation {
  const v = shape(value, ['credential_id', 'purpose', 'version', 'deleted'])
  const metadata = parseProjectCredentialMetadata(
    { credential_id: v.credential_id, purpose: v.purpose, version: v.version },
    command.kind === 'create' ? undefined : command.credential_id,
  )
  requireProjectModelValue(v.deleted === (command.kind === 'delete'))
  if (command.kind === 'create') {
    requireProjectModelValue(metadata.version === '1')
    return Object.freeze({ ...metadata, version: '1', deleted: false })
  }
  requireProjectModelValue(BigInt(metadata.version) === BigInt(command.expected_version) + 1n)
  return Object.freeze({ ...metadata, deleted: command.kind === 'delete' })
}
export function parseProjectCredentialObservation(
  value: unknown,
  command: ProjectCredentialLookupTarget,
): ProjectCredentialObservation {
  const v = shape(value, ['observed', 'result'])
  requireProjectModelValue(typeof v.observed === 'boolean')
  if (!v.observed) {
    requireProjectModelValue(v.result === null)
    return Object.freeze({ observed: false, result: null })
  }
  return Object.freeze({
    observed: true,
    result: parseProjectCredentialMutation(v.result, command),
  })
}
export function createProjectModelCredentialAPI(fetcher?: Fetch): ProjectModelCredentialsAPI {
  const request = accountTransport(fetcher)
  return {
    async getCredentialMetadata(projectID, credentialID, signal) {
      const project = captureProjectModelID(projectID),
        target = captureProjectModelID(credentialID)
      return request(
        'getProjectModelCredentialMetadata',
        (value) => parseProjectCredentialMetadata(value, target),
        { signal, projectID: project, target },
      )
    },
    async createCredential(projectID, value, options) {
      const project = captureProjectModelID(projectID),
        material = captureProjectCredentialValue(value),
        write = captureProjectModelWriteOptions(options)
      return request(
        'createProjectModelCredential',
        (value) => parseProjectCredentialMutation(value, { kind: 'create' }),
        { ...write, projectID: project, body: { value: material } },
      )
    },
    async updateCredential(projectID, credentialID, expectedVersion, value, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectCredentialTarget({
          kind: 'update',
          credential_id: credentialID,
          expected_version: expectedVersion,
        }),
        material = captureProjectCredentialValue(value),
        write = captureProjectModelWriteOptions(options)
      const body = { expected_version: command.expected_version, value: material }
      captureProjectModelInput(() => projectModelByteBudget(body, 409600))
      return request(
        'updateProjectModelCredential',
        (value) => parseProjectCredentialMutation(value, command),
        { ...write, projectID: project, target: command.credential_id, body },
      )
    },
    async deleteCredential(projectID, credentialID, expectedVersion, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectCredentialTarget({
          kind: 'delete',
          credential_id: credentialID,
          expected_version: expectedVersion,
        }),
        write = captureProjectModelWriteOptions(options)
      return request(
        'deleteProjectModelCredential',
        (value) => parseProjectCredentialMutation(value, command),
        {
          ...write,
          projectID: project,
          target: command.credential_id,
          body: { expected_version: command.expected_version },
        },
      )
    },
    async lookupCredential(projectID, original, options) {
      const project = captureProjectModelID(projectID),
        command = captureProjectCredentialTarget(original),
        write = captureProjectModelWriteOptions(options)
      return request(
        'lookupProjectModelCredential',
        (value) => parseProjectCredentialObservation(value, command),
        { ...write, projectID: project, body: command },
      )
    },
  }
}
