import { AccountFailure, object, shape, uuid7 } from './client'

type Parser<T> = (value: unknown) => T
type Fields = Readonly<Record<string, Parser<unknown>>>
type Parsed<F extends Fields> = { readonly [K in keyof F]: ReturnType<F[K]> }
export function requireAudit(valid: unknown): asserts valid {
  if (!valid) throw new AccountFailure('invalid-response')
}
function oneOf<const T extends readonly string[]>(allowed: T): Parser<T[number]> {
  return (value) => {
    requireAudit(typeof value === 'string' && allowed.includes(value))
    return value as T[number]
  }
}
function literal<const T extends string>(expected: T): Parser<T> {
  return oneOf([expected])
}
function record<R extends Fields, O extends Fields = Record<never, never>>(
  required: R,
  optional: O = {} as O,
): Parser<Readonly<Parsed<R> & Partial<Parsed<O>>>> {
  return (value) => {
    const source = shape(value, Object.keys(required), Object.keys(optional))
    const result: Record<string, unknown> = {}
    for (const [key, parse] of Object.entries(required)) result[key] = parse(source[key])
    for (const [key, parse] of Object.entries(optional)) {
      if (Object.hasOwn(source, key)) result[key] = parse(source[key])
    }
    return Object.freeze(result) as Readonly<Parsed<R> & Partial<Parsed<O>>>
  }
}
export const auditID: Parser<string> = (value) => {
  requireAudit(typeof value === 'string' && uuid7.test(value))
  return value
}
const maximumInt64 = '9223372036854775807'
function integer(positive: boolean): Parser<string> {
  return (value) => {
    requireAudit(typeof value === 'string' && /^(0|[1-9][0-9]*)$/.test(value))
    requireAudit(!positive || value !== '0')
    requireAudit(value.length < 19 || (value.length === 19 && value <= maximumInt64))
    return value
  }
}
const version = integer(true),
  progress = integer(false)
function fields<const T extends readonly string[]>(allowed: T): Parser<readonly T[number][]> {
  return (value) => {
    requireAudit(Array.isArray(value) && value.length > 0 && value.length <= allowed.length)
    const result = value.map(oneOf(allowed))
    requireAudit(result.every((entry, index) => index === 0 || result[index - 1]! < entry))
    return Object.freeze(result)
  }
}
function exactFields<const T extends readonly string[]>(expected: T): Parser<Readonly<T>> {
  return (value) => {
    const parsed = fields(expected)(value)
    requireAudit(parsed.length === expected.length && parsed.every((v, i) => v === expected[i]))
    return parsed as Readonly<T>
  }
}

export const auditConsumers = [
  'model',
  'mcp',
  'runner',
  'smtp',
  'object_storage',
  'system',
] as const
export const auditReasons = [
  'permission_denied',
  'scope_mismatch',
  'lease_invalid',
  'decrypt_failed',
  'key_unavailable',
  'ciphertext_invalid',
  'rotation_failed',
  'invalid_target',
  'address_forbidden',
  'private_not_allowed',
  'port_denied',
  'http_denied',
  'tls_failed',
  'dns_failed',
  'redirect_denied',
  'policy_unavailable',
  'binding_invalid',
  'consumer_denied',
  'credential_denied',
  'origin_denied',
  'response_limit',
  'timeout',
  'cancelled',
  'internal_error',
  'storage_unavailable',
  'payload_missing',
  'integrity_mismatch',
] as const
const consumer = oneOf(auditConsumers),
  reason = oneOf(auditReasons)
const channel = oneOf(['smtp', 'log'] as const)
const deliveryReason = oneOf([
  'delivery_rejected',
  'timeout',
  'cancelled',
  'delivery_unknown',
] as const)
const loginReason = oneOf([
  'credentials_rejected',
  'challenge_required',
  'challenge_invalid',
  'session_invalid',
] as const)

// Go's MIME grammar permits a single token and {}. Canonical Parse/Format does
// not require a slash. Parameters remain the contract's two charset values.
export const auditMediaType: Parser<string> = (value) => {
  requireAudit(typeof value === 'string' && new TextEncoder().encode(value).byteLength <= 256)
  requireAudit(
    /^[!#$%&'*+.^_`|~{}0-9a-z-]+(?:\/[!#$%&'*+.^_`|~{}0-9a-z-]+)?(?:; charset=(?:utf-8|us-ascii))?$/.test(
      value,
    ),
  )
  return value
}
export function auditMetadataJSONBytes(value: unknown): number {
  const encoded = JSON.stringify(value)
  requireAudit(typeof encoded === 'string')
  // Count the actual safe projection as Go's default json.Marshal would encode
  // it, including HTML and line-separator escapes rather than JS-only bytes.
  return new TextEncoder().encode(
    encoded.replace(
      /[<>&\u2028\u2029]/g,
      (char) => '\\u' + char.charCodeAt(0).toString(16).padStart(4, '0'),
    ),
  ).byteLength
}

type ContentInitiator =
  | Readonly<{ initiator_kind: 'human' | 'service'; initiator_id: string }>
  | Readonly<{ initiator_kind: 'agent_run'; initiator_id: string; initiator_execution_id: string }>
type ObjectMetadata<P extends string, R extends boolean> = Readonly<{
  object_id: string
  media_type: string
  byte_size: string
  sent_bytes: string
  phase: P
}> &
  ContentInitiator &
  (R extends true ? Readonly<{ reason: (typeof auditReasons)[number] }> : object)
function content<P extends string, R extends boolean>(
  phase: P,
  withReason: R,
): Parser<ObjectMetadata<P, R>> {
  return (value) => {
    const raw = object(value)
    const initiator = oneOf(['human', 'agent_run', 'service'] as const)(raw.initiator_kind)
    const parsed = record({
      object_id: auditID,
      initiator_kind: literal(initiator),
      initiator_id: auditID,
      ...(initiator === 'agent_run' ? { initiator_execution_id: auditID } : {}),
      media_type: auditMediaType,
      byte_size: progress,
      sent_bytes: progress,
      phase: literal(phase),
      ...(withReason ? { reason } : {}),
    })(value)
    requireAudit(BigInt(parsed.sent_bytes) <= BigInt(parsed.byte_size))
    return parsed as ObjectMetadata<P, R>
  }
}
function login(value: unknown) {
  const phase = object(value).phase
  if (phase === 'authenticated')
    return record({
      version,
      phase: literal('authenticated'),
      attempt_id: auditID,
      user_id: auditID,
      session_id: auditID,
    })(value)
  return record(
    { version, phase: literal('rejected'), attempt_id: auditID, reason: loginReason },
    { user_id: auditID },
  )(value)
}
function delivery(value: unknown) {
  const rawPhase = object(value).phase
  if (rawPhase === 'sent')
    return record(
      { version, phase: literal('sent'), job_id: auditID, attempt_id: auditID, channel },
      { initiator_id: auditID },
    )(value)
  const phase = oneOf(['failed', 'unknown'] as const)(rawPhase)
  return record(
    {
      version,
      phase: literal(phase),
      job_id: auditID,
      attempt_id: auditID,
      channel,
      reason: deliveryReason,
    },
    { initiator_id: auditID },
  )(value)
}
function modelDelete(value: unknown) {
  const parsed = record(
    {
      provider_id: auditID,
      model_id: auditID,
      version,
      changed_fields: fields(['deleted', 'replacement'] as const),
      affected_count: progress,
    },
    { replacement_id: auditID },
  )(value)
  requireAudit(
    parsed.changed_fields.join(',') === (parsed.replacement_id ? 'deleted,replacement' : 'deleted'),
  )
  return parsed
}
const providerChanges = [
  'name',
  'enabled',
  'base_url',
  'credential_ref',
  'provider_options',
] as const
const modelChanges = [
  'name',
  'enabled',
  'model_id',
  'parameters',
  'request_overwrite',
  'header_overwrite',
  'capabilities',
] as const

const parsers = {
  'secret.create': record({ version, changed_fields: fields(['value', 'purpose'] as const) }),
  'secret.update': record({ version, changed_fields: fields(['value', 'purpose'] as const) }),
  'secret.delete': record({ version }),
  'secret.resolve': record({ lease_id: auditID, consumer }, { reason }),
  'secret.master.register': record({ version }),
  'secret.master.rotation.start': record({ version, rotation_id: auditID, count: progress }),
  'secret.master.rotation.complete': record({ version, rotation_id: auditID, count: progress }),
  'secret.master.rotation.failed': record({
    version,
    rotation_id: auditID,
    count: progress,
    reason,
  }),
  'outbound.policy.update': record({
    version,
    rule_count: (value: unknown) => {
      const count = progress(value)
      requireAudit(BigInt(count) <= 256n)
      return count
    },
  }),
  'outbound.access.deny': record({ version, consumer, reason }),
  'object.upload.complete': content('published', false),
  'object.upload.failed': content('failed', true),
  'object.delete': content('deleted', false),
  'outbox.delivery.requeue': record({
    delivery_id: auditID,
    event_id: auditID,
    handler_id: (value: unknown) => {
      requireAudit(typeof value === 'string' && /^[a-z][a-z0-9_.-]{0,127}$/.test(value))
      return value
    },
    from_state: oneOf(['failed', 'dead_letter'] as const),
    redrive_cycle: version,
    reason_code: oneOf(['operator_retry', 'schema_available', 'dependency_restored'] as const),
  }),
  'account.bootstrap': record({ version, phase: literal('created'), user_id: auditID }),
  'account.login': login,
  'account.logout': record({
    version,
    phase: literal('revoked'),
    user_id: auditID,
    session_id: auditID,
  }),
  'account.invite.create': record(
    { version, phase: literal('created'), invitation_id: auditID },
    { initiator_id: auditID, channel },
  ),
  'account.invite.revoke': record(
    { version, phase: literal('revoked'), invitation_id: auditID },
    { initiator_id: auditID },
  ),
  'account.invite.redeem': record({
    version,
    phase: literal('redeemed'),
    user_id: auditID,
    invitation_id: auditID,
  }),
  'account.password.change': record({
    version,
    phase: literal('updated'),
    user_id: auditID,
    changed_fields: exactFields(['password'] as const),
  }),
  'account.password.reset.request': record(
    { version, phase: literal('accepted'), attempt_id: auditID },
    { user_id: auditID },
  ),
  'account.password.reset.complete': record({
    version,
    phase: literal('redeemed'),
    user_id: auditID,
    reset_id: auditID,
  }),
  'account.profile.update': record({
    version,
    phase: literal('updated'),
    user_id: auditID,
    changed_fields: fields(['username', 'display_name', 'theme'] as const),
  }),
  'account.avatar.update': record(
    {
      version,
      phase: literal('updated'),
      user_id: auditID,
      changed_fields: exactFields(['avatar'] as const),
    },
    { object_id: auditID },
  ),
  'account.settings.update': record(
    {
      version,
      phase: literal('updated'),
      changed_fields: fields([
        'session_idle_seconds',
        'session_absolute_seconds',
        'password_reset_seconds',
        'challenge_after_failures',
      ] as const),
    },
    { initiator_id: auditID },
  ),
  'smtp.settings.update': record(
    {
      version,
      phase: literal('updated'),
      changed_fields: fields([
        'host',
        'port',
        'tls_mode',
        'auth_username',
        'password',
        'from',
        'enabled',
        'sender_name',
        'auto_retry_count',
        'retry_interval_seconds',
      ] as const),
    },
    { initiator_id: auditID },
  ),
  'smtp.test.request': record(
    { version, phase: literal('accepted'), job_id: auditID },
    { initiator_id: auditID, channel },
  ),
  'smtp.delivery.retry': record({
    version,
    phase: literal('accepted'),
    job_id: auditID,
    initiator_id: auditID,
  }),
  'smtp.delivery': delivery,
  'provider.create': record({
    provider_id: auditID,
    version,
    changed_fields: exactFields(['created'] as const),
  }),
  'provider.update': record({
    provider_id: auditID,
    version,
    changed_fields: fields(providerChanges),
  }),
  'provider.delete': record({
    provider_id: auditID,
    version,
    changed_fields: exactFields(['deleted'] as const),
  }),
  'model.create': record({
    provider_id: auditID,
    model_id: auditID,
    version,
    changed_fields: exactFields(['created'] as const),
  }),
  'model.update': record({
    provider_id: auditID,
    model_id: auditID,
    version,
    changed_fields: fields(modelChanges),
  }),
  'model.delete': modelDelete,
  'model.selection.update': record({
    selection_id: auditID,
    version,
    selector_kind: literal('platform'),
    changed_fields: exactFields(['selection'] as const),
  }),
} as const

export type SystemAuditAction = keyof typeof parsers
export type AuditMetadataByAction = {
  readonly [A in SystemAuditAction]: ReturnType<(typeof parsers)[A]>
}
export type TypedAuditMetadata = {
  readonly [A in SystemAuditAction]: Readonly<{ action: A; metadata: AuditMetadataByAction[A] }>
}[SystemAuditAction]
export const systemAuditActions = Object.freeze(Object.keys(parsers) as SystemAuditAction[])
export function parseAuditMetadata(action: unknown, value: unknown): TypedAuditMetadata {
  requireAudit(typeof action === 'string' && Object.hasOwn(parsers, action))
  const parsed = parsers[action as SystemAuditAction](value)
  requireAudit(auditMetadataJSONBytes(parsed) <= 4096)
  return Object.freeze({ action, metadata: parsed }) as TypedAuditMetadata
}

export const auditServices = [
  'secret',
  'secret-maintenance',
  'outbound',
  'object',
  'object-maintenance',
  'project-lifecycle',
  'project-initialization',
  'outbox-delivery',
  'account-bootstrap',
  'account-auth',
  'account-maintenance',
  'account-mail',
  'model-runtime',
] as const
export const systemAuditResourceKinds = [
  'secret',
  'secret_master',
  'secret_rotation',
  'outbound_policy',
  'stored_object',
  'outbox_delivery',
  'user',
  'session',
  'account_attempt',
  'invitation',
  'password_reset',
  'account_settings',
  'smtp_settings',
  'mail_job',
  'model_provider',
  'model_config',
  'model_selection',
] as const
export const auditOutcomes = ['success', 'denied', 'failed', 'unknown'] as const
export type AuditOutcome = (typeof auditOutcomes)[number]
export type AuditActor =
  | Readonly<{ kind: 'human'; id: string }>
  | Readonly<{ kind: 'service'; service: (typeof auditServices)[number]; cause_ref: string }>
export type AuditResource =
  | Readonly<{ kind: 'secret_master' | 'outbound_policy' }>
  | Readonly<{
      kind: Exclude<(typeof systemAuditResourceKinds)[number], 'secret_master' | 'outbound_policy'>
      id: string
    }>
export const auditAssociationFields = [
  'tool_id',
  'request_id',
  'runner_id',
  'correlation_id',
  'http_trace_id',
] as const
export type AuditAssociations = Readonly<
  Partial<Record<(typeof auditAssociationFields)[number], string>>
>
export function parseAuditActor(value: unknown): AuditActor {
  if (object(value).kind === 'human') return record({ kind: literal('human'), id: auditID })(value)
  return record({
    kind: literal('service'),
    service: oneOf(auditServices),
    cause_ref: (value: unknown) => {
      requireAudit(
        typeof value === 'string' && (uuid7.test(value) || /^sha256:[0-9a-f]{64}$/.test(value)),
      )
      return value
    },
  })(value)
}
export function parseAuditResource(value: unknown): AuditResource {
  const kind = oneOf(systemAuditResourceKinds)(object(value).kind)
  if (kind === 'secret_master' || kind === 'outbound_policy')
    return record({ kind: literal(kind) })(value)
  return record({ kind: literal(kind), id: auditID })(value)
}
export function parseAuditAssociations(value: unknown): AuditAssociations {
  return record(
    {},
    {
      tool_id: auditID,
      request_id: auditID,
      runner_id: auditID,
      correlation_id: auditID,
      http_trace_id: auditID,
    },
  )(value)
}
export function auditSummary(action: SystemAuditAction): string {
  const summaries: Partial<Record<SystemAuditAction, string>> = {
    'secret.create': 'Secret created',
    'secret.update': 'Secret updated',
    'secret.delete': 'Secret deleted',
    'secret.resolve': 'Secret use recorded',
    'secret.master.register': 'Master key registered',
    'secret.master.rotation.start': 'Master key rotation started',
    'secret.master.rotation.complete': 'Master key rotation completed',
    'secret.master.rotation.failed': 'Master key rotation failed',
    'outbound.policy.update': 'Outbound policy updated',
    'outbound.access.deny': 'Outbound access denied',
  }
  return summaries[action] ?? 'Audit event'
}

export function validateAuditRelations(
  entry: TypedAuditMetadata,
  context: Readonly<{
    actor: AuditActor
    outcome: AuditOutcome
    resource: AuditResource
    associations: AuditAssociations
  }>,
): void {
  const { action, metadata: m } = entry
  const { actor, outcome, resource, associations } = context
  const same = (kind: string, id?: string) =>
    resource.kind === kind && ('id' in resource ? resource.id : undefined) === id
  if (action.startsWith('account.') || action.startsWith('smtp.')) {
    // All branches above have already validated their distinct phase/field shape.
    const metadata = m as AuditMetadataByAction['account.bootstrap'] &
      Partial<{
        attempt_id: string
        session_id: string
        invitation_id: string
        reset_id: string
        job_id: string
      }>
    let kind = 'user',
      id: string | undefined = metadata.user_id
    switch (action) {
      case 'account.login':
      case 'account.password.reset.request':
        kind = 'account_attempt'
        id = metadata.attempt_id
        break
      case 'account.logout':
        kind = 'session'
        id = metadata.session_id
        break
      case 'account.invite.create':
      case 'account.invite.revoke':
        kind = 'invitation'
        id = metadata.invitation_id
        break
      case 'account.password.reset.complete':
        kind = 'password_reset'
        id = metadata.reset_id
        break
      case 'account.settings.update':
        kind = 'account_settings'
        id = 'id' in resource ? resource.id : undefined
        break
      case 'smtp.settings.update':
        kind = 'smtp_settings'
        id = 'id' in resource ? resource.id : undefined
        break
      case 'smtp.test.request':
      case 'smtp.delivery.retry':
      case 'smtp.delivery':
        kind = 'mail_job'
        id = metadata.job_id
        break
    }
    const phase: string = metadata.phase
    const expected =
      phase === 'rejected'
        ? 'denied'
        : phase === 'failed'
          ? 'failed'
          : phase === 'unknown'
            ? 'unknown'
            : 'success'
    requireAudit(
      same(kind, id) &&
        outcome === expected &&
        (action !== 'smtp.delivery.retry' || actor.kind === 'human'),
    )
    return
  }
  if (action.startsWith('provider.') || action.startsWith('model.')) {
    requireAudit(
      actor.kind === 'human' &&
        outcome === 'success' &&
        !associations.tool_id &&
        !associations.request_id &&
        !associations.runner_id,
    )
    if (action === 'model.selection.update')
      requireAudit(
        same(
          'model_selection',
          (m as AuditMetadataByAction['model.selection.update']).selection_id,
        ),
      )
    else if (action.startsWith('provider.'))
      requireAudit(
        same('model_provider', (m as AuditMetadataByAction['provider.create']).provider_id),
      )
    else requireAudit(same('model_config', (m as AuditMetadataByAction['model.create']).model_id))
    return
  }
  switch (entry.action) {
    case 'secret.create':
    case 'secret.update':
    case 'secret.delete':
    case 'secret.resolve':
      requireAudit(resource.kind === 'secret')
      break
    case 'secret.master.register':
      requireAudit(same('secret_master') && outcome === 'success')
      break
    case 'secret.master.rotation.start':
    case 'secret.master.rotation.complete':
    case 'secret.master.rotation.failed':
      requireAudit(
        same('secret_rotation', entry.metadata.rotation_id) &&
          outcome === (entry.action === 'secret.master.rotation.failed' ? 'failed' : 'success'),
      )
      break
    case 'outbound.policy.update':
      requireAudit(same('outbound_policy'))
      break
    case 'outbound.access.deny':
      requireAudit(
        (resource.kind === 'outbound_policy' || resource.kind === 'secret') && outcome === 'denied',
      )
      break
    case 'object.upload.complete':
    case 'object.upload.failed':
    case 'object.delete':
      requireAudit(
        same('stored_object', entry.metadata.object_id) &&
          actor.kind === 'service' &&
          ['object', 'object-maintenance'].includes(actor.service),
      )
      requireAudit(
        entry.action === 'object.upload.failed'
          ? outcome === 'failed' || outcome === 'unknown'
          : outcome === 'success',
      )
      break
    case 'outbox.delivery.requeue':
      requireAudit(
        same('outbox_delivery', entry.metadata.delivery_id) &&
          actor.kind === 'human' &&
          outcome === 'success',
      )
      break
    default:
      throw new AccountFailure('invalid-response')
  }
}

const metadataLabels = [
  ['version', '版本'],
  ['user_id', 'User ID'],
  ['session_id', 'Session ID'],
  ['attempt_id', 'Attempt ID'],
  ['invitation_id', 'Invitation ID'],
  ['reset_id', 'Reset ID'],
  ['job_id', 'Job ID'],
  ['provider_id', 'Provider ID'],
  ['model_id', 'Model ID'],
  ['selection_id', 'Selection ID'],
  ['replacement_id', 'Replacement ID'],
  ['selector_kind', '选择器类型'],
  ['affected_count', '影响引用数'],
  ['changed_fields', '变更字段'],
  ['channel', '渠道'],
  ['phase', '阶段'],
  ['reason', '原因'],
  ['initiator_id', 'Initiator ID'],
  ['initiator_kind', '发起者类型'],
  ['initiator_execution_id', '发起者 Execution ID'],
  ['lease_id', 'Lease ID'],
  ['consumer', '使用方'],
  ['rotation_id', 'Rotation ID'],
  ['count', '数量'],
  ['rule_count', '规则数量'],
  ['object_id', 'Object ID'],
  ['media_type', '媒体类型'],
  ['byte_size', '字节数'],
  ['sent_bytes', '已传输字节数'],
  ['delivery_id', 'Delivery ID'],
  ['event_id', 'Event ID'],
  ['handler_id', '处理器'],
  ['from_state', '原状态'],
  ['redrive_cycle', '重新投递周期'],
  ['reason_code', '重新投递原因'],
] as const
export function auditMetadataFields(
  entry: TypedAuditMetadata,
): readonly Readonly<{ key: string; label: string; value: string }>[] {
  const metadata = entry.metadata as Readonly<Record<string, string | readonly string[]>>
  return Object.freeze(
    metadataLabels.flatMap(([key, label]) => {
      if (!Object.hasOwn(metadata, key)) return []
      const value = metadata[key]!
      return [
        Object.freeze({ key, label, value: typeof value === 'string' ? value : value.join(', ') }),
      ]
    }),
  )
}
