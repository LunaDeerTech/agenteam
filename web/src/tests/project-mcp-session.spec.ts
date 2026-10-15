import { describe, expect, it, vi } from 'vitest'
import { createAccountAPI } from '../api/account'
import { createProjectMCPAPI } from '../api/project-mcp'
import { createMCPCredentialAPI } from '../api/project-mcp-credentials'
import { type Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  config = id(2),
  connection = id(3)
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
const connected = {
  connection: {
    id: connection,
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
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })
function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
async function fixture(mcp: Fetch) {
  const fetch = vi.fn<Fetch>(async (url, init) =>
    url === '/api/v1/session' ? json(session) : mcp(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { mcp: createProjectMCPAPI(fetch), mcpCredentials: createMCPCredentialAPI(fetch) }
  const auth = createSessionController(...args)
  await auth.restore()
  return { auth, fetch }
}
const command = {
  kind: 'connection.connect',
  input: { config_id: config, expected_version: '0' },
} as const
describe('MCP original Session command ownership', () => {
  it('ends a stopped refresh intent without replay and permits only an explicit new operation', async () => {
    let writes = 0
    const { auth, fetch } = await fixture(async (url) => {
      if (url.endsWith('/commands/lookup')) return json({ state: 'stopped' })
      if (++writes === 1) throw new Error('original refresh response lost')
      return json(connected)
    })
    const refresh = { kind: 'connection.refresh', target: connection, version: '1' } as const
    try {
      await expect(auth.projectMCP.start(project, refresh)).rejects.toMatchObject({
        kind: 'transport',
      })
      await expect(auth.projectMCP.start(project, refresh)).rejects.toMatchObject({ kind: 'busy' })
      await expect(auth.projectMCP.lookup()).resolves.toEqual({ state: 'stopped' })
      expect(auth.projectMCP.progress).toMatchObject({
        phase: 'stopped',
        observation: 'stopped',
        contextValid: true,
      })
      expect(writes).toBe(1)
      await expect(auth.projectMCP.lookup()).rejects.toMatchObject({ kind: 'invalid-input' })
      await auth.projectMCP.start(project, { ...refresh, version: '2' })
      const calls = fetch.mock.calls.filter(([url]) => url !== '/api/v1/session')
      const keys = calls.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))
      expect(keys[0]).toBe(keys[1])
      expect(keys[2]).not.toBe(keys[0])
      expect(writes).toBe(2)
    } finally {
      auth.leave()
    }
  })
  it('does not publish a late stopped observation into a replacement identity', async () => {
    const sent = deferred<void>(),
      response = deferred<Response>()
    const { auth } = await fixture(async (url) => {
      if (!url.endsWith('/commands/lookup')) throw new Error('lost refresh')
      sent.resolve()
      return response.promise
    })
    await expect(
      auth.projectMCP.start(project, {
        kind: 'connection.refresh',
        target: connection,
        version: '1',
      }),
    ).rejects.toMatchObject({ kind: 'transport' })
    const pending = auth.projectMCP.lookup()
    await sent.promise
    auth.leave()
    response.resolve(json({ state: 'stopped' }))
    await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    expect(auth.projectMCP.progress).toBeNull()
  })
  it('retains only the credential receipt identity after Unknown and never resends material', async () => {
    let lookups = 0
    const receipt = { credential_id: id(30), purpose: 'mcp', version: '1', deleted: false }
    const { auth, fetch } = await fixture(async (url) => {
      if (url.endsWith('/credentials')) throw new Error('transport disconnected')
      return json(
        ++lookups === 1 ? { observed: false, result: null } : { observed: true, result: receipt },
      )
    })
    try {
      await expect(
        auth.projectMCP.credentials.start(project, { kind: 'create' }, 'private-token', config),
      ).rejects.toMatchObject({ kind: 'transport' })
      expect(auth.projectMCP.credentials.progress).toMatchObject({
        phase: 'uncertain',
        config,
        contextValid: true,
      })
      expect(JSON.stringify(auth.projectMCP.credentials.progress)).not.toContain('private-token')
      await expect(auth.projectMCP.start(project, command)).rejects.toMatchObject({ kind: 'busy' })
      await expect(
        auth.projectMCP.credentials.start(project, { kind: 'create' }, 'new-token'),
      ).rejects.toMatchObject({ kind: 'busy' })
      await auth.projectMCP.credentials.lookup()
      expect(auth.projectMCP.credentials.progress).toMatchObject({
        phase: 'uncertain',
        observation: 'not_found',
      })
      await auth.projectMCP.credentials.lookup()
      expect(auth.projectMCP.credentials.progress).toMatchObject({
        phase: 'confirmed',
        observation: 'complete',
        result: receipt,
      })
      const calls = fetch.mock.calls.filter(([url]) => url !== '/api/v1/session')
      expect(calls.filter(([url]) => url.endsWith('/credentials'))).toHaveLength(1)
      expect(
        new Set(calls.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
      ).toBe(1)
      for (const [, init] of calls.slice(1)) expect(init.body).toBe('{"kind":"create"}')
    } finally {
      auth.leave()
    }
  })
  it('uses the same live cookie owner and suppresses credential receipt publication after identity leaves', async () => {
    const sent = deferred<void>(),
      response = deferred<Response>()
    const { auth } = await fixture(async () => {
      sent.resolve()
      return response.promise
    })
    const pending = auth.projectMCP.credentials.start(
      project,
      { kind: 'create' },
      'private-token',
      config,
    )
    await sent.promise
    await expect(auth.projectMCP.connections(project)).rejects.toMatchObject({ kind: 'busy' })
    auth.leave()
    response.resolve(json({ credential_id: id(30), purpose: 'mcp', version: '1', deleted: false }))
    await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    expect(auth.projectMCP.credentials.progress).toBeNull()
  })
  it('retains unknown original key and only looks it up, including not_found and pending', async () => {
    let reads = 0
    const { auth, fetch } = await fixture(async (url) => {
      if (url.endsWith('/connect')) throw new Error('transport disconnected')
      return json(
        ++reads === 1
          ? { state: 'not_found' }
          : reads === 2
            ? { state: 'pending' }
            : { state: 'complete', result: connected },
      )
    })
    try {
      await expect(auth.projectMCP.start(project, command)).rejects.toMatchObject({
        kind: 'transport',
      })
      expect(auth.projectMCP.progress).toMatchObject({ phase: 'uncertain', contextValid: true })
      await expect(auth.projectMCP.start(project, command)).rejects.toMatchObject({ kind: 'busy' })
      await auth.projectMCP.lookup()
      expect(auth.projectMCP.progress).toMatchObject({
        phase: 'uncertain',
        observation: 'not_found',
      })
      await auth.projectMCP.lookup()
      expect(auth.projectMCP.progress).toMatchObject({ phase: 'uncertain', observation: 'pending' })
      await auth.projectMCP.lookup()
      expect(auth.projectMCP.progress).toMatchObject({
        phase: 'confirmed',
        observation: 'complete',
        contextValid: true,
      })
      const writes = fetch.mock.calls.filter(([url]) => url !== '/api/v1/session')
      expect(writes.filter(([url]) => url.endsWith('/connect'))).toHaveLength(1)
      expect(
        new Set(writes.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
      ).toBe(1)
      expect(
        writes.every(
          ([, init]) => new Headers(init.headers).get('X-CSRF-Token') === session.csrf_token,
        ),
      ).toBe(true)
    } finally {
      auth.leave()
    }
  })
  it('retains the actual request tail after navigation and prevents late publication', async () => {
    const sent = deferred<void>(),
      response = deferred<Response>()
    const { auth } = await fixture(async () => {
      sent.resolve()
      return response.promise
    })
    const pending = auth.projectMCP.start(project, command)
    await sent.promise
    auth.leave()
    response.resolve(json(connected))
    await expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    expect(auth.projectMCP.progress).toBeNull()
  })
  it('shares the cookie/request owner with other Session operations', async () => {
    const sent = deferred<void>(),
      response = deferred<Response>()
    const { auth, fetch } = await fixture(async () => {
      sent.resolve()
      return response.promise
    })
    try {
      const pending = auth.projectMCP.start(project, command)
      await sent.promise
      await expect(auth.projectMCP.connections(project)).rejects.toMatchObject({ kind: 'busy' })
      expect(fetch).toHaveBeenCalledTimes(2)
      response.resolve(json(connected))
      await pending
      expect(auth.projectMCP.progress).toMatchObject({ phase: 'confirmed' })
    } finally {
      response.resolve(json(connected))
      auth.leave()
    }
  })
})
