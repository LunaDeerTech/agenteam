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
import { AccountFailure, uuid7, type Problem } from '../api/client'
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
  captureMeetingSummaryCommand,
  type MeetingSummaryCommand,
} from '../api/system-meeting-summary'
import {
  createSystemAccountSecurityAPI,
  captureAccountSecurityUpdate,
  type SystemAccountSecurityAPI,
  type AccountSecuritySettings,
  type AccountSecurityUpdateInput,
} from '../api/system-account-security'

import {
  createSystemSMTPSettingsAPI,
  captureSMTPUpdate,
  captureSMTPUnconfigure,
  validateSMTPPassword,
  type SystemSMTPSettingsAPI,
  type SMTPSettings,
  type SMTPUpdateInput,
  type SMTPUnconfigureInput,
  type SMTPResult,
} from '../api/system-smtp-settings'
import {
  createSystemSMTPDeliveryAPI,
  captureSMTPDeliveryCommand,
  type SystemSMTPDeliveryAPI,
  type SMTPDeliveryCommand,
  type SMTPDeliveryResult,
  type SMTPDeliveryQuery,
  type SMTPDeliveryPage,
  type SMTPDeliveryJob,
  type SMTPTestInput,
  type SMTPRetryInput,
} from '../api/system-smtp-delivery'

import {
  createSystemOutboundPolicyAPI,
  captureOutboundPolicyUpdate,
  type SystemOutboundPolicyAPI,
  type OutboundPolicy,
  type OutboundPolicyUpdate,
  type OutboundPolicyReceipt,
} from '../api/system-outbound-policy'

import {
  createSystemAuditAPI,
  captureAuditQuery,
  type SystemAuditAPI,
  type AuditQuery,
} from '../api/system-audit'
import {
  createSystemRuntimeInformationAPI,
  type SystemRuntimeInformationAPI,
} from '../api/system-runtime-information'

import {
  createProjectOwnerAPI,
  captureProjectUpdate,
  type ProjectOwnerAPI,
  type Project,
  type ProjectQuery,
  type ProjectAddress,
  type ProjectUpdate,
  type ProjectLookup,
} from '../api/project-owner'
import {
  createProjectAuditAPI,
  captureProjectAuditID,
  type ProjectAuditAPI,
} from '../api/project-audit'
import {
  createProjectModelSettingsAPI,
  captureProjectConfigurationCommand,
  captureProjectModelContext,
  captureProjectModelID,
  captureProjectModelInput,
  captureProjectModelQuery,
  projectConfigurationBody,
  parseProjectConfigurationReceipt,
  parseProjectConfigurationObservation,
  type ProjectModelSettingsAPI,
  type ProjectConfigurationCommand,
  type ProjectConfigurationReceipt,
  type ProjectConfigurationObservation,
  type ProjectModelPageQuery,
  type ProjectProviderWriteInput,
  type ProjectModelWriteInput,
  type ProjectModelWriteContext,
  type ProjectModelWriteTarget,
} from '../api/project-models'
import {
  captureProjectCredentialTarget,
  captureProjectCredentialValue,
  parseProjectCredentialMutation,
  parseProjectCredentialObservation,
  type ProjectCredentialLookupTarget,
  type ProjectCredentialMutation,
  type ProjectCredentialCreated,
  type ProjectCredentialUpdated,
  type ProjectCredentialDeleted,
  type ProjectCredentialObservation,
} from '../api/project-model-credentials'
import { shape } from '../api/client'
import {
  createKnowledgeOwnerAPI,
  captureKnowledgeID,
  captureKnowledgeQuery,
  captureKnowledgeReadRequest,
  type KnowledgeOwnerAPI,
  type KnowledgeQuery,
  type KnowledgeReadRequest,
} from '../api/knowledge-owner'
const projectModelReadActions = [
  'project-model-provider-list',
  'project-model-provider-get',
  'project-model-list',
  'project-model-get',
  'project-model-available-list',
  'project-model-credential-get',
] as const
const projectModelWriteActions = [
  'project-model-provider-create',
  'project-model-provider-update',
  'project-model-provider-delete',
  'project-model-create',
  'project-model-update',
  'project-model-delete',
  'project-model-credential-create',
  'project-model-credential-update',
  'project-model-credential-delete',
] as const
const projectModelLookupActions = [
  'project-model-configuration-lookup',
  'project-model-credential-lookup',
] as const
type ProjectModelReadAction = (typeof projectModelReadActions)[number]
type ProjectModelWriteAction = (typeof projectModelWriteActions)[number]
type ProjectModelLookupAction = (typeof projectModelLookupActions)[number]
type ProjectModelAction =
  ProjectModelReadAction | ProjectModelWriteAction | ProjectModelLookupAction
type ProjectModelPrivateCommand =
  | Readonly<{ domain: 'configuration'; command: ProjectConfigurationCommand }>
  | Readonly<{
      domain: 'credential'
      command: ProjectCredentialLookupTarget
      material: string | null
    }>
type ProjectModelExecution = ProjectConfigurationReceipt | ProjectCredentialMutation
type ProjectModelObservation = ProjectConfigurationObservation | ProjectCredentialObservation
type ProjectModelIntent = {
  projectID: string
  identity: PersonalIdentity
  csrf: string
  key: string
  payload: { command: ProjectModelPrivateCommand | null; body: string | null }
  uncertain: boolean
  keyConflict: boolean
}
export type ProjectModelSettingsProgress = Readonly<{
  projectID: string
  domain: 'configuration' | 'credential'
  kind: ProjectConfigurationCommand['kind'] | ProjectCredentialLookupTarget['kind']
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  receipt: ProjectModelExecution | null
  observation: 'none' | 'not_observed' | 'observed' | 'failed'
  observedResult: ProjectModelObservation | null
  lookingUp: boolean
  keyConflict: boolean
  contextValid: boolean
  canRetryOriginal: boolean
}>
type ProjectAction = 'project-read' | 'project-update' | 'project-lookup'
type ProjectAuditAction = 'project-audit-list' | 'project-audit-get'
export type ProjectProgress = Readonly<{
  targetID: string
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  receipt: Project | null
  observation: 'none' | 'committed' | 'in_progress' | 'not_observed' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type ProjectIntent = {
  targetID: string
  identity: PersonalIdentity
  csrf: string
  key: string
  payload: { input: ProjectUpdate | null; body: string | null }
  uncertain: boolean
  keyConflict: boolean
}

type OutboundPolicyAction = 'outbound-policy-read' | 'outbound-policy-write'
export type SystemOutboundPolicyProgress = Readonly<{
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  receipt: OutboundPolicyReceipt | null
  observation: 'none' | 'current' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type SystemOutboundPolicyIntent = Readonly<{
  payload: { input: OutboundPolicyUpdate | null; body: string | null }
  identity: PersonalIdentity
  csrf: string
  key: string
}>

type SMTPDeliveryAction = 'smtp-delivery-read' | 'smtp-delivery-write'
export type SystemSMTPDeliveryProgress = Readonly<{
  kind: SMTPDeliveryCommand['kind']
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  result: SMTPDeliveryResult | null
  observation: 'none' | 'current' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
export type SMTPDeliveryObservation =
  | Readonly<{ kind: 'list'; value: SMTPDeliveryPage }>
  | Readonly<{ kind: 'detail'; value: SMTPDeliveryJob }>
type SystemSMTPDeliveryIntent = Readonly<{
  kind: SMTPDeliveryCommand['kind']
  payload: { command: SMTPDeliveryCommand | null; body: string | null }
  identity: PersonalIdentity
  csrf: string
  key: string
}>

type SMTPAction = 'smtp-settings-read' | 'smtp-settings-write'
export type SystemSMTPKind = 'configure' | 'policy' | 'unconfigure'
export type SystemSMTPProgress = Readonly<{
  kind: SystemSMTPKind
  phase: 'submitting' | 'uncertain' | 'rejected' | 'confirmed'
  result: SMTPResult | null
  observation: 'none' | 'current' | 'failed'
  contextValid: boolean
  canRetryOriginal: boolean
}>
type SystemSMTPIntent = Readonly<{
  kind: SystemSMTPKind
  payload: { input: SMTPUpdateInput | SMTPUnconfigureInput | null; body: string | null }
  singletonID: string
  identity: PersonalIdentity
  csrf: string
  key: string
}>

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
export type MeetingSummaryRead =
  | 'meeting-summary-state'
  | 'meeting-summary-reference'
  | 'meeting-summary-providers'
  | 'meeting-summary-provider'
  | 'meeting-summary-models'
type MeetingSummaryAction = MeetingSummaryRead | 'meeting-summary-write' | 'meeting-summary-lookup'
type PlatformSelectionAction = SystemModelSelectionRead | 'selection-write' | 'selection-lookup'
type SelectionAction = PlatformSelectionAction | MeetingSummaryAction
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
type MeetingSummaryIntent = Readonly<{
  command: MeetingSummaryCommand
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
  | 'audit-read'
  | 'knowledge-read'
  | 'runtime-information-read'
  | 'invitation-read'
  | 'invitation-write'
  | ProviderAction
  | ModelAction
  | SelectionAction
  | AccountSecurityAction
  | SMTPAction
  | SMTPDeliveryAction
  | OutboundPolicyAction
  | ProjectAction
  | ProjectAuditAction
  | ProjectModelAction
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
  smtpAPI: SystemSMTPSettingsAPI = createSystemSMTPSettingsAPI(),
  smtpDeliveryAPI: SystemSMTPDeliveryAPI = createSystemSMTPDeliveryAPI(),
  outboundAPI: SystemOutboundPolicyAPI = createSystemOutboundPolicyAPI(),
  auditAPI: SystemAuditAPI = createSystemAuditAPI(),
  runtimeInformationAPI: SystemRuntimeInformationAPI = createSystemRuntimeInformationAPI(),
  projectAPI: ProjectOwnerAPI = createProjectOwnerAPI(),
  projectAuditAPI: ProjectAuditAPI = createProjectAuditAPI(),
  projectModelSettingsAPI: ProjectModelSettingsAPI = createProjectModelSettingsAPI(),
  knowledgeAPI: KnowledgeOwnerAPI = createKnowledgeOwnerAPI(),
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
  let systemRevision = 0,
    auditRevision = 0,
    projectAuditRevision = 0,
    knowledgeRevision = 0,
    runtimeInformationRevision = 0
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
  const projectRevisions: Record<ProjectAction, number> = {
    'project-read': 0,
    'project-update': 0,
    'project-lookup': 0,
  }
  const isProjectAction = (kind: Action): kind is ProjectAction =>
    Object.hasOwn(projectRevisions, kind)
  let projectIntent: ProjectIntent | null = null
  const projectState = shallowReactive<{
    progress: Omit<ProjectProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const projectModelRevisions = Object.fromEntries(
    [...projectModelReadActions, ...projectModelWriteActions, ...projectModelLookupActions].map(
      (kind) => [kind, 0],
    ),
  ) as Record<ProjectModelAction, number>
  const isProjectModelAction = (kind: Action): kind is ProjectModelAction =>
    Object.hasOwn(projectModelRevisions, kind)
  const isProjectModelRead = (kind: Action): kind is ProjectModelReadAction =>
    (projectModelReadActions as readonly string[]).includes(kind)
  let projectModelIntent: ProjectModelIntent | null = null
  const projectModelState = shallowReactive<{
    progress: Omit<ProjectModelSettingsProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const outboundRevisions: Record<OutboundPolicyAction, number> = {
    'outbound-policy-read': 0,
    'outbound-policy-write': 0,
  }
  const isOutboundPolicyAction = (kind: Action): kind is OutboundPolicyAction =>
    Object.hasOwn(outboundRevisions, kind)
  let outboundIntent: SystemOutboundPolicyIntent | null = null,
    outboundUncertain = false,
    outboundChecked = false,
    outboundKeyConflict = false
  const outboundState = shallowReactive<{
    progress: Omit<SystemOutboundPolicyProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
  const smtpDeliveryRevisions: Record<SMTPDeliveryAction, number> = {
    'smtp-delivery-read': 0,
    'smtp-delivery-write': 0,
  }
  const isSMTPDeliveryAction = (kind: Action): kind is SMTPDeliveryAction =>
    Object.hasOwn(smtpDeliveryRevisions, kind)
  let smtpDeliveryIntent: SystemSMTPDeliveryIntent | null = null,
    smtpDeliveryUncertain = false,
    smtpDeliveryChecked = false,
    smtpDeliveryKeyConflict = false
  const smtpDeliveryState = shallowReactive<{
    progress: Omit<SystemSMTPDeliveryProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
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
    'meeting-summary-state': 0,
    'meeting-summary-reference': 0,
    'meeting-summary-providers': 0,
    'meeting-summary-provider': 0,
    'meeting-summary-models': 0,
    'meeting-summary-write': 0,
    'meeting-summary-lookup': 0,
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
  const isPlatformSelectionAction = (kind: Action): kind is PlatformSelectionAction =>
    isSelectionAction(kind) && kind.startsWith('selection-')
  const isMeetingSummaryAction = (kind: Action): kind is MeetingSummaryAction =>
    isSelectionAction(kind) && kind.startsWith('meeting-summary-')
  let meetingSummaryIntent: MeetingSummaryIntent | null = null,
    meetingSummaryUncertain = false,
    meetingSummaryChecked = false,
    meetingSummaryKeyConflict = false
  const meetingSummaryState = shallowReactive<{
    progress: Omit<SystemModelSelectionProgress, 'contextValid' | 'canRetryOriginal'> | null
  }>({ progress: null })
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
  const smtpRevisions: Record<SMTPAction, number> = {
    'smtp-settings-read': 0,
    'smtp-settings-write': 0,
  }
  const isSMTPAction = (kind: Action): kind is SMTPAction => Object.hasOwn(smtpRevisions, kind)
  let smtpIntent: SystemSMTPIntent | null = null,
    smtpSingleton: Readonly<{ identity: PersonalIdentity; settings: SMTPSettings }> | null = null,
    smtpMaterial: string | null = null,
    smtpUncertain = false,
    smtpChecked = false,
    smtpKeyConflict = false
  const smtpState = shallowReactive<{
    progress: Omit<SystemSMTPProgress, 'contextValid' | 'canRetryOriginal'> | null
    hasMaterial: boolean
    materialInvalid: boolean
    materialRevision: number
  }>({ progress: null, hasMaterial: false, materialInvalid: false, materialRevision: 0 })
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
    clearKnowledgeRead()
    clearProjectAuditRead()
    clearProjectModelReads()
    state.user = null
    state.session = null
    if (invalidate) {
      clearProjectModelState()
      clearProjectState()
      clearInvitationState()
      clearProviderState()
      clearModelState()
      clearSelectionState()
      clearAccountSecurityState(true)
      clearSMTPState(true)
      clearSMTPDeliveryState()
      clearOutboundPolicyState()
      clearAuditRead()
      clearRuntimeInformationRead()
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
      clearProjectModelState()
      clearProjectState()
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
      (selectionIntent && selectionIntent.csrf !== sessionCSRF) ||
      (meetingSummaryIntent && meetingSummaryIntent.csrf !== sessionCSRF)
    )
      clearSelectionState()
    if (
      !same ||
      view.user.role !== 'admin' ||
      (accountSecurityIntent && accountSecurityIntent.csrf !== sessionCSRF)
    )
      clearAccountSecurityState(true)
    if (!same || view.user.role !== 'admin' || (smtpIntent && smtpIntent.csrf !== sessionCSRF))
      clearSMTPState(true)
    if (
      !same ||
      view.user.role !== 'admin' ||
      (smtpDeliveryIntent && smtpDeliveryIntent.csrf !== sessionCSRF)
    )
      clearSMTPDeliveryState()
    if (
      !same ||
      view.user.role !== 'admin' ||
      (outboundIntent && outboundIntent.csrf !== sessionCSRF)
    )
      clearOutboundPolicyState()
    if (!same || view.user.role !== 'admin') clearAuditRead()
    if (!same) clearProjectAuditRead()
    if (!same) clearKnowledgeRead()
    if (!same || view.user.role !== 'admin') clearRuntimeInformationRead()
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
      | 'audit-read'
      | 'knowledge-read'
      | 'runtime-information-read'
      | 'invitation-read'
      | 'invitation-write'
      | ProviderAction
      | ModelAction
      | SelectionAction
      | AccountSecurityAction
      | SMTPAction
      | SMTPDeliveryAction
      | OutboundPolicyAction
      | ProjectAction
      | ProjectAuditAction
      | ProjectModelAction = 'personal',
  ): Promise<T> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    const revisionNow = () =>
      kind === 'knowledge-read'
        ? knowledgeRevision
        : isProjectModelAction(kind)
          ? projectModelRevisions[kind]
          : isProjectAuditAction(kind)
            ? projectAuditRevision
            : isProjectAction(kind)
              ? projectRevisions[kind]
              : kind === 'system'
                ? systemRevision
                : kind === 'audit-read'
                  ? auditRevision
                  : kind === 'runtime-information-read'
                    ? runtimeInformationRevision
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
                                : isSMTPAction(kind)
                                  ? smtpRevisions[kind]
                                  : isSMTPDeliveryAction(kind)
                                    ? smtpDeliveryRevisions[kind]
                                    : isOutboundPolicyAction(kind)
                                      ? outboundRevisions[kind]
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
        (kind === 'knowledge-read' ||
        isProjectAction(kind) ||
        isProjectAuditAction(kind) ||
        isProjectModelAction(kind)
          ? state.phase === 'authenticated' &&
            personalContext.phase === 'current' &&
            state.user?.id === identity.userID &&
            state.session?.id === identity.sessionID
          : state.phase === 'authenticated' && state.user?.role === 'admin' && !systemDenied()))
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
          kind === 'knowledge-read'
            ? knowledgeFailure(current, error)
            : isProjectModelAction(kind)
              ? projectModelFailure(kind, current, error)
              : isProjectAuditAction(kind)
                ? projectAuditFailure(current, error)
                : isProjectAction(kind)
                  ? projectFailure(identity, op, error)
                  : kind !== 'personal'
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
          'meeting-summary-write',
          'meeting-summary-lookup',
          'account-security-write',
          'smtp-settings-write',
          'smtp-delivery-write',
        ].includes(op.kind) &&
          e.problem?.code === 'CSRF_FAILED') ||
        (op.kind === 'outbound-policy-write' &&
          e.problem?.status === 403 &&
          e.problem.code === 'CSRF_FAILED')
      ) {
        clearIdentity()
        clearBrowser()
        state.phase = 'unavailable'
        state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
      } else if (
        current() &&
        ((op.kind !== 'audit-read' && op.kind !== 'runtime-information-read') ||
          e.kind === 'problem') &&
        e.problem?.status === 403 &&
        e.problem.code === 'FORBIDDEN'
      ) {
        clearInvitationState()
        clearProviderState()
        clearModelState()
        clearSelectionState()
        clearAccountSecurityState(true)
        clearSMTPState(true)
        clearSMTPDeliveryState()
        clearOutboundPolicyState()
        clearAuditRead()
        clearRuntimeInformationRead()
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
    clearPlatformSelectionState()
    clearMeetingSummaryState()
  }
  function clearMeetingSummaryState() {
    const retiring = owner && isMeetingSummaryAction(owner.kind) ? owner : null
    for (const scope of Object.keys(selectionRevisions) as SelectionAction[])
      if (scope.startsWith('meeting-summary-')) ++selectionRevisions[scope]
    meetingSummaryIntent = null
    meetingSummaryUncertain = meetingSummaryChecked = meetingSummaryKeyConflict = false
    meetingSummaryState.progress = null
    retiring?.abandon?.()
  }
  function abandonMeetingSummaryRead(scope: MeetingSummaryRead) {
    if (
      !Object.hasOwn(selectionRevisions, scope) ||
      !scope.startsWith('meeting-summary-') ||
      scope === ('meeting-summary-write' as string) ||
      scope === ('meeting-summary-lookup' as string)
    )
      throw new AccountFailure('invalid-input')
    ++selectionRevisions[scope]
    if (owner?.kind === scope) owner.abandon?.()
  }
  function readMeetingSummary<T>(
    scope: MeetingSummaryRead,
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
  function publishMeetingSummary(
    phase: SystemModelSelectionProgress['phase'],
    receipt: SelectionReceipt | null = null,
  ) {
    meetingSummaryState.progress = Object.freeze({ phase, receipt, observation: 'none' })
  }
  function performMeetingSummary(original: MeetingSummaryIntent): Promise<SelectionReceipt> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (meetingSummaryIntent !== original || !invitationContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = selectionRevisions['meeting-summary-write']
    const live = () =>
      meetingSummaryIntent === original &&
      revision === selectionRevisions['meeting-summary-write'] &&
      invitationContext(original)
    let dispatched = false
    meetingSummaryChecked = false
    publishMeetingSummary('submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        dispatched = true
        return selectionAPI.updateMeetingSummary(original.command, {
          csrfToken: original.csrf,
          key: original.key,
          signal: op.abort.signal,
        })
      },
      undefined,
      'meeting-summary-write',
    ).then(
      (receipt) => {
        if (!live()) throw new AccountFailure('cancelled')
        publishMeetingSummary('confirmed', receipt)
        // Synchronous consumers may retire this intent and start a successor.
        if (!live()) throw new AccountFailure('cancelled')
        meetingSummaryIntent = null
        meetingSummaryUncertain = meetingSummaryChecked = meetingSummaryKeyConflict = false
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          meetingSummaryIntent === original &&
          revision === selectionRevisions['meeting-summary-write'] &&
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
          meetingSummaryKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          meetingSummaryUncertain ||= !known || meetingSummaryKeyConflict
          meetingSummaryChecked = false
          publishMeetingSummary(meetingSummaryUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const meetingSummary = {
    get progress(): SystemModelSelectionProgress | null {
      const value = meetingSummaryState.progress
      if (!value) return null
      const contextValid = !!meetingSummaryIntent && invitationContext(meetingSummaryIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!meetingSummaryIntent &&
          meetingSummaryUncertain &&
          meetingSummaryChecked &&
          !meetingSummaryKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    get() {
      return readMeetingSummary('meeting-summary-state', (signal) =>
        selectionAPI.getMeetingSummary(signal),
      )
    },
    getSavedModel(id: string) {
      return readMeetingSummary('meeting-summary-reference', (signal) =>
        selectionAPI.getSavedModel(id, signal),
      )
    },
    async listProviders(query: ProviderQuery) {
      const captured = captureModelRead(query)
      return readMeetingSummary('meeting-summary-providers', (signal) =>
        selectionAPI.listProviders(captured, signal),
      )
    },
    getProvider(id: string) {
      return readMeetingSummary('meeting-summary-provider', (signal) =>
        selectionAPI.getProvider(id, signal),
      )
    },
    async listModels(query: ProviderModelQuery) {
      const captured = captureModelRead(query)
      return readMeetingSummary('meeting-summary-models', (signal) =>
        selectionAPI.listModels(captured, signal),
      )
    },
    abandonRead: abandonMeetingSummaryRead,
    abandon: clearMeetingSummaryState,
    start(command: MeetingSummaryCommand) {
      try {
        const identity = invitationIdentity()
        if (owner || meetingSummaryIntent || personalIntent || pending)
          throw new AccountFailure('busy')
        const captured = captureMeetingSummaryCommand(command)
        const original = Object.freeze({
          command: captured,
          identity,
          csrf: sessionCSRF,
          key: newKey(),
        })
        meetingSummaryIntent = original
        meetingSummaryUncertain = meetingSummaryChecked = meetingSummaryKeyConflict = false
        return performMeetingSummary(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal() {
      const original = meetingSummaryIntent
      if (!original || !meetingSummaryUncertain || owner) throw new AccountFailure('invalid-input')
      meetingSummaryChecked = false
      await restore()
      if (meetingSummaryIntent !== original || !invitationContext(original))
        throw new AccountFailure('cancelled')
      meetingSummaryChecked = true
      // A refreshed identity permits explicit Execute; lookup never confirms its body.
      return runAuthorized(
        original.identity,
        async (op, current) => {
          if (meetingSummaryIntent !== original || !current()) throw new AccountFailure('cancelled')
          const observed = await selectionAPI.lookupMeetingSummaryCommand(original.command, {
            csrfToken: original.csrf,
            key: original.key,
            signal: op.abort.signal,
          })
          if (current() && meetingSummaryIntent === original && meetingSummaryState.progress)
            meetingSummaryState.progress = Object.freeze({
              ...meetingSummaryState.progress,
              observation: observed.found ? 'found' : 'missing',
            })
          return observed
        },
        undefined,
        'meeting-summary-lookup',
      ).catch((error: unknown) => {
        if (
          meetingSummaryIntent === original &&
          invitationContext(original) &&
          meetingSummaryState.progress
        )
          meetingSummaryState.progress = Object.freeze({
            ...meetingSummaryState.progress,
            observation: 'failed',
          })
        throw error
      })
    },
    retryOriginal() {
      if (!meetingSummaryIntent || !meetingSummary.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performMeetingSummary(meetingSummaryIntent)
    },
  }
  function clearPlatformSelectionState() {
    const retiring = owner && isPlatformSelectionAction(owner.kind) ? owner : null
    for (const scope of Object.keys(selectionRevisions) as SelectionAction[])
      if (scope.startsWith('selection-')) ++selectionRevisions[scope]
    selectionIntent = null
    selectionUncertain = selectionChecked = selectionKeyConflict = false
    selectionState.progress = null
    retiring?.abandon?.()
  }
  function abandonSelectionRead(scope: SystemModelSelectionRead) {
    if (
      !Object.hasOwn(selectionRevisions, scope) ||
      !scope.startsWith('selection-') ||
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
    meetingSummary,
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
    abandon: clearPlatformSelectionState,
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
  function clearSMTPMaterial() {
    smtpMaterial = null
    smtpState.hasMaterial = smtpState.materialInvalid = false
    ++smtpState.materialRevision
  }
  function clearSMTPPayload(original: SystemSMTPIntent | null) {
    if (original) {
      original.payload.input = null
      original.payload.body = null
    }
  }
  function clearSMTPState(clearSingleton = false) {
    const retiring = owner && isSMTPAction(owner.kind) ? owner : null
    ++smtpRevisions['smtp-settings-read']
    ++smtpRevisions['smtp-settings-write']
    clearSMTPPayload(smtpIntent)
    smtpIntent = null
    smtpUncertain = smtpChecked = smtpKeyConflict = false
    clearSMTPMaterial()
    smtpState.progress = null
    if (clearSingleton) smtpSingleton = null
    retiring?.abandon?.()
  }
  function captureSMTPObservation(identity: PersonalIdentity, value: SMTPSettings) {
    if (
      smtpSingleton &&
      (!sameIdentity(smtpSingleton.identity, identity) || value.id !== smtpSingleton.settings.id)
    )
      throw new AccountFailure('invalid-response')
    if (!smtpSingleton || BigInt(value.version) >= BigInt(smtpSingleton.settings.version))
      smtpSingleton = Object.freeze({ identity, settings: value })
  }
  function readSMTP(): Promise<SMTPSettings> {
    try {
      const identity = invitationIdentity()
      return runAuthorized(
        identity,
        async (op, current) => {
          const value = await smtpAPI.getSettings(op.abort.signal)
          if (!current()) throw new AccountFailure('cancelled')
          captureSMTPObservation(identity, value)
          return value
        },
        undefined,
        'smtp-settings-read',
      )
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function publishSMTP(
    original: SystemSMTPIntent,
    phase: SystemSMTPProgress['phase'],
    result: SMTPResult | null = null,
  ) {
    smtpState.progress = Object.freeze({ kind: original.kind, phase, result, observation: 'none' })
  }
  function performSMTP(original: SystemSMTPIntent): Promise<SMTPResult> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (
      smtpIntent !== original ||
      !invitationContext(original) ||
      !original.payload.input ||
      !original.payload.body
    )
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = smtpRevisions['smtp-settings-write']
    const live = () =>
      smtpIntent === original &&
      revision === smtpRevisions['smtp-settings-write'] &&
      invitationContext(original)
    let dispatched = false
    smtpChecked = false
    publishSMTP(original, 'submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        const captured = original.payload.input
        if (!captured || JSON.stringify(captured) !== original.payload.body)
          throw new AccountFailure('invalid-input')
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        dispatched = true
        try {
          const value =
            original.kind === 'configure'
              ? await smtpAPI.updateSettings(captured as SMTPUpdateInput, options)
              : await smtpAPI.unconfigure(captured, options)
          if (value.settings.id !== original.singletonID)
            throw new AccountFailure('invalid-response')
          return value
        } finally {
          // Explicit abandonment already removes recovery access; the API owns
          // only the temporary original request through its actual I/O tail.
          if (smtpIntent !== original) clearSMTPPayload(original)
        }
      },
      undefined,
      'smtp-settings-write',
    ).then(
      (value) => {
        if (!live()) throw new AccountFailure('cancelled')
        captureSMTPObservation(original.identity, value.settings)
        // At this point the transport's original EOF/cancel has actually joined.
        clearSMTPPayload(original)
        clearSMTPMaterial()
        if (!live()) throw new AccountFailure('cancelled')
        publishSMTP(original, 'confirmed', value)
        // Synchronous consumers may abandon and create a successor here.
        if (!live()) throw new AccountFailure('cancelled')
        smtpIntent = null
        smtpUncertain = smtpChecked = smtpKeyConflict = false
        return value
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          smtpIntent === original &&
          revision === smtpRevisions['smtp-settings-write'] &&
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
          // Both write endpoints perform a separate authorized current GET after
          // committing. Its 5xx/not_started is not evidence that the write failed.
          smtpKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          smtpUncertain ||= !known || smtpKeyConflict
          smtpChecked = false
          publishSMTP(original, smtpUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  function startSMTP(kind: SystemSMTPKind, input: SMTPUpdateInput | SMTPUnconfigureInput) {
    try {
      const identity = invitationIdentity()
      if (owner || smtpIntent || personalIntent || pending) throw new AccountFailure('busy')
      if (!smtpSingleton || !sameIdentity(smtpSingleton.identity, identity))
        throw new AccountFailure('invalid-input')
      if (!input || typeof input !== 'object' || Object.hasOwn(input, 'password'))
        throw new AccountFailure('invalid-input')
      if (smtpState.materialInvalid) throw new AccountFailure('invalid-input')
      const captured =
        kind === 'configure'
          ? captureSMTPUpdate({
              ...(input as SMTPUpdateInput),
              ...(smtpMaterial === null ? {} : { password: smtpMaterial }),
            })
          : captureSMTPUnconfigure(input)
      if (kind === 'configure' && captured.version === smtpSingleton.settings.version) {
        const update = captured as SMTPUpdateInput
        const credential =
          update.credential_action === 'remove'
            ? false
            : !!update.password || smtpSingleton.settings.credential_present
        if ((update.username !== '') !== credential) throw new AccountFailure('invalid-input')
      }
      const original: SystemSMTPIntent = Object.freeze({
        kind,
        payload: { input: captured, body: JSON.stringify(captured) },
        singletonID: smtpSingleton.settings.id,
        identity,
        csrf: sessionCSRF,
        key: newKey(),
      })
      smtpIntent = original
      smtpMaterial = null
      smtpState.hasMaterial = kind === 'configure' && !!(captured as SMTPUpdateInput).password
      smtpUncertain = smtpChecked = smtpKeyConflict = false
      return performSMTP(original)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  const smtp = {
    get progress(): SystemSMTPProgress | null {
      const value = smtpState.progress
      if (!value) return null
      const contextValid = !!smtpIntent && invitationContext(smtpIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!smtpIntent &&
          !!smtpIntent.payload.input &&
          smtpUncertain &&
          smtpChecked &&
          !smtpKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    get material() {
      return Object.freeze({
        present: smtpState.hasMaterial,
        invalid: smtpState.materialInvalid,
        revision: smtpState.materialRevision,
      })
    },
    setMaterial(value: string) {
      invitationIdentity()
      if (smtpIntent) throw new AccountFailure('busy')
      if (value === '') {
        clearSMTPMaterial()
        return
      }
      try {
        validateSMTPPassword(value)
        smtpMaterial = value
        smtpState.hasMaterial = true
        smtpState.materialInvalid = false
        ++smtpState.materialRevision
      } catch (error) {
        clearSMTPMaterial()
        smtpState.materialInvalid = true
        throw error
      }
    },
    get: readSMTP,
    abandonRead() {
      ++smtpRevisions['smtp-settings-read']
      if (owner?.kind === 'smtp-settings-read') owner.abandon?.()
    },
    abandon() {
      clearSMTPState()
    },
    editRejected() {
      if (
        !smtpIntent ||
        smtpUncertain ||
        smtpState.progress?.phase !== 'rejected' ||
        owner ||
        !invitationContext(smtpIntent)
      )
        throw new AccountFailure('invalid-input')
      smtpMaterial = (smtpIntent.payload.input as SMTPUpdateInput | null)?.password ?? null
      clearSMTPPayload(smtpIntent)
      smtpIntent = null
      smtpState.progress = null
      smtpUncertain = smtpChecked = smtpKeyConflict = false
    },
    startUpdate(input: Omit<SMTPUpdateInput, 'password'>) {
      return startSMTP('configure', input)
    },
    startUnconfigure(input: SMTPUnconfigureInput, kind: 'policy' | 'unconfigure' = 'unconfigure') {
      if (kind !== 'policy' && kind !== 'unconfigure')
        return Promise.reject(new AccountFailure('invalid-input'))
      return startSMTP(kind, input)
    },
    async checkOriginal() {
      const original = smtpIntent
      if (!original || !smtpUncertain || owner) throw new AccountFailure('invalid-input')
      smtpChecked = false
      await restore()
      if (smtpIntent !== original || !invitationContext(original))
        throw new AccountFailure('cancelled')
      smtpChecked = true
      try {
        const value = await readSMTP()
        if (smtpIntent === original && invitationContext(original) && smtpState.progress)
          smtpState.progress = Object.freeze({ ...smtpState.progress, observation: 'current' })
        return value
      } catch (error) {
        if (smtpIntent === original && invitationContext(original) && smtpState.progress)
          smtpState.progress = Object.freeze({ ...smtpState.progress, observation: 'failed' })
        throw error
      }
    },
    retryOriginal() {
      if (!smtpIntent || !smtp.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performSMTP(smtpIntent)
    },
  }
  function clearSMTPDeliveryPayload(original: SystemSMTPDeliveryIntent | null) {
    if (!original) return
    original.payload.command = null
    original.payload.body = null
  }
  function clearSMTPDeliveryState() {
    const retiring = owner && isSMTPDeliveryAction(owner.kind) ? owner : null
    ++smtpDeliveryRevisions['smtp-delivery-read']
    ++smtpDeliveryRevisions['smtp-delivery-write']
    clearSMTPDeliveryPayload(smtpDeliveryIntent)
    smtpDeliveryIntent = null
    smtpDeliveryUncertain = smtpDeliveryChecked = smtpDeliveryKeyConflict = false
    smtpDeliveryState.progress = null
    retiring?.abandon?.()
  }
  function readSMTPDelivery<T>(work: (signal: AbortSignal) => Promise<T>): Promise<T> {
    try {
      return runAuthorized(
        invitationIdentity(),
        (op) => work(op.abort.signal),
        undefined,
        'smtp-delivery-read',
      )
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function smtpDeliveryBody(command: SMTPDeliveryCommand) {
    return JSON.stringify(
      command.kind === 'test' ? command.input : { version: command.input.version },
    )
  }
  function publishSMTPDelivery(
    original: SystemSMTPDeliveryIntent,
    phase: SystemSMTPDeliveryProgress['phase'],
    result: SMTPDeliveryResult | null = null,
  ) {
    smtpDeliveryState.progress = Object.freeze({
      kind: original.kind,
      phase,
      result,
      observation: 'none',
    })
  }
  function performSMTPDelivery(original: SystemSMTPDeliveryIntent): Promise<SMTPDeliveryResult> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (
      smtpDeliveryIntent !== original ||
      !original.payload.command ||
      !original.payload.body ||
      !invitationContext(original)
    )
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = smtpDeliveryRevisions['smtp-delivery-write']
    const live = () =>
      smtpDeliveryIntent === original &&
      revision === smtpDeliveryRevisions['smtp-delivery-write'] &&
      invitationContext(original)
    let dispatched = false
    smtpDeliveryChecked = false
    publishSMTPDelivery(original, 'submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized<SMTPDeliveryResult>(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        const command = original.payload.command
        if (!command || smtpDeliveryBody(command) !== original.payload.body)
          throw new AccountFailure('invalid-input')
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        dispatched = true
        try {
          return command.kind === 'test'
            ? { kind: 'test', value: await smtpDeliveryAPI.testSMTP(command.input, options) }
            : { kind: 'retry', value: await smtpDeliveryAPI.retryJob(command.input, options) }
        } finally {
          if (smtpDeliveryIntent !== original) clearSMTPDeliveryPayload(original)
        }
      },
      undefined,
      'smtp-delivery-write',
    ).then(
      (result) => {
        if (!live()) throw new AccountFailure('cancelled')
        clearSMTPDeliveryPayload(original)
        publishSMTPDelivery(original, 'confirmed', result)
        if (!live()) throw new AccountFailure('cancelled')
        smtpDeliveryIntent = null
        smtpDeliveryUncertain = smtpDeliveryChecked = smtpDeliveryKeyConflict = false
        return result
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          smtpDeliveryIntent === original &&
          revision === smtpDeliveryRevisions['smtp-delivery-write'] &&
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
                'INVALID_STATE',
                'RATE_LIMITED',
                'RESOURCE_DELETED',
              ].includes(p.code))
          // Test and retry both read the current job after a commit or historical
          // match. In particular 404/not_started does not reject that command.
          smtpDeliveryKeyConflict ||= p?.code === 'IDEMPOTENCY_KEY_REUSED'
          smtpDeliveryUncertain ||= !known || smtpDeliveryKeyConflict
          smtpDeliveryChecked = false
          publishSMTPDelivery(original, smtpDeliveryUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  function startSMTPDelivery(command: SMTPDeliveryCommand) {
    try {
      const identity = invitationIdentity()
      if (owner || smtpDeliveryIntent || personalIntent || pending) throw new AccountFailure('busy')
      const captured = captureSMTPDeliveryCommand(command)
      const original: SystemSMTPDeliveryIntent = Object.freeze({
        kind: captured.kind,
        payload: { command: captured, body: smtpDeliveryBody(captured) },
        identity,
        csrf: sessionCSRF,
        key: newKey(),
      })
      smtpDeliveryIntent = original
      smtpDeliveryUncertain = smtpDeliveryChecked = smtpDeliveryKeyConflict = false
      return performSMTPDelivery(original)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  const smtpDelivery = {
    get progress(): SystemSMTPDeliveryProgress | null {
      const value = smtpDeliveryState.progress
      if (!value) return null
      const contextValid = !!smtpDeliveryIntent && invitationContext(smtpDeliveryIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!smtpDeliveryIntent?.payload.command &&
          smtpDeliveryUncertain &&
          smtpDeliveryChecked &&
          !smtpDeliveryKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    list(query: SMTPDeliveryQuery = {}) {
      return readSMTPDelivery((signal) => smtpDeliveryAPI.listJobs(query, signal))
    },
    get(jobID: string) {
      return readSMTPDelivery((signal) => smtpDeliveryAPI.getJob(jobID, signal))
    },
    abandonRead() {
      ++smtpDeliveryRevisions['smtp-delivery-read']
      if (owner?.kind === 'smtp-delivery-read') owner.abandon?.()
    },
    abandon: clearSMTPDeliveryState,
    editRejected() {
      if (
        !smtpDeliveryIntent ||
        smtpDeliveryUncertain ||
        smtpDeliveryState.progress?.phase !== 'rejected' ||
        owner ||
        !invitationContext(smtpDeliveryIntent)
      )
        throw new AccountFailure('invalid-input')
      clearSMTPDeliveryState()
    },
    startTest(input: SMTPTestInput) {
      return startSMTPDelivery({ kind: 'test', input })
    },
    startRetry(input: SMTPRetryInput) {
      return startSMTPDelivery({ kind: 'retry', input })
    },
    async checkOriginal(): Promise<SMTPDeliveryObservation> {
      const original = smtpDeliveryIntent
      if (!original?.payload.command || !smtpDeliveryUncertain || owner)
        throw new AccountFailure('invalid-input')
      smtpDeliveryChecked = false
      await restore()
      if (
        smtpDeliveryIntent !== original ||
        !original.payload.command ||
        !invitationContext(original)
      )
        throw new AccountFailure('cancelled')
      smtpDeliveryChecked = true
      try {
        const command = original.payload.command
        const value: SMTPDeliveryObservation =
          command.kind === 'test'
            ? Object.freeze({ kind: 'list', value: await smtpDelivery.list() })
            : Object.freeze({ kind: 'detail', value: await smtpDelivery.get(command.input.job_id) })
        if (
          smtpDeliveryIntent === original &&
          invitationContext(original) &&
          smtpDeliveryState.progress
        )
          smtpDeliveryState.progress = Object.freeze({
            ...smtpDeliveryState.progress,
            observation: 'current',
          })
        return value
      } catch (error) {
        if (
          smtpDeliveryIntent === original &&
          invitationContext(original) &&
          smtpDeliveryState.progress
        )
          smtpDeliveryState.progress = Object.freeze({
            ...smtpDeliveryState.progress,
            observation: 'failed',
          })
        throw error
      }
    },
    retryOriginal() {
      if (!smtpDeliveryIntent || !smtpDelivery.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performSMTPDelivery(smtpDeliveryIntent)
    },
  }
  function projectContext(original: Pick<ProjectIntent, 'identity' | 'csrf'>) {
    return (
      sameIdentity(original.identity, personalContext.identity) &&
      personalContext.phase === 'current' &&
      state.phase === 'authenticated' &&
      state.user?.id === original.identity.userID &&
      state.session?.id === original.identity.sessionID &&
      !!sessionCSRF &&
      original.csrf === sessionCSRF
    )
  }
  function clearProjectPayload(original: ProjectIntent | null) {
    if (original) {
      original.payload.input = null
      original.payload.body = null
    }
  }
  function clearProjectState() {
    const retiring = owner && isProjectAction(owner.kind) ? owner : null
    for (const key of Object.keys(projectRevisions) as ProjectAction[]) ++projectRevisions[key]
    clearProjectPayload(projectIntent)
    projectIntent = null
    projectState.progress = null
    retiring?.abandon?.()
  }
  function projectFailure(identity: PersonalIdentity, op: Operation, error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    // A local Project denial never changes the independent System admin gate.
    if (
      sameIdentity(identity, personalContext.identity) &&
      generation === op.generation &&
      (unavailableSession(e) || (e.problem?.status === 403 && e.problem.code === 'CSRF_FAILED'))
    ) {
      clearIdentity()
      clearBrowser()
      state.phase = 'unavailable'
      state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
    }
    return e
  }
  function publishProject(
    original: ProjectIntent,
    phase: ProjectProgress['phase'],
    receipt: Project | null = null,
    observation: ProjectProgress['observation'] = 'none',
  ) {
    projectState.progress = Object.freeze({
      targetID: original.targetID,
      phase,
      receipt,
      observation,
    })
  }
  function performProject(
    original: ProjectIntent,
    lookup = false,
  ): Promise<Project | ProjectLookup> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (projectIntent !== original || !original.payload.input || !projectContext(original))
      return Promise.reject(new AccountFailure('invalid-input'))
    const kind: ProjectAction = lookup ? 'project-lookup' : 'project-update'
    const revision = projectRevisions[kind]
    const live = () =>
      projectIntent === original && revision === projectRevisions[kind] && projectContext(original)
    let dispatched = false
    if (!lookup) publishProject(original, 'submitting')
    return runAuthorized<Project | ProjectLookup>(
      original.identity,
      async (op, current) => {
        const input = original.payload.input
        if (!input || !current() || !live()) throw new AccountFailure('cancelled')
        if (JSON.stringify(input) !== original.payload.body)
          throw new AccountFailure('invalid-input')
        dispatched = true
        try {
          const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
          return lookup
            ? await projectAPI.lookup(original.targetID, original.identity.userID, input, options)
            : await projectAPI.update(original.targetID, original.identity.userID, input, options)
        } finally {
          if (projectIntent !== original) clearProjectPayload(original)
        }
      },
      undefined,
      kind,
    ).then(
      (value) => {
        if (!live()) throw new AccountFailure('cancelled')
        if ('state' in value && value.state !== 'committed') {
          publishProject(original, 'uncertain', null, value.state)
          return value
        }
        const receipt = 'state' in value ? value.result.project : value
        publishProject(original, 'confirmed', receipt, lookup ? 'committed' : 'none')
        if (!live()) {
          clearProjectPayload(original)
          throw new AccountFailure('cancelled')
        }
        clearProjectPayload(original)
        projectIntent = null
        return value
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          projectIntent === original &&
          revision === projectRevisions[kind] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          const known =
            !dispatched ||
            (p &&
              (p.commit_state === 'not_started' || p.commit_state === 'not_committed') &&
              ((p.status === 400 && p.code === 'INVALID_ARGUMENT') ||
                (p.status === 409 &&
                  ['VERSION_CONFLICT', 'INVALID_STATE', 'PROJECT_NOT_ACTIVE'].includes(p.code)) ||
                (p.status === 409 &&
                  p.code === 'RESOURCE_BUSY' &&
                  p.field_errors?.length === 1 &&
                  p.field_errors[0]?.path === '/name' &&
                  p.field_errors[0].code === 'NAME_TAKEN')))
          original.keyConflict ||= p?.status === 409 && p.code === 'IDEMPOTENCY_KEY_REUSED'
          original.uncertain ||= lookup || !known || original.keyConflict
          publishProject(
            original,
            original.uncertain ? 'uncertain' : 'rejected',
            null,
            lookup ? 'failed' : 'none',
          )
        }
        throw e
      },
    )
  }
  function projectRead<T>(work: (identity: PersonalIdentity, signal: AbortSignal) => Promise<T>) {
    try {
      const identity = personalIdentity()
      return runAuthorized(
        identity,
        (op) => work(identity, op.abort.signal),
        undefined,
        'project-read',
      )
    } catch (error) {
      return Promise.reject(error)
    }
  }
  const projects = {
    get progress(): ProjectProgress | null {
      const value = projectState.progress
      if (!value) return null
      const contextValid = !!projectIntent && projectContext(projectIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!projectIntent?.payload.input &&
          projectIntent.uncertain &&
          !projectIntent.keyConflict &&
          value.observation !== 'in_progress' &&
          contextValid &&
          !state.busy,
      })
    },
    list(query: ProjectQuery) {
      return projectRead((_identity, signal) => projectAPI.list(query, signal))
    },
    resolve(address: ProjectAddress) {
      return projectRead((identity, signal) => projectAPI.resolve(address, identity.userID, signal))
    },
    get(id: string) {
      return projectRead((identity, signal) => projectAPI.get(id, identity.userID, signal))
    },
    abandonRead() {
      ++projectRevisions['project-read']
      if (owner?.kind === 'project-read') owner.abandon?.()
    },
    abandon: clearProjectState,
    editRejected() {
      if (
        owner ||
        !projectIntent ||
        projectIntent.uncertain ||
        projectState.progress?.phase !== 'rejected' ||
        !projectContext(projectIntent)
      )
        throw new AccountFailure('invalid-input')
      clearProjectState()
    },
    startUpdate(id: string, value: ProjectUpdate) {
      try {
        const identity = personalIdentity()
        if (projectIntent || personalIntent || pending) throw new AccountFailure('busy')
        if (!uuid7.test(id)) throw new AccountFailure('invalid-input')
        const input = captureProjectUpdate(value)
        const original: ProjectIntent = {
          targetID: id,
          identity,
          csrf: sessionCSRF,
          key: newKey(),
          payload: { input, body: JSON.stringify(input) },
          uncertain: false,
          keyConflict: false,
        }
        projectIntent = original
        return performProject(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    checkOriginal() {
      if (!projectIntent?.uncertain || !projectContext(projectIntent))
        return Promise.reject(new AccountFailure('invalid-input'))
      return performProject(projectIntent, true)
    },
    retryOriginal() {
      if (!projectIntent || !projects.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performProject(projectIntent)
    },
  }
  function clearOutboundPolicyPayload(original: SystemOutboundPolicyIntent | null) {
    if (!original) return
    original.payload.input = null
    original.payload.body = null
  }
  function clearOutboundPolicyState() {
    const retiring = owner && isOutboundPolicyAction(owner.kind) ? owner : null
    ++outboundRevisions['outbound-policy-read']
    ++outboundRevisions['outbound-policy-write']
    clearOutboundPolicyPayload(outboundIntent)
    outboundIntent = null
    outboundUncertain = outboundChecked = outboundKeyConflict = false
    outboundState.progress = null
    retiring?.abandon?.()
  }
  function publishOutboundPolicy(
    phase: SystemOutboundPolicyProgress['phase'],
    receipt: OutboundPolicyReceipt | null = null,
  ) {
    outboundState.progress = Object.freeze({ phase, receipt, observation: 'none' })
  }
  function performOutboundPolicy(
    original: SystemOutboundPolicyIntent,
  ): Promise<OutboundPolicyReceipt> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    if (
      outboundIntent !== original ||
      !original.payload.input ||
      !original.payload.body ||
      !invitationContext(original)
    )
      return Promise.reject(new AccountFailure('invalid-input'))
    const revision = outboundRevisions['outbound-policy-write']
    const live = () =>
      outboundIntent === original &&
      revision === outboundRevisions['outbound-policy-write'] &&
      invitationContext(original)
    let dispatched = false
    outboundChecked = false
    publishOutboundPolicy('submitting')
    if (!live()) return Promise.reject(new AccountFailure('cancelled'))
    return runAuthorized<OutboundPolicyReceipt>(
      original.identity,
      async (op, current) => {
        if (!current() || !live()) throw new AccountFailure('cancelled')
        const input = original.payload.input
        if (!input || JSON.stringify(input) !== original.payload.body)
          throw new AccountFailure('invalid-input')
        dispatched = true
        try {
          return await outboundAPI.updatePolicy(input, {
            csrfToken: original.csrf,
            key: original.key,
            signal: op.abort.signal,
          })
        } finally {
          if (outboundIntent !== original) clearOutboundPolicyPayload(original)
        }
      },
      undefined,
      'outbound-policy-write',
    ).then(
      (receipt) => {
        if (!live()) throw new AccountFailure('cancelled')
        clearOutboundPolicyPayload(original)
        publishOutboundPolicy('confirmed', receipt)
        if (!live()) throw new AccountFailure('cancelled')
        outboundIntent = null
        outboundUncertain = outboundChecked = outboundKeyConflict = false
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          outboundIntent === original &&
          revision === outboundRevisions['outbound-policy-write'] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          const p = e.problem
          // Only these exact, uncommitted business rejections can end a first
          // attempt. In particular a 503/not_started can follow a real commit.
          const known =
            !dispatched ||
            (p &&
              (p.commit_state === 'not_started' || p.commit_state === 'not_committed') &&
              ((p.status === 400 && p.code === 'INVALID_ARGUMENT') ||
                (p.status === 404 && p.code === 'NOT_FOUND') ||
                (p.status === 409 &&
                  (p.code === 'VERSION_CONFLICT' || p.code === 'INVALID_STATE'))))
          outboundKeyConflict ||= p?.status === 409 && p.code === 'IDEMPOTENCY_KEY_REUSED'
          outboundUncertain ||= !known || outboundKeyConflict
          outboundChecked = false
          publishOutboundPolicy(outboundUncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  const outboundPolicy = {
    get progress(): SystemOutboundPolicyProgress | null {
      const value = outboundState.progress
      if (!value) return null
      const contextValid = !!outboundIntent && invitationContext(outboundIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!outboundIntent?.payload.input &&
          outboundUncertain &&
          outboundChecked &&
          !outboundKeyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    get(): Promise<OutboundPolicy> {
      try {
        return runAuthorized(
          invitationIdentity(),
          (op) => outboundAPI.getPolicy(op.abort.signal),
          undefined,
          'outbound-policy-read',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    abandonRead() {
      ++outboundRevisions['outbound-policy-read']
      if (owner?.kind === 'outbound-policy-read') owner.abandon?.()
    },
    abandon: clearOutboundPolicyState,
    editRejected() {
      if (
        !outboundIntent ||
        outboundUncertain ||
        outboundState.progress?.phase !== 'rejected' ||
        owner ||
        !invitationContext(outboundIntent)
      )
        throw new AccountFailure('invalid-input')
      clearOutboundPolicyState()
    },
    startUpdate(input: OutboundPolicyUpdate) {
      try {
        const identity = invitationIdentity()
        if (owner || outboundIntent || personalIntent || pending) throw new AccountFailure('busy')
        const captured = captureOutboundPolicyUpdate(input)
        const original: SystemOutboundPolicyIntent = Object.freeze({
          payload: { input: captured, body: JSON.stringify(captured) },
          identity,
          csrf: sessionCSRF,
          key: newKey(),
        })
        outboundIntent = original
        outboundUncertain = outboundChecked = outboundKeyConflict = false
        return performOutboundPolicy(original)
      } catch (error) {
        return Promise.reject(error)
      }
    },
    async checkOriginal(): Promise<OutboundPolicy> {
      const original = outboundIntent
      if (!original?.payload.input || !outboundUncertain || owner)
        throw new AccountFailure('invalid-input')
      outboundChecked = false
      await restore()
      if (outboundIntent !== original || !original.payload.input || !invitationContext(original))
        throw new AccountFailure('cancelled')
      outboundChecked = true
      try {
        const value = await outboundPolicy.get()
        if (outboundIntent === original && invitationContext(original) && outboundState.progress)
          outboundState.progress = Object.freeze({
            ...outboundState.progress,
            observation: 'current',
          })
        return value
      } catch (error) {
        if (outboundIntent === original && invitationContext(original) && outboundState.progress)
          outboundState.progress = Object.freeze({
            ...outboundState.progress,
            observation: 'failed',
          })
        throw error
      }
    },
    retryOriginal() {
      if (!outboundIntent || !outboundPolicy.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performOutboundPolicy(outboundIntent)
    },
  }
  function clearProjectModelPayload(original: ProjectModelIntent | null) {
    if (!original) return
    original.payload.command = null
    original.payload.body = null
  }
  function clearProjectModelReads() {
    for (const kind of projectModelReadActions) ++projectModelRevisions[kind]
    if (owner && isProjectModelRead(owner.kind)) owner.abandon?.()
  }
  function clearProjectModelPending() {
    const retiring =
      owner && isProjectModelAction(owner.kind) && !isProjectModelRead(owner.kind) ? owner : null
    for (const kind of [...projectModelWriteActions, ...projectModelLookupActions])
      ++projectModelRevisions[kind]
    clearProjectModelPayload(projectModelIntent)
    projectModelIntent = null
    projectModelState.progress = null
    retiring?.abandon?.()
  }
  function clearProjectModelState() {
    clearProjectModelReads()
    clearProjectModelPending()
  }
  function projectModelFailure(kind: ProjectModelAction, current: () => boolean, error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (
      current() &&
      (unavailableSession(e) ||
        (!isProjectModelRead(kind) &&
          e.problem?.status === 403 &&
          e.problem.code === 'CSRF_FAILED'))
    ) {
      clearIdentity()
      clearBrowser()
      state.phase = 'unavailable'
      state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
    }
    return e
  }
  function projectModelBody(value: ProjectModelPrivateCommand): string {
    if (value.domain === 'configuration')
      return JSON.stringify(projectConfigurationBody(value.command))
    const command = value.command
    return JSON.stringify(
      command.kind === 'create'
        ? { value: value.material }
        : command.kind === 'update'
          ? { expected_version: command.expected_version, value: value.material }
          : { expected_version: command.expected_version },
    )
  }
  function projectModelMutationAction(value: ProjectModelPrivateCommand): ProjectModelWriteAction {
    if (value.domain === 'credential') {
      switch (value.command.kind) {
        case 'create':
          return 'project-model-credential-create'
        case 'update':
          return 'project-model-credential-update'
        case 'delete':
          return 'project-model-credential-delete'
      }
    }
    switch (value.command.kind) {
      case 'provider.create':
        return 'project-model-provider-create'
      case 'provider.update':
        return 'project-model-provider-update'
      case 'provider.delete':
        return 'project-model-provider-delete'
      case 'model.create':
        return 'project-model-create'
      case 'model.update':
        return 'project-model-update'
      case 'model.delete':
        return 'project-model-delete'
    }
  }
  function knownProjectModelRejection(
    value: ProjectModelPrivateCommand,
    e: AccountFailure,
  ): boolean {
    const p = e.problem
    if (!p || (p.commit_state !== 'not_started' && p.commit_state !== 'not_committed')) return false
    if (
      (p.status === 400 && p.code === 'INVALID_ARGUMENT') ||
      (p.status === 413 && p.code === 'PAYLOAD_TOO_LARGE') ||
      (p.status === 415 && p.code === 'UNSUPPORTED_MEDIA_TYPE')
    )
      return true
    if (
      p.status === 409 &&
      ['VERSION_CONFLICT', 'INVALID_STATE', 'RESOURCE_BUSY', 'PROJECT_NOT_ACTIVE'].includes(p.code)
    )
      return true
    if (
      value.domain === 'configuration' &&
      ((p.status === 422 && p.code === 'CAPABILITY_UNSUPPORTED') ||
        (p.status === 503 && p.code === 'DEPENDENCY_UNBOUND'))
    )
      return true
    return (
      (p.status === 401 && ['UNAUTHENTICATED', 'SESSION_REVOKED'].includes(p.code)) ||
      (p.status === 403 && ['FORBIDDEN', 'CSRF_FAILED', 'ORIGIN_DENIED'].includes(p.code)) ||
      (p.status === 404 && p.code === 'NOT_FOUND')
    )
  }
  function publishProjectModel(
    original: ProjectModelIntent,
    command: ProjectModelPrivateCommand,
    phase: ProjectModelSettingsProgress['phase'],
    receipt: ProjectModelExecution | null = null,
  ) {
    projectModelState.progress = Object.freeze({
      projectID: original.projectID,
      domain: command.domain,
      kind: command.command.kind,
      phase,
      receipt,
      observation: 'none',
      observedResult: null,
      lookingUp: false,
      keyConflict: original.keyConflict,
    })
  }
  async function executeProjectModel(
    original: ProjectModelIntent,
    captured: ProjectModelPrivateCommand,
    signal: AbortSignal,
  ): Promise<ProjectModelExecution> {
    const options = { csrfToken: original.csrf, key: original.key, signal },
      project = original.projectID
    if (captured.domain === 'credential') {
      const command = captured.command
      let result: ProjectCredentialMutation
      switch (command.kind) {
        case 'create':
          if (captured.material === null) throw new AccountFailure('invalid-input')
          result = await projectModelSettingsAPI.createCredential(
            project,
            captured.material,
            options,
          )
          break
        case 'update':
          if (captured.material === null) throw new AccountFailure('invalid-input')
          result = await projectModelSettingsAPI.updateCredential(
            project,
            command.credential_id,
            command.expected_version,
            captured.material,
            options,
          )
          break
        case 'delete':
          result = await projectModelSettingsAPI.deleteCredential(
            project,
            command.credential_id,
            command.expected_version,
            options,
          )
          break
      }
      return parseProjectCredentialMutation(result, command)
    }
    const command = captured.command
    let result: ProjectConfigurationReceipt
    switch (command.kind) {
      case 'provider.create':
        result = await projectModelSettingsAPI.createProvider(project, command.input, options)
        break
      case 'provider.update':
        result = await projectModelSettingsAPI.updateProvider(
          project,
          command.id,
          command.expected_version,
          command.input,
          options,
        )
        break
      case 'provider.delete':
        result = await projectModelSettingsAPI.deleteProvider(
          project,
          command.id,
          command.expected_version,
          options,
        )
        break
      case 'model.create':
        result = await projectModelSettingsAPI.createModel(
          project,
          { provider_id: command.provider_id, protocol: command.protocol },
          command.input,
          options,
        )
        break
      case 'model.update':
        result = await projectModelSettingsAPI.updateModel(
          project,
          { id: command.id, provider_id: command.provider_id, protocol: command.protocol },
          command.expected_version,
          command.input,
          options,
        )
        break
      case 'model.delete':
        result = await projectModelSettingsAPI.deleteModel(
          project,
          command.id,
          command.expected_version,
          command.replacement,
          options,
        )
        break
    }
    return parseProjectConfigurationReceipt(result, command)
  }
  function performProjectModel(original: ProjectModelIntent): Promise<ProjectModelExecution> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    const captured = original.payload.command
    if (
      projectModelIntent !== original ||
      !captured ||
      !projectContext(original) ||
      original.keyConflict
    )
      return Promise.reject(new AccountFailure('invalid-input'))
    const kind = projectModelMutationAction(captured),
      revision = projectModelRevisions[kind]
    const live = () =>
      projectModelIntent === original &&
      revision === projectModelRevisions[kind] &&
      projectContext(original)
    let dispatched = false
    publishProjectModel(original, captured, 'submitting')
    return runAuthorized(
      original.identity,
      async (op, current) => {
        if (!current() || !live() || original.payload.command !== captured)
          throw new AccountFailure('cancelled')
        if (projectModelBody(captured) !== original.payload.body)
          throw new AccountFailure('invalid-input')
        dispatched = true
        try {
          return await executeProjectModel(original, captured, op.abort.signal)
        } finally {
          if (projectModelIntent !== original) clearProjectModelPayload(original)
        }
      },
      undefined,
      kind,
    ).then(
      (receipt) => {
        if (!live()) throw new AccountFailure('cancelled')
        // Retire this private intent before synchronous receipt consumers can start another.
        clearProjectModelPayload(original)
        projectModelIntent = null
        publishProjectModel(original, captured, 'confirmed', receipt)
        return receipt
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (
          projectModelIntent === original &&
          revision === projectModelRevisions[kind] &&
          sameIdentity(original.identity, personalContext.identity)
        ) {
          original.keyConflict ||=
            e.problem?.status === 409 && e.problem.code === 'IDEMPOTENCY_KEY_REUSED'
          original.uncertain ||=
            original.keyConflict || (dispatched && !knownProjectModelRejection(captured, e))
          publishProjectModel(original, captured, original.uncertain ? 'uncertain' : 'rejected')
        }
        throw e
      },
    )
  }
  function startProjectModel(
    projectID: string,
    command: ProjectModelPrivateCommand,
  ): Promise<ProjectModelExecution> {
    try {
      const identity = personalIdentity()
      if (projectModelIntent || personalIntent || pending) throw new AccountFailure('busy')
      const project = captureProjectModelID(projectID)
      const original: ProjectModelIntent = {
        projectID: project,
        identity,
        csrf: sessionCSRF,
        key: newKey(),
        payload: { command, body: projectModelBody(command) },
        uncertain: false,
        keyConflict: false,
      }
      projectModelIntent = original
      return performProjectModel(original)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function startProjectConfiguration<C extends ProjectConfigurationCommand>(
    projectID: string,
    command: C,
  ): Promise<ProjectConfigurationReceipt<C['kind']>> {
    try {
      // Availability precedes input capture, preserving the old single-owner busy boundary.
      personalIdentity()
      if (projectModelIntent) throw new AccountFailure('busy')
      const captured = captureProjectConfigurationCommand(command)
      return startProjectModel(
        projectID,
        Object.freeze({ domain: 'configuration', command: captured }),
      ) as Promise<ProjectConfigurationReceipt<C['kind']>>
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function startProjectCredential(
    projectID: string,
    command: ProjectCredentialLookupTarget,
    material: string | null,
  ): Promise<ProjectCredentialMutation> {
    try {
      personalIdentity()
      if (projectModelIntent) throw new AccountFailure('busy')
      const captured = captureProjectCredentialTarget(command),
        value = captured.kind === 'delete' ? null : captureProjectCredentialValue(material!)
      return startProjectModel(
        projectID,
        Object.freeze({ domain: 'credential', command: captured, material: value }),
      ) as Promise<ProjectCredentialMutation>
    } catch (error) {
      return Promise.reject(error)
    }
  }
  function observeProjectModel(
    domain: ProjectModelPrivateCommand['domain'],
  ): Promise<ProjectModelObservation> {
    if (owner) return Promise.reject(new AccountFailure('busy'))
    const original = projectModelIntent,
      captured = original?.payload.command
    if (
      !original ||
      !captured ||
      captured.domain !== domain ||
      !original.uncertain ||
      original.keyConflict ||
      !projectContext(original)
    )
      return Promise.reject(new AccountFailure('invalid-input'))
    const kind =
        domain === 'configuration'
          ? 'project-model-configuration-lookup'
          : 'project-model-credential-lookup',
      revision = projectModelRevisions[kind]
    const live = () =>
      projectModelIntent === original &&
      revision === projectModelRevisions[kind] &&
      projectContext(original)
    if (projectModelState.progress)
      projectModelState.progress = Object.freeze({ ...projectModelState.progress, lookingUp: true })
    return runAuthorized<ProjectModelObservation>(
      original.identity,
      async (op, current) => {
        if (
          !current() ||
          !live() ||
          original.payload.command !== captured ||
          projectModelBody(captured) !== original.payload.body
        )
          throw new AccountFailure('cancelled')
        const options = { csrfToken: original.csrf, key: original.key, signal: op.abort.signal }
        return captured.domain === 'configuration'
          ? parseProjectConfigurationObservation(
              await projectModelSettingsAPI.lookupConfiguration(
                original.projectID,
                captured.command,
                options,
              ),
              captured.command,
            )
          : parseProjectCredentialObservation(
              await projectModelSettingsAPI.lookupCredential(
                original.projectID,
                captured.command,
                options,
              ),
              captured.command,
            )
      },
      undefined,
      kind,
    ).then(
      (value) => {
        if (!live() || !projectModelState.progress) throw new AccountFailure('cancelled')
        const observed = 'found' in value ? value.found : value.observed
        projectModelState.progress = Object.freeze({
          ...projectModelState.progress,
          phase: 'uncertain',
          lookingUp: false,
          observation: observed ? 'observed' : 'not_observed',
          observedResult: value,
        })
        return value
      },
      (error: unknown) => {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (live() && projectModelState.progress) {
          original.keyConflict ||=
            e.problem?.status === 409 && e.problem.code === 'IDEMPOTENCY_KEY_REUSED'
          projectModelState.progress = Object.freeze({
            ...projectModelState.progress,
            lookingUp: false,
            observation: 'failed',
            observedResult: null,
            keyConflict: original.keyConflict,
          })
        }
        throw e
      },
    )
  }
  function readProjectModel<T>(
    kind: ProjectModelReadAction,
    capture: () => (signal: AbortSignal) => Promise<T>,
  ): Promise<T> {
    try {
      const identity = personalIdentity(),
        work = capture()
      return runAuthorized(identity, (op) => work(op.abort.signal), undefined, kind)
    } catch (error) {
      return Promise.reject(error)
    }
  }
  const projectModelSettings = {
    get progress(): ProjectModelSettingsProgress | null {
      const value = projectModelState.progress
      if (!value) return null
      const contextValid = !!projectModelIntent && projectContext(projectModelIntent)
      return Object.freeze({
        ...value,
        contextValid,
        canRetryOriginal:
          !!projectModelIntent?.payload.command &&
          projectModelIntent.uncertain &&
          !projectModelIntent.keyConflict &&
          contextValid &&
          !state.busy,
      })
    },
    listProviders(projectID: string, query: ProjectModelPageQuery) {
      return readProjectModel('project-model-provider-list', () => {
        const project = captureProjectModelID(projectID),
          captured = captureProjectModelQuery(query)
        return (signal) => projectModelSettingsAPI.listProviders(project, captured, signal)
      })
    },
    getProvider(projectID: string, providerID: string) {
      return readProjectModel('project-model-provider-get', () => {
        const project = captureProjectModelID(projectID),
          target = captureProjectModelID(providerID)
        return (signal) => projectModelSettingsAPI.getProvider(project, target, signal)
      })
    },
    listModels(projectID: string, query: ProjectModelPageQuery) {
      return readProjectModel('project-model-list', () => {
        const project = captureProjectModelID(projectID),
          captured = captureProjectModelQuery(query)
        return (signal) => projectModelSettingsAPI.listModels(project, captured, signal)
      })
    },
    getModel(projectID: string, modelID: string) {
      return readProjectModel('project-model-get', () => {
        const project = captureProjectModelID(projectID),
          target = captureProjectModelID(modelID)
        return (signal) => projectModelSettingsAPI.getModel(project, target, signal)
      })
    },
    listAvailableChatModels(projectID: string, query: ProjectModelPageQuery) {
      return readProjectModel('project-model-available-list', () => {
        const project = captureProjectModelID(projectID),
          captured = captureProjectModelQuery(query)
        return (signal) =>
          projectModelSettingsAPI.listAvailableChatModels(project, captured, signal)
      })
    },
    getCredentialMetadata(projectID: string, credentialID: string) {
      return readProjectModel('project-model-credential-get', () => {
        const project = captureProjectModelID(projectID),
          target = captureProjectModelID(credentialID)
        return (signal) => projectModelSettingsAPI.getCredentialMetadata(project, target, signal)
      })
    },
    createProvider(projectID: string, input: ProjectProviderWriteInput) {
      return startProjectConfiguration(projectID, { kind: 'provider.create', input })
    },
    updateProvider(
      projectID: string,
      providerID: string,
      expectedVersion: string,
      input: ProjectProviderWriteInput,
    ) {
      return startProjectConfiguration(projectID, {
        kind: 'provider.update',
        id: providerID,
        expected_version: expectedVersion,
        input,
      })
    },
    deleteProvider(projectID: string, providerID: string, expectedVersion: string) {
      return startProjectConfiguration(projectID, {
        kind: 'provider.delete',
        id: providerID,
        expected_version: expectedVersion,
      })
    },
    createModel(
      projectID: string,
      context: ProjectModelWriteContext,
      input: ProjectModelWriteInput,
    ) {
      try {
        personalIdentity()
        return startProjectConfiguration(projectID, {
          kind: 'model.create',
          ...captureProjectModelContext(context),
          input,
        })
      } catch (error) {
        return Promise.reject(error)
      }
    },
    updateModel(
      projectID: string,
      target: ProjectModelWriteTarget,
      expectedVersion: string,
      input: ProjectModelWriteInput,
    ) {
      try {
        personalIdentity()
        const captured = captureProjectModelInput(() => {
          const value = { ...shape(target, ['id', 'provider_id', 'protocol']) }
          return {
            id: captureProjectModelID(value.id),
            ...captureProjectModelContext({
              provider_id: value.provider_id as string,
              protocol: value.protocol as ProjectModelWriteContext['protocol'],
            }),
          }
        })
        return startProjectConfiguration(projectID, {
          kind: 'model.update',
          ...captured,
          expected_version: expectedVersion,
          input,
        })
      } catch (error) {
        return Promise.reject(error)
      }
    },
    deleteModel(
      projectID: string,
      modelID: string,
      expectedVersion: string,
      replacement: string | null,
    ) {
      return startProjectConfiguration(projectID, {
        kind: 'model.delete',
        id: modelID,
        expected_version: expectedVersion,
        replacement,
      })
    },
    createCredential(projectID: string, value: string) {
      return startProjectCredential(
        projectID,
        { kind: 'create' },
        value,
      ) as Promise<ProjectCredentialCreated>
    },
    updateCredential(
      projectID: string,
      credentialID: string,
      expectedVersion: string,
      value: string,
    ) {
      return startProjectCredential(
        projectID,
        { kind: 'update', credential_id: credentialID, expected_version: expectedVersion },
        value,
      ) as Promise<ProjectCredentialUpdated>
    },
    deleteCredential(projectID: string, credentialID: string, expectedVersion: string) {
      return startProjectCredential(
        projectID,
        { kind: 'delete', credential_id: credentialID, expected_version: expectedVersion },
        null,
      ) as Promise<ProjectCredentialDeleted>
    },
    lookupConfiguration() {
      return observeProjectModel('configuration') as Promise<ProjectConfigurationObservation>
    },
    lookupCredential() {
      return observeProjectModel('credential') as Promise<ProjectCredentialObservation>
    },
    retryOriginal() {
      if (!projectModelIntent || !projectModelSettings.progress?.canRetryOriginal)
        return Promise.reject(new AccountFailure('invalid-input'))
      return performProjectModel(projectModelIntent)
    },
    abandonReads: clearProjectModelReads,
    abandonPending: clearProjectModelPending,
    editRejected() {
      if (
        owner ||
        !projectModelIntent ||
        projectModelIntent.uncertain ||
        projectModelIntent.keyConflict ||
        projectModelState.progress?.phase !== 'rejected' ||
        !projectContext(projectModelIntent)
      )
        throw new AccountFailure('invalid-input')
      clearProjectModelPending()
    },
  }
  function clearRuntimeInformationRead() {
    ++runtimeInformationRevision
    if (owner?.kind === 'runtime-information-read') owner.abandon?.()
  }
  const runtimeInformation = {
    get() {
      try {
        const identity = invitationIdentity()
        return runAuthorized(
          identity,
          (op) => runtimeInformationAPI.get(op.abort.signal),
          undefined,
          'runtime-information-read',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    abandon: clearRuntimeInformationRead,
  }
  function clearKnowledgeRead() {
    ++knowledgeRevision
    if (owner?.kind === 'knowledge-read') owner.abandon?.()
  }
  function knowledgeFailure(current: () => boolean, error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    // Only this still-current Human read can invalidate the current Session.
    // Local 403/404 and a retired response never set System/admin denied state.
    if (current() && unavailableSession(e)) {
      clearIdentity()
      clearBrowser()
      state.phase = 'unavailable'
      state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
    }
    return e
  }
  function knowledgeRead<T>(capture: () => (signal: AbortSignal) => Promise<T>): Promise<T> {
    try {
      const identity = personalIdentity(),
        work = capture()
      return runAuthorized(identity, (op) => work(op.abort.signal), undefined, 'knowledge-read')
    } catch (error) {
      return Promise.reject(error)
    }
  }
  const knowledge = {
    children(projectID: string, parentID: string | null, query: KnowledgeQuery) {
      return knowledgeRead(() => {
        const project = captureKnowledgeID(projectID),
          parent = parentID === null ? null : captureKnowledgeID(parentID),
          captured = captureKnowledgeQuery(query)
        return (signal) => knowledgeAPI.children(project, parent, captured, signal)
      })
    },
    get(projectID: string, documentID: string) {
      return knowledgeRead(() => {
        const project = captureKnowledgeID(projectID),
          target = captureKnowledgeID(documentID)
        return (signal) => knowledgeAPI.get(project, target, signal)
      })
    },
    ancestors(projectID: string, documentID: string) {
      return knowledgeRead(() => {
        const project = captureKnowledgeID(projectID),
          target = captureKnowledgeID(documentID)
        return (signal) => knowledgeAPI.ancestors(project, target, signal)
      })
    },
    readContent(projectID: string, documentID: string, request: KnowledgeReadRequest) {
      return knowledgeRead(() => {
        const project = captureKnowledgeID(projectID),
          target = captureKnowledgeID(documentID),
          captured = captureKnowledgeReadRequest(request)
        return (signal) => knowledgeAPI.readContent(project, target, captured, signal)
      })
    },
    abandon: clearKnowledgeRead,
  }
  function clearAuditRead() {
    ++auditRevision
    if (owner?.kind === 'audit-read') owner.abandon?.()
  }
  function isProjectAuditAction(kind: Action): kind is ProjectAuditAction {
    return kind === 'project-audit-list' || kind === 'project-audit-get'
  }
  function clearProjectAuditRead() {
    ++projectAuditRevision
    if (owner && isProjectAuditAction(owner.kind)) owner.abandon?.()
  }
  function projectAuditFailure(current: () => boolean, error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    // Only the current domain revision/operation/full identity can invalidate
    // the Session. GET-local denial and CSRF codes do not authorize cleanup.
    if (current() && unavailableSession(e)) {
      clearIdentity()
      clearBrowser()
      state.phase = 'unavailable'
      state.notice = '当前登录上下文已失效，请检查当前会话或重新登录。'
    }
    return e
  }
  const projectAudit = {
    list(projectID: string, query: AuditQuery) {
      try {
        const identity = personalIdentity()
        const project = captureProjectAuditID(projectID),
          captured = captureAuditQuery(query)
        return runAuthorized(
          identity,
          (op) => projectAuditAPI.list(project, captured, op.abort.signal),
          undefined,
          'project-audit-list',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    get(projectID: string, auditID: string) {
      try {
        const identity = personalIdentity()
        const project = captureProjectAuditID(projectID),
          target = captureProjectAuditID(auditID)
        return runAuthorized(
          identity,
          (op) => projectAuditAPI.get(project, target, op.abort.signal),
          undefined,
          'project-audit-get',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    abandon: clearProjectAuditRead,
  }
  const audit = {
    list(query: AuditQuery) {
      try {
        const identity = invitationIdentity()
        const captured = captureAuditQuery(query)
        return runAuthorized(
          identity,
          (op) => auditAPI.list(captured, op.abort.signal),
          undefined,
          'audit-read',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    get(id: string) {
      try {
        const identity = invitationIdentity()
        if (typeof id !== 'string' || !uuid7.test(id)) throw new AccountFailure('invalid-input')
        const target = id
        return runAuthorized(
          identity,
          (op) => auditAPI.get(target, op.abort.signal),
          undefined,
          'audit-read',
        )
      } catch (error) {
        return Promise.reject(error)
      }
    },
    abandon: clearAuditRead,
  }
  const system = {
    runtimeInformation,
    audit,
    outboundPolicy,
    smtpDelivery,
    smtp,
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
    projects,
    projectAudit,
    knowledge,
    projectModelSettings,
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
