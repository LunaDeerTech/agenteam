import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'

export interface User {
  id: string
  email: string
  username: string
  display_name: string
  role: 'admin' | 'user'
  theme: 'system' | 'light' | 'dark'
  version: string
  initial_password_suggestion: boolean
}
export interface Session {
  id: string
  issued_at: string
  absolute_expires_at: string
  idle_expires_at: string
}
export interface SessionView {
  user: User
  session: Session
  csrf_token: string
}
export interface LoginResult {
  user: User
  session: Session
  next_path: '/'
}
export interface Bootstrap {
  csrf_token: string
  challenge_modes: ['rotate']
  delivery_channel: 'smtp' | 'backend_log'
}
export interface LoginInput {
  email: string
  password: string
  challenge_pass?: string
}
export interface Challenge {
  id: string
  mode: 'rotate'
  master: string
  thumb: string
  expires_at: string
}
export interface ChallengeInput {
  mode: 'rotate'
  email: string
  login_key: string
}
export interface VerifyInput {
  email: string
  login_key: string
  challenge_id: string
  proof: { angle: number }
}

function requireValue(condition: boolean) {
  if (!condition) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function instant(value: unknown) {
  const result = string(value, 27, 27)
  requireValue(
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(result) &&
      Number.isFinite(Date.parse(result)),
  )
  return result
}
function csrf(value: unknown) {
  const result = string(value, 43, 43)
  requireValue(/^[A-Za-z0-9_-]{43}$/.test(result))
  return result
}
function pass(value: unknown) {
  const result = string(value, 80, 80)
  requireValue(/^[\x21-\x7e]{80}$/.test(result))
  return result
}
function email(value: unknown) {
  const result = string(value, 3, 254)
  requireValue(/^[\x21-\x7e]+$/.test(result) && /^[^@<>(),;:]+@[^@<>(),;:]+$/.test(result))
  return result
}
function user(value: unknown): User {
  const u = shape(value, [
    'id',
    'email',
    'username',
    'display_name',
    'role',
    'theme',
    'version',
    'initial_password_suggestion',
  ])
  requireValue(u.role === 'admin' || u.role === 'user')
  requireValue(u.theme === 'system' || u.theme === 'light' || u.theme === 'dark')
  const version = string(u.version, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(version) && BigInt(version) <= 9223372036854775807n)
  const displayName = string(u.display_name, 0, 80)
  requireValue(
    !/[\x00-\x1f\x7f]/.test(displayName) && typeof u.initial_password_suggestion === 'boolean',
  )
  return {
    id: id(u.id),
    email: email(u.email),
    username: string(u.username, 0, 32),
    display_name: displayName,
    role: u.role as User['role'],
    theme: u.theme as User['theme'],
    version,
    initial_password_suggestion: u.initial_password_suggestion as boolean,
  }
}
function session(value: unknown): Session {
  const s = shape(value, ['id', 'issued_at', 'absolute_expires_at', 'idle_expires_at'])
  return {
    id: id(s.id),
    issued_at: instant(s.issued_at),
    absolute_expires_at: instant(s.absolute_expires_at),
    idle_expires_at: instant(s.idle_expires_at),
  }
}
function sessionView(value: unknown): SessionView {
  const v = shape(value, ['user', 'session', 'csrf_token'])
  return { user: user(v.user), session: session(v.session), csrf_token: csrf(v.csrf_token) }
}
function loginResult(value: unknown): LoginResult {
  const v = shape(value, ['user', 'session', 'next_path'])
  requireValue(v.next_path === '/')
  return { user: user(v.user), session: session(v.session), next_path: '/' }
}
function challenge(value: unknown): Challenge {
  const v = shape(value, ['id', 'mode', 'master', 'thumb', 'expires_at'])
  requireValue(v.mode === 'rotate')
  const master = string(v.master, 1, 262144),
    thumb = string(v.thumb, 1, 262144)
  requireValue(
    master.length + thumb.length <= 262144 &&
      [master, thumb].every((image) => /^data:image\/png;base64,[A-Za-z0-9+/]+={0,2}$/.test(image)),
  )
  return { id: id(v.id), mode: 'rotate', master, thumb, expires_at: instant(v.expires_at) }
}
function input<T>(validate: () => T): T {
  try {
    return validate()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function key(value: string) {
  requireValue(/^[A-Za-z0-9._:/-]{1,128}$/.test(value))
  return value
}
function loginInput(value: LoginInput): LoginInput {
  const password = string(value.password, 15, 128)
  requireValue(new TextEncoder().encode(password).byteLength <= 512)
  return {
    email: email(value.email),
    password,
    ...(value.challenge_pass === undefined ? {} : { challenge_pass: pass(value.challenge_pass) }),
  }
}

export function createAccountAPI(fetcher?: Fetch) {
  const request = accountTransport(fetcher)
  return {
    bootstrap(signal: AbortSignal): Promise<Bootstrap> {
      return request(
        'bootstrap',
        (value) => {
          const v = shape(value, ['csrf_token', 'challenge_modes', 'delivery_channel'])
          requireValue(
            Array.isArray(v.challenge_modes) &&
              v.challenge_modes.length === 1 &&
              v.challenge_modes[0] === 'rotate',
          )
          requireValue(v.delivery_channel === 'smtp' || v.delivery_channel === 'backend_log')
          return {
            csrf_token: csrf(v.csrf_token),
            challenge_modes: ['rotate'],
            delivery_channel: v.delivery_channel as Bootstrap['delivery_channel'],
          }
        },
        { signal },
      )
    },
    getSession(signal: AbortSignal) {
      return request('session', sessionView, { signal })
    },
    login(value: LoginInput, token: string, commandKey: string, signal: AbortSignal) {
      const options = input(() => ({
        body: loginInput(value),
        csrf: csrf(token),
        key: key(commandKey),
        signal,
      }))
      return request('login', loginResult, options)
    },
    logout(token: string, commandKey: string, signal: AbortSignal) {
      const options = input(() => ({ body: {}, csrf: csrf(token), key: key(commandKey), signal }))
      return request('logout', () => undefined, options)
    },
    createChallenge(value: ChallengeInput, token: string, signal: AbortSignal) {
      const options = input(() => {
        requireValue(value.mode === 'rotate')
        return {
          body: { mode: 'rotate', email: email(value.email), login_key: key(value.login_key) },
          csrf: csrf(token),
          signal,
        }
      })
      return request('challenge', challenge, options)
    },
    verifyChallenge(value: VerifyInput, token: string, signal: AbortSignal) {
      const options = input(() => {
        requireValue(
          Number.isInteger(value.proof.angle) && value.proof.angle >= 0 && value.proof.angle <= 360,
        )
        return {
          body: {
            email: email(value.email),
            login_key: key(value.login_key),
            challenge_id: id(value.challenge_id),
            proof: { angle: value.proof.angle },
          },
          csrf: csrf(token),
          signal,
        }
      })
      return request('verify', (value) => ({ pass: pass(shape(value, ['pass']).pass) }), options)
    },
  }
}
export type AccountAPI = ReturnType<typeof createAccountAPI>
