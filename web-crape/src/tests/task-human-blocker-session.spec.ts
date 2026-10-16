import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import type { Fetch } from '../api/client'
import { createWorkTaskBlockersAPI } from '../api/work-task-blockers'
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
  resolveInput,
  resolvePath,
  session,
  t,
  task,
} from './task-human-blocker-fixture'
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
  args[19] = { taskBlockers: createWorkTaskBlockersAPI(fetcher) }
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
describe('Human blocker original Session intent', () => {
  it('binds version-protected Task fields across Unknown while accepting a spectator rank rebalance', async () => {
    const f = await fixture()
    const before = { ...task(), state: 'todo' as const, assignee_agent_id: id(7) }
    f.setReply(async () => problem())
    const saving = f.auth.workTaskBlockers.add(p, t, draft(), before)
    before.assignee_agent_id = id(80)
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const input = JSON.parse(f.writes()[0]![1].body as string)
    const valid = receipt(input)
    valid.task = { ...valid.task, state: 'todo', assignee_agent_id: id(7), manual_rank: '9'.repeat(32) }
    for (const change of [
      { state: 'in_review' },
      { assignee_agent_id: id(80) },
    ]) {
      f.setReply(async () => json({ status: 'committed', receipt: { ...valid, task: { ...valid.task, ...change } } }))
      await expect(f.auth.workTaskBlockers.lookup()).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(f.auth.workTaskBlockers.progress).toMatchObject({ phase: 'uncertain', receipt: null })
    }
    f.setReply(async () => json({ status: 'committed', receipt: valid }))
    await f.auth.workTaskBlockers.lookup()
    expect(f.auth.workTaskBlockers.progress).toMatchObject({ phase: 'confirmed', receipt: { task: { state: 'todo', assignee_agent_id: id(7), manual_rank: '9'.repeat(32) } } })
    expect(f.writes()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual(input)
  })
  it('rejects stale, terminal or foreign source Tasks before sending a Blocker command', async () => {
    const f = await fixture()
    for (const before of [
      { ...task(), version: '1' },
      { ...task(), state: 'done' as const },
      { ...task(), state: 'cancelled' as const },
      { ...task(), project_id: id(90) },
    ]) await expect(f.auth.workTaskBlockers.add(p, t, draft(), before)).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.writes()).toHaveLength(0)
    expect(f.auth.workTaskBlockers.progress).toBeNull()
  })
  it('mints one ID and key, retains the complete add intent through Lookup and blocks competing Work writes', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    const mutable = draft()
    const saving = f.auth.workTaskBlockers.add(p, t, mutable, task())
    mutable.request.description = 'edited after click'
    mutable.expected_version = '2'
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const original = JSON.parse(f.writes()[0]![1].body as string)
    expect(original).toMatchObject(draft())
    expect(original.request.blocker_id).toMatch(
      /^[\da-f]{8}-[\da-f]{4}-7[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}$/,
    )
    expect(f.auth.workTaskBlockers.progress).toMatchObject({
      kind: 'blocker-add',
      phase: 'uncertain',
      projectID: p,
      taskID: t,
      contextValid: true,
    })
    await expect(f.auth.workTaskBlockers.add(p, t, draft(), task())).rejects.toMatchObject({ kind: 'busy' })
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
      await f.auth.workTaskBlockers.lookup()
      expect(f.auth.workTaskBlockers.progress?.phase).toBe(
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
    expect(JSON.stringify(f.auth.workTaskBlockers.progress)).not.toContain(key)
  })
  it('keeps same-identity Unknown during checking but retires it on a real Session change', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    await expect(f.auth.workTaskBlockers.add(p, t, draft(), task())).rejects.toBeDefined()
    const release = f.holdRestore(),
      checking = f.auth.restore()
    await flushPromises()
    expect(f.auth.workTaskBlockers.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    release()
    await checking
    expect(f.auth.workTaskBlockers.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    f.setSession(session(70))
    await f.auth.restore()
    expect(f.auth.workTaskBlockers.progress).toBeNull()
    await expect(f.auth.workTaskBlockers.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
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
    await expect(f.auth.workTaskBlockers.resolve(p, t, original, task(original.expected_version))).rejects.toMatchObject({
      kind: 'transport',
    })
    original.request.resolution_comment = 'later edit'
    f.setReply(null)
    await f.auth.workTaskBlockers.lookup()
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe(resolvePath)
    expect(JSON.parse(f.lookups()[0]![1].body as string)).toMatchObject({
      command: 'work.task.blocker.resolve',
      request: {
        blocker_id: resolveInput().request.blocker_id,
        resolution_comment: '  已核对\n原说明  ',
      },
    })
    expect(f.auth.workTaskBlockers.progress?.receipt).toMatchObject({
      blocker: { resolution_comment: '  已核对\n原说明  ' },
    })
    await f.auth.workTaskBlockers.resolve(p, t, resolveInput(), task(resolveInput().expected_version))
    expect(JSON.parse(f.writes()[1]![1].body as string).request.resolution_comment).toBeNull()
  })
  it('requires explicit release after a definite rejection before accepting a newly confirmed version', async () => {
    const f = await fixture()
    f.setReply(async () => problem('VERSION_CONFLICT', 409, 'not_committed'))
    await expect(f.auth.workTaskBlockers.add(p, t, draft(), task())).rejects.toBeDefined()
    expect(f.auth.workTaskBlockers.progress?.phase).toBe('rejected')
    await expect(f.auth.workTaskBlockers.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(f.auth.workTaskBlockers.add(p, t, draft(), task())).rejects.toMatchObject({ kind: 'busy' })
    f.auth.workReview.editRejected()
    f.setReply(null)
    await f.auth.workTaskBlockers.add(p, t, { ...draft(), expected_version: '9007199254740998' }, task('9007199254740998'))
    expect(f.auth.workTaskBlockers.progress?.receipt?.task.version).toBe('9007199254740999')
    const [first, second] = f.writes().map(([, init]) => JSON.parse(init.body as string))
    expect(first.request.blocker_id).not.toBe(second.request.blocker_id)
    expect(f.lookups()).toHaveLength(0)
  })
  it('clears command material on current denial or an exact unavailable object, not a different Project', async () => {
    const f = await fixture()
    f.setReply(async () => problem())
    await expect(f.auth.workTaskBlockers.add(p, t, draft(), task())).rejects.toBeDefined()
    f.auth.workTaskBlockers.forgetUnavailable(id(80), t)
    expect(f.auth.workTaskBlockers.progress?.phase).toBe('uncertain')
    f.auth.workTaskBlockers.forgetUnavailable(p, id(81))
    expect(f.auth.workTaskBlockers.progress?.phase).toBe('uncertain')
    f.setReply(async () => problem('NOT_FOUND', 404, 'not_started'))
    await expect(f.auth.workTaskBlockers.lookup()).rejects.toBeDefined()
    expect(f.auth.workTaskBlockers.progress).toBeNull()
    f.setReply(null)
    await f.auth.workTaskBlockers.add(p, t, draft(), task())
    f.auth.workTaskBlockers.forgetUnavailable(p, t)
    expect(f.auth.workTaskBlockers.progress).toBeNull()
  })
  it('discards a late original response after identity invalidation without publishing confirmed progress', async () => {
    const f = await fixture()
    let release!: (response: Response) => void
    const held = new Promise<Response>((resolve) => {
      release = resolve
    })
    f.setReply(() => held)
    const saving = f.auth.workTaskBlockers.add(p, t, draft(), task()).catch((error: unknown) => error)
    await flushPromises()
    const original = JSON.parse(f.writes()[0]![1].body as string)
    const done = () => release(json(receipt(original)))
    releases.push(done)
    f.auth.leave()
    done()
    await saving
    expect(f.auth.workTaskBlockers.progress).toBeNull()
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.lookups()).toHaveLength(0)
  })
})
