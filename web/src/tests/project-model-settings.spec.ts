import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, isNavigationFailure, type Router } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectAuditAPI } from '../api/project-audit'
import { createProjectModelSettingsAPI } from '../api/project-models'
import { type CommitState, type Fetch } from '../api/client'
import { installAuthentication } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import ProjectNav from '../components/layout/ProjectNav.vue'
import ProjectModelProvidersView from '../views/projects/ProjectModelProvidersView.vue'
import ProjectAvailableModelsView from '../views/projects/ProjectAvailableModelsView.vue'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-08T12:34:56.123456Z',
  projectID = id(10),
  providerID = id(20),
  modelID = id(30),
  credentialID = id(40),
  prefix = `/api/v1/projects/${projectID}`
const providerPath = '/owner/demo/settings/model-providers',
  availablePath = '/owner/demo/settings/available-models'
const view = (): SessionView => ({
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
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const project = (lifecycle: Project['lifecycle'] = 'active', name = 'Demo'): Project => ({
  id: projectID,
  owner_user_id: id(1),
  name,
  normalized_name: name.toLowerCase(),
  description: 'Owner description',
  lifecycle,
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: lifecycle === 'archived' ? time : null,
})
const provider = () => ({
  id: providerID,
  scope: { kind: 'project', project_id: projectID },
  version: '1',
  created_at: time,
  updated_at: time,
  input: {
    name: 'Fixture Provider',
    protocol: 'openai-chat-completions',
    base_url: 'https://models.example/v1',
    enabled: true,
    credential_ref: credentialID,
    options: {},
  },
})
const capabilities = () => ({
  tool_calls: true,
  parallel_tool_calls: false,
  streaming: true,
  reasoning: false,
  input_modalities: ['text'],
  output_modalities: ['text'],
  structured_output_modes: ['text'],
  reasoning_efforts: [],
  context_length: '9007199254740993',
  max_output: null,
})
const model = () => ({
  id: modelID,
  provider_id: providerID,
  scope: { kind: 'project', project_id: projectID },
  version: '1',
  created_at: time,
  updated_at: time,
  input: {
    name: 'Fixture Chat',
    provider_model_id: 'native-chat',
    type: 'chat',
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: capabilities(),
  },
})
const available = (system = false) => ({
  id: system ? id(31) : modelID,
  provider_id: providerID,
  scope: system ? { kind: 'system' } : { kind: 'project', project_id: projectID },
  name: system ? 'System Safe Chat' : 'Project Safe Chat',
  provider_name: system ? 'System Safe Provider' : 'Project Safe Provider',
  version: '1',
  capabilities: capabilities(),
})
const receipt = (kind = 'provider.create', version = kind.endsWith('create') ? '1' : '2') => ({
  kind,
  resource_id: kind.startsWith('provider.') ? providerID : modelID,
  version,
  affected_references: '0',
})
const credential = (version = '1', deleted = false) => ({
  credential_id: credentialID,
  purpose: 'model',
  version,
  deleted,
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (
  code = 'DEPENDENCY_UNAVAILABLE',
  status = 503,
  commit: CommitState = 'not_started',
) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      detail: 'private diagnostic that must not render',
      instance: prefix,
      status,
      code,
      request_id: id(99),
      commit_state: commit,
    },
    status,
  )
function barrier<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const modelEndpoint = (path: string) =>
  /^\/api\/v1\/projects\/[^/]+\/(?:model-providers|models|available-chat-models|model-credentials|model-commands|model-credential-commands)(?:[/?]|$)/.test(
    path,
  )
const normalModels: Fetch = async (path, init) => {
  const pieces = path.split('?')[0]!.split('/'),
    family = pieces[5],
    target = pieces[6]
  if (init.method === 'GET') {
    if (family === 'model-credentials')
      return json({ credential_id: credentialID, purpose: 'model', version: '1' })
    if (family === 'available-chat-models')
      return json({ items: [available(), available(true)], next_cursor: null })
    const item = family === 'model-providers' ? provider() : model()
    return json(target ? item : { items: [item], next_cursor: null })
  }
  if (family === 'model-commands') return json({ found: false, receipt: null })
  if (family === 'model-credential-commands') return json({ observed: false, result: null })
  const body = JSON.parse(String(init.body)) as { expected_version?: string },
    kind = init.method === 'POST' ? 'create' : init.method === 'PUT' ? 'update' : 'delete',
    version = body.expected_version ? String(BigInt(body.expected_version) + 1n) : '1'
  return json(
    family === 'model-credentials'
      ? credential(version, kind === 'delete')
      : receipt(`${family === 'model-providers' ? 'provider' : 'model'}.${kind}`, version),
  )
}
const owners: SessionController[] = [],
  routers: Router[] = [],
  releases: (() => void)[] = []
let wrapper: VueWrapper | undefined,
  narrow = false
beforeEach(() => {
  narrow = false
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: narrow && media === '(max-width: 760px)',
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  // DOM focus behavior only. Actual geometry/themes/Tab/reduced-motion remain
  // in the separately authorized real-browser and image verification.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  for (const release of releases.splice(0)) release()
  wrapper?.unmount()
  wrapper = undefined
  for (const owner of owners.splice(0)) owner.leave()
  await flushPromises()
  for (const router of routers.splice(0)) router.options.history.destroy()
  selected.auth = null
  document.body.innerHTML = ''
  useTheme().setTheme('system')
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function app(
  target = providerPath,
  options: {
    anonymous?: boolean
    lifecycle?: Project['lifecycle']
    name?: string
    models?: Fetch
    owner?: Fetch
  } = {},
) {
  let signedIn = !options.anonymous,
    current = view(),
    currentProject = project(options.lifecycle, options.name),
    session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401)),
    models: Fetch = options.models ?? normalModels,
    owner: Fetch | null = options.owner ?? null
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (modelEndpoint(path)) return models(path, init)
    if (path.startsWith('/api/v1/projects/resolve?') || path.startsWith('/api/v1/projects/'))
      return owner ? owner(path, init) : json(currentProject)
    if (path.startsWith('/api/v1/projects?')) return json({ items: [], next_cursor: null })
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/login')) {
      signedIn = true
      return json({ user: current.user, session: current.session, next_path: '/' })
    }
    if (path.endsWith('/logout')) {
      signedIn = false
      return new Response(null, { status: 204 })
    }
    throw new Error('Unexpected controlled endpoint')
  })
  const auth = createSessionController(
    createAccountAPI(fetch),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetch),
    createProjectAuditAPI(fetch),
    createProjectModelSettingsAPI(fetch),
  )
  selected.auth = auth
  owners.push(auth)
  await auth.restore()
  const { router: production } = await import('../router')
  production.options.history.destroy()
  const router = createRouter({ history: createMemoryHistory(), routes: production.options.routes })
  routers.push(router)
  installAuthentication(router, auth)
  await router.push(target)
  await router.isReady()
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  wrapper = mount(App, { attachTo: host, global: { plugins: [router] } })
  await flushPromises()
  return {
    auth,
    fetch,
    router,
    wrapper,
    setModels(value: Fetch) {
      models = value
    },
    setOwner(value: Fetch | null) {
      owner = value
    },
    setProject(value: Project) {
      currentProject = value
    },
    setView(value: SessionView) {
      current = value
    },
    setSession(value: Fetch) {
      session = value
    },
    reads: () =>
      fetch.mock.calls.filter(([path, init]) => modelEndpoint(path) && init.method === 'GET'),
    writes: () =>
      fetch.mock.calls.filter(([path, init]) => modelEndpoint(path) && init.method !== 'GET'),
  }
}
function accessibleText(node: Element) {
  const copy = node.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((x) => x.remove())
  return (node.getAttribute('aria-label') ?? copy.textContent ?? '').trim()
}
function button(name: string, scope: ParentNode = document) {
  const matches = [...scope.querySelectorAll<HTMLButtonElement>('button')].filter(
    (node) => accessibleText(node) === name && !node.closest('[aria-hidden="true"]'),
  )
  if (matches.length !== 1)
    throw new Error(`Expected one public button: ${name}; got ${matches.length}`)
  return matches[0]!
}
function field(name: string, scope: ParentNode = document) {
  const matches = [...scope.querySelectorAll<HTMLLabelElement>('label')].filter(
    (node) => accessibleText(node) === name,
  )
  if (matches.length !== 1 || !matches[0]!.htmlFor)
    throw new Error(`Expected one public field: ${name}`)
  const input = document.getElementById(matches[0]!.htmlFor)
  if (!(input instanceof HTMLInputElement || input instanceof HTMLTextAreaElement))
    throw new Error('Expected editable input')
  return input
}
async function fill(name: string, value: string) {
  const input = field(name)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function click(name: string, scope: ParentNode = document) {
  button(name, scope).click()
  await flushPromises()
}
function topDialog() {
  const dialogs = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].filter(
    (node) => !node.closest('[aria-hidden="true"]'),
  )
  if (!dialogs.length) throw new Error('Missing public dialog')
  return dialogs.at(-1)!
}
async function providerDraft() {
  await click('创建 Provider')
  await fill('Provider 名称', 'New Provider')
  await fill('Base URL', 'https://models.example/v1')
}

describe('Project model production routes and visible authority', () => {
  it.each([providerPath, availablePath])(
    'renders ordinary Owner %s within dual navigation and the exact two-leaf group',
    async (target) => {
      const f = await app(target)
      expect(f.wrapper.find('nav[aria-label="系统导航"]').text()).toBe('项目')
      expect(f.wrapper.find('nav[aria-label="项目导航"] a[aria-current="page"]').text()).toBe(
        '项目设置',
      )
      expect(
        f.wrapper.find('nav[aria-label="项目导航"] a[aria-current="page"]').attributes('href'),
      ).toBe('/owner/demo/settings/general')
      expect(f.wrapper.findAll('nav[aria-label="项目设置"] a').map((x) => x.text())).toEqual([
        '基本信息',
        '项目审计',
        'Providers',
        '可用模型',
      ])
      expect(f.reads()).toHaveLength(1)
      expect(f.fetch.mock.calls.some(([path]) => path.startsWith('/api/v1/system/'))).toBe(false)
      expect(f.writes()).toHaveLength(0)
      expect(f.wrapper.findAll('img').length).toBeGreaterThan(0)
    },
  )
  it.each(['model-providers', 'available-models'])(
    'anonymous login return preserves the valid dotted %s leaf and only then resolves/gets',
    async (leaf) => {
      const path = `/OWNER/OWNER.DOT-NAME/settings/${leaf}`
      const f = await app(path, { anonymous: true, name: 'owner.dot-name' })
      expect(f.router.currentRoute.value.path).toBe('/login')
      expect(f.router.currentRoute.value.query.return).toBe(
        `/owner/owner.dot-name/settings/${leaf}`,
      )
      expect(f.fetch.mock.calls.some(([url]) => url.startsWith('/api/v1/projects'))).toBe(false)
      await f.wrapper.find('#login-email').setValue('owner@example.test')
      await f.wrapper.find('#login-password').setValue('controlled login password')
      await f.wrapper.find('form').trigger('submit')
      await flushPromises()
      expect(f.router.currentRoute.value.path).toBe(`/owner/owner.dot-name/settings/${leaf}`)
      const requests = f.fetch.mock.calls.map(([url]) => url)
      const resolve = requests.findIndex((url) => url.startsWith('/api/v1/projects/resolve?')),
        get = requests.findIndex((url) => url === prefix),
        models = requests.findIndex(modelEndpoint)
      expect(resolve).toBeGreaterThan(-1)
      expect(get).toBeGreaterThan(resolve)
      expect(models).toBeGreaterThan(get)
    },
  )
  it.each(
    ['model-providers', 'available-models'].flatMap((leaf) =>
      ['', '/child', '?', '?cursor=x', '#', '#item', '%2Fchild', '\\child'].map(
        (suffix) => `/owner/demo/settings/${leaf}${suffix || '/'}`,
      ),
    ),
  )('rejects malformed raw %s before any Project request', async (target) => {
    const f = await app(target)
    expect(f.router.currentRoute.value.name).toBe('not-found')
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith('/api/v1/projects'))).toBe(false)
    expect(f.wrapper.findComponent(ProjectNav).exists()).toBe(false)
  })
  it('current Owner Get must finish before any model data can load or show', async () => {
    const owner = barrier<Response>()
    releases.push(() => owner.resolve(json(project())))
    const f = await app(providerPath, {
      owner: async (path) =>
        path.startsWith('/api/v1/projects/resolve?') ? json(project()) : owner.promise,
    })
    expect(f.reads()).toHaveLength(0)
    expect(f.wrapper.findComponent(ProjectModelProvidersView).exists()).toBe(false)
    expect(document.body.textContent).not.toContain('Fixture Provider')
    owner.resolve(json(project()))
    await flushPromises()
    expect(f.reads()).toHaveLength(1)
    expect(f.wrapper.findComponent(ProjectModelProvidersView).text()).toContain('Fixture Provider')
  })
  it.each(['archiving', 'archived'] as const)(
    '%s remains readable but disables new mutations',
    async (lifecycle) => {
      const f = await app(providerPath, { lifecycle })
      expect(f.wrapper.findComponent(ProjectModelProvidersView).text()).toContain(
        'Fixture Provider',
      )
      expect(button('创建 Provider').disabled).toBe(true)
      expect(button('创建凭据').disabled).toBe(true)
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('available projection reveals no endpoint, credentials, native name or System edit link', async () => {
    const f = await app(availablePath),
      page = f.wrapper.getComponent(ProjectAvailableModelsView)
    expect(page.text()).toContain('System Safe Chat')
    expect(page.text()).toContain('Project Safe Chat')
    expect(page.text()).not.toContain('models.example')
    expect(page.text()).not.toContain(credentialID)
    expect(page.text()).not.toContain('native-chat')
    expect(page.find('a[href^="/system"]').exists()).toBe(false)
    expect(f.reads()).toHaveLength(1)
  })
  it('a malformed whole page is an error with no partial rows or private diagnostic', async () => {
    const f = await app(providerPath, {
      models: async () =>
        json({
          items: [provider(), { ...provider(), id: id(21), scope: { kind: 'system' } }],
          next_cursor: null,
        }),
    })
    const page = f.wrapper.getComponent(ProjectModelProvidersView)
    expect(page.text()).not.toContain('Fixture Provider')
    expect(page.text()).not.toContain('private diagnostic')
    expect(f.writes()).toHaveLength(0)
  })
})

describe('Project model editor behavior with the real App owner', () => {
  it('Provider explicit save uses original version and confirmed read failure never repeats a write', async () => {
    const f = await app()
    await click(`读取 Provider ${providerID}`)
    expect(button('保存 Provider').disabled).toBe(true)
    await fill('Provider 名称', 'Changed Provider')
    await click('保存 Provider')
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(String(f.writes()[0]![1].body))).toMatchObject({
      expected_version: '1',
      input: { name: 'Changed Provider' },
    })
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
    expect(button('已保存').disabled).toBe(true)
    f.setModels(async () => problem())
    await click('重新读取 Provider')
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
    expect(f.writes()).toHaveLength(1)
    expect(document.body.textContent).not.toContain('private diagnostic')
  })
  it('invalid Provider input stays local and focuses a visible invalid field', async () => {
    const f = await app()
    await providerDraft()
    await fill('Base URL', 'not an endpoint')
    await click('保存 Provider')
    expect(f.writes()).toHaveLength(0)
    const invalid = topDialog().querySelector<HTMLElement>('[aria-invalid="true"]')
    expect(invalid).not.toBeNull()
    expect(document.activeElement).toBe(invalid)
    expect(field('Base URL').value).toBe('not an endpoint')
  })
  it('Unknown recovery remains reachable in the modal and lookup is historical only', async () => {
    const f = await app()
    await providerDraft()
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await click('保存 Provider')
    expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    const original = f.writes()[0]![1],
      dialog = topDialog()
    expect(button('查证原请求', dialog).disabled).toBe(false)
    f.setModels(async () => json({ found: true, receipt: receipt() }))
    await click('查证原请求', dialog)
    expect(f.auth.projectModelSettings.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'observed',
    })
    expect(f.writes()).toHaveLength(2)
    expect(topDialog().textContent).toContain('历史')
    f.setModels(normalModels)
    await click('按原请求重放', topDialog())
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
    expect(f.writes().at(-1)![1].body).toBe(original.body)
    expect(new Headers(f.writes().at(-1)![1].headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
  })
  it('Credential confirmation leaves a safe unbound ref and never auto-saves Provider', async () => {
    const f = await app()
    await click('创建凭据')
    const input = field('新凭据材料')
    expect(input.type).toBe('password')
    expect(input.getAttribute('autocomplete')).toBe('off')
    await fill('新凭据材料', 'controlled-private-material')
    await click('创建凭据', topDialog())
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe(`${prefix}/model-credentials`)
    expect(input.value).toBe('')
    expect(topDialog().textContent).toContain(credentialID)
    expect(topDialog().textContent).toContain('尚未绑定')
    expect(document.body.textContent).not.toContain('controlled-private-material')
    expect(JSON.stringify(f.auth.projectModelSettings.progress)).not.toContain(
      'controlled-private-material',
    )
  })
  it('Model creation uses the formal Provider context with fixed chat type and capability input', async () => {
    const f = await app()
    await click(`为 Provider ${providerID} 创建 Model`)
    expect(f.reads().at(-1)![0]).toBe(`${prefix}/model-providers/${providerID}`)
    expect(topDialog().textContent).toContain('chat（只读）')
    expect(topDialog().textContent).toContain('openai-chat-completions')
    expect(topDialog().querySelector('[role="listbox"]')).toBeNull()
    await fill('Model 名称', 'Created Chat')
    await fill('原生型号', 'native-created-chat')
    await fill('上下文长度', '9007199254740993')
    await click('保存 Model', topDialog())
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(String(f.writes()[0]![1].body))).toMatchObject({
      provider_id: providerID,
      input: {
        name: 'Created Chat',
        provider_model_id: 'native-created-chat',
        type: 'chat',
        parameters: {},
        request_overwrite: {},
        header_overwrite: {},
        capabilities: {
          input_modalities: ['text'],
          output_modalities: ['text'],
          structured_output_modes: ['text'],
          reasoning_efforts: [],
          context_length: '9007199254740993',
        },
      },
    })
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
  })
  it('Model delete requires an explicit replacement decision and retains the formal Get version', async () => {
    const f = await app()
    await click('项目 Models（全部 Providers）')
    await click(`读取 Model ${modelID}`)
    expect(f.reads().some(([path]) => path === `${prefix}/models/${modelID}`)).toBe(true)
    expect(f.reads().some(([path]) => path === `${prefix}/model-providers/${providerID}`)).toBe(
      true,
    )
    expect(topDialog().textContent).toContain('所属 Provider 和类型创建后不可修改')
    await click('删除 Model', topDialog())
    await click('确认删除', topDialog())
    expect(f.writes()).toHaveLength(0)
    expect(topDialog().textContent).toContain('请明确选择无替代')
    await click('删除替代', topDialog())
    await click('无替代')
    await click('确认删除', topDialog())
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe(`${prefix}/models/${modelID}`)
    expect(f.writes()[0]![1].method).toBe('DELETE')
    expect(JSON.parse(String(f.writes()[0]![1].body))).toEqual({
      expected_version: '1',
      replacement: null,
    })
    expect(f.auth.projectModelSettings.progress).toMatchObject({
      kind: 'model.delete',
      phase: 'confirmed',
    })
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith('/api/v1/system'))).toBe(false)
  })
  it('prepared Credential only enters a Provider draft and survives a separately rejected Provider save', async () => {
    const f = await app()
    await providerDraft()
    await click('创建凭据', topDialog())
    await fill('新凭据材料', 'controlled-partial-material')
    await click('创建凭据', topDialog())
    await click('使用已创建凭据', topDialog())
    expect(f.writes()).toHaveLength(1)
    await click('关闭', topDialog().querySelector('footer')!)
    expect(field('凭据 ID').value).toBe(credentialID)
    expect(topDialog().textContent).toContain('尚未绑定')
    f.setModels(async () => problem('RESOURCE_BUSY', 409))
    await click('保存 Provider', topDialog())
    expect(f.writes()).toHaveLength(2)
    expect(f.auth.projectModelSettings.progress?.phase).toBe('rejected')
    expect(topDialog().textContent).toContain(credentialID)
    expect(topDialog().textContent).toContain('尚未绑定')
    f.setModels(normalModels)
    await fill('Provider 名称', 'Changed Partial Provider')
    await click('保存 Provider', topDialog())
    expect(f.writes()).toHaveLength(3)
    expect(f.writes().filter(([path]) => path.endsWith('/model-credentials'))).toHaveLength(1)
    expect(JSON.parse(String(f.writes().at(-1)![1].body)).input.credential_ref).toBe(credentialID)
    expect(f.auth.projectModelSettings.progress?.phase).toBe('confirmed')
    expect(document.body.textContent).not.toContain('controlled-partial-material')
  })
  it('Credential rotation needs explicit metadata, clears material, and cannot reuse it for another ID', async () => {
    const f = await app()
    await click('管理凭据')
    expect(button('轮换凭据', topDialog()).disabled).toBe(true)
    await fill('Credential ID', credentialID)
    await click('读取凭据信息', topDialog())
    expect(field('新凭据材料').value).toBe('')
    await fill('新凭据材料', 'controlled-rotation-material')
    await click('轮换凭据', topDialog())
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe(`${prefix}/model-credentials/${credentialID}`)
    expect(JSON.parse(String(f.writes()[0]![1].body)).expected_version).toBe('1')
    expect(field('新凭据材料').value).toBe('')
    await fill('Credential ID', id(41))
    expect(button('轮换凭据', topDialog()).disabled).toBe(true)
    expect(button('删除凭据', topDialog()).disabled).toBe(true)
    expect(f.writes()).toHaveLength(1)
    expect(document.body.textContent).not.toContain('controlled-rotation-material')
  })
  it('S2 App keeps a dirty dialog hidden after same identity checking until a held fresh Owner Get completes', async () => {
    const f = await app()
    await providerDraft()
    const gate = barrier<Response>(),
      owner = barrier<Response>(),
      modelReads = f.reads().length,
      ownerReads = () => f.fetch.mock.calls.filter(([path]) => path === prefix).length,
      priorOwnerReads = ownerReads()
    releases.push(
      () => gate.resolve(json(view())),
      () => owner.resolve(json(project())),
    )
    f.setSession(() => gate.promise)
    const restoring = f.auth.restore()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.wrapper.text()).toContain('正在确认会话')
    gate.resolve(json(view()))
    await restoring
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.wrapper.text()).toContain('配置内容已隐藏')
    expect(f.reads()).toHaveLength(modelReads)
    expect(ownerReads()).toBe(priorOwnerReads)
    f.setOwner((path) => {
      expect(path).toBe(prefix)
      return owner.promise
    })
    await click('重新读取项目')
    expect(ownerReads()).toBe(priorOwnerReads + 1)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.reads()).toHaveLength(modelReads)
    owner.resolve(json({ ...project(), version: '8' }))
    await flushPromises()
    expect(field('Provider 名称').value).toBe('New Provider')
    expect(f.reads()).toHaveLength(modelReads + 1)
    expect(f.writes()).toHaveLength(0)
  })
  it('S2 App resumes archived Unknown Credential only for historical lookup after explicit fresh Owner Get', async () => {
    const f = await app()
    await click('创建凭据')
    await fill('新凭据材料', 'controlled-private-original')
    f.setModels(async () => problem('COMMIT_UNKNOWN', 503, 'unknown'))
    await click('创建凭据', topDialog())
    expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    f.setModels(normalModels)
    f.setProject(project('archived'))
    const reads = f.reads().length,
      writes = f.writes().length
    await f.auth.restore()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.reads()).toHaveLength(reads)
    await click('重新读取项目')
    expect(f.wrapper.text()).toContain('archived')
    expect(button('查证原请求', topDialog()).disabled).toBe(false)
    expect(button('按原请求重放', topDialog()).disabled).toBe(true)
    expect(f.auth.projectModelSettings.progress?.phase).toBe('uncertain')
    expect(f.writes()).toHaveLength(writes)
    expect(document.body.textContent).not.toContain('controlled-private-original')
  })
  it('S2 App keeps private content hidden and clears local material after fresh Owner denial', async () => {
    const f = await app()
    await providerDraft()
    const reads = f.reads().length
    f.setOwner(async () => problem('FORBIDDEN', 403))
    await f.auth.restore()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.reads()).toHaveLength(reads)
    await click('重新读取项目')
    expect(f.wrapper.text()).toContain('项目不可用')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(f.wrapper.findComponent(ProjectModelProvidersView).exists()).toBe(false)
    expect(f.auth.projectModelSettings.progress).toBeNull()
    expect(f.reads()).toHaveLength(reads)
    expect(f.writes()).toHaveLength(0)
  })
  it.each([
    [providerPath, availablePath],
    [availablePath, providerPath],
  ])('S2 real router from %s to %s cannot reuse cached Owner authority', async (start, target) => {
    const f = await app(start)
    const priorReads = f.reads().length,
      ownerReads = () => f.fetch.mock.calls.filter(([path]) => path === prefix).length,
      priorOwnerReads = ownerReads()
    await f.router.push(target)
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe(target)
    expect(ownerReads()).toBe(priorOwnerReads)
    expect(f.reads()).toHaveLength(priorReads)
    expect(button('重新读取项目').disabled).toBe(false)
    await click('重新读取项目')
    expect(ownerReads()).toBe(priorOwnerReads + 1)
    expect(f.reads()).toHaveLength(priorReads + 1)
    expect(f.reads().at(-1)![0]).toBe(
      `${prefix}/${target === availablePath ? 'available-chat-models' : 'model-providers'}?limit=25`,
    )
    expect(f.writes()).toHaveLength(0)
  })
  it('S2 confirmed draft discard and leaving for General do not waive authority on return', async () => {
    const f = await app()
    await providerDraft()
    const priorReads = f.reads().length,
      ownerReads = () => f.fetch.mock.calls.filter(([path]) => path === prefix).length,
      priorOwnerReads = ownerReads(),
      moving = f.router.push('/owner/demo/settings/general')
    await flushPromises()
    expect(topDialog().textContent).toContain('离开项目模型设置')
    await click('放弃并离开', topDialog())
    await moving
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(ownerReads()).toBe(priorOwnerReads)
    await f.router.push(providerPath)
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe(providerPath)
    expect(f.reads()).toHaveLength(priorReads)
    expect(ownerReads()).toBe(priorOwnerReads)
    await click('重新读取项目')
    expect(ownerReads()).toBe(priorOwnerReads + 1)
    await click('创建 Provider')
    expect(field('Provider 名称').value).toBe('')
    expect(f.writes()).toHaveLength(0)
  })
  it('real Session change removes old private input and cannot revive pending recovery', async () => {
    const f = await app()
    await click('创建凭据')
    await fill('新凭据材料', 'controlled-private-material')
    const next = view()
    next.session.id = id(3)
    f.setView(next)
    await f.auth.restore()
    await flushPromises()
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(f.auth.projectModelSettings.progress).toBeNull()
    expect(f.writes()).toHaveLength(0)
  })
  it('cancelling route and Logout confirmations retains draft, URL and request count', async () => {
    const f = await app()
    await providerDraft()
    const moving = f.router.push(availablePath)
    await flushPromises()
    expect(topDialog().textContent).toContain('离开项目模型设置')
    await click('继续编辑', topDialog())
    expect(isNavigationFailure(await moving)).toBe(true)
    expect(f.router.currentRoute.value.path).toBe(providerPath)
    expect(field('Provider 名称').value).toBe('New Provider')
    // Use the public account action after closing the edit modal only by
    // explicit confirmation; the local dirty dialog cannot be bypassed.
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    await click('退出登录')
    expect(topDialog().textContent).toContain('离开项目模型设置')
    await click('继续编辑', topDialog())
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/logout'))).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })
  it('confirmed navigation clears the local draft and selects the independent available leaf', async () => {
    const f = await app()
    await providerDraft()
    const moving = f.router.push(availablePath)
    await flushPromises()
    await click('放弃并离开', topDialog())
    await moving
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe(availablePath)
    expect(f.wrapper.findComponent(ProjectAvailableModelsView).exists()).toBe(true)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.writes()).toHaveLength(0)
  })
  it('narrow settings menu exposes the same two Model leaves without inventing a third Models route', async () => {
    narrow = true
    const f = await app()
    await click('项目设置栏目')
    const nav = topDialog().querySelector('nav[aria-label="项目设置"]')!
    expect([...nav.querySelectorAll('a')].map((node) => node.getAttribute('href'))).toContain(
      providerPath,
    )
    expect([...nav.querySelectorAll('a')].map((node) => node.getAttribute('href'))).toContain(
      availablePath,
    )
    expect(
      [...nav.querySelectorAll('a')].some(
        (node) => node.getAttribute('href') === '/owner/demo/settings/models',
      ),
    ).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })
})
