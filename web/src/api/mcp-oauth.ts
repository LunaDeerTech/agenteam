import {
  AccountFailure,
  accountTransport,
  object,
  shape,
  string,
  uuid7,
  type Fetch,
} from './client'
import type { WriteOptions } from './account'
import { parseMCPConnection, type MCPConnection } from './project-mcp'

export type MCPOAuthBegin = Readonly<{ config_id: string; expected_version: string }>
export type MCPOAuthReturn = Readonly<{ state: string; code: string; issuer: string }>
export type MCPOAuthCommand = 'oauth.begin' | 'oauth.complete'
export type MCPOAuthResult = Readonly<{
  connection: MCPConnection
  oauth?: Readonly<{ flow_id: string; authorization_url: string; expires_at: string }>
}>
export type MCPOAuthObservation = Readonly<
  { state: 'pending' | 'not_found' | 'stopped' } | { state: 'complete'; result: MCPOAuthResult }
>
export interface MCPOAuthAPI {
  begin(project: string, input: MCPOAuthBegin, options: WriteOptions): Promise<MCPOAuthResult>
  complete(input: MCPOAuthReturn, options: WriteOptions): Promise<MCPOAuthResult>
  lookup(
    project: string,
    command: MCPOAuthCommand,
    options: WriteOptions,
  ): Promise<MCPOAuthObservation>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function capture<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureMCPOAuthBegin(input: unknown): MCPOAuthBegin {
  return capture(() => {
    const v = shape(input, ['config_id', 'expected_version'])
    const version = string(v.expected_version, 1, 19)
    requireValue(/^(0|[1-9][0-9]*)$/.test(version) && BigInt(version) <= 9223372036854775807n)
    return Object.freeze({ config_id: id(v.config_id), expected_version: version })
  })
}
export function captureMCPOAuthReturn(input: unknown): MCPOAuthReturn {
  return capture(() => {
    const v = shape(input, ['state', 'code', 'issuer'])
    const state = string(v.state, 43, 43),
      code = string(v.code, 1, 8192),
      issuer = string(v.issuer, 0, 16384)
    requireValue(/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(state) && /^[\x21-\x7e]+$/.test(code))
    requireValue(!issuer.includes('\0') && new TextEncoder().encode(issuer).byteLength <= 16384)
    return Object.freeze({ state, code, issuer })
  })
}
export function parseMCPOAuthResult(
  value: unknown,
  command: MCPOAuthCommand,
  project?: string,
): MCPOAuthResult {
  const v = shape(value, ['connection'], command === 'oauth.begin' ? ['oauth'] : [])
  const connectionValue = object(v.connection)
  const actualProject = id(connectionValue.project_id)
  requireValue(project === undefined || actualProject === project)
  const connection = parseMCPConnection(v.connection, actualProject)
  if (!Object.hasOwn(v, 'oauth')) return Object.freeze({ connection })
  const o = shape(v.oauth, ['flow_id', 'authorization_url', 'expires_at'])
  const authorizationURL = string(o.authorization_url, 1, 16384),
    expiresAt = string(o.expires_at, 1, 64)
  const url = new URL(authorizationURL)
  requireValue(
    url.protocol === 'https:' &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.hash &&
      !/[\u0000-\u0020\u007f]/.test(authorizationURL),
  )
  requireValue(
    url.searchParams.getAll('state').length === 1 &&
      /^[A-Za-z0-9_-]{43}$/.test(url.searchParams.get('state') ?? ''),
  )
  requireValue(Number.isFinite(Date.parse(expiresAt)))
  return Object.freeze({
    connection,
    oauth: Object.freeze({
      flow_id: id(o.flow_id),
      authorization_url: authorizationURL,
      expires_at: expiresAt,
    }),
  })
}
export function createMCPOAuthAPI(fetcher?: Fetch): MCPOAuthAPI {
  const request = accountTransport(fetcher)
  const options = (body: unknown, write: WriteOptions) => ({
    body,
    csrf: write.csrfToken,
    key: write.key,
    signal: write.signal ?? new AbortController().signal,
  })
  return {
    begin(project, input, write) {
      const p = capture(() => id(project)),
        body = captureMCPOAuthBegin(input)
      return request('beginProjectMCPOAuth', (v) => parseMCPOAuthResult(v, 'oauth.begin', p), {
        ...options(body, write),
        projectID: p,
      })
    },
    complete(input, write) {
      const body = captureMCPOAuthReturn(input)
      return request(
        'completeMCPOAuth',
        (v) => parseMCPOAuthResult(v, 'oauth.complete'),
        options(body, write),
      )
    },
    lookup(project, command, write) {
      const p = capture(() => id(project))
      if (command !== 'oauth.begin' && command !== 'oauth.complete')
        throw new AccountFailure('invalid-input')
      return request(
        'lookupProjectMCP',
        (v) => {
          const o = shape(v, ['state'], ['result'])
          if (o.state === 'complete')
            return Object.freeze({
              state: 'complete' as const,
              result: parseMCPOAuthResult(o.result, command, p),
            })
          shape(v, ['state'])
          requireValue(o.state === 'pending' || o.state === 'not_found' || o.state === 'stopped')
          return Object.freeze({ state: o.state })
        },
        { ...options({ command }, write), projectID: p },
      )
    },
  }
}
