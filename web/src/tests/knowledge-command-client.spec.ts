import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const base = `/api/v1/projects/${id(1)}/knowledge/documents`
const input = { expected_version: '9007199254740993', title: '  中文🙂  ' }
const write = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-key',
  signal: new AbortController().signal,
})
const row = () => ({
  id: id(2),
  project_id: id(1),
  parent_document_id: id(3),
  title: input.title,
  content_version: '9007199254740994',
  source_kind: 'text',
  media_type: 'text/plain',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(4) },
  created_at: '2026-10-10T12:00:00Z',
  updated_at: '2026-10-10T12:00:00Z',
})
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })

describe('Knowledge rename and original lookup HTTP contract', () => {
  it('uses exactly two POSTs with original key/CSRF and exact Unicode/version bytes', async () => {
    const responses = [
      { document: row() },
      { state: 'committed', receipt: { command: 'update', document: row(), changed: true } },
    ]
    const fetch = vi.fn<Fetch>(async () => json(responses.shift()))
    const api = createKnowledgeCommandsAPI(fetch),
      options = write()
    const renamed = await api.rename(id(1), id(2), input, options)
    const found = await api.lookup(id(1), id(2), input, options)
    expect(renamed.document.parent_document_id).toBe(id(3))
    expect(found).toEqual({
      state: 'committed',
      receipt: { command: 'update', document: row(), changed: true },
    })
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/${id(2)}/rename`,
      `${base}/commands/lookup`,
    ])
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      input,
      { command: 'update', document_id: id(2), request: input },
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: options.signal,
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(options.key)
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(options.csrfToken)
    }
    expect(Object.isFrozen(renamed.document)).toBe(true)
  })
  it('accepts no-op only with unchanged version and validates noncommitted receipt:null', async () => {
    const unchanged = { ...row(), content_version: input.expected_version }
    let value: unknown = { document: unchanged }
    const api = createKnowledgeCommandsAPI(async () => json(value))
    expect((await api.rename(id(1), id(2), input, write())).document).toEqual(unchanged)
    value = {
      state: 'committed',
      receipt: { command: 'update', document: unchanged, changed: false },
    }
    await expect(api.lookup(id(1), id(2), input, write())).resolves.toEqual(value)
    for (const state of ['not_observed', 'in_progress']) {
      value = { state, receipt: null }
      await expect(api.lookup(id(1), id(2), input, write())).resolves.toEqual(value)
    }
  })
  it.each([
    { ...input, title: '' },
    { ...input, title: '\ud800' },
    { ...input, title: 'a\n' },
    { ...input, title: '🙂'.repeat(513) },
    { ...input, expected_version: '01' },
    { ...input, expected_version: '9223372036854775808' },
    { ...input, extra: true },
  ])('rejects bad intent before dispatch %j', async (value) => {
    const fetch = vi.fn<Fetch>(),
      api = createKnowledgeCommandsAPI(fetch)
    await expect(api.rename(id(1), id(2), value, write())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([
    { document: { ...row(), id: id(8) } },
    { document: { ...row(), title: 'other' } },
    { document: { ...row(), content_version: '9007199254740995' } },
    { document: { ...row(), object_id: id(8) } },
    { document: row(), changed: true },
  ])('rejects malformed or unrelated rename result %j', async (value) => {
    await expect(
      createKnowledgeCommandsAPI(async () => json(value)).rename(id(1), id(2), input, write()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each([
    { state: 'not_observed' },
    { state: 'in_progress', receipt: {} },
    { state: 'committed', receipt: { command: 'move', document: row(), changed: true } },
    { state: 'committed', receipt: { command: 'update', document: row(), changed: false } },
  ])('rejects invalid lookup union or version %j', async (value) => {
    await expect(
      createKnowledgeCommandsAPI(async () => json(value)).lookup(id(1), id(2), input, write()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('keeps closed transport options and waits for original body cancellation', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    const response = json({ document: row() })
    const outer = vi.spyOn(response.body!, 'cancel').mockImplementation(() => held)
    const api = createKnowledgeCommandsAPI(async () => response)
    let returned = false
    const pending = api.rename(id(1), id(2), input, write()).then(() => {
      returned = true
    })
    await vi.waitFor(() => expect(outer).toHaveBeenCalledOnce())
    expect(returned).toBe(false)
    release()
    await pending
    const fetch = vi.fn<Fetch>()
    await expect(
      accountTransport(fetch)('knowledgeRename', (x) => x, {
        projectID: id(1),
        target: id(2),
        body: input,
        csrf: 'S'.repeat(43),
        key: 'original-key',
        signal: new AbortController().signal,
        knowledgeContent: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
})
