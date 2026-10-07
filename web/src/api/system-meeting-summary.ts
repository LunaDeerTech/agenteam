import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import type { SelectionReceipt, SelectionObservation } from './system-model-selection'

export type MeetingSummaryState = Readonly<{ id: string; version: string; model: string | null }>
export type MeetingSummaryCommand = Readonly<{
  kind: 'model.selection.update'
  id: string
  expected_version: string
  model: string
}>
const maximumVersion = 9223372036854775807n
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function version(value: unknown, maximum = maximumVersion): string {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= maximum)
  return result
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function meetingSummaryCommandBody(command: MeetingSummaryCommand) {
  return Object.freeze({
    id: command.id,
    expected_version: command.expected_version,
    model: command.model,
  })
}
export function captureMeetingSummaryCommand(value: MeetingSummaryCommand): MeetingSummaryCommand {
  return input(() => {
    const v = shape(value, ['kind', 'id', 'expected_version', 'model'])
    requireValue(v.kind === 'model.selection.update')
    const result: MeetingSummaryCommand = Object.freeze({
      kind: 'model.selection.update',
      id: id(v.id),
      expected_version: version(v.expected_version, maximumVersion - 1n),
      model: id(v.model),
    })
    requireValue(
      new TextEncoder().encode(JSON.stringify(meetingSummaryCommandBody(result))).byteLength <=
        16 * 1024,
    )
    return result
  })
}
function selection(value: unknown): MeetingSummaryState {
  const v = shape(value, ['id', 'version', 'model'])
  return Object.freeze({
    id: id(v.id),
    version: version(v.version),
    model: v.model === null ? null : id(v.model),
  })
}
function receipt(value: unknown, command: MeetingSummaryCommand): SelectionReceipt {
  const v = shape(value, ['kind', 'resource_id', 'version', 'affected_references'])
  requireValue(v.kind === command.kind && v.affected_references === '0')
  const resource_id = id(v.resource_id),
    observedVersion = version(v.version)
  requireValue(
    resource_id === command.id && BigInt(observedVersion) === BigInt(command.expected_version) + 1n,
  )
  return Object.freeze({
    kind: command.kind,
    resource_id,
    version: observedVersion,
    affected_references: '0',
  })
}
function writeOptions(value: WriteOptions) {
  return input(() => {
    const v = shape(value, ['csrfToken', 'key', 'signal'])
    return { csrf: v.csrfToken as string, key: v.key as string, signal: v.signal as AbortSignal }
  })
}
export function createMeetingSummaryAPI(fetcher?: Fetch) {
  const request = accountTransport(fetcher)
  return {
    getMeetingSummary: (signal: AbortSignal): Promise<MeetingSummaryState> =>
      request('getMeetingSummary', selection, { signal }),
    updateMeetingSummary(
      value: MeetingSummaryCommand,
      write: WriteOptions,
    ): Promise<SelectionReceipt> {
      const command = captureMeetingSummaryCommand(value)
      return request('updateMeetingSummary', (v) => receipt(v, command), {
        ...writeOptions(write),
        body: meetingSummaryCommandBody(command),
      })
    },
    lookupMeetingSummaryCommand(
      value: MeetingSummaryCommand,
      write: WriteOptions,
    ): Promise<SelectionObservation> {
      const command = captureMeetingSummaryCommand(value)
      return request(
        'lookupModelSelectionCommand',
        (value) => {
          const v = shape(value, ['found', 'receipt'])
          requireValue(typeof v.found === 'boolean')
          if (v.found) return Object.freeze({ found: true, receipt: receipt(v.receipt, command) })
          requireValue(v.receipt === null)
          return Object.freeze({ found: false, receipt: null })
        },
        { ...writeOptions(write), body: { command: command.kind } },
      )
    },
  }
}
