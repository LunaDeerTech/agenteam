import { shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import { captureKnowledgeID, type KnowledgeDocument } from '../api/knowledge-owner'
import {
  captureKnowledgeFile,
  type KnowledgeFileAPI,
  type KnowledgeFileIntent,
  type KnowledgeFileLookup,
  type KnowledgeFileResult,
} from '../api/knowledge-file'
import type { PersonalIdentity } from './useSession'

export type KnowledgeFileAction =
  'knowledge-file-read' | 'knowledge-file-write' | 'knowledge-file-lookup'
export interface KnowledgeFileSessionPorts {
  capture(): { identity: PersonalIdentity; csrf: string; key: string }
  identity(): PersonalIdentity
  current(identity: PersonalIdentity): boolean
  busy(): boolean
  run<T>(
    identity: PersonalIdentity,
    kind: KnowledgeFileAction,
    work: (signal: AbortSignal, current: () => boolean) => Promise<T>,
  ): Promise<T>
  cancelRead(): void
}
type Original = {
  identity: PersonalIdentity
  csrf: string
  key: string
  project: string
  target: string
  command: 'create' | 'update'
  input: KnowledgeFileIntent | null
  body: string | null
  uncertain: boolean
  keyConflict: boolean
}
export type KnowledgeFileProgress = Readonly<{
  projectID: string
  documentID: string
  command: 'create' | 'update'
  phase: 'submitting' | 'uncertain' | 'confirmed' | 'rejected'
  observation: 'none' | 'in_progress' | 'not_observed' | 'committed' | 'failed'
  receipt: KnowledgeDocument | null
  contextValid: boolean
  canRetryOriginal: boolean
}>

// The host owns the single Session request, cancellation and actual promise
// retirement. This module owns only this command's immutable original intent.
export function createKnowledgeFileSession(
  api: KnowledgeFileAPI,
  ports: KnowledgeFileSessionPorts,
) {
  let original: Original | null = null
  let identity: PersonalIdentity | null = null
  const state = shallowRef<Omit<KnowledgeFileProgress, 'contextValid' | 'canRetryOriginal'> | null>(
    null,
  )
  const live = (value: Original) => original === value && ports.current(value.identity)
  const erase = (value: Original | null) => {
    if (value) {
      value.input = null
      value.body = null
    }
  }
  function publish(
    value: Original,
    phase: KnowledgeFileProgress['phase'],
    observation: KnowledgeFileProgress['observation'] = 'none',
    receipt: KnowledgeDocument | null = null,
  ) {
    identity = value.identity
    const publication = Object.freeze({
      projectID: value.project,
      documentID: value.target,
      command: value.command,
      phase,
      observation,
      receipt,
    })
    state.value = publication
    return publication
  }
  async function perform(
    value: Original,
    lookup: boolean,
  ): Promise<KnowledgeFileResult | KnowledgeFileLookup> {
    if (ports.busy() || !live(value) || !value.input) throw new AccountFailure('busy')
    let dispatched = false
    if (!lookup) publish(value, 'submitting')
    try {
      const result = await ports.run(
        value.identity,
        lookup ? 'knowledge-file-lookup' : 'knowledge-file-write',
        async (signal, current) => {
          const input = value.input
          if (!input || !current() || !live(value)) throw new AccountFailure('cancelled')
          if (JSON.stringify(input) !== value.body) throw new AccountFailure('invalid-input')
          dispatched = true
          try {
            const options = { csrfToken: value.csrf, key: value.key, signal }
            return await (lookup
              ? api.lookup(value.project, input, options)
              : api.write(value.project, input, options))
          } finally {
            if (original !== value) erase(value)
          }
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      let publication: NonNullable<typeof state.value>
      if ('state' in result && result.state !== 'committed') {
        value.uncertain = true
        publication = publish(value, 'uncertain', result.state)
      } else {
        const receipt = 'state' in result ? result.receipt.document : result.document
        original = null
        erase(value)
        publication = publish(value, 'confirmed', lookup ? 'committed' : 'none', receipt)
      }
      if (state.value !== publication || !ports.current(value.identity))
        throw new AccountFailure('cancelled')
      return result
    } catch (error) {
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (original === value) {
        const p = failure.problem
        value.keyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
        const known =
          !dispatched ||
          (!!p &&
            ['not_started', 'not_committed'].includes(p.commit_state) &&
            [
              'INVALID_ARGUMENT',
              'VERSION_CONFLICT',
              'INVALID_STATE',
              'PROJECT_NOT_ACTIVE',
              'NOT_FOUND',
              'FORBIDDEN',
              'RESOURCE_DELETED',
              'PAYLOAD_TOO_LARGE',
              'UNSUPPORTED_MEDIA_TYPE',
            ].includes(p.code))
        value.uncertain ||= lookup || !known || value.keyConflict
        publish(value, value.uncertain ? 'uncertain' : 'rejected', lookup ? 'failed' : 'none')
        if (!value.uncertain) {
          original = null
          erase(value)
        }
      }
      throw failure
    }
  }
  return {
    get pending() {
      return original !== null
    },
    get progress(): KnowledgeFileProgress | null {
      const current = state.value
      if (!current) return null
      const contextValid = !!identity && ports.current(identity)
      return Object.freeze({
        ...current,
        contextValid,
        canRetryOriginal:
          !!original?.input &&
          original.uncertain &&
          !original.keyConflict &&
          current.observation === 'not_observed' &&
          contextValid &&
          !ports.busy(),
      })
    },
    start(projectID: string, value: KnowledgeFileIntent) {
      if (original || ports.busy()) return Promise.reject(new AccountFailure('busy'))
      try {
        const input = captureKnowledgeFile(value),
          captured = ports.capture()
        const next: Original = {
          ...captured,
          project: captureKnowledgeID(projectID),
          target: input.document_id,
          command: input.command,
          input,
          body: JSON.stringify(input),
          uncertain: false,
          keyConflict: false,
        }
        original = next
        return perform(next, false)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    lookup() {
      if (!original || state.value?.phase !== 'uncertain')
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, true)
    },
    replay() {
      if (
        !original ||
        !original.uncertain ||
        original.keyConflict ||
        state.value?.observation !== 'not_observed'
      )
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, false)
    },
    source(projectID: string, target: string, version: string) {
      const project = captureKnowledgeID(projectID),
        document = captureKnowledgeID(target)
      const currentIdentity = ports.identity()
      return ports.run(currentIdentity, 'knowledge-file-read', (signal, current) => {
        if (!current()) throw new AccountFailure('cancelled')
        return api.source(project, document, version, signal)
      })
    },
    abandonRead: ports.cancelRead,
    abandon() {
      if (ports.busy()) return false
      erase(original)
      original = null
      identity = null
      state.value = null
      return true
    },
    clear() {
      erase(original)
      original = null
      identity = null
      state.value = null
    },
  }
}
