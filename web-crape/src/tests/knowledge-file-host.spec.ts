import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { AccountFailure, type Fetch } from '../api/client'
import { createKnowledgeFileAPI, type KnowledgeFileIntent } from '../api/knowledge-file'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
import { createSessionController } from '../composables/useSession'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  document = id(2)
const session = {
  user: {
    id: id(8),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: {
    id: id(9),
    issued_at: '2026-10-14T00:00:00.123456Z',
    idle_expires_at: '2026-10-15T00:00:00.123456Z',
    absolute_expires_at: '2026-10-16T00:00:00.123456Z',
  },
  csrf_token: 'S'.repeat(43),
}
const row = {
  id: document,
  project_id: project,
  parent_document_id: null,
  title: 'Original file',
  content_version: '1',
  source_kind: 'file',
  media_type: 'application/pdf',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: id(8) },
  created_at: '2026-10-14T00:00:00.123456Z',
  updated_at: '2026-10-14T00:00:00.123456Z',
}
const intent: KnowledgeFileIntent = {
  command: 'create',
  document_id: document,
  request: {
    parent_document_id: null,
    title: 'Original file',
    media_type: 'application/pdf',
    bytes_base64: 'AP8NCg==',
  },
}
const textIntent = {
  command: 'create' as const,
  document_id: id(3),
  request: {
    parent_document_id: null,
    title: 'Text',
    media_type: 'text/plain' as const,
    text: 'Original text',
  },
}
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
async function fixture(work: Fetch) {
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session)) : work(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = {
    knowledgeFile: createKnowledgeFileAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }
  const auth = createSessionController(...args)
  await auth.restore()
  return { auth, fetch }
}

describe('Knowledge files use the original Session request owner', () => {
  it('keeps original Unknown bytes and key for lookup and blocks competing Knowledge intents', async () => {
    const { auth, fetch } = await fixture(async (url) => {
      if (url.endsWith('/create-file')) throw new AccountFailure('transport')
      return json({
        state: 'committed',
        receipt: { command: 'create', document: row, changed: true },
      })
    })
    try {
      await expect(auth.knowledgeFiles.start(project, intent)).rejects.toMatchObject({
        kind: 'transport',
      })
      await expect(auth.knowledgeText.start(project, textIntent)).rejects.toMatchObject({
        kind: 'busy',
      })
      await expect(
        auth.knowledgeCommands.startRename(project, document, {
          expected_version: '1',
          title: 'Renamed',
        }),
      ).rejects.toMatchObject({ kind: 'busy' })
      await expect(
        auth.knowledgeMove.startMove(project, document, {
          expected_parent_id: null,
          target_parent_id: id(10),
        }),
      ).rejects.toMatchObject({ kind: 'busy' })
      await expect(auth.knowledgeFiles.replay()).rejects.toMatchObject({ kind: 'invalid-input' })
      await auth.knowledgeFiles.lookup()
      expect(auth.knowledgeFiles.pending).toBe(false)
      expect(auth.knowledgeFiles.progress).toMatchObject({ phase: 'confirmed', contextValid: true })
      const calls = fetch.mock.calls.filter(([url]) => url !== '/api/v1/session')
      expect(calls).toHaveLength(2)
      expect(calls[0]![0]).toContain('/create-file')
      expect(calls[1]![0]).toContain('/file-commands/lookup')
      expect(JSON.parse(calls[0]![1].body as string)).toEqual(intent.request)
      expect(JSON.parse(calls[1]![1].body as string)).toEqual(intent)
      expect(new Headers(calls[0]![1].headers).get('Idempotency-Key')).toBe(
        new Headers(calls[1]![1].headers).get('Idempotency-Key'),
      )
    } finally {
      auth.leave()
    }
  })
  it('rejects file submission while an original text write is uncertain', async () => {
    const { auth, fetch } = await fixture(async () => {
      throw new AccountFailure('transport')
    })
    try {
      await expect(auth.knowledgeText.start(project, textIntent)).rejects.toMatchObject({
        kind: 'transport',
      })
      await expect(auth.knowledgeFiles.start(project, intent)).rejects.toMatchObject({
        kind: 'busy',
      })
      expect(auth.knowledgeFiles.progress).toBeNull()
      expect(fetch).toHaveBeenCalledTimes(2)
    } finally {
      auth.leave()
    }
  })
  it('does not release the request owner until a cancelled source read actually returns', async () => {
    const sent = deferred<AbortSignal>(),
      response = deferred<Response>()
    const { auth } = await fixture(async (_url, init) => {
      sent.resolve(init.signal!)
      return response.promise
    })
    const reading = auth.knowledgeFiles.source(project, document, '1')
    const failed = expect(reading).rejects.toMatchObject({ kind: 'cancelled' })
    const signal = await sent.promise
    auth.knowledgeFiles.abandonRead()
    await failed
    expect(signal.aborted).toBe(true)
    expect(auth.state.busy).toBe(true)
    await expect(auth.knowledgeFiles.start(project, intent)).rejects.toMatchObject({ kind: 'busy' })
    response.resolve(json({ document: row, bytes_base64: 'AP8NCg==' }))
    await vi.waitFor(() => expect(auth.state.busy).toBe(false))
    auth.leave()
  })
  it('clears the captured file on identity leave and suppresses the late receipt', async () => {
    const sent = deferred<void>(),
      response = deferred<Response>()
    const { auth } = await fixture(async () => {
      sent.resolve()
      return response.promise
    })
    const writing = auth.knowledgeFiles.start(project, intent)
    const failed = expect(writing).rejects.toMatchObject({ kind: 'cancelled' })
    await sent.promise
    auth.leave()
    await failed
    expect(auth.knowledgeFiles.progress).toBeNull()
    expect(auth.knowledgeFiles.pending).toBe(false)
    expect(auth.state.busy).toBe(true)
    response.resolve(json({ document: row }))
    await vi.waitFor(() => expect(auth.state.busy).toBe(false))
    expect(auth.knowledgeFiles.progress).toBeNull()
  })
})
