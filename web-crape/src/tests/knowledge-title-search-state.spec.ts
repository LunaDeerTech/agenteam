import { afterEach, describe, expect, it, vi } from 'vitest'
import { shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { KnowledgeTitlePage } from '../api/knowledge-title-search'
import {
  useKnowledgeTitleSearch,
  type KnowledgeTitleSearch,
} from '../composables/useKnowledgeTitleSearch'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  titleDocument as doc,
  titleID as id,
  titleProject,
  titleDeferred,
} from './knowledge-title-search.fixture'
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const pages: KnowledgeTitleSearch[] = []
afterEach(() => {
  for (const page of pages.splice(0)) page.dispose()
})
const result = (n = 10, title = 'x', next_cursor?: string): KnowledgeTitlePage => ({
  items: [{ document: doc(n, title), ancestors: [] }],
  ...(next_cursor ? { next_cursor } : {}),
})
function fixture() {
  const context = shallowRef<Context | null>({
    projectID: titleProject,
    generation: 1,
    readGeneration: 1,
    identity: { userID: id(2), sessionID: id(3), epoch: 1 },
  })
  const blocked = shallowRef(false)
  const port = { searchTitles: vi.fn(async () => result()), abandon: vi.fn() }
  const page = useKnowledgeTitleSearch(port, context, blocked, 1)
  pages.push(page)
  return { page, port, context, blocked }
}
describe('Knowledge title search observation ownership', () => {
  it('requires explicit search, then binds each next page to the captured query and cursor', async () => {
    const { page, port } = fixture()
    page.setDraft('x')
    expect(port.searchTitles).not.toHaveBeenCalled()
    port.searchTitles
      .mockResolvedValueOnce(result(10, 'x old', 'next'))
      .mockResolvedValueOnce(result(10, 'x renamed'))
    await page.search()
    await page.more()
    expect(port.searchTitles.mock.calls).toEqual([
      [titleProject, { title_query: 'x', limit: 1 }],
      [titleProject, { title_query: 'x', limit: 1, cursor: 'next' }],
    ])
    expect(page.state.items).toHaveLength(1)
    expect(page.state.items[0]?.document.title).toBe('x renamed')
    expect(page.canMore.value).toBe(false)
  })
  it('retires an edited query and never publishes its late result or cancels another owner', async () => {
    const { page, port, blocked } = fixture(),
      pending = titleDeferred<KnowledgeTitlePage>()
    port.searchTitles.mockReturnValueOnce(pending.promise)
    page.setDraft('x')
    const old = page.search()
    blocked.value = true
    page.setDraft('new')
    expect(port.abandon).toHaveBeenCalledTimes(1)
    page.clear()
    expect(port.abandon).toHaveBeenCalledTimes(1)
    page.setDraft('new')
    await page.search()
    expect(port.searchTitles).toHaveBeenCalledTimes(1)
    pending.resolve(result())
    await old
    expect(page.state.items).toEqual([])
    expect(page.state.draft).toBe('new')
  })
  it('clears private query/results/cursor on checking or project generation changes without auto-query', async () => {
    const { page, context, port } = fixture()
    page.setDraft('x')
    port.searchTitles.mockResolvedValueOnce(result(10, 'x', 'next'))
    await page.search()
    const previous = context.value!
    context.value = null
    expect(page.visible.value).toBe(false)
    expect(page.state).toMatchObject({ draft: '', items: [], query: null, nextCursor: undefined })
    context.value = { ...previous, readGeneration: 2 }
    expect(page.visible.value).toBe(true)
    expect(port.searchTitles).toHaveBeenCalledTimes(1)
  })
  it('preserves only previous complete results on page loss and lets the user explicitly retry', async () => {
    const { page, port } = fixture()
    page.setDraft('x')
    port.searchTitles
      .mockResolvedValueOnce(result(10, 'x', 'next'))
      .mockRejectedValueOnce(new AccountFailure('transport'))
    await page.search()
    await page.more()
    expect(page.state.phase).toBe('error')
    expect(page.state.items).toHaveLength(1)
    expect(page.canMore.value).toBe(true)
    await page.more()
    expect(page.state.phase).toBe('ready')
    expect(port.searchTitles).toHaveBeenCalledTimes(3)
  })
  it('drops current denied metadata and binds selection across asynchronous unsaved confirmation', async () => {
    const { page, port } = fixture()
    page.setDraft('x')
    await page.search()
    const selected = page.selection(id(10))!
    expect(selected()).toBe(true)
    page.setDraft('changed')
    expect(selected()).toBe(false)
    const problem = new AccountFailure('problem', {
      type: 'urn:test',
      title: 'Forbidden',
      status: 403,
      detail: '',
      instance: '/',
      code: 'FORBIDDEN',
      request_id: id(99),
      commit_state: 'not_started',
    })
    port.searchTitles.mockRejectedValueOnce(problem)
    await page.search()
    expect(page.state).toMatchObject({ draft: '', items: [], query: null, phase: 'unavailable' })
    expect(page.busy.value).toBe(false)
  })
  it('never starts calls after dispose and rejects a repeating pagination cursor', async () => {
    const { page, port } = fixture()
    page.setDraft('x')
    port.searchTitles
      .mockResolvedValueOnce(result(10, 'x', 'next'))
      .mockResolvedValueOnce(result(11, 'x2', 'next'))
    await page.search()
    await page.more()
    expect(page.state.phase).toBe('error')
    expect(page.state.items.map((item) => item.document.id)).toEqual([id(10)])
    page.dispose()
    page.setDraft('x')
    await page.search()
    expect(port.searchTitles).toHaveBeenCalledTimes(2)
  })
})
