import { validAccountLinkToken } from '../api/account'
import { safeReturnTarget } from './auth'

export type AccountEntryMode = 'invitation' | 'forgot' | 'reset'
export type CapturedAccountLink =
  | { mode: 'invitation' | 'reset'; kind: 'valid'; token: string }
  | { mode: 'invitation' | 'reset'; kind: 'missing' | 'invalid' | 'unsafe' }

export function accountEntryMode(path: string): AccountEntryMode | null {
  return path === '/invite'
    ? 'invitation'
    : path === '/forgot-password'
      ? 'forgot'
      : path === '/reset-password'
        ? 'reset'
        : null
}

function safeHistoryState(
  value: unknown,
  fragment: string,
  seen = new WeakSet<object>(),
  depth = 0,
): unknown {
  if (typeof value === 'string')
    return (fragment && value.includes(fragment)) ||
      /[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}/.test(
        value,
      )
      ? undefined
      : value
  if (value === null || typeof value !== 'object') return value
  if (depth > 32 || seen.has(value)) return undefined
  seen.add(value)
  if (Array.isArray(value))
    return value.map((item) => safeHistoryState(item, fragment, seen, depth + 1))
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, item]) => {
      if (safeHistoryState(key, fragment, seen, depth + 1) === undefined) return []
      const cleaned = safeHistoryState(item, fragment, seen, depth + 1)
      return cleaned === undefined ? [] : [[key, cleaned]]
    }),
  )
}

// One capture slot is the pending replacement. The active capability belongs only
// to the page owner, never to a Route, a reactive store or history.state.
export function createAccountLinkCapture(target: Window) {
  let pending: CapturedAccountLink | null = null
  let listener: (() => void) | null = null
  let disposed = false
  function capture() {
    if (disposed) return
    const mode = accountEntryMode(target.location.pathname)
    if (!mode || mode === 'forgot') return
    const hash = target.location.hash
    const raw = hash.slice(1)
    let kind: CapturedAccountLink['kind'] = validAccountLinkToken(raw) ? 'valid' : 'invalid'
    const url = new URL(target.location.href)
    const returns = url.searchParams.getAll('return')
    const next = returns.length === 1 ? safeReturnTarget(returns[0]) : '/'
    const clean = url.pathname + (next === '/' ? '' : `?return=${encodeURIComponent(next)}`)
    try {
      // Vue Router stores path strings under back/current/forward. Preserve its
      // scroll/key fields, but never preserve a capability in one of these paths.
      const previous = target.history.state
      const state = previous && typeof previous === 'object' ? { ...previous } : previous
      if (state && typeof state === 'object') {
        for (const key of ['back', 'current', 'forward']) {
          if (typeof state[key] === 'string') {
            const value = new URL(state[key], target.location.origin)
            if (accountEntryMode(value.pathname)) {
              value.hash = ''
              const values = value.searchParams.getAll('return')
              const returned = values.length === 1 ? safeReturnTarget(values[0]) : '/'
              state[key] =
                value.pathname + (returned === '/' ? '' : `?return=${encodeURIComponent(returned)}`)
            }
          }
        }
      }
      target.history.replaceState(safeHistoryState(state, raw), '', clean)
      if (target.location.hash) kind = 'unsafe'
    } catch {
      kind = 'unsafe'
    }
    if (hash || kind === 'unsafe') {
      pending = kind === 'valid' ? { mode, kind, token: raw } : { mode, kind }
      listener?.()
    }
  }
  // Registration and the first scrub both precede createWebHistory.
  target.addEventListener('hashchange', capture)
  target.addEventListener('popstate', capture)
  capture()
  return {
    take(mode: 'invitation' | 'reset'): CapturedAccountLink {
      const value = pending
      pending = null
      return value?.mode === mode ? value : { mode, kind: 'missing' }
    },
    get pendingMode() {
      return pending?.mode ?? null
    },
    get disposed() {
      return disposed
    },
    subscribe(next: () => void) {
      listener = next
      return () => {
        if (listener === next) listener = null
      }
    },
    discard() {
      pending = null
    },
    dispose() {
      disposed = true
      pending = null
      listener = null
      target.removeEventListener('hashchange', capture)
      target.removeEventListener('popstate', capture)
    },
  }
}
export type AccountLinkCapture = ReturnType<typeof createAccountLinkCapture>
let capture: AccountLinkCapture | undefined
export function installAccountLinkCapture() {
  if (!capture || capture.disposed) capture = createAccountLinkCapture(window)
  return capture
}
