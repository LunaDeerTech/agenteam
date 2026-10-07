import {
  AccountFailure,
  accountTransport,
  auditFilterFields,
  captureAuditWireQuery,
  shape,
  type AuditFilterField,
  type AuditWireQuery,
  type Fetch,
} from './client'
import { parseSystemInstant } from './system-account'
import {
  auditID,
  auditOutcomes,
  auditSummary,
  parseAuditActor,
  parseAuditAssociations,
  parseAuditMetadata,
  parseAuditResource,
  requireAudit,
  validateAuditRelations,
  type AuditActor,
  type AuditAssociations,
  type AuditOutcome,
  type AuditResource,
  type TypedAuditMetadata,
} from './system-audit-metadata'

export { auditFilterActions, auditFilterFields, auditFilterResourceKinds } from './client'
export type { AuditFilterField } from './client'
export type AuditQuery = AuditWireQuery
export type AuditQueryField = AuditFilterField | 'limit' | 'cursor'
export type SystemAuditRecord = TypedAuditMetadata &
  Readonly<{
    audit_id: string
    created_at: string
    scope: 'system'
    actor: AuditActor
    outcome: AuditOutcome
    resource: AuditResource
    associations: AuditAssociations
    summary: string
  }>
export type SystemAuditPage = Readonly<{
  items: readonly SystemAuditRecord[]
  next_cursor: string | null
}>
export interface SystemAuditAPI {
  list(query: AuditQuery, signal: AbortSignal): Promise<SystemAuditPage>
  get(id: string, signal: AbortSignal): Promise<SystemAuditRecord>
}
export class AuditQueryFailure extends AccountFailure {
  readonly fields: readonly AuditQueryField[]
  constructor(fields: readonly AuditQueryField[] = []) {
    super('invalid-input')
    this.fields = Object.freeze([...fields])
  }
}

export function captureAuditInstant(value: unknown): string {
  try {
    if (typeof value !== 'string') throw new Error()
    const match =
      /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,6}))?(Z|([+-])([01]\d|2[0-3]):([0-5]\d))$/.exec(
        value,
      )
    if (!match) throw new Error()
    const fraction = (match[7] ?? '').padEnd(6, '0')
    parseSystemInstant(
      `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${match[6]}.${fraction}Z`,
    )
    // Set the full year explicitly: Date.UTC treats 0000–0099 as 1900–1999.
    // Only whole seconds enter Date; the original six microsecond digits stay separate.
    const date = new Date(0)
    date.setUTCFullYear(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
    date.setUTCHours(Number(match[4]), Number(match[5]), Number(match[6]), 0)
    const minutes =
      match[8] === 'Z'
        ? 0
        : (Number(match[10]) * 60 + Number(match[11])) * (match[9] === '+' ? 1 : -1)
    date.setTime(date.getTime() - minutes * 60_000)
    const year = date.getUTCFullYear()
    if (year < 0 || year > 9999) throw new Error()
    return parseSystemInstant(`${date.toISOString().slice(0, 19)}.${fraction}Z`)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}

export function captureAuditQuery(value: unknown): AuditQuery {
  let source: Record<string, unknown>
  const keys = [...auditFilterFields, 'limit', 'cursor'] as const
  try {
    source = shape(value, [], keys)
  } catch {
    throw new AuditQueryFailure()
  }
  const captured: Partial<Record<AuditQueryField, string>> = {}
  for (const key of keys) {
    if (!Object.hasOwn(source, key)) continue
    try {
      const original = source[key]
      const normalized =
        (key === 'from' || key === 'to') && original !== ''
          ? captureAuditInstant(original)
          : original
      const field = captureAuditWireQuery({ [key]: normalized })
      if (Object.hasOwn(field, key)) captured[key] = field[key]
    } catch {
      throw new AuditQueryFailure([key])
    }
  }
  if (captured.from && captured.to && captured.from >= captured.to)
    throw new AuditQueryFailure(['from', 'to'])
  if (captured.actor_kind === 'service' && captured.actor_id)
    throw new AuditQueryFailure(['actor_kind', 'actor_id'])
  try {
    return captureAuditWireQuery(captured)
  } catch {
    throw new AuditQueryFailure()
  }
}

export function parseSystemAuditRecord(value: unknown): SystemAuditRecord {
  const raw = shape(value, [
    'audit_id',
    'created_at',
    'scope',
    'actor',
    'action',
    'outcome',
    'resource',
    'metadata',
    'associations',
    'summary',
  ])
  requireAudit(raw.scope === 'system')
  const audit_id = auditID(raw.audit_id),
    created_at = parseSystemInstant(raw.created_at)
  const actor = parseAuditActor(raw.actor),
    resource = parseAuditResource(raw.resource)
  const associations = parseAuditAssociations(raw.associations)
  requireAudit(
    typeof raw.outcome === 'string' && (auditOutcomes as readonly string[]).includes(raw.outcome),
  )
  const outcome = raw.outcome as AuditOutcome
  const typed = parseAuditMetadata(raw.action, raw.metadata)
  requireAudit(raw.summary === auditSummary(typed.action))
  validateAuditRelations(typed, { actor, resource, associations, outcome })
  return Object.freeze({
    audit_id,
    created_at,
    scope: 'system',
    actor,
    ...typed,
    outcome,
    resource,
    associations,
    summary: raw.summary as string,
  })
}
export function parseSystemAuditPage(value: unknown, limit = 50): SystemAuditPage {
  requireAudit(Number.isInteger(limit) && limit >= 1 && limit <= 200)
  const raw = shape(value, ['items', 'next_cursor'])
  requireAudit(Array.isArray(raw.items) && raw.items.length <= limit)
  const ids = new Set<string>()
  let previous: SystemAuditRecord | undefined
  const items = raw.items.map((candidate) => {
    const row = parseSystemAuditRecord(candidate)
    requireAudit(!ids.has(row.audit_id))
    requireAudit(
      !previous ||
        row.created_at < previous.created_at ||
        (row.created_at === previous.created_at && row.audit_id < previous.audit_id),
    )
    ids.add(row.audit_id)
    previous = row
    return row
  })
  const cursor = raw.next_cursor
  if (cursor !== null) {
    requireAudit(
      typeof cursor === 'string' && /^[A-Za-z0-9_.-]+$/.test(cursor) && cursor.length <= 8192,
    )
    requireAudit(items.length === limit)
  }
  return Object.freeze({ items: Object.freeze(items), next_cursor: cursor as string | null })
}
export function createSystemAuditAPI(fetcher?: Fetch): SystemAuditAPI {
  const request = accountTransport(fetcher)
  return {
    async list(query, signal) {
      const captured = captureAuditQuery(query)
      const limit = captured.limit === undefined ? 50 : Number(captured.limit)
      return request('listSystemAudit', (value) => parseSystemAuditPage(value, limit), {
        signal,
        audit: captured,
      })
    },
    async get(id, signal) {
      try {
        auditID(id)
      } catch {
        throw new AccountFailure('invalid-input')
      }
      const target = id
      return request(
        'getSystemAudit',
        (value) => {
          const record = parseSystemAuditRecord(value)
          requireAudit(record.audit_id === target)
          return record
        },
        { signal, target },
      )
    },
  }
}
