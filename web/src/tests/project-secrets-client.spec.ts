import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import { createProjectSecretsAPI, parseProjectSecretMetadata } from '../api/project-secrets'
import {
  secretBase,
  secretID,
  secretJSON,
  secretMetadata,
  secretProject,
} from './project-secrets.fixture'

describe('Secret metadata transport and closed DTO', () => {
  it('uses only the two exact GET routes with encoded opaque cursor and no command headers', async () => {
    const fetch = vi.fn<Fetch>(async (path) =>
      secretJSON(path.includes('?') ? { items: [secretMetadata()] } : secretMetadata()),
    )
    const api = createProjectSecretsAPI(fetch),
      signal = new AbortController().signal
    await api.list(secretProject, { cursor: 'opaque +/=' }, signal)
    const detail = await api.get(secretProject, secretID(10), signal)
    expect(fetch.mock.calls[0]![0]).toBe(secretBase + '?limit=50&cursor=opaque+%2B%2F%3D')
    expect(fetch.mock.calls[1]![0]).toBe(secretBase + '/' + secretID(10))
    for (const [, options] of fetch.mock.calls) {
      expect(options.method).toBe('GET')
      expect(options.credentials).toBe('same-origin')
      expect(options.body).toBeUndefined()
      expect(new Headers(options.headers).has('Idempotency-Key')).toBe(false)
      expect(new Headers(options.headers).has('X-CSRF-Token')).toBe(false)
    }
    expect(Object.keys(detail).sort()).toEqual(
      [
        'id',
        'project_id',
        'type',
        'name',
        'description',
        'version',
        'created_at',
        'updated_at',
      ].sort(),
    )
    expect(detail.version).toBe('9007199254740993')
    expect(Object.isFrozen(detail)).toBe(true)
  })
  it.each([
    'value',
    'ciphertext',
    'credential_ref',
    'digest',
    'length',
    'mask',
    'authorized_agents',
    'valid',
  ])('rejects undeclared %s without echoing it', (key) => {
    try {
      parseProjectSecretMetadata(
        { ...secretMetadata(), [key]: 'controlled-rejected-extra' },
        secretProject,
      )
      throw new Error('Expected closed DTO rejection')
    } catch (error) {
      expect(error).toBeInstanceOf(AccountFailure)
      expect((error as AccountFailure).kind).toBe('invalid-response')
      expect(String(error)).not.toContain('controlled-rejected-extra')
      expect(String(error)).toBe('AccountFailure: invalid-response')
    }
  })
  it('enforces typed identities, byte caps, version, name and time semantics', () => {
    const original = secretMetadata()
    for (const patch of [
      { type: 'variable' },
      { project_id: secretID(90) },
      { id: secretID(10).toUpperCase() },
      { name: 'AGENTEAM_TOKEN' },
      { name: 'AGENTEAM' },
      { name: 'NAME\n' },
      { name: '0NAME' },
      { description: '中'.repeat(1366) },
      { description: '\u0085' },
      { description: '\ud800' },
      { version: 1 },
      { version: '9223372036854775808' },
      { version: '1\n' },
      { version: '0' },
      { created_at: '0001-01-01T00:00:00.000000Z' },
      { updated_at: '2026-02-30T00:00:00.000000Z' },
      { updated_at: '2025-01-01T00:00:00.000000Z' },
      { description: null },
    ])
      expect(() => parseProjectSecretMetadata({ ...original, ...patch }, secretProject)).toThrow(
        AccountFailure,
      )
    expect(() => parseProjectSecretMetadata(original, secretProject, secretID(20))).toThrow(
      AccountFailure,
    )
    expect(
      parseProjectSecretMetadata(
        { ...original, description: '\t\r\n' + 'a'.repeat(4093), version: '9223372036854775807' },
        secretProject,
      ).description,
    ).toHaveLength(4096)
  })
  it('rejects a whole page with invalid final item, duplicates or non-C ordering', async () => {
    let response: unknown
    const api = createProjectSecretsAPI(async () => secretJSON(response)),
      signal = new AbortController().signal
    for (const items of [
      [secretMetadata(), { ...secretMetadata(11), value: 'controlled-extra' }],
      [secretMetadata(10, 'A'), secretMetadata(10, 'B')],
      [secretMetadata(11, 'a'), secretMetadata(10, 'Z')],
      [secretMetadata(11, 'A'), secretMetadata(10, 'A')],
      [secretMetadata(), { ...secretMetadata(11), project_id: secretID(90) }],
    ]) {
      response = { items }
      await expect(api.list(secretProject, {}, signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    response = { items: [secretMetadata(10, 'Z'), secretMetadata(11, 'a')] }
    expect((await api.list(secretProject, {}, signal)).items.map((v) => v.name)).toEqual(['Z', 'a'])
    response = { items: [secretMetadata()], next_cursor: null }
    await expect(api.list(secretProject, {}, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('rejects duplicate raw JSON fields, truncated and oversized representations', async () => {
    let raw = JSON.stringify(secretMetadata()).replace(
      '"type":"secret"',
      '"type":"secret","type":"secret"',
    )
    const api = createProjectSecretsAPI(
      async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
    )
    const signal = new AbortController().signal
    await expect(api.get(secretProject, secretID(10), signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    raw = JSON.stringify(secretMetadata()).slice(0, -1)
    await expect(api.get(secretProject, secretID(10), signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    raw = ' '.repeat(1024 * 1024) + JSON.stringify(secretMetadata())
    await expect(api.get(secretProject, secretID(10), signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    raw = ' '.repeat(5 * 1024 * 1024) + '{"items":[]}'
    await expect(api.list(secretProject, {}, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('captures only exact input options before any request', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createProjectSecretsAPI(fetch),
      signal = new AbortController().signal
    for (const query of [
      { limit: 0 },
      { limit: 101 },
      { limit: 1.5 },
      { cursor: null },
      { cursor: '\n' },
      { cursor: 'a'.repeat(8193) },
      { value: 'controlled-extra' },
    ])
      await expect(api.list(secretProject, query as never, signal)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    await expect(api.get(secretProject, '../other', signal)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await expect(
      accountTransport(fetch)('listProjectSecrets', (v) => v, {
        signal,
        projectID: secretProject,
        secretQuery: { limit: 50 },
        body: {},
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
})
