import { describe, expect, it, vi } from 'vitest'
import { createProjectSchedulerAPI } from '../api/project-scheduler'
import { type Fetch } from '../api/client'
import {
  schedulerBase,
  schedulerID as id,
  schedulerJSON as json,
  schedulerLookup,
} from './project-scheduler.fixture'

const input = {
  expected_version: '9007199254740993',
  scheduler_enabled: false,
  scheduler_max_concurrency: '9223372036854775807',
}
const write = {
  csrfToken: 'S'.repeat(43),
  key: 'scheduler.original:key',
  signal: new AbortController().signal,
}
describe('Scheduler unique client transport', () => {
  it('sends exact GET/PATCH and the unchanged key-only Lookup without rewriting string integers', async () => {
    const fetch = vi.fn<Fetch>(async (path, init) =>
      path === schedulerLookup
        ? json({ state: 'not_observed' })
        : init.method === 'GET'
          ? json({
              project_id: id(10),
              version: input.expected_version,
              scheduler_enabled: true,
              scheduler_max_concurrency: null,
            })
          : json({ project_id: id(10), version: '9007199254740994' }),
    )
    const api = createProjectSchedulerAPI(fetch)
    await api.get(id(10), write.signal)
    await api.update(id(10), input, write)
    await api.lookup(id(10), id(1), write)
    expect(fetch.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      [schedulerBase, 'GET'],
      [schedulerBase, 'PATCH'],
      [schedulerLookup, 'POST'],
    ])
    for (const [, init] of fetch.mock.calls)
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
    expect(fetch.mock.calls[0]![1].body).toBeUndefined()
    expect(fetch.mock.calls[1]![1].body).toBe(JSON.stringify(input))
    expect(fetch.mock.calls[2]![1].body).toBe('{"command":"update"}')
    expect(new Headers(fetch.mock.calls[1]![1].headers).get('X-CSRF-Token')).toBe(write.csrfToken)
    expect(new Headers(fetch.mock.calls[2]![1].headers).get('Idempotency-Key')).toBe(write.key)
  })
  it('rejects duplicate members and oversized complete representations', async () => {
    for (const text of [
      '{"project_id":"' +
        id(10) +
        '","version":"1","scheduler_enabled":true,"scheduler_enabled":false,"scheduler_max_concurrency":null}',
      ' '.repeat(4097) + '{}',
    ]) {
      const api = createProjectSchedulerAPI(
        async () => new Response(text, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(api.get(id(10), write.signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('captures input before asynchronous work and refuses missing fields without a fetch', async () => {
    const fetch = vi.fn<Fetch>(async () =>
      json({ project_id: id(10), version: input.expected_version }),
    )
    const api = createProjectSchedulerAPI(fetch),
      mutable = { ...input }
    const request = api.update(id(10), mutable, write)
    mutable.scheduler_enabled = true
    await request
    expect(fetch.mock.calls[0]![1].body).toBe(JSON.stringify(input))
    expect(() => api.update(id(10), { expected_version: '1' } as typeof input, write)).toThrow()
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
