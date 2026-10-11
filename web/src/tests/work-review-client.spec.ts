import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import {
  captureReviewInput,
  captureReadyInput,
  createWorkReviewAPI,
  type ReviewInput,
} from '../api/work-review'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1)
const target = id(10)
const instant = '2026-10-11T12:00:00.000000Z'
const write = { csrfToken: 'S'.repeat(43), key: 'original-review-1' }
const signal = () => new AbortController().signal
const input = (state: ReviewInput['request']['target_state'] = 'in_review'): ReviewInput => ({
  expected_version: '9007199254740993',
  request: {
    target_state: state,
    comment: '  原始评审意见\n请检查边界 🧩  ',
    ...(state === 'done' ? {} : { assignee_agent_id: id(20) }),
  },
})
const task = (state: ReviewInput['request']['target_state'] = 'in_review') => ({
  id: target,
  project_id: project,
  milestone_id: id(2),
  sprint_id: id(3),
  title: '检查原始实现',
  manual_rank: '80000000000000000000000000000000',
  version: '9007199254740994',
  created_at: instant,
  updated_at: instant,
  type: 'task',
  priority: 'high',
  state,
  assignee_agent_id: id(20),
  description: '原始描述',
  plan: '原始计划',
})
const receipt = (state: ReviewInput['request']['target_state'] = 'in_review') => ({
  task: task(state),
  task_event_ids: [id(40), id(41)],
  event_ids: [id(50)],
})
const response = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
const problem = (code: string, status: number, commitState = 'not_started') => ({
  type: 'urn:agenteam:problem:test',
  title: 'Unable to apply',
  status,
  detail: 'safe failure',
  instance: `/api/v1/projects/${project}/tasks/${target}/transfer`,
  code,
  request_id: id(90),
  commit_state: commitState,
})
const problemResponse = (body: ReturnType<typeof problem>) =>
  new Response(JSON.stringify(body), {
    status: body.status,
    headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
  })

describe('Human review closed client', () => {
  it('sends the three existing Transfer intents and preserves exact comments and string versions', async () => {
    const bodies = [receipt(), receipt('done'), receipt('todo')]
    const fetcher = vi.fn<Fetch>(async () => response(bodies.shift()))
    const api = createWorkReviewAPI(fetcher)
    for (const state of ['in_review', 'done', 'todo'] as const) {
      const captured = captureReviewInput(input(state))
      expect(Object.isFrozen(captured)).toBe(true)
      expect(Object.isFrozen(captured.request)).toBe(true)
      const result = await api.transfer(project, target, captured, {
        ...write,
        key: `review-${state}`,
      })
      expect(result).toEqual(receipt(state))
      expect(Object.isFrozen(result.task)).toBe(true)
      expect(Object.isFrozen(result.task_event_ids)).toBe(true)
      expect(captured.request.comment).toBe('  原始评审意见\n请检查边界 🧩  ')
      expect(Object.hasOwn(captured.request, 'assignee_agent_id')).toBe(state !== 'done')
    }
    expect(fetcher).toHaveBeenCalledTimes(3)
    fetcher.mock.calls.forEach(([path, init], n) => {
      expect(path).toBe(`/api/v1/projects/${project}/tasks/${target}/transfer`)
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': `review-${(['in_review', 'done', 'todo'] as const)[n]!}`,
      })
      expect(JSON.parse(init.body as string)).toEqual(
        input((['in_review', 'done', 'todo'] as const)[n]!),
      )
    })
  })

  it('rejects unsupported or incomplete review intents before network', async () => {
    const fetcher = vi.fn<Fetch>(async () => response(receipt()))
    const api = createWorkReviewAPI(fetcher)
    for (const value of [
      { ...input(), expected_version: 1 },
      { ...input(), request: { target_state: 'in_review', comment: 'required reviewer' } },
      { ...input(), request: { ...input().request, comment: ' \t\u3000' } },
      { ...input(), request: { ...input().request, assignee_agent_id: null } },
      { ...input(), request: { ...input().request, reviewer_agent_id: id(20) } },
      { ...input(), request: { ...input().request, target_state: 'in_progress' } },
      { expected_version: '1', request: { target_state: 'todo', assignee_agent_id: id(20) } },
    ]) {
      await expect(
        Promise.resolve().then(() => api.transfer(project, target, value as never, write)),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    const ready = {
      expected_version: '1',
      request: { target_state: 'todo', assignee_agent_id: id(20) },
    }
    // Comment omission belongs only to the separately captured backlog-ready intent.
    expect(captureReadyInput(ready)).toEqual(ready)
    for (const request of [
      { target_state: 'todo' },
      { target_state: 'todo', assignee_agent_id: null },
      { ...ready.request, target_state: 'in_review' },
      { ...ready.request, comment: ' ' },
    ]) {
      await expect(
        Promise.resolve().then(() =>
          api.ready(project, target, { expected_version: '1', request } as never, write),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('requires a complete historical receipt for the exact Task, version, target and assignee', async () => {
    for (const change of [
      { project_id: id(99) },
      { id: id(11) },
      { version: '9007199254740995' },
      { state: 'todo' },
      { assignee_agent_id: id(21) },
      { plan: undefined },
    ]) {
      const api = createWorkReviewAPI(async () =>
        response({ ...receipt(), task: { ...task(), ...change } }),
      )
      await expect(api.transfer(project, target, input(), write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    for (const change of [
      { task_event_ids: [id(40), id(40)] },
      { task_event_ids: [id(41), id(40)] },
      { event_ids: [] },
      { changed: false },
    ]) {
      const api = createWorkReviewAPI(async () => response({ ...receipt(), ...change }))
      await expect(api.transfer(project, target, input(), write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    // This wire value contains both Tasks, with a valid original receipt last.
    const duplicate = `{"task":${JSON.stringify(task('todo'))},${JSON.stringify(receipt()).slice(1)}`
    const rawAPI = createWorkReviewAPI(
      async () => new Response(duplicate, { headers: { 'Content-Type': 'application/json' } }),
    )
    await expect(rawAPI.transfer(project, target, input(), write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })

  it('retains an Unknown original intent and uses only explicit Lookup without resubmitting', async () => {
    const unknown = problem('COMMIT_UNKNOWN', 503, 'unknown')
    const responses = [
      problemResponse(unknown),
      response({ status: 'not_observed', receipt: null }),
      response({ status: 'in_progress', receipt: null }),
      response({ status: 'committed', receipt: receipt() }),
    ]
    const fetcher = vi.fn<Fetch>(async () => responses.shift()!)
    const api = createWorkReviewAPI(fetcher)
    const original = input()
    const captured = captureReviewInput(original)
    await expect(api.transfer(project, target, captured, write)).rejects.toMatchObject({
      kind: 'problem',
      problem: unknown,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(await api.lookup(project, target, captured, write)).toEqual({
      status: 'not_observed',
      receipt: null,
    })
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(await api.lookup(project, target, captured, write)).toEqual({
      status: 'in_progress',
      receipt: null,
    })
    expect(fetcher).toHaveBeenCalledTimes(3)
    expect(await api.lookup(project, target, captured, write)).toEqual({
      status: 'committed',
      receipt: receipt(),
    })
    expect(fetcher).toHaveBeenCalledTimes(4)
    for (const [path, init] of fetcher.mock.calls.slice(1)) {
      expect(path).toBe(`/api/v1/projects/${project}/task-transition-commands/lookup`)
      expect(init.method).toBe('POST')
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.transfer',
        target_id: target,
        ...original,
      })
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': write.key,
      })
    }
  })

  it('rejects malformed Lookup states and preserves domain conflicts without retry', async () => {
    for (const body of [
      { status: 'committed', receipt: null },
      { status: 'not_observed', receipt: receipt() },
      { status: 'in_progress', receipt: null, extra: true },
      { state: 'committed', result: receipt() },
    ]) {
      const fetcher = vi.fn<Fetch>(async () => response(body))
      await expect(
        createWorkReviewAPI(fetcher).lookup(project, target, input(), write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(fetcher).toHaveBeenCalledTimes(1)
    }
    const duplicate = `{"status":"in_progress","status":"committed","receipt":${JSON.stringify(receipt())}}`
    const rawAPI = createWorkReviewAPI(
      async () => new Response(duplicate, { headers: { 'Content-Type': 'application/json' } }),
    )
    await expect(rawAPI.lookup(project, target, input(), write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    const ambiguous = `{"commit_state":"unknown",${JSON.stringify(problem('COMMIT_UNKNOWN', 503)).slice(1)}`
    const ambiguousAPI = createWorkReviewAPI(
      async () =>
        new Response(ambiguous, {
          status: 503,
          headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
        }),
    )
    await expect(ambiguousAPI.transfer(project, target, input(), write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    const body = problem('COMMENT_REQUIRED', 409)
    const fetcher = vi.fn<Fetch>(async () => problemResponse(body))
    await expect(
      createWorkReviewAPI(fetcher).transfer(project, target, input(), write),
    ).rejects.toMatchObject({ kind: 'problem', problem: body })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})
