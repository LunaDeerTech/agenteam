import { vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import { createWorkReviewAPI } from '../api/work-review'
import type { Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectTasks, projectTasksKey } from '../composables/useProjectTasks'
import ProjectTasksView from '../views/projects/ProjectTasksView.vue'

export const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const projectID = id(10),
  milestoneID = id(11),
  sprintID = id(12)
export const selectedAgentID = id(100),
  otherAgentID = id(150)
export const prefix = `/api/v1/projects/${projectID}`
const time = '2026-10-13T10:00:00.000000Z'
const session: SessionView = {
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
const base = (resource: string, title: string) => ({
  id: resource,
  project_id: projectID,
  title,
  manual_rank: '8' + '0'.repeat(31),
  version: '1',
  created_at: time,
  updated_at: time,
})
const milestone = { ...base(milestoneID, '筛选工作'), description: '' }
const sprint = {
  ...base(sprintID, '当前迭代'),
  milestone_id: milestoneID,
  state: 'current',
  description: '',
  started_at: time,
  started_by: { kind: 'human', user_id: id(1) },
  completed_at: null,
  completed_by: null,
}
const agents = Array.from({ length: 51 }, (_, n) => ({
  id: id(150 - n),
  project_id: projectID,
  name: `worker-${150 - n}`,
  display_name: n === 50 ? '第二页 Agent' : null,
  tag_color: null,
  description: '',
  version: '1',
  created_at: time,
  updated_at: time,
}))
const tasks = Array.from({ length: 51 }, (_, n) => ({
  ...base(id(1000 + n), `任务 ${n} literal %_`),
  milestone_id: milestoneID,
  sprint_id: sprintID,
  state: 'todo',
  type: 'bug',
  priority: 'high',
  assignee_agent_id: selectedAgentID,
  manual_rank: (n + 1).toString(16).padStart(32, '0'),
}))
const other = {
  ...tasks[0]!,
  id: id(2000),
  title: '其他 Agent 任务',
  assignee_agent_id: otherAgentID,
  manual_rank: 'f' + '0'.repeat(31),
}
const unassigned = {
  ...tasks[0]!,
  id: id(2001),
  title: '未指派任务',
  state: 'backlog',
  assignee_agent_id: null,
}
const rows = [...tasks, other, unassigned]
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
const disposals: (() => void)[] = []
export function prepareAssigneeDOM() {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
}
export async function disposeAssigneeFixtures() {
  disposals.splice(0).forEach((dispose) => dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
}

// Controlled GET responses exercise the real codecs, Session, Workspace and
// Tasks controller. Server authorization and persistent facts remain native-test work.
export async function assigneeFixture(withView = false) {
  let directoryFailure = ''
  let projectDenied = false
  let directoryGate: Promise<void> | undefined
  let releaseDirectory: (() => void) | undefined
  const unexpected: string[] = []
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    const url = new URL(path, 'https://example.test'),
      route = url.pathname
    if (init.method !== 'GET') {
      unexpected.push(`${init.method} ${route}`)
      throw new Error('Unexpected mutation')
    }
    if (route === '/api/v1/session') return json(session)
    if (route === prefix && projectDenied)
      return json(
        {
          type: 'urn:agenteam:problem:test',
          title: 'Project unavailable',
          status: 403,
          detail: '',
          instance: route,
          code: 'FORBIDDEN',
          request_id: id(99),
          commit_state: 'not_started',
        },
        403,
      )
    if (route === '/api/v1/projects/resolve' || route === prefix)
      return json({
        id: projectID,
        owner_user_id: id(1),
        name: 'Demo',
        normalized_name: 'demo',
        description: '',
        lifecycle: 'active',
        version: '1',
        current_sprint_id: sprintID,
        created_at: time,
        updated_at: time,
        archived_at: null,
      })
    if (route === `${prefix}/milestones`)
      return json({ items: [base(milestoneID, milestone.title)] })
    if (route === `${prefix}/milestones/${milestoneID}`) return json(milestone)
    if (route === `${prefix}/sprints`)
      return json({
        items: [
          {
            ...base(sprintID, sprint.title),
            milestone_id: milestoneID,
            state: 'current',
          },
        ],
      })
    if (route === `${prefix}/sprints/${sprintID}`) return json(sprint)
    if (route === `${prefix}/agents`) {
      if (directoryGate) await directoryGate
      if (directoryFailure) {
        const code = directoryFailure
        directoryFailure = ''
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Directory unavailable',
            status: 400,
            detail: '',
            instance: route,
            code,
            request_id: id(99),
            commit_state: 'not_started',
          },
          400,
        )
      }
      return json(
        url.searchParams.has('cursor')
          ? { items: agents.slice(50) }
          : { items: agents.slice(0, 50), next_cursor: 'agents-page-2' },
      )
    }
    if (route.startsWith(`${prefix}/agents/`)) {
      const agent = agents.find((item) => route === `${prefix}/agents/${item.id}`)
      if (agent) return json(agent)
    }
    if (route === `${prefix}/tasks`) {
      const q = url.searchParams
      const filtered = rows.filter(
        (row) =>
          row.state === q.get('state') &&
          (!q.has('assignee_agent_id') ||
            row.assignee_agent_id ===
              (q.get('assignee_agent_id') === 'null' ? null : q.get('assignee_agent_id'))) &&
          (!q.has('priority') || row.priority === q.get('priority')) &&
          (!q.has('type') || row.type === q.get('type')) &&
          (!q.has('text') || row.title.includes(q.get('text')!)),
      )
      const start = q.has('cursor') ? 50 : 0
      return json({
        items: filtered.slice(start, start + 50),
        ...(filtered.length > start + 50 ? { next_cursor: 'tasks-page-2' } : {}),
      })
    }
    const row = rows.find((item) => route === `${prefix}/tasks/${item.id}`)
    if (row) return json({ ...row, description: '', plan: '' })
    if (/\/tasks\/[^/]+\/(blockers|events)$/.test(route)) return json({ items: [] })
    unexpected.push(route)
    throw new Error('Unexpected GET endpoint')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[12] = createProjectOwnerAPI(fetcher)
  args[16] = createWorkReviewAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  const auth = createSessionController(...args)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  const owner = createProjectTasks(auth, workspace, async (path) => {
    workspace.afterNavigation(path)
    owner.afterNavigation(path)
  })
  const route = '/owner/demo/tasks'
  workspace.afterNavigation(route)
  owner.afterNavigation(route)
  await flushPromises()
  let wrapper: VueWrapper | undefined
  if (withView) {
    wrapper = mount(ProjectTasksView, {
      attachTo: document.body,
      global: { provide: { [projectTasksKey as symbol]: owner } },
    })
    await flushPromises()
  }
  disposals.push(() => {
    releaseDirectory?.()
    wrapper?.unmount()
    owner.dispose()
    workspace.dispose()
    auth.leave()
  })
  return {
    auth,
    workspace,
    owner,
    wrapper,
    fetcher,
    unexpected,
    queries: () =>
      fetcher.mock.calls
        .filter(([path]) => new URL(path, 'https://example.test').pathname === `${prefix}/tasks`)
        .map(([path]) => Object.fromEntries(new URL(path, 'https://example.test').searchParams)),
    directoryQueries: () =>
      fetcher.mock.calls
        .filter(([path]) => new URL(path, 'https://example.test').pathname === `${prefix}/agents`)
        .map(([path]) => Object.fromEntries(new URL(path, 'https://example.test').searchParams)),
    failDirectory(code: string) {
      directoryFailure = code
    },
    denyProject() {
      projectDenied = true
    },
    holdDirectory() {
      directoryGate = new Promise<void>((resolve) => {
        releaseDirectory = resolve
      })
      return () => {
        releaseDirectory?.()
        directoryGate = undefined
      }
    },
  }
}
