import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive, shallowReactive, shallowRef } from 'vue'
import type { MeetingSubscriptionState } from '../composables/meeting-realtime'
import type { MeetingContribution, MeetingContributionExecution } from '../api/meetings'
import MeetingContributionExecutionView from '../components/meetings/MeetingContributionExecution.vue'
const ports = vi.hoisted(() => ({ auth: null as unknown, workspace: null as unknown }))
vi.mock('../composables/useSession', () => ({ useSession: () => ports.auth }))
vi.mock('../composables/useProjectWorkspace', () => ({
  useProjectWorkspace: () => ports.workspace,
}))
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-14T00:00:00Z'
const contribution: MeetingContribution = {
  id: id(4),
  turn_id: id(3),
  participant_id: id(5),
  kind: 'agent',
  order: 1,
  status: 'running',
  generation: '1',
  attempt: '1',
  execution_id: id(7),
  waiting_on_execution_id: null,
  cancel_requested: false,
  message: null,
  history: [],
  started_at: at,
  completed_at: null,
}
const running: MeetingContributionExecution = {
  id: id(7),
  project_id: id(1),
  meeting_id: id(2),
  turn_id: id(3),
  contribution_id: id(4),
  participant_id: id(5),
  agent_id: id(6),
  generation: '1',
  attempt: '1',
  status: 'running',
  version: '1',
  waiting_reason: null,
  waiting_reference: null,
  cancel_requested_at: null,
  created_at: at,
  started_at: at,
  completed_at: null,
}
const waiting: MeetingContributionExecution = {
  ...running,
  status: 'waiting',
  version: '2',
  waiting_reason: 'approval',
  waiting_reference: id(8),
}
let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  vi.restoreAllMocks()
})
describe('Meeting current Execution automatic progress', () => {
  it('shows a newly pending approval after opening a still-running execution, including a shared-read busy interval', async () => {
    vi.useFakeTimers()
    const state = reactive({ busy: false })
    const read = vi.fn().mockResolvedValueOnce(running).mockResolvedValue(waiting)
    ports.auth = {
      state,
      meetings: { contributionExecution: read },
      executionApproval: { meetingProgress: null, forgetMeetingUnavailable: vi.fn() },
    }
    ports.workspace = {
      currentReadContext: shallowRef({
        projectID: id(1),
        identity: { userID: id(10), sessionID: id(11), epoch: 1 },
        generation: 1,
        readGeneration: 1,
      }),
    }
    wrapper = mount(MeetingContributionExecutionView, {
      props: { projectID: id(1), meetingID: id(2), contribution, mutable: true, disabled: false },
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('正在执行')
    state.busy = true
    await vi.advanceTimersByTimeAsync(2500)
    expect(read).toHaveBeenCalledTimes(1)
    state.busy = false
    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('等待工具操作审批')
    expect(
      wrapper.findAll('button').some((b) => {
        const copy = b.element.cloneNode(true) as Element
        copy.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
        return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === '查看待审批操作'
      }),
    ).toBe(true)
  })
  it('renders committed execution text from the shared subscription and refreshes approval facts only on a changed version', async () => {
    vi.useFakeTimers()
    const state = reactive({ busy: false })
    const read = vi.fn().mockResolvedValueOnce(running).mockResolvedValue(waiting)
    const stream = shallowReactive<MeetingSubscriptionState>({
      phase: 'connecting',
      reason: '',
      snapshot: null,
    })
    const handle = { state: stream, refresh: vi.fn(), dispose: vi.fn() }
    const subscribe = vi.fn(() => handle)
    ports.auth = {
      state,
      meetings: { contributionExecution: read },
      meetingRealtime: { subscribe },
      executionApproval: { meetingProgress: null, forgetMeetingUnavailable: vi.fn() },
    }
    ports.workspace = {
      currentReadContext: shallowRef({
        projectID: id(1),
        identity: { userID: id(10), sessionID: id(11), epoch: 1 },
        generation: 1,
        readGeneration: 1,
      }),
    }
    wrapper = mount(MeetingContributionExecutionView, {
      props: { projectID: id(1), meetingID: id(2), contribution, mutable: true, disabled: false },
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    const topic = {
      kind: 'meeting.execution' as const,
      project_id: id(1),
      meeting_id: id(2),
      contribution_id: id(4),
      generation: '1',
      attempt: '1',
      execution_id: id(7),
    }
    expect(subscribe).toHaveBeenCalledWith(topic)
    stream.phase = 'ready'
    stream.snapshot = {
      topic,
      schema: 'meeting_execution_runtime_v1',
      digest: 'validated-by-original-codec',
      content: {
        schema: 'execution_public_runtime_v1',
        execution_id: id(7),
        project_id: id(1),
        execution_version: '2',
        status: 'waiting',
        transcript_through: '1',
        items: [
          {
            id: 'state',
            seq: '1',
            revision: '2',
            type: 'state',
            transcript_sequence: '0',
            state: { status: 'waiting' },
          },
          {
            id: 'waiting/' + id(8),
            seq: '2',
            revision: '2',
            type: 'waiting',
            transcript_sequence: '0',
            waiting: { reason: 'approval', reference: id(8) },
          },
          {
            id: 'round/' + id(20) + '/0',
            seq: '260',
            revision: '1',
            type: 'text',
            transcript_sequence: '1',
            round_id: id(20),
            text: { content: 'Committed calibration result.' },
          },
        ],
      },
    }
    await flushPromises()
    expect(wrapper.get('[aria-label="执行过程"]').text()).toContain('Committed calibration result.')
    expect(wrapper.text()).toContain('等待工具操作审批')
    expect(read).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5000)
    expect(read).toHaveBeenCalledTimes(2)
    stream.phase = 'stale'
    stream.reason = 'disconnected'
    await flushPromises()
    expect(wrapper.text()).toContain('更新已暂停')
    expect(wrapper.text()).toContain('重新连接执行过程')
    await wrapper.setProps({
      contribution: { ...contribution, execution_id: id(21), generation: '2' },
    })
    expect(handle.dispose).toHaveBeenCalledOnce()
    expect(wrapper.find('[aria-label="执行过程"]').exists()).toBe(false)
  })
})
