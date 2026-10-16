import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
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
import { createProjectAuditAPI, type ProjectAuditAPI } from '../api/project-audit'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectAudit, type ProjectAuditController } from '../composables/useProjectAudit'
import { useTheme } from '../composables/useTheme'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-08T12:34:56.123456Z',
  projectID = id(10),
  endpoint = `/api/v1/projects/${projectID}/audit`
const view = (sessionID = id(2), role: 'admin' | 'user' = 'admin'): SessionView => ({
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
  session: { id: sessionID, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
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
const record = (n = 100, ownerProject = projectID) => ({
  audit_id: id(n),
  created_at: time,
  scope: 'project',
  project_id: ownerProject,
  actor: { kind: 'human', id: id(1) },
  action: 'secret.create',
  outcome: 'success',
  resource: { kind: 'secret', id: id(3) },
  metadata: { version: '1', changed_fields: ['value'] },
  associations: {},
  summary: 'Secret created',
})
const rows = (count = 1, start = 100, cursor: string | null = null, ownerProject = projectID) => ({
  items: Array.from({ length: count }, (_, i) => record(start - i, ownerProject)),
  next_cursor: cursor,
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problemValue = (code: string, status = 503, commit_state: CommitState = 'not_started') => ({
  type: 'urn:agenteam:problem:test',
  title: 'Failure',
  status,
  code,
  detail: '',
  instance: endpoint,
  request_id: id(9),
  commit_state,
  retry_hint: 'lookup' as const,
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
  pages: ProjectAuditController[] = [],
  releases: (() => void)[] = []
function nativeHold(media = 'application/json', value?: unknown) {
  const entered = barrier(),
    joined = barrier()
  let control!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      control = c
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
    control,
    stream,
    release,
  }
}
const smtpFields = () => ({
  host: 'mail.example',
  port: 587,
  encryption: 'starttls' as const,
  username: '',
  sender_email: 'sender@example.test',
  sender_name: '',
  auto_retry_count: '3',
  retry_interval_seconds: '60',
})
const smtpSettings = () => ({
  id: id(20),
  version: '1',
  configured: true,
  ...smtpFields(),
  credential_present: false,
})
async function fixture(
  options: {
    role?: 'user' | 'admin'
    bind?: boolean
    lifecycle?: Project['lifecycle']
    audit?: ProjectAuditAPI
  } = {},
) {
  let current = view(id(2), options.role ?? 'admin'),
    currentProject = project(10, options.lifecycle)
  let session: Fetch = async () => json(current)
  let audit: Fetch = async (path) =>
    json(
      path.split('?')[0]!.split('/').length === 7
        ? record(100, path.split('/')[4]!)
        : rows(1, 100, null, path.split('/')[4]!),
    )
  let other: Fetch = async (path) =>
    path === '/api/v1/me'
      ? json({ user: current.user, avatar: null })
      : path === '/api/v1/system/smtp'
        ? json(smtpSettings())
        : json({ items: [], next_cursor: null })
  let ownerRead: Fetch | null = null
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    if (/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path)) return audit(path, init)
    if (path.startsWith('/api/v1/projects/resolve?'))
      return ownerRead ? ownerRead(path, init) : json(currentProject)
    if (path.startsWith('/api/v1/projects/'))
      return ownerRead
        ? ownerRead(path, init)
        : init.method === 'GET'
          ? json(currentProject)
          : problem('COMMIT_UNKNOWN', 503, 'unknown')
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
    options.audit ?? createProjectAuditAPI(fetch),
  )
  owners.push(auth)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspaces.push(workspace)
  if (options.bind !== false) {
    workspace.afterNavigation('/owner/main/settings/audit')
    await flushPromises()
    expect(workspace.detail.phase).toBe('current')
  }
  return {
    auth,
    workspace,
    fetch,
    page() {
      const value = useProjectAudit(auth, workspace)
      pages.push(value)
      return value
    },
    setAudit(value: Fetch) {
      audit = value
    },
    setOther(value: Fetch) {
      other = value
    },
    setOwner(value: Fetch | null) {
      ownerRead = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    setView(value: SessionView) {
      current = value
    },
    setProject(value: Project) {
      currentProject = value
    },
    reads() {
      return fetch.mock.calls.filter(([path]) =>
        /^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path),
      )
    },
    writes() {
      return fetch.mock.calls.filter(([, init]) => init.method !== 'GET')
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

const domains = [
  {
    name: 'personal',
    read: (a: SessionController) => a.personal.getProfile(),
    abandon: (a: SessionController) => a.personal.abandon(),
  },
  {
    name: 'directory',
    read: (a: SessionController) => a.system.listUsers({}),
    abandon: (a: SessionController) => a.system.abandon(),
  },
  {
    name: 'invitation',
    read: (a: SessionController) => a.system.listInvitations({}),
    abandon: (a: SessionController) => a.system.abandonInvitationRead(),
  },
  {
    name: 'provider',
    read: (a: SessionController) => a.system.providers.list({}),
    abandon: (a: SessionController) => a.system.providers.abandonRead('provider-list'),
  },
  {
    name: 'model',
    read: (a: SessionController) =>
      a.system.models.get({ id: id(3), provider_id: id(4), protocol: 'openai-chat-completions' }),
    abandon: (a: SessionController) => a.system.models.abandonRead('model-detail'),
  },
  {
    name: 'selection',
    read: (a: SessionController) => a.system.selection.get(),
    abandon: (a: SessionController) => a.system.selection.abandonRead('selection-state'),
  },
  {
    name: 'summary',
    read: (a: SessionController) => a.system.selection.meetingSummary.get(),
    abandon: (a: SessionController) =>
      a.system.selection.meetingSummary.abandonRead('meeting-summary-state'),
  },
  {
    name: 'account-security',
    read: (a: SessionController) => a.system.accountSecurity.get(),
    abandon: (a: SessionController) => a.system.accountSecurity.abandonRead(),
  },
  {
    name: 'smtp',
    read: (a: SessionController) => a.system.smtp.get(),
    abandon: (a: SessionController) => a.system.smtp.abandonRead(),
  },
  {
    name: 'delivery',
    read: (a: SessionController) => a.system.smtpDelivery.list(),
    abandon: (a: SessionController) => a.system.smtpDelivery.abandonRead(),
  },
  {
    name: 'outbound',
    read: (a: SessionController) => a.system.outboundPolicy.get(),
    abandon: (a: SessionController) => a.system.outboundPolicy.abandonRead(),
  },
  {
    name: 'system-audit',
    read: (a: SessionController) => a.system.audit.list({}),
    abandon: (a: SessionController) => a.system.audit.abandon(),
  },
  {
    name: 'runtime',
    read: (a: SessionController) => a.system.runtimeInformation.get(),
    abandon: (a: SessionController) => a.system.runtimeInformation.abandon(),
  },
  {
    name: 'owner',
    read: (a: SessionController) => a.projects.get(projectID),
    abandon: (a: SessionController) => a.projects.abandonRead(),
  },
] as const

describe('independent Human Project Audit Session domain', () => {
  it.each(['user', 'admin'] as const)(
    'accepts current %s Human without an admin shortcut or System-denied gate',
    async (role) => {
      const f = await fixture({ role })
      expect((await f.auth.projectAudit.list(projectID, {})).items).toHaveLength(1)
      expect((await f.auth.projectAudit.get(projectID, id(100))).audit_id).toBe(id(100))
      expect(f.writes()).toHaveLength(0)
      if (role === 'admin') {
        f.setOther(async () => problem('FORBIDDEN', 403))
        await resultOf(f.auth.system.listUsers({}))
        expect(f.auth.system.denied).toBe(true)
        expect((await f.auth.projectAudit.list(projectID, {})).items).toHaveLength(1)
      }
    },
  )
  it.each([
    [403, 'PERMISSION_DENIED'],
    [404, 'NOT_FOUND'],
    [403, 'CSRF_FAILED'],
    [401, 'PERMISSION_DENIED'],
    [403, 'UNAUTHENTICATED'],
    [503, 'SESSION_REVOKED'],
    [503, 'COMMIT_UNKNOWN'],
    [500, 'INTERNAL'],
  ])('keeps local %s/%s out of global identity and write recovery', async (status, code) => {
    const f = await fixture()
    f.setAudit(async () =>
      problem(String(code), Number(status), code === 'COMMIT_UNKNOWN' ? 'unknown' : 'not_started'),
    )
    await resultOf(f.auth.projectAudit.list(projectID, {}))
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.system.denied).toBe(false)
    expect(f.auth.projects.progress).toBeNull()
    expect(f.writes()).toHaveLength(0)
  })
  it.each(['UNAUTHENTICATED', 'SESSION_REVOKED'])(
    'only current 401/%s invalidates the Session',
    async (code) => {
      const f = await fixture()
      f.setAudit(async () => problem(code, 401))
      await resultOf(f.auth.projectAudit.list(projectID, {}))
      expect(f.auth.state.phase).toBe('unavailable')
      expect(f.auth.personalContext.identity).toBeNull()
    },
  )
  it('ignores a retired domain revision late 401 even while full identity is unchanged', async () => {
    const gate = barrier<never>()
    void gate.promise.catch(() => undefined)
    releases.push(() => gate.reject(new AccountFailure('cancelled')))
    const api: ProjectAuditAPI = { list: () => gate.promise, get: () => gate.promise }
    const f = await fixture({ audit: api })
    const pending = resultOf(f.auth.projectAudit.list(projectID, {}))
    await flushPromises()
    f.auth.projectAudit.abandon()
    await pending
    gate.reject(new AccountFailure('problem', problemValue('SESSION_REVOKED', 401)))
    await flushPromises()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.identity?.sessionID).toBe(id(2))
    expect(f.auth.state.busy).toBe(false)
  })
  it('captures invalid input before scheduling work and never sends lookup despite an unknown read', async () => {
    const f = await fixture()
    for (const request of [
      () => f.auth.projectAudit.list('bad', {}),
      () => f.auth.projectAudit.get(projectID, 'bad'),
      () => f.auth.projectAudit.list(projectID, { actor_kind: 'service', actor_id: id(1) }),
    ])
      await expect(request()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.reads()).toHaveLength(0)
    f.setAudit(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    const p = f.page()
    await flushPromises()
    expect(p.state.list.phase).toBe('error')
    await flushPromises()
    expect(f.reads()).toHaveLength(1)
    expect(f.fetch.mock.calls.some(([path]) => path.includes('lookup'))).toBe(false)
  })
  it.each(domains)('does not cross the actual $name tail or abandon its owner', async (domain) => {
    const f = await fixture(),
      held = nativeHold('text/plain')
    f.setOther(async () => held.response())
    f.setOwner(async () => held.response())
    const original = resultOf(domain.read(f.auth))
    await held.entered.promise
    f.auth.projectAudit.abandon()
    await expect(f.auth.projectAudit.list(projectID, {})).rejects.toMatchObject({ kind: 'busy' })
    await expect(f.auth.projectAudit.get(projectID, id(100))).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.reads()).toHaveLength(0)
    domain.abandon(f.auth)
    await original
    expect(f.auth.state.busy).toBe(true)
    held.release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect((await f.auth.projectAudit.list(projectID, {})).items).toHaveLength(1)
  })
  it.each(['list', 'detail'] as const)(
    'holds every old Cookie domain through a cancelled %s native tail',
    async (kind) => {
      const f = await fixture(),
        held = nativeHold('application/json', kind === 'list' ? rows() : record())
      f.setAudit(async () => held.response())
      const pending = resultOf(
        kind === 'list'
          ? f.auth.projectAudit.list(projectID, {})
          : f.auth.projectAudit.get(projectID, id(100)),
      )
      await flushPromises()
      f.auth.projectAudit.abandon()
      await pending
      await held.entered.promise
      const before = f.fetch.mock.calls.length
      for (const domain of domains)
        await expect(domain.read(f.auth)).rejects.toMatchObject({ kind: 'busy' })
      await f.auth.logout()
      await f.auth.restore()
      await f.auth.login('owner@example.test', 'Long enough password for login')
      expect(f.fetch.mock.calls).toHaveLength(before)
      expect(f.auth.state.busy).toBe(true)
      held.release()
      await flushPromises()
      expect(f.auth.state.busy).toBe(false)
      expect(held.stream.locked).toBe(false)
    },
  )
  it('keeps the 30s visible bound separate from actual cancellation completion', async () => {
    const f = await fixture(),
      held = nativeHold('application/json', rows())
    f.setAudit(async () => held.response())
    vi.useFakeTimers()
    const pending = resultOf(f.auth.projectAudit.list(projectID, {}))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(30000)
    expect(await pending).toMatchObject({ error: { kind: 'cancelled' } })
    await held.entered.promise
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.projects.get(projectID)).rejects.toMatchObject({ kind: 'busy' })
    held.release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
  })
  it('does not destroy pending Owner Update material through Audit failure or abandon', async () => {
    const f = await fixture({ role: 'user' })
    await resultOf(
      f.auth.projects.startUpdate(projectID, {
        expected_version: '1',
        description: 'Original exact body',
      }),
    )
    expect(f.auth.projects.progress?.phase).toBe('uncertain')
    const original = f.writes()[0]![1]
    f.setAudit(async () => problem('PERMISSION_DENIED', 403))
    await resultOf(f.auth.projectAudit.list(projectID, {}))
    f.auth.projectAudit.abandon()
    expect(f.auth.projects.progress?.phase).toBe('uncertain')
    await resultOf(f.auth.projects.retryOriginal())
    const retried = f.writes().at(-1)![1]
    expect(retried.body).toBe(original.body)
    expect(new Headers(retried.headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
  })
  it('keeps both independent Selection and Summary pending intents during local Audit rejection', async () => {
    const f = await fixture()
    f.setOther(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await resultOf(
      f.auth.system.selection.start({
        kind: 'model.selection.update',
        id: id(30),
        expected_version: '1',
        embedding: id(31),
        memory: id(32),
        reranker: null,
        image: null,
      }),
    )
    await resultOf(
      f.auth.system.selection.meetingSummary.start({
        kind: 'model.selection.update',
        id: id(40),
        expected_version: '1',
        model: id(32),
      }),
    )
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    const originals = f.writes().map(([, init]) => ({
      body: init.body,
      key: new Headers(init.headers).get('Idempotency-Key'),
    }))
    expect(originals).toHaveLength(2)
    expect(originals[0]?.key).not.toBe(originals[1]?.key)
    f.setAudit(async () => problem('FORBIDDEN', 403))
    await resultOf(f.auth.projectAudit.list(projectID, {}))
    f.auth.projectAudit.abandon()
    expect(f.auth.system.denied).toBe(false)
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    expect(f.writes()).toHaveLength(2)
    expect(f.auth.system.selection.progress?.contextValid).toBe(true)
    expect(f.auth.system.selection.meetingSummary.progress?.contextValid).toBe(true)
  })
  it('preserves exact pending SMTP credential material and excludes Audit during its actual tail', async () => {
    const f = await fixture(),
      gate = barrier<Response>()
    f.setOther(async () => json(smtpSettings()))
    await f.auth.system.smtp.get()
    f.auth.system.smtp.setMaterial('  Exact private credential  ')
    let originalSignal: AbortSignal | null | undefined
    f.setOther(async (_path, init) => {
      originalSignal = init.signal
      return gate.promise
    })
    releases.push(() => gate.resolve(problem('COMMIT_UNKNOWN', 503, 'unknown')))
    const pending = resultOf(
      f.auth.system.smtp.startUpdate({
        version: '1',
        ...smtpFields(),
        username: 'smtp-user',
        credential_action: 'keep',
      }),
    )
    await flushPromises()
    const present = f.auth.system.smtp.material.present
    f.auth.projectAudit.abandon()
    await expect(f.auth.projectAudit.list(projectID, {})).rejects.toMatchObject({ kind: 'busy' })
    expect(originalSignal?.aborted).toBe(false)
    expect(f.auth.system.smtp.material.present).toBe(present)
    gate.resolve(problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await pending
    await flushPromises()
    expect(f.auth.system.smtp.progress?.phase).toBe('uncertain')
    const original = f.writes()[0]![1]
    f.setAudit(async () => problem('NOT_FOUND', 404))
    await resultOf(f.auth.projectAudit.get(projectID, id(100)))
    f.auth.projectAudit.abandon()
    f.setOther(async (_path, init) =>
      init.method === 'GET' ? json(smtpSettings()) : problem('COMMIT_UNKNOWN', 503, 'unknown'),
    )
    await f.auth.system.smtp.checkOriginal()
    await resultOf(f.auth.system.smtp.retryOriginal())
    expect(f.writes().at(-1)![1].body).toBe(original.body)
    expect(new Headers(f.writes().at(-1)![1].headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
  })
})

describe('workspace-bound Project Audit page lifetime', () => {
  it.each(['active', 'archiving', 'archived'] as const)(
    'binds only complete current Get, while %s remains readable',
    async (lifecycle) => {
      const f = await fixture({ role: 'user', lifecycle })
      const context = f.workspace.currentReadContext.value
      expect(context?.projectID).toBe(projectID)
      expect(Object.isFrozen(context) && Object.isFrozen(context?.identity)).toBe(true)
      const p = f.page()
      await flushPromises()
      expect(p.state.list.phase).toBe('ready')
      expect(f.reads()).toHaveLength(1)
    },
  )
  it('never treats Resolve alone or a failed current Get as Audit authority', async () => {
    const f = await fixture({ bind: false }),
      gate = barrier<Response>()
    f.setOwner(async (path) => (path.includes('/resolve?') ? json(project()) : gate.promise))
    releases.push(() => gate.resolve(problem('DEPENDENCY_UNAVAILABLE', 503)))
    f.workspace.afterNavigation('/owner/main/settings/audit')
    const p = f.page()
    await flushPromises()
    expect(f.workspace.currentReadContext.value).toBeNull()
    expect(f.reads()).toHaveLength(0)
    gate.resolve(problem('DEPENDENCY_UNAVAILABLE', 503))
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('read-error')
    expect(f.workspace.currentReadContext.value).toBeNull()
    expect(p.state.list.phase).toBe('waiting')
    f.setOwner(null)
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.reads()).toHaveLength(1)
  })
  it('allows initial Owner Get to finish publishing its General draft before Audit claims the next Cookie owner', async () => {
    const f = await fixture({ bind: false }),
      gate = barrier<Response>(),
      held = nativeHold('application/json', rows())
    f.setOwner(async (path) => (path.includes('/resolve?') ? json(project()) : gate.promise))
    releases.push(() => gate.resolve(json(project())))
    f.setAudit(async () => held.response())
    const p = f.page()
    f.workspace.afterNavigation('/owner/main/settings/audit')
    await flushPromises()
    gate.resolve(json(project()))
    await flushPromises()
    expect(p.state.list.phase).toBe('loading')
    expect(f.workspace.draft.name).toBe('Main')
    expect(f.workspace.draft.description).toBe('Safe description')
    expect(f.workspace.editor.ready).toBe(true)
  })
  it('keeps filter drafts inert, applies all query fields explicitly, and preserves bad input', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    p.updateFilter('tool_id', 'not-an-id')
    await p.apply()
    expect(p.draft.tool_id).toBe('not-an-id')
    expect(p.state.fieldErrors.tool_id).toBeTruthy()
    expect(f.reads()).toHaveLength(1)
    const values = {
      from: '0000-01-01T00:00:00Z',
      to: '9999-12-31T23:59:59.999999Z',
      actor_kind: 'agent_run',
      actor_id: id(2),
      action: 'account.login',
      outcome: 'denied',
      resource_kind: 'session',
      resource_id: id(3),
      tool_id: id(4),
      execution_id: id(5),
      operation_id: id(6),
      approval_id: id(7),
      runner_id: id(8),
      agent_id: id(9),
      limit: '25',
    }
    for (const [key, value] of Object.entries(values))
      p.updateFilter(key as keyof typeof p.draft, value)
    expect(f.reads()).toHaveLength(1)
    f.setAudit(async () => json(rows(0)))
    await p.apply()
    expect(p.state.list.phase).toBe('empty')
    expect(new URL(f.reads().at(-1)![0], 'https://local.invalid').searchParams.size).toBe(15)
    expect(Object.isFrozen(p.state.applied)).toBe(true)
    await p.reset()
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
  })
  it('uses explicit cursor history, rejects a cursor failure locally and offers only a fresh first page', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    p.updateFilter('limit', '1')
    f.setAudit(async (path) =>
      json(
        rows(1, path.includes('cursor=') ? 90 : 100, path.includes('cursor=') ? null : 'page.two'),
      ),
    )
    await p.apply()
    await p.next()
    expect(p.state.list.page).toBe(2)
    expect(p.state.list.hasPrevious).toBe(true)
    await p.previous()
    expect(p.state.list.page).toBe(1)
    f.setAudit(async () => problem('CURSOR_INVALID', 400))
    await p.next()
    expect(p.state.list.cursorInvalid).toBe(true)
    const count = f.reads().length
    await p.retry()
    expect(f.reads()).toHaveLength(count)
    f.setAudit(async () => json(rows(0)))
    await p.refresh()
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=1')
  })
  it('details always GET independently; 404 is not an empty list and back performs no list GET', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    f.setAudit(async () => problem('NOT_FOUND', 404))
    await p.openDetail(id(100))
    expect(p.state.detail.phase).toBe('not-found')
    expect(p.state.detail.record).toBeNull()
    const count = f.reads().length
    p.backToList()
    expect(f.reads()).toHaveLength(count)
    expect(p.state.list.items).toHaveLength(1)
    expect(p.state.detail.target).toBeNull()
  })
  it.each(['list', 'detail'] as const)(
    'clears %s candidates on cancel, retains only explicit retry and waits for native tail',
    async (kind) => {
      const f = await fixture(),
        p = f.page()
      await flushPromises()
      const held = nativeHold('application/json', kind === 'list' ? rows() : record())
      f.setAudit(async () => held.response())
      const pending = kind === 'list' ? p.refresh() : p.openDetail(id(100))
      await flushPromises()
      p.cancel()
      await pending
      await held.entered.promise
      expect((kind === 'list' ? p.state.list : p.state.detail).phase).toBe('error')
      expect(p.blocked.value).toBe(true)
      const count = f.reads().length
      held.release()
      await flushPromises()
      expect(f.reads()).toHaveLength(count)
      f.setAudit(async () => json(kind === 'list' ? rows() : record()))
      if (kind === 'list') await p.retry()
      else await p.retryDetail()
      expect((kind === 'list' ? p.state.list : p.state.detail).phase).toBe('ready')
    },
  )
  it.each(['new-page', 'cancelled-wait', 'disposed', 'left'] as const)(
    'spends initial eligibility exactly once for %s waiting behind a previous tail',
    async (mode) => {
      const f = await fixture(),
        held = nativeHold('text/plain')
      f.setAudit(async () => held.response())
      const old = resultOf(f.auth.projectAudit.list(projectID, {}))
      await held.entered.promise
      f.auth.projectAudit.abandon()
      await old
      const p = f.page()
      expect(p.state.list.phase).toBe('waiting')
      if (mode === 'cancelled-wait') p.cancel()
      if (mode === 'disposed') p.dispose()
      if (mode === 'left') p.leave()
      f.setAudit(async () => json(rows(0)))
      held.release()
      await flushPromises()
      expect(f.reads()).toHaveLength(mode === 'new-page' ? 2 : 1)
      if (mode === 'cancelled-wait' || mode === 'left') {
        await p.retry()
        expect(f.reads()).toHaveLength(2)
      }
    },
  )
  it('does not resurrect a cancelled initial wait when Owner Get subsequently grants the first context', async () => {
    const f = await fixture({ bind: false }),
      p = f.page()
    p.cancel()
    f.workspace.afterNavigation('/owner/main/settings/audit')
    await flushPromises()
    expect(f.reads()).toHaveLength(0)
    expect(p.state.list.phase).toBe('error')
    await p.retry()
    expect(f.reads()).toHaveLength(1)
  })
  it('clears all private page state through checking/503 and only a new same-Session instance can restart', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    p.updateFilter('action', 'secret.create')
    await p.apply()
    p.updateFilter('tool_id', id(9))
    f.setSessionRequest(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await f.auth.restore()
    await flushPromises()
    expect(p.state.list.phase).toBe('inactive')
    expect(p.draft.tool_id).toBe('')
    expect(p.draft.action).toBe('')
    expect(p.state.detail.target).toBeNull()
    expect(f.workspace.currentReadContext.value).toBeNull()
    const count = f.reads().length
    f.setSessionRequest(async () => json(view()))
    await f.auth.restore()
    await flushPromises()
    await p.refresh()
    expect(f.reads()).toHaveLength(count)
    const next = f.page()
    await flushPromises()
    expect(next.state.list.phase).toBe('ready')
    expect(f.reads()).toHaveLength(count + 1)
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
  })
  it('invalidates old project scope/filters/cursor on parameter reuse and waits for stable new Get', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    p.updateFilter('action', 'secret.create')
    await p.apply()
    p.updateFilter('tool_id', id(4))
    const oldStamp = p.focusGeneration(),
      oldContext = f.workspace.currentReadContext.value
    f.setProject(project(11))
    p.leave()
    f.workspace.afterNavigation('/owner/second/settings/audit')
    expect(p.state.list.items).toEqual([])
    expect(p.state.detail.target).toBeNull()
    await flushPromises()
    expect(f.workspace.currentReadContext.value?.projectID).toBe(id(11))
    expect(f.workspace.currentReadContext.value?.generation).not.toBe(oldContext?.generation)
    expect(p.draft.action).toBe('')
    expect(p.draft.tool_id).toBe('')
    expect(p.isCurrent(oldStamp)).toBe(false)
    expect(p.state.list.items[0]?.project_id).toBe(id(11))
    expect(f.reads().at(-1)![0]).toBe(`/api/v1/projects/${id(11)}/audit?limit=50`)
  })
  it('makes a cancelled navigation explicitly recoverable while retiring late publication and focus', async () => {
    const f = await fixture(),
      p = f.page()
    await flushPromises()
    const held = nativeHold('application/json', rows())
    f.setAudit(async () => held.response())
    const pending = p.refresh()
    const stamp = p.focusGeneration()
    await flushPromises()
    p.leave()
    await pending
    await held.entered.promise
    expect(p.state.list.phase).toBe('error')
    expect(p.isCurrent(stamp)).toBe(false)
    const count = f.reads().length
    held.release()
    await flushPromises()
    expect(f.reads()).toHaveLength(count)
    f.setAudit(async () => json(rows()))
    await p.retry()
    expect(p.state.list.phase).toBe('ready')
  })
})
