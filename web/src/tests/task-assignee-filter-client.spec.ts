import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createWorkReviewAPI, type TaskQuery } from '../api/work-review'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const signal = () => new AbortController().signal
const project = id(1),
  sprint = id(2),
  agent = id(3)
const response = (items: unknown[]) =>
  new Response(JSON.stringify({ items }), {
    headers: { 'Content-Type': 'application/json' },
  })
const summary = (assignee: string | null) => ({
  id: id(4),
  project_id: project,
  milestone_id: id(5),
  sprint_id: sprint,
  title: '任务',
  manual_rank: '8'.repeat(32),
  version: '1',
  created_at: '2026-10-13T10:00:00.000000Z',
  updated_at: '2026-10-13T10:00:00.000000Z',
  state: 'blocked',
  type: 'bug',
  priority: 'high',
  assignee_agent_id: assignee,
})

describe('Task assignee filter codec', () => {
  it('distinguishes omission, null and Agent ID with the existing AND filters and cursor', async () => {
    const fetcher = vi.fn<Fetch>(async () => response([]))
    const api = createWorkReviewAPI(fetcher)
    for (const selected of [undefined, null, agent]) {
      for (const cursor of [undefined, 'original-cursor']) {
        await api.tasks(
          project,
          {
            sprint_id: sprint,
            state: 'blocked',
            text: 'literal %_',
            priority: 'high',
            type: 'bug',
            ...(selected === undefined ? {} : { assignee_agent_id: selected }),
            ...(cursor ? { cursor } : {}),
          },
          signal(),
        )
        const [path, init] = fetcher.mock.calls.at(-1)!
        expect(Object.fromEntries(new URL(path, 'https://example.test').searchParams)).toEqual({
          limit: '50',
          sprint_id: sprint,
          state: 'blocked',
          text: 'literal %_',
          priority: 'high',
          type: 'bug',
          ...(selected === undefined ? {} : { assignee_agent_id: selected ?? 'null' }),
          ...(cursor ? { cursor } : {}),
        })
        expect(init.method).toBe('GET')
        expect(init.body).toBeUndefined()
      }
    }
  })

  it('rejects malformed selectors and keeps null unavailable for every other ID field', async () => {
    const fetcher = vi.fn<Fetch>(async () => response([]))
    const api = createWorkReviewAPI(fetcher)
    for (const value of ['', 'null', 'all', 'unassigned', 'NULL', id(0xab).toUpperCase(), 7, {}]) {
      await expect(
        Promise.resolve().then(() =>
          api.tasks(
            project,
            {
              sprint_id: sprint,
              state: 'blocked',
              assignee_agent_id: value,
            } as TaskQuery,
            signal(),
          ),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    const call = accountTransport(fetcher)
    await expect(
      call('reviewTasks', (v) => v, {
        projectID: project,
        signal: signal(),
        workQuery: {
          limit: '50',
          sprint_id: 'null',
          state: 'blocked',
          assignee_agent_id: 'null',
        },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      call('reviewSprints', (v) => v, {
        projectID: project,
        signal: signal(),
        workQuery: { limit: '50', milestone_id: 'null' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      call('directoryAgents', (v) => v, {
        projectID: project,
        signal: signal(),
        workQuery: { limit: '50', assignee_agent_id: 'null' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('rejects whole pages with an incorrect assignee instead of silently filtering responses', async () => {
    const fetcher = vi.fn<Fetch>(async () => response([summary(agent)]))
    const api = createWorkReviewAPI(fetcher)
    for (const selected of [null, id(30)]) {
      await expect(
        api.tasks(
          project,
          {
            sprint_id: sprint,
            state: 'blocked',
            assignee_agent_id: selected,
          },
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(
      api.tasks(project, { sprint_id: sprint, state: 'blocked' }, signal()),
    ).resolves.toEqual({ items: [summary(agent)] })
    fetcher.mockImplementationOnce(async () => response([summary(null)]))
    await expect(
      api.tasks(
        project,
        {
          sprint_id: sprint,
          state: 'blocked',
          assignee_agent_id: null,
        },
        signal(),
      ),
    ).resolves.toEqual({ items: [summary(null)] })
  })

  it('validates a late response against the captured Agent, not a subsequently edited query', async () => {
    let release!: (response: Response) => void
    const fetcher = vi.fn<Fetch>(
      () =>
        new Promise<Response>((resolve) => {
          release = resolve
        }),
    )
    const query = { sprint_id: sprint, state: 'blocked' as const, assignee_agent_id: agent }
    const pending = createWorkReviewAPI(fetcher).tasks(project, query, signal())
    query.assignee_agent_id = id(30)
    release(response([summary(agent)]))
    await expect(pending).resolves.toEqual({ items: [summary(agent)] })
    expect(
      new URL(fetcher.mock.calls[0]![0], 'https://example.test').searchParams.get(
        'assignee_agent_id',
      ),
    ).toBe(agent)
  })
})
