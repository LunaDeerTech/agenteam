import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { type Fetch } from '../api/client'
import {
  createWorkPlanningAPI,
  type WorkMilestone,
  type WorkSprint,
  type WorkTask,
} from '../api/work-planning'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectWorkPlanning } from '../composables/useProjectWorkPlanning'

const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const at = '2026-10-09T10:00:00.000000Z'
const home = '/owner/demo'
const session = (): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: '',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
  csrf_token: 'S'.repeat(43),
})
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const reject = (code: string, status = 409) =>
  response(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      instance: '/api/v1/projects',
      status,
      code,
      commit_state: 'not_committed',
      request_id: id(99),
    },
    status,
  )
const cleanup: (() => void)[] = []
afterEach(() => {
  cleanup
    .splice(0)
    .reverse()
    .forEach((stop) => stop())
  vi.restoreAllMocks()
})
async function fixture(options: { current?: boolean; state?: WorkTask['state'] } = {}) {
  let account = session()
  const project: Project = {
    id: id(10),
    owner_user_id: id(1),
    name: 'Demo',
    normalized_name: 'demo',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: options.current ? id(12) : null,
    created_at: at,
    updated_at: at,
    archived_at: null,
  }
  let milestone: WorkMilestone = {
    id: id(11),
    project_id: project.id,
    title: 'Milestone one',
    description: 'original',
    manual_rank: '8'.repeat(32),
    version: '1',
    created_at: at,
    updated_at: at,
  }
  const sprint: WorkSprint = {
    ...milestone,
    id: id(12),
    title: 'Sprint one',
    milestone_id: milestone.id,
    state: 'planned',
    started_at: null,
    started_by: null,
    completed_at: null,
    completed_by: null,
  }
  const task: WorkTask = {
    ...milestone,
    id: id(13),
    title: 'Task one',
    milestone_id: milestone.id,
    sprint_id: sprint.id,
    type: 'task',
    priority: 'medium',
    state: options.state ?? 'backlog',
    assignee_agent_id: null,
    plan: 'original plan',
  }
  const base = `/api/v1/projects/${project.id}`
  let intercept: Fetch | undefined
  const summary = <T extends WorkMilestone>(value: T) => {
    const { description: _description, ...rest } = value
    return rest
  }
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return response(account)
    if (path.startsWith('/api/v1/projects/resolve?') || path === base) return response(project)
    if (path.startsWith(`${base}/milestones?`)) return response({ items: [summary(milestone)] })
    if (path === `${base}/milestones/${milestone.id}` && init.method === 'GET')
      return response(milestone)
    if (path === `${base}/sprints/${sprint.id}`) return response(sprint)
    if (path === `${base}/tasks/${task.id}`) return response(task)
    if (path.startsWith(`${base}/sprints?`) || path.startsWith(`${base}/tasks?`))
      return response({ items: [] })
    if (path === `${base}/milestones/${milestone.id}` && init.method === 'PATCH') {
      const body = JSON.parse(init.body as string) as {
        request: { title?: string; description?: string }
      }
      milestone = { ...milestone, ...body.request, version: String(BigInt(milestone.version) + 1n) }
      return response({
        command: 'work.milestone.update',
        changed: true,
        milestone,
        sprint: null,
        event_id: id(20),
      })
    }
    throw new Error(`Unexpected fixture endpoint: ${init.method} ${path}`)
  }
  const fetcher = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const auth = createSessionController(
    createAccountAPI(fetcher),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetcher),
    undefined,
    undefined,
    createWorkPlanningAPI(fetcher),
  )
  await auth.restore()
  const replacements: string[] = []
  const workspace = createProjectWorkspace(auth)
  const work = createProjectWorkPlanning(auth, workspace, async (path) => {
    replacements.push(path)
    workspace.afterNavigation(path)
    work.afterNavigation(path)
    await flushPromises()
  })
  cleanup.push(() => {
    work.dispose()
    workspace.dispose()
    auth.leave()
  })
  async function open(suffix = `/tasks/explore/milestones/${milestone.id}`) {
    workspace.afterNavigation(home + suffix)
    work.afterNavigation(home + suffix)
    await flushPromises()
    await flushPromises()
  }
  return {
    auth,
    workspace,
    work,
    project,
    sprint,
    task,
    fetcher,
    normal,
    base,
    replacements,
    open,
    milestone: () => milestone,
    changeMilestone(value: Partial<WorkMilestone>) {
      milestone = { ...milestone, ...value }
    },
    intercept(value?: Fetch) {
      intercept = value
    },
    changeSession() {
      account = {
        ...account,
        session: { ...account.session, id: id(3) },
        csrf_token: 'T'.repeat(43),
      }
    },
  }
}

describe('Work planning current objects and original intent', () => {
  it('finishes a created Sprint route and its parent reads before enabling original lookup', async () => {
    const f = await fixture()
    await f.open()
    let created: WorkSprint | null = null
    let releaseRead: (() => void) | undefined
    f.intercept(async (path, init) => {
      if (path === `${f.base}/sprints` && init.method === 'POST') {
        const input = JSON.parse(init.body as string).request
        created = {
          ...f.sprint,
          id: input.sprint_id,
          title: input.title,
          description: input.description ?? '',
        }
        return response({
          command: 'work.sprint.create',
          changed: true,
          milestone: null,
          sprint: created,
          event_id: id(20),
        })
      }
      if (created && path === `${f.base}/sprints/${created.id}`) {
        const value = created
        return new Promise<Response>((resolve) => {
          releaseRead = () => resolve(response(value))
        })
      }
      return f.normal(path, init)
    })
    await f.work.beginCreate('sprint')
    f.work.draft.title = 'Created Sprint'
    await f.work.save()
    await flushPromises()
    expect(f.work.progress.value?.phase).toBe('confirmed')
    expect(f.work.progress.value?.canLookup).toBe(false)
    expect(f.work.blocked.value).toBe(true)
    expect(releaseRead).toBeDefined()
    releaseRead!()
    await flushPromises()
    await flushPromises()
    expect(f.auth.workPlanning.progress?.phase).toBe('confirmed')
    expect(f.work.detail.sprint?.title).toBe('Created Sprint')
    expect(f.work.detail.milestone?.id).toBe(f.milestone().id)
    expect(f.work.blocked.value).toBe(false)
    expect(f.auth.workPlanning.progress?.canLookup).toBe(true)
    expect(f.work.progress.value?.canLookup).toBe(true)
  })

  it('invalidates an expired opaque cursor chain without silently falling back to page one', async () => {
    const f = await fixture()
    const rows = Array.from({ length: 50 }, (_, n) => ({
      id: id(100 + n),
      project_id: f.project.id,
      title: 'row ' + n,
      manual_rank: (n + 1).toString(16).padStart(32, '0'),
      version: '1',
      created_at: at,
      updated_at: at,
    }))
    f.intercept((path, init) =>
      path.startsWith(f.base + '/milestones?')
        ? Promise.resolve(
            new URL(path, 'https://unit.test').searchParams.has('cursor')
              ? reject('CURSOR_STALE')
              : response({ items: rows, next_cursor: 'opaque-first' }),
          )
        : f.normal(path, init),
    )
    await f.open()
    expect(f.work.milestones.items).toHaveLength(50)
    await f.work.loadMilestones('next')
    expect(f.work.milestones.invalid).toBe(true)
    expect(f.work.milestones.stale).toBe(true)
    expect(f.work.milestones.previous).toEqual([])
    expect(f.work.milestones.items).toHaveLength(50)
    const count = f.fetcher.mock.calls.length
    await f.work.loadMilestones('next')
    await flushPromises()
    expect(f.fetcher.mock.calls.length).toBe(count)
    await f.work.loadMilestones('first')
    expect(f.work.milestones.invalid).toBe(false)
  })
  it('loads a direct Task and its actual parents without fabricating an in-page node', async () => {
    const f = await fixture()
    await f.open(`/tasks/explore/tasks/${f.task.id}`)
    expect(f.work.detail.phase).toBe('ready')
    expect(f.work.detail.task?.id).toBe(f.task.id)
    expect(f.work.detail.sprint?.id).toBe(f.sprint.id)
    expect(f.work.draft.plan).toBe('original plan')
    expect(f.work.tasks.size).toBe(0)
    expect(f.work.editableTask.value).toBe(true)
  })
  it('selects the real Current pointer and never the first planned Sprint', async () => {
    const f = await fixture({ current: true })
    await f.open('/tasks/explore')
    expect(f.replacements).toEqual([`${home}/tasks/explore/sprints/${f.sprint.id}`])
    expect(f.work.detail.sprint?.id).toBe(f.sprint.id)
  })
  it('leaves no-Current selection empty', async () => {
    const f = await fixture()
    await f.open('/tasks/explore')
    expect(f.replacements).toEqual([])
    expect(f.work.detail.phase).toBe('empty')
    expect(f.work.detail.sprint).toBeNull()
  })
  it('keeps a directly addressed non-backlog Task read-only', async () => {
    const f = await fixture({ state: 'done' })
    await f.open(`/tasks/explore/tasks/${f.task.id}`)
    expect(f.work.detail.task?.state).toBe('done')
    expect(f.work.editorReadOnly.value).toBe(true)
    f.work.draft.title = 'not writable'
    await f.work.save()
    expect(f.fetcher.mock.calls.some(([, init]) => init.method === 'PATCH')).toBe(false)
  })
  it('requires explicit type and priority for a new Task before any write', async () => {
    const f = await fixture()
    await f.open(`/tasks/explore/sprints/${f.sprint.id}`)
    await f.work.beginCreate('task')
    f.work.draft.title = 'new'
    expect(f.work.draft.type).toBe('')
    expect(f.work.draft.priority).toBe('')
    await f.work.save()
    expect(f.work.editor.errors.type).toBeTruthy()
    expect(f.work.editor.errors.priority).toBeTruthy()
    expect(f.fetcher.mock.calls.some(([, init]) => init.method === 'POST')).toBe(false)
  })
  it('sends only changed fields and preserves raw whitespace; receipt and current read are separate', async () => {
    const f = await fixture()
    await f.open()
    f.work.draft.description = '  原文\n'
    await f.work.save()
    const write = f.fetcher.mock.calls.find(([, init]) => init.method === 'PATCH')!
    expect(JSON.parse(write[1].body as string)).toEqual({
      expected_version: '1',
      request: { description: '  原文\n' },
    })
    expect(f.work.progress.value?.phase).toBe('confirmed')
    expect(f.work.detail.milestone?.version).toBe('2')
    expect(f.work.dirty.value).toBe(false)
    expect(f.work.milestones.invalid).toBe(true)
  })
  it('retains version-conflicted text until a separate current read and explicit adoption', async () => {
    const f = await fixture()
    await f.open()
    f.work.draft.title = 'my draft'
    f.changeMilestone({ title: 'concurrent title', version: '2' })
    f.intercept((path, init) =>
      init.method === 'PATCH' ? Promise.resolve(reject('VERSION_CONFLICT')) : f.normal(path, init),
    )
    await f.work.save()
    expect(f.work.editor.conflict).toBe(true)
    expect(f.work.editor.version).toBe('1')
    await f.work.loadSelection()
    expect(f.work.detail.milestone?.title).toBe('concurrent title')
    expect(f.work.draft.title).toBe('my draft')
    const adoption = f.work.adoptCurrent()
    expect(f.work.confirmation.open).toBe(true)
    f.work.confirm()
    await adoption
    expect(f.work.draft.title).toBe('concurrent title')
    expect(f.work.editor.version).toBe('2')
  })
  it('does not clear an uncertain command using a same-value Get or a canceled leave', async () => {
    const f = await fixture()
    await f.open()
    f.work.draft.title = 'saved maybe'
    f.intercept(async (path, init) => {
      if (init.method === 'PATCH') {
        await f.normal(path, init)
        throw new TypeError('response lost')
      }
      return f.normal(path, init)
    })
    await f.work.save()
    expect(f.work.progress.value?.phase).toBe('uncertain')
    await f.work.loadSelection()
    expect(f.work.detail.milestone?.title).toBe('saved maybe')
    expect(f.work.progress.value?.phase).toBe('uncertain')
    const leaving = f.work.confirmLeave('/projects')
    f.work.cancelConfirmation()
    expect(await leaving).toBe(false)
    expect(f.work.draft.title).toBe('saved maybe')
    expect(f.work.progress.value?.phase).toBe('uncertain')
  })
  it('preserves the draft through same-session checking and destroys it after a different Session', async () => {
    const f = await fixture()
    await f.open()
    f.work.draft.title = 'keep privately'
    await f.auth.restore()
    await flushPromises()
    expect(f.work.draft.title).toBe('keep privately')
    f.changeSession()
    await f.auth.restore()
    await flushPromises()
    expect(f.work.draft.title).not.toBe('keep privately')
    expect(f.work.progress.value).toBeNull()
  })
  it('requests an unfiltered Task reorder group with the exact current Sprint/state/priority', async () => {
    const f = await fixture()
    await f.open(`/tasks/explore/tasks/${f.task.id}`)
    f.work.filters.text = 'hidden'
    f.work.filters.type = 'bug'
    await f.work.openReorder()
    const path = f.fetcher.mock.calls
      .map(([url]) => url)
      .find((url) => url.startsWith(`${f.base}/tasks?`))!
    const query = new URL(path, 'https://unit.test').searchParams
    expect(query.get('sprint_id')).toBe(f.sprint.id)
    expect(query.get('state')).toBe('backlog')
    expect(query.get('priority')).toBe('medium')
    expect(query.has('text')).toBe(false)
    expect(query.has('type')).toBe(false)
    expect(query.has('assignee_agent_id')).toBe(false)
  })
  it('reads a dependency through current authority and clears that detail when its page changes', async () => {
    const f = await fixture()
    await f.open(`/tasks/explore/tasks/${f.task.id}`)
    const related = { ...f.task, id: id(42), title: 'Actual related task', state: 'done' as const }
    f.intercept(async (path, init) => {
      if (path.includes(`/tasks/${f.task.id}/blockers?`))
        return response({
          items: [
            {
              id: id(43),
              project_id: f.task.project_id,
              task_id: f.task.id,
              type: 'rely_on',
              metadata: { related_task_id: related.id },
              description: '',
              created_at: at,
              created_by: { type: 'human', user_id: id(1), source: 'task_domain' },
              resolved_at: null,
              resolved_by: null,
              resolution_comment: null,
            },
          ],
        })
      if (path === `${f.base}/tasks/${related.id}`) return response(related)
      return f.normal(path, init)
    })
    await f.work.loadBlockers()
    expect(f.work.relatedTasks.size).toBe(0)
    await f.work.loadRelatedTask(related.id)
    expect(f.work.relatedTasks.get(related.id)?.task?.title).toBe(related.title)
    expect(f.work.relatedTasks.get(related.id)?.task?.state).toBe('done')
    expect(f.work.detail.task?.id).toBe(f.task.id)
    await f.work.loadBlockers()
    expect(f.work.relatedTasks.size).toBe(0)
    const count = f.fetcher.mock.calls.length
    await f.work.loadRelatedTask(id(44))
    expect(f.fetcher.mock.calls.length).toBe(count)
  })
})
