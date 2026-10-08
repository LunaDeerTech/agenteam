import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'

export type ProjectLifecycle = 'active' | 'archiving' | 'archived' | 'deleting'
export type Project = Readonly<{
  id: string
  owner_user_id: string
  name: string
  normalized_name: string
  description: string
  lifecycle: Exclude<ProjectLifecycle, 'deleting'>
  version: string
  current_sprint_id: string | null
  created_at: string
  updated_at: string
  archived_at: string | null
}>
export type ResolvedProject = Readonly<Omit<Project, 'lifecycle'> & { lifecycle: ProjectLifecycle }>
export type ProjectListItem = Readonly<
  { id: string; name: string; version: string } & (
    | { lifecycle: 'active' | 'archived'; description: string }
    | { lifecycle: 'archiving'; description: string; operation_id: string }
    | { lifecycle: 'deleting'; operation_id: string }
  )
>
export type ProjectPage = Readonly<{
  items: readonly ProjectListItem[]
  next_cursor: string | null
}>
export type ProjectQuery = Readonly<{
  limit: number
  lifecycle?: readonly ProjectLifecycle[]
  cursor?: string
}>
export type ProjectAddress = Readonly<{ username: string; project_name: string }>
export type ProjectUpdate = Readonly<{
  expected_version: string
  name?: string
  description?: string
}>
export type ProjectLookup =
  | Readonly<{ state: 'committed'; result: Readonly<{ command: 'update'; project: Project }> }>
  | Readonly<{ state: 'in_progress' | 'not_observed' }>
export interface ProjectOwnerAPI {
  list(query: ProjectQuery, signal: AbortSignal): Promise<ProjectPage>
  resolve(address: ProjectAddress, ownerID: string, signal: AbortSignal): Promise<ResolvedProject>
  get(id: string, ownerID: string, signal: AbortSignal): Promise<Project>
  update(id: string, ownerID: string, input: ProjectUpdate, write: WriteOptions): Promise<Project>
  lookup(
    id: string,
    ownerID: string,
    input: ProjectUpdate,
    write: WriteOptions,
  ): Promise<ProjectLookup>
}
const encoder = new TextEncoder()
export const projectMaximumVersion = '9223372036854775807'
const lifecycles = ['active', 'archiving', 'archived', 'deleting'] as const
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
function unicode(value: unknown, minimum: number, maximum: number) {
  const result = string(value, minimum, maximum)
  // UTF-8 must not silently replace a lone surrogate with U+FFFD.
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  requireValue(encoder.encode(result).byteLength <= maximum)
  return result
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function version(value: unknown) {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= BigInt(projectMaximumVersion))
  return result
}
function name(value: unknown) {
  const result = string(value, 1, 64)
  requireValue(/^[A-Za-z0-9._-]+$/.test(result) && result !== '.' && result !== '..')
  return result
}
function description(value: unknown) {
  const result = unicode(value, 0, 8192)
  requireValue(!/[\u0000-\u0008\u000b-\u001f\u007f]/u.test(result))
  return result
}
function cursor(value: unknown) {
  return unicode(value, 1, 8192)
}
export function captureProjectAddress(value: unknown): ProjectAddress {
  return input(() => {
    const v = shape(value, ['username', 'project_name'])
    const username = string(v.username, 3, 32)
    requireValue(/^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])$/.test(username))
    return Object.freeze({
      username: username.toLowerCase(),
      project_name: name(v.project_name).toLowerCase(),
    })
  })
}
export function captureProjectQuery(value: unknown): ProjectQuery {
  return input(() => {
    const v = shape(value, ['limit'], ['cursor', 'lifecycle'])
    requireValue(
      typeof v.limit === 'number' && Number.isInteger(v.limit) && v.limit >= 1 && v.limit <= 100,
    )
    const captured: { limit: number; lifecycle?: readonly ProjectLifecycle[]; cursor?: string } = {
      limit: v.limit,
    }
    if (Object.hasOwn(v, 'lifecycle')) {
      requireValue(Array.isArray(v.lifecycle) && v.lifecycle.length >= 1 && v.lifecycle.length <= 4)
      const selected = v.lifecycle as ProjectLifecycle[]
      requireValue(
        selected.every((value) => lifecycles.includes(value)) &&
          new Set(selected).size === selected.length,
      )
      captured.lifecycle = Object.freeze(lifecycles.filter((value) => selected.includes(value)))
    }
    if (Object.hasOwn(v, 'cursor')) captured.cursor = cursor(v.cursor)
    return Object.freeze(captured)
  })
}
export function captureProjectUpdate(value: unknown): ProjectUpdate {
  return input(() => {
    const v = shape(value, ['expected_version'], ['name', 'description'])
    requireValue(Object.hasOwn(v, 'name') || Object.hasOwn(v, 'description'))
    const result = Object.freeze({
      expected_version: version(v.expected_version),
      ...(Object.hasOwn(v, 'name') ? { name: name(v.name) } : {}),
      ...(Object.hasOwn(v, 'description') ? { description: description(v.description) } : {}),
    })
    requireValue(encoder.encode(JSON.stringify(result)).byteLength <= 64 * 1024)
    return result
  })
}
function project(
  value: unknown,
  ownerID: string,
  targetID: string | null,
  deleting: boolean,
): ResolvedProject {
  const v = shape(value, [
    'id',
    'owner_user_id',
    'name',
    'normalized_name',
    'description',
    'lifecycle',
    'version',
    'current_sprint_id',
    'created_at',
    'updated_at',
    'archived_at',
  ])
  const projectID = id(v.id),
    owner = id(v.owner_user_id),
    projectName = name(v.name)
  requireValue(owner === ownerID && (targetID === null || projectID === targetID))
  requireValue(v.normalized_name === projectName.toLowerCase())
  requireValue(
    lifecycles.includes(v.lifecycle as ProjectLifecycle) &&
      (deleting || v.lifecycle !== 'deleting'),
  )
  const created = parseSystemInstant(v.created_at),
    updated = parseSystemInstant(v.updated_at)
  const archived = v.archived_at === null ? null : parseSystemInstant(v.archived_at)
  requireValue(
    created <= updated && (archived === null || (created <= archived && archived <= updated)),
  )
  requireValue(
    v.lifecycle === 'deleting' ||
      (v.lifecycle === 'archived' ? archived !== null : archived === null),
  )
  return Object.freeze({
    id: projectID,
    owner_user_id: owner,
    name: projectName,
    normalized_name: v.normalized_name as string,
    description: description(v.description),
    lifecycle: v.lifecycle as ProjectLifecycle,
    version: version(v.version),
    current_sprint_id: v.current_sprint_id === null ? null : id(v.current_sprint_id),
    created_at: created,
    updated_at: updated,
    archived_at: archived,
  })
}
function receipt(value: unknown, target: string, owner: string, captured: ProjectUpdate): Project {
  const result = project(value, owner, target, false)
  requireValue(result.lifecycle === 'active')
  requireValue(
    result.version === captured.expected_version ||
      BigInt(result.version) === BigInt(captured.expected_version) + 1n,
  )
  if (captured.name !== undefined) requireValue(result.name === captured.name)
  if (captured.description !== undefined) requireValue(result.description === captured.description)
  return result as Project
}
function page(value: unknown, query: ProjectQuery): ProjectPage {
  const v = shape(value, ['items', 'next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= query.limit)
  const seen = new Set<string>()
  const items = v.items.map((value): ProjectListItem => {
    const base = shape(
      value,
      ['id', 'name', 'lifecycle', 'version'],
      ['description', 'operation_id'],
    )
    const lifecycle = base.lifecycle as ProjectLifecycle
    requireValue(
      lifecycles.includes(lifecycle) && (!query.lifecycle || query.lifecycle.includes(lifecycle)),
    )
    shape(value, [
      'id',
      'name',
      'lifecycle',
      'version',
      ...(lifecycle === 'deleting' ? [] : ['description']),
      ...(['archiving', 'deleting'].includes(lifecycle) ? ['operation_id'] : []),
    ])
    const projectID = id(base.id)
    requireValue(!seen.has(projectID))
    seen.add(projectID)
    const common = { id: projectID, name: name(base.name), version: version(base.version) }
    if (lifecycle === 'deleting')
      return Object.freeze({ ...common, lifecycle, operation_id: id(base.operation_id) })
    const text = description(base.description)
    if (lifecycle === 'archiving')
      return Object.freeze({
        ...common,
        lifecycle,
        description: text,
        operation_id: id(base.operation_id),
      })
    return Object.freeze({ ...common, lifecycle, description: text })
  })
  const next = v.next_cursor === null ? null : cursor(v.next_cursor)
  requireValue(next === null || items.length === query.limit)
  return Object.freeze({ items: Object.freeze(items), next_cursor: next })
}
function target(idValue: string, owner: string) {
  return input(() => ({ target: id(idValue), owner: id(owner) }))
}
function writeOptions(write: WriteOptions) {
  return input(() => {
    const v = shape(write, ['csrfToken', 'key'], ['signal'])
    const csrf = string(v.csrfToken, 43, 43),
      key = string(v.key, 1, 128)
    requireValue(/^[A-Za-z0-9_-]{43}$/.test(csrf) && /^[A-Za-z0-9._:/-]{1,128}$/.test(key))
    return { csrf, key, signal: write.signal ?? new AbortController().signal }
  })
}
export function createProjectOwnerAPI(fetcher?: Fetch): ProjectOwnerAPI {
  const request = accountTransport(fetcher)
  return {
    async list(value, signal) {
      const query = captureProjectQuery(value)
      return request('listOwnerProjects', (value) => page(value, query), {
        signal,
        projects: query,
      })
    },
    async resolve(value, ownerID, signal) {
      const address = captureProjectAddress(value),
        owner = input(() => id(ownerID))
      return request(
        'resolveOwnerProject',
        (value) => {
          const result = project(value, owner, null, true)
          requireValue(result.normalized_name === address.project_name)
          return result
        },
        { signal, projectAddress: address },
      )
    },
    async get(idValue, ownerID, signal) {
      const t = target(idValue, ownerID)
      return request(
        'getOwnerProject',
        (value) => project(value, t.owner, t.target, false) as Project,
        { signal, target: t.target },
      )
    },
    async update(idValue, ownerID, value, write) {
      const t = target(idValue, ownerID),
        captured = captureProjectUpdate(value)
      return request('updateOwnerProject', (value) => receipt(value, t.target, t.owner, captured), {
        ...writeOptions(write),
        target: t.target,
        body: captured,
      })
    },
    async lookup(idValue, ownerID, value, write) {
      const t = target(idValue, ownerID),
        captured = captureProjectUpdate(value)
      return request(
        'lookupOwnerProject',
        (value): ProjectLookup => {
          const v = shape(value, ['state'], ['result'])
          if (v.state === 'in_progress' || v.state === 'not_observed') {
            shape(value, ['state'])
            return Object.freeze({ state: v.state })
          }
          requireValue(v.state === 'committed')
          const result = shape(v.result, ['command', 'project'])
          requireValue(result.command === 'update')
          return Object.freeze({
            state: 'committed',
            result: Object.freeze({
              command: 'update',
              project: receipt(result.project, t.target, t.owner, captured),
            }),
          })
        },
        { ...writeOptions(write), target: t.target, body: { command: 'update' } },
      )
    },
  }
}
