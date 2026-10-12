import { describe, expect, it, vi } from 'vitest'
import { type Fetch } from '../api/client'
import {
  captureProjectVariableCommand,
  captureProjectVariableUpdate,
  createProjectVariablesAPI,
} from '../api/project-variables'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  v = id(2),
  at = '2026-10-11T10:00:00.000001Z',
  signal = new AbortController().signal,
  csrf = 'S'.repeat(43),
  key = 'ordinary-variable-original/1'
const fields = {
  name: 'BROWSER_ORDINARY',
  description: 'Description',
  value: '  complete\nvalue  ',
}
const variable = (version = '1') => ({
  id: v,
  project_id: p,
  type: 'variable',
  ...fields,
  version,
  created_at: at,
  updated_at: at,
})
const receipt = (command = 'project.variable.create', version = '1', changed = true) => ({
  command,
  changed,
  variable: variable(version),
  event_id: changed ? id(3) : null,
  audit_id: changed ? id(4) : null,
})
const json = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
describe('ordinary Project Variable closed client', () => {
  it('deletes with the original version and binds Lookup to the same tombstone without a request payload', async () => {
    const expected = '9007199254740993'
    const deleted = {
      command: 'project.variable.delete',
      changed: true,
      deleted: {
        id: v,
        project_id: p,
        type: 'variable',
        version: '9007199254740994',
        deleted_at: at,
      },
      event_id: id(3),
      audit_id: id(4),
    }
    const fetcher = vi
      .fn<Fetch>()
      .mockResolvedValueOnce(json(deleted))
      .mockResolvedValueOnce(json({ status: 'committed', receipt: deleted }))
    const api = createProjectVariablesAPI(fetcher)
    const result = await api.delete(p, v, expected, csrf, key, signal)
    expect(result).toEqual(deleted)
    expect(Object.isFrozen(result.deleted)).toBe(true)
    const original = captureProjectVariableCommand({
      kind: 'delete',
      projectID: p,
      targetID: v,
      expectedVersion: expected,
    })
    expect(await api.lookup(original, csrf, key, signal)).toEqual({
      status: 'committed',
      receipt: deleted,
    })
    expect(
      fetcher.mock.calls.map(([path, init]) => [
        path,
        init.method,
        JSON.parse(init.body as string),
      ]),
    ).toEqual([
      [`/api/v1/projects/${p}/variables/${v}`, 'DELETE', { expected_version: expected }],
      [
        `/api/v1/projects/${p}/variables/commands/lookup`,
        'POST',
        { command: 'project.variable.delete', target_id: v, expected_version: expected },
      ],
    ])
    for (const [, init] of fetcher.mock.calls)
      expect(init.headers).toMatchObject({ 'Idempotency-Key': key, 'X-CSRF-Token': csrf })
  })
  it('rejects incompatible delete tombstones, including foreign identity, stale version and mixed receipt shapes', async () => {
    const deleted = { id: v, project_id: p, type: 'variable', version: '2', deleted_at: at }
    const good = {
      command: 'project.variable.delete',
      changed: true,
      deleted,
      event_id: id(3),
      audit_id: id(4),
    }
    const fetcher = vi.fn<Fetch>(),
      api = createProjectVariablesAPI(fetcher)
    for (const bad of [
      { ...good, deleted: { ...deleted, id: id(99) } },
      { ...good, deleted: { ...deleted, project_id: id(99) } },
      { ...good, deleted: { ...deleted, version: '1' } },
      { ...good, deleted: { ...deleted, value: '' } },
      { ...good, variable: variable() },
      { ...good, event_id: null },
      { ...good, changed: false },
    ]) {
      fetcher.mockResolvedValueOnce(json(bad))
      await expect(api.delete(p, v, '1', csrf, key, signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    const count = fetcher.mock.calls.length
    for (const version of ['', '0', '01', '9223372036854775808'])
      await expect(api.delete(p, v, version, csrf, key, signal)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    expect(fetcher).toHaveBeenCalledTimes(count)
    expect(() =>
      captureProjectVariableCommand({
        kind: 'delete',
        projectID: p,
        targetID: v,
        expectedVersion: '1',
        request: {},
      }),
    ).toThrow()
  })
  it('creates complete text and separates the eight-field directory from the nine-field detail', async () => {
    const { value: _value, ...summary } = variable()
    const fetcher = vi
      .fn<Fetch>()
      .mockResolvedValueOnce(json(receipt()))
      .mockResolvedValueOnce(json({ items: [summary] }))
      .mockResolvedValueOnce(json(variable()))
    const api = createProjectVariablesAPI(fetcher)
    const result = await api.create(p, { variable_id: v, ...fields }, csrf, key, signal)
    expect(result).toEqual(receipt())
    expect(Object.isFrozen(result.variable)).toBe(true)
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual({
      request: { variable_id: v, ...fields },
    })
    expect(fetcher.mock.calls[0]![1]).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      redirect: 'error',
      cache: 'no-store',
      signal,
    })
    expect(fetcher.mock.calls[0]![1].headers).toMatchObject({
      'Idempotency-Key': key,
      'X-CSRF-Token': csrf,
    })
    const page = await api.list(p, { limit: 50 }, signal)
    expect(Object.keys(page.items[0]!)).toHaveLength(8)
    expect(Object.hasOwn(page.items[0]!, 'value')).toBe(false)
    expect(fetcher.mock.calls[1]![0]).toBe(`/api/v1/projects/${p}/variables?limit=50`)
    expect(await api.get(p, v, signal)).toEqual(variable())
    expect(fetcher.mock.calls[2]![0]).toBe(`/api/v1/projects/${p}/variables/${v}`)
  })
  it('preserves explicit clear and a large original version through changed, noop and exact lookup', async () => {
    const expected = '9007199254740993',
      input = captureProjectVariableUpdate({ description: '', value: '' })
    expect(Object.keys(input)).toEqual(['description', 'value'])
    const changed = {
      ...receipt('project.variable.update', '9007199254740994'),
      variable: { ...variable('9007199254740994'), description: '', value: '' },
    }
    const noop = {
      ...changed,
      changed: false,
      variable: { ...changed.variable, version: expected },
      event_id: null,
      audit_id: null,
    }
    const fetcher = vi
      .fn<Fetch>()
      .mockResolvedValueOnce(json(changed))
      .mockResolvedValueOnce(json(noop))
      .mockResolvedValueOnce(json({ status: 'committed', receipt: changed }))
    const api = createProjectVariablesAPI(fetcher)
    expect(await api.update(p, v, expected, input, csrf, key, signal)).toEqual(changed)
    expect(await api.update(p, v, expected, input, csrf, key, signal)).toEqual(noop)
    const original = captureProjectVariableCommand({
      kind: 'update',
      projectID: p,
      targetID: v,
      expectedVersion: expected,
      request: input,
    })
    expect(original.kind === 'update' && Object.isFrozen(original.request)).toBe(true)
    expect(await api.lookup(original, csrf, key, signal)).toEqual({
      status: 'committed',
      receipt: changed,
    })
    expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      [`/api/v1/projects/${p}/variables/${v}`, 'PATCH'],
      [`/api/v1/projects/${p}/variables/${v}`, 'PATCH'],
      [`/api/v1/projects/${p}/variables/commands/lookup`, 'POST'],
    ])
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual({
      expected_version: expected,
      request: input,
    })
    expect(JSON.parse(fetcher.mock.calls[2]![1].body as string)).toEqual({
      command: 'project.variable.update',
      target_id: v,
      expected_version: expected,
      request: input,
    })
    expect(fetcher.mock.calls[2]![1].headers).toMatchObject({ 'Idempotency-Key': key })
  })
  it('rejects null, absent patches and reserved names before network without changing explicit empty values', async () => {
    const fetcher = vi.fn<Fetch>(),
      api = createProjectVariablesAPI(fetcher)
    for (const input of [
      {},
      { value: null },
      { value: undefined },
      { secret: true },
      { name: 'AgEnTeAm_token' },
      { value: '\0' },
    ])
      await expect(api.update(p, v, '1', input as never, csrf, key, signal)).rejects.toMatchObject({
        kind: 'invalid-input',
      })
    await expect(
      api.create(
        p,
        { variable_id: v, name: 'BAD-NAME', description: '', value: '' },
        csrf,
        key,
        signal,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(api.list(p, { limit: 101 }, signal)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetcher).not.toHaveBeenCalled()
    expect(captureProjectVariableUpdate({ value: '' })).toEqual({ value: '' })
  })
  it('rejects raw duplicates, disclosure in summaries and incompatible successful receipts as whole responses', async () => {
    const fetcher = vi.fn<Fetch>(),
      api = createProjectVariablesAPI(fetcher)
    fetcher.mockResolvedValueOnce(
      new Response(
        JSON.stringify(variable()).replace('"value":', '"value":"overwritten","value":'),
        { headers: { 'Content-Type': 'application/json' } },
      ),
    )
    await expect(api.get(p, v, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    fetcher.mockResolvedValueOnce(json({ items: [variable()] }))
    await expect(api.list(p, { limit: 50 }, signal)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    for (const bad of [
      { ...receipt(), audit_id: null },
      { ...receipt(), variable: { ...variable(), project_id: id(99) } },
      {
        ...receipt('project.variable.update', '2'),
        variable: { ...variable('2'), value: 'ignored clear' },
      },
    ]) {
      fetcher.mockResolvedValueOnce(json(bad))
      await expect(api.update(p, v, '1', { value: '' }, csrf, key, signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    expect(fetcher).toHaveBeenCalledTimes(5)
  })
  it('keeps an opaque cursor and exposes its original Problem without silently starting a fresh directory', async () => {
    const token = 'opaque+cursor/with=',
      fetcher = vi.fn<Fetch>(
        async () =>
          new Response(
            JSON.stringify({
              type: 'urn:agenteam:problem:cursor',
              title: 'Rejected',
              detail: '',
              status: 409,
              instance: `/api/v1/projects/${p}/variables`,
              code: 'CURSOR_STALE',
              request_id: id(90),
              commit_state: 'not_started',
            }),
            {
              status: 409,
              headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
            },
          ),
      )
    await expect(
      createProjectVariablesAPI(fetcher).list(p, { limit: 50, cursor: token }, signal),
    ).rejects.toMatchObject({ problem: { code: 'CURSOR_STALE' } })
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(
      new URL(fetcher.mock.calls[0]![0], 'https://owner.test').searchParams.get('cursor'),
    ).toBe(token)
  })
})
