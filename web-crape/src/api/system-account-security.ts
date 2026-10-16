import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'

export type AccountSecurityValues = Readonly<{
  session_idle_seconds: string
  session_absolute_seconds: string
  password_reset_seconds: string
  challenge_after_failures: string
}>
export type AccountSecuritySettings = AccountSecurityValues &
  Readonly<{
    id: string
    version: string
    lifetime_changes_apply_to: 'newly_issued_sessions_and_tokens'
  }>
export type AccountSecurityUpdateInput = AccountSecurityValues & Readonly<{ version: string }>
export interface SystemAccountSecurityAPI {
  getSettings(signal: AbortSignal): Promise<AccountSecuritySettings>
  updateSettings(
    input: AccountSecurityUpdateInput,
    options: WriteOptions,
  ): Promise<AccountSecuritySettings>
}

const valueKeys = [
  'session_idle_seconds',
  'session_absolute_seconds',
  'password_reset_seconds',
  'challenge_after_failures',
] as const
const maximumVersion = 9223372036854775807n
function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function decimal(value: unknown, minimum: bigint, maximum: bigint): string {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result))
  requireValue(BigInt(result) >= minimum && BigInt(result) <= maximum)
  return result
}
function values(value: Record<string, unknown>): AccountSecurityValues {
  const result = {
    session_idle_seconds: decimal(value.session_idle_seconds, 900n, 2592000n),
    session_absolute_seconds: decimal(value.session_absolute_seconds, 3600n, 7776000n),
    password_reset_seconds: decimal(value.password_reset_seconds, 300n, 7200n),
    challenge_after_failures: decimal(value.challenge_after_failures, 1n, 20n),
  }
  requireValue(BigInt(result.session_idle_seconds) <= BigInt(result.session_absolute_seconds))
  return Object.freeze(result)
}
function settings(value: unknown): AccountSecuritySettings {
  const v = shape(value, ['id', 'version', ...valueKeys, 'lifetime_changes_apply_to'])
  const id = string(v.id, 36, 36)
  requireValue(uuid7.test(id) && v.lifetime_changes_apply_to === 'newly_issued_sessions_and_tokens')
  return Object.freeze({
    id,
    version: decimal(v.version, 1n, maximumVersion),
    ...values(v),
    lifetime_changes_apply_to: 'newly_issued_sessions_and_tokens',
  })
}
function input<T>(work: () => T): T {
  try {
    return work()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureAccountSecurityUpdate(value: AccountSecurityUpdateInput) {
  return input(() => {
    const v = shape(value, ['version', ...valueKeys])
    const result: AccountSecurityUpdateInput = Object.freeze({
      version: decimal(v.version, 1n, maximumVersion - 1n),
      ...values(v),
    })
    requireValue(new TextEncoder().encode(JSON.stringify(result)).byteLength <= 16 * 1024)
    return result
  })
}
export function createSystemAccountSecurityAPI(fetcher?: Fetch): SystemAccountSecurityAPI {
  const request = accountTransport(fetcher)
  return {
    getSettings: (signal) => request('getAccountSecurity', settings, { signal }),
    async updateSettings(value, write) {
      const captured = captureAccountSecurityUpdate(value)
      const options = input(() => {
        const v = shape(write, ['csrfToken', 'key', 'signal'])
        return {
          csrf: v.csrfToken as string,
          key: v.key as string,
          signal: v.signal as AbortSignal,
        }
      })
      return request(
        'updateAccountSecurity',
        (value) => {
          const result = settings(value)
          requireValue(BigInt(result.version) === BigInt(captured.version) + 1n)
          requireValue(valueKeys.every((key) => result[key] === captured[key]))
          // Only the Session owner knows the singleton captured by its first GET.
          return result
        },
        { ...options, body: captured },
      )
    },
  }
}
