import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createMCPOAuthAPI } from '../api/mcp-oauth'
import { createProjectMCPAPI } from '../api/project-mcp'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1)
const connection = {
  id: id(2),
  project_id: project,
  config_id: id(3),
  version: '2',
  config_revision: '1',
  state: 'connected',
  enabled: true,
  tools_state: 'ready',
  resources_state: 'ready',
  tools_revision: '1',
  resources_revision: '1',
}
const state = 'A'.repeat(43)
const oauth = {
  flow_id: id(4),
  authorization_url: `https://auth.example.test/authorize?state=${state}`,
  expires_at: '2026-10-14T23:59:59Z',
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })
const write = { csrfToken: 'S'.repeat(43), key: id(5), signal: new AbortController().signal }

describe('MCP OAuth account transport', () => {
  it('uses project Begin and scope-free Complete with only original passive lookup', async () => {
    const fetcher = vi.fn<Fetch>(async (url, options) => {
      if (url.endsWith('/commands/lookup')) {
        const begin = JSON.parse(String(options.body)).command === 'oauth.begin'
        return json({ state: 'complete', result: { connection, ...(begin ? { oauth } : {}) } })
      }
      return json({ connection, ...(url.endsWith('/begin') ? { oauth } : {}) })
    })
    const api = createMCPOAuthAPI(fetcher)
    await expect(
      api.begin(project, { config_id: id(3), expected_version: '0' }, write),
    ).resolves.toMatchObject({ connection, oauth })
    await expect(api.lookup(project, 'oauth.begin', write)).resolves.toMatchObject({
      state: 'complete',
      result: { connection, oauth },
    })
    await expect(api.complete({ state, code: 'owned-code', issuer: '' }, write)).resolves.toEqual({
      connection,
    })
    await expect(api.lookup(project, 'oauth.complete', write)).resolves.toEqual({
      state: 'complete',
      result: { connection },
    })
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      `/api/v1/projects/${project}/mcp/oauth/begin`,
      `/api/v1/projects/${project}/mcp/commands/lookup`,
      '/api/v1/mcp/oauth/complete',
      `/api/v1/projects/${project}/mcp/commands/lookup`,
    ])
    expect(JSON.parse(String(fetcher.mock.calls[2]![1].body))).toEqual({
      state,
      code: 'owned-code',
      issuer: '',
    })
    for (const [, request] of fetcher.mock.calls) {
      expect(request.method).toBe('POST')
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(write.key)
      expect(new Headers(request.headers).get('X-CSRF-Token')).toBe(write.csrfToken)
    }
  })

  it('refuses browser-supplied callback scope and malformed callback bytes before fetch', async () => {
    const fetcher = vi.fn<Fetch>(async () => json({ connection }))
    const call = accountTransport(fetcher)
    const options = {
      signal: write.signal,
      csrf: write.csrfToken,
      key: write.key,
      body: { state, code: 'owned-code', issuer: '' },
    }
    const scoped = { ...options, projectID: project }
    await expect(call('completeMCPOAuth', (v) => v, scoped)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    for (const body of [
      { ...options.body, state: 'A'.repeat(42) + 'B' },
      { ...options.body, code: 'not ascii\u00ff' },
      { ...options.body, code: 'x'.repeat(8193) },
      { ...options.body, issuer: 'x'.repeat(16385) },
      { ...options.body, project_id: project },
      { ...options.body, redirect_uri: 'https://other.test' },
    ])
      await expect(call('completeMCPOAuth', (v) => v, { ...options, body })).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('creates a configuration with the actual OAuth scheme and no browser credential', async () => {
    const input = {
      name: 'OAuth MCP',
      description: '',
      endpoint: 'https://mcp.example.test/mcp',
      authentication: { scheme: 'oauth' as const },
    }
    const config = {
      ...input,
      id: id(3),
      scope: 'project',
      project_id: project,
      version: '1',
      runtime_revision: '1',
    }
    const fetcher = vi.fn<Fetch>(async () => json({ config }))
    await expect(
      createProjectMCPAPI(fetcher).createConfig(project, { expected_version: '0', input }, write),
    ).resolves.toEqual({ config })
    expect(JSON.parse(String(fetcher.mock.calls[0]![1].body))).toEqual({
      expected_version: '0',
      input,
    })
  })
})
