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
export interface WorkMilestonePlanningAPI {
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
export function createWorkMilestonePlanningAPI(fetcher?: Fetch): WorkMilestonePlanningAPI {
  const call = accountTransport(fetcher)
  return {
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
