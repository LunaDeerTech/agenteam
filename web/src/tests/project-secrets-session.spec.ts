import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { effectScope, shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectSecrets } from '../composables/useProjectSecrets'
import {
  base,
  barrier,
  fixture,
  id,
  json,
  metadata,
  problem,
  project,
  projectID,
  receipt,
  session,
  target,
} from './project-secrets-fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) cleanup()
  await flushPromises()
  vi.useRealTimers()
})
async function owner() {
  const f = fixture()
  cleanups.push(() => f.auth.leave())
  await f.auth.restore()
  return f
}
const create = () => ({
  kind: 'create' as const,
  projectID,
  request: { variable_id: id(30), name: 'NEW', description: '', value: 'ONE-TIME-CANARY' },
})
describe('Secret Human Session command ownership', () => {
  it('keeps material out of recovery and Lookup; not_observed stays uncertain across same Session restore', async () => {
    const f = await owner()
    f.intercept(async (url, init) =>
      url === base ? Promise.reject(new Error('ONE-TIME-CANARY')) : f.normal(url, init),
    )
    await expect(f.auth.secrets.execute(create())).rejects.toMatchObject({ kind: 'transport' })
    expect(f.auth.secrets.progress?.phase).toBe('uncertain')
    expect(JSON.stringify(f.auth.secrets.progress)).not.toContain('ONE-TIME-CANARY')
    expect(JSON.stringify(f.auth.secrets.progress)).not.toContain('original-key')
    f.intercept()
    await f.auth.restore()
    expect(f.auth.secrets.progress?.phase).toBe('uncertain')
    await f.auth.secrets.lookup()
    expect(f.auth.secrets.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    const call = f.fetch.mock.calls.find(([url]) => url.endsWith('/commands/lookup'))!
    expect(JSON.parse(call[1].body as string)).toEqual({
      command: 'project.secret_variable.create',
      target_id: id(30),
    })
    expect(call[1].body).not.toContain('ONE-TIME-CANARY')
    expect(new Headers(call[1].headers).get('Idempotency-Key')).toBe(
      new Headers(f.fetch.mock.calls.find(([url]) => url === base)![1].headers).get(
        'Idempotency-Key',
      ),
    )
    await expect(f.auth.secrets.execute(create())).rejects.toMatchObject({ kind: 'busy' })
  })
  it('accepts identity-only historical receipt but does not synthesize current metadata', async () => {
    const f = await owner()
    f.intercept(async (url, init) => {
      const response = await f.normal(url, init)
      if (url === base) throw new Error('connection ended')
      return response
    })
    await expect(f.auth.secrets.execute(create())).rejects.toMatchObject({ kind: 'transport' })
    f.intercept()
    await f.auth.secrets.lookup()
    expect(f.auth.secrets.progress).toMatchObject({
      phase: 'confirmed',
      observation: 'committed',
      receipt: { variable: { id: id(30), name: 'NEW' } },
    })
    expect(f.fetch.mock.calls.filter(([url]) => url === base)).toHaveLength(1)
  })
  it('clears private recovery when actual Session identity changes', async () => {
    const f = await owner()
    f.intercept(async (url, init) =>
      url === base ? Promise.reject(new Error('lost')) : f.normal(url, init),
    )
    await expect(f.auth.secrets.execute(create())).rejects.toBeDefined()
    f.intercept()
    f.session({ ...session(), session: { ...session().session, id: id(3) } })
    await f.auth.restore()
    expect(f.auth.secrets.progress).toBeNull()
    await expect(f.auth.secrets.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
  })
  it.each([
    [401, 'SESSION_REVOKED'],
    [403, 'CSRF_FAILED'],
  ] as const)('invalidates current Human on %s %s', async (status, code) => {
    const f = await owner()
    f.intercept(async () => problem(status, code))
    await expect(f.auth.secrets.execute(create())).rejects.toBeDefined()
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.auth.secrets.progress).toBeNull()
  })
  it('local forbidden never grants or denies System and retains ordinary Human Session', async () => {
    const f = await owner()
    f.intercept(async () => problem(403, 'FORBIDDEN'))
    await expect(f.auth.secrets.get(projectID, target)).rejects.toBeDefined()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.state.user?.role).toBe('user')
  })
  it('does not free the Cookie owner until original cancellation has actually returned', async () => {
    const f = await owner(),
      held = barrier(),
      started = barrier()
    cleanups.push(() => held.resolve())
    let read = 0
    const release = vi.fn()
    f.intercept(
      async () =>
        ({
          status: 200,
          redirected: false,
          type: 'basic',
          headers: new Headers({ 'Content-Type': 'application/json' }),
          body: {
            getReader: () => ({
              read: async () =>
                read++
                  ? { done: true }
                  : {
                      done: false,
                      value: new TextEncoder().encode(
                        JSON.stringify(
                          receipt('create', {
                            ...metadata(),
                            id: id(30),
                            name: 'NEW',
                            description: '',
                          }),
                        ),
                      ),
                    },
              cancel: async () => {
                started.resolve()
                await held.promise
              },
              releaseLock: release,
            }),
            cancel: async () => undefined,
          },
        }) as unknown as Response,
    )
    const attempt = f.auth.secrets.execute(create()).catch((e) => e)
    await started.promise
    f.auth.secrets.abandon()
    expect(await attempt).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    expect(release).not.toHaveBeenCalled()
    await expect(f.auth.secrets.execute(create())).rejects.toMatchObject({ kind: 'busy' })
    held.resolve()
    await flushPromises()
    expect(release).toHaveBeenCalledOnce()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.secrets.progress).toBeNull()
  })
})
describe('Secret controller scope and current reads', () => {
  async function page(archived = false) {
    const f = await owner()
    if (archived)
      f.project({ ...project(), lifecycle: 'archived', archived_at: metadata().created_at })
    const workspace = createProjectWorkspace(f.auth)
    cleanups.push(() => workspace.dispose())
    workspace.afterNavigation('/owner/demo/settings/secrets')
    await flushPromises()
    const scope = effectScope(),
      location = shallowRef('/owner/demo/settings/secrets')
    const page = scope.run(() => useProjectSecrets(location, f.auth, workspace))!
    cleanups.push(() => scope.stop())
    await flushPromises()
    return { ...f, page, workspace, location }
  }
  it('allows archived safe reads while disabling all new mutations', async () => {
    const f = await page(true)
    expect(f.page.list.items).toHaveLength(1)
    expect(f.page.canWrite.value).toBe(false)
    f.page.select(f.page.list.items[0]!)
    await flushPromises()
    expect(f.page.detail.value?.id).toBe(target)
    f.page.open('update')
    expect(f.page.editor.open).toBe(false)
  })
  it('clears protected metadata on current 403 and suppresses stale details after route retirement', async () => {
    const f = await page(),
      held = barrier<Response>()
    cleanups.push(() => held.resolve(json(metadata())))
    f.intercept(async (url, init) =>
      url === base + '/' + target ? held.promise : f.normal(url, init),
    )
    f.page.select(f.page.list.items[0]!)
    await flushPromises()
    f.location.value = '/owner/other/settings/secrets'
    held.resolve(json(metadata()))
    await flushPromises()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.detail.value).toBeNull()
    expect(f.page.list.items).toEqual([])
    f.intercept(async (url, init) =>
      url.startsWith(base) ? problem(403, 'FORBIDDEN') : f.normal(url, init),
    )
    f.location.value = '/owner/demo/settings/secrets'
    await flushPromises()
    expect(f.page.list.phase).toBe('unavailable')
    expect(f.page.list.items).toEqual([])
  })
  it('requires a new GET before adopting a conflict version and never automatically retries', async () => {
    const f = await page()
    f.page.select(f.page.list.items[0]!)
    await flushPromises()
    f.page.open('update')
    f.page.editor.name = 'NEW'
    f.rows([{ ...metadata(), version: '2' }])
    await f.page.submit('one-time')
    await flushPromises()
    expect(f.page.editor.conflict).toBe(true)
    f.page.adopt()
    expect(f.page.editor.baseline?.version).toBe('1')
    f.page.reread()
    await flushPromises()
    expect(f.page.editor.baseline?.version).toBe('1')
    f.page.adopt()
    expect(f.page.editor.baseline?.version).toBe('2')
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
  })
})
