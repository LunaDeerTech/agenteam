import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

// Secret reads have no material field. Never use the ordinary Variable DTO here.
export type ProjectSecretMetadata = Readonly<{
  id: string
  project_id: string
  type: 'secret'
  name: string
  description: string
  version: string
  created_at: string
  updated_at: string
}>
export type ProjectSecretPage = Readonly<{
  items: readonly ProjectSecretMetadata[]
  next_cursor?: string
}>
export type ProjectSecretQuery = Readonly<{ limit?: number; cursor?: string }>
export interface ProjectSecretsAPI {
  list(projectID: string, query: ProjectSecretQuery, signal: AbortSignal): Promise<ProjectSecretPage>
  get(projectID: string, secretID: string, signal: AbortSignal): Promise<ProjectSecretMetadata>
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
function text(value: unknown, maximum: number): string {
  const result = string(value, 0, maximum)
  requireValue(
    encoder.encode(result).byteLength <= maximum &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
function cursor(value: unknown): string {
  const result = text(value, 8192)
  requireValue(result.length > 0 && !/[\u0000-\u001f\u007f-\u009f]/u.test(result))
  return result
}
export function captureProjectSecretID(value: unknown): string {
  try { return id(value) } catch { throw new AccountFailure('invalid-input') }
}
export function captureProjectSecretQuery(value: unknown): Readonly<{ limit: number; cursor?: string }> {
  try {
    const query = shape(value, [], ['limit', 'cursor'])
    const limit = Object.hasOwn(query, 'limit') ? query.limit : 50
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100)
    return Object.freeze({ limit, ...(Object.hasOwn(query, 'cursor') ? { cursor: cursor(query.cursor) } : {}) })
  } catch { throw new AccountFailure('invalid-input') }
}
export function compareProjectSecrets(a: ProjectSecretMetadata, b: ProjectSecretMetadata): number {
  return a.name < b.name ? -1 : a.name > b.name ? 1 : a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}
export function parseProjectSecretMetadata(value: unknown, projectID: string, target?: string): ProjectSecretMetadata {
  const v = shape(value, ['id', 'project_id', 'type', 'name', 'description', 'version', 'created_at', 'updated_at'])
  const secretID = id(v.id), project = id(v.project_id), name = string(v.name, 1, 128)
  const description = text(v.description, 4096), version = string(v.version, 1, 19)
  const created = parseSystemInstant(v.created_at), updated = parseSystemInstant(v.updated_at)
  requireValue(
    project === projectID && (target === undefined || secretID === target) && v.type === 'secret' &&
    /^[A-Za-z_]/.test(name) && !/[^A-Za-z0-9_]/.test(name) && !/^AGENTEAM(?:_|$)/i.test(name) &&
    !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/u.test(description) &&
    /^[1-9]/.test(version) && !/[^0-9]/.test(version) && BigInt(version) <= 9223372036854775807n &&
    created !== '0001-01-01T00:00:00.000000Z' && updated >= created,
  )
  return Object.freeze({ id: secretID, project_id: project, type: 'secret', name, description, version, created_at: created, updated_at: updated })
}
export function createProjectSecretsAPI(fetcher?: Fetch): ProjectSecretsAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, query, signal) {
      const project = captureProjectSecretID(projectID), q = captureProjectSecretQuery(query)
      return request('listProjectSecrets', value => {
        const page = shape(value, ['items'], ['next_cursor'])
        requireValue(Array.isArray(page.items) && page.items.length <= q.limit)
        const items = page.items.map(item => parseProjectSecretMetadata(item, project))
        requireValue(new Set(items.map(item => item.id)).size === items.length)
        requireValue(items.every((item, index) => index === 0 || compareProjectSecrets(items[index - 1]!, item) < 0))
        const next = Object.hasOwn(page, 'next_cursor') ? cursor(page.next_cursor) : undefined
        requireValue(next === undefined || (items.length === q.limit && next !== q.cursor))
        return Object.freeze({ items: Object.freeze(items), ...(next === undefined ? {} : { next_cursor: next }) })
      }, { signal, projectID: project, secretQuery: q })
    },
    async get(projectID, secretID, signal) {
      const project = captureProjectSecretID(projectID), target = captureProjectSecretID(secretID)
      return request('getProjectSecret', value => parseProjectSecretMetadata(value, project, target), { signal, projectID: project, target })
    },
  }
}
