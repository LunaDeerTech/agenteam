import { describe, expect, it, vi } from 'vitest'
import { createExecutionApprovalAPI, type ExecutionApproval } from '../api/execution-approval'
import { createTaskExecutionsAPI } from '../api/task-executions'
import type { Fetch } from '../api/client'
import { at, execution, id, json, p, t } from './task-execution-fixture'

const scope = { projectID: p, executionID: id(100), agentID: id(7), approvalID: id(400) }
const pending = (): ExecutionApproval => ({
  id: scope.approvalID,
  project_id: p,
  execution_id: scope.executionID,
  agent_id: scope.agentID,
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
    ...pending(),
    status: decision === 'approve' ? 'approved' : 'rejected',
    version: '2',
    resolved_at: at,
    resolved_by: id(8),
  },
})
const decision = { decision: 'approve' as const, expected_version: '1' }
const write = { csrfToken: 'S'.repeat(43), key: 'original-approval-1' }

describe('One-operation approval API', () => {
  it('reads a current scoped approval and preserves one decision and original lookup identity', async () => {
    const fetch = vi.fn<Fetch>(async (url, init) =>
      init.method === 'GET'
        ? json({ approval: pending() })
        : url.endsWith('/lookup')
          ? json({ status: 'committed', receipt: receipt() })
          : json({ receipt: receipt() }),
    )
    const api = createExecutionApprovalAPI(fetch)
    const before = await api.get(scope, new AbortController().signal)
    expect(before.tool_name).toBe('run-command')
    expect(await api.decide(before, decision, id(8), write)).toEqual(receipt())
    expect(await api.lookup(before, decision, id(8), write)).toEqual({
      status: 'committed',
      receipt: receipt(),
    })
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      `/api/v1/projects/${p}/approvals/${scope.approvalID}`,
      `/api/v1/projects/${p}/approvals/${scope.approvalID}/decision`,
      `/api/v1/projects/${p}/approvals/${scope.approvalID}/decision/lookup`,
    ])
    for (const [, init] of fetch.mock.calls.slice(1)) {
      expect(init.body).toBe(JSON.stringify(decision))
      expect(new Headers(init.headers).get('Idempotency-Key')).toBe(write.key)
    }
  })
  it('rejects foreign scope, private extensions and inconsistent decision provenance', async () => {
    let value: unknown
    const api = createExecutionApprovalAPI(async () => json(value))
    for (const patch of [
      { execution_id: id(99) },
      { agent_id: id(99) },
      { arguments: 'private' },
      { tool_name: 'untrusted-command' },
      { status: 'approved' },
    ]) {
      value = { approval: { ...pending(), ...patch } }
      await expect(api.get(scope, new AbortController().signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    for (const patch of [
      { operation_id: id(99) },
      { operation_fingerprint: 'sha256:' + 'b'.repeat(64) },
      { resolved_by: id(99) },
      { status: 'rejected' },
    ]) {
      value = { receipt: { ...receipt(), approval: { ...receipt().approval, ...patch } } }
      await expect(api.decide(pending(), decision, id(8), write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    value = { status: 'not_observed', receipt: null }
    expect(await api.lookup(pending(), decision, id(8), write)).toEqual(value)
    value = { status: 'not_observed', receipt: receipt() }
    await expect(api.lookup(pending(), decision, id(8), write)).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('keeps legacy execution records compatible while validating the entire new waiting triple', async () => {
    let row = execution()
    const api = createTaskExecutionsAPI(async () => json({ execution: row })),
      signal = new AbortController().signal
    expect(await api.get(p, t, id(100), signal)).toEqual(row)
    row = execution(100, { waiting_reason: null, waiting_reference: null, waiting_at: null })
    expect(await api.get(p, t, id(100), signal)).toEqual(row)
    const waiting = execution(100, {
      status: 'waiting',
      started_at: at,
      waiting_reason: 'approval',
      waiting_reference: scope.approvalID,
      waiting_at: at,
    })
    row = waiting
    expect((await api.get(p, t, id(100), signal)).waiting_reference).toBe(scope.approvalID)
    for (const patch of [
      { status: 'running' },
      { waiting_reference: null },
      { waiting_at: '2026-01-01T00:00:00.000000Z' },
      { started_at: null },
    ]) {
      row = { ...waiting, ...patch } as typeof row
      await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    row = execution(100, { waiting_reference: scope.approvalID })
    await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
