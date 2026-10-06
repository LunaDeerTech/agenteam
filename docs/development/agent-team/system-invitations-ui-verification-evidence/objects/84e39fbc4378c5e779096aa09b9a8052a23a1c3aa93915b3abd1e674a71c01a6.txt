import { parseAccountUser, type User } from './account'
import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'

export type SystemUser = Readonly<User & { created_at: string }>
export type SystemUserPage = Readonly<{
  items: readonly SystemUser[]
  next_cursor?: string
}>
export type SystemUserQuery = Readonly<{ cursor?: string }>
export interface SystemAccountAPI {
  listUsers(query: SystemUserQuery, signal: AbortSignal): Promise<SystemUserPage>
}

function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}
function createdAt(value: unknown): string {
  const result = string(value, 27, 27)
  requireValue(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(result))
  const year = Number(result.slice(0, 4)),
    month = Number(result.slice(5, 7)),
    day = Number(result.slice(8, 10))
  // foundation.Instant includes year 0000. Check the Gregorian calendar without
  // Date's 0–99 year shortcut, normalization or loss of the last three digits.
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  requireValue(
    month >= 1 &&
      month <= 12 &&
      day >= 1 &&
      day <= days[month - 1]! &&
      Number(result.slice(11, 13)) <= 23 &&
      Number(result.slice(14, 16)) <= 59 &&
      Number(result.slice(17, 19)) <= 59,
  )
  return result
}
export { createdAt as parseSystemInstant }
function parsePage(value: unknown): SystemUserPage {
  const page = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(page.items) && page.items.length <= 25)
  const ids = new Set<string>()
  let previous: SystemUser | undefined
  const items = (page.items as unknown[]).map((value) => {
    const u = shape(value, [
      'id',
      'email',
      'username',
      'display_name',
      'role',
      'theme',
      'version',
      'initial_password_suggestion',
      'created_at',
    ])
    const { created_at, ...account } = u
    const result = Object.freeze({
      ...parseAccountUser(account),
      created_at: createdAt(created_at),
    })
    requireValue(!ids.has(result.id))
    requireValue(
      !previous ||
        result.created_at < previous.created_at ||
        (result.created_at === previous.created_at && result.id < previous.id),
    )
    ids.add(result.id)
    previous = result
    return result
  })
  const cursor = Object.hasOwn(page, 'next_cursor') ? string(page.next_cursor, 1, 8192) : undefined
  requireValue(cursor === undefined || items.length === 25)
  return Object.freeze({
    items: Object.freeze(items),
    ...(cursor === undefined ? {} : { next_cursor: cursor }),
  })
}

export function createSystemAccountAPI(fetcher?: Fetch): SystemAccountAPI {
  const request = accountTransport(fetcher)
  return {
    listUsers(query, signal) {
      return request('systemUsers', parsePage, { signal, users: query })
    },
  }
}
