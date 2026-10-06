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

export type Version = string
export type Progress = string
export type Digest = string
export type Theme = User['theme']
export type AvatarMedia = 'image/jpeg' | 'image/png' | 'image/webp'
export type AvatarMetadata = Readonly<{
  media_type: AvatarMedia
  byte_size: Progress
  sha256: Digest
}>
export type ProfileView = Readonly<{ user: User; avatar: AvatarMetadata | null }>
export type PreferencesView = Readonly<{ version: Version; theme: Theme }>
export type ProfileInput = Readonly<
  { version: Version } & (
    { username: string; display_name?: string } | { username?: never; display_name: string }
  )
>
export type PasswordInput = Readonly<{
  version: Version
  current_password: string
  new_password: string
  confirmation: string
}>
export type WriteOptions = Readonly<{ csrfToken: string; key: string; signal?: AbortSignal }>
export type AvatarDownload = Readonly<{ metadata: AvatarMetadata; blob: Blob }>
export type DeliveryChannel = 'smtp' | 'backend_log'
export type LinkInput = Readonly<{ token: string }>
export type BrowserReadOptions = Readonly<{ csrfToken: string; signal?: AbortSignal }>
export type InvitationInspection = Readonly<{ email: string; expires_at: string }>
export type InvitationRedeemed = Readonly<{ completed: true; login_required: true }>
export type InvitationInput = Readonly<{
  token: string
  username: string
  display_name?: string
  password: string
  confirmation: string
}>
export type ResetAccepted = Readonly<{ accepted: true; delivery_channel: DeliveryChannel }>
export type ResetInspection = Readonly<{ valid: true; expires_at: string }>
export type ResetInput = Readonly<{ token: string; new_password: string; confirmation: string }>

export function validAccountLinkToken(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length === 80 &&
    uuid7.test(value.slice(0, 36)) &&
    /^\.[A-Za-z0-9_-]{43}$/.test(value.slice(36))
  )
}

function linkInput(value: LinkInput): LinkInput {
  const v = shape(value, ['token'])
  requireValue(validAccountLinkToken(v.token))
  return { token: v.token as string }
}
function newPassword(value: unknown, confirmation: unknown) {
  const password = string(value, 15, 128)
  requireValue(password === confirmation && new TextEncoder().encode(password).byteLength <= 512)
  return password
}
function invitationInput(value: InvitationInput): InvitationInput {
  const v = shape(value, ['token', 'username', 'password', 'confirmation'], ['display_name'])
  const profile = profileInput({
    version: '1',
    username: v.username as string,
    ...(v.display_name === undefined ? {} : { display_name: v.display_name as string }),
  })
  const password = newPassword(v.password, v.confirmation)
  return {
    ...linkInput({ token: v.token as string }),
    username: profile.username!,
    ...(profile.display_name === undefined ? {} : { display_name: profile.display_name }),
    password,
    confirmation: password,
  }
}
function browserReadOptions(options: BrowserReadOptions) {
  return {
    csrf: csrf(options.csrfToken),
    signal: options.signal ?? new AbortController().signal,
  }
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

function version(value: unknown, zero = false): string {
  const result = string(value, 1, 19)
  requireValue(
    (zero ? /^(0|[1-9][0-9]*)$/ : /^[1-9][0-9]*$/).test(result) &&
      BigInt(result) <= 9223372036854775807n,
  )
  return result
}
function theme(value: unknown): Theme {
  requireValue(value === 'system' || value === 'light' || value === 'dark')
  return value as Theme
}
function media(value: unknown): AvatarMedia {
  requireValue(value === 'image/jpeg' || value === 'image/png' || value === 'image/webp')
  return value as AvatarMedia
}
function avatarMetadata(value: unknown): AvatarMetadata {
  const v = shape(value, ['media_type', 'byte_size', 'sha256'])
  const digest = string(v.sha256, 71, 71)
  requireValue(/^sha256:[0-9a-f]{64}$/.test(digest))
  return { media_type: media(v.media_type), byte_size: version(v.byte_size, true), sha256: digest }
}
function profile(value: unknown): ProfileView {
  const v = shape(value, ['user', 'avatar'])
  return { user: user(v.user), avatar: v.avatar === null ? null : avatarMetadata(v.avatar) }
}
function preferences(value: unknown): PreferencesView {
  const v = shape(value, ['version', 'theme'])
  return { version: version(v.version), theme: theme(v.theme) }
}
function profileInput(value: ProfileInput): ProfileInput {
  const v = shape(value, ['version'], ['username', 'display_name'])
  requireValue(v.username !== undefined || v.display_name !== undefined)
  let username: string | undefined, displayName: string | undefined
  if (Object.hasOwn(v, 'username')) {
    username = string(v.username, 3, 32)
    requireValue(/^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(username))
  }
  if (Object.hasOwn(v, 'display_name')) {
    displayName = string(v.display_name, 0, 80)
    requireValue(
      !/[\x00-\x1f\x7f-\x9f]/.test(displayName) &&
        new TextEncoder().encode(displayName).byteLength <= 320,
    )
  }
  return {
    version: version(v.version),
    ...(username === undefined ? {} : { username }),
    ...(displayName === undefined ? {} : { display_name: displayName }),
  } as ProfileInput
}
function passwordInput(value: PasswordInput): PasswordInput {
  const v = shape(value, ['version', 'current_password', 'new_password', 'confirmation'])
  const current = string(v.current_password, 1, 512),
    next = string(v.new_password, 15, 128),
    confirmation = string(v.confirmation, 15, 128)
  requireValue(
    [current, next, confirmation].every((s) => new TextEncoder().encode(s).byteLength <= 512) &&
      next === confirmation,
  )
  return {
    version: version(v.version),
    current_password: current,
    new_password: next,
    confirmation,
  }
}
function writeOptions(options: WriteOptions) {
  return {
    csrf: csrf(options.csrfToken),
    key: key(options.key),
    signal: options.signal ?? new AbortController().signal,
  }
}

export function createAccountAPI(fetcher?: Fetch) {
  const request = accountTransport(fetcher)
  return {
    inspectInvitation(
      value: LinkInput,
      options: BrowserReadOptions,
    ): Promise<InvitationInspection> {
      return request(
        'inspectInvitation',
        (value) => {
          const v = shape(value, ['email', 'expires_at'])
          return { email: email(v.email), expires_at: instant(v.expires_at) }
        },
        input(() => ({ body: linkInput(value), ...browserReadOptions(options) })),
      )
    },
    redeemInvitation(value: InvitationInput, options: WriteOptions): Promise<InvitationRedeemed> {
      return request(
        'redeemInvitation',
        (value) => {
          const v = shape(value, ['completed', 'login_required'])
          requireValue(v.completed === true && v.login_required === true)
          return { completed: true, login_required: true }
        },
        input(() => ({ body: invitationInput(value), ...writeOptions(options) })),
      )
    },
    requestPasswordReset(
      value: Readonly<{ email: string }>,
      options: WriteOptions,
    ): Promise<ResetAccepted> {
      return request(
        'requestPasswordReset',
        (value) => {
          const v = shape(value, ['accepted', 'delivery_channel'])
          requireValue(
            v.accepted === true &&
              (v.delivery_channel === 'smtp' || v.delivery_channel === 'backend_log'),
          )
          return { accepted: true, delivery_channel: v.delivery_channel as DeliveryChannel }
        },
        input(() => ({
          body: { email: email(shape(value, ['email']).email) },
          ...writeOptions(options),
        })),
      )
    },
    inspectPasswordReset(value: LinkInput, options: BrowserReadOptions): Promise<ResetInspection> {
      return request(
        'inspectPasswordReset',
        (value) => {
          const v = shape(value, ['valid', 'expires_at'])
          requireValue(v.valid === true)
          return { valid: true, expires_at: instant(v.expires_at) }
        },
        input(() => ({ body: linkInput(value), ...browserReadOptions(options) })),
      )
    },
    completePasswordReset(value: ResetInput, options: WriteOptions): Promise<void> {
      return request(
        'completePasswordReset',
        () => undefined,
        input(() => {
          const v = shape(value, ['token', 'new_password', 'confirmation'])
          const password = newPassword(v.new_password, v.confirmation)
          return {
            body: {
              ...linkInput({ token: v.token as string }),
              new_password: password,
              confirmation: password,
            },
            ...writeOptions(options),
          }
        }),
      )
    },
    getProfile(signal = new AbortController().signal): Promise<ProfileView> {
      return request('profile', profile, { signal })
    },
    updateProfile(value: ProfileInput, options: WriteOptions): Promise<ProfileView> {
      return request(
        'updateProfile',
        profile,
        input(() => ({ body: profileInput(value), ...writeOptions(options) })),
      )
    },
    getPreferences(signal = new AbortController().signal): Promise<PreferencesView> {
      return request('preferences', preferences, { signal })
    },
    setPreferences(value: PreferencesView, options: WriteOptions): Promise<PreferencesView> {
      return request(
        'setPreferences',
        preferences,
        input(() => ({ body: preferences(value), ...writeOptions(options) })),
      )
    },
    readAvatar(signal = new AbortController().signal): Promise<AvatarDownload> {
      return request(
        'avatar',
        (value) => {
          const v = shape(value, ['metadata', 'blob'])
          const metadata = avatarMetadata(v.metadata)
          requireValue(
            v.blob instanceof Blob &&
              v.blob.size === Number(metadata.byte_size) &&
              v.blob.type === metadata.media_type,
          )
          return { metadata, blob: v.blob as Blob }
        },
        { signal },
      )
    },
    putAvatar(
      value: Readonly<{ version: Version; file: File; mediaType: AvatarMedia }>,
      options: WriteOptions,
    ): Promise<ProfileView> {
      return request(
        'putAvatar',
        profile,
        input(() => {
          const v = shape(value, ['version', 'file', 'mediaType'])
          requireValue(v.file instanceof File && v.file.size > 0 && v.file.size <= 5 * 1024 * 1024)
          return {
            avatar: {
              file: v.file as File,
              version: version(v.version),
              mediaType: media(v.mediaType),
            },
            ...writeOptions(options),
          }
        }),
      )
    },
    deleteAvatar(value: Readonly<{ version: Version }>, options: WriteOptions): Promise<void> {
      return request(
        'deleteAvatar',
        () => undefined,
        input(() => ({
          body: { version: version(shape(value, ['version']).version) },
          ...writeOptions(options),
        })),
      )
    },
    changePassword(
      value: PasswordInput,
      options: WriteOptions,
    ): Promise<Readonly<{ completed: true; next_path: '/' }>> {
      return request(
        'changePassword',
        (value) => {
          const v = shape(value, ['completed', 'next_path'])
          requireValue(v.completed === true && v.next_path === '/')
          return { completed: true, next_path: '/' }
        },
        input(() => ({ body: passwordInput(value), ...writeOptions(options) })),
      )
    },
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
