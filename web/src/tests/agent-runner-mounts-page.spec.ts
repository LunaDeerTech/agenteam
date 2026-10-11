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
  allowed_secret_variable_ids: [],
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

async function page() {
  // A controlled HTTP boundary supplies known JSON. Real Session ownership,
  // codecs and page/controller execute together; this is not a PG/Account or
  // native browser authorization proof. Project context is explicit and local.
  let current = { ...initial },
    mappings = structuredClone(initialMounts)
  let mappingFailure: 'missing' | 'denied' | null = null
  let patch: Fetch | null = null,
    lookup: Fetch | null = null
  const reads = new Map<string, Fetch>()
  const unexpected: string[] = []
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
      if (path === configPath) return json(current)
      if (path === mappingPath)
        return mappingFailure
          ? problem(
              mappingFailure === 'denied' ? 404 : 503,
              mappingFailure === 'denied' ? 'NOT_FOUND' : 'DEPENDENCY_UNBOUND',
            )
          : json(mappings)
    }
    if (path === configPath && init.method === 'PATCH') {
      if (patch) return patch(url, init)
      const input = JSON.parse(init.body as string) as AgentUpdateInput
      const declarations = input.request.runner_mounts
      // This small response fixture supports the two explicit page operations:
      // description-only and clearing all mappings. Other writes are rejected.
      if (
        Object.keys(input.request).length !== 1 ||
        (declarations === undefined && input.request.description === undefined) ||
        (declarations !== undefined && declarations.length !== 0)
      ) {
        unexpected.push('unexpected-patch')
        throw new Error('Unexpected page write')
      }
      current = {
        ...current,
        description: input.request.description ?? current.description,
        allowed_mount_ids: declarations === undefined ? current.allowed_mount_ids : [],
        version: String(BigInt(current.version) + 1n),
      }
      mappings = {
        ...mappings,
        agent_version: current.version,
        items: declarations === undefined ? mappings.items : [],
      }
      const receipt: AgentUpdateReceipt = {
        agent: current,
        changed: true,
        event_ids: [id(50)],
        ...(declarations === undefined ? {} : { runner_mounts: [] }),
      }
      return json(receipt)
    }
    if (path === lookupPath && init.method === 'POST' && lookup) return lookup(url, init)
    unexpected.push(`${init.method} ${path}`)
    throw new Error('Unexpected page HTTP request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[14] = createProjectModelSettingsAPI(fetcher)
  args[17] = createAgentDirectoryAPI(fetcher)
  args[19] = {
    agentConfiguration: createAgentConfigurationAPI(fetcher),
    agentRunnerMounts: createAgentRunnerMountsAPI(fetcher),
  }
  const auth = createSessionController(...args)
  sessions.push(auth)
  await auth.restore()
  expect(auth.state.user?.role).toBe('user')
  const context = shallowRef({
    identity: auth.personalContext.identity!,
    projectID: project,
    generation: 1,
    readGeneration: 1,
  })
  const detail = shallowReactive({ project: { id: project, lifecycle: 'active' } })
  const workspace = {
    currentReadContext: context,
    detail,
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
    detail,
    unexpected,
    calls: (method: string, path: string) =>
      fetcher.mock.calls.filter(
        ([url, init]) => init.method === method && String(url).split('?')[0] === path,
      ),
    writes: () => fetcher.mock.calls.filter(([, init]) => init.method !== 'GET'),
    setMappingFailure: (value: typeof mappingFailure) => {
      mappingFailure = value
    },
    setMappingVersion: (value: string) => {
      mappings = { ...mappings, agent_version: value }
    },
    setCurrent: (value: AgentConfiguration, valueMounts: AgentRunnerMounts) => {
      current = value
      mappings = valueMounts
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

describe('Agent Runner page using real controls and Session codecs', () => {
  it('lets an ordinary project Owner select offline and incompatible Runners with exact directory bytes and add/remove focus', async () => {
    const f = await page()
    expect(button('创建 Agent').element.disabled).toBe(false)
    await click('创建 Agent')
    await field('Agent 名称').setValue('New-Agent')
    await choose('主模型', 'Text · Provider（系统）')
    await choose('AGENTS.md 注入', '注入 AGENTS.md')
    await choose('审批策略', '默认审批')
    await click('添加资源目录')
    const trigger = button('Runner 1')
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const list = body().get('[role="listbox"][aria-label="Runner 1"]')
    const options = list.findAll<HTMLButtonElement>('[role="option"]')
    expect(options.map((item) => [label(item.element), item.element.disabled])).toEqual([
      ['Offline worker（离线）', false],
      ['Protocol worker（协议不兼容）', false],
    ])
    await list.trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(options[1]!.element)
    await options[1]!.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(trigger.element)
    expect(body().text()).toContain('协议不兼容说明')
    const directory = 'W' + 'a'.repeat(254)
    await field('资源目录名 1').setValue(directory)
    expect(field('资源目录名 1').element.value).toBe(directory)
    await click('添加资源目录')
    expect(document.activeElement).toBe(button('Runner 2').element)
    await choose('Runner 2', 'Offline worker（离线）')
    await field('资源目录名 2').setValue('offline.dir')
    expect(button('确认创建 Agent').element.disabled).toBe(false)
    await click('移除资源目录 1')
    expect(document.activeElement).toBe(button('添加资源目录').element)
    expect(field('资源目录名 1').element.value).toBe('offline.dir')
    expect(button('Runner 1').text()).toContain('Offline worker（离线）')
    await click('移除资源目录 1')
    expect(document.activeElement).toBe(button('添加资源目录').element)
    expect(body().text()).toContain('未配置 Runner。')
    expect(f.writes()).toHaveLength(0)
    f.detail.project = { id: project, lifecycle: 'archived' }
    await flushPromises()
    expect(button('添加资源目录').element.disabled).toBe(true)
    expect(button('确认创建 Agent').element.disabled).toBe(true)
    expect(f.unexpected).toEqual([])
  })

  it('omits untouched mounts from a metadata edit and sends an explicit empty array only after removing the row', async () => {
    const f = await page()
    await edit(f)
    expect(field('资源目录名 1').element.value).toBe('original.dir')
    await field('Agent 描述').setValue('Edited metadata only')
    await click('保存配置')
    const metadata = f.calls('PATCH', configPath)[0]![1]
    expect(JSON.parse(metadata.body as string)).toEqual({
      expected_version: '1',
      request: { description: 'Edited metadata only' },
    })
    expect(f.calls('GET', configPath)).toHaveLength(2)
    expect(f.calls('GET', mappingPath)).toHaveLength(2)
    expect(body().get('[aria-label="当前 Runner 配置"]').text()).toContain('original.dir')
    await click('编辑配置')
    await click('移除资源目录 1')
    expect(document.activeElement).toBe(button('添加资源目录').element)
    await click('保存配置')
    const cleared = f.calls('PATCH', configPath)[1]![1]
    expect(JSON.parse(cleared.body as string)).toEqual({
      expected_version: '2',
      request: { runner_mounts: [] },
    })
    expect(f.calls('GET', configPath)).toHaveLength(3)
    expect(f.calls('GET', mappingPath)).toHaveLength(3)
    expect(body().get('[aria-label="当前 Runner 配置"]').text()).toContain('未配置 Runner。')
    expect(f.unexpected).toEqual([])
  })

  it.each(['version-mismatch', 'legacy-unmapped'] as const)(
    'blocks mount editing for %s until an explicit complete current reread',
    async (mode) => {
      const f = await page()
      if (mode === 'version-mismatch') f.setMappingVersion('2')
      else f.setMappingFailure('missing')
      await edit(f)
      const section = body().get('[aria-label="Runner 与资源目录"]')
      expect(section.text()).toContain(
        mode === 'version-mismatch' ? '配置版本或挂载引用已变化' : '未能完整读取当前 Runner 配置',
      )
      expect(section.text()).not.toContain('未配置 Runner。')
      expect(maybeButton('添加资源目录')).toBeUndefined()
      expect(values()).not.toContain('original.dir')
      await field('Agent 描述').setValue('Keep this draft')
      expect(button('保存配置').element.disabled).toBe(false)
      f.setMappingVersion('1')
      f.setMappingFailure(null)
      await click('重新读取 Runner 配置')
      expect(field('资源目录名 1').element.value).toBe('original.dir')
      expect(field('Agent 描述').element.value).toBe('Keep this draft')
      await click('保存配置')
      expect(JSON.parse(f.calls('PATCH', configPath)[0]![1].body as string)).toEqual({
        expected_version: '1',
        request: { description: 'Keep this draft' },
      })
      expect(f.calls('GET', mappingPath)).toHaveLength(3)
      expect(f.unexpected).toEqual([])
    },
  )

  it('keeps the original Unknown PATCH key and declaration for Lookup, then renders separately fetched current mappings', async () => {
    const f = await page()
    await edit(f)
    f.onPatch(async () => problem(503, 'COMMIT_UNKNOWN', 'unknown'))
    const historical: AgentUpdateReceipt = {
      agent: { ...initial, version: '2', allowed_mount_ids: [id(6)] },
      changed: true,
      event_ids: [id(51)],
      runner_mounts: [
        { runner_id: id(4), workspace: 'original.edit', mount_id: id(6), workspace_id: id(12) },
      ],
    }
    f.onLookup(async () => json({ status: 'committed', receipt: historical }))
    await field('资源目录名 1').setValue('original.edit')
    await click('保存配置')
    expect(body().get('[aria-label="Agent 保存结果恢复"]').text()).toContain('保存结果待确认')
    expect(maybeButton('保存配置')).toBeUndefined()
    expect(maybeButton('添加资源目录')).toBeUndefined()
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    const original = f.calls('PATCH', configPath)[0]![1]
    const originalBody = {
      expected_version: '1',
      request: { runner_mounts: [{ runner_id: id(4), workspace: 'original.edit' }] },
    }
    expect(JSON.parse(original.body as string)).toEqual(originalBody)
    f.setCurrent(
      { ...initial, version: '3', allowed_mount_ids: [id(13)] },
      {
        project_id: project,
        agent_id: agent,
        agent_version: '3',
        items: [{ mount_id: id(13), runner_id: id(8), workspace: 'current.dir', name: id(13) }],
      },
    )
    await click('查询原保存结果')
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(f.calls('POST', lookupPath)).toHaveLength(1)
    const lookup = f.calls('POST', lookupPath)[0]![1]
    expect(JSON.parse(lookup.body as string)).toEqual({
      command: 'agent.update',
      target_id: agent,
      ...originalBody,
    })
    const originalHeaders = new Headers(original.headers),
      lookupHeaders = new Headers(lookup.headers)
    expect(originalHeaders.get('Idempotency-Key')).toBeTruthy()
    expect(lookupHeaders.get('Idempotency-Key')).toBe(originalHeaders.get('Idempotency-Key'))
    expect(lookupHeaders.get('X-CSRF-Token')).toBe(session.csrf_token)
    expect(f.calls('GET', configPath)).toHaveLength(2)
    expect(f.calls('GET', mappingPath)).toHaveLength(2)
    const shown = body().get('[aria-label="当前 Runner 配置"]')
    expect(shown.text()).toContain('current.dir')
    expect(shown.text()).not.toContain('original.edit')
    expect(body().text()).not.toContain(originalHeaders.get('Idempotency-Key')!)
    await click('编辑配置')
    expect(field('资源目录名 1').element.value).toBe('current.dir')
    expect(body().get('form[aria-label="编辑 Agent 配置"]').text()).toContain('基于版本 3')
    expect(f.unexpected).toEqual([])
  })

  it('withdraws denied PATCH configuration and its private draft without another save', async () => {
    const f = await page()
    await edit(f)
    await field('资源目录名 1').setValue('private.denied-draft')
    f.onPatch(async () => problem(403, 'FORBIDDEN'))
    await click('保存配置')
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(body().find('[aria-label="当前 Runner 配置"]').exists()).toBe(false)
    expect(values()).not.toContain('private.denied-draft')
    expect(maybeButton('保存配置')).toBeUndefined()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.writes()).toHaveLength(1)
    expect(f.unexpected).toEqual([])
  })

  it('withdraws denied Lookup observations while retaining the original Unknown command and key', async () => {
    const f = await page()
    await edit(f)
    await field('资源目录名 1').setValue('private.unknown-draft')
    f.onPatch(async () => problem(503, 'COMMIT_UNKNOWN', 'unknown'))
    await click('保存配置')
    const original = f.calls('PATCH', configPath)[0]![1]
    f.onLookup(async () => problem(404, 'NOT_FOUND'))
    await click('查询原保存结果')
    expect(body().find('[aria-label="当前 Runner 配置"]').exists()).toBe(false)
    expect(values()).not.toContain('private.unknown-draft')
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(maybeButton('保存配置')).toBeUndefined()
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(f.auth.agentConfiguration.progress).toMatchObject({
      phase: 'uncertain',
      contextValid: true,
    })
    // Denial withdraws observations; it must not invent a new command or lose
    // the held original intent. A later explicit query keeps this same key.
    f.onLookup(async () => json({ status: 'not_observed', receipt: null }))
    await click('查询原保存结果')
    const lookups = f.calls('POST', lookupPath)
    expect(lookups).toHaveLength(2)
    for (const [, request] of lookups) {
      expect(JSON.parse(request.body as string)).toEqual({
        command: 'agent.update',
        target_id: agent,
        expected_version: '1',
        request: { runner_mounts: [{ runner_id: id(4), workspace: 'private.unknown-draft' }] },
      })
      expect(new Headers(request.headers).get('Idempotency-Key')).toBe(
        new Headers(original.headers).get('Idempotency-Key'),
      )
    }
    expect(f.calls('PATCH', configPath)).toHaveLength(1)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.unexpected).toEqual([])
  })

  it('withdraws the open configuration after a current entity refusal without granting administrator access', async () => {
    const f = await page()
    await edit(f)
    await field('资源目录名 1').setValue('private.draft')
    f.setMappingFailure('denied')
    // The explicit page refresh invokes the same guarded read used by retry.
    // It is a public controller action; no internal state is overwritten.
    await f.owner.loadConfiguration(agent)
    await flushPromises()
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(values()).not.toContain('private.draft')
    expect(body().find('[aria-label="当前 Runner 配置"]').exists()).toBe(false)
    expect(maybeButton('保存配置')).toBeUndefined()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.writes()).toHaveLength(0)
    expect(f.unexpected).toEqual([])
  })
})

function heldRead(response: () => Response) {
  let release!: (value: Response) => void
  const promise = new Promise<Response>((resolve) => {
    release = resolve
  })
  releaseReads.push(() => release(response()))
  return { promise, release: (value = response()) => release(value) }
}
async function initializingForm(mode: 'create' | 'edit') {
  const f = await page()
  if (mode === 'edit') {
    await f.wrapper.get('.agent-entry').trigger('click')
    await flushPromises()
  }
  const models = heldRead(() => json({ items: [model], next_cursor: null }))
  const candidates = heldRead(() => json({ items: runners }))
  f.onRead(`${root}/available-chat-models`, async () => models.promise)
  f.onRead(`${root}/available-runners`, async () => candidates.promise)
  await click(mode === 'create' ? '创建 Agent' : '编辑配置')
  const name = field('Agent 名称').element
  const focus = vi.fn()
  name.addEventListener('focus', focus)
  expect(name.disabled).toBe(true)
  expect(focus).not.toHaveBeenCalled()
  return { ...f, models, candidates, name, focus }
}

describe('form initialization focus across actual sequential reads', () => {
  it.each(['create', 'edit'] as const)(
    'focuses %s only after both reads return and does not reclaim focus on manual refresh',
    async (mode) => {
      const f = await initializingForm(mode)
      f.models.release()
      await flushPromises()
      expect(f.calls('GET', `${root}/available-runners`)).toHaveLength(1)
      expect(f.name.disabled).toBe(true)
      // A real focus event in this gap is premature even though jsdom does not
      // reproduce Chromium's automatic blur when the second read disables it.
      expect(f.focus).not.toHaveBeenCalled()
      f.candidates.release()
      await flushPromises()
      expect(f.name.disabled).toBe(false)
      expect(document.activeElement).toBe(f.name)
      expect(f.focus).toHaveBeenCalledTimes(1)
      const outside = document.createElement('button')
      outside.textContent = 'outside current form'
      document.body.append(outside)
      outside.focus()
      f.onRead(`${root}/available-chat-models`, null)
      f.onRead(`${root}/available-runners`, null)
      await f.owner.loadModels()
      await f.owner.loadRunners()
      await flushPromises()
      expect(document.activeElement).toBe(outside)
      expect(f.focus).toHaveBeenCalledTimes(1)
    },
  )
  it.each([
    { mode: 'create', end: 'leave' },
    { mode: 'edit', end: 'denied' },
  ] as const)('does not focus a retired $mode form after $end', async ({ mode, end }) => {
    const f = await initializingForm(mode)
    f.models.release()
    await flushPromises()
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    if (end === 'leave') f.owner.afterNavigation('/owner/demo/settings')
    f.candidates.release(end === 'denied' ? problem(403, 'FORBIDDEN') : json({ items: runners }))
    await flushPromises()
    expect(f.name.isConnected).toBe(false)
    expect(f.focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(outside)
    expect(f.owner.canSave.value).toBe(false)
    if (end === 'denied') expect(f.owner.canCreate.value).toBe(false)
    expect(f.writes()).toEqual([])
  })
})
