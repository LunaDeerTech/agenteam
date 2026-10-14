import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createTaskExecutionCancelAPI } from '../api/task-execution-cancel'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { at, execution, id, json, p, path, problem, session, t } from './task-execution-fixture'

const owners: SessionController[] = [],
  releases: (() => void)[] = []
const input = {
  expected_version: '9007199254740993',
  expected_task_version: '3',
  reason: 'stop this execution',
}
const receipt = () => ({
  execution: execution(100, { version: '9007199254740994', cancel_requested_at: at }),
  task: { id: t, version: '4', state: 'blocked', blocker_id: id(500) },
})
afterEach(async () => {
  owners.splice(0).forEach((owner) => owner.leave())
  releases.splice(0).forEach((release) => release())
  await flushPromises()
  vi.restoreAllMocks()
})
async function fixture() {
  let handler: Fetch = async () => json({ receipt: receipt(), replayed: false })
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { taskExecutionCancel: createTaskExecutionCancelAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    intercept: (next: Fetch) => {
      handler = next
    },
  }
}

describe('Execution cancellation Session ownership', () => {
  it('keeps Unknown in the shared Task mutation slot and only looks up the original immutable intent', async () => {
    const f = await fixture()
    let found = false
    f.intercept(async (url) => {
      if (url.endsWith('/lookup')) return json({ found, receipt: found ? receipt() : null })
      throw new Error('lost response')
    })
    const draft = { ...input }
    await expect(f.auth.executionCancel.start(p, t, id(100), draft)).rejects.toMatchObject({
      kind: 'transport',
    })
    draft.reason = 'edited after dispatch'
    expect(f.auth.executionCancel.progress?.phase).toBe('uncertain')
    await expect(f.auth.executionCancel.start(p, t, id(100), input)).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.workReview.startTransfer(p, t, {
        expected_version: '3',
        request: { target_state: 'cancelled' },
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.executionCancel.checkOriginal()
    expect(f.auth.executionCancel.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    found = true
    await f.auth.executionCancel.checkOriginal()
    expect(f.auth.executionCancel.progress).toMatchObject({
      phase: 'confirmed',
      receipt: { task: { state: 'blocked' }, execution: { status: 'created' } },
    })
    const requests = f.fetch.mock.calls.filter(([url]) => url.startsWith(path))
    expect(requests).toHaveLength(3)
    expect(requests.filter(([url]) => !url.endsWith('/lookup'))).toHaveLength(1)
    const keys = requests.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))
    expect(new Set(keys).size).toBe(1)
    expect(keys[0]).toBeTruthy()
    expect(requests.every(([, init]) => init.body === JSON.stringify(input))).toBe(true)
  })
  it('distinguishes a proven version conflict and requires an explicit edit before a new intent', async () => {
    const f = await fixture()
    f.intercept(async () => problem('VERSION_CONFLICT', 409))
    await expect(f.auth.executionCancel.start(p, t, id(100), input)).rejects.toMatchObject({
      problem: { code: 'VERSION_CONFLICT' },
    })
    expect(f.auth.executionCancel.progress?.phase).toBe('rejected')
    await expect(f.auth.executionCancel.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    f.auth.executionCancel.editRejected()
    f.intercept(async () => json({ receipt: receipt(), replayed: false }))
    await f.auth.executionCancel.start(p, t, id(100), input)
    const requests = f.fetch.mock.calls.filter(([url]) => url.startsWith(path))
    expect(requests).toHaveLength(2)
    expect(new Headers(requests[0]![1].headers).get('Idempotency-Key')).not.toBe(
      new Headers(requests[1]![1].headers).get('Idempotency-Key'),
    )
  })
  it('holds the abandoned original HTTP tail and discards its late receipt after Session exit', async () => {
    const f = await fixture()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>((resolve) => {
      finish = resolve
    })
    releases.push(() => finish(json({ receipt: receipt(), replayed: false })))
    f.intercept(async () => tail)
    const saving = f.auth.executionCancel.start(p, t, id(100), input).catch((error) => error)
    await flushPromises()
    f.auth.leave()
    expect(f.auth.executionCancel.progress).toBeNull()
    expect(f.auth.state.busy).toBe(true)
    finish(json({ receipt: receipt(), replayed: false }))
    await saving
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.executionCancel.progress).toBeNull()
    await f.auth.restore()
    await expect(f.auth.executionCancel.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.fetch.mock.calls.filter(([url]) => url.startsWith(path))).toHaveLength(1)
  })
})
