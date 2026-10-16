import { webcrypto } from 'node:crypto'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { AccountFailure } from '../api/client'
import {
  createArtifactUploadAPI,
  type ArtifactUploadAPI,
  type ArtifactUploadResult,
} from '../api/artifact-upload'
import {
  createArtifactUploadSession,
  type ArtifactUploadSessionPorts,
} from '../composables/artifact-upload-session'
import {
  artifactID,
  artifactProject,
  artifactRecord,
  artifactSessionFixture,
  artifactJSON,
  artifactDeferred,
  artifactProblem,
} from './project-artifacts.fixture'
const cleanup: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanup.splice(0)) close()
  await flushPromises()
  vi.unstubAllGlobals()
})
const file = () => new File(['hello'], 'report.txt', { type: 'text/plain' })
const pending = () => ({ state: 'pending', artifact: null }) as const
const uploaded = () => ({ state: 'uploaded', artifact: null }) as const
function fixture() {
  let valid = true,
    busy = false
  const identity = { userID: artifactID(2), sessionID: artifactID(3), epoch: 1 }
  const receipt = {
    ...artifactRecord(),
    kind: 'user_upload' as const,
    name: 'report.txt',
    object: { ...artifactRecord().object, byte_size: '5' },
  }
  const api = {
    begin: vi.fn<ArtifactUploadAPI['begin']>(async () => pending()),
    payload: vi.fn<ArtifactUploadAPI['payload']>(async () => uploaded()),
    complete: vi.fn<ArtifactUploadAPI['complete']>(async () => ({ artifact: receipt })),
    lookup: vi.fn<ArtifactUploadAPI['lookup']>(async () => pending()),
    cancel: vi.fn<ArtifactUploadAPI['cancel']>(async () => ({
      state: 'cancelled',
      cleanup: 'completed',
    })),
  }
  const ports: ArtifactUploadSessionPorts = {
    capture: () => ({ identity, key: 'original-upload', csrf: 'S'.repeat(43) }),
    current: (value) => value === identity && valid,
    busy: () => busy,
    stopPayload: vi.fn(),
    async run(value, _action, work) {
      expect(value).toBe(identity)
      if (busy) throw new AccountFailure('busy')
      busy = true
      try {
        return await work(new AbortController().signal, () => valid)
      } finally {
        busy = false
      }
    },
  }
  const session = createArtifactUploadSession(api, ports)
  return {
    session,
    api,
    receipt,
    retire() {
      valid = false
      session.clear()
    },
  }
}
describe('Artifact upload original Session intent', () => {
  it('advances one fresh File through begin, raw payload and publication using the original key', async () => {
    const f = fixture(),
      original = file(),
      meeting = artifactID(7)
    expect(await f.session.start(artifactProject, meeting, original)).toEqual(f.receipt)
    expect(f.api.begin).toHaveBeenCalledTimes(1)
    expect(f.api.payload).toHaveBeenCalledTimes(1)
    expect(f.api.payload.mock.calls[0]![3]).toBe(original)
    expect(f.api.complete).toHaveBeenCalledTimes(1)
    expect(f.api.lookup).not.toHaveBeenCalled()
    expect(f.session.pending).toBe(false)
    expect(f.session.progress).toMatchObject({
      projectID: artifactProject,
      meetingID: meeting,
      phase: 'confirmed',
      receipt: f.receipt,
    })
    for (const call of [f.api.begin.mock.calls[0]!, f.api.complete.mock.calls[0]!]) {
      expect(call[0]).toBe(artifactProject)
      expect(call[1]).toBe(artifactID(2))
      expect(call[3]).toMatchObject({ key: 'original-upload', csrfToken: 'S'.repeat(43) })
    }
    expect(f.session.dismiss()).toBe(true)
  })
  it('never resends payload after Unknown; only an uploaded observation permits explicit completion', async () => {
    const f = fixture()
    f.api.payload.mockRejectedValueOnce(new AccountFailure('transport'))
    await expect(f.session.start(artifactProject, null, file())).rejects.toMatchObject({
      kind: 'transport',
    })
    expect(f.session.progress).toMatchObject({
      phase: 'uncertain',
      canLookup: true,
      canComplete: false,
    })
    await expect(f.session.start(artifactProject, null, file())).rejects.toMatchObject({
      kind: 'busy',
    })
    await f.session.lookup()
    expect(f.session.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'pending',
      canComplete: false,
    })
    expect(f.api.payload).toHaveBeenCalledTimes(1)
    expect(f.api.complete).not.toHaveBeenCalled()
    f.api.lookup.mockResolvedValueOnce(uploaded())
    await f.session.lookup()
    expect(f.session.progress).toMatchObject({ phase: 'uploaded', canComplete: true })
    expect(f.api.complete).not.toHaveBeenCalled()
    await f.session.complete()
    expect(f.session.progress?.phase).toBe('confirmed')
    expect(f.api.begin).toHaveBeenCalledTimes(1)
    expect(f.api.payload).toHaveBeenCalledTimes(1)
    expect(f.api.complete.mock.calls[0]![3].key).toBe('original-upload')
  })
  it('keeps an uncertain begin when cancellation only observes no row, until cancellation is confirmed', async () => {
    const f = fixture()
    f.api.begin.mockRejectedValueOnce(new AccountFailure('transport'))
    await expect(f.session.start(artifactProject, null, file())).rejects.toMatchObject({
      kind: 'transport',
    })
    f.api.lookup.mockResolvedValueOnce({ state: 'not_observed', artifact: null })
    await f.session.lookup()
    f.api.cancel.mockResolvedValueOnce({ state: 'not_observed', cleanup: null })
    await f.session.cancel()
    expect(f.session.pending).toBe(true)
    expect(f.session.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
      canLookup: true,
    })
    expect(f.session.dismiss()).toBe(false)
    await expect(f.session.start(artifactProject, null, file())).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.api.payload).not.toHaveBeenCalled()
    await f.session.cancel()
    expect(f.session.pending).toBe(false)
    expect(f.session.progress).toMatchObject({ phase: 'cancelled', observation: 'cancelled' })
    expect(f.session.dismiss()).toBe(true)
  })
  it('retains unresolved publication until lookup and never publishes a late result after Session retirement', async () => {
    const f = fixture()
    let finish!: (value: ArtifactUploadResult) => void
    f.api.payload.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const promise = f.session.start(artifactProject, null, file())
    await vi.waitFor(() => expect(f.api.payload).toHaveBeenCalledTimes(1))
    f.retire()
    finish(uploaded())
    await expect(promise).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.api.complete).not.toHaveBeenCalled()
    expect(f.session.progress).toBeNull()
  })
})

async function originalSession() {
  vi.stubGlobal('crypto', webcrypto)
  const f = artifactSessionFixture((fetch) => ({ artifactUpload: createArtifactUploadAPI(fetch) }))
  await f.auth.restore()
  cleanup.push(() => f.auth.leave())
  return f
}
describe('Artifact file transfer in the original authorized Session lane', () => {
  it('stops a transfer without reporting cancellation or freeing its actual response body owner early', async () => {
    const f = await originalSession(),
      tail = artifactDeferred<void>(),
      reading = artifactDeferred<void>()
    cleanup.push(() => tail.resolve())
    const body = new ReadableStream<Uint8Array>(
      {
        pull() {
          reading.resolve()
        },
        cancel() {
          return tail.promise
        },
      },
      { highWaterMark: 0 },
    )
    f.intercept(async (path) =>
      path.endsWith('/begin')
        ? artifactJSON(pending())
        : new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    )
    const started = f.auth.artifactUploads
      .start(artifactProject, artifactID(7), file())
      .catch((error) => error)
    await reading.promise
    f.auth.artifactUploads.stop()
    expect(await started).toMatchObject({ kind: 'cancelled' })
    expect(f.auth.artifactUploads.progress).toMatchObject({ phase: 'uncertain', canLookup: false })
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.artifactUploads.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'PUT')).toHaveLength(1)
    tail.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.intercept(async () => artifactJSON({ state: 'cancel_requested', artifact: null }))
    await f.auth.artifactUploads.lookup()
    expect(f.auth.artifactUploads.progress?.phase).toBe('cancelling')
    f.intercept(async () => artifactJSON({ state: 'cancelled', artifact: null }))
    await f.auth.artifactUploads.lookup()
    expect(f.auth.artifactUploads.progress?.phase).toBe('cancelled')
  })
  it('retains the same original lookup across Session checking but erases it for a different Session', async () => {
    const f = await originalSession()
    f.intercept(async () => {
      const response = artifactProblem(503),
        problem = await response.json()
      return new Response(JSON.stringify({ ...problem, commit_state: 'not_committed' }), {
        status: 503,
        headers: response.headers,
      })
    })
    await expect(f.auth.artifactUploads.start(artifactProject, null, file())).rejects.toBeDefined()
    expect(f.auth.artifactUploads.progress?.phase).toBe('uncertain')
    const first = f.fetch.mock.calls.find(([path]) => path.endsWith('/begin'))![1]
    f.intercept(f.normal)
    await f.auth.restore()
    f.intercept(async () => artifactJSON(pending()))
    await f.auth.artifactUploads.lookup()
    const lookup = f.fetch.mock.calls.find(([path]) => path.endsWith('/lookup'))![1]
    expect(lookup.body).toBe(first.body)
    expect(new Headers(lookup.headers).get('Idempotency-Key')).toBe(
      new Headers(first.headers).get('Idempotency-Key'),
    )
    f.session({ ...f.sessionValue(), session: { ...f.sessionValue().session, id: artifactID(9) } })
    f.intercept(f.normal)
    await f.auth.restore()
    expect(f.auth.artifactUploads.progress).toBeNull()
    await expect(f.auth.artifactUploads.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
  })
})
