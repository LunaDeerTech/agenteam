import { readonly, shallowReactive } from 'vue'
import {
  createAccountAPI,
  type AccountAPI,
  type Challenge,
  type LoginResult,
  type Session,
  type SessionView,
  type User,
  type AvatarDownload,
  type AvatarMedia,
  type PasswordInput,
  type PreferencesView,
  type ProfileInput,
  type ProfileView,
  type Theme,
  type Version,
  type DeliveryChannel,
  type LinkInput,
  type InvitationInput,
  type InvitationInspection,
  type InvitationRedeemed,
  type ResetInput,
  type ResetInspection,
  type ResetAccepted,
} from '../api/account'
import { AccountFailure, type Problem } from '../api/client'
import {
  createSystemAccountAPI,
  type SystemAccountAPI,
  type SystemUserQuery,
} from '../api/system-account'
import { useTheme } from './useTheme'
import {
  createSystemInvitationAPI,
  captureInvitationCommand,
  type SystemInvitationAPI,
  type InvitationCommand,
  type InvitationMutationResult,
  type InvitationCreate,
  type InvitationTarget,
  type DeliveryTarget,
  type InvitationQuery,
} from '../api/system-invitations'

import {
  createSystemProviderAPI,
  captureProviderCommand,
  validateCredentialValue,
  type SystemProviderAPI,
  type Provider,
  type ProviderInput,
  type ProviderCommand,
  type ProviderReceipt,
  type CredentialCreated,
  type ProviderQuery,
  type ProviderModelQuery,
} from '../api/system-providers'
import {
  createSystemModelAPI,
  captureModelCommand,
  type SystemModelAPI,
  type ModelCommand,
  type ModelTarget,
  type ModelReceipt,
} from '../api/system-models'
import {
  createSystemModelSelectionAPI,
  captureSelectionCommand,
  type SystemModelSelectionAPI,
  type SelectionCommand,
  type SelectionReceipt,
} from '../api/system-model-selection'
import {
  createSystemAccountSecurityAPI,
  captureAccountSecurityUpdate,
  type SystemAccountSecurityAPI,
  type AccountSecuritySettings,
  type AccountSecurityUpdateInput,
} from '../api/system-account-security'

type AccountSecurityAction = 'account-security-read' | 'account-security-write'
export type SystemAccountSecurityProgress = Readonly<{
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  settings: AccountSecuritySettings | null
  observation: 'none' | 'current' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type SystemAccountSecurityIntent = Readonly<{
  input: AccountSecurityUpdateInput
  body: string
  singletonID: string
  identity: PersonalIdentity
  csrf: string
  key: string
}>

export type SystemModelSelectionRead =
  | 'selection-state'
  | 'selection-reference'
  | 'selection-providers'
  | 'selection-provider'
  | 'selection-models'
type SelectionAction = SystemModelSelectionRead | 'selection-write' | 'selection-lookup'
export type SystemModelSelectionProgress = Readonly<{
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  receipt: SelectionReceipt | null
  observation: 'none' | 'found' | 'missing' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type SystemModelSelectionIntent = Readonly<{
  command: SelectionCommand
  identity: PersonalIdentity
  csrf: string
  key: string
}>

export type SystemModelRead =
  | 'model-providers'
  | 'model-provider'
  | 'model-list'
  | 'model-detail'
  | 'model-impact'
  | 'model-replacement-providers'
  | 'model-replacement-provider'
  | 'model-replacement-list'
type ModelAction = SystemModelRead | 'model-write' | 'model-lookup'
export type SystemModelProgress = Readonly<{
  kind: ModelCommand['kind']
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  receipt: ModelReceipt | null
  observation: 'none' | 'found' | 'missing' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type SystemModelIntent = Readonly<{
  command: ModelCommand
  identity: PersonalIdentity
  csrf: string
  key: string
}>

type ProviderRead = 'provider-list' | 'provider-detail' | 'provider-models' | 'provider-metadata'
type ProviderAction = ProviderRead | 'provider-write' | 'provider-lookup'
export type SystemProviderProgress = Readonly<{
  kind: ProviderCommand['kind']
  stage: 'credential' | 'provider'
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  credential: CredentialCreated | null
  receipt: ProviderReceipt | null
  observation: 'none' | 'found' | 'missing' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
  canRebase: boolean
}>
interface SystemProviderIntent {
  command: ProviderCommand
  identity: PersonalIdentity
  csrf: string
  providerKey: string
  credentialKey: string | null
  value: string | null
  credential: CredentialCreated | null
  stage: 'credential' | 'provider'
  uncertain: boolean
  rejected: boolean
  checked: boolean
  keyConflict: boolean
  rebase: Provider | null
}

export type SessionPhase =
  | 'checking'
  | 'anonymous'
  | 'authenticating'
  | 'authenticated'
  | 'signing-out'
  | 'uncertain'
  | 'unavailable'
interface PublicState {
  phase: SessionPhase
  user: User | null
  session: Session | null
  busy: boolean
  challengeBusy: boolean
  challengeRequired: boolean
  challenge: Challenge | null
  passReady: boolean
  notice: string
  fields: Record<string, string>
  canRetryOriginal: boolean
}
interface LoginIntent {
  kind: 'login'
  key: string
  browser: number
  email: string
  password: string
  pass?: string
}
interface LogoutIntent {
  kind: 'logout'
  key: string
  session: string
  csrf: string
}
type Intent = LoginIntent | LogoutIntent
type Action =
  | 'restore'
  | 'login'
  | 'logout'
  | 'personal'
  | 'entry'
  | 'system'
  | 'invitation-read'
  | 'invitation-write'
  | ProviderAction
  | ModelAction
  | SelectionAction
  | AccountSecurityAction
interface Operation {
  generation: number
  kind: Action
  abort: AbortController
  expired: boolean
  confirmed: boolean
  sessionReconciled?: boolean
  visible: Promise<void>
  abandon?: () => void
}

export type PersonalIdentity = Readonly<{ userID: string; sessionID: string; epoch: number }>
export type SystemInvitationProgress = Readonly<{
  kind: InvitationCommand['kind']
  phase: 'submitting' | 'uncertain' | 'confirmed'
  canRetryOriginal: boolean
  contextValid: boolean
}>
type SystemInvitationIntent = Readonly<{
  command: InvitationCommand
  identity: PersonalIdentity
  key: string
  csrf: string
}>
export type EntrySession =
  Readonly<{ kind: 'anonymous' }> | Readonly<{ kind: 'authenticated'; identity: PersonalIdentity }>
export type EntryPreparation = Readonly<{ deliveryChannel: DeliveryChannel; session: EntrySession }>
export type ResetConfirmation =
  | Readonly<{ commandConfirmed: true; session: EntrySession }>
  | Readonly<{
      commandConfirmed: true
      session: Readonly<{ kind: 'unconfirmed' }>
      failure: AccountFailure
    }>
export type EntryMutationResult =
  | Readonly<{ kind: 'invitation'; value: InvitationRedeemed }>
  | Readonly<{ kind: 'reset-request'; value: ResetAccepted }>
  | Readonly<{ kind: 'reset-complete'; value: ResetConfirmation }>
type EntryProgress = Readonly<{
  kind: EntryMutationResult['kind']
  phase: 'submitting' | 'uncertain' | 'confirmed' | 'confirming-session' | 'session-unconfirmed'
  canRetryOriginal: boolean
  contextValid: boolean
}>
type EntryInput =
  | { kind: 'invitation'; input: InvitationInput | null }
  | { kind: 'reset-request'; input: Readonly<{ email: string }> | null }
  | { kind: 'reset-complete'; input: ResetInput | null }
type EntryCommand = EntryInput & {
  key: string
  browser: number
  csrf: string
  identity: PersonalIdentity | null
  uncertain: boolean
}
export type PasswordConfirmation =
  | Readonly<{ commandConfirmed: true; sessionConfirmed: true }>
  | Readonly<{ commandConfirmed: true; sessionConfirmed: false; failure: AccountFailure }>
export type PersonalMutationResult =
  | Readonly<{ kind: 'profile' | 'avatar-put'; value: ProfileView }>
  | Readonly<{ kind: 'preferences'; value: PreferencesView }>
  | Readonly<{ kind: 'avatar-delete' }>
  | Readonly<{ kind: 'password'; value: PasswordConfirmation }>
type PasswordProgress = Readonly<{
  identity: PersonalIdentity
  requestGeneration: number
  phase: 'confirming-session' | 'confirmed' | 'session-unconfirmed'
}>
type PersonalCommand = (
  | { kind: 'profile'; input: ProfileInput }
  | { kind: 'preferences'; input: PreferencesView }
  | {
      kind: 'avatar-put'
      input: Readonly<{ version: Version; file: File; mediaType: AvatarMedia }>
    }
  | { kind: 'avatar-delete'; input: Readonly<{ version: Version }> }
  | { kind: 'password'; input: PasswordInput | null }
) & { identity: PersonalIdentity; key: string; csrf: string; checked: boolean; unsettled: boolean }
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch

const unavailableSession = (e: unknown) =>
  e instanceof AccountFailure &&
  e.kind === 'problem' &&
  e.problem?.status === 401 &&
  ['UNAUTHENTICATED', 'SESSION_REVOKED'].includes(e.problem.code)
const isUnknown = (e: AccountFailure) =>
  ['transport', 'cancelled', 'invalid-response'].includes(e.kind) ||
  e.problem?.commit_state === 'unknown' ||
  e.problem?.code === 'COMMIT_UNKNOWN' ||
  e.problem?.code === 'RESOURCE_BUSY'

// Only this controller owns authentication materials and the four Cookie-producing
// endpoints. Its public state has no token, command key, password or retry body.
export function createSessionController(
  api: AccountAPI = createAccountAPI(),
  systemAPI: SystemAccountAPI = createSystemAccountAPI(),
  invitationAPI: SystemInvitationAPI = createSystemInvitationAPI(),
  providerAPI: SystemProviderAPI = createSystemProviderAPI(),
  modelAPI: SystemModelAPI = createSystemModelAPI(),
  selectionAPI: SystemModelSelectionAPI = createSystemModelSelectionAPI(),
  accountSecurityAPI: SystemAccountSecurityAPI = createSystemAccountSecurityAPI(),
) {
  const state = shallowReactive<PublicState>({
    phase: 'checking',
    user: null,
    session: null,
    busy: false,
    challengeBusy: false,
    challengeRequired: false,
    challenge: null,
    passReady: false,
    notice: '',
    fields: {},
    canRetryOriginal: false,
  })
  let generation = 0,
    browser = 0,
    challengeGeneration = 0
  let anonymousCSRF = '',
    sessionCSRF = ''
  let intent: Intent | null = null
  let expectedSession: LoginResult | null = null
  let owner: Operation | null = null
  let challengeAbort: AbortController | null = null
  const challengeTails = new Set<Promise<void>>()
  let pending = false
  const personalContext = shallowReactive<{
    phase: 'current' | 'checking' | 'invalid'
    identity: PersonalIdentity | null
  }>({ phase: 'invalid', identity: null })
  const personalState = shallowReactive<{ passwordProgress: PasswordProgress | null }>({
    passwordProgress: null,
  })
  const systemState = shallowReactive<{ deniedIdentity: PersonalIdentity | null }>({
    deniedIdentity: null,
  })
  let systemRevision = 0
  let invitationReadRevision = 0,
    invitationRevision = 0
  let invitationIntent: SystemInvitationIntent | null = null
  let invitationUncertain = false,
    invitationChecked = false
  const invitationState = shallowReactive<{
    progress: Readonly<{
      kind: InvitationCommand['kind']
      phase: SystemInvitationProgress['phase']
    }> | null
  }>({ progress: null })
  const providerRevisions: Record<ProviderAction, number> = {
    'provider-list': 0,
    'provider-detail': 0,
    'provider-models': 0,
    'provider-metadata': 0,
    'provider-write': 0,
    'provider-lookup': 0,
  }
  let providerIntent: SystemProviderIntent | null = null
  let providerMaterial: string | null = null
  const providerState = shallowReactive<{
    progress: Omit<SystemProviderProgress, 'contextValid' | 'canRetryOriginal' | 'canRebase'> | null
    hasMaterial: boolean
    materialInvalid: boolean
    materialRevision: number
  }>({ progress: null, hasMaterial: false, materialInvalid: false, materialRevision: 0 })
  const modelRevisions: Record<ModelAction, number> = {
    'model-providers': 0,
    'model-provider': 0,
    'model-list': 0,
    'model-detail': 0,
    'model-impact': 0,
    'model-replacement-providers': 0,
    'model-replacement-provider': 0,
    'model-replacement-list': 0,
    'model-write': 0,
    'model-lookup': 0,
  }
  const isModelAction = (kind: Action): kind is ModelAction => Object.hasOwn(modelRevisions, kind)
  let modelIntent: SystemModelIntent | null = null,
    modelUncertain = false,
    modelChecked = false,
    modelKeyConflict = false
  const modelState = shallowReactive<{
    progress: Omit<SystemModelProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const selectionRevisions: Record<SelectionAction, number> = {
    'selection-state': 0,
    'selection-reference': 0,
    'selection-providers': 0,
    'selection-provider': 0,
    'selection-models': 0,
    'selection-write': 0,
    'selection-lookup': 0,
  }
  const isSelectionAction = (kind: Action): kind is SelectionAction =>
    Object.hasOwn(selectionRevisions, kind)
  let selectionIntent: SystemModelSelectionIntent | null = null,
    selectionUncertain = false,
    selectionChecked = false,
    selectionKeyConflict = false
  const selectionState = shallowReactive<{
    progress: Omit<SystemModelSelectionProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const accountSecurityRevisions: Record<AccountSecurityAction, number> = {
    'account-security-read': 0,
    'account-security-write': 0,
  }
  const isAccountSecurityAction = (kind: Action): kind is AccountSecurityAction =>
    Object.hasOwn(accountSecurityRevisions, kind)
  let accountSecurityIntent: SystemAccountSecurityIntent | null = null,
    accountSecuritySingleton: Readonly<{ identity: PersonalIdentity; id: string }> | null = null,
    accountSecurityUncertain = false,
    accountSecurityChecked = false,
    accountSecurityKeyConflict = false
  const accountSecurityState = shallowReactive<{
    progress: Omit<SystemAccountSecurityProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const systemDenied = () => sameIdentity(systemState.deniedIdentity, personalContext.identity)
  let identityEpoch = 0,
    personalRevision = 0
  let trustedUser: User | null = null
  let personalIntent: PersonalCommand | null = null
  let preview: { identity: PersonalIdentity; theme: Theme } | null = null
  let passwordSessionCheck = false
  let passwordExpected: PersonalIdentity | null = null
  let deliveryChannel: DeliveryChannel | null = null
  let entryRevision = 0
  let entryIntent: EntryCommand | null = null
  const entryState = shallowReactive<{ progress: EntryProgress | null }>({ progress: null })
  const valid = (op: Operation) => generation === op.generation && !op.expired
  function clearChallenge() {
    ++challengeGeneration
    challengeAbort?.abort()
    challengeAbort = null
    state.challengeBusy = false
    state.challenge = null
    state.passReady = false
    if (intent?.kind === 'login') delete intent.pass
  }
  function applyTheme() {
    useTheme().setTheme(
      sameIdentity(preview?.identity ?? null, personalContext.identity)
        ? preview!.theme
        : (trustedUser?.theme ?? 'system'),
    )
  }
  function clearIdentity(invalidate = true) {
    state.user = null
    state.session = null
    if (invalidate) {
      clearInvitationState()
      clearProviderState()
      clearModelState()
      clearSelectionState()
      clearAccountSecurityState(true)
      ++systemRevision
      systemState.deniedIdentity = null
      sessionCSRF = ''
      ++identityEpoch
      ++personalRevision
      personalContext.identity = null
      personalContext.phase = 'invalid'
      trustedUser = null
      preview = null
      personalIntent = null
      personalState.passwordProgress = null
      passwordSessionCheck = false
      passwordExpected = null
    } else {
      personalContext.phase = 'checking'
    }
    applyTheme()
  }
  function forgetIntent() {
    clearChallenge()
    intent = null
    pending = false
    state.canRetryOriginal = false
    state.challengeRequired = false
  }
  function clearBrowser() {
    anonymousCSRF = ''
    deliveryChannel = null
    ++browser
  }
  function canRetry() {
    return (
      pending &&
      !!intent &&
      (intent.kind === 'login'
        ? intent.browser === browser && anonymousCSRF !== ''
        : intent.session === state.session?.id && intent.csrf === sessionCSRF && sessionCSRF !== '')
    )
  }
  function publish(view: SessionView, preserveBrowser = false) {
    if (
      passwordExpected &&
      (view.user.id !== passwordExpected.userID || view.session.id === passwordExpected.sessionID)
    )
      throw new AccountFailure('invalid-response')
    const passwordChecked = passwordExpected
    const previous = personalContext.identity
    const same =
      previous?.userID === view.user.id &&
      previous.sessionID === view.session.id &&
      (!sessionCSRF || sessionCSRF === view.csrf_token)
    if (!same) {
      ++personalRevision
      personalContext.identity = Object.freeze({
        userID: view.user.id,
        sessionID: view.session.id,
        epoch: ++identityEpoch,
      })
      personalIntent = null
      preview = null
    } else if (personalIntent) personalIntent.checked = true
    personalContext.phase = 'current'
    systemState.deniedIdentity = null
    passwordSessionCheck = false
    passwordExpected = null
    trustedUser =
      same && trustedUser && BigInt(trustedUser.version) > BigInt(view.user.version)
        ? trustedUser
        : Object.freeze({ ...view.user })
    state.user = trustedUser
    state.session = view.session
    sessionCSRF = view.csrf_token
    if (!preserveBrowser) clearBrowser()
    forgetIntent()
    expectedSession = null
    state.phase = 'authenticated'
    if (invitationIntent) {
      if (!same || view.user.role !== 'admin' || invitationIntent.csrf !== sessionCSRF)
        clearInvitationState()
      else invitationChecked = true
    }
    if (
      !same ||
      view.user.role !== 'admin' ||
      (providerIntent && providerIntent.csrf !== sessionCSRF)
    )
      clearProviderState()
    if (!same || view.user.role !== 'admin' || (modelIntent && modelIntent.csrf !== sessionCSRF))
      clearModelState()
    if (
      !same ||
      view.user.role !== 'admin' ||
      (selectionIntent && selectionIntent.csrf !== sessionCSRF)
    )
      clearSelectionState()
    if (
      !same ||
      view.user.role !== 'admin' ||
      (accountSecurityIntent && accountSecurityIntent.csrf !== sessionCSRF)
    )
      clearAccountSecurityState(true)
    state.notice = ''
    state.fields = {}
    if (passwordChecked && personalState.passwordProgress?.identity === passwordChecked)
      personalState.passwordProgress = { ...personalState.passwordProgress, phase: 'confirmed' }
    applyTheme()
  }
  function fields(problem?: Problem) {
    const result: Record<string, string> = {}
    for (const field of problem?.field_errors ?? []) {
      if (field.path === '/email') result.email = '请检查邮箱地址。'
      if (field.path === '/password') result.password = '密码应为 15–128 个字符，最多 512 字节。'
    }
    return result
  }
  function failure(op: Operation, error: unknown) {
    if (!valid(op)) return
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    state.fields = fields(e.problem)
    if (op.kind === 'restore' || op.confirmed) {
      clearIdentity(false)
      state.phase = pending ? 'uncertain' : 'unavailable'
      state.notice = op.confirmed
        ? '登录响应已接收，但当前会话尚未确认。请检查当前会话。'
        : '暂时无法确认当前会话，请重试检查。'
    } else if (isUnknown(e)) {
      pending = true
      state.phase = 'uncertain'
      state.notice = '请求结果尚未确认。可以检查当前会话，或在原上下文中重试原请求。'
    } else if (pending) {
      // A later rejected replay is not a negative observation of the earlier write.
      if (e.problem?.code === 'CSRF_FAILED') {
        clearBrowser()
        clearIdentity()
      }
      if (op.kind === 'logout' && unavailableSession(e)) clearIdentity()
      state.phase = 'uncertain'
      state.notice =
        '原请求结果仍未确认，当前上下文无法继续该次操作。可以检查当前会话或明确重新建立登录上下文。'
    } else if (
      ['CHALLENGE_REQUIRED', 'CHALLENGE_INVALID'].includes(e.problem?.code ?? '') &&
      op.kind === 'login'
    ) {
      clearChallenge()
      state.phase = 'anonymous'
      state.challengeRequired = true
      state.notice = '请完成旋转验证，再继续登录。'
    } else if (e.problem?.code === 'CSRF_FAILED') {
      clearBrowser()
      clearIdentity()
      forgetIntent()
      state.phase = 'unavailable'
      state.notice = '认证上下文已失效，请重新建立登录上下文。'
    } else if (e.problem?.code === 'IDEMPOTENCY_KEY_REUSED') {
      state.phase = 'unavailable'
      state.notice = '原请求标识与输入不一致。请检查后明确重新建立登录上下文。'
    } else if (op.kind === 'logout') {
      if (unavailableSession(e)) {
        clearIdentity()
        state.phase = 'unavailable'
        state.notice = '当前会话不可用，请重新登录。此结果不确认原注销命令是否提交。'
      } else {
        state.phase = 'authenticated'
        state.notice = e.problem?.detail || '退出登录未完成，请重试。'
      }
      forgetIntent()
    } else {
      forgetIntent()
      state.phase = 'anonymous'
      state.notice = unavailableSession(e)
        ? '邮箱或密码不正确。'
        : e.kind === 'invalid-input'
          ? '请检查邮箱和密码格式。密码须保留原输入。'
          : e.problem?.detail || '登录未完成，请重试。'
    }
    state.canRetryOriginal = canRetry()
  }
  function run(kind: Action, work: (op: Operation) => Promise<void>): Promise<void> {
    if (entryIntent) return Promise.resolve()
    if (owner) return owner.kind === kind ? owner.visible : Promise.resolve()
    const op: Operation = {
      generation: ++generation,
      kind,
      abort: new AbortController(),
      expired: false,
      confirmed: false,
      visible: Promise.resolve(),
    }
    owner = op
    state.busy = true
    state.canRetryOriginal = false
    let timer: ReturnType<typeof setTimeout>
    const expired = new Promise<void>((resolve) => {
      timer = setTimeout(() => {
        if (valid(op)) {
          failure(op, new AccountFailure('cancelled'))
          op.expired = true
        }
        op.abort.abort()
        resolve()
      }, 30_000)
    })
    // Logical timeout can finish the caller's wait, but actual fetch/body completion
    // still owns this lane. An abort-ignoring transport never frees the Cookie lane.
    const actual = Promise.resolve()
      .then(() => (valid(op) ? work(op) : undefined))
      .catch((e: unknown) => failure(op, e))
      .finally(() => {
        clearTimeout(timer)
        if (owner === op) {
          owner = null
          state.busy = false
          state.canRetryOriginal = canRetry()
        }
      })
    op.visible = Promise.race([actual, expired])
    return op.visible
  }
  async function bootstrap(op: Operation) {
    const result = await api.bootstrap(op.abort.signal)
    if (!valid(op)) return
    ++browser
    anonymousCSRF = result.csrf_token
    deliveryChannel = result.delivery_channel
    state.phase = 'anonymous'
  }
  function restore() {
    return run('restore', async (op) => {
      state.notice = ''
      state.phase = 'checking'
      // A restored identity is never displayed while its current authority is unknown.
      const previous = state.session?.id
      const previousCSRF = sessionCSRF
      clearIdentity(false)
      try {
        const result = await api.getSession(op.abort.signal)
        if (!valid(op)) return
        if (
          expectedSession &&
          (expectedSession.user.id !== result.user.id ||
            expectedSession.session.id !== result.session.id)
        ) {
          state.phase = 'unavailable'
          state.notice = '当前身份与登录响应不同，请重新检查或建立登录上下文。'
          return
        }
        if (
          pending &&
          intent?.kind === 'logout' &&
          intent.session === result.session.id &&
          previous === result.session.id &&
          previousCSRF === result.csrf_token
        ) {
          state.user = result.user
          state.session = result.session
          sessionCSRF = result.csrf_token
          state.phase = 'uncertain'
          state.notice = '当前会话仍可用；原注销结果尚未确认。'
          return
        }
        publish(result)
      } catch (e) {
        if (!valid(op)) return
        if (!unavailableSession(e)) throw e
        clearIdentity()
        if (pending || expectedSession) {
          state.phase = 'uncertain'
          state.notice = '当前会话不可用；这不证明原请求未提交。'
          return
        }
        forgetIntent()
        await bootstrap(op)
      }
    })
  }
  function newKey() {
    try {
      return crypto.randomUUID()
    } catch {
      throw new AccountFailure('invalid-input')
    }
  }
  function performLogin(original: LoginIntent) {
    return run('login', async (op) => {
      state.phase = 'authenticating'
      state.notice = ''
      state.fields = {}
      const result = await api.login(
        {
          email: original.email,
          password: original.password,
          ...(original.pass === undefined ? {} : { challenge_pass: original.pass }),
        },
        anonymousCSRF,
        original.key,
        op.abort.signal,
      )
      if (!valid(op)) return
      op.confirmed = true
      expectedSession = result
      forgetIntent()
      clearBrowser()
      const view = await api.getSession(op.abort.signal)
      if (!valid(op)) return
      if (view.user.id !== result.user.id || view.session.id !== result.session.id) {
        clearIdentity()
        state.phase = 'unavailable'
        state.notice = '当前身份与登录响应不同，请重新检查。'
        return
      }
      publish(view)
    })
  }
  function login(email: string, password: string) {
    if (owner || entryIntent || state.challengeBusy || pending) return Promise.resolve()
    if (!anonymousCSRF) {
      state.notice = '请先确认当前登录上下文。'
      return Promise.resolve()
    }
    try {
      if (!(
        intent?.kind === 'login' &&
        intent.browser === browser &&
        intent.email === email &&
        intent.password === password &&
        state.challengeRequired
      )) {
        forgetIntent()
        intent = { kind: 'login', key: newKey(), browser, email, password }
      }
      return performLogin(intent as LoginIntent)
    } catch {
      forgetIntent()
      state.notice = '无法安全建立登录请求，请检查浏览器环境。'
      return Promise.resolve()
    }
  }
  function performLogout(original: LogoutIntent) {
    return run('logout', async (op) => {
      state.phase = 'signing-out'
      state.notice = ''
      state.fields = {}
      await api.logout(original.csrf, original.key, op.abort.signal)
      if (!valid(op)) return
      clearIdentity()
      clearBrowser()
      forgetIntent()
      expectedSession = null
      state.phase = 'anonymous'
      state.notice = '已退出登录。'
    })
  }
  function logout() {
    if (owner || entryIntent || pending || passwordSessionCheck || !state.session || !sessionCSRF)
      return Promise.resolve()
    try {
      intent = { kind: 'logout', key: newKey(), session: state.session.id, csrf: sessionCSRF }
      return performLogout(intent)
    } catch {
      state.notice = '无法安全建立退出请求，请重试。'
      return Promise.resolve()
    }
  }
  function retryOriginal() {
    if (owner || !canRetry() || !intent) return Promise.resolve()
    return intent.kind === 'login' ? performLogin(intent) : performLogout(intent)
  }
  function inputChanged() {
    if (owner || pending) return
    forgetIntent()
    state.notice = ''
    state.fields = {}
  }
  function runChallenge<T>(
    original: LoginIntent,
    work: (signal: AbortSignal) => Promise<T>,
    apply: (result: T) => void,
    notice: string,
  ) {
    const captured = generation,
      revision = challengeGeneration
    const abort = new AbortController()
    challengeAbort = abort
    state.challengeBusy = true
    const current = () =>
      captured === generation &&
      revision === challengeGeneration &&
      intent === original &&
      !abort.signal.aborted
    let timeout: ReturnType<typeof setTimeout>
    const expired = new Promise<void>((resolve) => {
      timeout = setTimeout(() => {
        if (current()) {
          clearChallenge()
          state.notice = notice
        } else abort.abort()
        resolve()
      }, 30_000)
    })
    const actual = Promise.resolve()
      .then(async () => {
        if (!current()) return
        const result = await work(abort.signal)
        if (current()) apply(result)
      })
      .catch(() => {
        if (current()) {
          delete original.pass
          state.passReady = false
          state.challenge = null
          state.notice = notice
        }
      })
      .finally(() => {
        clearTimeout(timeout)
        challengeTails.delete(actual)
        if (current()) {
          state.challengeBusy = false
          challengeAbort = null
        }
      })
    // Non-Cookie requests may be logically replaced after timeout. Keep each actual
    // API/body tail owned independently; stale results/finally cannot modify its successor.
    challengeTails.add(actual)
    return Promise.race([actual, expired])
  }
  async function createChallenge() {
    if (
      owner ||
      pending ||
      intent?.kind !== 'login' ||
      intent.browser !== browser ||
      !anonymousCSRF
    )
      return
    const original = intent,
      csrf = anonymousCSRF
    clearChallenge()
    await runChallenge(
      original,
      (signal) =>
        api.createChallenge(
          { mode: 'rotate', email: original.email, login_key: original.key },
          csrf,
          signal,
        ),
      (result) => {
        state.challenge = result
        state.notice = ''
      },
      '未取得有效挑战，请明确请求新题。',
    )
  }
  async function verifyChallenge(angle: number) {
    if (
      owner ||
      pending ||
      state.challengeBusy ||
      !state.challenge ||
      intent?.kind !== 'login' ||
      intent.browser !== browser ||
      !anonymousCSRF
    )
      return
    const original = intent,
      question = state.challenge,
      csrf = anonymousCSRF
    await runChallenge(
      original,
      (signal) =>
        api.verifyChallenge(
          {
            email: original.email,
            login_key: original.key,
            challenge_id: question.id,
            proof: { angle },
          },
          csrf,
          signal,
        ),
      (result) => {
        original.pass = result.pass
        state.passReady = true
        state.challenge = null
        state.notice = '验证已完成，请继续登录。'
      },
      '验证未确认，请请求新题。',
    )
  }
  function personalIdentity(): PersonalIdentity {
    if (owner || entryIntent) throw new AccountFailure('busy')
    if (
      personalContext.phase !== 'current' ||
      !personalContext.identity ||
      state.phase !== 'authenticated' ||
      !sessionCSRF ||
      passwordSessionCheck
    )
      throw new AccountFailure('invalid-input')
    return personalContext.identity
  }
  function personalFailure(identity: PersonalIdentity, error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (
      sameIdentity(identity, personalContext.identity) &&
      (unavailableSession(e) || e.problem?.code === 'CSRF_FAILED')
    ) {
      clearIdentity()
      clearBrowser()
      state.phase = 'unavailable'
      state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
    }
    return e
  }
  function acceptUser(identity: PersonalIdentity, value: User) {
    if (!sameIdentity(identity, personalContext.identity) || value.id !== identity.userID)
      throw new AccountFailure('invalid-response')
    if (!trustedUser || BigInt(value.version) >= BigInt(trustedUser.version)) {
      trustedUser = Object.freeze({ ...value })
      if (state.phase === 'authenticated') state.user = trustedUser
      applyTheme()
    }
  }
  function runAuthorized<T>(
    identity: PersonalIdentity,
    work: (op: Operation, current: () => boolean) => Promise<T>,
    command?: PersonalCommand,
    kind:
      | 'personal'
      | 'system'
      | 'invitation-read'
      | 'invitation-write'
      | ProviderAction
      | ModelAction
      | SelectionAction
      | AccountSecurityAction = 'personal',
  ): Promise<T> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    const revisionNow = () =>
      kind === 'system'
        ? systemRevision
        : kind === 'invitation-read'
          ? invitationReadRevision
          : kind === 'invitation-write'
            ? invitationRevision
            : kind === 'personal'
              ? personalRevision
              : isModelAction(kind)
                ? modelRevisions[kind]
                : isSelectionAction(kind)
                  ? selectionRevisions[kind]
                  : isAccountSecurityAction(kind)
                    ? accountSecurityRevisions[kind]
                    : providerRevisions[kind]
    const revision = revisionNow()
    const op: Operation = {
      kind,
      generation: ++generation,
      abort: new AbortController(),
      expired: false,
      confirmed: false,
      visible: Promise.resolve(),
    }
    const current = () =>
      valid(op) &&
      revision === revisionNow() &&
      sameIdentity(identity, personalContext.identity) &&
      (kind === 'personal' ||
        (state.phase === 'authenticated' && state.user?.role === 'admin' && !systemDenied()))
    owner = op
    state.busy = true
    let resolveVisible!: (value: T) => void, rejectVisible!: (failure: AccountFailure) => void
    const visible = new Promise<T>((resolve, reject) => {
      resolveVisible = resolve
      rejectVisible = reject
    })
    op.visible = visible.then(
      () => undefined,
      () => undefined,
    )
    const unknown = (e: AccountFailure) => {
      if (command && personalIntent === command && isUnknown(e)) {
        command.checked = false
        command.unsettled = true
      }
      if (command?.kind === 'password' && sameIdentity(identity, personalContext.identity))
        passwordSessionCheck = true
    }
    const endVisible = (e: AccountFailure) => {
      if (command?.kind === 'password' && op.confirmed) {
        if (current())
          personalState.passwordProgress = {
            identity,
            requestGeneration: op.generation,
            phase: 'session-unconfirmed',
          }
        resolveVisible({
          kind: 'password',
          value: { commandConfirmed: true, sessionConfirmed: false, failure: e },
        } as T)
      } else {
        unknown(e)
        rejectVisible(e)
      }
    }
    op.abandon = () => {
      endVisible(new AccountFailure('cancelled'))
      // A password response can rotate the Cookie. Keep its original bounded
      // POST -> Session check alive even when its page no longer consumes it.
      if (command?.kind !== 'password') op.abort.abort()
    }
    const timer = setTimeout(() => {
      endVisible(new AccountFailure('cancelled'))
      op.expired = true
      op.abort.abort()
    }, 30_000)
    // This is the same owner as authentication, not another queue. The visible
    // promise is bounded; actual body/cancel completion alone releases ownership.
    const actual = Promise.resolve()
      .then(async () => {
        if (!current()) throw new AccountFailure('cancelled')
        const result = await work(op, current)
        const confirmedPassword = command?.kind === 'password' && op.confirmed
        if (!current() && !confirmedPassword) throw new AccountFailure('cancelled')
        return result
      })
      .catch((error: unknown) => {
        const e =
          kind !== 'personal'
            ? systemFailure(identity, op, current, error)
            : personalFailure(identity, error)
        if (command && personalIntent === command) {
          if (command.unsettled || isUnknown(e) || e.problem?.code === 'IDEMPOTENCY_KEY_REUSED') {
            command.checked = false
            command.unsettled = true
          } else personalIntent = null
        }
        if (command?.kind === 'password' && !op.confirmed) {
          if (isUnknown(e)) unknown(e)
          else if (sameIdentity(identity, personalContext.identity)) passwordSessionCheck = false
        }
        if (command?.kind === 'password' && op.confirmed)
          return {
            kind: 'password',
            value: { commandConfirmed: true, sessionConfirmed: false, failure: e },
          } as T
        throw e
      })
      .finally(() => {
        clearTimeout(timer)
        op.abandon = undefined
        if (owner === op) {
          owner = null
          state.busy = false
        }
      })
    void actual.then(resolveVisible, rejectVisible)
    return visible
  }
  function readPersonal<T>(
    work: (identity: PersonalIdentity, op: Operation, current: () => boolean) => Promise<T>,
  ): Promise<T> {
    try {
      const identity = personalIdentity()
      return runAuthorized(identity, (op, current) => work(identity, op, current))
    } catch (e) {
      return Promise.reject(e)
    }
  }
  function startPersonal(
    command: Omit<PersonalCommand, 'identity' | 'key' | 'csrf' | 'checked' | 'unsettled'>,
  ): Promise<PersonalMutationResult> {
    try {
      const identity = personalIdentity()
      if (personalIntent) throw new AccountFailure('busy')
      personalState.passwordProgress = null
      const original = {
        ...command,
        identity,
        key: newKey(),
        csrf: sessionCSRF,
        checked: true,
        unsettled: false,
      } as PersonalCommand
      personalIntent = original
      return performPersonal(original)
    } catch (e) {
      return Promise.reject(e)
    }
  }
  function performPersonal(original: PersonalCommand): Promise<PersonalMutationResult> {
    const identity = original.identity
    return runAuthorized(
      identity,
      async (op, current) => {
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        let result: PersonalMutationResult
        switch (original.kind) {
          case 'profile': {
            const value = await api.updateProfile(original.input, options)
            if (current()) acceptUser(identity, value.user)
            result = { kind: original.kind, value }
            break
          }
          case 'avatar-put': {
            const value = await api.putAvatar(original.input, options)
            if (current()) acceptUser(identity, value.user)
            result = { kind: original.kind, value }
            break
          }
          case 'preferences': {
            const value = await api.setPreferences(original.input, options)
            if (current() && trustedUser) acceptUser(identity, { ...trustedUser, ...value })
            result = { kind: original.kind, value }
            break
          }
          case 'avatar-delete':
            await api.deleteAvatar(original.input, options)
            result = { kind: original.kind }
            break
          case 'password': {
            if (!original.input) throw new AccountFailure('invalid-input')
            passwordSessionCheck = true
            await api.changePassword(original.input, options)
            // The exact 200 is a command fact, independent of the following GET.
            original.input = null
            op.confirmed = true
            if (sameIdentity(identity, personalContext.identity)) passwordExpected = identity
            if (personalIntent === original) personalIntent = null
            if (current())
              personalState.passwordProgress = {
                identity,
                requestGeneration: op.generation,
                phase: 'confirming-session',
              }
            try {
              if (op.abort.signal.aborted) throw new AccountFailure('cancelled')
              const view = await api.getSession(op.abort.signal)
              if (view.user.id !== identity.userID || view.session.id === identity.sessionID)
                throw new AccountFailure('invalid-response')
              // Page abandonment cannot skip Cookie reconciliation. Publication is
              // still restricted to this original actual owner/identity and deadline.
              if (valid(op) && sameIdentity(identity, personalContext.identity)) {
                const showProgress = current()
                publish(view)
                if (showProgress)
                  personalState.passwordProgress = {
                    identity,
                    requestGeneration: op.generation,
                    phase: 'confirmed',
                  }
              } else throw new AccountFailure('cancelled')
              result = {
                kind: 'password',
                value: { commandConfirmed: true, sessionConfirmed: true },
              }
            } catch (error) {
              const e = personalFailure(identity, error)
              if (valid(op) && sameIdentity(identity, personalContext.identity)) {
                if (current())
                  personalState.passwordProgress = {
                    identity,
                    requestGeneration: op.generation,
                    phase: 'session-unconfirmed',
                  }
                clearIdentity(false)
                state.phase = 'unavailable'
                state.notice =
                  '密码修改已确认，但新会话尚未确认。请检查当前会话或重新登录，不要再次提交改密。'
              }
              result = {
                kind: 'password',
                value: { commandConfirmed: true, sessionConfirmed: false, failure: e },
              }
            }
            break
          }
        }
        if (personalIntent === original) personalIntent = null
        return result
      },
      original,
    )
  }
  function systemFailure(
    identity: PersonalIdentity,
    op: Operation,
    current: () => boolean,
    error: unknown,
  ) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (sameIdentity(identity, personalContext.identity) && generation === op.generation) {
      // A late 401 may already have removed this same Session Cookie. It still
      // invalidates that identity, but never a later identity or another owner.
      if (
        unavailableSession(e) ||
        ([
          'invitation-write',
          'provider-write',
          'provider-lookup',
          'model-write',
          'model-lookup',
          'selection-write',
          'selection-lookup',
          'account-security-write',
        ].includes(op.kind) &&
          e.problem?.code === 'CSRF_FAILED')
      ) {
        clearIdentity()
        clearBrowser()
        state.phase = 'unavailable'
        state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
      } else if (current() && e.problem?.status === 403 && e.problem.code === 'FORBIDDEN') {
        clearInvitationState()
        clearProviderState()
        clearModelState()
        clearSelectionState()
        clearAccountSecurityState(true)
        systemState.deniedIdentity = identity
      }
    }
    return e
  }
  function clearInvitationState() {
    ++invitationRevision
    ++invitationReadRevision
    invitationIntent = null
    invitationUncertain = false
    invitationChecked = false
    invitationState.progress = null
    if (owner?.kind === 'invitation-read' || owner?.kind === 'invitation-write') owner.abandon?.()
  }
  function invitationContext(original: Pick<SystemInvitationIntent, 'identity' | 'csrf'>) {
    return (
      sameIdentity(original.identity, personalContext.identity) &&
      personalContext.phase === 'current' &&
      state.phase === 'authenticated' &&
      state.user?.role === 'admin' &&
      !systemDenied() &&
      !!sessionCSRF &&
      original.csrf === sessionCSRF
    )
  }
  function invitationIdentity() {
    const identity = personalIdentity()
    if (state.user?.role !== 'admin' || systemDenied()) throw new AccountFailure('invalid-input')
    return identity
  }
  function performInvitation(original: SystemInvitationIntent): Promise<InvitationMutationResult> {
    if (!invitationContext(original) || invitationIntent !== original)
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = invitationRevision
    let dispatched = false
    invitationState.progress = { kind: original.command.kind, phase: 'submitting' }
    return runAuthorized<InvitationMutationResult>(
      original.identity,
      async (op) => {
        const command = original.command
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        dispatched = true
        switch (command.kind) {
          case 'create':
            return {
              kind: 'create',
              value: await invitationAPI.createInvitation(command.input, options),
            }
          case 'resend':
            return {
              kind: 'resend',
              value: await invitationAPI.resendInvitation(command.input, options),
            }
          case 'revoke':
            await invitationAPI.revokeInvitation(command.input, options)
            return { kind: 'revoke' }
          case 'retry':
            return {
              kind: 'retry',
              value: await invitationAPI.retryDelivery(command.input, options),
            }
        }
      },
      undefined,
      'invitation-write',
    ).then(
      (result) => {
        if (
          invitationIntent !== original ||
          revision !== invitationRevision ||
          !invitationContext(original)
        )
          throw new AccountFailure('cancelled')
        invitationIntent = null
        invitationUncertain = false
        invitationChecked = false
        invitationState.progress = { kind: result.kind, phase: 'confirmed' }
        return result
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          invitationIntent === original &&
          revision === invitationRevision &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const problem = e.problem
          // RetryMailJob can commit and then fail its read with 5xx/not_started.
          // A later refusal never erases an earlier unconfirmed attempt.
          if (
            invitationUncertain ||
            (dispatched &&
              (isUnknown(e) ||
                (problem?.status ?? 0) >= 500 ||
                problem?.commit_state === 'committed' ||
                problem?.code === 'IDEMPOTENCY_KEY_REUSED'))
          ) {
            invitationUncertain = true
            invitationChecked = false
            invitationState.progress = { kind: original.command.kind, phase: 'uncertain' }
          } else {
            invitationIntent = null
            invitationState.progress = null
          }
        }
        throw e
      },
    )
  }
  function startInvitation(command: InvitationCommand) {
    try {
      const captured = captureInvitationCommand(command)
      const identity = invitationIdentity()
      if (invitationIntent || personalIntent || pending) throw new AccountFailure('busy')
      const original = Object.freeze({
        command: captured,
        identity,
        key: newKey(),
        csrf: sessionCSRF,
      })
      invitationIntent = original
      invitationChecked = true
      invitationUncertain = false
      return performInvitation(original)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function clearProviderMaterial() {
    providerMaterial = null
    providerState.hasMaterial = false
    providerState.materialInvalid = false
    ++providerState.materialRevision
  }
  function abandonProviderRead(scope: ProviderRead) {
    ++providerRevisions[scope]
    if (owner?.kind === scope) owner.abandon?.()
  }
  function clearProviderState() {
    for (const scope of Object.keys(providerRevisions) as ProviderAction[])
      ++providerRevisions[scope]
    if (providerIntent) providerIntent.value = null
    providerIntent = null
    clearProviderMaterial()
    providerState.progress = null
    if (owner?.kind.startsWith('provider-')) owner.abandon?.()
  }
  function providerContext(original: SystemProviderIntent) {
    return invitationContext(original)
  }
  function publishProvider(
    original: SystemProviderIntent,
    phase: SystemProviderProgress['phase'],
    receipt: ProviderReceipt | null = null,
  ) {
    providerState.progress = Object.freeze({
      kind: original.command.kind,
      stage: original.stage,
      phase,
      credential: original.credential,
      receipt,
      observation: 'none',
    })
  }
  function readProvider<T>(
    scope: ProviderRead,
    work: (signal: AbortSignal) => Promise<T>,
  ): Promise<T> {
    try {
      const identity = invitationIdentity()
      return runAuthorized(identity, (op) => work(op.abort.signal), undefined, scope)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function performProvider(original: SystemProviderIntent): Promise<ProviderReceipt> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (providerIntent !== original || !providerContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = providerRevisions['provider-write']
    let dispatched = false
    original.checked = false
    original.rebase = null
    publishProvider(original, 'submitting')
    return runAuthorized(
      original.identity,
      async (op, current) => {
        const live = () =>
          current() &&
          !op.abort.signal.aborted &&
          providerIntent === original &&
          providerContext(original)
        const assertLive = () => {
          if (!live()) throw new AccountFailure('cancelled')
        }
        assertLive()
        if (original.stage === 'credential') {
          if (!original.value || !original.credentialKey) throw new AccountFailure('invalid-input')
          dispatched = true
          const created = await providerAPI.createCredential(original.value, {
            csrfToken: original.csrf,
            key: original.credentialKey,
            signal: op.abort.signal,
          })
          assertLive()
          // The strict API only returns after fetch/body/cancel actually join.
          // No copy of the write-only value survives for Provider recovery.
          original.value = null
          clearProviderMaterial()
          // Clearing the public presence flag can synchronously abandon this
          // workflow. Never publish its old reference after that notification.
          assertLive()
          original.credential = created
          const command = original.command
          if (command.kind === 'provider.delete') throw new AccountFailure('invalid-input')
          original.command = captureProviderCommand({
            ...command,
            input: { ...command.input, credential_ref: created.credential_id },
          })
          original.stage = 'provider'
          original.uncertain = false
          original.keyConflict = false
          dispatched = false
          publishProvider(original, 'submitting')
          // Synchronous consumers may abandon on a stage transition. Recheck
          // after publication as well as after the preceding actual transport.
          assertLive()
        }
        const command = original.command
        const write = {
          csrfToken: original.csrf,
          key: original.providerKey,
          signal: op.abort.signal,
        }
        dispatched = true
        if (command.kind === 'provider.create')
          return providerAPI.createProvider(command.input, write)
        if (command.kind === 'provider.update')
          return providerAPI.updateProvider(
            command.id,
            command.expected_version,
            command.input,
            write,
          )
        return providerAPI.deleteProvider(command.id, command.expected_version, write)
      },
      undefined,
      'provider-write',
    ).then(
      (receipt) => {
        if (
          providerIntent !== original ||
          revision !== providerRevisions['provider-write'] ||
          !providerContext(original)
        )
          throw new AccountFailure('cancelled')
        publishProvider(original, 'confirmed', receipt)
        // A synchronous receipt consumer may abandon and start a new intent.
        // The old continuation must not erase that new intent or its material.
        if (
          providerIntent !== original ||
          revision !== providerRevisions['provider-write'] ||
          !providerContext(original)
        )
          throw new AccountFailure('cancelled')
        original.value = null
        providerIntent = null
        clearProviderMaterial()
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          providerIntent === original &&
          revision === providerRevisions['provider-write'] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          const known =
            !dispatched ||
            (p &&
              ['not_started', 'not_committed'].includes(p.commit_state) &&
              p.status < 500 &&
              [
                'INVALID_ARGUMENT',
                'VERSION_CONFLICT',
                'NOT_FOUND',
                'INVALID_STATE',
                'CAPABILITY_UNSUPPORTED',
                'RATE_LIMITED',
                'RESOURCE_DELETED',
              ].includes(p.code))
          original.keyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          original.uncertain ||= !known || original.keyConflict
          original.rejected = !original.uncertain
          original.checked = false
          publishProvider(original, original.uncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const providers = {
    get progress(): SystemProviderProgress | null {
      const value = providerState.progress
      if (!value) return null
      const original = providerIntent
      const contextValid = !!original && providerContext(original)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!original &&
          original.uncertain &&
          original.checked &&
          !original.keyConflict &&
          contextValid &&
          !state.busy,
        canRebase:
          !!original &&
          original.stage === 'provider' &&
          original.command.kind === 'provider.update' &&
          original.rejected &&
          !original.uncertain &&
          contextValid &&
          !state.busy,
      })
    },
    get material() {
      return Object.freeze({
        present: providerState.hasMaterial,
        invalid: providerState.materialInvalid,
        revision: providerState.materialRevision,
      })
    },
    setMaterial(value: string) {
      invitationIdentity()
      if (providerIntent) throw new AccountFailure('busy')
      if (value === '') {
        clearProviderMaterial()
        return
      }
      try {
        providerMaterial = validateCredentialValue(value)
        providerState.hasMaterial = true
        providerState.materialInvalid = false
      } catch (error) {
        clearProviderMaterial()
        providerState.materialInvalid = true
        throw error
      }
    },
    list(query: ProviderQuery) {
      const captured = Object.freeze({ ...query })
      return readProvider('provider-list', (signal) => providerAPI.listProviders(captured, signal))
    },
    get(id: string) {
      return readProvider('provider-detail', (signal) => providerAPI.getProvider(id, signal))
    },
    models(query: ProviderModelQuery) {
      const captured = Object.freeze({ ...query })
      return readProvider('provider-models', (signal) => providerAPI.listModels(captured, signal))
    },
    metadata(id: string) {
      return readProvider('provider-metadata', (signal) =>
        providerAPI.getCredentialMetadata(id, signal),
      )
    },
    abandonRead: abandonProviderRead,
    abandon: clearProviderState,
    start(command: ProviderCommand) {
      try {
        const identity = invitationIdentity()
        if (owner || providerIntent || personalIntent || pending) throw new AccountFailure('busy')
        if (
          providerState.materialInvalid ||
          (command.kind === 'provider.delete' && providerMaterial !== null)
        )
          throw new AccountFailure('invalid-input')
        const value = providerMaterial === null ? null : validateCredentialValue(providerMaterial)
        const captured = captureProviderCommand(command, value !== null)
        const original: SystemProviderIntent = {
          command: captured,
          identity,
          csrf: sessionCSRF,
          providerKey: newKey(),
          credentialKey: value === null ? null : newKey(),
          value,
          credential: null,
          stage: value === null ? 'provider' : 'credential',
          uncertain: false,
          rejected: false,
          checked: false,
          keyConflict: false,
          rebase: null,
        }
        providerIntent = original
        providerMaterial = null
        return performProvider(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal() {
      const original = providerIntent
      if (!original || !original.uncertain || owner) throw new AccountFailure('invalid-input')
      original.checked = false
      await restore()
      if (providerIntent !== original || !providerContext(original))
        throw new AccountFailure('cancelled')
      // Session identity and original CSRF, not a list/metadata observation,
      // determine whether the exact historical Execute can be retried.
      original.checked = true
      return runAuthorized(
        original.identity,
        async (op, current) => {
          const options = {
            csrfToken: original.csrf,
            key: original.stage === 'credential' ? original.credentialKey! : original.providerKey,
            signal: op.abort.signal,
          }
          const observed =
            original.stage === 'credential'
              ? (await providerAPI.lookupCredentialCreate(options)).observed
              : (await providerAPI.lookupProviderCommand(original.command, options)).found
          if (current() && providerIntent === original && providerState.progress)
            providerState.progress = {
              ...providerState.progress,
              observation: observed ? 'found' : 'missing',
            }
          return observed
        },
        undefined,
        'provider-lookup',
      ).catch((error: unknown) => {
        if (providerIntent === original && providerContext(original) && providerState.progress)
          providerState.progress = { ...providerState.progress, observation: 'failed' }
        throw error
      })
    },
    retryOriginal() {
      const original = providerIntent
      if (!original || !providers.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performProvider(original)
    },
    async readForRebase() {
      const original = providerIntent
      if (
        !original ||
        !providers.progress?.canRebase ||
        original.command.kind !== 'provider.update'
      )
        throw new AccountFailure('invalid-input')
      const target = original.command.id
      const value = await readProvider('provider-detail', (signal) =>
        providerAPI.getProvider(target, signal),
      )
      if (providerIntent !== original || !providerContext(original))
        throw new AccountFailure('cancelled')
      if (
        original.command.kind !== 'provider.update' ||
        value.input.protocol !== original.command.input.protocol
      )
        throw new AccountFailure('invalid-response')
      original.rebase = value
      return value
    },
    rebase(input: ProviderInput) {
      try {
        const original = providerIntent
        if (
          !original ||
          !providers.progress?.canRebase ||
          !original.rebase ||
          original.command.kind !== 'provider.update'
        )
          throw new AccountFailure('invalid-input')
        const ref = original.credential?.credential_id ?? original.rebase.input.credential_ref
        if (input.protocol !== original.command.input.protocol || input.credential_ref !== ref)
          throw new AccountFailure('invalid-input')
        const captured = captureProviderCommand({
          kind: 'provider.update',
          id: original.command.id,
          expected_version: original.rebase.version,
          input,
        })
        original.command = captured
        original.providerKey = newKey()
        original.rejected = false
        original.rebase = null
        return performProvider(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
  }
  function clearModelState() {
    for (const scope of Object.keys(modelRevisions) as ModelAction[]) ++modelRevisions[scope]
    modelIntent = null
    modelUncertain = modelChecked = modelKeyConflict = false
    modelState.progress = null
    if (owner && isModelAction(owner.kind)) owner.abandon?.()
  }
  function abandonModelRead(scope: SystemModelRead) {
    if (
      !Object.hasOwn(modelRevisions, scope) ||
      scope === ('model-write' as string) ||
      scope === ('model-lookup' as string)
    )
      throw new AccountFailure('invalid-input')
    ++modelRevisions[scope]
    if (owner?.kind === scope) owner.abandon?.()
  }
  function readModel<T>(
    scope: SystemModelRead,
    work: (signal: AbortSignal) => Promise<T>,
  ): Promise<T> {
    try {
      const identity = invitationIdentity()
      return runAuthorized(identity, (op) => work(op.abort.signal), undefined, scope)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function captureModelRead<T extends object>(value: T): Readonly<T> {
    if (!value || typeof value !== 'object' || Array.isArray(value))
      throw new AccountFailure('invalid-input')
    return Object.freeze({ ...value })
  }
  function publishModel(
    original: SystemModelIntent,
    phase: SystemModelProgress['phase'],
    receipt: ModelReceipt | null = null,
  ) {
    modelState.progress = Object.freeze({
      kind: original.command.kind,
      phase,
      receipt,
      observation: 'none',
    })
  }
  function performModel(original: SystemModelIntent): Promise<ModelReceipt> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (modelIntent !== original || !invitationContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = modelRevisions['model-write']
    let dispatched = false
    modelChecked = false
    publishModel(original, 'submitting')
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || modelIntent !== original || !invitationContext(original))
          throw new AccountFailure('cancelled')
        const command = original.command
        const context = { provider_id: command.provider_id, protocol: command.protocol }
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        dispatched = true
        if (command.kind === 'model.create')
          return modelAPI.createModel(context, command.input, options)
        const target = { ...context, id: command.id }
        if (command.kind === 'model.update')
          return modelAPI.updateModel(target, command.expected_version, command.input, options)
        return modelAPI.deleteModel(target, command.expected_version, command.replacement, options)
      },
      undefined,
      'model-write',
    ).then(
      (receipt) => {
        const current = () =>
          modelIntent === original &&
          revision === modelRevisions['model-write'] &&
          invitationContext(original)
        if (!current()) throw new AccountFailure('cancelled')
        publishModel(original, 'confirmed', receipt)
        // A synchronous consumer may abandon or start another command from this
        // notification. The old completion must never clear that successor.
        if (!current()) throw new AccountFailure('cancelled')
        modelIntent = null
        modelUncertain = modelChecked = modelKeyConflict = false
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          modelIntent === original &&
          revision === modelRevisions['model-write'] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          const known =
            !dispatched ||
            (p &&
              ['not_started', 'not_committed'].includes(p.commit_state) &&
              p.status < 500 &&
              [
                'INVALID_ARGUMENT',
                'VERSION_CONFLICT',
                'NOT_FOUND',
                'INVALID_STATE',
                'CAPABILITY_UNSUPPORTED',
                'RATE_LIMITED',
                'RESOURCE_DELETED',
              ].includes(p.code))
          modelKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          modelUncertain ||= !known || modelKeyConflict
          modelChecked = false
          publishModel(original, modelUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const models = {
    get progress(): SystemModelProgress | null {
      const value = modelState.progress
      if (!value) return null
      const contextValid = !!modelIntent && invitationContext(modelIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!modelIntent &&
          modelUncertain &&
          modelChecked &&
          !modelKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    async listProviders(query: ProviderQuery, replacement = false) {
      const captured = captureModelRead(query)
      return readModel(replacement ? 'model-replacement-providers' : 'model-providers', (signal) =>
        modelAPI.listProviders(captured, signal),
      )
    },
    getProvider(id: string, replacement = false) {
      return readModel(replacement ? 'model-replacement-provider' : 'model-provider', (signal) =>
        modelAPI.getProvider(id, signal),
      )
    },
    async list(query: ProviderModelQuery, replacement = false) {
      const captured = captureModelRead(query)
      return readModel(replacement ? 'model-replacement-list' : 'model-list', (signal) =>
        modelAPI.listModels(captured, signal),
      )
    },
    async get(target: ModelTarget) {
      const captured = captureModelRead(target)
      return readModel('model-detail', (signal) => modelAPI.getModel(captured, signal))
    },
    impact(id: string) {
      return readModel('model-impact', (signal) => modelAPI.getDeletionImpact(id, signal))
    },
    abandonRead: abandonModelRead,
    abandon: clearModelState,
    start(command: ModelCommand) {
      try {
        const identity = invitationIdentity()
        if (owner || modelIntent || personalIntent || pending) throw new AccountFailure('busy')
        const captured = captureModelCommand(command)
        const original = Object.freeze({
          command: captured,
          identity,
          csrf: sessionCSRF,
          key: newKey(),
        })
        modelIntent = original
        modelUncertain = modelChecked = modelKeyConflict = false
        return performModel(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal() {
      const original = modelIntent
      if (!original || !modelUncertain || owner) throw new AccountFailure('invalid-input')
      modelChecked = false
      await restore()
      if (modelIntent !== original || !invitationContext(original))
        throw new AccountFailure('cancelled')
      modelChecked = true
      // Lookup observes history only. Session/CSRF validation allows a later
      // explicit original Execute even when lookup is missing or unavailable.
      return runAuthorized(
        original.identity,
        async (op, current) => {
          if (modelIntent !== original || !current()) throw new AccountFailure('cancelled')
          const observed = await modelAPI.lookupModelCommand(original.command, {
            csrfToken: original.csrf,
            key: original.key,
            signal: op.abort.signal,
          })
          if (current() && modelIntent === original && modelState.progress)
            modelState.progress = {
              ...modelState.progress,
              observation: observed.found ? 'found' : 'missing',
            }
          return observed
        },
        undefined,
        'model-lookup',
      ).catch((error: unknown) => {
        if (modelIntent === original && invitationContext(original) && modelState.progress)
          modelState.progress = { ...modelState.progress, observation: 'failed' }
        throw error
      })
    },
    retryOriginal() {
      if (!modelIntent || !models.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performModel(modelIntent)
    },
  }
  function clearSelectionState() {
    const retiring = owner && isSelectionAction(owner.kind) ? owner : null
    for (const scope of Object.keys(selectionRevisions) as SelectionAction[])
      ++selectionRevisions[scope]
    selectionIntent = null
    selectionUncertain = selectionChecked = selectionKeyConflict = false
    selectionState.progress = null
    retiring?.abandon?.()
  }
  function abandonSelectionRead(scope: SystemModelSelectionRead) {
    if (
      !Object.hasOwn(selectionRevisions, scope) ||
      scope === ('selection-write' as string) ||
      scope === ('selection-lookup' as string)
    )
      throw new AccountFailure('invalid-input')
    ++selectionRevisions[scope]
    if (owner?.kind === scope) owner.abandon?.()
  }
  function readSelection<T>(
    scope: SystemModelSelectionRead,
    work: (signal: AbortSignal) => Promise<T>,
  ): Promise<T> {
    try {
      const identity = invitationIdentity()
      // One operation encloses the complete private Model -> Provider read.
      return runAuthorized(identity, (op) => work(op.abort.signal), undefined, scope)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function publishSelection(
    phase: SystemModelSelectionProgress['phase'],
    receipt: SelectionReceipt | null = null,
  ) {
    selectionState.progress = Object.freeze({ phase, receipt, observation: 'none' })
  }
  function performSelection(original: SystemModelSelectionIntent): Promise<SelectionReceipt> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (selectionIntent !== original || !invitationContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = selectionRevisions['selection-write']
    const live = () =>
      selectionIntent === original &&
      revision === selectionRevisions['selection-write'] &&
      invitationContext(original)
    let dispatched = false
    selectionChecked = false
    publishSelection('submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        dispatched = true
        return selectionAPI.updateSelection(original.command, {
          csrfToken: original.csrf,
          key: original.key,
          signal: op.abort.signal,
        })
      },
      undefined,
      'selection-write',
    ).then(
      (receipt) => {
        if (!live()) throw new AccountFailure('cancelled')
        publishSelection('confirmed', receipt)
        // Synchronous consumers may retire this intent and start a successor.
        if (!live()) throw new AccountFailure('cancelled')
        selectionIntent = null
        selectionUncertain = selectionChecked = selectionKeyConflict = false
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          selectionIntent === original &&
          revision === selectionRevisions['selection-write'] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          const known =
            !dispatched ||
            (p &&
              ['not_started', 'not_committed'].includes(p.commit_state) &&
              p.status < 500 &&
              [
                'INVALID_ARGUMENT',
                'VERSION_CONFLICT',
                'NOT_FOUND',
                'INVALID_STATE',
                'CAPABILITY_UNSUPPORTED',
                'RATE_LIMITED',
                'RESOURCE_DELETED',
              ].includes(p.code))
          selectionKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          selectionUncertain ||= !known || selectionKeyConflict
          selectionChecked = false
          publishSelection(selectionUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const selection = {
    get progress(): SystemModelSelectionProgress | null {
      const value = selectionState.progress
      if (!value) return null
      const contextValid = !!selectionIntent && invitationContext(selectionIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!selectionIntent &&
          selectionUncertain &&
          selectionChecked &&
          !selectionKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    get() {
      return readSelection('selection-state', (signal) => selectionAPI.getSelection(signal))
    },
    getSavedModel(id: string) {
      return readSelection('selection-reference', (signal) =>
        selectionAPI.getSavedModel(id, signal),
      )
    },
    async listProviders(query: ProviderQuery) {
      const captured = captureModelRead(query)
      return readSelection('selection-providers', (signal) =>
        selectionAPI.listProviders(captured, signal),
      )
    },
    getProvider(id: string) {
      return readSelection('selection-provider', (signal) => selectionAPI.getProvider(id, signal))
    },
    async listModels(query: ProviderModelQuery) {
      const captured = captureModelRead(query)
      return readSelection('selection-models', (signal) =>
        selectionAPI.listModels(captured, signal),
      )
    },
    abandonRead: abandonSelectionRead,
    abandon: clearSelectionState,
    start(command: SelectionCommand) {
      try {
        const identity = invitationIdentity()
        if (owner || selectionIntent || personalIntent || pending) throw new AccountFailure('busy')
        const captured = captureSelectionCommand(command)
        const original = Object.freeze({
          command: captured,
          identity,
          csrf: sessionCSRF,
          key: newKey(),
        })
        selectionIntent = original
        selectionUncertain = selectionChecked = selectionKeyConflict = false
        return performSelection(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal() {
      const original = selectionIntent
      if (!original || !selectionUncertain || owner) throw new AccountFailure('invalid-input')
      selectionChecked = false
      await restore()
      if (selectionIntent !== original || !invitationContext(original))
        throw new AccountFailure('cancelled')
      selectionChecked = true
      // A refreshed identity permits explicit Execute; lookup never confirms its body.
      return runAuthorized(
        original.identity,
        async (op, current) => {
          if (selectionIntent !== original || !current()) throw new AccountFailure('cancelled')
          const observed = await selectionAPI.lookupSelectionCommand(original.command, {
            csrfToken: original.csrf,
            key: original.key,
            signal: op.abort.signal,
          })
          if (current() && selectionIntent === original && selectionState.progress)
            selectionState.progress = Object.freeze({
              ...selectionState.progress,
              observation: observed.found ? 'found' : 'missing',
            })
          return observed
        },
        undefined,
        'selection-lookup',
      ).catch((error: unknown) => {
        if (selectionIntent === original && invitationContext(original) && selectionState.progress)
          selectionState.progress = Object.freeze({
            ...selectionState.progress,
            observation: 'failed',
          })
        throw error
      })
    },
    retryOriginal() {
      if (!selectionIntent || !selection.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performSelection(selectionIntent)
    },
  }
  function clearAccountSecurityState(clearSingleton = false) {
    const retiring = owner && isAccountSecurityAction(owner.kind) ? owner : null
    ++accountSecurityRevisions['account-security-read']
    ++accountSecurityRevisions['account-security-write']
    accountSecurityIntent = null
    accountSecurityUncertain = accountSecurityChecked = accountSecurityKeyConflict = false
    accountSecurityState.progress = null
    if (clearSingleton) accountSecuritySingleton = null
    retiring?.abandon?.()
  }
  function readAccountSecurity(): Promise<AccountSecuritySettings> {
    try {
      const identity = invitationIdentity()
      return runAuthorized(
        identity,
        async (op, current) => {
          const value = await accountSecurityAPI.getSettings(op.abort.signal)
          if (!current()) throw new AccountFailure('cancelled')
          if (
            accountSecuritySingleton &&
            (!sameIdentity(accountSecuritySingleton.identity, identity) ||
              value.id !== accountSecuritySingleton.id)
          )
            throw new AccountFailure('invalid-response')
          accountSecuritySingleton ??= Object.freeze({ identity, id: value.id })
          return value
        },
        undefined,
        'account-security-read',
      )
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function publishAccountSecurity(
    phase: SystemAccountSecurityProgress['phase'],
    settings: AccountSecuritySettings | null = null,
  ) {
    accountSecurityState.progress = Object.freeze({ phase, settings, observation: 'none' })
  }
  function performAccountSecurity(
    original: SystemAccountSecurityIntent,
  ): Promise<AccountSecuritySettings> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (accountSecurityIntent !== original || !invitationContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = accountSecurityRevisions['account-security-write']
    const live = () =>
      accountSecurityIntent === original &&
      revision === accountSecurityRevisions['account-security-write'] &&
      invitationContext(original)
    let dispatched = false
    accountSecurityChecked = false
    publishAccountSecurity('submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        // The private bytes, values and version stay bound to one original intent.
        if (JSON.stringify(original.input) !== original.body)
          throw new AccountFailure('invalid-input')
        dispatched = true
        const value = await accountSecurityAPI.updateSettings(original.input, {
          csrfToken: original.csrf,
          key: original.key,
          signal: op.abort.signal,
        })
        if (value.id !== original.singletonID) throw new AccountFailure('invalid-response')
        return value
      },
      undefined,
      'account-security-write',
    ).then(
      (value) => {
        if (!live()) throw new AccountFailure('cancelled')
        publishAccountSecurity('confirmed', value)
        // A synchronous consumer can abandon or start another operation here.
        if (!live()) throw new AccountFailure('cancelled')
        accountSecurityIntent = null
        accountSecurityUncertain = accountSecurityChecked = accountSecurityKeyConflict = false
        return value
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          accountSecurityIntent === original &&
          revision === accountSecurityRevisions['account-security-write'] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          const known =
            !dispatched ||
            (p &&
              ['not_started', 'not_committed'].includes(p.commit_state) &&
              p.status < 500 &&
              [
                'INVALID_ARGUMENT',
                'VERSION_CONFLICT',
                'NOT_FOUND',
                'INVALID_STATE',
                'RATE_LIMITED',
                'RESOURCE_DELETED',
              ].includes(p.code))
          accountSecurityKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          accountSecurityUncertain ||= !known || accountSecurityKeyConflict
          accountSecurityChecked = false
          publishAccountSecurity(accountSecurityUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const accountSecurity = {
    get progress(): SystemAccountSecurityProgress | null {
      const value = accountSecurityState.progress
      if (!value) return null
      const contextValid = !!accountSecurityIntent && invitationContext(accountSecurityIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!accountSecurityIntent &&
          accountSecurityUncertain &&
          accountSecurityChecked &&
          !accountSecurityKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    get: readAccountSecurity,
    abandonRead() {
      ++accountSecurityRevisions['account-security-read']
      if (owner?.kind === 'account-security-read') owner.abandon?.()
    },
    abandon() {
      clearAccountSecurityState()
    },
    start(input: AccountSecurityUpdateInput) {
      try {
        const identity = invitationIdentity()
        if (owner || accountSecurityIntent || personalIntent || pending)
          throw new AccountFailure('busy')
        if (!accountSecuritySingleton || !sameIdentity(accountSecuritySingleton.identity, identity))
          throw new AccountFailure('invalid-input')
        const captured = captureAccountSecurityUpdate(input)
        const original: SystemAccountSecurityIntent = Object.freeze({
          input: captured,
          body: JSON.stringify(captured),
          singletonID: accountSecuritySingleton.id,
          identity,
          csrf: sessionCSRF,
          key: newKey(),
        })
        accountSecurityIntent = original
        accountSecurityUncertain = accountSecurityChecked = accountSecurityKeyConflict = false
        return performAccountSecurity(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal() {
      const original = accountSecurityIntent
      if (!original || !accountSecurityUncertain || owner) throw new AccountFailure('invalid-input')
      accountSecurityChecked = false
      await restore()
      if (accountSecurityIntent !== original || !invitationContext(original))
        throw new AccountFailure('cancelled')
      accountSecurityChecked = true
      // Account exposes no command lookup. This GET is only a current observation.
      try {
        const value = await readAccountSecurity()
        if (
          accountSecurityIntent === original &&
          invitationContext(original) &&
          accountSecurityState.progress
        )
          accountSecurityState.progress = Object.freeze({
            ...accountSecurityState.progress,
            observation: 'current',
          })
        return value
      } catch (error) {
        if (
          accountSecurityIntent === original &&
          invitationContext(original) &&
          accountSecurityState.progress
        )
          accountSecurityState.progress = Object.freeze({
            ...accountSecurityState.progress,
            observation: 'failed',
          })
        throw error
      }
    },
    retryOriginal() {
      if (!accountSecurityIntent || !accountSecurity.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performAccountSecurity(accountSecurityIntent)
    },
  }
  const system = {
    accountSecurity,
    selection,
    models,
    providers,
    get denied() {
      return systemDenied()
    },
    get invitationProgress(): SystemInvitationProgress | null {
      const progress = invitationState.progress
      if (!progress) return null
      const contextValid = !!invitationIntent && invitationContext(invitationIntent)
      return Object.freeze({
        ...progress,
        contextValid,
        canRetryOriginal:
          progress.phase === 'uncertain' && contextValid && invitationChecked && !state.busy,
      })
    },
    listInvitations(query: InvitationQuery) {
      try {
        const identity = invitationIdentity()
        if (!query || typeof query !== 'object' || Array.isArray(query))
          throw new AccountFailure('invalid-input')
        const captured = Object.freeze({ ...query })
        return runAuthorized(
          identity,
          (op) => invitationAPI.listInvitations(captured, op.abort.signal),
          undefined,
          'invitation-read',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    createInvitation(input: InvitationCreate) {
      return startInvitation({ kind: 'create', input })
    },
    resendInvitation(input: InvitationTarget) {
      return startInvitation({ kind: 'resend', input })
    },
    revokeInvitation(input: InvitationTarget) {
      return startInvitation({ kind: 'revoke', input })
    },
    retryDelivery(input: DeliveryTarget) {
      return startInvitation({ kind: 'retry', input })
    },
    retryInvitationOriginal() {
      if (
        !invitationIntent ||
        !invitationUncertain ||
        !invitationChecked ||
        !invitationContext(invitationIntent) ||
        owner
      )
        return Promise.reject(new AccountFailure('invalid-input'))
      return performInvitation(invitationIntent)
    },
    abandonInvitationRead() {
      ++invitationReadRevision
      if (owner?.kind === 'invitation-read') owner.abandon?.()
    },
    abandonInvitations() {
      clearInvitationState()
    },
    listUsers(query: SystemUserQuery) {
      try {
        const identity = personalIdentity()
        if (
          state.user?.role !== 'admin' ||
          systemDenied() ||
          !query ||
          typeof query !== 'object' ||
          Array.isArray(query)
        )
          throw new AccountFailure('invalid-input')
        const input = Object.freeze({ ...query })
        return runAuthorized(
          identity,
          (op) => systemAPI.listUsers(input, op.abort.signal),
          undefined,
          'system',
        )
      } catch (e) {
        return Promise.reject(e)
      }
    },
    abandon() {
      ++systemRevision
      if (owner?.kind === 'system') owner.abandon?.()
    },
  }
  const personal = {
    get passwordProgress() {
      return readonly(personalState).passwordProgress
    },
    getProfile(): Promise<ProfileView> {
      return readPersonal(async (identity, op, current) => {
        const value = await api.getProfile(op.abort.signal)
        if (current()) acceptUser(identity, value.user)
        return value
      })
    },
    getPreferences(): Promise<PreferencesView> {
      return readPersonal(async (identity, op, current) => {
        const value = await api.getPreferences(op.abort.signal)
        if (current() && trustedUser) acceptUser(identity, { ...trustedUser, ...value })
        return value
      })
    },
    readAvatar(): Promise<AvatarDownload> {
      return readPersonal((_identity, op) => api.readAvatar(op.abort.signal))
    },
    async updateProfile(input: ProfileInput): Promise<ProfileView> {
      const result = await startPersonal({ kind: 'profile', input: Object.freeze({ ...input }) })
      if (result.kind !== 'profile') throw new AccountFailure('invalid-response')
      return result.value
    },
    async setPreferences(input: PreferencesView): Promise<PreferencesView> {
      const result = await startPersonal({
        kind: 'preferences',
        input: Object.freeze({ ...input }),
      })
      if (result.kind !== 'preferences') throw new AccountFailure('invalid-response')
      return result.value
    },
    async putAvatar(
      input: Readonly<{ version: Version; file: File; mediaType: AvatarMedia }>,
    ): Promise<ProfileView> {
      const result = await startPersonal({ kind: 'avatar-put', input: Object.freeze({ ...input }) })
      if (result.kind !== 'avatar-put') throw new AccountFailure('invalid-response')
      return result.value
    },
    async deleteAvatar(input: Readonly<{ version: Version }>): Promise<void> {
      const result = await startPersonal({
        kind: 'avatar-delete',
        input: Object.freeze({ ...input }),
      })
      if (result.kind !== 'avatar-delete') throw new AccountFailure('invalid-response')
    },
    async changePassword(input: PasswordInput): Promise<PasswordConfirmation> {
      const result = await startPersonal({ kind: 'password', input: Object.freeze({ ...input }) })
      if (result.kind !== 'password') throw new AccountFailure('invalid-response')
      return result.value
    },
    retryOriginal(): Promise<PersonalMutationResult> {
      try {
        const identity = personalIdentity()
        if (
          !personalIntent ||
          !personalIntent.checked ||
          !sameIdentity(identity, personalIntent.identity) ||
          personalIntent.csrf !== sessionCSRF
        )
          throw new AccountFailure('invalid-input')
        return performPersonal(personalIntent)
      } catch (e) {
        return Promise.reject(e)
      }
    },
    abandon() {
      ++personalRevision
      personalIntent = null
      personalState.passwordProgress = null
      if (owner?.kind === 'personal') owner.abandon?.()
    },
    previewTheme(identity: PersonalIdentity, value: Theme) {
      if (
        personalContext.phase !== 'current' ||
        !sameIdentity(identity, personalContext.identity) ||
        !['system', 'light', 'dark'].includes(value)
      )
        return
      preview = { identity, theme: value }
      applyTheme()
    },
    clearThemePreview(identity: PersonalIdentity) {
      if (!sameIdentity(identity, preview?.identity ?? null)) return
      preview = null
      applyTheme()
    },
  }
  function entrySession(): EntrySession {
    return state.phase === 'authenticated' && personalContext.identity
      ? { kind: 'authenticated', identity: personalContext.identity }
      : { kind: 'anonymous' }
  }
  function entryContext(original: EntryCommand) {
    return (
      !!anonymousCSRF &&
      original.browser === browser &&
      original.csrf === anonymousCSRF &&
      (original.kind !== 'invitation' ||
        (original.identity === null
          ? personalContext.identity === null
          : sameIdentity(original.identity, personalContext.identity)))
    )
  }
  function entryProgress(original: EntryCommand, phase: EntryProgress['phase']) {
    entryState.progress = {
      kind: original.kind,
      phase,
      canRetryOriginal: phase === 'uncertain' && !owner && entryContext(original),
      contextValid: entryContext(original),
    }
  }
  function entryFailure(error: unknown, current: boolean) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (current) {
      if (e.problem?.code === 'CSRF_FAILED') clearBrowser()
      if (unavailableSession(e)) {
        clearIdentity()
        state.phase = 'unavailable'
      }
    }
    return e
  }
  function assertEntryAvailable() {
    if (
      owner ||
      pending ||
      intent ||
      expectedSession ||
      personalIntent ||
      passwordSessionCheck ||
      state.challengeBusy
    )
      throw new AccountFailure('busy')
  }
  function runEntry<T>(
    work: (op: Operation, current: () => boolean) => Promise<T>,
    original?: EntryCommand,
  ): Promise<T> {
    try {
      assertEntryAvailable()
    } catch (e) {
      return Promise.reject(e)
    }
    const revision = entryRevision
    const op: Operation = {
      kind: 'entry',
      generation: ++generation,
      abort: new AbortController(),
      expired: false,
      confirmed: false,
      visible: Promise.resolve(),
    }
    const current = () => valid(op) && revision === entryRevision
    owner = op
    state.busy = true
    if (original) entryProgress(original, 'submitting')
    let resolveVisible!: (value: T) => void, rejectVisible!: (e: AccountFailure) => void
    const visible = new Promise<T>((resolve, reject) => {
      resolveVisible = resolve
      rejectVisible = reject
    })
    op.visible = visible.then(
      () => undefined,
      () => undefined,
    )
    const rejected = (error: unknown) => {
      const e = entryFailure(error, current())
      if (original && entryIntent === original && current()) {
        if (original.uncertain || isUnknown(e)) {
          original.uncertain = true
          entryProgress(original, 'uncertain')
        } else {
          entryIntent = null
          entryState.progress = null
        }
      }
      if (original?.kind === 'reset-complete' && op.confirmed) {
        // A confirmed command does not confirm the previous Session after a
        // failed or expired follow-up GET, including after page abandonment.
        if (valid(op) && !op.sessionReconciled) {
          clearIdentity(false)
          state.phase = 'unavailable'
          state.notice = '密码重置已确认，当前会话尚未确认。请检查会话，不要再次重置。'
        }
        if (current()) entryProgress(original, 'session-unconfirmed')
        resolveVisible({
          kind: 'reset-complete',
          value: { commandConfirmed: true, session: { kind: 'unconfirmed' }, failure: e },
        } as T)
      } else rejectVisible(e)
    }
    op.abandon = () => {
      rejectVisible(new AccountFailure('cancelled'))
      // A successful reset still reconciles the Cookie under this original owner.
      if (original?.kind !== 'reset-complete') op.abort.abort()
    }
    const timer = setTimeout(() => {
      rejected(new AccountFailure('cancelled'))
      op.expired = true
      op.abort.abort()
    }, 30_000)
    const actual = Promise.resolve()
      .then(async () => {
        if (!current()) throw new AccountFailure('cancelled')
        const value = await work(op, current)
        if (!current()) throw new AccountFailure('cancelled')
        return value
      })
      .finally(() => {
        clearTimeout(timer)
        op.abandon = undefined
        if (owner === op) {
          owner = null
          state.busy = false
          if (entryIntent === original && original?.uncertain) entryProgress(original, 'uncertain')
        }
      })
    // Failure is processed before releasing the actual lane; the final retry flag
    // is refreshed after this handler, without allowing another request to overlap.
    void actual.then(resolveVisible, (error) => {
      rejected(error)
      if (entryIntent === original && original?.uncertain && revision === entryRevision)
        entryProgress(original, 'uncertain')
    })
    return visible
  }
  function prepareEntry(): Promise<EntryPreparation> {
    if (entryIntent) return Promise.reject(new AccountFailure('busy'))
    return runEntry(async (op, current) => {
      try {
        const view = await api.getSession(op.abort.signal)
        if (!current()) throw new AccountFailure('cancelled')
        publish(view, true)
      } catch (e) {
        if (!current()) throw new AccountFailure('cancelled')
        if (!unavailableSession(e)) {
          clearIdentity(false)
          state.phase = 'unavailable'
          throw e
        }
        clearIdentity()
        state.phase = 'anonymous'
      }
      if (!anonymousCSRF || !deliveryChannel) {
        const result = await api.bootstrap(op.abort.signal)
        if (!current()) throw new AccountFailure('cancelled')
        ++browser
        anonymousCSRF = result.csrf_token
        deliveryChannel = result.delivery_channel
      }
      return { deliveryChannel, session: entrySession() }
    })
  }
  function readEntry<T>(work: (signal: AbortSignal, csrfToken: string) => Promise<T>): Promise<T> {
    if (entryIntent) return Promise.reject(new AccountFailure('busy'))
    if (!anonymousCSRF) return Promise.reject(new AccountFailure('invalid-input'))
    const csrfToken = anonymousCSRF
    return runEntry((op) => work(op.abort.signal, csrfToken))
  }
  function performEntry(original: EntryCommand): Promise<EntryMutationResult> {
    return runEntry(async (op, current) => {
      const options = { key: original.key, csrfToken: original.csrf, signal: op.abort.signal }
      let result: EntryMutationResult
      if (!original.input) throw new AccountFailure('invalid-input')
      switch (original.kind) {
        case 'invitation':
          result = {
            kind: 'invitation',
            value: await api.redeemInvitation(original.input, options),
          }
          break
        case 'reset-request':
          result = {
            kind: 'reset-request',
            value: await api.requestPasswordReset(original.input, options),
          }
          break
        case 'reset-complete': {
          await api.completePasswordReset(original.input, options)
          if (!valid(op)) throw new AccountFailure('cancelled')
          // 204 is the command fact. Release secrets before starting the GET.
          op.confirmed = true
          original.input = null
          if (entryIntent === original) entryIntent = null
          if (current()) entryProgress(original, 'confirming-session')
          clearIdentity(false)
          state.phase = 'checking'
          let value: ResetConfirmation
          try {
            if (op.abort.signal.aborted) throw new AccountFailure('cancelled')
            const view = await api.getSession(op.abort.signal)
            if (!valid(op)) throw new AccountFailure('cancelled')
            publish(view, true)
            value = { commandConfirmed: true, session: entrySession() }
          } catch (error) {
            if (valid(op) && unavailableSession(error)) {
              clearIdentity()
              state.phase = 'anonymous'
              value = { commandConfirmed: true, session: { kind: 'anonymous' } }
            } else {
              const e = entryFailure(error, valid(op))
              if (valid(op)) {
                clearIdentity(false)
                state.phase = 'unavailable'
                state.notice = '密码重置已确认，当前会话尚未确认。请检查会话，不要再次重置。'
              }
              value = { commandConfirmed: true, session: { kind: 'unconfirmed' }, failure: e }
            }
          }
          if (current())
            entryProgress(
              original,
              value.session.kind === 'unconfirmed' ? 'session-unconfirmed' : 'confirmed',
            )
          op.sessionReconciled = value.session.kind !== 'unconfirmed'
          return { kind: 'reset-complete', value }
        }
      }
      if (!current()) throw new AccountFailure('cancelled')
      original.input = null
      if (entryIntent === original) entryIntent = null
      entryProgress(original, 'confirmed')
      return result
    }, original)
  }
  function startEntry(input: EntryInput): Promise<EntryMutationResult> {
    try {
      assertEntryAvailable()
      if (entryIntent) throw new AccountFailure('busy')
      if (!anonymousCSRF) throw new AccountFailure('invalid-input')
      const original = {
        ...input,
        key: newKey(),
        csrf: anonymousCSRF,
        browser,
        identity: personalContext.identity,
        uncertain: false,
      } as EntryCommand
      entryIntent = original
      return performEntry(original)
    } catch (e) {
      return Promise.reject(e)
    }
  }
  const entry = {
    get progress() {
      return readonly(entryState).progress
    },
    prepare: prepareEntry,
    inspectInvitation(input: LinkInput): Promise<InvitationInspection> {
      const captured = Object.freeze({ ...input })
      return readEntry((signal, csrfToken) =>
        api.inspectInvitation(captured, { signal, csrfToken }),
      )
    },
    inspectPasswordReset(input: LinkInput): Promise<ResetInspection> {
      const captured = Object.freeze({ ...input })
      return readEntry((signal, csrfToken) =>
        api.inspectPasswordReset(captured, { signal, csrfToken }),
      )
    },
    async redeemInvitation(input: InvitationInput): Promise<InvitationRedeemed> {
      const result = await startEntry({ kind: 'invitation', input: Object.freeze({ ...input }) })
      if (result.kind !== 'invitation') throw new AccountFailure('invalid-response')
      return result.value
    },
    async requestPasswordReset(input: Readonly<{ email: string }>): Promise<ResetAccepted> {
      const result = await startEntry({ kind: 'reset-request', input: Object.freeze({ ...input }) })
      if (result.kind !== 'reset-request') throw new AccountFailure('invalid-response')
      return result.value
    },
    async completePasswordReset(input: ResetInput): Promise<ResetConfirmation> {
      const result = await startEntry({
        kind: 'reset-complete',
        input: Object.freeze({ ...input }),
      })
      if (result.kind !== 'reset-complete') throw new AccountFailure('invalid-response')
      return result.value
    },
    retryOriginal(): Promise<EntryMutationResult> {
      try {
        assertEntryAvailable()
        if (!entryIntent || !entryIntent.uncertain || !entryContext(entryIntent))
          throw new AccountFailure('invalid-input')
        return performEntry(entryIntent)
      } catch (e) {
        return Promise.reject(e)
      }
    },
    abandon() {
      ++entryRevision
      entryIntent = null
      entryState.progress = null
      if (owner?.kind === 'entry') owner.abandon?.()
    },
  }
  function leave() {
    entry.abandon()
    owner?.abandon?.()
    ++generation
    forgetIntent()
    expectedSession = null
    clearBrowser()
    clearIdentity()
    state.phase = 'checking'
    state.notice = ''
    state.fields = {}
    // The Cookie operation's private captured input remains owned until actual return.
    // No abort, queue release or automatic opposite mutation occurs on unmount.
  }
  function restart() {
    if (owner) return Promise.resolve()
    leave()
    return restore()
  }
  function dismissChallenge() {
    if (owner || pending) return
    clearChallenge()
    state.notice = '验证尚未完成。'
  }
  return {
    state: readonly(state),
    personalContext: readonly(personalContext),
    personal,
    system,
    entry,
    restore,
    login,
    logout,
    retryOriginal,
    inputChanged,
    createChallenge,
    verifyChallenge,
    dismissChallenge,
    leave,
    restart,
  }
}

export type SessionController = ReturnType<typeof createSessionController>
let instance: SessionController | undefined
export function useSession() {
  return (instance ??= createSessionController())
}
