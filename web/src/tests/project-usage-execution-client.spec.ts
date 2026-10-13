import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureProjectUsageDailyQuery,
  captureProjectUsageQuery,
  createProjectUsageAPI,
} from '../api/project-usage'
import { dailyPage, dailyQuery } from './project-usage-summary.fixture'
import {
  usageBase,
  usageID,
  usageJSON,
  usagePage,
  usageProject,
  usageQuery,
  usageRecord,
} from './project-usage.fixture'

describe('Execution-scoped Usage reads', () => {
  it('captures one canonical execution in both read queries and sends only the bounded GET fields', async () => {
    const execution = usageID(21)
    const fetch = vi.fn<Fetch>(async (path) =>
      usageJSON(path.includes('/summary?') ? dailyPage() : usagePage()),
    )
    const api = createProjectUsageAPI(fetch),
      signal = new AbortController().signal
    const query = { ...usageQuery, execution_id: execution, cursor: 'opaque +/=' }
    const captured = captureProjectUsageQuery(query)
    query.execution_id = usageID(22)
    expect(captured.execution_id).toBe(execution)
    expect(Object.isFrozen(captured)).toBe(true)
    await api.list(usageProject, captured, signal)
    await api.daily(usageProject, { ...dailyQuery, execution_id: execution }, signal)
    const queries = fetch.mock.calls.map(([path]) => new URL(path, 'https://owned.invalid'))
    expect(queries[0]!.pathname).toBe(usageBase)
    expect(Object.fromEntries(queries[0]!.searchParams)).toEqual({
      ...usageQuery,
      limit: '50',
      execution_id: execution,
      cursor: 'opaque +/=',
    })
    expect(queries[1]!.pathname).toBe(usageBase + '/summary')
    expect(Object.fromEntries(queries[1]!.searchParams)).toEqual({
      ...dailyQuery,
      group_by: 'day',
      limit: '100',
      execution_id: execution,
    })
    for (const [, init] of fetch.mock.calls) {
      expect(init.method).toBe('GET')
      expect(init.body).toBeUndefined()
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
      expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
    }
  })

  it('rejects null, noncanonical execution IDs and other filter keys before any transport', async () => {
    const fetch = vi.fn<Fetch>(),
      request = accountTransport(fetch)
    for (const patch of [
      { execution_id: null },
      { execution_id: undefined },
      { execution_id: '' },
      { execution_id: 'null' },
      { execution_id: usageID(21).toUpperCase() },
      { execution_id: usageID(21).replace('-7000-', '-4000-') },
      { agent_id: usageID(20) },
    ]) {
      expect(() => captureProjectUsageQuery({ ...usageQuery, ...patch })).toThrow('invalid-input')
      expect(() => captureProjectUsageDailyQuery({ ...dailyQuery, ...patch })).toThrow(
        'invalid-input',
      )
      await expect(
        request('listProjectUsage', (value) => value, {
          signal: new AbortController().signal,
          projectID: usageProject,
          usageQuery: { ...usageQuery, ...patch } as never,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      await expect(
        request('aggregateProjectUsage', (value) => value, {
          signal: new AbortController().signal,
          projectID: usageProject,
          usageQuery: { ...dailyQuery, group_by: 'day', limit: 100, ...patch } as never,
        }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    await expect(
      request('listProjectUsage', (value) => value, {
        signal: new AbortController().signal,
        projectID: 'null',
        usageQuery: { ...usageQuery, execution_id: usageID(21) },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })

  it('rejects an entire page containing another execution while retaining legitimate empty and unfiltered reads', async () => {
    const other = { ...usageRecord(99), execution_id: usageID(22) }
    let body: unknown = usagePage([usageRecord(), other])
    const api = createProjectUsageAPI(async () => usageJSON(body))
    const query = { ...usageQuery, execution_id: usageID(21) },
      signal = new AbortController().signal
    await expect(api.list(usageProject, query, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect((await api.list(usageProject, usageQuery, signal)).items).toHaveLength(2)
    body = usagePage([])
    expect(
      (await api.list(usageProject, { ...query, execution_id: usageID(999) }, signal)).items,
    ).toEqual([])
    body = dailyPage([])
    expect(
      (await api.daily(usageProject, { ...dailyQuery, execution_id: usageID(999) }, signal)).items,
    ).toEqual([])
  })
})
