import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AccountFailure, type Fetch } from '../api/client'
import { captureMeetingCommand, createMeetingsAPI, type MeetingProposal } from '../api/meetings'
import { createMeetingSession } from '../composables/meeting-session'
import MeetingProposalReview from '../components/meetings/MeetingProposalReview.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  meeting = id(2),
  at = '2026-10-15T00:00:00Z'
const proposal: MeetingProposal = {
  session: {
    id: meeting,
    project_id: project,
    version: '9007199254740993',
    status: 'proposed',
    title: null,
    summary: null,
    deleting: false,
    created_at: at,
    updated_at: at,
    proposal_content: 'Discuss <script>the release</script> together.',
  },
  source_execution_id: id(3),
  created_by_participant_id: id(4),
  origin: { kind: 'task', id: id(5) },
}
const input = { expected_version: proposal.session.version, approve: false }
const command = { kind: 'proposal-review' as const, meeting, input }
const receipt = { meeting_id: meeting, version: '9007199254740994', replayed: false }
const json = (v: unknown) =>
  new Response(JSON.stringify(v), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
  })

describe('Meeting proposal original HTTP and intent', () => {
  it('requires an explicit boolean decision and preserves the exact version', () => {
    expect(captureMeetingCommand(command)).toEqual(command)
    for (const value of [undefined, null, 'false', 0]) {
      expect(() =>
        captureMeetingCommand({
          ...command,
          input: { expected_version: input.expected_version, approve: value },
        }),
      ).toThrow(AccountFailure)
    }
    expect(() =>
      captureMeetingCommand({ ...command, input: { ...input, turn_id: id(6) } }),
    ).toThrow(AccountFailure)
  })
  it('reads only the requested project and proposal, including immutable origin', async () => {
    const fetcher = vi
      .fn<Fetch>()
      .mockResolvedValueOnce(json(proposal))
      .mockResolvedValueOnce(
        json({ ...proposal, session: { ...proposal.session, project_id: id(8) } }),
      )
    const api = createMeetingsAPI(fetcher)
    await expect(api.proposal(project, meeting, new AbortController().signal)).resolves.toEqual(
      proposal,
    )
    expect(String(fetcher.mock.calls[0]![0])).toBe(
      `/api/v1/projects/${project}/meetings/${meeting}/proposal`,
    )
    await expect(
      api.proposal(project, meeting, new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('queries an uncertain decision with its original key and payload, without resubmitting', async () => {
    const fetcher = vi
      .fn<Fetch>()
      .mockRejectedValueOnce(new TypeError('response lost'))
      .mockResolvedValueOnce(json({ status: 'not_found' }))
      .mockResolvedValueOnce(json({ status: 'committed', receipt }))
    const identity = { userID: id(10), sessionID: id(11), epoch: 1 }
    const owner = createMeetingSession(createMeetingsAPI(fetcher), {
      capture: () => ({ identity, csrf: 'S'.repeat(43), key: id(12) }),
      identity: () => identity,
      current: (value) => value === identity,
      busy: () => false,
      run: async (_identity, _kind, work) => work(new AbortController().signal, () => true),
      cancelRead: () => {},
    })
    await expect(owner.start(project, command)).rejects.toMatchObject({ kind: 'transport' })
    expect(owner.progress?.phase).toBe('uncertain')
    await expect(
      owner.start(project, { ...command, input: { ...input, approve: true } }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(owner.lookup()).resolves.toEqual({ status: 'not_found' })
    expect(owner.progress?.phase).toBe('uncertain')
    await expect(owner.lookup()).resolves.toEqual({ status: 'committed', receipt })
    expect(fetcher.mock.calls).toHaveLength(3)
    expect(fetcher.mock.calls.map(([path]) => String(path))).toEqual([
      `/api/v1/projects/${project}/meetings/${meeting}/proposal/review`,
      `/api/v1/projects/${project}/meetings/${meeting}/proposal/review/lookup`,
      `/api/v1/projects/${project}/meetings/${meeting}/proposal/review/lookup`,
    ])
    for (const [, options] of fetcher.mock.calls) {
      expect(new Headers(options.headers).get('Idempotency-Key')).toBe(id(12))
      expect(JSON.parse(String(options.body))).toEqual(input)
    }
  })
  it('rejects a review response that claims to create a Turn', async () => {
    const api = createMeetingsAPI(async () => json({ ...receipt, turn_id: id(6) }))
    await expect(
      api.command(project, command, { csrfToken: 'S'.repeat(43), key: id(12) }),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})

const wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((w) => w.unmount())
  await flushPromises()
})
function view() {
  const wrapper = mount(MeetingProposalReview, {
    props: {
      proposal,
      current: true,
      busy: false,
      progress: null,
      inboxPath: '/owner/demo/inbox',
      participants: [
        {
          id: id(4),
          meeting_id: meeting,
          kind: 'agent',
          source_id: id(13),
          name: 'Planner',
          order: 0,
          active: true,
        },
      ],
    },
    global: {
      stubs: {
        Teleport: true,
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })
  wrappers.push(wrapper)
  const button = (label: string) =>
    wrapper.findAll('button').find((b) => {
      const copy = b.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
      return copy.textContent?.trim() === label
    })!
  return { wrapper, button }
}
describe('Meeting proposal visible review', () => {
  it('shows the reason safely and requires a captured confirmation for either choice', async () => {
    const { wrapper, button } = view()
    expect(wrapper.get('[aria-label="会议提案"]').text()).toContain('由 Planner 提议')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.get('a').attributes('href')).toBe('/owner/demo/inbox')
    await button('接受会议').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('接受这份会议提案？')
    expect(wrapper.emitted('review')).toBeUndefined()
    await button('确认接受').trigger('click')
    expect(wrapper.emitted('review')).toEqual([
      [{ expected_version: input.expected_version, approve: true }],
    ])
    await button('拒绝并归档').trigger('click')
    await button('确认拒绝并归档').trigger('click')
    expect(wrapper.emitted('review')?.[1]).toEqual([input])
  })
  it('refuses a confirmation after the proposal version changes', async () => {
    const { wrapper, button } = view()
    await button('接受会议').trigger('click')
    await wrapper.setProps({
      proposal: { ...proposal, session: { ...proposal.session, version: '9007199254740994' } },
    })
    expect(wrapper.get('[role="dialog"]').text()).toContain('提案已变化')
    expect(button('确认接受').attributes('disabled')).toBeDefined()
    await button('确认接受').trigger('click')
    expect(wrapper.emitted('review')).toBeUndefined()
  })
  it('shows only original lookup while uncertain and respects the original identity gate', async () => {
    const { wrapper, button } = view()
    const progress = {
      project,
      meeting,
      kind: 'proposal-review' as const,
      phase: 'uncertain' as const,
      contextValid: false,
    }
    await wrapper.setProps({ progress })
    expect(button('接受会议')).toBeUndefined()
    expect(button('查询原操作').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ progress: { ...progress, contextValid: true } })
    await button('查询原操作').trigger('click')
    expect(wrapper.emitted('lookup')).toHaveLength(1)
    expect(wrapper.emitted('review')).toBeUndefined()
  })
})
