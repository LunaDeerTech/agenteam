import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import type {
  CapturedInboxQuery,
  InboxItem,
  InboxSource,
  InboxPage,
  InboxQuery,
  ProjectInboxAPI,
} from './inbox-types'

function check(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  check(uuid7.test(result))
  return result
}
function text(value: unknown, maximum: number) {
  const result = string(value, 0, maximum)
  const bytes = new TextEncoder().encode(result)
  check(
    bytes.length <= maximum && !result.includes('\0') && new TextDecoder().decode(bytes) === result,
  )
  return result
}
function instant(value: unknown) {
  const raw = string(value, 20, 32)
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,6}))?(Z|[+-]\d{2}:\d{2})$/.exec(
    raw,
  )
  check(match)
  parseSystemInstant(match[1] + '.000000Z')
  if (match[3] !== 'Z')
    check(Number(match[3]!.slice(1, 3)) <= 23 && Number(match[3]!.slice(4)) <= 59)
  const parsed = Date.parse(raw)
  check(Number.isFinite(parsed))
  const utc = new Date(parsed).toISOString()
  check(utc.length === 24)
  return utc.slice(0, 19) + '.' + (match[2] ?? '').padEnd(6, '0') + 'Z'
}
export function captureInboxProject(value: unknown) {
  try {
    return id(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureInboxQuery(value: unknown): CapturedInboxQuery {
  try {
    const v = shape(value, [], ['status', 'limit', 'before'])
    const status = Object.hasOwn(v, 'status') ? v.status : 'open',
      limit = Object.hasOwn(v, 'limit') ? v.limit : 50
    check(
      (status === 'open' || status === 'resolved') &&
        Number.isInteger(limit) &&
        Number(limit) >= 1 &&
        Number(limit) <= 100,
    )
    return Object.freeze({
      status,
      limit: Number(limit),
      ...(Object.hasOwn(v, 'before') ? { before: id(v.before) } : {}),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function inboxSourceKey(item: InboxItem) {
  return [item.source_type, item.source_id, item.action_type].join(':')
}
export function compareInboxItems(a: InboxItem, b: InboxItem) {
  if (a.blocking !== b.blocking) return a.blocking ? -1 : 1
  if (a.priority !== b.priority) return b.priority - a.priority
  if (a.updated_at !== b.updated_at) return a.updated_at > b.updated_at ? -1 : 1
  return a.id === b.id ? 0 : a.id > b.id ? -1 : 1
}
function item(value: unknown, project: string, query: CapturedInboxQuery): InboxItem {
  const v = shape(value, [
    'id',
    'project_id',
    'source_type',
    'source_id',
    'source_context',
    'action_type',
    'status',
    'blocking',
    'priority',
    'dismissible',
    'title',
    'summary',
    'source_version',
    'source_occurred_at',
    'created_at',
    'updated_at',
    'resolved_at',
  ])
  check(id(v.project_id) === project && v.status === query.status && v.dismissible === false)
  check(
    typeof v.blocking === 'boolean' &&
      Number.isInteger(v.priority) &&
      Number(v.priority) >= -2147483648 &&
      Number(v.priority) <= 2147483647,
  )
  let source: InboxSource
  if (v.source_type === 'meeting' && v.action_type === 'meeting.review_proposal') {
    shape(v.source_context, [])
    check(v.blocking === false)
    source = {
      source_type: 'meeting',
      source_id: id(v.source_id),
      action_type: 'meeting.review_proposal',
      source_context: Object.freeze({}),
    }
  } else {
    check(
      v.source_type === 'decision_request' &&
        v.action_type === 'decision.answer' &&
        v.blocking === true,
    )
    const context = shape(v.source_context, ['meeting_id'])
    source = {
      source_type: 'decision_request',
      source_id: id(v.source_id),
      action_type: 'decision.answer',
      source_context: Object.freeze({ meeting_id: id(context.meeting_id) }),
    }
  }
  const version = string(v.source_version, 1, 19)
  check(/^[1-9][0-9]{0,18}$/.test(version) && BigInt(version) <= 9223372036854775807n)
  const created = instant(v.created_at),
    updated = instant(v.updated_at),
    resolved = v.resolved_at === null ? null : instant(v.resolved_at)
  check(
    updated >= created &&
      (query.status === 'resolved') === (resolved !== null) &&
      (resolved === null || resolved >= created),
  )
  return Object.freeze({
    id: id(v.id),
    project_id: project,
    ...source,
    status: query.status,
    blocking: v.blocking,
    priority: Number(v.priority),
    dismissible: false,
    title: text(v.title, 4096),
    summary: text(v.summary, 32768),
    source_version: version,
    source_occurred_at: instant(v.source_occurred_at),
    created_at: created,
    updated_at: updated,
    resolved_at: resolved,
  })
}
export function decodeInboxPage(
  value: unknown,
  project: string,
  query: CapturedInboxQuery,
): InboxPage {
  const v = shape(value, ['items'], ['next_before'])
  check(Array.isArray(v.items) && v.items.length <= query.limit)
  const items = v.items.map((value) => item(value, project, query))
  const ids = new Set<string>(),
    sources = new Set<string>()
  for (const [index, entry] of items.entries()) {
    check(
      !ids.has(entry.id) &&
        !sources.has(inboxSourceKey(entry)) &&
        (index === 0 || compareInboxItems(items[index - 1]!, entry) < 0),
    )
    ids.add(entry.id)
    sources.add(inboxSourceKey(entry))
  }
  const next = Object.hasOwn(v, 'next_before') ? id(v.next_before) : null
  check(
    next === null ||
      (items.length === query.limit && next === items.at(-1)?.id && next !== query.before),
  )
  return Object.freeze({ items: Object.freeze(items), next_before: next })
}
export function createProjectInboxAPI(fetcher?: Fetch): ProjectInboxAPI {
  const transport = accountTransport(fetcher)
  return {
    list(projectID: string, query: InboxQuery, signal: AbortSignal) {
      const project = captureInboxProject(projectID),
        captured = captureInboxQuery(query)
      return transport('listProjectInbox', (value) => decodeInboxPage(value, project, captured), {
        signal,
        projectID: project,
        inboxQuery: captured,
      })
    },
  }
}
