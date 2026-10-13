import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import type { Fetch } from '../api/client'
import { createTaskExecutionOutputAPI } from '../api/task-execution-output'
import { createSessionController, type SessionController } from '../composables/useSession'
import { id, json, p, problem, session, t } from './task-execution-fixture'
import { output, outputPath } from './task-execution-output-fixture'
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((owner) => owner.leave())
  releases.splice(0).forEach((fn) => fn())
  await flushPromises()
})
async function fixture() {
  let handler: Fetch = async () => json(output())
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { taskExecutionOutput: createTaskExecutionOutputAPI(fetch) }
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
describe('Task Execution output Session read ownership', () => {
  it('retains the actual cancelled request tail and admits a new read only after it returns', async () => {
    const f = await fixture()
    let finish!: (value: Response) => void
    const tail = new Promise<Response>((resolve) => {
      finish = resolve
    })
    releases.push(() => finish(json(output())))
    f.intercept(async () => tail)
    const pending = f.auth.workReview.executionOutput(p, t, id(100)).catch((error) => error)
    await flushPromises()
    f.auth.workReview.abandonRead()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.workReview.executionOutput(p, t, id(100))).rejects.toMatchObject({
      kind: 'busy',
    })
    finish(json(output()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.intercept(async () => json(output('fresh')))
    expect(
      (await f.auth.workReview.executionOutput(p, t, id(100))).snapshot?.items[0]?.data.content,
    ).toBe('fresh')
    expect(f.fetch.mock.calls.filter((call) => call[0] === outputPath)).toHaveLength(2)
  })
  it('keeps object permission failure scoped and permits an explicit subsequent read', async () => {
    const f = await fixture()
    f.intercept(async () => problem('PERMISSION_DENIED', 403))
    await expect(f.auth.workReview.executionOutput(p, t, id(100))).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.phase).toBe('current')
    f.intercept(async () => json(output()))
    expect((await f.auth.workReview.executionOutput(p, t, id(100))).availability).toBe('available')
    expect(f.auth.workReview.progress).toBeNull()
  })
})
