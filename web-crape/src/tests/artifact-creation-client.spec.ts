import { webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import { captureArtifactContent, createArtifactCreationAPI } from '../api/artifact-creation'
import {
  artifactBase,
  artifactID,
  artifactJSON,
  artifactProject,
  creationInput,
  createdRecord,
} from './artifact-creation.fixture'
const options = () => ({
  key: 'original-key',
  csrfToken: 'S'.repeat(43),
  signal: new AbortController().signal,
})
beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto)
})
afterEach(() => {
  vi.unstubAllGlobals()
})
describe('Artifact full original intent HTTP commands', () => {
  it('sends exact unchanged content/key to three POST entries and parses five history states', async () => {
    const row = createdRecord()
    const replies = [
      { artifact: row },
      ...['not_observed', 'pending', 'cancel_requested', 'cancelled'].map((state) => ({
        state,
        artifact: null,
      })),
      { state: 'committed', artifact: row },
      { state: 'not_observed', cleanup: null },
      { state: 'cancel_requested', cleanup: 'pending' },
      { state: 'cancelled', cleanup: 'completed' },
    ]
    const fetch = vi.fn<Fetch>(async () => artifactJSON(replies.shift())),
      api = createArtifactCreationAPI(fetch)
    await expect(
      api.create(artifactProject, artifactID(2), creationInput, options()),
    ).resolves.toEqual({ artifact: row })
    for (const state of ['not_observed', 'pending', 'cancel_requested', 'cancelled', 'committed'])
      await expect(
        api.lookup(artifactProject, artifactID(2), creationInput, options()),
      ).resolves.toMatchObject({ state })
    for (const state of ['not_observed', 'cancel_requested', 'cancelled'])
      await expect(
        api.cancel(artifactProject, artifactID(2), creationInput, options()),
      ).resolves.toMatchObject({ state })
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      artifactBase + '/create-content',
      ...Array(5).fill(artifactBase + '/creation-commands/lookup'),
      ...Array(3).fill(artifactBase + '/creation-commands/cancel'),
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(JSON.parse(init.body as string)).toEqual(creationInput)
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        redirect: 'error',
        cache: 'no-store',
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe('original-key')
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('S'.repeat(43))
    }
  })
  it('validates raw UTF-8 limits, permits empty content and maximum JSON escapes, and never trims', async () => {
    const maximum = {
      name: 'é'.repeat(127) + 'x',
      description: '界'.repeat(1365) + 'x',
      media_type: 'text/plain' as const,
      content: '\0'.repeat(1048576),
    }
    expect(captureArtifactContent(maximum)).toEqual(maximum)
    const fetch = vi.fn<Fetch>(async () => artifactJSON({ state: 'not_observed', artifact: null }))
    await createArtifactCreationAPI(fetch).lookup(
      artifactProject,
      artifactID(2),
      maximum,
      options(),
    )
    expect((fetch.mock.calls[0]![1].body as string).length).toBeGreaterThan(6 * 1048576)
    expect(captureArtifactContent({ ...creationInput, content: '' }).content).toBe('')
    for (const value of [
      { ...maximum, name: maximum.name + 'x' },
      { ...maximum, description: maximum.description + 'x' },
      { ...maximum, content: maximum.content + 'x' },
      { ...creationInput, content: '\ud800' },
      { ...creationInput, name: 'a/b' },
      { ...creationInput, name: 'a\u0085b' },
      { ...creationInput, description: '\0' },
      { ...creationInput, media_type: 'text/html' },
      { ...creationInput, source_execution_id: artifactID(9) },
    ])
      expect(() => captureArtifactContent(value)).toThrow(AccountFailure)
    expect(captureArtifactContent(creationInput)).toEqual(creationInput)
  })
  it('rejects mismatched immutable receipts and impossible cancellation success without private fields', async () => {
    const row = createdRecord()
    for (const artifact of [
      { ...row, name: row.name.trim() },
      { ...row, created_by_user_id: artifactID(9) },
      { ...row, source_execution_id: artifactID(9) },
      { ...row, object: { ...row.object, sha256: 'sha256:' + 'b'.repeat(64) } },
      { ...row, artifact_ref: { ...row.artifact_ref, project_id: artifactID(9) } },
    ]) {
      const api = createArtifactCreationAPI(async () => artifactJSON({ artifact }))
      await expect(
        api.create(artifactProject, artifactID(2), creationInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    for (const result of [
      { state: 'pending', cleanup: null },
      { state: 'cancelled', cleanup: 'pending' },
      { state: 'not_observed', cleanup: null, object_id: artifactID(9) },
    ]) {
      const api = createArtifactCreationAPI(async () => artifactJSON(result))
      await expect(
        api.cancel(artifactProject, artifactID(2), creationInput, options()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
})
