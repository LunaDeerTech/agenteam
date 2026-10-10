import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import {
  createKnowledgeOwnerAPI,
  compareKnowledgeDocuments,
  parseKnowledgeDocument,
} from '../api/knowledge-owner'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(10),
  base = `/api/v1/projects/${project}/knowledge/documents`
const signal = () => new AbortController().signal
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
const document = (n = 10, parent: string | null = null) => ({
  id: id(n),
  project_id: project,
  parent_document_id: parent,
  title: '中文 <script>',
  content_version: '9007199254740993',
  source_kind: 'text',
  media_type: 'text/markdown',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(2) },
  created_at: '2026-10-10T12:00:00.123456Z',
  updated_at: '2026-10-10T12:00:00.123456Z',
})
const text = (value = '中文🙂', offset = '10', truncated = false) => ({
  document: document(),
  text: { text: value, next_byte_offset: offset, truncated },
})

describe('Knowledge four current Owner reads', () => {
  it('uses fixed same-origin GETs, explicit root and exact decimal offsets without write headers', async () => {
    const responses = [{ items: [document()] }, { active: document() }, { items: [] }, text()]
    const fetch = vi.fn<Fetch>(async () => json(responses.shift()))
    const api = createKnowledgeOwnerAPI(fetch)
    const page = await api.children(project, null, { limit: 50 }, signal())
    const head = await api.get(project, target, signal())
    const ancestors = await api.ancestors(project, target, signal())
    const content = await api.readContent(project, target, {}, signal())
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/children?parent_document_id=null&limit=50`,
      `${base}/${target}`,
      `${base}/${target}/ancestors`,
      `${base}/${target}/content?byte_offset=0&max_bytes=65536`,
    ])
    expect(page.items[0]?.content_version).toBe('9007199254740993')
    expect(ancestors).toEqual([])
    expect(content).toMatchObject({ text: { next_byte_offset: '10' } })
    expect([page, page.items, page.items[0], head, content].every(Object.isFrozen)).toBe(true)
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.body).toBeUndefined()
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
      expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
    }
  })
  it('keeps non-ASCII title ordering aligned with UTF-8 C ordering', async () => {
    const first = { ...document(10), title: '\uE000' },
      last = { ...document(11), title: '🙂' }
    expect(
      compareKnowledgeDocuments(
        parseKnowledgeDocument(first, project),
        parseKnowledgeDocument(last, project),
      ),
    ).toBeLessThan(0)
    const api = createKnowledgeOwnerAPI(async () => json({ items: [first, last] }))
    expect((await api.children(project, null, { limit: 50 }, signal())).items).toHaveLength(2)
  })
  it('captures query values once and does not retain mutable caller inputs', async () => {
    let reads = 0
    const query = {
      get limit() {
        reads++
        return 1
      },
      cursor: 'previous._-',
    }
    const api = createKnowledgeOwnerAPI(async (path) => {
      expect(path).toBe(`${base}/children?parent_document_id=${id(3)}&limit=1&cursor=previous._-`)
      return json({ items: [document(10, id(3))], next_cursor: 'next' })
    })
    await api.children(project, id(3), query, signal())
    expect(reads).toBe(1)
  })
  it.each([
    { limit: 0 },
    { limit: 201 },
    { limit: 1.5 },
    { limit: 50, cursor: '' },
    { limit: 50, cursor: '\ud800' },
    { limit: 50, cursor: 'x'.repeat(8193) },
    { limit: 50, other: true },
  ])('rejects invalid children inputs before fetch: %j', async (query) => {
    const fetch = vi.fn<Fetch>(),
      api = createKnowledgeOwnerAPI(fetch)
    await expect(api.children(project, null, query, signal())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    { byte_offset: '01' },
    { byte_offset: '9223372036854775808' },
    { byte_offset: -1 },
    { max_bytes: 0 },
    { max_bytes: 1048577 },
    { max_bytes: '65536' },
  ])('rejects invalid content inputs before fetch: %j', async (request) => {
    const fetch = vi.fn<Fetch>(),
      api = createKnowledgeOwnerAPI(fetch)
    await expect(
      api.readContent(project, target, request as never, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('accepts EOF and a valid no-progress small budget without inventing an offset', async () => {
    let result = text('', '9007199254740993', false)
    const api = createKnowledgeOwnerAPI(async () => json(result))
    expect(
      await api.readContent(project, target, { byte_offset: '9007199254740993' }, signal()),
    ).toEqual(result)
    result = text('', '0', true)
    expect(await api.readContent(project, target, { max_bytes: 1 }, signal())).toEqual(result)
  })
  it('accepts only the current file-unavailable branch and a safe tombstone', async () => {
    const file = { ...document(), source_kind: 'file', media_type: 'application/pdf' }
    let result: unknown = { document: file, unavailable: 'dependency_unbound' }
    const api = createKnowledgeOwnerAPI(async () => json(result))
    expect(await api.readContent(project, target, {}, signal())).toEqual(result)
    result = {
      deleted: {
        id: target,
        project_id: project,
        content_version: '2',
        deleted_at: '2026-10-10T12:00:00.123456Z',
      },
    }
    expect(await api.get(project, target, signal())).toEqual(result)
  })
  it.each([
    { items: [document(), { ...document(11), object_id: id(90) }] },
    { items: [document(), document()] },
    { items: [{ ...document(), project_id: id(9) }] },
    { items: [document(10, id(3))] },
    { items: [document()], next_cursor: null },
    { items: [document()], next_cursor: 'next' },
    { items: [{ ...document(), title: '\ud800' }] },
    {
      items: [
        {
          ...document(),
          created_by: {
            kind: 'agent_run',
            project_id: id(9),
            agent_id: id(3),
            execution_id: id(4),
          },
        },
      ],
    },
  ])('rejects a malformed complete page without returning an earlier prefix', async (result) => {
    await expect(
      createKnowledgeOwnerAPI(async () => json(result)).children(
        project,
        null,
        { limit: 50 },
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('validates the full ancestor chain without treating it as a children page', async () => {
    const result = { items: [document(3), document(4, id(3))] }
    const api = createKnowledgeOwnerAPI(async () => json(result))
    expect(await api.ancestors(project, target, signal())).toEqual(result.items)
    result.items[1] = document(4, null)
    await expect(api.ancestors(project, target, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    result.items = [document(10)]
    await expect(api.ancestors(project, target, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it.each([
    text('中文', '2'),
    text('\ud800', '3'),
    { ...text(), unavailable: 'dependency_unbound' },
    { ...text(), document: { ...document(), id: id(11) } },
    { document: document(), unavailable: 'dependency_unbound' },
    { document: document(), text: { text: 'x', next_byte_offset: '1', truncated: 1 } },
  ])('rejects cross-target, malformed union or mismatched byte identity', async (result) => {
    await expect(
      createKnowledgeOwnerAPI(async () => json(result)).readContent(project, target, {}, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects duplicate JSON members and wrong endpoint options on the actual transport', async () => {
    const api = createKnowledgeOwnerAPI(
      async () =>
        new Response('{"items":[],"items":[]}', {
          headers: { 'Content-Type': 'application/json' },
        }),
    )
    await expect(api.children(project, null, { limit: 50 }, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    const fetch = vi.fn<Fetch>()
    await expect(
      accountTransport(fetch)('knowledgeDocument', (v) => v, {
        projectID: project,
        target,
        signal: signal(),
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('uses the existing content representation limit instead of the generic 600000-byte cap', async () => {
    const body = 'x'.repeat(1048576),
      result = text(body, '1048576')
    const api = createKnowledgeOwnerAPI(async () => json(result))
    expect(await api.readContent(project, target, { max_bytes: 1048576 }, signal())).toEqual(result)
  })
})
