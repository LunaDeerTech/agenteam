import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  schedulerFixture,
  schedulerBase,
  schedulerID as id,
  schedulerSession,
  schedulerJSON as json,
  schedulerDeferred,
  schedulerProblem,
} from './project-scheduler.fixture'
const fixtures: Awaited<ReturnType<typeof schedulerFixture>>[] = []
afterEach(async () => {
  for (const f of fixtures.splice(0)) await f.close()
})
async function fixture() {
  const f = await schedulerFixture()
  fixtures.push(f)
  return f
}
const value = (f: Awaited<ReturnType<typeof fixture>>) => ({
  expected_version: f.page.draft.expected_version,
  scheduler_enabled: f.page.draft.enabled,
  scheduler_max_concurrency: f.page.draft.unlimited ? null : f.page.draft.limit,
})
describe('Scheduler current configuration and original intent', () => {
  it('saves a paused configuration then reads its current value rather than displaying acknowledgement as data', async () => {
    const f = await fixture()
    expect(f.page.current.value?.version).toBe('1')
    Object.assign(f.page.draft, { enabled: false, unlimited: false, limit: '9007199254740993' })
    await f.page.submit(value(f))
    expect(f.page.current.value).toEqual({
      project_id: id(10),
      version: '2',
      scheduler_enabled: false,
      scheduler_max_concurrency: '9007199254740993',
    })
    expect(
      f.fetch.mock.calls.filter(([p, i]) => p === schedulerBase && i.method === 'GET'),
    ).toHaveLength(2)
    expect(f.page.state.needsRead).toBe(false)
  })
  it('preserves conflict input and original version until an explicit current-version adoption', async () => {
    const f = await fixture()
    Object.assign(f.page.draft, { enabled: false, unlimited: false, limit: '7' })
    f.configuration.value = {
      ...f.configuration.value,
      version: '2',
      scheduler_max_concurrency: '4',
    }
    await f.page.submit(value(f))
    expect(f.page.state.conflict).toBe(true)
    expect(f.page.draft.expected_version).toBe('1')
    expect(f.page.draft.limit).toBe('7')
    await f.page.readCurrent()
    expect(f.page.current.value?.version).toBe('2')
    expect(f.page.draft.expected_version).toBe('1')
    await f.page.submit(value(f))
    expect(f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')).toHaveLength(1)
    f.page.adoptCurrentVersion()
    expect(f.page.draft.expected_version).toBe('2')
    expect(f.page.draft.limit).toBe('7')
    await f.page.submit(value(f))
    expect(f.page.current.value?.version).toBe('3')
    expect(f.page.current.value?.scheduler_max_concurrency).toBe('7')
  })
  it('keeps confirmed writes when GET fails and blocks another write until current configuration is read', async () => {
    const f = await fixture()
    f.page.draft.enabled = false
    f.intercept(async (path, init) => {
      if (path === schedulerBase && init.method === 'GET')
        throw new Error('controlled current-read loss')
      return f.normal(path, init)
    })
    await f.page.submit(value(f))
    expect(f.page.progress.value?.phase).toBe('confirmed')
    expect(f.page.state.needsRead).toBe(true)
    expect(f.page.current.value).toBeNull()
    expect(f.page.state.message).toContain('操作已确认')
    await f.page.submit(value(f))
    expect(f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')).toHaveLength(1)
    f.intercept()
    await f.page.readCurrent()
    expect(f.page.current.value?.scheduler_enabled).toBe(false)
    expect(f.page.state.needsRead).toBe(false)
  })
  it('retains Unknown draft across a same-Session check and separates historical Lookup from newer GET', async () => {
    const f = await fixture()
    Object.assign(f.page.draft, { enabled: false, unlimited: false, limit: '8' })
    f.intercept(async (path, init) => {
      const result = await f.normal(path, init)
      if (path === schedulerBase && init.method === 'PATCH')
        throw new Error('controlled lost response')
      return result
    })
    await f.page.submit(value(f))
    expect(f.page.progress.value?.phase).toBe('uncertain')
    const restore = schedulerDeferred<Response>()
    f.intercept((path, init) =>
      path === '/api/v1/session' ? restore.promise : f.normal(path, init),
    )
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.page.visible.value).toBe(false)
    expect(f.page.draft.limit).toBe('8')
    restore.resolve(json(schedulerSession))
    await checking
    await flushPromises()
    expect(f.page.visible.value).toBe(true)
    expect(f.page.draft).toMatchObject({ expected_version: '1', limit: '8' })
    f.configuration.value = {
      project_id: id(10),
      version: '3',
      scheduler_enabled: true,
      scheduler_max_concurrency: '5',
    }
    f.intercept()
    await f.page.lookupOriginal()
    expect(f.page.current.value).toEqual(f.configuration.value)
    expect(f.page.draft.limit).toBe('5')
    expect(f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')).toHaveLength(1)
  })
  it('current denial clears private draft and does not automatically reread', async () => {
    const f = await fixture()
    f.page.draft.limit = '99'
    f.page.draft.unlimited = false
    f.intercept(async (path, init) =>
      path === schedulerBase ? schedulerProblem(403, 'FORBIDDEN') : f.normal(path, init),
    )
    await f.page.readCurrent()
    await flushPromises()
    expect(f.page.draft.expected_version).toBe('')
    expect(f.page.current.value).toBeNull()
    expect(f.page.state.denied).toBe(true)
    expect(f.fetch.mock.calls.filter(([p]) => p === schedulerBase)).toHaveLength(2)
  })
  it.each([403, 404])(
    'releases local writing/busy after a current PATCH %s refusal',
    async (status) => {
      const f = await fixture()
      f.page.draft.enabled = false
      f.intercept(async (path, init) =>
        path === schedulerBase && init.method === 'PATCH'
          ? schedulerProblem(status, status === 403 ? 'FORBIDDEN' : 'NOT_FOUND')
          : f.normal(path, init),
      )
      await f.page.submit(value(f))
      await flushPromises()
      expect(f.page.draft.expected_version).toBe('')
      expect(f.auth.projectScheduler.progress).toBeNull()
      expect(f.auth.state.busy).toBe(false)
      expect(f.page.state.writing).toBe(false)
      expect(f.page.busy.value).toBe(false)
    },
  )
  it('keeps archived Unknown queryable but never replays until the Project is active again', async () => {
    const f = await fixture()
    f.page.draft.enabled = false
    f.intercept(async (path, init) => {
      if (path === schedulerBase && init.method === 'PATCH')
        throw new Error('controlled unconfirmed write')
      return f.normal(path, init)
    })
    await f.page.submit(value(f))
    const original = f.fetch.mock.calls.find(
      ([p, i]) => p === schedulerBase && i.method === 'PATCH',
    )![1]
    f.configuration.value = { ...f.configuration.value, version: '2' }
    f.intercept(async (path, init) =>
      path === `/api/v1/projects/${id(10)}`
        ? json({ ...f.project(), lifecycle: 'archived', archived_at: f.project().updated_at })
        : f.normal(path, init),
    )
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.page.readOnly.value).toBe(true)
    expect(f.page.progress.value?.phase).toBe('uncertain')
    await f.page.replayOriginal()
    expect(f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')).toHaveLength(1)
    await f.page.lookupOriginal()
    expect(f.page.progress.value?.observation).toBe('not_observed')
    f.configuration.value = { ...f.configuration.value, version: '3' }
    f.intercept()
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.page.readOnly.value).toBe(false)
    await f.page.replayOriginal()
    const patches = f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')
    expect(patches).toHaveLength(2)
    expect(patches[1]![1].body).toBe(original.body)
    expect(new Headers(patches[1]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
  })
  it('ignores a delayed old response after confirmed navigation retirement', async () => {
    const f = await fixture(),
      late = schedulerDeferred<Response>()
    f.intercept((path, init) => (path === schedulerBase ? late.promise : f.normal(path, init)))
    const pending = f.page.readCurrent()
    await flushPromises()
    f.page.afterNavigation('/owner/demo/settings/general')
    late.resolve(
      json({
        project_id: id(10),
        version: '9',
        scheduler_enabled: false,
        scheduler_max_concurrency: '4',
      }),
    )
    await pending
    await flushPromises()
    expect(f.page.current.value).toBeNull()
    expect(f.page.draft.expected_version).toBe('')
    expect(f.page.visible.value).toBe(false)
  })
})
