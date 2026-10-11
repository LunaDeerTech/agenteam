import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import { agentLabel, createAgentDirectoryAPI } from '../api/agent-directory'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1)
const instant = '2026-10-11T12:00:00.000000Z'
const signal = () => new AbortController().signal
const entry = (n = 30) => ({
  id: id(n),
  project_id: project,
  name: 'Review-Agent',
  display_name: null,
  tag_color: null,
  description: '负责评审，不包含运行配置。',
  version: '9007199254740993',
  created_at: instant,
  updated_at: instant,
})
const response = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })

describe('Agent directory closed client', () => {
  it('reads safe identities and carries the opaque cursor to the next explicit page', async () => {
    const first = { ...entry(), display_name: '评审员 🧩', tag_color: '#123abc' }
    const cursor = 'opaque+/=字'
    const bodies = [{ items: [first], next_cursor: cursor }, { items: [entry(20)] }, entry()]
    const fetcher = vi.fn<Fetch>(async () => response(bodies.shift()))
    const api = createAgentDirectoryAPI(fetcher)
    const page = await api.list(project, { limit: 1 }, signal())
    expect(page.items).toEqual([first])
    expect(agentLabel(page.items[0]!)).toBe('评审员 🧩')
    expect(Object.isFrozen(page)).toBe(true)
    expect(Object.isFrozen(page.items)).toBe(true)
    expect(Object.isFrozen(page.items[0])).toBe(true)
    const next = await api.list(project, { limit: 1, cursor: page.next_cursor! }, signal())
    expect(next).toEqual({ items: [entry(20)] })
    expect(agentLabel(next.items[0]!)).toBe('Review-Agent')
    expect(await api.get(project, id(30), signal())).toEqual(entry())
    expect(fetcher.mock.calls.map(([path]) => path)).toEqual([
      `/api/v1/projects/${project}/agents?limit=1`,
      `/api/v1/projects/${project}/agents?limit=1&cursor=opaque%2B%2F%3D%E5%AD%97`,
      `/api/v1/projects/${project}/agents/${id(30)}`,
    ])
    for (const [, init] of fetcher.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.body).toBeUndefined()
      expect(init.headers).not.toHaveProperty('X-CSRF-Token')
      expect(init.headers).not.toHaveProperty('Idempotency-Key')
    }
    expect(Object.keys(page.items[0]!).sort()).toEqual([
      'created_at',
      'description',
      'display_name',
      'id',
      'name',
      'project_id',
      'tag_color',
      'updated_at',
      'version',
    ])
  })

  it('rejects foreign, extended and malformed entries and publishes no partial page', async () => {
    for (const change of [
      { project_id: id(2) },
      { id: id(31) },
      { instructions: 'private runtime configuration' },
      { display_name: undefined },
      { version: 1 },
      { updated_at: '2025-10-11T12:00:00.000000Z' },
    ]) {
      const api = createAgentDirectoryAPI(async () => response({ ...entry(), ...change }))
      await expect(api.get(project, id(30), signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    // Keep both raw members: ordinary JSON.parse would retain the valid last ID.
    const duplicate = `{"id":"${id(31)}",${JSON.stringify(entry()).slice(1)}`
    const rawAPI = createAgentDirectoryAPI(
      async () => new Response(duplicate, { headers: { 'Content-Type': 'application/json' } }),
    )
    await expect(rawAPI.get(project, id(30), signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    for (const body of [
      { items: [entry(), { ...entry(20), model_ref: id(9) }] },
      { items: [entry(), entry()] },
      { items: [entry(20), entry()] },
      { items: [], next_cursor: 'next' },
      { items: [entry(), entry(20)], next_cursor: 'same' },
    ]) {
      const api = createAgentDirectoryAPI(async () => response(body))
      await expect(api.list(project, { limit: 2, cursor: 'same' }, signal())).rejects.toMatchObject(
        {
          kind: 'invalid-response',
        },
      )
    }
  })

  it('rejects invalid local scope and queries before issuing a request', async () => {
    const fetcher = vi.fn<Fetch>(async () => response({ items: [] }))
    const api = createAgentDirectoryAPI(fetcher)
    for (const query of [{ limit: 201 }, { cursor: '' }, { cursor: '\ud800' }, { busy: false }]) {
      await expect(
        Promise.resolve().then(() => api.list(project, query as never, signal())),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    await expect(
      Promise.resolve().then(() => api.get('../other', id(30), signal())),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('preserves current-owner and cursor Problems without automatic page recovery', async () => {
    for (const [code, status] of [
      ['NOT_FOUND', 404],
      ['CURSOR_INVALID', 400],
      ['UNAUTHENTICATED', 401],
    ] as const) {
      const body = {
        type: 'urn:agenteam:problem:test',
        title: 'Denied',
        status,
        detail: 'safe failure',
        instance: `/api/v1/projects/${project}/agents`,
        code,
        request_id: id(90),
        commit_state: 'not_started',
      }
      const fetcher = vi.fn<Fetch>(
        async () =>
          new Response(JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
          }),
      )
      await expect(
        createAgentDirectoryAPI(fetcher).list(project, { cursor: 'original' }, signal()),
      ).rejects.toMatchObject({ kind: 'problem', problem: body })
      expect(fetcher).toHaveBeenCalledTimes(1)
    }
  })
})
