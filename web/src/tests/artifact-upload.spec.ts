import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import {
  captureArtifactUpload,
  captureArtifactUploadFile,
  createArtifactUploadAPI,
} from '../api/artifact-upload'
import {
  artifactBase,
  artifactID,
  artifactJSON,
  artifactProject,
  artifactRecord,
} from './project-artifacts.fixture'
const options = () => ({
  key: 'original-upload',
  csrfToken: 'S'.repeat(43),
  signal: new AbortController().signal,
})
const input = Object.freeze({ name: 'report.txt', media_type: 'text/plain', byte_size: '5' })
const uploaded = () => ({
  ...artifactRecord(),
  kind: 'user_upload',
  name: input.name,
  description: undefined,
  object: { ...artifactRecord().object, byte_size: '5' },
})
function metadata() {
  const { description: _, ...row } = uploaded()
  return row
}
describe('Artifact original file upload HTTP', () => {
  it('sends one raw Blob between original intent stages and never manufactures a reference', async () => {
    const replies = [
      { state: 'pending', artifact: null },
      { state: 'uploaded', artifact: null },
      { artifact: metadata() },
      { state: 'committed', artifact: metadata() },
      { state: 'cancel_requested', cleanup: 'pending' },
    ]
    const fetch = vi.fn<Fetch>(async () => artifactJSON(replies.shift())),
      api = createArtifactUploadAPI(fetch)
    const file = new File(['hello'], input.name, { type: 'text/plain' })
    expect(captureArtifactUploadFile(file)).toEqual(input)
    expect((await api.begin(artifactProject, artifactID(2), input, options())).state).toBe(
      'pending',
    )
    expect((await api.payload(artifactProject, artifactID(2), input, file, options())).state).toBe(
      'uploaded',
    )
    expect((await api.complete(artifactProject, artifactID(2), input, options())).artifact).toEqual(
      metadata(),
    )
    expect((await api.lookup(artifactProject, artifactID(2), input, options())).state).toBe(
      'committed',
    )
    expect((await api.cancel(artifactProject, artifactID(2), input, options())).state).toBe(
      'cancel_requested',
    )
    expect(fetch.mock.calls.map(([path]) => path)).toEqual(
      ['begin', 'payload', 'complete', 'lookup', 'cancel'].map(
        (stage) => artifactBase + '/upload-commands/' + stage,
      ),
    )
    fetch.mock.calls.forEach(([, init], index) => {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe('original-upload')
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('S'.repeat(43))
      if (index === 1) {
        expect(init.method).toBe('PUT')
        expect(init.body).toBe(file)
        expect(new Headers(init.headers).get('Content-Type')).toBe('text/plain')
        expect(new Headers(init.headers).has('Content-Length')).toBe(false)
      } else {
        expect(init.method).toBe('POST')
        expect(JSON.parse(init.body as string)).toEqual(input)
      }
    })
  })
  it('rejects unbound receipts and malformed states instead of exposing a file identity', async () => {
    const row = metadata()
    for (const artifact of [
      { ...row, kind: 'generated' },
      { ...row, created_by_user_id: artifactID(8) },
      { ...row, source_execution_id: artifactID(8) },
      { ...row, artifact_ref: { ...row.artifact_ref, project_id: artifactID(8) } },
      { ...row, object: { ...row.object, byte_size: '6' } },
    ]) {
      const api = createArtifactUploadAPI(async () =>
        artifactJSON({ state: 'committed', artifact }),
      )
      await expect(
        api.lookup(artifactProject, artifactID(2), input, options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    for (const value of [
      { state: 'pending', artifact: row },
      { state: 'uploaded', artifact: null, upload_id: artifactID(8) },
      { state: 'stopped', artifact: null },
    ]) {
      const api = createArtifactUploadAPI(async () => artifactJSON(value))
      await expect(
        api.lookup(artifactProject, artifactID(2), input, options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const api = createArtifactUploadAPI(async () =>
      artifactJSON({ state: 'pending', artifact: null }),
    )
    await expect(
      api.payload(artifactProject, artifactID(2), input, new Blob(['hello']), options()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('captures original bytes and a bounded intent before any request and preserves unknown transport outcomes', async () => {
    const fetch = vi.fn<Fetch>(async () => {
        throw new Error('lost response')
      }),
      api = createArtifactUploadAPI(fetch)
    for (const value of [
      { ...input, byte_size: 5 },
      { ...input, byte_size: '05' },
      { ...input, byte_size: '1073741825' },
      { ...input, sha256: null },
      { ...input, name: '../report' },
      { ...input, name: '\ud800' },
      { ...input, media_type: 'text/plain\r\nUnsafe: header' },
      { ...input, file_id: artifactID(9) },
    ])
      expect(() => captureArtifactUpload(value)).toThrow(AccountFailure)
    await expect(
      api.payload(artifactProject, artifactID(2), input, new Blob(['too long']), options()),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    await expect(
      api.payload(artifactProject, artifactID(2), input, new Blob(['hello']), options()),
    ).rejects.toMatchObject({ kind: 'transport' })
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(captureArtifactUploadFile(new File([], 'empty.bin'))).toEqual({
      name: 'empty.bin',
      media_type: 'application/octet-stream',
      byte_size: '0',
    })
    const transport = accountTransport(fetch)
    await expect(
      transport('writeArtifactUploadPayload', (value) => value, {
        projectID: artifactProject,
        csrf: 'S'.repeat(43),
        key: 'k',
        signal: new AbortController().signal,
        artifactPayload: { file: 'not a blob' as unknown as Blob, mediaType: 'text/plain' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
