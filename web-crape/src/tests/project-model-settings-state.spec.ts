import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { watch } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { AccountFailure, type Fetch, type CommitState } from '../api/client'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import { createSystemAccountSecurityAPI } from '../api/system-account-security'
import { createSystemSMTPSettingsAPI } from '../api/system-smtp-settings'
import { createSystemSMTPDeliveryAPI } from '../api/system-smtp-delivery'
import { createSystemOutboundPolicyAPI } from '../api/system-outbound-policy'
import { createSystemAuditAPI } from '../api/system-audit'
import { createSystemRuntimeInformationAPI } from '../api/system-runtime-information'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectAuditAPI } from '../api/project-audit'
import {
  createProjectModelSettingsAPI,
  type ProjectModelSettingsAPI,
  type ProjectModelWriteInput,
  type ProjectProviderWriteInput,
} from '../api/project-models'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectModelSettings } from '../composables/useProjectModelSettings'
import { useTheme } from '../composables/useTheme'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-08T12:34:56.123456Z',
  projectID = id(10),
  providerID = id(20),
  modelID = id(30),
  credentialID = id(40),
  prefix = `/api/v1/projects/${projectID}`
const view = (session = id(2), role: 'admin' | 'user' = 'user'): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role,
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: session, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const project = (n = 10, lifecycle: Project['lifecycle'] = 'active'): Project => ({
  id: id(n),
  owner_user_id: id(1),
  name: n === 10 ? 'Main' : 'Second',
  normalized_name: n === 10 ? 'main' : 'second',
  description: 'Safe description',
  lifecycle,
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: lifecycle === 'archived' ? time : null,
})
const providerInput = (): ProjectProviderWriteInput => ({
  name: 'Provider',
  protocol: 'openai-chat-completions',
  base_url: 'https://models.example/v1',
  enabled: true,
  credential_ref: null,
  options: {},
})
const modelInput = (): ProjectModelWriteInput => ({
  name: 'Chat',
  provider_model_id: 'native-chat',
  type: 'chat',
  enabled: true,
  parameters: {},
  request_overwrite: {},
  header_overwrite: {},
  capabilities: {
    tool_calls: true,
    parallel_tool_calls: false,
    streaming: true,
    reasoning: false,
    input_modalities: ['text'],
    output_modalities: ['text'],
    structured_output_modes: ['text'],
    reasoning_efforts: [],
    context_length: '9007199254740993',
    max_output: null,
  },
})
const provider = (p = projectID, n = 20, version = '1') => ({
  id: id(n),
  scope: { kind: 'project', project_id: p },
  input: providerInput(),
  version,
  created_at: time,
  updated_at: time,
})
const model = (p = projectID, version = '1') => ({
  ...provider(p, 30, version),
  provider_id: providerID,
  input: modelInput(),
})
const available = (system = false, n = 30) => ({
  id: id(n),
  provider_id: providerID,
  scope: system ? { kind: 'system' } : { kind: 'project', project_id: projectID },
  name: 'Chat',
  provider_name: 'Provider',
  version: '1',
  capabilities: modelInput().capabilities,
})
const metadata = (version = '1') => ({ credential_id: credentialID, purpose: 'model', version })
const receipt = (kind = 'provider.create', version = kind.endsWith('.create') ? '1' : '2') => ({
  kind,
  resource_id: kind.startsWith('provider.') ? providerID : modelID,
  version,
  affected_references: '0',
})
const mutation = (kind = 'create', version = kind === 'create' ? '1' : '2') => ({
  ...metadata(version),
  deleted: kind === 'delete',
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problemValue = (code: string, status = 503, commit_state: CommitState = 'not_started') => ({
  type: 'urn:agenteam:problem:test',
  title: 'Failure',
  status,
  code,
  detail: '',
  instance: prefix,
  request_id: id(99),
  commit_state,
})
const problem = (code: string, status = 503, commit: CommitState = 'not_started') =>
  json(problemValue(code, status, commit), status)
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void, reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}
const resultOf = (promise: Promise<unknown>) =>
  promise.then(
    (value) => ({ value }),
    (error: unknown) => ({ error }),
  )
const owners: SessionController[] = [],
  workspaces: ProjectWorkspace[] = [],
  pages: ReturnType<typeof createProjectModelSettings>[] = [],
  releases: (() => void)[] = []
function nativeHold(value?: unknown, media = 'application/json') {
  const entered = barrier(),
    joined = barrier()
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      if (value !== undefined) c.enqueue(new TextEncoder().encode(JSON.stringify(value)))
    },
    cancel() {
      entered.resolve()
      return joined.promise
    },
  })
  const release = () => joined.resolve()
  releases.push(release)
  return {
    response: () => new Response(stream, { headers: { 'Content-Type': media } }),
    entered,
    stream,
    release,
  }
}
const defaultModels: Fetch = async (path, init) => {
  const parts = path.split('?')[0]!.split('/'),
    p = parts[4]!,
    family = parts[5]!,
    target = parts[6]
  if (init.method === 'GET') {
    if (family === 'model-credentials') return json(metadata())
    if (family === 'available-chat-models')
      return json({ items: [available(), available(true, 31)], next_cursor: null })
    const item = family === 'model-providers' ? provider(p) : model(p)
    return json(target ? item : { items: [item], next_cursor: null })
  }
  if (family === 'model-commands') return json({ found: false, receipt: null })
  if (family === 'model-credential-commands') return json({ observed: false, result: null })
  const kind = init.method === 'POST' ? 'create' : init.method === 'PUT' ? 'update' : 'delete'
  const body = JSON.parse(String(init.body)) as { expected_version?: string }
  const version = body.expected_version ? String(BigInt(body.expected_version) + 1n) : '1'
  return json(
    family === 'model-credentials'
      ? mutation(kind, version)
      : receipt(`${family === 'model-providers' ? 'provider' : 'model'}.${kind}`, version),
  )
}
const isModels = (path: string) =>
  /^\/api\/v1\/projects\/[^/]+\/(?:model-providers|models|available-chat-models|model-commands|model-credentials|model-credential-commands)(?:[/?]|$)/.test(
    path,
  )
async function fixture(
  options: {
    role?: 'user' | 'admin'
    lifecycle?: Project['lifecycle']
    api?: ProjectModelSettingsAPI
  } = {},
) {
  let current = view(id(2), options.role),
    currentProject = project(10, options.lifecycle),
    models: Fetch = defaultModels,
    session: Fetch = async () => json(current),
    owner: Fetch | null = null,
    other: Fetch = async () => json({ items: [], next_cursor: null })
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    if (isModels(path)) return models(path, init)
    if (path.startsWith('/api/v1/projects/'))
      return owner ? owner(path, init) : json(currentProject)
    if (path === '/api/v1/me') return json({ user: current.user, avatar: null })
    return other(path, init)
  })
  const auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    createSystemModelSelectionAPI(fetch),
    createSystemAccountSecurityAPI(fetch),
    createSystemSMTPSettingsAPI(fetch),
    createSystemSMTPDeliveryAPI(fetch),
    createSystemOutboundPolicyAPI(fetch),
    createSystemAuditAPI(fetch),
    createSystemRuntimeInformationAPI(fetch),
    createProjectOwnerAPI(fetch),
    createProjectAuditAPI(fetch),
    options.api ?? createProjectModelSettingsAPI(fetch),
  )
  owners.push(auth)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspaces.push(workspace)
  return {
    auth,
    workspace,
    fetch,
    async page(suffix = 'model-providers') {
      const path = `/owner/main/settings/${suffix}`
      workspace.afterNavigation(path)
      await flushPromises()
      expect(workspace.detail.phase).toBe('current')
      const value = createProjectModelSettings(auth, workspace)
      pages.push(value)
      value.afterNavigation(path)
      await flushPromises()
      expect(value.visible.value).toBe(true)
      return value
    },
    setModels(value: Fetch) {
      models = value
    },
    setOwner(value: Fetch | null) {
      owner = value
    },
    setOther(value: Fetch) {
      other = value
    },
    setSession(value: Fetch) {
      session = value
    },
    setView(value: SessionView) {
      current = value
    },
    setProject(value: Project) {
      currentProject = value
    },
    calls() {
      return fetch.mock.calls.filter(([path]) => isModels(path))
    },
    writes() {
      return fetch.mock.calls.filter(([path, init]) => isModels(path) && init.method !== 'GET')
    },
  }
}
afterEach(async () => {
  for (const page of pages.splice(0)) page.dispose()
  for (const workspace of workspaces.splice(0)) workspace.dispose()
  for (const owner of owners.splice(0)) owner.leave()
  for (const release of releases.splice(0)) release()
  await flushPromises()
  useTheme().setTheme('system')
  vi.useRealTimers()
  vi.restoreAllMocks()
})
const reads = [
  {
    name: 'providers',
    run: (a: SessionController) => a.projectModelSettings.listProviders(projectID, {}),
  },
  {
    name: 'provider',
    run: (a: SessionController) => a.projectModelSettings.getProvider(projectID, providerID),
  },
  {
    name: 'models',
    run: (a: SessionController) => a.projectModelSettings.listModels(projectID, {}),
  },
  {
    name: 'model',
    run: (a: SessionController) => a.projectModelSettings.getModel(projectID, modelID),
  },
  {
    name: 'available',
    run: (a: SessionController) => a.projectModelSettings.listAvailableChatModels(projectID, {}),
  },
  {
    name: 'metadata',
    run: (a: SessionController) =>
      a.projectModelSettings.getCredentialMetadata(projectID, credentialID),
  },
] as const
const writes = [
  {
    kind: 'provider.create',
    run: (a: SessionController) =>
      a.projectModelSettings.createProvider(projectID, providerInput()),
  },
  {
    kind: 'provider.update',
    run: (a: SessionController) =>
      a.projectModelSettings.updateProvider(projectID, providerID, '1', providerInput()),
  },
  {
    kind: 'provider.delete',
    run: (a: SessionController) =>
      a.projectModelSettings.deleteProvider(projectID, providerID, '1'),
  },
  {
    kind: 'model.create',
    run: (a: SessionController) =>
      a.projectModelSettings.createModel(
        projectID,
        { provider_id: providerID, protocol: 'openai-chat-completions' },
        modelInput(),
      ),
  },
  {
    kind: 'model.update',
    run: (a: SessionController) =>
      a.projectModelSettings.updateModel(
        projectID,
        { id: modelID, provider_id: providerID, protocol: 'openai-chat-completions' },
        '1',
        modelInput(),
      ),
  },
  {
    kind: 'model.delete',
    run: (a: SessionController) =>
      a.projectModelSettings.deleteModel(projectID, modelID, '1', id(31)),
  },
  {
    kind: 'create',
    run: (a: SessionController) =>
      a.projectModelSettings.createCredential(projectID, 'private original value'),
  },
  {
    kind: 'update',
    run: (a: SessionController) =>
      a.projectModelSettings.updateCredential(
        projectID,
        credentialID,
        '1',
        'private original value',
      ),
  },
  {
    kind: 'delete',
    run: (a: SessionController) =>
      a.projectModelSettings.deleteCredential(projectID, credentialID, '1'),
  },
] as const
const oldReads = [
  {
    name: 'personal',
    run: (a: SessionController) => a.personal.getPreferences(),
    abandon: (a: SessionController) => a.personal.abandon(),
  },
  {
    name: 'directory',
    run: (a: SessionController) => a.system.listUsers({}),
    abandon: (a: SessionController) => a.system.abandon(),
  },
  {
    name: 'invitation',
    run: (a: SessionController) => a.system.listInvitations({}),
    abandon: (a: SessionController) => a.system.abandonInvitationRead(),
  },
  {
    name: 'Provider',
    run: (a: SessionController) => a.system.providers.list({}),
    abandon: (a: SessionController) => a.system.providers.abandonRead('provider-list'),
  },
  {
    name: 'Model',
    run: (a: SessionController) =>
      a.system.models.get({
        id: modelID,
        provider_id: providerID,
        protocol: 'openai-chat-completions',
      }),
    abandon: (a: SessionController) => a.system.models.abandonRead('model-detail'),
  },
  {
    name: 'Selection',
    run: (a: SessionController) => a.system.selection.get(),
    abandon: (a: SessionController) => a.system.selection.abandonRead('selection-state'),
  },
  {
    name: 'Summary',
    run: (a: SessionController) => a.system.selection.meetingSummary.get(),
    abandon: (a: SessionController) =>
      a.system.selection.meetingSummary.abandonRead('meeting-summary-state'),
  },
  {
    name: 'security',
    run: (a: SessionController) => a.system.accountSecurity.get(),
    abandon: (a: SessionController) => a.system.accountSecurity.abandonRead(),
  },
  {
    name: 'SMTP',
    run: (a: SessionController) => a.system.smtp.get(),
    abandon: (a: SessionController) => a.system.smtp.abandonRead(),
  },
  {
    name: 'delivery',
    run: (a: SessionController) => a.system.smtpDelivery.list(),
    abandon: (a: SessionController) => a.system.smtpDelivery.abandonRead(),
  },
  {
    name: 'outbound',
    run: (a: SessionController) => a.system.outboundPolicy.get(),
    abandon: (a: SessionController) => a.system.outboundPolicy.abandonRead(),
  },
  {
    name: 'System Audit',
    run: (a: SessionController) => a.system.audit.list({}),
    abandon: (a: SessionController) => a.system.audit.abandon(),
  },
  {
    name: 'runtime',
    run: (a: SessionController) => a.system.runtimeInformation.get(),
    abandon: (a: SessionController) => a.system.runtimeInformation.abandon(),
  },
  {
    name: 'Owner',
    run: (a: SessionController) => a.projects.get(projectID),
    abandon: (a: SessionController) => a.projects.abandonRead(),
  },
  {
    name: 'Project Audit',
    run: (a: SessionController) => a.projectAudit.list(projectID, {}),
    abandon: (a: SessionController) => a.projectAudit.abandon(),
  },
] as const
describe('Project model Session action classification and original intents', () => {
  it.each(reads)('ordinary Owner can read $name without System authorization', async ({ run }) => {
    const f = await fixture()
    await run(f.auth)
    expect(f.calls()).toHaveLength(1)
    expect(f.writes()).toHaveLength(0)
    expect(f.auth.state.phase).toBe('authenticated')
  })
  it.each(writes)(
    'ordinary Owner executes exact $kind through its own action',
    async ({ kind, run }) => {
      const f = await fixture()
      await run(f.auth)
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        kind,
        phase: 'confirmed',
        receipt: kind.includes('.') ? receipt(kind) : mutation(kind),
      })
      expect(f.writes()).toHaveLength(1)
      expect(f.writes()[0]![1]).toMatchObject({ credentials: 'same-origin', redirect: 'error' })
      expect(new Headers(f.writes()[0]![1].headers).get('X-CSRF-Token')).toBe('S'.repeat(43))
      expect(f.auth.system.denied).toBe(false)
    },
  )
  describe.each([false, true])('confirmed successor with explicit abandon=%s', (abandon) => {
    it.each(
      writes.map((original, index) => ({ original, next: writes[(index + 1) % writes.length]! })),
    )(
      '$original.kind preserves independent $next.kind started by a synchronous confirmed consumer',
      async ({ original, next }) => {
        const f = await fixture(),
          response = barrier<Response>(),
          expected = next.kind.includes('.') ? receipt(next.kind) : mutation(next.kind)
        releases.push(() => response.resolve(json(expected)))
        let observed = false,
          successor: ReturnType<typeof resultOf> | undefined
        const stop = watch(
          () => f.auth.projectModelSettings.progress,
          (progress) => {
            if (progress?.phase !== 'confirmed' || observed) return
            observed = true
            if (abandon) f.auth.projectModelSettings.abandonPending()
            f.setModels(async () => response.promise)
            successor = resultOf(next.run(f.auth))
          },
          { flush: 'sync' },
        )
        try {
          expect(await original.run(f.auth)).toEqual(
            original.kind.includes('.') ? receipt(original.kind) : mutation(original.kind),
          )
          await flushPromises()
          expect(observed).toBe(true)
          expect(f.writes()).toHaveLength(2)
          expect(f.auth.projectModelSettings.progress).toMatchObject({
            kind: next.kind,
            phase: 'submitting',
            receipt: null,
          })
          expect(new Headers(f.writes()[1]![1].headers).get('Idempotency-Key')).not.toBe(
            new Headers(f.writes()[0]![1].headers).get('Idempotency-Key'),
          )
          response.resolve(json(expected))
          expect(await successor).toEqual({ value: expected })
          expect(f.auth.projectModelSettings.progress).toMatchObject({
            kind: next.kind,
            phase: 'confirmed',
            receipt: expected,
          })
          expect(f.auth.state.phase).toBe('authenticated')
          expect(f.auth.state.busy).toBe(false)
        } finally {
          stop()
          response.resolve(json(expected))
          await successor
        }
      },
    )
  })
  it('confirmed consumer can retire the old identity before a different Session starts a command', async () => {
    const f = await fixture()
    let observed = false
    const stop = watch(
      () => f.auth.projectModelSettings.progress,
      (progress) => {
        if (progress?.phase !== 'confirmed' || observed) return
        observed = true
        f.auth.leave()
      },
      { flush: 'sync' },
    )
    try {
      expect(await writes[0].run(f.auth)).toEqual(receipt())
      expect(observed).toBe(true)
      expect(f.auth.projectModelSettings.progress).toBeNull()
      f.setView(view(id(3)))
      await f.auth.restore()
      expect(f.auth.state.session?.id).toBe(id(3))
      expect(await writes[6].run(f.auth)).toEqual(mutation())
      expect(f.writes()).toHaveLength(2)
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        kind: 'create',
        phase: 'confirmed',
        receipt: mutation(),
      })
    } finally {
      stop()
    }
  })
  it('System denial does not change current Project read or write authority', async () => {
    const f = await fixture({ role: 'admin' })
    f.setOther(async () => problem('FORBIDDEN', 403))
    await resultOf(f.auth.system.listUsers({}))
    expect(f.auth.system.denied).toBe(true)
    await reads[0].run(f.auth)
    await writes[0].run(f.auth)
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
  })
  it.each(reads)(
    'GET $name CSRF failure stays local; current POST CSRF invalidates',
    async ({ run }) => {
      const f = await fixture()
      f.setModels(async () => problem('CSRF_FAILED', 403))
      await resultOf(run(f.auth))
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.system.denied).toBe(false)
      expect(f.auth.projectModelSettings.progress).toBeNull()
      await resultOf(writes[0].run(f.auth))
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.projectModelSettings.progress).toBeNull()
    },
  )
  it.each(['UNAUTHENTICATED', 'SESSION_REVOKED'])(
    'current 401 %s clears full identity',
    async (code) => {
      const f = await fixture()
      f.setModels(async () => problem(code, 401))
      await resultOf(reads[0].run(f.auth))
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.personalContext.identity).toBeNull()
    },
  )
  it.each(writes)(
    '$kind keeps original bytes/key across passive history, rejection, then explicit replay',
    async ({ kind, run }) => {
      const f = await fixture()
      f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await resultOf(run(f.auth))
      const [path, original] = f.writes()[0]!,
        originalKey = new Headers(original.headers).get('Idempotency-Key')
      expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
      await expect(writes.find((w) => w.kind !== kind)!.run(f.auth)).rejects.toMatchObject({
        kind: 'busy',
      })
      const configuration = kind.includes('.'),
        success = configuration ? receipt(kind) : mutation(kind)
      const lookup = () =>
        configuration
          ? f.auth.projectModelSettings.lookupConfiguration()
          : f.auth.projectModelSettings.lookupCredential()
      f.setModels(async () =>
        json(configuration ? { found: false, receipt: null } : { observed: false, result: null }),
      )
      await lookup()
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        phase: 'uncertain',
        observation: 'not_observed',
        receipt: null,
      })
      f.setModels(async () =>
        json(
          configuration ? { found: true, receipt: success } : { observed: true, result: success },
        ),
      )
      await lookup()
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        phase: 'uncertain',
        observation: 'observed',
        receipt: null,
      })
      const observation = f.writes().at(-1)![1]
      expect(JSON.parse(String(observation.body))).toEqual(
        configuration
          ? { command: kind }
          : kind === 'create'
            ? { kind }
            : { kind, credential_id: credentialID, expected_version: '1' },
      )
      expect(new Headers(observation.headers).get('Idempotency-Key')).toBe(originalKey)
      f.setModels(async () => problem('VERSION_CONFLICT', 409))
      await resultOf(f.auth.projectModelSettings.retryOriginal())
      expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
      f.setModels(async () => json(success))
      await f.auth.projectModelSettings.retryOriginal()
      const [retryPath, retried] = f.writes().at(-1)!
      expect(retryPath).toBe(path)
      expect(retried.body).toBe(original.body)
      expect(new Headers(retried.headers).get('Idempotency-Key')).toBe(originalKey)
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        phase: 'confirmed',
        receipt: success,
        canRetryOriginal: false,
      })
      expect(f.calls().every(([, init]) => init.method !== 'GET')).toBe(true)
    },
  )
  it.each([
    [400, 'INVALID_ARGUMENT', true],
    [413, 'PAYLOAD_TOO_LARGE', true],
    [415, 'UNSUPPORTED_MEDIA_TYPE', true],
    [409, 'VERSION_CONFLICT', true],
    [409, 'INVALID_STATE', true],
    [409, 'RESOURCE_BUSY', true],
    [409, 'PROJECT_NOT_ACTIVE', true],
    [422, 'CAPABILITY_UNSUPPORTED', true],
    [503, 'DEPENDENCY_UNBOUND', true],
    [403, 'FORBIDDEN', true],
    [403, 'ORIGIN_DENIED', true],
    [404, 'NOT_FOUND', true],
    [503, 'DEPENDENCY_UNAVAILABLE', false],
    [500, 'INTERNAL_ERROR', false],
    [400, 'VERSION_CONFLICT', false],
    [409, 'INVALID_ARGUMENT', false],
    [503, 'OTHER_CODE', false],
  ] as const)(
    'classifies only the exact first %i/%s rejection pair',
    async (status, code, rejected) => {
      const f = await fixture()
      f.setModels(async () => problem(code, status))
      await resultOf(writes[0].run(f.auth))
      expect(f.auth.projectModelSettings.progress?.phase).toBe(rejected ? 'rejected' : 'uncertain')
      expect(f.auth.system.denied).toBe(false)
      if (rejected) {
        f.auth.projectModelSettings.editRejected()
        expect(f.auth.projectModelSettings.progress).toBeNull()
      } else expect(() => f.auth.projectModelSettings.editRejected()).toThrow()
    },
  )
  it.each(['CAPABILITY_UNSUPPORTED', 'DEPENDENCY_UNBOUND'])(
    'credential never borrows configuration rejection %s',
    async (code) => {
      const f = await fixture()
      f.setModels(async () => problem(code, code === 'CAPABILITY_UNSUPPORTED' ? 422 : 503))
      await resultOf(writes[6].run(f.auth))
      expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    },
  )
  it.each(['unknown', 'committed'] as const)(
    'commit state %s overrides a familiar rejection',
    async (commit) => {
      const f = await fixture()
      f.setModels(async () => problem('VERSION_CONFLICT', 409, commit))
      await resultOf(writes[0].run(f.auth))
      expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    },
  )
  it.each(['write', 'lookup'] as const)(
    'key conflict from %s blocks replay and retains unknown until explicit abandonment',
    async (source) => {
      const f = await fixture()
      f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await resultOf(writes[6].run(f.auth))
      f.setModels(async () => problem('IDEMPOTENCY_KEY_REUSED', 409))
      await resultOf(
        source === 'write'
          ? f.auth.projectModelSettings.retryOriginal()
          : f.auth.projectModelSettings.lookupCredential(),
      )
      const before = f.calls().length
      expect(f.auth.projectModelSettings.progress).toMatchObject({
        phase: 'uncertain',
        keyConflict: true,
        canRetryOriginal: false,
      })
      await expect(f.auth.projectModelSettings.retryOriginal()).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      await expect(f.auth.projectModelSettings.lookupCredential()).rejects.toMatchObject({
        kind: 'invalid-input',
      })
      expect(f.calls()).toHaveLength(before)
      f.auth.projectModelSettings.abandonPending()
      expect(f.auth.projectModelSettings.progress).toBeNull()
    },
  )
  it('captures nested input once and rejects invalid inputs without creating an intent', async () => {
    const f = await fixture(),
      input = {
        ...modelInput(),
        capabilities: {
          ...modelInput().capabilities,
          input_modalities: [...modelInput().capabilities.input_modalities],
        },
      }
    for (const execute of [
      () => f.auth.projectModelSettings.createProvider('bad', providerInput()),
      () => f.auth.projectModelSettings.createCredential(projectID, ''),
      () =>
        f.auth.projectModelSettings.updateCredential(
          projectID,
          credentialID,
          '9223372036854775807',
          'value',
        ),
      () => f.auth.projectModelSettings.deleteModel(projectID, modelID, '1', modelID),
    ])
      await expect(execute()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.calls()).toHaveLength(0)
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    const attempt = resultOf(
      f.auth.projectModelSettings.createModel(
        projectID,
        { provider_id: providerID, protocol: 'openai-chat-completions' },
        input,
      ),
    )
    input.name = 'Edited after dispatch'
    input.capabilities.input_modalities.push('image')
    await attempt
    const body = JSON.parse(String(f.writes()[0]![1].body))
    expect(body.input.name).toBe('Chat')
    expect(body.input.capabilities.input_modalities).toEqual(['text'])
    expect(JSON.stringify(f.auth.projectModelSettings.progress)).not.toContain('Idempotency')
  })
})

describe('Project model ownership through actual request tails', () => {
  it.each(oldReads)(
    'all seventeen actions stay behind the old $name actual tail',
    async ({ run, abandon }) => {
      const f = await fixture({ role: 'admin' }),
        held = nativeHold({}, 'text/plain')
      f.setOther(async () => held.response())
      f.setOwner(async () => held.response())
      const pending = resultOf(run(f.auth))
      await held.entered.promise
      f.auth.projectModelSettings.abandonReads()
      f.auth.projectModelSettings.abandonPending()
      for (const read of reads)
        await expect(read.run(f.auth)).rejects.toMatchObject({ kind: 'busy' })
      for (const write of writes)
        await expect(write.run(f.auth)).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.projectModelSettings.lookupConfiguration()).rejects.toMatchObject({
        kind: 'busy',
      })
      await expect(f.auth.projectModelSettings.lookupCredential()).rejects.toMatchObject({
        kind: 'busy',
      })
      expect(f.calls()).toHaveLength(0)
      abandon(f.auth)
      await pending
      expect(f.auth.state.busy).toBe(true)
      held.release()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      expect(held.stream.locked).toBe(false)
    },
  )
  it.each(reads)(
    'abandoned $name keeps the shared Cookie owner until native cancel joins',
    async ({ run }) => {
      const f = await fixture(),
        held = nativeHold({}, 'text/plain')
      f.setModels(async () => held.response())
      const pending = resultOf(run(f.auth))
      await held.entered.promise
      f.auth.projectModelSettings.abandonReads()
      expect(await pending).toMatchObject({ error: { kind: 'cancelled' } })
      const before = f.fetch.mock.calls.length
      await expect(f.auth.personal.getProfile()).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.projects.get(projectID)).rejects.toMatchObject({ kind: 'busy' })
      await expect(f.auth.projectAudit.list(projectID, {})).rejects.toMatchObject({ kind: 'busy' })
      for (const write of writes)
        await expect(write.run(f.auth)).rejects.toMatchObject({ kind: 'busy' })
      await f.auth.restore()
      await f.auth.logout()
      expect(f.fetch.mock.calls).toHaveLength(before)
      expect(f.auth.state.busy).toBe(true)
      held.release()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      expect(held.stream.locked).toBe(false)
    },
  )
  it('30-second visible timeout keeps write Unknown and actual tail separate', async () => {
    const f = await fixture(),
      held = nativeHold(receipt())
    f.setModels(async () => held.response())
    vi.useFakeTimers()
    const pending = resultOf(writes[0].run(f.auth))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expect(await pending).toMatchObject({ error: { kind: 'cancelled' } })
    await held.entered.promise
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.projectModelSettings.progress).toMatchObject({
      phase: 'uncertain',
      canRetryOriginal: false,
    })
    await expect(f.auth.projectModelSettings.lookupConfiguration()).rejects.toMatchObject({
      kind: 'busy',
    })
    held.release()
    await flushPromises()
    expect(f.auth.projectModelSettings.progress).toMatchObject({
      phase: 'uncertain',
      canRetryOriginal: true,
    })
    expect(held.stream.locked).toBe(false)
  })
  it.each([401, 403])('retired late %i never clears the still current identity', async (status) => {
    const gate = barrier<never>(),
      api = createProjectModelSettingsAPI(async () => json({}))
    void gate.promise.catch(() => undefined)
    releases.push(() => gate.reject(new AccountFailure('cancelled')))
    const f = await fixture({ api: { ...api, getCredentialMetadata: () => gate.promise } })
    const pending = resultOf(reads[5].run(f.auth))
    await flushPromises()
    f.auth.projectModelSettings.abandonReads()
    await pending
    gate.reject(
      new AccountFailure(
        'problem',
        problemValue(status === 401 ? 'SESSION_REVOKED' : 'CSRF_FAILED', status),
      ),
    )
    await flushPromises()
    expect(f.auth.personalContext.identity?.sessionID).toBe(id(2))
    expect(f.auth.state.phase).toBe('authenticated')
  })
  it('strictly revalidates dependency receipts; malformed lookup cannot confirm a bad Execute', async () => {
    const api = createProjectModelSettingsAPI(async () => json({})),
      f = await fixture({
        api: {
          ...api,
          createProvider: async () =>
            ({ ...receipt(), version: '2' }) as Awaited<
              ReturnType<ProjectModelSettingsAPI['createProvider']>
            >,
          lookupConfiguration: async () =>
            ({ found: true, receipt: { ...receipt(), kind: 'model.create' } }) as Awaited<
              ReturnType<ProjectModelSettingsAPI['lookupConfiguration']>
            >,
        },
      })
    await resultOf(writes[0].run(f.auth))
    expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    await resultOf(f.auth.projectModelSettings.lookupConfiguration())
    expect(f.auth.projectModelSettings.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'failed',
      receipt: null,
    })
  })
  it('expired write late CSRF and finally cannot revoke restored authority or confirm a retired intent', async () => {
    const gate = barrier<never>(),
      api = createProjectModelSettingsAPI(async () => json({}))
    void gate.promise.catch(() => undefined)
    releases.push(() => gate.reject(new AccountFailure('cancelled')))
    const f = await fixture({ api: { ...api, createCredential: () => gate.promise } })
    vi.useFakeTimers()
    const original = resultOf(writes[6].run(f.auth))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expect(await original).toMatchObject({ error: { kind: 'cancelled' } })
    expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    f.auth.projectModelSettings.abandonPending()
    expect(f.auth.state.busy).toBe(true)
    gate.reject(new AccountFailure('problem', problemValue('CSRF_FAILED', 403)))
    await flushPromises()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.projectModelSettings.progress).toBeNull()
    f.setView(view(id(3)))
    await f.auth.restore()
    expect(f.auth.personalContext.identity?.sessionID).toBe(id(3))
    expect(f.auth.state.busy).toBe(false)
  })
  it('local model failure and abandonment leave Owner, Selection and Summary independent intents intact', async () => {
    const f = await fixture({ role: 'admin' })
    f.setOther(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    f.setOwner(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await resultOf(
      f.auth.projects.startUpdate(projectID, { expected_version: '1', description: 'Owner input' }),
    )
    await resultOf(
      f.auth.system.selection.start({
        kind: 'model.selection.update',
        id: id(50),
        expected_version: '1',
        embedding: id(51),
        memory: id(52),
        reranker: null,
        image: null,
      }),
    )
    await resultOf(
      f.auth.system.selection.meetingSummary.start({
        kind: 'model.selection.update',
        id: id(60),
        expected_version: '1',
        model: id(52),
      }),
    )
    expect(f.auth.projects.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    f.setModels(async () => problem('FORBIDDEN', 403))
    await resultOf(reads[0].run(f.auth))
    f.auth.projectModelSettings.abandonPending()
    expect(f.auth.projects.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
  })
})

describe('App-lifetime Project model coordinator', () => {
  it('initial leaf is one list only; independent paging failures retain a marked old page and exact retry', async () => {
    const f = await fixture(),
      page = await f.page()
    expect(f.calls().map(([path]) => path)).toEqual([`${prefix}/model-providers?limit=25`])
    f.setModels(async () =>
      json({
        items: Array.from({ length: 25 }, (_, i) => provider(projectID, 100 - i)),
        next_cursor: 'cursor A',
      }),
    )
    await page.providers.fromFirst()
    expect(page.providers.state.hasNext).toBe(true)
    f.setModels(async () => problem('CURSOR_INVALID', 400))
    await page.providers.next()
    expect(page.providers.state).toMatchObject({
      phase: 'error',
      stale: true,
      page: 1,
      cursorInvalid: true,
    })
    const failedPath = f.calls().at(-1)![0]
    await page.providers.retry()
    expect(f.calls().at(-1)![0]).toBe(failedPath)
    f.setModels(defaultModels)
    await page.providers.setLimit(50)
    expect(f.calls().at(-1)![0]).toBe(`${prefix}/model-providers?limit=50`)
    await page.showModels()
    expect(f.calls().at(-1)![0]).toBe(`${prefix}/models?limit=25`)
    expect(page.models.state.limit).toBe(25)
    expect(page.available.state.phase).toBe('waiting')
  })
  it('available directory is a safe mixed projection without per-row detail or System reads', async () => {
    const f = await fixture(),
      page = await f.page('available-models')
    expect(page.available.state.items.map((x) => x.scope.kind)).toEqual(['project', 'system'])
    expect(f.calls().map(([path]) => path)).toEqual([`${prefix}/available-chat-models?limit=25`])
    expect(f.fetch.mock.calls.some(([path]) => path.includes('/system/'))).toBe(false)
  })
  it('Owner basic draft/conflict does not authorize or disable this independent model domain', async () => {
    const f = await fixture(),
      page = await f.page()
    f.workspace.draft.name = 'Unsaved owner name'
    f.workspace.editor.conflict = true
    await page.newProvider()
    Object.assign(page.providerForm, providerInput(), { credential_ref: '' })
    expect(page.canSaveProvider.value).toBe(true)
    expect(await page.saveProvider()).toBe(true)
    expect(f.workspace.draft.name).toBe('Unsaved owner name')
    expect(f.workspace.editor.conflict).toBe(true)
  })
  it('no-op/invalid Provider is zero write; strict confirmation requires explicit subsequent read', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.readProvider(providerID)
    expect(page.canSaveProvider.value).toBe(false)
    await page.saveProvider()
    page.providerForm.name = ''
    expect(await page.saveProvider()).toBe(false)
    expect(f.writes()).toHaveLength(0)
    page.providerForm.name = 'Changed name'
    expect(await page.saveProvider()).toBe(true)
    expect(page.provider.requiresRead).toBe(true)
    expect(page.providerDirty.value).toBe(false)
    const writesBefore = f.writes().length
    f.setModels(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await page.readProvider(providerID, true)
    expect(page.progress.value?.phase).toBe('confirmed')
    expect(f.writes()).toHaveLength(writesBefore)
    expect(page.canSaveProvider.value).toBe(false)
  })
  it('full noneditable configuration remains visible and cannot be flattened into a write', async () => {
    const f = await fixture(),
      page = await f.page()
    const input = { ...providerInput(), options: { temperature: 0.5 } }
    f.setModels(async () => json({ ...provider(), input }))
    await page.readProvider(providerID)
    expect(page.provider.original?.input.options).toEqual({ temperature: 0.5 })
    expect(page.provider.supported).toBe(false)
    page.providerForm.name = 'New name'
    expect(page.canSaveProvider.value).toBe(false)
    await page.saveProvider()
    expect(f.writes()).toHaveLength(0)
  })
  it('version conflict keeps original input/version; only fresh Get plus explicit adopt rebases', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.readProvider(providerID)
    page.providerForm.name = 'Local input'
    f.setModels(async () => problem('VERSION_CONFLICT', 409))
    await page.saveProvider()
    expect(page.provider).toMatchObject({ conflict: true, requiresRead: true, version: '1' })
    expect(page.canAdoptProvider.value).toBe(false)
    f.setModels(async () =>
      json({
        ...provider(projectID, 20, '8'),
        input: { ...providerInput(), name: 'Current remote' },
      }),
    )
    await page.readProvider(providerID, true)
    expect(page.providerForm.name).toBe('Local input')
    expect(page.provider.version).toBe('1')
    expect(page.canAdoptProvider.value).toBe(true)
    const adopting = page.adoptProvider()
    expect(page.confirmation.open).toBe(true)
    page.confirm()
    expect(await adopting).toBe(true)
    expect(page.providerForm.name).toBe('Current remote')
    expect(page.provider.version).toBe('8')
    expect(f.writes()).toHaveLength(1)
  })
  it('Credential create, choose ref, Provider save are three explicit steps and metadata is reread before delete', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    Object.assign(page.providerForm, providerInput(), { credential_ref: '' })
    await page.openCredential('create')
    page.credentialValue.value = 'private material'
    expect(await page.createCredential()).toBe(true)
    expect(page.credentialValue.value).toBe('')
    expect(page.preparedCredential.value).toMatchObject({
      credential_id: credentialID,
      bound: false,
    })
    expect(f.writes()).toHaveLength(1)
    expect(page.providerForm.credential_ref).toBe('')
    expect(page.usePreparedCredential()).toBe(true)
    expect(f.writes()).toHaveLength(1)
    await page.readCredential()
    expect(page.credential.metadata?.version).toBe('1')
    expect(await page.saveProvider()).toBe(true)
    expect(f.writes()).toHaveLength(2)
    expect(page.preparedCredential.value?.bound).toBe(true)
    expect(page.credential.metadata).toBeNull()
    expect(await page.openDelete('credential')).toBe(false)
    await page.readCredential()
    expect(await page.openDelete('credential')).toBe(true)
    expect(f.writes()).toHaveLength(2)
  })
  it('Credential Unknown and historical observation never bind Provider or clear private replay material', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.openCredential('create')
    page.credentialValue.value = 'original private value'
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await page.createCredential()
    const original = f.writes()[0]![1]
    expect(page.credentialValue.value).toBe('')
    expect(page.preparedCredential.value).toBeNull()
    f.setModels(async () => json({ observed: true, result: mutation() }))
    await page.lookupOriginal()
    expect(page.progress.value).toMatchObject({ phase: 'uncertain', observation: 'observed' })
    expect(page.preparedCredential.value).toBeNull()
    expect(page.canCreateCredential.value).toBe(false)
    f.setModels(async () => json(mutation()))
    expect(await page.replayOriginal()).toBe(true)
    expect(f.writes().at(-1)![1].body).toBe(original.body)
    expect(page.preparedCredential.value?.bound).toBe(false)
    expect(f.writes().every(([path]) => !path.includes('/model-providers'))).toBe(true)
    expect(JSON.stringify(page.progress.value)).not.toContain('original private value')
  })
  it.each(['archiving', 'archived'] as const)(
    'S2 fresh %s Owner Get after checking permits configuration original Execute but only passive Credential recovery',
    async (lifecycle) => {
      const f = await fixture(),
        page = await f.page()
      await page.newProvider()
      Object.assign(page.providerForm, providerInput(), { credential_ref: '' })
      f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await page.saveProvider()
      expect(page.progress.value?.phase).toBe('uncertain')
      f.setProject(project(10, lifecycle))
      f.setModels(defaultModels)
      await f.auth.restore()
      await flushPromises()
      expect(page.visible.value).toBe(false)
      expect(page.canReplay.value).toBe(false)
      await page.readOwner()
      await flushPromises()
      expect(page.canMutate.value).toBe(false)
      expect(page.canReplay.value).toBe(true)
      expect(await page.replayOriginal()).toBe(true)
      const other = await fixture(),
        credentialPage = await other.page()
      await credentialPage.openCredential('create')
      credentialPage.credentialValue.value = 'private'
      other.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await credentialPage.createCredential()
      other.setProject(project(10, lifecycle))
      other.setModels(defaultModels)
      await other.auth.restore()
      await flushPromises()
      expect(credentialPage.visible.value).toBe(false)
      expect(credentialPage.canLookup.value).toBe(false)
      await credentialPage.readOwner()
      await flushPromises()
      expect(credentialPage.canReplay.value).toBe(false)
      expect(credentialPage.canLookup.value).toBe(true)
      const before = other.writes().length
      expect(await credentialPage.replayOriginal()).toBe(false)
      expect(other.writes()).toHaveLength(before)
      await credentialPage.lookupOriginal()
      expect(credentialPage.progress.value?.phase).toBe('uncertain')
    },
  )
  it('S2 same identity checking keeps Unknown hidden through a held fresh Owner Get before exact replay', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    Object.assign(page.providerForm, providerInput(), { credential_ref: '' })
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await page.saveProvider()
    const original = f.writes()[0]![1]
    const session = barrier<Response>(),
      owner = barrier<Response>(),
      priorRead = page.viewContext.value!.readGeneration,
      readsBefore = f.calls().length
    releases.push(
      () => session.resolve(json(view())),
      () => owner.resolve(json(project())),
    )
    f.setSession(() => session.promise)
    const restoring = f.auth.restore()
    await flushPromises()
    expect(page.visible.value).toBe(false)
    expect(page.providers.state.items).toHaveLength(0)
    expect(page.providerForm.name).toBe('Provider')
    expect(page.progress.value?.phase).toBe('uncertain')
    f.setModels(defaultModels)
    session.resolve(json(view()))
    await restoring
    await flushPromises()
    expect(page.visible.value).toBe(false)
    expect(page.viewContext.value).toBeNull()
    expect(f.calls()).toHaveLength(readsBefore)
    expect(await page.lookupOriginal()).toBe(false)
    expect(await page.replayOriginal()).toBe(false)
    expect(f.writes()).toHaveLength(1)
    f.setOwner((path) => {
      expect(path).toBe(`/api/v1/projects/${projectID}`)
      return owner.promise
    })
    const reading = page.readOwner()
    await flushPromises()
    expect(page.visible.value).toBe(false)
    expect(page.viewContext.value).toBeNull()
    expect(page.providerForm.name).toBe('Provider')
    expect(page.progress.value?.phase).toBe('uncertain')
    expect(f.calls()).toHaveLength(readsBefore)
    owner.resolve(json({ ...project(), version: '9' }))
    await reading
    await flushPromises()
    expect(page.visible.value).toBe(true)
    expect(page.viewContext.value!.readGeneration).toBeGreaterThan(priorRead)
    expect(page.providerForm.name).toBe('Provider')
    expect(await page.replayOriginal()).toBe(true)
    expect(f.writes().at(-1)![1].body).toBe(original.body)
    expect(new Headers(f.writes().at(-1)![1].headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
  })
  it('S2 failed fresh Owner Get retains dirty version, prepared ref and unsubmitted material until an explicit successful read', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.readProvider(providerID)
    page.providerForm.name = 'Keep the original draft'
    await page.openCredential('create')
    page.credentialValue.value = 'created private material'
    expect(await page.createCredential()).toBe(true)
    const prepared = page.preparedCredential.value
    expect(prepared?.credential_id).toBe(credentialID)
    page.credentialValue.value = 'unsubmitted private material'
    const modelCalls = f.calls().length,
      writes = f.writes().length,
      priorRead = page.viewContext.value!.readGeneration
    f.setOwner(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await f.auth.restore()
    await flushPromises()
    expect(page.visible.value).toBe(false)
    expect(page.viewContext.value).toBeNull()
    expect(f.calls()).toHaveLength(modelCalls)
    await page.readOwner()
    expect(f.workspace.detail.phase).toBe('read-error')
    expect(page.visible.value).toBe(false)
    expect(page.providerForm.name).toBe('Keep the original draft')
    expect(page.provider.version).toBe('1')
    expect(page.preparedCredential.value).toEqual(prepared)
    expect(page.credentialValue.value).toBe('unsubmitted private material')
    expect(f.calls()).toHaveLength(modelCalls)
    f.setOwner(async () => json({ ...project(), version: '19' }))
    f.setModels(async (path, init) =>
      init.method === 'GET' && path.includes('/model-providers')
        ? json({ items: [provider(projectID, 20, '7')], next_cursor: null })
        : defaultModels(path, init),
    )
    await page.readOwner()
    await flushPromises()
    expect(page.visible.value).toBe(true)
    expect(page.viewContext.value!.readGeneration).toBeGreaterThan(priorRead)
    expect(page.currentProject.value?.version).toBe('19')
    expect(page.providers.state.items[0]?.version).toBe('7')
    expect(page.providerForm.name).toBe('Keep the original draft')
    expect(page.provider.version).toBe('1')
    expect(page.preparedCredential.value).toEqual(prepared)
    expect(page.credentialValue.value).toBe('unsubmitted private material')
    expect(f.writes()).toHaveLength(writes)
  })
  it('S2 navigation clearing a confirmed local draft does not waive the fresh Owner Get gate', async () => {
    const f = await fixture(),
      page = await f.page(),
      target = '/owner/main/settings/available-models'
    await page.newProvider()
    page.providerForm.name = 'Discard only this draft'
    const moving = page.confirmLeave(target)
    page.confirm()
    expect(await moving).toBe(true)
    const before = f.calls().length
    await f.auth.restore()
    f.workspace.afterNavigation(target)
    page.afterNavigation(target)
    await flushPromises()
    expect(page.providerForm.name).toBe('')
    expect(page.visible.value).toBe(false)
    expect(page.viewContext.value).toBeNull()
    expect(f.calls()).toHaveLength(before)
    await page.readOwner()
    await flushPromises()
    expect(page.visible.value).toBe(true)
    expect(page.available.state.items).toHaveLength(2)
    expect(page.providerForm.name).toBe('')
    expect(f.writes()).toHaveLength(0)
  })
  it.each(['session', 'csrf', 'user'] as const)(
    'real %s identity change clears all material/drafts/observations',
    async (change) => {
      const f = await fixture(),
        page = await f.page()
      await page.openCredential('create')
      page.credentialValue.value = 'private retained'
      f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
      await page.createCredential()
      await flushPromises()
      const next = view(change === 'session' ? id(3) : id(2))
      if (change === 'csrf') next.csrf_token = 'T'.repeat(43)
      if (change === 'user') next.user.id = id(4)
      f.setView(next)
      f.setModels(defaultModels)
      await f.auth.restore()
      await flushPromises()
      expect(page.progress.value).toBeNull()
      expect(page.credentialValue.value).toBe('')
      expect(page.credential.open).toBe(false)
      expect(page.preparedCredential.value).toBeNull()
      expect(await page.replayOriginal()).toBe(false)
    },
  )
  it('local denied model response hides data until explicit Owner read; actual Owner loss clears draft', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    page.providerForm.name = 'Keep this input'
    f.setModels(async () => problem('NOT_FOUND', 404))
    await page.providers.fromFirst()
    expect(page.visible.value).toBe(false)
    expect(page.providerForm.name).toBe('Keep this input')
    expect(f.auth.system.denied).toBe(false)
    f.setOwner(async () => problem('FORBIDDEN', 403))
    await page.readOwner()
    expect(page.visible.value).toBe(false)
    expect(page.providerForm.name).toBe('')
    expect(page.dirty.value).toBe(false)
  })
  it('navigation cancellation retains draft and beforeunload, while confirmed leave clears only this domain', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    page.providerForm.name = 'Unsaved draft'
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    const cancelled = page.confirmLeave('/owner/main/settings/available-models')
    page.cancelConfirmation()
    expect(await cancelled).toBe(false)
    expect(page.providerForm.name).toBe('Unsaved draft')
    const accepted = page.confirmLeave('/system')
    page.confirm()
    expect(await accepted).toBe(true)
    expect(page.dirty.value).toBe(false)
    expect(page.providerForm.name).toBe('')
    expect(f.writes()).toHaveLength(0)
  })
  it('pending new-editor microtask cannot open a draft after leaving the Project', async () => {
    const f = await fixture(),
      page = await f.page()
    const opening = page.newProvider()
    page.afterNavigation('/system')
    expect(await opening).toBe(false)
    expect(page.provider.open).toBe(false)
    expect(page.visible.value).toBe(false)
    expect(page.viewContext.value).toBeNull()
  })
  it('same-Project canonical route update preserves drafts and the current read generation', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    page.providerForm.name = 'Retained draft'
    const context = page.viewContext.value,
      requests = f.calls().length
    expect(await page.confirmLeave('/OWNER/MAIN/settings/model-providers')).toBe(true)
    page.afterNavigation('/OWNER/MAIN/settings/model-providers')
    expect(page.viewContext.value).toBe(context)
    expect(page.providerForm.name).toBe('Retained draft')
    expect(f.calls()).toHaveLength(requests)
  })
  it('Model deletion requires explicit null or an available candidate; current Get failure does not block captured DELETE replay', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.readModel(modelID)
    expect(await page.openDelete('model')).toBe(true)
    expect(await page.deleteSelected()).toBe(false)
    expect(f.writes()).toHaveLength(0)
    page.deletion.replacement = ''
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await page.deleteSelected()
    const original = f.writes()[0]![1]
    expect(JSON.parse(String(original.body))).toEqual({ expected_version: '1', replacement: null })
    f.setModels(async () => json(receipt('model.delete')))
    expect(await page.replayOriginal()).toBe(true)
    expect(f.writes().at(-1)![1].body).toBe(original.body)
    expect(page.deletion.open).toBe(false)
  })
  it('Model protocol context stays local, no-op is blocked, and fresh review requires explicit adoption', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.readModel(modelID)
    expect(
      f
        .calls()
        .slice(-2)
        .map(([path]) => path),
    ).toEqual([`${prefix}/models/${modelID}`, `${prefix}/model-providers/${providerID}`])
    expect(page.canSaveModel.value).toBe(false)
    page.modelForm.provider_model_id = 'changed-native'
    f.setModels(async () => problem('VERSION_CONFLICT', 409))
    await page.saveModel()
    expect(page.model).toMatchObject({ conflict: true, requiresRead: true, version: '1' })
    const submitted = JSON.parse(String(f.writes()[0]![1].body))
    expect(Object.keys(submitted).sort()).toEqual(['expected_version', 'input'])
    f.setModels(async (path) =>
      json(path.includes('/models/') ? model(projectID, '3') : provider()),
    )
    await page.readModel(modelID, true)
    expect(page.modelForm.provider_model_id).toBe('changed-native')
    expect(page.canAdoptModel.value).toBe(true)
    const adopting = page.adoptModel()
    page.confirm()
    expect(await adopting).toBe(true)
    expect(page.model.version).toBe('3')
    expect(page.modelForm.provider_model_id).toBe('native-chat')
    expect(f.writes()).toHaveLength(1)
  })
  it('Credential rotation is explicit and empty material is a no-op; RESOURCE_BUSY never auto-unbinds', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.openCredential('manage', credentialID)
    expect(page.credential.metadata?.version).toBe('1')
    expect(page.canRotateCredential.value).toBe(false)
    await page.rotateCredential()
    expect(f.writes()).toHaveLength(0)
    page.credentialValue.value = 'new private material'
    expect(await page.rotateCredential()).toBe(true)
    expect(page.credential.metadata).toBeNull()
    expect(page.credentialValue.value).toBe('')
    await page.readCredential()
    await page.openDelete('credential')
    f.setModels(async () => problem('RESOURCE_BUSY', 409))
    expect(await page.deleteSelected()).toBe(false)
    expect(page.progress.value?.phase).toBe('rejected')
    expect(f.writes()).toHaveLength(2)
    expect(f.writes().every(([path]) => path.includes('/model-credentials/'))).toBe(true)
  })
  it('explicit abandonment clears only local Unknown, without writes or compensating credential deletion', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.openCredential('create')
    page.credentialValue.value = 'private'
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await page.createCredential()
    const cancel = page.abandonPending()
    page.cancelConfirmation()
    expect(await cancel).toBe(false)
    expect(page.progress.value?.phase).toBe('uncertain')
    const abandon = page.abandonPending()
    page.confirm()
    expect(await abandon).toBe(true)
    expect(page.progress.value).toBeNull()
    expect(page.credentialValue.value).toBe('')
    expect(f.writes()).toHaveLength(1)
    expect(await page.replayOriginal()).toBe(false)
  })
  it('stable Project replacement under the same route cannot inherit the old draft', async () => {
    const f = await fixture(),
      page = await f.page()
    await page.newProvider()
    page.providerForm.name = 'Old Project draft'
    f.setProject({ ...project(11), name: 'Main', normalized_name: 'main' })
    // The real workspace resolves the same address again after the current
    // Project becomes unavailable. No caller supplies a new read grant.
    f.setOwner(async () => problem('NOT_FOUND', 404))
    await page.readOwner()
    expect(page.providerForm.name).toBe('')
    f.setOwner(null)
    f.workspace.afterNavigation('/owner/main/settings/model-providers')
    await flushPromises()
    expect(page.providerForm.name).toBe('')
    expect(page.progress.value).toBeNull()
  })
})
