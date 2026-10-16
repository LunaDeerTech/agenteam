import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import {
  createAgentConfigurationAPI,
  type AgentConfiguration,
  type AgentUpdateInput,
  type AgentUpdateReceipt,
} from '../api/agent-configuration'
import {
  createAgentRunnerMountsAPI,
  type AgentRunnerMounts,
  type AvailableRunner,
} from '../api/agent-runner-mounts'
import {
  createProjectModelSettingsAPI,
  type ProjectAvailableChatModel,
} from '../api/project-models'
import { createProjectSecretsAPI, type ProjectSecretMetadata } from '../api/project-secrets'
import type { Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  createProjectAgents,
  projectAgentsKey,
  type ProjectAgents,
} from '../composables/useProjectAgents'
import ProjectAgentsView from '../views/projects/ProjectAgentsView.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  agent = id(2),
  instant = '2026-10-11T10:00:00.000000Z'
const root = `/api/v1/projects/${project}`
const configPath = `${root}/agents/${agent}/configuration`
const mappingPath = `${root}/agents/${agent}/runner-mounts`
const lookupPath = `${root}/agent-commands/lookup`
const initial: AgentConfiguration = {
  id: agent,
  project_id: project,
  name: 'Mount-Agent',
  normalized_name: 'mount-agent',
  display_name: '目录助手',
  tag_color: null,
  description: '原描述',
  instructions: '原指令',
  inject_agents_md: true,
  model_ref: id(3),
  reasoning_effort: null,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [id(5)],
  allowed_secret_variable_ids: [id(20)],
  lifecycle: 'active',
  version: '1',
  created_at: instant,
  updated_at: instant,
}
const initialMounts: AgentRunnerMounts = {
  project_id: project,
  agent_id: agent,
  agent_version: '1',
  items: [{ mount_id: id(5), runner_id: id(4), workspace: 'original.dir', name: 'Legacy alias' }],
}
const runners: readonly AvailableRunner[] = [
  { id: id(4), name: 'Offline worker', description: '离线说明', version: '1', status: 'offline' },
  {
    id: id(8),
    name: 'Protocol worker',
    description: '协议不兼容说明',
    version: '1',
    status: 'incompatible',
  },
]
const model: ProjectAvailableChatModel = {
  id: id(3),
  provider_id: id(7),
  scope: { kind: 'system' },
  name: 'Text',
  provider_name: 'Provider',
  version: '1',
  capabilities: {
    tool_calls: false,
    parallel_tool_calls: false,
    streaming: true,
    reasoning: false,
    input_modalities: ['text'],
    output_modalities: ['text'],
    reasoning_efforts: [],
    structured_output_modes: ['text'],
    context_length: null,
    max_output: null,
  },
}
const session: SessionView = {
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
}
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(90),
    },
  })
function problem(status: number, code: string, commit_state = 'not_started') {
  return json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Unavailable',
      status,
      detail: '',
      instance: configPath,
      code,
      request_id: id(90),
      commit_state,
    },
    status,
  )
}
function summary(a: AgentConfiguration) {
  return {
    id: a.id,
    project_id: a.project_id,
    name: a.name,
    display_name: a.display_name,
    tag_color: a.tag_color,
    description: a.description,
    version: a.version,
    created_at: a.created_at,
    updated_at: a.updated_at,
  }
}
const mounted: VueWrapper[] = [],
  owners: ProjectAgents[] = [],
  sessions: SessionController[] = [],
  releaseReads: (() => void)[] = []
beforeEach(() => {
  // Geometry only; all selects, popovers, fields and focus handlers are real.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  mounted.splice(0).forEach((w) => w.unmount())
  owners.splice(0).forEach((o) => o.dispose())
  sessions.splice(0).forEach((s) => s.leave())
  releaseReads.splice(0).forEach((release) => release())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const secretsPath = `${root}/secret-variables`
const secret = (n: number): ProjectSecretMetadata => ({
  id: id(19 + n),
  project_id: project,
  type: 'secret',
  name: `SECRET_${String(n).padStart(3, '0')}`,
  description: `Safe description ${n}`,
  version: '1',
  created_at: instant,
  updated_at: instant,
})
const secrets = [secret(1), secret(2)]
async function page() {
  let current = { ...initial },
    mappings = structuredClone(initialMounts)
  let patch: Fetch | null = null,
    lookup: Fetch | null = null
  const reads = new Map<string, Fetch>(),
    unexpected: string[] = []
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    const path = String(url).split('?')[0]
    if (path === '/api/v1/session') return json(session)
    if (init.method === 'GET') {
      const read = reads.get(path)
      if (read) return read(url, init)
      if (path === `${root}/agents`) return json({ items: [summary(current)] })
      if (path === `${root}/available-chat-models`)
        return json({ items: [model], next_cursor: null })
      if (path === `${root}/available-runners`) return json({ items: runners })
      if (path === secretsPath) return json({ items: secrets })
      if (path === `${root}/agents/${current.id}/configuration`) return json(current)
      if (path === `${root}/agents/${current.id}/runner-mounts`) return json(mappings)
    }
    if (path === configPath && init.method === 'PATCH') {
      if (patch) return patch(url, init)
      const input = JSON.parse(init.body as string) as AgentUpdateInput
      current = {
        ...current,
        ...input.request,
        version: String(BigInt(input.expected_version) + 1n),
      }
      mappings = { ...mappings, agent_version: current.version }
      return json({ agent: current, changed: true, event_ids: [id(50)] })
    }
    if (path === `${root}/agents` && init.method === 'POST') {
      const { request } = JSON.parse(init.body as string)
      current = {
        ...initial,
        id: request.agent_id,
        name: request.name,
        display_name: request.display_name ?? null,
        tag_color: request.tag_color ?? null,
        normalized_name: request.name.toLowerCase(),
        description: request.description ?? '',
        instructions: request.instructions ?? '',
        allowed_mount_ids: [],
        allowed_tool_ids: request.install_skill_enabled === false ? [] : [id(30)],
        allowed_secret_variable_ids: request.allowed_secret_variable_ids,
        inject_agents_md: request.inject_agents_md,
        version: '1',
      }
      mappings = { project_id: project, agent_id: current.id, agent_version: '1', items: [] }
      return json({ agent: current, changed: true, event_ids: [id(50)], runner_mounts: [] })
    }
    if (path === lookupPath && init.method === 'POST' && lookup) return lookup(url, init)
    unexpected.push(`${init.method} ${path}`)
    throw new Error('Unexpected allowlist page request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[14] = createProjectModelSettingsAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  args[19] = {
    agentConfiguration: createAgentConfigurationAPI(fetcher),
    agentRunnerMounts: createAgentRunnerMountsAPI(fetcher),
    projectSecrets: createProjectSecretsAPI(fetcher),
  }
  const auth = createSessionController(...args)
  sessions.push(auth)
  await auth.restore()
  expect(auth.state.user?.role).toBe('user')
  const context = shallowRef<{
    identity: NonNullable<typeof auth.personalContext.identity>
    projectID: string
    generation: number
    readGeneration: number
  } | null>({
    identity: auth.personalContext.identity!,
    projectID: project,
    generation: 1,
    readGeneration: 1,
  })
  const workspace = {
    currentReadContext: context,
    detail: shallowReactive({ project: { id: project, lifecycle: 'active' } }),
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectAgents(auth, workspace)
  owners.push(owner)
  owner.afterNavigation('/owner/demo/settings/agents')
  const wrapper = mount(ProjectAgentsView, {
    attachTo: document.body,
    global: { provide: { [projectAgentsKey as symbol]: owner } },
  })
  mounted.push(wrapper)
  await flushPromises()
  return {
    auth,
    owner,
    wrapper,
    context,
    unexpected,
    calls: (method: string, path: string) =>
      fetcher.mock.calls.filter(
        ([url, init]) => init.method === method && String(url).split('?')[0] === path,
      ),
    setCurrent: (value: AgentConfiguration) => {
      current = value
      mappings = { ...mappings, agent_id: value.id, agent_version: value.version }
    },
    onRead: (path: string, fn: Fetch | null) => {
      if (fn) reads.set(path, fn)
      else reads.delete(path)
    },
    onPatch: (fn: Fetch) => {
      patch = fn
    },
    onLookup: (fn: Fetch) => {
      lookup = fn
    },
  }
}
const body = () => new DOMWrapper(document.body)
function label(element: Element) {
  const copy = element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim() ?? ''
}
function maybeButton(name: string) {
  return body()
    .findAll<HTMLButtonElement>('button')
    .find((b) => label(b.element) === name)
}
function button(name: string) {
  const b = maybeButton(name)
  expect(b, `missing button ${name}`).toBeDefined()
  return b!
}
async function click(name: string) {
  await button(name).trigger('click')
  await flushPromises()
}
function field(name: string) {
  const l = body()
    .findAll('label[for]')
    .find((item) => label(item.element) === name)
  expect(l, `missing field ${name}`).toBeDefined()
  const element = document.getElementById(l!.attributes('for')!)
  expect(element).toBeInstanceOf(HTMLElement)
  return new DOMWrapper(element as HTMLInputElement | HTMLTextAreaElement)
}
async function choose(name: string, value: string) {
  await click(name)
  const option = body()
    .get(`[role="listbox"][aria-label="${name}"]`)
    .findAll<HTMLButtonElement>('[role="option"]')
    .find((item) => label(item.element) === value)
  expect(option, `missing choice ${value}`).toBeDefined()
  expect(option!.element.disabled).toBe(false)
  await option!.trigger('click')
  await flushPromises()
}
async function edit(f: Awaited<ReturnType<typeof page>>) {
  await f.wrapper.get('.agent-entry').trigger('click')
  await flushPromises()
  await click('编辑配置')
}
function values() {
  return body()
    .findAll<HTMLInputElement>('input')
    .map((input) => input.element.value)
}

const readSecrets = () => click('重新读取 Secret Variable 目录')
async function addSecret(name: string) {
  await choose('添加 Secret Variable', name)
  await click('添加到白名单')
}

describe('Secret allowlist through the real Session, codecs and Agent page', () => {
  it('lets a non-admin create with chosen stable IDs and reads the committed current configuration', async () => {
    const f = await page()
    await click('创建 Agent')
    await field('Agent 名称').setValue('Secret-Agent')
    await choose('主模型', 'Text · Provider（系统）')
    await choose('AGENTS.md 注入', '注入 AGENTS.md')
    await choose('审批策略', '默认审批')
    await readSecrets()
    const trigger = button('添加 Secret Variable')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const list = body().get('[role="listbox"][aria-label="添加 Secret Variable"]')
    const options = list.findAll<HTMLButtonElement>('[role="option"]')
    expect(options.map((o) => o.text())).toEqual(['SECRET_001', 'SECRET_002'])
    await list.trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(options[1]!.element)
    await options[1]!.trigger('click')
    await click('添加到白名单')
    expect(document.activeElement).toBe(trigger.element)
    await addSecret('SECRET_001')
    expect(f.owner.draft.secretIDs).toEqual([id(20), id(21)])
    await click('移除 SECRET_002')
    expect(document.activeElement).toBe(trigger.element)
    expect(button('确认创建 Agent').element.disabled).toBe(false)
    await click('确认创建 Agent')
    const writes = f.calls('POST', `${root}/agents`)
    expect(writes).toHaveLength(1)
    const input = JSON.parse(writes[0]![1].body as string)
    expect(input.request.allowed_secret_variable_ids).toEqual([id(20)])
    expect(f.owner.state.configuration?.allowed_secret_variable_ids).toEqual([id(20)])
    expect(f.calls('GET', `${root}/agents/${input.request.agent_id}/configuration`)).toHaveLength(1)
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.unexpected).toEqual([])
  })

  it('keeps untouched references omitted despite a failed directory and clears only on explicit removal', async () => {
    const f = await page()
    await edit(f)
    expect(f.owner.edit.secretIDs).toEqual([id(20)])
    expect(body().text()).toContain('未在已读取目录中；保留此 ID')
    f.onRead(secretsPath, async () => problem(503, 'DEPENDENCY_UNBOUND'))
    await readSecrets()
    expect(f.owner.edit.secretIDs).toEqual([id(20)])
    await field('Agent 描述').setValue('metadata only')
    await click('保存配置')
    expect(JSON.parse(f.calls('PATCH', configPath)[0]![1].body as string)).toEqual({
      expected_version: '1',
      request: { description: 'metadata only' },
    })
    await click('编辑配置')
    await click(`移除 ${id(20)}`)
    await click('保存配置')
    expect(JSON.parse(f.calls('PATCH', configPath)[1]![1].body as string)).toEqual({
      expected_version: '2',
      request: { allowed_secret_variable_ids: [] },
    })
    expect(f.owner.state.configuration?.allowed_secret_variable_ids).toEqual([])
    expect(f.unexpected).toEqual([])
  })

  it('preserves selected IDs outside the first page and publishes complete ordered pages only', async () => {
    const f = await page()
    f.setCurrent({ ...initial, allowed_secret_variable_ids: [secret(51).id] })
    await edit(f)
    const first = Array.from({ length: 50 }, (_, n) => secret(n + 1))
    let tail: unknown = { ...secret(51), value: 'forbidden-value-marker' }
    f.onRead(secretsPath, async (url) =>
      new URL(String(url), 'http://fixture.test').searchParams.has('cursor')
        ? json({ items: [tail] })
        : json({ items: first, next_cursor: 'opaque-page-2' }),
    )
    await readSecrets()
    expect(f.owner.edit.secretIDs).toEqual([secret(51).id])
    expect(f.owner.editDirty.value).toBe(false)
    await click('读取更多 Secret Variable')
    expect(f.owner.state.secrets).toHaveLength(50)
    expect(f.owner.edit.secretIDs).toEqual([secret(51).id])
    expect(body().text()).not.toContain('forbidden-value-marker')
    tail = secret(51)
    await click('读取更多 Secret Variable')
    expect(f.owner.state.secrets).toHaveLength(51)
    expect(button('移除 SECRET_051')).toBeDefined()
    const last = f.calls('GET', secretsPath).at(-1)!
    expect(new URL(String(last[0]), 'http://fixture.test').searchParams.get('cursor')).toBe(
      'opaque-page-2',
    )
    tail = { ...secret(1), name: 'ZZ_DUPLICATE' }
    await readSecrets()
    await click('读取更多 Secret Variable')
    expect(f.owner.state.secrets).toHaveLength(50)
    expect(f.owner.state.secretNext).toBeUndefined()
    expect(f.owner.edit.secretIDs).toEqual([secret(51).id])
    expect(f.unexpected).toEqual([])
  })

  it('withdraws current metadata and draft on Owner refusal without upgrading the account', async () => {
    const f = await page()
    await edit(f)
    await readSecrets()
    await addSecret('SECRET_002')
    f.onRead(secretsPath, async () => problem(403, 'FORBIDDEN'))
    await readSecrets()
    expect(f.owner.state.secrets).toEqual([])
    expect(f.owner.state.configuration).toBeNull()
    expect(f.owner.edit.secretIDs).toEqual([])
    expect(f.owner.canSave.value).toBe(false)
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.calls('PATCH', configPath)).toHaveLength(0)
    expect(f.unexpected).toEqual([])
  })

  it('retains exact Unknown intent and key, then reads a newer current version after historical Lookup', async () => {
    const f = await page()
    await edit(f)
    await readSecrets()
    await click('移除 SECRET_001')
    await addSecret('SECRET_002')
    f.onPatch(async () => problem(503, 'COMMIT_UNKNOWN', 'unknown'))
    await click('保存配置')
    expect(f.auth.agentConfiguration.progress?.phase).toBe('uncertain')
    const original = f.calls('PATCH', configPath)[0]![1]
    f.onLookup(async () => json({ status: 'not_observed', receipt: null }))
    await click('查询原保存结果')
    expect(f.owner.edit.secretIDs).toEqual([id(21)])
    const historical = { ...initial, allowed_secret_variable_ids: [id(21)], version: '2' }
    f.setCurrent({ ...historical, allowed_secret_variable_ids: [], version: '3' })
    f.onLookup(async () =>
      json({
        status: 'committed',
        receipt: { agent: historical, changed: true, event_ids: [id(50)] },
      }),
    )
    await click('查询原保存结果')
    expect(f.owner.state.configuration?.version).toBe('3')
    expect(f.owner.state.configuration?.allowed_secret_variable_ids).toEqual([])
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(f.calls('POST', lookupPath)).toHaveLength(2)
    for (const [, request] of f.calls('POST', lookupPath)) {
      expect(JSON.parse(request.body as string)).toEqual({
        command: 'agent.update',
        target_id: agent,
        expected_version: '1',
        request: { allowed_secret_variable_ids: [id(21)] },
      })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
    expect(f.calls('GET', configPath)).toHaveLength(2)
    expect(f.unexpected).toEqual([])
  })

  it('keeps the original version and selection on conflict until the user discards and rereads', async () => {
    const f = await page()
    await edit(f)
    await readSecrets()
    await addSecret('SECRET_002')
    f.onPatch(async () => problem(409, 'VERSION_CONFLICT'))
    await click('保存配置')
    expect(f.owner.edit.secretIDs).toEqual([id(20), id(21)])
    expect(f.owner.edit.version).toBe('1')
    expect(f.owner.editStale.value).toBe(true)
    expect(button('保存配置').element.disabled).toBe(true)
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(f.unexpected).toEqual([])
  })

  it('counts retained Tool and Mount references even when their mapping or candidate pages are unavailable', async () => {
    const f = await page()
    f.setCurrent({
      ...initial,
      allowed_secret_variable_ids: [],
      allowed_tool_ids: Array.from({ length: 128 }, (_, n) => id(300 + n)),
      allowed_mount_ids: Array.from({ length: 128 }, (_, n) => id(500 + n)),
    })
    await edit(f)
    expect(f.owner.state.mountsPhase).toBe('error')
    await readSecrets()
    await addSecret('SECRET_001')
    expect(f.owner.edit.secretIDs).toEqual([id(20)])
    expect(f.owner.editSecretMessage.value).toContain('256')
    expect(button('保存配置').element.disabled).toBe(true)
    expect(f.calls('PATCH', configPath)).toHaveLength(0)
    expect(f.unexpected).toEqual([])
  })

  it('unblocks the preserved create selection after checking and a successful explicit catalogue reread', async () => {
    const f = await page()
    await click('创建 Agent')
    await field('Agent 名称').setValue('Secret-Agent')
    await choose('主模型', 'Text · Provider（系统）')
    await choose('AGENTS.md 注入', '注入 AGENTS.md')
    await choose('审批策略', '默认审批')
    await readSecrets()
    await addSecret('SECRET_001')
    expect(f.owner.canSubmit.value).toBe(true)
    const previous = f.context.value!
    // Workspace explicitly withdraws its checked context while the same
    // Session identity remains present; the unsubmitted form is preserved.
    f.context.value = null
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.draft.secretIDs).toEqual([id(20)])
    f.context.value = { ...previous, readGeneration: previous.readGeneration + 1 }
    await flushPromises()
    f.owner.changeMounts(false, [{ runner_id: id(4), workspace: 'after-check' }])
    await flushPromises()
    expect(f.owner.draftSecretMessage.value).not.toBe('')
    expect(f.owner.canSubmit.value).toBe(false)
    await readSecrets()
    expect(f.owner.state.secretPhase).toBe('ready')
    expect(f.owner.draft.secretIDs).toEqual([id(20)])
    expect(f.owner.draftSecretMessage.value).toBe('')
    expect(f.owner.canSubmit.value).toBe(true)
    expect(f.calls('POST', `${root}/agents`)).toHaveLength(0)
    expect(f.unexpected).toEqual([])
  })

  it('does not release the shared Session or publish a retired page before actual body cancellation returns', async () => {
    const f = await page()
    await edit(f)
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    releaseReads.push(release)
    const cancel = vi.fn(() => held)
    f.onRead(
      secretsPath,
      async () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(new TextEncoder().encode(JSON.stringify({ items: secrets })))
            },
            cancel,
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    await click('重新读取 Secret Variable 目录')
    expect(f.owner.busy.value).toBe(true)
    f.owner.afterNavigation('/owner/demo/settings')
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(f.auth.state.busy).toBe(true)
    f.owner.afterNavigation('/owner/demo/settings/agents')
    const readsBefore = f.calls('GET', `${root}/agents`).length
    await flushPromises()
    expect(f.calls('GET', `${root}/agents`)).toHaveLength(readsBefore)
    expect(f.owner.state.secrets).toEqual([])
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.calls('GET', `${root}/agents`)).toHaveLength(readsBefore + 1)
    expect(f.owner.state.secrets).toEqual([])
    expect(f.unexpected).toEqual([])
  })
})
