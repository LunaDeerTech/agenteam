import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { captureProjectUpdate } from './project-owner'
import { parseSystemInstant } from './system-account'

export type ProjectSchedulerConfiguration = Readonly<{
  project_id: string
  version: string
  scheduler_enabled: boolean
  scheduler_max_concurrency: string | null
}>
export type ProjectSchedulerUpdate = Readonly<{
  expected_version: string
  scheduler_enabled: boolean
  scheduler_max_concurrency: string | null
}>
// Acknowledgement contains no configuration values and is never a current read.
export type ProjectSchedulerReceipt = Readonly<{ project_id: string; version: string }>
export type ProjectSchedulerLookup =
  | Readonly<{ state: 'committed'; receipt: ProjectSchedulerReceipt }>
  | Readonly<{ state: 'in_progress' | 'not_observed' }>
export interface ProjectSchedulerAPI {
  get(projectID: string, signal: AbortSignal): Promise<ProjectSchedulerConfiguration>
  update(
    projectID: string,
    input: ProjectSchedulerUpdate,
    write: WriteOptions,
  ): Promise<ProjectSchedulerReceipt>
  lookup(projectID: string, ownerID: string, write: WriteOptions): Promise<ProjectSchedulerLookup>
}
export type ProjectSchedulerDraft = {
  expected_version: string
  enabled: boolean
  unlimited: boolean
  limit: string
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function positive(value: unknown): string {
  const result = string(value, 1, 19)
  requireValue(
    /^[1-9][0-9]{0,18}$/.test(result) &&
      !/[^0-9]/.test(result) &&
      BigInt(result) <= 9223372036854775807n,
  )
  return result
}
function projectID(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function enabled(value: unknown): boolean {
  requireValue(typeof value === 'boolean')
  return value
}
function limit(value: unknown): string | null {
  return value === null ? null : positive(value)
}
export function captureProjectSchedulerUpdate(value: unknown): ProjectSchedulerUpdate {
  try {
    const v = shape(value, ['expected_version', 'scheduler_enabled', 'scheduler_max_concurrency'])
    return Object.freeze({
      expected_version: positive(v.expected_version),
      scheduler_enabled: enabled(v.scheduler_enabled),
      scheduler_max_concurrency: limit(v.scheduler_max_concurrency),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function parseProjectSchedulerConfiguration(
  value: unknown,
  target: string,
): ProjectSchedulerConfiguration {
  const v = shape(value, [
    'project_id',
    'version',
    'scheduler_enabled',
    'scheduler_max_concurrency',
  ])
  requireValue(projectID(v.project_id) === target)
  return Object.freeze({
    project_id: target,
    version: positive(v.version),
    scheduler_enabled: enabled(v.scheduler_enabled),
    scheduler_max_concurrency: limit(v.scheduler_max_concurrency),
  })
}
export function parseProjectSchedulerReceipt(
  value: unknown,
  target: string,
  original: ProjectSchedulerUpdate,
): ProjectSchedulerReceipt {
  const v = shape(value, ['project_id', 'version'])
  const version = positive(v.version)
  requireValue(projectID(v.project_id) === target)
  requireValue(
    version === original.expected_version ||
      BigInt(version) === BigInt(original.expected_version) + 1n,
  )
  return Object.freeze({ project_id: target, version })
}
export function parseProjectSchedulerLookup(
  value: unknown,
  target: string,
  owner: string,
): ProjectSchedulerLookup {
  const v = shape(value, ['state'], ['result'])
  if (v.state === 'in_progress' || v.state === 'not_observed') {
    shape(value, ['state'])
    return Object.freeze({ state: v.state })
  }
  requireValue(v.state === 'committed')
  const result = shape(v.result, ['command', 'project'])
  requireValue(result.command === 'update')
  // The unchanged Project lookup returns historical full metadata, keyed only
  // by Project + update + key. It neither carries nor verifies Scheduler intent.
  const p = shape(result.project, [
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
  requireValue(projectID(p.id) === target && projectID(p.owner_user_id) === owner)
  requireValue(p.lifecycle === 'active' && p.archived_at === null)
  const version = positive(p.version)
  try {
    const metadata = captureProjectUpdate({
      expected_version: version,
      name: p.name,
      description: p.description,
    })
    requireValue(p.normalized_name === metadata.name!.toLowerCase())
  } catch {
    throw new AccountFailure('invalid-response')
  }
  if (p.current_sprint_id !== null) projectID(p.current_sprint_id)
  requireValue(parseSystemInstant(p.created_at) <= parseSystemInstant(p.updated_at))
  return Object.freeze({
    state: 'committed',
    receipt: Object.freeze({ project_id: target, version }),
  })
}

export function captureProjectSchedulerID(value: unknown): string {
  try {
    return projectID(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function createProjectSchedulerAPI(fetcher?: Fetch): ProjectSchedulerAPI {
  const request = accountTransport(fetcher)
  return {
    get(project, signal) {
      const target = captureProjectSchedulerID(project)
      return request(
        'getProjectScheduler',
        (value) => parseProjectSchedulerConfiguration(value, target),
        { target, signal },
      )
    },
    update(project, input, write) {
      const target = captureProjectSchedulerID(project),
        captured = captureProjectSchedulerUpdate(input)
      return request(
        'updateProjectScheduler',
        (value) => parseProjectSchedulerReceipt(value, target, captured),
        {
          target,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: captured,
        },
      )
    },
    lookup(project, owner, write) {
      const target = captureProjectSchedulerID(project),
        caller = captureProjectSchedulerID(owner)
      return request(
        'lookupProjectScheduler',
        (value) => parseProjectSchedulerLookup(value, target, caller),
        {
          target,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'update' },
        },
      )
    },
  }
}
