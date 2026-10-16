import { describe, expect, it, vi } from 'vitest'
import { effectScope, reactive, ref, shallowReactive, shallowRef } from 'vue'
import type { MeetingSubscriptionState } from '../composables/meeting-realtime'
import { flushPromises } from '@vue/test-utils'
import { AccountFailure } from '../api/client'
import type { MeetingDetail, MeetingProposal } from '../api/meetings'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import type { SessionController, MeetingExecutionApprovalProgress } from '../composables/useSession'
import { useProjectMeetings } from '../composables/useProjectMeetings'
import type { MeetingProgress } from '../composables/meeting-session'
import type { ArtifactUploadProgress } from '../composables/artifact-upload-session'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const instant = '2026-10-14T00:00:00Z'
const detail: MeetingDetail = {
  session: {
    id: id(2),
    project_id: id(1),
    version: '1',
    status: 'active',
    title: 'Private meeting',
    summary: null,
    deleting: false,
    created_at: instant,
    updated_at: instant,
  },
  participants: [
    {
      id: id(7),
      meeting_id: id(2),
      kind: 'agent',
      source_id: id(8),
      name: 'Agent',
      order: 1,
      active: true,
    },
  ],
  references: [],
  turns: [],
  decisions: [],
}
function setup(read: () => Promise<MeetingDetail>, readProposal?: () => Promise<MeetingProposal>) {
  const selected = ref<string | null>(id(2)),
    context = shallowRef({
      projectID: id(1),
      identity: { userID: id(5), sessionID: id(6), epoch: 1 },
      generation: 1,
      readGeneration: 1,
    })
  const progress = shallowRef<MeetingProgress | null>(null)
  const methods = {
    list: vi.fn(async () => [detail.session]),
    detail: vi.fn(read),
    proposal: vi.fn(
      readProposal ??
        (async () => {
          throw new Error('Unexpected proposal read')
        }),
    ),
    start: vi.fn(),
    lookup: vi.fn(),
    abandonRead: vi.fn(),
    get progress() {
      return progress.value
    },
  }
  const approvalProgress = shallowRef<MeetingExecutionApprovalProgress | null>(null)
  const uploadProgress = shallowRef<ArtifactUploadProgress | null>(null)
  const auth = {
    state: reactive({ busy: false }),
    meetings: methods,
    agentConfiguration: { list: vi.fn(async () => ({ items: [] })), abandonRead: vi.fn() },
    executionApproval: {
      get meetingProgress() {
        return approvalProgress.value
      },
      forgetMeetingUnavailable: vi.fn(),
    },
    artifactUploads: {
      get progress() {
        return uploadProgress.value
      },
    },
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project: { lifecycle: 'active' } },
  } as unknown as ProjectWorkspace
  const scope = effectScope(),
    owner = scope.run(() => useProjectMeetings(() => selected.value, workspace, auth))!
  return {
    selected,
    context,
    methods,
    auth,
    workspace,
    scope,
    owner,
    approvalProgress,
    progress,
    uploadProgress,
  }
}
describe('Meeting view response ownership', () => {
  it('keeps the current draft unsent while its original upload remains uncertain', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.owner.draft.text = 'Keep this draft'
      expect(f.owner.canSend.value).toBe(true)
      f.uploadProgress.value = {
        projectID: id(1),
        meetingID: id(2),
        name: 'notes.txt',
        byteSize: '8',
        phase: 'uncertain',
        observation: 'not_observed',
        receipt: null,
        contextValid: true,
        canLookup: true,
        canComplete: false,
        canCancel: true,
      }
      expect(f.owner.fileUploadPending.value).toBe(true)
      expect(f.owner.canSend.value).toBe(false)
      await f.owner.submit()
      expect(f.methods.start).not.toHaveBeenCalled()
      expect(f.owner.draft.nodes).toEqual([{ type: 'text', text: 'Keep this draft' }])
      f.uploadProgress.value = { ...f.uploadProgress.value, meetingID: id(99) }
      expect(f.owner.canSend.value).toBe(true)
      f.uploadProgress.value = {
        ...f.uploadProgress.value,
        meetingID: id(2),
        phase: 'cancelled',
        observation: 'cancelled',
      }
      expect(f.owner.canSend.value).toBe(true)
      expect(f.owner.draft.nodes).toEqual([{ type: 'text', text: 'Keep this draft' }])
    } finally {
      f.scope.stop()
    }
  })
  it('sends ordered draft identities and blocks a removed Agent mention without rewriting it', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.selected.value = null
      await flushPromises()
      f.owner.draft.agents = [id(8)]
      const nodes = [
        { type: 'text' as const, text: 'Please check ' },
        { type: 'mention' as const, agent_id: id(8) },
        {
          type: 'knowledge' as const,
          reference: { kind: 'knowledge' as const, id: id(20), label: 'Design' },
        },
      ]
      f.owner.draft.nodes = nodes
      expect(f.owner.canSend.value).toBe(true)
      f.owner.toggleAgent(id(8), false)
      expect(f.owner.canSend.value).toBe(false)
      expect(f.owner.draft.nodes).toEqual(nodes)
      f.owner.toggleAgent(id(8), true)
      f.methods.start.mockResolvedValue({ meeting_id: id(30), version: '1', replayed: false })
      await f.owner.submit()
      expect(f.methods.start).toHaveBeenCalledWith(id(1), {
        kind: 'start',
        input: { agents: [id(8)], mode: 'parallel', content_nodes: nodes },
      })
      expect(f.owner.draft.nodes).toEqual([{ type: 'text', text: '' }])
    } finally {
      f.scope.stop()
    }
  })
  it('pins an original inline identity separately without changing the message or current draft', async () => {
    const reference = { kind: 'link' as const, id: 'HTTPS://Example.test:443', label: 'Design' }
    const message = {
      id: id(20),
      participant_id: id(7),
      content_nodes: [{ type: 'link' as const, reference }],
      created_at: instant,
    }
    const view: MeetingDetail = {
      ...detail,
      turns: [
        {
          id: id(3),
          mode: 'parallel',
          status: 'completed',
          version: '1',
          created_at: instant,
          contributions: [
            {
              id: id(4),
              turn_id: id(3),
              participant_id: id(7),
              kind: 'agent',
              order: 1,
              status: 'completed',
              generation: '1',
              attempt: '1',
              execution_id: id(9),
              waiting_on_execution_id: null,
              cancel_requested: false,
              message,
              history: [],
              started_at: instant,
              completed_at: instant,
            },
          ],
        },
      ],
    }
    const original = JSON.stringify(message),
      f = setup(async () => view)
    try {
      await flushPromises()
      f.owner.draft.text = 'Keep writing'
      expect(await f.owner.pinReference({ kind: 'task', id: id(99) })).toBe(false)
      f.methods.start.mockResolvedValue({ meeting_id: id(2), version: '2', replayed: false })
      f.methods.detail.mockResolvedValue({
        ...view,
        session: { ...view.session, version: '2' },
        references: [{ ...reference, id: 'https://example.test/' }],
      })
      expect(await f.owner.pinReference(reference)).toBe(true)
      expect(f.methods.start).toHaveBeenCalledWith(id(1), {
        kind: 'references',
        meeting: id(2),
        input: { expected_version: '1', references: [reference] },
      })
      expect(f.owner.referencePinned(reference)).toBe(true)
      expect(await f.owner.pinReference(reference)).toBe(true)
      expect(f.methods.start).toHaveBeenCalledTimes(1)
      expect(f.owner.draft.text).toBe('Keep writing')
      expect(JSON.stringify(message)).toBe(original)
    } finally {
      f.scope.stop()
    }
  })
  it('keeps a reply to an original message across replacement, conflict and Unknown until lookup confirms', async () => {
    const message = {
      id: id(20),
      participant_id: id(7),
      content_nodes: [{ type: 'text' as const, text: 'Original answer' }],
      created_at: instant,
    }
    const contribution = {
      id: id(4),
      turn_id: id(3),
      participant_id: id(7),
      kind: 'agent' as const,
      order: 1,
      status: 'completed' as const,
      generation: '1',
      attempt: '1',
      execution_id: id(9),
      waiting_on_execution_id: null,
      cancel_requested: false,
      message,
      history: [],
      started_at: instant,
      completed_at: instant,
    }
    const view: MeetingDetail = {
      ...detail,
      turns: [
        {
          id: id(3),
          mode: 'parallel',
          status: 'completed',
          version: '1',
          created_at: instant,
          contributions: [contribution],
        },
      ],
    }
    const f = setup(async () => view)
    try {
      await flushPromises()
      expect(f.owner.replyToMessage(id(99))).toBe(false)
      expect(f.owner.replyToMessage(message.id)).toBe(true)
      expect(f.owner.dirty.value).toBe(true)
      expect(f.owner.messagePreview(message.id)).toBe('Agent：Original answer')
      f.owner.draft.text = 'Follow up'
      f.methods.detail.mockResolvedValue({
        ...view,
        session: { ...view.session, version: '2' },
        turns: [
          {
            ...view.turns[0]!,
            contributions: [
              {
                ...contribution,
                generation: '2',
                execution_id: id(10),
                message: { ...message, id: id(21) },
                history: [
                  {
                    execution_id: id(9),
                    generation: '1',
                    attempt: '1',
                    status: 'succeeded',
                    retry_of_execution_id: null,
                    regenerate_of_execution_id: null,
                    message,
                  },
                ],
              },
            ],
          },
        ],
      })
      await f.owner.loadDetail()
      expect(f.owner.draft.replyTo).toBe(message.id)
      expect(f.owner.canSend.value).toBe(true)
      f.methods.start.mockRejectedValue(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:version-conflict',
          title: 'Conflict',
          status: 409,
          detail: '',
          instance: '/',
          code: 'VERSION_CONFLICT',
          request_id: id(30),
          commit_state: 'not_committed',
        }),
      )
      await f.owner.submit()
      expect(f.owner.draft.replyTo).toBe(message.id)
      expect(f.owner.draft.text).toBe('Follow up')
      expect(f.methods.start).toHaveBeenLastCalledWith(id(1), {
        kind: 'send',
        meeting: id(2),
        input: {
          expected_version: '2',
          mode: 'parallel',
          content_nodes: [{ type: 'text', text: 'Follow up' }],
          interrupt: false,
          reply_to_message_id: message.id,
        },
      })
      f.progress.value = {
        project: id(1),
        meeting: id(2),
        kind: 'send',
        phase: 'uncertain',
        contextValid: true,
      }
      expect(f.owner.cancelReply()).toBe(false)
      expect(f.owner.replyToMessage(id(21))).toBe(false)
      f.methods.lookup.mockResolvedValueOnce({ status: 'not_found' })
      await f.owner.submit(true)
      expect(f.owner.draft.replyTo).toBe(message.id)
      f.methods.lookup.mockResolvedValueOnce({
        status: 'committed',
        receipt: { meeting_id: id(2), version: '3', replayed: true },
      })
      await f.owner.submit(true)
      expect(f.owner.draft.replyTo).toBe('')
      expect(f.owner.draft.text).toBe('')
    } finally {
      f.scope.stop()
    }
  })
  it('cancels a local reply without clearing its text and drops it on meeting navigation', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.owner.draft.text = 'Draft'
      f.owner.draft.replyTo = id(99)
      expect(f.owner.canSend.value).toBe(false)
      expect(f.owner.cancelReply()).toBe(true)
      expect(f.owner.draft.text).toBe('Draft')
      f.owner.draft.replyTo = id(99)
      f.selected.value = null
      expect(f.owner.draft.replyTo).toBe('')
      expect(f.owner.draft.text).toBe('')
    } finally {
      f.scope.stop()
    }
  })
  it('continues polling a replacement in a completed Turn and cannot restart a stale execution', async () => {
    vi.useFakeTimers()
    const contribution = {
      id: id(4),
      turn_id: id(3),
      participant_id: id(5),
      kind: 'agent' as const,
      order: 1,
      status: 'running' as const,
      generation: '2',
      attempt: '1',
      execution_id: id(7),
      waiting_on_execution_id: null,
      cancel_requested: false,
      message: null,
      history: [],
      started_at: instant,
      completed_at: null,
    }
    const view: MeetingDetail = {
      ...detail,
      turns: [
        {
          id: id(3),
          mode: 'sequential',
          status: 'completed',
          version: '1',
          created_at: instant,
          contributions: [contribution],
        },
      ],
    }
    const f = setup(async () => view)
    try {
      await flushPromises()
      expect(f.owner.activeTurns.value).toBe(false)
      expect(f.owner.state.polling).toBe(true)
      await vi.advanceTimersByTimeAsync(2500)
      expect(f.methods.detail).toHaveBeenCalledTimes(2)
      f.methods.detail.mockResolvedValue({
        ...view,
        turns: [{ ...view.turns[0]!, contributions: [{ ...contribution, status: 'failed' }] }],
      })
      await f.owner.loadDetail()
      await f.owner.restartContribution(
        { ...contribution, status: 'failed', execution_id: id(30) },
        'retry-contribution',
      )
      expect(f.methods.start).not.toHaveBeenCalled()
      f.methods.start.mockResolvedValue({ meeting_id: id(2), version: '2', replayed: false })
      await f.owner.restartContribution({ ...contribution, status: 'failed' }, 'retry-contribution')
      expect(f.methods.start).toHaveBeenCalledWith(id(1), {
        kind: 'control',
        meeting: id(2),
        input: {
          action: 'retry-contribution',
          expected_version: '1',
          turn_id: id(3),
          contribution_id: id(4),
        },
      })
    } finally {
      f.scope.stop()
      vi.useRealTimers()
    }
  })
  it('keeps navigation and unrelated commands closed while the original Meeting approval is unknown', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.approvalProgress.value = {
        projectID: id(1),
        meetingID: id(2),
        contributionID: id(3),
        executionID: id(4),
        generation: '1',
        attempt: '1',
        approvalID: id(7),
        decision: 'approve',
        phase: 'uncertain',
        receipt: null,
        observation: 'none',
        contextValid: true,
      }
      expect(f.owner.unsettled.value).toBe(true)
      expect(await f.owner.confirmLeave()).toBe(false)
      expect(f.owner.canControl.value).toBe(false)
      f.approvalProgress.value = { ...f.approvalProgress.value!, meetingID: id(10) }
      expect(f.owner.unsettled.value).toBe(false)
    } finally {
      f.scope.stop()
    }
  })
  it('discards a completed old detail after selection changes', async () => {
    let resolve!: (v: MeetingDetail) => void
    const f = setup(
      () =>
        new Promise((answer) => {
          resolve = answer
        }),
    )
    try {
      await flushPromises()
      expect(f.methods.detail).toHaveBeenCalledTimes(1)
      f.selected.value = null
      resolve(detail)
      await flushPromises()
      expect(f.owner.state.detail).toBeNull()
      expect(f.methods.abandonRead).toHaveBeenCalled()
    } finally {
      f.scope.stop()
    }
  })
  it('clears private timeline on access denial and keeps draft on a definite send conflict', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      expect(f.owner.state.detail?.session.title).toBe('Private meeting')
      f.owner.draft.text = 'Keep this draft'
      f.methods.start.mockRejectedValue(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:version-conflict',
          title: 'Conflict',
          status: 409,
          detail: '',
          instance: '/',
          code: 'VERSION_CONFLICT',
          request_id: id(9),
          commit_state: 'not_committed',
        }),
      )
      await f.owner.submit()
      expect(f.owner.draft.text).toBe('Keep this draft')
      expect(f.owner.state.feedback).toContain('新变化')
      f.methods.detail.mockRejectedValue(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:forbidden',
          title: 'Forbidden',
          status: 403,
          detail: '',
          instance: '/',
          code: 'FORBIDDEN',
          request_id: id(9),
          commit_state: 'not_started',
        }),
      )
      await f.owner.loadDetail()
      expect(f.owner.state.detail).toBeNull()
      expect(f.owner.state.items).toEqual([])
      expect(f.owner.state.detailMessage).toContain('无权')
    } finally {
      f.scope.stop()
    }
  })
  it('requires an explicit choice before discarding an unsent draft', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.owner.draft.text = 'Unsent'
      const first = f.owner.confirmLeave()
      expect(f.owner.state.leaveOpen).toBe(true)
      f.owner.answerLeave(false)
      expect(await first).toBe(false)
      expect(f.owner.draft.text).toBe('Unsent')
      const second = f.owner.confirmLeave()
      f.owner.answerLeave(true)
      expect(await second).toBe(true)
      expect(f.owner.draft.text).toBe('')
    } finally {
      f.scope.stop()
    }
  })
})

describe('Meeting Human controls', () => {
  it('keeps metadata drafts in navigation protection and rejects an outdated roster version', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.owner.state.metadataDirty = true
      const leaving = f.owner.confirmLeave()
      expect(f.owner.state.leaveOpen).toBe(true)
      f.owner.answerLeave(false)
      expect(await leaving).toBe(false)
      const command = {
        kind: 'participants' as const,
        meeting: id(2),
        input: { expected_version: '2', agents: [id(8)] },
      }
      expect(await f.owner.updateMetadata(command)).toBe(false)
      expect(f.methods.start).not.toHaveBeenCalled()
      f.methods.start.mockResolvedValue({ meeting_id: id(2), version: '2', replayed: false })
      expect(
        await f.owner.updateMetadata({
          ...command,
          input: { ...command.input, expected_version: '1' },
        }),
      ).toBe(true)
      expect(f.methods.start).toHaveBeenCalledTimes(1)
    } finally {
      f.scope.stop()
    }
  })
  it('uses the original lookup for uncertain reference edits and keeps message drafts intact', async () => {
    const f = setup(async () => detail)
    try {
      await flushPromises()
      f.owner.draft.text = 'A later message'
      f.progress.value = {
        project: id(1),
        meeting: id(2),
        kind: 'references',
        phase: 'uncertain',
        contextValid: true,
      }
      f.methods.lookup.mockResolvedValueOnce({ status: 'not_found' }).mockResolvedValueOnce({
        status: 'found',
        receipt: { meeting_id: id(2), version: '2', replayed: true },
      })
      expect(await f.owner.submit(true)).toBeNull()
      expect(f.owner.state.feedback).toContain('尚未查到原操作')
      expect(await f.owner.submit(true)).toBe(id(2))
      expect(f.methods.start).not.toHaveBeenCalled()
      expect(f.methods.lookup).toHaveBeenCalledTimes(2)
      expect(f.owner.draft.text).toBe('A later message')
    } finally {
      f.scope.stop()
    }
  })
  it('does not start a new Turn with an explicitly empty Agent roster', async () => {
    const f = setup(async () => ({ ...detail, participants: [] }))
    try {
      await flushPromises()
      f.owner.draft.text = 'Wait for participants'
      expect(f.owner.canSend.value).toBe(false)
      await f.owner.submit()
      expect(f.methods.start).not.toHaveBeenCalled()
      expect(f.owner.canWrite.value).toBe(true)
    } finally {
      f.scope.stop()
    }
  })
  it('submits the Decision version and preserves unrelated message drafts', async () => {
    const decision = {
      id: id(20),
      contribution_id: id(21),
      execution_id: id(22),
      question: 'Choose?',
      options: ['A', 'B'],
      status: 'pending' as const,
      answer: null,
      version: '4',
    }
    const f = setup(async () => ({ ...detail, decisions: [decision] }))
    try {
      await flushPromises()
      f.owner.draft.text = 'A separate next message'
      f.owner.answers[decision.id] = 'Use B'
      f.methods.start.mockResolvedValue({ meeting_id: id(2), version: '10', replayed: false })
      await f.owner.respond(decision, 'answer')
      expect(f.methods.start).toHaveBeenCalledWith(id(1), {
        kind: 'decision',
        meeting: id(2),
        input: {
          decision_id: decision.id,
          action: 'answer',
          expected_version: '4',
          answer: 'Use B',
        },
      })
      expect(f.owner.draft.text).toBe('A separate next message')
      expect(f.owner.answers[decision.id]).toBeUndefined()
    } finally {
      f.scope.stop()
    }
  })
  it('keeps answer drafts on conflict and lets an archived session be restored', async () => {
    const decision = {
      id: id(20),
      contribution_id: id(21),
      execution_id: id(22),
      question: 'Choose?',
      options: [],
      status: 'pending' as const,
      answer: null,
      version: '1',
    }
    const f = setup(async () => ({
      ...detail,
      session: { ...detail.session, status: 'archive' },
      decisions: [decision],
    }))
    try {
      await flushPromises()
      expect(f.owner.canWrite.value).toBe(false)
      expect(f.owner.canControl.value).toBe(true)
      f.owner.answers[decision.id] = 'Keep this answer'
      f.methods.start.mockRejectedValue(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:version-conflict',
          title: 'Conflict',
          status: 409,
          detail: '',
          instance: '/',
          code: 'VERSION_CONFLICT',
          request_id: id(9),
          commit_state: 'not_committed',
        }),
      )
      await f.owner.respond(decision, 'answer')
      expect(f.owner.answers[decision.id]).toBe('Keep this answer')
      expect(f.owner.dirty.value).toBe(true)
      await f.owner.control('unarchive')
      expect(f.methods.start).toHaveBeenLastCalledWith(id(1), {
        kind: 'control',
        meeting: id(2),
        input: { action: 'unarchive', expected_version: '1' },
      })
    } finally {
      f.scope.stop()
    }
  })
})

describe('Meeting proposal page joins the original read and command owner', () => {
  const proposed: MeetingDetail = {
    ...detail,
    session: { ...detail.session, status: 'proposed', proposal_content: 'Discuss the release.' },
  }
  const source: MeetingProposal = {
    session: proposed.session,
    source_execution_id: id(20),
    created_by_participant_id: id(7),
    origin: { kind: 'task', id: id(21) },
  }
  it('requires matching detail and proposal versions, then uses the original review lane', async () => {
    const f = setup(
      async () => proposed,
      async () => source,
    )
    try {
      await flushPromises()
      expect(f.owner.canWrite.value).toBe(false)
      expect(f.owner.canReviewProposal.value).toBe(true)
      await expect(f.owner.reviewProposal({ expected_version: '2', approve: true })).resolves.toBe(
        false,
      )
      expect(f.methods.start).not.toHaveBeenCalled()
      f.methods.start.mockResolvedValue({ meeting_id: id(2), version: '2', replayed: false })
      const accepted = {
        ...proposed,
        session: { ...proposed.session, version: '2', status: 'active' as const },
      }
      f.methods.detail.mockResolvedValue(accepted)
      f.methods.proposal.mockResolvedValue({ ...source, session: accepted.session })
      await expect(f.owner.reviewProposal({ expected_version: '1', approve: true })).resolves.toBe(
        true,
      )
      expect(f.methods.start).toHaveBeenCalledExactlyOnceWith(id(1), {
        kind: 'proposal-review',
        meeting: id(2),
        input: { expected_version: '1', approve: true },
      })
      expect(f.owner.state.detail?.turns).toHaveLength(0)
      expect(f.owner.canReviewProposal.value).toBe(false)
      expect(f.owner.canWrite.value).toBe(true)
    } finally {
      f.scope.stop()
    }
  })
  it('does not combine an old detail with a changed proposal', async () => {
    const f = setup(
      async () => proposed,
      async () => ({ ...source, session: { ...source.session, version: '2' } }),
    )
    try {
      await flushPromises()
      expect(f.owner.state.proposal).toBeNull()
      expect(f.owner.state.proposalMessage).toContain('提案已有新变化')
      await expect(f.owner.reviewProposal({ expected_version: '1', approve: false })).resolves.toBe(
        false,
      )
      expect(f.methods.start).not.toHaveBeenCalled()
    } finally {
      f.scope.stop()
    }
  })
  it('drops a late proposal after navigation instead of granting a stale review', async () => {
    let resolve!: (value: MeetingProposal) => void
    const f = setup(
      async () => proposed,
      () =>
        new Promise<MeetingProposal>((r) => {
          resolve = r
        }),
    )
    try {
      await flushPromises()
      expect(f.methods.proposal).toHaveBeenCalledOnce()
      f.selected.value = null
      await flushPromises()
      resolve(source)
      await flushPromises()
      expect(f.owner.state.proposal).toBeNull()
      expect(f.owner.canReviewProposal.value).toBe(false)
      expect(f.methods.start).not.toHaveBeenCalled()
    } finally {
      f.scope.stop()
    }
  })
})

describe('Meeting deletion page state', () => {
  it('keeps accepted cancellation distinct from deletion and clears the old detail only on the completed receipt', async () => {
    const f = setup(async () => detail)
    await flushPromises()
    const accepted = {
      deletion_id: id(30),
      meeting_id: id(2),
      accepted_version: '2',
      phase: 'cancelling' as const,
      requested_at: instant,
      replayed: false,
    }
    f.methods.start.mockImplementationOnce(async () => {
      f.progress.value = {
        project: id(1),
        meeting: id(2),
        kind: 'delete',
        phase: 'processing',
        contextValid: true,
      }
      return accepted
    })
    expect(await f.owner.deleteMeeting('2')).toBeNull()
    expect(f.methods.start).not.toHaveBeenCalled()
    await f.owner.deleteMeeting('1')
    expect(f.owner.state.deletion?.phase).toBe('cancelling')
    expect(f.owner.state.detail).not.toBeNull()
    expect(f.owner.state.feedback).not.toBe('会议已删除。')
    expect(f.owner.canDelete.value).toBe(false)
    f.methods.lookup.mockImplementationOnce(async () => {
      f.progress.value = {
        project: id(1),
        meeting: id(2),
        kind: 'delete',
        phase: 'confirmed',
        contextValid: false,
      }
      return {
        status: 'committed',
        receipt: { ...accepted, phase: 'deleted', deleted_at: instant },
      }
    })
    await f.owner.submit(true)
    expect(f.owner.state.deletion?.phase).toBe('deleted')
    expect(f.owner.state.detail).toBeNull()
    expect(f.owner.state.items).toEqual([])
    expect(f.owner.state.feedback).toBe('会议已删除。')
    f.scope.stop()
  })
})

describe('Meeting subscribed timeline view', () => {
  it('applies canonical snapshots without polling or clearing the draft and rejects a retired source', async () => {
    vi.useFakeTimers()
    const f = setup(async () => detail)
    const state = shallowReactive<MeetingSubscriptionState>({
      phase: 'connecting',
      reason: '',
      snapshot: null,
    })
    const handle = { state, refresh: vi.fn(), dispose: vi.fn() }
    const subscribe = vi.fn(() => handle)
    Object.assign(f.auth, { meetingRealtime: { subscribe } })
    try {
      await flushPromises()
      expect(subscribe).toHaveBeenCalledWith({
        kind: 'meeting.timeline',
        project_id: id(1),
        meeting_id: id(2),
      })
      f.owner.draft.text = 'Keep this unsent draft.'
      state.phase = 'ready'
      state.snapshot = {
        topic: { kind: 'meeting.timeline', project_id: id(1), meeting_id: id(2) },
        schema: 'meeting_timeline_v1',
        digest: 'validated-by-original-codec',
        content: {
          ...detail,
          session: { ...detail.session, version: '2', title: 'Current meeting' },
        },
      }
      await flushPromises()
      expect(f.owner.state.detail?.session.title).toBe('Current meeting')
      expect(f.owner.draft.text).toBe('Keep this unsent draft.')
      await vi.advanceTimersByTimeAsync(5000)
      expect(f.methods.detail).toHaveBeenCalledTimes(1)
      f.owner.reconnect()
      expect(handle.refresh).toHaveBeenCalledOnce()
      f.selected.value = null
      await flushPromises()
      expect(handle.dispose).toHaveBeenCalledOnce()
      state.snapshot = {
        ...state.snapshot,
        content: { ...detail, session: { ...detail.session, version: '3' } },
      }
      await flushPromises()
      expect(f.owner.state.detail).toBeNull()
    } finally {
      f.scope.stop()
      vi.useRealTimers()
    }
  })
})
