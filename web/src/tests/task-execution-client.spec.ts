import { describe, expect, it, vi } from 'vitest'
import { createTaskExecutionsAPI } from '../api/task-executions'
import type { Fetch } from '../api/client'
import { at, execution, id, json, p, path, problem, t } from './task-execution-fixture'

describe('Task Execution owner reads', () => {
  it('uses the Task-scoped GETs with exact IDs, opaque cursor and preserved int64 values', async () => {
    const row = execution(100, { status: 'succeeded', started_at: at, completed_at: at, snapshot_id: id(200), reason: 'completed' })
    const fetch = vi.fn<Fetch>(async url => url.includes('?') ? json({ items: [row], next_cursor: null }) : json({ execution: row }))
    const api = createTaskExecutionsAPI(fetch), signal = new AbortController().signal
    expect((await api.list(p, t, { limit: 1, cursor: 'opaque+/=' }, signal)).items[0]?.version).toBe('9007199254740993')
    expect(await api.get(p, t, row.id, signal)).toEqual(row)
    expect(fetch.mock.calls.map(call => call[0])).toEqual([path + '?limit=1&cursor=opaque%2B%2F%3D', path + '/' + row.id])
    for (const [, init] of fetch.mock.calls) {
      expect(init.method).toBe('GET')
      expect(init.credentials).toBe('same-origin')
      expect(init.body).toBeUndefined()
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
    }
  })
  it('rejects foreign scope, undeclared material, invalid terminal facts and partial pages', async () => {
    let value: unknown
    const api = createTaskExecutionsAPI(async () => json(value)), signal = new AbortController().signal
    for (const patch of [
      { project_id: id(99) }, { task_id: id(98) }, { id: id(97) }, { prompt: 'private' },
      { status: 'running', reason: 'completed' }, { status: 'succeeded', completed_at: null },
      { reason: 'provider_secret' }, { version: '9223372036854775808' }, { created_at: '2026-02-30T10:00:00.000000Z' },
    ]) {
      value = { execution: { ...execution(), ...patch } }
      await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    for (const page of [
      { items: [execution(), execution()], next_cursor: null },
      { items: [execution(100), execution(101)], next_cursor: null },
      { items: [execution()], next_cursor: 'early' },
      { items: [execution()] },
      { items: [execution(), execution(99, { task_id: id(55) })], next_cursor: null },
    ]) {
      value = page
      await expect(api.list(p, t, { limit: 50 }, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('fails closed before dispatch for malformed IDs and queries and retains safe HTTP problems', async () => {
    const fetch = vi.fn<Fetch>(async () => problem('PERMISSION_DENIED', 403))
    const api = createTaskExecutionsAPI(fetch), signal = new AbortController().signal
    for (const q of [{ limit: 0 }, { limit: 101 }, { limit: null }, { cursor: '' }, { cursor: 'bad\n' }, { task_id: id(88) }])
      await expect(api.list(p, t, q as never, signal)).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(api.get(p, t, '../foreign', signal)).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    await expect(api.list(p, t, {}, signal)).rejects.toMatchObject({ kind: 'problem', problem: { status: 403, code: 'PERMISSION_DENIED' } })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
