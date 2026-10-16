import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureMilestoneUpdateInput,
  createWorkMilestonePlanningAPI,
  type MilestoneUpdateInput,
} from '../api/work-milestone-planning'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  target = id(2),
  at = '2026-10-11T10:00:00.000001Z'
const write = { csrfToken: 'C'.repeat(43), key: 'K'.repeat(32) }
const original = { expected_version: '9007199254740993', request: { title: '  e\u0301 项目😀  ' } }
function receipt(input: MilestoneUpdateInput = original, changed = true) {
  return {
    command: 'work.milestone.update',
    changed,
    sprint: null,
    event_id: changed ? id(3) : null,
    milestone: {
      id: target,
      project_id: project,
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
describe('Milestone edit wire contract', () => {
  it('preserves original bytes, exact presence, large versions and explicit empty description', async () => {
    const draft = { expected_version: original.expected_version, request: { ...original.request } }
    const captured = captureMilestoneUpdateInput(draft)
    draft.request.title = 'Later'
    expect(captured).toEqual(original)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const fetch = vi.fn<Fetch>(async () => json(receipt()))
    const result = await createWorkMilestonePlanningAPI(fetch).update(
      project,
      target,
      captured,
      write,
    )
    expect(result).toEqual(receipt())
    expect(fetch.mock.calls[0]![0]).toBe(`/api/v1/projects/${project}/milestones/${target}`)
    expect(fetch.mock.calls[0]![1].method).toBe('PATCH')
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(original)
    expect(
      captureMilestoneUpdateInput({ expected_version: '1', request: { description: '' } }).request,
    ).toEqual({ description: '' })
  })
  it('rejects invalid title, null, empty patch, unknown fields and malformed version before fetch', () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkMilestonePlanningAPI(fetch)
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
      { milestone_id: target, title: 'Wrong' },
    ])
      expect(() =>
        api.update(
          project,
          target,
          { expected_version: '1', request } as unknown as MilestoneUpdateInput,
          write,
        ),
      ).toThrowError(AccountFailure)
    for (const version of ['01', '0', '9223372036854775808', 1])
      expect(() =>
        captureMilestoneUpdateInput({ expected_version: version, request: { title: 'x' } }),
      ).toThrowError(AccountFailure)
    expect(fetch).not.toHaveBeenCalled()
  })
  it('accepts a MaxInt64 no-op and requires precise changed/version/event and original fields', async () => {
    const max = { expected_version: '9223372036854775807', request: { description: '' } }
    const fetch = vi.fn<Fetch>().mockResolvedValueOnce(json(receipt(max, false))),
      api = createWorkMilestonePlanningAPI(fetch)
    expect(await api.update(project, target, max, write)).toEqual(receipt(max, false))
    const good = receipt()
    for (const bad of [
      { ...good, command: 'work.milestone.create' },
      { ...good, sprint: {} },
      { ...good, event_id: null },
      { ...good, changed: false },
      { ...good, milestone: { ...good.milestone, id: id(8) } },
      { ...good, milestone: { ...good.milestone, project_id: id(8) } },
      { ...good, milestone: { ...good.milestone, title: 'Trimmed' } },
      { ...good, milestone: { ...good.milestone, version: original.expected_version } },
      { ...receipt(max, false), event_id: id(3) },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.update(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('uses Structure state/result and the original target, version, patch and key for all Lookup observations', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkMilestonePlanningAPI(fetch)
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
        command: 'work.milestone.update',
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
        result: { ...receipt(), milestone: { ...receipt().milestone, title: 'Wrong' } },
      },
    ]) {
      fetch.mockResolvedValueOnce(json(bad))
      await expect(api.lookupUpdate(project, target, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
})
