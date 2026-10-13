import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useProjectUsage } from '../composables/useProjectUsage'
import { dailyPage } from './project-usage-summary.fixture'
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
  workspace.afterNavigation(location.value)
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
      now += 60000
    },
  }
}
function queries(f: Awaited<ReturnType<typeof setup>>) {
  return f.fetch.mock.calls
    .filter(([path]) => path.startsWith(usageBase))
    .map(([path]) => new URL(path, 'https://owned.invalid').searchParams)
}
const fullPage = () => Array.from({ length: 50 }, (_, i) => usageRecord(200 - i))

describe('Execution Usage filter through the existing Session owner', () => {
  it('retains the applied execution and UTC window across pagination, daily mode and refresh; clearing discards old cursors', async () => {
    const f = await setup(),
      original = { ...f.owner.window.value }
    f.intercept(async (path, init) => {
      if (!path.startsWith(usageBase)) return f.normal(path, init)
      const query = new URL(path, 'https://owned.invalid').searchParams
      return usageJSON(
        path.includes('/summary?')
          ? dailyPage()
          : query.has('cursor')
            ? usagePage([usageRecord(150)])
            : usagePage(fullPage(), query.has('execution_id') ? 'execution-page-2' : 'all-page-2'),
      )
    })
    f.owner.select(usageID(100))
    expect(f.owner.filterSelectedExecution()).toBe(true)
    expect(f.owner.selected.value).toBeNull()
    await flushPromises()
    expect(f.owner.executionID.value).toBe(usageID(21))
    expect(f.owner.page.items).toHaveLength(50)
    expect(queries(f).at(-1)!.has('cursor')).toBe(false)
    f.owner.next()
    await flushPromises()
    expect(f.owner.page.index).toBe(1)
    expect(queries(f).at(-1)!.get('cursor')).toBe('execution-page-2')
    f.owner.previous()
    await flushPromises()
    expect(f.owner.page.index).toBe(0)
    f.owner.showMode('daily')
    await flushPromises()
    expect(f.owner.daily.items[0]!.summary.confirmed_invocations).toBe('51')
    expect(
      queries(f)
        .slice(1)
        .every(
          (q) =>
            q.get('execution_id') === usageID(21) &&
            q.get('from') === original.from &&
            q.get('to') === original.to,
        ),
    ).toBe(true)
    f.advance()
    f.owner.refresh()
    await flushPromises()
    const refreshed = { ...f.owner.window.value }
    expect(refreshed.to).not.toBe(original.to)
    expect(queries(f).at(-1)!.get('execution_id')).toBe(usageID(21))
    expect(f.owner.clearExecutionFilter()).toBe(true)
    await flushPromises()
    expect(f.owner.mode.value).toBe('daily')
    expect(f.owner.executionID.value).toBeNull()
    expect(queries(f).at(-1)!.get('to')).toBe(refreshed.to)
    expect(queries(f).at(-1)!.has('execution_id')).toBe(false)
    f.owner.showMode('records')
    await flushPromises()
    expect(f.owner.page.index).toBe(0)
    const count = f.fetch.mock.calls.length
    f.owner.previous()
    expect(f.fetch.mock.calls).toHaveLength(count)
    f.owner.next()
    await flushPromises()
    expect(queries(f).at(-1)!.get('cursor')).toBe('all-page-2')
    expect(queries(f).at(-1)!.has('execution_id')).toBe(false)
  })

  it('keeps a recoverable filter on read failure and clears it on a current permission rejection', async () => {
    const f = await setup(),
      original = { ...f.owner.window.value }
    f.intercept((path, init) =>
      path.startsWith(usageBase) ? Promise.resolve(usageProblem(503)) : f.normal(path, init),
    )
    f.owner.select(usageID(100))
    f.owner.filterSelectedExecution()
    await flushPromises()
    expect(f.owner.page.phase).toBe('error')
    expect(f.owner.executionID.value).toBe(usageID(21))
    expect(f.owner.window.value?.from).toBe(original.from)
    expect(f.owner.page.items).toEqual([])
    f.intercept()
    f.owner.clearExecutionFilter()
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(queries(f).at(-1)!.get('to')).toBe(original.to)
    f.intercept((path, init) =>
      path.startsWith(usageBase) ? Promise.resolve(usageProblem(403)) : f.normal(path, init),
    )
    f.owner.select(usageID(100))
    f.owner.filterSelectedExecution()
    await flushPromises()
    expect(f.owner.page.phase).toBe('unavailable')
    expect(f.owner.executionID.value).toBeNull()
    expect(f.owner.window.value).toBeNull()
    expect(f.owner.selected.value).toBeNull()
    expect(f.auth.state.phase).toBe('authenticated')
    const count = f.fetch.mock.calls.length
    expect(f.owner.filterSelectedExecution()).toBe(false)
    expect(f.owner.clearExecutionFilter()).toBe(false)
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(count)
  })

  it('does not publish a cancelled execution read or use its cursor for a subsequently selected execution', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(usagePage(fullPage(), 'retired-cursor'))))
    f.intercept((path, init) => (path.startsWith(usageBase) ? held.promise : f.normal(path, init)))
    f.owner.select(usageID(100))
    f.owner.filterSelectedExecution()
    await flushPromises()
    f.owner.cancel()
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.executionID.value).toBe(usageID(21))
    expect(f.owner.clearExecutionFilter()).toBe(false)
    held.resolve(usageJSON(usagePage(fullPage(), 'retired-cursor')))
    await flushPromises()
    expect(f.owner.page.phase).toBe('error')
    expect(f.owner.page.nextCursor).toBeNull()
    expect(f.auth.state.busy).toBe(false)
    const other = { ...usageRecord(), execution_id: usageID(22) }
    f.intercept((path, init) =>
      path.startsWith(usageBase)
        ? Promise.resolve(usageJSON(usagePage([other])))
        : f.normal(path, init),
    )
    f.owner.clearExecutionFilter()
    await flushPromises()
    f.owner.select(other.id)
    expect(f.owner.filterSelectedExecution()).toBe(true)
    await flushPromises()
    expect(f.owner.executionID.value).toBe(usageID(22))
    expect(f.owner.page.items[0]!.execution_id).toBe(usageID(22))
    expect(queries(f).at(-1)!.has('cursor')).toBe(false)
    expect(queries(f).at(-1)!.get('execution_id')).toBe(usageID(22))
  })

  it('clears the execution filter across projects and rejects the previous project late summary', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(dailyPage())))
    f.owner.select(usageID(100))
    f.owner.filterSelectedExecution()
    await flushPromises()
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
    expect(f.owner.executionID.value).toBeNull()
    held.resolve(usageJSON(dailyPage()))
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.owner.daily.items).toEqual([])
    const last = new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid')
    expect(last.pathname).toBe('/api/v1/projects/' + usageID(60) + '/model-usage/summary')
    expect(last.searchParams.has('execution_id')).toBe(false)
  })

  it('drops execution context during identity checking and starts the new Session without the old filter', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(f.sessionValue())))
    f.owner.select(usageID(100))
    f.owner.filterSelectedExecution()
    await flushPromises()
    f.intercept((path, init) => (path === '/api/v1/session' ? held.promise : f.normal(path, init)))
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.owner.executionID.value).toBeNull()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.page.items).toEqual([])
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: usageID(4) } })
    held.resolve(usageJSON(f.sessionValue()))
    await checking
    await flushPromises()
    f.workspace.afterNavigation(usagePath)
    await flushPromises()
    expect(f.owner.page.phase).toBe('ready')
    expect(f.auth.personalContext.identity?.sessionID).toBe(usageID(4))
    expect(queries(f).at(-1)!.has('execution_id')).toBe(false)
  })
})
