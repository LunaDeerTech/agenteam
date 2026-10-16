import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  schedulerFixture,
  schedulerBase,
  schedulerLookup,
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
  const f = await schedulerFixture(false)
  fixtures.push(f)
  return f
}
const input = {
  expected_version: '1',
  scheduler_enabled: false,
  scheduler_max_concurrency: '9007199254740993',
}
describe('Scheduler original Session command owner', () => {
  it('retains original key/body after a transport failure and only explicitly replays after key-only not_observed', async () => {
    const f = await fixture()
    f.intercept(async (path, init) => {
      if (path === schedulerBase && init.method === 'PATCH') throw new Error('controlled loss')
      return f.normal(path, init)
    })
    const mutable = { ...input }
    await expect(f.auth.projectScheduler.startUpdate(id(10), mutable)).rejects.toMatchObject({
      kind: 'transport',
    })
    mutable.expected_version = '9'
    mutable.scheduler_enabled = true
    expect(f.auth.projectScheduler.progress).toMatchObject({ phase: 'uncertain' })
    expect(f.fetch.mock.calls.filter(([, i]) => i.method === 'PATCH')).toHaveLength(1)
    f.intercept()
    await f.auth.projectScheduler.checkOriginal()
    expect(f.auth.projectScheduler.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    await f.auth.projectScheduler.retryOriginal()
    const calls = f.fetch.mock.calls.filter(([p]) => p === schedulerBase || p === schedulerLookup)
    expect(calls.map(([, i]) => i.body)).toEqual([
      JSON.stringify(input),
      '{"command":"update"}',
      JSON.stringify(input),
    ])
    expect(new Set(calls.map(([, i]) => new Headers(i.headers).get('Idempotency-Key'))).size).toBe(
      1,
    )
    expect(f.auth.projectScheduler.progress).toMatchObject({
      phase: 'confirmed',
      receipt: { project_id: id(10), version: '2' },
    })
  })
  it('hides recovery during same-Session checking, then retains it; a new Session clears it', async () => {
    const f = await fixture()
    f.intercept(async (path, init) => {
      if (path === schedulerBase) throw new Error('controlled loss')
      return f.normal(path, init)
    })
    await expect(f.auth.projectScheduler.startUpdate(id(10), input)).rejects.toThrow()
    const restore = schedulerDeferred<Response>()
    f.intercept((path, init) =>
      path === '/api/v1/session' ? restore.promise : f.normal(path, init),
    )
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.auth.projectScheduler.progress?.contextValid).toBe(false)
    expect(f.auth.projectScheduler.progress?.phase).toBe('uncertain')
    restore.resolve(json(schedulerSession))
    await checking
    expect(f.auth.projectScheduler.progress?.contextValid).toBe(true)
    f.intercept(async (path, init) =>
      path === '/api/v1/session'
        ? json({ ...schedulerSession, session: { ...schedulerSession.session, id: id(3) } })
        : f.normal(path, init),
    )
    await f.auth.restore()
    expect(f.auth.projectScheduler.progress).toBeNull()
    await expect(f.auth.projectScheduler.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it('keeps the sole Cookie owner until original reader cancellation settles', async () => {
    const f = await fixture(),
      tail = schedulerDeferred<void>()
    f.intercept(
      async () =>
        new Response(
          new ReadableStream({
            start(c) {
              c.enqueue(new TextEncoder().encode('{'))
            },
            cancel() {
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    const pending = f.auth.projectScheduler.get(id(10))
    await flushPromises()
    f.auth.projectScheduler.abandonRead()
    await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.projectScheduler.startUpdate(id(10), input)).rejects.toMatchObject({
      kind: 'busy',
    })
    tail.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
  })
  it('retiring a rejected metadata edit preserves an independent Scheduler Unknown intent', async () => {
    const f = await fixture()
    f.intercept(async (path, init) => {
      if (path === `/api/v1/projects/${id(10)}` && init.method === 'PATCH')
        return schedulerProblem(409, 'VERSION_CONFLICT', 'not_committed')
      if (path === schedulerBase && init.method === 'PATCH')
        throw new Error('controlled Scheduler loss')
      return f.normal(path, init)
    })
    await expect(
      f.auth.projects.startUpdate(id(10), { expected_version: '1', name: 'Other' }),
    ).rejects.toMatchObject({ problem: { code: 'VERSION_CONFLICT' } })
    await expect(f.auth.projectScheduler.startUpdate(id(10), input)).rejects.toMatchObject({
      kind: 'transport',
    })
    expect(f.auth.projects.progress?.phase).toBe('rejected')
    const original = f.auth.projectScheduler.progress
    f.auth.projects.editRejected()
    expect(f.auth.projectScheduler.progress).toEqual(original)
    f.intercept()
    await f.auth.projectScheduler.checkOriginal()
    expect(f.auth.projectScheduler.progress?.observation).toBe('not_observed')
  })
  it('a current Project refusal drops the private original intent without inventing an admin failure', async () => {
    const f = await fixture()
    f.intercept(async () => schedulerProblem(403, 'FORBIDDEN'))
    await expect(f.auth.projectScheduler.startUpdate(id(10), input)).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.projectScheduler.progress).toBeNull()
    expect(f.auth.state.phase).toBe('authenticated')
  })
})
