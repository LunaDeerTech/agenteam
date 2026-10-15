import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureKnowledgeFile,
  createKnowledgeFileAPI,
  encodeKnowledgeFile,
  type KnowledgeFileAPI,
  type KnowledgeFileIntent,
} from '../api/knowledge-file'
import type { KnowledgeDocument } from '../api/knowledge-owner'
import {
  createKnowledgeFileSession,
  type KnowledgeFileSessionPorts,
} from '../composables/knowledge-file-session'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const intent: KnowledgeFileIntent = {
  command: 'create',
  document_id: id(2),
  request: {
    parent_document_id: null,
    title: '原文件',
    media_type: 'application/pdf',
    bytes_base64: 'AP8NCg==',
  },
}
const row = (): KnowledgeDocument => ({
  id: id(2),
  project_id: id(1),
  parent_document_id: null,
  title: '原文件',
  content_version: '1',
  source_kind: 'file',
  media_type: 'application/pdf',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(4) },
  created_at: '2026-10-10T12:00:00.000000Z',
  updated_at: '2026-10-10T12:00:00.000000Z',
})
const options = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-key',
  signal: new AbortController().signal,
})
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })

describe('Knowledge file original intent and current source', () => {
  it('sends the captured binary identity and checks exact source revision', async () => {
    const replies = [
      { document: row() },
      { state: 'not_observed', receipt: null },
      { document: row(), bytes_base64: 'AP8NCg==' },
    ]
    const fetch = vi.fn<Fetch>(async () => json(replies.shift())),
      api = createKnowledgeFileAPI(fetch)
    await api.write(id(1), intent, options())
    await api.lookup(id(1), intent, options())
    const source = await api.source(id(1), id(2), '1', new AbortController().signal)
    expect(source.bytes).toEqual(new Uint8Array([0, 255, 13, 10]))
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(intent.request)
    expect(JSON.parse(fetch.mock.calls[1]![1].body as string)).toEqual(intent)
    expect(fetch.mock.calls[2]![0]).toContain(`/revisions/1/source`)
    await expect(
      createKnowledgeFileAPI(async () =>
        json({ document: { ...row(), content_version: '2' }, bytes_base64: 'YQ==' }),
      ).source(id(1), id(2), '1', new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('retains exactly the admitted size and rejects ambiguous encodings and media', () => {
    const maximum = encodeKnowledgeFile(new Uint8Array(1048576))
    expect(
      captureKnowledgeFile({ ...intent, request: { ...intent.request, bytes_base64: maximum } })
        .request.bytes_base64,
    ).toBe(maximum)
    expect(() => encodeKnowledgeFile(new Uint8Array(1048577))).toThrow()
    for (const bytes_base64 of ['YR==', 'YQ==\n', 'YQ', '_w==', btoa('x'.repeat(1048577))])
      expect(() =>
        captureKnowledgeFile({ ...intent, request: { ...intent.request, bytes_base64 } }),
      ).toThrow()
    expect(() =>
      captureKnowledgeFile({ ...intent, request: { ...intent.request, media_type: 'text/html' } }),
    ).toThrow()
  })
})

function sessionFixture(api: KnowledgeFileAPI) {
  const identity = { userID: id(4), sessionID: id(5), epoch: 1 }
  let active = true,
    busy = false
  const ports: KnowledgeFileSessionPorts = {
    capture: () => ({ identity, csrf: 'S'.repeat(43), key: 'same-original-key' }),
    identity: () => identity,
    current: (candidate) => candidate === identity && active,
    busy: () => busy,
    cancelRead: vi.fn(),
    async run(_identity, _kind, work) {
      if (busy) throw new AccountFailure('busy')
      busy = true
      try {
        return await work(new AbortController().signal, () => active)
      } finally {
        busy = false
      }
    },
  }
  return {
    state: createKnowledgeFileSession(api, ports),
    leave: () => {
      active = false
    },
    busy: () => busy,
  }
}
const controlledAPI = (): KnowledgeFileAPI => ({
  write: vi.fn(async () => ({ document: row() })),
  lookup: vi.fn(async () => ({ state: 'not_observed' as const, receipt: null })),
  source: vi.fn(async () => ({ document: row(), bytes: new Uint8Array([1]) })),
})

describe('Knowledge file Session original-call ownership', () => {
  it('retains uncertain original bytes/key and only explicitly replays after absent lookup', async () => {
    const api = controlledAPI(),
      writes = vi.mocked(api.write)
    writes.mockRejectedValueOnce(new AccountFailure('transport'))
    const { state } = sessionFixture(api)
    await expect(state.start(id(1), intent)).rejects.toMatchObject({ kind: 'transport' })
    expect(state.pending).toBe(true)
    expect(state.progress?.phase).toBe('uncertain')
    await expect(state.replay()).rejects.toMatchObject({ kind: 'invalid-input' })
    await state.lookup()
    expect(writes).toHaveBeenCalledTimes(1)
    expect(state.progress?.canRetryOriginal).toBe(true)
    await state.replay()
    expect(writes).toHaveBeenCalledTimes(2)
    expect(writes.mock.calls[0]![1]).toEqual(writes.mock.calls[1]![1])
    expect(writes.mock.calls[0]![2].key).toBe(writes.mock.calls[1]![2].key)
    expect(state.progress?.phase).toBe('confirmed')
    expect(state.pending).toBe(false)
  })
  it('clears identity payload without publishing a late result or retiring the active host', async () => {
    let resolve!: (value: { document: KnowledgeDocument }) => void
    const api = controlledAPI()
    vi.mocked(api.write).mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done
        }),
    )
    const f = sessionFixture(api),
      original = f.state.start(id(1), intent)
    expect(f.busy()).toBe(true)
    f.leave()
    f.state.clear()
    expect(f.state.progress).toBe(null)
    expect(f.busy()).toBe(true)
    resolve({ document: row() })
    await expect(original).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.busy()).toBe(false)
    expect(f.state.progress).toBe(null)
    await expect(f.state.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
  })
})
