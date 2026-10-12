import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureProjectUsageDailyQuery,
  createProjectUsageAPI,
  parseProjectUsageDailyPage,
  usageSummaryFields,
} from '../api/project-usage'
import { usageBase, usageJSON, usageProject } from './project-usage.fixture'
import { dailyAsOf, dailyPage, dailyQuery, zeroSummary } from './project-usage-summary.fixture'

describe('Project daily server summary contract', () => {
  it('requests the full daily server aggregation using exact UTC bounds and no command headers', async () => {
    const fetch = vi.fn<Fetch>(async () => usageJSON(dailyPage())),
      signal = new AbortController().signal
    const result = await createProjectUsageAPI(fetch).daily(usageProject, dailyQuery, signal)
    const [path, init] = fetch.mock.calls[0]!
    const url = new URL(path, 'https://owned.invalid')
    expect(url.pathname).toBe(usageBase + '/summary')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      ...dailyQuery,
      limit: '100',
      group_by: 'day',
    })
    expect(init.method).toBe('GET')
    expect(init.credentials).toBe('same-origin')
    expect(init.body).toBeUndefined()
    expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
    expect(result).toEqual(dailyPage())
    expect(Object.isFrozen(result.items[0]!.summary.input)).toBe(true)
  })
  it('preserves all six exact sums and denominators, with unknown distinct from zero and an empty page', () => {
    const summary = parseProjectUsageDailyPage(dailyPage(), dailyQuery).items[0]!.summary
    expect(summary.input.sum).toBe('9007199254740993')
    expect(summary.total.sum).toBe('9223372036854775807')
    expect(summary.output.sum).toBe('0')
    expect(summary.cached_input).toEqual({ sum: null, known_count: '0', unknown_count: '51' })
    expect(summary.dispatch_unknown).toBe('2')
    for (const field of usageSummaryFields)
      expect(BigInt(summary[field].known_count) + BigInt(summary[field].unknown_count)).toBe(51n)
    expect(parseProjectUsageDailyPage(dailyPage([]), dailyQuery)).toEqual({
      items: [],
      next_cursor: null,
      as_of: dailyAsOf,
    })
    const zero = dailyPage()
    zero.items[0]!.summary = zeroSummary()
    expect(parseProjectUsageDailyPage(zero, dailyQuery).items[0]!.summary.reasoning.sum).toBe('0')
  })
  it('rejects invalid denominators, overflow, nullable sums and status arithmetic as a complete response', () => {
    for (const patch of [
      { input: { sum: '1', known_count: '50', unknown_count: '2' } },
      { input: { sum: null, known_count: '50', unknown_count: '1' } },
      { input: { sum: '0', known_count: '0', unknown_count: '51' } },
      { input: { sum: '9223372036854775808', known_count: '50', unknown_count: '1' } },
      { input: { sum: 4, known_count: '50', unknown_count: '1' } },
      { dispatch_unknown: '9223372036854775807' },
      { succeeded: '54' },
      { succeeded: '01' },
      { as_of: '2026-10-11T13:00:00.000000Z' },
      { tokens: {} },
    ]) {
      const page = dailyPage(['2026-10-10', '2026-10-11'])
      Object.assign(page.items[1]!.summary, patch)
      expect(() => parseProjectUsageDailyPage(page, dailyQuery)).toThrow(AccountFailure)
    }
  })
  it('requires complete UTC-day groups with matching as_of, unique ascending days and no hidden cursor', () => {
    for (const value of [
      { ...dailyPage(), next_cursor: 'more' },
      { items: [], as_of: dailyAsOf },
      { ...dailyPage(), as_of: 'invalid' },
      dailyPage(['2026-10-11', '2026-10-10']),
      dailyPage(['2026-10-11', '2026-10-11']),
      dailyPage(['2026-09-10']),
      dailyPage(['2026-10-12']),
      dailyPage(['2026-02-30']),
      {
        ...dailyPage(),
        items: [{ ...dailyPage().items[0], key: { by: 'model', id: usageProject, day: null } }],
      },
    ])
      expect(() => parseProjectUsageDailyPage(value, dailyQuery)).toThrow(AccountFailure)
    expect(
      parseProjectUsageDailyPage(dailyPage(['2026-09-11', '2026-10-11']), dailyQuery).items,
    ).toHaveLength(2)
  })
  it('rejects wider or malformed windows before I/O and duplicate/truncated/oversized JSON after I/O', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectUsageAPI(fetch),
      signal = new AbortController().signal
    for (const patch of [
      { to: dailyQuery.from },
      { from: 'invalid' },
      { to: '2026-10-11T13:00:00.000001Z' },
      { limit: 50 },
      { group_by: 'model' },
      { cursor: 'x' },
    ])
      await expect(
        api.daily(usageProject, { ...dailyQuery, ...patch }, signal),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    expect(
      captureProjectUsageDailyQuery({
        from: '2026-09-11T13:00:00.123456Z',
        to: '2026-10-11T13:00:00.123456Z',
      }).limit,
    ).toBe(100)
    for (const raw of [
      JSON.stringify(dailyPage()).replace(
        '"next_cursor":null',
        '"next_cursor":null,"next_cursor":null',
      ),
      JSON.stringify(dailyPage()).slice(0, -1),
      ' '.repeat(1024 * 1024) + JSON.stringify(dailyPage()),
    ]) {
      fetch.mockImplementationOnce(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(api.daily(usageProject, dailyQuery, signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
})
