import { AccountFailure, object, shape, uuid7 } from './client'
import {
  auditConsumers,
  auditID,
  auditMediaType,
  auditMetadataJSONBytes,
  auditReasons,
  auditServices,
  requireAudit,
  type AuditOutcome,
} from './system-audit-metadata'

type Parser<T> = (value: unknown) => T
type Fields = Readonly<Record<string, Parser<unknown>>>
type Parsed<F extends Fields> = { readonly [K in keyof F]: ReturnType<F[K]> }
function oneOf<const T extends readonly string[]>(values: T): Parser<T[number]> {
  return (value) => {
    requireAudit(typeof value === 'string' && values.includes(value))
    return value as T[number]
  }
}
function literal<const T extends string>(value: T): Parser<T> {
  return oneOf([value])
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
function integer(positive: boolean): Parser<string> {
  return (value) => {
    requireAudit(typeof value === 'string' && /^(0|[1-9][0-9]*)$/.test(value))
    requireAudit(!positive || value !== '0')
    requireAudit(value.length < 19 || (value.length === 19 && value <= '9223372036854775807'))
    return value
  }
}
const version = integer(true),
  progress = integer(false),
  reason = oneOf(auditReasons),
  consumer = oneOf(auditConsumers)
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
    const result = fields(expected)(value)
    requireAudit(result.length === expected.length && result.every((v, i) => v === expected[i]))
    return result as Readonly<T>
  }
}

const objectPhases = {
  'object.upload.complete': 'published',
  'object.upload.failed': 'failed',
  'object.delete': 'deleted',
  'object.transfer.issue': 'issued',
  'object.transfer.complete': 'sent',
  'object.transfer.revoke': 'revoked',
} as const
type ObjectAction = keyof typeof objectPhases
type ContentInitiator =
  | Readonly<{ initiator_kind: 'human' | 'service'; initiator_id: string }>
  | Readonly<{ initiator_kind: 'agent_run'; initiator_id: string; initiator_execution_id: string }>
type ObjectMetadata<A extends ObjectAction> = Readonly<{
  object_id: string
  media_type: string
  byte_size: string
  sent_bytes: string
  phase: (typeof objectPhases)[A]
}> &
  ContentInitiator &
  (A extends `object.transfer.${string}` ? Readonly<{ transfer_id: string }> : object) &
  (A extends 'object.upload.failed'
    ? Readonly<{ reason: (typeof auditReasons)[number] }>
    : A extends 'object.transfer.revoke'
      ? Readonly<{ reason?: (typeof auditReasons)[number] }>
      : object)
function objectContent<A extends ObjectAction>(action: A): Parser<ObjectMetadata<A>> {
  return (value) => {
    const initiator = oneOf(['human', 'agent_run', 'service'] as const)(
      object(value).initiator_kind,
    )
    const parsed = record(
      {
        object_id: auditID,
        ...(action.startsWith('object.transfer.') ? { transfer_id: auditID } : {}),
        initiator_kind: literal(initiator),
        initiator_id: auditID,
        ...(initiator === 'agent_run' ? { initiator_execution_id: auditID } : {}),
        media_type: auditMediaType,
        byte_size: progress,
        sent_bytes: progress,
        phase: literal(objectPhases[action]),
        ...(action === 'object.upload.failed' ? { reason } : {}),
      },
      action === 'object.transfer.revoke' ? { reason } : {},
    )(value)
    requireAudit(BigInt(parsed.sent_bytes) <= BigInt(parsed.byte_size))
    if (action === 'object.transfer.complete') requireAudit(parsed.sent_bytes === parsed.byte_size)
    return parsed as ObjectMetadata<A>
  }
}
const sourceKinds = [
  'inline',
  'uploaded_object',
  'artifact_file',
  'knowledge_file',
  'execution_file',
] as const
type ArtifactSource =
  | Readonly<{ source_kind?: 'inline' }>
  | Readonly<{
      source_kind: Exclude<(typeof sourceKinds)[number], 'inline'>
      source_id: string
      source_revision?: string
    }>
type ArtifactPhase<A extends string> = A extends 'artifact.create'
  ? 'published'
  : A extends 'artifact.read'
    ? 'read'
    : 'issued' | 'started' | 'sent' | 'failed'
type ArtifactMetadata<A extends string> = Readonly<{
  artifact_id: string
  object_id: string
  media_type: string
  byte_size: string
  sent_bytes: string
}> &
  ArtifactSource &
  (A extends 'artifact.create' ? Readonly<{ source_kind: (typeof sourceKinds)[number] }> : object) &
  (
    | Readonly<{ phase: Exclude<ArtifactPhase<A>, 'failed'> }>
    | (A extends 'artifact.download'
        ? Readonly<{ phase: 'failed'; reason: (typeof auditReasons)[number] }>
        : never)
  )
function artifact<A extends 'artifact.create' | 'artifact.read' | 'artifact.download'>(
  action: A,
): Parser<ArtifactMetadata<A>> {
  return (value) => {
    const raw = object(value)
    const phase =
      action === 'artifact.create'
        ? literal('published')(raw.phase)
        : action === 'artifact.read'
          ? literal('read')(raw.phase)
          : oneOf(['issued', 'started', 'sent', 'failed'] as const)(raw.phase)
    const hasSource = Object.hasOwn(raw, 'source_kind')
    const source = hasSource ? oneOf(sourceKinds)(raw.source_kind) : undefined
    requireAudit(action !== 'artifact.create' || hasSource)
    const reference = source !== undefined && source !== 'inline'
    const parsed = record(
      {
        artifact_id: auditID,
        object_id: auditID,
        media_type: auditMediaType,
        byte_size: progress,
        sent_bytes: progress,
        phase: literal(phase),
        ...(source !== undefined ? { source_kind: literal(source) } : {}),
        ...(reference ? { source_id: auditID } : {}),
        ...(phase === 'failed' ? { reason } : {}),
      },
      reference ? { source_revision: version } : {},
    )(value)
    requireAudit(BigInt(parsed.sent_bytes) <= BigInt(parsed.byte_size))
    if (action === 'artifact.create') requireAudit(parsed.sent_bytes === '0')
    return parsed as ArtifactMetadata<A>
  }
}
const projectBase = { project_id: auditID, initiator_id: auditID, project_version: version }
const operationBase = { ...projectBase, operation_id: auditID, operation_version: version }
function creation(value: unknown) {
  const parsed = record({
    ...projectBase,
    project_version: literal('1'),
    creation_id: auditID,
    creation_version: version,
  })(value)
  return parsed
}
function projectUpdate(value: unknown) {
  const parsed = record({
    ...projectBase,
    changed_fields: fields(['description', 'name'] as const),
  })(value)
  requireAudit(BigInt(parsed.project_version) >= 2n)
  return parsed
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

// These are the Project HTTP projection's 31 actions, independent of the
// System projection. Filter choices intentionally remain the wider 53 values.
const parsers = {
  'secret.create': record({ version, changed_fields: fields(['purpose', 'value'] as const) }),
  'secret.update': record({ version, changed_fields: fields(['purpose', 'value'] as const) }),
  'secret.delete': record({ version }),
  'secret.resolve': record({ lease_id: auditID, consumer }, { reason }),
  'outbound.access.deny': record({ version, consumer, reason }),
  'object.upload.complete': objectContent('object.upload.complete'),
  'object.upload.failed': objectContent('object.upload.failed'),
  'object.delete': objectContent('object.delete'),
  'object.transfer.issue': objectContent('object.transfer.issue'),
  'object.transfer.complete': objectContent('object.transfer.complete'),
  'object.transfer.revoke': objectContent('object.transfer.revoke'),
  'artifact.create': artifact('artifact.create'),
  'artifact.read': artifact('artifact.read'),
  'artifact.download': artifact('artifact.download'),
  'artifact.list': record({ phase: literal('listed'), count: progress }),
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
  'project.create.accepted': creation,
  'project.create.completed': creation,
  'project.update': projectUpdate,
  'project.archive.accepted': record({
    ...operationBase,
    from: literal('active'),
    to: literal('archiving'),
    action: literal('archive'),
  }),
  'project.archive.completed': record({
    ...operationBase,
    from: literal('archiving'),
    to: literal('archived'),
    action: literal('archive'),
  }),
  'project.restore': record({
    ...projectBase,
    from: literal('archived'),
    to: literal('active'),
    action: literal('restore'),
  }),
  'project.delete.accepted': record({
    ...operationBase,
    from: oneOf(['active', 'archived'] as const),
    to: literal('deleting'),
    action: literal('delete'),
  }),
  'project.lifecycle.retry': record({
    ...operationBase,
    action: oneOf(['archive', 'delete'] as const),
  }),
  'provider.create': record({
    provider_id: auditID,
    version,
    changed_fields: exactFields(['created'] as const),
  }),
  'provider.update': record({
    provider_id: auditID,
    version,
    changed_fields: fields([
      'name',
      'enabled',
      'base_url',
      'credential_ref',
      'provider_options',
    ] as const),
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
    changed_fields: fields([
      'name',
      'enabled',
      'model_id',
      'parameters',
      'request_overwrite',
      'header_overwrite',
      'capabilities',
    ] as const),
  }),
  'model.delete': modelDelete,
  'project.variable.create': record({
    variable_id: auditID,
    version,
    changed_fields: exactFields(['created'] as const),
  }),
  'project.variable.update': record({
    variable_id: auditID,
    version,
    changed_fields: fields(['description', 'name', 'value'] as const),
  }),
  'project.variable.delete': record({
    variable_id: auditID,
    version,
    changed_fields: exactFields(['deleted'] as const),
  }),
  'knowledge.delete_subtree': record({
    project_id: auditID,
    root_id: auditID,
    initiator_id: auditID,
    scope_digest: (value: unknown) => {
      requireAudit(typeof value === 'string' && /^sha256:[0-9a-f]{64}$/.test(value))
      return value
    },
    deleted_count: version,
  }),
} as const

export type ProjectAuditAction = keyof typeof parsers
export type ProjectAuditMetadataByAction = {
  readonly [A in ProjectAuditAction]: ReturnType<(typeof parsers)[A]>
}
export type TypedProjectAuditMetadata = {
  readonly [A in ProjectAuditAction]: Readonly<{
    action: A
    metadata: ProjectAuditMetadataByAction[A]
  }>
}[ProjectAuditAction]
export const projectAuditActions = Object.freeze(Object.keys(parsers) as ProjectAuditAction[])
export function parseProjectAuditMetadata(
  action: unknown,
  metadata: unknown,
): TypedProjectAuditMetadata {
  requireAudit(typeof action === 'string' && Object.hasOwn(parsers, action))
  requireAudit(auditMetadataJSONBytes(metadata) <= 4096)
  const key = action as ProjectAuditAction
  return Object.freeze({
    action: key,
    metadata: parsers[key](metadata),
  }) as TypedProjectAuditMetadata
}

export type ProjectAuditActor =
  | Readonly<{ kind: 'human'; id: string }>
  | Readonly<{ kind: 'agent_run'; id: string; project_id: string; execution_id: string }>
  | Readonly<{
      kind: 'service'
      service: (typeof auditServices)[number]
      cause_ref: string
      project_id: string
    }>
export const projectAuditResourceKinds = [
  'secret',
  'outbound_policy',
  'agent',
  'stored_object',
  'object_transfer',
  'artifact',
  'artifact_collection',
  'outbox_delivery',
  'project',
  'project_operation',
  'project_creation',
  'model_provider',
  'model_config',
  'knowledge_document',
  'project_variable',
] as const
export type ProjectAuditResource =
  | Readonly<{ kind: 'outbound_policy' }>
  | Readonly<{
      kind: Exclude<(typeof projectAuditResourceKinds)[number], 'outbound_policy'>
      id: string
    }>
export const projectAuditAssociationFields = [
  'tool_id',
  'execution_id',
  'tool_call_id',
  'operation_id',
  'request_id',
  'approval_id',
  'runner_id',
  'correlation_id',
  'http_trace_id',
] as const
export type ProjectAuditAssociations = Readonly<
  Partial<Record<(typeof projectAuditAssociationFields)[number], string>>
>
export function parseProjectAuditActor(value: unknown, projectID: string): ProjectAuditActor {
  const kind = oneOf(['human', 'agent_run', 'service'] as const)(object(value).kind)
  if (kind === 'human') return record({ kind: literal('human'), id: auditID })(value)
  if (kind === 'agent_run')
    return record({
      kind: literal('agent_run'),
      id: auditID,
      project_id: literal(projectID),
      execution_id: auditID,
    })(value)
  return record({
    kind: literal('service'),
    service: oneOf(auditServices),
    project_id: literal(projectID),
    cause_ref: (value: unknown) => {
      requireAudit(
        typeof value === 'string' && (uuid7.test(value) || /^sha256:[0-9a-f]{64}$/.test(value)),
      )
      return value
    },
  })(value)
}
export function parseProjectAuditResource(value: unknown): ProjectAuditResource {
  const kind = oneOf(projectAuditResourceKinds)(object(value).kind)
  if (kind === 'outbound_policy') return record({ kind: literal(kind) })(value)
  return record({ kind: literal(kind), id: auditID })(value)
}
export function parseProjectAuditAssociations(value: unknown): ProjectAuditAssociations {
  return record(
    {},
    {
      tool_id: auditID,
      execution_id: auditID,
      tool_call_id: auditID,
      operation_id: auditID,
      request_id: auditID,
      approval_id: auditID,
      runner_id: auditID,
      correlation_id: auditID,
      http_trace_id: auditID,
    },
  )(value)
}
export function projectAuditSummary(action: ProjectAuditAction): string {
  const summaries: Partial<Record<ProjectAuditAction, string>> = {
    'secret.create': 'Secret created',
    'secret.update': 'Secret updated',
    'secret.delete': 'Secret deleted',
    'secret.resolve': 'Secret use recorded',
    'outbound.access.deny': 'Outbound access denied',
  }
  return summaries[action] ?? 'Audit event'
}
export function validateProjectAuditRelations(
  entry: TypedProjectAuditMetadata,
  context: Readonly<{
    projectID: string
    actor: ProjectAuditActor
    resource: ProjectAuditResource
    outcome: AuditOutcome
    associations: ProjectAuditAssociations
  }>,
): void {
  const { actor, resource, outcome, associations: links, projectID } = context
  if (actor.kind === 'agent_run') requireAudit(links.execution_id === actor.execution_id)
  const same = (kind: string, id?: string) =>
    resource.kind === kind && ('id' in resource ? resource.id : undefined) === id
  const { action, metadata } = entry
  if (
    entry.action === 'project.variable.create' ||
    entry.action === 'project.variable.update' ||
    entry.action === 'project.variable.delete'
  ) {
    requireAudit(
      actor.kind === 'human' &&
        outcome === 'success' &&
        Object.keys(links).length === 0 &&
        same('project_variable', entry.metadata.variable_id),
    )
    requireAudit(
      entry.action === 'project.variable.create'
        ? entry.metadata.version === '1'
        : BigInt(entry.metadata.version) >= 2n,
    )
    return
  }
  if (action.startsWith('project.')) {
    const m = metadata as ProjectAuditMetadataByAction['project.update'] &
      Partial<{ creation_id: string; operation_id: string }>
    requireAudit(
      m.project_id === projectID &&
        outcome === 'success' &&
        !links.tool_id &&
        !links.execution_id &&
        !links.tool_call_id &&
        !links.approval_id &&
        (!links.operation_id || links.operation_id === m.operation_id),
    )
    requireAudit(
      m.creation_id
        ? same('project_creation', m.creation_id)
        : m.operation_id
          ? same('project_operation', m.operation_id)
          : same('project', projectID),
    )
    if (action === 'project.create.completed')
      requireAudit(
        actor.kind === 'service' &&
          actor.service === 'project-initialization' &&
          actor.cause_ref === m.creation_id,
      )
    else if (action === 'project.archive.completed')
      requireAudit(
        actor.kind === 'service' &&
          actor.service === 'project-lifecycle' &&
          actor.cause_ref === m.operation_id,
      )
    else requireAudit(actor.kind === 'human' && actor.id === m.initiator_id)
    return
  }
  if (action.startsWith('provider.') || action.startsWith('model.')) {
    requireAudit(
      actor.kind === 'human' &&
        outcome === 'success' &&
        projectAuditAssociationFields.every(
          (key) => key === 'correlation_id' || key === 'http_trace_id' || !links[key],
        ),
    )
    if (action.startsWith('provider.'))
      requireAudit(
        same(
          'model_provider',
          (metadata as ProjectAuditMetadataByAction['provider.create']).provider_id,
        ),
      )
    else
      requireAudit(
        same('model_config', (metadata as ProjectAuditMetadataByAction['model.create']).model_id),
      )
    return
  }
  switch (entry.action) {
    case 'knowledge.delete_subtree':
      requireAudit(
        actor.kind === 'human' &&
          actor.id === entry.metadata.initiator_id &&
          entry.metadata.project_id === projectID &&
          outcome === 'success' &&
          same('knowledge_document', entry.metadata.root_id) &&
          Object.keys(links).length === 0,
      )
      break
    case 'secret.create':
    case 'secret.update':
    case 'secret.delete':
    case 'secret.resolve':
      requireAudit(resource.kind === 'secret')
      break
    case 'outbound.access.deny':
      requireAudit(
        outcome === 'denied' && ['outbound_policy', 'secret', 'agent'].includes(resource.kind),
      )
      break
    case 'object.upload.complete':
    case 'object.upload.failed':
    case 'object.delete':
    case 'object.transfer.issue':
    case 'object.transfer.complete':
    case 'object.transfer.revoke':
      requireAudit(
        actor.kind === 'service' && ['object', 'object-maintenance'].includes(actor.service),
      )
      requireAudit(
        'transfer_id' in entry.metadata
          ? same('object_transfer', entry.metadata.transfer_id)
          : same('stored_object', entry.metadata.object_id),
      )
      requireAudit(
        entry.action === 'object.upload.failed'
          ? outcome === 'failed' || outcome === 'unknown'
          : outcome === 'success',
      )
      break
    case 'artifact.create':
    case 'artifact.read':
    case 'artifact.download':
    case 'artifact.list':
      requireAudit(actor.kind === 'human' || actor.kind === 'agent_run')
      requireAudit(
        entry.action === 'artifact.list'
          ? same('artifact_collection', projectID)
          : same('artifact', entry.metadata.artifact_id),
      )
      requireAudit(
        entry.metadata.phase === 'failed'
          ? entry.action === 'artifact.download' && outcome === 'failed'
          : outcome === 'success',
      )
      break
    case 'outbox.delivery.requeue':
      requireAudit(
        actor.kind === 'human' &&
          outcome === 'success' &&
          same('outbox_delivery', entry.metadata.delivery_id),
      )
      break
    default:
      throw new AccountFailure('invalid-response')
  }
}

const metadataLabels = {
  variable_id: '变量 ID',
  project_id: '项目 ID',
  creation_id: '创建 ID',
  creation_version: '创建版本',
  operation_id: '操作 ID',
  operation_version: '操作版本',
  project_version: '项目版本',
  provider_id: 'Provider ID',
  model_id: '模型 ID',
  replacement_id: '替换模型 ID',
  version: '版本',
  changed_fields: '变更字段',
  affected_count: '受影响数量',
  initiator_kind: '发起者类型',
  initiator_id: '发起者 ID',
  initiator_execution_id: '发起执行 ID',
  from: '原状态',
  to: '新状态',
  from_state: '原投递状态',
  action: '生命周期操作',
  lease_id: '租约 ID',
  consumer: '消费者',
  object_id: '对象 ID',
  transfer_id: '传输 ID',
  artifact_id: '制品 ID',
  source_kind: '来源类型',
  source_id: '来源 ID',
  source_revision: '来源版本',
  root_id: '根文档 ID',
  scope_digest: '范围摘要',
  deleted_count: '删除数量',
  media_type: '媒体类型',
  byte_size: '字节数',
  sent_bytes: '已传字节数',
  phase: '阶段',
  count: '数量',
  delivery_id: '投递 ID',
  event_id: '事件 ID',
  handler_id: '处理器 ID',
  redrive_cycle: '重驱动轮次',
  reason_code: '原因代码',
  reason: '原因',
} as const
const associationLabels: Record<(typeof projectAuditAssociationFields)[number], string> = {
  tool_id: '工具 ID',
  execution_id: '执行 ID',
  tool_call_id: '工具调用 ID',
  operation_id: '操作 ID',
  request_id: '请求 ID',
  approval_id: '审批 ID',
  runner_id: 'Runner ID',
  correlation_id: '关联 ID',
  http_trace_id: 'HTTP 跟踪 ID',
}
export type ProjectAuditDisplayField = Readonly<{ key: string; label: string; value: string }>
export function projectAuditMetadataFields(
  entry: TypedProjectAuditMetadata,
): readonly ProjectAuditDisplayField[] {
  const source: Readonly<Record<string, unknown>> = entry.metadata
  return Object.freeze(
    Object.entries(metadataLabels).flatMap(([key, label]) => {
      if (!Object.hasOwn(source, key)) return []
      const value = source[key]
      return [
        Object.freeze({
          key,
          label,
          value: Array.isArray(value) ? value.join('、') : String(value),
        }),
      ]
    }),
  )
}
export function projectAuditAssociationRows(
  links: ProjectAuditAssociations,
): readonly ProjectAuditDisplayField[] {
  return Object.freeze(
    projectAuditAssociationFields.flatMap((key) =>
      links[key] === undefined
        ? []
        : [Object.freeze({ key, label: associationLabels[key], value: links[key]! })],
    ),
  )
}
