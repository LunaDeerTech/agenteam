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
import { createSystemProviderAPI, type ProviderInput } from '../api/system-providers'
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemInvitationsView from '../views/system/SystemInvitationsView.vue'
import SystemProvidersView from '../views/system/SystemProvidersView.vue'
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
const input = (): ProviderInput => ({
  name: 'Original Provider',
  protocol: 'openai-chat-completions',
  base_url: 'https://provider.example/v1',
  enabled: false,
  credential_ref: null,
  options: {},
})
const row = (n = 100) => ({
  id: id(n),
  input: input(),
  version: '1',
  created_at: time,
  updated_at: time,
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
      detail: 'private detail',
      instance: '/api/v1/system/model-providers',
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
  // State/DOM only. Real focus and native default actions are browser assertions.
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
  path = '/system/providers',
  role: 'admin' | 'user' = 'admin',
  anonymous = false,
) {
  let current: SessionView = {
      user: { ...user, role },
      session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
      csrf_token: 'S'.repeat(43),
    },
    signedIn = !anonymous
  let item = row(),
    session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
  let list: Fetch = async () => json({ items: [item], next_cursor: null }),
    detail: Fetch = async () => json(item),
    models: Fetch = async () => json({ items: [], next_cursor: null }),
    metadata: Fetch = async () => json({ credential_id: id(200), purpose: 'model', version: '2' })
  let mutation: Fetch = async (p, init) => {
    if (p.endsWith('/model-credentials'))
      return json({ credential_id: id(200), purpose: 'model', version: '1', deleted: false })
    if (p.endsWith('credential-commands/lookup'))
      return json({
        observed: true,
        result: { credential_id: id(200), purpose: 'model', version: '1', deleted: false },
      })
    if (p.endsWith('model-commands/lookup'))
      return json({
        found: true,
        receipt: {
          kind: 'provider.create',
          resource_id: id(100),
          version: '1',
          affected_references: '0',
        },
      })
    const body = JSON.parse(init.body as string)
    if (body.input)
      item = {
        ...item,
        input: body.input,
        version: init.method === 'POST' ? '1' : String(BigInt(body.expected_version) + 1n),
      }
    return json({
      kind:
        init.method === 'POST'
          ? 'provider.create'
          : init.method === 'PUT'
            ? 'provider.update'
            : 'provider.delete',
      resource_id: item.id,
      version: init.method === 'DELETE' ? String(BigInt(body.expected_version) + 1n) : item.version,
      affected_references: '0',
    })
  }
  const fetch = vi.fn<Fetch>(async (p, init) => {
    if (p === '/api/v1/session') return session(p, init)
    if (p.startsWith('/api/v1/system/model-providers?')) return list(p, init)
    if (p.startsWith('/api/v1/system/model-providers/') && init.method === 'GET')
      return detail(p, init)
    if (p.startsWith('/api/v1/system/models?')) return models(p, init)
    if (p.startsWith('/api/v1/system/model-credentials/') && init.method === 'GET')
      return metadata(p, init)
    if (p.startsWith('/api/v1/system/model-')) return mutation(p, init)
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
        ],
      },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(path)
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
    setList(value: Fetch) {
      list = value
    },
    setDetail(value: Fetch) {
      detail = value
    },
    setModels(value: Fetch) {
      models = value
    },
    setMetadata(value: Fetch) {
      metadata = value
    },
    setMutation(value: Fetch) {
      mutation = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    resetSession() {
      session = async () => json(current)
    },
    setItem(value: typeof item) {
      item = value
    },
    writes() {
      return fetch.mock.calls.filter(
        ([p, init]) => p.startsWith('/api/v1/system/model-') && init.method !== 'GET',
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
  const element = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (node) => node.textContent?.replace(' *', '').trim() === label,
  )
  expect(element, `field ${label}`).toBeTruthy()
  return document.getElementById(element!.htmlFor) as HTMLInputElement | HTMLTextAreaElement
}
async function set(label: string, value: string) {
  const control = field(label)
  control.value = value
  control.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function fill() {
  await set('名称', 'Draft Provider')
  await set('Base URL', 'https://provider.example/custom')
  await click('协议', dialog())
  await click('openai-chat-completions')
  await click('启用状态', dialog())
  await click('启用')
}
async function openDirty() {
  await click('创建 Provider')
  await fill()
}

describe('Provider actual App/router/controller composition', () => {
  it('keeps users as default, has exactly six leaves in three groups and validates ten exact return paths', async () => {
    const f = await page('/system')
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(
      f.wrapper
        .get('nav[aria-label="系统设置"]')
        .findAll('a')
        .map((a) => a.text()),
    ).toEqual(['用户', '待注册邀请', 'Providers', 'Models', '平台模型用途', '账号安全'])
    expect(f.wrapper.findAll('.settings-group-toggle')).toHaveLength(3)
    await f.wrapper.get('a[href="/system/providers"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('Providers')
    expect(safeReturnTarget('/system/providers')).toBe('/system/providers')
    expect(safeReturnTarget('/system/models')).toBe('/system/models')
    expect(safeReturnTarget('/system/model-selection')).toBe('/system/model-selection')
    for (const value of [
      '/system/providers?x=1',
      '/system/providers#x',
      ['/system/providers'],
      '//evil/system/providers',
    ])
      expect(safeReturnTarget(value)).toBe('/')
    const { router } = await import('../router/index')
    expect(router.resolve('/system/providers').meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
  })
  it('ordinary users have no leaf and make no management request', async () => {
    const f = await page('/system/providers', 'user')
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(f.fetch.mock.calls.some(([p]) => p.startsWith('/api/v1/system/model'))).toBe(false)
  })
  it('returns through login to Providers and requires explicit protocol and enabled selection', async () => {
    const f = await page('/system/providers', 'admin', true)
    expect(f.router.currentRoute.value.query.return).toBe('/system/providers')
    await f.wrapper.get('#login-email').setValue('admin@example.com')
    await f.wrapper.get('#login-password').setValue('Generated password 123')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    await click('创建 Provider')
    await set('名称', 'Draft Provider')
    await set('Base URL', 'https://provider.example/v1')
    await click('保存配置')
    expect(f.writes()).toHaveLength(0)
    expect(dialog().textContent).toContain('请检查名称')
    await click('协议', dialog())
    await click('openai-chat-completions')
    await click('启用状态', dialog())
    await click('禁用')
    await click('保存配置')
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string).input).toMatchObject({
      enabled: false,
      credential_ref: null,
      options: {},
    })
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(f.wrapper.text()).toContain('Provider 配置已保存')
  })
  it('gets current edit version, keeps protocol readonly, zero-writes unchanged edits and preserves the old ref on blank material', async () => {
    const f = await page()
    f.setItem({ ...row(), input: { ...input(), credential_ref: id(200) } })
    await click('查看 Original Provider')
    await click('编辑 Provider')
    expect((field('协议') as HTMLInputElement).readOnly).toBe(true)
    expect(button('保存配置').disabled).toBe(true)
    await click('保存配置')
    expect(f.writes()).toHaveLength(0)
    await set('名称', 'Revised')
    await click('保存配置')
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
      expected_version: '1',
      input: { ...input(), name: 'Revised', credential_ref: id(200) },
    })
    expect(
      f.fetch.mock.calls.filter(([p]) => p === `/api/v1/system/model-providers/${id(100)}`),
    ).toHaveLength(4)
  })
  it('clears DOM material at strict Credential confirmation and retains partial success on Provider rejection', async () => {
    const f = await page(),
      held = barrier<Response>()
    f.setMutation(async (p) =>
      p.endsWith('/model-credentials')
        ? json({ credential_id: id(200), purpose: 'model', version: '1', deleted: false })
        : held.promise,
    )
    await openDirty()
    await set('新凭据（仅写入）', 'generated-only')
    await click('保存配置')
    expect(field('新凭据（仅写入）').value).toBe('')
    expect(dialog().textContent).toContain('新凭据已创建')
    held.resolve(problem('VERSION_CONFLICT', 409))
    await flushPromises()
    expect(dialog().textContent).toContain('Provider尚未确认保存')
    expect(f.writes().filter(([p]) => p.endsWith('/model-credentials'))).toHaveLength(1)
    expect(f.writes().some(([, init]) => init.method === 'DELETE')).toBe(false)
  })
  it('metadata404 is not unconfigured and a failed read does not clear ordinary editor fields', async () => {
    const f = await page()
    f.setItem({ ...row(), input: { ...input(), credential_ref: id(200) } })
    f.setMetadata(async () => problem('NOT_FOUND', 404))
    await click('查看 Original Provider')
    expect(f.wrapper.text()).toContain('引用不存在或不可读取')
    await click('编辑 Provider')
    await set('名称', 'Still here')
    expect(field('名称').value).toBe('Still here')
    expect(f.auth.system.providers.material.present).toBe(false)
  })
  it('only an empty successful first Model page permits delete and cancelled deletion dispatches nothing', async () => {
    const f = await page()
    await click('查看 Original Provider')
    expect(button('删除 Provider').disabled).toBe(false)
    await click('删除 Provider')
    await click('取消', dialog())
    expect(f.writes()).toHaveLength(0)
    f.setModels(async () => problem('INTERNAL_ERROR'))
    await click('编辑 Provider')
    await click('取消', dialog())
    expect(button('删除 Provider').disabled).toBe(true)
    expect(f.wrapper.text()).toContain('Models读取失败')
  })
  it('confirmed write remains confirmed after independent GET failure', async () => {
    const f = await page()
    await openDirty()
    f.setDetail(async () => problem('INTERNAL_ERROR'))
    await click('保存配置')
    expect(f.wrapper.text()).toContain('操作已确认，当前配置读取失败')
    expect(f.auth.system.providers.progress?.phase).toBe('confirmed')
    expect(f.writes()).toHaveLength(1)
    await click('重新读取当前配置')
    expect(f.writes()).toHaveLength(1)
  })
  it('immediate new creation clears completed feedback and old Credential tracking without waiting 2400ms', async () => {
    const f = await page()
    await openDirty()
    await set('新凭据（仅写入）', 'generated-only')
    await click('保存配置')
    expect(f.auth.system.providers.progress?.credential?.credential_id).toBe(id(200))
    await click('操作已确认')
    expect(field('名称').value).toBe('')
    expect(field('新凭据（仅写入）').value).toBe('')
    expect(f.auth.system.providers.progress).toBeNull()
    expect(button('保存配置', dialog()).disabled).toBe(true)
    expect(dialog().textContent).not.toContain('新凭据已创建')
    await set('名称', 'Next Provider')
    await set('Base URL', 'https://next.example/v1')
    await click('协议', dialog())
    await click('openai-chat-completions')
    await click('启用状态', dialog())
    await click('启用')
    await click('保存配置', dialog())
    const writes = f.writes()
    expect(writes.filter(([p]) => p.endsWith('/model-credentials'))).toHaveLength(1)
    expect(JSON.parse(writes.at(-1)![1].body as string).input.credential_ref).toBeNull()
  })
  it('a pending confirmation without a business Dialog survives checking and settles through its real buttons', async () => {
    const f = await page(),
      held = barrier<Response>()
    f.setMutation(async () => {
      throw new Error('lost')
    })
    await f.auth.system.providers
      .start({ kind: 'provider.create', input: input() })
      .catch(() => undefined)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    const navigation = f.router.push('/system/users')
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    f.setSessionRequest(() => held.promise)
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    held.resolve(problem('INTERNAL_ERROR'))
    await flushPromises()
    expect(button('检查当前会话').closest('[inert]')).toBeNull()
    f.resetSession()
    await click('检查当前会话')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(dialog().inert).toBe(false)
    expect(dialog().getAttribute('aria-hidden')).toBeNull()
    await click('继续编辑', dialog())
    expect(isNavigationFailure(await navigation, NavigationFailureType.aborted)).toBe(true)
    expect(f.auth.system.providers.progress?.phase).toBe('uncertain')
    const leave = f.router.push('/system/users')
    await flushPromises()
    await click('放弃修改', dialog())
    await leave
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(f.auth.system.providers.progress).toBeNull()
    expect(f.writes()).toHaveLength(1)
  })
  it.each(['close', 'route', 'logout'] as const)(
    'dirty %s confirmation is cohosted last and checking failure/recovery preserves its pending answer',
    async (kind) => {
      const f = await page(),
        held = barrier<Response>()
      await openDirty()
      await set('新凭据（仅写入）', 'generated-only')
      let navigation: Promise<unknown> | undefined
      if (kind === 'close') await click('取消', dialog())
      else if (kind === 'route') {
        navigation = f.router.push('/system/users')
        await flushPromises()
      } else await click('退出登录')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
      expect(dialog().textContent).toContain('放弃未保存修改')
      expect(dialog().inert).toBe(false)
      f.setSessionRequest(() => held.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(f.auth.state.phase).toBe('checking')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      held.resolve(problem('INTERNAL_ERROR'))
      await flushPromises()
      const recovery = button('检查当前会话')
      expect(recovery.closest('[inert]')).toBeNull()
      expect(recovery.disabled).toBe(false)
      f.resetSession()
      await click('检查当前会话')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
      const top = dialog()
      expect(top.inert).toBe(false)
      expect(top.getAttribute('aria-hidden')).toBeNull()
      expect(field('名称').value).toBe('Draft Provider')
      expect(field('新凭据（仅写入）').value).toBe('')
      expect(f.auth.system.providers.material.present).toBe(true)
      expect(f.writes()).toHaveLength(0)
      await click('继续编辑', top)
      if (navigation) await navigation
      expect(f.router.currentRoute.value.path).toBe('/system/providers')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(f.writes()).toHaveLength(0)
      await click('取消', dialog())
      await click('放弃修改', dialog())
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.auth.system.providers.material.present).toBe(false)
    },
  )
  it.each(['new-session', 'lost-role', 'dispose'] as const)(
    'pending confirmation is settled false on %s, never carried into replacement identity',
    async (kind) => {
      const f = await page()
      await openDirty()
      const navigation = f.router.push('/system/users')
      await flushPromises()
      if (kind === 'dispose') {
        f.wrapper.unmount()
        wrapper = undefined
      } else {
        f.setSession({
          user: { ...user, ...(kind === 'lost-role' ? { role: 'user', version: '2' } : {}) },
          session: {
            id: kind === 'new-session' ? id(3) : id(2),
            issued_at: time,
            idle_expires_at: time,
            absolute_expires_at: time,
          },
          csrf_token: 'S'.repeat(43),
        })
        await f.auth.restore()
        await flushPromises()
      }
      const result = await navigation
      expect(isNavigationFailure(result, NavigationFailureType.aborted)).toBe(true)
      // Vue Router resets its location when the last App is unmounted. The
      // aborted pending navigation, not that reset location, proves false.
      if (kind !== 'dispose') expect(f.router.currentRoute.value.path).toBe('/system/providers')
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.auth.system.providers.material.present).toBe(false)
    },
  )
})
