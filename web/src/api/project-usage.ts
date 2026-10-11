import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export const usageTokenFields = [
  'input_tokens',
  'output_tokens',
  'total_tokens',
  'cached_input_tokens',
  'cache_write_tokens',
  'reasoning_tokens',
] as const
export const usagePurposes = [
  'agent_generation',
  'agent_compaction',
  'approval_auto',
  'meeting_summary_initial',
  'meeting_summary_update',
  'knowledge_embedding',
  'memory_embedding',
  'memory_extraction',
  'memory_consolidation',
  'memory_reflection',
  'rerank',
  'image_generation',
] as const
const consumers = ['agent', 'meeting', 'knowledge', 'memory', 'tool'] as const
const dispatches = ['reserved', 'authorized', 'sent', 'not_sent', 'unknown'] as const
const statuses = ['succeeded', 'failed', 'cancelled', 'unknown'] as const
const errors = [
  'invalid_request',
  'authentication',
  'permission',
  'model_not_found',
  'rate_limited',
  'context_too_large',
  'provider_unavailable',
  'timeout',
  'network',
  'cancelled',
  'content_filter',
  'unsupported_feature',
  'provider_error',
  'unknown',
] as const
const protocolTypes = {
  'openai-chat-completions': 'chat',
  'anthropic-messages': 'chat',
  'openai-embeddings': 'embedding',
  'jina-rerank': 'reranker',
  'openai-images-generations': 'image_generation',
} as const
export type ProjectUsageTokens = Readonly<
  Record<(typeof usageTokenFields)[number], string | null> & {
    source: 'provider' | 'unknown'
  }
>
export type ProjectUsageFinal = Readonly<{
  status: (typeof statuses)[number]
  finished_at: string
  error_category: (typeof errors)[number] | null
}>
export type ProjectUsageRecord = Readonly<{
  id: string
  call_id: string
  attempt_index: string
  project_id: string
  consumer_kind: (typeof consumers)[number]
  purpose: (typeof usagePurposes)[number]
  agent_id: string | null
  execution_id: string | null
  meeting_id: string | null
  provider_id: string
  model_id: string
  provider_name: string
  model_name: string
  provider_model_id: string
  protocol: keyof typeof protocolTypes
  model_type: (typeof protocolTypes)[keyof typeof protocolTypes]
  live_provider_id: string | null
  live_model_id: string | null
  dispatch: (typeof dispatches)[number]
  started_at: string
  dispatched_at: string | null
  final: ProjectUsageFinal | null
  usage: ProjectUsageTokens
  provider_request_id: string | null
}>
export type ProjectUsageQuery = Readonly<{
  from: string
  to: string
  limit?: number
  cursor?: string
}>
export type CapturedUsageQuery = Readonly<{
  from: string
  to: string
  limit: number
  cursor?: string
}>
export type ProjectUsagePage = Readonly<{
  items: readonly ProjectUsageRecord[]
  next_cursor: string | null
}>
export interface ProjectUsageAPI {
  list(projectID: string, query: ProjectUsageQuery, signal: AbortSignal): Promise<ProjectUsagePage>
}
const encoder = new TextEncoder()
function requireValue(condition: boolean): asserts condition {
  if (!condition) throw new AccountFailure('invalid-response')
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function nullableID(value: unknown): string | null {
  return value === null ? null : id(value)
}
function enumeration<const T extends readonly string[]>(value: unknown, values: T): T[number] {
  requireValue(typeof value === 'string' && values.includes(value))
  return value
}
function text(value: unknown, bytes: number, scalars = bytes): string {
  const result = string(value, 1, scalars)
  requireValue(
    encoder.encode(result).byteLength <= bytes &&
      !result.includes('\0') &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
function integer(value: unknown, positive = false): string {
  const result = string(value, 1, 19)
  requireValue(
    !/[^0-9]/.test(result) &&
      (result === '0' || result[0] !== '0') &&
      (!positive || result !== '0') &&
      BigInt(result) <= 9223372036854775807n,
  )
  return result
}
function cursor(value: unknown): string {
  return text(value, 8192)
}
function tokens(value: unknown): ProjectUsageTokens {
  const raw = shape(value, [...usageTokenFields, 'source'])
  const source = enumeration(raw.source, ['provider', 'unknown'] as const)
  const fields = Object.fromEntries(
    usageTokenFields.map((key) => [key, raw[key] === null ? null : integer(raw[key])]),
  ) as Record<(typeof usageTokenFields)[number], string | null>
  const known = usageTokenFields.some((key) => fields[key] !== null)
  requireValue(source === 'provider' ? known : !known)
  return Object.freeze({ ...fields, source })
}
export function captureProjectUsageID(value: unknown): string {
  try {
    return id(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureProjectUsageQuery(value: unknown): CapturedUsageQuery {
  try {
    const raw = shape(value, ['from', 'to'], ['limit', 'cursor'])
    const from = parseSystemInstant(raw.from),
      to = parseSystemInstant(raw.to)
    const limit = Object.hasOwn(raw, 'limit') ? raw.limit : 50
    requireValue(
      from < to &&
        typeof limit === 'number' &&
        Number.isInteger(limit) &&
        limit >= 1 &&
        limit <= 100,
    )
    return Object.freeze({
      from,
      to,
      limit,
      ...(Object.hasOwn(raw, 'cursor') ? { cursor: cursor(raw.cursor) } : {}),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureUsageWindow(now = Date.now()): CapturedUsageQuery {
  try {
    const instant = (value: number) =>
      new Date(value).toISOString().replace(/\.(\d{3})Z$/, '.$1000Z')
    return captureProjectUsageQuery({
      from: instant(now - 30 * 24 * 60 * 60 * 1000),
      to: instant(now),
      limit: 50,
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function compareProjectUsage(a: ProjectUsageRecord, b: ProjectUsageRecord): number {
  return a.started_at > b.started_at
    ? -1
    : a.started_at < b.started_at
      ? 1
      : a.id > b.id
        ? -1
        : a.id < b.id
          ? 1
          : 0
}
export function parseProjectUsageRecord(value: unknown, projectID: string): ProjectUsageRecord {
  id(projectID)
  const v = shape(value, [
    'id',
    'call_id',
    'attempt_index',
    'project_id',
    'consumer_kind',
    'purpose',
    'agent_id',
    'execution_id',
    'meeting_id',
    'provider_id',
    'model_id',
    'provider_name',
    'model_name',
    'provider_model_id',
    'protocol',
    'model_type',
    'live_provider_id',
    'live_model_id',
    'dispatch',
    'started_at',
    'dispatched_at',
    'final',
    'usage',
    'provider_request_id',
  ])
  const consumer_kind = enumeration(v.consumer_kind, consumers),
    purpose = enumeration(v.purpose, usagePurposes)
  const protocol = enumeration(
    v.protocol,
    Object.keys(protocolTypes) as (keyof typeof protocolTypes)[],
  )
  const model_type = protocolTypes[protocol]
  const agent_id = nullableID(v.agent_id),
    execution_id = nullableID(v.execution_id),
    meeting_id = nullableID(v.meeting_id)
  const provider_id = id(v.provider_id),
    model_id = id(v.model_id)
  const live_provider_id = nullableID(v.live_provider_id),
    live_model_id = nullableID(v.live_model_id)
  const dispatch = enumeration(v.dispatch, dispatches),
    started_at = parseSystemInstant(v.started_at)
  const dispatched_at = v.dispatched_at === null ? null : parseSystemInstant(v.dispatched_at)
  const usage = tokens(v.usage)
  const purposeType =
    purpose === 'knowledge_embedding' || purpose === 'memory_embedding'
      ? 'embedding'
      : purpose === 'rerank'
        ? 'reranker'
        : purpose === 'image_generation'
          ? 'image_generation'
          : 'chat'
  requireValue(
    id(v.project_id) === projectID &&
      v.model_type === model_type &&
      model_type === purposeType &&
      (live_provider_id === null || live_provider_id === provider_id) &&
      (live_model_id === null || live_model_id === model_id) &&
      (dispatch === 'sent') === (dispatched_at !== null) &&
      (dispatched_at === null || dispatched_at >= started_at) &&
      (dispatch === 'sent' || usage.source === 'unknown'),
  )
  switch (consumer_kind) {
    case 'agent':
      requireValue(
        agent_id !== null &&
          execution_id !== null &&
          ['agent_generation', 'agent_compaction'].includes(purpose),
      )
      break
    case 'meeting':
      requireValue(
        agent_id === null &&
          execution_id === null &&
          meeting_id !== null &&
          ['meeting_summary_initial', 'meeting_summary_update'].includes(purpose),
      )
      break
    case 'knowledge':
      requireValue(
        agent_id === null &&
          execution_id === null &&
          meeting_id === null &&
          ['knowledge_embedding', 'rerank'].includes(purpose),
      )
      break
    case 'memory':
      requireValue(
        agent_id !== null &&
          execution_id === null &&
          meeting_id === null &&
          [
            'memory_embedding',
            'memory_extraction',
            'memory_consolidation',
            'memory_reflection',
            'rerank',
          ].includes(purpose),
      )
      break
    case 'tool':
      requireValue(
        agent_id === null &&
          meeting_id === null &&
          ['approval_auto', 'image_generation'].includes(purpose),
      )
      break
  }
  let final: ProjectUsageFinal | null = null
  if (v.final !== null) {
    const raw = shape(v.final, ['status', 'finished_at', 'error_category'])
    const status = enumeration(raw.status, statuses),
      finished_at = parseSystemInstant(raw.finished_at)
    const error_category =
      raw.error_category === null ? null : enumeration(raw.error_category, errors)
    requireValue(
      finished_at >= started_at &&
        (dispatched_at === null || finished_at >= dispatched_at) &&
        (status !== 'succeeded' || (error_category === null && dispatch === 'sent')),
    )
    final = Object.freeze({ status, finished_at, error_category })
  }
  const provider_request_id =
    v.provider_request_id === null ? null : text(v.provider_request_id, 256)
  requireValue(provider_request_id === null || !/[^A-Za-z0-9_.:-]/.test(provider_request_id))
  return Object.freeze({
    id: id(v.id),
    call_id: id(v.call_id),
    attempt_index: integer(v.attempt_index, true),
    project_id: projectID,
    consumer_kind,
    purpose,
    agent_id,
    execution_id,
    meeting_id,
    provider_id,
    model_id,
    provider_name: text(v.provider_name, 512, 128),
    model_name: text(v.model_name, 512, 128),
    provider_model_id: text(v.provider_model_id, 256),
    protocol,
    model_type,
    live_provider_id,
    live_model_id,
    dispatch,
    started_at,
    dispatched_at,
    final,
    usage,
    provider_request_id,
  })
}
export function parseProjectUsagePage(
  value: unknown,
  projectID: string,
  query: ProjectUsageQuery,
): ProjectUsagePage {
  const q = captureProjectUsageQuery(query),
    raw = shape(value, ['items', 'next_cursor'])
  requireValue(Array.isArray(raw.items) && raw.items.length <= q.limit)
  const items = raw.items.map((item) => parseProjectUsageRecord(item, projectID))
  requireValue(
    new Set(items.map((item) => item.id)).size === items.length &&
      items.every(
        (item, index) =>
          item.started_at >= q.from &&
          item.started_at < q.to &&
          (index === 0 || compareProjectUsage(items[index - 1]!, item) < 0),
      ),
  )
  const next_cursor = raw.next_cursor === null ? null : cursor(raw.next_cursor)
  requireValue(next_cursor === null || (items.length === q.limit && next_cursor !== q.cursor))
  return Object.freeze({ items: Object.freeze(items), next_cursor })
}
export function createProjectUsageAPI(fetcher?: Fetch): ProjectUsageAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, query, signal) {
      const project = captureProjectUsageID(projectID),
        captured = captureProjectUsageQuery(query)
      return request(
        'listProjectUsage',
        (value) => parseProjectUsagePage(value, project, captured),
        {
          signal,
          projectID: project,
          usageQuery: captured,
        },
      )
    },
  }
}
