import { describe, expect, it, vi } from 'vitest'
import { type Fetch } from '../api/client'
import { captureKnowledgeMove, createKnowledgeMoveAPI } from '../api/knowledge-move'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const base = `/api/v1/projects/${id(1)}/knowledge/documents`
const input = { expected_parent_id: id(3), target_parent_id: null }
const write = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-move',
  signal: new AbortController().signal,
})
const row = () => ({
  id: id(2),
  project_id: id(1),
  parent_document_id: null,
  title: '并发编辑后的标题',
  content_version: '9007199254740993',
  source_kind: 'text',
  media_type: 'text/plain',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(4) },
  created_at: '2026-10-10T12:00:00.000000Z',
  updated_at: '2026-10-10T12:00:00.000000Z',
})
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })

describe('Knowledge Move complete original-parent contract', () => {
  it('uses the original two parents and key for Move and full-intent Lookup, accepting concurrent content', async () => {
    const values = [
      { document: row(), changed: true },
      { state: 'committed', receipt: { command: 'move', document: row(), changed: true } },
    ]
    const fetch = vi.fn<Fetch>(async () => json(values.shift()))
    const api = createKnowledgeMoveAPI(fetch),
      options = write()
    const moved = await api.move(id(1), id(2), input, options)
    expect(moved.document.content_version).toBe('9007199254740993')
    await expect(api.lookup(id(1), id(2), input, options)).resolves.toMatchObject({
      state: 'committed',
      receipt: { document: row(), changed: true },
    })
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `${base}/${id(2)}/move`,
      `${base}/commands/lookup`,
    ])
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      input,
      { command: 'move', document_id: id(2), request: input },
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: options.signal,
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe('original-move')
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(options.csrfToken)
    }
  })
  it.each([
    { target_parent_id: null },
    { expected_parent_id: null },
    { ...input, expected_parent_id: undefined },
    { ...input, target_parent_id: undefined },
    { ...input, expected_version: '1' },
    { ...input, target_parent_id: 'not-an-id' },
  ])('rejects incomplete/extra parents before transport %j', async (value) => {
    expect(() => captureKnowledgeMove(value)).toThrowError()
    const fetch = vi.fn<Fetch>()
    await expect(
      createKnowledgeMoveAPI(fetch).move(id(1), id(2), value as never, write()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('supports explicit root/root no-op and noncommitted receipt:null', async () => {
    const same = { expected_parent_id: null, target_parent_id: null }
    const api = createKnowledgeMoveAPI(async () => json({ document: row(), changed: false }))
    await expect(api.move(id(1), id(2), same, write())).resolves.toEqual({
      document: row(),
      changed: false,
    })
    for (const state of ['not_observed', 'in_progress']) {
      await expect(
        createKnowledgeMoveAPI(async () => json({ state, receipt: null })).lookup(
          id(1),
          id(2),
          same,
          write(),
        ),
      ).resolves.toEqual({ state, receipt: null })
    }
  })
  it.each([
    { document: row(), changed: false },
    { document: { ...row(), parent_document_id: id(3) }, changed: true },
    { document: { ...row(), project_id: id(9) }, changed: true },
    { document: { ...row(), object_id: id(9) }, changed: true },
    { document: row() },
  ])('rejects mismatched or unsafe complete receipt %j', async (value) => {
    await expect(
      createKnowledgeMoveAPI(async () => json(value)).move(id(1), id(2), input, write()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects wrong Lookup command and partial/noncommitted receipts', async () => {
    for (const value of [
      { state: 'committed', receipt: { command: 'update', document: row(), changed: true } },
      { state: 'not_observed' },
      { state: 'in_progress', receipt: {} },
    ])
      await expect(
        createKnowledgeMoveAPI(async () => json(value)).lookup(id(1), id(2), input, write()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('does not publish before EOF and joins response cancellation', async () => {
    let finish!: () => void
    const response = new Response(
      new ReadableStream({
        start(controller) {
          controller.enqueue(
            new TextEncoder().encode(JSON.stringify({ document: row(), changed: true })),
          )
          finish = () => controller.close()
        },
      }),
      { headers: { 'Content-Type': 'application/json' } },
    )
    let returned = false
    const pending = createKnowledgeMoveAPI(async () => response)
      .move(id(1), id(2), input, write())
      .then((v) => {
        returned = true
        return v
      })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(returned).toBe(false)
    finish()
    await expect(pending).resolves.toMatchObject({ changed: true })
  })
})
