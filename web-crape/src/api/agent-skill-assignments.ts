import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'
import { workID, workVersion, workText } from './work-review'

export type AgentSkillAssignment = Readonly<{
  id: string
  project_id: string
  agent_id: string
  skill_id: string
  enabled: boolean
  assignment_sequence: string
  created_at: string
}>
export type AgentSkillPage = Readonly<{
  project_id: string
  agent_id: string
  agent_version: string
  assignment_sequence: string
  items: readonly AgentSkillAssignment[]
  next_cursor?: string
}>
export type AgentSkillQuery = Readonly<{ limit?: number; cursor?: string }>
export type AgentSkillCommand =
  | Readonly<{
      kind: 'assign'
      input: Readonly<{
        expected_version: string
        request: Readonly<{ skill_id: string; enabled: boolean }>
      }>
    }>
  | Readonly<{
      kind: 'remove'
      input: Readonly<{ expected_version: string; request: Readonly<{ assignment_id: string }> }>
    }>
export type AgentSkillMutation = Readonly<{
  project_id: string
  agent_id: string
  agent_version: string
  assignment_sequence: string
  change_id: string
  changed: boolean
  assignment: AgentSkillAssignment | null
  runtime: Readonly<{ change_id: string; queued: boolean; execution_id: string | null }> | null
}>
export type AgentSkillLookup = Readonly<
  { state: 'not_observed'; receipt: null } | { state: 'committed'; receipt: AgentSkillMutation }
>
export interface AgentSkillAssignmentsAPI {
  list(
    project: string,
    agent: string,
    query: AgentSkillQuery,
    signal: AbortSignal,
  ): Promise<AgentSkillPage>
  execute(
    project: string,
    agent: string,
    command: AgentSkillCommand,
    write: WriteOptions,
  ): Promise<AgentSkillMutation>
  lookup(
    project: string,
    agent: string,
    command: AgentSkillCommand,
    write: WriteOptions,
  ): Promise<AgentSkillLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(capture: () => T): T {
  try {
    return capture()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function bool(value: unknown): boolean {
  requireValue(typeof value === 'boolean')
  return value
}
function freeze<T>(value: T): T {
  if (value && typeof value === 'object') {
    Object.values(value).forEach(freeze)
    Object.freeze(value)
  }
  return value
}
export function captureAgentSkillCommand(value: unknown): AgentSkillCommand {
  return input(() => {
    const v = shape(value, ['kind', 'input']),
      i = shape(v.input, ['expected_version', 'request'])
    const expected_version = workVersion(i.expected_version)
    if (v.kind === 'assign') {
      const r = shape(i.request, ['skill_id', 'enabled'])
      return freeze({
        kind: 'assign' as const,
        input: {
          expected_version,
          request: { skill_id: workID(r.skill_id), enabled: bool(r.enabled) },
        },
      })
    }
    requireValue(v.kind === 'remove')
    const r = shape(i.request, ['assignment_id'])
    return freeze({
      kind: 'remove' as const,
      input: { expected_version, request: { assignment_id: workID(r.assignment_id) } },
    })
  })
}
function query(value: AgentSkillQuery) {
  return input(() => {
    const q = shape(value, [], ['limit', 'cursor']),
      limit = q.limit ?? 25
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100)
    const cursor = Object.hasOwn(q, 'cursor') ? workText(q.cursor, 8192) : undefined
    requireValue(cursor === undefined || cursor.length > 0)
    return Object.freeze({ limit: String(limit), ...(cursor === undefined ? {} : { cursor }) })
  })
}
function assignment(value: unknown, project: string, agent: string): AgentSkillAssignment {
  const v = shape(value, [
    'id',
    'project_id',
    'agent_id',
    'skill_id',
    'enabled',
    'assignment_sequence',
    'created_at',
  ])
  requireValue(workID(v.project_id) === project && workID(v.agent_id) === agent)
  return freeze({
    id: workID(v.id),
    project_id: project,
    agent_id: agent,
    skill_id: workID(v.skill_id),
    enabled: bool(v.enabled),
    assignment_sequence: workVersion(v.assignment_sequence),
    created_at: parseSystemInstant(v.created_at),
  })
}
function mutation(
  value: unknown,
  project: string,
  agent: string,
  command: AgentSkillCommand,
): AgentSkillMutation {
  const v = shape(value, [
    'project_id',
    'agent_id',
    'agent_version',
    'assignment_sequence',
    'change_id',
    'changed',
    'assignment',
    'runtime',
  ])
  requireValue(workID(v.project_id) === project && workID(v.agent_id) === agent)
  const changed = bool(v.changed),
    version = workVersion(v.agent_version),
    sequence = workVersion(v.assignment_sequence),
    change = workID(v.change_id)
  requireValue(BigInt(version) === BigInt(command.input.expected_version) + (changed ? 1n : 0n))
  const item = v.assignment === null ? null : assignment(v.assignment, project, agent)
  if (command.kind === 'assign') {
    requireValue(
      item &&
        item.skill_id === command.input.request.skill_id &&
        item.enabled === command.input.request.enabled &&
        BigInt(item.assignment_sequence) <= BigInt(sequence),
    )
    if (changed) requireValue(item.assignment_sequence === sequence)
  } else requireValue(item === null)
  let runtime: AgentSkillMutation['runtime'] = null
  if (command.kind === 'assign' && command.input.request.enabled && changed) {
    const r = shape(v.runtime, ['change_id', 'queued', 'execution_id'])
    const queued = bool(r.queued),
      execution = r.execution_id === null ? null : workID(r.execution_id)
    requireValue(workID(r.change_id) === change && queued === (execution !== null))
    runtime = freeze({ change_id: change, queued, execution_id: execution })
  } else requireValue(v.runtime === null)
  return freeze({
    project_id: project,
    agent_id: agent,
    agent_version: version,
    assignment_sequence: sequence,
    change_id: change,
    changed,
    assignment: item,
    runtime,
  })
}
export function createAgentSkillAssignmentsAPI(fetcher?: Fetch): AgentSkillAssignmentsAPI {
  const call = accountTransport(fetcher)
  const ids = (project: string, agent: string) =>
    input(() => ({ projectID: workID(project), target: workID(agent) }))
  return {
    list(project, agent, value, signal) {
      const scope = ids(project, agent),
        q = query(value)
      return call(
        'agentSkillAssignments',
        (value) => {
          const v = shape(
            value,
            ['project_id', 'agent_id', 'agent_version', 'assignment_sequence', 'items'],
            ['next_cursor'],
          )
          requireValue(
            workID(v.project_id) === project &&
              workID(v.agent_id) === agent &&
              Array.isArray(v.items) &&
              v.items.length <= Number(q.limit),
          )
          const sequence = workVersion(v.assignment_sequence),
            items = v.items.map((v) => assignment(v, project, agent))
          requireValue(
            new Set(items.map((item) => item.id)).size === items.length &&
              items.every(
                (item, index) =>
                  BigInt(item.assignment_sequence) <= BigInt(sequence) &&
                  (index === 0 || items[index - 1]!.skill_id < item.skill_id),
              ),
          )
          const next = Object.hasOwn(v, 'next_cursor') ? workText(v.next_cursor, 8192) : undefined
          requireValue(
            next === undefined ||
              (next.length > 0 && items.length === Number(q.limit) && next !== q.cursor),
          )
          return freeze({
            project_id: project,
            agent_id: agent,
            agent_version: workVersion(v.agent_version),
            assignment_sequence: sequence,
            items,
            ...(next === undefined ? {} : { next_cursor: next }),
          })
        },
        { ...scope, signal, workQuery: q },
      )
    },
    execute(project, agent, value, write) {
      const scope = ids(project, agent),
        command = captureAgentSkillCommand(value)
      return call(
        command.kind === 'assign' ? 'agentSkillAssign' : 'agentSkillRemove',
        (value) => mutation(value, project, agent, command),
        {
          ...scope,
          signal: write.signal ?? new AbortController().signal,
          body: command.input,
          csrf: write.csrfToken,
          key: write.key,
        },
      )
    },
    lookup(project, agent, value, write) {
      const scope = ids(project, agent),
        command = captureAgentSkillCommand(value)
      return call(
        'agentSkillLookup',
        (value) => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'committed')
            return freeze({
              state: 'committed' as const,
              receipt: mutation(v.receipt, project, agent, command),
            })
          requireValue(v.state === 'not_observed' && v.receipt === null)
          return freeze({ state: 'not_observed' as const, receipt: null })
        },
        {
          ...scope,
          signal: write.signal ?? new AbortController().signal,
          body: { command: `skill.${command.kind}`, ...command.input },
          csrf: write.csrfToken,
          key: write.key,
        },
      )
    },
  }
}
