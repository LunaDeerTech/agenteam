import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { AccountFailure, type Fetch } from '../api/client'
import { createProjectVariablesAPI, type ProjectVariablesAPI, type ProjectVariable } from '../api/project-variables'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(10), v = id(11), at = '2026-10-11T10:00:00.000001Z', fields = { name: 'ORDINARY', description: '', value: 'complete\nvalue' }
const view = (n = 2): SessionView => ({ user: { id: id(1), email: 'owner@example.test', username: 'owner', display_name: 'Owner', role: 'user', theme: 'system', version: '1', initial_password_suggestion: false }, session: { id: id(n), issued_at: at, absolute_expires_at: at, idle_expires_at: at }, csrf_token: 'S'.repeat(43) })
const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
const owners: SessionController[] = [], releases: (() => void)[] = []
afterEach(async () => { owners.splice(0).forEach(a => a.leave()); releases.splice(0).forEach(f => f()); await flushPromises() })
async function setup(override?: (api: ProjectVariablesAPI) => ProjectVariablesAPI) {
  let current = view(), cut = false
  let stored: ProjectVariable = { id: v, project_id: p, type: 'variable', ...fields, version: '1', created_at: at, updated_at: at }
  const receipt = (command: string) => ({ command, changed: true, variable: stored, event_id: id(20), audit_id: id(21) })
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return json(current)
    const base = `/api/v1/projects/${p}/variables`
    if (path === base && init.method === 'POST') {
      const body = JSON.parse(init.body as string)
      stored = { ...stored, ...body.request, id: body.request.variable_id }; delete (stored as unknown as Record<string, unknown>).variable_id
      if (cut) throw new Error('cut original response')
      return json(receipt('project.variable.create'))
    }
    if (path === `${base}/${stored.id}` && init.method === 'PATCH') {
      const body = JSON.parse(init.body as string); stored = { ...stored, ...body.request, version: String(BigInt(body.expected_version) + 1n) }
      if (cut) throw new Error('cut original response')
      return json(receipt('project.variable.update'))
    }
    if (path === `${base}/commands/lookup`) { const b = JSON.parse(init.body as string); return json({ status: 'committed', receipt: receipt(b.command) }) }
    if (path === `${base}/${stored.id}`) return json(stored)
    if (path === `${base}?limit=50`) { const { value: _value, ...summary } = stored; return json({ items: [summary] }) }
    throw new Error('Unexpected ordinary variable endpoint')
  })
  const api = createProjectVariablesAPI(fetcher), args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { projectVariables: override ? override(api) : api }
  const auth = createSessionController(...args); owners.push(auth); await auth.restore()
  return { auth, fetcher, cut() { cut = true }, replaceSession() { current = view(3) }, stored: () => stored }
}
describe('ordinary Project Variable Session owner', () => {
  it('mints once, publishes the original receipt and retires it after real current reads before the next patch', async () => {
    const f = await setup(), result = await f.auth.projectVariables.create(p, fields)
    expect('variable' in result).toBe(true)
    expect(f.auth.projectVariables.progress?.receipt).toBe(result)
    expect(f.auth.projectVariables.progress).toMatchObject({ projectID: p, kind: 'create', phase: 'confirmed', contextValid: true })
    const first = f.fetcher.mock.calls.find(([path, init]) => path.endsWith('/variables') && init.method === 'POST')!
    const body = JSON.parse(first[1].body as string)
    expect(body.request.variable_id).toMatch(/^[0-9a-f-]{14}7[0-9a-f-]{21}$/)
    expect(body.request).toMatchObject(fields)
    await expect(f.auth.projectVariables.create(p, fields)).rejects.toMatchObject({ kind: 'busy' })
    const current = await f.auth.projectVariables.get(p, body.request.variable_id)
    expect(current.value).toBe(fields.value)
    expect((await f.auth.projectVariables.list(p, { limit: 50 })).items).toHaveLength(1)
    f.auth.projectVariables.finishConfirmed()
    const updated = await f.auth.projectVariables.update(p, current.id, current.version, { value: '' })
    expect('variable' in updated && updated.variable.value).toBe('')
    const write = f.fetcher.mock.calls.find(([, init]) => init.method === 'PATCH')!
    expect(JSON.parse(write[1].body as string)).toEqual({ expected_version: '1', request: { value: '' } })
    expect((write[1].headers as Record<string, string>)['Idempotency-Key']).not.toBe((first[1].headers as Record<string, string>)['Idempotency-Key'])
  })
  it('keeps an unknown create bound to one key and full intent and permits only the original lookup', async () => {
    const f = await setup(); f.cut()
    await expect(f.auth.projectVariables.create(p, fields)).rejects.toMatchObject({ kind: 'transport' })
    expect(f.auth.projectVariables.progress).toMatchObject({ phase: 'uncertain', receipt: null, contextValid: true })
    await expect(f.auth.projectVariables.update(p, v, '1', { value: '' })).rejects.toMatchObject({ kind: 'busy' })
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
    const observed = await f.auth.projectVariables.checkOriginal()
    expect(observed).toMatchObject({ status: 'committed' })
    const writes = f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(writes).toHaveLength(2)
    expect(writes[1]![0]).toBe(`/api/v1/projects/${p}/variables/commands/lookup`)
    expect(JSON.parse(writes[1]![1].body as string)).toEqual({ command: 'project.variable.create', ...JSON.parse(writes[0]![1].body as string) })
    expect((writes[1]![1].headers as Record<string, string>)['Idempotency-Key']).toBe((writes[0]![1].headers as Record<string, string>)['Idempotency-Key'])
    expect(f.auth.projectVariables.progress?.receipt).toBe('receipt' in observed ? observed.receipt : null)
  })
  it('joins the original abandoned read and ignores its late authorization failure', async () => {
    let reject!: (e: unknown) => void
    const tail = new Promise<ProjectVariable>((_resolve, r) => { reject = r })
    releases.push(() => reject(new AccountFailure('cancelled')))
    const f = await setup(api => ({ ...api, get: () => tail }))
    const read = f.auth.projectVariables.get(p, v)
    const visible = expect(read).rejects.toMatchObject({ kind: 'cancelled' })
    await flushPromises(); f.auth.projectVariables.abandonReads(); await visible
    expect(f.auth.state.busy).toBe(true)
    await expect(f.auth.projectVariables.list(p, { limit: 50 })).rejects.toMatchObject({ kind: 'busy' })
    reject(new AccountFailure('problem', { type: 'urn:agenteam:problem:test', title: 'Denied', status: 401, detail: '', instance: '/', code: 'SESSION_REVOKED', request_id: id(90), commit_state: 'not_started' }))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.personalContext.identity?.sessionID).toBe(id(2))
  })
  it('erases the old pending plaintext and refuses lookup after an actual Session replacement', async () => {
    const f = await setup(); f.cut()
    await expect(f.auth.projectVariables.create(p, fields)).rejects.toMatchObject({ kind: 'transport' })
    f.replaceSession(); await f.auth.restore()
    expect(f.auth.projectVariables.progress).toBeNull()
    await expect(f.auth.projectVariables.checkOriginal()).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(f.fetcher.mock.calls.filter(([path]) => path.endsWith('/commands/lookup'))).toHaveLength(0)
  })
})
