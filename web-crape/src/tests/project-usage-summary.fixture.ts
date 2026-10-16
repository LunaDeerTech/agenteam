import { usageSummaryFields, type ProjectUsageSummary } from '../api/project-usage'

export const dailyQuery = { from: '2026-09-11T13:00:00.000000Z', to: '2026-10-11T13:00:00.000000Z' }
export const dailyAsOf = '2026-10-11T13:00:01.123456Z'
export function dailySummary(): ProjectUsageSummary {
  return {
    confirmed_invocations: '51',
    dispatch_unknown: '2',
    succeeded: '48',
    failed: '1',
    cancelled: '1',
    unknown: '1',
    input: { sum: '9007199254740993', known_count: '50', unknown_count: '1' },
    output: { sum: '0', known_count: '49', unknown_count: '2' },
    total: { sum: '9223372036854775807', known_count: '1', unknown_count: '50' },
    cached_input: { sum: null, known_count: '0', unknown_count: '51' },
    cache_write: { sum: '1', known_count: '1', unknown_count: '50' },
    reasoning: { sum: null, known_count: '0', unknown_count: '51' },
    as_of: dailyAsOf,
  }
}
export function dailyPage(days = ['2026-10-11']) {
  return {
    items: days.map((day) => ({ key: { by: 'day', id: null, day }, summary: dailySummary() })),
    next_cursor: null,
    as_of: dailyAsOf,
  }
}
export function zeroSummary(): ProjectUsageSummary {
  return {
    ...dailySummary(),
    confirmed_invocations: '0',
    dispatch_unknown: '0',
    succeeded: '0',
    failed: '0',
    cancelled: '0',
    unknown: '0',
    ...Object.fromEntries(
      usageSummaryFields.map((field) => [
        field,
        { sum: '0', known_count: '0', unknown_count: '0' },
      ]),
    ),
  }
}
