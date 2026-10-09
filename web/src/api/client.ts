export type CommitState = 'not_started' | 'not_committed' | 'committed' | 'unknown'
export interface Problem {
  type: string
  title: string
  status: number
  detail: string
  instance: string
  code: string
  request_id: string
  commit_state: CommitState
  field_errors?: { path: string; code: string }[]
  retry_hint?: string
}
export type FailureKind =
  'problem' | 'transport' | 'invalid-response' | 'cancelled' | 'invalid-input' | 'busy'

// No request, raw response, password, token or underlying exception is retained.
export class AccountFailure extends Error {
  constructor(
    readonly kind: FailureKind,
    readonly problem?: Problem,
  ) {
    super(kind)
    this.name = 'AccountFailure'
  }
}

export const uuid7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
export function object(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new AccountFailure('invalid-response')
  }
  return value as Record<string, unknown>
}
export function shape(
  value: unknown,
  required: readonly string[],
  optional: readonly string[] = [],
) {
  const result = object(value)
  if (
    required.some((key) => !Object.hasOwn(result, key)) ||
    Object.keys(result).some((key) => !required.includes(key) && !optional.includes(key))
  ) {
    throw new AccountFailure('invalid-response')
  }
  return result
}
export function string(value: unknown, minimum: number, maximum: number): string {
  if (typeof value !== 'string' || [...value].length < minimum || [...value].length > maximum) {
    throw new AccountFailure('invalid-response')
  }
  return value
}

const endpoints = {
  listOwnerProjects: ['GET', '/api/v1/projects', 200],
  getOwnerProject: ['GET', '/api/v1/projects/{id}', 200],
  resolveOwnerProject: ['GET', '/api/v1/projects/resolve', 200],
  updateOwnerProject: ['PATCH', '/api/v1/projects/{id}', 200],
  lookupOwnerProject: ['POST', '/api/v1/projects/{id}/commands/lookup', 200],
  listProjectAudit: ['GET', '/api/v1/projects/{project_id}/audit', 200],
  getProjectAudit: ['GET', '/api/v1/projects/{project_id}/audit/{audit_id}', 200],
  listProjectModelProviders: ['GET', '/api/v1/projects/{project_id}/model-providers', 200],
  getProjectModelProvider: ['GET', '/api/v1/projects/{project_id}/model-providers/{target}', 200],
  listProjectModels: ['GET', '/api/v1/projects/{project_id}/models', 200],
  getProjectModel: ['GET', '/api/v1/projects/{project_id}/models/{target}', 200],
  listProjectAvailableChatModels: [
    'GET',
    '/api/v1/projects/{project_id}/available-chat-models',
    200,
  ],
  createProjectModelProvider: ['POST', '/api/v1/projects/{project_id}/model-providers', 200],
  updateProjectModelProvider: [
    'PUT',
    '/api/v1/projects/{project_id}/model-providers/{target}',
    200,
  ],
  deleteProjectModelProvider: [
    'DELETE',
    '/api/v1/projects/{project_id}/model-providers/{target}',
    200,
  ],
  createProjectModel: ['POST', '/api/v1/projects/{project_id}/models', 200],
  updateProjectModel: ['PUT', '/api/v1/projects/{project_id}/models/{target}', 200],
  deleteProjectModel: ['DELETE', '/api/v1/projects/{project_id}/models/{target}', 200],
  lookupProjectModelConfiguration: [
    'POST',
    '/api/v1/projects/{project_id}/model-commands/lookup',
    200,
  ],
  getProjectModelCredentialMetadata: [
    'GET',
    '/api/v1/projects/{project_id}/model-credentials/{target}',
    200,
  ],
  createProjectModelCredential: ['POST', '/api/v1/projects/{project_id}/model-credentials', 200],
  updateProjectModelCredential: [
    'PUT',
    '/api/v1/projects/{project_id}/model-credentials/{target}',
    200,
  ],
  deleteProjectModelCredential: [
    'DELETE',
    '/api/v1/projects/{project_id}/model-credentials/{target}',
    200,
  ],
  lookupProjectModelCredential: [
    'POST',
    '/api/v1/projects/{project_id}/model-credential-commands/lookup',
    200,
  ],
  bootstrap: ['GET', '/api/v1/auth/bootstrap', 200],
  session: ['GET', '/api/v1/session', 200],
  login: ['POST', '/api/v1/sessions/login', 200],
  logout: ['POST', '/api/v1/sessions/logout', 204],
  challenge: ['POST', '/api/v1/auth/challenges', 201],
  verify: ['POST', '/api/v1/auth/challenges/verify', 200],
  profile: ['GET', '/api/v1/me', 200],
  updateProfile: ['PATCH', '/api/v1/me', 200],
  preferences: ['GET', '/api/v1/me/preferences', 200],
  setPreferences: ['PUT', '/api/v1/me/preferences', 200],
  avatar: ['GET', '/api/v1/me/avatar', 200],
  putAvatar: ['PUT', '/api/v1/me/avatar', 200],
  deleteAvatar: ['DELETE', '/api/v1/me/avatar', 204],
  changePassword: ['POST', '/api/v1/me/change-password', 200],
  inspectInvitation: ['POST', '/api/v1/invitations/inspect', 200],
  redeemInvitation: ['POST', '/api/v1/invitations/redeem', 201],
  requestPasswordReset: ['POST', '/api/v1/password-resets/request', 202],
  inspectPasswordReset: ['POST', '/api/v1/password-resets/inspect', 200],
  completePasswordReset: ['POST', '/api/v1/password-resets/complete', 204],
  systemUsers: ['GET', '/api/v1/system/users', 200],
  listSystemAudit: ['GET', '/api/v1/system/audit', 200],
  getSystemAudit: ['GET', '/api/v1/system/audit/{id}', 200],
  getSystemRuntimeInformation: ['GET', '/api/v1/system/runtime-information', 200],
  listProviders: ['GET', '/api/v1/system/model-providers', 200],
  getProvider: ['GET', '/api/v1/system/model-providers/{id}', 200],
  createProvider: ['POST', '/api/v1/system/model-providers', 200],
  updateProvider: ['PUT', '/api/v1/system/model-providers/{id}', 200],
  deleteProvider: ['DELETE', '/api/v1/system/model-providers/{id}', 200],
  listProviderModels: ['GET', '/api/v1/system/models', 200],
  getModel: ['GET', '/api/v1/system/models/{id}', 200],
  createModel: ['POST', '/api/v1/system/models', 200],
  updateModel: ['PUT', '/api/v1/system/models/{id}', 200],
  deleteModel: ['DELETE', '/api/v1/system/models/{id}', 200],
  getModelDeletionImpact: ['GET', '/api/v1/system/models/{id}/deletion-impact', 200],
  lookupModelCommand: ['POST', '/api/v1/system/model-commands/lookup', 200],
  getModelSelection: ['GET', '/api/v1/system/model-selection', 200],
  getMeetingSummary: ['GET', '/api/v1/system/model-selection/meeting-summary', 200],
  updateMeetingSummary: ['PUT', '/api/v1/system/model-selection/meeting-summary', 200],
  updateModelSelection: ['PUT', '/api/v1/system/model-selection', 200],
  lookupModelSelectionCommand: ['POST', '/api/v1/system/model-commands/lookup', 200],
  getAccountSecurity: ['GET', '/api/v1/system/account-settings', 200],
  updateAccountSecurity: ['PUT', '/api/v1/system/account-settings', 200],
  getOutboundPolicy: ['GET', '/api/v1/system/outbound-policy', 200],
  updateOutboundPolicy: ['PUT', '/api/v1/system/outbound-policy', 200],
  getSMTPSettings: ['GET', '/api/v1/system/smtp', 200],
  updateSMTPSettings: ['PUT', '/api/v1/system/smtp', 200],
  unconfigureSMTP: ['POST', '/api/v1/system/smtp/unconfigure', 200],
  testSMTP: ['POST', '/api/v1/system/smtp/test', 202],
  listMailJobManagement: ['GET', '/api/v1/system/mail-jobs/management', 200],
  getMailJobManagement: ['GET', '/api/v1/system/mail-jobs/{id}/management', 200],
  createModelCredential: ['POST', '/api/v1/system/model-credentials', 200],
  getModelCredentialMetadata: ['GET', '/api/v1/system/model-credentials/{id}', 200],
  lookupProviderCommand: ['POST', '/api/v1/system/model-commands/lookup', 200],
  lookupModelCredentialCreate: ['POST', '/api/v1/system/model-credential-commands/lookup', 200],
  systemInvitations: ['GET', '/api/v1/system/invitations', 200],
  createSystemInvitation: ['POST', '/api/v1/system/invitations', 201],
  resendSystemInvitation: ['POST', '/api/v1/system/invitations/{id}/resend', 202],
  revokeSystemInvitation: ['POST', '/api/v1/system/invitations/{id}/revoke', 204],
  retrySystemDelivery: ['POST', '/api/v1/system/mail-jobs/{id}/retry', 202],
} as const
export type Fetch = (input: string, init: RequestInit) => Promise<Response>

function problem(value: unknown, status: number, requestID: string | null): Problem {
  const p = shape(
    value,
    ['type', 'title', 'status', 'detail', 'instance', 'code', 'request_id', 'commit_state'],
    ['field_errors', 'retry_hint'],
  )
  const fail = () => {
    throw new AccountFailure('invalid-response')
  }
  if (
    p.status !== status ||
    ![400, 401, 403, 404, 405, 409, 410, 413, 415, 416, 422, 429, 500, 502, 503].includes(status) ||
    !/^urn:agenteam:problem:[a-z]+(?:-[a-z]+)*$/.test(string(p.type, 1, 256)) ||
    !/^\/[^?#]*$/.test(string(p.instance, 1, 2048)) ||
    !/^[A-Z][A-Z0-9_]*$/.test(string(p.code, 1, 128)) ||
    !uuid7.test(string(p.request_id, 36, 36)) ||
    p.request_id !== requestID ||
    typeof p.commit_state !== 'string' ||
    !['not_started', 'not_committed', 'committed', 'unknown'].includes(p.commit_state)
  )
    fail()
  const parsed: Problem = {
    type: p.type as string,
    title: string(p.title, 0, 1024),
    status,
    detail: string(p.detail, 0, 8192),
    instance: p.instance as string,
    code: p.code as string,
    request_id: p.request_id as string,
    commit_state: p.commit_state as CommitState,
  }
  if (p.retry_hint !== undefined) {
    parsed.retry_hint = string(p.retry_hint, 1, 64)
    if (!/^[a-z][a-z0-9_]*$/.test(parsed.retry_hint)) fail()
  }
  if (p.field_errors !== undefined) {
    if (
      !Array.isArray(p.field_errors) ||
      p.field_errors.length === 0 ||
      p.field_errors.length > 128
    )
      fail()
    parsed.field_errors = (p.field_errors as unknown[]).map((entry) => {
      const e = shape(entry, ['path', 'code'])
      const path = string(e.path, 0, 1024),
        code = string(e.code, 1, 128)
      if (!/^(?:\/(?:[^~]|~[01])*)*$/.test(path) || !/^[A-Z][A-Z0-9_]*$/.test(code)) fail()
      return { path, code }
    })
  }
  return parsed
}

// Only the five Project model reads need RawMessage size provenance. Retain
// numeric token widths as metadata, never raw bodies. Go compacts RawMessage
// and HTML-escapes strings; those injected escapes give a necessary lower
// bound on the original bytes, not proof of the unavailable stored spelling.
// Project Audit reuses the token walk for duplicate-member rejection.
const projectModelJSONSizes = new WeakMap<object, number>()
function projectModelJSON(text: string): unknown {
  const value: unknown = JSON.parse(text)
  const encoder = new TextEncoder()
  let position = 0
  const whitespace = () => {
    while (position < text.length && /[ \t\r\n]/.test(text[position]!)) position++
  }
  function quoted() {
    const start = position++
    let reduction = 0
    while (position < text.length) {
      const ch = text[position++]
      if (ch === '"') break
      if (ch === '\\') {
        const escape = text[position++]
        if (escape === 'u') {
          const hex = text.slice(position, position + 4)
          if (hex === '003c' || hex === '003e' || hex === '0026') reduction += 5
          if (hex === '2028' || hex === '2029') reduction += 3
          position += 4
        }
      }
    }
    return {
      start,
      end: position,
      bytes: encoder.encode(text.slice(start, position)).byteLength - reduction,
    }
  }
  function visit(current: unknown): number {
    whitespace()
    const marker = text[position]
    if (marker === '"') return quoted().bytes
    if (marker !== '{' && marker !== '[') {
      const start = position
      while (position < text.length && !/[,}\]\s]/.test(text[position]!)) position++
      return position - start
    }
    const array = marker === '[',
      close = array ? ']' : '}'
    position++
    whitespace()
    let bytes = 2,
      index = 0
    const seen = new Set<string>()
    while (text[position] !== close) {
      if (index > 0) {
        position++
        bytes++
        whitespace()
      }
      if (array) bytes += visit((current as unknown[])[index])
      else {
        const key = quoted(),
          name: string = JSON.parse(text.slice(key.start, key.end))
        if (seen.has(name)) throw new AccountFailure('invalid-response')
        seen.add(name)
        whitespace()
        position++
        bytes += key.bytes + 1 + visit((current as Record<string, unknown>)[name])
      }
      index++
      whitespace()
    }
    position++
    projectModelJSONSizes.set(current as object, bytes)
    return bytes
  }
  visit(value)
  return value
}
// Direct typed-parser users have no wire lexemes. Use a representation lower
// bound there too; Number.toString alone is not a raw configuration budget.
export function projectModelJSONBytes(value: unknown): number {
  if (value !== null && typeof value === 'object') {
    const measured = projectModelJSONSizes.get(value)
    if (measured !== undefined) return measured
    if (Array.isArray(value))
      return (
        2 +
        Math.max(0, value.length - 1) +
        value.reduce((sum, item) => sum + projectModelJSONBytes(item), 0)
      )
    const entries = Object.entries(value)
    return (
      2 +
      Math.max(0, entries.length - 1) +
      entries.reduce(
        (sum, [key, item]) => sum + projectModelJSONBytes(key) + 1 + projectModelJSONBytes(item),
        0,
      )
    )
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return Infinity
    let minimum = String(value).length
    const negative = value < 0 ? '-' : '',
      [mantissa, power] = Math.abs(value).toExponential().split('e'),
      digits = mantissa!.replace('.', ''),
      exponent = Number(power)
    for (let place = 1; place <= digits.length; place++) {
      const coefficient =
          negative +
          digits.slice(0, place) +
          (place === digits.length ? '' : '.' + digits.slice(place)),
        shifted = exponent - place + 1
      minimum = Math.min(
        minimum,
        coefficient.length + (shifted === 0 ? 0 : 1 + String(shifted).length),
      )
    }
    return minimum
  }
  if (typeof value === 'string' || typeof value === 'boolean' || value === null)
    return new TextEncoder().encode(JSON.stringify(value)).byteLength
  return Infinity
}

async function readJSON(
  response: Response,
  signal: AbortSignal,
  maximum = 600_000,
  preserveProjectModelJSON = false,
  checkProjectAuditMembers = false,
): Promise<unknown> {
  const reader = response.body?.getReader()
  if (!reader) throw new AccountFailure('invalid-response')
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let text = '',
    bytes = 0
  let cancelled: Promise<void> | undefined
  const cancel = () => (cancelled ??= reader.cancel().catch(() => undefined))
  // The listener starts cancellation; the same promise is joined in finally.
  const abort = () => {
    void cancel()
  }
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      bytes += value.byteLength
      if (bytes > maximum) throw new AccountFailure('invalid-response')
      text += decoder.decode(value, { stream: true })
    }
    text += decoder.decode()
    // Audit pages can mix actions. Check member identity before any action
    // discriminator can be replaced by a later duplicate JSON member.
    return preserveProjectModelJSON || checkProjectAuditMembers
      ? projectModelJSON(text)
      : (JSON.parse(text) as unknown)
  } finally {
    signal.removeEventListener('abort', abort)
    await cancel()
    reader.releaseLock()
  }
}

async function readEmptyBody(response: Response, signal: AbortSignal): Promise<void> {
  const reader = response.body?.getReader()
  if (!reader) return
  let cancelled: Promise<void> | undefined
  const cancel = () => (cancelled ??= reader.cancel().catch(() => undefined))
  const abort = () => {
    void cancel()
  }
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) return
      if (value.byteLength !== 0) throw new AccountFailure('invalid-response')
    }
  } finally {
    signal.removeEventListener('abort', abort)
    await cancel()
    reader.releaseLock()
  }
}

async function readAvatar(response: Response, signal: AbortSignal) {
  const media = response.headers.get('Content-Type')
  const length = response.headers.get('Content-Length')
  const etag = response.headers.get('ETag')
  if (
    !['image/jpeg', 'image/png', 'image/webp'].includes(media ?? '') ||
    !length ||
    !/^[1-9][0-9]{0,6}$/.test(length) ||
    Number(length) > 5 * 1024 * 1024 ||
    !etag ||
    !/^"sha256:[0-9a-f]{64}"$/.test(etag)
  )
    throw new AccountFailure('invalid-response')
  const reader = response.body?.getReader()
  if (!reader) throw new AccountFailure('invalid-response')
  let cancelled: Promise<void> | undefined
  const cancel = () => (cancelled ??= reader.cancel().catch(() => undefined))
  const abort = () => {
    void cancel()
  }
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  const bytes = new Uint8Array(Number(length))
  let offset = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (offset + value.byteLength > bytes.byteLength) throw new AccountFailure('invalid-response')
      bytes.set(value, offset)
      offset += value.byteLength
    }
    if (signal.aborted) throw new AccountFailure('cancelled')
    if (offset !== bytes.byteLength) throw new AccountFailure('invalid-response')
    return {
      metadata: { media_type: media, byte_size: length, sha256: etag.slice(1, -1) },
      blob: new Blob([bytes], { type: media! }),
    }
  } catch (e) {
    throw e instanceof AccountFailure
      ? e
      : new AccountFailure(signal.aborted ? 'cancelled' : 'invalid-response')
  } finally {
    signal.removeEventListener('abort', abort)
    await cancel()
    reader.releaseLock()
  }
}

type RequestOptions = {
  body?: unknown
  csrf?: string
  key?: string
  signal: AbortSignal
  avatar?: { file: File; mediaType: string; version: string }
}
type UsersOptions = {
  signal: AbortSignal
  users: Readonly<{ cursor?: string }>
  body?: never
  csrf?: never
  key?: never
  avatar?: never
}
type InvitationTarget = 'resendSystemInvitation' | 'revokeSystemInvitation' | 'retrySystemDelivery'
type InvitationReadOptions = Omit<UsersOptions, 'users'> & {
  invitations: Readonly<{ cursor?: string }>
}
type ProviderEndpoint =
  | 'listProviders'
  | 'getProvider'
  | 'createProvider'
  | 'updateProvider'
  | 'deleteProvider'
  | 'listProviderModels'
  | 'createModelCredential'
  | 'getModelCredentialMetadata'
  | 'lookupProviderCommand'
  | 'lookupModelCredentialCreate'
type ProviderOptions<E extends ProviderEndpoint> = E extends 'listProviders'
  ? { signal: AbortSignal; providers: Readonly<{ cursor?: string }> }
  : E extends 'listProviderModels'
    ? { signal: AbortSignal; models: Readonly<{ provider_id: string; cursor?: string }> }
    : E extends 'getProvider' | 'getModelCredentialMetadata'
      ? { signal: AbortSignal; target: string }
      : E extends 'updateProvider' | 'deleteProvider'
        ? RequestOptions & { target: string; body: unknown; csrf: string; key: string }
        : RequestOptions & { body: unknown; csrf: string; key: string }
const providerEndpoints: readonly ProviderEndpoint[] = [
  'listProviders',
  'getProvider',
  'createProvider',
  'updateProvider',
  'deleteProvider',
  'listProviderModels',
  'createModelCredential',
  'getModelCredentialMetadata',
  'lookupProviderCommand',
  'lookupModelCredentialCreate',
]
type ModelEndpoint =
  | 'getModel'
  | 'createModel'
  | 'updateModel'
  | 'deleteModel'
  | 'getModelDeletionImpact'
  | 'lookupModelCommand'
type ModelOptions<E extends ModelEndpoint> = E extends 'getModel' | 'getModelDeletionImpact'
  ? { signal: AbortSignal; target: string }
  : E extends 'updateModel' | 'deleteModel'
    ? RequestOptions & { target: string; body: unknown; csrf: string; key: string }
    : RequestOptions & { body: unknown; csrf: string; key: string }
const modelEndpoints: readonly ModelEndpoint[] = [
  'getModel',
  'createModel',
  'updateModel',
  'deleteModel',
  'getModelDeletionImpact',
  'lookupModelCommand',
]
type SelectionEndpoint =
  | 'getModelSelection'
  | 'updateModelSelection'
  | 'lookupModelSelectionCommand'
  | 'getMeetingSummary'
  | 'updateMeetingSummary'
type SelectionOptions<E extends SelectionEndpoint> = E extends
  'getModelSelection' | 'getMeetingSummary'
  ? { signal: AbortSignal }
  : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const selectionEndpoints: readonly SelectionEndpoint[] = [
  'getMeetingSummary',
  'updateMeetingSummary',
  'getModelSelection',
  'updateModelSelection',
  'lookupModelSelectionCommand',
]
type AccountSecurityEndpoint = 'getAccountSecurity' | 'updateAccountSecurity'
type AccountSecurityOptions<E extends AccountSecurityEndpoint> = E extends 'getAccountSecurity'
  ? { signal: AbortSignal }
  : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const accountSecurityEndpoints: readonly AccountSecurityEndpoint[] = [
  'getAccountSecurity',
  'updateAccountSecurity',
]

type SMTPEndpoint = 'getSMTPSettings' | 'updateSMTPSettings' | 'unconfigureSMTP'
type SMTPOptions<E extends SMTPEndpoint> = E extends 'getSMTPSettings'
  ? { signal: AbortSignal }
  : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const smtpEndpoints: readonly SMTPEndpoint[] = [
  'getSMTPSettings',
  'updateSMTPSettings',
  'unconfigureSMTP',
]

type SMTPDeliveryEndpoint = 'testSMTP' | 'listMailJobManagement' | 'getMailJobManagement'
type SMTPDeliveryOptions<E extends SMTPDeliveryEndpoint> = E extends 'listMailJobManagement'
  ? { signal: AbortSignal; mailJobs: Readonly<{ cursor?: string }> }
  : E extends 'getMailJobManagement'
    ? { signal: AbortSignal; target: string }
    : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const smtpDeliveryEndpoints: readonly SMTPDeliveryEndpoint[] = [
  'testSMTP',
  'listMailJobManagement',
  'getMailJobManagement',
]

type OutboundPolicyEndpoint = 'getOutboundPolicy' | 'updateOutboundPolicy'
type OutboundPolicyOptions<E extends OutboundPolicyEndpoint> = E extends 'getOutboundPolicy'
  ? { signal: AbortSignal }
  : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const outboundPolicyEndpoints: readonly OutboundPolicyEndpoint[] = [
  'getOutboundPolicy',
  'updateOutboundPolicy',
]

export const auditFilterActions = [
  'secret.create',
  'secret.update',
  'secret.delete',
  'secret.resolve',
  'secret.master.register',
  'secret.master.rotation.start',
  'secret.master.rotation.complete',
  'secret.master.rotation.failed',
  'outbound.policy.update',
  'outbound.access.deny',
  'object.upload.complete',
  'object.upload.failed',
  'object.delete',
  'object.transfer.issue',
  'object.transfer.complete',
  'object.transfer.revoke',
  'artifact.create',
  'artifact.list',
  'artifact.read',
  'artifact.download',
  'outbox.delivery.requeue',
  'account.bootstrap',
  'account.login',
  'account.logout',
  'account.invite.create',
  'account.invite.revoke',
  'account.invite.redeem',
  'account.password.change',
  'account.password.reset.request',
  'account.password.reset.complete',
  'account.profile.update',
  'account.avatar.update',
  'account.settings.update',
  'smtp.settings.update',
  'smtp.test.request',
  'smtp.delivery',
  'smtp.delivery.retry',
  'project.create.accepted',
  'project.create.completed',
  'project.update',
  'project.archive.accepted',
  'project.archive.completed',
  'project.restore',
  'project.delete.accepted',
  'project.lifecycle.retry',
  'provider.create',
  'provider.update',
  'provider.delete',
  'model.create',
  'model.update',
  'model.delete',
  'model.selection.update',
  'knowledge.delete_subtree',
  'project.secret_variable.create',
  'project.secret_variable.update',
  'project.secret_variable.delete',
] as const
export const auditFilterResourceKinds = [
  'secret',
  'secret_master',
  'secret_rotation',
  'outbound_policy',
  'agent',
  'stored_object',
  'object_transfer',
  'artifact',
  'artifact_collection',
  'outbox_delivery',
  'user',
  'session',
  'account_attempt',
  'invitation',
  'password_reset',
  'account_settings',
  'smtp_settings',
  'mail_job',
  'project',
  'project_operation',
  'project_creation',
  'model_provider',
  'model_config',
  'model_selection',
  'knowledge_document',
  'project_variable',
] as const
export const auditFilterFields = [
  'from',
  'to',
  'actor_kind',
  'actor_id',
  'action',
  'outcome',
  'resource_kind',
  'resource_id',
  'tool_id',
  'execution_id',
  'operation_id',
  'approval_id',
  'runner_id',
  'agent_id',
] as const
export type AuditFilterField = (typeof auditFilterFields)[number]
export type AuditWireQuery = Readonly<
  Partial<Record<AuditFilterField | 'limit' | 'cursor', string>>
>
const auditQueryKeys = [...auditFilterFields, 'limit', 'cursor'] as const

function auditCanonicalTime(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value)) return false
  const y = Number(value.slice(0, 4)),
    m = Number(value.slice(5, 7)),
    d = Number(value.slice(8, 10))
  const days = [
    31,
    y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ]
  return (
    m >= 1 &&
    m <= 12 &&
    d >= 1 &&
    d <= days[m - 1]! &&
    Number(value.slice(11, 13)) <= 23 &&
    Number(value.slice(14, 16)) <= 59 &&
    Number(value.slice(17, 19)) <= 59
  )
}

// The transport accepts only a captured canonical query, never raw URLs or headers.
// The page API normalizes legal offset Instants before this independent wire gate.
export function captureAuditWireQuery(value: unknown): AuditWireQuery {
  try {
    const source = shape(value, [], auditQueryKeys)
    const result: Partial<Record<(typeof auditQueryKeys)[number], string>> = {}
    for (const key of auditQueryKeys) {
      if (!Object.hasOwn(source, key)) continue
      const entry = source[key]
      if (typeof entry !== 'string') throw new Error()
      if (key === 'limit') {
        if (!/^[1-9][0-9]{0,2}$/.test(entry) || Number(entry) > 200) throw new Error()
      } else {
        if (entry === '') continue
        if (key === 'from' || key === 'to') {
          if (!auditCanonicalTime(entry)) throw new Error()
        } else if (key === 'cursor') {
          if (!/^[A-Za-z0-9_.-]+$/.test(entry) || new TextEncoder().encode(entry).byteLength > 8192)
            throw new Error()
        } else if (key === 'actor_kind') {
          if (!['human', 'agent_run', 'service'].includes(entry)) throw new Error()
        } else if (key === 'action') {
          if (!(auditFilterActions as readonly string[]).includes(entry)) throw new Error()
        } else if (key === 'outcome') {
          if (!['success', 'denied', 'failed', 'unknown'].includes(entry)) throw new Error()
        } else if (key === 'resource_kind') {
          if (!(auditFilterResourceKinds as readonly string[]).includes(entry)) throw new Error()
        } else if (!uuid7.test(entry)) throw new Error()
      }
      result[key] = entry
    }
    if (result.from && result.to && result.from >= result.to) throw new Error()
    if (result.actor_kind === 'service' && result.actor_id) throw new Error()
    const encoded = new URLSearchParams(result).toString()
    if (new TextEncoder().encode(encoded).byteLength > 32 * 1024) throw new Error()
    return Object.freeze(result)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}

type AuditEndpoint = 'listSystemAudit' | 'getSystemAudit'
type AuditOptions<E extends AuditEndpoint> = E extends 'listSystemAudit'
  ? { signal: AbortSignal; audit: AuditWireQuery }
  : { signal: AbortSignal; target: string }

type ProjectAuditEndpoint = 'listProjectAudit' | 'getProjectAudit'
type ProjectAuditOptions<E extends ProjectAuditEndpoint> = E extends 'listProjectAudit'
  ? { signal: AbortSignal; projectID: string; audit: AuditWireQuery }
  : { signal: AbortSignal; projectID: string; target: string }

const projectModelReads = [
  'listProjectModelProviders',
  'getProjectModelProvider',
  'listProjectModels',
  'getProjectModel',
  'listProjectAvailableChatModels',
] as const
const projectConfigurationWrites = [
  'createProjectModelProvider',
  'updateProjectModelProvider',
  'deleteProjectModelProvider',
  'createProjectModel',
  'updateProjectModel',
  'deleteProjectModel',
  'lookupProjectModelConfiguration',
] as const
const projectModelEndpoints = [
  ...projectModelReads,
  ...projectConfigurationWrites,
  'getProjectModelCredentialMetadata',
  'createProjectModelCredential',
  'updateProjectModelCredential',
  'deleteProjectModelCredential',
  'lookupProjectModelCredential',
] as const
type ProjectModelEndpoint = (typeof projectModelEndpoints)[number]
type ProjectModelWireQuery = Readonly<{ cursor?: string; limit?: number }>
type ProjectModelOptions<E extends ProjectModelEndpoint> = E extends
  'listProjectModelProviders' | 'listProjectModels' | 'listProjectAvailableChatModels'
  ? { signal: AbortSignal; projectID: string; projectModels: ProjectModelWireQuery }
  : E extends 'getProjectModelProvider' | 'getProjectModel' | 'getProjectModelCredentialMetadata'
    ? { signal: AbortSignal; projectID: string; target: string }
    : E extends
          | 'updateProjectModelProvider'
          | 'deleteProjectModelProvider'
          | 'updateProjectModel'
          | 'deleteProjectModel'
          | 'updateProjectModelCredential'
          | 'deleteProjectModelCredential'
      ? {
          signal: AbortSignal
          projectID: string
          target: string
          body: unknown
          csrf: string
          key: string
        }
      : { signal: AbortSignal; projectID: string; body: unknown; csrf: string; key: string }

type ProjectEndpoint =
  | 'listOwnerProjects'
  | 'getOwnerProject'
  | 'resolveOwnerProject'
  | 'updateOwnerProject'
  | 'lookupOwnerProject'
type ProjectWireQuery = Readonly<{ limit: number; lifecycle?: readonly string[]; cursor?: string }>
type ProjectWireAddress = Readonly<{ username: string; project_name: string }>
type ProjectOptions<E extends ProjectEndpoint> = E extends 'listOwnerProjects'
  ? { signal: AbortSignal; projects: ProjectWireQuery }
  : E extends 'resolveOwnerProject'
    ? { signal: AbortSignal; projectAddress: ProjectWireAddress }
    : E extends 'getOwnerProject'
      ? { signal: AbortSignal; target: string }
      : { signal: AbortSignal; target: string; body: unknown; csrf: string; key: string }
const projectEndpoints: readonly ProjectEndpoint[] = [
  'listOwnerProjects',
  'getOwnerProject',
  'resolveOwnerProject',
  'updateOwnerProject',
  'lookupOwnerProject',
]

export function accountTransport(fetcher: Fetch = (url, init) => fetch(url, init)) {
  function request<T, E extends ProjectModelEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: ProjectModelOptions<E>,
  ): Promise<T>
  function request<T, E extends ProjectAuditEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: ProjectAuditOptions<E>,
  ): Promise<T>
  function request<T, E extends ProjectEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: ProjectOptions<E>,
  ): Promise<T>
  function request<T>(
    endpoint: 'getSystemRuntimeInformation',
    parse: (value: unknown) => T,
    options: { signal: AbortSignal },
  ): Promise<T>
  function request<T, E extends AuditEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: AuditOptions<E>,
  ): Promise<T>

  function request<T, E extends OutboundPolicyEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: OutboundPolicyOptions<E>,
  ): Promise<T>
  function request<T, E extends SMTPDeliveryEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: SMTPDeliveryOptions<E>,
  ): Promise<T>
  function request<T, E extends SMTPEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: SMTPOptions<E>,
  ): Promise<T>
  function request<T, E extends AccountSecurityEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: AccountSecurityOptions<E>,
  ): Promise<T>
  function request<T, E extends SelectionEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: SelectionOptions<E>,
  ): Promise<T>
  function request<T, E extends ModelEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: ModelOptions<E>,
  ): Promise<T>
  function request<T, E extends ProviderEndpoint>(
    endpoint: E,
    parse: (value: unknown) => T,
    options: ProviderOptions<E>,
  ): Promise<T>
  function request<T>(
    endpoint: 'systemInvitations',
    parse: (value: unknown) => T,
    options: InvitationReadOptions,
  ): Promise<T>
  function request<T>(
    endpoint: InvitationTarget,
    parse: (value: unknown) => T,
    options: RequestOptions & { target: string; users?: never; invitations?: never },
  ): Promise<T>
  function request<T>(
    endpoint: 'systemUsers',
    parse: (value: unknown) => T,
    options: UsersOptions,
  ): Promise<T>
  function request<T>(
    endpoint: Exclude<
      keyof typeof endpoints,
      | ProjectEndpoint
      | ProjectAuditEndpoint
      | ProjectModelEndpoint
      | 'systemUsers'
      | 'systemInvitations'
      | InvitationTarget
      | ProviderEndpoint
      | ModelEndpoint
      | SelectionEndpoint
      | AccountSecurityEndpoint
      | SMTPEndpoint
      | SMTPDeliveryEndpoint
      | OutboundPolicyEndpoint
      | AuditEndpoint
      | 'getSystemRuntimeInformation'
    >,
    parse: (value: unknown) => T,
    options: RequestOptions & { users?: never; invitations?: never; target?: never },
  ): Promise<T>
  async function request<T>(
    endpoint: keyof typeof endpoints,
    parse: (value: unknown) => T,
    options: RequestOptions & {
      users?: Readonly<{ cursor?: string }>
      invitations?: Readonly<{ cursor?: string }>
      providers?: Readonly<{ cursor?: string }>
      models?: Readonly<{ provider_id: string; cursor?: string }>
      mailJobs?: Readonly<{ cursor?: string }>
      audit?: AuditWireQuery
      projects?: ProjectWireQuery
      projectModels?: ProjectModelWireQuery
      projectAddress?: ProjectWireAddress
      projectID?: string
      target?: string
    },
  ): Promise<T> {
    if (!Object.hasOwn(endpoints, endpoint)) throw new AccountFailure('invalid-input')
    const [method, basePath, status] = endpoints[endpoint]
    let path: string = basePath
    if ((projectModelEndpoints as readonly string[]).includes(endpoint)) {
      try {
        const target = basePath.includes('{target}')
        const list =
          endpoint === 'listProjectModelProviders' ||
          endpoint === 'listProjectModels' ||
          endpoint === 'listProjectAvailableChatModels'
        shape(options, [
          'signal',
          'projectID',
          ...(target ? ['target'] : []),
          ...(list ? ['projectModels'] : []),
          ...(method === 'GET' ? [] : ['body', 'csrf', 'key']),
        ])
        const projectID = string(options.projectID, 36, 36)
        if (!uuid7.test(projectID)) throw new Error()
        path = basePath.replace('{project_id}', projectID)
        if (target) {
          const id = string(options.target, 36, 36)
          if (!uuid7.test(id)) throw new Error()
          path = path.replace('{target}', id)
        }
        if (list) {
          const query = shape(options.projectModels, [], ['cursor', 'limit'])
          const params = new URLSearchParams()
          if (Object.hasOwn(query, 'cursor')) {
            const cursor = string(query.cursor, 1, 8192)
            if (
              cursor.includes('\0') ||
              new TextEncoder().encode(cursor).byteLength > 8192 ||
              /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(cursor)
            )
              throw new Error()
            params.set('cursor', cursor)
          }
          if (Object.hasOwn(query, 'limit')) {
            const limit = query.limit
            if (typeof limit !== 'number' || !Number.isInteger(limit) || limit < 1 || limit > 100)
              throw new Error()
            params.set('limit', String(limit))
          }
          const encoded = params.toString()
          if (new TextEncoder().encode(encoded).byteLength > 32768) throw new Error()
          if (encoded) path += '?' + encoded
        }
        if (
          method !== 'GET' &&
          (!/^[A-Za-z0-9_-]{43}$/.test(string(options.csrf, 43, 43)) ||
            !/^[A-Za-z0-9._:/-]{1,128}$/.test(string(options.key, 1, 128)))
        )
          throw new Error()
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (endpoint === 'listProjectAudit' || endpoint === 'getProjectAudit') {
      try {
        shape(options, [
          'signal',
          'projectID',
          endpoint === 'listProjectAudit' ? 'audit' : 'target',
        ])
        const projectID = string(options.projectID, 36, 36)
        if (!uuid7.test(projectID)) throw new Error()
        path = basePath.replace('{project_id}', projectID)
        if (endpoint === 'listProjectAudit') {
          const encoded = new URLSearchParams(captureAuditWireQuery(options.audit)).toString()
          if (encoded) path += '?' + encoded
        } else {
          const target = string(options.target, 36, 36)
          if (!uuid7.test(target)) throw new Error()
          path = path.replace('{audit_id}', target)
        }
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (projectEndpoints.includes(endpoint as ProjectEndpoint)) {
      try {
        const hasTarget = basePath.includes('{id}')
        shape(options, [
          'signal',
          ...(hasTarget ? ['target'] : []),
          ...(method !== 'GET' ? ['body', 'csrf', 'key'] : []),
          ...(endpoint === 'listOwnerProjects' ? ['projects'] : []),
          ...(endpoint === 'resolveOwnerProject' ? ['projectAddress'] : []),
        ])
        if (hasTarget) {
          if (!uuid7.test(string(options.target, 36, 36))) throw new Error()
          path = basePath.replace('{id}', options.target!)
        }
        const params = new URLSearchParams()
        if (endpoint === 'listOwnerProjects') {
          const query = shape(options.projects, ['limit'], ['lifecycle', 'cursor'])
          if (
            typeof query.limit !== 'number' ||
            !Number.isInteger(query.limit) ||
            query.limit < 1 ||
            query.limit > 100
          )
            throw new Error()
          params.set('limit', String(query.limit))
          if (Object.hasOwn(query, 'lifecycle')) {
            const states = ['active', 'archiving', 'archived', 'deleting']
            if (
              !Array.isArray(query.lifecycle) ||
              !query.lifecycle.length ||
              query.lifecycle.length > 4 ||
              new Set(query.lifecycle).size !== query.lifecycle.length ||
              !query.lifecycle.every((v) => states.includes(v))
            )
              throw new Error()
            params.set(
              'lifecycle',
              states.filter((v) => (query.lifecycle as string[]).includes(v)).join(','),
            )
          }
          if (Object.hasOwn(query, 'cursor')) {
            const cursor = string(query.cursor, 1, 8192)
            if (
              new TextEncoder().encode(cursor).byteLength > 8192 ||
              /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(cursor)
            )
              throw new Error()
            params.set('cursor', cursor)
          }
        }
        if (endpoint === 'resolveOwnerProject') {
          const address = shape(options.projectAddress, ['username', 'project_name'])
          const username = string(address.username, 3, 32),
            name = string(address.project_name, 1, 64)
          if (
            !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])$/.test(username) ||
            !/^[a-z0-9._-]+$/.test(name) ||
            name === '.' ||
            name === '..'
          )
            throw new Error()
          params.set('username', username)
          params.set('project_name', name)
        }
        if (params.size) path += '?' + params.toString()
        if (
          method !== 'GET' &&
          (!/^[A-Za-z0-9_-]{43}$/.test(string(options.csrf, 43, 43)) ||
            !/^[A-Za-z0-9._:/-]{1,128}$/.test(string(options.key, 1, 128)))
        )
          throw new Error()
        if (
          endpoint === 'lookupOwnerProject' &&
          shape(options.body, ['command']).command !== 'update'
        )
          throw new Error()
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (endpoint === 'getSystemRuntimeInformation') {
      try {
        shape(options, ['signal'])
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (endpoint === 'listSystemAudit' || endpoint === 'getSystemAudit') {
      try {
        if (endpoint === 'listSystemAudit') {
          shape(options, ['signal', 'audit'])
          const encoded = new URLSearchParams(captureAuditWireQuery(options.audit)).toString()
          if (encoded) path += '?' + encoded
        } else {
          shape(options, ['signal', 'target'])
          if (!uuid7.test(string(options.target, 36, 36))) throw new Error()
          path = basePath.replace('{id}', options.target!)
        }
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (
      providerEndpoints.includes(endpoint as ProviderEndpoint) ||
      modelEndpoints.includes(endpoint as ModelEndpoint) ||
      selectionEndpoints.includes(endpoint as SelectionEndpoint) ||
      accountSecurityEndpoints.includes(endpoint as AccountSecurityEndpoint) ||
      smtpEndpoints.includes(endpoint as SMTPEndpoint) ||
      smtpDeliveryEndpoints.includes(endpoint as SMTPDeliveryEndpoint) ||
      outboundPolicyEndpoints.includes(endpoint as OutboundPolicyEndpoint)
    ) {
      try {
        const target = basePath.includes('{id}')
        const queryKey =
          endpoint === 'listProviders'
            ? 'providers'
            : endpoint === 'listProviderModels'
              ? 'models'
              : endpoint === 'listMailJobManagement'
                ? 'mailJobs'
                : undefined
        shape(options, [
          'signal',
          ...(method === 'GET' ? [] : ['body', 'csrf', 'key']),
          ...(target ? ['target'] : []),
          ...(queryKey ? [queryKey] : []),
        ])
        if (target) {
          if (!uuid7.test(string(options.target, 36, 36))) throw new Error()
          path = basePath.replace('{id}', options.target!)
        }
        if (queryKey) {
          const query = shape(options[queryKey], queryKey === 'models' ? ['provider_id'] : [], [
            'cursor',
          ])
          const params = new URLSearchParams()
          if (queryKey === 'models') {
            const provider = string(query.provider_id, 36, 36)
            if (!uuid7.test(provider)) throw new Error()
            params.set('provider_id', provider)
          }
          params.set('limit', '25')
          if (Object.hasOwn(query, 'cursor')) {
            const cursor = string(query.cursor, 1, 8192)
            if (queryKey === 'mailJobs' && new TextEncoder().encode(cursor).byteLength > 8192)
              throw new Error()
            params.set('cursor', cursor)
          }
          path += '?' + params.toString()
        }
        if (
          method !== 'GET' &&
          (!/^[A-Za-z0-9_-]{43}$/.test(string(options.csrf, 43, 43)) ||
            !/^[A-Za-z0-9._:/-]{1,128}$/.test(string(options.key, 1, 128)))
        )
          throw new Error()
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (endpoint === 'systemUsers') {
      try {
        shape(options, ['signal', 'users'])
        const query = shape(options.users, [], ['cursor'])
        const params = new URLSearchParams({ limit: '25' })
        if (Object.hasOwn(query, 'cursor')) params.set('cursor', string(query.cursor, 1, 8192))
        path += '?' + params.toString()
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (endpoint === 'systemInvitations') {
      try {
        shape(options, ['signal', 'invitations'])
        const query = shape(options.invitations, [], ['cursor'])
        const params = new URLSearchParams({ limit: '25' })
        if (Object.hasOwn(query, 'cursor')) params.set('cursor', string(query.cursor, 1, 8192))
        path += '?' + params.toString()
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (
      ['resendSystemInvitation', 'revokeSystemInvitation', 'retrySystemDelivery'].includes(endpoint)
    ) {
      try {
        shape(options, ['signal', 'body', 'csrf', 'key', 'target'])
        if (!uuid7.test(string(options.target, 36, 36))) throw new AccountFailure('invalid-input')
        path = path.replace('{id}', options.target!)
      } catch {
        throw new AccountFailure('invalid-input')
      }
    } else if (
      Object.hasOwn(options, 'projects') ||
      Object.hasOwn(options, 'projectModels') ||
      Object.hasOwn(options, 'projectAddress') ||
      Object.hasOwn(options, 'audit') ||
      Object.hasOwn(options, 'users') ||
      Object.hasOwn(options, 'invitations') ||
      Object.hasOwn(options, 'providers') ||
      Object.hasOwn(options, 'models') ||
      Object.hasOwn(options, 'mailJobs') ||
      Object.hasOwn(options, 'target')
    ) {
      throw new AccountFailure('invalid-input')
    }
    if (endpoint === 'createSystemInvitation') {
      try {
        shape(options, ['signal', 'body', 'csrf', 'key'])
      } catch {
        throw new AccountFailure('invalid-input')
      }
    }
    const headers: Record<string, string> = { Accept: 'application/json, application/problem+json' }
    let body: BodyInit | undefined
    if (endpoint === 'putAvatar' && options.avatar) {
      body = options.avatar.file
      headers['Content-Type'] = options.avatar.mediaType
      headers['If-Match'] = `"${options.avatar.version}"`
    } else if (method !== 'GET') {
      const maximum = (projectConfigurationWrites as readonly string[]).includes(endpoint)
        ? 1048576
        : endpoint === 'createProjectModelCredential' || endpoint === 'updateProjectModelCredential'
          ? 409600
          : endpoint === 'deleteProjectModelCredential' ||
              endpoint === 'lookupProjectModelCredential'
            ? 1024
            : endpoint === 'updateOwnerProject'
              ? 64 * 1024
              : endpoint === 'lookupOwnerProject'
                ? 1024
                : endpoint === 'updateOutboundPolicy'
                  ? 1024 * 1024
                  : endpoint === 'createModelCredential'
                    ? 512 * 1024
                    : endpoint === 'createProvider' ||
                        endpoint === 'updateProvider' ||
                        endpoint === 'updateSMTPSettings'
                      ? 32 * 1024
                      : 16 * 1024
      try {
        body = JSON.stringify(options.body)
      } catch {
        throw new AccountFailure('invalid-input')
      }
      if (!body || new TextEncoder().encode(body).byteLength > maximum)
        throw new AccountFailure('invalid-input')
      if (endpoint === 'updateOutboundPolicy') {
        try {
          // Measure the actual serialized field, not a second read of a mutable input.
          const actual = shape(JSON.parse(body), ['expected_version', 'rules'])
          const rules = JSON.stringify(actual.rules)
          if (!rules || new TextEncoder().encode(rules).byteLength > 512 * 1024) throw new Error()
        } catch {
          throw new AccountFailure('invalid-input')
        }
      }
      headers['Content-Type'] = 'application/json'
    }
    if (endpoint === 'avatar')
      headers.Accept = 'image/jpeg, image/png, image/webp, application/problem+json'
    if (options.csrf !== undefined) headers['X-CSRF-Token'] = options.csrf
    if (options.key !== undefined) headers['Idempotency-Key'] = options.key
    let response: Response
    try {
      response = await fetcher(path, {
        method,
        headers,
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: options.signal,
        ...(body === undefined ? {} : { body }),
      })
    } catch {
      throw new AccountFailure(options.signal.aborted ? 'cancelled' : 'transport')
    }
    try {
      if (options.signal.aborted) throw new AccountFailure('cancelled')
      if (response.redirected || response.type === 'opaqueredirect')
        throw new AccountFailure('invalid-response')
      if (response.status === 204 && status === 204) {
        if (endpoint === 'completePasswordReset' || endpoint === 'revokeSystemInvitation') {
          // A network 204 may expose an empty stream. Confirm its actual EOF,
          // rather than requiring the Fetch implementation to return null.
          try {
            await readEmptyBody(response, options.signal)
          } catch {
            throw new AccountFailure(options.signal.aborted ? 'cancelled' : 'invalid-response')
          }
          if (options.signal.aborted) throw new AccountFailure('cancelled')
        }
        return parse(undefined)
      }
      if (endpoint === 'avatar' && response.status === 200)
        return parse(await readAvatar(response, options.signal))
      const contentType = response.headers.get('Content-Type')?.split(';')[0]?.trim().toLowerCase()
      const success = response.status === status
      if (contentType !== (success ? 'application/json' : 'application/problem+json'))
        throw new AccountFailure('invalid-response')
      let value: unknown
      try {
        value = await readJSON(
          response,
          options.signal,
          (projectModelEndpoints as readonly string[]).includes(endpoint) && success
            ? (projectModelReads as readonly string[]).includes(endpoint)
              ? 8388608
              : 1024
            : endpoint === 'listOwnerProjects' && success
              ? 5 * 1024 * 1024
              : projectEndpoints.includes(endpoint as ProjectEndpoint) && success
                ? 64 * 1024
                : endpoint === 'getSystemRuntimeInformation' && success
                  ? 16 * 1024
                  : endpoint === 'listProviders' && success
                    ? 2 * 1024 * 1024
                    : (endpoint === 'listSystemAudit' ||
                          endpoint === 'getSystemAudit' ||
                          endpoint === 'listProjectAudit' ||
                          endpoint === 'getProjectAudit') &&
                        success
                      ? 1024 * 1024
                      : 600_000,
          success && (projectModelReads as readonly string[]).includes(endpoint),
          success && (endpoint === 'listProjectAudit' || endpoint === 'getProjectAudit'),
        )
      } catch {
        throw new AccountFailure(options.signal.aborted ? 'cancelled' : 'invalid-response')
      }
      if (options.signal.aborted) throw new AccountFailure('cancelled')
      if (!success)
        throw new AccountFailure(
          'problem',
          problem(value, response.status, response.headers.get('X-Request-ID')),
        )
      try {
        return parse(value)
      } catch {
        throw new AccountFailure('invalid-response')
      }
    } finally {
      // Includes aborted/redirected/wrong-media-type responses which never acquired a reader.
      // The controller may finish its bounded UI wait, but owns us until this actually returns.
      await response.body?.cancel().catch(() => undefined)
    }
  }
  return request
}
