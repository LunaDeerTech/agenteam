import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'

export type SkillMetadata = Readonly<{
  id: string
  project_id: string
  name: string
  normalized_name: string
  description: string
  protected: boolean
  current_revision: string
  version: string
}>
export type SkillDirectory = Readonly<{ items: readonly SkillMetadata[]; next_cursor?: string }>
export type SkillQuery = Readonly<{ limit?: number; cursor?: string }>
export interface SkillOwnerAPI {
  list(projectID: string, query: SkillQuery, signal: AbortSignal): Promise<SkillDirectory>
  get(projectID: string, skillID: string, signal: AbortSignal): Promise<SkillMetadata>
}

const encoder = new TextEncoder()
function requireValue(condition: boolean): asserts condition {
  if (!condition) throw new AccountFailure('invalid-response')
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
export function captureSkillID(value: unknown): string {
  try {
    return id(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function cursor(value: unknown): string {
  const result = string(value, 1, 8192)
  requireValue(
    encoder.encode(result).byteLength <= 8192 &&
      !/[\u0000-\u001f\u007f-\u009f]/u.test(result) &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
export function captureSkillQuery(value: unknown): Readonly<{ limit: number; cursor?: string }> {
  try {
    const q = shape(value, [], ['limit', 'cursor'])
    const limit = Object.hasOwn(q, 'limit') ? q.limit : 25
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100)
    return Object.freeze({
      limit,
      ...(Object.hasOwn(q, 'cursor') ? { cursor: cursor(q.cursor) } : {}),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function text(value: unknown, maximum: number, multiline = false): string {
  const result = string(value, 1, maximum)
  requireValue(
    encoder.encode(result).byteLength <= maximum &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result) &&
      !/^\p{White_Space}*$/u.test(result) &&
      !(
        multiline ? /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/u : /[\u0000-\u001f\u007f-\u009f]/u
      ).test(result),
  )
  return result
}
function version(value: unknown): string {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && !/[^0-9]/.test(result))
  requireValue(BigInt(result) <= 9223372036854775807n)
  return result
}
export function parseSkillMetadata(
  value: unknown,
  projectID: string,
  target?: string,
): SkillMetadata {
  const v = shape(value, [
    'id',
    'project_id',
    'name',
    'normalized_name',
    'description',
    'protected',
    'current_revision',
    'version',
  ])
  const skillID = id(v.id),
    name = text(v.name, 128)
  requireValue(
    id(v.project_id) === projectID &&
      (target === undefined || skillID === target) &&
      !/^\p{White_Space}|\p{White_Space}$/u.test(name) &&
      !/[/\\]/u.test(name) &&
      name !== '.' &&
      name !== '..' &&
      typeof v.protected === 'boolean',
  )
  // Full NFC/case-fold name equality is the service's domain validation.
  // These wire values are never rewritten or used as an authorization decision.
  return Object.freeze({
    id: skillID,
    project_id: projectID,
    name,
    normalized_name: text(v.normalized_name, 384),
    description: text(v.description, 8192, true),
    protected: v.protected as boolean,
    current_revision: version(v.current_revision),
    version: version(v.version),
  })
}
export function createSkillOwnerAPI(fetcher?: Fetch): SkillOwnerAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, query, signal) {
      const project = captureSkillID(projectID),
        q = captureSkillQuery(query)
      return request(
        'listProjectSkills',
        (value) => {
          const v = shape(value, ['items'], ['next_cursor'])
          requireValue(Array.isArray(v.items) && v.items.length <= q.limit)
          const items = v.items.map((item) => parseSkillMetadata(item, project))
          requireValue(items.every((item, index) => index === 0 || items[index - 1]!.id < item.id))
          const next = Object.hasOwn(v, 'next_cursor') ? cursor(v.next_cursor) : undefined
          requireValue(next === undefined || (items.length === q.limit && next !== q.cursor))
          return Object.freeze({
            items: Object.freeze(items),
            ...(next === undefined ? {} : { next_cursor: next }),
          })
        },
        { signal, projectID: project, skillQuery: q },
      )
    },
    async get(projectID, skillID, signal) {
      const project = captureSkillID(projectID),
        target = captureSkillID(skillID)
      return request('getProjectSkill', (value) => parseSkillMetadata(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
  }
}
