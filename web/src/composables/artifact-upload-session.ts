import { shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import { captureArtifactID, type ArtifactMetadata } from '../api/project-artifacts'
import {
  captureArtifactUploadFile,
  type ArtifactUploadAPI,
  type ArtifactUploadIntent,
  type ArtifactUploadResult,
  type ArtifactUploadCancellation,
} from '../api/artifact-upload'
import type { PersonalIdentity } from './useSession'

export type ArtifactUploadAction =
  'artifact-upload-write' | 'artifact-upload-lookup' | 'artifact-upload-payload'
export interface ArtifactUploadSessionPorts {
  capture(): { identity: PersonalIdentity; csrf: string; key: string }
  current(identity: PersonalIdentity): boolean
  busy(): boolean
  stopPayload(): void
  run<T>(
    identity: PersonalIdentity,
    action: ArtifactUploadAction,
    work: (signal: AbortSignal, current: () => boolean) => Promise<T>,
  ): Promise<T>
}
type Original = {
  identity: PersonalIdentity
  csrf: string
  key: string
  project: string
  meeting: string | null
  input: ArtifactUploadIntent
  body: string
  file: File | null
  payloadClaimed: boolean
  keyConflict: boolean
}
type Stage = 'begin' | 'payload' | 'complete' | 'lookup' | 'cancel'
type Result =
  ArtifactUploadResult | ArtifactUploadCancellation | Readonly<{ artifact: ArtifactMetadata }>
export type ArtifactUploadProgress = Readonly<{
  projectID: string
  meetingID: string | null
  name: string
  byteSize: string
  phase:
    | 'beginning'
    | 'uploading'
    | 'publishing'
    | 'uncertain'
    | 'uploaded'
    | 'cancelling'
    | 'confirmed'
    | 'cancelled'
    | 'rejected'
  observation: ArtifactUploadResult['state'] | 'none' | 'failed'
  receipt: ArtifactMetadata | null
  contextValid: boolean
  canLookup: boolean
  canComplete: boolean
  canCancel: boolean
}>
// One user action owns one immutable original key and File. Only the successful
// fresh begin call can advance to its one payload call; lookup never grants it.
export function createArtifactUploadSession(
  api: ArtifactUploadAPI,
  ports: ArtifactUploadSessionPorts,
) {
  let original: Original | null = null,
    identity: PersonalIdentity | null = null
  const state = shallowRef<Omit<
    ArtifactUploadProgress,
    'contextValid' | 'canLookup' | 'canComplete' | 'canCancel'
  > | null>(null)
  const live = (value: Original) => original === value && ports.current(value.identity)
  function publish(
    value: Original,
    phase: ArtifactUploadProgress['phase'],
    observation: ArtifactUploadProgress['observation'] = 'none',
    receipt: ArtifactMetadata | null = null,
  ) {
    identity = value.identity
    state.value = Object.freeze({
      projectID: value.project,
      meetingID: value.meeting,
      name: value.input.name,
      byteSize: value.input.byte_size,
      phase,
      observation,
      receipt,
    })
  }
  function adopt(value: Original, result: Result, stage: Stage) {
    if (!('state' in result) || result.state === 'committed') {
      publish(value, 'confirmed', 'committed', result.artifact)
      value.file = null
      original = null
    } else if (result.state === 'cancelled') {
      publish(value, 'cancelled', 'cancelled')
      value.file = null
      original = null
    } else if (result.state === 'uploaded') publish(value, 'uploaded', 'uploaded')
    else if (result.state === 'cancel_requested') publish(value, 'cancelling', 'cancel_requested')
    else
      publish(
        value,
        stage === 'begin' && result.state === 'pending' ? 'beginning' : 'uncertain',
        result.state,
      )
    // Cancel not_observed is just an observation. It has no durable tombstone
    // or proof that an earlier uncertain begin has retired.
  }
  async function perform(value: Original, stage: Stage): Promise<Result> {
    if (ports.busy() || !live(value)) throw new AccountFailure('busy')
    if (stage === 'payload' && (value.payloadClaimed || !value.file))
      throw new AccountFailure('invalid-input')
    if (stage === 'payload') value.payloadClaimed = true
    if (stage !== 'lookup')
      publish(
        value,
        { begin: 'beginning', payload: 'uploading', complete: 'publishing', cancel: 'cancelling' }[
          stage
        ] as ArtifactUploadProgress['phase'],
      )
    let dispatched = false
    try {
      const result = await ports.run(
        value.identity,
        stage === 'lookup'
          ? 'artifact-upload-lookup'
          : stage === 'payload'
            ? 'artifact-upload-payload'
            : 'artifact-upload-write',
        async (signal, current) => {
          if (!current() || !live(value)) throw new AccountFailure('cancelled')
          if (JSON.stringify(value.input) !== value.body) throw new AccountFailure('invalid-input')
          const options = { csrfToken: value.csrf, key: value.key, signal },
            user = value.identity.userID
          dispatched = true
          if (stage === 'payload') {
            if (!value.file) throw new AccountFailure('cancelled')
            try {
              return await api.payload(value.project, user, value.input, value.file, options)
            } finally {
              value.file = null
            }
          }
          return api[stage](value.project, user, value.input, options)
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      adopt(value, result, stage)
      if (!ports.current(value.identity)) throw new AccountFailure('cancelled')
      return result
    } catch (error) {
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      value.file = null
      if (live(value)) {
        value.keyConflict ||= failure.problem?.code === 'IDEMPOTENCY_KEY_REUSED'
        const rejected =
          stage === 'begin' &&
          (!dispatched ||
            (!!failure.problem &&
              ['not_started', 'not_committed'].includes(failure.problem.commit_state) &&
              [
                'INVALID_ARGUMENT',
                'PROJECT_NOT_ACTIVE',
                'NOT_FOUND',
                'FORBIDDEN',
                'RESOURCE_DELETED',
                'PAYLOAD_TOO_LARGE',
                'UNSUPPORTED_MEDIA_TYPE',
              ].includes(failure.problem.code) &&
              !value.keyConflict))
        publish(value, rejected ? 'rejected' : 'uncertain', stage === 'lookup' ? 'failed' : 'none')
        if (rejected) original = null
      }
      throw failure
    }
  }
  return {
    get pending() {
      return original !== null
    },
    get progress(): ArtifactUploadProgress | null {
      const current = state.value
      if (!current) return null
      const contextValid = !!identity && ports.current(identity),
        available = !!original && contextValid && !ports.busy()
      return Object.freeze({
        ...current,
        contextValid,
        canLookup: available && ['uncertain', 'uploaded', 'cancelling'].includes(current.phase),
        canComplete:
          available &&
          current.phase === 'uploaded' &&
          current.observation === 'uploaded' &&
          !original!.keyConflict,
        canCancel:
          available &&
          ['uncertain', 'uploaded', 'cancelling'].includes(current.phase) &&
          !original!.keyConflict,
      })
    },
    async start(
      projectID: string,
      meetingID: string | null,
      file: File,
    ): Promise<ArtifactMetadata | null> {
      if (original || ports.busy()) throw new AccountFailure('busy')
      const project = captureArtifactID(projectID),
        meeting = meetingID === null ? null : captureArtifactID(meetingID),
        input = captureArtifactUploadFile(file),
        captured = ports.capture()
      const value: Original = {
        ...captured,
        project,
        meeting,
        input,
        body: JSON.stringify(input),
        file,
        payloadClaimed: false,
        keyConflict: false,
      }
      original = value
      try {
        let result = await perform(value, 'begin')
        if ('state' in result && result.state === 'pending' && live(value))
          result = await perform(value, 'payload')
        if ('state' in result && result.state === 'uploaded' && live(value))
          result = await perform(value, 'complete')
        return 'artifact' in result ? result.artifact : null
      } catch (error) {
        if (live(value) && state.value?.phase === 'beginning') publish(value, 'uncertain')
        throw error
      } finally {
        value.file = null
      }
    },
    lookup() {
      if (!original || !this.progress?.canLookup)
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, 'lookup')
    },
    complete() {
      if (!original || !this.progress?.canComplete)
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, 'complete')
    },
    cancel() {
      if (!original || !this.progress?.canCancel)
        return Promise.reject(new AccountFailure('invalid-input'))
      return perform(original, 'cancel')
    },
    stop() {
      if (original && live(original) && state.value?.phase === 'uploading') ports.stopPayload()
    },
    dismiss() {
      if (original || ports.busy()) return false
      identity = null
      state.value = null
      return true
    },
    clear() {
      if (original) original.file = null
      original = null
      identity = null
      state.value = null
    },
  }
}
