import { shallowRef } from 'vue'
import { AccountFailure, shape, string, uuid7 } from '../api/client'
import {
  captureMCPOAuthBegin,
  captureMCPOAuthReturn,
  type MCPOAuthAPI,
  type MCPOAuthBegin,
  type MCPOAuthReturn,
  type MCPOAuthResult,
} from '../api/mcp-oauth'
import type { MCPSessionPorts } from './mcp-session'
import type { PersonalIdentity } from './useSession'

const storageKey = 'agenteam.mcp.oauth.v1'
type Original = {
  user: string
  session: string
  project: string
  returnPath: string
  command: 'oauth.begin' | 'oauth.complete'
  key: string
  flow: string
  state: string
  expires: string
}
export type MCPOAuthProgress = Readonly<{
  project: string
  returnPath: string
  phase: 'authorizing' | 'completing' | 'uncertain' | 'confirmed' | 'rejected' | 'stopped'
  observation: 'none' | 'pending' | 'not_found' | 'complete' | 'failed' | 'stopped'
  contextValid: boolean
}>
export interface MCPOAuthBrowser {
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
  navigate(url: string): void
}
function returnPath(value: string) {
  if (!/^\/[^/?#\\]+\/[^/?#\\]+\/settings\/mcp$/.test(value) || /%(?:2f|5c|00)/i.test(value))
    throw new AccountFailure('invalid-input')
  const url = new URL(value, 'https://app.invalid')
  if (url.origin !== 'https://app.invalid' || url.pathname !== value)
    throw new AccountFailure('invalid-input')
  return value
}
export function createMCPOAuthSession(
  api: MCPOAuthAPI,
  ports: MCPSessionPorts,
  suppliedBrowser?: MCPOAuthBrowser,
) {
  const browser = () =>
    suppliedBrowser ?? {
      storage: window.sessionStorage,
      navigate: (url: string) => window.location.assign(url),
    }
  const state = shallowRef<Omit<MCPOAuthProgress, 'contextValid'> | null>(null)
  let context: PersonalIdentity | null = null
  let original: Original | null = null
  let callback: MCPOAuthReturn | null = null
  let callbackInvalid = false
  let active = false
  let generation = 0
  function read(): Original | null {
    const raw = browser().storage.getItem(storageKey)
    if (!raw) return null
    try {
      if (raw.length > 4096) throw new Error()
      const r = shape(JSON.parse(raw), [
        'user',
        'session',
        'project',
        'returnPath',
        'command',
        'key',
        'flow',
        'state',
        'expires',
      ])
      const value: Original = {
        user: string(r.user, 36, 36),
        session: string(r.session, 36, 36),
        project: string(r.project, 36, 36),
        returnPath: returnPath(string(r.returnPath, 1, 1024)),
        command: r.command as Original['command'],
        key: string(r.key, 1, 128),
        flow: string(r.flow, 0, 36),
        state: string(r.state, 0, 43),
        expires: string(r.expires, 0, 64),
      }
      if (
        ![value.user, value.session, value.project].every((v) => uuid7.test(v)) ||
        !['oauth.begin', 'oauth.complete'].includes(value.command) ||
        !/^[A-Za-z0-9._:/-]{1,128}$/.test(value.key)
      )
        throw new Error()
      if (
        value.state &&
        (!/^[A-Za-z0-9_-]{43}$/.test(value.state) ||
          !uuid7.test(value.flow) ||
          !Number.isFinite(Date.parse(value.expires)))
      )
        throw new Error()
      return value
    } catch {
      throw new AccountFailure('invalid-input')
    }
  }
  function save(value: Original) {
    try {
      browser().storage.setItem(storageKey, JSON.stringify(value))
    } catch {
      throw new AccountFailure('invalid-input')
    }
    original = value
  }
  function remove() {
    browser().storage.removeItem(storageKey)
    original = null
  }
  function match(value: Original, identity: PersonalIdentity) {
    return (
      value.user === identity.userID &&
      value.session === identity.sessionID &&
      ports.current(identity)
    )
  }
  function publish(
    value: Original,
    identity: PersonalIdentity,
    phase: MCPOAuthProgress['phase'],
    observation: MCPOAuthProgress['observation'] = 'none',
  ) {
    context = identity
    state.value = Object.freeze({
      project: value.project,
      returnPath: value.returnPath,
      phase,
      observation,
    })
  }
  function settle(value: Original, identity: PersonalIdentity, result: MCPOAuthResult) {
    if (result.connection.project_id !== value.project) throw new AccountFailure('invalid-response')
    if (value.command === 'oauth.begin' && result.oauth) {
      if (Date.parse(result.oauth.expires_at) <= Date.now())
        throw new AccountFailure('invalid-response')
      const url = new URL(result.oauth.authorization_url)
      save({
        ...value,
        flow: result.oauth.flow_id,
        state: url.searchParams.get('state')!,
        expires: result.oauth.expires_at,
      })
      publish(value, identity, 'authorizing')
      browser().navigate(result.oauth.authorization_url)
      return
    }
    remove()
    publish(
      value,
      identity,
      result.connection.state === 'connected' ? 'confirmed' : 'rejected',
      'complete',
    )
  }
  async function perform(
    value: Original,
    identity: PersonalIdentity,
    csrf: string,
    body: MCPOAuthBegin | MCPOAuthReturn | null,
  ) {
    if (active || ports.busy() || !match(value, identity)) throw new AccountFailure('busy')
    active = true
    const ticket = generation
    let dispatched = false
    try {
      const result = await ports.run(
        identity,
        body ? 'mcp-write' : 'mcp-lookup',
        async (signal, current) => {
          if (ticket !== generation || !current() || !match(value, identity))
            throw new AccountFailure('cancelled')
          dispatched = true
          const options = { signal, key: value.key, csrfToken: csrf }
          if (!body) return api.lookup(value.project, value.command, options)
          return value.command === 'oauth.begin'
            ? api.begin(value.project, body as MCPOAuthBegin, options)
            : api.complete(body as MCPOAuthReturn, options)
        },
      )
      if (ticket !== generation || !match(value, identity)) throw new AccountFailure('cancelled')
      if ('state' in result) {
        if (result.state === 'stopped') {
          remove()
          publish(value, identity, 'stopped', 'stopped')
          return
        }
        if (result.state !== 'complete') {
          publish(value, identity, 'uncertain', result.state)
          return
        }
        settle(value, identity, result.result)
      } else settle(value, identity, result)
    } catch (error) {
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (ticket === generation && match(value, identity)) {
        const rejected =
          !!body &&
          (!dispatched ||
            (!!failure.problem &&
              ['not_started', 'not_committed'].includes(failure.problem.commit_state) &&
              failure.problem.code !== 'IDEMPOTENCY_KEY_REUSED'))
        publish(value, identity, rejected ? 'rejected' : 'uncertain', body ? 'none' : 'failed')
        if (rejected) remove()
      }
      throw failure
    } finally {
      if (ticket === generation) active = false
    }
  }
  return {
    restore(publishedIdentity?: PersonalIdentity) {
      if (active) return
      const identity = publishedIdentity ?? ports.identity()
      if (!ports.current(identity)) return
      try {
        const saved = read()
        if (!saved || !match(saved, identity)) return
        original = saved
        publish(
          saved,
          identity,
          saved.command === 'oauth.begin' && !!saved.state ? 'authorizing' : 'uncertain',
        )
      } catch {
        state.value = Object.freeze({
          project: '',
          returnPath: '/projects',
          phase: 'rejected',
          observation: 'none',
        })
        context = identity
      }
    },
    get pending() {
      return active || !!original
    },
    get progress(): MCPOAuthProgress | null {
      return state.value
        ? Object.freeze({ ...state.value, contextValid: !!context && ports.current(context) })
        : null
    },
    async begin(project: string, input: MCPOAuthBegin, path: string) {
      if (active || ports.busy()) throw new AccountFailure('busy')
      const captured = ports.capture(),
        body = captureMCPOAuthBegin(input)
      if (!uuid7.test(project)) throw new AccountFailure('invalid-input')
      const saved = read()
      if (
        saved &&
        match(saved, captured.identity) &&
        (saved.command === 'oauth.complete' ||
          !saved.expires ||
          Date.parse(saved.expires) > Date.now())
      )
        throw new AccountFailure('busy')
      const value: Original = {
        user: captured.identity.userID,
        session: captured.identity.sessionID,
        project,
        returnPath: returnPath(path),
        command: 'oauth.begin',
        key: captured.key,
        flow: '',
        state: '',
        expires: '',
      }
      save(value)
      publish(value, captured.identity, 'authorizing')
      await perform(value, captured.identity, captured.csrf, body)
    },
    // The router calls this synchronously before session restoration. A second
    // navigation to the clean URL must retain the first in-memory callback.
    captureReturn(url: string, replaceURL: (path: string) => void) {
      const parsed = new URL(url, 'https://app.invalid')
      if (parsed.pathname !== '/mcp/oauth/callback' || (!parsed.search && !parsed.hash)) return
      const query = parsed.searchParams
      replaceURL('/mcp/oauth/callback')
      callback = null
      callbackInvalid = false
      try {
        if (
          parsed.hash ||
          [...query.keys()].some((k) => !['state', 'code', 'iss'].includes(k)) ||
          ['state', 'code'].some((k) => query.getAll(k).length !== 1) ||
          query.getAll('iss').length > 1
        )
          throw new Error()
        callback = captureMCPOAuthReturn({
          state: query.get('state'),
          code: query.get('code'),
          issuer: query.get('iss') ?? '',
        })
      } catch {
        callbackInvalid = true
      }
    },
    async completeReturn() {
      if (active || ports.busy()) throw new AccountFailure('busy')
      const captured = ports.capture(),
        value = original ?? read(),
        supplied = callback
      callback = null
      if (
        !value ||
        !match(value, captured.identity) ||
        callbackInvalid ||
        (supplied && supplied.state !== value.state)
      ) {
        state.value = Object.freeze({
          project: '',
          returnPath: '/projects',
          phase: 'rejected',
          observation: 'none',
        })
        context = captured.identity
        throw new AccountFailure('invalid-input')
      }
      original = value
      if (value.command === 'oauth.complete') {
        publish(value, captured.identity, 'uncertain')
        return perform(value, captured.identity, captured.csrf, null)
      }
      if (!supplied || !value.state || Date.parse(value.expires) <= Date.now()) {
        publish(value, captured.identity, 'rejected')
        remove()
        throw new AccountFailure('invalid-input')
      }
      const completing = { ...value, command: 'oauth.complete' as const, key: captured.key }
      // Persist the original exchange key before dispatch, never the code.
      save(completing)
      publish(completing, captured.identity, 'completing')
      await perform(completing, captured.identity, captured.csrf, supplied)
    },
    async lookup() {
      const captured = ports.capture(),
        value = original ?? read()
      if (!value || !match(value, captured.identity)) throw new AccountFailure('invalid-input')
      original = value
      publish(value, captured.identity, 'uncertain')
      await perform(value, captured.identity, captured.csrf, null)
    },
    clear(options?: { preserveUnboundReturn?: boolean }) {
      const preserveReturn = options?.preserveUnboundReturn === true && context === null && !active
      ++generation
      active = false
      original = null
      context = null
      state.value = null
      if (!preserveReturn) {
        callback = null
        callbackInvalid = false
        try {
          browser().storage.removeItem(storageKey)
        } catch {
          /* Browser storage cannot prevent identity revocation. */
        }
      }
      // Only the first identity publication may preserve a captured return.
      // Logout, revocation and later identity changes erase it synchronously.
    },
  }
}
