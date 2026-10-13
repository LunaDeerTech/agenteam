import type { TaskExecution } from '../api/task-executions'
export { task, session } from './task-human-blocker-fixture'
export const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const p = id(1), t = id(4), at = '2026-10-13T10:00:00.000001Z'
export const path = `/api/v1/projects/${p}/tasks/${t}/executions`
export const execution = (n = 100, patch: Partial<TaskExecution> = {}): TaskExecution => ({
  id: id(n), project_id: p, task_id: t, agent_id: id(7), purpose: 'task/work',
  status: 'created', version: '9007199254740993', created_at: at,
  started_at: null, completed_at: null, cancel_requested_at: null, snapshot_id: null, reason: null,
  ...patch,
})
export const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
  status, headers: { 'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json', 'X-Request-ID': id(999) },
})
export const problem = (code: string, status: number) => json({
  type: 'urn:agenteam:problem:test', title: 'Rejected', detail: '', instance: path,
  code, status, request_id: id(999), commit_state: 'not_started',
}, status)
