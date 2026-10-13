import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import { captureTaskReorderInput, createWorkTaskReorderAPI } from '../api/work-task-reorder'
import { id, p, t, task, json } from './task-human-blocker-fixture'

const write = { csrfToken: 'S'.repeat(43), key: 'original-task-reorder' }
const original = () => ({ expected_version: task().version, request: { before_id: id(6) } })
function receipt(changed = true) {
  return {
    task: task(String(BigInt(task().version) + (changed ? 1n : 0n))),
    changed,
    task_event_id: changed ? id(10) : null,
    event_ids: changed ? [id(11)] : [],
  }
}

describe('Task same-group reorder closed wire', () => {
  it('sends before or omitted tail with the exact version and validates changed/no-op receipts', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(receipt()))
    const api = createWorkTaskReorderAPI(fetcher)
    const captured = captureTaskReorderInput(original(), t)
    expect(Object.isFrozen(captured.request)).toBe(true)
    expect(await api.reorder(p, t, captured, write)).toEqual(receipt())
    const [path, init] = fetcher.mock.calls[0]!
    expect(path).toBe(`/api/v1/projects/${p}/tasks/${t}/reorder`)
    expect(init).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
    })
    expect(init.headers).toMatchObject({
      'X-CSRF-Token': write.csrfToken,
      'Idempotency-Key': write.key,
    })
    expect(JSON.parse(init.body as string)).toEqual(original())
    fetcher.mockImplementationOnce(async () => json(receipt(false)))
    await expect(
      api.reorder(p, t, { expected_version: task().version, request: {} }, write),
    ).resolves.toEqual(receipt(false))
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string).request).toEqual({})
  })
  it('looks up all three states using the original full intent without mutation', async () => {
    const fetcher = vi.fn<Fetch>()
    const api = createWorkTaskReorderAPI(fetcher)
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      const value = { status, receipt: status === 'committed' ? receipt() : null }
      fetcher.mockResolvedValueOnce(json(value))
      expect(await api.lookup(p, t, original(), write)).toEqual(value)
    }
    expect(fetcher).toHaveBeenCalledTimes(3)
    for (const [path, init] of fetcher.mock.calls) {
      expect(path).toBe(`/api/v1/projects/${p}/task-commands/lookup`)
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.reorder',
        target_id: t,
        ...original(),
      })
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
    }
  })
  it('rejects self anchors, null, cross-group input fields, malformed versions and partial receipts', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(receipt()))
    const api = createWorkTaskReorderAPI(fetcher)
    for (const value of [
      { ...original(), request: { before_id: t } },
      { ...original(), request: { before_id: null } },
      { ...original(), request: { before_id: id(6), priority: 'low' } },
      { ...original(), request: { sprint_id: id(3) } },
      { ...original(), request: { manual_rank: '8'.repeat(32) } },
      { ...original(), expected_version: 2 },
      { ...original(), expected_version: '01' },
    ]) {
      expect(() => captureTaskReorderInput(value, t)).toThrow()
    }
    expect(fetcher).not.toHaveBeenCalled()
    for (const value of [
      { ...receipt(), task: { ...receipt().task, id: id(7) } },
      { ...receipt(), task: { ...receipt().task, project_id: id(7) } },
      { ...receipt(), task: { ...receipt().task, version: task().version } },
      { ...receipt(), task: { ...receipt().task, assignee_agent_id: id(7) } },
      { ...receipt(), task_event_id: null },
      { ...receipt(), event_ids: [] },
      { ...receipt(false), event_ids: [id(11)] },
      { ...receipt(false), task_event_id: id(10) },
    ]) {
      fetcher.mockResolvedValueOnce(json(value))
      await expect(api.reorder(p, t, original(), write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    fetcher.mockResolvedValueOnce(json({ status: 'not_observed', receipt: receipt() }))
    await expect(api.lookup(p, t, original(), write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
})
