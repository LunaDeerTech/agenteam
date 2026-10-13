import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import type { Fetch } from '../api/client'
import { createTaskExecutionsAPI } from '../api/task-executions'
import {
  createTaskExecutionOutputAPI,
  type TaskExecutionOutput,
} from '../api/task-execution-output'
import type { TaskQuery } from '../api/work-review'
import { createProjectTasks, type ProjectTasks } from '../composables/useProjectTasks'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import TaskReviewDetail from '../components/work/TaskReviewDetail.vue'
import { at, execution, id, json, p, path, problem, t, task } from './task-execution-fixture'
import { finalExecution, output, outputPath } from './task-execution-output-fixture'
const owners: ProjectTasks[] = [],
  wrappers: VueWrapper[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach((fn) => fn())
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
function visibleLabel(element: Element) {
  const copy = element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((child) => child.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim() ?? ''
}
async function fixture() {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    identity,
    projectID: p,
    generation: 1,
    readGeneration: 1,
  })
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const row = execution(100, { status: 'running', started_at: at, snapshot_id: id(200) })
  let handler: Fetch = async () => json(output())
  const fetch = vi.fn<Fetch>((url, init) =>
    url === path + '?limit=50'
      ? Promise.resolve(json({ items: [row], next_cursor: null }))
      : url === path + '/' + row.id
        ? Promise.resolve(json({ execution: row }))
        : handler(url, init),
  )
  const api = createTaskExecutionsAPI(fetch),
    outputAPI = createTaskExecutionOutputAPI(fetch),
    signal = new AbortController().signal
  const sprint = {
    ...task(),
    id: id(3),
    title: '迭代',
    state: 'current',
    started_at: at,
    completed_at: null,
  }
  const milestone = { ...sprint, id: id(2), title: '目标' }
  const reads = {
    progress: null,
    milestones: vi.fn(async () => ({ items: [milestone] })),
    milestone: vi.fn(async () => milestone),
    sprints: vi.fn(async () => ({ items: [sprint] })),
    sprint: vi.fn(async () => sprint),
    task: vi.fn(async (_p: string, target: string) => ({ ...task(), id: target })),
    tasks: vi.fn(async (_p: string, query: TaskQuery) => ({
      items: query.state === 'backlog' ? [task()] : [],
    })),
    blockers: vi.fn(async () => ({ items: [] })),
    events: vi.fn(async () => ({ items: [] })),
    agents: vi.fn(async () => ({ items: [] })),
    abandonRead: vi.fn(),
    executions: vi.fn((project: string, target: string, q: { limit?: number; cursor?: string }) =>
      api.list(project, target, q, signal),
    ),
    execution: vi.fn((project: string, target: string, e: string) =>
      api.get(project, target, e, signal),
    ),
    executionOutput: vi.fn((project: string, target: string, e: string) =>
      outputAPI.get(project, target, e, signal),
    ),
  }
  const auth = {
    state: authState,
    personalContext: shallowReactive({ identity }),
    workReview: reads,
    workMilestonePlanning: { progress: null },
    workSprintLifecycle: { progress: null, createProgress: null },
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    paths: computed(() => ({ home: '/owner/demo' })),
    detail: shallowReactive({
      phase: 'current',
      project: { id: p, lifecycle: 'active', current_sprint_id: id(3) },
    }),
  } as unknown as ProjectWorkspace
  const owner = createProjectTasks(auth, workspace, async (destination) => {
    owner.afterNavigation(destination)
  })
  owners.push(owner)
  owner.afterNavigation(`/owner/demo/tasks/${t}`)
  const wrapper = mount(TaskReviewDetail, { props: { owner }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  const button = (text: string) => {
    const result = wrapper.findAll('button').find((item) => visibleLabel(item.element) === text)
    if (!result) throw new Error('Missing button: ' + text)
    return result
  }
  async function click(text: string) {
    const b = button(text)
    ;(b.element as HTMLButtonElement).focus()
    await b.trigger('click')
    await flushPromises()
  }
  async function open() {
    await click('查看执行记录')
    await wrapper.get(`[data-execution-id="${row.id}"]`).trigger('click')
    await flushPromises()
  }
  return {
    owner,
    wrapper,
    reads,
    fetch,
    context,
    row,
    button,
    click,
    open,
    intercept: (next: Fetch) => {
      handler = next
    },
  }
}
describe('Task detail final output', () => {
  it('loads only explicitly, refreshes pending to complete literal text, and adopts its summary without changing the Task', async () => {
    const f = await fixture()
    await f.open()
    expect(f.fetch).toHaveBeenCalledTimes(2)
    f.intercept(async () => json({ execution: f.row, availability: 'pending', snapshot: null }))
    await f.click('加载最终输出')
    expect(f.wrapper.text()).toContain('尚未生成最终输出')
    expect(f.fetch).toHaveBeenCalledTimes(3)
    const text = '  原文\n<script>window.secret = true</script>\n<img src=x onerror=alert(1)>\n🙂  '
    f.intercept(async () => json(output(text, finalExecution({ version: '9007199254740994' }))))
    await f.click('刷新最终输出')
    expect(f.wrapper.get('[aria-label="最终输出正文"]').element.textContent).toBe(text)
    expect(f.wrapper.find('.execution-output script').exists()).toBe(false)
    expect(f.wrapper.find('.execution-output img').exists()).toBe(false)
    expect(f.wrapper.get('.execution-facts').text()).toContain('执行成功')
    expect(f.wrapper.get('.execution-facts').text()).toContain('9007199254740994')
    expect(f.owner.state.task?.state).toBe('backlog')
    expect(f.fetch.mock.calls.map((call) => call[0])).toEqual([
      path + '?limit=50',
      path + '/' + id(100),
      outputPath,
      outputPath,
    ])
    expect(document.activeElement).toBe(f.button('刷新最终输出').element)
    await f.click('刷新执行详情')
    expect(f.wrapper.find('[aria-label="最终输出正文"]').exists()).toBe(false)
    expect(f.button('加载最终输出').exists()).toBe(true)
    expect(f.fetch).toHaveBeenCalledTimes(5)
  })
  it('keeps missing records, successful empty text and failed or cancelled empty snapshots distinct', async () => {
    const cases: [TaskExecutionOutput, string, string][] = [
      [
        { execution: finalExecution(), availability: 'not_recorded', snapshot: null },
        '未记录最终输出',
        '执行成功',
      ],
      [output(''), '最终输出为空文本', '执行成功'],
      [
        output('', finalExecution({ status: 'failed', reason: 'model_failed' })),
        '执行已结束，无最终文本',
        '模型调用失败',
      ],
      [
        output('', finalExecution({ status: 'cancelled', reason: 'cancelled' })),
        '执行已结束，无最终文本',
        '执行已取消',
      ],
    ]
    for (const [value, title, reason] of cases) {
      const f = await fixture()
      await f.open()
      f.intercept(async () => json(value))
      await f.click('加载最终输出')
      expect(f.wrapper.get('.execution-output').text()).toContain(title)
      expect(f.wrapper.get('.execution-facts').text()).toContain(reason)
      expect(f.wrapper.find('[aria-label="最终输出正文"]').exists()).toBe(false)
      expect(f.fetch).toHaveBeenCalledTimes(3)
    }
  })
  it('removes old text while refreshing, rejects incomplete content and retries only on request', async () => {
    const f = await fixture()
    await f.open()
    await f.click('加载最终输出')
    let finish!: (value: Response) => void
    const tail = new Promise<Response>((resolve) => {
      finish = resolve
    })
    const broken = () =>
      new Response('{"execution":', {
        headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(999) },
      })
    releases.push(() => finish(broken()))
    f.intercept(async () => tail)
    await f.button('刷新最终输出').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('正在读取最终输出')
    expect(f.wrapper.find('[aria-label="最终输出正文"]').exists()).toBe(false)
    expect((f.button('返回执行列表').element as HTMLButtonElement).disabled).toBe(true)
    finish(broken())
    await flushPromises()
    expect(f.wrapper.text()).toContain('未能读取最终输出')
    expect(f.fetch).toHaveBeenCalledTimes(4)
    f.intercept(async () => json(output('重新读取完整正文')))
    await f.click('重试最终输出')
    expect(f.wrapper.get('[aria-label="最终输出正文"]').text()).toBe('重新读取完整正文')
    expect(f.fetch).toHaveBeenCalledTimes(5)
  })
  it('clears all execution observations after a permission refusal and restores a visible focus target', async () => {
    const f = await fixture()
    await f.open()
    await f.click('加载最终输出')
    f.intercept(async () => problem('PERMISSION_DENIED', 403))
    await f.click('刷新最终输出')
    expect(f.wrapper.find('.execution-facts').exists()).toBe(false)
    expect(f.wrapper.find('.execution-output').exists()).toBe(false)
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(0)
    expect(f.wrapper.text()).toContain('执行记录不可用或无权访问')
    expect(document.activeElement?.textContent).toBe('执行记录')
    expect(f.fetch).toHaveBeenCalledTimes(4)
    await f.click('重试执行列表')
    expect(f.wrapper.findAll('[data-execution-id]')).toHaveLength(1)
    expect(f.fetch).toHaveBeenCalledTimes(5)
  })
  it.each(['task', 'identity'])(
    'discards late final output after a %s change without implicit successor reads',
    async (change) => {
      const f = await fixture()
      await f.open()
      let finish!: (value: Response) => void
      const tail = new Promise<Response>((resolve) => {
        finish = resolve
      })
      releases.push(() => finish(json(output('old private text'))))
      f.intercept(async () => tail)
      await f.button('加载最终输出').trigger('click')
      await flushPromises()
      if (change === 'task') f.owner.afterNavigation(`/owner/demo/tasks/${id(88)}`)
      else
        f.context.value = {
          ...f.context.value!,
          identity: { userID: id(80), sessionID: id(90), epoch: 2 },
        }
      await flushPromises()
      finish(json(output('old private text')))
      await flushPromises()
      expect(f.wrapper.text()).not.toContain('old private text')
      expect(f.wrapper.find('.execution-output').exists()).toBe(false)
      expect(f.fetch).toHaveBeenCalledTimes(3)
      expect(f.reads.executionOutput).toHaveBeenCalledTimes(1)
    },
  )
  it('does not replace a newer execution detail with a regressed runtime response', async () => {
    const f = await fixture()
    await f.open()
    f.intercept(async () => json(output('stale', finalExecution({ version: '2' }))))
    await f.click('加载最终输出')
    expect(f.wrapper.text()).toContain('未能读取最终输出')
    expect(f.wrapper.find('[aria-label="最终输出正文"]').exists()).toBe(false)
    expect(f.wrapper.get('.execution-facts').text()).toContain(f.row.version)
    expect(f.fetch).toHaveBeenCalledTimes(3)
  })
})
