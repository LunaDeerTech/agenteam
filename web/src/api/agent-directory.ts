import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import { workID, workText, workVersion, type Page, type PageQuery } from './work-review'
export type DirectoryAgent = Readonly<{
  id: string
  project_id: string
  name: string
  display_name: string | null
  tag_color: string | null
  description: string
  version: string
  created_at: string
  updated_at: string
}>
export interface AgentDirectoryAPI {
  list(project: string, query: PageQuery, signal: AbortSignal): Promise<Page<DirectoryAgent>>
  get(project: string, target: string, signal: AbortSignal): Promise<DirectoryAgent>
}
export const agentLabel = (agent: DirectoryAgent) => agent.display_name ?? agent.name
function requireValue(ok: unknown): asserts ok {
  if (!ok) throw new AccountFailure('invalid-response')
}
function parse(value: unknown, project: string, target?: string): DirectoryAgent {
  const v = shape(value, [
    'id',
    'project_id',
    'name',
    'display_name',
    'tag_color',
    'description',
    'version',
    'created_at',
    'updated_at',
  ])
  const result = {
    id: workID(v.id),
    project_id: workID(v.project_id),
    name: string(v.name, 3, 32),
    display_name: v.display_name === null ? null : workText(v.display_name, 1024, true),
    tag_color: v.tag_color === null ? null : string(v.tag_color, 7, 7),
    description: workText(v.description, 8192),
    version: workVersion(v.version),
    created_at: parseSystemInstant(v.created_at),
    updated_at: parseSystemInstant(v.updated_at),
  }
  requireValue(
    result.project_id === project &&
      (!target || result.id === target) &&
      /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])$/.test(result.name) &&
      result.created_at <= result.updated_at &&
      (result.tag_color === null || /^#[0-9a-f]{6}$/.test(result.tag_color)),
  )
  return Object.freeze(result)
}
export function createAgentDirectoryAPI(fetcher?: Fetch): AgentDirectoryAPI {
  const call = accountTransport(fetcher)
  function input<T>(f: () => T): T {
    try {
      return f()
    } catch {
      throw new AccountFailure('invalid-input')
    }
  }
  return {
    list(project, q, signal) {
      const query = input(() => {
        workID(project)
        const v = shape(q, [], ['limit', 'cursor']),
          limit = v.limit ?? 50
        requireValue(
          typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 200,
        )
        return {
          limit: String(limit),
          ...(Object.hasOwn(v, 'cursor') ? { cursor: workText(v.cursor, 8192, false, true) } : {}),
        }
      })
      return call(
        'directoryAgents',
        (value) => {
          const v = shape(value, ['items'], ['next_cursor'])
          requireValue(Array.isArray(v.items) && v.items.length <= Number(query.limit))
          const items = v.items.map((item) => parse(item, project))
          const seen = new Set<string>()
          for (let n = 0; n < items.length; n++) {
            const row = items[n]!
            requireValue(!seen.has(row.id))
            seen.add(row.id)
            if (n) {
              const old = items[n - 1]!
              requireValue(
                old.created_at > row.created_at ||
                  (old.created_at === row.created_at && old.id > row.id),
              )
            }
          }
          const next = Object.hasOwn(v, 'next_cursor')
            ? workText(v.next_cursor, 8192, false, true)
            : undefined
          requireValue(!next || (items.length === Number(query.limit) && next !== query.cursor))
          return Object.freeze({
            items: Object.freeze(items),
            ...(next ? { next_cursor: next } : {}),
          })
        },
        { projectID: project, signal, workQuery: query },
      )
    },
    get(project, target, signal) {
      input(() => {
        workID(project)
        workID(target)
      })
      return call('directoryAgent', (v) => parse(v, project, target), {
        projectID: project,
        target,
        signal,
      })
    },
  }
}
