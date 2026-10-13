import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import type { Fetch } from '../api/client'
import { createTaskExecutionsAPI } from '../api/task-executions'
import type { TaskQuery } from '../api/work-review'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import TaskReviewDetail from '../components/work/TaskReviewDetail.vue'
import { at, execution, id, json, p, path, problem, t, task } from './task-execution-fixture'
const owners: ProjectTasks[] = [], wrappers: VueWrapper[] = [], releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach(release => release())
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  owners.splice(0).forEach(owner => owner.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
async function fixture() {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({ identity, projectID: p, generation: 1, readGeneration: 1 })
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const rows = Array.from({ length: 51 }, (_, index) => execution(200 - index))
  const detail = execution(200, { status: 'failed', reason: 'model_failed', completed_at: at, started_at: at, snapshot_id: id(500) })
  let handler: Fetch = async url => {
    if (url === path + '?limit=50') return json({ items: rows.slice(0, 50), next_cursor: 'older' })
    if (url === path + '?limit=50&cursor=older') return json({ items: rows.slice(50), next_cursor: null })
    if (url === path + '/' + id(200)) return json({ execution: detail })
    throw new Error('Unexpected execution read')
  }
  const fetch = vi.fn<Fetch>((url, init) => handler(url, init)), api = createTaskExecutionsAPI(fetch)
  const signal = new AbortController().signal
  const currentSprint = { ...task(), id: id(3), title: '迭代', state: 'current', started_at: at, completed_at: null }
  const milestone = { ...currentSprint, id: id(2), title: '目标' }
  const reads = {
    progress: null,
    milestones: vi.fn(async () => ({ items: [milestone] })), milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [currentSprint] })), sprint: vi.fn(async () => currentSprint),
    task: vi.fn(async (_p: string, target: string) => ({ ...task(), id: target })),
    tasks: vi.fn(async (_p: string, query: TaskQuery) => ({ items: query.state === 'backlog' ? [task()] : [] })),
    blockers: vi.fn(async () => ({ items: [] })), events: vi.fn(async () => ({ items: [] })),
    agents: vi.fn(async () => ({ items: [] })), abandonRead: vi.fn(),
    executions: vi.fn((project: string, target: string, query: { limit?: number; cursor?: string }) => api.list(project, target, query, signal)),
    execution: vi.fn((project: string, target: string, targetExecution: string) => api.get(project, target, targetExecution, signal)),
  }
  const auth = { state: authState, personalContext: shallowReactive({ identity }), workReview: reads,
    workMilestonePlanning: { progress: null }, workSprintLifecycle: { progress: null, createProgress: null },
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context, paths: computed(() => ({ home: '/owner/demo' })),
    detail: shallowReactive({ phase: 'current', project: { id: p, lifecycle: 'active', current_sprint_id: id(3) } }),
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async destination => { owner.afterNavigation(destination) })
  owners.push(owner)
  owner.afterNavigation(`/owner/demo/tasks/${t}`)
  const wrapper = mount(TaskReviewDetail, { props: { owner }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  const button = (text: string) => {
    const match = wrapper.findAll('button').find(button => button.text() === text)
    if (!match) throw new Error('Missing button: ' + text)
    return match
  }
  async function click(text: string) { const b = button(text); (b.element as HTMLButtonElement).focus(); await b.trigger('click'); await flushPromises() }
  return { owner, wrapper, reads, fetch, context, authState, rows, detail, button, click, intercept: (next: Fetch) => { handler = next } }
}
describe('Task detail Execution section', () => {
  it('opens real Task-scoped pages, independently reads detail, restores focus and keeps Task status', async () => {
    const f = await fixture()
    expect(f.fetch).not.toHaveBeenCalled()
    await f.click('查看执行记录')
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(50)
    await f.click('加载较早执行')
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(51)
    const trigger = f.wrapper.get(`[data-execution-id="${id(200)}"]`)
    ;(trigger.element as HTMLButtonElement).focus()
    await trigger.trigger('click'); await flushPromises()
    expect(document.activeElement?.textContent).toBe('执行详情')
    expect(f.wrapper.get('.execution-facts').text()).toContain('模型调用失败')
    expect(f.wrapper.get('.task-facts').text()).toContain('待规划')
    expect(f.owner.state.task?.state).toBe('backlog')
    expect(f.fetch.mock.calls.map(call => call[0])).toEqual([path + '?limit=50', path + '?limit=50&cursor=older', path + '/' + id(200)])
    await f.click('刷新执行详情')
    expect(f.fetch).toHaveBeenCalledTimes(4)
    await f.click('返回执行列表')
    expect(document.activeElement).toBe(f.wrapper.get(`[data-execution-id="${id(200)}"]`).element)
    await f.click('收起执行记录')
    expect(document.activeElement).toBe(f.button('查看执行记录').element)
  })
  it('offers explicit retry and first-page recovery after a stale cursor without automatic re-reads', async () => {
    const f = await fixture()
    await f.click('查看执行记录')
    f.intercept(async () => problem('CURSOR_INVALID', 400))
    await f.click('加载较早执行')
    expect(f.wrapper.text()).toContain('执行记录分页已失效')
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(50)
    expect(f.fetch).toHaveBeenCalledTimes(2)
    f.intercept(async url => { expect(url).toBe(path + '?limit=50'); return json({ items: [], next_cursor: null }) })
    await f.click('重试执行列表')
    expect(f.wrapper.text()).toContain('暂无执行记录')
    expect(f.fetch).toHaveBeenCalledTimes(3)
  })
  it('hides the previous detail during refresh and clears observations on a permission refusal', async () => {
    const f = await fixture()
    await f.click('查看执行记录')
    await f.wrapper.get(`[data-execution-id="${id(200)}"]`).trigger('click'); await flushPromises()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>(resolve => { finish = resolve })
    releases.push(() => finish(problem('PERMISSION_DENIED', 403)))
    f.intercept(async () => tail)
    await f.button('刷新执行详情').trigger('click'); await flushPromises()
    expect(f.wrapper.text()).toContain('正在读取执行详情')
    expect(f.wrapper.find('.execution-facts').exists()).toBe(false)
    finish(problem('PERMISSION_DENIED', 403)); await flushPromises()
    expect(f.wrapper.text()).toContain('执行记录不可用或无权访问')
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(0)
    expect(f.wrapper.find('.execution-facts').exists()).toBe(false)
    expect(f.fetch).toHaveBeenCalledTimes(3)
  })
  it('discards an old Task response after navigation and never loads its successor implicitly', async () => {
    const f = await fixture()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>(resolve => { finish = resolve })
    releases.push(() => finish(json({ items: [execution()], next_cursor: null })))
    f.intercept(async () => tail)
    await f.button('查看执行记录').trigger('click'); await flushPromises()
    f.owner.afterNavigation(`/owner/demo/tasks/${id(88)}`); await flushPromises()
    finish(json({ items: [execution()], next_cursor: null })); await flushPromises()
    expect(f.owner.state.task?.id).toBe(id(88))
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(0)
    expect(f.button('查看执行记录').exists()).toBe(true)
    expect(f.fetch).toHaveBeenCalledTimes(1)
    f.context.value = null
    await flushPromises()
    expect(f.wrapper.find('[aria-label="执行记录"]').exists()).toBe(false)
  })
  it('renders cancellation intent and a nullable reason faithfully, with only explicit refresh', async () => {
    const f = await fixture()
    const row = execution(200, { status: 'running', started_at: at, cancel_requested_at: at, snapshot_id: id(500), reason: null })
    f.intercept(async url => url.includes('?') ? json({ items: [row], next_cursor: null }) : json({ execution: row }))
    await f.click('查看执行记录')
    expect(f.wrapper.get('.execution-list').text()).toContain('正在取消')
    await f.wrapper.get(`[data-execution-id="${row.id}"]`).trigger('click'); await flushPromises()
    expect(f.wrapper.get('.execution-facts').text()).toContain('未提供')
    expect(f.wrapper.get('.execution-facts').text()).toContain('尚未结束')
    expect(f.fetch).toHaveBeenCalledTimes(2)
    expect(f.wrapper.findAll('button').some(button => button.text() === '取消执行')).toBe(false)
  })
})
