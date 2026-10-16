import type { HumanBlockerAddInput, HumanBlockerResolveInput } from '../api/work-task-blockers'
import type { SessionView } from '../api/account'
import type { Task } from '../api/work-review'
export const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const p = id(1),
  t = id(4),
  at = '2026-10-13T10:00:00.000001Z'
export const addPath = `/api/v1/projects/${p}/tasks/${t}/blockers`
export const resolvePath = `${addPath}/resolve`
export const lookupPath = `/api/v1/projects/${p}/tasks/${t}/blocker-commands/lookup`
export const draft = () => ({
  expected_version: '9007199254740993',
  request: {
    type: 'waiting_for_human' as const,
    description: '  等待人工\n核对 🧩  ',
    metadata: {},
  },
})
export const addInput = (): HumanBlockerAddInput => ({
  ...draft(),
  request: { blocker_id: id(5), ...draft().request },
})
export const resolveInput = (): HumanBlockerResolveInput => ({
  expected_version: '9007199254740994',
  request: { blocker_id: id(5), resolution_comment: null },
})
export function task(version = draft().expected_version): Task {
  return {
    id: t,
    project_id: p,
    milestone_id: id(2),
    sprint_id: id(3),
    title: '等待人工核对',
    description: '原描述',
    plan: '原计划',
    type: 'task' as const,
    priority: 'high' as const,
    state: 'backlog' as const,
    assignee_agent_id: null,
    manual_rank: '8'.repeat(32),
    version,
    created_at: at,
    updated_at: at,
  }
}
export function receipt(input: HumanBlockerAddInput | HumanBlockerResolveInput = addInput()) {
  const adding = 'description' in input.request
  const actor = { type: 'human', user_id: id(8), source: 'task_domain' }
  return {
    task: task(String(BigInt(input.expected_version) + 1n)),
    blocker: {
      id: input.request.blocker_id,
      project_id: p,
      task_id: t,
      type: 'waiting_for_human',
      description: adding
        ? (input as HumanBlockerAddInput).request.description
        : draft().request.description,
      metadata: {},
      created_at: at,
      created_by: actor,
      resolved_at: adding ? null : at,
      resolved_by: adding ? null : actor,
      resolution_comment: adding
        ? null
        : (input as HumanBlockerResolveInput).request.resolution_comment,
    },
    task_event_id: id(10),
    event_ids: [id(11)],
  }
}
export const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
export const problem = (code = 'COMMIT_UNKNOWN', status = 409, commit_state = 'unknown') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status,
      detail: '',
      instance: addPath,
      code,
      request_id: id(99),
      commit_state,
    },
    status,
  )
export const session = (n = 9): SessionView => ({
  user: {
    id: id(8),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(n), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
