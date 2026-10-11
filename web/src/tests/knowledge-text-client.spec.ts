import { describe, expect, it, vi } from 'vitest'
import {
  captureKnowledgeText,
  createKnowledgeTextAPI,
  type KnowledgeTextIntent,
} from '../api/knowledge-text'
import { type Fetch } from '../api/client'
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const intent: KnowledgeTextIntent = {
  command: 'create',
  document_id: id(2),
  request: {
    parent_document_id: null,
    title: '  文档🙂  ',
    media_type: 'text/markdown',
    text: '  # 中文\r\n\n  ',
  },
}
const options = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-key',
  signal: new AbortController().signal,
})
const row = () => ({
  id: id(2),
  project_id: id(1),
  parent_document_id: null,
  title: intent.request.title,
  content_version: '1',
  source_kind: 'text',
  media_type: 'text/markdown',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(4) },
  created_at: '2026-10-10T12:00:00.000000Z',
  updated_at: '2026-10-10T12:00:00.000000Z',
})
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
describe('Knowledge text complete encoded intent wire', () => {
  it('dispatches closed create, replace and original lookup with unchanged bytes and key', async () => {
    const update: KnowledgeTextIntent = {
      command: 'update',
      document_id: id(2),
      request: { expected_version: '1', media_type: 'text/plain', text: '' },
    }
    const changed = { ...row(), content_version: '2', media_type: 'text/plain' }
    const replies = [
      { document: row() },
      { document: changed },
      { state: 'committed', receipt: { command: 'update', document: changed, changed: true } },
    ]
    const fetch = vi.fn<Fetch>(async () => json(replies.shift())),
      api = createKnowledgeTextAPI(fetch)
    await api.write(id(1), intent, options())
    await api.write(id(1), update, options())
    await api.lookup(id(1), update, options())
    const base = `/api/v1/projects/${id(1)}/knowledge/documents`
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/${id(2)}/create-text`,
      `${base}/${id(2)}/replace-text`,
      `${base}/text-commands/lookup`,
    ])
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      intent.request,
      update.request,
      update,
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe('original-key')
    }
  })
  it('looks up the accepted maximum-size body and rejects one extra encoded byte', async () => {
    const empty = { ...intent, request: { ...intent.request, text: '' } }
    const amount = 1048576 - new TextEncoder().encode(JSON.stringify(empty.request)).length
    const maximum = captureKnowledgeText({
      ...empty,
      request: { ...empty.request, text: 'x'.repeat(amount) },
    })
    expect(new TextEncoder().encode(JSON.stringify(maximum.request)).length).toBe(1048576)
    expect(new TextEncoder().encode(JSON.stringify(maximum)).length).toBe(1048660)
    const fetch = vi.fn<Fetch>(async () => json({ state: 'not_observed', receipt: null }))
    await expect(createKnowledgeTextAPI(fetch).lookup(id(1), maximum, options())).resolves.toEqual({
      state: 'not_observed',
      receipt: null,
    })
    expect(fetch).toHaveBeenCalledOnce()
    expect(() =>
      captureKnowledgeText({
        ...maximum,
        request: { ...maximum.request, text: maximum.request.text + 'x' },
      }),
    ).toThrow()
    expect(() =>
      captureKnowledgeText({ ...empty, request: { ...empty.request, text: '\n'.repeat(524288) } }),
    ).toThrow()
  })
  it.each([
    { ...intent, document_id: 'other' },
    { ...intent, request: { ...intent.request, parent_document_id: id(2) } },
    { ...intent, request: { ...intent.request, title: '\ud800' } },
    { ...intent, request: { ...intent.request, title: 'x\n' } },
    { ...intent, request: { ...intent.request, title: '🙂'.repeat(513) } },
    { ...intent, request: { ...intent.request, text: '\udc00' } },
    { ...intent, request: { ...intent.request, media_type: 'text/html' } },
    { ...intent, request: { ...intent.request, object_id: id(3) } },
    {
      command: 'update',
      document_id: id(2),
      request: { expected_version: '01', media_type: 'text/plain', text: '' },
    },
  ])('rejects invalid original intent %#', (value) => {
    expect(() => captureKnowledgeText(value)).toThrowError()
  })
  it('validates no-op version and rejects receipt identity, metadata and changed mismatches', async () => {
    const update: KnowledgeTextIntent = {
      command: 'update',
      document_id: id(2),
      request: { expected_version: '1', media_type: 'text/markdown', text: 'same' },
    }
    const valid = {
      state: 'committed',
      receipt: { command: 'update', document: row(), changed: false },
    }
    await expect(
      createKnowledgeTextAPI(async () => json(valid)).lookup(id(1), update, options()),
    ).resolves.toEqual(valid)
    for (const receipt of [
      { ...valid.receipt, changed: true },
      { ...valid.receipt, command: 'create' },
      { ...valid.receipt, document: { ...row(), id: id(8) } },
      { ...valid.receipt, document: { ...row(), object_id: id(8) } },
    ])
      await expect(
        createKnowledgeTextAPI(async () => json({ ...valid, receipt })).lookup(
          id(1),
          update,
          options(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
