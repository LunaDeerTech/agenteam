import { shallowRef } from 'vue'
import { AccountFailure, uuid7 } from '../api/client'
import {
  captureMCPConfigWrite,
  captureMCPConnect,
  type ProjectMCPAPI,
  type MCPConfigWrite,
  type MCPConnectInput,
  type MCPConnectionAction,
  type MCPCommand,
  type MCPCommandResult,
  type MCPLookup,
} from '../api/project-mcp'
import type { PersonalIdentity } from './useSession'
import type { MCPCredentialsAPI } from '../api/project-mcp-credentials'
import { createMCPCredentialSession } from './mcp-credential-session'
import { createMCPOAuthAPI, type MCPOAuthAPI } from '../api/mcp-oauth'
import { createMCPOAuthSession } from './mcp-oauth-session'

export type MCPAction = 'mcp-read' | 'mcp-write' | 'mcp-lookup'
export type MCPMutation = Readonly<
  | { kind: 'config.create'; input: MCPConfigWrite }
  | { kind: 'config.update'; target: string; input: MCPConfigWrite }
  | { kind: 'config.delete'; target: string; version: string }
  | { kind: 'connection.connect'; input: MCPConnectInput }
  | { kind: `connection.${MCPConnectionAction}`; target: string; version: string }
>
type Original = {
  identity: PersonalIdentity
  csrf: string
  key: string
  project: string
  input: MCPMutation
  body: string
}
export type MCPProgress = Readonly<{
  project: string
  command: MCPCommand
  target: string | null
  phase: 'submitting' | 'uncertain' | 'confirmed' | 'rejected' | 'stopped'
  observation: 'none' | 'pending' | 'not_found' | 'failed' | 'complete' | 'stopped'
  contextValid: boolean
}>
function identifier(v: string): string {
  if (typeof v !== 'string' || !uuid7.test(v)) throw new AccountFailure('invalid-input')
  return v
}
function version(v: string): string {
  if (typeof v !== 'string' || !/^[1-9][0-9]{0,18}$/.test(v) || BigInt(v) > 9223372036854775807n)
    throw new AccountFailure('invalid-input')
  return v
}
function capture(input: MCPMutation): MCPMutation {
  switch (input.kind) {
    case 'config.create':
      return Object.freeze({ kind: input.kind, input: captureMCPConfigWrite(input.input, true) })
    case 'config.update':
      return Object.freeze({
        kind: input.kind,
        target: identifier(input.target),
        input: captureMCPConfigWrite(input.input, false),
      })
    case 'connection.connect':
      return Object.freeze({ kind: input.kind, input: captureMCPConnect(input.input) })
    case 'config.delete':
    case 'connection.refresh':
    case 'connection.enable':
    case 'connection.disable':
    case 'connection.disconnect':
      return Object.freeze({
        kind: input.kind,
        target: identifier(input.target),
        version: version(input.version),
      })
    default:
      throw new AccountFailure('invalid-input')
  }
}
export interface MCPSessionPorts {
  capture(): { identity: PersonalIdentity; csrf: string; key: string }
  identity(): PersonalIdentity
  current(identity: PersonalIdentity): boolean
  busy(): boolean
  run<T>(
    identity: PersonalIdentity,
    kind: MCPAction,
    work: (signal: AbortSignal, current: () => boolean) => Promise<T>,
  ): Promise<T>
  cancelRead(): void
}
export function createMCPSession(
  api: ProjectMCPAPI,
  ports: MCPSessionPorts,
  credentialAPI: MCPCredentialsAPI,
  oauthAPI: MCPOAuthAPI = createMCPOAuthAPI(),
) {
  let original: Original | null = null
  let oauth: ReturnType<typeof createMCPOAuthSession>
  const credentials = createMCPCredentialSession(credentialAPI, {
    ...ports,
    busy: () => !!original || !!oauth?.pending || ports.busy(),
  })
  oauth = createMCPOAuthSession(oauthAPI, {
    ...ports,
    busy: () => !!original || credentials.pending || ports.busy(),
  })
  let context: PersonalIdentity | null = null
  const state = shallowRef<Omit<MCPProgress, 'contextValid'> | null>(null)
  const live = (value: Original) => original === value && ports.current(value.identity)
  function publish(
    value: Original,
    phase: MCPProgress['phase'],
    observation: MCPProgress['observation'] = 'none',
  ) {
    context = value.identity
    state.value = Object.freeze({
      project: value.project,
      command: value.input.kind,
      target: 'target' in value.input ? value.input.target : null,
      phase,
      observation,
    })
  }
  function dispatch(value: Original, signal: AbortSignal): Promise<MCPCommandResult> {
    const { project, input } = value
    const write = { signal, csrfToken: value.csrf, key: value.key }
    switch (input.kind) {
      case 'config.create':
        return api.createConfig(project, input.input, write)
      case 'config.update':
        return api.updateConfig(project, input.target, input.input, write)
      case 'config.delete':
        return api.deleteConfig(project, input.target, input.version, write)
      case 'connection.connect':
        return api.connect(project, input.input, write)
      default:
        return api.changeConnection(
          project,
          input.target,
          input.kind.slice('connection.'.length) as MCPConnectionAction,
          input.version,
          write,
        )
    }
  }
  async function perform(value: Original, lookup: boolean): Promise<MCPCommandResult | MCPLookup> {
    let dispatched = false
    try {
      if (ports.busy() || !live(value)) throw new AccountFailure('busy')
      const result = await ports.run(
        value.identity,
        lookup ? 'mcp-lookup' : 'mcp-write',
        async (signal, current) => {
          if (!current() || !live(value)) throw new AccountFailure('cancelled')
          if (JSON.stringify(value.input) !== value.body) throw new AccountFailure('invalid-input')
          dispatched = true
          return lookup
            ? api.lookup(value.project, value.input.kind, {
                signal,
                csrfToken: value.csrf,
                key: value.key,
              })
            : dispatch(value, signal)
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      if ('state' in result && result.state !== 'complete') {
        if (result.state === 'stopped') {
          if (!lookup || value.input.kind !== 'connection.refresh')
            throw new AccountFailure('invalid-input')
          publish(value, 'stopped', 'stopped')
          original = null
          return result
        }
        publish(value, 'uncertain', result.state)
        return result
      }
      publish(value, 'confirmed', lookup ? 'complete' : 'none')
      original = null
      return result
    } catch (error) {
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (live(value)) {
        const rejected =
          !lookup &&
          (!dispatched ||
            (!!failure.problem &&
              ['not_started', 'not_committed'].includes(failure.problem.commit_state) &&
              failure.problem.code !== 'IDEMPOTENCY_KEY_REUSED'))
        publish(value, rejected ? 'rejected' : 'uncertain', lookup ? 'failed' : 'none')
        if (rejected) original = null
      }
      throw failure
    }
  }
  return {
    credentials,
    oauth,
    get progress(): MCPProgress | null {
      return state.value
        ? Object.freeze({ ...state.value, contextValid: !!context && ports.current(context) })
        : null
    },
    async configs(project: string) {
      const target = identifier(project)
      return ports.run(ports.identity(), 'mcp-read', (signal) => api.configs(target, signal))
    },
    async connections(project: string) {
      const target = identifier(project)
      return ports.run(ports.identity(), 'mcp-read', (signal) => api.connections(target, signal))
    },
    async tools(project: string, connection: string) {
      const target = identifier(project),
        c = identifier(connection)
      return ports.run(ports.identity(), 'mcp-read', (signal) => api.tools(target, c, signal))
    },
    async resources(project: string, connection: string) {
      const target = identifier(project),
        c = identifier(connection)
      return ports.run(ports.identity(), 'mcp-read', (signal) => api.resources(target, c, signal))
    },
    async start(project: string, input: MCPMutation) {
      if (original || credentials.pending || oauth.pending || ports.busy())
        return Promise.reject(new AccountFailure('busy'))
      const captured = ports.capture(),
        command = capture(input)
      const value: Original = {
        ...captured,
        project: identifier(project),
        input: command,
        body: JSON.stringify(command),
      }
      original = value
      publish(value, 'submitting')
      return perform(value, false)
    },
    lookup() {
      if (!original || state.value?.phase !== 'uncertain')
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, true)
    },
    abandonRead: ports.cancelRead,
    clear(options?: { preserveUnboundReturn?: boolean }) {
      oauth.clear(options)
      credentials.clear()
      original = null
      context = null
      state.value = null
    },
  }
}
