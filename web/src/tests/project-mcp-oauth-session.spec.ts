import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createAccountAPI } from '../api/account'
import { type Fetch } from '../api/client'
import { createMCPOAuthAPI } from '../api/mcp-oauth'
import { createProjectMCPAPI } from '../api/project-mcp'
import { createMCPCredentialAPI } from '../api/project-mcp-credentials'
import { createSessionController, type SessionController } from '../composables/useSession'
import { installAuthentication } from '../router/auth'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  config = id(2),
  state = 'A'.repeat(43)
const storageKey = 'agenteam.mcp.oauth.v1'
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
    idle_expires_at: '2099-10-15T00:00:00.123456Z',
    absolute_expires_at: '2099-10-16T00:00:00.123456Z',
  },
  csrf_token: 'S'.repeat(43),
}
const connected = {
  connection: {
    id: id(3),
    project_id: project,
    config_id: config,
    version: '2',
    config_revision: '1',
    state: 'connected',
    enabled: true,
    tools_state: 'ready',
    resources_state: 'ready',
    tools_revision: '1',
    resources_revision: '1',
  },
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })
const controllers: SessionController[] = []
afterEach(() => {
  controllers.forEach((v) => v.leave())
  controllers.length = 0
  sessionStorage.clear()
  window.history.replaceState(null, '', '/')
  vi.restoreAllMocks()
})
function originalFlow() {
  sessionStorage.setItem(
    storageKey,
    JSON.stringify({
      user: session.user.id,
      session: session.session.id,
      project,
      returnPath: '/owner/project/settings/mcp',
      command: 'oauth.begin',
      key: id(10),
      flow: id(11),
      state,
      expires: '2099-10-14T23:59:59Z',
    }),
  )
}
function fixture(mcp: Fetch, onSession?: () => void) {
  const fetch = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') {
      onSession?.()
      return json(session)
    }
    return mcp(url, init)
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = {
    mcp: createProjectMCPAPI(fetch),
    mcpCredentials: createMCPCredentialAPI(fetch),
    mcpOAuth: createMCPOAuthAPI(fetch),
  }
  const auth = createSessionController(...args)
  controllers.push(auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/mcp/oauth/callback',
        component: { template: '<div />' },
        meta: { authentication: true, protected: true },
      },
      {
        path: '/login',
        name: 'login',
        component: { template: '<div />' },
        meta: { authentication: true },
      },
    ],
  })
  installAuthentication(router, auth)
  return { auth, router, fetch }
}
async function returnTo(f: ReturnType<typeof fixture>) {
  const path = `/mcp/oauth/callback?state=${state}&code=owned-code&iss=https%3A%2F%2Fauth.example.test`
  window.history.replaceState(null, '', path)
  await f.router.push(path)
  await f.router.isReady()
}

describe('OAuth callback through the actual Session, router and account transport', () => {
  it('clears callback bytes before Session restoration and exchanges only by CSRF POST', async () => {
    originalFlow()
    const f = fixture(
      async () => {
        const saved = sessionStorage.getItem(storageKey)!
        expect(saved).not.toContain('owned-code')
        expect(saved).not.toContain(session.csrf_token)
        expect(JSON.parse(saved).command).toBe('oauth.complete')
        return json(connected)
      },
      () => {
        expect(window.location.pathname).toBe('/mcp/oauth/callback')
        expect(window.location.search).toBe('')
        expect(window.location.hash).toBe('')
      },
    )
    await returnTo(f)
    expect(f.router.currentRoute.value.fullPath).toBe('/mcp/oauth/callback')
    expect(f.fetch.mock.calls.map(([url]) => url)).toEqual(['/api/v1/session'])
    await f.auth.projectMCP.oauth.completeReturn()
    const [url, request] = f.fetch.mock.calls[1]!
    expect(url).toBe('/api/v1/mcp/oauth/complete')
    expect(request.method).toBe('POST')
    expect(new Headers(request.headers).get('X-CSRF-Token')).toBe(session.csrf_token)
    expect(new Headers(request.headers).get('Idempotency-Key')).not.toBe(id(10))
    expect(JSON.parse(String(request.body))).toEqual({
      state,
      code: 'owned-code',
      issuer: 'https://auth.example.test',
    })
    expect(f.auth.projectMCP.oauth.progress).toMatchObject({
      phase: 'confirmed',
      contextValid: true,
      returnPath: '/owner/project/settings/mcp',
    })
    expect(sessionStorage.getItem(storageKey)).toBeNull()
  })

  it('restores an uncertain Complete by the original passive lookup without a second exchange', async () => {
    originalFlow()
    const first = fixture(async () => {
      throw new Error('response lost')
    })
    await returnTo(first)
    await expect(first.auth.projectMCP.oauth.completeReturn()).rejects.toMatchObject({
      kind: 'transport',
    })
    const originalKey = new Headers(first.fetch.mock.calls[1]![1].headers).get('Idempotency-Key')
    const saved = sessionStorage.getItem(storageKey)!
    expect(saved).not.toContain('owned-code')
    expect(first.auth.projectMCP.oauth.progress).toMatchObject({ phase: 'uncertain' })
    await expect(
      first.auth.projectMCP.start(project, {
        kind: 'connection.connect',
        input: { config_id: config, expected_version: '0' },
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      first.auth.projectMCP.credentials.start(project, { kind: 'create' }, 'unused'),
    ).rejects.toMatchObject({ kind: 'busy' })

    // A fresh page has no callback code. Only same-tab, same-Session metadata survives.
    const restored = fixture(async () => json({ state: 'complete', result: connected }))
    await restored.router.push('/mcp/oauth/callback')
    expect(restored.auth.projectMCP.oauth.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    expect(restored.fetch.mock.calls).toHaveLength(1)
    await restored.auth.projectMCP.oauth.completeReturn()
    const [url, request] = restored.fetch.mock.calls[1]!
    expect(url).toBe(`/api/v1/projects/${project}/mcp/commands/lookup`)
    expect(JSON.parse(String(request.body))).toEqual({ command: 'oauth.complete' })
    expect(new Headers(request.headers).get('Idempotency-Key')).toBe(originalKey)
    expect(restored.auth.projectMCP.oauth.progress).toMatchObject({ phase: 'confirmed' })
    expect(restored.fetch.mock.calls.some(([url]) => url.endsWith('/oauth/complete'))).toBe(false)
  })

  it('erases the captured return when the original identity is left before dispatch', async () => {
    originalFlow()
    const f = fixture(async () => json(connected))
    await returnTo(f)
    f.auth.leave()
    expect(sessionStorage.getItem(storageKey)).toBeNull()
    expect(f.auth.projectMCP.oauth.progress).toBeNull()
    await expect(f.auth.projectMCP.oauth.completeReturn()).rejects.toBeDefined()
    expect(f.fetch.mock.calls.map(([url]) => url)).toEqual(['/api/v1/session'])
  })

  it.each(['oauth.begin', 'oauth.complete'])(
    'ends only the retired %s intent on stopped and requires an explicit new authorization',
    async (command) => {
      originalFlow()
      const saved = JSON.parse(sessionStorage.getItem(storageKey)!)
      saved.command = command
      if (command === 'oauth.begin') saved.expires = '2020-01-01T00:00:00Z'
      sessionStorage.setItem(storageKey, JSON.stringify(saved))
      const f = fixture(async (url) =>
        json(
          url.endsWith('/lookup')
            ? { state: 'stopped' }
            : { connection: { ...connected.connection, state: 'auth_required' } },
        ),
      )
      await f.auth.restore()
      expect(f.auth.projectMCP.oauth.pending).toBe(true)
      await f.auth.projectMCP.oauth.lookup()
      expect(f.auth.projectMCP.oauth.progress).toMatchObject({
        phase: 'stopped',
        observation: 'stopped',
        contextValid: true,
      })
      expect(f.auth.projectMCP.oauth.pending).toBe(false)
      expect(sessionStorage.getItem(storageKey)).toBeNull()
      expect(f.fetch.mock.calls.map(([url]) => url)).toEqual([
        '/api/v1/session',
        `/api/v1/projects/${project}/mcp/commands/lookup`,
      ])
      expect(new Headers(f.fetch.mock.calls[1]![1].headers).get('Idempotency-Key')).toBe(saved.key)
      await f.auth.projectMCP.oauth.begin(
        project,
        { config_id: config, expected_version: '2' },
        '/owner/project/settings/mcp',
      )
      const [url, request] = f.fetch.mock.calls[2]!
      expect(url).toBe(`/api/v1/projects/${project}/mcp/oauth/begin`)
      expect(new Headers(request.headers).get('Idempotency-Key')).not.toBe(saved.key)
      expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/oauth/complete'))).toBe(false)
    },
  )
})
