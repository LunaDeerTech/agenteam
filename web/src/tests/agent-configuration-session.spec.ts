import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { type Fetch, type Problem, uuid7 } from '../api/client'
import {
  createAgentConfigurationAPI,
  type AgentCreateFields,
  type AgentCreateInput,
  type AgentCreateReceipt,
  type AgentUpdateInput,
  type AgentUpdateReceipt,
} from '../api/agent-configuration'
import { createSessionController, type SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const instant = '2026-10-11T10:00:00.000000Z',
  project = id(10)
const updateAgent = id(60),
  updatePath = `/api/v1/projects/${project}/agents/${updateAgent}/configuration`
const createPath = `/api/v1/projects/${project}/agents`,
  lookupPath = `/api/v1/projects/${project}/agent-commands/lookup`
const session = (n = 2): SessionView => ({
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
  session: {
    id: id(n),
    issued_at: instant,
    absolute_expires_at: instant,
    idle_expires_at: instant,
  },
  csrf_token: 'S'.repeat(43),
})
const fields = (): Omit<AgentCreateFields, 'agent_id'> => ({
  name: 'Review-Agent',
  model_ref: id(20),
  inject_agents_md: true,
  approval_policy: 'default',
  allowed_tool_ids: [],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [],
  display_name: null,
  instructions: ' 原始指令\n原字节。 ',
  add_skills_enabled: false,
})
function receipt(value: AgentCreateInput): AgentCreateReceipt {
  const r = value.request
  return {
    changed: true,
    event_ids: [id(30)],
    agent: {
      id: r.agent_id,
      project_id: project,
      name: r.name,
      normalized_name: r.name.toLowerCase(),
      display_name: r.display_name ?? null,
      tag_color: r.tag_color ?? null,
      description: r.description ?? '',
      instructions: r.instructions ?? '',
      inject_agents_md: r.inject_agents_md,
      model_ref: r.model_ref,
      reasoning_effort: r.reasoning_effort ?? null,
      approval_policy: r.approval_policy,
      approval_model_ref: r.approval_model_ref ?? null,
      allowed_tool_ids: [id(21)],
      allowed_mount_ids: r.allowed_mount_ids!,
      allowed_secret_variable_ids: r.allowed_secret_variable_ids,
      lifecycle: 'active',
      version: '1',
      created_at: instant,
      updated_at: instant,
    },
  }
}
function updated(value: AgentUpdateInput): AgentUpdateReceipt {
  const base = receipt({ request: { ...fields(), agent_id: updateAgent } }).agent
  return {
    changed: true,
    event_ids: [id(61)],
    agent: {
      ...base,
      ...value.request,
      normalized_name: (value.request.name ?? base.name).toLowerCase(),
      version: String(BigInt(value.expected_version) + 1n),
    },
  }
}
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (code: string, commit_state: Problem['commit_state']) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status: 503,
      detail: '',
      instance: createPath,
      code,
      request_id: id(99),
      commit_state,
    },
    503,
  )
const owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((a) => a.leave())
  releases.splice(0).forEach((release) => release())
  await flushPromises()
  vi.useRealTimers()
})
async function fixture() {
  let current = session(),
    create: Fetch | null = null,
    update: Fetch | null = null
  let status: 'not_observed' | 'in_progress' | 'committed' = 'committed'
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return json(current)
    if (path === createPath)
      return create ? create(path, init) : json(receipt(JSON.parse(init.body as string)))
    if (path === updatePath)
      return update ? update(path, init) : json(updated(JSON.parse(init.body as string)))
    if (path === lookupPath) {
      const body = JSON.parse(init.body as string)
      return json({
        status,
        receipt:
          status === 'committed'
            ? body.command === 'agent.update'
              ? updated(body)
              : receipt(body)
            : null,
      })
    }
    throw new Error('Unexpected Agent Session request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[19] = { agentConfiguration: createAgentConfigurationAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetch,
    setCreate(value: Fetch | null) {
      create = value
    },
    setUpdate(value: Fetch | null) {
      update = value
    },
    updates: () => fetch.mock.calls.filter(([path]) => path === updatePath),
    setStatus(value: typeof status) {
      status = value
    },
    setSession(value: SessionView) {
      current = value
    },
    creates: () => fetch.mock.calls.filter(([path]) => path === createPath),
    lookups: () => fetch.mock.calls.filter(([path]) => path === lookupPath),
  }
}

describe('Agent create original Session ownership', () => {
  it('freezes its own UUIDv7, presence and key and only queries that original command after Unknown', async () => {
    const f = await fixture()
    f.setCreate(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    const draft = { ...fields(), allowed_mount_ids: [id(40)] },
      expected = structuredClone(draft)
    const operation = f.auth.agentConfiguration.startCreate(project, draft)
    draft.name = 'Different-Agent'
    draft.allowed_mount_ids.push(id(41))
    await expect(operation).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const original = JSON.parse(f.creates()[0]![1].body as string) as AgentCreateInput
    const headers = f.creates()[0]![1].headers as Record<string, string>
    expect(uuid7.test(original.request.agent_id)).toBe(true)
    expect(original.request).toEqual({ ...expected, agent_id: original.request.agent_id })
    expect(Object.hasOwn(original.request, 'install_skill_enabled')).toBe(false)
    expect(Object.hasOwn(original.request, 'tag_color')).toBe(false)
    expect(f.auth.agentConfiguration.progress).toMatchObject({
      phase: 'uncertain',
      agentID: original.request.agent_id,
      contextValid: true,
    })
    expect(JSON.stringify(f.auth.agentConfiguration.progress)).not.toContain(
      headers['Idempotency-Key'],
    )
    expect(f.auth.workReview.progress).toBeNull()
    await expect(f.auth.agentConfiguration.startCreate(project, fields())).rejects.toMatchObject({
      kind: 'busy',
    })
    await f.auth.restore()
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setStatus(status)
      expect(await f.auth.agentConfiguration.checkOriginal()).toMatchObject({ status })
      const request = f.lookups().at(-1)![1]
      expect(JSON.parse(request.body as string)).toEqual({ command: 'agent.create', ...original })
      expect(request.headers).toMatchObject({
        'Idempotency-Key': headers['Idempotency-Key'],
        'X-CSRF-Token': session().csrf_token,
      })
    }
    expect(f.creates()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(3)
    expect(f.auth.agentConfiguration.progress).toMatchObject({
      phase: 'confirmed',
      receipt: receipt(original),
    })
  })

  it('allows explicit editing after a proven rejection with a new command and never converts Agent progress to Work', async () => {
    const f = await fixture()
    f.setCreate(async () => problem('DEPENDENCY_UNAVAILABLE', 'not_started'))
    await expect(f.auth.agentConfiguration.startCreate(project, fields())).rejects.toMatchObject({
      kind: 'problem',
    })
    expect(f.auth.agentConfiguration.progress?.phase).toBe('rejected')
    f.auth.agentConfiguration.editRejected()
    f.setCreate(null)
    const created = await f.auth.agentConfiguration.startCreate(project, {
      ...fields(),
      name: 'Second-Agent',
    })
    expect(created).toHaveProperty('agent.name', 'Second-Agent')
    const [first, second] = f.creates().map(([, request]) => request)
    expect(JSON.parse(first!.body as string).request.agent_id).not.toBe(
      JSON.parse(second!.body as string).request.agent_id,
    )
    expect((first!.headers as Record<string, string>)['Idempotency-Key']).not.toBe(
      (second!.headers as Record<string, string>)['Idempotency-Key'],
    )
    expect(f.auth.workReview.progress).toBeNull()
    expect(f.lookups()).toHaveLength(0)
  })

  it('retains the actual Cookie owner after visible timeout and rejects late results across Session retirement', async () => {
    const f = await fixture()
    vi.useFakeTimers()
    let release!: () => void
    f.setCreate(
      async (_path, init) =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(receipt(JSON.parse(init.body as string))))
          releases.push(release)
        }),
    )
    const pending = f.auth.agentConfiguration.startCreate(project, fields())
    const rejected = expect(pending).rejects.toMatchObject({ kind: 'cancelled' })
    await vi.advanceTimersByTimeAsync(30_000)
    await rejected
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.agentConfiguration.progress?.phase).toBe('uncertain')
    await expect(f.auth.agentConfiguration.checkOriginal()).rejects.toMatchObject({ kind: 'busy' })
    f.auth.leave()
    expect(f.auth.agentConfiguration.progress).toBeNull()
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.agentConfiguration.progress).toBeNull()
    vi.useRealTimers()
    f.setSession(session(3))
    await f.auth.restore()
    await expect(f.auth.agentConfiguration.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.creates()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(0)
  })
})

describe('Agent update original Session ownership', () => {
  it('keeps the original Agent, version, presence and key through Unknown without mixing Create or resending Update', async () => {
    const f = await fixture()
    f.setUpdate(async () => problem('COMMIT_UNKNOWN', 'unknown'))
    const value = {
      expected_version: '9007199254740993',
      request: { description: '', instructions: ' original\nbytes ' },
    }
    const expected = structuredClone(value)
    const saving = f.auth.agentConfiguration.startUpdate(project, updateAgent, value)
    value.expected_version = '2'
    value.request.instructions = 'later'
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    expect(f.auth.agentConfiguration.progress).toMatchObject({
      kind: 'update',
      phase: 'uncertain',
      agentID: updateAgent,
    })
    await expect(f.auth.agentConfiguration.startCreate(project, fields())).rejects.toMatchObject({
      kind: 'busy',
    })
    await expect(
      f.auth.agentConfiguration.startUpdate(project, id(62), value),
    ).rejects.toMatchObject({ kind: 'busy' })
    const first = f.updates()[0]![1]
    expect(JSON.parse(first.body as string)).toEqual(expected)
    for (const status of ['not_observed', 'in_progress', 'committed'] as const) {
      f.setStatus(status)
      expect(await f.auth.agentConfiguration.checkOriginal()).toMatchObject({ status })
      const lookup = f.lookups().at(-1)![1]
      expect(JSON.parse(lookup.body as string)).toEqual({
        command: 'agent.update',
        target_id: updateAgent,
        ...expected,
      })
      expect(lookup.headers).toMatchObject({
        'Idempotency-Key': (first.headers as Record<string, string>)['Idempotency-Key'],
      })
    }
    expect(f.auth.agentConfiguration.progress).toMatchObject({
      kind: 'update',
      phase: 'confirmed',
      receipt: updated(expected),
    })
    expect(f.updates()).toHaveLength(1)
    expect(f.creates()).toHaveLength(0)
    expect(f.auth.workReview.progress).toBeNull()
  })

  it('holds the update Cookie owner until its actual tail returns and never publishes it into a later Session', async () => {
    const f = await fixture()
    let release!: () => void
    f.setUpdate(
      async (_path, init) =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(updated(JSON.parse(init.body as string))))
          releases.push(release)
        }),
    )
    const saving = f.auth.agentConfiguration.startUpdate(project, updateAgent, {
      expected_version: '1',
      request: { name: 'Changed-Agent' },
    })
    const rejected = expect(saving).rejects.toMatchObject({ kind: 'cancelled' })
    await flushPromises()
    f.auth.leave()
    await rejected
    expect(f.auth.state.busy).toBe(true)
    expect(f.auth.agentConfiguration.progress).toBeNull()
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.setSession(session(3))
    await f.auth.restore()
    await expect(f.auth.agentConfiguration.checkOriginal()).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(f.auth.agentConfiguration.progress).toBeNull()
    expect(f.updates()).toHaveLength(1)
    expect(f.lookups()).toHaveLength(0)
  })
})
