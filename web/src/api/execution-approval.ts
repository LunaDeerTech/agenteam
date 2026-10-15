import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'
import { workID, workVersion } from './work-review'

export type ExecutionApprovalScope = Readonly<{
  projectID: string
  executionID: string
  agentID: string
  approvalID: string
}>
export type ExecutionApproval = Readonly<{
  id: string
  project_id: string
  execution_id: string
  agent_id: string
  tool_id: string
  tool_name: string
  tool_call_id: string
  operation_id: string
  operation_fingerprint: string
  status: 'pending' | 'approved' | 'rejected'
  version: '1' | '2'
  created_at: string
  resolved_at: string | null
  resolved_by: string | null
}>
export type ExecutionApprovalDecision = Readonly<{
  decision: 'approve' | 'reject'
  expected_version: string
}>
export type ExecutionApprovalReceipt = Readonly<{ command_id: string; approval: ExecutionApproval }>
export type ExecutionApprovalLookup =
  | Readonly<{ status: 'committed'; receipt: ExecutionApprovalReceipt }>
  | Readonly<{ status: 'not_observed'; receipt: null }>
export interface ExecutionApprovalAPI {
  get(scope: ExecutionApprovalScope, signal: AbortSignal): Promise<ExecutionApproval>
  decide(
    before: ExecutionApproval,
    input: ExecutionApprovalDecision,
    userID: string,
    write: WriteOptions,
  ): Promise<ExecutionApprovalReceipt>
  lookup(
    before: ExecutionApproval,
    input: ExecutionApprovalDecision,
    userID: string,
    write: WriteOptions,
  ): Promise<ExecutionApprovalLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(read: () => T): T {
  try {
    return read()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function captureScope(value: unknown): ExecutionApprovalScope {
  return input(() => {
    const v = shape(value, ['projectID', 'executionID', 'agentID', 'approvalID'])
    return Object.freeze({
      projectID: workID(v.projectID),
      executionID: workID(v.executionID),
      agentID: workID(v.agentID),
      approvalID: workID(v.approvalID),
    })
  })
}
export function captureExecutionApprovalDecision(value: unknown): ExecutionApprovalDecision {
  return input(() => {
    const v = shape(value, ['decision', 'expected_version'])
    requireValue(v.decision === 'approve' || v.decision === 'reject')
    return Object.freeze({
      decision: v.decision,
      expected_version: workVersion(v.expected_version),
    })
  })
}
// Matches tool/contract.ValidMCPKey: immutable Config UUID plus bounded remote
// name, distinct from Runner's run-command even when the remote name matches.
function validApprovalToolName(value: string): boolean {
  if (value === 'run-command') return true
  const parts = value.split(':')
  return (
    parts.length === 3 &&
    parts[0] === 'mcp' &&
    uuid7.test(parts[1]!) &&
    /^[A-Za-z0-9_.-]{1,128}$/.test(parts[2]!)
  )
}
export function parseExecutionApproval(
  value: unknown,
  scope: ExecutionApprovalScope,
): ExecutionApproval {
  const v = shape(value, [
    'id',
    'project_id',
    'execution_id',
    'agent_id',
    'tool_id',
    'tool_name',
    'tool_call_id',
    'operation_id',
    'operation_fingerprint',
    'status',
    'version',
    'created_at',
    'resolved_at',
    'resolved_by',
  ])
  const result = {
    id: workID(v.id),
    project_id: workID(v.project_id),
    execution_id: workID(v.execution_id),
    agent_id: workID(v.agent_id),
    tool_id: workID(v.tool_id),
    tool_name: string(v.tool_name, 1, 169),
    tool_call_id: string(v.tool_call_id, 1, 256),
    operation_id: workID(v.operation_id),
    operation_fingerprint: string(v.operation_fingerprint, 71, 71),
    status: string(v.status, 1, 16),
    version: workVersion(v.version),
    created_at: parseSystemInstant(v.created_at),
    resolved_at: v.resolved_at === null ? null : parseSystemInstant(v.resolved_at),
    resolved_by: v.resolved_by === null ? null : workID(v.resolved_by),
  }
  requireValue(
    result.id === scope.approvalID &&
      result.project_id === scope.projectID &&
      result.execution_id === scope.executionID &&
      result.agent_id === scope.agentID &&
      validApprovalToolName(result.tool_name) &&
      /^sha256:[0-9a-f]{64}$/.test(result.operation_fingerprint) &&
      (result.status === 'pending'
        ? result.version === '1' && result.resolved_at === null && result.resolved_by === null
        : ['approved', 'rejected'].includes(result.status) &&
          result.version === '2' &&
          result.resolved_at !== null &&
          result.resolved_by !== null &&
          result.resolved_at >= result.created_at),
  )
  return Object.freeze(result) as ExecutionApproval
}
export function capturePendingExecutionApproval(value: ExecutionApproval): ExecutionApproval {
  return input(() => {
    const parsed = parseExecutionApproval(
      value,
      captureScope({
        projectID: value.project_id,
        executionID: value.execution_id,
        agentID: value.agent_id,
        approvalID: value.id,
      }),
    )
    requireValue(parsed.status === 'pending')
    return parsed
  })
}
function parseReceipt(
  value: unknown,
  before: ExecutionApproval,
  original: ExecutionApprovalDecision,
  userID: string,
): ExecutionApprovalReceipt {
  const v = shape(value, ['command_id', 'approval'])
  const approval = parseExecutionApproval(v.approval, {
    projectID: before.project_id,
    executionID: before.execution_id,
    agentID: before.agent_id,
    approvalID: before.id,
  })
  requireValue(
    approval.status === (original.decision === 'approve' ? 'approved' : 'rejected') &&
      BigInt(approval.version) === BigInt(original.expected_version) + 1n &&
      approval.resolved_by === userID,
  )
  for (const key of [
    'tool_id',
    'tool_name',
    'tool_call_id',
    'operation_id',
    'operation_fingerprint',
    'created_at',
  ] as const)
    requireValue(approval[key] === before[key])
  return Object.freeze({ command_id: workID(v.command_id), approval })
}
export function createExecutionApprovalAPI(fetcher?: Fetch): ExecutionApprovalAPI {
  const call = accountTransport(fetcher)
  function capture(
    before: ExecutionApproval,
    value: ExecutionApprovalDecision,
    userID: string,
    write: WriteOptions,
  ) {
    const original = captureExecutionApprovalDecision(value),
      pending = capturePendingExecutionApproval(before),
      user = input(() => workID(userID))
    if (original.expected_version !== pending.version) throw new AccountFailure('invalid-input')
    return {
      original,
      pending,
      user,
      options: {
        projectID: pending.project_id,
        target: pending.id,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      },
    }
  }
  return {
    get(value, signal) {
      const scope = captureScope(value)
      return call(
        'getExecutionApproval',
        (value) => parseExecutionApproval(shape(value, ['approval']).approval, scope),
        { projectID: scope.projectID, target: scope.approvalID, signal },
      )
    },
    decide(before, value, userID, write) {
      const { original, pending, user, options } = capture(before, value, userID, write)
      return call(
        'decideExecutionApproval',
        (value) => parseReceipt(shape(value, ['receipt']).receipt, pending, original, user),
        options,
      )
    },
    lookup(before, value, userID, write) {
      const { original, pending, user, options } = capture(before, value, userID, write)
      return call(
        'lookupExecutionApprovalDecision',
        (value): ExecutionApprovalLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: parseReceipt(v.receipt, pending, original, user),
            })
          requireValue(v.status === 'not_observed' && v.receipt === null)
          return Object.freeze({ status: 'not_observed', receipt: null })
        },
        options,
      )
    },
  }
}
