import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import type { Fetch } from '../api/client'
import { createWorkTaskReorderAPI } from '../api/work-task-reorder'
import { createSessionController, type SessionController } from '../composables/useSession'
import { id, p, t, task, json, problem, session } from './task-human-blocker-fixture'
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach((f) => f())
  owners.splice(0).forEach((owner) => owner.leave())
  await flushPromises()
  vi.restoreAllMocks()
})
const input = () => ({ expected_version: task().version, request: { before_id: id(6) } })
const receipt = () => ({
  task: task(String(BigInt(task().version) + 1n)),
  changed: true,
  task_event_id: id(10),
  event_ids: [id(11)],
})
const path = `/api/v1/projects/${p}/tasks/${t}/reorder`,
  lookupPath = `/api/v1/projects/${p}/task-commands/lookup`
async function fixture() {
  let current = session()
  let writeReply: () => Promise<Response> = async () => problem()
  let status: 'committed' | 'not_observed' | 'in_progress' = 'committed'
  const fetcher = vi.fn<Fetch>(async (url) => {
    if (url === '/api/v1/session') return json(current)
    if (url === path) return writeReply()
    if (url === lookupPath)
      return json({ status, receipt: status === 'committed' ? receipt() : null })
    throw new Error('Unexpected route')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { taskReorder: createWorkTaskReorderAPI(fetcher) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetcher,
    writes: () => fetcher.mock.calls.filter(([url]) => url === path),
    lookups: () => fetcher.mock.calls.filter(([url]) => url === lookupPath),
    reply(fn: typeof writeReply) {
      writeReply = fn
    },
    observation(value: typeof status) {
      status = value
    },
    session(value: ReturnType<typeof session>) {
      current = value
    },
  }
}
describe('Task reorder original Session request', () => {
  it('keeps original key/body through Unknown and explicit repeated Lookup, blocking other Task writers', async () => {
    const f = await fixture(),
      draft = input()
    await expect(f.auth.workReview.startReorder(p, t, draft)).rejects.toMatchObject({
      kind: 'problem',
    })
    draft.request.before_id = id(7)
    expect(f.auth.workReview.progress).toMatchObject({
      kind: 'reorder',
      phase: 'uncertain',
      contextValid: true,
    })
    await expect(f.auth.workReview.startReorder(p, t, input())).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.workReview.startEdit(p, t, {
        expected_version: task().version,
        request: { title: 'changed' },
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      f.observation(status)
      await f.auth.workReview.checkOriginal()
      expect(f.auth.workReview.progress?.phase).toBe(
        status === 'committed' ? 'confirmed' : 'uncertain',
      )
    }
    expect(f.writes()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    const first = f.writes()[0]![1]
    expect(JSON.parse(first.body as string)).toEqual(input())
    for (const [, init] of f.lookups()) {
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.reorder',
        target_id: t,
        ...input(),
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(
        new Headers(first.headers).get('Idempotency-Key'),
      )
    }
  })
  it('keeps the request owner until actual completion and discards a late result after leaving', async () => {
    const f = await fixture()
    let release!: () => void
    f.reply(
      () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(receipt()))
        }),
    )
    const pending = f.auth.workReview.startReorder(p, t, input()).catch((error: unknown) => error)
    await flushPromises()
    releases.push(() => release())
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.workReview.startReorder(p, t, input())).rejects.toMatchObject({
      kind: 'busy',
    })
    f.auth.leave()
    release()
    await pending
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.writes()).toHaveLength(1)
  })
  it('requires explicit new intent after a definite rejection and clears Unknown on real Session replacement', async () => {
    const f = await fixture()
    f.reply(async () => problem('TASK_VERSION_CONFLICT', 409, 'not_committed'))
    await expect(f.auth.workReview.startReorder(p, t, input())).rejects.toMatchObject({
      kind: 'problem',
    })
    expect(f.auth.workReview.progress?.phase).toBe('rejected')
    f.auth.workReview.editRejected()
    f.reply(async () => problem())
    await expect(f.auth.workReview.startReorder(p, t, input())).rejects.toMatchObject({
      kind: 'problem',
    })
    f.session(session(10))
    await f.auth.restore()
    expect(f.auth.workReview.progress).toBeNull()
    await expect(f.auth.workReview.checkOriginal()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.lookups()).toHaveLength(0)
  })
})
