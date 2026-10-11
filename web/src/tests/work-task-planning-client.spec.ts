import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import {
  captureTaskCreateDraft,
  captureTaskCreateInput,
  createWorkTaskPlanningAPI,
  newTaskID,
  type TaskCreateInput,
} from '../api/work-task-planning'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1)
const instant = '2026-10-11T12:00:00.000000Z'
const write = { csrfToken: 'S'.repeat(43), key: 'original-task-create-1' }
const input = (): TaskCreateInput => ({
  request: {
    task_id: id(10),
    sprint_id: id(3),
    title: '  核对原始任务 🧩  ',
    description: '',
    type: 'task',
    priority: 'high',
    plan: '第一步\n保留原始计划  ',
  },
})
const receipt = () => {
  const { task_id, ...fields } = input().request
  return {
    task: {
      id: task_id,
      project_id: project,
      milestone_id: id(2),
      ...fields,
      manual_rank: '80000000000000000000000000000000',
      version: '1',
      created_at: instant,
      updated_at: instant,
      state: 'backlog',
      assignee_agent_id: null,
    },
    changed: true,
    task_event_id: id(40),
    event_ids: [id(50)],
  }
}
const response = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })

describe('Human Task planning closed client', () => {
  it('creates the exact backlog intent and accepts only its immutable typed receipt', async () => {
    const { task_id, ...draft } = input().request
    const capturedDraft = captureTaskCreateDraft(draft)
    expect(capturedDraft).toEqual(draft)
    expect(Object.isFrozen(capturedDraft)).toBe(true)
    const original = captureTaskCreateInput({ request: { task_id, ...capturedDraft } })
    const fetcher = vi.fn<Fetch>(async () => response(receipt()))
    const result = await createWorkTaskPlanningAPI(fetcher).create(project, original, write)
    expect(result).toEqual(receipt())
    expect(Object.isFrozen(original.request)).toBe(true)
    expect(Object.isFrozen(result)).toBe(true)
    expect(Object.isFrozen(result.task)).toBe(true)
    expect(Object.isFrozen(result.event_ids)).toBe(true)
    expect(fetcher).toHaveBeenCalledTimes(1)
    const [path, init] = fetcher.mock.calls[0]!
    expect(path).toBe(`/api/v1/projects/${project}/tasks`)
    expect(init).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
    })
    expect(init.headers).toMatchObject({
      'X-CSRF-Token': write.csrfToken,
      'Idempotency-Key': write.key,
    })
    expect(JSON.parse(init.body as string)).toEqual(input())
    for (const change of [
      { id: id(11) },
      { sprint_id: id(4) },
      { version: '2' },
      { title: 'changed title' },
      { state: 'todo', assignee_agent_id: id(20) },
    ]) {
      const api = createWorkTaskPlanningAPI(async () =>
        response({ ...receipt(), task: { ...receipt().task, ...change } }),
      )
      await expect(api.create(project, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })

  it('keeps an Unknown create intent and uses its full original request and key only for Lookup', async () => {
    const unknown = {
      type: 'urn:agenteam:problem:test',
      title: 'Unable to confirm',
      status: 503,
      detail: 'safe failure',
      instance: `/api/v1/projects/${project}/tasks`,
      code: 'COMMIT_UNKNOWN',
      request_id: id(90),
      commit_state: 'unknown',
    }
    const replies = [
      new Response(JSON.stringify(unknown), {
        status: 503,
        headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
      }),
      response({ status: 'not_observed', receipt: null }),
      response({ status: 'in_progress', receipt: null }),
      response({ status: 'committed', receipt: receipt() }),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createWorkTaskPlanningAPI(fetcher)
    const mutable = { request: { ...input().request } }
    const original = captureTaskCreateInput(mutable)
    mutable.request.title = 'later draft is not the original intent'
    await expect(api.create(project, original, write)).rejects.toMatchObject({
      kind: 'problem',
      problem: unknown,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
    for (const [n, status] of ['not_observed', 'in_progress', 'committed'].entries()) {
      expect(await api.lookup(project, original, write)).toEqual({
        status,
        receipt: status === 'committed' ? receipt() : null,
      })
      expect(fetcher).toHaveBeenCalledTimes(n + 2)
    }
    for (const [path, init] of fetcher.mock.calls.slice(1)) {
      expect(path).toBe(`/api/v1/projects/${project}/task-commands/lookup`)
      expect(init.method).toBe('POST')
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': write.key,
      })
      expect(JSON.parse(init.body as string)).toEqual({ command: 'work.task.create', ...input() })
    }
    const malformed = createWorkTaskPlanningAPI(async () =>
      response({ status: 'committed', receipt: null }),
    )
    await expect(malformed.lookup(project, original, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })

  it('creates UUIDv7 resource identities and rejects invalid drafts before sending a request', async () => {
    const clock = vi.spyOn(Date, 'now').mockReturnValue(0x019000000123)
    try {
      const first = newTaskID()
      const second = newTaskID()
      expect(first).toMatch(/^01900000-0123-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
      expect(second).toMatch(/^01900000-0123-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
      expect(second).not.toBe(first)
      expect(
        captureTaskCreateInput({ request: { ...input().request, task_id: first } }).request.task_id,
      ).toBe(first)
      clock.mockReturnValue(Number.NaN)
      expect(() => newTaskID()).toThrowError('invalid-input')
    } finally {
      clock.mockRestore()
    }
    const fetcher = vi.fn<Fetch>(async () => response(receipt()))
    const api = createWorkTaskPlanningAPI(fetcher)
    for (const request of [
      { ...input().request, task_id: '01900000-0000-4000-8000-000000000010' },
      { ...input().request, title: ' \t\u3000' },
      { ...input().request, plan: undefined },
      { ...input().request, assignee_agent_id: id(20) },
    ]) {
      await expect(
        Promise.resolve().then(() => api.create(project, { request } as never, write)),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetcher).not.toHaveBeenCalled()
  })
})
