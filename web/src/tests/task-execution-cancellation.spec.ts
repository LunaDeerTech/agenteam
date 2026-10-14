import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowRef } from 'vue'
import { createAccountAPI } from '../api/account'
import { createTaskExecutionCancelAPI } from '../api/task-execution-cancel'
import { createWorkReviewAPI } from '../api/work-review'
import type { Fetch } from '../api/client'
import TaskExecutionCancellation from '../components/work/TaskExecutionCancellation.vue'
import {
  createTaskExecutionCancellation,
  type TaskExecutionCancellationScope,
} from '../composables/useTaskExecutionCancellation'
import { createSessionController } from '../composables/useSession'
import { at, execution, id, json, p, path, session, t } from './task-execution-fixture'

const cleanups: (() => void)[] = [],
  wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  cleanups.splice(0).forEach((cleanup) => cleanup())
  await flushPromises()
  vi.restoreAllMocks()
})
const accepted = () => ({
  execution: execution(100, {
    version: '9007199254740994',
    status: 'running',
    started_at: at,
    cancel_requested_at: at,
  }),
  task: { id: t, version: '4', state: 'blocked', blocker_id: id(500) },
})
async function fixture() {
  let handler: Fetch = async () => json({ receipt: accepted(), replayed: false })
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { taskExecutionCancel: createTaskExecutionCancelAPI(fetch) }
  const auth = createSessionController(...args)
  await auth.restore()
  const scope = shallowRef<TaskExecutionCancellationScope | null>({
    projectID: p,
    taskID: t,
    taskVersion: '3',
    key: 'task/execution/identity1',
    mutable: true,
    execution: execution(100, { status: 'running', started_at: at }),
  })
  const refresh = vi.fn(async () => {})
  const owner = createTaskExecutionCancellation(
    scope,
    computed(() => auth.state.busy),
    auth.executionCancel,
    refresh,
  )
  cleanups.push(owner.dispose, () => auth.leave())
  const wrapper = mount(TaskExecutionCancellation, { props: { owner } })
  wrappers.push(wrapper)
  const button = (label: string) =>
    wrapper.findAll('button').find((button) => {
      const copy = button.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((child) => child.remove())
      return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
    })
  async function click(label: string) {
    expect(button(label), label).toBeDefined()
    await button(label)!.trigger('click')
    await flushPromises()
  }
  return {
    auth,
    owner,
    scope,
    wrapper,
    fetch,
    refresh,
    button,
    click,
    intercept: (next: Fetch) => {
      handler = next
    },
  }
}

describe('Execution cancellation confirmation and recovery', () => {
  it('requires explicit confirmation and then refreshes authoritative Task and execution facts without manufacturing a terminal state', async () => {
    const f = await fixture()
    await f.click('取消当前执行')
    expect(f.fetch.mock.calls.filter(([url]) => url.startsWith(path))).toHaveLength(0)
    await f.wrapper.get('textarea').setValue('暂停本次执行')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    const requests = f.fetch.mock.calls.filter(([url]) => url.startsWith(path))
    expect(requests).toHaveLength(1)
    expect(JSON.parse(String(requests[0]![1].body))).toEqual({
      expected_version: '9007199254740993',
      expected_task_version: '3',
      reason: '暂停本次执行',
    })
    expect(f.refresh).toHaveBeenCalledExactlyOnceWith(p, t, id(100), true)
    expect(f.wrapper.text()).toContain('原取消请求已接受')
    expect(f.scope.value?.execution?.status).toBe('running')
    expect(f.scope.value?.taskVersion).toBe('3')
    expect(f.button('确认取消执行')).toBeUndefined()
  })
  it('keeps an Unknown decision read-only except for original lookup and never sends a second cancel', async () => {
    const f = await fixture()
    let found = false
    f.intercept(async (url) => {
      if (url.endsWith('/lookup')) return json({ found, receipt: found ? accepted() : null })
      throw new Error('lost response')
    })
    await f.click('取消当前执行')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.wrapper.text()).toContain('原执行取消结果尚未确认')
    expect(f.button('确认取消执行')).toBeUndefined()
    await f.click('核对原执行取消')
    expect(f.wrapper.text()).toContain('尚未查到原取消请求')
    expect(f.refresh).not.toHaveBeenCalled()
    found = true
    await f.click('核对原执行取消')
    expect(f.refresh).toHaveBeenCalledExactlyOnceWith(p, t, id(100), true)
    expect(f.fetch.mock.calls.filter(([url]) => url.endsWith('/cancel'))).toHaveLength(1)
  })
  it('invalidates the confirmation when the Task version or selected execution changes', async () => {
    const f = await fixture()
    await f.click('取消当前执行')
    f.scope.value = { ...f.scope.value!, taskVersion: '4' }
    await flushPromises()
    expect((f.button('确认取消执行')!.element as HTMLButtonElement).disabled).toBe(true)
    expect(f.wrapper.text()).toContain('任务或执行已更新')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.fetch.mock.calls.filter(([url]) => url.startsWith(path))).toHaveLength(0)
    f.scope.value = { ...f.scope.value!, key: 'another-execution', execution: execution(101) }
    await flushPromises()
    expect(f.wrapper.find('form').exists()).toBe(false)
  })
})

describe('Cancellation blocker read projection', () => {
  it('retains cancellation provenance and accepts the formal AgentRun resolver without enabling a Human add command', async () => {
    const actor = { type: 'human', user_id: id(8), source: 'task_domain' }
    let value: unknown = {
      items: [
        {
          id: id(500),
          project_id: p,
          task_id: t,
          type: 'user_cancelled_execution',
          metadata: { execution_id: id(100), cancelled_by: id(8), reason: 'owner pause' },
          description: 'Execution cancelled by user',
          created_at: at,
          created_by: actor,
          resolved_at: at,
          resolved_by: {
            type: 'agent_run',
            agent_id: id(7),
            execution_id: id(101),
            source: 'task_domain',
          },
          resolution_comment: 'resume',
        },
      ],
    }
    const api = createWorkReviewAPI(async () => json(value)),
      signal = new AbortController().signal
    expect((await api.blockers(p, t, {}, signal)).items[0]).toMatchObject({
      type: 'user_cancelled_execution',
      cancellation: { execution_id: id(100), cancelled_by: id(8), reason: 'owner pause' },
    })
    const row = (value as { items: Record<string, unknown>[] }).items[0]!
    value = {
      items: [{ ...row, metadata: { execution_id: id(100), cancelled_by: id(9), reason: null } }],
    }
    await expect(api.blockers(p, t, {}, signal)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
