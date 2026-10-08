import { describe, expect, it, vi } from 'vitest'
import { barrier, cleanup, fixture, flushPromises, id, json, problem, project, session } from './fixture'
import { createSystemModelSelection } from '/workspace/agenteam/web/src/composables/useSystemModelSelection'

const oldMutators = [
  ['profile', (a: any) => a.personal.updateProfile({ version: '1', display_name: 'x' })],
  ['preferences', (a: any) => a.personal.setPreferences({ version: '1', theme: 'dark' })],
  ['avatar put', (a: any) => a.personal.putAvatar({ version: '1', file: new File([], 'x.png'), mediaType: 'image/png' })],
  ['avatar delete', (a: any) => a.personal.deleteAvatar({ version: '1' })],
  ['password', (a: any) => a.personal.changePassword({})],
  ['invitation', (a: any) => a.system.createInvitation({ email: 'other@example.test' })],
  ['provider', (a: any) => a.system.providers.start({})],
  ['model', (a: any) => a.system.models.start({})],
  ['platform selection', (a: any) => a.system.selection.start({})],
  ['summary selection', (a: any) => a.system.selection.meetingSummary.start({})],
  ['account security', (a: any) => a.system.accountSecurity.start({})],
  ['SMTP settings', (a: any) => a.system.smtp.startUpdate({})],
  ['SMTP delivery', (a: any) => a.system.smtpDelivery.startTest({})],
  ['outbound policy', (a: any) => a.system.outboundPolicy.startUpdate({})],
] as const

describe('independent shared owner and identity tests', () => {
  it.each(['resolve', 'reject'] as const)('body cancel %s keeps all 14 old mutators, Logout and restore blocked until actual tail', async (ending) => {
    const f = await fixture(true)
    const cancel = barrier<void>(undefined), entered = barrier<void>(undefined)
    const body = new ReadableStream<Uint8Array>({
      start(stream) { stream.enqueue(new Uint8Array([1])) },
      async cancel() { entered.resolve(); await cancel.promise; if (ending === 'reject') throw new Error('controlled cancel rejected') },
    })
    const response = new Response(body, { headers: { 'Content-Type': 'text/plain' } })
    f.intercept(async (path, init) => path.startsWith('/api/v1/projects/') ? response : f.normal(path, init))
    const reading = f.auth.projects.get(id(10)).catch((error) => error)
    await entered.promise
    f.auth.projects.abandonRead()
    expect((await reading).kind).toBe('cancelled')
    expect(body.locked).toBe(false) // Content-Type rejection uses native response.body.cancel.
    expect(f.auth.state.busy).toBe(true)
    const before = f.fetcher.mock.calls.length
    for (const [name, mutate] of oldMutators) {
      const failure = await mutate(f.auth).catch((error: unknown) => error)
      expect(failure, name).toMatchObject({ kind: 'busy' })
    }
    await f.auth.logout()
    await f.auth.restore()
    expect(f.fetcher.mock.calls.length).toBe(before)
    expect(f.auth.state.busy).toBe(true)
    cancel.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.intercept()
    await expect(f.auth.projects.get(id(10))).resolves.toEqual(project())
  })

  it('reverse ownership refuses a Project update without retaining a private intent', async () => {
    const f = await fixture()
    const tail = barrier(json({ user: session().user, avatar: null }))
    f.intercept((path, init) => path === '/api/v1/me' ? tail.promise : f.normal(path, init))
    const reading = f.auth.personal.getProfile()
    await flushPromises()
    await expect(f.auth.projects.startUpdate(id(10), { expected_version: '1', description: 'x' })).rejects.toMatchObject({ kind: 'busy' })
    expect(f.auth.projects.progress).toBeNull()
    tail.resolve(json({ user: session().user, avatar: null }))
    await reading
    await expect(f.auth.projects.startUpdate(id(10), { expected_version: '1', description: 'x' })).resolves.toMatchObject({ description: 'x', version: '2' })
  })

  it.each([403, 404])('Project %i is isolated while independent System denial still permits Owner', async (status) => {
    const f = await fixture(true)
    f.intercept(async (path, init) => path.startsWith('/api/v1/projects/') ? problem(status === 403 ? 'FORBIDDEN' : 'NOT_FOUND', status) : f.normal(path, init))
    await expect(f.auth.projects.get(id(10))).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.system.denied).toBe(false)
    await expect(f.auth.system.listUsers({})).resolves.toEqual({ items: [] })
    f.intercept(async (path, init) => path.startsWith('/api/v1/system/users') ? problem('FORBIDDEN', 403) : f.normal(path, init))
    await expect(f.auth.system.listUsers({})).rejects.toBeDefined()
    expect(f.auth.system.denied).toBe(true)
    await expect(f.auth.projects.get(id(10))).resolves.toEqual(project())
    expect(f.auth.system.denied).toBe(true)
  })

  it.each([['UNAUTHENTICATED', 401], ['CSRF_FAILED', 403]] as const)('current %s invalidates one shared identity', async (code, status) => {
    const f = await fixture(true)
    f.intercept(async () => problem(code, status))
    await expect(f.auth.projects.get(id(10))).rejects.toBeDefined()
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.workspace.visible.value).toBe(false)
  })

  it.each(['success', '401', '403'])('old %s tail after identity leave cannot publish or poison next Session', async (ending) => {
    const f = await fixture()
    const tail = barrier(json(project()))
    f.intercept((path, init) => path.includes('/resolve?') ? tail.promise : f.normal(path, init))
    f.workspace.afterNavigation('/owner/demo')
    await flushPromises()
    const oldSignal = f.requests()[0]![1].signal!
    f.auth.leave()
    expect(oldSignal.aborted).toBe(true)
    expect(f.auth.state.busy).toBe(true)
    const before = f.fetcher.mock.calls.length
    await f.auth.restore()
    expect(f.fetcher.mock.calls.length).toBe(before)
    const late = ending === 'success' ? json(project()) : problem(ending === '401' ? 'UNAUTHENTICATED' : 'FORBIDDEN', Number(ending))
    tail.resolve(late)
    await flushPromises()
    expect(f.workspace.detail.project).toBeNull()
    f.workspace.afterNavigation('/projects')
    const next = session()
    next.session = { ...next.session, id: id(3) }
    f.session(next)
    f.intercept()
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.identity?.sessionID).toBe(id(3))
    expect(f.workspace.list.phase).toBe('current')
  })

  it('Project local abandon and confirmation leave the two old Selection intents independent; aggregate cancellation retains both', async () => {
    const f = await fixture(true)
    await f.open()
    const selection = createSystemModelSelection(f.auth)
    cleanup(() => selection.dispose())
    f.intercept(async (path, init) => path.startsWith('/api/v1/system/model-selection') ? Promise.reject(new Error('controlled unknown')) : f.normal(path, init))
    await expect(f.auth.system.selection.start({ kind: 'model.selection.update', id: id(40), expected_version: '1', embedding: id(41), memory: id(42), reranker: null, image: null })).rejects.toBeDefined()
    await expect(f.auth.system.selection.meetingSummary.start({ kind: 'model.selection.update', id: id(43), expected_version: '1', model: id(42) })).rejects.toBeDefined()
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    f.workspace.draft.description = 'dirty Project'
    const leaveProject = f.workspace.confirmLeave('/projects')
    f.workspace.finishConfirmation(true)
    expect(await leaveProject).toBe(true)
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    const aggregate = selection.confirmLeave()
    expect(selection.confirmation.open).toBe(true)
    expect(selection.confirmation.message).toContain('四项平台用途')
    expect(selection.confirmation.message).toContain('会议 Summary')
    selection.finishConfirmation(false)
    expect(await aggregate).toBe(false)
    expect(f.auth.system.selection.progress?.phase).toBe('uncertain')
    expect(f.auth.system.selection.meetingSummary.progress?.phase).toBe('uncertain')
    const accepted = selection.confirmLeave()
    selection.finishConfirmation(true)
    expect(await accepted).toBe(true)
    expect(f.auth.system.selection.progress).toBeNull()
    expect(f.auth.system.selection.meetingSummary.progress).toBeNull()
  })
})
