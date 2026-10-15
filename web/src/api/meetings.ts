import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'
import { workID, workVersion } from './work-review'

export type MeetingMode = 'parallel' | 'sequential'
export type MeetingReference = Readonly<{
  kind: 'task' | 'knowledge' | 'link' | 'file'
  id: string
  label?: string
}>
export type MeetingNode = Readonly<
  | { type: 'text'; text: string }
  | { type: 'mention'; participant_id?: string; agent_id?: string }
  | { type: MeetingReference['kind']; reference: MeetingReference }
>
export type MeetingSummary = Readonly<{
  version: string
  goals: string
  decisions: string
  unresolved: string
  facts: string
  pending_update: boolean
  input_digest?: string
}>
export type MeetingSession = Readonly<{
  id: string
  project_id: string
  status: 'proposed' | 'active' | 'archive'
  version: string
  title: string | null
  proposal_content?: string
  summary: MeetingSummary | null
  deleting: boolean
  created_at: string
  updated_at: string
}>
export type MeetingParticipant = Readonly<{
  id: string
  meeting_id: string
  kind: 'user' | 'agent'
  source_id: string
  name: string
  description?: string
  order: number
  active: boolean
}>
export type MeetingMessage = Readonly<{
  id: string
  participant_id: string
  content_nodes: readonly MeetingNode[]
  reply_to_message_id?: string
  created_at: string
}>
export type MeetingContributionHistory = Readonly<{
  execution_id: string
  generation: string
  attempt: string
  status: 'created' | 'preparing' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'cancelled'
  retry_of_execution_id: string | null
  regenerate_of_execution_id: string | null
  message: MeetingMessage | null
}>
export type MeetingContribution = Readonly<{
  id: string
  turn_id: string
  participant_id: string
  kind: 'user' | 'agent'
  order: number
  status:
    'pending' | 'waiting_for_agent' | 'running' | 'completed' | 'failed' | 'cancelled' | 'skipped'
  generation: string
  attempt: string
  execution_id: string | null
  waiting_on_execution_id: string | null
  cancel_requested: boolean
  message: MeetingMessage | null
  history: readonly MeetingContributionHistory[]
  started_at: string | null
  completed_at: string | null
}>
export type MeetingTurn = Readonly<{
  id: string
  mode: MeetingMode
  status: 'queued' | 'running' | 'finalizing' | 'completed' | 'cancelled'
  result?: string
  version: string
  created_at: string
  contributions: readonly MeetingContribution[]
  summary_error?: string
}>
export type MeetingContributionExecution = Readonly<{
  id: string
  project_id: string
  meeting_id: string
  turn_id: string
  contribution_id: string
  participant_id: string
  agent_id: string
  generation: string
  attempt: string
  status: 'created' | 'preparing' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'cancelled'
  version: string
  waiting_reason: 'approval' | 'decision' | null
  waiting_reference: string | null
  cancel_requested_at: string | null
  created_at: string
  started_at: string | null
  completed_at: string | null
}>
export type MeetingDecision = Readonly<{
  id: string
  contribution_id: string
  execution_id: string
  question: string
  options: readonly string[]
  status: 'pending' | 'answered' | 'skipped' | 'cancelled'
  answer: string | null
  version: string
}>
export type MeetingDetail = Readonly<{
  session: MeetingSession
  participants: readonly MeetingParticipant[]
  references: readonly MeetingReference[]
  turns: readonly MeetingTurn[]
  decisions: readonly MeetingDecision[]
}>
export type MeetingStart = Readonly<{
  agents: readonly string[]
  mode: MeetingMode
  content_nodes: readonly MeetingNode[]
}>
export type MeetingSend = Readonly<{
  expected_version: string
  mode: MeetingMode
  content_nodes: readonly MeetingNode[]
  interrupt: boolean
  reply_to_message_id?: string
}>
export type MeetingControl = Readonly<{
  action:
    | 'archive'
    | 'unarchive'
    | 'cancel-turn'
    | 'cancel-contribution'
    | 'skip-contribution'
    | 'retry-contribution'
    | 'regenerate-contribution'
    | 'retry-summary'
  expected_version: string
  turn_id?: string
  contribution_id?: string
}>
export type MeetingDecisionResponse = Readonly<{
  decision_id: string
  action: 'answer' | 'skip'
  expected_version: string
  answer?: string
}>
export type MeetingParticipantsUpdate = Readonly<{
  expected_version: string
  agents: readonly string[]
}>
export type MeetingReferencesUpdate = Readonly<{
  expected_version: string
  references: readonly MeetingReference[]
}>
export type MeetingProposal = Readonly<{
  session: MeetingSession
  source_execution_id: string
  created_by_participant_id: string
  origin?: Readonly<{ kind: 'task' | 'meeting'; id: string }>
}>
export type MeetingProposalReview = Readonly<{
  expected_version: string
  approve: boolean
}>
export type MeetingCommand = Readonly<
  | { kind: 'start'; input: MeetingStart }
  | { kind: 'send'; meeting: string; input: MeetingSend }
  | { kind: 'control'; meeting: string; input: MeetingControl }
  | { kind: 'decision'; meeting: string; input: MeetingDecisionResponse }
  | { kind: 'participants'; meeting: string; input: MeetingParticipantsUpdate }
  | { kind: 'references'; meeting: string; input: MeetingReferencesUpdate }
  | { kind: 'proposal-review'; meeting: string; input: MeetingProposalReview }
  | { kind: 'delete'; meeting: string; input: Readonly<{ expected_version: string }> }
>
export type MeetingReceipt = Readonly<{
  meeting_id: string
  turn_id?: string
  version: string
  replayed: boolean
}>
export type MeetingDeletionReceipt = Readonly<{
  deletion_id: string
  meeting_id: string
  accepted_version: string
  phase: 'cancelling' | 'deleted'
  requested_at: string
  deleted_at?: string
  error_code?: string
  replayed: boolean
}>
export type MeetingCommandResult = MeetingReceipt | MeetingDeletionReceipt
export type MeetingLookup = Readonly<
  { status: 'committed'; receipt: MeetingCommandResult } | { status: 'not_found' }
>
export interface MeetingsAPI {
  list(project: string, signal: AbortSignal): Promise<readonly MeetingSession[]>
  detail(project: string, meeting: string, signal: AbortSignal): Promise<MeetingDetail>
  proposal(project: string, meeting: string, signal: AbortSignal): Promise<MeetingProposal>
  contributionExecution(
    project: string,
    meeting: string,
    contribution: string,
    signal: AbortSignal,
  ): Promise<MeetingContributionExecution>
  command(
    project: string,
    command: MeetingCommand,
    write: WriteOptions,
  ): Promise<MeetingCommandResult>
  lookup(project: string, command: MeetingCommand, write: WriteOptions): Promise<MeetingLookup>
}
function check(v: unknown): asserts v {
  if (!v) throw new AccountFailure('invalid-response')
}
function text(v: unknown, max = 131072) {
  const s = string(v, 0, max)
  check(
    !s.includes('\0') &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(s) &&
      new TextEncoder().encode(s).byteLength <= max,
  )
  return s
}
function choice<const T extends readonly string[]>(v: unknown, choices: T): T[number] {
  check(typeof v === 'string' && choices.includes(v))
  return v as T[number]
}
function bool(v: unknown) {
  check(typeof v === 'boolean')
  return v
}
function integer(v: unknown, max = 1000000) {
  check(typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 && v <= max)
  return v
}
function array<T>(v: unknown, max: number, parse: (v: unknown) => T): readonly T[] {
  check(Array.isArray(v) && v.length <= max)
  return Object.freeze(v.map(parse))
}
function unique<T extends { id: string }>(v: readonly T[]) {
  check(new Set(v.map((x) => x.id)).size === v.length)
  return v
}
function nullable<T>(v: unknown, parse: (v: unknown) => T): T | null {
  return v === null ? null : parse(v)
}
function instant(v: unknown) {
  const s = string(v, 20, 35)
  check(
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(s) &&
      Number.isFinite(Date.parse(s)),
  )
  parseSystemInstant(s.slice(0, 19) + '.000000Z')
  const offset = /([+-])(\d{2}):(\d{2})$/.exec(s)
  check(!offset || (Number(offset[2]) <= 23 && Number(offset[3]) <= 59))
  return s
}
const mode = (v: unknown) => choice(v, ['parallel', 'sequential'] as const)
function reference(value: unknown): MeetingReference {
  const v = shape(value, ['kind', 'id'], ['label'])
  return Object.freeze({
    kind: choice(v.kind, ['task', 'knowledge', 'link', 'file'] as const),
    id: string(v.id, 1, 8192),
    ...(v.label === undefined ? {} : { label: text(v.label, 1024) }),
  })
}
function node(value: unknown, draft = false): MeetingNode {
  const v = shape(value, ['type'], ['text', 'participant_id', 'agent_id', 'reference'])
  if (v.type === 'text') {
    shape(v, ['type'], ['text'])
    return Object.freeze({ type: 'text', text: text(v.text ?? '') })
  }
  if (v.type === 'mention') {
    check(draft || v.agent_id === undefined)
    check((v.participant_id === undefined) !== (v.agent_id === undefined))
    shape(v, ['type', v.participant_id === undefined ? 'agent_id' : 'participant_id'])
    return Object.freeze({
      type: 'mention',
      ...(v.participant_id === undefined
        ? { agent_id: workID(v.agent_id) }
        : { participant_id: workID(v.participant_id) }),
    })
  }
  shape(v, ['type', 'reference'])
  const ref = reference(v.reference)
  check(ref.kind === v.type)
  return Object.freeze({ type: ref.kind, reference: ref })
}
function content(v: unknown, draft: boolean) {
  const nodes = array(v, 1024, (x) => node(x, draft))
  check(nodes.length > 0 && nodes.some((n) => n.type !== 'text' || n.text.trim() !== ''))
  check(
    nodes.reduce(
      (n, x) => n + (x.type === 'text' ? new TextEncoder().encode(x.text).byteLength : 0),
      0,
    ) <= 131072,
  )
  return nodes
}
export function captureMeetingCommand(value: unknown): MeetingCommand {
  try {
    const v = shape(value, ['kind', 'input'], ['meeting'])
    if (v.kind === 'start') {
      shape(v, ['kind', 'input'])
      const x = shape(v.input, ['agents', 'mode', 'content_nodes']),
        agents = array(x.agents, 32, workID)
      check(agents.length > 0 && new Set(agents).size === agents.length)
      const nodes = content(x.content_nodes, true)
      check(
        nodes.every((n) => n.type !== 'mention' || (!!n.agent_id && agents.includes(n.agent_id))),
      )
      return Object.freeze({
        kind: 'start',
        input: Object.freeze({ agents, mode: mode(x.mode), content_nodes: nodes }),
      })
    }
    if (v.kind === 'control') {
      const x = shape(v.input, ['action', 'expected_version'], ['turn_id', 'contribution_id'])
      const action = choice(x.action, [
        'archive',
        'unarchive',
        'cancel-turn',
        'cancel-contribution',
        'skip-contribution',
        'retry-contribution',
        'regenerate-contribution',
        'retry-summary',
      ] as const)
      const contribution = [
        'cancel-contribution',
        'skip-contribution',
        'retry-contribution',
        'regenerate-contribution',
      ].includes(action)
      const turn = contribution || ['cancel-turn', 'retry-summary'].includes(action)
      check(
        turn === Object.hasOwn(x, 'turn_id') &&
          contribution === Object.hasOwn(x, 'contribution_id'),
      )
      return Object.freeze({
        kind: 'control',
        meeting: workID(v.meeting),
        input: Object.freeze({
          action,
          expected_version: workVersion(x.expected_version),
          ...(turn ? { turn_id: workID(x.turn_id) } : {}),
          ...(contribution ? { contribution_id: workID(x.contribution_id) } : {}),
        }),
      })
    }
    if (v.kind === 'decision') {
      const x = shape(v.input, ['decision_id', 'action', 'expected_version'], ['answer'])
      const action = choice(x.action, ['answer', 'skip'] as const)
      check((action === 'answer') === Object.hasOwn(x, 'answer'))
      const answer = action === 'answer' ? text(x.answer, 32768) : undefined
      check(answer === undefined || answer.trim() !== '')
      return Object.freeze({
        kind: 'decision',
        meeting: workID(v.meeting),
        input: Object.freeze({
          decision_id: workID(x.decision_id),
          action,
          expected_version: workVersion(x.expected_version),
          ...(answer === undefined ? {} : { answer }),
        }),
      })
    }
    if (v.kind === 'participants') {
      const x = shape(v.input, ['expected_version', 'agents'])
      const agents = array(x.agents, 32, workID)
      check(new Set(agents).size === agents.length)
      return Object.freeze({
        kind: 'participants',
        meeting: workID(v.meeting),
        input: Object.freeze({ expected_version: workVersion(x.expected_version), agents }),
      })
    }
    if (v.kind === 'references') {
      const x = shape(v.input, ['expected_version', 'references'])
      const references = array(x.references, 256, (value) => {
        const ref = reference(value)
        if (ref.kind === 'link') {
          check(ref.id.trim() === ref.id && !/[\u0000-\u0020]/.test(ref.id))
          const url = new URL(ref.id)
          check(
            ['http:', 'https:'].includes(url.protocol) &&
              !!url.hostname &&
              !url.username &&
              !url.password &&
              !/^https?:\/\/[^/?#]*@/i.test(ref.id),
          )
        } else workID(ref.id)
        return ref
      })
      check(new Set(references.map((ref) => `${ref.kind}:${ref.id}`)).size === references.length)
      return Object.freeze({
        kind: 'references',
        meeting: workID(v.meeting),
        input: Object.freeze({ expected_version: workVersion(x.expected_version), references }),
      })
    }
    if (v.kind === 'delete') {
      const x = shape(v.input, ['expected_version'])
      const expected = workVersion(x.expected_version)
      check(BigInt(expected) < 9223372036854775807n)
      return Object.freeze({
        kind: 'delete',
        meeting: workID(v.meeting),
        input: Object.freeze({ expected_version: expected }),
      })
    }
    if (v.kind === 'proposal-review') {
      const x = shape(v.input, ['expected_version', 'approve'])
      return Object.freeze({
        kind: 'proposal-review',
        meeting: workID(v.meeting),
        input: Object.freeze({
          expected_version: workVersion(x.expected_version),
          approve: bool(x.approve),
        }),
      })
    }
    check(v.kind === 'send')
    const x = shape(
      v.input,
      ['expected_version', 'mode', 'content_nodes', 'interrupt'],
      ['reply_to_message_id'],
    )
    return Object.freeze({
      kind: 'send',
      meeting: workID(v.meeting),
      input: Object.freeze({
        expected_version: workVersion(x.expected_version),
        mode: mode(x.mode),
        content_nodes: content(x.content_nodes, false),
        interrupt: bool(x.interrupt),
        ...(Object.hasOwn(x, 'reply_to_message_id')
          ? { reply_to_message_id: workID(x.reply_to_message_id) }
          : {}),
      }),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function summary(value: unknown): MeetingSummary {
  const v = shape(
    value,
    ['version', 'goals', 'decisions', 'unresolved', 'facts', 'pending_update'],
    ['input_digest'],
  )
  return Object.freeze({
    version: workVersion(v.version),
    goals: text(v.goals),
    decisions: text(v.decisions),
    unresolved: text(v.unresolved),
    facts: text(v.facts),
    pending_update: bool(v.pending_update),
    ...(v.input_digest === undefined ? {} : { input_digest: string(v.input_digest, 1, 128) }),
  })
}
function session(value: unknown, project: string, id?: string): MeetingSession {
  const v = shape(
    value,
    [
      'id',
      'project_id',
      'status',
      'version',
      'title',
      'summary',
      'deleting',
      'created_at',
      'updated_at',
    ],
    ['proposal_content'],
  )
  check(workID(v.project_id) === project && (!id || workID(v.id) === id))
  return Object.freeze({
    id: workID(v.id),
    project_id: project,
    status: choice(v.status, ['proposed', 'active', 'archive'] as const),
    version: workVersion(v.version),
    title: nullable(v.title, (x) => text(x, 4096)),
    summary: nullable(v.summary, summary),
    deleting: bool(v.deleting),
    created_at: instant(v.created_at),
    updated_at: instant(v.updated_at),
    ...(v.proposal_content === undefined ? {} : { proposal_content: text(v.proposal_content) }),
  })
}
function participant(value: unknown, meeting: string): MeetingParticipant {
  const v = shape(
    value,
    ['id', 'meeting_id', 'kind', 'source_id', 'name', 'order', 'active'],
    ['description'],
  )
  check(workID(v.meeting_id) === meeting)
  return Object.freeze({
    id: workID(v.id),
    meeting_id: meeting,
    kind: choice(v.kind, ['user', 'agent'] as const),
    source_id: workID(v.source_id),
    name: text(v.name, 1024),
    order: integer(v.order),
    active: bool(v.active),
    ...(v.description === undefined ? {} : { description: text(v.description, 8192) }),
  })
}
function message(value: unknown): MeetingMessage {
  const v = shape(
    value,
    ['id', 'participant_id', 'content_nodes', 'created_at'],
    ['reply_to_message_id'],
  )
  return Object.freeze({
    id: workID(v.id),
    participant_id: workID(v.participant_id),
    content_nodes: array(v.content_nodes, 1024, (x) => node(x)),
    created_at: instant(v.created_at),
    ...(v.reply_to_message_id === undefined
      ? {}
      : { reply_to_message_id: workID(v.reply_to_message_id) }),
  })
}
function contribution(value: unknown, turn: string): MeetingContribution {
  const v = shape(value, [
    'id',
    'turn_id',
    'participant_id',
    'kind',
    'order',
    'status',
    'generation',
    'attempt',
    'execution_id',
    'waiting_on_execution_id',
    'cancel_requested',
    'message',
    'history',
    'started_at',
    'completed_at',
  ])
  check(workID(v.turn_id) === turn)
  const result = Object.freeze({
    id: workID(v.id),
    turn_id: turn,
    participant_id: workID(v.participant_id),
    kind: choice(v.kind, ['user', 'agent'] as const),
    order: integer(v.order),
    status: choice(v.status, [
      'pending',
      'waiting_for_agent',
      'running',
      'completed',
      'failed',
      'cancelled',
      'skipped',
    ] as const),
    generation: workVersion(v.generation),
    attempt: workVersion(v.attempt),
    execution_id: nullable(v.execution_id, workID),
    waiting_on_execution_id: nullable(v.waiting_on_execution_id, workID),
    cancel_requested: bool(v.cancel_requested),
    message: nullable(v.message, message),
    history: array(v.history, 1000, contributionHistory),
    started_at: nullable(v.started_at, instant),
    completed_at: nullable(v.completed_at, instant),
  })
  check(!result.message || result.message.participant_id === result.participant_id)
  check(!result.cancel_requested || (result.kind === 'agent' && result.execution_id !== null))
  check(result.kind === 'agent' || result.history.length === 0)
  check(new Set(result.history.map((h) => h.execution_id)).size === result.history.length)
  check(
    new Set(result.history.map((h) => `${h.generation}:${h.attempt}`)).size ===
      result.history.length,
  )
  for (const h of result.history) {
    check(!h.message || h.message.participant_id === result.participant_id)
    check(
      BigInt(h.generation) < BigInt(result.generation) ||
        (h.generation === result.generation && BigInt(h.attempt) <= BigInt(result.attempt)),
    )
    if (h.execution_id === result.execution_id)
      check(h.generation === result.generation && h.attempt === result.attempt)
  }
  return result
}
function contributionHistory(value: unknown): MeetingContributionHistory {
  const v = shape(value, [
    'execution_id',
    'generation',
    'attempt',
    'status',
    'retry_of_execution_id',
    'regenerate_of_execution_id',
    'message',
  ])
  const result = Object.freeze({
    execution_id: workID(v.execution_id),
    generation: workVersion(v.generation),
    attempt: workVersion(v.attempt),
    status: choice(v.status, [
      'created',
      'preparing',
      'running',
      'waiting',
      'succeeded',
      'failed',
      'cancelled',
    ] as const),
    retry_of_execution_id: nullable(v.retry_of_execution_id, workID),
    regenerate_of_execution_id: nullable(v.regenerate_of_execution_id, workID),
    message: nullable(v.message, message),
  })
  check(!(result.retry_of_execution_id && result.regenerate_of_execution_id))
  check(
    result.retry_of_execution_id !== result.execution_id &&
      result.regenerate_of_execution_id !== result.execution_id,
  )
  check(result.message === null || result.status === 'succeeded')
  return result
}
function turn(value: unknown): MeetingTurn {
  const v = shape(
      value,
      ['id', 'mode', 'status', 'version', 'created_at', 'contributions'],
      ['result', 'summary_error'],
    ),
    id = workID(v.id)
  return Object.freeze({
    id,
    mode: mode(v.mode),
    status: choice(v.status, [
      'queued',
      'running',
      'finalizing',
      'completed',
      'cancelled',
    ] as const),
    version: workVersion(v.version),
    created_at: instant(v.created_at),
    contributions: unique(array(v.contributions, 33, (x) => contribution(x, id))),
    ...(v.result === undefined ? {} : { result: text(v.result, 256) }),
    ...(v.summary_error === undefined ? {} : { summary_error: text(v.summary_error, 4096) }),
  })
}
function decision(value: unknown): MeetingDecision {
  const v = shape(value, [
    'id',
    'contribution_id',
    'execution_id',
    'question',
    'options',
    'status',
    'answer',
    'version',
  ])
  return Object.freeze({
    id: workID(v.id),
    contribution_id: workID(v.contribution_id),
    execution_id: workID(v.execution_id),
    question: text(v.question),
    options: array(v.options, 128, (x) => text(x, 8192)),
    status: choice(v.status, ['pending', 'answered', 'skipped', 'cancelled'] as const),
    answer: nullable(v.answer, text),
    version: workVersion(v.version),
  })
}
export function parseMeetingDetail(
  value: unknown,
  project: string,
  meeting: string,
): MeetingDetail {
  const v = shape(value, ['session', 'participants', 'references', 'turns', 'decisions']),
    participants = unique(array(v.participants, 10000, (x) => participant(x, meeting))),
    turns = unique(array(v.turns, 1000, turn)),
    ids = new Set(participants.map((x) => x.id))
  check(turns.every((t) => t.contributions.every((c) => ids.has(c.participant_id))))
  return Object.freeze({
    session: session(v.session, project, meeting),
    participants,
    references: array(v.references, 10000, reference),
    turns,
    decisions: unique(array(v.decisions, 10000, decision)),
  })
}
export function parseMeetingContributionExecution(
  value: unknown,
  project: string,
  meeting: string,
  contribution: string,
): MeetingContributionExecution {
  const v = shape(value, [
    'id',
    'project_id',
    'meeting_id',
    'turn_id',
    'contribution_id',
    'participant_id',
    'agent_id',
    'generation',
    'attempt',
    'status',
    'version',
    'waiting_reason',
    'waiting_reference',
    'cancel_requested_at',
    'created_at',
    'started_at',
    'completed_at',
  ])
  const result = Object.freeze({
    id: workID(v.id),
    project_id: workID(v.project_id),
    meeting_id: workID(v.meeting_id),
    turn_id: workID(v.turn_id),
    contribution_id: workID(v.contribution_id),
    participant_id: workID(v.participant_id),
    agent_id: workID(v.agent_id),
    generation: workVersion(v.generation),
    attempt: workVersion(v.attempt),
    version: workVersion(v.version),
    status: choice(v.status, [
      'created',
      'preparing',
      'running',
      'waiting',
      'succeeded',
      'failed',
      'cancelled',
    ] as const),
    waiting_reason: nullable(v.waiting_reason, (x) => choice(x, ['approval', 'decision'] as const)),
    waiting_reference: nullable(v.waiting_reference, workID),
    cancel_requested_at: nullable(v.cancel_requested_at, instant),
    created_at: instant(v.created_at),
    started_at: nullable(v.started_at, instant),
    completed_at: nullable(v.completed_at, instant),
  })
  check(
    result.project_id === project &&
      result.meeting_id === meeting &&
      result.contribution_id === contribution,
  )
  const terminal = ['succeeded', 'failed', 'cancelled'].includes(result.status)
  check(terminal === (result.completed_at !== null))
  check(!['running', 'waiting', 'succeeded'].includes(result.status) || result.started_at !== null)
  check(
    result.status === 'waiting'
      ? result.waiting_reason !== null && result.waiting_reference !== null
      : result.waiting_reason === null && result.waiting_reference === null,
  )
  for (const time of [result.started_at, result.completed_at, result.cancel_requested_at])
    check(time === null || Date.parse(time) >= Date.parse(result.created_at))
  check(
    result.started_at === null ||
      result.completed_at === null ||
      Date.parse(result.completed_at) >= Date.parse(result.started_at),
  )
  return result
}
function receipt(value: unknown, command: MeetingCommand): MeetingCommandResult {
  if (command.kind === 'delete') {
    const v = shape(
      value,
      ['deletion_id', 'meeting_id', 'accepted_version', 'phase', 'requested_at'],
      ['deleted_at', 'error_code', 'replayed'],
    )
    const result = Object.freeze({
      deletion_id: workID(v.deletion_id),
      meeting_id: workID(v.meeting_id),
      accepted_version: workVersion(v.accepted_version),
      phase: choice(v.phase, ['cancelling', 'deleted'] as const),
      requested_at: instant(v.requested_at),
      replayed: v.replayed === undefined ? false : bool(v.replayed),
      ...(v.deleted_at === undefined ? {} : { deleted_at: instant(v.deleted_at) }),
      ...(v.error_code === undefined ? {} : { error_code: string(v.error_code, 1, 128) }),
    })
    check(
      result.meeting_id === command.meeting &&
        BigInt(result.accepted_version) === BigInt(command.input.expected_version) + 1n,
    )
    check(result.error_code === undefined || /^[A-Z][A-Z0-9_]*$/.test(result.error_code))
    check(
      result.phase === 'cancelling'
        ? result.deleted_at === undefined
        : result.deleted_at !== undefined &&
            Date.parse(result.deleted_at) >= Date.parse(result.requested_at) &&
            result.error_code === undefined,
    )
    return result
  }
  const v = shape(value, ['meeting_id', 'version', 'replayed'], ['turn_id'])
  check(command.kind === 'start' || workID(v.meeting_id) === command.meeting)
  check(command.kind !== 'proposal-review' || !Object.hasOwn(v, 'turn_id'))
  return Object.freeze({
    meeting_id: workID(v.meeting_id),
    version: workVersion(v.version),
    replayed: bool(v.replayed),
    ...(v.turn_id === undefined ? {} : { turn_id: workID(v.turn_id) }),
  })
}
export function createMeetingsAPI(fetcher?: Fetch): MeetingsAPI {
  const call = accountTransport(fetcher)
  return {
    list: (project, signal) =>
      call(
        'meetingList',
        (v) => unique(array(shape(v, ['meetings']).meetings, 200, (x) => session(x, project))),
        { projectID: workID(project), signal },
      ),
    detail: (project, meeting, signal) =>
      call('meetingDetail', (v) => parseMeetingDetail(v, project, meeting), {
        projectID: workID(project),
        target: workID(meeting),
        signal,
      }),
    proposal: (project, meeting, signal) =>
      call(
        'meetingProposal',
        (value) => {
          const v = shape(
            value,
            ['session', 'source_execution_id', 'created_by_participant_id'],
            ['origin'],
          )
          const proposal = session(v.session, project, meeting)
          check(proposal.proposal_content !== undefined && proposal.proposal_content.trim() !== '')
          const origin = v.origin === undefined ? undefined : shape(v.origin, ['kind', 'id'])
          return Object.freeze({
            session: proposal,
            source_execution_id: workID(v.source_execution_id),
            created_by_participant_id: workID(v.created_by_participant_id),
            ...(origin === undefined
              ? {}
              : {
                  origin: Object.freeze({
                    kind: choice(origin.kind, ['task', 'meeting'] as const),
                    id: workID(origin.id),
                  }),
                }),
          })
        },
        { projectID: workID(project), target: workID(meeting), signal },
      ),
    contributionExecution: (project, meeting, contribution, signal) =>
      call(
        'getMeetingContributionExecution',
        (v) =>
          parseMeetingContributionExecution(
            shape(v, ['execution']).execution,
            project,
            meeting,
            contribution,
          ),
        {
          projectID: workID(project),
          target: workID(meeting),
          contributionID: workID(contribution),
          signal,
        },
      ),
    command(project, value, write) {
      const command = captureMeetingCommand(value)
      return call(
        (
          {
            start: 'meetingStart',
            send: 'meetingSend',
            control: 'meetingControl',
            decision: 'meetingDecision',
            participants: 'meetingParticipants',
            references: 'meetingReferences',
            'proposal-review': 'meetingProposalReview',
            delete: 'meetingDelete',
          } as const
        )[command.kind],
        (v) => receipt(v, command),
        {
          projectID: workID(project),
          ...(command.kind !== 'start' ? { target: command.meeting } : {}),
          body: command.input,
          csrf: write.csrfToken,
          key: write.key,
          signal: write.signal ?? new AbortController().signal,
        },
      )
    },
    lookup(project, value, write) {
      const command = captureMeetingCommand(value)
      return call(
        (
          {
            start: 'meetingStartLookup',
            send: 'meetingSendLookup',
            control: 'meetingControlLookup',
            decision: 'meetingDecisionLookup',
            participants: 'meetingParticipantsLookup',
            references: 'meetingReferencesLookup',
            'proposal-review': 'meetingProposalReviewLookup',
            delete: 'meetingDeleteLookup',
          } as const
        )[command.kind],
        (value) => {
          const v = shape(value, ['status'], ['receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed' as const,
              receipt: receipt(v.receipt, command),
            })
          check(v.status === 'not_found' && !Object.hasOwn(v, 'receipt'))
          return Object.freeze({ status: 'not_found' as const })
        },
        {
          projectID: workID(project),
          ...(command.kind !== 'start' ? { target: command.meeting } : {}),
          body: command.input,
          csrf: write.csrfToken,
          key: write.key,
          signal: write.signal ?? new AbortController().signal,
        },
      )
    },
  }
}
