import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { workID, workText, workVersion, type Milestone } from './work-review'
import { parseSystemInstant } from './system-account'

export type MilestoneCreateDraft = Readonly<{ title: string; description?: string }>
export type MilestoneCreateInput = Readonly<{
  request: MilestoneCreateDraft & Readonly<{ milestone_id: string }>
}>
export type MilestoneCreateReceipt = Readonly<{
  command: 'work.milestone.create'
  changed: true
  milestone: Milestone
  sprint: null
  event_id: string
}>
export type MilestoneCreateLookup =
  | Readonly<{ state: 'committed'; result: MilestoneCreateReceipt }>
  | Readonly<{ state: 'in_progress' | 'not_observed'; result: null }>
export type MilestoneUpdateInput = Readonly<{
  expected_version: string
  request: Readonly<{ title?: string; description?: string }>
}>
export type MilestoneUpdateReceipt = Readonly<{
  command: 'work.milestone.update'
  changed: boolean
  milestone: Milestone
  sprint: null
  event_id: string | null
}>
export type MilestoneUpdateLookup =
  | Readonly<{ state: 'committed'; result: MilestoneUpdateReceipt }>
  | Readonly<{ state: 'in_progress' | 'not_observed'; result: null }>
export interface WorkMilestonePlanningAPI {
  update(
    project: string,
    target: string,
    input: MilestoneUpdateInput,
    write: WriteOptions,
  ): Promise<MilestoneUpdateReceipt>
  lookupUpdate(
    project: string,
    target: string,
    input: MilestoneUpdateInput,
    write: WriteOptions,
  ): Promise<MilestoneUpdateLookup>
  create(
    project: string,
    input: MilestoneCreateInput,
    write: WriteOptions,
  ): Promise<MilestoneCreateReceipt>
  lookup(
    project: string,
    input: MilestoneCreateInput,
    write: WriteOptions,
  ): Promise<MilestoneCreateLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(fn: () => T): T {
  try {
    return fn()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureMilestoneCreateDraft(value: unknown): MilestoneCreateDraft {
  return input(() => {
    const v = shape(value, ['title'], ['description'])
    return Object.freeze({
      title: workText(v.title, 1024, true),
      ...(Object.hasOwn(v, 'description') ? { description: workText(v.description, 32768) } : {}),
    })
  })
}
export function captureMilestoneCreateInput(value: unknown): MilestoneCreateInput {
  return input(() => {
    const v = shape(value, ['request'])
    const r = shape(v.request, ['milestone_id', 'title'], ['description'])
    const { milestone_id, ...draft } = r
    return Object.freeze({
      request: Object.freeze({
        milestone_id: workID(milestone_id),
        ...captureMilestoneCreateDraft(draft),
      }),
    })
  })
}
function createReceipt(
  value: unknown,
  project: string,
  original: MilestoneCreateInput,
): MilestoneCreateReceipt {
  const v = shape(value, ['command', 'changed', 'milestone', 'sprint', 'event_id'])
  const m = shape(v.milestone, [
    'id',
    'project_id',
    'title',
    'description',
    'manual_rank',
    'version',
    'created_at',
    'updated_at',
  ])
  const created = parseSystemInstant(m.created_at),
    updated = parseSystemInstant(m.updated_at)
  const rank = string(m.manual_rank, 32, 32),
    r = original.request
  requireValue(v.command === 'work.milestone.create' && v.changed === true && v.sprint === null)
  requireValue(workID(m.id) === r.milestone_id && workID(m.project_id) === project)
  requireValue(
    m.title === r.title &&
      m.description === (r.description ?? '') &&
      workVersion(m.version) === '1' &&
      created === updated,
  )
  requireValue(/^[0-9a-f]{32}$/.test(rank) && rank !== '0'.repeat(32) && rank !== 'f'.repeat(32))
  return Object.freeze({
    command: 'work.milestone.create',
    changed: true,
    sprint: null,
    event_id: workID(v.event_id),
    milestone: Object.freeze({
      id: r.milestone_id,
      project_id: project,
      title: r.title,
      description: r.description ?? '',
      manual_rank: rank,
      version: '1',
      created_at: created,
      updated_at: updated,
    }),
  })
}
export function captureMilestoneUpdateInput(value: unknown): MilestoneUpdateInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, [], ['title', 'description'])
    requireValue(Object.keys(r).length > 0)
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        ...(Object.hasOwn(r, 'title') ? { title: workText(r.title, 1024, true) } : {}),
        ...(Object.hasOwn(r, 'description') ? { description: workText(r.description, 32768) } : {}),
      }),
    })
  })
}
function updateReceipt(
  value: unknown,
  project: string,
  target: string,
  original: MilestoneUpdateInput,
): MilestoneUpdateReceipt {
  const v = shape(value, ['command', 'changed', 'milestone', 'sprint', 'event_id'])
  const m = shape(v.milestone, [
    'id',
    'project_id',
    'title',
    'description',
    'manual_rank',
    'version',
    'created_at',
    'updated_at',
  ])
  const created = parseSystemInstant(m.created_at),
    updated = parseSystemInstant(m.updated_at)
  const rank = string(m.manual_rank, 32, 32),
    version = workVersion(m.version)
  const title = workText(m.title, 1024, true),
    description = workText(m.description, 32768)
  requireValue(v.command === 'work.milestone.update' && v.sprint === null)
  requireValue(v.changed === true || v.changed === false)
  requireValue(workID(m.id) === target && workID(m.project_id) === project && created <= updated)
  requireValue(/^[0-9a-f]{32}$/.test(rank) && rank !== '0'.repeat(32) && rank !== 'f'.repeat(32))
  requireValue(BigInt(version) === BigInt(original.expected_version) + (v.changed ? 1n : 0n))
  if (Object.hasOwn(original.request, 'title')) requireValue(title === original.request.title)
  if (Object.hasOwn(original.request, 'description'))
    requireValue(description === original.request.description)
  if (!v.changed) requireValue(v.event_id === null)
  return Object.freeze({
    command: 'work.milestone.update',
    changed: v.changed,
    sprint: null,
    event_id: v.changed ? workID(v.event_id) : null,
    milestone: Object.freeze({
      id: target,
      project_id: project,
      title,
      description,
      manual_rank: rank,
      version,
      created_at: created,
      updated_at: updated,
    }),
  })
}
export function createWorkMilestonePlanningAPI(fetcher?: Fetch): WorkMilestonePlanningAPI {
  const call = accountTransport(fetcher)
  return {
    update(project, target, value, write) {
      const original = captureMilestoneUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(target) }))
      return call('milestoneUpdate', (v) => updateReceipt(v, project, target, original), {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookupUpdate(project, target, value, write) {
      const original = captureMilestoneUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(target) }))
      return call(
        'milestoneUpdateLookup',
        (value): MilestoneUpdateLookup => {
          const v = shape(value, ['state', 'result'])
          if (v.state === 'committed')
            return Object.freeze({
              state: 'committed',
              result: updateReceipt(v.result, project, target, original),
            })
          requireValue(
            (v.state === 'in_progress' || v.state === 'not_observed') && v.result === null,
          )
          return Object.freeze({ state: v.state as 'in_progress' | 'not_observed', result: null })
        },
        {
          projectID: scope.projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.milestone.update', target_id: scope.target, ...original },
        },
      )
    },
    create(project, value, write) {
      const original = captureMilestoneCreateInput(value)
      const projectID = input(() => workID(project))
      return call('milestoneCreate', (value) => createReceipt(value, projectID, original), {
        projectID,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookup(project, value, write) {
      const original = captureMilestoneCreateInput(value)
      const projectID = input(() => workID(project))
      return call(
        'milestoneCreateLookup',
        (value): MilestoneCreateLookup => {
          const v = shape(value, ['state', 'result'])
          if (v.state === 'committed')
            return Object.freeze({
              state: 'committed',
              result: createReceipt(v.result, projectID, original),
            })
          requireValue(
            (v.state === 'in_progress' || v.state === 'not_observed') && v.result === null,
          )
          return Object.freeze({ state: v.state as 'in_progress' | 'not_observed', result: null })
        },
        {
          projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.milestone.create', ...original },
        },
      )
    },
  }
}
