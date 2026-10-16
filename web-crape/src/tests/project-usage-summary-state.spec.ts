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
  usagePath,
  usageProblem,
  usageSessionFixture,
} from './project-usage.fixture'
import { dailyAsOf, dailyPage } from './project-usage-summary.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) cleanup()
  await flushPromises()
})
async function setup() {
  const f = usageSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth),
    location = shallowRef(usagePath)
  workspace.afterNavigation(location.value)
  await flushPromises()
  let now = usageNow
  const owner = useProjectUsage(f.auth, workspace, location, () => now)
  cleanups.push(() => {
    owner.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  f.intercept((path, init) =>
    path.startsWith(usageBase + '/summary?')
      ? Promise.resolve(usageJSON(dailyPage()))
      : f.normal(path, init),
  )
  await flushPromises()
  return {
    ...f,
    owner,
    workspace,
    location,
    advance() {
      now += 60000
    },
  }
}
describe('Daily summaries through the real Session read owner', () => {
  it('keeps the current UTC window while switching, reads server totals beyond the current list, and explicitly refreshes', async () => {
    const f = await setup(),
      window = { ...f.owner.window.value }
    expect(f.owner.page.items).toHaveLength(1)
    f.owner.showMode('daily')
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.daily.items[0]!.summary.confirmed_invocations).toBe('51')
    expect(f.owner.daily.asOf).toBe(dailyAsOf)
    const request = f.fetch.mock.calls.find(([path]) => path.includes('/summary?'))!
    const query = new URL(request[0], 'https://owned.invalid').searchParams
    expect(query.get('from')).toBe(window.from)
    expect(query.get('to')).toBe(window.to)
    expect(query.get('group_by')).toBe('day')
    const count = f.fetch.mock.calls.length
    f.owner.next()
    f.owner.previous()
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(count)
    f.advance()
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.window.value!.to).not.toBe(window.to)
    f.owner.showMode('records')
    await flushPromises()
    expect(f.owner.daily.items).toEqual([])
    expect(f.owner.page.items).toHaveLength(1)
  })
  it('clears cross-Project and checking observations and refuses the original late summary', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(dailyPage())))
    f.intercept((path, init) =>
      path.startsWith(usageBase + '/summary?')
        ? held.promise
        : path.includes('/model-usage/summary?')
          ? Promise.resolve(usageJSON(dailyPage([])))
          : f.normal(path, init),
    )
    f.owner.showMode('daily')
    await flushPromises()
    f.project({
      ...f.workspace.detail.project!,
      id: usageID(60),
      name: 'Other',
      normalized_name: 'other',
    })
    f.location.value = '/owner/other/settings/model-usage'
    f.workspace.afterNavigation(f.location.value)
    expect(f.owner.daily.items).toEqual([])
    held.resolve(usageJSON(dailyPage()))
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.daily.items).toEqual([])
    expect(f.fetch.mock.calls.at(-1)![0]).toContain('/' + usageID(60) + '/model-usage/summary?')
    const checking = usageDeferred<Response>()
    cleanups.unshift(() => checking.resolve(usageJSON(f.sessionValue())))
    f.intercept((path, init) =>
      path === '/api/v1/session' ? checking.promise : f.normal(path, init),
    )
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.daily.asOf).toBe('')
    checking.resolve(usageJSON(f.sessionValue()))
    await restore
  })
  it('keeps the original cancelled transport owner busy until its body tail returns', async () => {
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
      path.includes('/summary?')
        ? Promise.resolve(new Response(body, { headers: { 'Content-Type': 'application/json' } }))
        : f.normal(path, init),
    )
    f.owner.showMode('daily')
    await flushPromises()
    f.owner.cancel()
    await flushPromises()
    expect(cancelled).toHaveBeenCalledOnce()
    expect(f.auth.state.busy).toBe(true)
    expect(f.owner.daily.items).toEqual([])
    const count = f.fetch.mock.calls.length
    f.owner.refresh()
    f.owner.showMode('records')
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(count)
    tail.resolve()
    await flushPromises()
    expect(body.locked).toBe(false)
    expect(f.auth.state.busy).toBe(false)
  })
  it('clears denied summaries and handles a real empty response without inventing zero rows', async () => {
    const f = await setup()
    f.owner.showMode('daily')
    await flushPromises()
    f.intercept((path, init) =>
      path.includes('/summary?') ? Promise.resolve(usageProblem(403)) : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.phase).toBe('unavailable')
    expect(f.owner.daily.items).toEqual([])
    expect(f.owner.daily.asOf).toBe('')
    f.intercept((path, init) =>
      path.includes('/summary?') ? Promise.resolve(usageJSON(dailyPage([]))) : f.normal(path, init),
    )
    f.owner.refresh()
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.daily.items).toEqual([])
    expect(f.owner.daily.asOf).toBe(dailyAsOf)
  })
})
