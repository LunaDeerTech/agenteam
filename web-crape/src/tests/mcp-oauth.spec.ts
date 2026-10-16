import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureMCPOAuthReturn,
  createMCPOAuthAPI,
  type MCPOAuthAPI,
  type MCPOAuthResult,
} from '../api/mcp-oauth'
import { createMCPOAuthSession } from '../composables/mcp-oauth-session'
import type { MCPSessionPorts } from '../composables/mcp-session'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  config = id(2),
  oauthState = 'A'.repeat(43)
const input = { config_id: config, expected_version: '0' }
const result: MCPOAuthResult = {
  connection: {
    id: id(3),
    project_id: project,
    config_id: config,
    version: '1',
    config_revision: '1',
    state: 'connected',
    enabled: true,
    tools_state: 'ready',
    resources_state: 'ready',
    tools_revision: '1',
    resources_revision: '1',
  },
}
const beginResult: MCPOAuthResult = {
  ...result,
  oauth: {
    flow_id: id(4),
    authorization_url: `https://issuer.fixture/authorize?state=${oauthState}`,
    expires_at: new Date(Date.now() + 600000).toISOString(),
  },
}
const body = { state: oauthState, code: 'private-code', issuer: 'https://issuer.fixture' }
const write = {
  csrfToken: 'S'.repeat(43),
  key: 'original-command',
  signal: new AbortController().signal,
}
function fixture() {
  let identity = { userID: id(8), sessionID: id(9), epoch: 1 },
    key = 0
  const storage = new Map<string, string>()
  const browser = {
    storage: {
      getItem: (k: string) => storage.get(k) ?? null,
      setItem: (k: string, v: string) => {
        storage.set(k, v)
      },
      removeItem: (k: string) => {
        storage.delete(k)
      },
    },
    navigate: vi.fn(),
  }
  const ports: MCPSessionPorts = {
    capture: () => ({ identity, csrf: write.csrfToken, key: `original-${++key}` }),
    identity: () => identity,
    current: (v) => v === identity,
    busy: () => false,
    run: (_identity, _kind, work) => work(new AbortController().signal, () => true),
    cancelRead() {},
  }
  const api = {
    begin: vi.fn<MCPOAuthAPI['begin']>().mockResolvedValue(beginResult),
    complete: vi.fn<MCPOAuthAPI['complete']>().mockResolvedValue(result),
    lookup: vi.fn<MCPOAuthAPI['lookup']>().mockResolvedValue({ state: 'complete', result }),
  }
  const make = () => createMCPOAuthSession(api, ports, browser)
  return {
    api,
    ports,
    browser,
    make,
    storage,
    swapSession: () => {
      identity = { ...identity, sessionID: id(10), epoch: 2 }
    },
  }
}
const capture = (
  session: ReturnType<typeof createMCPOAuthSession>,
  query = `state=${oauthState}&code=private-code&iss=https%3A%2F%2Fissuer.fixture`,
) => {
  const replace = vi.fn()
  session.captureReturn(`https://app.fixture/mcp/oauth/callback?${query}`, replace)
  expect(replace).toHaveBeenCalledExactlyOnceWith('/mcp/oauth/callback')
  return replace
}
describe('OAuth original callback and passive recovery', () => {
  it('accepts only the current published identity for metadata restore while Session publication owns admission', async () => {
    const f = fixture()
    await f.make().begin(project, input, '/owner/project/settings/mcp')
    const published = f.ports.identity()
    f.ports.identity = () => {
      throw new AccountFailure('busy')
    }
    const reload = f.make()
    expect(() => reload.restore(published)).not.toThrow()
    expect(reload.progress).toMatchObject({ phase: 'authorizing', contextValid: true })
    expect(f.api.lookup).not.toHaveBeenCalled()
    const foreign = f.make()
    foreign.restore({ ...published })
    expect(foreign.progress).toBeNull()
    expect(f.api.complete).not.toHaveBeenCalled()
  })
  it('restores original pending metadata after reload without making a request', async () => {
    const f = fixture(),
      session = f.make()
    f.api.begin.mockRejectedValueOnce(new AccountFailure('transport'))
    await expect(
      session.begin(project, input, '/owner/project/settings/mcp'),
    ).rejects.toMatchObject({ kind: 'transport' })
    const reload = f.make()
    reload.restore()
    expect(reload.pending).toBe(true)
    expect(reload.progress).toMatchObject({ project, phase: 'uncertain', contextValid: true })
    expect(f.api.lookup).not.toHaveBeenCalled()
    expect(f.api.begin).toHaveBeenCalledTimes(1)
    reload.clear()
    reload.restore()
    expect(reload.progress).toBeNull()
    expect(f.storage.size).toBe(0)
  })
  it('clears the callback synchronously and completes after session restoration without persisting its code', async () => {
    const f = fixture(),
      first = f.make()
    await first.begin(project, input, '/owner/project/settings/mcp')
    expect(f.browser.navigate).toHaveBeenCalledExactlyOnceWith(beginResult.oauth!.authorization_url)
    const callback = f.make()
    capture(callback)
    expect(f.api.complete).not.toHaveBeenCalled()
    callback.captureReturn('https://app.fixture/mcp/oauth/callback', vi.fn())
    callback.clear({ preserveUnboundReturn: true }) // Initial identity publication only.
    await callback.completeReturn()
    expect(f.api.complete).toHaveBeenCalledExactlyOnceWith(
      body,
      expect.objectContaining({ key: 'original-2', csrfToken: write.csrfToken }),
    )
    expect(callback.progress).toMatchObject({
      phase: 'confirmed',
      returnPath: '/owner/project/settings/mcp',
      contextValid: true,
    })
    expect([...f.storage.values()].join('')).not.toContain(body.code)
    expect([...f.storage.values()].join('')).not.toContain(write.csrfToken)
  })
  it('uses the persisted original exchange key for reload and pending/not-found observations without exchanging again', async () => {
    const f = fixture()
    await f.make().begin(project, input, '/owner/project/settings/mcp')
    f.api.complete.mockRejectedValueOnce(new AccountFailure('transport'))
    const callback = f.make()
    capture(callback)
    await expect(callback.completeReturn()).rejects.toMatchObject({ kind: 'transport' })
    const saved = [...f.storage.values()].join('')
    expect(saved).not.toContain(body.code)
    f.api.lookup
      .mockResolvedValueOnce({ state: 'pending' })
      .mockResolvedValueOnce({ state: 'not_found' })
    const reload = f.make()
    await reload.completeReturn()
    expect(reload.progress).toMatchObject({ phase: 'uncertain', observation: 'pending' })
    await reload.lookup()
    expect(reload.progress).toMatchObject({ observation: 'not_found' })
    await reload.lookup()
    expect(reload.progress?.phase).toBe('confirmed')
    expect(f.api.complete).toHaveBeenCalledTimes(1)
    expect(f.api.lookup.mock.calls.every((call) => call[2].key === 'original-2')).toBe(true)
  })
  it('recovers an uncertain begin with an original-key lookup and never repeats discovery', async () => {
    const f = fixture(),
      session = f.make()
    f.api.begin.mockRejectedValueOnce(new AccountFailure('transport'))
    await expect(
      session.begin(project, input, '/owner/project/settings/mcp'),
    ).rejects.toMatchObject({ kind: 'transport' })
    f.api.lookup.mockResolvedValueOnce({ state: 'complete', result: beginResult })
    await session.lookup()
    expect(f.api.lookup).toHaveBeenCalledExactlyOnceWith(
      project,
      'oauth.begin',
      expect.objectContaining({ key: 'original-1' }),
    )
    expect(f.api.begin).toHaveBeenCalledTimes(1)
    expect(f.browser.navigate).toHaveBeenCalledTimes(1)
  })
  it.each([
    'state=wrong&code=private-code',
    `state=${oauthState}&code=one&code=two`,
    `state=${oauthState}&error=access_denied`,
  ])('clears and rejects malformed or declined callbacks without HTTP: %s', async (query) => {
    const f = fixture()
    await f.make().begin(project, input, '/owner/project/settings/mcp')
    const session = f.make()
    capture(session, query)
    await expect(session.completeReturn()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.api.complete).not.toHaveBeenCalled()
    expect(f.api.lookup).not.toHaveBeenCalled()
  })
  it('rejects another session and a missing original tab flow before exchange', async () => {
    const f = fixture()
    await f.make().begin(project, input, '/owner/project/settings/mcp')
    f.swapSession()
    const session = f.make()
    capture(session)
    await expect(session.completeReturn()).rejects.toMatchObject({ kind: 'invalid-input' })
    f.storage.clear()
    capture(session)
    await expect(session.completeReturn()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.api.complete).not.toHaveBeenCalled()
  })
  it('suppresses late success after the original Session context changes', async () => {
    const f = fixture(),
      session = f.make()
    let resolve!: (v: MCPOAuthResult) => void
    f.api.begin.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r
        }),
    )
    const pending = session.begin(project, input, '/owner/project/settings/mcp')
    f.swapSession()
    resolve(beginResult)
    await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    expect(f.browser.navigate).not.toHaveBeenCalled()
    expect(session.progress?.contextValid).toBe(false)
  })
})
describe('OAuth closed Account transport', () => {
  it('accepts stopped only as a passive observation without a fabricated result', async () => {
    const fetch = vi.fn<Fetch>().mockResolvedValueOnce(
      new Response(JSON.stringify({ state: 'stopped' }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    const api = createMCPOAuthAPI(fetch)
    expect(await api.lookup(project, 'oauth.complete', write)).toEqual({ state: 'stopped' })
    fetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ state: 'stopped', result }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await expect(api.lookup(project, 'oauth.complete', write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('uses same-origin POST and the original CSRF/key for begin, complete and passive lookup', async () => {
    const fetch = vi.fn<Fetch>(
      async (url) =>
        new Response(
          JSON.stringify(
            url.endsWith('/lookup')
              ? { state: 'complete', result }
              : url.endsWith('/begin')
                ? beginResult
                : result,
          ),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    const api = createMCPOAuthAPI(fetch)
    await api.begin(project, input, write)
    await api.complete(body, write)
    await api.lookup(project, 'oauth.complete', write)
    expect(fetch.mock.calls.map((c) => c[0])).toEqual([
      `/api/v1/projects/${project}/mcp/oauth/begin`,
      '/api/v1/mcp/oauth/complete',
      `/api/v1/projects/${project}/mcp/commands/lookup`,
    ])
    for (const [, options] of fetch.mock.calls) {
      expect(options.method).toBe('POST')
      expect(new Headers(options.headers).get('Idempotency-Key')).toBe(write.key)
      expect(new Headers(options.headers).get('X-CSRF-Token')).toBe(write.csrfToken)
    }
    expect(JSON.parse(String(fetch.mock.calls[1]![1].body))).toEqual(body)
  })
  it('keeps issuer optional and applies the exact ASCII/code and canonical-state boundaries', () => {
    expect(captureMCPOAuthReturn({ ...body, issuer: '', code: 'a'.repeat(8192) }).issuer).toBe('')
    for (const bad of [
      { ...body, code: 'a'.repeat(8193) },
      { ...body, code: '中文' },
      { ...body, state: 'B'.repeat(43) },
      { ...body, project_id: project },
    ])
      expect(() => captureMCPOAuthReturn(bad)).toThrow(AccountFailure)
  })
})
