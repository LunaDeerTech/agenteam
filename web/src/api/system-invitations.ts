import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'

export const deliveryPhases = [
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
export const deliveryReasons = [
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
export type InvitationDelivery = Readonly<{
  job_id: string
  accepted_at: string
  phase: (typeof deliveryPhases)[number]
  attempts: string
  version: string
  channel: 'smtp' | 'backend_log' | null
  attempt_result: 'sent' | 'failed' | 'unknown' | 'cancelled' | null
  reason?: (typeof deliveryReasons)[number]
}>
export type Invitation = Readonly<{
  id: string
  email: string
  version: string
  created_at: string
  expires_at: string
  latest_delivery: InvitationDelivery
}>
export type InvitationPage = Readonly<{ items: readonly Invitation[]; next_cursor?: string }>
export type InvitationQuery = Readonly<{ cursor?: string }>
export type InvitationCreate = Readonly<{ email: string }>
export type InvitationTarget = Readonly<{ id: string; version: string }>
export type DeliveryTarget = Readonly<{ job_id: string; version: string }>
export type InvitationReceipt = Readonly<{ id: string; job_id: string; version: string }>
export type DeliveryReceipt = Readonly<{ job_id: string; version: string }>
export type InvitationCommand =
  | Readonly<{ kind: 'create'; input: InvitationCreate }>
  | Readonly<{ kind: 'resend' | 'revoke'; input: InvitationTarget }>
  | Readonly<{ kind: 'retry'; input: DeliveryTarget }>
export type InvitationMutationResult =
  | Readonly<{ kind: 'create' | 'resend'; value: InvitationReceipt }>
  | Readonly<{ kind: 'revoke' }>
  | Readonly<{ kind: 'retry'; value: DeliveryReceipt }>
export interface SystemInvitationAPI {
  listInvitations(query: InvitationQuery, signal: AbortSignal): Promise<InvitationPage>
  createInvitation(input: InvitationCreate, options: WriteOptions): Promise<InvitationReceipt>
  resendInvitation(input: InvitationTarget, options: WriteOptions): Promise<InvitationReceipt>
  revokeInvitation(input: InvitationTarget, options: WriteOptions): Promise<void>
  retryDelivery(input: DeliveryTarget, options: WriteOptions): Promise<DeliveryReceipt>
}
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function version(value: unknown) {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= 9223372036854775807n)
  return result
}
function email(value: unknown) {
  const result = string(value, 3, 254)
  requireValue(/^[\x21-\x7e]+$/.test(result) && result.toLowerCase() === result)
  const parts = result.split('@')
  requireValue(parts.length === 2)
  const atom = /^[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*$/
  requireValue(atom.test(parts[0]!))
  const domain = parts[1]!
  if (domain.startsWith('[') && domain.endsWith(']')) {
    // Go's producer accepts any ParseIP with IPv6:, otherwise only To4
    // addresses (including mapped IPv6), then lowercases the original text.
    const literal = domain.slice(1, -1),
      prefixed = literal.startsWith('ipv6:')
    const address = prefixed ? literal.slice(5) : literal
    const ipv4 =
      /^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(address) &&
      address.split('.').every((part) => Number(part) <= 255)
    let ipv6 = false
    if (/^[0-9a-f:.]+$/.test(address) && address.includes(':')) {
      try {
        const host = new URL(`http://[${address}]/`).hostname
        ipv6 = prefixed || /^\[::ffff:[0-9a-f]{1,4}:[0-9a-f]{1,4}\]$/.test(host)
      } catch {
        /* Invalid literal. */
      }
    }
    requireValue(ipv4 || ipv6)
  } else requireValue(atom.test(domain))
  return result
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
// Used before creating an owner intent as well as by the API. Original email,
// target and expected version are preserved, with no trim or later substitution.
export function captureInvitationCommand(command: InvitationCommand): InvitationCommand {
  return input(() => {
    const c = shape(command, ['kind', 'input'])
    if (c.kind === 'create') {
      const v = shape(c.input, ['email'])
      return Object.freeze({
        kind: 'create',
        input: Object.freeze({ email: string(v.email, 1, 254) }),
      })
    }
    if (c.kind === 'resend' || c.kind === 'revoke') {
      const v = shape(c.input, ['id', 'version'])
      return Object.freeze({
        kind: c.kind,
        input: Object.freeze({ id: id(v.id), version: version(v.version) }),
      })
    }
    if (c.kind === 'retry') {
      const v = shape(c.input, ['job_id', 'version'])
      return Object.freeze({
        kind: 'retry',
        input: Object.freeze({ job_id: id(v.job_id), version: version(v.version) }),
      })
    }
    throw new AccountFailure('invalid-input')
  })
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
function delivery(value: unknown): InvitationDelivery {
  const v = shape(
    value,
    ['job_id', 'accepted_at', 'phase', 'attempts', 'version', 'channel', 'attempt_result'],
    ['reason'],
  )
  requireValue(deliveryPhases.includes(v.phase as InvitationDelivery['phase']))
  requireValue(typeof v.attempts === 'string' && /^[0-6]$/.test(v.attempts))
  requireValue(v.channel === null || v.channel === 'smtp' || v.channel === 'backend_log')
  requireValue(
    v.attempt_result === null ||
      ['sent', 'failed', 'unknown', 'cancelled'].includes(v.attempt_result as string),
  )
  if (Object.hasOwn(v, 'reason'))
    requireValue(deliveryReasons.includes(v.reason as NonNullable<InvitationDelivery['reason']>))
  // Match only facts expressible by the accepted projection; never invent an
  // io_joined flag or equate phase with the current attempt's outcome.
  if (v.channel === null) {
    requireValue(
      v.attempt_result === null &&
        !['claimed', 'sending', 'retry_wait', 'sent', 'failed'].includes(v.phase as string),
    )
  } else requireValue(v.attempts !== '0')
  if (v.phase === 'enqueue_pending')
    requireValue(
      v.attempts === '0' && v.version === '1' && v.channel === null && !Object.hasOwn(v, 'reason'),
    )
  return Object.freeze({
    job_id: id(v.job_id),
    accepted_at: parseSystemInstant(v.accepted_at),
    phase: v.phase as InvitationDelivery['phase'],
    attempts: v.attempts as string,
    version: version(v.version),
    channel: v.channel as InvitationDelivery['channel'],
    attempt_result: v.attempt_result as InvitationDelivery['attempt_result'],
    ...(Object.hasOwn(v, 'reason')
      ? { reason: v.reason as NonNullable<InvitationDelivery['reason']> }
      : {}),
  })
}
function page(value: unknown): InvitationPage {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= 25)
  const ids = new Set<string>()
  let previous: Invitation | undefined
  const items = (v.items as unknown[]).map((value) => {
    const row = shape(value, [
      'id',
      'email',
      'version',
      'created_at',
      'expires_at',
      'latest_delivery',
    ])
    const item = Object.freeze({
      id: id(row.id),
      email: email(row.email),
      version: version(row.version),
      created_at: parseSystemInstant(row.created_at),
      expires_at: parseSystemInstant(row.expires_at),
      latest_delivery: delivery(row.latest_delivery),
    })
    requireValue(
      !ids.has(item.id) &&
        (!previous ||
          item.created_at < previous.created_at ||
          (item.created_at === previous.created_at && item.id < previous.id)),
    )
    ids.add(item.id)
    previous = item
    return item
  })
  const cursor = Object.hasOwn(v, 'next_cursor') ? string(v.next_cursor, 1, 8192) : undefined
  requireValue(cursor === undefined || items.length === 25)
  return Object.freeze({
    items: Object.freeze(items),
    ...(cursor === undefined ? {} : { next_cursor: cursor }),
  })
}
function receipt(value: unknown): InvitationReceipt {
  const v = shape(value, ['id', 'job_id', 'version'])
  return Object.freeze({ id: id(v.id), job_id: id(v.job_id), version: version(v.version) })
}
export function createSystemInvitationAPI(fetcher?: Fetch): SystemInvitationAPI {
  const request = accountTransport(fetcher)
  return {
    listInvitations(query, signal) {
      return request('systemInvitations', page, { invitations: query, signal })
    },
    createInvitation(value, opts) {
      const c = captureInvitationCommand({ kind: 'create', input: value })
      return request('createSystemInvitation', receipt, { body: c.input, ...options(opts) })
    },
    resendInvitation(value, opts) {
      const c = captureInvitationCommand({ kind: 'resend', input: value }) as Extract<
        InvitationCommand,
        { kind: 'resend' | 'revoke' }
      >
      return request(
        'resendSystemInvitation',
        (value) => {
          const result = receipt(value)
          requireValue(result.id === c.input.id)
          return result
        },
        { target: c.input.id, body: { version: c.input.version }, ...options(opts) },
      )
    },
    revokeInvitation(value, opts) {
      const c = captureInvitationCommand({ kind: 'revoke', input: value }) as Extract<
        InvitationCommand,
        { kind: 'resend' | 'revoke' }
      >
      return request('revokeSystemInvitation', () => undefined, {
        target: c.input.id,
        body: { version: c.input.version },
        ...options(opts),
      })
    },
    retryDelivery(value, opts) {
      const c = captureInvitationCommand({ kind: 'retry', input: value }) as Extract<
        InvitationCommand,
        { kind: 'retry' }
      >
      return request(
        'retrySystemDelivery',
        (value) => {
          const v = shape(value, ['job_id', 'version']),
            job = id(v.job_id)
          requireValue(job !== c.input.job_id)
          return Object.freeze({ job_id: job, version: version(v.version) })
        },
        { target: c.input.job_id, body: { version: c.input.version }, ...options(opts) },
      )
    },
  }
}
