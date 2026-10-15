import { describe, expect, it, vi } from 'vitest'
import {
  createMCPCredentialAPI,
  parseMCPCredentialMetadata,
  parseMCPCredentialObservation,
} from '../api/project-mcp-credentials'
import type { Fetch } from '../api/client'
const p = '01970000-0000-7000-8000-000000000010',
  c = '01970000-0000-7000-8000-000000000011'
const base = `/api/v1/projects/${p}/mcp`
const write = {
  csrfToken: 'S'.repeat(43),
  key: 'mcp-credential:original',
  signal: new AbortController().signal,
}
const metadata = { credential_id: c, purpose: 'mcp', version: '1' }
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
describe('MCP Project credential original transport', () => {
  it('uses fixed MCP paths and exact successor metadata without exposing material', async () => {
    const fetch = vi.fn<Fetch>(async (path, init) =>
      json(
        init.method === 'GET'
          ? metadata
          : path.endsWith('/lookup')
            ? { observed: false, result: null }
            : {
                ...metadata,
                version: init.method === 'POST' ? '1' : '9007199254740994',
                deleted: init.method === 'DELETE',
              },
      ),
    )
    const api = createMCPCredentialAPI(fetch)
    expect(await api.createCredential(p, 'owned private', write)).toEqual({
      ...metadata,
      deleted: false,
    })
    await api.getCredentialMetadata(p, c, write.signal)
    await api.updateCredential(p, c, '9007199254740993', 'rotated private', write)
    await api.deleteCredential(p, c, '9007199254740993', write)
    await api.lookupCredential(p, { kind: 'create' }, write)
    expect(fetch.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      [`${base}/credentials`, 'POST'],
      [`${base}/credentials/${c}`, 'GET'],
      [`${base}/credentials/${c}`, 'PATCH'],
      [`${base}/credentials/${c}`, 'DELETE'],
      [`${base}/credential-commands/lookup`, 'POST'],
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init.credentials).toBe('same-origin')
      expect(init.redirect).toBe('error')
      expect(init.cache).toBe('no-store')
    }
    expect(JSON.parse(String(fetch.mock.calls[4]![1].body))).toEqual({ kind: 'create' })
    expect(() => parseMCPCredentialMetadata({ ...metadata, purpose: 'model' })).toThrow()
    expect(() => parseMCPCredentialMetadata({ ...metadata, value: 'hidden' })).toThrow()
    expect(() =>
      parseMCPCredentialObservation(
        { observed: true, result: { ...metadata, deleted: false, version: '2' } },
        { kind: 'create' },
      ),
    ).toThrow()
  })
  it('does not retry Unknown and queries only the original command identity', async () => {
    const fetch = vi.fn<Fetch>(async (path) => {
      if (path.endsWith('/lookup')) return json({ observed: false, result: null })
      throw new Error('owned transport lost')
    })
    const api = createMCPCredentialAPI(fetch)
    await expect(api.createCredential(p, 'owned private', write)).rejects.toMatchObject({
      kind: 'transport',
    })
    await expect(api.lookupCredential(p, { kind: 'create' }, write)).resolves.toEqual({
      observed: false,
      result: null,
    })
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(JSON.parse(String(fetch.mock.calls[1]![1].body))).toEqual({ kind: 'create' })
    expect(
      new Set(fetch.mock.calls.map(([, init]) => new Headers(init.headers).get('Idempotency-Key')))
        .size,
    ).toBe(1)
  })
  it('rejects malformed material and noncanonical versions before network, and duplicate reply fields', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ ...metadata, deleted: false })),
      api = createMCPCredentialAPI(fetch)
    for (const value of ['', '\ud800', 'a'.repeat(65537)])
      await expect(api.createCredential(p, value, write)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    await expect(
      api.updateCredential(p, c, '9223372036854775807', 'value', write),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
    const duplicate = createMCPCredentialAPI(
      async () =>
        new Response(
          `{"credential_id":"${c}","purpose":"mcp","version":"1","deleted":false,"deleted":true}`,
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    await expect(duplicate.createCredential(p, 'owned value', write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
})
