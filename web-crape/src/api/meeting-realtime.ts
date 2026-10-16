import { AccountFailure, parseExactJSON, shape, string } from './client'
import { parseMeetingDetail, type MeetingDetail } from './meetings'
import {
  parseMeetingRuntime,
  runtimeProgress,
  type MeetingRuntimeProjection,
} from './meeting-runtime'
import { workID, workVersion } from './work-review'

export type MeetingTimelineTopic = Readonly<{
  kind: 'meeting.timeline'
  project_id: string
  meeting_id: string
}>
export type MeetingExecutionTopic = Readonly<{
  kind: 'meeting.execution'
  project_id: string
  meeting_id: string
  contribution_id: string
  generation: string
  attempt: string
  execution_id: string
}>
export type MeetingTopic = MeetingTimelineTopic | MeetingExecutionTopic
export type MeetingWatermark = Readonly<{ epoch: string; sequence: string }>
export type MeetingRealtimeSnapshot = Readonly<
  | {
      topic: MeetingTimelineTopic
      schema: 'meeting_timeline_v1'
      content: MeetingDetail
      digest: string
    }
  | {
      topic: MeetingExecutionTopic
      schema: 'meeting_execution_runtime_v1'
      content: MeetingRuntimeProjection
      digest: string
    }
>
export type MeetingRealtimeMessage = Readonly<
  | { type: 'unsubscribed'; subscription_id: string }
  | {
      type: 'subscribed' | 'invalidated'
      subscription_id: string
      topic: MeetingTopic
      watermark: MeetingWatermark
    }
  | {
      type: 'snapshot'
      subscription_id: string
      topic: MeetingTopic
      watermark: MeetingWatermark
      snapshot: MeetingRealtimeSnapshot
    }
  | {
      type: 'resync'
      subscription_id: string
      topic: MeetingTopic
      watermark: MeetingWatermark
      reason: 'overflow' | 'epoch_changed' | 'unavailable'
    }
>
export const meetingRealtimeLimits = Object.freeze({
  subscriptions: 16,
  queued: 64,
  snapshotBytes: (16 << 20) + (256 << 10),
})

function check(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function topic(value: unknown): MeetingTopic {
  const v = shape(
    value,
    ['kind', 'project_id', 'meeting_id'],
    ['contribution_id', 'generation', 'attempt', 'execution_id'],
  )
  const base = { project_id: workID(v.project_id), meeting_id: workID(v.meeting_id) }
  if (v.kind === 'meeting.timeline') {
    shape(v, ['kind', 'project_id', 'meeting_id'])
    return Object.freeze({ ...base, kind: 'meeting.timeline' })
  }
  check(v.kind === 'meeting.execution')
  shape(v, [
    'kind',
    'project_id',
    'meeting_id',
    'contribution_id',
    'generation',
    'attempt',
    'execution_id',
  ])
  return Object.freeze({
    ...base,
    kind: 'meeting.execution',
    contribution_id: workID(v.contribution_id),
    generation: workVersion(v.generation),
    attempt: workVersion(v.attempt),
    execution_id: workID(v.execution_id),
  })
}
export function captureMeetingTopic(value: unknown): MeetingTopic {
  try {
    return topic(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function sameMeetingTopic(a: MeetingTopic, b: MeetingTopic) {
  return (
    a.kind === b.kind &&
    a.project_id === b.project_id &&
    a.meeting_id === b.meeting_id &&
    (a.kind === 'meeting.timeline' ||
      (b.kind === 'meeting.execution' &&
        a.contribution_id === b.contribution_id &&
        a.generation === b.generation &&
        a.attempt === b.attempt &&
        a.execution_id === b.execution_id))
  )
}
function watermark(value: unknown): MeetingWatermark {
  const v = shape(value, ['epoch', 'sequence'])
  return Object.freeze({ epoch: workID(v.epoch), sequence: runtimeProgress(v.sequence) })
}

// canonical-v1 uses sorted object keys and Go JSON's HTML/line-separator escapes.
// All object shapes have already been validated by the domain decoder below.
function canonical(value: unknown, depth = 0): string {
  check(depth <= 32)
  if (value === null || typeof value === 'boolean') return JSON.stringify(value)
  if (typeof value === 'string') {
    check(new TextDecoder().decode(new TextEncoder().encode(value)) === value)
    return JSON.stringify(value).replace(
      /[<>&\u2028\u2029]/g,
      (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'),
    )
  }
  if (typeof value === 'number') {
    check(Number.isSafeInteger(value))
    return String(value)
  }
  if (Array.isArray(value)) return '[' + value.map((v) => canonical(v, depth + 1)).join(',') + ']'
  check(typeof value === 'object' && value !== null)
  const object = value as Record<string, unknown>
  return (
    '{' +
    Object.keys(object)
      .sort()
      .map((key) => canonical(key, depth + 1) + ':' + canonical(object[key], depth + 1))
      .join(',') +
    '}'
  )
}
async function snapshot(value: unknown, expected: MeetingTopic): Promise<MeetingRealtimeSnapshot> {
  const v = shape(value, ['topic', 'schema', 'content', 'digest']),
    actual = topic(v.topic)
  check(sameMeetingTopic(actual, expected))
  const digest = string(v.digest, 71, 71)
  check(/^sha256:[0-9a-f]{64}$/.test(digest))
  let result: MeetingRealtimeSnapshot
  if (actual.kind === 'meeting.timeline') {
    check(v.schema === 'meeting_timeline_v1')
    result = Object.freeze({
      topic: actual,
      schema: 'meeting_timeline_v1',
      content: parseMeetingDetail(v.content, actual.project_id, actual.meeting_id),
      digest,
    })
  } else {
    check(v.schema === 'meeting_execution_runtime_v1')
    result = Object.freeze({
      topic: actual,
      schema: 'meeting_execution_runtime_v1',
      content: parseMeetingRuntime(v.content, actual.project_id, actual.execution_id),
      digest,
    })
  }
  const bytes = new TextEncoder().encode(canonical(v.content))
  check(bytes.length <= meetingRealtimeLimits.snapshotBytes)
  const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))
  check(digest === 'sha256:' + [...hash].map((v) => v.toString(16).padStart(2, '0')).join(''))
  return result
}
export async function decodeMeetingRealtime(raw: unknown): Promise<MeetingRealtimeMessage> {
  check(
    typeof raw === 'string' &&
      raw.length > 0 &&
      new TextEncoder().encode(raw).length <= meetingRealtimeLimits.snapshotBytes + 8192,
  )
  let value: unknown
  try {
    value = parseExactJSON(raw)
  } catch {
    throw new AccountFailure('invalid-response')
  }
  const v = shape(value, ['type', 'subscription_id'], ['topic', 'watermark', 'snapshot', 'reason'])
  const subscription_id = workID(v.subscription_id)
  if (v.type === 'unsubscribed') {
    shape(v, ['type', 'subscription_id'])
    return Object.freeze({ type: 'unsubscribed', subscription_id })
  }
  const currentTopic = topic(v.topic),
    currentWatermark = watermark(v.watermark)
  if (v.type === 'subscribed' || v.type === 'invalidated') {
    shape(v, ['type', 'subscription_id', 'topic', 'watermark'])
    return Object.freeze({
      type: v.type,
      subscription_id,
      topic: currentTopic,
      watermark: currentWatermark,
    })
  }
  if (v.type === 'snapshot') {
    shape(v, ['type', 'subscription_id', 'topic', 'watermark', 'snapshot'])
    return Object.freeze({
      type: 'snapshot',
      subscription_id,
      topic: currentTopic,
      watermark: currentWatermark,
      snapshot: await snapshot(v.snapshot, currentTopic),
    })
  }
  check(v.type === 'resync')
  shape(v, ['type', 'subscription_id', 'topic', 'watermark', 'reason'])
  check(v.reason === 'overflow' || v.reason === 'epoch_changed' || v.reason === 'unavailable')
  return Object.freeze({
    type: 'resync',
    subscription_id,
    topic: currentTopic,
    watermark: currentWatermark,
    reason: v.reason,
  })
}

export function meetingRealtimeClientMessage(
  type: 'subscribe' | 'snapshot' | 'unsubscribe',
  subscriptionID: string,
  value?: MeetingTopic,
) {
  try {
    const subscription_id = workID(subscriptionID)
    if (type === 'subscribe')
      return JSON.stringify({ type, subscription_id, topic: captureMeetingTopic(value) })
    check((type === 'snapshot' || type === 'unsubscribe') && value === undefined)
    return JSON.stringify({ type, subscription_id })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function newMeetingSubscriptionID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  let timestamp = BigInt(Date.now())
  if (timestamp < 0n || timestamp >= 1n << 48n) throw new AccountFailure('invalid-input')
  for (let n = 5; n >= 0; n--) {
    bytes[n] = Number(timestamp & 255n)
    timestamp >>= 8n
  }
  bytes[6] = (bytes[6]! & 15) | 0x70
  bytes[8] = (bytes[8]! & 63) | 0x80
  const hex = [...bytes].map((v) => v.toString(16).padStart(2, '0')).join('')
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join('-')
}
