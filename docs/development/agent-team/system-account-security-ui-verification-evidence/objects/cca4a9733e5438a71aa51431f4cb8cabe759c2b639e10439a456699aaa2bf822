import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  createMemoryHistory,
  createRouter,
  isNavigationFailure,
  NavigationFailureType,
} from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import {
  createSystemProviderAPI,
  type Provider,
  type ProviderProtocol,
} from '../api/system-providers'
import {
  createSystemModelAPI,
  modelTypeForProtocol,
  type SystemModel,
  type ModelInput,
  type ModelDeletionImpact,
} from '../api/system-models'
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemInvitationsView from '../views/system/SystemInvitationsView.vue'
import SystemProvidersView from '../views/system/SystemProvidersView.vue'
import SystemModelsView from '../views/system/SystemModelsView.vue'
import { useTheme } from '../composables/useTheme'
const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const user = {
  id: id(1),
  email: 'admin@example.com',
  username: 'admin',
  display_name: 'Admin',
  role: 'admin' as const,
  theme: 'dark' as const,
  version: '1',
  initial_password_suggestion: false,
}
const sessionView = (): SessionView => ({
  user,
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const provider = (n = 100, protocol: ProviderProtocol = 'openai-chat-completions'): Provider => ({
  id: id(n),
  input: {
    name: 'Provider ' + n,
    protocol,
    base_url: 'https://provider.example/v1',
    enabled: true,
    credential_ref: null,
    options: {},
  },
  version: '1',
  created_at: time,
  updated_at: time,
})
function input(protocol: ProviderProtocol = 'openai-chat-completions'): ModelInput {
  const type = modelTypeForProtocol(protocol)
  return {
    name: 'Original Model',
    provider_model_id: ' native ',
    type,
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      input_modalities: ['text'],
      output_modalities:
        type === 'chat'
          ? ['text']
          : type === 'embedding'
            ? ['vector']
            : type === 'image_generation'
              ? ['image']
              : [],
      reasoning_efforts: [],
      structured_output_modes: type === 'chat' ? ['text'] : [],
      context_length: null,
      max_output: null,
    },
  }
}
const model = (n = 200, source = provider()): SystemModel => ({
  id: id(n),
  provider_id: source.id,
  input: input(source.input.protocol),
  version: '1',
  created_at: time,
  updated_at: time,
})
const preview = (): ModelDeletionImpact => ({
  model_id: id(200),
  version: '1',
  reference_count: '0',
  reference_groups: [],
  replacement_requirement: 'none',
  delete_blocker: null,
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problem = (code: string, status = 503) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      status,
      code,
      detail: 'untrusted detail',
      instance: '/api/v1/system/models',
      request_id: id(9),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      media,
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  // jsdom state/DOM evidence only; actual native focus and hit testing remain browser checks.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  selected.auth?.leave()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})
async function page(
  options: {
    path?: string
    role?: 'admin' | 'user'
    anonymous?: boolean
    protocol?: ProviderProtocol
  } = {},
) {
  let current: SessionView = { ...sessionView(), user: { ...user, role: options.role ?? 'admin' } },
    signedIn = !options.anonymous
  const sources = new Map<string, Provider>([[id(100), provider(100, options.protocol)]])
  const records = new Map<string, SystemModel>([[id(200), model(200, sources.get(id(100))!)]])
  let session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
  let listProviders: Fetch = async () =>
    json({
      items: [...sources.values()].sort((a, b) => b.id.localeCompare(a.id)),
      next_cursor: null,
    })
  let getProvider: Fetch = async (p) => json(sources.get(p.split('/').at(-1)!))
  let listModels: Fetch = async (p) =>
    json({
      items: [...records.values()]
        .filter(
          (r) =>
            r.provider_id === new URL(p, 'http://test.invalid').searchParams.get('provider_id'),
        )
        .sort((a, b) => b.id.localeCompare(a.id)),
      next_cursor: null,
    })
  let getModel: Fetch = async (p) =>
    records.has(p.split('/').at(-1)!)
      ? json(records.get(p.split('/').at(-1)!))
      : problem('NOT_FOUND', 404)
  let impact: Fetch = async () => json(preview())
  let lookup: Fetch = async () => json({ found: false, receipt: null })
  let mutation: Fetch = async (p, init) => {
    const body = JSON.parse(init.body as string),
      creating = init.method === 'POST'
    const target = creating ? id(201) : p.split('/').at(-1)!,
      version = creating ? '1' : String(BigInt(body.expected_version) + 1n)
    if (init.method === 'DELETE') records.delete(target)
    else
      records.set(target, {
        id: target,
        provider_id: creating ? body.provider_id : records.get(target)!.provider_id,
        input: body.input,
        version,
        created_at: time,
        updated_at: time,
      })
    return json({
      kind: creating ? 'model.create' : init.method === 'PUT' ? 'model.update' : 'model.delete',
      resource_id: target,
      version,
      affected_references: init.method === 'DELETE' ? '10001' : '0',
    })
  }
  const fetch = vi.fn<Fetch>(async (p, init) => {
    if (p === '/api/v1/session') return session(p, init)
    if (p.startsWith('/api/v1/system/model-providers?')) return listProviders(p, init)
    if (p.startsWith('/api/v1/system/model-providers/') && init.method === 'GET')
      return getProvider(p, init)
    if (p.startsWith('/api/v1/system/models?')) return listModels(p, init)
    if (p.endsWith('/deletion-impact')) return impact(p, init)
    if (p.startsWith('/api/v1/system/models/') && init.method === 'GET') return getModel(p, init)
    if (p.endsWith('/model-commands/lookup')) return lookup(p, init)
    if (p.startsWith('/api/v1/system/models')) return mutation(p, init)
    if (p.startsWith('/api/v1/system/users?') || p.startsWith('/api/v1/system/invitations?'))
      return json({ items: [] })
    if (p.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (p.endsWith('/login')) {
      signedIn = true
      return json({ user: current.user, session: current.session, next_path: '/' })
    }
    if (p.endsWith('/logout')) {
      signedIn = false
      return new Response(null, { status: 204 })
    }
    throw new Error('Unexpected fixture route')
  })
  selected.auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
  )
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView, meta: { authentication: true, protected: true } },
      { path: '/login', name: 'login', component: LoginView, meta: { authentication: true } },
      {
        path: '/settings/profile',
        component: { template: '<h1>个人资料</h1>' },
        meta: { authentication: true, protected: true },
      },
      {
        path: '/system',
        component: SystemSettingsView,
        redirect: '/system/users',
        meta: {
          authentication: true,
          protected: true,
          systemAdmin: true,
          navigation: { label: '系统设置', order: 20 },
        },
        children: [
          { path: 'users', component: SystemUsersView },
          { path: 'invitations', component: SystemInvitationsView },
          { path: 'providers', component: SystemProvidersView },
          { path: 'models', component: SystemModelsView },
        ],
      },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(options.path ?? '/system/models')
  await router.isReady()
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  wrapper = mount(App, { attachTo: host, global: { plugins: [router] } })
  await flushPromises()
  return {
    wrapper,
    router,
    auth: selected.auth,
    fetch,
    sources,
    records,
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    resetSession() {
      session = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
    },
    setListProviders(value: Fetch) {
      listProviders = value
    },
    setGetProvider(value: Fetch) {
      getProvider = value
    },
    setListModels(value: Fetch) {
      listModels = value
    },
    setGetModel(value: Fetch) {
      getModel = value
    },
    setImpact(value: Fetch) {
      impact = value
    },
    setMutation(value: Fetch) {
      mutation = value
    },
    setLookup(value: Fetch) {
      lookup = value
    },
    writes() {
      return fetch.mock.calls.filter(
        ([p, init]) => p.startsWith('/api/v1/system/models') && init.method !== 'GET',
      )
    },
  }
}
function button(label: string, root: ParentNode = document) {
  const found = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) =>
      (
        b.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? b.textContent
      )?.trim() === label || b.getAttribute('aria-label') === label,
  )
  expect(found, `button ${label}`).toBeTruthy()
  return found!
}
async function click(label: string, root: ParentNode = document) {
  button(label, root).click()
  await flushPromises()
}
function dialog() {
  const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  expect(layers.length).toBeGreaterThan(0)
  return layers.at(-1)!
}
function field(label: string) {
  const found = [...dialog().querySelectorAll<HTMLLabelElement>('label')].find(
    (n) => n.textContent?.replace(' *', '').trim() === label,
  )
  expect(found, `field ${label}`).toBeTruthy()
  return document.getElementById(found!.htmlFor) as HTMLInputElement
}
async function set(label: string, value: string) {
  const control = field(label)
  control.value = value
  control.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function check(legend: string, label: string) {
  const root = [...dialog().querySelectorAll('fieldset')].find(
    (n) => n.querySelector('legend')?.textContent === legend,
  )
  expect(root).toBeTruthy()
  const checkbox = [...root!.querySelectorAll('label')]
    .find((n) => n.textContent === label)
    ?.querySelector('input')
  expect(checkbox).toBeTruthy()
  checkbox!.click()
  await flushPromises()
}
async function selectProvider() {
  await click('选择 Provider')
}
async function selectModel() {
  await click('查看 Model')
}
async function fill(protocol: ProviderProtocol = 'openai-chat-completions') {
  const type = modelTypeForProtocol(protocol)
  await set('名称', ' Draft Model ')
  await set('原生 Model ID', ' native exact ')
  await click('启用状态', dialog())
  await click('启用')
  await check('输入模态', 'text')
  if (type !== 'reranker')
    await check(
      '输出模态',
      type === 'embedding' ? 'vector' : type === 'image_generation' ? 'image' : 'text',
    )
  if (type === 'chat') await check('结构化输出声明', 'text')
}
async function dirty() {
  await selectProvider()
  await click('创建 Model')
  await fill()
}
function heading(title: string) {
  return [...document.querySelectorAll('h3')].find(
    (n) =>
      [...n.childNodes]
        .filter((c) => !(c instanceof HTMLElement && c.getAttribute('aria-hidden') === 'true'))
        .map((c) => c.textContent)
        .join('')
        .trim() === title,
  )
}
function confirmationRestored() {
  const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  expect(layers).toHaveLength(2)
  const [business, confirm] = layers
  expect(business!.inert).toBe(true)
  expect(confirm!.inert).toBe(false)
  expect(confirm!.getAttribute('aria-hidden')).toBeNull()
  expect(confirm!.textContent).toContain('放弃未保存修改')
  expect(
    business!.querySelector<HTMLInputElement>('.model-editor input:not([readonly])')!.value,
  ).toBe(' Draft Model ')
  return { business: business!, confirm: confirm! }
}

describe('Model actual App/router/form composition', () => {
  it('keeps the users default and five leaves with the exact Model safe return', async () => {
    const f = await page({ path: '/system' })
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(
      f.wrapper
        .get('nav[aria-label="系统设置"]')
        .findAll('a')
        .map((a) => a.text()),
    ).toEqual(['用户', '待注册邀请', 'Providers', 'Models', '平台模型用途', '账号安全'])
    expect(f.wrapper.findAll('.settings-group-toggle')).toHaveLength(3)
    await f.wrapper.get('a[href="/system/models"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('Models')
    expect(safeReturnTarget('/system/models')).toBe('/system/models')
    for (const bad of [
      '/system/models?x=1',
      '/system/models#x',
      '//evil/system/models',
      ['/system/models'],
      '/system/models/',
    ])
      expect(safeReturnTarget(bad)).toBe('/')
    const { router } = await import('../router/index')
    expect(router.resolve('/system/models').meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
  })
  it('shows no management leaf or requests to an ordinary user', async () => {
    const f = await page({ role: 'user' })
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(f.wrapper.find('nav[aria-label="系统设置"]').exists()).toBe(false)
    expect(f.fetch.mock.calls.filter(([p]) => p.startsWith('/api/v1/system/'))).toHaveLength(0)
  })
  it('logs in through the existing form and exact Model return target', async () => {
    const f = await page({ anonymous: true })
    expect(f.router.currentRoute.value.query.return).toBe('/system/models')
    await f.wrapper.get('#login-email').setValue(user.email)
    await f.wrapper.get('#login-password').setValue('Original password 123')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/system/models')
    expect(f.wrapper.get('h1').text()).toBe('Models')
  })
  it.each([
    'openai-chat-completions',
    'anthropic-messages',
    'openai-embeddings',
    'jina-rerank',
    'openai-images-generations',
  ] as const)(
    'submits the current legal complete %s shape without normalizing ordinary fields',
    async (protocol) => {
      const f = await page({ protocol })
      await selectProvider()
      await click('创建 Model')
      await fill(protocol)
      expect(field('Provider').readOnly).toBe(true)
      expect(field('类型').value).toBe(modelTypeForProtocol(protocol))
      expect(field('类型').readOnly).toBe(true)
      await set('上下文容量', '9223372036854775807')
      await set('最大输出容量', '17')
      await click('保存 Model 配置', dialog())
      expect(f.writes()).toHaveLength(1)
      const [path, options] = f.writes()[0]!
      expect(path).toBe('/api/v1/system/models')
      expect(options.method).toBe('POST')
      const body = JSON.parse(options.body as string)
      expect(body.provider_id).toBe(id(100))
      expect(body.input).toEqual({
        ...input(protocol),
        name: ' Draft Model ',
        provider_model_id: ' native exact ',
        capabilities: {
          ...input(protocol).capabilities,
          context_length: '9223372036854775807',
          max_output: '17',
        },
      })
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.wrapper.text()).toContain('Model配置已保存')
      expect(f.wrapper.text()).toContain('不会调用或验证外部模型服务')
      expect(f.fetch.mock.calls.some(([p]) => p.includes('credential'))).toBe(false)
    },
  )
  it.each(['parallel', 'capacity', 'unicode', 'native-bytes', 'unset-enabled'] as const)(
    'retains rejected %s fields, dispatches zero writes and restores the submitter',
    async (invalid) => {
      const f = await page()
      await selectProvider()
      await click('创建 Model')
      if (invalid === 'unset-enabled') {
        await set('名称', 'Name')
        await set('原生 Model ID', 'ID')
      } else await fill()
      if (invalid === 'parallel') await check('能力声明', '并行工具调用（需要工具调用）')
      if (invalid === 'capacity') {
        await set('上下文容量', '1')
        await set('最大输出容量', '2')
      }
      if (invalid === 'unicode') await set('名称', 'bad\u0000name')
      if (invalid === 'native-bytes') await set('原生 Model ID', '界'.repeat(86))
      const submit = button('保存 Model 配置', dialog())
      submit.focus()
      submit.click()
      await flushPromises()
      expect(f.writes()).toHaveLength(0)
      expect(dialog().textContent).toContain('输入不会自动改写')
      expect(document.activeElement).toBe(submit)
      if (invalid === 'native-bytes') expect(field('原生 Model ID').value).toBe('界'.repeat(86))
    },
  )
  it('keeps confirmed receipt separate from a failed refresh and opens a new form with an ordinary submit label immediately', async () => {
    const f = await page()
    await dirty()
    f.setListModels(async () => problem('DEPENDENCY_UNAVAILABLE'))
    await click('保存 Model 配置', dialog())
    expect(f.writes()).toHaveLength(1)
    expect(heading('操作已确认，当前配置读取失败')).toBeTruthy()
    expect(f.auth.system.models.progress?.phase).toBe('confirmed')
    await click('创建 Model')
    expect(field('名称').value).toBe('')
    const save = button('保存 Model 配置', dialog())
    expect(save.getAttribute('aria-label')).not.toBe('已完成')
    expect(f.writes()).toHaveLength(1)
  })
  it('requires explicit current-version review after a real structured conflict and preserves the ordinary draft', async () => {
    const f = await page()
    await selectProvider()
    await selectModel()
    await click('编辑 Model')
    await set('名称', ' Edited ')
    f.setMutation(async () => problem('VERSION_CONFLICT', 409))
    await click('保存 Model 配置', dialog())
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string).expected_version).toBe('1')
    expect(button('保存 Model 配置', dialog()).disabled).toBe(true)
    expect(dialog().textContent).toContain('当前版本已变化')
    f.records.set(id(200), { ...model(), version: '2', input: { ...input(), name: 'Concurrent' } })
    await click('读取当前版本并核对', dialog())
    expect(field('名称').value).toBe(' Edited ')
    expect(f.writes()).toHaveLength(1)
    f.setMutation(async (_p, init) =>
      json({
        kind: 'model.update',
        resource_id: id(200),
        version: String(BigInt(JSON.parse(init.body as string).expected_version) + 1n),
        affected_references: '0',
      }),
    )
    await click('保存 Model 配置', dialog())
    expect(f.writes()).toHaveLength(2)
    expect(JSON.parse(f.writes()[1]![1].body as string)).toMatchObject({
      expected_version: '2',
      input: { name: ' Edited ' },
    })
  })
})

describe('Model deletion previews and original command recovery', () => {
  it.each(['error', 'mismatch', 'unbound'] as const)(
    'cannot dispatch a DELETE from %s Impact',
    async (mode) => {
      const f = await page()
      await selectProvider()
      await selectModel()
      f.setImpact(async () =>
        mode === 'error'
          ? problem('RESOURCE_BUSY', 409)
          : json(
              mode === 'mismatch'
                ? { ...preview(), version: '2' }
                : {
                    ...preview(),
                    reference_count: '1',
                    reference_groups: [{ owner_kind: 'agent', role: 'agent_model', count: '1' }],
                    replacement_requirement: 'required',
                    delete_blocker: 'reference_adapter_unbound',
                  },
            ),
      )
      await click('删除 Model')
      expect(dialog().querySelectorAll('.replacement-picker')).toHaveLength(0)
      expect(
        [...dialog().querySelectorAll('button')].some((b) =>
          b.textContent?.includes('确认删除 Model'),
        ),
      ).toBe(false)
      expect(f.writes()).toHaveLength(0)
      expect(dialog().textContent).toContain(
        mode === 'unbound' ? '当前引用替换能力尚未绑定' : '删除影响尚未确认',
      )
    },
  )
  it('confirms no-reference DELETE once and renders the actual unbounded nonnegative receipt count', async () => {
    const f = await page()
    await selectProvider()
    await selectModel()
    await click('删除 Model')
    expect(dialog().textContent).toContain('当前未登记引用')
    await click('确认删除 Model', dialog())
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
      expected_version: '1',
      replacement: null,
    })
    expect(f.wrapper.text()).toContain('实际处理 10001 条登记引用')
    expect(f.records.has(id(200))).toBe(false)
  })
  it('requires an explicit optional clear and records that choice as null only after confirmation', async () => {
    const f = await page({ protocol: 'jina-rerank' })
    await selectProvider()
    await selectModel()
    f.setImpact(async () =>
      json({
        ...preview(),
        reference_count: '1',
        reference_groups: [{ owner_kind: 'platform_selector', role: 'reranker', count: '1' }],
        replacement_requirement: 'optional',
      }),
    )
    await click('删除 Model')
    expect(button('确认删除 Model', dialog()).disabled).toBe(true)
    await click('清空相应用途引用', dialog())
    expect(button('确认删除 Model', dialog()).disabled).toBe(false)
    await click('确认删除 Model', dialog())
    expect(JSON.parse(f.writes()[0]![1].body as string).replacement).toBeNull()
  })
  it('enforces same type, nonself, enabled Provider/Model and memory json_schema before choosing a cross-Provider replacement', async () => {
    const f = await page()
    f.sources.set(id(101), provider(101))
    const good = {
      ...model(203, provider(101)),
      input: {
        ...input(),
        name: 'Eligible',
        capabilities: { ...input().capabilities, structured_output_modes: ['text', 'json_schema'] },
      },
    }
    f.records.set(good.id, good)
    f.records.set(id(202), {
      ...model(202, provider(101)),
      input: { ...input(), name: 'No schema' },
    })
    f.records.set(id(201), {
      ...model(201, provider(101)),
      input: { ...good.input, name: 'Disabled', enabled: false },
    })
    await selectProvider()
    await selectModel()
    f.setImpact(async () =>
      json({
        ...preview(),
        reference_count: '1',
        reference_groups: [{ owner_kind: 'platform_selector', role: 'memory', count: '1' }],
        replacement_requirement: 'required',
      }),
    )
    await click('删除 Model')
    expect(dialog().textContent).not.toContain('清空相应用途引用')
    const candidate = [...dialog().querySelectorAll('li')].find((n) =>
      n.textContent?.includes('Provider 101'),
    )!
    await click('查看此 Provider 的 Models', candidate)
    const choices = [...dialog().querySelectorAll('.choices li')]
    const missing = choices.find((n) => n.textContent?.includes('No schema'))!
    expect(button('选择此替代 Model', missing).disabled).toBe(true)
    expect(missing.textContent).toContain('json_schema')
    expect(
      button(
        '选择此替代 Model',
        choices.find((n) => n.textContent?.includes('Disabled'))!,
      ).disabled,
    ).toBe(true)
    await click(
      '选择此替代 Model',
      choices.find((n) => n.textContent?.includes('Eligible'))!,
    )
    expect(button('确认删除 Model', dialog()).disabled).toBe(false)
    await click('确认删除 Model', dialog())
    expect(JSON.parse(f.writes()[0]![1].body as string).replacement).toBe(good.id)
  })
  it('replays the original DELETE after an actual current GET404 and failed Impact without using those reads as permission', async () => {
    const f = await page()
    await selectProvider()
    await selectModel()
    await click('删除 Model')
    f.setMutation(async () => {
      f.records.delete(id(200))
      throw new Error('lost response')
    })
    await click('确认删除 Model', dialog())
    expect(f.auth.system.models.progress?.phase).toBe('uncertain')
    const first = f.writes()[0]!
    f.setImpact(async () => problem('NOT_FOUND', 404))
    f.setLookup(async () =>
      json({
        found: true,
        receipt: {
          kind: 'model.delete',
          resource_id: id(200),
          version: '2',
          affected_references: '3',
        },
      }),
    )
    await click('检查原请求', dialog())
    expect(f.writes()).toHaveLength(1)
    expect(f.auth.system.models.progress?.phase).toBe('uncertain')
    expect(dialog().textContent).toContain('尚未核对原输入')
    f.setMutation(async () =>
      json({ kind: 'model.delete', resource_id: id(200), version: '2', affected_references: '3' }),
    )
    await click('重试原请求', dialog())
    expect(f.writes()).toHaveLength(2)
    const replay = f.writes()[1]!
    expect(replay[0]).toBe(first[0])
    expect(replay[1].body).toBe(first[1].body)
    expect(new Headers(replay[1].headers).get('Idempotency-Key')).toBe(
      new Headers(first[1].headers).get('Idempotency-Key'),
    )
    expect(f.wrapper.text()).toContain('实际处理 3 条登记引用')
  })
})

describe('Model cohosted confirmation through the actual App lifecycle', () => {
  it.each(['close', 'route', 'logout'] as const)(
    'preserves pending %s across Session503 and same-identity restoration without new writes',
    async (action) => {
      const f = await page()
      await dirty()
      let pending: Promise<unknown> | undefined
      if (action === 'close') {
        button('取消', dialog()).click()
        await flushPromises()
      } else if (action === 'route') {
        pending = f.router.push('/system/providers')
        await flushPromises()
      } else await click('退出登录')
      confirmationRestored()
      const held = barrier<Response>()
      f.setSessionRequest(() => held.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(f.auth.personalContext.phase).toBe('checking')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      held.resolve(problem('DEPENDENCY_UNAVAILABLE'))
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      const recovery = button('检查当前会话')
      expect(recovery.disabled).toBe(false)
      expect(recovery.closest('[inert]')).toBeNull()
      expect(document.body.style.overflow).not.toBe('hidden')
      f.resetSession()
      await click('检查当前会话')
      confirmationRestored()
      expect(f.writes()).toHaveLength(0)
      await click('继续编辑', dialog())
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(field('名称').value).toBe(' Draft Model ')
      expect(f.router.currentRoute.value.path).toBe('/system/models')
      expect(dialog().contains(document.activeElement)).toBe(true)
      if (pending)
        expect(isNavigationFailure(await pending, NavigationFailureType.aborted)).toBe(true)
      expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/logout'))).toHaveLength(0)
    },
  )
  it.each(['route', 'logout'] as const)(
    'resumes only the original pending %s when discard is explicitly chosen after restoration',
    async (action) => {
      const f = await page()
      await dirty()
      const pending = action === 'route' ? f.router.push('/system/providers') : undefined
      if (action === 'logout') button('退出登录').click()
      await flushPromises()
      confirmationRestored()
      const held = barrier<Response>()
      f.setSessionRequest(() => held.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      held.resolve(json(sessionView()))
      await flushPromises()
      confirmationRestored()
      f.resetSession()
      await click('放弃修改', dialog())
      await pending
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.router.currentRoute.value.path).toBe(
        action === 'route' ? '/system/providers' : '/login',
      )
      expect(f.writes()).toHaveLength(0)
    },
  )
  it.each(['session', 'role', 'dispose'] as const)(
    'ends pending old navigation false on %s and cannot transfer its answer',
    async (change) => {
      const f = await page()
      await dirty()
      const pending = f.router.push('/system/providers')
      await flushPromises()
      confirmationRestored()
      if (change === 'dispose') {
        f.wrapper.unmount()
        wrapper = undefined
      } else {
        const current = sessionView()
        f.setSession(
          change === 'session'
            ? { ...current, session: { ...current.session, id: id(3) } }
            : { ...current, user: { ...current.user, role: 'user', version: '2' } },
        )
        window.dispatchEvent(new Event('pageshow'))
        await flushPromises()
      }
      expect(isNavigationFailure(await pending, NavigationFailureType.aborted)).toBe(true)
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('restores a pending confirmation with no business Dialog, without synthesizing a write or answering it', async () => {
    const f = await page()
    f.setMutation(async () => {
      throw new Error('lost')
    })
    await f.auth.system.models
      .start({
        kind: 'model.create',
        provider_id: id(100),
        protocol: 'openai-chat-completions',
        input: input(),
      })
      .catch(() => undefined)
    const pending = f.router.push('/system/providers')
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    const held = barrier<Response>()
    f.setSessionRequest(() => held.promise)
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    held.resolve(json(sessionView()))
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog().inert).toBe(false)
    expect(dialog().getAttribute('aria-hidden')).toBeNull()
    expect(f.writes()).toHaveLength(1)
    await click('继续编辑', dialog())
    expect(isNavigationFailure(await pending, NavigationFailureType.aborted)).toBe(true)
    expect(f.auth.system.models.progress?.phase).toBe('uncertain')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
  })
  it('restores a dirty deletion selection below its confirmation and keeps continue focus inside that business layer', async () => {
    const f = await page({ protocol: 'jina-rerank' })
    await selectProvider()
    await selectModel()
    f.setImpact(async () =>
      json({
        ...preview(),
        reference_count: '1',
        reference_groups: [{ owner_kind: 'platform_selector', role: 'reranker', count: '1' }],
        replacement_requirement: 'optional',
      }),
    )
    await click('删除 Model')
    await click('清空相应用途引用', dialog())
    await click('取消', dialog())
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
    const held = barrier<Response>()
    f.setSessionRequest(() => held.promise)
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    held.resolve(json(sessionView()))
    await flushPromises()
    const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
    expect(layers).toHaveLength(2)
    expect(layers[0]!.inert).toBe(true)
    expect(layers[1]!.inert).toBe(false)
    await click('继续编辑', dialog())
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog().textContent).toContain('已明确选择清空')
    expect(dialog().contains(document.activeElement)).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })
  it('blocks dirty beforeunload and removes its listener when App is disposed', async () => {
    const f = await page()
    await dirty()
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    f.wrapper.unmount()
    wrapper = undefined
    const after = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })
})
