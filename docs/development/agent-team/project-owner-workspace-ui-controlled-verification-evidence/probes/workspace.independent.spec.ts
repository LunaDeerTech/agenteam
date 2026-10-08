import { describe, expect, it } from 'vitest'
import { barrier, fixture, flushPromises, id, json, problem, project, session } from './fixture'

describe('independent Project state and recovery contracts', () => {
  it.each(['/owner/%64emo', '/owner/demo/settings?return=/projects', '/owner/demo#section', '/settings/unknown', '/api/v1', '/owner/demo/extra', '/owner/../settings'])('raw %s produces zero Project requests', async (path) => {
    const f = await fixture()
    await f.open(path)
    expect(f.requests()).toHaveLength(0)
    expect(f.workspace.detail.project).toBeNull()
  })

  it.each([403, 404])('Resolve is unpublished location only and subsequent Get %i removes all content', async (status) => {
    const f = await fixture()
    const resolve = barrier(json(project())), get = barrier(json(project()))
    f.intercept((path, init) => path.includes('/resolve?') ? resolve.promise : path === '/api/v1/projects/' + id(10) ? get.promise : f.normal(path, init))
    f.workspace.afterNavigation('/owner/demo')
    await flushPromises()
    expect(f.requests()).toHaveLength(1)
    expect(f.workspace.detail.project).toBeNull()
    resolve.resolve(json(project({ description: 'Resolve must never publish' })))
    await flushPromises()
    expect(f.requests().map(([path]) => path)).toEqual(['/api/v1/projects/resolve?username=owner&project_name=demo', '/api/v1/projects/' + id(10)])
    expect(f.workspace.detail.project).toBeNull()
    expect(f.workspace.paths.value.home).toBe('')
    get.resolve(problem(status === 403 ? 'FORBIDDEN' : 'NOT_FOUND', status))
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.workspace.detail.project).toBeNull()
    expect(f.workspace.editor.ready).toBe(false)
    expect(f.auth.system.denied).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('an abandoned route generation waits for the old actual Resolve tail before fresh Resolve/Get', async () => {
    const f = await fixture()
    const tail = barrier(json(project()))
    f.intercept((path, init) => path.includes('project_name=demo') ? tail.promise : f.normal(path, init))
    f.workspace.afterNavigation('/owner/demo/settings/general')
    await flushPromises()
    f.current(project({ id: id(11), name: 'Other', normalized_name: 'other', description: 'new object' }))
    f.workspace.afterNavigation('/owner/other/settings/general')
    await flushPromises()
    expect(f.requests()).toHaveLength(1)
    expect(f.auth.state.busy).toBe(true)
    expect(f.workspace.detail.project).toBeNull()
    tail.resolve(json(project({ description: 'old object private content' })))
    await flushPromises()
    expect(f.requests().map(([path]) => path)).toEqual([
      '/api/v1/projects/resolve?username=owner&project_name=demo',
      '/api/v1/projects/resolve?username=owner&project_name=other',
      '/api/v1/projects/' + id(11),
    ])
    expect(f.workspace.detail.project?.id).toBe(id(11))
    expect(f.workspace.draft.description).toBe('new object')
  })

  it('a conflict requires fresh Get then explicit confirmed adoption without rewriting expected_version', async () => {
    const f = await fixture()
    await f.open()
    f.workspace.draft.description = 'keep my input'
    f.intercept((path, init) => init.method === 'PATCH' ? Promise.resolve(problem('VERSION_CONFLICT', 409, 'not_committed')) : f.normal(path, init))
    await f.workspace.save()
    expect(f.workspace.editor.conflict).toBe(true)
    expect(f.workspace.editor.requiresRead).toBe(true)
    expect(f.workspace.editor.version).toBe('1')
    expect(f.workspace.draft.description).toBe('keep my input')
    expect(f.workspace.canAdoptCurrent.value).toBe(false)
    f.current(project({ version: '3', description: 'concurrent current' }))
    await f.workspace.readCurrent()
    expect(f.workspace.editor.version).toBe('1')
    expect(f.workspace.draft.description).toBe('keep my input')
    expect(f.workspace.detail.project?.version).toBe('3')
    const cancel = f.workspace.adoptCurrent()
    expect(f.workspace.confirmation.open).toBe(true)
    f.workspace.finishConfirmation(false)
    await cancel
    expect(f.workspace.draft.description).toBe('keep my input')
    const adopt = f.workspace.adoptCurrent()
    f.workspace.finishConfirmation(true)
    await adopt
    expect(f.workspace.draft.description).toBe('concurrent current')
    expect(f.workspace.editor.version).toBe('3')
    expect(f.workspace.editor.conflict).toBe(false)
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string).expected_version).toBe('1')
  })

  it('sticky unknown and not_observed preserve original key/body, while in_progress prevents replay', async () => {
    const f = await fixture()
    await f.open()
    f.workspace.draft.description = 'first exact intent'
    f.intercept(async (_path, init) => init.method === 'PATCH' ? problem('COMMIT_UNKNOWN', 503, 'unknown') : json({ state: 'in_progress' }))
    await f.workspace.save()
    const first = f.writes()[0]![1]
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    f.workspace.draft.description = 'later local modification'
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.observation).toBe('in_progress')
    expect(f.workspace.canReplay.value).toBe(false)
    await f.workspace.replayOriginal()
    expect(f.writes()).toHaveLength(1)
    f.intercept(async (_path, init) => init.method === 'PATCH' ? problem('INVALID_ARGUMENT', 400, 'not_committed') : json({ state: 'not_observed' }))
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    expect(f.workspace.progress.value?.observation).toBe('not_observed')
    expect(f.workspace.canReplay.value).toBe(true)
    await f.workspace.replayOriginal()
    const replay = f.writes()[1]![1]
    expect(replay.body).toBe(first.body)
    expect(new Headers(replay.headers).get('Idempotency-Key')).toBe(new Headers(first.headers).get('Idempotency-Key'))
    expect(f.workspace.progress.value?.phase).toBe('uncertain')
    expect(f.workspace.draft.description).toBe('later local modification')
    const exposed = JSON.stringify({ progress: f.workspace.progress.value, detail: f.workspace.detail, editor: f.workspace.editor, draft: f.workspace.draft })
    expect(exposed).not.toContain(new Headers(first.headers).get('Idempotency-Key'))
    expect(exposed).not.toContain('S'.repeat(43))
  })

  it('lookup historical receipt cannot supply canonical name; successful current Get owns navigation', async () => {
    const f = await fixture()
    await f.open()
    f.workspace.draft.description = 'original intent'
    f.intercept(async (path, init) => init.method === 'PATCH' ? problem('COMMIT_UNKNOWN', 503, 'unknown') : f.normal(path, init))
    await f.workspace.save()
    const historical = project({ name: 'Historical', normalized_name: 'historical', description: 'original intent', version: '2' })
    const present = project({ name: 'Current', normalized_name: 'current', description: 'later edit', version: '3' })
    f.current(present)
    f.intercept(async (path, init) => path.endsWith('/commands/lookup') ? json({ state: 'committed', result: { command: 'update', project: historical } }) : f.normal(path, init))
    await f.workspace.checkOriginal()
    expect(f.workspace.progress.value?.receipt?.name).toBe('Historical')
    expect(f.workspace.detail.project).toEqual(present)
    expect(f.replacements).toEqual(['/owner/current/settings/general'])
    expect(f.workspace.editor.version).toBe('3')
    expect(f.writes()).toHaveLength(1)
  })

  it('a valid no-op receipt at the original version is confirmed without invented increment', async () => {
    const f = await fixture()
    f.intercept(async () => json(project()))
    await expect(f.auth.projects.startUpdate(id(10), { expected_version: '1', description: 'original' })).resolves.toMatchObject({ version: '1' })
    expect(f.auth.projects.progress).toMatchObject({ phase: 'confirmed', receipt: { version: '1' }, contextValid: false, canRetryOriginal: false })
    expect(f.writes()).toHaveLength(1)
    expect(f.requests()).toHaveLength(1)
  })

  it.each(['same', 'session', 'csrf'] as const)('Session checking %s preserves only the complete same identity draft', async (change) => {
    const f = await fixture()
    await f.open()
    f.workspace.draft.description = 'private unsaved text'
    const next = session()
    if (change === 'session') next.session = { ...next.session, id: id(3) }
    if (change === 'csrf') next.csrf_token = 'T'.repeat(43)
    const tail = barrier(json(next))
    f.intercept((path, init) => path === '/api/v1/session' ? tail.promise : f.normal(path, init))
    const restoring = f.auth.restore()
    await flushPromises()
    expect(f.workspace.visible.value).toBe(false)
    expect(f.workspace.draft.description).toBe('private unsaved text')
    tail.resolve(json(next))
    await restoring
    await flushPromises()
    expect(f.workspace.visible.value).toBe(true)
    expect(f.workspace.draft.description).toBe(change === 'same' ? 'private unsaved text' : 'original')
    expect(f.writes()).toHaveLength(0)
  })

  it('same-Project suffix navigation requests confirmation, and a repeated leave cannot silently discard', async () => {
    const f = await fixture()
    await f.open()
    f.workspace.draft.description = 'dirty'
    const first = f.workspace.confirmLeave('/owner/demo')
    expect(f.workspace.confirmation.open).toBe(true)
    expect(await f.workspace.confirmLeave('/projects')).toBe(false)
    f.workspace.finishConfirmation(false)
    expect(await first).toBe(false)
    expect(f.workspace.draft.description).toBe('dirty')
    expect(f.writes()).toHaveLength(0)
  })
})
