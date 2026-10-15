import { shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureMemoryDelete,
  type AgentMemoryAPI,
  type MemoryDelete,
  type MemoryLookup,
  type MemoryQuery,
  type MemoryReceipt,
} from '../api/agent-memory'
import { workID } from '../api/work-review'
import type { PersonalIdentity } from './useSession'

export type MemoryAction = 'agent-memory-read' | 'agent-memory-delete' | 'agent-memory-lookup'
type Original = {
  identity: PersonalIdentity
  csrf: string
  key: string
  project: string
  agent: string
  input: MemoryDelete
  body: string
}
export type MemoryProgress = Readonly<{
  project: string
  agent: string
  memory: string
  phase: 'submitting' | 'uncertain' | 'confirmed' | 'rejected'
  contextValid: boolean
}>
export function createAgentMemorySession(
  api: AgentMemoryAPI,
  ports: {
    capture(): { identity: PersonalIdentity; csrf: string; key: string }
    identity(): PersonalIdentity
    current(identity: PersonalIdentity): boolean
    busy(): boolean
    run<T>(
      identity: PersonalIdentity,
      kind: MemoryAction,
      work: (signal: AbortSignal, current: () => boolean) => Promise<T>,
    ): Promise<T>
    cancelRead(): void
  },
) {
  let original: Original | null = null
  const state = shallowRef<Omit<MemoryProgress, 'contextValid'> | null>(null)
  const live = (value: Original) => original === value && ports.current(value.identity)
  function publish(value: Original, phase: MemoryProgress['phase']) {
    state.value = Object.freeze({
      project: value.project,
      agent: value.agent,
      memory: value.input.memory_id,
      phase,
    })
  }
  async function perform(value: Original, lookup: boolean): Promise<MemoryReceipt | MemoryLookup> {
    let dispatched = false
    try {
      if (ports.busy() || !live(value)) throw new AccountFailure('busy')
      const result = await ports.run(
        value.identity,
        lookup ? 'agent-memory-lookup' : 'agent-memory-delete',
        async (signal, current) => {
          if (!current() || !live(value)) throw new AccountFailure('cancelled')
          if (JSON.stringify(value.input) !== value.body) throw new AccountFailure('invalid-input')
          dispatched = true
          const write = { signal, csrfToken: value.csrf, key: value.key }
          return lookup
            ? api.lookup(value.project, value.agent, value.input, write)
            : api.delete(value.project, value.agent, value.input, write)
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      if ('status' in result && result.status === 'not_found') {
        publish(value, 'uncertain')
        if (!live(value)) throw new AccountFailure('cancelled')
        return result
      }
      publish(value, 'confirmed')
      if (!live(value)) throw new AccountFailure('cancelled')
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
        publish(value, rejected ? 'rejected' : 'uncertain')
        if (rejected) original = null
      }
      throw failure
    }
  }
  return {
    get progress(): MemoryProgress | null {
      return state.value
        ? Object.freeze({
            ...state.value,
            contextValid: !!original && ports.current(original.identity),
          })
        : null
    },
    list(project: string, agent: string, query: MemoryQuery) {
      return ports.run(ports.identity(), 'agent-memory-read', (signal) =>
        api.list(project, agent, query, signal),
      )
    },
    history(project: string, agent: string, memory: string, query: MemoryQuery) {
      return ports.run(ports.identity(), 'agent-memory-read', (signal) =>
        api.history(project, agent, memory, query, signal),
      )
    },
    start(project: string, agent: string, input: MemoryDelete) {
      if (original || ports.busy()) return Promise.reject(new AccountFailure('busy'))
      const captured = ports.capture(),
        command = captureMemoryDelete(input)
      const value: Original = {
        ...captured,
        project: workID(project),
        agent: workID(agent),
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
    clear() {
      original = null
      state.value = null
    },
  }
}
