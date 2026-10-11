import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import {
  captureUsageWindow,
  createProjectUsageAPI,
  parseProjectUsagePage,
  parseProjectUsageRecord,
} from '../api/project-usage'
import {
  usageBase,
  usageID,
  usageJSON,
  usageNow,
  usagePage,
  usageProject,
  usageQuery,
  usageRecord,
} from './project-usage.fixture'

describe('Project usage read contract', () => {
  it('uses the exact bounded GET with a captured UTC window, opaque cursor and no command headers', async () => {
    const fetch = vi.fn<Fetch>(async () => usageJSON(usagePage())),
      signal = new AbortController().signal
    const query = { ...captureUsageWindow(usageNow), cursor: 'opaque +/=' }
    expect(query).toEqual({ ...usageQuery, cursor: 'opaque +/=' })
    const page = await createProjectUsageAPI(fetch).list(usageProject, query, signal)
    expect(fetch.mock.calls[0]![0]).toBe(
      usageBase +
        '?from=2026-09-11T13%3A00%3A00.000000Z&to=2026-10-11T13%3A00%3A00.000000Z&limit=50&cursor=opaque+%2B%2F%3D',
    )
    const options = fetch.mock.calls[0]![1],
      headers = new Headers(options.headers)
    expect(options.method).toBe('GET')
    expect(options.credentials).toBe('same-origin')
    expect(options.body).toBeUndefined()
    expect(headers.has('Idempotency-Key')).toBe(false)
    expect(headers.has('X-CSRF-Token')).toBe(false)
    expect(Object.keys(page.items[0]!)).toHaveLength(24)
    expect(page.items[0]!.usage).toEqual(usageRecord().usage)
    expect(page.items[0]!.attempt_index).toBe('9007199254740993')
    expect(Object.isFrozen(page.items[0]!.usage)).toBe(true)
    expect(page.next_cursor).toBeNull()
  })
  it('keeps unknown, exact zero, MaxInt64 and historical names distinct', () => {
    const original = usageRecord()
    const row = parseProjectUsageRecord(
      {
        ...original,
        provider_name: ' e\u0301 ',
        model_name: '中'.repeat(128),
        usage: { ...original.usage, input_tokens: '9223372036854775807' },
      },
      usageProject,
    )
    expect(row.provider_name).toBe(' e\u0301 ')
    expect(row.usage.input_tokens).toBe('9223372036854775807')
    expect(row.usage.output_tokens).toBe('0')
    expect(row.usage.total_tokens).toBeNull()
    const unknown = {
      input_tokens: null,
      output_tokens: null,
      total_tokens: null,
      cached_input_tokens: null,
      cache_write_tokens: null,
      reasoning_tokens: null,
      source: 'unknown',
    }
    expect(
      parseProjectUsageRecord(
        { ...original, dispatch: 'unknown', dispatched_at: null, final: null, usage: unknown },
        usageProject,
      ).usage.source,
    ).toBe('unknown')
    for (const usage of [
      { ...original.usage, input_tokens: 1 },
      { ...original.usage, input_tokens: '01' },
      { ...original.usage, input_tokens: '-1' },
      { ...original.usage, input_tokens: '9223372036854775808' },
      { ...original.usage, source: 'unknown' },
      { ...unknown, source: 'provider' },
    ])
      expect(() => parseProjectUsageRecord({ ...original, usage }, usageProject)).toThrow(
        AccountFailure,
      )
  })
  it('validates the complete safe row and cross-field historical identity without leaking extras', () => {
    const original = usageRecord()
    for (const patch of [
      { credential_ref: 'controlled-sensitive-extra' },
      { project_id: usageID(9) },
      { attempt_index: '0' },
      { live_provider_id: usageID(9) },
      { live_model_id: usageID(9) },
      { provider_name: 'a'.repeat(129) },
      { model_name: '\ud800' },
      { provider_model_id: '中'.repeat(86) },
      { provider_request_id: 'unsafe\n' },
      { dispatched_at: null },
      { dispatched_at: '2025-10-11T12:00:00.123456Z' },
      { started_at: '2026-02-30T00:00:00.000000Z' },
      { model_type: 'embedding' },
      { purpose: 'memory_embedding' },
      { consumer_kind: 'meeting' },
      { agent_id: null },
      { execution_id: null },
      { dispatch: 'reserved' },
      { final: { ...original.final, error_category: 'network' } },
      { final: { ...original.final, finished_at: '2025-01-01T00:00:00.000000Z' } },
    ]) {
      try {
        parseProjectUsageRecord({ ...original, ...patch }, usageProject)
        throw new Error('Expected rejection')
      } catch (error) {
        expect(error).toBeInstanceOf(AccountFailure)
        expect(String(error)).toBe('AccountFailure: invalid-response')
      }
    }
  })
  it('rejects invalid final rows, missing nullable cursor, bad order and duplicate IDs as a whole page', () => {
    const first = usageRecord(100),
      second = usageRecord(99)
    expect(
      parseProjectUsagePage(usagePage([first, second]), usageProject, usageQuery).items,
    ).toHaveLength(2)
    for (const page of [
      { items: [first] },
      { items: [first], next_cursor: undefined },
      { ...usagePage(), summary: {} },
      usagePage([first, { ...second, project_id: usageID(9) }]),
      usagePage([second, first]),
      usagePage([first, first]),
      usagePage([first], 'next'),
      usagePage([{ ...first, started_at: usageQuery.to }]),
      usagePage([{ ...first, usage: { ...first.usage, extra: 'controlled-extra' } } as never]),
    ])
      expect(() => parseProjectUsagePage(page, usageProject, usageQuery)).toThrow(AccountFailure)
    expect(() =>
      parseProjectUsagePage(usagePage([first], 'same'), usageProject, {
        ...usageQuery,
        limit: 1,
        cursor: 'same',
      }),
    ).toThrow(AccountFailure)
  })
  it('rejects duplicate raw fields, incomplete JSON and representations exceeding 1 MiB', async () => {
    let raw = JSON.stringify(usagePage()).replace(
      '"next_cursor":null',
      '"next_cursor":null,"next_cursor":null',
    )
    const api = createProjectUsageAPI(
      async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
    )
    const signal = new AbortController().signal
    await expect(api.list(usageProject, usageQuery, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    raw = JSON.stringify(usagePage()).slice(0, -1)
    await expect(api.list(usageProject, usageQuery, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    raw = ' '.repeat(1024 * 1024) + JSON.stringify(usagePage())
    await expect(api.list(usageProject, usageQuery, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('captures exact query and transport inputs before calling fetch', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectUsageAPI(fetch),
      signal = new AbortController().signal
    for (const patch of [
      { from: undefined },
      { to: usageQuery.from },
      { from: '2026-02-30T00:00:00.000000Z' },
      { limit: 0 },
      { limit: 101 },
      { limit: 1.5 },
      { cursor: null },
      { cursor: '' },
      { cursor: '\0' },
      { cursor: '\ud800' },
      { cursor: 'a'.repeat(8193) },
      { summary: true },
    ])
      await expect(
        api.list(usageProject, { ...usageQuery, ...patch } as never, signal),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(api.list('../wrong', usageQuery, signal)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(
      accountTransport(fetch)('listProjectUsage', (v) => v, {
        signal,
        projectID: usageProject,
        usageQuery,
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
})
