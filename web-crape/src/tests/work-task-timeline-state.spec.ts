import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive } from 'vue'
import { createAccountAPI } from '../api/account'
import { type Fetch } from '../api/client'
import { createWorkReviewAPI } from '../api/work-review'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import { createWorkTaskTimelineAPI } from '../api/work-task-timeline'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { createProjectWorkspace } from '../composables/useProjectWorkspace'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const p = id(10),
  taskID = id(20),
  otherTask = id(21),
  agentID = id(30)
const session = {
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
function event(n: number, task = taskID, project = p) {
  return {
    id: id(100 + n),
    project_id: project,
    task_id: task,
    task_version: '4',
    type: 'comment',
    actor: { type: 'human', user_id: id(1), source: 'task_domain' },
    operation_id: id(200),
    correlation_id: id(200),
    payload: { body: `原评论 ${n}` },
    created_at: `2026-10-11T10:00:00.${String(n).padStart(6, '0')}Z`,
  }
}
const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
const problem = (code: string, status: number, commit = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: code,
      detail: '',
      instance: '/api/v1/session',
      status,
      code,
      commit_state: commit,
      request_id: id(99),
    },
    status,
  )
const releases: (() => void)[] = []
function deferred<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
let tasks: ProjectTasks | undefined, auth: SessionController | undefined
// Only Project resolution and HTTP data are controlled. Session ownership,
// strict decoders, request retirement and Task publication use production code.
async function fixture() {
  let history: Fetch = async () =>
    json({ items: [event(4), event(3)], next_cursor: 'first-watermark' })
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    const url = new URL(path, 'https://example.test')
    if (url.pathname === '/api/v1/session') return json(session)
    if (url.pathname === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'B'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (url.pathname.endsWith('/events')) return history(path, init)
    if (url.pathname.endsWith('/transfer')) return problem('COMMIT_UNKNOWN', 503, 'unknown')
    if (url.pathname.endsWith('/task-transition-commands/lookup'))
      return json({ status: 'not_observed', receipt: null })
    const project = url.pathname.split('/')[4]!
    const base = {
      project_id: project,
      manual_rank: '8'.repeat(32),
      version: '4',
      created_at: time,
      updated_at: time,
    }
    const milestone = { ...base, id: id(11), title: '里程碑', description: '' }
    const sprint = {
      ...base,
      id: id(12),
      title: '当前迭代',
      milestone_id: id(11),
      description: '',
      state: 'current',
      started_at: time,
      completed_at: null,
      started_by: { kind: 'human', user_id: id(1) },
      completed_by: null,
    }
    if (url.pathname.endsWith('/milestones'))
      return json({ items: [(({ description: _d, ...row }) => row)(milestone)] })
    if (url.pathname.endsWith(`/milestones/${id(11)}`)) return json(milestone)
    if (url.pathname.endsWith('/sprints'))
      return json({
        items: [{ ...base, id: id(12), title: '当前迭代', milestone_id: id(11), state: 'current' }],
      })
    if (url.pathname.endsWith(`/sprints/${id(12)}`)) return json(sprint)
    if (url.pathname.endsWith('/agents'))
      return json({
        items: [
          {
            id: agentID,
            project_id: project,
            name: 'review-agent',
            display_name: '评审员',
            tag_color: null,
            description: '',
            version: '1',
            created_at: time,
            updated_at: time,
          },
        ],
      })
    if (url.pathname.endsWith('/blockers') || url.pathname.endsWith('/tasks'))
      return json({ items: [] })
    if (url.pathname.includes('/tasks/'))
      return json({
        ...base,
        id: url.pathname.split('/').at(-1),
        title: '原任务',
        milestone_id: id(11),
        sprint_id: id(12),
        type: 'task',
        priority: 'high',
        state: 'in_progress',
        assignee_agent_id: agentID,
        description: '',
        plan: '',
      })
    throw new Error('Unexpected timeline test route')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[16] = createWorkReviewAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  args[19] = { taskTimeline: createWorkTaskTimelineAPI(fetcher) }
  auth = createSessionController(...args)
  await auth.restore()
  const owner = auth
  const project = shallowReactive({
    id: p,
    lifecycle: 'active',
    current_sprint_id: id(12),
    address: '/owner/demo',
  })
  const context = computed(() =>
    owner.personalContext.identity
      ? {
          identity: owner.personalContext.identity,
          projectID: project.id,
          generation: 1,
          readGeneration: 1,
        }
      : null,
  )
  const workspace = {
    currentReadContext: context,
    detail: { project },
    paths: computed(() => ({ home: project.address })),
  } as unknown as ReturnType<typeof createProjectWorkspace>
  tasks = createProjectTasks(owner, workspace, async (path) => {
    tasks!.afterNavigation(path)
  })
  const current = tasks
  current.afterNavigation(`/owner/demo/tasks/${taskID}`)
  await flushPromises()
  expect(current.state.phase).toBe('ready')
  expect(current.timeline.phase).toBe('ready')
  return {
    owner,
    tasks: current,
    fetcher,
    project,
    history(value: Fetch) {
      history = value
    },
    reads: () =>
      fetcher.mock.calls.filter(([path]) =>
        new URL(path, 'https://example.test').pathname.endsWith('/events'),
      ),
  }
}
afterEach(async () => {
  tasks?.dispose()
  auth?.leave()
  releases.splice(0).forEach((release) => release())
  tasks = undefined
  auth = undefined
  await flushPromises()
  vi.restoreAllMocks()
})

describe('Task timeline original Session and page ownership', () => {
  it('loads complete older pages atomically and refreshes explicitly without confirming an Unknown write', async () => {
    const f = await fixture()
    expect(f.tasks.timeline.items.map((e) => e.id)).toEqual([id(104), id(103)])
    f.history(async (path) => {
      expect(new URL(path, 'https://example.test').searchParams.get('cursor')).toBe(
        'first-watermark',
      )
      return json({ items: [event(2), event(1)], next_cursor: 'older-watermark' })
    })
    await f.tasks.loadTimeline(true)
    expect(f.tasks.timeline.items.map((e) => e.id)).toEqual([id(104), id(103), id(102), id(101)])
    const accepted = f.tasks.timeline.items
    f.history(async () => json({ items: [event(2)], next_cursor: 'duplicate-page' }))
    await f.tasks.loadTimeline(true)
    expect(f.tasks.timeline.phase).toBe('error')
    expect(f.tasks.timeline.items).toBe(accepted)
    expect(f.tasks.timeline.next).toBe('older-watermark')
    expect(f.reads()).toHaveLength(3)
    f.history(async () => json({ items: [event(5)] }))
    await f.tasks.loadTimeline()
    expect(new URL(f.reads().at(-1)![0], 'https://example.test').searchParams.has('cursor')).toBe(
      false,
    )
    expect(f.tasks.timeline.items.map((e) => e.id)).toEqual([id(105)])
    expect(f.tasks.timeline.next).toBeUndefined()
    await expect(
      f.owner.workReview.startTransfer(p, taskID, {
        expected_version: '4',
        request: { target_state: 'in_review', comment: '原评审说明', assignee_agent_id: agentID },
      }),
    ).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    await flushPromises()
    const pending = f.owner.workReview.progress
    await f.tasks.loadTimeline()
    await f.tasks.setTimelineFilter('type', 'comment')
    await f.tasks.setTimelineFilter('order', 'asc')
    expect(f.owner.workReview.progress).toEqual(pending)
    expect(pending).toMatchObject({ phase: 'uncertain', taskID })
    expect(f.fetcher.mock.calls.filter(([path]) => path.endsWith('/transfer'))).toHaveLength(1)
    expect(f.fetcher.mock.calls.some(([path]) => path.includes('commands/lookup'))).toBe(false)
    const write = f.fetcher.mock.calls.find(([path]) => path.endsWith('/transfer'))![1]
    await f.owner.workReview.checkOriginal()
    const lookup = f.fetcher.mock.calls.find(([path]) =>
      path.endsWith('/task-transition-commands/lookup'),
    )![1]
    expect(new Headers(lookup.headers).get('Idempotency-Key')).toBe(
      new Headers(write.headers).get('Idempotency-Key'),
    )
    expect(JSON.parse(lookup.body as string)).toMatchObject({
      command: 'work.task.transfer',
      ...JSON.parse(write.body as string),
    })
    expect(f.owner.workReview.progress).toMatchObject({ phase: 'uncertain', taskID })
  })

  it('reads each type on the server, resets old pages and errors, and refuses duplicate or busy changes', async () => {
    const f = await fixture()
    const initial = new URL(f.reads()[0]![0], 'https://example.test').searchParams
    expect(initial.get('order')).toBe('desc')
    expect(initial.has('types')).toBe(false)
    f.history(async () => problem('CURSOR_INVALID', 400))
    await f.tasks.loadTimeline(true)
    expect(f.tasks.timeline).toMatchObject({
      phase: 'error',
      retryMore: true,
      next: 'first-watermark',
    })
    expect(f.tasks.timeline.message).not.toBe('')
    const held = deferred<Response>(json({ items: [] }))
    f.history(async () => held.promise)
    const changed = f.tasks.setTimelineFilter('type', 'comment')
    await flushPromises()
    expect(f.tasks.timeline).toMatchObject({
      type: 'comment',
      order: 'desc',
      phase: 'loading',
      items: [],
      next: undefined,
      message: '',
      retryMore: false,
    })
    const count = f.reads().length
    await f.tasks.setTimelineFilter('order', 'asc')
    await f.tasks.setTimelineFilter('type', 'state_changed')
    expect(f.reads()).toHaveLength(count)
    expect(f.tasks.timeline).toMatchObject({ type: 'comment', order: 'desc' })
    held.resolve(json({ items: [event(8)], next_cursor: 'comment-watermark' }))
    await changed
    await f.tasks.setTimelineFilter('type', 'comment')
    await f.tasks.setTimelineFilter('type', 'invented')
    await f.tasks.setTimelineFilter('order', 'invented')
    expect(f.reads()).toHaveLength(count)
    f.history(async () => json({ items: [] }))
    for (const type of [
      'task_created',
      'fields_updated',
      'blocker_added',
      'blocker_resolved',
      'state_changed',
      'assignee_changed',
      'all',
    ]) {
      await f.tasks.setTimelineFilter('type', type)
      const query = new URL(f.reads().at(-1)![0], 'https://example.test').searchParams
      expect(query.get('types')).toBe(type === 'all' ? null : type)
      expect(query.get('order')).toBe('desc')
      expect(query.has('cursor')).toBe(false)
      expect(f.tasks.timeline).toMatchObject({ phase: 'ready', items: [], next: undefined })
    }
    const commentQuery = new URL(f.reads()[2]![0], 'https://example.test').searchParams
    expect(commentQuery.get('types')).toBe('comment')
    expect(commentQuery.has('cursor')).toBe(false)
  })

  it.each(['asc', 'desc'] as const)(
    'continues %s pages with the same filter and strict timestamp/ID boundaries',
    async (order) => {
      const f = await fixture()
      const sameTime = (n: number) => ({ ...event(n), created_at: time })
      f.history(async () => json({ items: [], next_cursor: undefined }))
      await f.tasks.setTimelineFilter('type', 'comment')
      f.history(async () =>
        json({
          items: (order === 'asc' ? [1, 2] : [4, 3]).map(sameTime),
          next_cursor: 'first-filter-page',
        }),
      )
      if (order === 'asc') await f.tasks.setTimelineFilter('order', order)
      else await f.tasks.loadTimeline()
      const firstQuery = new URL(f.reads().at(-1)![0], 'https://example.test').searchParams
      expect(firstQuery.get('order')).toBe(order)
      expect(firstQuery.has('cursor')).toBe(false)
      f.history(async (path) => {
        const query = new URL(path, 'https://example.test').searchParams
        expect(query.get('types')).toBe('comment')
        expect(query.get('order')).toBe(order)
        expect(query.get('cursor')).toBe('first-filter-page')
        return json({
          items: (order === 'asc' ? [3, 4] : [2, 1]).map(sameTime),
          next_cursor: 'second-filter-page',
        })
      })
      await f.tasks.loadTimeline(true)
      expect(f.tasks.timeline.items.map((row) => row.id)).toEqual(
        (order === 'asc' ? [101, 102, 103, 104] : [104, 103, 102, 101]).map(id),
      )
      const accepted = f.tasks.timeline.items
      for (const bad of [sameTime(2), sameTime(order === 'asc' ? 0 : 6)]) {
        f.history(async () => json({ items: [bad], next_cursor: 'bad-page' }))
        await f.tasks.loadTimeline(true)
        expect(f.tasks.timeline).toMatchObject({
          phase: 'error',
          next: 'second-filter-page',
          retryMore: true,
        })
        expect(f.tasks.timeline.items).toBe(accepted)
      }
      f.history(async () => problem('CURSOR_INVALID', 400))
      await f.tasks.loadTimeline(true)
      expect(f.tasks.timeline.items).toBe(accepted)
      expect(f.tasks.timeline.next).toBe('second-filter-page')
      f.history(async (path) => {
        expect(new URL(path, 'https://example.test').searchParams.get('cursor')).toBe(
          'second-filter-page',
        )
        return json({ items: [sameTime(order === 'asc' ? 5 : 0)] })
      })
      await f.tasks.loadTimeline(true)
      expect(f.tasks.timeline.phase).toBe('ready')
      expect(f.tasks.timeline.items).toHaveLength(5)
      expect(f.tasks.timeline.next).toBeUndefined()
      await f.tasks.setTimelineFilter('order', order)
      const count = f.reads().length
      f.history(async () => json({ items: [] }))
      await f.tasks.refresh()
      expect(f.reads()).toHaveLength(count + 1)
      expect(f.tasks.timeline).toMatchObject({ type: 'comment', order })
      expect(new URL(f.reads().at(-1)![0], 'https://example.test').searchParams.has('cursor')).toBe(
        false,
      )
    },
  )

  it('retires the original read before a new Task or Project publishes, and clears history on Session revocation', async () => {
    const f = await fixture(),
      held = deferred<Response>(json({ items: [] }))
    f.history(async () => json({ items: [] }))
    await f.tasks.setTimelineFilter('type', 'comment')
    await f.tasks.setTimelineFilter('order', 'asc')
    let signal: AbortSignal | undefined
    f.history((_path, init) => {
      signal = init.signal as AbortSignal
      return held.promise
    })
    const original = f.tasks.loadTimeline()
    await flushPromises()
    expect(f.owner.state.busy).toBe(true)
    f.tasks.afterNavigation(`/owner/demo/tasks/${otherTask}`)
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    expect(f.tasks.timeline.items).toEqual([])
    expect(f.tasks.timeline).toMatchObject({ type: 'all', order: 'desc' })
    expect(f.owner.state.busy).toBe(true)
    const count = f.reads().length
    f.project.id = id(50)
    f.project.address = '/owner/other'
    f.tasks.afterNavigation(`/owner/other/tasks/${otherTask}`)
    await flushPromises()
    expect(f.reads()).toHaveLength(count)
    f.history(async () => json({ items: [event(6, otherTask, id(50))] }))
    held.resolve(json({ items: [event(9)] }))
    await original
    await flushPromises()
    expect(f.tasks.state.task?.project_id).toBe(id(50))
    expect(f.tasks.state.task?.id).toBe(otherTask)
    expect(f.tasks.timeline.items.map((e) => e.id)).toEqual([id(106)])
    expect(f.owner.state.busy).toBe(false)
    f.history(async () => json({ items: [] }))
    await f.tasks.setTimelineFilter('type', 'comment')
    await f.tasks.setTimelineFilter('order', 'asc')
    expect(f.tasks.timeline).toMatchObject({ type: 'comment', order: 'asc' })
    f.history(async () => problem('SESSION_REVOKED', 401))
    await f.tasks.loadTimeline()
    await flushPromises()
    expect(f.owner.state.phase).not.toBe('authenticated')
    expect(f.tasks.state.task).toBeNull()
    expect(f.tasks.timeline.items).toEqual([])
    expect(f.tasks.timeline.next).toBeUndefined()
    expect(f.tasks.timeline).toMatchObject({ type: 'all', order: 'desc' })
  })

  it('keeps the Session lane until an aborted response body actually retires before reading another Task', async () => {
    const f = await fixture()
    const tail = deferred<void>(undefined)
    const cancel = vi.fn(() => tail.promise)
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(JSON.stringify({ items: [event(7)] })))
        // Complete JSON bytes without EOF: the original body still owns the lane.
      },
      cancel,
    })
    f.history(async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }))
    const original = f.tasks.setTimelineFilter('type', 'comment')
    await flushPromises()
    expect(f.tasks.busy.value).toBe(true)
    expect(f.tasks.timeline).toMatchObject({ type: 'comment', phase: 'loading', items: [] })
    const count = f.reads().length
    await f.tasks.setTimelineFilter('type', 'state_changed')
    await f.tasks.setTimelineFilter('order', 'asc')
    expect(f.tasks.timeline).toMatchObject({ type: 'comment', order: 'desc' })
    expect(f.reads()).toHaveLength(count)
    f.tasks.afterNavigation(`/owner/demo/tasks/${otherTask}`)
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(f.owner.state.busy).toBe(true)
    expect(f.reads()).toHaveLength(count)
    expect(f.tasks.timeline).toMatchObject({ type: 'all', order: 'desc', items: [] })
    f.history(async () => json({ items: [event(6, otherTask)] }))
    tail.resolve(undefined)
    await original
    await flushPromises()
    expect(f.owner.state.busy).toBe(false)
    expect(f.tasks.state.task?.id).toBe(otherTask)
    expect(f.tasks.timeline.items.map((row) => row.id)).toEqual([id(106)])
  })
})
