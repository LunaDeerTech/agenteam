import { describe, expect, it, vi } from 'vitest'
import { watch } from 'vue'
import { AccountFailure, type Fetch } from '../api/client'
import { createAgentMemoryAPI, type AgentMemoryAPI, type MemoryDelete } from '../api/agent-memory'
import { createAgentMemorySession } from '../composables/agent-memory-session'
import { createSessionController } from '../composables/useSession'
import { createAccountAPI } from '../api/account'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  agent = id(2),
  memory = id(3),
  instant = '2026-10-14T00:00:00.000000Z'
const input: MemoryDelete = { memory_id: memory, expected_revision: '9007199254740993' }
const receipt = { memory_id: memory, revision: input.expected_revision, deleted: true as const }
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
  })
const revision = {
  memory_id: memory,
  revision: input.expected_revision,
  status: 'active',
  type: 'procedure',
  text: '迁移前验证 schema。',
  tags: ['migration'],
  importance: 'high',
  provenance: { execution_id: id(4), operation_id: id(5), derived_from_memory_ids: [] },
  created_at: instant,
}
describe('Memory management wire contract', () => {
  it('preserves exact revision and namespace, rejects foreign or hidden fields', async () => {
    const record = {
      namespace: { project_id: project, agent_id: agent },
      current: revision,
      updated_at: instant,
    }
    const fetch = vi.fn<Fetch>(async () => json({ memories: [record] })),
      api = createAgentMemoryAPI(fetch)
    expect(
      (await api.list(project, agent, {}, new AbortController().signal)).memories[0]?.current
        .revision,
    ).toBe(input.expected_revision)
    expect(fetch.mock.calls[0]?.[0]).toBe(
      `/api/v1/projects/${project}/agents/${agent}/memory?limit=50`,
    )
    for (const wrong of [
      { ...record, namespace: { project_id: project, agent_id: id(6) } },
      { ...record, current: { ...revision, secret: 'unexpected' } },
      { ...record, current: { ...revision, status: 'deleted' } },
    ]) {
      await expect(
        createAgentMemoryAPI(async () => json({ memories: [wrong] })).list(
          project,
          agent,
          {},
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('binds delete and lookup to the original exact revision and request key', async () => {
    const fetch = vi.fn<Fetch>(async (url) =>
        json(String(url).endsWith('delete-lookup') ? { status: 'committed', receipt } : receipt),
      ),
      api = createAgentMemoryAPI(fetch),
      write = { csrfToken: 'S'.repeat(43), key: id(7) }
    await api.delete(project, agent, input, write)
    await api.lookup(project, agent, input, write)
    for (const [, options] of fetch.mock.calls) {
      expect(options?.body).toBe(JSON.stringify({ expected_revision: input.expected_revision }))
      expect(new Headers(options?.headers).get('Idempotency-Key')).toBe(write.key)
    }
    await expect(
      createAgentMemoryAPI(async () => json({ ...receipt, revision: '1' })).delete(
        project,
        agent,
        input,
        write,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
function setup() {
  let identity = { userID: id(10), sessionID: id(11), epoch: 1 },
    busy = false
  const api: AgentMemoryAPI = {
    list: vi.fn(),
    history: vi.fn(),
    delete: vi.fn(async () => {
      throw new AccountFailure('transport')
    }),
    lookup: vi.fn(async () => ({ status: 'committed' as const, receipt })),
  }
  const session = createAgentMemorySession(api, {
    capture: () => ({ identity, csrf: 'S'.repeat(43), key: id(12) }),
    identity: () => identity,
    current: (value) => value === identity,
    busy: () => busy,
    cancelRead() {},
    async run(_identity, _kind, work) {
      busy = true
      try {
        return await work(new AbortController().signal, () => true)
      } finally {
        busy = false
      }
    },
  })
  return {
    api,
    session,
    changeIdentity() {
      identity = { ...identity, epoch: 2 }
    },
    setBusy(value: boolean) {
      busy = value
    },
  }
}
describe('Memory original delete ownership', () => {
  it('releases an undispatched command if the request owner becomes busy during publication', async () => {
    const { api, session, setBusy } = setup()
    const stop = watch(
      () => session.progress,
      (value) => {
        if (value?.phase === 'submitting') setBusy(true)
      },
      { flush: 'sync' },
    )
    try {
      await expect(session.start(project, agent, input)).rejects.toMatchObject({ kind: 'busy' })
      expect(session.progress?.phase).toBe('rejected')
      expect(api.delete).not.toHaveBeenCalled()
    } finally {
      stop()
      setBusy(false)
    }
    vi.mocked(api.delete).mockResolvedValue(receipt)
    await session.start(project, agent, input)
    expect(api.delete).toHaveBeenCalledTimes(1)
    expect(session.progress?.phase).toBe('confirmed')
  })
  it('uses the real Session cookie owner and captured CSRF for recovery', async () => {
    const calls: { path: string; body: unknown; key: string | null; csrf: string | null }[] = []
    const fetch: Fetch = async (path, options) => {
      if (path === '/api/v1/session')
        return json({
          user: {
            id: id(10),
            email: 'owner@example.test',
            username: 'owner',
            display_name: 'Owner',
            role: 'user',
            theme: 'system',
            version: '1',
            initial_password_suggestion: false,
          },
          session: {
            id: id(11),
            issued_at: instant,
            absolute_expires_at: instant,
            idle_expires_at: instant,
          },
          csrf_token: 'S'.repeat(43),
        })
      const headers = new Headers(options.headers)
      calls.push({
        path,
        body: JSON.parse(String(options.body)),
        key: headers.get('Idempotency-Key'),
        csrf: headers.get('X-CSRF-Token'),
      })
      if (path.endsWith('/delete')) throw new TypeError('response lost')
      return json({ status: 'committed', receipt })
    }
    const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
    args[19] = { agentMemory: createAgentMemoryAPI(fetch) }
    const auth = createSessionController(...args)
    try {
      await auth.restore()
      await expect(auth.agentMemory.start(project, agent, input)).rejects.toMatchObject({
        kind: 'transport',
      })
      expect(auth.state.busy).toBe(false)
      await auth.agentMemory.lookup()
      expect(calls).toHaveLength(2)
      expect(calls[0]?.path).toContain('/delete')
      expect(calls[1]?.path).toContain('/delete-lookup')
      expect(calls[1]?.body).toEqual(calls[0]?.body)
      expect(calls[1]?.key).toBe(calls[0]?.key)
      expect(calls[1]?.csrf).toBe('S'.repeat(43))
      expect(auth.agentMemory.progress?.phase).toBe('confirmed')
    } finally {
      auth.leave()
    }
  })
  it('keeps unknown input for lookup and never dispatches a second delete', async () => {
    const { api, session } = setup()
    await expect(session.start(project, agent, input)).rejects.toMatchObject({ kind: 'transport' })
    expect(session.progress?.phase).toBe('uncertain')
    await expect(session.start(project, agent, input)).rejects.toMatchObject({ kind: 'busy' })
    await session.lookup()
    expect(api.delete).toHaveBeenCalledTimes(1)
    expect(api.lookup).toHaveBeenCalledTimes(1)
    const original = vi.mocked(api.delete).mock.calls[0]!,
      lookup = vi.mocked(api.lookup).mock.calls[0]!
    expect(lookup.slice(0, 3)).toEqual(original.slice(0, 3))
    expect(lookup[3].key).toBe(original[3].key)
    expect(session.progress?.phase).toBe('confirmed')
  })
  it('does not interpret missing receipt as permission to replay', async () => {
    const { api, session } = setup()
    vi.mocked(api.lookup).mockResolvedValue({ status: 'not_found' })
    await expect(session.start(project, agent, input)).rejects.toBeInstanceOf(AccountFailure)
    await session.lookup()
    expect(session.progress?.phase).toBe('uncertain')
    expect(api.delete).toHaveBeenCalledTimes(1)
  })
  it('rejects recovery after session identity changes', async () => {
    const { api, session, changeIdentity } = setup()
    await expect(session.start(project, agent, input)).rejects.toBeInstanceOf(AccountFailure)
    changeIdentity()
    expect(session.progress?.contextValid).toBe(false)
    await expect(session.lookup()).rejects.toMatchObject({ kind: 'busy' })
    expect(api.lookup).not.toHaveBeenCalled()
  })
  it('rejects confirmed publication if a synchronous observer clears identity', async () => {
    const { api, session } = setup()
    vi.mocked(api.delete).mockResolvedValue(receipt)
    const stop = watch(
      () => session.progress,
      (value) => {
        if (value?.phase === 'confirmed') session.clear()
      },
      { flush: 'sync' },
    )
    await expect(session.start(project, agent, input)).rejects.toMatchObject({ kind: 'cancelled' })
    stop()
  })
})
