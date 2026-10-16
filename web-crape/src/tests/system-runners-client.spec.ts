import { describe, expect, it, vi } from 'vitest'
import {
  createSystemRunnersAPI,
  captureRunnerCommand,
  captureRunnerQuery,
  parseSystemRunner,
  type RunnerCommand,
} from '../api/system-runners'
import type { Fetch } from '../api/client'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const row = (n = 3) => ({
  id: id(n),
  name: 'Fixture Runner',
  description: '',
  tags: ['one', 'two'],
  root_path: '/owned/workspace',
  version: '1',
  credential_generation: '1',
  status: 'offline',
  public_key_fingerprint: null,
  enrolled_at: null,
  last_seen_at: null,
  last_hello: null,
  created_at: time,
  updated_at: time,
})
const json = (v: unknown) =>
  new Response(JSON.stringify(v), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(90) },
  })
const command = (): RunnerCommand => ({
  command: 'runner.create',
  target: id(3),
  request: {
    runner_id: id(3),
    name: 'Fixture Runner',
    description: '',
    tags: ['one', 'two'],
    root_path: '/owned/workspace',
  },
})
const receipt = () => ({
  command_id: id(10),
  command: 'runner.create',
  runner: row(),
  changed: true,
})
const options = () => ({
  csrfToken: 'S'.repeat(43),
  key: 'original-fixture-key',
  signal: new AbortController().signal,
})
describe('System Runner strict public and one-time wire', () => {
  it('accepts public snapshots and protocol objects without imposing an invented capability order', () => {
    const value = {
      ...row(),
      status: 'online',
      public_key_fingerprint: 'sha256:' + 'a'.repeat(64),
      enrolled_at: time,
      last_seen_at: time,
      last_hello: {
        runner_id: id(3),
        runner_version: '1.0\u0085',
        protocol_version: { major: 1, minor: 0 },
        os: 'linux',
        arch: 'amd64',
        headless: true,
        capabilities: ['workspace.write', 'workspace.read'],
        feature_flags: ['zeta', 'alpha'],
      },
    }
    expect(parseSystemRunner(value)).toEqual(value)
    for (const extra of ['enrollment_token', 'credential_ref', 'private_key', 'unknown'])
      expect(() => parseSystemRunner({ ...row(), [extra]: 'private-marker' })).toThrow(
        'invalid-response',
      )
    expect(() =>
      parseSystemRunner({ ...value, last_hello: { ...value.last_hello, protocol_version: '1.0' } }),
    ).toThrow('invalid-response')
    expect(() => parseSystemRunner({ ...row(), version: '1\n' })).toThrow('invalid-response')
  })
  it('decodes the entire ordered page atomically with the exact after_id query and mandatory next', async () => {
    let value: unknown = { items: [row(3), row(4)], next: id(4) }
    const fetcher = vi.fn<Fetch>(async () => json(value)),
      api = createSystemRunnersAPI(fetcher),
      signal = new AbortController().signal
    expect((await api.list({ limit: 2, after_id: id(2) }, signal)).items).toHaveLength(2)
    expect(fetcher.mock.calls[0]![0]).toBe(`/api/v1/system/runners?limit=2&after_id=${id(2)}`)
    value = { items: [row(3), { ...row(4), private_key: 'private-marker' }], next: id(4) }
    await expect(api.list({ limit: 2 }, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    value = { items: [row(4), row(3)], next: id(3) }
    await expect(api.list({ limit: 2 }, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    value = { items: [row(3)] }
    await expect(api.list({}, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('accepts a byte-budget short page with next but rejects empty or mismatched next', async () => {
    let value: unknown = { items: [row(3)], next: id(3) }
    const api = createSystemRunnersAPI(async () => json(value)),
      signal = new AbortController().signal
    expect(await api.list({ limit: 200 }, signal)).toEqual(value)
    value = { items: [], next: id(3) }
    await expect(api.list({ limit: 200 }, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    value = { items: [row(3)], next: id(4) }
    await expect(api.list({ limit: 200 }, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('rejects duplicate JSON and malformed command input before transmission', async () => {
    const fetcher = vi.fn<Fetch>(
        async () =>
          new Response('{"items":[],"next":null,"next":null}', {
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
      api = createSystemRunnersAPI(fetcher)
    await expect(api.list({}, new AbortController().signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    const create = command()
    for (const request of [
      { ...create.request, root_path: '/x/../y' },
      { ...create.request, tags: Array(1) },
      { ...create.request, tags: ['two', 'one'] },
      { ...create.request, runner_id: id(4) },
    ])
      expect(() => captureRunnerCommand({ ...create, request })).toThrow('invalid-input')
    expect(() =>
      captureRunnerCommand({
        command: 'runner.update',
        target: id(3),
        request: { expected_version: '1', root_path: '/changed' } as never,
      }),
    ).toThrow('invalid-input')
    expect(() =>
      captureRunnerCommand({
        command: 'runner.update',
        target: id(3),
        request: { expected_version: '1' },
      }),
    ).toThrow('invalid-input')
    expect(() => captureRunnerQuery({ limit: null } as never)).toThrow('invalid-input')
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it('binds first material to the exact receipt and never accepts token material in Lookup', async () => {
    let value: unknown = {
      receipt: receipt(),
      token_available: true,
      enrollment_token: 'A'.repeat(43),
      expires_at: time,
    }
    const fetcher = vi.fn<Fetch>(async () => json(value)),
      api = createSystemRunnersAPI(fetcher),
      c = command()
    expect((await api.execute(c, options())).token_available).toBe(true)
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual(c.request)
    value = { state: 'committed', receipt: receipt(), token_available: false }
    expect((await api.lookup(c, options())).state).toBe('committed')
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toEqual({
      command: c.command,
      request: c.request,
    })
    value = {
      state: 'committed',
      receipt: receipt(),
      token_available: true,
      enrollment_token: 'A'.repeat(43),
    }
    await expect(api.lookup(c, options())).rejects.toMatchObject({ kind: 'invalid-response' })
    value = { receipt: { ...receipt(), runner: { ...row(), id: id(4) } }, token_available: false }
    await expect(api.execute(c, options())).rejects.toMatchObject({ kind: 'invalid-response' })
    value = {
      receipt: { ...receipt(), runner: { ...row(), version: '2' } },
      token_available: false,
    }
    await expect(api.execute(c, options())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('preserves update field presence and rejects material on update or revoke', async () => {
    let value: unknown = {
      receipt: {
        ...receipt(),
        command: 'runner.update',
        runner: { ...row(), description: 'new', version: '2' },
      },
      token_available: false,
    }
    const fetcher = vi.fn<Fetch>(async () => json(value)),
      api = createSystemRunnersAPI(fetcher)
    const c: RunnerCommand = {
      command: 'runner.update',
      target: id(3),
      request: { expected_version: '1', description: 'new' },
    }
    await api.execute(c, { csrfToken: 'S'.repeat(43), key: 'optional-signal-fixture' })
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual({
      expected_version: '1',
      description: 'new',
    })
    value = {
      ...(value as object),
      token_available: true,
      enrollment_token: 'A'.repeat(43),
      expires_at: time,
    }
    await expect(api.execute(c, options())).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
