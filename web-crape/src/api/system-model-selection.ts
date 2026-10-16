import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  createMeetingSummaryAPI,
  type MeetingSummaryCommand,
  type MeetingSummaryState,
} from './system-meeting-summary'
import {
  createSystemProviderAPI,
  parseProviderModel,
  type ConfigurationPage,
  type Provider,
  type ProviderModel,
  type ProviderModelQuery,
  type ProviderQuery,
} from './system-providers'

export type SelectionPurpose = 'embedding' | 'memory' | 'reranker' | 'image'
export type SelectionConfigured = Readonly<{
  embedding: string
  memory: string
  reranker: string | null
  image: string | null
}>
export type SelectionState = Readonly<{
  id: string
  version: string
  configured: SelectionConfigured | null
}>
export type SelectionCommand = SelectionConfigured &
  Readonly<{ kind: 'model.selection.update'; id: string; expected_version: string }>
export type SelectionReceipt = Readonly<{
  kind: 'model.selection.update'
  resource_id: string
  version: string
  affected_references: '0'
}>
export type SelectionObservation =
  Readonly<{ found: false; receipt: null }> | Readonly<{ found: true; receipt: SelectionReceipt }>
export type SavedModel = Readonly<{ model: ProviderModel; provider: Provider }>
export interface SystemModelSelectionAPI {
  getMeetingSummary(signal: AbortSignal): Promise<MeetingSummaryState>
  updateMeetingSummary(
    command: MeetingSummaryCommand,
    options: WriteOptions,
  ): Promise<SelectionReceipt>
  lookupMeetingSummaryCommand(
    command: MeetingSummaryCommand,
    options: WriteOptions,
  ): Promise<SelectionObservation>
  getSelection(signal: AbortSignal): Promise<SelectionState>
  updateSelection(command: SelectionCommand, options: WriteOptions): Promise<SelectionReceipt>
  lookupSelectionCommand(
    command: SelectionCommand,
    options: WriteOptions,
  ): Promise<SelectionObservation>
  listProviders(query: ProviderQuery, signal: AbortSignal): Promise<ConfigurationPage<Provider>>
  getProvider(id: string, signal: AbortSignal): Promise<Provider>
  listModels(
    query: ProviderModelQuery,
    signal: AbortSignal,
  ): Promise<ConfigurationPage<ProviderModel>>
  getSavedModel(modelID: string, signal: AbortSignal): Promise<SavedModel>
}

const maximumVersion = 9223372036854775807n
const purposes = ['embedding', 'memory', 'reranker', 'image'] as const
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
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
function configured(value: unknown): SelectionConfigured {
  const v = shape(value, purposes)
  return Object.freeze({
    embedding: id(v.embedding),
    memory: id(v.memory),
    reranker: v.reranker === null ? null : id(v.reranker),
    image: v.image === null ? null : id(v.image),
  })
}
function selection(value: unknown): SelectionState {
  const v = shape(value, ['id', 'version', 'configured'])
  return Object.freeze({
    id: id(v.id),
    version: version(v.version),
    configured: v.configured === null ? null : configured(v.configured),
  })
}
export function selectionCommandBody(command: SelectionCommand) {
  return Object.freeze({
    id: command.id,
    expected_version: command.expected_version,
    embedding: command.embedding,
    memory: command.memory,
    reranker: command.reranker,
    image: command.image,
  })
}
export function captureSelectionCommand(value: SelectionCommand): SelectionCommand {
  return input(() => {
    const v = shape(value, ['kind', 'id', 'expected_version', ...purposes])
    requireValue(v.kind === 'model.selection.update')
    const result: SelectionCommand = Object.freeze({
      kind: 'model.selection.update',
      id: id(v.id),
      expected_version: version(v.expected_version, maximumVersion - 1n),
      ...configured(Object.fromEntries(purposes.map((purpose) => [purpose, v[purpose]]))),
    })
    requireValue(
      new TextEncoder().encode(JSON.stringify(selectionCommandBody(result))).byteLength <=
        16 * 1024,
    )
    return result
  })
}
function writeOptions(value: WriteOptions) {
  return input(() => {
    const v = shape(value, ['csrfToken', 'key', 'signal'])
    return { csrf: v.csrfToken as string, key: v.key as string, signal: v.signal as AbortSignal }
  })
}
function receipt(value: unknown, command: SelectionCommand): SelectionReceipt {
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
function live(signal: AbortSignal) {
  if (signal.aborted) throw new AccountFailure('cancelled')
}
export function createSystemModelSelectionAPI(fetcher?: Fetch): SystemModelSelectionAPI {
  const request = accountTransport(fetcher)
  const providers = createSystemProviderAPI(fetcher)
  return {
    ...createMeetingSummaryAPI(fetcher),
    getSelection: (signal) => request('getModelSelection', selection, { signal }),
    async updateSelection(value, write) {
      const command = captureSelectionCommand(value)
      return request('updateModelSelection', (v) => receipt(v, command), {
        ...writeOptions(write),
        body: selectionCommandBody(command),
      })
    },
    async lookupSelectionCommand(value, write) {
      const command = captureSelectionCommand(value)
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
    listProviders: providers.listProviders,
    getProvider: providers.getProvider,
    listModels: providers.listModels,
    async getSavedModel(value, signal) {
      const target = input(() => id(value))
      let raw: Record<string, unknown> | undefined
      try {
        live(signal)
        const providerID = await request(
          'getModel',
          (value) => {
            const v = shape(value, [
              'id',
              'provider_id',
              'input',
              'version',
              'created_at',
              'updated_at',
            ])
            requireValue(id(v.id) === target)
            const providerID = id(v.provider_id)
            // Only these two IDs are trusted here. The raw DTO never leaves this call.
            raw = v
            return providerID
          },
          { target, signal },
        )
        live(signal)
        const provider = await providers.getProvider(providerID, signal)
        live(signal)
        const model = parseProviderModel(raw, provider.id, provider.input.protocol)
        requireValue(model.id === target && model.provider_id === provider.id)
        live(signal)
        return Object.freeze({ model, provider })
      } finally {
        raw = undefined
      }
    },
  }
}
