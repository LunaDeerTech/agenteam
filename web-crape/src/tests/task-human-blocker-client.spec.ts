import { describe, expect, it, vi } from 'vitest'
import { type Fetch } from '../api/client'
import {
  captureHumanBlockerAdd,
  captureHumanBlockerDraft,
  captureHumanBlockerResolve,
  createWorkTaskBlockersAPI,
} from '../api/work-task-blockers'
import {
  addInput,
  addPath,
  draft,
  id,
  json,
  lookupPath,
  p,
  receipt,
  resolveInput,
  resolvePath,
  t,
} from './task-human-blocker-fixture'
const write = { csrfToken: 'S'.repeat(43), key: 'original-human-blocker-1' }
describe('Human blocker closed HTTP contract', () => {
  it('accepts every nonterminal receipt and refuses terminal or unassigned active-state receipts', async () => {
    const original = receipt()
    for (const state of ['backlog', 'todo', 'in_progress', 'in_review', 'blocked']) {
      const value = { ...original, task: { ...original.task, state, assignee_agent_id: id(7) } }
      await expect(createWorkTaskBlockersAPI(async () => json(value)).add(p, t, addInput(), write))
        .resolves.toMatchObject({ task: { state, assignee_agent_id: id(7) } })
    }
    for (const state of ['done', 'cancelled', 'todo', 'in_progress', 'in_review']) {
      const value = { ...original, task: { ...original.task, state, assignee_agent_id: null } }
      await expect(createWorkTaskBlockersAPI(async () => json(value)).add(p, t, addInput(), write))
        .rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })
  it('uses exact add and resolve routes, body and complete immutable receipts', async () => {
    const fetcher = vi.fn<Fetch>(async (url, init) => {
      expect([addPath, resolvePath]).toContain(url)
      return json(receipt(JSON.parse(init.body as string)))
    })
    const api = createWorkTaskBlockersAPI(fetcher)
    const added = await api.add(p, t, addInput(), write)
    const resolved = await api.resolve(p, t, resolveInput(), write)
    expect(added.blocker).toMatchObject({
      id: id(5),
      type: 'waiting_for_human',
      description: draft().request.description,
      resolved_at: null,
    })
    expect(resolved.blocker).toMatchObject({ id: id(5), resolution_comment: null })
    expect(resolved.task.version).toBe('9007199254740995')
    expect(Object.isFrozen(added)).toBe(true)
    expect(Object.isFrozen(added.blocker)).toBe(true)
    expect(Object.isFrozen(added.event_ids)).toBe(true)
    for (const [i, [path, init]] of fetcher.mock.calls.entries()) {
      expect(path).toBe(i ? resolvePath : addPath)
      expect(init).toMatchObject({
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        headers: { 'X-CSRF-Token': write.csrfToken, 'Idempotency-Key': write.key },
      })
      expect(JSON.parse(init.body as string)).toEqual(i ? resolveInput() : addInput())
    }
  })
  it('validates UTF-8/control characters and only the explicit human kind without normalizing input', () => {
    for (const description of ['', '  \n\t ', '🧩'.repeat(256)]) {
      expect(
        captureHumanBlockerDraft({ ...draft(), request: { ...draft().request, description } })
          .request.description,
      ).toBe(description)
    }
    for (const bad of [
      { ...draft(), expected_version: '01' },
      { ...draft(), expected_version: '9223372036854775808' },
      { ...draft(), request: { ...draft().request, description: '🧩'.repeat(257) } },
      { ...draft(), request: { ...draft().request, description: '\u0000' } },
      { ...draft(), request: { ...draft().request, description: '\ud800' } },
      { ...draft(), request: { ...draft().request, type: 'technical' } },
      { ...draft(), request: { ...draft().request, metadata: { related_task_id: id(9) } } },
      { ...draft(), request: { ...draft().request, blocker_id: id(9) } },
    ])
      expect(() => captureHumanBlockerDraft(bad)).toThrowError()
    for (const resolution_comment of ['', ' \n\t ', '🧩'.repeat(257)])
      expect(() =>
        captureHumanBlockerResolve({
          ...resolveInput(),
          request: { blocker_id: id(5), resolution_comment },
        }),
      ).toThrowError()
    const comment = '  已核对\n🧩  '
    expect(
      captureHumanBlockerResolve({
        ...resolveInput(),
        request: { blocker_id: id(5), resolution_comment: comment },
      }).request.resolution_comment,
    ).toBe(comment)
    expect(captureHumanBlockerResolve(resolveInput()).request.resolution_comment).toBeNull()
    expect(() =>
      captureHumanBlockerResolve({ ...resolveInput(), request: { blocker_id: id(5) } }),
    ).toThrowError()
  })
  it('captures ID and nested original bytes then looks up all observations with the Task in the path', async () => {
    const mutable = addInput(),
      original = captureHumanBlockerAdd(mutable)
    expect(Object.isFrozen(original.request.metadata)).toBe(true)
    let observation = 'not_observed'
    const fetcher = vi.fn<Fetch>(async () =>
      json({
        status: observation,
        receipt: observation === 'committed' ? receipt(original) : null,
      }),
    )
    const api = createWorkTaskBlockersAPI(fetcher)
    for (observation of ['not_observed', 'in_progress', 'committed']) {
      const value = await api.lookup(p, t, 'work.task.blocker.add', original, write)
      expect(value.status).toBe(observation)
    }
    for (const [path, init] of fetcher.mock.calls) {
      expect(path).toBe(lookupPath)
      expect(init.headers).toMatchObject({ 'Idempotency-Key': write.key })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.blocker.add',
        ...original,
      })
      expect(JSON.parse(init.body as string)).not.toHaveProperty('target_id')
    }
    observation = 'not_observed'
    await expect(
      api.lookup(p, t, 'work.task.blocker.resolve', resolveInput(), write),
    ).resolves.toEqual({ status: 'not_observed', receipt: null })
    expect(JSON.parse(fetcher.mock.calls[3]![1].body as string)).toEqual({
      command: 'work.task.blocker.resolve',
      ...resolveInput(),
    })
  })
  it('rejects malformed or cross-target receipts atomically', async () => {
    const valid = receipt()
    for (const invalid of [
      { ...valid, task: { ...valid.task, version: addInput().expected_version } },
      { ...valid, task: { ...valid.task, project_id: id(60) } },
      { ...valid, task: { ...valid.task, state: 'done' } },
      { ...valid, blocker: { ...valid.blocker, task_id: id(60) } },
      { ...valid, blocker: { ...valid.blocker, id: id(60) } },
      { ...valid, blocker: { ...valid.blocker, description: 'different original' } },
      { ...valid, blocker: { ...valid.blocker, metadata: { unexpected: true } } },
      { ...valid, blocker: { ...valid.blocker, created_by: { type: 'system' } } },
      {
        ...valid,
        blocker: {
          ...valid.blocker,
          resolved_at: valid.blocker.created_at,
          resolved_by: valid.blocker.created_by,
        },
      },
      { ...valid, task: { ...valid.task, updated_at: '2026-10-13T11:00:00.000001Z' } },
      { ...valid, task_event_id: null },
      { ...valid, event_ids: [] },
      { ...valid, changed: true },
    ])
      await expect(
        createWorkTaskBlockersAPI(async () => json(invalid)).add(p, t, addInput(), write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const resolved = receipt(resolveInput())
    await expect(
      createWorkTaskBlockersAPI(async () =>
        json({ ...resolved, blocker: { ...resolved.blocker, resolution_comment: 'unexpected' } }),
      ).resolve(p, t, resolveInput(), write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects invalid parent IDs before fetch and wrong Lookup envelopes', async () => {
    const fetcher = vi.fn<Fetch>(async () => json({ state: 'committed', result: receipt() }))
    const api = createWorkTaskBlockersAPI(fetcher)
    for (const [project, target] of [
      ['null', t],
      [p, 'null'],
    ])
      expect(() => api.add(project!, target!, addInput(), write)).toThrowError()
    expect(fetcher).not.toHaveBeenCalled()
    await expect(
      api.lookup(p, t, 'work.task.blocker.add', addInput(), write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
