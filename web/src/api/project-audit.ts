import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import { captureAuditQuery, type AuditQuery } from './system-audit'
import { auditID, auditOutcomes, requireAudit, type AuditOutcome } from './system-audit-metadata'
import {
  parseProjectAuditActor,
  parseProjectAuditAssociations,
  parseProjectAuditMetadata,
  parseProjectAuditResource,
  projectAuditSummary,
  validateProjectAuditRelations,
  type ProjectAuditActor,
  type ProjectAuditAssociations,
  type ProjectAuditResource,
  type TypedProjectAuditMetadata,
} from './project-audit-metadata'

export {
  AuditQueryFailure,
  auditFilterActions,
  auditFilterFields,
  auditFilterResourceKinds,
  captureAuditInstant,
  captureAuditQuery,
} from './system-audit'
export type { AuditQuery, AuditQueryField, AuditFilterField } from './system-audit'
export type ProjectAuditRecord = TypedProjectAuditMetadata &
  Readonly<{
    audit_id: string
    created_at: string
    scope: 'project'
    project_id: string
    actor: ProjectAuditActor
    outcome: AuditOutcome
    resource: ProjectAuditResource
    associations: ProjectAuditAssociations
    summary: string
  }>
export type ProjectAuditPage = Readonly<{
  items: readonly ProjectAuditRecord[]
  next_cursor: string | null
}>
export interface ProjectAuditAPI {
  list(projectID: string, query: AuditQuery, signal: AbortSignal): Promise<ProjectAuditPage>
  get(projectID: string, auditID: string, signal: AbortSignal): Promise<ProjectAuditRecord>
}
export function captureProjectAuditID(value: unknown): string {
  try {
    return auditID(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function parseProjectAuditRecord(value: unknown, projectID: string): ProjectAuditRecord {
  auditID(projectID)
  const raw = shape(value, [
    'audit_id',
    'created_at',
    'scope',
    'project_id',
    'actor',
    'action',
    'outcome',
    'resource',
    'metadata',
    'associations',
    'summary',
  ])
  requireAudit(raw.scope === 'project' && raw.project_id === projectID)
  const audit_id = auditID(raw.audit_id),
    created_at = parseSystemInstant(raw.created_at)
  const actor = parseProjectAuditActor(raw.actor, projectID)
  const resource = parseProjectAuditResource(raw.resource)
  const associations = parseProjectAuditAssociations(raw.associations)
  requireAudit(
    typeof raw.outcome === 'string' && (auditOutcomes as readonly string[]).includes(raw.outcome),
  )
  const outcome = raw.outcome as AuditOutcome
  const typed = parseProjectAuditMetadata(raw.action, raw.metadata)
  requireAudit(raw.summary === projectAuditSummary(typed.action))
  validateProjectAuditRelations(typed, { projectID, actor, resource, associations, outcome })
  return Object.freeze({
    audit_id,
    created_at,
    scope: 'project',
    project_id: projectID,
    actor,
    ...typed,
    outcome,
    resource,
    associations,
    summary: raw.summary as string,
  })
}
function matchesFilters(row: ProjectAuditRecord, query: AuditQuery): boolean {
  if ((query.from && row.created_at < query.from) || (query.to && row.created_at >= query.to))
    return false
  const matches: Readonly<Partial<Record<keyof AuditQuery, string | undefined>>> = {
    actor_kind: row.actor.kind,
    actor_id: 'id' in row.actor ? row.actor.id : undefined,
    action: row.action,
    outcome: row.outcome,
    resource_kind: row.resource.kind,
    resource_id: 'id' in row.resource ? row.resource.id : undefined,
    tool_id: row.associations.tool_id,
    execution_id: row.associations.execution_id,
    operation_id: row.associations.operation_id,
    approval_id: row.associations.approval_id,
    runner_id: row.associations.runner_id,
  }
  for (const [key, value] of Object.entries(matches)) {
    const expected = query[key as keyof AuditQuery]
    if (expected && expected !== value) return false
  }
  return (
    !query.agent_id ||
    (row.actor.kind === 'agent_run' && row.actor.id === query.agent_id) ||
    (row.resource.kind === 'agent' && row.resource.id === query.agent_id)
  )
}
export function parseProjectAuditPage(
  value: unknown,
  projectID: string,
  query: AuditQuery = {},
): ProjectAuditPage {
  auditID(projectID)
  const captured = captureAuditQuery(query)
  const limit = captured.limit === undefined ? 50 : Number(captured.limit)
  const raw = shape(value, ['items', 'next_cursor'])
  requireAudit(Array.isArray(raw.items) && raw.items.length <= limit)
  const ids = new Set<string>()
  let previous: ProjectAuditRecord | undefined
  const items = raw.items.map((candidate) => {
    const row = parseProjectAuditRecord(candidate, projectID)
    requireAudit(!ids.has(row.audit_id) && matchesFilters(row, captured))
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
export function createProjectAuditAPI(fetcher?: Fetch): ProjectAuditAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, query, signal) {
      const project = captureProjectAuditID(projectID),
        captured = captureAuditQuery(query)
      return request(
        'listProjectAudit',
        (value) => parseProjectAuditPage(value, project, captured),
        {
          signal,
          projectID: project,
          audit: captured,
        },
      )
    },
    async get(projectID, auditID, signal) {
      const project = captureProjectAuditID(projectID),
        target = captureProjectAuditID(auditID)
      return request(
        'getProjectAudit',
        (value) => {
          const result = parseProjectAuditRecord(value, project)
          requireAudit(result.audit_id === target)
          return result
        },
        { signal, projectID: project, target },
      )
    },
  }
}
