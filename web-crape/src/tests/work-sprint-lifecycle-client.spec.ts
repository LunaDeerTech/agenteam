import { describe, expect, it, vi } from 'vitest'
import {
  createWorkSprintLifecycleAPI,
  captureSprintStartInput,
  captureSprintCreateInput,
  captureSprintCreateDraft,
} from '../api/work-sprint-lifecycle'
import type { Fetch } from '../api/client'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  s = id(2),
  owner = id(3),
  at = '2026-10-11T10:00:00.000001Z'
const startPath = `/api/v1/projects/${p}/sprints/${s}/start`
const lookupPath = `/api/v1/projects/${p}/sprint-lifecycle-commands/lookup`
const original = { expected_version: '9007199254740993', request: {} }
const write = { csrfToken: 'S'.repeat(43), key: 'K'.repeat(32) }
function receipt() {
  return {
    sprint: {
      id: s,
      project_id: p,
      milestone_id: id(4),
      title: 'Sprint title',
      description: 'Original\ntext',
      manual_rank: '8'.repeat(32),
      version: '9007199254740994',
      created_at: at,
      updated_at: at,
      state: 'current',
      started_at: at,
      started_by: { kind: 'human', user_id: owner },
      completed_at: null,
      completed_by: null,
    },
    project: {
      id: p,
      owner_user_id: owner,
      name: 'Project-One',
      normalized_name: 'project-one',
      description: '',
      lifecycle: 'active',
      version: '2',
      current_sprint_id: s,
      created_at: at,
      updated_at: at,
    },
    event_id: id(5),
  }
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })

describe('Sprint Start wire contract', () => {
  it('captures the explicit empty request and decodes the complete immutable original Start receipt', async () => {
    const fetch = vi.fn<Fetch>(async () => json(receipt()))
    const api = createWorkSprintLifecycleAPI(fetch)
    const captured = captureSprintStartInput(original)
    expect(captured).toEqual(original)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const result = await api.start(p, s, captured, write)
    expect(result).toEqual(receipt())
    expect(Object.isFrozen(result.sprint.started_by)).toBe(true)
    expect(Object.isFrozen(result.project)).toBe(true)
    expect(result.sprint.version).toBe('9007199254740994')
    expect(fetch).toHaveBeenCalledTimes(1)
    const [path, init] = fetch.mock.calls[0]!
    expect(path).toBe(startPath)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual(original)
    expect(init.headers).toMatchObject({
      'Idempotency-Key': write.key,
      'X-CSRF-Token': write.csrfToken,
    })
    expect(init.credentials).toBe('same-origin')
    expect(Object.hasOwn(result.project, 'scheduler_enabled')).toBe(false)
  })

  it('uses only the original key and complete Start identity for every Lookup outcome', async () => {
    const fetch = vi.fn<Fetch>()
    const api = createWorkSprintLifecycleAPI(fetch)
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      const expected = { status, receipt: status === 'committed' ? receipt() : null }
      fetch.mockResolvedValueOnce(json(expected))
      expect(await api.lookup(p, s, original, write)).toEqual(expected)
      const [path, init] = fetch.mock.calls.at(-1)!
      expect(path).toBe(lookupPath)
      expect(init.method).toBe('POST')
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.sprint.start',
        target_id: s,
        ...original,
      })
    }
    expect(fetch).toHaveBeenCalledTimes(3)
  })

  it('rejects absent or nonempty requests before transport and rejects inconsistent or duplicate successful receipts', async () => {
    const fetch = vi.fn<Fetch>()
    const api = createWorkSprintLifecycleAPI(fetch)
    for (const value of [
      { expected_version: '1' },
      { expected_version: '1', request: null },
      { expected_version: 1, request: {} },
      { expected_version: '1', request: { enable: true } },
    ]) {
      expect(() => captureSprintStartInput(value)).toThrowError('invalid-input')
    }
    expect(() => api.start(id(1).replace('-7000-', '-4000-'), s, original, write)).toThrowError(
      'invalid-input',
    )
    expect(fetch).not.toHaveBeenCalled()
    const broken = [
      { ...receipt(), project: { ...receipt().project, current_sprint_id: id(9) } },
      { ...receipt(), sprint: { ...receipt().sprint, version: original.expected_version } },
      {
        ...receipt(),
        sprint: { ...receipt().sprint, started_by: { kind: 'human', user_id: id(9) } },
      },
      {
        ...receipt(),
        project: { ...receipt().project, updated_at: '2026-10-11T10:00:00.000002Z' },
      },
    ]
    for (const value of broken) {
      fetch.mockResolvedValueOnce(json(value))
      await expect(api.start(p, s, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    fetch.mockResolvedValueOnce(
      new Response(
        `{"status":"not_observed","status":"committed","receipt":${JSON.stringify(receipt())}}`,
        { headers: { 'Content-Type': 'application/json' } },
      ),
    )
    await expect(api.lookup(p, s, original, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(fetch).toHaveBeenCalledTimes(5)
  })
})

const createInput = { request: { sprint_id: s, milestone_id: id(4), title: 'Planned Sprint' } }
function created() {
  return {
    command: 'work.sprint.create',
    changed: true,
    milestone: null,
    sprint: {
      ...receipt().sprint,
      title: createInput.request.title,
      description: '',
      version: '1',
      state: 'planned',
      started_at: null,
      started_by: null,
    },
    event_id: id(6),
  }
}
describe('Sprint Create wire contract', () => {
  it('preserves optional description and accepts only the complete original planned Sprint receipt', async () => {
    const fetch = vi.fn<Fetch>(async () => json(created()))
    const api = createWorkSprintLifecycleAPI(fetch)
    const captured = captureSprintCreateInput(createInput)
    expect(Object.hasOwn(captured.request, 'description')).toBe(false)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const result = await api.create(p, captured, write)
    expect(result).toEqual(created())
    expect(Object.isFrozen(result.sprint)).toBe(true)
    const [path, init] = fetch.mock.calls[0]!
    expect(path).toBe(`/api/v1/projects/${p}/sprints`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual(createInput)
    expect(init.headers).toMatchObject({
      'Idempotency-Key': write.key,
      'X-CSRF-Token': write.csrfToken,
    })
    expect(
      captureSprintCreateDraft({ milestone_id: id(4), title: 'Planned Sprint', description: '' }),
    ).toEqual({ milestone_id: id(4), title: 'Planned Sprint', description: '' })
  })
  it('looks up the original create body and key using structure state/result without target or version', async () => {
    const fetch = vi.fn<Fetch>()
    const api = createWorkSprintLifecycleAPI(fetch)
    const original = captureSprintCreateInput({
      request: { ...createInput.request, description: '' },
    })
    for (const state of ['not_observed', 'in_progress', 'committed'] as const) {
      const result = { state, result: state === 'committed' ? created() : null }
      fetch.mockResolvedValueOnce(json(result))
      expect(await api.lookupCreate(p, original, write)).toEqual(result)
      const [path, init] = fetch.mock.calls.at(-1)!
      expect(path).toBe(`/api/v1/projects/${p}/structure-commands/lookup`)
      expect(init.method).toBe('POST')
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.sprint.create',
        ...original,
      })
    }
    expect(fetch).toHaveBeenCalledTimes(3)
  })
  it('rejects invalid presence before dispatch and rejects wrong scope, state, version or duplicate structure receipts', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintLifecycleAPI(fetch)
    for (const value of [
      { ...createInput, expected_version: '1' },
      { request: { ...createInput.request, description: null } },
      { request: { ...createInput.request, description: undefined } },
      { request: { ...createInput.request, sprint_id: s.replace('-7000-', '-4000-') } },
      { request: { ...createInput.request, title: '  ' } },
    ])
      expect(() => api.create(p, value as typeof createInput, write)).toThrowError('invalid-input')
    expect(fetch).not.toHaveBeenCalled()
    for (const value of [
      { ...created(), sprint: { ...created().sprint, milestone_id: id(30) } },
      { ...created(), sprint: { ...created().sprint, state: 'current' } },
      { ...created(), sprint: { ...created().sprint, version: '2' } },
      { ...created(), changed: false, event_id: null },
    ]) {
      fetch.mockResolvedValueOnce(json(value))
      await expect(api.create(p, createInput, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    fetch.mockResolvedValueOnce(
      new Response('{"state":"not_observed","state":"committed","result":null}', {
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await expect(api.lookupCreate(p, createInput, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
    expect(fetch).toHaveBeenCalledTimes(5)
  })
})

function completedReceipt() {
  const started = receipt()
  return {
    ...started,
    sprint: {
      ...started.sprint,
      state: 'completed',
      started_by: { kind: 'human', user_id: id(30) },
      completed_at: at,
      completed_by: { kind: 'human', user_id: owner },
    },
    project: { ...started.project, current_sprint_id: null },
  }
}
describe('Sprint Complete full receipt and original lookup', () => {
  it('keeps historical actor, requires the complete DTO and sends the empty request with original key', async () => {
    const fetch = vi.fn<Fetch>(async () => json(completedReceipt()))
    const api = createWorkSprintLifecycleAPI(fetch)
    expect(await api.complete(p, s, original, write)).toEqual(completedReceipt())
    expect(fetch.mock.calls[0]![0]).toBe(`/api/v1/projects/${p}/sprints/${s}/complete`)
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(original)
    expect(fetch.mock.calls[0]![1].headers).toMatchObject({ 'Idempotency-Key': write.key })
    for (const status of ['in_progress', 'not_observed', 'committed'] as const) {
      fetch.mockImplementationOnce(async () =>
        json({ status, receipt: status === 'committed' ? completedReceipt() : null }),
      )
      expect(await api.lookupComplete(p, s, original, write)).toMatchObject({ status })
      const [, init] = fetch.mock.calls.at(-1)!
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.sprint.complete',
        target_id: s,
        ...original,
      })
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
    }
  })
  it.each([
    [
      'non-null current pointer',
      (v: ReturnType<typeof completedReceipt>) => {
        v.project.current_sprint_id = s as never
      },
    ],
    [
      'wrong completion owner',
      (v: ReturnType<typeof completedReceipt>) => {
        v.sprint.completed_by.user_id = id(31)
      },
    ],
    [
      'missing full Sprint field',
      (v: ReturnType<typeof completedReceipt>) => {
        delete (v.sprint as Partial<typeof v.sprint>).description
      },
    ],
    [
      'extra rollover material',
      (v: ReturnType<typeof completedReceipt>) => {
        Object.assign(v, { rollover: {} })
      },
    ],
    [
      'wrong version',
      (v: ReturnType<typeof completedReceipt>) => {
        v.sprint.version = original.expected_version
      },
    ],
    [
      'completion time diverges',
      (v: ReturnType<typeof completedReceipt>) => {
        v.sprint.completed_at = '2026-10-11T10:00:01.000001Z'
      },
    ],
    [
      'completion before start',
      (v: ReturnType<typeof completedReceipt>) => {
        v.sprint.started_at = '2026-10-11T10:00:01.000001Z'
      },
    ],
    [
      'cross-project historical agent',
      (v: ReturnType<typeof completedReceipt>) => {
        Object.assign(v.sprint, {
          started_by: {
            kind: 'agent_run',
            project_id: id(50),
            agent_id: id(51),
            execution_id: id(52),
          },
        })
      },
    ],
    [
      'wrong Sprint identity',
      (v: ReturnType<typeof completedReceipt>) => {
        v.sprint.id = id(60)
      },
    ],
  ])('rejects %s atomically', async (_label, mutate) => {
    const invalid = completedReceipt()
    mutate(invalid)
    const api = createWorkSprintLifecycleAPI(async () => json(invalid))
    await expect(api.complete(p, s, original, write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('accepts a same-project historical Agent without treating it as the completing human', async () => {
    const value = completedReceipt()
    const actor = { kind: 'agent_run', project_id: p, agent_id: id(51), execution_id: id(52) }
    Object.assign(value.sprint, { started_by: actor })
    const result = await createWorkSprintLifecycleAPI(async () => json(value)).complete(
      p,
      s,
      original,
      write,
    )
    expect(result.sprint.started_by).toEqual(actor)
    expect(result.sprint.completed_by.user_id).toBe(owner)
  })
})
