import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AccountFailure } from '../api/client'
import { captureMeetingCommand, createMeetingsAPI, type MeetingContribution } from '../api/meetings'
import MeetingContributionVersions from '../components/meetings/MeetingContributionVersions.vue'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-14T00:00:00Z'
const message = {
  id: id(8),
  participant_id: id(5),
  content_nodes: [{ type: 'text' as const, text: '<script>old reply</script>' }],
  created_at: at,
}
const contribution: MeetingContribution = {
  id: id(4),
  turn_id: id(3),
  participant_id: id(5),
  kind: 'agent',
  order: 1,
  status: 'completed',
  generation: '2',
  attempt: '1',
  execution_id: id(7),
  waiting_on_execution_id: null,
  cancel_requested: false,
  message,
  started_at: at,
  completed_at: at,
  history: [
    {
      execution_id: id(6),
      generation: '1',
      attempt: '9007199254740993',
      status: 'succeeded',
      retry_of_execution_id: null,
      regenerate_of_execution_id: null,
      message,
    },
  ],
}
function view(c: unknown) {
  return {
    session: {
      id: id(2),
      project_id: id(1),
      status: 'active',
      version: '5',
      title: 'Discussion',
      summary: null,
      deleting: false,
      created_at: at,
      updated_at: at,
    },
    participants: [
      {
        id: id(5),
        meeting_id: id(2),
        kind: 'agent',
        source_id: id(9),
        name: 'Agent',
        order: 1,
        active: true,
      },
    ],
    references: [],
    decisions: [],
    turns: [
      {
        id: id(3),
        mode: 'sequential',
        status: 'completed',
        version: '1',
        created_at: at,
        contributions: [c],
      },
    ],
  }
}
async function parse(c: unknown) {
  return createMeetingsAPI(
    async () =>
      new Response(JSON.stringify(view(c)), {
        headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(20) },
      }),
  ).detail(id(1), id(2), new AbortController().signal)
}
const wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((w) => w.unmount())
  await flushPromises()
})
function fixture(c = contribution) {
  const w = mount(MeetingContributionVersions, {
    props: { contribution: c, mutable: true, participantName: () => 'Agent' },
    global: { stubs: { Teleport: true } },
  })
  wrappers.push(w)
  const button = (label: string) =>
    w.findAll('button').find((b) => {
      const copy = b.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
      return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
    })!
  return { w, button }
}
describe('Meeting replacement replies', () => {
  it('replies to the immutable historical message and stops after the Meeting becomes readonly', async () => {
    const { w, button } = fixture()
    await w.setProps({ canReply: true })
    await button('回复第 1 版消息').trigger('click')
    expect(w.emitted('reply')).toEqual([[message.id]])
    expect(w.emitted('restart')).toBeUndefined()
    expect(w.get('.history-message').text()).toBe('<script>old reply</script>')
    await w.setProps({ canReply: false })
    expect(button('回复第 1 版消息').attributes('disabled')).toBeDefined()
    await button('回复第 1 版消息').trigger('click')
    expect(w.emitted('reply')).toHaveLength(1)
  })
  it('parses immutable public history without losing large attempt versions and rejects mixed identity', async () => {
    const result = (await parse(contribution)).turns[0]!.contributions[0]!
    expect(result.history[0]!.attempt).toBe('9007199254740993')
    const first = contribution.history[0]!
    for (const history of [
      [{ ...first, message: { ...message, participant_id: id(90) } }],
      [first, first],
      [{ ...first, generation: '3' }],
      [{ ...first, execution_id: contribution.execution_id }],
      [{ ...first, raw_response: 'private' }],
      [{ ...first, retry_of_execution_id: id(10), regenerate_of_execution_id: id(11) }],
    ])
      await expect(parse({ ...contribution, history })).rejects.toBeInstanceOf(AccountFailure)
  })
  it.each(['retry-contribution', 'regenerate-contribution'] as const)(
    'captures %s with the exact original Turn and contribution',
    (action) => {
      const command = {
        kind: 'control',
        meeting: id(2),
        input: {
          action,
          expected_version: '9007199254740993',
          turn_id: id(3),
          contribution_id: id(4),
        },
      }
      expect(captureMeetingCommand(command)).toEqual(command)
      for (const missing of ['turn_id', 'contribution_id']) {
        const input: Record<string, unknown> = { ...command.input }
        delete input[missing]
        expect(() => captureMeetingCommand({ ...command, input })).toThrow(AccountFailure)
      }
    },
  )
  it('shows history as inert readonly content and confirms only the current reply', async () => {
    const { w, button } = fixture()
    expect(w.get('summary').text()).toBe('历史版本（1）')
    expect(w.get('.history-message').text()).toBe('<script>old reply</script>')
    expect(w.find('script').exists()).toBe(false)
    expect(w.findAll('details button')).toHaveLength(0)
    await button('重新生成回复').trigger('click')
    expect(w.get('[role="dialog"]').text()).toContain('旧版本仍可查看')
    await button('确认重新生成').trigger('click')
    expect(w.emitted('restart')).toEqual([[contribution, 'regenerate-contribution']])
  })
  it('keeps a failed replacement reply visible and prevents confirmation after the current execution changes', async () => {
    const failed: MeetingContribution = { ...contribution, status: 'failed' }
    const { w, button } = fixture(failed)
    await button('重试此 Agent').trigger('click')
    await w.setProps({ contribution: { ...failed, execution_id: id(30), attempt: '2' } })
    expect(button('确认重试').attributes('disabled')).toBeDefined()
    await button('确认重试').trigger('click')
    expect(w.emitted('restart')).toBeUndefined()
    await w.setProps({ contribution: { ...failed, status: 'running' }, mutable: false })
    expect(w.text()).toContain('当前仍显示上一次成功的回复')
    expect(w.findAll('details button')).toHaveLength(0)
  })
})
