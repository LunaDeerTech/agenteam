import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureSprintUpdateInput,
  createWorkSprintPlanningAPI,
  type SprintUpdateInput,
} from '../api/work-sprint-planning'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(2),
  at = '2026-10-11T10:00:00.000001Z'
const write = { csrfToken: 'C'.repeat(43), key: 'K'.repeat(32) }
const original = { expected_version: '9007199254740993', request: { title: '  e\u0301 项目😀  ' } }
function receipt(input: SprintUpdateInput = original, changed = true) {
  return {
    command: 'work.sprint.update',
    changed,
    milestone: null,
    event_id: changed ? id(3) : null,
    sprint: {
      id: target,
      project_id: project,
      milestone_id: id(10),
      state: 'planned',
      started_at: null,
      started_by: null,
      completed_at: null,
      completed_by: null,
      title: 'Old',
      description: 'Unchanged',
      ...input.request,
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
describe('Sprint edit wire contract', () => {
  it('preserves original bytes, exact presence, large versions and explicit empty description', async () => {
    const draft = { expected_version: original.expected_version, request: { ...original.request } }
    const captured = captureSprintUpdateInput(draft)
    draft.request.title = 'Later'
    expect(captured).toEqual(original)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const fetch = vi.fn<Fetch>(async () => json(receipt()))
    const result = await createWorkSprintPlanningAPI(fetch).update(project, target, captured, write)
    expect(result).toEqual(receipt())
    expect(fetch.mock.calls[0]![0]).toBe(`/api/v1/projects/${project}/sprints/${target}`)
    expect(fetch.mock.calls[0]![1].method).toBe('PATCH')
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(original)
    expect(
      captureSprintUpdateInput({ expected_version: '1', request: { description: '' } }).request,
    ).toEqual({ description: '' })
  })
  it('rejects invalid title, null, empty patch, unknown fields and malformed version before fetch', () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintPlanningAPI(fetch)
    for (const request of [
      {},
      { title: null },
      { description: null },
      { title: '' },
      { title: ' \n' },
      { title: 'x\u007f' },
      { title: '\ud800' },
      { title: '😀'.repeat(257) },
      { description: 'x'.repeat(32769) },
      { Title: 'Wrong' },
      { sprint_id: target, title: 'Wrong' },
      { milestone_id: id(10), title: 'Wrong' },
      { state: 'current', title: 'Wrong' },
      { current_sprint_id: target, title: 'Wrong' },
      { manual_rank: '8'.repeat(32), title: 'Wrong' },
    ])
      expect(() =>
        api.update(
          project,
          target,
          { expected_version: '1', request } as unknown as SprintUpdateInput,
          write,
        ),
      ).toThrowError(AccountFailure)
    for (const version of ['01', '0', '9223372036854775808', 1])
      expect(() =>
        captureSprintUpdateInput({ expected_version: version, request: { title: 'x' } }),
      ).toThrowError(AccountFailure)
    expect(fetch).not.toHaveBeenCalled()
  })
  it('accepts a MaxInt64 no-op and requires precise changed/version/event and original fields', async () => {
    const max = { expected_version: '9223372036854775807', request: { description: '' } }
    const fetch = vi.fn<Fetch>().mockResolvedValueOnce(json(receipt(max, false))),
      api = createWorkSprintPlanningAPI(fetch)
    expect(await api.update(project, target, max, write)).toEqual(receipt(max, false))
    const good = receipt()
    for (const bad of [
      { ...good, command: 'work.sprint.create' },
      { ...good, milestone: {} },
      { ...good, event_id: null },
      { ...good, changed: false },
      { ...good, sprint: { ...good.sprint, id: id(8) } },
      { ...good, sprint: { ...good.sprint, project_id: id(8) } },
      { ...good, sprint: { ...good.sprint, title: 'Trimmed' } },
      { ...good, sprint: { ...good.sprint, version: original.expected_version } },
      { ...receipt(max, false), event_id: id(3) },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.update(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('validates planned/current lifecycle and every historical actor union, never treating the receipt as current state', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintPlanningAPI(fetch)
    const good = receipt()
    for (const actor of [
      { kind: 'human', user_id: id(12) },
      { kind: 'agent_run', project_id: project, agent_id: id(13), execution_id: id(14) },
      { kind: 'service', project_id: project, service_name: 'scheduler', cause_ref: id(15) },
      {
        kind: 'service',
        project_id: project,
        service_name: 'outbox-delivery',
        cause_ref: 'sha256:' + 'a'.repeat(64),
      },
    ]) {
      const current = {
        ...good,
        sprint: { ...good.sprint, state: 'current', started_at: at, started_by: actor },
      }
      fetch.mockResolvedValueOnce(json({ state: 'committed', result: current }))
      expect(await api.lookupUpdate(project, target, original, write)).toEqual({
        state: 'committed',
        result: current,
      })
    }
    for (const change of [
      {
        state: 'completed',
        started_at: at,
        started_by: { kind: 'human', user_id: id(12) },
        completed_at: at,
        completed_by: { kind: 'human', user_id: id(12) },
      },
      ...[null, id(18)].map((project_id) => ({
        state: 'current',
        started_at: at,
        started_by: {
          kind: 'service',
          project_id,
          service_name: 'outbox-delivery',
          cause_ref: 'sha256:' + 'a'.repeat(64),
        },
      })),
      { state: 'current' },
      { started_at: at, started_by: { kind: 'human', user_id: id(12) } },
      {
        state: 'current',
        started_at: at,
        started_by: {
          kind: 'agent_run',
          project_id: id(18),
          agent_id: id(13),
          execution_id: id(14),
        },
      },
      {
        state: 'current',
        started_at: at,
        started_by: { kind: 'human', user_id: id(12), session_id: id(20) },
      },
      {
        state: 'current',
        started_at: at,
        started_by: {
          kind: 'service',
          project_id: project,
          service_name: 'invented',
          cause_ref: id(15),
        },
      },
      {
        state: 'current',
        started_at: at,
        started_by: {
          kind: 'service',
          project_id: project,
          service_name: 'scheduler',
          cause_ref: 'not-an-id-or-digest',
        },
      },
      {
        state: 'current',
        started_at: '2026-10-12T10:00:00.000001Z',
        started_by: { kind: 'human', user_id: id(12) },
      },
      { completed_by: { kind: 'human', user_id: id(12) } },
    ]) {
      fetch.mockResolvedValueOnce(json({ ...good, sprint: { ...good.sprint, ...change } }))
      await expect(api.update(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('uses Structure state/result and the original target, version, patch and key for all Lookup observations', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkSprintPlanningAPI(fetch)
    for (const state of ['not_observed', 'in_progress', 'committed'] as const) {
      fetch.mockResolvedValueOnce(json({ state, result: state === 'committed' ? receipt() : null }))
      expect(await api.lookupUpdate(project, target, original, write)).toEqual({
        state,
        result: state === 'committed' ? receipt() : null,
      })
    }
    for (const [path, init] of fetch.mock.calls) {
      expect(path).toBe(`/api/v1/projects/${project}/structure-commands/lookup`)
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.sprint.update',
        target_id: target,
        ...original,
      })
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
    }
    for (const bad of [
      { status: 'committed', receipt: receipt() },
      { state: 'in_progress' },
      { state: 'not_observed', result: receipt() },
      {
        state: 'committed',
        result: { ...receipt(), sprint: { ...receipt().sprint, title: 'Wrong' } },
      },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.lookupUpdate(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
})
