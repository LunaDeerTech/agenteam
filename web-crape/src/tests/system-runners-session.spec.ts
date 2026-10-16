import { watch } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createSystemRunnersAPI, type RunnerCommand } from '../api/system-runners'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const session = {
  user: {
    id: id(1),
    email: 'admin@example.test',
    username: 'admin',
    display_name: 'Admin',
    role: 'admin',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const row = () => ({
  id: id(3),
  name: 'Fixture Runner',
  description: '',
  tags: [],
  root_path: '/owned/workspace',
  version: '1',
  credential_generation: '1',
  status: 'offline',
  public_key_fingerprint: null,
  enrolled_at: null,
  last_seen_at: null,
  last_hello: null,
  created_at: time,
  updated_at: time,
})
const command = (): RunnerCommand => ({
  command: 'runner.create',
  target: id(3),
  request: {
    runner_id: id(3),
    name: 'Fixture Runner',
    description: '',
    tags: [],
    root_path: '/owned/workspace',
  },
})
const receipt = () => ({
  command_id: id(10),
  command: 'runner.create',
  runner: row(),
  changed: true,
})
const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
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
      title: 'Failure',
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
  owners.splice(0).forEach((x) => x.leave())
  releases.splice(0).forEach((r) => r())
  await flushPromises()
})
async function fixture(handler: Fetch, role = 'admin') {
  const fetcher = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session'
      ? Promise.resolve(json({ ...session, user: { ...session.user, role } }))
      : handler(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { systemRunners: createSystemRunnersAPI(fetcher) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return { auth, fetcher }
}
describe('System Runner commands use the actual Session owner', () => {
  it('rejects an ordinary user before any Runner request', async () => {
    const f = await fixture(async () => json({ items: [], next: null }), 'user')
    await expect(f.auth.system.runners.list({})).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(f.auth.system.runners.start(command())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.fetcher).toHaveBeenCalledTimes(1)
  })
  it('keeps first-response material out of public Session progress', async () => {
    const f = await fixture(async () =>
      json({
        receipt: receipt(),
        token_available: true,
        enrollment_token: 'A'.repeat(43),
        expires_at: time,
      }),
    )
    expect((await f.auth.system.runners.start(command())).token_available).toBe(true)
    expect(f.auth.system.runners.progress?.phase).toBe('confirmed')
    expect(JSON.stringify(f.auth.system.runners.progress)).not.toContain('enrollment_token')
    expect(JSON.stringify(f.auth.system.runners.progress)).not.toContain('A'.repeat(43))
  })
  it('retains the original ID, full body and key after Unknown, and only sends explicit Lookups', async () => {
    let observed = false
    const f = await fixture(async (url) =>
      String(url).endsWith('/lookup')
        ? json(
            observed
              ? { state: 'committed', receipt: receipt(), token_available: false }
              : { state: 'not_observed', token_available: false },
          )
        : failure(503, 'COMMIT_UNKNOWN', 'unknown'),
    )
    const c = command()
    await expect(f.auth.system.runners.start(c)).rejects.toMatchObject({
      problem: { code: 'COMMIT_UNKNOWN' },
    })
    ;(c.request as { name: string }).name = 'locally changed'
    await expect(f.auth.system.runners.start(command())).rejects.toMatchObject({ kind: 'busy' })
    await f.auth.system.runners.checkOriginal()
    expect(f.auth.system.runners.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    observed = true
    await f.auth.system.runners.checkOriginal()
    expect(f.auth.system.runners.progress?.phase).toBe('confirmed')
    const writes = f.fetcher.mock.calls.filter(([, i]) => i.method === 'POST')
    expect(writes).toHaveLength(3)
    const original = JSON.parse(writes[0]![1].body as string)
    for (const [url, i] of writes.slice(1)) {
      expect(url).toBe(`/api/v1/system/runners/${id(3)}/commands/lookup`)
      expect(JSON.parse(i.body as string)).toEqual({ command: 'runner.create', request: original })
      expect((i.headers as Record<string, string>)['Idempotency-Key']).toBe(
        (writes[0]![1].headers as Record<string, string>)['Idempotency-Key'],
      )
    }
  })
  it('keeps the actual Cookie owner until held body cancellation returns', async () => {
    let release!: () => void, entered!: () => void
    const gate = new Promise<void>((r) => {
        release = r
        releases.push(r)
      }),
      cancelling = new Promise<void>((r) => (entered = r))
    const cancel = vi.fn(() => {
      entered()
      return gate
    })
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"items":[],"next":null}'))
      },
      cancel,
    })
    const f = await fixture(
      async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }),
    )
    const running = f.auth.system.runners.list({}).catch((e) => e)
    await flushPromises()
    f.auth.system.runners.cancel()
    await cancelling
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.system.runners.get(id(3))).rejects.toMatchObject({ kind: 'busy' })
    release()
    expect(await running).toMatchObject({ kind: 'cancelled' })
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(f.auth.state.busy).toBe(false)
  })
  it('current Admin denial revokes the management context', async () => {
    const f = await fixture(async () => failure(403, 'FORBIDDEN'))
    await expect(f.auth.system.runners.get(id(3))).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.system.denied).toBe(true)
    await expect(f.auth.system.runners.start(command())).rejects.toMatchObject({
      kind: 'invalid-input',
    })
  })
  it('does not deliver one-time material after a confirmed synchronous consumer retires identity', async () => {
    const f = await fixture(async () =>
      json({
        receipt: receipt(),
        token_available: true,
        enrollment_token: 'A'.repeat(43),
        expires_at: time,
      }),
    )
    const stop = watch(
      () => f.auth.system.runners.progress?.phase,
      (phase) => {
        if (phase === 'confirmed') f.auth.leave()
      },
      { flush: 'sync' },
    )
    try {
      await expect(f.auth.system.runners.start(command())).rejects.toMatchObject({
        kind: 'cancelled',
      })
      expect(f.auth.system.runners.progress).toBeNull()
    } finally {
      stop()
    }
  })
  it('blocks denied recovery and resumes only the same original Lookup after current Admin recheck', async () => {
    const f = await fixture(async (url, init) => {
      if (init.method === 'GET') return failure(403, 'FORBIDDEN')
      if (url.endsWith('/lookup')) return json({ state: 'not_observed', token_available: false })
      return failure(503, 'COMMIT_UNKNOWN', 'unknown')
    })
    await expect(f.auth.system.runners.start(command())).rejects.toMatchObject({ kind: 'problem' })
    await expect(f.auth.system.runners.get(id(3))).rejects.toMatchObject({
      problem: { status: 403 },
    })
    expect(f.auth.system.runners.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: false,
    })
    await expect(f.auth.system.runners.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    await f.auth.restore()
    await f.auth.system.runners.checkOriginal()
    const writes = f.fetcher.mock.calls.filter(([, i]) => i.method === 'POST')
    expect(writes).toHaveLength(2)
    expect(JSON.parse(writes[1]![1].body as string)).toEqual({
      command: 'runner.create',
      request: JSON.parse(writes[0]![1].body as string),
    })
    expect((writes[1]![1].headers as Record<string, string>)['Idempotency-Key']).toBe(
      (writes[0]![1].headers as Record<string, string>)['Idempotency-Key'],
    )
  })
})
