import { describe, expect, it, vi } from 'vitest'
import {
  createAgentRunnerMountsAPI,
  matchingRunnerMounts,
  captureRunnerMountDeclarations,
} from '../api/agent-runner-mounts'
import {
  captureAgentCreateInput,
  captureAgentUpdateInput,
  createAgentConfigurationAPI,
} from '../api/agent-configuration'
import type { Fetch } from '../api/client'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const signal = () => new AbortController().signal
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })
const candidate = (n = 2) => ({
  id: id(n),
  name: '系统 Runner',
  description: '安全说明',
  version: '9007199254740993',
  status: 'offline',
})
const declaration = () => ({ runner_id: id(2), workspace: 'sources.v1' })
const mounts = () => ({
  project_id: id(1),
  agent_id: id(3),
  agent_version: '4',
  items: [{ mount_id: id(4), ...declaration(), name: '旧目录名称' }],
})
const fields = () => ({
  agent_id: id(3),
  name: 'Test-Agent',
  model_ref: id(5),
  inject_agents_md: true,
  approval_policy: 'default',
  allowed_tool_ids: [],
  allowed_secret_variable_ids: [],
  install_skill_enabled: false,
  runner_mounts: [declaration()],
})
const config = () => ({
  id: id(3),
  project_id: id(1),
  name: 'Test-Agent',
  normalized_name: 'test-agent',
  display_name: null,
  tag_color: null,
  description: '',
  instructions: '',
  model_ref: id(5),
  reasoning_effort: null,
  inject_agents_md: true,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [id(4)],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
  version: '1',
  created_at: '2026-10-11T10:00:00.000000Z',
  updated_at: '2026-10-11T10:00:00.000000Z',
})
const receipt = () => ({
  agent: config(),
  changed: true,
  event_ids: [id(6)],
  runner_mounts: [{ ...declaration(), mount_id: id(4), workspace_id: id(7) }],
})
const write = { csrfToken: 's'.repeat(43), key: 'runner-original/1' }

describe('Agent Runner configuration wire', () => {
  it('reads safe system candidates including offline/incompatible with explicit pages', async () => {
    const fetcher = vi.fn<Fetch>(async () =>
      json({
        items: [candidate(), { ...candidate(8), status: 'incompatible' }],
        next_cursor: 'opaque',
      }),
    )
    const api = createAgentRunnerMountsAPI(fetcher)
    const page = await api.list(id(1), { limit: 2 }, signal())
    expect(page.items.map((r) => r.status)).toEqual(['offline', 'incompatible'])
    expect(page.items[0]!.version).toBe('9007199254740993')
    expect(fetcher.mock.calls[0]![0]).toBe(`/api/v1/projects/${id(1)}/available-runners?limit=2`)
    expect(fetcher.mock.calls[0]![1]).toMatchObject({
      method: 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
    })
    expect(fetcher.mock.calls[0]![1].body).toBeUndefined()
    for (const q of [{ limit: 51 }, { limit: null }, { cursor: null }, { root_path: '/' }])
      expect(() => api.list(id(1), q as never, signal())).toThrow('invalid-input')
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it('rejects administrative fields, partial/bad final rows, duplicate JSON and oversized reads', async () => {
    for (const body of [
      { items: [{ ...candidate(), root_path: '/PRIVATE_MARKER' }] },
      { items: [candidate(), { ...candidate(8), version: '0' }] },
      { items: [candidate(), candidate()] },
      { items: [], next_cursor: null },
    ])
      await expect(
        createAgentRunnerMountsAPI(async () => json(body)).list(id(1), {}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const raw of ['{"items":[],"items":[]}', ' '.repeat(2 * 1024 * 1024) + '{"items":[]}']) {
      const api = createAgentRunnerMountsAPI(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(api.list(id(1), {}, signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('requires full current scope, sorted mapping identities and matching Agent version/references', async () => {
    const value = await createAgentRunnerMountsAPI(async () => json(mounts())).get(
      id(1),
      id(3),
      signal(),
    )
    expect(matchingRunnerMounts({ ...config(), version: '4' }, value)).toBe(true)
    expect(matchingRunnerMounts(config(), value)).toBe(false)
    expect(matchingRunnerMounts({ ...config(), version: '4', allowed_mount_ids: [] }, value)).toBe(
      false,
    )
    for (const body of [
      { ...mounts(), agent_id: id(9) },
      { ...mounts(), project_id: id(9) },
      { ...mounts(), items: null },
      { ...mounts(), items: [mounts().items[0], mounts().items[0]] },
      { ...mounts(), workspace_id: id(7) },
    ])
      await expect(
        createAgentRunnerMountsAPI(async () => json(body)).get(id(1), id(3), signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('captures exact declarations and the legacy profile without normalizing names', () => {
    const original = fields(),
      captured = captureAgentCreateInput({ request: original })
    expect(captured.request.runner_mounts).toEqual([declaration()])
    expect(captured.request).not.toHaveProperty('allowed_mount_ids')
    original.runner_mounts[0]!.workspace = 'changed'
    expect(captured.request.runner_mounts![0]!.workspace).toBe('sources.v1')
    expect(Object.isFrozen(captured.request.runner_mounts)).toBe(true)
    const { runner_mounts: _omit, ...legacy } = fields()
    expect(
      captureAgentCreateInput({ request: { ...legacy, allowed_mount_ids: [] } }).request
        .allowed_mount_ids,
    ).toEqual([])
    for (const request of [
      legacy,
      { ...fields(), allowed_mount_ids: [] },
      { ...fields(), runner_mounts: null },
    ])
      expect(() => captureAgentCreateInput({ request })).toThrow('invalid-input')
    for (const workspace of [
      '.',
      '../x',
      'x/y',
      'x\\y',
      'COM1.txt',
      'NUL',
      'x.',
      ' x',
      '中文',
      'x'.repeat(256),
    ])
      expect(() => captureRunnerMountDeclarations([{ ...declaration(), workspace }])).toThrow(
        'invalid-input',
      )
    expect(() => captureRunnerMountDeclarations([declaration(), declaration()])).toThrow(
      'invalid-input',
    )
    expect(
      captureRunnerMountDeclarations([{ ...declaration(), workspace: 'x'.repeat(255) }]),
    ).toHaveLength(1)
  })
  it('preserves update omission versus clear and does not accept null', () => {
    expect(
      captureAgentUpdateInput({ expected_version: '4', request: { description: 'edit' } }).request,
    ).not.toHaveProperty('runner_mounts')
    expect(
      captureAgentUpdateInput({ expected_version: '4', request: { runner_mounts: [] } }).request,
    ).toEqual({ runner_mounts: [] })
    expect(() =>
      captureAgentUpdateInput({ expected_version: '4', request: { runner_mounts: null } }),
    ).toThrow('invalid-input')
  })
  it('matches receipt generated identities and keeps the original body/key in Lookup', async () => {
    const original = captureAgentCreateInput({ request: fields() })
    const fetcher = vi.fn<Fetch>(async (path) =>
      json(
        String(path).endsWith('/lookup') ? { status: 'committed', receipt: receipt() } : receipt(),
      ),
    )
    const api = createAgentConfigurationAPI(fetcher)
    expect(await api.create(id(1), original, write)).toEqual(receipt())
    expect((await api.lookup(id(1), original, write)).status).toBe('committed')
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toEqual({
      command: 'agent.create',
      ...original,
    })
    expect(fetcher.mock.calls[1]![1].headers).toMatchObject({ 'Idempotency-Key': write.key })
    for (const wrong of [
      undefined,
      [],
      null,
      [{ ...receipt().runner_mounts[0], workspace: 'other' }],
      [{ ...receipt().runner_mounts[0], mount_id: id(9) }],
    ]) {
      const bad = { ...receipt(), runner_mounts: wrong }
      await expect(
        createAgentConfigurationAPI(async () => json(bad)).create(id(1), original, write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
})

it('rejects terminal separators in exact workspace declarations', () => {
  for (const workspace of [
    'workspace\n',
    'workspace\r',
    'workspace\u2028',
    'workspace\u2029',
    'x'.repeat(255) + '\n',
  ])
    expect(
      () => captureRunnerMountDeclarations([{ runner_id: id(2), workspace }]),
      JSON.stringify(workspace),
    ).toThrow('invalid-input')
})

it('rejects sparse declarations before JSON can turn holes into null', () => {
  expect(() => captureRunnerMountDeclarations(Array(1))).toThrow('invalid-input')
})
it('rejects noncanonical decimal versions in both new current reads', async () => {
  await expect(
    createAgentRunnerMountsAPI(async () =>
      json({ items: [{ ...candidate(), version: '1\n' }] }),
    ).list(id(1), {}, signal()),
  ).rejects.toMatchObject({ kind: 'invalid-response' })
  await expect(
    createAgentRunnerMountsAPI(async () => json({ ...mounts(), agent_version: '4\n' })).get(
      id(1),
      id(3),
      signal(),
    ),
  ).rejects.toMatchObject({ kind: 'invalid-response' })
})
