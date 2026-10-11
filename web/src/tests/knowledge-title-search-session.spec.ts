import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed } from 'vue'
import { useKnowledgeTitleSearch } from '../composables/useKnowledgeTitleSearch'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  titleSessionFixture,
  titleDeferred,
  titleJSON,
  titleChild,
  titleRoot,
  titleBase,
  titleID,
} from './knowledge-title-search.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
})
async function fixture() {
  const f = titleSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation('/owner/title-search/knowledge')
  await flushPromises()
  expect(workspace.currentReadContext.value).not.toBeNull()
  const page = useKnowledgeTitleSearch(
    f.auth.knowledge,
    workspace.currentReadContext,
    computed(() => f.auth.state.busy),
  )
  cleanups.push(() => {
    page.dispose()
    workspace.dispose()
    f.auth.leave()
  })
  return { ...f, page, workspace }
}
describe('Title search with the real Session request owner', () => {
  it('retains Cookie ownership until original reader cancellation returns, then permits a new explicit search', async () => {
    const f = await fixture(),
      tail = titleDeferred<void>(),
      cancelled = vi.fn(() => tail.promise)
    cleanups.unshift(() => tail.resolve())
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"items":['))
      },
      cancel: cancelled,
    })
    f.intercept(async (path, init) =>
      path.startsWith(`${titleBase}/search-titles?`)
        ? new Response(stream, { headers: { 'Content-Type': 'application/json' } })
        : f.normal(path, init),
    )
    f.page.setDraft('%_!')
    const first = f.page.search()
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    f.page.cancel()
    await flushPromises()
    expect(cancelled).toHaveBeenCalledOnce()
    expect(f.auth.state.busy).toBe(true)
    f.page.setDraft('子')
    await f.page.search()
    expect(f.fetch.mock.calls.filter(([p]) => p.includes('/search-titles?'))).toHaveLength(1)
    tail.resolve()
    await first
    await flushPromises()
    expect(stream.locked).toBe(false)
    expect(f.auth.state.busy).toBe(false)
    f.intercept()
    await f.page.search()
    expect(f.page.state.items[0]?.document.id).toBe(titleChild().id)
  })
  it('retires an old Project response before publishing into a newly resolved workspace', async () => {
    const f = await fixture(),
      response = titleDeferred<Response>()
    cleanups.unshift(() => response.resolve(titleJSON({ items: [] })))
    f.intercept(async (path, init) =>
      path.startsWith(`${titleBase}/search-titles?`) ? response.promise : f.normal(path, init),
    )
    f.page.setDraft('%_!')
    const old = f.page.search()
    await flushPromises()
    f.project({
      ...f.workspace.detail.project!,
      id: titleID(50),
      name: 'Other',
      normalized_name: 'other',
    })
    f.workspace.afterNavigation('/owner/other/knowledge')
    expect(f.page.state.items).toEqual([])
    expect(f.page.state.draft).toBe('')
    response.resolve(titleJSON({ items: [{ document: titleChild(), ancestors: [titleRoot()] }] }))
    await old
    await flushPromises()
    expect(f.page.state.items).toEqual([])
    expect(f.auth.state.busy).toBe(false)
    expect(f.workspace.currentReadContext.value?.projectID).toBe(titleID(50))
  })
  it('clears old read observations during the same Session check without automatically searching on return', async () => {
    const f = await fixture()
    f.page.setDraft('%_!')
    await f.page.search()
    const check = titleDeferred<Response>()
    cleanups.unshift(() => check.resolve(titleJSON({})))
    f.intercept((path, init) => (path === '/api/v1/session' ? check.promise : f.normal(path, init)))
    const restore = f.auth.restore()
    await flushPromises()
    expect(f.page.state.items).toEqual([])
    expect(f.page.visible.value).toBe(false)
    check.resolve(await f.normal('/api/v1/session', { method: 'GET' }))
    await restore
    await flushPromises()
    expect(f.page.state.draft).toBe('')
    expect(f.fetch.mock.calls.filter(([p]) => p.includes('/search-titles?'))).toHaveLength(1)
  })
})
