import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import {
  createWorkReviewAPI,
  type ReviewInput,
  type ReadyInput,
  type Task,
} from '../api/work-review'
import {
  createWorkTaskPlanningAPI,
  type TaskCreateInput,
  type TaskCreateReceipt,
} from '../api/work-task-planning'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  createProjectTasks,
  projectTasksKey,
  type ProjectTasks,
} from '../composables/useProjectTasks'
import ProjectTasksView from '../views/projects/ProjectTasksView.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const projectID = id(10),
  milestoneID = id(11),
  sprintID = id(12),
  taskID = id(20)
const workerID = id(30),
  reviewerID = id(31)
const prefix = `/api/v1/projects/${projectID}`
const transferPath = `${prefix}/tasks/${taskID}/transfer`
const lookupPath = `${prefix}/task-transition-commands/lookup`
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
const milestone = { ...base(milestoneID, '发布里程碑'), description: '里程碑说明' }
const sprint = {
  ...base(sprintID, '当前迭代'),
  milestone_id: milestoneID,
  state: 'current',
  description: '迭代说明',
  started_at: time,
  started_by: { kind: 'human', user_id: id(1) },
  completed_at: null,
  completed_by: null,
}
const agents = [
  { id: reviewerID, name: 'review-agent', display_name: '评审员' },
  { id: workerID, name: 'work-agent', display_name: '执行员' },
].map((agent) => ({
  ...agent,
  project_id: projectID,
  tag_color: null,
  description: '',
  version: '1',
  created_at: time,
  updated_at: time,
}))
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
const mounted: VueWrapper[] = []
const owners: {
  tasks: ProjectTasks
  workspace: ReturnType<typeof createProjectWorkspace>
  auth: SessionController
}[] = []

beforeEach(() => {
  // jsdom has no layout; retain the real Ui layer's focus and Teleport behavior.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount())
  owners.splice(0).forEach(({ tasks, workspace, auth }) => {
    tasks.dispose()
    workspace.dispose()
    auth.leave()
  })
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function page(state: Task['state'] = 'in_progress', empty = false, blocked = false) {
  // Controlled HTTP facts feed the production codecs, Session owner and page
  // owner. These tests make no claim about server authorization or SQL writes.
  let current: Task = {
    ...base(taskID, '核对任务边界'),
    milestone_id: milestoneID,
    sprint_id: sprintID,
    description: '保留当前任务描述',
    plan: '保留当前执行计划',
    type: 'task',
    priority: 'high',
    state,
    assignee_agent_id: state === 'in_progress' ? workerID : reviewerID,
    version: '4',
  }
  let uncertain = false
  let hasTask = !empty
  let creationLost = false
  let creationObservation: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
  let creationReceipt: TaskCreateReceipt | undefined
  let blockers: 'none' | 'unresolved' | 'unavailable' | 'resolved' = blocked ? 'unresolved' : 'none'
  let observation: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
  let original: ReviewInput | ReadyInput | undefined
  const unexpected: string[] = []
  const commit = (input: ReviewInput | ReadyInput) => {
    current = {
      ...current,
      state: input.request.target_state,
      assignee_agent_id: input.request.assignee_agent_id ?? current.assignee_agent_id,
      version: String(BigInt(input.expected_version) + 1n),
    }
    return { task: current, task_event_ids: [id(40), id(41)], event_ids: [id(42)] }
  }
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    const url = new URL(path, 'https://example.test'),
      route = url.pathname
    if (route === '/api/v1/session') return json(session)
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
        items: [{ ...base(sprintID, sprint.title), milestone_id: milestoneID, state: 'current' }],
      })
    if (route === `${prefix}/sprints/${sprintID}`) return json(sprint)
    if (route === `${prefix}/agents`) return json({ items: agents })
    if (route === `${prefix}/agents/${workerID}`) return json(agents[1])
    if (route === `${prefix}/agents/${reviewerID}`) return json(agents[0])
    if (route === `${prefix}/tasks/${current.id}`) return json(current)
    if (route === `${prefix}/tasks/${current.id}/blockers`) {
      if (blockers === 'unavailable')
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Unavailable',
            detail: '',
            instance: route,
            status: 503,
            code: 'DEPENDENCY_UNAVAILABLE',
            request_id: id(99),
            commit_state: 'not_started',
          },
          503,
        )
      return json({
        items:
          blockers === 'none'
            ? []
            : [
                {
                  id: id(70),
                  project_id: projectID,
                  task_id: current.id,
                  type: 'waiting_for_human',
                  metadata: {},
                  description: '等待负责人确认',
                  created_at: time,
                  created_by: { type: 'human', user_id: id(1), source: 'task_domain' },
                  resolved_at: blockers === 'resolved' ? time : null,
                  resolved_by:
                    blockers === 'resolved'
                      ? { type: 'human', user_id: id(1), source: 'task_domain' }
                      : null,
                  resolution_comment: null,
                },
              ],
      })
    }
    if (route === `${prefix}/tasks` && init.method === 'POST') {
      const input = JSON.parse(init.body as string) as TaskCreateInput
      const { task_id, ...fields } = input.request
      current = {
        ...current,
        ...fields,
        id: task_id,
        state: 'backlog',
        assignee_agent_id: null,
        version: '1',
      }
      hasTask = true
      creationReceipt = { task: current, changed: true, task_event_id: id(43), event_ids: [id(44)] }
      if (creationLost)
        return new Response(
          new ReadableStream({
            start(controller) {
              controller.error(new TypeError('receipt interrupted'))
            },
          }),
          {
            status: 200,
            headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
          },
        )
      return json(creationReceipt)
    }
    if (route === `${prefix}/task-commands/lookup`)
      return json({
        status: creationObservation,
        receipt: creationObservation === 'committed' ? creationReceipt : null,
      })
    if (route === `${prefix}/tasks`) {
      const { description: _description, plan: _plan, ...summary } = current
      return json({
        items: hasTask && url.searchParams.get('state') === current.state ? [summary] : [],
      })
    }
    if (route === `${prefix}/tasks/${current.id}/transfer`) {
      original = JSON.parse(init.body as string) as ReviewInput | ReadyInput
      if (uncertain)
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Unknown',
            detail: '',
            instance: transferPath,
            status: 503,
            code: 'COMMIT_UNKNOWN',
            request_id: id(99),
            commit_state: 'unknown',
          },
          503,
        )
      return json(commit(original))
    }
    if (route === lookupPath)
      return json({
        status: observation,
        receipt: observation === 'committed' ? commit(original!) : null,
      })
    unexpected.push(path)
    throw new Error('Unexpected page endpoint')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[12] = createProjectOwnerAPI(fetcher)
  args[16] = createWorkReviewAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  args[18] = createWorkTaskPlanningAPI(fetcher)
  const auth = createSessionController(...args)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  const tasks = createProjectTasks(auth, workspace, async (path) => {
    workspace.afterNavigation(path)
    tasks.afterNavigation(path)
  })
  owners.push({ auth, workspace, tasks })
  const path = empty ? `/owner/demo/tasks/sprints/${sprintID}` : `/owner/demo/tasks/${taskID}`
  workspace.afterNavigation(path)
  tasks.afterNavigation(path)
  await flushPromises()
  const wrapper = mount(ProjectTasksView, {
    attachTo: document.body,
    global: { provide: { [projectTasksKey as symbol]: tasks } },
  })
  mounted.push(wrapper)
  await flushPromises()
  expect(unexpected).toEqual([])
  expect(tasks.state.phase).toBe('ready')
  expect(tasks.busy.value).toBe(false)
  return {
    fetcher,
    unknown() {
      uncertain = true
    },
    observe(value: typeof observation) {
      observation = value
    },
    blockers(value: typeof blockers) {
      blockers = value
    },
    loseCreation() {
      creationLost = true
    },
    observeCreation(value: typeof creationObservation) {
      creationObservation = value
    },
    creates: () =>
      fetcher.mock.calls.filter(
        ([path, init]) => path === `${prefix}/tasks` && init.method === 'POST',
      ),
    creationLookups: () =>
      fetcher.mock.calls.filter(([path]) => path === `${prefix}/task-commands/lookup`),
    transfers: () =>
      fetcher.mock.calls.filter(
        ([path]) => path.startsWith(`${prefix}/tasks/`) && path.endsWith('/transfer'),
      ),
    lookups: () => fetcher.mock.calls.filter(([path]) => path === lookupPath),
  }
}
const body = () => new DOMWrapper(document.body)
function buttonLabel(item: DOMWrapper<HTMLButtonElement>) {
  const element = item.element.cloneNode(true) as HTMLButtonElement
  for (const hidden of element.querySelectorAll('[aria-hidden="true"]')) hidden.remove()
  return element.getAttribute('aria-label') ?? element.textContent?.trim() ?? ''
}
function button(label: string) {
  const result = body()
    .findAll<HTMLButtonElement>('button')
    .find((item) => buttonLabel(item) === label)
  expect(result, `missing button: ${label}`).toBeDefined()
  return result!
}
async function click(label: string) {
  await button(label).trigger('click')
  await flushPromises()
}
async function selectAgent(label: string, option: string) {
  await body().get(`button[aria-label="${label}"]`).trigger('click')
  await flushPromises()
  const selected = body()
    .findAll('[role="option"]')
    .find((item) => item.text() === option)
  expect(selected).toBeDefined()
  await selected!.trigger('click')
  await flushPromises()
}

describe('Human review page with real UI controls', () => {
  it('creates a confirmed backlog first and requires a second explicit Agent choice before todo', async () => {
    const f = await page('in_progress', true)
    await click('新建任务')
    const form = body().get('form[aria-label="新建任务"]')
    expect(document.activeElement).toBe(form.get('input').element)
    await click('取消创建')
    expect(document.activeElement).toBe(button('新建任务').element)
    await click('新建任务')
    expect(button('确认创建任务').element.disabled).toBe(true)
    expect(f.creates()).toHaveLength(0)
    await body().get('form[aria-label="新建任务"] input').setValue('用户明确创建的任务')
    await selectAgent('任务类型', '任务')
    expect(button('确认创建任务').element.disabled).toBe(true)
    await selectAgent('优先级', '高')
    const fields = body().findAll('form[aria-label="新建任务"] textarea')
    await fields[0]!.setValue('原始任务描述')
    await fields[1]!.setValue('原始执行计划')
    await click('确认创建任务')
    expect(f.creates()).toHaveLength(1)
    expect(f.creationLookups()).toHaveLength(0)
    expect(f.transfers()).toHaveLength(0)
    const original = f.creates()[0]![1]
    const input = JSON.parse(original.body as string) as TaskCreateInput
    expect(input.request).toEqual({
      task_id: input.request.task_id,
      sprint_id: sprintID,
      title: '用户明确创建的任务',
      description: '原始任务描述',
      plan: '原始执行计划',
      type: 'task',
      priority: 'high',
    })
    expect(body().get('[role="dialog"]').text()).toContain('待规划')
    expect(body().get('[role="dialog"]').text()).toContain('未指派')
    await click('指派并加入待执行')
    expect(button('确认指派').element.disabled).toBe(true)
    expect(body().get('button[aria-label="执行 Agent"]').text()).toContain('请选择')
    await selectAgent('执行 Agent', '执行员 (work-agent)')
    expect(body().get<HTMLTextAreaElement>('[role="dialog"] textarea').element.value).toBe('')
    expect(button('确认指派').element.disabled).toBe(false)
    await click('确认指派')
    expect(f.transfers()).toHaveLength(1)
    const ready = f.transfers()[0]![1]
    expect(JSON.parse(ready.body as string)).toEqual({
      expected_version: '1',
      request: { target_state: 'todo', assignee_agent_id: workerID },
    })
    expect(new Headers(ready.headers).get('Idempotency-Key')).not.toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
    expect(body().get('[role="dialog"]').text()).toContain('待执行')
    expect(body().get('[role="dialog"]').text()).toContain('执行员')
  })

  it('recovers a lost creation receipt on the Sprint before any Task detail or ready action', async () => {
    const f = await page('in_progress', true)
    f.loseCreation()
    await click('新建任务')
    await body().get('form[aria-label="新建任务"] input').setValue('待确认的原任务')
    await selectAgent('任务类型', '任务')
    await selectAgent('优先级', '中')
    await click('确认创建任务')
    const original = f.creates()[0]![1]
    const input = JSON.parse(original.body as string) as TaskCreateInput
    expect(body().get('[aria-label="创建结果恢复"]').text()).toContain(
      '创建结果待确认。请查询原创建结果，不要重复创建。',
    )
    expect(body().find('[role="dialog"]').exists()).toBe(false)
    expect(body().find('form[aria-label="新建任务"]').exists()).toBe(false)
    expect(
      f.fetcher.mock.calls.filter(([path]) => path === `${prefix}/tasks/${input.request.task_id}`),
    ).toHaveLength(0)
    expect(f.creationLookups()).toHaveLength(0)
    await click('查询原创建结果')
    expect(body().text()).toContain('尚未观察到原创建结果')
    f.observeCreation('in_progress')
    await click('查询原创建结果')
    expect(body().text()).toContain('原创建仍在处理中')
    expect(f.transfers()).toHaveLength(0)
    f.observeCreation('committed')
    await click('查询原创建结果')
    expect(body().get('[role="dialog"]').text()).toContain('待规划')
    expect(f.creates()).toHaveLength(1)
    expect(f.creationLookups()).toHaveLength(3)
    for (const [, request] of f.creationLookups()) {
      expect(JSON.parse(request.body as string)).toEqual({ command: 'work.task.create', ...input })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
    await click('指派并加入待执行')
    await selectAgent('执行 Agent', '执行员 (work-agent)')
    await click('确认指派')
    expect(f.transfers()).toHaveLength(1)
    expect(body().get('[role="dialog"]').text()).toContain('待执行')
  })

  it('requires an explicit reviewer and comment before submitting for review', async () => {
    const f = await page('in_progress', false, true)
    expect(body().get('[role="dialog"]').text()).toContain('执行员')
    expect(body().get('[role="dialog"]').text()).toContain('等待负责人确认')
    await click('提交评审')
    expect(body().get('button[aria-label="评审 Agent"]').text()).toContain('请选择')
    expect(button('确认提交评审').element.disabled).toBe(true)
    await body().get('textarea').setValue('  请复核边界\n保留原说明。  ')
    expect(button('确认提交评审').element.disabled).toBe(true)
    await selectAgent('评审 Agent', '评审员 (review-agent)')
    expect(button('确认提交评审').element.disabled).toBe(false)
    await click('确认提交评审')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: {
        target_state: 'in_review',
        assignee_agent_id: reviewerID,
        comment: '  请复核边界\n保留原说明。  ',
      },
    })
    expect(body().get('[role="dialog"]').text()).toContain('评审中')
    expect(button('接受并完成').exists()).toBe(true)
  })

  it('returns review to todo with an explicitly selected execution Agent', async () => {
    const f = await page('in_review', false, true)
    await click('退回修改')
    expect(body().get('button[aria-label="执行 Agent"]').text()).toContain('请选择')
    await selectAgent('执行 Agent', '执行员 (work-agent)')
    expect(button('确认退回修改').element.disabled).toBe(true)
    await body().get('textarea').setValue('请补充回归检查。')
    expect(button('确认退回修改').element.disabled).toBe(true)
    f.blockers('unavailable')
    await click('重新读取任务')
    expect(body().text()).toContain('未取得完整阻塞信息')
    expect(button('确认退回修改').element.disabled).toBe(true)
    f.blockers('resolved')
    await click('重新读取阻塞')
    expect(button('确认退回修改').element.disabled).toBe(false)
    await click('确认退回修改')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { target_state: 'todo', assignee_agent_id: workerID, comment: '请补充回归检查。' },
    })
    expect(body().get('[role="dialog"]').text()).toContain('待执行')
  })

  it('completes review without sending an assignee and renders the result read-only', async () => {
    const f = await page('in_review', false, true)
    await click('接受并完成')
    expect(body().find('button[aria-label="评审 Agent"]').exists()).toBe(false)
    expect(body().find('button[aria-label="执行 Agent"]').exists()).toBe(false)
    expect(button('确认接受并完成').element.disabled).toBe(true)
    await body().get('textarea').setValue('确认实现符合要求。')
    await click('确认接受并完成')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { target_state: 'done', comment: '确认实现符合要求。' },
    })
    const detail = body().get('[role="dialog"]')
    expect(detail.text()).toContain('此任务已结束，保留只读信息。')
    expect(detail.text()).toContain('保留当前任务描述')
    expect(detail.text()).toContain('保留当前执行计划')
    expect(detail.find('form').exists()).toBe(false)
    expect(
      detail
        .findAll<HTMLButtonElement>('button')
        .some((item) => ['提交评审', '接受并完成', '退回修改'].includes(buttonLabel(item))),
    ).toBe(false)
  })

  it('offers only explicit original Lookup while an Unknown transfer remains unsettled', async () => {
    const f = await page('in_review')
    f.unknown()
    await click('接受并完成')
    await body().get('textarea').setValue('原始确认说明')
    await click('确认接受并完成')
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(0)
    expect(body().get('[role="dialog"]').text()).toContain('提交结果待确认')
    expect(body().find('form').exists()).toBe(false)
    await click('查询原操作结果')
    expect(body().text()).toContain('尚未观察到原操作结果')
    f.observe('in_progress')
    await click('查询原操作结果')
    expect(body().text()).toContain('原操作仍在处理中')
    f.observe('committed')
    await click('查询原操作结果')
    expect(body().get('[role="dialog"]').text()).toContain('此任务已结束，保留只读信息。')
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    const original = f.transfers()[0]![1]
    for (const [, request] of f.lookups()) {
      expect(JSON.parse(request.body as string)).toEqual({
        command: 'work.task.transfer',
        target_id: taskID,
        ...JSON.parse(original.body as string),
      })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
  })

  it('keeps all seven empty state columns visible without inventing Task cards', async () => {
    const f = await page('in_progress', true)
    const board = body().get('[aria-label="任务看板"]')
    const columns = board.findAll('section')
    expect(columns.map((column) => column.attributes('aria-label'))).toEqual([
      '待规划',
      '待执行',
      '进行中',
      '评审中',
      '受阻',
      '已完成',
      '已取消',
    ])
    for (const column of columns) {
      expect(column.text()).toContain('暂无任务')
      expect(column.get('[aria-label="已读取任务数"]').text()).toBe('0')
      expect(column.find('button').exists()).toBe(false)
    }
    expect(body().find('[role="dialog"]').exists()).toBe(false)
    expect(f.transfers()).toHaveLength(0)
  })
})
