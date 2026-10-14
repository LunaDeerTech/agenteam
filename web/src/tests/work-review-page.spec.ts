import { createWorkTaskTimelineAPI } from '../api/work-task-timeline'
import { createTaskExecutionsAPI } from '../api/task-executions'
import { createTaskExecutionOutputAPI } from '../api/task-execution-output'
import { multiRoundOutput } from './task-execution-output-fixture'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import {
  createWorkReviewAPI,
  type TransferInput,
  type CancelInput,
  type ReadyInput,
  type Task,
} from '../api/work-review'
import {
  createWorkTaskPlanningAPI,
  type TaskCreateInput,
  type TaskCreateReceipt,
} from '../api/work-task-planning'
import {
  createWorkTaskEditAPI,
  type TaskUpdateInput,
  type TaskUpdateReceipt,
} from '../api/work-task-edit'
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

async function page(
  state: Task['state'] = 'in_progress',
  empty = false,
  blocked = false,
  sprintState: 'planned' | 'current' | 'completed' = 'current',
) {
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
    assignee_agent_id: state === 'backlog' ? null : state === 'in_progress' ? workerID : reviewerID,
    version: '4',
  }
  let transferRejected = ''
  let uncertain = false
  let hasTask = !empty
  let creationLost = false
  let creationObservation: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
  let creationReceipt: TaskCreateReceipt | undefined
  let editLost = false
  let editObservation: 'not_observed' | 'committed' = 'not_observed'
  let editReceipt: TaskUpdateReceipt | undefined
  let blockers: 'none' | 'unresolved' | 'unavailable' | 'resolved' = blocked ? 'unresolved' : 'none'
  let observation: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
  let original: TransferInput | ReadyInput | undefined
  const executionResult = multiRoundOutput('已完成读取、写入与检查。\n结果：report.txt', {
    project_id: projectID,
    task_id: taskID,
    agent_id: workerID,
  })
  const unexpected: string[] = []
  const commit = (input: TransferInput | ReadyInput) => {
    if (
      'resolve_blocker_ids' in input.request &&
      input.request.resolve_blocker_ids.includes(id(70))
    )
      blockers = 'resolved'
    current = {
      ...current,
      state: input.request.target_state,
      assignee_agent_id:
        ('assignee_agent_id' in input.request ? input.request.assignee_agent_id : undefined) ??
        current.assignee_agent_id,
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
        items: [{ ...base(sprintID, sprint.title), milestone_id: milestoneID, state: sprintState }],
      })
    if (route === `${prefix}/sprints/${sprintID}`)
      return json({
        ...sprint,
        state: sprintState,
        started_at: sprintState === 'planned' ? null : time,
        started_by: sprintState === 'planned' ? null : sprint.started_by,
        completed_at: sprintState === 'completed' ? time : null,
        completed_by: sprintState === 'completed' ? sprint.started_by : null,
      })
    if (route === `${prefix}/agents`) return json({ items: agents })
    if (route === `${prefix}/agents/${workerID}`) return json(agents[1])
    if (route === `${prefix}/agents/${reviewerID}`) return json(agents[0])
    const executionPath = `${prefix}/tasks/${current.id}/executions`
    if (route === executionPath)
      return json({ items: [executionResult.execution], next_cursor: null })
    if (route === `${executionPath}/${executionResult.execution.id}`)
      return json({ execution: executionResult.execution })
    if (route === `${executionPath}/${executionResult.execution.id}/runtime-view`)
      return json(executionResult)
    if (route === `${prefix}/tasks/${current.id}` && init.method === 'PATCH') {
      const input = JSON.parse(init.body as string) as TaskUpdateInput
      const fields = Object.keys(input.request) as (keyof typeof input.request)[]
      const changed = fields.some((field) => current[field] !== input.request[field])
      current = {
        ...current,
        ...input.request,
        version: String(BigInt(input.expected_version) + (changed ? 1n : 0n)),
      }
      editReceipt = changed
        ? { task: current, changed: true, task_event_id: id(45), event_ids: [id(46)] }
        : { task: current, changed: false, task_event_id: null, event_ids: [] }
      if (editLost)
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Unknown',
            detail: '',
            instance: route,
            status: 503,
            code: 'COMMIT_UNKNOWN',
            request_id: id(99),
            commit_state: 'unknown',
          },
          503,
        )
      return json(editReceipt)
    }
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
    if (
      route === `${prefix}/task-commands/lookup` &&
      JSON.parse(init.body as string).command === 'work.task.update'
    )
      return json({
        status: editObservation,
        receipt: editObservation === 'committed' ? editReceipt : null,
      })
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
      original = JSON.parse(init.body as string) as TransferInput | ReadyInput
      if (transferRejected)
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Rejected',
            detail: '',
            instance: transferPath,
            status: 409,
            code: transferRejected,
            request_id: id(99),
            commit_state: 'not_committed',
          },
          409,
        )
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
    if (route === `${prefix}/tasks/${current.id}/events`) return json({ items: [] })
    unexpected.push(path)
    throw new Error('Unexpected page endpoint')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[12] = createProjectOwnerAPI(fetcher)
  args[16] = createWorkReviewAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  args[18] = createWorkTaskPlanningAPI(fetcher)
  args[19] = {
    edit: createWorkTaskEditAPI(fetcher),
    taskTimeline: createWorkTaskTimelineAPI(fetcher),
    taskExecutions: createTaskExecutionsAPI(fetcher),
    taskExecutionOutput: createTaskExecutionOutputAPI(fetcher),
  }
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
    tasks,
    changeTask() {
      current = { ...current, version: String(BigInt(current.version) + 1n) }
    },
    rejectTransfer(code: string) {
      transferRejected = code
    },
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
    loseEdit() {
      editLost = true
    },
    observeEdit() {
      editObservation = 'committed'
    },
    edits: () =>
      fetcher.mock.calls.filter(
        ([path, init]) => path === `${prefix}/tasks/${current.id}` && init.method === 'PATCH',
      ),
    editLookups: () =>
      fetcher.mock.calls.filter(
        ([path, init]) =>
          path === `${prefix}/task-commands/lookup` &&
          JSON.parse(init.body as string).command === 'work.task.update',
      ),
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
  it('shows a multi-round result without advancing the Task and submits only the explicitly confirmed Owner review', async () => {
    const f = await page('in_progress')
    await click('查看执行记录')
    await body()
      .get(`[data-execution-id="${id(100)}"]`)
      .trigger('click')
    await flushPromises()
    await click('加载最终输出')
    expect(body().get('[aria-label="最终输出正文"]').element.textContent).toBe(
      '已完成读取、写入与检查。\n结果：report.txt',
    )
    expect(body().get('.execution-facts').text()).toContain('执行成功')
    expect(f.transfers()).toHaveLength(0)
    expect(f.fetcher.mock.calls.every(([, request]) => request.method === 'GET')).toBe(true)
    expect(button('提交评审').exists()).toBe(true)
    expect(
      body()
        .findAll('button')
        .some((item) => item.text() === '接受并完成'),
    ).toBe(false)

    await click('提交评审')
    expect(button('确认提交评审').element.disabled).toBe(true)
    await selectAgent('评审 Agent', '评审员 (review-agent)')
    expect(button('确认提交评审').element.disabled).toBe(true)
    await body().get('textarea').setValue('已核对执行结果，请审核 report.txt。')
    expect(f.transfers()).toHaveLength(0)
    await click('确认提交评审')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: {
        target_state: 'in_review',
        assignee_agent_id: reviewerID,
        comment: '已核对执行结果，请审核 report.txt。',
      },
    })
    expect(body().get('[role="dialog"]').text()).toContain('评审中')
    expect(button('接受并完成').exists()).toBe(true)
    expect(f.lookups()).toHaveLength(0)
  })
  it('opens Sprint selection in a modal, restores its trigger and closes after a selection', async () => {
    const f = await page('todo', true)
    const trigger = button('选择 Sprint')
    trigger.element.focus()
    await trigger.trigger('click')
    await flushPromises()
    expect(body().get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    expect(body().get('[role="dialog"]').text()).toContain('当前迭代')
    await body().get('[role="dialog"] button[aria-label="关闭"]').trigger('click')
    await flushPromises()
    expect(body().find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('click')
    await flushPromises()
    await body().get('[role="dialog"] nav[aria-label="Sprint 列表"] button').trigger('click')
    await flushPromises()
    expect(body().find('[role="dialog"]').exists()).toBe(false)
    expect(f.tasks.state.sprint?.id).toBe(sprintID)
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })

  it('edits an unassigned backlog with explicit field presence and confirms same-value No-op separately', async () => {
    const f = await page('backlog')
    await click('编辑任务')
    expect(document.activeElement).toBe(body().get('form[aria-label="编辑任务"] input').element)
    expect(button('保存任务').element.disabled).toBe(true)
    await click('取消编辑')
    expect(document.activeElement).toBe(button('编辑任务').element)
    await click('编辑任务')
    await body().get('form[aria-label="编辑任务"] input').setValue('完整保存的规划任务')
    await selectAgent('任务类型', '缺陷')
    await selectAgent('优先级', '低')
    const fields = body().findAll('form[aria-label="编辑任务"] textarea')
    await fields[0]!.setValue('')
    await fields[1]!.setValue('')
    await click('保存任务')
    expect(f.edits()).toHaveLength(1)
    expect(JSON.parse(f.edits()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: {
        title: '完整保存的规划任务',
        description: '',
        type: 'bug',
        priority: 'low',
        plan: '',
      },
    })
    expect(body().get('[role="dialog"]').text()).toContain('任务已保存')
    expect(body().get('[role="dialog"]').text()).toContain('暂无描述')
    expect(body().get('[role="dialog"]').text()).toContain('暂无计划')
    await click('编辑任务')
    const title = body().get('form[aria-label="编辑任务"] input')
    await title.setValue('暂时修改')
    await title.setValue('完整保存的规划任务')
    await click('保存任务')
    expect(f.edits()).toHaveLength(2)
    expect(JSON.parse(f.edits()[1]![1].body as string)).toEqual({
      expected_version: '5',
      request: { title: '完整保存的规划任务' },
    })
    expect(body().get('[role="dialog"]').text()).toContain('任务内容没有变化')
    await body().get('.task-metadata summary').trigger('click')
    expect(
      body()
        .get('.task-metadata .task-facts')
        .findAll('div')
        .find((row) => row.get('dt').text() === '版本')
        ?.get('dd')
        .text(),
    ).toBe('5')
    expect(f.transfers()).toHaveLength(0)
    expect(f.creates()).toHaveLength(0)
  })

  it('hides edit and ready while an edit result is Unknown and queries only its original patch', async () => {
    const f = await page('backlog')
    f.loseEdit()
    await click('编辑任务')
    await body()
      .findAll('form[aria-label="编辑任务"] textarea')[1]!
      .setValue('保留原始执行计划补充')
    await click('保存任务')
    expect(f.edits()).toHaveLength(1)
    expect(f.editLookups()).toHaveLength(0)
    expect(body().get('[role="dialog"]').text()).toContain('提交结果待确认')
    expect(body().find('form[aria-label="编辑任务"]').exists()).toBe(false)
    expect(
      body()
        .findAll<HTMLButtonElement>('button')
        .some((b) => ['编辑任务', '指派并加入待执行'].includes(buttonLabel(b))),
    ).toBe(false)
    await click('查询原操作结果')
    expect(body().text()).toContain('尚未观察到原操作结果')
    f.observeEdit()
    await click('查询原操作结果')
    expect(body().get('[role="dialog"]').text()).toContain('保留原始执行计划补充')
    expect(button('编辑任务').element.disabled).toBe(false)
    expect(f.edits()).toHaveLength(1)
    expect(f.editLookups()).toHaveLength(2)
    const original = f.edits()[0]![1]
    for (const [, request] of f.editLookups()) {
      expect(JSON.parse(request.body as string)).toEqual({
        command: 'work.task.update',
        target_id: taskID,
        ...JSON.parse(original.body as string),
      })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
    expect(f.transfers()).toHaveLength(0)
  })

  it('creates a confirmed backlog first and requires a second explicit Agent choice before todo', async () => {
    const f = await page('in_progress', true)
    await click('新建任务')
    const form = body().get('form[aria-label="新建任务"]')
    expect(document.activeElement).toBe(form.get('input').element)
    expect(body().get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    expect(body().get('.dialog-footer button[type="submit"]').attributes('form')).toBe(
      form.attributes('id'),
    )
    await body().get('.ui-overlay').trigger('pointerdown')
    expect(body().find('form[aria-label="新建任务"]').exists()).toBe(true)
    await body().get('[role="dialog"] button[aria-label="关闭"]').trigger('click')
    await flushPromises()
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
    const detail = body().get('[role="dialog"]')
    expect(detail.text()).toContain('提交结果待确认')
    expect(detail.find('form').exists()).toBe(false)
    // The existing board search is read-only; only mutation forms must disappear.
    expect(
      body()
        .findAll('form')
        .map((form) => form.attributes('aria-label')),
    ).toEqual(['Sprint 任务搜索'])
    expect(
      detail
        .findAll<HTMLButtonElement>('button')
        .some((item) =>
          ['编辑任务', '指派并加入待执行', '提交评审', '接受并完成', '退回修改'].includes(
            buttonLabel(item),
          ),
        ),
    ).toBe(false)
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

describe('Task Cancel controlled visible workflow', () => {
  it('confirms cancellation explicitly, keeps assignee and Blocker facts, then rereads Task, Timeline and columns', async () => {
    const f = await page('blocked', false, true)
    await click('取消任务')
    expect(document.activeElement).toBe(button('确认取消任务').element)
    const confirmation = body().get('form[aria-label="取消任务确认"]')
    expect(confirmation.text()).toContain('取消后不可恢复')
    expect(confirmation.findAll('[role="combobox"]')).toHaveLength(0)
    expect(button('确认取消任务').element.disabled).toBe(false)
    await click('保留任务')
    expect(document.activeElement).toBe(button('取消任务').element)
    expect(f.transfers()).toHaveLength(0)
    await click('取消任务')
    await body().get('form[aria-label="取消任务确认"] textarea').setValue('  明确取消\n保留说明  ')
    await click('确认取消任务')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { target_state: 'cancelled', comment: '  明确取消\n保留说明  ' },
    })
    expect(body().get('.task-detail').text()).toContain('已取消')
    expect(body().get('.task-detail').text()).toContain('等待负责人确认')
    expect(body().get('.task-detail').text()).toContain('未解除')
    expect(document.activeElement).toBe(body().get('.task-detail h2').element)
    expect(body().find('form[aria-label="取消任务确认"]').exists()).toBe(false)
    const sent = f.fetcher.mock.calls.findIndex(
      ([path, init]) => path === transferPath && init.method === 'POST',
    )
    const reads = f.fetcher.mock.calls
      .slice(sent + 1)
      .filter(([, init]) => init.method === 'GET')
      .map(([path]) => new URL(path, 'https://example.test'))
    expect(reads.some((url) => url.pathname === `${prefix}/tasks/${taskID}`)).toBe(true)
    expect(reads.some((url) => url.pathname === `${prefix}/tasks/${taskID}/events`)).toBe(true)
    expect(
      reads.some(
        (url) =>
          url.pathname === `${prefix}/tasks` && url.searchParams.get('state') === 'cancelled',
      ),
    ).toBe(true)
  })
  it('omits an empty optional comment and needs no assignee for an unassigned backlog', async () => {
    const f = await page('backlog')
    await click('取消任务')
    expect(button('确认取消任务').element.disabled).toBe(false)
    await click('确认取消任务')
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { target_state: 'cancelled' },
    })
    expect(body().get('.task-detail').text()).toContain('任务已取消。')
  })
  it.each(['done', 'cancelled'] as const)(
    'does not expose cancellation for terminal %s',
    async (state) => {
      const f = await page(state)
      expect(
        body()
          .findAll<HTMLButtonElement>('button')
          .some((item) => buttonLabel(item) === '取消任务'),
      ).toBe(false)
      expect(f.transfers()).toHaveLength(0)
    },
  )
  it('shows occupancy refusal without making an Execution cancellation or claiming task success', async () => {
    const f = await page('todo')
    f.rejectTransfer('RESOURCE_BUSY')
    await click('取消任务')
    await click('确认取消任务')
    expect(body().text()).toContain('暂不能取消')
    expect(body().text()).toContain('取消任务会请求停止关联 Execution；停止可能需要一些时间。')
    expect(body().text()).not.toContain('任务已取消。')
    expect(f.transfers()).toHaveLength(1)
    expect(
      f.fetcher.mock.calls.some(
        ([path, init]) => path.includes('/executions/') && init.method !== 'GET',
      ),
    ).toBe(false)
  })
  it('recovers only the original unknown cancellation before showing the reread cancelled task', async () => {
    const f = await page('in_review')
    f.unknown()
    await click('取消任务')
    await click('确认取消任务')
    expect(body().find('form[aria-label="取消任务确认"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button('查询原操作结果').element)
    await click('查询原操作结果')
    expect(body().text()).toContain('尚未观察到原操作结果')
    f.observe('committed')
    await click('查询原操作结果')
    expect(body().get('.task-detail').text()).toContain('已取消')
    expect(document.activeElement).toBe(body().get('.task-detail h2').element)
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(2)
    const original = f.transfers()[0]![1]
    for (const [, init] of f.lookups()) {
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'work.task.transfer',
        target_id: taskID,
        ...JSON.parse(original.body as string),
      })
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
  })
})

describe('Task blocking and atomic recovery page', () => {
  it('marks a planned todo without changing its assignee, then explicitly selects one atomic recovery and restores focus', async () => {
    const f = await page('todo', false, true, 'planned')
    await click('标记受阻')
    expect(document.activeElement).toBe(body().get('form[aria-label="标记受阻"] textarea').element)
    await click('保留当前状态')
    expect(document.activeElement).toBe(button('标记受阻').element)
    expect(f.transfers()).toHaveLength(0)
    await click('标记受阻')
    await click('确认标记受阻')
    expect(f.transfers()).toHaveLength(1)
    expect(JSON.parse(f.transfers()[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { target_state: 'blocked' },
    })
    expect(body().get('.task-detail').text()).toContain('任务已标记受阻')
    expect(document.activeElement).toBe(body().get('.task-detail h2').element)
    await click('解决阻塞并恢复待执行')
    const checkbox = body().get<HTMLInputElement>(
      'form[aria-label="解决阻塞并恢复待执行"] input[type="checkbox"]',
    )
    expect(document.activeElement).toBe(checkbox.element)
    expect(checkbox.element.checked).toBe(false)
    expect(button('确认解决阻塞并恢复待执行').element.disabled).toBe(true)
    await checkbox.setValue(true)
    await click('确认解决阻塞并恢复待执行')
    expect(f.transfers()).toHaveLength(2)
    expect(JSON.parse(f.transfers()[1]![1].body as string)).toEqual({
      expected_version: '5',
      request: { target_state: 'todo', resolve_blocker_ids: [id(70)] },
    })
    expect(body().get('.task-detail').text()).toContain('已恢复待执行')
    expect(body().get('.task-detail').text()).toContain('已解除')
    expect(document.activeElement).toBe(body().get('.task-detail h2').element)
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(2)
  })
  it('keeps recovery Unknown on the original key and selection until Lookup and real reread finish', async () => {
    const f = await page('blocked', false, true)
    f.unknown()
    await click('解决阻塞并恢复待执行')
    await body()
      .get('form[aria-label="解决阻塞并恢复待执行"] input[type="checkbox"]')
      .setValue(true)
    await click('确认解决阻塞并恢复待执行')
    expect(body().find('form[aria-label="解决阻塞并恢复待执行"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button('查询原操作结果').element)
    await click('查询原操作结果')
    expect(body().text()).toContain('尚未观察到原操作结果')
    f.observe('committed')
    await click('查询原操作结果')
    expect(f.transfers()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(2)
    expect(body().get('.task-detail').text()).toContain('待执行')
    expect(body().get('.task-detail').text()).toContain('已解除')
    expect(document.activeElement).toBe(body().get('.task-detail h2').element)
    const sent = f.transfers()[0]![1]
    for (const [, lookup] of f.lookups()) {
      expect(JSON.parse(lookup.body as string)).toEqual({
        command: 'work.task.transfer',
        target_id: taskID,
        ...JSON.parse(sent.body as string),
      })
      expect(new Headers(lookup.headers).get('Idempotency-Key')).toBe(
        new Headers(sent.headers).get('Idempotency-Key'),
      )
    }
  })
  it('keeps an opened resolution draft bound to its version and refuses unread blocker facts', async () => {
    const f = await page('blocked', false, true)
    await click('解决阻塞并恢复待执行')
    await body()
      .get('form[aria-label="解决阻塞并恢复待执行"] input[type="checkbox"]')
      .setValue(true)
    f.changeTask()
    await f.tasks.refresh()
    await flushPromises()
    expect(body().text()).toContain('任务已变化，请选择“保留当前状态”')
    expect(button('确认解决阻塞并恢复待执行').element.disabled).toBe(true)
    expect(f.transfers()).toHaveLength(0)
    await click('保留当前状态')
    f.blockers('unavailable')
    await f.tasks.refresh()
    await flushPromises()
    await click('解决阻塞并恢复待执行')
    expect(button('确认解决阻塞并恢复待执行').element.disabled).toBe(true)
    expect(f.transfers()).toHaveLength(0)
  })
  it('shows authoritative occupancy refusal and makes no extra unblock or execution command', async () => {
    const f = await page('todo', false, true)
    f.rejectTransfer('RESOURCE_BUSY')
    await click('标记受阻')
    await click('确认标记受阻')
    expect(body().text()).toContain('任务有尚未结束的操作')
    expect(body().text()).not.toContain('任务已标记受阻。')
    expect(f.transfers()).toHaveLength(1)
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
    expect(button('确认标记受阻').element.disabled).toBe(true)
  })
})
