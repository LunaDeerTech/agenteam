import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectSecrets } from '../composables/useProjectSecrets'
import {
  secretBase,
  secretDeferred,
  secretID,
  secretJSON,
  secretMetadata,
  secretPath,
  secretProblem,
  secretSessionFixture,
} from './project-secrets.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
async function setup() {
  const f = secretSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(secretPath)
  workspace.afterNavigation(secretPath)
  await flushPromises()
  const owner = useProjectSecrets(f.auth, workspace, location)
  cleanups.push(() => {
    owner.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  expect(owner.page.phase).toBe('ready')
  return { ...f, workspace, location, owner }
}
describe('Secret reads through actual Session and Workspace controllers', () => {
  it('reads a selected detail freshly and clears it on a real refresh failure', async () => {
    const f = await setup()
    f.owner.select(secretID(10))
    await flushPromises()
    expect(f.owner.detail.value?.version).toBe('9007199254740993')
    f.intercept((path, init) =>
      path === secretBase + '/' + secretID(10)
        ? Promise.resolve(secretProblem(503))
        : f.normal(path, init),
    )
    f.owner.refreshDetail()
    expect(f.owner.detail.value).toBeNull()
    await flushPromises()
    expect(f.owner.detail.phase).toBe('error')
    expect(f.owner.page.items).toHaveLength(1)
    expect(f.auth.state.phase).toBe('authenticated')
    f.intercept()
    f.owner.refreshDetail()
    await flushPromises()
    expect(f.owner.detail.phase).toBe('ready')
  })
  it.each([403, 404])(
    'clears current Secrets on %s while retaining the current Session',
    async (status) => {
      const f = await setup()
      f.intercept((path, init) =>
        path === secretBase + '/' + secretID(10)
          ? Promise.resolve(secretProblem(status))
          : f.normal(path, init),
      )
      f.owner.select(secretID(10))
      await flushPromises()
      expect(f.owner.page.phase).toBe('unavailable')
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.detail.value).toBeNull()
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.personalContext.phase).toBe('current')
    },
  )
  it('hides observations during checking and binds fresh reads to a different same-user Session', async () => {
    const f = await setup(),
      check = secretDeferred<Response>()
    cleanups.unshift(() => check.resolve(secretJSON(f.sessionValue())))
    f.owner.select(secretID(10))
    await flushPromises()
    f.intercept((path, init) => (path === '/api/v1/session' ? check.promise : f.normal(path, init)))
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.detail.value).toBeNull()
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: secretID(4) } })
    check.resolve(secretJSON(f.sessionValue()))
    await checking
    await flushPromises()
    f.workspace.afterNavigation(secretPath)
    await flushPromises()
    expect(f.auth.personalContext.identity?.sessionID).toBe(secretID(4))
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.selected.value).toBeNull()
  })
  it('retires delayed cross-Project and off-route responses before publishing', async () => {
    const f = await setup(),
      response = secretDeferred<Response>()
    cleanups.unshift(() => response.resolve(secretJSON(secretMetadata())))
    f.intercept((path, init) =>
      path === secretBase + '/' + secretID(10) ? response.promise : f.normal(path, init),
    )
    f.owner.select(secretID(10))
    await flushPromises()
    f.project({
      ...f.workspace.detail.project!,
      id: secretID(60),
      name: 'Other',
      normalized_name: 'other',
    })
    f.location.value = '/owner/other/settings/secrets'
    f.workspace.afterNavigation(f.location.value)
    expect(f.owner.detail.value).toBeNull()
    response.resolve(secretJSON(secretMetadata()))
    await flushPromises()
    expect(f.workspace.currentReadContext.value?.projectID).toBe(secretID(60))
    expect(f.owner.page.items[0]?.project_id).toBe(secretID(60))
    expect(f.owner.selected.value).toBeNull()
    f.location.value = '/owner/other/settings/variables'
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
  })
  it('waits for the original non-EOF reader cancel tail before new requests', async () => {
    const f = await setup(),
      tail = secretDeferred<void>(),
      cancelled = vi.fn(() => tail.promise)
    cleanups.unshift(() => tail.resolve())
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"id":'))
      },
      cancel: cancelled,
    })
    f.intercept((path, init) =>
      path === secretBase + '/' + secretID(10)
        ? Promise.resolve(new Response(body, { headers: { 'Content-Type': 'application/json' } }))
        : f.normal(path, init),
    )
    f.owner.select(secretID(10))
    await flushPromises()
    const count = f.fetch.mock.calls.length
    f.owner.cancel()
    await flushPromises()
    expect(cancelled).toHaveBeenCalledOnce()
    expect(f.auth.state.busy).toBe(true)
    f.owner.refresh()
    f.owner.select(secretID(10))
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(count)
    tail.resolve()
    await flushPromises()
    expect(body.locked).toBe(false)
    expect(f.auth.state.busy).toBe(false)
    f.intercept()
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
  })
  it('joins pages strictly, keeps a failed whole page out, and resets stale cursors explicitly', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, n) =>
        secretMetadata(100 + n, 'KEY_' + String(n).padStart(3, '0')),
      )
    let mode = 'first'
    f.intercept(async (path, init) => {
      if (!path.startsWith(secretBase + '?')) return f.normal(path, init)
      if (!path.includes('cursor=')) return secretJSON({ items: first, next_cursor: 'cursor-a' })
      if (mode === 'bad') return secretJSON({ items: [secretMetadata(100, 'KEY_999')] })
      if (mode === 'stale') return secretProblem(409, 'CURSOR_STALE')
      return secretJSON({ items: [secretMetadata(150, 'KEY_050')] })
    })
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(50)
    mode = 'bad'
    f.owner.loadMore()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(50)
    expect(f.owner.page.nextCursor).toBe('cursor-a')
    expect(f.owner.page.message).not.toBe('')
    mode = 'good'
    f.owner.loadMore()
    await flushPromises()
    expect(f.owner.page.items).toHaveLength(51)
    expect(f.owner.page.nextCursor).toBeUndefined()
    f.owner.refresh()
    await flushPromises()
    mode = 'stale'
    f.owner.loadMore()
    await flushPromises()
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.page.nextCursor).toBeUndefined()
    expect(f.owner.page.message).toContain('第一页')
    f.owner.refresh()
    await flushPromises()
    expect(f.fetch.mock.calls.at(-1)?.[0]).toBe(secretBase + '?limit=50')
  })
})
