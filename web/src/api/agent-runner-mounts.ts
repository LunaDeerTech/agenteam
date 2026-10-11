import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { workID, workText, workVersion, type PageQuery } from './work-review'

export type RunnerMountDeclaration = Readonly<{ runner_id: string; workspace: string }>
export type RunnerMountBinding = RunnerMountDeclaration &
  Readonly<{ mount_id: string; workspace_id: string }>
export type AvailableRunner = Readonly<{
  id: string
  name: string
  description: string
  version: string
  status: 'online' | 'offline' | 'incompatible'
}>
export type AvailableRunnerPage = Readonly<{
  items: readonly AvailableRunner[]
  next_cursor?: string
}>
export type AgentRunnerMounts = Readonly<{
  project_id: string
  agent_id: string
  agent_version: string
  items: readonly (RunnerMountDeclaration & Readonly<{ mount_id: string; name: string }>)[]
}>
export interface AgentRunnerMountsAPI {
  list(project: string, query: PageQuery, signal: AbortSignal): Promise<AvailableRunnerPage>
  get(project: string, agent: string, signal: AbortSignal): Promise<AgentRunnerMounts>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
// This is a logical single directory name, never a host path or Workspace ID.
export function workspaceName(value: unknown): string {
  requireValue(typeof value === 'string' && /^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$/.test(value))
  requireValue(
    !value.endsWith('.') && !/^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$/i.test(value.split('.')[0]!),
  )
  return value
}
function safeText(value: unknown, max: number, required = false): string {
  const text = workText(value, max)
  requireValue((!required || text.length > 0) && !/[\u0000-\u001f\u007f-\u009f]/u.test(text))
  return text
}
export function runnerMountDeclarations(value: unknown): readonly RunnerMountDeclaration[] {
  requireValue(Array.isArray(value) && value.length <= 128)
  const pairs = new Set<string>()
  return Object.freeze(
    Array.from(value, (item) => {
      const v = shape(item, ['runner_id', 'workspace'])
      const result = Object.freeze({
        runner_id: workID(v.runner_id),
        workspace: workspaceName(v.workspace),
      })
      const key = `${result.runner_id}/${result.workspace}`
      requireValue(!pairs.has(key))
      pairs.add(key)
      return result
    }),
  )
}
export function captureRunnerMountDeclarations(value: unknown) {
  return input(() => runnerMountDeclarations(value))
}
// A historical receipt must match the original declaration in order and its
// generated IDs must match the receipt Agent. It is not a current read.
export function runnerMountReceipt(
  value: unknown,
  original: readonly RunnerMountDeclaration[],
  mountIDs: readonly string[],
): readonly RunnerMountBinding[] {
  requireValue(Array.isArray(value) && value.length === original.length)
  const workspaces = new Set<string>(),
    mounts = new Set<string>()
  const items = value.map((item, n) => {
    const v = shape(item, ['mount_id', 'workspace_id', 'runner_id', 'workspace'])
    const result = Object.freeze({
      mount_id: workID(v.mount_id),
      workspace_id: workID(v.workspace_id),
      runner_id: workID(v.runner_id),
      workspace: workspaceName(v.workspace),
    })
    requireValue(
      result.runner_id === original[n]!.runner_id && result.workspace === original[n]!.workspace,
    )
    requireValue(!mounts.has(result.mount_id) && !workspaces.has(result.workspace_id))
    mounts.add(result.mount_id)
    workspaces.add(result.workspace_id)
    return result
  })
  requireValue(mounts.size === mountIDs.length && mountIDs.every((id) => mounts.has(id)))
  return Object.freeze(items)
}
export function matchingRunnerMounts(
  config: Readonly<{
    id: string
    project_id: string
    version: string
    allowed_mount_ids: readonly string[]
  }>,
  mounts: AgentRunnerMounts,
): boolean {
  return (
    config.project_id === mounts.project_id &&
    config.id === mounts.agent_id &&
    config.version === mounts.agent_version &&
    config.allowed_mount_ids.length === mounts.items.length &&
    config.allowed_mount_ids.every((id, n) => id === mounts.items[n]!.mount_id)
  )
}
export function createAgentRunnerMountsAPI(fetcher?: Fetch): AgentRunnerMountsAPI {
  const call = accountTransport(fetcher)
  return {
    list(project, query, signal) {
      const q = input(() => {
        workID(project)
        const v = shape(query, [], ['limit', 'cursor']),
          limit = Object.hasOwn(v, 'limit') ? v.limit : 50
        requireValue(Number.isInteger(limit) && Number(limit) >= 1 && Number(limit) <= 50)
        const cursor = Object.hasOwn(v, 'cursor') ? safeText(v.cursor, 8192, true) : undefined
        return { limit: String(limit), ...(cursor === undefined ? {} : { cursor }) }
      })
      return call(
        'availableAgentRunners',
        (value) => {
          const v = shape(value, ['items'], ['next_cursor'])
          requireValue(Array.isArray(v.items) && v.items.length <= Number(q.limit))
          const items = v.items.map((item): AvailableRunner => {
            const r = shape(item, ['id', 'name', 'description', 'version', 'status'])
            requireValue(
              r.status === 'online' || r.status === 'offline' || r.status === 'incompatible',
            )
            return Object.freeze({
              id: workID(r.id),
              name: safeText(r.name, 128, true),
              description: safeText(r.description, 4096),
              version: workVersion(r.version),
              status: r.status,
            })
          })
          for (let n = 1; n < items.length; n++) requireValue(items[n - 1]!.id < items[n]!.id)
          const next = Object.hasOwn(v, 'next_cursor')
            ? safeText(v.next_cursor, 8192, true)
            : undefined
          requireValue(
            next === undefined || (items.length === Number(q.limit) && next !== q.cursor),
          )
          return Object.freeze({
            items: Object.freeze(items),
            ...(next === undefined ? {} : { next_cursor: next }),
          })
        },
        { signal, projectID: project, workQuery: q },
      )
    },
    get(project, agent, signal) {
      input(() => {
        workID(project)
        workID(agent)
      })
      return call(
        'agentRunnerMounts',
        (value) => {
          const v = shape(value, ['project_id', 'agent_id', 'agent_version', 'items'])
          requireValue(workID(v.project_id) === project && workID(v.agent_id) === agent)
          requireValue(Array.isArray(v.items) && v.items.length <= 128)
          const pairs = new Set<string>()
          const items = v.items.map((item) => {
            const r = shape(item, ['mount_id', 'runner_id', 'workspace', 'name'])
            const result = Object.freeze({
              mount_id: workID(r.mount_id),
              runner_id: workID(r.runner_id),
              workspace: workspaceName(r.workspace),
              name: safeText(r.name, 128, true),
            })
            const key = `${result.runner_id}/${result.workspace}`
            requireValue(!pairs.has(key))
            pairs.add(key)
            return result
          })
          for (let n = 1; n < items.length; n++)
            requireValue(items[n - 1]!.mount_id < items[n]!.mount_id)
          return Object.freeze({
            project_id: project,
            agent_id: agent,
            agent_version: workVersion(v.agent_version),
            items: Object.freeze(items),
          })
        },
        { signal, projectID: project, target: agent },
      )
    },
  }
}
