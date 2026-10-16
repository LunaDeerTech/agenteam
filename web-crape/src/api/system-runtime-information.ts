import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type RuntimeInformationStatus =
  | Readonly<{ status: 'available'; safe_reason: null }>
  | Readonly<{ status: 'unavailable'; safe_reason: 'check_unavailable' }>
  | Readonly<{ status: 'stale'; safe_reason: 'sample_stale' }>
export type SystemRuntimeInformation = Readonly<{
  observed_at: string
  central: Readonly<{ version: null; safe_reason: 'build_version_not_recorded' }>
  database: RuntimeInformationStatus &
    Readonly<{
      last_success: Readonly<{
        checked_at: string
        received_at: string
        postgresql_version: string
        pgvector_version: string
      }>
    }>
  object_storage: RuntimeInformationStatus &
    Readonly<{
      backend: 'minio'
      assessment: 'object_storage_aggregate'
      last_success_received_at: string
      details: 'not_reported'
    }>
  readiness: Readonly<{
    ready: false
    safe_reason: 'DEPENDENCY_UNAVAILABLE' | 'DEPENDENCY_UNBOUND'
  }>
}>
export interface SystemRuntimeInformationAPI {
  get(signal: AbortSignal): Promise<SystemRuntimeInformation>
}

function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function instant(value: unknown): string {
  const parsed = parseSystemInstant(value)
  requireValue(parsed !== '0001-01-01T00:00:00.000000Z')
  return parsed
}
function version(value: unknown): string {
  requireValue(typeof value === 'string' && /^[\x20-\x7e]{1,256}$/.test(value))
  return value as string
}
function status(value: unknown, reason: unknown): RuntimeInformationStatus {
  if (value === 'available' && reason === null) return { status: value, safe_reason: reason }
  if (value === 'unavailable' && reason === 'check_unavailable')
    return { status: value, safe_reason: reason }
  if (value === 'stale' && reason === 'sample_stale') return { status: value, safe_reason: reason }
  throw new AccountFailure('invalid-response')
}

export function parseSystemRuntimeInformation(value: unknown): SystemRuntimeInformation {
  const raw = shape(value, ['observed_at', 'central', 'database', 'object_storage', 'readiness'])
  const observed_at = instant(raw.observed_at)
  const central = shape(raw.central, ['version', 'safe_reason'])
  requireValue(central.version === null && central.safe_reason === 'build_version_not_recorded')
  const database = shape(raw.database, ['status', 'safe_reason', 'last_success'])
  const databaseStatus = status(database.status, database.safe_reason)
  const last = shape(database.last_success, [
    'checked_at',
    'received_at',
    'postgresql_version',
    'pgvector_version',
  ])
  const last_success = Object.freeze({
    checked_at: instant(last.checked_at),
    received_at: instant(last.received_at),
    postgresql_version: version(last.postgresql_version),
    pgvector_version: version(last.pgvector_version),
  })
  const storage = shape(raw.object_storage, [
    'backend',
    'assessment',
    'status',
    'safe_reason',
    'last_success_received_at',
    'details',
  ])
  requireValue(
    storage.backend === 'minio' &&
      storage.assessment === 'object_storage_aggregate' &&
      storage.details === 'not_reported',
  )
  const storageStatus = status(storage.status, storage.safe_reason)
  const last_success_received_at = instant(storage.last_success_received_at)
  const readiness = shape(raw.readiness, ['ready', 'safe_reason'])
  requireValue(
    readiness.ready === false &&
      (readiness.safe_reason === 'DEPENDENCY_UNAVAILABLE' ||
        (readiness.safe_reason === 'DEPENDENCY_UNBOUND' &&
          databaseStatus.status === 'available' &&
          storageStatus.status === 'available')),
  )
  return Object.freeze({
    observed_at,
    central: Object.freeze({ version: null, safe_reason: 'build_version_not_recorded' }),
    database: Object.freeze({ ...databaseStatus, last_success }),
    object_storage: Object.freeze({
      ...storageStatus,
      backend: 'minio',
      assessment: 'object_storage_aggregate',
      last_success_received_at,
      details: 'not_reported',
    }),
    readiness: Object.freeze({
      ready: false,
      safe_reason: readiness.safe_reason as SystemRuntimeInformation['readiness']['safe_reason'],
    }),
  })
}

export function createSystemRuntimeInformationAPI(fetcher?: Fetch): SystemRuntimeInformationAPI {
  const request = accountTransport(fetcher)
  return {
    get(signal) {
      return request('getSystemRuntimeInformation', parseSystemRuntimeInformation, { signal })
    },
  }
}
