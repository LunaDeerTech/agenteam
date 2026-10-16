import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import {
  inboxBase,
  inboxDeferred,
  inboxFixture,
  inboxJSON,
  inboxPage,
  inboxProblem,
  inboxProject,
} from './project-inbox.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  cleanups.splice(0).forEach((close) => close())
  await flushPromises()
})
describe('Inbox reads use the original Human Session owner', () => {
  it('admits an ordinary Owner and captures the filter before asynchronous dispatch', async () => {
    const f = inboxFixture()
    cleanups.push(() => f.auth.leave())
    await f.auth.restore()
    const query = { status: 'open' as 'open' | 'resolved', limit: 50 }
    const pending = f.auth.inbox.list(inboxProject, query)
    query.status = 'resolved'
    expect((await pending).items[0]!.status).toBe('open')
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.fetch.mock.calls.at(-1)![0]).toBe(inboxBase + '?status=open&limit=50')
  })
  it('cancels visible reads while retaining the actual request tail and prevents a second Cookie request', async () => {
    const f = inboxFixture(),
      held = inboxDeferred<Response>()
    cleanups.push(() => f.auth.leave())
    await f.auth.restore()
    f.intercept((path, init) => (path.startsWith(inboxBase) ? held.promise : f.normal(path, init)))
    const pending = f.auth.inbox.list(inboxProject, {}).catch((error) => error)
    await flushPromises()
    f.auth.inbox.abandonRead()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.state.busy).toBe(true)
    const count = f.fetch.mock.calls.length
    expect(() => f.auth.inbox.list(inboxProject, {})).toThrow(
      expect.objectContaining({ kind: 'busy' }),
    )
    expect(f.fetch.mock.calls).toHaveLength(count)
    held.resolve(inboxJSON(inboxPage()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.fetch.mock.calls).toHaveLength(count)
  })
  it('keeps project refusal local and discards a result after identity retirement', async () => {
    const f = inboxFixture()
    cleanups.push(() => f.auth.leave())
    await f.auth.restore()
    f.intercept((path, init) =>
      path.startsWith(inboxBase) ? Promise.resolve(inboxProblem(403)) : f.normal(path, init),
    )
    await expect(f.auth.inbox.list(inboxProject, {})).rejects.toMatchObject({
      kind: 'problem',
      problem: { status: 403 },
    })
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.system.denied).toBe(false)
    const held = inboxDeferred<Response>()
    f.intercept((path, init) => (path.startsWith(inboxBase) ? held.promise : f.normal(path, init)))
    const pending = f.auth.inbox.list(inboxProject, {}).catch((error) => error)
    await flushPromises()
    f.auth.leave()
    held.resolve(inboxJSON(inboxPage()))
    await flushPromises()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.personalContext.identity).toBeNull()
  })
})
