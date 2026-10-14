import { describe, expect, it, vi } from 'vitest'
import {
  createTaskExecutionCancelAPI,
  type TaskExecutionCancelInput,
} from '../api/task-execution-cancel'
import type { Fetch } from '../api/client'
import { at, execution, id, json, p, path, t } from './task-execution-fixture'

const original: TaskExecutionCancelInput = {
  expected_version: '9007199254740993',
  expected_task_version: '3',
  reason: '  Owner pause\n',
}
const write = { csrfToken: 'S'.repeat(43), key: 'original-execution-cancel-1' }
function receipt() {
  return {
    execution: execution(100, {
      status: 'running',
      started_at: at,
      version: '9007199254740994',
      cancel_requested_at: at,
    }),
    task: { id: t, version: '4', state: 'blocked', blocker_id: id(500) },
  }
}
describe('Human Execution cancellation API', () => {
  it('sends one exact versioned intent and looks up that original key and body without inferring terminal cancellation', async () => {
    const fetch = vi.fn<Fetch>(async (url) =>
      url.endsWith('/lookup')
        ? json({ found: true, receipt: receipt() })
        : json({ receipt: receipt(), replayed: false }),
    )
    const api = createTaskExecutionCancelAPI(fetch)
    const result = await api.cancel(p, t, id(100), original, write)
    expect(result.receipt.execution.status).toBe('running')
    expect(result.receipt.execution.cancel_requested_at).toBe(at)
    expect(result.receipt.task.state).toBe('blocked')
    expect(await api.lookup(p, t, id(100), original, write)).toEqual({
      found: true,
      receipt: receipt(),
    })
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      path + '/' + id(100) + '/cancel',
      path + '/' + id(100) + '/cancel/lookup',
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init.method).toBe('POST')
      expect(init.body).toBe(JSON.stringify(original))
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(write.key)
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(write.csrfToken)
    }
  })
  it('rejects an unrelated or inconsistent receipt and keeps a not-observed lookup distinct', async () => {
    let value: unknown
    const api = createTaskExecutionCancelAPI(async () => json(value))
    const valid = receipt()
    for (const patch of [
      { ...valid, task: { ...valid.task, id: id(999) } },
      { ...valid, task: { ...valid.task, state: 'cancelled' } },
      { ...valid, task: { ...valid.task, version: '3' } },
      { ...valid, execution: { ...valid.execution, version: original.expected_version } },
      { ...valid, execution: { ...valid.execution, cancel_requested_at: null } },
      {
        ...valid,
        execution: {
          ...valid.execution,
          status: 'cancelled',
          completed_at: at,
          reason: 'cancelled',
        },
      },
    ]) {
      value = { receipt: patch, replayed: false }
      await expect(api.cancel(p, t, id(100), original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    value = { found: false, receipt: null }
    expect(await api.lookup(p, t, id(100), original, write)).toEqual(value)
    value = { found: false, receipt: valid }
    await expect(api.lookup(p, t, id(100), original, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('preserves omitted versus explicit reason and rejects malformed inputs before dispatch', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ receipt: receipt(), replayed: true }))
    const api = createTaskExecutionCancelAPI(fetch)
    for (const reason of [null, '', ' \n\t', 'a\0b', '中'.repeat(342), '\ud800']) {
      expect(() =>
        api.cancel(p, t, id(100), { ...original, reason } as never, write),
      ).toThrowError()
    }
    expect(() => api.cancel('../foreign', t, id(100), original, write)).toThrowError()
    expect(fetch).not.toHaveBeenCalled()
    const input = { expected_version: original.expected_version, expected_task_version: '3' }
    expect((await api.cancel(p, t, id(100), input, write)).replayed).toBe(true)
    expect(JSON.parse(String(fetch.mock.calls[0]?.[1].body))).toEqual(input)
  })
  it('does not retry or turn an incomplete response into a confirmed cancellation', async () => {
    const fetch = vi.fn<Fetch>(
      async () =>
        new Response('{"receipt":', {
          headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(999) },
        }),
    )
    const api = createTaskExecutionCancelAPI(fetch)
    await expect(api.cancel(p, t, id(100), original, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
