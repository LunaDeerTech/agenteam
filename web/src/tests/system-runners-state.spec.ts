import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createSystemRunnersAPI } from '../api/system-runners'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createSystemRunners, type SystemRunnersController } from '../composables/useSystemRunners'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const user = {
  id: id(1),
  email: 'admin@example.test',
  username: 'admin',
  display_name: 'Admin',
  role: 'admin',
  theme: 'system',
  version: '1',
  initial_password_suggestion: false,
}
const session = {
  user,
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const row = () => ({
  id: id(3),
  name: 'Fixture Runner',
  description: '',
  tags: [] as string[],
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
function barrier<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}
const owners: SessionController[] = [],
  pages: SystemRunnersController[] = []
afterEach(async () => {
  pages.splice(0).forEach((p) => p.dispose())
  owners.splice(0).forEach((a) => a.leave())
  await flushPromises()
})
async function fixture() {
  let item = row(),
    sessionRead: Fetch = async () => json(session),
    list: Fetch = async () => json({ items: [item], next: null }),
    detail: Fetch = async () => json(item)
  let write: Fetch = async (_url, init) => {
      const input = JSON.parse(init.body as string)
      item = {
        ...item,
        ...input,
        id: input.runner_id ?? item.id,
        version: input.expected_version ? String(BigInt(input.expected_version) + 1n) : '1',
      }
      delete (item as Record<string, unknown>).runner_id
      delete (item as Record<string, unknown>).expected_version
      return json({
        receipt: {
          command_id: id(10),
          command: input.runner_id ? 'runner.create' : 'runner.update',
          runner: item,
          changed: true,
        },
        token_available: !!input.runner_id,
        ...(input.runner_id
          ? {
              enrollment_token: 'A'.repeat(43),
              expires_at: new Date(Date.now() + 600000).toISOString().replace('Z', '000Z'),
            }
          : {}),
      })
    },
    lookup: Fetch = async () => json({ state: 'not_observed', token_available: false })
  const fetcher = vi.fn<Fetch>((url, init) =>
    url === '/api/v1/session'
      ? sessionRead(url, init)
      : url.includes('/commands/lookup')
        ? lookup(url, init)
        : init.method !== 'GET'
          ? write(url, init)
          : url.includes('?')
            ? list(url, init)
            : detail(url, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { systemRunners: createSystemRunnersAPI(fetcher) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  const page = createSystemRunners(auth)
  pages.push(page)
  page.attach()
  await flushPromises()
  return {
    auth,
    page,
    fetcher,
    get item() {
      return item
    },
    setItem(v: ReturnType<typeof row>) {
      item = v
    },
    setSession(v: Fetch) {
      sessionRead = v
    },
    setList(v: Fetch) {
      list = v
    },
    setDetail(v: Fetch) {
      detail = v
    },
    setWrite(v: Fetch) {
      write = v
    },
    setLookup(v: Fetch) {
      lookup = v
    },
  }
}
function fill(p: SystemRunnersController) {
  p.openCreate()
  Object.assign(p.draft, {
    name: 'Fixture Runner',
    description: '',
    tags: '',
    root_path: '/owned/workspace',
  })
}
describe('System Runner current page and private material lifetime', () => {
  it('creates then GETs current facts, keeps material only in view memory, and clears on checking', async () => {
    const f = await fixture()
    fill(f.page)
    await f.page.save()
    expect(f.page.material.token.length).toBe(43)
    expect(f.page.state.detail?.id).toBe(f.item.id)
    const writes = f.fetcher.mock.calls.filter(([, i]) => i.method === 'POST')
    expect(writes).toHaveLength(1)
    const writeIndex = f.fetcher.mock.calls.indexOf(writes[0]!)
    expect(f.fetcher.mock.calls[writeIndex + 1]![0]).toBe(`/api/v1/system/runners/${f.item.id}`)
    expect(JSON.stringify(f.auth.system.runners.progress)).not.toContain(f.page.material.token)
    const gate = barrier<Response>()
    f.setSession(() => gate.promise)
    const checking = f.auth.restore()
    await flushPromises()
    expect(f.page.material.token).toBe('')
    expect(f.page.visible.value).toBe(false)
    gate.resolve(json(session))
    await checking
    await flushPromises()
    expect(f.page.material.token).toBe('')
    expect(f.page.state.phase).toBe('ready')
  })
  it('discards late material after page retirement without abandoning the original Unknown intent', async () => {
    const f = await fixture(),
      gate = barrier<Response>()
    fill(f.page)
    const target = f.page.editor.target
    f.setWrite(() => gate.promise)
    const saving = f.page.save()
    await flushPromises()
    f.page.detach()
    await flushPromises()
    gate.resolve(
      json({
        receipt: {
          command_id: id(10),
          command: 'runner.create',
          runner: { ...row(), id: target },
          changed: true,
        },
        token_available: true,
        enrollment_token: 'A'.repeat(43),
        expires_at: new Date(Date.now() + 600000).toISOString().replace('Z', '000Z'),
      }),
    )
    await saving
    await flushPromises()
    expect(f.page.material.token).toBe('')
    expect(f.page.state.detail).toBeNull()
    expect(f.auth.system.runners.progress?.phase).toBe('uncertain')
  })
  it('preserves conflict draft, explicitly adopts a fresh version, and preserves draft on transient GET failure', async () => {
    const f = await fixture()
    await f.page.select(id(3))
    f.page.openEdit()
    f.page.draft.description = 'local draft'
    f.setWrite(async () => failure(409, 'VERSION_CONFLICT', 'not_committed'))
    await f.page.save()
    expect(f.page.editor.conflict).toBe(true)
    expect(f.page.draft.description).toBe('local draft')
    expect(f.page.canSave.value).toBe(false)
    f.setDetail(async () => failure(503, 'DEPENDENCY_UNBOUND'))
    await f.page.rebase()
    expect(f.page.draft.description).toBe('local draft')
    expect(f.page.editor.version).toBe('1')
    f.setDetail(async () => json({ ...row(), version: '4' }))
    await f.page.rebase()
    expect(f.page.editor.version).toBe('4')
    expect(f.page.draft.description).toBe('local draft')
    expect(f.page.canSave.value).toBe(true)
  })
  it('keeps Unknown frozen, does not infer rollback, and reads current data after a historical receipt', async () => {
    const f = await fixture()
    fill(f.page)
    const target = f.page.editor.target
    f.setWrite(async () => failure(503, 'COMMIT_UNKNOWN', 'unknown'))
    await f.page.save()
    expect(f.page.unsettled.value).toBe(true)
    await f.page.checkOriginal()
    expect(f.page.progress.value?.observation).toBe('not_observed')
    expect(f.page.canSave.value).toBe(false)
    f.setLookup(async () =>
      json({
        state: 'committed',
        receipt: {
          command_id: id(10),
          command: 'runner.create',
          runner: { ...row(), id: target },
          changed: true,
        },
        token_available: false,
      }),
    )
    f.setDetail(async () => json({ ...row(), id: target, name: 'Current name', version: '4' }))
    await f.page.checkOriginal()
    expect(f.page.state.detail?.version).toBe('4')
    expect(f.page.state.detail?.name).toBe('Current name')
    expect(f.page.material.token).toBe('')
    expect(
      f.fetcher.mock.calls.filter(([url, i]) => i.method === 'POST' && !url.endsWith('/lookup')),
    ).toHaveLength(1)
    f.page.openBusiness('enrollment')
    expect(f.page.business.version).toBe('4')
  })
  it('clears private material and current observations on a current forbidden response', async () => {
    const f = await fixture()
    fill(f.page)
    await f.page.save()
    expect(f.page.material.token.length).toBe(43)
    f.setDetail(async () => failure(403, 'FORBIDDEN'))
    await f.page.refreshDetail()
    await flushPromises()
    expect(f.page.material.token).toBe('')
    expect(f.page.visible.value).toBe(false)
    expect(f.page.state.detail).toBeNull()
    expect(f.page.state.rows).toEqual([])
  })
  it('rejects an entire bad appended page without publishing its prefix', async () => {
    const f = await fixture()
    f.setList(async () => json({ items: [row()], next: id(3) }))
    await f.page.readList()
    f.setList(async () =>
      json({
        items: [
          { ...row(), id: id(4) },
          { ...row(), id: id(5), enrollment_token: 'private-marker' },
        ],
        next: null,
      }),
    )
    await f.page.readList(true)
    expect(f.page.state.rows.map((x) => x.id)).toEqual([id(3)])
    expect(f.page.state.next).toBe(id(3))
  })
  it('restarts the current directory only after the retired original body cancellation joins', async () => {
    const f = await fixture(),
      gate = barrier<void>(),
      cancelling = barrier<void>()
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode('{"items":[],"next":null}'))
      },
      cancel() {
        cancelling.resolve()
        return gate.promise
      },
    })
    f.setList(async () => new Response(body, { headers: { 'Content-Type': 'application/json' } }))
    const old = f.page.readList()
    await flushPromises()
    f.page.detach()
    f.page.attach()
    await cancelling.promise
    expect(f.auth.state.busy).toBe(true)
    f.setList(async () => json({ items: [{ ...row(), name: 'Current directory' }], next: null }))
    gate.resolve()
    await old
    await flushPromises()
    expect(f.page.state.phase).toBe('ready')
    expect(f.page.state.rows[0]?.name).toBe('Current directory')
  })
})
