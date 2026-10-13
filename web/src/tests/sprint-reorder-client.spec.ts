import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureSprintReorderInput,
  createWorkSprintPlanningAPI,
  type SprintReorderInput,
} from '../api/work-sprint-planning'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(2),
  anchor = id(4),
  parent = id(5),
  at = '2026-10-13T10:00:00.000001Z'
const write = { csrfToken: 'C'.repeat(43), key: 'K'.repeat(32) }
const original = {
  expected_version: '9007199254740993',
  request: { milestone_id: parent, before_id: anchor },
}
function receipt(input: SprintReorderInput = original, changed = true) {
  return {
    command: 'work.sprint.reorder',
    changed,
    milestone: null,
    event_id: changed ? id(3) : null,
    sprint: {
      id: target,
      project_id: project,
      milestone_id: parent,
      state: 'planned',
      started_at: null,
      started_by: null,
      completed_at: null,
      completed_by: null,
      title: 'Sprint',
      description: 'Original description',
      manual_rank: '8'.repeat(32),
      version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
      created_at: at,
      updated_at: at,
    },
  }
}
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
  })
describe('Sprint reorder wire contract', () => {
  it('freezes a before anchor and posts exact original version; tail keeps the original parent and omits only the anchor', async () => {
    const draft = { expected_version: original.expected_version, request: { ...original.request } }
    const captured = captureSprintReorderInput(draft, target)
    draft.request.before_id = id(7)
    expect(captured).toEqual(original)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const fetch = vi.fn<Fetch>(async () => json(receipt()))
    const api = createWorkSprintPlanningAPI(fetch)
    expect(await api.reorder(project, target, captured, write)).toEqual(receipt())
    expect(fetch.mock.calls[0]![0]).toBe(`/api/v1/projects/${project}/sprints/${target}/reorder`)
    expect(fetch.mock.calls[0]![1].method).toBe('POST')
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(original)
    const tail = { expected_version: original.expected_version, request: { milestone_id: parent } }
    await api.reorder(project, target, tail, write)
    expect(JSON.parse(fetch.mock.calls[1]![1].body as string)).toEqual(tail)
  })
  it('rejects null, self, rank, unknown fields and malformed versions before fetch', () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintPlanningAPI(fetch)
    for (const request of [
      { milestone_id: null },
      { milestone_id: undefined },
      { milestone_id: 'bad' },
      { before_id: null },
      { before_id: target },
      { before_id: 'bad' },
      { before_id: undefined },
      { manual_rank: '8'.repeat(32) },
      { Before_id: anchor },
      { project_id: project },
      { before_id: anchor, title: 'extra' },
    ])
      expect(() =>
        api.reorder(
          project,
          target,
          {
            expected_version: '1',
            request: { milestone_id: parent, ...request },
          } as unknown as SprintReorderInput,
          write,
        ),
      ).toThrowError(AccountFailure)
    for (const expected_version of ['01', '0', '9223372036854775808', 1])
      expect(() =>
        captureSprintReorderInput({ expected_version, request: { milestone_id: parent } }),
      ).toThrowError(AccountFailure)
    expect(() => captureSprintReorderInput({ expected_version: '1', request: {} })).toThrowError(
      AccountFailure,
    )
    expect(fetch).not.toHaveBeenCalled()
  })
  it('accepts MaxInt64 no-op and rejects wrong receipt identity, version, event or shape', async () => {
    const max = { expected_version: '9223372036854775807', request: { milestone_id: parent } }
    const fetch = vi.fn<Fetch>().mockResolvedValueOnce(json(receipt(max, false))),
      api = createWorkSprintPlanningAPI(fetch)
    expect(await api.reorder(project, target, max, write)).toEqual(receipt(max, false))
    const good = receipt()
    for (const bad of [
      { ...good, command: 'work.sprint.update' },
      { ...good, sprint: {} },
      { ...good, event_id: null },
      { ...good, changed: false },
      { ...good, sprint: { ...good.sprint, milestone_id: id(8) } },
      { ...good, sprint: { ...good.sprint, state: 'completed' } },
      { ...good, sprint: { ...good.sprint, started_by: {} } },
      { ...good, sprint: { ...good.sprint, id: anchor } },
      { ...good, sprint: { ...good.sprint, project_id: id(8) } },
      { ...good, sprint: { ...good.sprint, version: original.expected_version } },
      { ...good, sprint: { ...good.sprint, before_id: anchor } },
      { ...good, sprint: { ...good.sprint, manual_rank: '0'.repeat(32) } },
      { ...receipt(original, false), event_id: id(3) },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.reorder(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('accepts a current Sprint historical receipt and validates lifecycle identity without a current Get', async () => {
    const good = receipt()
    const current = {
      ...good,
      sprint: {
        ...good.sprint,
        state: 'current',
        started_at: at,
        started_by: { kind: 'human', user_id: id(10) },
      },
    }
    const fetch = vi.fn<Fetch>().mockResolvedValueOnce(json(current))
    const api = createWorkSprintPlanningAPI(fetch)
    expect(await api.reorder(project, target, original, write)).toEqual(current)
    expect(fetch).toHaveBeenCalledTimes(1)
    for (const bad of [
      { ...current, milestone: {} },
      { ...current, sprint: { ...current.sprint, started_at: null } },
      {
        ...current,
        sprint: { ...current.sprint, completed_by: { kind: 'human', user_id: id(10) } },
      },
      {
        ...current,
        sprint: {
          ...current.sprint,
          started_by: {
            kind: 'agent_run',
            project_id: id(11),
            agent_id: id(12),
            execution_id: id(13),
          },
        },
      },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.reorder(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('uses exact Structure Lookup state/result with the original target, version, anchor and key', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintPlanningAPI(fetch)
    for (const state of ['not_observed', 'in_progress', 'committed'] as const) {
      const result = state === 'committed' ? receipt() : null
      fetch.mockResolvedValueOnce(json({ state, result }))
      expect(await api.lookupReorder(project, target, original, write)).toEqual({ state, result })
    }
    for (const [path, init] of fetch.mock.calls) {
      expect(path).toBe(`/api/v1/projects/${project}/structure-commands/lookup`)
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.sprint.reorder',
        target_id: target,
        ...original,
      })
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
    }
    for (const bad of [
      { status: 'committed', receipt: receipt() },
      { state: 'in_progress' },
      { state: 'not_observed', result: receipt() },
      { state: 'committed', result: null },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.lookupReorder(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
})
