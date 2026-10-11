import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectUsage } from '../composables/useProjectUsage'
import {
  usageBase,
  usageDeferred,
  usageID,
  usageJSON,
  usageNow,
  usagePage,
  usagePath,
  usageProblem,
  usageRecord,
  usageSessionFixture,
} from './project-usage.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
async function setup() {
  const f = usageSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(usagePath)
  workspace.afterNavigation(usagePath)
  await flushPromises()
  let now = usageNow
  const owner = useProjectUsage(f.auth, workspace, location, () => now)
  cleanups.push(() => {
    owner.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  expect(owner.page.phase).toBe('ready')
  return {
    ...f,
    workspace,
    location,
    owner,
    advance() {
      now += 60_000
    },
  }
}
const usageCalls = (f: Awaited<ReturnType<typeof setup>>) =>
  f.fetch.mock.calls.filter(([path]) => path.startsWith(usageBase + '?'))
describe('Usage reads through actual Session and Workspace', () => {
  it('reads two pages in one time window, returns with a new forward chain and refreshes the window', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, i) => usageRecord(200 - i))
    let branch = 'old'
    f.intercept(async (path, init) => {
      if (!path.startsWith(usageBase + '?')) return f.normal(path, init)
      const cursor = new URL(path, 'https://owned.invalid').searchParams.get('cursor')
      return usageJSON(cursor ? usagePage([usageRecord(150)]) : usagePage(first, branch))
    })
    f.owner.refresh()
    await flushPromises()
    const initial = new URL(usageCalls(f).at(-1)![0], 'https://owned.invalid').searchParams
    f.owner.select(usageID(200))
    expect(f.owner.selected.value?.id).toBe(usageID(200))
    f.advance()
    f.owner.next()
    expect(f.owner.selected.value).toBeNull()
    await flushPromises()
    expect(f.owner.page.index).toBe(1)
    expect(f.owner.page.items.map((v) => v.id)).toEqual([usageID(150)])
    const next = new URL(usageCalls(f).at(-1)![0], 'https://owned.invalid').searchParams
    expect(next.get('from')).toBe(initial.get('from'))
    expect(next.get('to')).toBe(initial.get('to'))
    expect(next.get('cursor')).toBe('old')
    branch = 'new'
    f.owner.previous()
    await flushPromises()
    expect(f.owner.page.index).toBe(0)
    expect(f.owner.page.nextCursor).toBe('new')
    f.owner.next()
    await flushPromises()
    expect(
      new URL(usageCalls(f).at(-1)![0], 'https://owned.invalid').searchParams.get('cursor'),
    ).toBe('new')
    f.owner.refresh()
    await flushPromises()
    const refresh = new URL(usageCalls(f).at(-1)![0], 'https://owned.invalid').searchParams
    expect(refresh.has('cursor')).toBe(false)
    expect(refresh.get('to')).not.toBe(initial.get('to'))
    expect(f.owner.page.index).toBe(0)
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
  it('rejects overlap, cross-page wrong order, cursor cycles and invalid final rows without partial publication', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, i) => usageRecord(200 - i))
    let bad: unknown = usagePage([usageRecord(200)])
    f.intercept(async (path, init) =>
      !path.startsWith(usageBase + '?')
        ? f.normal(path, init)
        : usageJSON(path.includes('cursor=') ? bad : usagePage(first, 'cursor-a')),
    )
    for (const value of [
      usagePage([usageRecord(200)]),
      usagePage([usageRecord(300)]),
      usagePage(
        Array.from({ length: 50 }, (_, i) => usageRecord(150 - i)),
        'cursor-a',
      ),
      usagePage([
        usageRecord(150),
        { ...usageRecord(149), prompt: 'controlled-rejected' } as never,
      ]),
    ]) {
      bad = value
      f.owner.refresh()
      await flushPromises()
      f.owner.next()
      await flushPromises()
      expect(f.owner.page.phase).toBe('error')
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.selected.value).toBeNull()
    }
  })
  it.each([403, 404, 503])(
    'clears current observations on %s, retains Session and requires explicit refresh',
    async (status) => {
      const f = await setup()
      f.owner.select(usageID(100))
      f.intercept((path, init) =>
        path.startsWith(usageBase + '?')
          ? Promise.resolve(usageProblem(status))
          : f.normal(path, init),
      )
      f.owner.refresh()
      await flushPromises()
      expect(f.owner.page.phase).toBe(status === 503 ? 'error' : 'unavailable')
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.selected.value).toBeNull()
      expect(f.auth.state.phase).toBe('authenticated')
      expect(f.auth.personalContext.phase).toBe('current')
      const count = f.fetch.mock.calls.length
      await flushPromises()
      expect(f.fetch.mock.calls).toHaveLength(count)
      f.intercept()
      f.owner.refresh()
      await flushPromises()
      expect(f.owner.page.phase).toBe('ready')
    },
  )
  it('clears checking observations and restores under the actual new same-user Session', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(f.sessionValue())))
    f.owner.select(usageID(100))
    f.intercept((path, init) => (path === '/api/v1/session' ? held.promise : f.normal(path, init)))
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.selected.value).toBeNull()
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: usageID(4) } })
    held.resolve(usageJSON(f.sessionValue()))
    await checking
    await flushPromises()
    f.workspace.afterNavigation(usagePath)
    await flushPromises()
    expect(f.auth.personalContext.identity?.sessionID).toBe(usageID(4))
    expect(f.owner.page.phase).toBe('ready')
  })
  it('retires cross-Project and off-route reads without publishing the late result', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(usagePage())))
    f.intercept((path, init) =>
      path.startsWith(usageBase + '?') ? held.promise : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    f.project({
      ...f.workspace.detail.project!,
      id: usageID(60),
      name: 'Other',
      normalized_name: 'other',
    })
    f.location.value = '/owner/other/settings/model-usage'
    f.workspace.afterNavigation(f.location.value)
    held.resolve(usageJSON(usagePage()))
    await flushPromises()
    expect(f.owner.page.items[0]?.project_id).toBe(usageID(60))
    f.location.value = '/owner/other/settings/secrets'
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
  })
  it('retains the unique Session owner until the original non-EOF body cancel actually completes', async () => {
    const f = await setup(),
      tail = usageDeferred<void>(),
      cancelled = vi.fn(() => tail.promise)
    cleanups.unshift(() => tail.resolve())
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"items":'))
      },
      cancel: cancelled,
    })
    f.intercept((path, init) =>
      path.startsWith(usageBase + '?')
        ? Promise.resolve(new Response(body, { headers: { 'Content-Type': 'application/json' } }))
        : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    const count = f.fetch.mock.calls.length
    f.owner.cancel()
    await flushPromises()
    expect(cancelled).toHaveBeenCalledOnce()
    expect(f.auth.state.busy).toBe(true)
    f.owner.refresh()
    f.owner.next()
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
})
