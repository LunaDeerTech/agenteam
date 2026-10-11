import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, type Fetch } from '../api/client'
import {
  captureMilestoneCreateDraft,
  captureMilestoneCreateInput,
  createWorkMilestonePlanningAPI,
  type MilestoneCreateInput,
} from '../api/work-milestone-planning'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  m = id(2),
  time = '2026-10-11T10:00:00.000001Z'
const original = { request: { milestone_id: m, title: 'First milestone', description: '' } }
const write = { csrfToken: 'C'.repeat(43), key: 'K'.repeat(32) }
function receipt(input: MilestoneCreateInput = original) {
  return {
    command: 'work.milestone.create',
    changed: true,
    sprint: null,
    event_id: id(3),
    milestone: {
      id: input.request.milestone_id,
      project_id: p,
      title: input.request.title,
      description: input.request.description ?? '',
      version: '1',
      manual_rank: '8'.repeat(32),
      created_at: time,
      updated_at: time,
    },
  }
}
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
describe('Milestone planning client', () => {
  it('preserves optional description presence and accepts only a complete original Milestone creation receipt', async () => {
    const captured = captureMilestoneCreateInput(original)
    expect(Object.isFrozen(captured.request)).toBe(true)
    expect(captureMilestoneCreateDraft({ title: 'First' })).toEqual({ title: 'First' })
    const fetch = vi.fn<Fetch>(async () => json(receipt()))
    const result = await createWorkMilestonePlanningAPI(fetch).create(p, captured, write)
    expect(result).toEqual(receipt())
    expect(Object.isFrozen(result.milestone)).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(1)
    const [path, init] = fetch.mock.calls[0]!
    expect(path).toBe(`/api/v1/projects/${p}/milestones`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual(original)
    expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
  })
  it('sends only the original key and full create request to Structure Lookup and preserves a conflict', async () => {
    const fetch = vi
      .fn<Fetch>()
      .mockResolvedValueOnce(json({ state: 'committed', result: receipt() }))
      .mockResolvedValueOnce(json({ state: 'not_observed', result: null }))
      .mockResolvedValueOnce(
        json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Conflict',
            status: 409,
            detail: '',
            instance: `/api/v1/projects/${p}/milestones`,
            code: 'IDEMPOTENCY_KEY_REUSED',
            request_id: id(99),
            commit_state: 'not_started',
          },
          409,
        ),
      )
    const api = createWorkMilestonePlanningAPI(fetch)
    expect(await api.lookup(p, original, write)).toEqual({ state: 'committed', result: receipt() })
    expect(await api.lookup(p, original, write)).toEqual({ state: 'not_observed', result: null })
    await expect(api.create(p, original, write)).rejects.toMatchObject({
      problem: { code: 'IDEMPOTENCY_KEY_REUSED' },
    })
    expect(fetch).toHaveBeenCalledTimes(3)
    for (const [path, init] of fetch.mock.calls.slice(0, 2)) {
      expect(path).toBe(`/api/v1/projects/${p}/structure-commands/lookup`)
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.milestone.create',
        ...original,
      })
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
    }
  })
  it('rejects invalid inputs without I/O and rejects corrupt or duplicate-member successful responses', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createWorkMilestonePlanningAPI(fetch)
    for (const request of [
      { milestone_id: m, title: 'x', description: null },
      { milestone_id: m, title: 'x', description: undefined },
      { milestone_id: id(2).replace('-7000-', '-4000-'), title: 'x' },
      { milestone_id: m, title: 'x'.repeat(257) },
      { milestone_id: m, title: 'x', description: 'x'.repeat(32769) },
    ])
      expect(() =>
        api.create(p, { request } as unknown as MilestoneCreateInput, write),
      ).toThrowError(AccountFailure)
    expect(fetch).not.toHaveBeenCalled()
    for (const value of [
      { ...receipt(), changed: false },
      { ...receipt(), sprint: {} },
      { ...receipt(), milestone: { ...receipt().milestone, id: id(8) } },
      { ...receipt(), milestone: { ...receipt().milestone, version: '2' } },
      { ...receipt(), milestone: { ...receipt().milestone, title: 'Ignored original title' } },
    ]) {
      fetch.mockResolvedValueOnce(json(value))
      await expect(api.create(p, original, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    fetch.mockResolvedValueOnce(
      new Response(
        JSON.stringify(receipt()).replace('"changed":true', '"changed":false,"changed":true'),
        { status: 200, headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) } },
      ),
    )
    await expect(api.create(p, original, write)).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(fetch).toHaveBeenCalledTimes(6)
  })
})
