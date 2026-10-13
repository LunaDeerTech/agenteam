import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { AccountFailure, type Fetch } from '../api/client'
import { createTaskExecutionsAPI } from '../api/task-executions'
import { createSessionController, type SessionController } from '../composables/useSession'
import { execution, id, json, p, path, problem, session, t } from './task-execution-fixture'
const owners: SessionController[] = [], releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach(owner => owner.leave())
  releases.splice(0).forEach(release => release())
  await flushPromises()
})
async function fixture() {
  let handler: Fetch = async () => json({ items: [execution()], next_cursor: null })
  const fetch = vi.fn<Fetch>((url, init) => url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init))
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { taskExecutions: createTaskExecutionsAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return { auth, fetch, intercept: (next: Fetch) => { handler = next } }
}
describe('Task Execution Session request owner', () => {
  it('keeps the actual abandoned read tail owned and only then admits an explicit fresh read', async () => {
    const f = await fixture()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>(resolve => { finish = resolve })
    releases.push(() => finish(json({ items: [execution()], next_cursor: null })))
    f.intercept(async () => tail)
    const first = f.auth.workReview.executions(p, t, {}).catch(error => error)
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    f.auth.workReview.abandonRead()
    expect(await first).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.workReview.execution(p, t, id(100))).rejects.toMatchObject({ kind: 'busy' })
    finish(json({ items: [execution()], next_cursor: null }))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.intercept(async () => json({ execution: execution() }))
    expect((await f.auth.workReview.execution(p, t, id(100))).id).toBe(id(100))
    expect(f.fetch.mock.calls.filter(call => call[0].startsWith(path))).toHaveLength(2)
  })
  it('does not turn an object permission refusal into a Session or System permission failure', async () => {
    const f = await fixture()
    f.intercept(async () => problem('PERMISSION_DENIED', 403))
    await expect(f.auth.workReview.executions(p, t, {})).rejects.toBeInstanceOf(AccountFailure)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.phase).toBe('current')
    f.intercept(async () => json({ items: [], next_cursor: null }))
    expect(await f.auth.workReview.executions(p, t, {})).toEqual({ items: [], next_cursor: null })
  })
})
