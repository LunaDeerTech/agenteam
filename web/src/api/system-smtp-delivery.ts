import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'
import { captureSMTPEmail } from './system-smtp-settings'

export const smtpDeliveryPhases = [
  'enqueue_pending',
  'pending',
  'claimed',
  'sending',
  'retry_wait',
  'sent',
  'failed',
  'unknown',
  'cancelled',
] as const
export const smtpDeliveryReasons = [
  'sent',
  'token_invalid',
  'configuration_invalid',
  'policy_rejected',
  'network_failed',
  'smtp_rejected',
  'timeout',
  'cancelled',
  'unknown',
] as const
export type SMTPDeliveryJob = Readonly<{
  job_id: string
  kind: 'invitation' | 'password_reset' | 'test'
  phase: (typeof smtpDeliveryPhases)[number]
  attempts: string
  version: string
  channel: 'smtp' | 'backend_log' | null
  created_at: string
  attempt_channel: 'smtp' | 'backend_log' | null
  attempt_result: 'sent' | 'failed' | 'unknown' | 'cancelled' | null
  reason?: (typeof smtpDeliveryReasons)[number]
}>
export type SMTPDeliveryQuery = Readonly<{ cursor?: string }>
export type SMTPDeliveryPage = Readonly<{
  items: readonly SMTPDeliveryJob[]
  next_cursor?: string
}>
export type SMTPTestInput = Readonly<{ recipient: string }>
export type SMTPRetryInput = Readonly<{ job_id: string; version: string }>
export type SMTPTestReceipt = Readonly<{ job_id: string }>
export type SMTPRetryReceipt = Readonly<{ job_id: string; version: string }>
export type SMTPDeliveryCommand =
  | Readonly<{ kind: 'test'; input: SMTPTestInput }>
  | Readonly<{ kind: 'retry'; input: SMTPRetryInput }>
export type SMTPDeliveryResult =
  | Readonly<{ kind: 'test'; value: SMTPTestReceipt }>
  | Readonly<{ kind: 'retry'; value: SMTPRetryReceipt }>
export interface SystemSMTPDeliveryAPI {
  testSMTP(input: SMTPTestInput, options: WriteOptions): Promise<SMTPTestReceipt>
  listJobs(query: SMTPDeliveryQuery, signal: AbortSignal): Promise<SMTPDeliveryPage>
  getJob(jobID: string, signal: AbortSignal): Promise<SMTPDeliveryJob>
  retryJob(input: SMTPRetryInput, options: WriteOptions): Promise<SMTPRetryReceipt>
}

const encoder = new TextEncoder()
const maximumVersion = 9223372036854775807n
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function version(value: unknown, maximum = maximumVersion) {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= maximum)
  return result
}
function cursor(value: unknown) {
  const result = string(value, 1, 8192)
  requireValue(encoder.encode(result).byteLength <= 8192)
  return result
}
function query(value: SMTPDeliveryQuery): SMTPDeliveryQuery {
  const v = shape(value, [], ['cursor'])
  return Object.freeze(Object.hasOwn(v, 'cursor') ? { cursor: cursor(v.cursor) } : {})
}
function options(value: WriteOptions) {
  return input(() => {
    const v = shape(value, ['csrfToken', 'key'], ['signal'])
    const csrf = string(v.csrfToken, 43, 43),
      key = string(v.key, 1, 128)
    requireValue(/^[A-Za-z0-9_-]{43}$/.test(csrf) && /^[A-Za-z0-9._:/-]{1,128}$/.test(key))
    return { csrf, key, signal: value.signal ?? new AbortController().signal }
  })
}

// Capture before creating a private intent/key. Do not canonicalize the original
// recipient or replace a retry's source/version with a later observation.
export function captureSMTPDeliveryCommand(command: SMTPDeliveryCommand): SMTPDeliveryCommand {
  return input(() => {
    const c = shape(command, ['kind', 'input'])
    if (c.kind === 'test') {
      const v = shape(c.input, ['recipient'])
      const captured = Object.freeze({ recipient: captureSMTPEmail(v.recipient) })
      requireValue(encoder.encode(JSON.stringify(captured)).byteLength <= 16 * 1024)
      return Object.freeze({ kind: 'test', input: captured })
    }
    if (c.kind === 'retry') {
      const v = shape(c.input, ['job_id', 'version'])
      const captured = Object.freeze({
        job_id: id(v.job_id),
        version: version(v.version, maximumVersion - 1n),
      })
      requireValue(
        encoder.encode(JSON.stringify({ version: captured.version })).byteLength <= 16 * 1024,
      )
      return Object.freeze({ kind: 'retry', input: captured })
    }
    throw new AccountFailure('invalid-input')
  })
}

function job(value: unknown): SMTPDeliveryJob {
  const v = shape(
    value,
    [
      'job_id',
      'kind',
      'phase',
      'attempts',
      'version',
      'channel',
      'created_at',
      'attempt_channel',
      'attempt_result',
    ],
    ['reason'],
  )
  requireValue(['invitation', 'password_reset', 'test'].includes(v.kind as string))
  requireValue(smtpDeliveryPhases.includes(v.phase as SMTPDeliveryJob['phase']))
  requireValue(typeof v.attempts === 'string' && /^[0-6]$/.test(v.attempts))
  requireValue(v.channel === null || v.channel === 'smtp' || v.channel === 'backend_log')
  requireValue(
    v.attempt_channel === null ||
      v.attempt_channel === 'smtp' ||
      v.attempt_channel === 'backend_log',
  )
  requireValue(
    v.attempt_result === null ||
      ['sent', 'failed', 'unknown', 'cancelled'].includes(v.attempt_result as string),
  )
  if (Object.hasOwn(v, 'reason'))
    requireValue(smtpDeliveryReasons.includes(v.reason as NonNullable<SMTPDeliveryJob['reason']>))
  if (v.attempt_channel === null) {
    requireValue(
      v.attempt_result === null &&
        !['claimed', 'sending', 'retry_wait', 'sent', 'failed'].includes(v.phase as string),
    )
  } else requireValue(v.attempt_channel === v.channel && v.attempts !== '0')
  if (v.phase === 'enqueue_pending')
    requireValue(
      v.attempts === '0' &&
        v.version === '1' &&
        v.attempt_channel === null &&
        !Object.hasOwn(v, 'reason'),
    )
  // Only the exact current attempt is projected. Its result is independent from
  // the job phase; compatibility channel alone does not establish any attempt.
  return Object.freeze({
    job_id: id(v.job_id),
    kind: v.kind as SMTPDeliveryJob['kind'],
    phase: v.phase as SMTPDeliveryJob['phase'],
    attempts: v.attempts as string,
    version: version(v.version),
    channel: v.channel as SMTPDeliveryJob['channel'],
    created_at: parseSystemInstant(v.created_at),
    attempt_channel: v.attempt_channel as SMTPDeliveryJob['attempt_channel'],
    attempt_result: v.attempt_result as SMTPDeliveryJob['attempt_result'],
    ...(Object.hasOwn(v, 'reason')
      ? { reason: v.reason as NonNullable<SMTPDeliveryJob['reason']> }
      : {}),
  })
}
function page(value: unknown): SMTPDeliveryPage {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= 25)
  const ids = new Set<string>()
  let previous: SMTPDeliveryJob | undefined
  const items = (v.items as unknown[]).map((value) => {
    const item = job(value)
    requireValue(!ids.has(item.job_id))
    ids.add(item.job_id)
    if (previous)
      requireValue(
        previous.created_at > item.created_at ||
          (previous.created_at === item.created_at && previous.job_id > item.job_id),
      )
    previous = item
    return item
  })
  if (Object.hasOwn(v, 'next_cursor')) requireValue(items.length === 25)
  return Object.freeze({
    items: Object.freeze(items),
    ...(Object.hasOwn(v, 'next_cursor') ? { next_cursor: cursor(v.next_cursor) } : {}),
  })
}

export function createSystemSMTPDeliveryAPI(fetcher?: Fetch): SystemSMTPDeliveryAPI {
  const request = accountTransport(fetcher)
  return {
    async testSMTP(value, write) {
      const captured = captureSMTPDeliveryCommand({ kind: 'test', input: value })
      return request(
        'testSMTP',
        (value) => Object.freeze({ job_id: id(shape(value, ['job_id']).job_id) }),
        { ...options(write), body: captured.input },
      )
    },
    async listJobs(value, signal) {
      return request('listMailJobManagement', page, { signal, mailJobs: input(() => query(value)) })
    },
    async getJob(value, signal) {
      const target = input(() => id(value))
      return request(
        'getMailJobManagement',
        (value) => {
          const result = job(value)
          requireValue(result.job_id === target)
          return result
        },
        { signal, target },
      )
    },
    async retryJob(value, write) {
      const captured = captureSMTPDeliveryCommand({ kind: 'retry', input: value })
      if (captured.kind !== 'retry') throw new AccountFailure('invalid-input')
      return request(
        'retrySystemDelivery',
        (value) => {
          const v = shape(value, ['job_id', 'version']),
            job_id = id(v.job_id)
          requireValue(job_id !== captured.input.job_id)
          return Object.freeze({ job_id, version: version(v.version) })
        },
        {
          ...options(write),
          target: captured.input.job_id,
          body: { version: captured.input.version },
        },
      )
    },
  }
}
