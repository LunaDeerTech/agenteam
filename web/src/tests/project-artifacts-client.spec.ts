import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import {
  captureArtifactQuery,
  captureArtifactReadQuery,
  createProjectArtifactsAPI,
  decodeArtifactContent,
  decodeArtifactMetadata,
  decodeArtifactPage,
} from '../api/project-artifacts'
import {
  artifactBase,
  artifactContent,
  artifactID,
  artifactJSON,
  artifactPage,
  artifactProject,
  artifactRecord,
  artifactText,
} from './project-artifacts.fixture'

describe('Artifact Owner closed HTTP reads', () => {
  it('captures literal filters and the complete P/A/F reference into credentialed bounded GETs', async () => {
    const row = { ...artifactRecord(), source_execution_id: artifactID(50), name: 'A%_ 中文.txt' }
    const fetch = vi.fn<Fetch>(async (path) =>
      artifactJSON(path.includes('/content?') ? artifactContent(row) : artifactPage([row])),
    )
    const api = createProjectArtifactsAPI(fetch),
      signal = new AbortController().signal
    const page = await api.list(
      artifactProject,
      {
        execution_id: artifactID(50),
        kind: 'generated',
        media_type: 'text/plain',
        name_query: 'A%_ 中',
        cursor: 'opaque +/=',
      },
      signal,
    )
    const params = new URL(fetch.mock.calls[0]![0], 'https://owned.invalid').searchParams
    expect(params.get('name_query')).toBe('A%_ 中')
    expect(params.get('cursor')).toBe('opaque +/=')
    expect(params.get('limit')).toBe('50')
    expect(page.next_cursor).toBeNull()
    expect(page.items[0]!.version).toBe('9007199254740993')
    const result = await api.read(row.artifact_ref, {}, signal)
    expect(fetch.mock.calls[1]![0]).toBe(
      `${artifactBase}/${row.artifact_ref.artifact_id}/files/${row.artifact_ref.file_id}/content?offset=0&limit=8192`,
    )
    expect(result.text).toBe(artifactText)
    expect(Object.isFrozen(result.artifact.object)).toBe(true)
    for (const [, init] of fetch.mock.calls) {
      expect(init.method).toBe('GET')
      expect(init.credentials).toBe('same-origin')
      expect(init.body).toBeUndefined()
      expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
      expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
    }
  })
  it('rejects private fields, wrong references, creator ambiguity and invalid final rows before publishing a page', () => {
    const row = artifactRecord(),
      query = captureArtifactQuery({})
    for (const object of [
      { ...row.object, id: artifactID(9) },
      { ...row.object, sha256: 'a'.repeat(64) },
      { ...row.object, byte_size: 12 },
    ])
      expect(() => decodeArtifactMetadata({ ...row, object })).toThrow(AccountFailure)
    for (const patch of [
      { created_by_agent_id: artifactID(4) },
      { created_by_user_id: null },
      { source_operation_id: artifactID(6) },
      { name: 'unsafe\u0085.txt' },
      { created_at: '2026-02-30T00:00:00.000000Z' },
    ])
      expect(() => decodeArtifactMetadata({ ...row, ...patch })).toThrow(AccountFailure)
    for (const items of [
      [row, row],
      [artifactRecord(99), row],
      [row, { ...artifactRecord(99), signed_url: 'https://forbidden.invalid' }],
      [{ ...row, artifact_ref: { ...row.artifact_ref, project_id: artifactID(5) } }],
    ])
      expect(() => decodeArtifactPage({ items }, artifactProject, query)).toThrow(AccountFailure)
    for (const filter of [
      { kind: 'user_upload' },
      { name_query: 'REPORT' },
      { execution_id: artifactID(50) },
      { media_type: 'image/png' },
    ])
      expect(() =>
        decodeArtifactPage(artifactPage(), artifactProject, captureArtifactQuery(filter)),
      ).toThrow(AccountFailure)
    expect(() =>
      decodeArtifactPage({ items: [], next_cursor: 'stale' }, artifactProject, query),
    ).toThrow(AccountFailure)
  })
  it('accepts exact UTF-8 ranges including zero progress and safe non-text projections, rejects inconsistent content', () => {
    const row = { ...artifactRecord(), object: { ...artifactRecord().object, byte_size: '6' } },
      query = captureArtifactReadQuery({ limit: 3 })
    expect(
      decodeArtifactContent(artifactContent(row, '中', '0', true), row.artifact_ref, query)
        .next_offset,
    ).toBe('3')
    expect(
      decodeArtifactContent(artifactContent(row, '', '0', true), row.artifact_ref, {
        offset: '0',
        limit: 1,
      }).text,
    ).toBe('')
    for (const projection of ['image_ref', 'file_ref']) {
      const content = decodeArtifactContent(
        { artifact: row, projection, offset: '0', next_offset: '0', truncated: false },
        row.artifact_ref,
        query,
      )
      expect(content.text).toBe('')
      expect(content.invalid_utf8).toBe(false)
    }
    expect(
      decodeArtifactContent(
        {
          artifact: row,
          projection: 'file_ref',
          offset: '0',
          next_offset: '0',
          truncated: false,
          invalid_utf8: true,
        },
        row.artifact_ref,
        query,
      ).invalid_utf8,
    ).toBe(true)
    for (const patch of [
      { next_offset: '2' },
      { truncated: false },
      { offset: '1' },
      { invalid_utf8: true },
      { text: '\ud800' },
      { artifact: { ...row, artifact_ref: { ...row.artifact_ref, file_id: artifactID(8) } } },
      { download_url: '/download' },
    ])
      expect(() =>
        decodeArtifactContent(
          { ...artifactContent(row, '中', '0', true), ...patch },
          row.artifact_ref,
          query,
        ),
      ).toThrow(AccountFailure)
  })
  it('validates caller inputs and direct transport options without weakening any other endpoint', async () => {
    for (const query of [
      { limit: 201 },
      { kind: null },
      { execution_id: 'null' },
      { name_query: '中'.repeat(86) },
      { cursor: '\ud800' },
      { media_type: '' },
      { unknown: 1 },
    ])
      expect(() => captureArtifactQuery(query)).toThrow(AccountFailure)
    for (const query of [
      { offset: '-1' },
      { offset: '01' },
      { offset: '1073741825' },
      { limit: 65537 },
    ])
      expect(() => captureArtifactReadQuery(query)).toThrow(AccountFailure)
    const fetch = vi.fn<Fetch>(),
      transport = accountTransport(fetch),
      signal = new AbortController().signal
    await expect(
      transport('listProjectArtifacts', (value) => value, {
        projectID: artifactProject,
        artifactQuery: { limit: 50, execution_id: 'null' },
        signal,
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      transport('readProjectArtifact', (value) => value, {
        projectID: artifactProject,
        artifactID: artifactID(100),
        fileID: 'wrong',
        artifactRead: { offset: '0', limit: 8192 },
        signal,
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
})
