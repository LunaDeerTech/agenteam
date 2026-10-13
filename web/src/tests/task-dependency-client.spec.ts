import { describe, expect, it, vi } from 'vitest'
import { type Fetch } from '../api/client'
import { createWorkReviewAPI } from '../api/work-review'
import {
  captureTaskDependencyDraft,
  createWorkTaskDependenciesAPI,
} from '../api/work-task-dependencies'
import {
  addInput,
  candidate,
  draft,
  id,
  json,
  lookupPath,
  p,
  receipt,
  related,
  resolveInput,
  resolvePath,
  t,
} from './task-dependency-fixture'
const write = { csrfToken: 'S'.repeat(43), key: 'original-task-dependency-1' }
const signal = new AbortController().signal

describe('Task dependency closed HTTP contract', () => {
  it('reads a full Project page in Sprint/state/priority/rank/ID order without loosening board queries', async () => {
    const items = [
      candidate(61, 'done'),
      candidate(62, 'cancelled'),
      candidate(63, 'backlog', id(30)),
    ]
    const fetcher = vi.fn<Fetch>(async () => json({ items }))
    const api = createWorkTaskDependenciesAPI(fetcher)
    const page = await api.candidates(p, { text: '  Task  ', cursor: 'opaque', limit: 50 }, signal)
    expect(page.items.map((item) => item.id)).toEqual(items.map((item) => item.id))
    const query = new URL(fetcher.mock.calls[0]![0], 'https://local.test').searchParams
    expect(Object.fromEntries(query)).toEqual({ limit: '50', text: '  Task  ', cursor: 'opaque' })
    expect(Object.isFrozen(page.items)).toBe(true)
    for (const bad of [
      { sprint_id: id(3) },
      { state: 'backlog' },
      { limit: 51 },
      { text: ' ' },
      { text: 'a'.repeat(257) },
    ])
      expect(() => api.candidates(p, bad as never, signal)).toThrowError()
    expect(() => createWorkReviewAPI(fetcher).tasks(p, {} as never, signal)).toThrowError()
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it('rejects partial, foreign, duplicate, unsorted and invalid cursor pages atomically', async () => {
    for (const page of [
      { items: [candidate(), { ...candidate(61), project_id: id(90) }] },
      { items: [candidate(), candidate()] },
      { items: [candidate(60, 'done'), candidate(61, 'backlog')] },
      { items: [candidate(60, 'backlog', id(30)), candidate(61)] },
      { items: [candidate()], next_cursor: 'next' },
      { items: Array.from({ length: 51 }, (_, n) => candidate(100 + n)) },
      { items: [{ ...candidate(), unexpected: true }] },
    ])
      await expect(
        createWorkTaskDependenciesAPI(async () => json(page)).candidates(p, {}, signal),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const valid = {
      items: Array.from({ length: 50 }, (_, n) => candidate(100 + n)),
      next_cursor: 'next',
    }
    await expect(
      createWorkTaskDependenciesAPI(async () => json(valid)).candidates(p, {}, signal),
    ).resolves.toMatchObject({ next_cursor: 'next' })
    await expect(
      createWorkTaskDependenciesAPI(async () => json(valid)).candidates(
        p,
        { cursor: 'next' },
        signal,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('freezes rely_on metadata and leaves resolve private related ID out of the wire body', async () => {
    const original = captureTaskDependencyDraft(draft())
    expect(Object.isFrozen(original.request.metadata)).toBe(true)
    expect(() =>
      captureTaskDependencyDraft({ ...draft(), request: { ...draft().request, metadata: {} } }),
    ).toThrowError()
    expect(() =>
      captureTaskDependencyDraft({
        ...draft(),
        request: { ...draft().request, type: 'waiting_for_human' },
      }),
    ).toThrowError()
    const fetcher = vi.fn<Fetch>(async (_url, init) =>
      json(receipt(JSON.parse(init.body as string))),
    )
    const api = createWorkTaskDependenciesAPI(fetcher)
    const added = await api.add(p, t, addInput(), write)
    const resolved = await api.resolve(p, t, resolveInput(), related, write)
    expect(added.blocker).toMatchObject({
      type: 'rely_on',
      related_task_id: related,
      resolved_at: null,
    })
    expect(resolved.task.version).toBe('9007199254740995')
    expect(fetcher.mock.calls[1]![0]).toBe(resolvePath)
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toEqual(resolveInput())
  })
  it('requires the original rely_on type, related target and exact version in add/resolve/Lookup receipts', async () => {
    for (const input of [addInput(), resolveInput()]) {
      const valid = receipt(input)
      for (const invalid of [
        { ...valid, blocker: { ...valid.blocker, metadata: { related_task_id: id(90) } } },
        { ...valid, blocker: { ...valid.blocker, type: 'waiting_for_human', metadata: {} } },
        { ...valid, blocker: { ...valid.blocker, task_id: id(90) } },
        { ...valid, task: { ...valid.task, version: input.expected_version } },
      ]) {
        const api = createWorkTaskDependenciesAPI(async () => json(invalid))
        await expect(
          'description' in input.request
            ? api.add(p, t, addInput(), write)
            : api.resolve(p, t, resolveInput(), related, write),
        ).rejects.toMatchObject({ kind: 'invalid-response' })
        await expect(
          createWorkTaskDependenciesAPI(async () =>
            json({ status: 'committed', receipt: invalid }),
          ).lookup(
            p,
            t,
            'description' in input.request ? 'work.task.blocker.add' : 'work.task.blocker.resolve',
            input,
            related,
            write,
          ),
        ).rejects.toMatchObject({ kind: 'invalid-response' })
      }
    }
  })
  it('uses only the original command body for all explicit Lookup observations', async () => {
    let status = 'not_observed'
    const fetcher = vi.fn<Fetch>(async () =>
      json({ status, receipt: status === 'committed' ? receipt() : null }),
    )
    const api = createWorkTaskDependenciesAPI(fetcher)
    for (status of ['not_observed', 'in_progress', 'committed'])
      await api.lookup(p, t, 'work.task.blocker.add', addInput(), related, write)
    for (const [path, init] of fetcher.mock.calls) {
      expect(path).toBe(lookupPath)
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.blocker.add',
        ...addInput(),
      })
    }
  })
})
