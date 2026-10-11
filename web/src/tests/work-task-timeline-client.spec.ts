import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import { createWorkTaskTimelineAPI } from '../api/work-task-timeline'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1)
const target = id(2)
const path = `/api/v1/projects/${project}/tasks/${target}/events`
const signal = () => new AbortController().signal
const human = { type: 'human', user_id: id(3), source: 'task_domain' }
const longComment = '评审 🧩\n' + '<'.repeat(32756)
const event = (n: number, type: string, payload: unknown, system = false) => ({
  id: id(100 + n),
  project_id: project,
  task_id: target,
  task_version: type === 'task_created' ? '1' : '9007199254740993',
  type,
  actor: system
    ? { type: 'system', service_name: 'scheduler', source: 'scheduler', cause_id: id(200 + n) }
    : { ...human },
  operation_id: id(200 + n),
  correlation_id: id(200 + n),
  payload,
  created_at: `2026-10-11T12:00:00.${n.toString().padStart(6, '0')}Z`,
})
const events = () => [
  event(1, 'task_created', {
    initial_state: 'backlog',
    milestone_id: id(4),
    sprint_id: id(5),
    type: 'task',
    priority: 'medium',
  }),
  event(2, 'fields_updated', {
    changed_fields: ['description', 'title'],
    type_change: null,
    priority_change: null,
    position: null,
  }),
  event(3, 'blocker_added', { blocker_id: id(6), blocker_type: 'waiting_for_human' }),
  event(4, 'blocker_resolved', {
    blocker_id: id(6),
    blocker_type: 'waiting_for_human',
    resolution_comment: '  已确认\n保留原文  ',
  }),
  event(5, 'state_changed', {
    from_state: 'in_progress',
    to_state: 'in_review',
    reason_code: null,
  }),
  event(6, 'assignee_changed', { from_agent_id: null, to_agent_id: id(7) }),
  event(7, 'comment', { body: longComment }),
  event(
    8,
    'state_changed',
    {
      from_state: 'todo',
      to_state: 'in_progress',
      reason_code: 'scheduler_claim',
    },
    true,
  ),
  event(
    9,
    'state_changed',
    {
      from_state: 'in_progress',
      to_state: 'todo',
      reason_code: 'scheduler_agent_busy_compensation',
    },
    true,
  ),
  event(
    10,
    'blocker_added',
    {
      blocker_id: id(8),
      blocker_type: 'technical',
      reason_code: 'scheduler_launch_failed',
    },
    true,
  ),
  event(
    11,
    'state_changed',
    {
      from_state: 'in_review',
      to_state: 'blocked',
      reason_code: 'scheduler_launch_failed',
    },
    true,
  ),
  {
    ...event(12, 'blocker_resolved', {
      blocker_id: id(8),
      blocker_type: 'technical',
      resolution_comment: null,
    }),
    // This last pair shares a microsecond: only the exact ID breaks the tie.
    created_at: '2026-10-11T12:00:00.000011Z',
  },
]
const rawResponse = (body: string) =>
  new Response(body, { headers: { 'Content-Type': 'application/json' } })
const response = (body: unknown) => rawResponse(JSON.stringify(body))

describe('Owner Task timeline closed client', () => {
  it('reads all event families, preserves microsecond order and complete byte-short pages', async () => {
    const original = events()
    const descending = [...original].reverse()
    // Twenty full comments exceed the old transport budget, while the actual
    // Go-style HTML-escaped wire still fits the separate Timeline page budget.
    const large = Array.from({ length: 20 }, (_, n) =>
      event(n + 20, 'comment', { body: longComment }),
    ).reverse()
    const largeWire = JSON.stringify({ items: large, next_cursor: 'large-page-next' }).replaceAll(
      '<',
      '\\u003c',
    )
    expect(new TextEncoder().encode(longComment).byteLength).toBe(32768)
    expect(new TextEncoder().encode(largeWire).byteLength).toBeGreaterThan(600_000)
    expect(new TextEncoder().encode(largeWire).byteLength).toBeLessThan(4 * 1024 * 1024)
    const replies = [
      response({ items: original, next_cursor: 'short-page-next' }),
      response({ items: descending }),
      rawResponse(largeWire),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createWorkTaskTimelineAPI(fetcher)
    const first = await api.list(project, target, { order: 'asc' }, signal())
    expect(first).toEqual({ items: original, next_cursor: 'short-page-next' })
    expect(first.items).toHaveLength(12)
    expect(new Set(first.items.map((item) => item.type)).size).toBe(7)
    expect(Object.isFrozen(first)).toBe(true)
    expect(Object.isFrozen(first.items)).toBe(true)
    for (const item of first.items) {
      expect(Object.keys(item).sort()).toEqual([
        'actor',
        'correlation_id',
        'created_at',
        'id',
        'operation_id',
        'payload',
        'project_id',
        'task_id',
        'task_version',
        'type',
      ])
      expect(Object.isFrozen(item)).toBe(true)
      expect(Object.isFrozen(item.actor)).toBe(true)
      expect(Object.isFrozen(item.payload)).toBe(true)
      if (item.type === 'fields_updated')
        expect(Object.isFrozen(item.payload.changed_fields)).toBe(true)
    }
    expect(await api.list(project, target, {}, signal())).toEqual({ items: descending })
    expect(await api.list(project, target, {}, signal())).toEqual({
      items: large,
      next_cursor: 'large-page-next',
    })
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      `${path}?limit=50&order=asc`,
      `${path}?limit=50&order=desc`,
      `${path}?limit=50&order=desc`,
    ])
    for (const [, init] of fetcher.mock.calls) {
      expect(init).toMatchObject({
        method: 'GET',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      expect(init.body).toBeUndefined()
      expect(init.headers).not.toHaveProperty('X-CSRF-Token')
      expect(init.headers).not.toHaveProperty('Idempotency-Key')
    }
  })

  it('binds the original scope and filters and rejects a malformed page without partial publication', async () => {
    const comment = event(7, 'comment', { body: '保留原评审\n  不裁剪  ' })
    const cursor = 'opaque+/=字'
    const fetcher = vi.fn<Fetch>(async () =>
      response({ items: [comment], next_cursor: 'next-original-page' }),
    )
    const api = createWorkTaskTimelineAPI(fetcher)
    expect(
      await api.list(
        project,
        target,
        {
          limit: 2,
          cursor,
          types: ['state_changed', 'comment'],
          order: 'asc',
        },
        signal(),
      ),
    ).toEqual({ items: [comment], next_cursor: 'next-original-page' })
    expect(fetcher).toHaveBeenCalledTimes(1)
    const url = new URL(fetcher.mock.calls[0]![0], 'https://owned.invalid')
    expect(url.pathname).toBe(path)
    expect([...url.searchParams.entries()].sort()).toEqual([
      ['cursor', cursor],
      ['limit', '2'],
      ['order', 'asc'],
      ['types', 'comment,state_changed'],
    ])
    const next = event(8, 'comment', { body: 'another complete comment' })
    const all = events()
    const invalidPages = [
      { items: [comment, { ...next, type: 'future_event' }] },
      { items: [comment, { ...next, project_id: id(99) }] },
      { items: [comment, { ...next, task_id: id(99) }] },
      { items: [comment, { ...next, task_version: 2 }] },
      { items: [comment, { ...next, payload: { body: 'valid', secret: 'not a public field' } }] },
      { items: [comment, comment] },
      { items: [next, comment] },
      { items: [all[11], all[10]] },
      { items: [{ ...all[7], actor: { ...all[7]!.actor, cause_id: id(99) } }] },
      {
        items: [
          event(9, 'blocker_resolved', {
            blocker_id: id(8),
            blocker_type: 'technical',
            resolution_comment: 'not permitted',
          }),
        ],
      },
      { items: [], next_cursor: 'next' },
      { items: [comment], next_cursor: cursor },
    ]
    for (const body of invalidPages) {
      const invalid = vi.fn<Fetch>(async () => response(body))
      await expect(
        createWorkTaskTimelineAPI(invalid).list(
          project,
          target,
          { cursor, order: 'asc' },
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      expect(invalid).toHaveBeenCalledTimes(1)
    }
    const mismatch = createWorkTaskTimelineAPI(async () => response({ items: [comment] }))
    await expect(
      mismatch.list(project, target, { types: ['state_changed'] }, signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    // Do not JSON.stringify away duplicate members: the valid final member
    // must not hide the conflicting original event or duplicate page field.
    for (const raw of [
      `{"items":[{"id":"${id(99)}",${JSON.stringify(comment).slice(1)}]}`,
      '{"items":[{"type":"future_event"}],"items":[]}',
    ]) {
      await expect(
        createWorkTaskTimelineAPI(async () => rawResponse(raw)).list(project, target, {}, signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const oversized = Array.from({ length: 22 }, (_, n) =>
      event(n + 20, 'comment', { body: longComment }),
    ).reverse()
    const oversizedWire = JSON.stringify({ items: oversized }).replaceAll('<', '\\u003c')
    expect(new TextEncoder().encode(oversizedWire).byteLength).toBeGreaterThan(4 * 1024 * 1024)
    await expect(
      createWorkTaskTimelineAPI(async () => rawResponse(oversizedWire)).list(
        project,
        target,
        {},
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('rejects invalid local pagination before network and preserves cursor Problems without retry', async () => {
    const fetcher = vi.fn<Fetch>(async () => response({ items: [] }))
    const api = createWorkTaskTimelineAPI(fetcher)
    for (const query of [
      { limit: 0 },
      { limit: 201 },
      { limit: 1.5 },
      { cursor: '' },
      { cursor: 'a\u0000b' },
      { cursor: 'a'.repeat(8193) },
      { types: [] },
      { types: ['comment', 'comment'] },
      { types: ['future_event'] },
      { order: 'ascending' },
      { owner_id: id(3) },
    ]) {
      await expect(
        Promise.resolve().then(() => api.list(project, target, query as never, signal())),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    await expect(
      Promise.resolve().then(() => api.list('../other', target, {}, signal())),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      Promise.resolve().then(() => api.list(project, '../other', {}, signal())),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
    for (const [code, status] of [
      ['CURSOR_INVALID', 400],
      ['CURSOR_STALE', 409],
      ['UNAUTHENTICATED', 401],
    ] as const) {
      const problem = {
        type: 'urn:agenteam:problem:test',
        title: 'Cannot read the original page',
        status,
        detail: 'safe failure',
        instance: path,
        code,
        request_id: id(90),
        commit_state: 'not_started',
      }
      const failure = vi.fn<Fetch>(
        async () =>
          new Response(JSON.stringify(problem), {
            status,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
          }),
      )
      await expect(
        createWorkTaskTimelineAPI(failure).list(
          project,
          target,
          { cursor: 'original-cursor' },
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'problem', problem })
      expect(failure).toHaveBeenCalledTimes(1)
      expect(
        new URL(failure.mock.calls[0]![0], 'https://owned.invalid').searchParams.get('cursor'),
      ).toBe('original-cursor')
    }
  })
})
