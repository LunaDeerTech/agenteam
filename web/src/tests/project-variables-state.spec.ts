import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectVariablesAPI } from '../api/project-variables'
import { type CommitState, type Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectVariables } from '../composables/useProjectVariables'
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-09T12:00:00.000001Z',
  projectID = id(10),
  target = id(20),
  path = '/owner/project.name/settings/variables'
const view = (): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: at, idle_expires_at: at, absolute_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
const project = (): Project => ({
  id: projectID,
  owner_user_id: id(1),
  name: 'project.name',
  normalized_name: 'project.name',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
})
const summary = (n = 0) => ({
  id: id(n + 20),
  project_id: projectID,
  type: 'variable',
  name: `NAME_${String(n).padStart(3, '0')}`,
  description: '',
  version: '1',
  created_at: at,
  updated_at: at,
})
const variable = () => ({ ...summary(), value: 'old value' })
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(90),
    },
  })
const problem = (code: string, status = 409, commit_state: CommitState = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Test',
      detail: 'private detail never render',
      instance: '/api/v1/projects',
      code,
      status,
      commit_state,
      request_id: id(90),
    },
    status,
  )
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const cleanups: (() => void)[] = []
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup()
})
async function settle() {
  for (let i = 0; i < 4; i++) await flushPromises()
}
async function fixture() {
  let current = view(),
    owner = project(),
    liveVariable = variable(),
    variableCall: Fetch | null = null,
    sessionCall: Fetch | null = null,
    ownerCall: Fetch | null = null
  const receipts: unknown[] = []
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') return sessionCall ? sessionCall(url, init) : json(current)
    if (url.includes('/variables')) {
      if (variableCall) return variableCall(url, init)
      if (init.method === 'GET')
        return json(url.includes('/variables?') ? { items: [summary()] } : liveVariable)
      if (url.endsWith('/commands/lookup'))
        return json({ status: 'committed', receipt: receipts.at(-1) })
      const body = JSON.parse(String(init.body))
      const kind = init.method === 'POST' ? 'create' : init.method === 'PATCH' ? 'update' : 'delete'
      const nextVersion = kind === 'create' ? '1' : String(BigInt(body.expected_version) + 1n)
      const value =
        kind === 'create'
          ? {
              ...variable(),
              name: body.request.name,
              description: body.request.description,
              value: body.request.value,
              id: body.request.variable_id,
              version: nextVersion,
            }
          : { ...liveVariable, ...body.request, version: nextVersion }
      const receipt = {
        command: `project.variable.${kind}`,
        changed: true,
        event_id: id(80),
        audit_id: id(81),
        ...(kind === 'delete'
          ? {
              deleted: {
                id: target,
                project_id: projectID,
                type: 'variable',
                version: nextVersion,
                deleted_at: at,
              },
            }
          : { variable: value }),
      }
      if (kind !== 'delete') liveVariable = value
      receipts.push(receipt)
      return json(receipt)
    }
    if (url.startsWith('/api/v1/projects/')) return ownerCall ? ownerCall(url, init) : json(owner)
    throw new Error('unexpected fixture request')
  })
  const auth = createSessionController(
    createAccountAPI(fetcher),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetcher),
    undefined,
    undefined,
    createProjectVariablesAPI(fetcher),
  )
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  const page = createProjectVariables(auth, workspace)
  cleanups.push(() => {
    page.dispose()
    workspace.dispose()
    auth.leave()
  })
  function navigate(to: string) {
    workspace.afterNavigation(to, path)
    page.afterNavigation(to, path)
  }
  navigate(path)
  await settle()
  return {
    auth,
    workspace,
    page,
    fetcher,
    navigate,
    receipts,
    setSession(value: SessionView) {
      current = value
    },
    setProject(value: Project) {
      owner = value
    },
    setVariable(value: ReturnType<typeof variable>) {
      liveVariable = value
    },
    variableCall(value: Fetch | null) {
      variableCall = value
    },
    sessionCall(value: Fetch | null) {
      sessionCall = value
    },
    ownerCall(value: Fetch | null) {
      ownerCall = value
    },
  }
}
const variableCalls = (fetcher: ReturnType<typeof vi.fn<Fetch>>) =>
  fetcher.mock.calls.filter(([p]) => p.includes('/variables'))

describe('Variables page consumes current Project owner and six actual API methods', () => {
  it('binds through actual Resolve/Get before summary and only reads value on selection', async () => {
    const { page, fetcher } = await fixture()
    expect(page.visible.value).toBe(true)
    expect(page.page.phase).toBe('ready')
    expect(variableCalls(fetcher)).toHaveLength(1)
    expect(page.page.items[0]).not.toHaveProperty('value')
    expect(fetcher.mock.calls.slice(1, 3).every(([p]) => !p.includes('/variables'))).toBe(true)
    await page.select(target)
    expect(page.draft.value).toBe('old value')
    expect(page.editor.version).toBe('1')
    expect(variableCalls(fetcher)).toHaveLength(2)
  })
  it('preserves server order and opaque cursors; a bad last item publishes no partial page', async () => {
    const f = await fixture()
    let secondBad = false
    f.variableCall(async (url) => {
      if (url.includes('cursor='))
        return secondBad
          ? json({ items: [summary(50), { ...summary(51), value: 'forbidden' }] })
          : json({ items: [summary(50)] })
      return json({
        items: Array.from({ length: 50 }, (_, n) => summary(n)),
        next_cursor: 'opaque+/=字',
      })
    })
    await f.page.firstPage()
    expect(f.page.page.hasNext).toBe(true)
    const first = f.page.page.items
    secondBad = true
    await f.page.nextPage()
    expect(f.page.page.items).toBe(first)
    expect(f.page.page.page).toBe(1)
    expect(f.page.page.stale).toBe(true)
    secondBad = false
    await f.page.nextPage()
    expect(f.page.page.page).toBe(2)
    expect(variableCalls(f.fetcher).at(-1)![0]).toContain('cursor=opaque%2B%2F%3D%E5%AD%97')
    await f.page.previousPage()
    expect(f.page.page.page).toBe(1)
    expect(variableCalls(f.fetcher).at(-1)![0]).toMatch(/\?limit=50$/)
  })
  it('expired cursor never silently restarts at page one', async () => {
    const f = await fixture()
    let calls = 0
    f.variableCall(async () =>
      ++calls === 1
        ? json({ items: Array.from({ length: 50 }, (_, n) => summary(n)), next_cursor: 'next' })
        : problem('CURSOR_STALE'),
    )
    await f.page.firstPage()
    await f.page.nextPage()
    expect(calls).toBe(2)
    expect(f.page.page.cursorInvalid).toBe(true)
    expect(f.page.page.items).toHaveLength(50)
    await f.page.nextPage()
    expect(calls).toBe(2)
  })
  it('unchanged form makes no mutation; changed empty presence preserves expected version', async () => {
    const f = await fixture()
    await f.page.select(target)
    const before = f.fetcher.mock.calls.length
    expect(await f.page.save()).toBe(false)
    expect(f.fetcher).toHaveBeenCalledTimes(before)
    f.page.draft.value = ''
    expect(f.page.canSave.value).toBe(true)
    expect(await f.page.save()).toBe(true)
    const [, init] = variableCalls(f.fetcher).at(-1)!
    expect(init.method).toBe('PATCH')
    expect(JSON.parse(String(init.body))).toEqual({ expected_version: '1', request: { value: '' } })
    expect(f.page.progress.value?.phase).toBe('confirmed')
    expect(f.page.editor.requiresRead).toBe(true)
    expect(f.page.page.stale).toBe(true)
  })
  it('validates create UUID/name without trimming and permits empty description/value', async () => {
    const f = await fixture()
    await f.page.createNew()
    f.page.draft.name = ' AGENTEAM_X'
    const count = variableCalls(f.fetcher).length
    expect(await f.page.save()).toBe(false)
    expect(variableCalls(f.fetcher)).toHaveLength(count)
    f.page.draft.name = 'CUSTOM'
    expect(await f.page.save()).toBe(true)
    const body = JSON.parse(String(variableCalls(f.fetcher).at(-1)![1].body))
    expect(body.request).toMatchObject({ name: 'CUSTOM', description: '', value: '' })
    expect(body.request.variable_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    )
  })
  it('delete requires cancelable confirmation and the exact read version; history survives current absence', async () => {
    const f = await fixture()
    await f.page.select(target)
    let result = f.page.deleteSelected()
    expect(f.page.confirmation.message).toContain(target)
    expect(f.page.confirmation.message).toContain('版本 1')
    f.page.cancelConfirmation()
    expect(await result).toBe(false)
    result = f.page.deleteSelected()
    f.page.confirm()
    expect(await result).toBe(true)
    expect(JSON.parse(String(variableCalls(f.fetcher).at(-1)![1].body))).toEqual({
      expected_version: '1',
    })
    expect(f.page.editor.open).toBe(false)
    expect(f.page.progress.value?.receipt?.command).toBe('project.variable.delete')
    await f.page.lookupOriginal()
    expect(f.page.progress.value?.phase).toBe('confirmed')
    f.variableCall(async () => problem('NOT_FOUND', 404))
    await f.page.readOriginalTarget()
    expect(f.page.editor.missing).toBe(true)
    expect(f.page.progress.value?.phase).toBe('confirmed')
  })
  it('version conflict preserves draft until explicit re-read and adoption, never rebases automatically', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = 'my draft'
    f.variableCall(async (_url, init) =>
      init.method === 'PATCH'
        ? problem('VERSION_CONFLICT')
        : json({ ...variable(), value: 'current other', version: '2' }),
    )
    await f.page.save()
    expect(f.page.editor.conflict).toBe(true)
    expect(f.page.draft.value).toBe('my draft')
    expect(f.page.editRejected()).toBe(true)
    await f.page.readSelected()
    expect(f.page.editor.version).toBe('1')
    expect(f.page.editor.review?.version).toBe('2')
    expect(f.page.canSave.value).toBe(false)
    expect(f.page.adoptCurrent(true)).toBe(true)
    expect(f.page.editor.version).toBe('2')
    expect(f.page.draft.value).toBe('my draft')
    expect(f.page.canSave.value).toBe(true)
  })
  it('does not infer uncertain mutation from current value or not_observed, and never retries automatically', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = ''
    f.variableCall(async (url, init) =>
      url.endsWith('/lookup')
        ? json({ status: 'not_observed', receipt: null })
        : init.method === 'GET'
          ? json({ ...variable(), value: '', version: '2' })
          : Promise.reject(new TypeError('lost')),
    )
    await f.page.save()
    expect(f.page.progress.value?.phase).toBe('uncertain')
    const count = variableCalls(f.fetcher).length
    await settle()
    expect(variableCalls(f.fetcher)).toHaveLength(count)
    await f.page.readSelected()
    await f.page.lookupOriginal()
    expect(f.page.progress.value?.phase).toBe('uncertain')
    expect(f.page.editor.version).toBe('1')
    expect(f.page.adoptCurrent()).toBe(false)
    expect(f.page.editRejected()).toBe(false)
  })
  it('same identity checking hides contents and retains draft; actual new session destroys them', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = 'draft survives check'
    const held = barrier<Response>()
    f.sessionCall(() => held.promise)
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.draft.value).toBe('draft survives check')
    held.resolve(json(view()))
    await restore
    f.sessionCall(null)
    await settle()
    expect(f.page.visible.value).toBe(true)
    expect(f.page.draft.value).toBe('draft survives check')
    expect(f.page.editor.requiresRead).toBe(true)
    f.setSession({ ...view(), session: { ...view().session, id: id(3) } })
    await f.auth.restore()
    await settle()
    expect(f.page.editor.open).toBe(false)
    expect(f.page.draft.value).toBe('')
  })
  it.each(['archiving', 'archived'] as const)(
    '%s allows read but not new mutations',
    async (lifecycle) => {
      const f = await fixture()
      f.setProject({ ...project(), lifecycle, archived_at: lifecycle === 'archived' ? at : null })
      await f.workspace.readCurrent()
      await settle()
      expect(f.page.visible.value).toBe(true)
      expect(f.page.canMutate.value).toBe(false)
      expect(await f.page.createNew()).toBe(false)
      await f.page.select(target)
      expect(f.page.draft.value).toBe('old value')
      f.page.draft.value = 'new'
      expect(await f.page.save()).toBe(false)
    },
  )
  it('same identity recovery requires a fresh Owner Get before archived history becomes usable', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = 'committed before archive'
    await f.page.save()
    const original = f.page.progress.value?.receipt
    const held = barrier<Response>()
    const ownerCalls = vi.fn<Fetch>(() => held.promise)
    f.ownerCall(ownerCalls)
    const before = variableCalls(f.fetcher).length
    await f.auth.restore()
    await settle()
    expect(ownerCalls).toHaveBeenCalledTimes(1)
    expect(ownerCalls.mock.calls[0]![0]).toBe(`/api/v1/projects/${projectID}`)
    expect(f.page.visible.value).toBe(false)
    expect(f.page.canMutate.value).toBe(false)
    expect(f.page.canLookup.value).toBe(false)
    expect(f.page.canReplay.value).toBe(false)
    expect(variableCalls(f.fetcher)).toHaveLength(before)
    expect(f.page.progress.value?.receipt).toEqual(original)
    held.resolve(json({ ...project(), lifecycle: 'archived', archived_at: at }))
    await settle()
    expect(f.page.visible.value).toBe(true)
    expect(f.page.canMutate.value).toBe(false)
    expect(f.page.canLookup.value).toBe(true)
    expect(f.page.canReplay.value).toBe(true)
    expect(f.page.progress.value?.receipt).toEqual(original)
  })
  it.each([
    ['INTERNAL_ERROR', 500],
    ['FORBIDDEN', 403],
  ] as const)(
    'Owner requalification %s keeps Variables hidden until an explicit successful read',
    async (code, status) => {
      const f = await fixture()
      await f.page.select(target)
      f.page.draft.value = 'retained draft'
      const ownerCalls = vi.fn<Fetch>(async () => problem(code, status))
      f.ownerCall(ownerCalls)
      const before = variableCalls(f.fetcher).length
      await f.auth.restore()
      await settle()
      expect(ownerCalls).toHaveBeenCalledTimes(1)
      expect(f.page.visible.value).toBe(false)
      expect(f.page.canMutate.value).toBe(false)
      expect(f.page.canSave.value).toBe(false)
      expect(await f.page.createNew()).toBe(false)
      expect(variableCalls(f.fetcher)).toHaveLength(before)
      expect(f.page.draft.value).toBe('retained draft')
      await settle()
      expect(ownerCalls).toHaveBeenCalledTimes(1)
      f.ownerCall(null)
      await f.page.readOwner()
      await settle()
      expect(f.page.visible.value).toBe(true)
      expect(f.page.draft.value).toBe('retained draft')
      expect(f.page.editor.requiresRead).toBe(true)
      expect(f.page.canSave.value).toBe(false)
    },
  )
  it('late Owner requalification cannot republish after leaving the identity', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = 'old identity draft'
    const held = barrier<Response>()
    f.ownerCall(() => held.promise)
    await f.auth.restore()
    await settle()
    expect(f.auth.state.busy).toBe(true)
    expect(f.page.visible.value).toBe(false)
    f.auth.leave()
    expect(f.page.draft.value).toBe('')
    expect(f.page.editor.open).toBe(false)
    held.resolve(json(project()))
    await settle()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.canMutate.value).toBe(false)
    expect(f.workspace.detail.project).toBeNull()
    const newOwner = vi.fn<Fetch>(async () =>
      json({ ...project(), lifecycle: 'archived', archived_at: at }),
    )
    f.ownerCall(newOwner)
    f.setSession({ ...view(), session: { ...view().session, id: id(3) } })
    await f.auth.restore()
    await settle()
    expect(newOwner).toHaveBeenCalledTimes(2)
    expect(f.page.visible.value).toBe(true)
    expect(f.page.canMutate.value).toBe(false)
    expect(f.page.draft.value).toBe('')
  })
  it('leaving with dirty input is cancelable and accepted leave clears only Variables', async () => {
    const f = await fixture()
    await f.page.createNew()
    f.page.draft.name = 'DRAFT'
    const oldOwnerDraft = f.workspace.draft.name
    let leaving = f.page.confirmLeave('/projects')
    expect(f.page.confirmation.open).toBe(true)
    f.page.cancelConfirmation()
    expect(await leaving).toBe(false)
    expect(f.page.draft.name).toBe('DRAFT')
    leaving = f.page.confirmLeave('/projects')
    f.page.confirm()
    expect(await leaving).toBe(true)
    expect(f.page.draft.name).toBe('')
    expect(f.workspace.draft.name).toBe(oldOwnerDraft)
  })
  it('same stable Project canonical rename preserves draft without inventing a new navigation confirmation', async () => {
    const f = await fixture()
    await f.page.select(target)
    f.page.draft.value = 'draft'
    f.setProject({ ...project(), name: 'Renamed', normalized_name: 'renamed', version: '2' })
    await f.workspace.readCurrent()
    await settle()
    expect(await f.page.confirmLeave('/owner/renamed/settings/variables')).toBe(true)
    expect(f.page.confirmation.open).toBe(false)
    expect(f.page.draft.value).toBe('draft')
  })
  it('scope cancellation keeps actual owner until fetch returns and cannot publish a late old object', async () => {
    const f = await fixture(),
      entered = barrier(),
      release = barrier<Response>()
    let signal: AbortSignal | undefined
    f.variableCall(async (_url, init) => {
      signal = init.signal!
      entered.resolve()
      return release.promise
    })
    const selected = f.page.select(target)
    await entered.promise
    f.navigate('/owner/project.name/settings/general')
    await settle()
    expect(signal!.aborted).toBe(true)
    expect(f.auth.state.busy).toBe(true)
    expect(f.page.visible.value).toBe(false)
    release.resolve(json(variable()))
    await selected
    await settle()
    expect(f.auth.state.busy).toBe(false)
    expect(f.page.editor.original).toBeNull()
  })
  it.each([
    '/owner/project.name/settings/variables?x=1',
    '/owner/project.name/settings/%76ariables',
    '/owner/project.name/settings/variables/extra',
  ])('raw invalid route makes zero new variable requests: %s', async (value) => {
    const f = await fixture(),
      count = variableCalls(f.fetcher).length
    f.navigate(value)
    await settle()
    expect(f.page.visible.value).toBe(false)
    expect(variableCalls(f.fetcher)).toHaveLength(count)
  })
})
