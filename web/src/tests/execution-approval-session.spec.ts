import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createExecutionApprovalAPI, type ExecutionApproval } from '../api/execution-approval'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { at, execution, id, json, p, session, t } from './task-execution-fixture'

const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((owner) => owner.leave())
  releases.splice(0).forEach((release) => release())
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
const waiting = () =>
  execution(100, {
    status: 'waiting',
    started_at: at,
    waiting_reason: 'approval',
    waiting_reference: id(400),
    waiting_at: at,
  })
const receipt = () => ({
  command_id: id(403),
  approval: { ...before(), status: 'approved', version: '2', resolved_at: at, resolved_by: id(8) },
})
async function fixture() {
  let handler: Fetch = async () => json({ receipt: receipt() })
  const fetch = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session())) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { executionApproval: createExecutionApprovalAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    intercept: (next: Fetch) => {
      handler = next
    },
  }
}
describe('One-operation approval Session intent', () => {
  it('cannot turn a current GET into command confirmation and preserves the original decision through lookup', async () => {
    const f = await fixture()
    let found = false
    f.intercept(async (url, init) => {
      if (init.method === 'GET') return json({ approval: receipt().approval })
      if (url.endsWith('/lookup'))
        return json({
          status: found ? 'committed' : 'not_observed',
          receipt: found ? receipt() : null,
        })
      throw new Error('lost reply')
    })
    await expect(
      f.auth.executionApproval.start(p, t, waiting(), before(), 'approve'),
    ).rejects.toMatchObject({ kind: 'transport' })
    expect(f.auth.executionApproval.progress?.phase).toBe('uncertain')
    const current = await f.auth.executionApproval.get({
      projectID: p,
      executionID: id(100),
      agentID: id(7),
      approvalID: id(400),
    })
    expect(current.status).toBe('approved')
    expect(f.auth.executionApproval.progress?.phase).toBe('uncertain')
    await expect(
      f.auth.executionApproval.start(p, t, waiting(), before(), 'reject'),
    ).rejects.toMatchObject({ kind: 'busy' })
    await expect(
      f.auth.executionCancel.start(p, t, id(100), {
        expected_version: '1',
        expected_task_version: '3',
      }),
    ).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.executionApproval.checkOriginal()
    expect(f.auth.executionApproval.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    found = true
    await f.auth.executionApproval.checkOriginal()
    expect(f.auth.executionApproval.progress).toMatchObject({
      phase: 'confirmed',
      decision: 'approve',
      receipt: { approval: { status: 'approved', operation_id: id(402) } },
    })
    const writes = f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(writes).toHaveLength(3)
    expect(writes.filter(([url]) => url.endsWith('/decision'))).toHaveLength(1)
    expect(
      new Set(writes.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
    expect(
      writes.every(
        ([, init]) => init.body === JSON.stringify({ decision: 'approve', expected_version: '1' }),
      ),
    ).toBe(true)
  })
  it('rejects stale waiting identity and cancellation before dispatch', async () => {
    const f = await fixture()
    for (const row of [
      { ...waiting(), waiting_reference: id(499) },
      { ...waiting(), cancel_requested_at: at },
      { ...waiting(), agent_id: id(88) },
    ])
      await expect(
        f.auth.executionApproval.start(p, t, row, before(), 'approve'),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })
  it('retires approval progress on Session exit while the original response tail remains owned', async () => {
    const f = await fixture()
    let finish!: (response: Response) => void
    const tail = new Promise<Response>((resolve) => {
      finish = resolve
    })
    releases.push(() => finish(json({ receipt: receipt() })))
    f.intercept(async () => tail)
    const saving = f.auth.executionApproval
      .start(p, t, waiting(), before(), 'approve')
      .catch((error) => error)
    await flushPromises()
    f.auth.leave()
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.executionApproval.progress).toBeNull()
    finish(json({ receipt: receipt() }))
    await saving
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.executionApproval.progress).toBeNull()
    await f.auth.restore()
    await expect(f.auth.executionApproval.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
})
