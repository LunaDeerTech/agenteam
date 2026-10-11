import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import { type Fetch, type Problem } from '../api/client'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createWorkMilestonePlanningAPI } from '../api/work-milestone-planning'
import { createWorkSprintPlanningAPI } from '../api/work-sprint-planning'
import { createWorkReviewAPI, type Milestone } from '../api/work-review'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  m = id(2),
  s = id(5),
  other = id(7),
  at = '2026-10-11T10:00:00.000001Z'
const projectPath = `/api/v1/projects/${p}`,
  sprintPath = `${projectPath}/sprints/${s}`
const session: SessionView = {
  user: {
    id: id(3),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(4), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
  csrf_token: 'S'.repeat(43),
}
const project: Project = {
  id: p,
  owner_user_id: id(3),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
}
const milestone = (target = m, title = 'Current'): Milestone => ({
  id: target,
  project_id: p,
  title,
  description: 'Private description',
  manual_rank: '8'.repeat(32),
  version: '9007199254740993',
  created_at: at,
  updated_at: at,
})
const sprint = (target = s) => ({
  ...milestone(),
  id: target,
  milestone_id: m,
  title: 'Planned Sprint',
  state: 'planned' as const,
  started_at: null,
  started_by: null,
  completed_at: null,
  completed_by: null,
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (status: number, code: string, state: Problem['commit_state'] = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status,
      detail: '',
      instance: sprintPath,
      code,
      request_id: id(99),
      commit_state: state,
    },
    status,
  )
const tasks: ProjectTasks[] = [],
  workspaces: ProjectWorkspace[] = [],
  sessions: SessionController[] = []
afterEach(async () => {
  tasks.splice(0).forEach((v) => v.dispose())
  workspaces.splice(0).forEach((v) => v.dispose())
  sessions.splice(0).forEach((v) => v.leave())
  await flushPromises()
})
async function fixture() {
  let deniedPath = '',
    status = 404,
    updateCode = 'COMMIT_UNKNOWN'
  let restoreResponse: Promise<Response> | null = null
  let currentSession = session
  let heldUpdate: Promise<Response> | null = null
  let created = milestone(id(8), 'Next operation')
  const fetch = vi.fn<Fetch>(async (path, init) => {
    const url = new URL(path, 'https://owned.invalid')
    if (path === '/api/v1/session') return restoreResponse ?? json(currentSession)
    if (init.method === 'GET' && url.pathname === deniedPath)
      return problem(status, status === 503 ? 'SERVICE_UNAVAILABLE' : 'NOT_FOUND')
    if (path.startsWith('/api/v1/projects/resolve?') || path === projectPath) return json(project)
    if (path === sprintPath && init.method === 'PATCH')
      return (
        heldUpdate ??
        problem(409, updateCode, updateCode === 'COMMIT_UNKNOWN' ? 'unknown' : 'not_committed')
      )
    if (url.pathname === `${projectPath}/milestones` && init.method === 'POST') {
      const request = JSON.parse(init.body as string).request
      created = {
        ...milestone(request.milestone_id, request.title),
        version: '1',
        description: request.description ?? '',
      }
      return json({
        command: 'work.milestone.create',
        changed: true,
        milestone: created,
        sprint: null,
        event_id: id(9),
      })
    }
    if (url.pathname === `${projectPath}/milestones`)
      return json({
        items: [milestone(), milestone(other)].map(
          ({ description: _description, ...summary }) => summary,
        ),
      })
    if (url.pathname.startsWith(`${projectPath}/milestones/`))
      return json(
        url.pathname.endsWith(created.id) ? created : milestone(url.pathname.split('/').at(-1)),
      )
    if (url.pathname === `${projectPath}/sprints`)
      return json({
        items: [sprint(), sprint(other)].map(
          ({
            description: _d,
            started_at: _sa,
            started_by: _sb,
            completed_at: _ca,
            completed_by: _cb,
            ...summary
          }) => summary,
        ),
      })
    if (url.pathname.startsWith(`${projectPath}/sprints/`))
      return json(sprint(url.pathname.split('/').at(-1)))
    if (url.pathname === `${projectPath}/agents` || url.pathname === `${projectPath}/tasks`)
      return json({ items: [] })
    throw new Error(`Unexpected ${init.method} ${path}`)
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[16] = createWorkReviewAPI(fetch)
  args[17] = createAgentDirectoryAPI(fetch)
  args[19] = {
    milestonePlanning: createWorkMilestonePlanningAPI(fetch),
    sprintPlanning: createWorkSprintPlanningAPI(fetch),
  }
  const auth = createSessionController(...args)
  sessions.push(auth)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspaces.push(workspace)
  workspace.afterNavigation(`/owner/demo/tasks/sprints/${s}`)
  await flushPromises()
  expect(workspace.detail.phase).toBe('current')
  const owner = createProjectTasks(auth, workspace, async (to) => {
    if (!(await owner.confirmLeave(to))) return
    workspace.afterNavigation(to)
    owner.afterNavigation(to)
  })
  tasks.push(owner)
  owner.afterNavigation(`/owner/demo/tasks/sprints/${s}`)
  await flushPromises()
  expect(owner.state.phase).toBe('ready')
  owner.openSprintEdit()
  owner.touchSprintEdit('title', 'Private draft')
  expect(owner.sprintEdit.title).toBe('Private draft')
  return {
    auth,
    workspace,
    owner,
    fetch,
    deny(path: string, value = 404) {
      deniedPath = path
      status = value
    },
    changeSession() {
      currentSession = { ...session, session: { ...session.session, id: id(40) } }
    },
    holdUpdate() {
      let release!: (response: Response) => void
      heldUpdate = new Promise((resolve) => {
        release = resolve
      })
      return () =>
        release(
          json({
            command: 'work.sprint.update',
            changed: true,
            milestone: null,
            sprint: { ...sprint(), title: 'Private draft', version: '9007199254740994' },
            event_id: id(41),
          }),
        )
    },
    reject() {
      updateCode = 'VERSION_CONFLICT'
    },
    holdRestore() {
      let release!: (response: Response) => void
      restoreResponse = new Promise((resolve) => {
        release = resolve
      })
      return () => {
        restoreResponse = null
        release(json(currentSession))
      }
    },
    writes: () => fetch.mock.calls.filter(([, init]) => init.method === 'PATCH'),
    creates: () =>
      fetch.mock.calls.filter(
        ([path, init]) => path === `${projectPath}/milestones` && init.method === 'POST',
      ),
  }
}

describe('Sprint edit recovery through the real Session, Workspace and Tasks controllers', () => {
  it.each([403, 404])(
    'forgets this target draft and original Unknown after its normal refresh returns %s',
    async (status) => {
      const f = await fixture()
      await f.owner.submitSprintEdit()
      expect(f.auth.workSprintEditing.progress?.phase).toBe('uncertain')
      f.deny(sprintPath, status)
      await f.owner.refresh()
      expect(f.owner.state.phase).toBe('error')
      expect(f.owner.sprintEdit.title).toBe('')
      expect(f.auth.workSprintEditing.progress).toBeNull()
      expect(f.writes()).toHaveLength(1)
    },
  )
  it('forgets this Project draft and Unknown after an actual Workspace Owner read is refused', async () => {
    const f = await fixture()
    await f.owner.submitSprintEdit()
    f.deny(projectPath)
    await f.workspace.readCurrent()
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.workspace.currentReadContext.value).toBeNull()
    expect(f.owner.sprintEdit.title).toBe('')
    expect(f.auth.workSprintEditing.progress).toBeNull()
    expect(f.writes()).toHaveLength(1)
  })
  it('retains draft and original Unknown through an actual same-Session restore and Workspace reauthorization', async () => {
    const f = await fixture()
    await f.owner.submitSprintEdit()
    const release = f.holdRestore()
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.workspace.currentReadContext.value).toBeNull()
    expect(f.owner.sprintEdit.title).toBe('Private draft')
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    release()
    await checking
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('current')
    expect(f.owner.sprintEdit.title).toBe('Private draft')
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    expect(f.writes()).toHaveLength(1)
  })
  it('clears the original intent and draft after a real Session identity change', async () => {
    const f = await fixture()
    await f.owner.submitSprintEdit()
    f.changeSession()
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.workSprintEditing.progress).toBeNull()
    expect(f.owner.sprintEdit.title).toBe('')
    expect(f.writes()).toHaveLength(1)
  })
  it('does not publish a late confirmed receipt or read it into a different Sprint', async () => {
    const f = await fixture()
    const release = f.holdUpdate()
    const saving = f.owner.submitSprintEdit()
    await flushPromises()
    const target = `/owner/demo/tasks/sprints/${other}`
    const navigation = f.owner.confirmLeave(target)
    f.owner.confirm()
    expect(await navigation).toBe(true)
    f.workspace.afterNavigation(target)
    f.owner.afterNavigation(target)
    release()
    await saving
    await flushPromises()
    expect(f.auth.workSprintEditing.progress).toMatchObject({ phase: 'confirmed', sprintID: s })
    expect(f.owner.state.sprint?.id).toBe(other)
    expect(f.owner.state.sprint?.title).toBe('Planned Sprint')
    expect(f.owner.sprintEdit.needsRead).toBe(false)
    expect(f.writes()).toHaveLength(1)
  })
  it.each(['target', 'project'])(
    'retains draft and Unknown when the %s current read returns 503',
    async (scope) => {
      const f = await fixture()
      await f.owner.submitSprintEdit()
      f.deny(scope === 'target' ? sprintPath : projectPath, 503)
      if (scope === 'target') await f.owner.refresh()
      else await f.workspace.readCurrent()
      await flushPromises()
      expect(f.owner.sprintEdit.title).toBe('Private draft')
      expect(f.auth.workSprintEditing.progress?.phase).toBe('uncertain')
      expect(f.writes()).toHaveLength(1)
    },
  )
  it('preserves the original Unknown after confirmed navigation and a different Sprint refusal', async () => {
    const f = await fixture()
    await f.owner.submitSprintEdit()
    f.deny(`${projectPath}/sprints/${other}`)
    const navigation = f.owner.selectSprint(other)
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.confirm()
    await navigation
    await flushPromises()
    expect(f.owner.state.phase).toBe('error')
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      sprintID: s,
    })
    expect(f.writes()).toHaveLength(1)
  })
  it('preserves the old Project Unknown when navigation to another Project is refused', async () => {
    const f = await fixture()
    await f.owner.submitSprintEdit()
    const navigation = f.owner.confirmLeave('/owner/other/tasks')
    f.owner.confirm()
    expect(await navigation).toBe(true)
    f.deny('/api/v1/projects/resolve')
    f.workspace.afterNavigation('/owner/other/tasks')
    f.owner.afterNavigation('/owner/other/tasks')
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('unavailable')
    expect(f.auth.workSprintEditing.progress).toMatchObject({
      phase: 'uncertain',
      projectID: p,
      sprintID: s,
    })
    expect(f.writes()).toHaveLength(1)
  })
  it.each(['selection', 'route'])(
    'releases a definite rejection after explicit %s abandonment, allowing another authorized command',
    async (navigationKind) => {
      const f = await fixture()
      f.reject()
      await f.owner.submitSprintEdit()
      expect(f.auth.workSprintEditing.progress?.phase).toBe('rejected')
      const navigation =
        navigationKind === 'selection'
          ? f.owner.selectSprint(other)
          : f.owner.confirmLeave('/owner/demo/settings/general')
      expect(f.owner.confirmation.open).toBe(true)
      f.owner.confirm()
      await navigation
      await flushPromises()
      if (navigationKind === 'route') {
        for (const path of ['/owner/demo/settings/general', '/owner/demo/tasks']) {
          f.workspace.afterNavigation(path)
          f.owner.afterNavigation(path)
          await flushPromises()
        }
      }
      expect(f.owner.canCreateMilestone.value).toBe(true)
      f.owner.openMilestoneCreation()
      f.owner.milestoneCreation.title = 'Next operation'
      await f.owner.submitMilestoneCreation()
      expect(f.creates()).toHaveLength(1)
      expect(f.auth.workMilestonePlanning.progress?.phase).toBe('confirmed')
      expect(f.auth.workSprintEditing.progress).toBeNull()
      expect(f.writes()).toHaveLength(1)
    },
  )
})
