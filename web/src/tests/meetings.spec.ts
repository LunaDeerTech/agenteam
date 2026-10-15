import { describe, expect, it, vi } from 'vitest'
import { watch } from 'vue'
import { AccountFailure, type Fetch } from '../api/client'
import {
  createMeetingsAPI,
  captureMeetingCommand,
  parseMeetingContributionExecution,
  type MeetingsAPI,
  type MeetingCommand,
} from '../api/meetings'
import { createMeetingSession } from '../composables/meeting-session'
import { createSessionController } from '../composables/useSession'
import { createAccountAPI } from '../api/account'
import { projectRoute } from '../router/auth'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  meeting = id(2),
  instant = '2026-10-14T00:00:00.000000Z'
const command: MeetingCommand = {
  kind: 'send',
  meeting,
  input: {
    expected_version: '9007199254740993',
    mode: 'parallel',
    content_nodes: [{ type: 'text', text: 'Discuss.' }],
    interrupt: false,
  },
}
const receipt = {
  meeting_id: meeting,
  turn_id: id(3),
  version: '9007199254740994',
  replayed: false,
}
const session = {
  id: meeting,
  project_id: project,
  status: 'active',
  version: '9007199254740993',
  title: null,
  summary: null,
  deleting: false,
  created_at: instant,
  updated_at: instant,
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
  })
describe('Meeting wire and route contracts', () => {
  it('sends the same optional reply target through Send and its original lookup', async () => {
    const fetch = vi.fn<Fetch>(async (url) =>
      json(url.endsWith('/lookup') ? { status: 'committed', receipt } : receipt),
    )
    const api = createMeetingsAPI(fetch)
    const reply: MeetingCommand = {
      kind: 'send',
      meeting,
      input: { ...command.input, reply_to_message_id: id(20) },
    }
    const write = { csrfToken: 'S'.repeat(43), key: id(8) }
    await api.command(project, reply, write)
    await api.lookup(project, reply, write)
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `/api/v1/projects/${project}/meetings/${meeting}/turns`,
      `/api/v1/projects/${project}/meetings/${meeting}/turns/lookup`,
    ])
    for (const [, options] of fetch.mock.calls) {
      expect(JSON.parse(options.body as string)).toEqual(reply.input)
      expect(new Headers(options.headers).get('Idempotency-Key')).toBe(write.key)
    }
    expect(captureMeetingCommand(command).input).not.toHaveProperty('reply_to_message_id')
    for (const target of [null, '', id(26).toUpperCase(), 'message'])
      expect(() =>
        captureMeetingCommand({ ...reply, input: { ...reply.input, reply_to_message_id: target } }),
      ).toThrow(AccountFailure)
    expect(() =>
      captureMeetingCommand({
        kind: 'start',
        input: {
          agents: [id(6)],
          mode: 'parallel',
          content_nodes: [{ type: 'text', text: 'Start' }],
          reply_to_message_id: id(20),
        },
      }),
    ).toThrow(AccountFailure)
  })
  it('captures explicit roster and typed references without changing original request identity', () => {
    const roster = captureMeetingCommand({
      kind: 'participants',
      meeting,
      input: { expected_version: '9007199254740993', agents: [id(5), id(4)] },
    })
    expect(roster.kind).toBe('participants')
    if (roster.kind === 'participants') expect(roster.input.agents).toEqual([id(5), id(4)])
    expect(
      captureMeetingCommand({
        kind: 'participants',
        meeting,
        input: { expected_version: '1', agents: [] },
      }).kind,
    ).toBe('participants')
    const refs = captureMeetingCommand({
      kind: 'references',
      meeting,
      input: {
        expected_version: '1',
        references: [
          { kind: 'link', id: 'HTTPS://Example.com:443/a?q=1#anchor' },
          { kind: 'task', id: id(4) },
        ],
      },
    })
    if (refs.kind === 'references')
      expect(refs.input.references[0]?.id).toBe('HTTPS://Example.com:443/a?q=1#anchor')
    else throw new Error('wrong command kind')
    for (const input of [
      { kind: 'participants', meeting, input: { expected_version: '1' } },
      {
        kind: 'participants',
        meeting,
        input: { expected_version: '1', agents: [id(4), id(4)] },
      },
      {
        kind: 'references',
        meeting,
        input: {
          expected_version: '1',
          references: [{ kind: 'link', id: 'https://user:password@example.com/' }],
        },
      },
      {
        kind: 'references',
        meeting,
        input: { expected_version: '1', references: [{ kind: 'file', id: 'storage-key' }] },
      },
    ])
      expect(() => captureMeetingCommand(input)).toThrow(AccountFailure)
  })
  it('reads only the current contribution Execution and rejects mixed wait facts', async () => {
    const contribution = id(3)
    const execution = {
      id: id(4),
      project_id: project,
      meeting_id: meeting,
      turn_id: id(5),
      contribution_id: contribution,
      participant_id: id(6),
      agent_id: id(7),
      generation: '2',
      attempt: '3',
      version: '9007199254740993',
      status: 'waiting',
      waiting_reason: 'approval',
      waiting_reference: id(8),
      cancel_requested_at: null,
      created_at: instant,
      started_at: instant,
      completed_at: null,
    }
    const fetch = vi.fn<Fetch>(async () => json({ execution }))
    const result = await createMeetingsAPI(fetch).contributionExecution(
      project,
      meeting,
      contribution,
      new AbortController().signal,
    )
    expect(result).toEqual(execution)
    expect(fetch.mock.calls[0]?.[0]).toBe(
      `/api/v1/projects/${project}/meetings/${meeting}/contributions/${contribution}/execution`,
    )
    expect(fetch.mock.calls[0]?.[1].method).toBe('GET')
    for (const change of [
      { project_id: id(10) },
      { meeting_id: id(10) },
      { contribution_id: id(10) },
      { attempt: '0' },
      { version: 1 },
      { status: 'running' },
      { waiting_reference: null },
      { waiting_reason: 'task' },
      { completed_at: instant },
      { started_at: null },
      { raw_response: 'hidden' },
    ])
      expect(() =>
        parseMeetingContributionExecution(
          { ...execution, ...change },
          project,
          meeting,
          contribution,
        ),
      ).toThrow(AccountFailure)
  })
  it('preserves a single contribution cancellation and its exact command target', async () => {
    const contribution = {
      id: id(4),
      turn_id: id(3),
      participant_id: id(5),
      kind: 'agent',
      order: 1,
      status: 'running',
      generation: '1',
      attempt: '1',
      execution_id: id(6),
      waiting_on_execution_id: null,
      cancel_requested: true,
      message: null,
      history: [],
      started_at: instant,
      completed_at: null,
    }
    const view = {
      session,
      participants: [
        {
          id: id(5),
          meeting_id: meeting,
          kind: 'agent',
          source_id: id(7),
          name: 'Agent',
          order: 1,
          active: true,
        },
      ],
      references: [],
      turns: [
        {
          id: id(3),
          mode: 'sequential',
          status: 'running',
          version: '1',
          created_at: instant,
          contributions: [contribution],
        },
      ],
      decisions: [],
    }
    const api = createMeetingsAPI(async () => json(view))
    expect(
      (await api.detail(project, meeting, new AbortController().signal)).turns[0]?.contributions[0]
        ?.cancel_requested,
    ).toBe(true)
    const cancel = {
      kind: 'control',
      meeting,
      input: {
        action: 'cancel-contribution',
        expected_version: '9',
        turn_id: id(3),
        contribution_id: id(4),
      },
    }
    expect(captureMeetingCommand(cancel)).toEqual(cancel)
    expect(() =>
      captureMeetingCommand({ ...cancel, input: { ...cancel.input, contribution_id: undefined } }),
    ).toThrow(AccountFailure)
    for (const invalid of [
      { ...contribution, cancel_requested: 'true' },
      { ...contribution, execution_id: null },
    ])
      await expect(
        createMeetingsAPI(async () =>
          json({ ...view, turns: [{ ...view.turns[0], contributions: [invalid] }] }),
        ).detail(project, meeting, new AbortController().signal),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('binds readonly views to exact project and rejects hidden fields', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ meetings: [session] })),
      api = createMeetingsAPI(fetch)
    expect((await api.list(project, new AbortController().signal))[0]?.version).toBe(
      '9007199254740993',
    )
    expect(fetch.mock.calls[0]?.[0]).toBe(`/api/v1/projects/${project}/meetings`)
    for (const row of [
      { ...session, project_id: id(10) },
      { ...session, secret: 'hidden' },
    ])
      await expect(
        createMeetingsAPI(async () => json({ meetings: [row] })).list(
          project,
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('sends lookup with unchanged precise input and original key', async () => {
    const fetch = vi.fn<Fetch>(async (url) =>
        json(url.endsWith('/lookup') ? { status: 'committed', receipt } : receipt),
      ),
      api = createMeetingsAPI(fetch),
      write = { csrfToken: 'S'.repeat(43), key: id(8) }
    await api.command(project, command, write)
    await api.lookup(project, command, write)
    expect(fetch.mock.calls.map((c) => c[0])).toEqual([
      `/api/v1/projects/${project}/meetings/${meeting}/turns`,
      `/api/v1/projects/${project}/meetings/${meeting}/turns/lookup`,
    ])
    for (const [, o] of fetch.mock.calls) {
      expect(o.body).toBe(JSON.stringify(command.input))
      expect(new Headers(o.headers).get('Idempotency-Key')).toBe(write.key)
    }
    await expect(
      createMeetingsAPI(async () => json({ ...receipt, meeting_id: id(20) })).command(
        project,
        command,
        write,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('validates structured identity and keeps arbitrary text inert', () => {
    expect(() =>
      captureMeetingCommand({
        ...command,
        input: { ...command.input, expected_version: 9007199254740993 },
      }),
    ).toThrow(AccountFailure)
    expect(() =>
      captureMeetingCommand({
        kind: 'start',
        input: {
          agents: [id(6)],
          mode: 'parallel',
          content_nodes: [{ type: 'mention', agent_id: id(7) }],
        },
      }),
    ).toThrow(AccountFailure)
    expect(() =>
      captureMeetingCommand({
        ...command,
        input: { ...command.input, content_nodes: [{ type: 'text', text: '\ud800' }] },
      }),
    ).toThrow(AccountFailure)
    expect(projectRoute(`/owner/project/meetings/${meeting}`)?.suffix).toBe(`/meetings/${meeting}`)
    expect(projectRoute('/owner/project/meetings/other')).toBeNull()
  })
})
function setup() {
  let identity = { userID: id(10), sessionID: id(11), epoch: 1 },
    busy = false
  const api: MeetingsAPI = {
    list: vi.fn(),
    detail: vi.fn(),
    proposal: vi.fn(),
    contributionExecution: vi.fn(),
    command: vi.fn(async () => {
      throw new AccountFailure('transport')
    }),
    lookup: vi.fn(async () => ({ status: 'committed' as const, receipt })),
  }
  const session = createMeetingSession(api, {
    capture: () => ({ identity, csrf: 'S'.repeat(43), key: id(12) }),
    identity: () => identity,
    current: (v) => v === identity,
    busy: () => busy,
    cancelRead() {},
    async run(_id, _kind, work) {
      busy = true
      try {
        return await work(new AbortController().signal, () => true)
      } finally {
        busy = false
      }
    },
  })
  return {
    api,
    session,
    changeIdentity() {
      identity = { ...identity, epoch: 2 }
    },
  }
}
describe('Meeting original command ownership', () => {
  it('retains the captured reply target after Unknown even when the editable input changes', async () => {
    const f = setup()
    const input = {
      kind: 'send' as const,
      meeting,
      input: {
        expected_version: '1',
        mode: 'parallel' as const,
        content_nodes: [{ type: 'text' as const, text: 'Reply' }],
        interrupt: false,
        reply_to_message_id: id(20),
      },
    }
    await expect(f.session.start(project, input)).rejects.toMatchObject({ kind: 'transport' })
    input.input.reply_to_message_id = id(21)
    input.input.content_nodes[0]!.text = 'Changed'
    await expect(f.session.start(project, input)).rejects.toMatchObject({ kind: 'busy' })
    await f.session.lookup()
    const original = vi.mocked(f.api.command).mock.calls[0]!,
      lookup = vi.mocked(f.api.lookup).mock.calls[0]!
    expect(lookup.slice(0, 2)).toEqual(original.slice(0, 2))
    expect(lookup[1].input).toMatchObject({
      reply_to_message_id: id(20),
      content_nodes: [{ type: 'text', text: 'Reply' }],
    })
    expect(lookup[2].key).toBe(original[2].key)
    expect(f.api.command).toHaveBeenCalledTimes(1)
  })
  it.each(['retry-contribution', 'regenerate-contribution'] as const)(
    'keeps an uncertain %s bound to its original target and key',
    async (action) => {
      const f = setup()
      const input: MeetingCommand = {
        kind: 'control',
        meeting,
        input: {
          action,
          expected_version: '9007199254740993',
          turn_id: id(3),
          contribution_id: id(4),
        },
      }
      await expect(f.session.start(project, input)).rejects.toMatchObject({ kind: 'transport' })
      await expect(f.session.start(project, input)).rejects.toMatchObject({ kind: 'busy' })
      await f.session.lookup()
      const original = vi.mocked(f.api.command).mock.calls[0]!,
        lookup = vi.mocked(f.api.lookup).mock.calls[0]!
      expect(f.api.command).toHaveBeenCalledTimes(1)
      expect(lookup.slice(0, 2)).toEqual([project, input])
      expect(lookup[2].key).toBe(original[2].key)
      expect(f.session.progress?.phase).toBe('confirmed')
    },
  )
  it('releases an undispatched command when a synchronous observer takes the request owner', async () => {
    let busy = false
    const identity = { userID: id(10), sessionID: id(11), epoch: 1 }
    const api: MeetingsAPI = {
      list: vi.fn(),
      detail: vi.fn(),
      proposal: vi.fn(),
      contributionExecution: vi.fn(),
      command: vi.fn(),
      lookup: vi.fn(),
    }
    const owner = createMeetingSession(api, {
      capture: () => ({ identity, csrf: 'S'.repeat(43), key: id(12) }),
      identity: () => identity,
      current: (value) => value === identity,
      busy: () => busy,
      cancelRead() {},
      run: (_identity, _kind, work) => work(new AbortController().signal, () => true),
    })
    const stop = watch(
      () => owner.progress,
      (value) => {
        if (value?.phase === 'submitting') busy = true
      },
      { flush: 'sync' },
    )
    await expect(owner.start(project, command)).rejects.toMatchObject({ kind: 'busy' })
    expect(api.command).not.toHaveBeenCalled()
    expect(owner.progress?.phase).toBe('rejected')
    stop()
    busy = false
    vi.mocked(api.command).mockResolvedValue(receipt)
    await owner.start(project, command)
    expect(api.command).toHaveBeenCalledTimes(1)
  })

  it('retains Unknown for lookup without re-sending, including not-found', async () => {
    const f = setup()
    await expect(f.session.start(project, command)).rejects.toMatchObject({ kind: 'transport' })
    await expect(f.session.start(project, command)).rejects.toMatchObject({ kind: 'busy' })
    vi.mocked(f.api.lookup).mockResolvedValueOnce({ status: 'not_found' })
    await f.session.lookup()
    expect(f.session.progress?.phase).toBe('uncertain')
    await f.session.lookup()
    expect(f.session.progress?.phase).toBe('confirmed')
    expect(f.api.command).toHaveBeenCalledTimes(1)
    const original = vi.mocked(f.api.command).mock.calls[0]!,
      lookup = vi.mocked(f.api.lookup).mock.calls[0]!
    expect(lookup.slice(0, 2)).toEqual(original.slice(0, 2))
    expect(lookup[2].key).toBe(original[2].key)
  })
  it('cannot recover with another identity or publish after synchronous invalidation', async () => {
    const f = setup()
    await expect(f.session.start(project, command)).rejects.toThrow()
    f.changeIdentity()
    await expect(f.session.lookup()).rejects.toMatchObject({ kind: 'busy' })
    expect(f.api.lookup).not.toHaveBeenCalled()
    const g = setup()
    vi.mocked(g.api.command).mockResolvedValue(receipt)
    const stop = watch(
      () => g.session.progress,
      (p) => {
        if (p?.phase === 'confirmed') g.session.clear()
      },
      { flush: 'sync' },
    )
    await expect(g.session.start(project, command)).rejects.toMatchObject({ kind: 'cancelled' })
    stop()
  })
  it('uses the real Session Cookie owner and captured CSRF for original recovery', async () => {
    const calls: { path: string; body: unknown; key: string | null; csrf: string | null }[] = []
    const fetch: Fetch = async (path, options) => {
      if (path === '/api/v1/session')
        return json({
          user: {
            id: id(10),
            email: 'owner@example.test',
            username: 'owner',
            display_name: 'Owner',
            role: 'user',
            theme: 'system',
            version: '1',
            initial_password_suggestion: false,
          },
          session: {
            id: id(11),
            issued_at: instant,
            absolute_expires_at: instant,
            idle_expires_at: instant,
          },
          csrf_token: 'S'.repeat(43),
        })
      const headers = new Headers(options.headers)
      calls.push({
        path,
        body: JSON.parse(String(options.body)),
        key: headers.get('Idempotency-Key'),
        csrf: headers.get('X-CSRF-Token'),
      })
      if (!path.endsWith('/lookup')) throw new TypeError('response lost')
      return json({ status: 'committed', receipt })
    }
    const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
    args[19] = { meetings: createMeetingsAPI(fetch) }
    const auth = createSessionController(...args)
    try {
      await auth.restore()
      await expect(auth.meetings.start(project, command)).rejects.toMatchObject({
        kind: 'transport',
      })
      expect(auth.state.busy).toBe(false)
      await auth.meetings.lookup()
      expect(calls).toHaveLength(2)
      expect(calls[1]?.body).toEqual(calls[0]?.body)
      expect(calls[1]?.key).toBe(calls[0]?.key)
      expect(calls[1]?.csrf).toBe('S'.repeat(43))
      expect(auth.meetings.progress?.phase).toBe('confirmed')
    } finally {
      auth.leave()
    }
  })
})

describe('Meeting control and Decision commands', () => {
  it('keeps the original target, response version and key for each lookup', async () => {
    const fetch = vi.fn<Fetch>(async (url) =>
      json(url.endsWith('/lookup') ? { status: 'committed', receipt } : receipt),
    )
    const api = createMeetingsAPI(fetch),
      write = { csrfToken: 'S'.repeat(43), key: id(80) }
    const actions: MeetingCommand[] = [
      {
        kind: 'control',
        meeting,
        input: { action: 'cancel-turn', expected_version: '9', turn_id: id(3) },
      },
      {
        kind: 'decision',
        meeting,
        input: { decision_id: id(7), action: 'answer', expected_version: '1', answer: 'Use B' },
      },
      {
        kind: 'decision',
        meeting,
        input: { decision_id: id(7), action: 'skip', expected_version: '1' },
      },
    ]
    for (const action of actions) {
      fetch.mockClear()
      await api.command(project, action, write)
      await api.lookup(project, action, write)
      const suffix = action.kind === 'control' ? 'controls' : 'decisions'
      expect(fetch.mock.calls.map((c) => c[0])).toEqual([
        `/api/v1/projects/${project}/meetings/${meeting}/${suffix}`,
        `/api/v1/projects/${project}/meetings/${meeting}/${suffix}/lookup`,
      ])
      expect(fetch.mock.calls[0]![1].body).toBe(fetch.mock.calls[1]![1].body)
      expect(new Headers(fetch.mock.calls[1]![1].headers).get('Idempotency-Key')).toBe(write.key)
    }
  })
  it('rejects ambiguous answer/skip and control targets before any network request', () => {
    for (const input of [
      { decision_id: id(7), action: 'answer', expected_version: '1', answer: ' ' },
      { decision_id: id(7), action: 'skip', expected_version: '1', answer: 'hidden' },
      { decision_id: id(7), action: 'resume', expected_version: '1' },
      { decision_id: id(7), action: 'skip', expected_version: 1 },
    ])
      expect(() => captureMeetingCommand({ kind: 'decision', meeting, input })).toThrow(
        AccountFailure,
      )
    expect(() =>
      captureMeetingCommand({
        kind: 'control',
        meeting,
        input: { action: 'archive', expected_version: '2', turn_id: id(3) },
      }),
    ).toThrow(AccountFailure)
  })
  it('retains an uncertain Decision command in the original request owner', async () => {
    const f = setup()
    const action: MeetingCommand = {
      kind: 'decision',
      meeting,
      input: { decision_id: id(7), action: 'answer', expected_version: '1', answer: 'Use B' },
    }
    await expect(f.session.start(project, action)).rejects.toMatchObject({ kind: 'transport' })
    expect(f.session.progress).toMatchObject({ meeting, kind: 'decision', phase: 'uncertain' })
    await f.session.lookup()
    expect(f.api.command).toHaveBeenCalledTimes(1)
    expect(vi.mocked(f.api.lookup).mock.calls[0]?.[1]).toEqual(action)
  })
})

describe('Meeting deletion preserves its original accepted intent', () => {
  const deletion: MeetingCommand = {
    kind: 'delete',
    meeting,
    input: { expected_version: '9007199254740993' },
  }
  const accepted = {
    deletion_id: id(25),
    meeting_id: meeting,
    accepted_version: '9007199254740994',
    phase: 'cancelling' as const,
    requested_at: instant,
    replayed: false,
  }
  it('uses the dedicated HTTP paths and rejects completion claims or foreign accepted versions', async () => {
    const fetch = vi.fn<Fetch>(async (_url, init) =>
      json(
        String(_url).endsWith('/lookup') ? { status: 'committed', receipt: accepted } : accepted,
      ),
    )
    const api = createMeetingsAPI(fetch),
      write = { csrfToken: 'S'.repeat(43), key: 'original-delete' }
    await expect(api.command(project, deletion, write)).resolves.toEqual(accepted)
    await expect(api.lookup(project, deletion, write)).resolves.toEqual({
      status: 'committed',
      receipt: accepted,
    })
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `/api/v1/projects/${project}/meetings/${meeting}/deletion`,
      `/api/v1/projects/${project}/meetings/${meeting}/deletion/lookup`,
    ])
    expect(fetch.mock.calls[0]![1].body).toBe('{"expected_version":"9007199254740993"}')
    expect(fetch.mock.calls[1]![1].body).toBe(fetch.mock.calls[0]![1].body)
    for (const patch of [
      { meeting_id: id(80) },
      { accepted_version: '9007199254740993' },
      { phase: 'deleted' },
      { deleted_at: instant },
      { phase: 'deleted', deleted_at: instant, error_code: 'DEPENDENCY_UNAVAILABLE' },
    ]) {
      await expect(
        createMeetingsAPI(async () => json({ ...accepted, ...patch })).command(
          project,
          deletion,
          write,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    expect(() =>
      captureMeetingCommand({ ...deletion, input: { expected_version: '9223372036854775807' } }),
    ).toThrow(AccountFailure)
  })
  it('keeps a cancelling receipt recoverable using only the original lookup and same key', async () => {
    const f = setup()
    vi.mocked(f.api.command).mockResolvedValueOnce(accepted)
    vi.mocked(f.api.lookup).mockResolvedValueOnce({
      status: 'committed',
      receipt: { ...accepted, replayed: true },
    })
    await f.session.start(project, deletion)
    expect(f.session.progress).toMatchObject({
      kind: 'delete',
      phase: 'processing',
      contextValid: true,
    })
    await expect(f.session.start(project, deletion)).rejects.toMatchObject({ kind: 'busy' })
    await f.session.lookup()
    expect(f.session.progress?.phase).toBe('processing')
    vi.mocked(f.api.lookup).mockResolvedValueOnce({
      status: 'committed',
      receipt: { ...accepted, deletion_id: id(26) },
    })
    await expect(f.session.lookup()).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(f.session.progress?.phase).toBe('uncertain')
    vi.mocked(f.api.lookup).mockResolvedValueOnce({
      status: 'committed',
      receipt: { ...accepted, phase: 'deleted', deleted_at: instant, replayed: true },
    })
    await f.session.lookup()
    expect(f.session.progress?.phase).toBe('confirmed')
    expect(f.api.command).toHaveBeenCalledTimes(1)
    const original = vi.mocked(f.api.command).mock.calls[0]!
    for (const call of vi.mocked(f.api.lookup).mock.calls) {
      expect(call.slice(0, 2)).toEqual(original.slice(0, 2))
      expect(call[2].key).toBe(original[2].key)
    }
  })
})
