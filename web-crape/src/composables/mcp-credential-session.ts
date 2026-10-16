import { shallowRef } from 'vue'
import { AccountFailure, uuid7 } from '../api/client'
import {
  captureMCPCredentialTarget,
  captureMCPCredentialValue,
  type MCPCredentialsAPI,
  type MCPCredentialLookupTarget,
  type MCPCredentialMutation,
} from '../api/project-mcp-credentials'
import type { MCPSessionPorts } from './mcp-session'

export type MCPCredentialProgress = Readonly<{
  project: string
  config: string | null
  command: MCPCredentialLookupTarget
  phase: 'submitting' | 'uncertain' | 'confirmed' | 'rejected'
  observation: 'none' | 'not_found' | 'failed' | 'complete'
  result: MCPCredentialMutation | null
  contextValid: boolean
}>
type Original = ReturnType<MCPSessionPorts['capture']> & {
  project: string
  config: string | null
  command: MCPCredentialLookupTarget
}
function identifier(value: string): string {
  if (typeof value !== 'string' || !uuid7.test(value)) throw new AccountFailure('invalid-input')
  return value
}

// A pending original retains only its receipt identity. Material exists for
// the one dispatch and is never published, retained for lookup or retried.
export function createMCPCredentialSession(api: MCPCredentialsAPI, ports: MCPSessionPorts) {
  let original: Original | null = null
  let context: Original['identity'] | null = null
  const state = shallowRef<Omit<MCPCredentialProgress, 'contextValid'> | null>(null)
  const live = (value: Original) => original === value && ports.current(value.identity)
  function publish(
    value: Original,
    phase: MCPCredentialProgress['phase'],
    observation: MCPCredentialProgress['observation'] = 'none',
    result: MCPCredentialMutation | null = null,
  ) {
    context = value.identity
    state.value = Object.freeze({
      project: value.project,
      config: value.config,
      command: value.command,
      phase,
      observation,
      result,
    })
  }
  async function perform(value: Original, lookup: boolean, material?: string) {
    let dispatched = false
    try {
      if (ports.busy() || !live(value)) throw new AccountFailure('busy')
      const result = await ports.run(
        value.identity,
        lookup ? 'mcp-lookup' : 'mcp-write',
        async (signal, current) => {
          if (!current() || !live(value)) throw new AccountFailure('cancelled')
          const options = { signal, csrfToken: value.csrf, key: value.key }
          const command = value.command
          dispatched = true
          if (lookup) return api.lookupCredential(value.project, command, options)
          if (command.kind === 'create')
            return api.createCredential(value.project, material!, options)
          if (command.kind === 'update')
            return api.updateCredential(
              value.project,
              command.credential_id,
              command.expected_version,
              material!,
              options,
            )
          return api.deleteCredential(
            value.project,
            command.credential_id,
            command.expected_version,
            options,
          )
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      if ('observed' in result && !result.observed) {
        publish(value, 'uncertain', 'not_found')
        return result
      }
      publish(
        value,
        'confirmed',
        lookup ? 'complete' : 'none',
        'observed' in result ? result.result : result,
      )
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
    } finally {
      material = undefined
    }
  }
  return {
    get pending() {
      return original !== null
    },
    get progress(): MCPCredentialProgress | null {
      return state.value
        ? Object.freeze({ ...state.value, contextValid: !!context && ports.current(context) })
        : null
    },
    async metadata(project: string, credential: string) {
      const p = identifier(project),
        c = identifier(credential)
      return ports.run(ports.identity(), 'mcp-read', (signal) =>
        api.getCredentialMetadata(p, c, signal),
      )
    },
    async start(
      project: string,
      command: MCPCredentialLookupTarget,
      material?: string,
      config?: string,
    ) {
      if (original || ports.busy()) throw new AccountFailure('busy')
      const captured = captureMCPCredentialTarget(command)
      const secret = captured.kind === 'delete' ? undefined : captureMCPCredentialValue(material!)
      const value: Original = {
        ...ports.capture(),
        project: identifier(project),
        config: config === undefined ? null : identifier(config),
        command: captured,
      }
      original = value
      publish(value, 'submitting')
      return perform(value, false, secret)
    },
    lookup() {
      if (!original || state.value?.phase !== 'uncertain')
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, true)
    },
    clear() {
      original = null
      context = null
      state.value = null
    },
  }
}
