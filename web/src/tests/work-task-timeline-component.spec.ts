import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import TaskTimeline from '../components/work/TaskTimeline.vue'
import type { TaskTimelineEvent } from '../api/work-task-timeline'
import type { TaskTimelineState } from '../composables/useProjectTasks'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000001Z'
const common = { project_id: id(1), task_id: id(2), task_version: '4', operation_id: id(3), correlation_id: id(3), created_at: time }
const comment: TaskTimelineEvent = { ...common, id: id(4), actor: { type: 'human', source: 'task_domain', user_id: id(5) }, type: 'comment', payload: { body: '  原评审说明\n<script>alert("plain text")</script>\n完整保留末行。  ' } }
const claimed: TaskTimelineEvent = { ...common, id: id(6), actor: { type: 'system', service_name: 'scheduler', source: 'scheduler', cause_id: id(3) }, type: 'state_changed', payload: { from_state: 'todo', to_state: 'in_progress', reason_code: 'scheduler_claim' } }
const assigned: TaskTimelineEvent = { ...common, id: id(7), actor: comment.actor, type: 'assignee_changed', payload: { from_agent_id: null, to_agent_id: id(8) } }
let wrapper: VueWrapper | undefined
function button(label: string) {
  const result = wrapper!.findAll('button').find((node) => node.find('.button-label[aria-hidden="false"]').text() === label)
  if (!result) throw new Error(`Missing button ${label}`)
  return result
}
afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = '' })
describe('Task timeline detail presentation', () => {
  it('shows structured facts and complete plain comments with keyboard-accessible identity details', async () => {
    const state: TaskTimelineState = { phase: 'ready', items: [comment, claimed, assigned], next: 'older-page', message: '', retryMore: false }
    wrapper = mount(TaskTimeline, { attachTo: document.body, props: { state, busy: false, agentName: (agent: string | null) => agent === null ? '未指派' : '原评审 Agent' } })
    expect(wrapper.get('section').attributes('aria-label')).toBe('任务历史')
    expect(wrapper.findAll('ol > li')).toHaveLength(3)
    expect(wrapper.get('.event-text').element.textContent).toBe(comment.payload.body)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('待执行 → 进行中')
    expect(wrapper.text()).toContain('系统调度')
    expect(wrapper.text()).toContain('调度器领取任务')
    expect(wrapper.text()).toContain('未指派 → 原评审 Agent')
    expect(wrapper.findAll('time').every((item) => item.attributes('datetime') === time)).toBe(true)
    const details = wrapper.get('summary')
    ;(details.element as HTMLElement).focus()
    expect(document.activeElement).toBe(details.element)
    expect(wrapper.get('details').text()).toContain(id(5))
    const earlier = button('加载较早历史')
    ;(earlier.element as HTMLButtonElement).focus()
    await earlier.trigger('click')
    expect(wrapper.emitted('load')).toEqual([[true]])
    await wrapper.setProps({ state: { ...state, phase: 'loading' }, busy: true })
    expect(wrapper.get('section').attributes('aria-busy')).toBe('true')
    expect((button('加载较早历史').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.get('.event-text').element.textContent).toBe(comment.payload.body)
    await wrapper.setProps({ state: { ...state, items: [{ ...comment, id: id(9), created_at: '2026-10-11T10:00:00.000000Z' }, ...state.items] }, busy: false })
    expect(document.activeElement).toBe(earlier.element)
    await button('重新读取历史').trigger('click')
    expect(wrapper.emitted('load')).toEqual([[true], [false]])
  })

  it('keeps a failed older page separate from accepted history and exposes local retry and empty states', async () => {
    const state: TaskTimelineState = { phase: 'error', items: [comment], next: 'original-cursor', message: '未取得完整历史。', retryMore: true }
    wrapper = mount(TaskTimeline, { attachTo: document.body, props: { state, busy: false, agentName: () => 'Agent 信息不可用' } })
    expect(wrapper.get('[role="alert"]').text()).toContain('未取得完整历史。')
    expect(wrapper.findAll('ol > li')).toHaveLength(1)
    await button('重试读取历史').trigger('click')
    expect(wrapper.emitted('load')).toEqual([[true]])
    await wrapper.setProps({ state: { ...state, phase: 'ready', items: [], next: undefined, message: '', retryMore: false } })
    expect(wrapper.text()).toContain('暂无任务历史。')
    expect(wrapper.find('ol').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(1)
    await button('重新读取历史').trigger('click')
    expect(wrapper.emitted('load')).toEqual([[true], [false]])
  })
})
