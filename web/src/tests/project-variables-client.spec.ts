import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import {
  captureProjectVariableCommand,
  captureProjectVariableCreate,
  captureProjectVariableUpdate,
  createProjectVariablesAPI,
  newProjectVariableID,
  parseProjectVariableLookup,
  parseProjectVariablePage,
  parseProjectVariableReceipt,
  type ProjectVariableCommand,
} from '../api/project-variables'
const project = '01900000-0000-7000-8000-000000000001',
  target = '01900000-0000-7000-8000-000000000002',
  event = '01900000-0000-7000-8000-000000000003',
  audit = '01900000-0000-7000-8000-000000000004'
const at = '2026-10-09T09:00:00.000001Z'
const row = {
  id: target,
  project_id: project,
  type: 'variable',
  name: 'API_URL',
  description: '',
  version: '1',
  created_at: at,
  updated_at: at,
}
const full = { ...row, value: 'https://example.test/?a=1&b=2' }
const original = { variable_id: target, name: full.name, description: '', value: full.value }
const create: ProjectVariableCommand = { kind: 'create', projectID: project, request: original }
const update: ProjectVariableCommand = {
  kind: 'update',
  projectID: project,
  targetID: target,
  expectedVersion: '1',
  request: { value: '' },
}
const deletion: ProjectVariableCommand = {
  kind: 'delete',
  projectID: project,
  targetID: target,
  expectedVersion: '2',
}
const created = {
  command: 'project.variable.create',
  changed: true,
  variable: full,
  event_id: event,
  audit_id: audit,
}
const updated = {
  ...created,
  command: 'project.variable.update',
  variable: { ...full, value: '', version: '2' },
}
const deleted = {
  command: 'project.variable.delete',
  changed: true,
  deleted: { id: target, project_id: project, type: 'variable', version: '3', deleted_at: at },
  event_id: event,
  audit_id: audit,
}
const csrf = 'S'.repeat(43),
  key = 'variables-client-1'
const signal = () => new AbortController().signal
const response = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
describe('ordinary Project Variables closed protocol', () => {
  it('consumes six capabilities and all original lookup variants', async () => {
    const values = [
      { items: [row] },
      full,
      created,
      updated,
      deleted,
      ...[created, updated, deleted].map((receipt) => ({ status: 'committed', receipt })),
    ]
    const fetcher = vi.fn<Fetch>(async () => response(values.shift())),
      api = createProjectVariablesAPI(fetcher)
    await api.list(project, { limit: 50, cursor: 'opaque+/=字' }, signal())
    await api.get(project, target, signal())
    await api.create(project, original, csrf, key, signal())
    await api.update(project, target, '1', { value: '' }, csrf, key, signal())
    await api.delete(project, target, '2', csrf, key, signal())
    for (const c of [create, update, deletion]) await api.lookup(c, csrf, key, signal())
    const base = `/api/v1/projects/${project}/variables`
    expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
      [base + '?limit=50&cursor=opaque%2B%2F%3D%E5%AD%97', 'GET'],
      [base + '/' + target, 'GET'],
      [base, 'POST'],
      [base + '/' + target, 'PATCH'],
      [base + '/' + target, 'DELETE'],
      ...Array.from({ length: 3 }, () => [base + '/commands/lookup', 'POST']),
    ])
    expect(fetcher.mock.calls.slice(2).map(([, init]) => JSON.parse(String(init.body)))).toEqual([
      { request: original },
      { expected_version: '1', request: { value: '' } },
      { expected_version: '2' },
      { command: 'project.variable.create', request: original },
      {
        command: 'project.variable.update',
        target_id: target,
        expected_version: '1',
        request: { value: '' },
      },
      { command: 'project.variable.delete', target_id: target, expected_version: '2' },
    ])
    for (const [, init] of fetcher.mock.calls)
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
    for (const [, init] of fetcher.mock.calls.slice(2))
      expect(init.headers).toMatchObject({ 'X-CSRF-Token': csrf, 'Idempotency-Key': key })
    for (const [, init] of fetcher.mock.calls.slice(0, 2))
      expect(init.headers).not.toHaveProperty('X-CSRF-Token')
  })
  it('captures empty presence without aliasing and makes actual resource UUIDv7', () => {
    const request = { value: '', description: '' },
      c = captureProjectVariableCommand({ ...update, request })
    request.value = 'later'
    expect(c).toMatchObject({ request: { value: '', description: '' } })
    expect(Object.isFrozen(c)).toBe(true)
    const before = Date.now(),
      id = newProjectVariableID(),
      after = Date.now()
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    const time = Number.parseInt(id.replaceAll('-', '').slice(0, 12), 16)
    expect(time).toBeGreaterThanOrEqual(before)
    expect(time).toBeLessThanOrEqual(after)
    expect(id).not.toBe(newProjectVariableID())
  })
  it.each(['AGENTEAM', 'agenteam_X', ' API_URL', 'A-B', '9NAME', '字', 'a'.repeat(129)])(
    'rejects name before fetch %s',
    async (name) => {
      const fetcher = vi.fn<Fetch>()
      await expect(
        createProjectVariablesAPI(fetcher).create(
          project,
          { ...original, name },
          csrf,
          key,
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
      expect(fetcher).not.toHaveBeenCalled()
    },
  )
  it.each([
    {},
    { value: null },
    { value: undefined },
    { Value: 'x' },
    { description: '\u0080' },
    { description: '字'.repeat(1366) },
    { value: '\ud800' },
    { value: '\0' },
    { value: '字'.repeat(10923) },
  ])('rejects illegal field/presence/Unicode/bytes', (value) => {
    expect(() => captureProjectVariableUpdate(value)).toThrow()
  })
  it('accepts byte boundaries and leaves ordinary value uninterpreted', () => {
    expect(
      captureProjectVariableCreate({
        ...original,
        name: 'AGENTEAMX',
        description: '字'.repeat(1365) + '\t',
        value: '\u0001'.repeat(32768),
      }).value,
    ).toHaveLength(32768)
  })
  it.each([
    { items: [row], next_cursor: 'next' },
    { items: [{ ...row, value: 'leaked' }] },
    { items: [{ ...row, project_id: target }] },
    { items: [row, row] },
    { items: [row, { ...row, id: event, name: 'AAA' }] },
    { items: [row], next_cursor: null },
    { items: [{ ...row, updated_at: '2026-02-30T09:00:00.000001Z' }] },
  ])('rejects incomplete or corrupt whole pages', (page) => {
    expect(() => parseProjectVariablePage(page, project, { limit: 50 })).toThrow()
  })
  it('keeps opaque cursor and C order with no value in summary', () => {
    const page = parseProjectVariablePage(
      { items: [row, { ...row, id: event, name: 'a' }], next_cursor: 'opaque+/=' },
      project,
      { limit: 2 },
    )
    expect(page.next_cursor).toBe('opaque+/=')
    expect(page.items[0]).not.toHaveProperty('value')
  })
  it.each([
    { ...created, changed: false },
    { ...created, event_id: null },
    { ...created, audit_id: null },
    { ...created, variable: { ...full, id: event } },
    { ...created, variable: { ...full, project_id: target } },
    { ...created, variable: { ...full, value: 'other' } },
    { ...created, variable: { ...full, version: '2' } },
    { ...created, command: 'project.variable.update' },
    { ...created, deleted: deleted.deleted },
  ])('rejects wrong original receipt facts', (receipt) => {
    expect(() => parseProjectVariableReceipt(receipt, create)).toThrow()
  })
  it('keeps historical/no-op receipt separate from current state', () => {
    expect(
      parseProjectVariableLookup({ status: 'committed', receipt: created }, create).status,
    ).toBe('committed')
    const command = { ...update, request: { value: full.value } },
      noOp = {
        ...created,
        command: 'project.variable.update',
        changed: false,
        event_id: null,
        audit_id: null,
      }
    expect(parseProjectVariableReceipt(noOp, command).changed).toBe(false)
    expect(() => parseProjectVariableReceipt(noOp, { ...command, expectedVersion: '2' })).toThrow()
    expect(parseProjectVariableLookup({ status: 'in_progress', receipt: null }, update)).toEqual({
      status: 'in_progress',
      receipt: null,
    })
    expect(() =>
      parseProjectVariableLookup({ status: 'not_observed', receipt: created }, create),
    ).toThrow()
  })
  it.each([
    '{"items":[],"items":[]}',
    '{"items":[],"it\\u0065ms":[]}',
    JSON.stringify({ items: [row] }).replace(
      '"name":"API_URL"',
      '"name":"API_URL","na\\u006de":"API_URL"',
    ),
  ])('rejects raw duplicate and escaped member aliases', async (body) => {
    const api = createProjectVariablesAPI(
      async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    )
    await expect(api.list(project, { limit: 50 }, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('accepts valid summary above old cap without enlarging old domain', async () => {
    const items = Array.from({ length: 100 }, (_, i) => ({
        ...row,
        id: `01900000-0000-7000-8000-${(i + 100).toString(16).padStart(12, '0')}`,
        name: `VAR_${String(i).padStart(3, '0')}`,
        description: '"'.repeat(4096),
      })),
      body = JSON.stringify({ items })
    expect(new TextEncoder().encode(body).length).toBeGreaterThan(600_000)
    expect(
      (
        await createProjectVariablesAPI(
          async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
        ).list(project, { limit: 100 }, signal())
      ).items,
    ).toHaveLength(100)
    await expect(
      accountTransport(
        async () =>
          new Response(' '.repeat(600_001) + '{}', {
            headers: { 'Content-Type': 'application/json' },
          }),
      )('session', (v) => v, { signal: signal() }),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('requires EOF and joins actual cancellation before settling', async () => {
    let release!: () => void
    const cancelled = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          release = resolve
        }),
    )
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode(JSON.stringify(full)))
      },
      cancel: cancelled,
    })
    const controller = new AbortController()
    let settled = false
    const call = createProjectVariablesAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    ).get(project, target, controller.signal)
    void call.then(
      () => {
        settled = true
      },
      () => {
        settled = true
      },
    )
    for (let i = 0; i < 10; i++) await Promise.resolve()
    expect(settled).toBe(false)
    controller.abort()
    for (let i = 0; i < 10; i++) await Promise.resolve()
    expect(cancelled).toHaveBeenCalledTimes(1)
    expect(settled).toBe(false)
    release()
    await expect(call).rejects.toMatchObject({ kind: 'cancelled' })
  })
})

it('validates actual encoded Variables requests and decoded responses with the formal Draft2020-12 schema', async () => {
  const vectors: { schema: string; body: unknown; valid: boolean }[] = []
  const values = [
    { items: [row] },
    full,
    created,
    updated,
    deleted,
    ...[created, updated, deleted].map((receipt) => ({ status: 'committed', receipt })),
  ]
  const schemas = [
    'VariableSummaryPage',
    'Variable',
    'VariableMutation',
    'VariableMutation',
    'VariableMutation',
    'VariableCommandLookup',
    'VariableCommandLookup',
    'VariableCommandLookup',
  ]
  const bodies = [
    'VariableCreateBody',
    'VariableUpdateBody',
    'VariableDeleteBody',
    'VariableLookupBody',
    'VariableLookupBody',
    'VariableLookupBody',
  ]
  let responseIndex = 0,
    requestIndex = 0
  const api = createProjectVariablesAPI(async (_path, init) => {
    if (init.body)
      vectors.push({
        schema: bodies[requestIndex++]!,
        body: JSON.parse(String(init.body)),
        valid: true,
      })
    const value = values[responseIndex]
    vectors.push({
      schema: schemas[responseIndex++]!,
      body: JSON.parse(JSON.stringify(value)),
      valid: true,
    })
    return response(value)
  })
  await api.list(project, { limit: 50 }, signal())
  await api.get(project, target, signal())
  await api.create(project, original, csrf, key, signal())
  await api.update(project, target, '1', { value: '' }, csrf, key, signal())
  await api.delete(project, target, '2', csrf, key, signal())
  for (const command of [create, update, deletion]) await api.lookup(command, csrf, key, signal())
  for (const [schema, body] of [
    ['VariableCreateBody', { request: original, expected_version: '1' }],
    ['VariableDeleteBody', { expected_version: '2', request: {} }],
    ['VariableUpdateBody', { expected_version: '1', request: { value: null } }],
    ['VariableSummaryPage', { items: [full] }],
    ['VariableCommandLookup', { status: 'committed', receipt: null }],
    ['VariableCommandLookup', { status: 'not_observed', receipt: created }],
  ] as const)
    vectors.push({ schema, body, valid: false })
  const python =
    process.env.AGENTEAM_PROJECT_VARIABLE_SCHEMA_PYTHON ??
    '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3'
  const result = spawnSync(
    python,
    [
      '-c',
      `
import sys,json,pathlib
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
root=pathlib.Path(sys.argv[1]); base=(root/'project-variables.json').as_uri()
registry=Registry()
for name in ['project-variables.json','common.json']:
 doc=json.loads((root/name).read_bytes())
 registry=registry.with_resource((root/name).as_uri(),Resource.from_contents(doc,default_specification=DRAFT202012))
for n,vector in enumerate(json.load(sys.stdin)):
 valid=Draft202012Validator({'$ref':base+'#/components/schemas/'+vector['schema']},registry=registry,format_checker=FormatChecker()).is_valid(vector['body'])
 if valid!=vector['valid']:raise SystemExit('schema vector '+str(n)+' disagrees')
print('formal schema vectors accepted')
`,
      resolve(process.cwd(), '../api/openapi'),
    ],
    { input: JSON.stringify(vectors), encoding: 'utf8', timeout: 20000, maxBuffer: 1024 * 1024 },
  )
  expect(result.error).toBeUndefined()
  expect(result.status, result.stderr).toBe(0)
  expect(vectors).toHaveLength(20)
})
