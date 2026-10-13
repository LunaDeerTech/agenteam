import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import type { Fetch } from '../api/client'
import { createWorkTaskDependenciesAPI } from '../api/work-task-dependencies'
import { createSessionController, type SessionController } from '../composables/useSession'
import {
  addPath,
  draft,
  id,
  json,
  lookupPath,
  p,
  problem,
  receipt,
  related,
  resolveInput,
  resolvePath,
  session,
  t,
} from './task-dependency-fixture'
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  owners.splice(0).forEach((owner) => owner.leave())
  await flushPromises()
  vi.restoreAllMocks()
})
async function fixture() {
  let current = session(),
    reply: (() => Promise<Response>) | null = null
  let restoring: (() => Promise<Response>) | null = null
  let observation: 'committed' | 'not_observed' | 'in_progress' = 'committed'
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') return restoring ? restoring() : json(current)
    if (url.startsWith(`/api/v1/projects/${p}/tasks?`)) return reply ? reply() : json({ items: [] })
    if (url === addPath || url === resolvePath)
      return reply ? reply() : json(receipt(JSON.parse(init.body as string)))
    if (url === lookupPath)
      return reply && observation === 'committed'
        ? reply()
        : json({
            status: observation,
            receipt: observation === 'committed' ? receipt(JSON.parse(init.body as string)) : null,
          })
    throw new Error(`Unexpected request: ${url}`)
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { taskDependencies: createWorkTaskDependenciesAPI(fetcher) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetcher,
    writes: () => fetcher.mock.calls.filter(([url]) => url === addPath || url === resolvePath),
    lookups: () => fetcher.mock.calls.filter(([url]) => url === lookupPath),
    setReply(value: typeof reply) {
      reply = value
    },
    setObservation(value: typeof observation) {
      observation = value
    },
    setSession(value: SessionView) {
      current = value
    },
    holdRestore() {
      let release!: (response: Response) => void
      const held = new Promise<Response>((resolve) => {
        release = resolve
      })
      restoring = () => held
      const done = () => {
        restoring = null
        release(json(current))
      }
      releases.push(done)
      return done
    },
  }
}
describe('Task dependency original Session intent', () => {
  it('captures candidate text before dispatch and retains the single request owner through its actual tail', async () => {
    const f = await fixture()
    let release!: (value: Response) => void
    f.setReply(
      () =>
        new Promise<Response>((resolve) => {
          release = resolve
        }),
    )
    const query = { limit: 50 as const, text: 'Original' }
    const reading = f.auth.workTaskDependencies
      .candidates(p, query)
      .catch((error: unknown) => error)
    query.text = 'Edited later'
    await flushPromises()
    const call = f.fetcher.mock.calls.find(([url]) => url.includes('/tasks?'))!
    expect(new URL(call[0], 'https://local.test').searchParams.get('text')).toBe('Original')
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toMatchObject({
      kind: 'busy',
    })
    f.auth.leave()
    const done = () => release(json({ items: [] }))
    releases.push(done)
    done()
    await reading
    expect(f.auth.workTaskDependencies.progress).toBeNull()
    expect(f.writes()).toHaveLength(0)
  })

  it('mints one ID and key, retains the complete add intent through Lookup and blocks competing Work writes', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    const mutable = draft()
    const saving = f.auth.workTaskDependencies.add(p, t, mutable)
    mutable.request.description = 'edited after click'
    mutable.expected_version = '2'
    mutable.request.metadata.related_task_id = id(85)
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const original = JSON.parse(f.writes()[0]![1].body as string)
    expect(original).toMatchObject(draft())
    expect(original.request.blocker_id).toMatch(
      /^[\da-f]{8}-[\da-f]{4}-7[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/,
    )
    expect(f.auth.workTaskDependencies.progress).toMatchObject({
      kind: 'dependency-add',
      phase: 'uncertain',
      projectID: p,
      taskID: t,
      contextValid: true,
    })
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.workReview.startEdit(p, t, { expected_version: '1', request: { title: 'Blocked' } }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.workMilestonePlanning.create(p, { title: 'Blocked' }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.workSprintLifecycle.start(p, id(3), { expected_version: '1', request: {} }),
    ).rejects.toMatchObject({ kind: 'busy' })
    f.setReply(null)
    for (const observation of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setObservation(observation)
      await f.auth.workTaskDependencies.lookup()
      expect(f.auth.workTaskDependencies.progress?.phase).toBe(
        observation === 'committed' ? 'confirmed' : 'uncertain',
      )
    }
    const key = (f.writes()[0]![1].headers as Record<string, string>)['Idempotency-Key']
    expect(f.writes()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    for (const [, init] of f.lookups()) {
      expect(init.headers).toMatchObject({ 'Idempotency-Key': key })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.blocker.add',
        ...original,
      })
    }
    expect(JSON.stringify(f.auth.workTaskDependencies.progress)).not.toContain(key)
  })
  it('keeps same-identity Unknown during checking but retires it on a real Session change', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toBeDefined()
    const release = f.holdRestore(),
      checking = f.auth.restore()
    await flushPromises()
    expect(f.auth.workTaskDependencies.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    release()
    await checking
    expect(f.auth.workTaskDependencies.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    f.setSession(session(70))
    await f.auth.restore()
    expect(f.auth.workTaskDependencies.progress).toBeNull()
    await expect(f.auth.workTaskDependencies.lookup()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.writes()).toHaveLength(1)
  })
  it('preserves resolve ID, null and raw nonblank comment and never resends after transport uncertainty', async () => {
    const f = await fixture()
    const original = {
      ...resolveInput(),
      request: { ...resolveInput().request, resolution_comment: '  已核对\n原说明  ' },
    }
    f.setReply(async () => {
      throw new TypeError('network')
    })
    await expect(
      f.auth.workTaskDependencies.resolve(p, t, original, related),
    ).rejects.toMatchObject({
      kind: 'transport',
    })
    original.request.resolution_comment = 'later edit'
    f.setReply(null)
    await f.auth.workTaskDependencies.lookup()
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe(resolvePath)
    expect(JSON.parse(f.lookups()[0]![1].body as string)).toMatchObject({
      command: 'work.task.blocker.resolve',
      request: {
        blocker_id: resolveInput().request.blocker_id,
        resolution_comment: '  已核对\n原说明  ',
      },
    })
    expect(f.auth.workTaskDependencies.progress?.receipt).toMatchObject({
      blocker: {
        type: 'rely_on',
        related_task_id: related,
        resolution_comment: '  已核对\n原说明  ',
      },
    })
    await f.auth.workTaskDependencies.resolve(p, t, resolveInput(), related)
    expect(JSON.parse(f.writes()[1]![1].body as string).request.resolution_comment).toBeNull()
  })
  it('requires explicit release after a definite rejection before accepting a newly confirmed version', async () => {
    const f = await fixture()
    f.setReply(async () => problem('VERSION_CONFLICT', 409, 'not_committed'))
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toBeDefined()
    expect(f.auth.workTaskDependencies.progress?.phase).toBe('rejected')
    await expect(f.auth.workTaskDependencies.lookup()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toMatchObject({
      kind: 'busy',
    })
    f.auth.workReview.editRejected()
    f.setReply(null)
    await f.auth.workTaskDependencies.add(p, t, {
      ...draft(),
      expected_version: '9007199254740998',
    })
    expect(f.auth.workTaskDependencies.progress?.receipt?.task.version).toBe('9007199254740999')
    const [first, second] = f.writes().map(([, init]) => JSON.parse(init.body as string))
    expect(first.request.blocker_id).not.toBe(second.request.blocker_id)
    expect(f.lookups()).toHaveLength(0)
  })
  it('clears command material on current denial or an exact unavailable object, not a different Project', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    await expect(f.auth.workTaskDependencies.add(p, t, draft())).rejects.toBeDefined()
    f.auth.workTaskDependencies.forgetUnavailable(id(80), t)
    expect(f.auth.workTaskDependencies.progress?.phase).toBe('uncertain')
    f.auth.workTaskDependencies.forgetUnavailable(p, id(81))
    expect(f.auth.workTaskDependencies.progress?.phase).toBe('uncertain')
    f.setReply(async () => problem('NOT_FOUND', 404, 'not_started'))
    await expect(f.auth.workTaskDependencies.lookup()).rejects.toBeDefined()
    expect(f.auth.workTaskDependencies.progress).toBeNull()
    f.setReply(null)
    await f.auth.workTaskDependencies.add(p, t, draft())
    f.auth.workTaskDependencies.forgetUnavailable(p, t)
    expect(f.auth.workTaskDependencies.progress).toBeNull()
  })
  it('discards a late original response after identity invalidation without publishing confirmed progress', async () => {
    const f = await fixture()
    let release!: (response: Response) => void
    const held = new Promise<Response>((resolve) => {
      release = resolve
    })
    f.setReply(() => held)
    const saving = f.auth.workTaskDependencies.add(p, t, draft()).catch((error: unknown) => error)
    await flushPromises()
    const original = JSON.parse(f.writes()[0]![1].body as string)
    const done = () => release(json(receipt(original)))
    releases.push(done)
    f.auth.leave()
    done()
    await saving
    expect(f.auth.workTaskDependencies.progress).toBeNull()
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.lookups()).toHaveLength(0)
  })
})
