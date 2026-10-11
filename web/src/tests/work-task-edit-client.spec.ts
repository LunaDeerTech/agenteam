import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import {
  captureTaskUpdateInput,
  createWorkTaskEditAPI,
  type TaskUpdateInput,
} from '../api/work-task-edit'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1)
const target = id(10)
const instant = '2026-10-11T12:00:00.000000Z'
const write = { csrfToken: 'S'.repeat(43), key: 'original-task-update-1' }
const input = (): TaskUpdateInput => ({
  expected_version: '9007199254740993',
  request: { title: '  核对原始标题 🧩  ', description: '' },
})
const receipt = (original = input(), changed = true) => ({
  task: {
    id: target,
    project_id: project,
    milestone_id: id(2),
    sprint_id: id(3),
    title: '原始标题',
    description: '原始描述',
    type: 'task',
    priority: 'high',
    plan: '原始计划',
    ...original.request,
    manual_rank: '80000000000000000000000000000000',
    version: String(BigInt(original.expected_version) + (changed ? 1n : 0n)),
    created_at: instant,
    updated_at: instant,
    state: 'backlog',
    assignee_agent_id: null,
  },
  changed,
  task_event_id: changed ? id(40) : null,
  event_ids: changed ? [id(50)] : [],
})
const response = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
const problem = (code: string, status: number, commitState = 'not_started') => ({
  type: 'urn:agenteam:problem:test',
  title: 'Unable to apply',
  status,
  detail: 'safe failure',
  instance: `/api/v1/projects/${project}/tasks/${target}`,
  code,
  request_id: id(90),
  commit_state: commitState,
})
const problemResponse = (body: ReturnType<typeof problem>) =>
  new Response(JSON.stringify(body), {
    status: body.status,
    headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
  })

describe('Human Task content editing closed client', () => {
  it('preserves a partial patch and explicit clearing in exact changed and no-op receipts', async () => {
    const original = captureTaskUpdateInput(input())
    const unchanged = captureTaskUpdateInput({
      ...original,
      expected_version: receipt(original).task.version,
    })
    const commands = [
      { original, changed: true, key: write.key },
      { original: unchanged, changed: false, key: 'explicit-no-op-update-2' },
    ]
    const replies = [receipt(original), receipt(unchanged, false)]
    const fetcher = vi.fn<Fetch>(async () => response(replies.shift()))
    const api = createWorkTaskEditAPI(fetcher)
    expect(Object.isFrozen(original)).toBe(true)
    expect(Object.isFrozen(original.request)).toBe(true)
    expect(Object.keys(original.request)).toEqual(['title', 'description'])
    for (const command of commands) {
      const result = await api.update(project, target, command.original, {
        ...write,
        key: command.key,
      })
      expect(result).toEqual(receipt(command.original, command.changed))
      expect(Object.isFrozen(result)).toBe(true)
      expect(Object.isFrozen(result.task)).toBe(true)
      expect(Object.isFrozen(result.event_ids)).toBe(true)
    }
    expect(fetcher).toHaveBeenCalledTimes(2)
    for (const [n, [path, init]] of fetcher.mock.calls.entries()) {
      expect(path).toBe(`/api/v1/projects/${project}/tasks/${target}`)
      expect(init).toMatchObject({
        method: 'PATCH',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': commands[n]!.key,
      })
      expect(JSON.parse(init.body as string)).toEqual(commands[n]!.original)
    }
    for (const invalid of [
      { ...receipt(), task: { ...receipt().task, version: input().expected_version } },
      { ...receipt(), task: { ...receipt().task, title: 'different patch' } },
      { ...receipt(), task: { ...receipt().task, project_id: id(99) } },
      { ...receipt(), task: { ...receipt().task, state: 'todo', assignee_agent_id: id(20) } },
      { ...receipt(), task_event_id: null },
      { ...receipt(input(), false), event_ids: [id(50)] },
    ]) {
      await expect(
        createWorkTaskEditAPI(async () => response(invalid)).update(
          project,
          target,
          original,
          write,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })

  it('retains the full original Unknown intent and key for explicit Lookup without resubmitting', async () => {
    const mutable = {
      expected_version: input().expected_version,
      request: {
        title: '  原始意图  ',
        description: '保留换行\n与原始内容',
        type: 'bug',
        priority: 'medium',
        plan: '',
      },
    }
    const original = captureTaskUpdateInput(mutable)
    mutable.expected_version = '42'
    mutable.request.title = 'later draft'
    const unknown = problem('COMMIT_UNKNOWN', 503, 'unknown')
    const replies = [
      problemResponse(unknown),
      response({ status: 'not_observed', receipt: null }),
      response({ status: 'in_progress', receipt: null }),
      response({ status: 'committed', receipt: receipt(original) }),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createWorkTaskEditAPI(fetcher)
    await expect(api.update(project, target, original, write)).rejects.toMatchObject({
      kind: 'problem',
      problem: unknown,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
    for (const [n, status] of ['not_observed', 'in_progress', 'committed'].entries()) {
      const result = await api.lookup(project, target, original, write)
      expect(result).toEqual({
        status,
        receipt: status === 'committed' ? receipt(original) : null,
      })
      expect(Object.isFrozen(result)).toBe(true)
      expect(fetcher).toHaveBeenCalledTimes(n + 2)
    }
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual(original)
    for (const [path, init] of fetcher.mock.calls.slice(1)) {
      expect(path).toBe(`/api/v1/projects/${project}/task-commands/lookup`)
      expect(init.method).toBe('POST')
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': write.key,
      })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.update',
        target_id: target,
        ...original,
      })
    }
    for (const body of [
      JSON.stringify({ status: 'committed', receipt: null }),
      JSON.stringify({ status: 'in_progress', receipt: receipt(original) }),
      '{"status":"committed","status":"not_observed","receipt":null}',
    ]) {
      const malformed = createWorkTaskEditAPI(
        async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(malformed.lookup(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })

  it('rejects invalid field presence before network and preserves version conflicts without retry', async () => {
    const conflict = {
      ...problem('TASK_VERSION_CONFLICT', 409),
      retry_hint: 'reread',
    }
    const fetcher = vi.fn<Fetch>(async () => problemResponse(conflict))
    const api = createWorkTaskEditAPI(fetcher)
    for (const value of [
      { ...input(), expected_version: 1 },
      { ...input(), request: {} },
      { ...input(), request: { description: null } },
      { ...input(), request: { plan: undefined } },
      { ...input(), request: { title: ' \t\u3000' } },
      { ...input(), request: { priority: 'urgent' } },
      { ...input(), request: { assignee_agent_id: id(20) } },
    ]) {
      await expect(
        Promise.resolve().then(() => api.update(project, target, value as never, write)),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetcher).not.toHaveBeenCalled()
    await expect(api.update(project, target, input(), write)).rejects.toMatchObject({
      kind: 'problem',
      problem: conflict,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})
