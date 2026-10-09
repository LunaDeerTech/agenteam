import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type ProjectVariableSummary = Readonly<{
  id: string
  project_id: string
  type: 'variable'
  name: string
  description: string
  version: string
  created_at: string
  updated_at: string
}>
export type ProjectVariable = Readonly<ProjectVariableSummary & { value: string }>
export type ProjectVariablePage = Readonly<{
  items: readonly ProjectVariableSummary[]
  next_cursor?: string
}>
export type ProjectVariableQuery = Readonly<{ limit: number; cursor?: string }>
export type ProjectVariableCreate = Readonly<{
  variable_id: string
  name: string
  description: string
  value: string
}>
export type ProjectVariableUpdate = Readonly<{
  name?: string
  description?: string
  value?: string
}>
export type ProjectVariableCommand =
  | Readonly<{ kind: 'create'; projectID: string; request: ProjectVariableCreate }>
  | Readonly<{
      kind: 'update'
      projectID: string
      targetID: string
      expectedVersion: string
      request: ProjectVariableUpdate
    }>
  | Readonly<{ kind: 'delete'; projectID: string; targetID: string; expectedVersion: string }>
export type ProjectVariableDeleted = Readonly<{
  id: string
  project_id: string
  type: 'variable'
  version: string
  deleted_at: string
}>
export type ProjectVariableReceipt =
  | Readonly<{
      command: 'project.variable.create' | 'project.variable.update'
      changed: boolean
      variable: ProjectVariable
      event_id: string | null
      audit_id: string | null
    }>
  | Readonly<{
      command: 'project.variable.delete'
      changed: true
      deleted: ProjectVariableDeleted
      event_id: string
      audit_id: string
    }>
export type ProjectVariableLookup =
  | Readonly<{ status: 'committed'; receipt: ProjectVariableReceipt }>
  | Readonly<{ status: 'not_observed' | 'in_progress'; receipt: null }>
export interface ProjectVariablesAPI {
  list(
    projectID: string,
    query: ProjectVariableQuery,
    signal: AbortSignal,
  ): Promise<ProjectVariablePage>
  get(projectID: string, targetID: string, signal: AbortSignal): Promise<ProjectVariable>
  create(
    projectID: string,
    request: ProjectVariableCreate,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectVariableReceipt>
  update(
    projectID: string,
    targetID: string,
    expectedVersion: string,
    request: ProjectVariableUpdate,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectVariableReceipt>
  delete(
    projectID: string,
    targetID: string,
    expectedVersion: string,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectVariableReceipt>
  lookup(
    command: ProjectVariableCommand,
    csrf: string,
    key: string,
    signal: AbortSignal,
  ): Promise<ProjectVariableLookup>
}
const encoder = new TextEncoder()
const maximumVersion = 9223372036854775807n
const summaryKeys = [
  'id',
  'project_id',
  'type',
  'name',
  'description',
  'version',
  'created_at',
  'updated_at',
]
function requireValue(value: boolean): asserts value {
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
export function newProjectVariableID(): string {
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
export const captureProjectVariableID = (value: unknown): string => input(() => id(value))
export function captureProjectVariableQuery(value: unknown): ProjectVariableQuery {
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
export function captureProjectVariableCreate(value: unknown): ProjectVariableCreate {
  return input(() => {
    const v = shape(value, ['variable_id', 'name', 'description', 'value'])
    return Object.freeze({
      variable_id: id(v.variable_id),
      name: name(v.name),
      description: description(v.description),
      value: text(v.value, 32768),
    })
  })
}
export function captureProjectVariableUpdate(value: unknown): ProjectVariableUpdate {
  return input(() => {
    const v = shape(value, [], ['name', 'description', 'value'])
    requireValue(Object.keys(v).length > 0)
    return Object.freeze({
      ...(Object.hasOwn(v, 'name') ? { name: name(v.name) } : {}),
      ...(Object.hasOwn(v, 'description') ? { description: description(v.description) } : {}),
      ...(Object.hasOwn(v, 'value') ? { value: text(v.value, 32768) } : {}),
    })
  })
}
export function captureProjectVariableCommand(value: unknown): ProjectVariableCommand {
  return input(() => {
    const v = shape(value, ['kind', 'projectID'], ['targetID', 'expectedVersion', 'request'])
    const projectID = id(v.projectID)
    if (v.kind === 'create') {
      shape(v, ['kind', 'projectID', 'request'])
      return Object.freeze({
        kind: 'create',
        projectID,
        request: captureProjectVariableCreate(v.request),
      })
    }
    if (v.kind === 'update') {
      shape(v, ['kind', 'projectID', 'targetID', 'expectedVersion', 'request'])
      return Object.freeze({
        kind: 'update',
        projectID,
        targetID: id(v.targetID),
        expectedVersion: version(v.expectedVersion),
        request: captureProjectVariableUpdate(v.request),
      })
    }
    requireValue(v.kind === 'delete')
    shape(v, ['kind', 'projectID', 'targetID', 'expectedVersion'])
    return Object.freeze({
      kind: 'delete',
      projectID,
      targetID: id(v.targetID),
      expectedVersion: version(v.expectedVersion),
    })
  })
}
export function projectVariableTarget(command: ProjectVariableCommand): string {
  return command.kind === 'create' ? command.request.variable_id : command.targetID
}
export function projectVariableBody(
  command: ProjectVariableCommand,
): Readonly<Record<string, unknown>> {
  const c = captureProjectVariableCommand(command)
  return Object.freeze(
    c.kind === 'create'
      ? { request: c.request }
      : c.kind === 'update'
        ? { expected_version: c.expectedVersion, request: c.request }
        : { expected_version: c.expectedVersion },
  )
}
function parseSummaryFields(
  v: Record<string, unknown>,
  projectID: string,
  targetID?: string,
): ProjectVariableSummary {
  const target = id(v.id),
    project = id(v.project_id),
    created = instant(v.created_at),
    updated = instant(v.updated_at),
    revision = version(v.version)
  requireValue(
    project === projectID &&
      (targetID === undefined || targetID === target) &&
      v.type === 'variable' &&
      created <= updated,
  )
  requireValue(revision !== '1' || created === updated)
  return Object.freeze({
    id: target,
    project_id: project,
    type: 'variable',
    name: name(v.name),
    description: description(v.description),
    version: revision,
    created_at: created,
    updated_at: updated,
  })
}
export function parseProjectVariable(
  value: unknown,
  projectID: string,
  targetID: string,
): ProjectVariable {
  id(projectID)
  id(targetID)
  const v = shape(value, [...summaryKeys, 'value'])
  return Object.freeze({
    ...parseSummaryFields(v, projectID, targetID),
    value: text(v.value, 32768),
  })
}
export function parseProjectVariablePage(
  value: unknown,
  projectID: string,
  query: ProjectVariableQuery,
): ProjectVariablePage {
  id(projectID)
  const q = captureProjectVariableQuery(query),
    page = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(page.items) && page.items.length <= q.limit)
  const seen = new Set<string>(),
    names = new Set<string>()
  let previous: ProjectVariableSummary | undefined
  const items = page.items.map((row: unknown) => {
    const v = parseSummaryFields(shape(row, summaryKeys), projectID)
    requireValue(!seen.has(v.id) && !names.has(v.name))
    requireValue(
      !previous || previous.name < v.name || (previous.name === v.name && previous.id < v.id),
    )
    seen.add(v.id)
    names.add(v.name)
    previous = v
    return v
  })
  const next = Object.hasOwn(page, 'next_cursor') ? cursor(page.next_cursor) : undefined
  requireValue(next === undefined || (items.length === q.limit && next !== q.cursor))
  return Object.freeze({
    items: Object.freeze(items),
    ...(next === undefined ? {} : { next_cursor: next }),
  })
}
export function parseProjectVariableReceipt(
  value: unknown,
  command: ProjectVariableCommand,
): ProjectVariableReceipt {
  const c = captureProjectVariableCommand(command)
  const v = shape(value, [
    'command',
    'changed',
    'event_id',
    'audit_id',
    c.kind === 'delete' ? 'deleted' : 'variable',
  ])
  requireValue(v.command === `project.variable.${c.kind}` && typeof v.changed === 'boolean')
  const event = v.event_id === null ? null : id(v.event_id),
    audit = v.audit_id === null ? null : id(v.audit_id)
  requireValue(v.changed === (event !== null) && v.changed === (audit !== null))
  if (c.kind === 'delete') {
    requireValue(v.changed && event !== null && audit !== null)
    const d = shape(v.deleted, ['id', 'project_id', 'type', 'version', 'deleted_at'])
    requireValue(
      id(d.id) === c.targetID && id(d.project_id) === c.projectID && d.type === 'variable',
    )
    const revision = version(d.version)
    requireValue(BigInt(revision) === BigInt(c.expectedVersion) + 1n)
    return Object.freeze({
      command: 'project.variable.delete',
      changed: true,
      deleted: Object.freeze({
        id: c.targetID,
        project_id: c.projectID,
        type: 'variable',
        version: revision,
        deleted_at: instant(d.deleted_at),
      }),
      event_id: event,
      audit_id: audit,
    })
  }
  const variable = parseProjectVariable(v.variable, c.projectID, projectVariableTarget(c))
  if (c.kind === 'create')
    requireValue(
      v.changed && variable.version === '1' && variable.created_at === variable.updated_at,
    )
  else requireValue(BigInt(variable.version) === BigInt(c.expectedVersion) + (v.changed ? 1n : 0n))
  for (const key of ['name', 'description', 'value'] as const) {
    if (Object.hasOwn(c.request, key)) requireValue(variable[key] === c.request[key])
  }
  return Object.freeze({
    command: c.kind === 'create' ? 'project.variable.create' : 'project.variable.update',
    changed: v.changed,
    variable,
    event_id: event,
    audit_id: audit,
  })
}
export function parseProjectVariableLookup(
  value: unknown,
  command: ProjectVariableCommand,
): ProjectVariableLookup {
  const c = captureProjectVariableCommand(command),
    v = shape(value, ['status', 'receipt'])
  if (v.status === 'committed')
    return Object.freeze({
      status: 'committed',
      receipt: parseProjectVariableReceipt(v.receipt, c),
    })
  requireValue((v.status === 'not_observed' || v.status === 'in_progress') && v.receipt === null)
  return Object.freeze({ status: v.status, receipt: null })
}
export function createProjectVariablesAPI(fetcher?: Fetch): ProjectVariablesAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, query, signal) {
      const project = captureProjectVariableID(projectID),
        q = captureProjectVariableQuery(query)
      return request('listProjectVariables', (v) => parseProjectVariablePage(v, project, q), {
        projectID: project,
        variables: q,
        signal,
      })
    },
    async get(projectID, targetID, signal) {
      const project = captureProjectVariableID(projectID),
        target = captureProjectVariableID(targetID)
      return request('getProjectVariable', (v) => parseProjectVariable(v, project, target), {
        projectID: project,
        target,
        signal,
      })
    },
    async create(projectID, original, csrf, key, signal) {
      const c = captureProjectVariableCommand({ kind: 'create', projectID, request: original })
      return request('createProjectVariable', (v) => parseProjectVariableReceipt(v, c), {
        projectID: c.projectID,
        body: projectVariableBody(c),
        csrf,
        key,
        signal,
      })
    },
    async update(projectID, targetID, expectedVersion, original, csrf, key, signal) {
      const c = captureProjectVariableCommand({
        kind: 'update',
        projectID,
        targetID,
        expectedVersion,
        request: original,
      })
      return request('updateProjectVariable', (v) => parseProjectVariableReceipt(v, c), {
        projectID: c.projectID,
        target: projectVariableTarget(c),
        body: projectVariableBody(c),
        csrf,
        key,
        signal,
      })
    },
    async delete(projectID, targetID, expectedVersion, csrf, key, signal) {
      const c = captureProjectVariableCommand({
        kind: 'delete',
        projectID,
        targetID,
        expectedVersion,
      })
      return request('deleteProjectVariable', (v) => parseProjectVariableReceipt(v, c), {
        projectID: c.projectID,
        target: projectVariableTarget(c),
        body: projectVariableBody(c),
        csrf,
        key,
        signal,
      })
    },
    async lookup(command, csrf, key, signal) {
      const c = captureProjectVariableCommand(command)
      const body = Object.freeze({
        command: `project.variable.${c.kind}`,
        ...(c.kind === 'create' ? {} : { target_id: c.targetID }),
        ...projectVariableBody(c),
      })
      return request('lookupProjectVariable', (v) => parseProjectVariableLookup(v, c), {
        projectID: c.projectID,
        body,
        csrf,
        key,
        signal,
      })
    },
  }
}
