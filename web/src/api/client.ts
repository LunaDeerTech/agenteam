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
  updateModelSelection: ['PUT', '/api/v1/system/model-selection', 200],
  lookupModelSelectionCommand: ['POST', '/api/v1/system/model-commands/lookup', 200],
  getAccountSecurity: ['GET', '/api/v1/system/account-settings', 200],
  updateAccountSecurity: ['PUT', '/api/v1/system/account-settings', 200],
  getSMTPSettings: ['GET', '/api/v1/system/smtp', 200],
  updateSMTPSettings: ['PUT', '/api/v1/system/smtp', 200],
  unconfigureSMTP: ['POST', '/api/v1/system/smtp/unconfigure', 200],
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

async function readJSON(
  response: Response,
  signal: AbortSignal,
  maximum = 600_000,
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
    return JSON.parse(text) as unknown
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
  'getModelSelection' | 'updateModelSelection' | 'lookupModelSelectionCommand'
type SelectionOptions<E extends SelectionEndpoint> = E extends 'getModelSelection'
  ? { signal: AbortSignal }
  : { signal: AbortSignal; body: unknown; csrf: string; key: string }
const selectionEndpoints: readonly SelectionEndpoint[] = [
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

export function accountTransport(fetcher: Fetch = (url, init) => fetch(url, init)) {
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
      | 'systemUsers'
      | 'systemInvitations'
      | InvitationTarget
      | ProviderEndpoint
      | ModelEndpoint
      | SelectionEndpoint
      | AccountSecurityEndpoint
      | SMTPEndpoint
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
      target?: string
    },
  ): Promise<T> {
    if (!Object.hasOwn(endpoints, endpoint)) throw new AccountFailure('invalid-input')
    const [method, basePath, status] = endpoints[endpoint]
    let path: string = basePath
    if (
      providerEndpoints.includes(endpoint as ProviderEndpoint) ||
      modelEndpoints.includes(endpoint as ModelEndpoint) ||
      selectionEndpoints.includes(endpoint as SelectionEndpoint) ||
      accountSecurityEndpoints.includes(endpoint as AccountSecurityEndpoint) ||
      smtpEndpoints.includes(endpoint as SMTPEndpoint)
    ) {
      try {
        const target = basePath.includes('{id}')
        const queryKey =
          endpoint === 'listProviders'
            ? 'providers'
            : endpoint === 'listProviderModels'
              ? 'models'
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
          if (Object.hasOwn(query, 'cursor')) params.set('cursor', string(query.cursor, 1, 8192))
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
      Object.hasOwn(options, 'users') ||
      Object.hasOwn(options, 'invitations') ||
      Object.hasOwn(options, 'providers') ||
      Object.hasOwn(options, 'models') ||
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
      const maximum =
        endpoint === 'createModelCredential'
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
          endpoint === 'listProviders' && success ? 2 * 1024 * 1024 : 600_000,
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
