import { shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureMeetingCommand,
  type MeetingsAPI,
  type MeetingCommand,
  type MeetingLookup,
  type MeetingCommandResult,
  type MeetingDeletionReceipt,
} from '../api/meetings'
import { workID } from '../api/work-review'
import type { PersonalIdentity } from './useSession'

export type MeetingAction = 'meeting-read' | 'meeting-write' | 'meeting-lookup'
type Original = {
  identity: PersonalIdentity
  csrf: string
  key: string
  project: string
  input: MeetingCommand
  body: string
  acceptedDeletion?: MeetingDeletionReceipt
}
export type MeetingProgress = Readonly<{
  project: string
  meeting: string | null
  kind: MeetingCommand['kind']
  phase: 'submitting' | 'uncertain' | 'processing' | 'confirmed' | 'rejected'
  contextValid: boolean
}>
export function createMeetingSession(
  api: MeetingsAPI,
  ports: {
    capture(): { identity: PersonalIdentity; csrf: string; key: string }
    identity(): PersonalIdentity
    current(identity: PersonalIdentity): boolean
    busy(): boolean
    run<T>(
      identity: PersonalIdentity,
      kind: MeetingAction,
      work: (signal: AbortSignal, current: () => boolean) => Promise<T>,
    ): Promise<T>
    cancelRead(): void
  },
) {
  let original: Original | null = null
  const state = shallowRef<Omit<MeetingProgress, 'contextValid'> | null>(null)
  const live = (value: Original) => original === value && ports.current(value.identity)
  function publish(value: Original, phase: MeetingProgress['phase']) {
    state.value = Object.freeze({
      project: value.project,
      meeting: value.input.kind !== 'start' ? value.input.meeting : null,
      kind: value.input.kind,
      phase,
    })
  }
  async function perform(
    value: Original,
    lookup: boolean,
  ): Promise<MeetingCommandResult | MeetingLookup> {
    let dispatched = false
    try {
      if (ports.busy() || !live(value)) throw new AccountFailure('busy')
      const result = await ports.run(
        value.identity,
        lookup ? 'meeting-lookup' : 'meeting-write',
        async (signal, current) => {
          if (!current() || !live(value)) throw new AccountFailure('cancelled')
          if (JSON.stringify(value.input) !== value.body) throw new AccountFailure('invalid-input')
          dispatched = true
          const write = { signal, csrfToken: value.csrf, key: value.key }
          return lookup
            ? api.lookup(value.project, value.input, write)
            : api.command(value.project, value.input, write)
        },
      )
      if (!live(value)) throw new AccountFailure('cancelled')
      if ('status' in result && result.status === 'not_found') {
        publish(value, 'uncertain')
        if (!live(value)) throw new AccountFailure('cancelled')
        return result
      }
      const receipt = 'status' in result ? result.receipt : result
      if (value.input.kind === 'delete') {
        if (!('phase' in receipt)) throw new AccountFailure('invalid-response')
        const accepted = value.acceptedDeletion
        if (
          accepted &&
          (accepted.deletion_id !== receipt.deletion_id ||
            accepted.meeting_id !== receipt.meeting_id ||
            accepted.accepted_version !== receipt.accepted_version ||
            accepted.requested_at !== receipt.requested_at)
        )
          throw new AccountFailure('invalid-response')
        value.acceptedDeletion = receipt
        if (receipt.phase === 'cancelling') {
          publish(value, 'processing')
          if (!live(value)) throw new AccountFailure('cancelled')
          return result
        }
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
    get progress(): MeetingProgress | null {
      return state.value
        ? Object.freeze({
            ...state.value,
            contextValid: !!original && ports.current(original.identity),
          })
        : null
    },
    list(project: string) {
      return ports.run(ports.identity(), 'meeting-read', (signal) => api.list(project, signal))
    },
    detail(project: string, meeting: string) {
      return ports.run(ports.identity(), 'meeting-read', (signal) =>
        api.detail(project, meeting, signal),
      )
    },
    proposal(project: string, meeting: string) {
      return ports.run(ports.identity(), 'meeting-read', (signal) =>
        api.proposal(project, meeting, signal),
      )
    },
    contributionExecution(project: string, meeting: string, contribution: string) {
      return ports.run(ports.identity(), 'meeting-read', (signal) =>
        api.contributionExecution(project, meeting, contribution, signal),
      )
    },
    start(project: string, input: MeetingCommand) {
      if (original || ports.busy()) return Promise.reject(new AccountFailure('busy'))
      const captured = ports.capture(),
        command = captureMeetingCommand(input)
      const value: Original = {
        ...captured,
        project: workID(project),
        input: command,
        body: JSON.stringify(command),
      }
      original = value
      publish(value, 'submitting')
      return perform(value, false)
    },
    lookup() {
      if (!original || !['uncertain', 'processing'].includes(state.value?.phase ?? ''))
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
