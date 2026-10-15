import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowRef } from 'vue'
import { createAccountAPI } from '../api/account'
import { createExecutionApprovalAPI, type ExecutionApproval } from '../api/execution-approval'
import type { Fetch } from '../api/client'
import MeetingExecutionApproval from '../components/meetings/MeetingExecutionApproval.vue'
import {
  createMeetingExecutionApproval,
  type MeetingExecutionApprovalScope,
} from '../composables/useMeetingExecutionApproval'
import { createSessionController } from '../composables/useSession'
import { at, id, json, p, session } from './task-execution-fixture'
import type { MeetingContributionExecution } from '../api/meetings'
const meeting = id(200),
  contribution = id(201)
function execution(
  n: number,
  change: Partial<MeetingContributionExecution> = {},
): MeetingContributionExecution {
  return {
    id: id(n),
    project_id: p,
    meeting_id: meeting,
    turn_id: id(202),
    contribution_id: contribution,
    participant_id: id(203),
    agent_id: id(7),
    generation: '2',
    attempt: '1',
    status: 'running',
    version: '1',
    waiting_reason: null,
    waiting_reference: null,
    cancel_requested_at: null,
    created_at: at,
    started_at: at,
    completed_at: null,
    ...change,
  }
}

const cleanups: (() => void)[] = [],
  wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  cleanups.splice(0).forEach((cleanup) => cleanup())
  await flushPromises()
  vi.restoreAllMocks()
})
const before = (): ExecutionApproval => ({
  id: id(400),
  project_id: p,
  execution_id: id(100),
  agent_id: id(7),
  tool_id: id(401),
  tool_name: 'run-command',
  tool_call_id: 'call_run_check',
  operation_id: id(402),
  operation_fingerprint: 'sha256:' + 'a'.repeat(64),
  status: 'pending',
  version: '1',
  created_at: at,
  resolved_at: null,
  resolved_by: null,
})
const receipt = (decision: 'approve' | 'reject' = 'approve') => ({
  command_id: id(403),
  approval: {
    ...before(),
    status: decision === 'approve' ? 'approved' : 'rejected',
    version: '2',
    resolved_at: at,
    resolved_by: id(8),
  },
})
async function fixture() {
  let handler: Fetch = async (_url, init) =>
    init.method === 'GET'
      ? json({ approval: before() })
      : json({ receipt: receipt(JSON.parse(String(init.body)).decision) })
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { executionApproval: createExecutionApprovalAPI(fetch) }
  const auth = createSessionController(...args)
  await auth.restore()
  const scope = shallowRef<MeetingExecutionApprovalScope | null>({
    projectID: p,
    meetingID: meeting,
    contributionID: contribution,
    key: 'meeting/contribution/execution/identity1',
    mutable: true,
    execution: execution(100, {
      status: 'waiting',
      started_at: at,
      waiting_reason: 'approval',
      waiting_reference: id(400),
    }),
  })
  const refresh = vi.fn(async () => {})
  const owner = createMeetingExecutionApproval(
    scope,
    computed(() => auth.state.busy),
    auth.executionApproval,
    refresh,
  )
  cleanups.push(owner.dispose, () => auth.leave())
  const wrapper = mount(MeetingExecutionApproval, { props: { owner } })
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

describe('Meeting approval confirmation and original-operation recovery', () => {
  it.each(['approve', 'reject'] as const)(
    'explicitly confirms %s for only the selected operation and refreshes its execution',
    async (decision) => {
      const f = await fixture()
      await f.click('查看待审批操作')
      expect(f.wrapper.text()).toContain('工具操作（run-command）')
      expect(f.wrapper.text()).toContain(id(402))
      await f.click(decision === 'approve' ? '批准此次操作' : '拒绝此次操作')
      expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
      await f.wrapper.get('form').trigger('submit')
      await flushPromises()
      const writes = f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
      expect(writes).toHaveLength(1)
      expect(writes[0]![0]).toBe(`/api/v1/projects/${p}/approvals/${id(400)}/decision`)
      expect(JSON.parse(String(writes[0]![1].body))).toEqual({ decision, expected_version: '1' })
      expect(f.refresh).toHaveBeenCalledExactlyOnceWith()
      expect(f.wrapper.text()).toContain(
        `已确认${decision === 'approve' ? '批准' : '拒绝'}此次操作`,
      )
      expect(f.scope.value?.execution?.status).toBe('waiting')
      expect(f.wrapper.find('form').exists()).toBe(false)
    },
  )
  it('shows the captured MCP tool identity with a general tool confirmation', async () => {
    const f = await fixture()
    const toolName = `mcp:${id(500)}:search.documents`
    f.intercept(async () => json({ approval: { ...before(), tool_name: toolName } }))
    await f.click('查看待审批操作')
    expect(f.wrapper.text()).toContain(`工具操作（${toolName}）`)
    expect(f.wrapper.text()).not.toContain('运行命令')
    await f.click('批准此次操作')
    expect(f.wrapper.get('form').text()).toContain('确认批准此次工具操作？')
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })
  it('keeps an unknown decision immutable and only looks up the original command', async () => {
    const f = await fixture()
    let found = false
    f.intercept(async (url, init) => {
      if (init.method === 'GET') return json({ approval: before() })
      if (url.endsWith('/lookup'))
        return json({
          status: found ? 'committed' : 'not_observed',
          receipt: found ? receipt() : null,
        })
      throw new Error('lost response')
    })
    await f.click('查看待审批操作')
    await f.click('批准此次操作')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.wrapper.text()).toContain('原批准请求结果尚未确认')
    expect(f.button('拒绝此次操作')).toBeUndefined()
    await f.click('核对原审批决定')
    expect(f.wrapper.text()).toContain('尚未查到原审批决定的提交记录')
    expect(f.refresh).not.toHaveBeenCalled()
    found = true
    await f.click('核对原审批决定')
    expect(f.refresh).toHaveBeenCalledExactlyOnceWith()
    const writes = f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(writes.filter(([url]) => url.endsWith('/decision'))).toHaveLength(1)
    expect(new Set(writes.map(([, init]) => init.body)).size).toBe(1)
    expect(
      new Set(writes.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
  })
  it('retires a confirmation on approval-reference change and blocks a newly requested cancellation', async () => {
    const f = await fixture()
    await f.click('查看待审批操作')
    await f.click('批准此次操作')
    f.scope.value = {
      ...f.scope.value!,
      execution: { ...f.scope.value!.execution!, cancel_requested_at: at },
    }
    await flushPromises()
    expect((f.button('确认批准此次操作')!.element as HTMLButtonElement).disabled).toBe(true)
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
    f.scope.value = {
      ...f.scope.value!,
      execution: { ...f.scope.value!.execution!, waiting_reference: id(499) },
    }
    await flushPromises()
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.wrapper.text()).not.toContain(id(402))
    expect(f.button('查看待审批操作')).toBeDefined()
  })
  it('does not attach a late approval response to another selected execution', async () => {
    const f = await fixture()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>((resolve) => {
      finish = resolve
    })
    cleanups.push(() => finish(json({ approval: before() })))
    f.intercept(async () => tail)
    await f.button('查看待审批操作')!.trigger('click')
    await flushPromises()
    f.scope.value = { ...f.scope.value!, key: 'another-execution', execution: execution(101) }
    await flushPromises()
    finish(json({ approval: before() }))
    await flushPromises()
    expect(f.wrapper.find('section').exists()).toBe(false)
    expect(f.owner.state.approval).toBeNull()
    expect(f.refresh).not.toHaveBeenCalled()
  })
})
