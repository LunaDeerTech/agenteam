import { describe, expect, it, vi } from 'vitest'
import { createKnowledgeTitleSearchAPI } from '../api/knowledge-title-search'
import type { Fetch } from '../api/client'
import { titleDocument as doc, titleProject, titleDeferred } from './knowledge-title-search.fixture'
const signal = () => new AbortController().signal
const body = { items: [{ document: doc(), ancestors: [] }] }
const query = { title_query: '%_!', limit: 50 }
describe('Knowledge title search actual shared transport', () => {
  it('uses only a fixed same-origin GET with literal query and no write credentials', async () => {
    const fetch = vi.fn<Fetch>(
      async () =>
        new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }),
    )
    const page = await createKnowledgeTitleSearchAPI(fetch).searchTitles(
      titleProject,
      query,
      signal(),
    )
    expect(page.items[0]?.document.title).toBe(doc().title)
    const [path, init] = fetch.mock.calls[0]!
    expect(path).toBe(
      `/api/v1/projects/${titleProject}/knowledge/documents/search-titles?title_query=%25_%21&limit=50`,
    )
    expect(init).toMatchObject({
      method: 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
    })
    expect(init.body).toBeUndefined()
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
    expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
  })
  it('does not publish a complete-looking prefix until its original body reaches EOF', async () => {
    let controller!: ReadableStreamDefaultController<Uint8Array>
    const stream = new ReadableStream<Uint8Array>({
      start(value) {
        controller = value
      },
    })
    const api = createKnowledgeTitleSearchAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    )
    const published = vi.fn()
    const call = api.searchTitles(titleProject, query, signal()).then(published)
    controller.enqueue(new TextEncoder().encode(JSON.stringify(body)))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(published).not.toHaveBeenCalled()
    controller.close()
    await call
    expect(published).toHaveBeenCalledOnce()
    expect(stream.locked).toBe(false)
  })
  it('rejects malformed UTF-8 and duplicate JSON members before any typed publication', async () => {
    for (const raw of [
      new Uint8Array([0xff]),
      new TextEncoder().encode('{"items":[],"items":[]}'),
    ]) {
      const api = createKnowledgeTitleSearchAPI(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(api.searchTitles(titleProject, query, signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('keeps the read pending until the original cancellation returns after the 5MiB cap', async () => {
    const closed = titleDeferred<void>(),
      cancel = vi.fn(() => closed.promise)
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new Uint8Array(5 * 1024 * 1024 + 1))
      },
      cancel,
    })
    const api = createKnowledgeTitleSearchAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    )
    let done = false
    const call = api
      .searchTitles(titleProject, query, signal())
      .catch((error: unknown) => error)
      .finally(() => {
        done = true
      })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(cancel).toHaveBeenCalledOnce()
    expect(done).toBe(false)
    closed.resolve()
    expect(await call).toMatchObject({ kind: 'invalid-response' })
    expect(stream.locked).toBe(false)
  })
})
