import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import type { Fetch } from '../api/client'
import { createAgentConfigurationAPI } from '../api/agent-configuration'
import { createAgentRunnerMountsAPI } from '../api/agent-runner-mounts'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const session = {
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const project = id(3),
  runner = id(4),
  target = id(5)
const path = `/api/v1/projects/${project}/available-runners`
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(90),
    },
  })
const failure = (status: number, code: string, commit = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Unavailable',
      detail: '',
      status,
      code,
      instance: '/',
      request_id: id(90),
      commit_state: commit,
    },
    status,
  )
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((o) => o.leave())
  releases.splice(0).forEach((r) => r())
  await flushPromises()
})
async function fixture(handler: Fetch) {
  const fetcher = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session' ? Promise.resolve(json(session)) : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = {
    agentConfiguration: createAgentConfigurationAPI(fetcher),
    agentRunnerMounts: createAgentRunnerMountsAPI(fetcher),
  }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return { auth, fetcher }
}
describe('Agent Runner reads on the existing Session owner', () => {
  it('does not release the Cookie owner until the original body cancellation actually returns', async () => {
    let release!: () => void, entered!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
      releases.push(resolve)
    })
    const cancelling = new Promise<void>((resolve) => {
      entered = resolve
    })
    const cancel = vi.fn(() => {
      entered()
      return gate
    })
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"items":[]}'))
      },
      cancel,
    })
    const f = await fixture(
      async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    )
    const reading = f.auth.agentConfiguration.runners(project, { limit: 50 }).catch((e) => e)
    await flushPromises()
    f.auth.agentConfiguration.abandonRead()
    await cancelling
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.agentConfiguration.runnerMounts(project, target)).rejects.toMatchObject({
      kind: 'busy',
    })
    expect(f.fetcher.mock.calls.filter(([url]) => String(url).startsWith(path))).toHaveLength(1)
    release()
    expect(await reading).toMatchObject({ kind: 'cancelled' })
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(f.auth.state.busy).toBe(false)
  })
  it('keeps local read refusal local to Project and preserves the authenticated ordinary user', async () => {
    const f = await fixture(async () => failure(403, 'FORBIDDEN'))
    await expect(f.auth.agentConfiguration.runners(project, {})).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.identity?.userID).toBe(id(1))
    expect(f.auth.agentConfiguration.progress).toBeNull()
  })
  it('keeps exact runner declarations, expected version and key for Unknown while candidate reads change', async () => {
    const f = await fixture(async (url, init) => {
      if (String(url).startsWith(path))
        return json({
          items: [
            { id: runner, name: 'Offline', description: '', version: '1', status: 'offline' },
          ],
        })
      if (String(url).endsWith('/lookup')) return json({ status: 'not_observed', receipt: null })
      expect(init.method).toBe('PATCH')
      return failure(503, 'COMMIT_UNKNOWN', 'unknown')
    })
    const input = {
      expected_version: '4',
      request: { runner_mounts: [{ runner_id: runner, workspace: 'original.dir' }] },
    }
    await expect(
      f.auth.agentConfiguration.startUpdate(project, target, input),
    ).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    input.request.runner_mounts[0]!.workspace = 'changed'
    await f.auth.agentConfiguration.runners(project, {})
    await f.auth.agentConfiguration.checkOriginal()
    const writes = f.fetcher.mock.calls.filter(([, init]) => init.method !== 'GET')
    expect(writes).toHaveLength(2)
    expect(JSON.parse(writes[1]![1].body as string)).toEqual({
      command: 'agent.update',
      target_id: target,
      expected_version: '4',
      request: { runner_mounts: [{ runner_id: runner, workspace: 'original.dir' }] },
    })
    expect((writes[1]![1].headers as Record<string, string>)['Idempotency-Key']).toBe(
      (writes[0]![1].headers as Record<string, string>)['Idempotency-Key'],
    )
    expect(f.auth.agentConfiguration.progress?.phase).toBe('uncertain')
  })
})
