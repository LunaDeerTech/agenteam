import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, isNavigationFailure } from 'vue-router'
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
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import { createSystemAccountSecurityAPI } from '../api/system-account-security'
import { createSystemSMTPSettingsAPI } from '../api/system-smtp-settings'
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemSMTPSettingsView from '../views/system/SystemSMTPSettingsView.vue'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const sessionView = (): SessionView => ({
  user: {
    id: id(1),
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Admin',
    role: 'admin',
    theme: 'dark',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const settings = (version = '1', configured = true) => ({
  id: id(10),
  version,
  configured,
  host: configured ? 'mail.example' : '',
  port: configured ? 587 : 0,
  encryption: configured ? 'starttls' : '',
  username: '',
  sender_email: configured ? 'sender@example.test' : '',
  sender_name: '',
  credential_present: false,
  auto_retry_count: '3',
  retry_interval_seconds: '60',
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
      detail: '<private detail>',
      instance: '/api/v1/system/smtp',
      request_id: id(9),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((done) => (resolve = done))
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
  // DOM/state evidence. Native focus, pointer defaults and layout remain browser checks.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  selected.auth?.leave()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})
async function page(
  options: {
    path?: string
    role?: 'admin' | 'user'
    anonymous?: boolean
    missing?: boolean
    configured?: boolean
  } = {},
) {
  let current = sessionView(),
    signedIn = !options.anonymous,
    observed = settings('1', options.configured ?? true)
  current = { ...current, user: { ...current.user, role: options.role ?? 'admin' } }
  let session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
  let read: Fetch = async () => (options.missing ? problem('NOT_FOUND', 404) : json(observed))
  let mutation: Fetch = async (path, init) => {
    const input = JSON.parse(init.body as string)
    const configured = !path.endsWith('/unconfigure')
    observed = {
      ...settings(String(BigInt(input.version) + 1n), configured),
      ...(configured
        ? {
            host: input.host.toLowerCase().replace(/\.$/, ''),
            port: input.port,
            encryption: input.encryption,
            username: input.username,
            sender_email: input.sender_email.toLowerCase(),
            sender_name: input.sender_name,
            credential_present:
              input.credential_action !== 'remove' &&
              (!!input.password || observed.credential_present),
          }
        : {}),
      auto_retry_count: input.auto_retry_count,
      retry_interval_seconds: input.retry_interval_seconds,
    }
    return json({ applied_version: String(BigInt(input.version) + 1n), settings: observed })
  }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === '/api/v1/system/smtp' || path === '/api/v1/system/smtp/unconfigure')
      return init.method === 'GET' ? read(path, init) : mutation(path, init)
    if (path.startsWith('/api/v1/system/users?')) return json({ items: [] })
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
    throw new Error('Unexpected test route')
  })
  selected.auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    createSystemModelSelectionAPI(fetch),
    createSystemAccountSecurityAPI(fetch),
    createSystemSMTPSettingsAPI(fetch),
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
          { path: 'smtp', component: SystemSMTPSettingsView },
          ...['invitations', 'providers', 'models', 'model-selection', 'account-security'].map(
            (path) => ({
              path,
              component: { template: '<p>Accepted leaf outside this interaction</p>' },
            }),
          ),
        ],
      },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(options.path ?? '/system/smtp')
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
    setRead(value: Fetch) {
      read = value
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
      session = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
    },
    writes() {
      return fetch.mock.calls.filter(
        ([path, init]) => path.startsWith('/api/v1/system/smtp') && init.method !== 'GET',
      )
    },
  }
}
function button(label: string, root: ParentNode = document) {
  const value = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) =>
      (
        node.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? node.textContent
      )?.trim() === label || node.getAttribute('aria-label') === label,
  )
  expect(value, `button ${label}`).toBeTruthy()
  return value!
}
async function click(label: string, root: ParentNode = document) {
  button(label, root).click()
  await flushPromises()
}
function field(label: string) {
  const node = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (value) => value.textContent?.replace(/\s*\*$/, '') === label,
  )
  expect(node, `field ${label}`).toBeTruthy()
  return document.getElementById(node!.htmlFor) as HTMLInputElement
}
async function change(label: string, value: string) {
  const input = field(label)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function save(label = '保存 SMTP 配置') {
  const submitter = button(label)
  submitter.form!.dispatchEvent(
    new SubmitEvent('submit', { bubbles: true, cancelable: true, submitter }),
  )
  await flushPromises()
}
function dialogs(count = 1) {
  const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  expect(layers).toHaveLength(count)
  for (const layer of layers.slice(0, -1)) {
    expect(layer.inert).toBe(true)
    expect(layer.getAttribute('aria-hidden')).toBe('true')
  }
  const top = layers.at(-1)!
  expect(top.inert).toBe(false)
  expect(top.getAttribute('aria-hidden')).toBeNull()
  return { top, layers }
}
async function chooseEncryption(label: string) {
  await click('加密方式')
  await click(label)
}

describe('SMTP inline form and two persistent confirmation hosts in the actual App', () => {
  it('keeps SMTP as the eighth leaf and eleventh return within nine leaves, four groups and thirteen returns', async () => {
    const f = await page({ path: '/system', configured: false })
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(
      f.wrapper
        .get('nav[aria-label="系统设置"]')
        .findAll('a')
        .map((a) => a.text()),
    ).toEqual([
      '用户',
      '待注册邀请',
      'Providers',
      'Models',
      '平台模型用途',
      '系统审计',
      '账号安全',
      'SMTP',
      '出站规则',
    ])
    expect(f.wrapper.findAll('.settings-group-toggle').map((g) => g.text())).toEqual([
      '用户与邀请',
      '模型与提供商',
      '审计',
      '平台配置',
    ])
    await f.wrapper.get('a[href="/system/smtp"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('SMTP')
    expect(button('配置').getAttribute('aria-pressed')).toBe('true')
    expect(button('测试与任务').getAttribute('aria-pressed')).toBe('false')
    expect(f.wrapper.find('[aria-label="SMTP 测试与任务"]').exists()).toBe(false)
    expect(f.wrapper.findAll('form')).toHaveLength(1)
    expect(f.wrapper.findAll('input')).toHaveLength(2)
    expect(f.wrapper.text()).toContain('后台恢复链接由授权运维获取并转交')
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(button('保存重试策略').disabled).toBe(true)
    expect(f.writes()).toHaveLength(0)
    expect(safeReturnTarget('/system/smtp')).toBe('/system/smtp')
    for (const target of [
      '/system/smtp?x=1',
      '/system/smtp#x',
      '/system/smtp/',
      '/system/smtp/extra',
      ['/system/smtp'],
      '//evil/system/smtp',
      'https://evil/system/smtp',
    ])
      expect(safeReturnTarget(target)).toBe('/')
    const { router } = await import('../router/index')
    expect(router.resolve('/system/smtp').meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(f.fetch.mock.calls.some(([p]) => /lookup|test|jobs/.test(p))).toBe(false)
  })
  it('mounts no SMTP data or form for a regular user', async () => {
    const f = await page({ role: 'user' })
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.fetch.mock.calls.some(([p]) => p.startsWith('/api/v1/system/'))).toBe(false)
  })
  it('returns through real login and never invents settings after an unavailable singleton', async () => {
    const f = await page({ anonymous: true, missing: true })
    expect(f.router.currentRoute.value.query.return).toBe('/system/smtp')
    await f.wrapper.get('#login-email').setValue('admin@example.com')
    await f.wrapper.get('#login-password').setValue('Original password 123')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/system/smtp')
    expect(f.wrapper.text()).toContain('SMTP 配置读取失败')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })
  it('uses the policy action while unconfigured, then requires explicit complete transport fields without defaults', async () => {
    const f = await page({ configured: false })
    await change('自动重试次数', '0')
    await save('保存重试策略')
    expect(f.writes()).toHaveLength(1)
    expect(f.writes()[0]![0]).toBe('/api/v1/system/smtp/unconfigure')
    expect(f.wrapper.text()).toContain('重试策略已保存')
    expect(button('重试策略已保存').getAttribute('aria-label')).toBe('重试策略已保存')
    await click('配置 SMTP')
    expect(field('SMTP 主机').value).toBe('')
    expect(field('端口').value).toBe('')
    expect(field('发件邮箱').value).toBe('')
    expect(button('保存 SMTP 配置').disabled).toBe(true)
    await change('SMTP 主机', 'MAIL.Example.')
    await change('端口', '0587')
    await change('发件邮箱', 'Sender@Example.Test')
    await change('发件显示名', ' Sender ')
    await chooseEncryption('无加密（none）')
    expect(field('端口').getAttribute('aria-invalid')).toBe('true')
    await save()
    expect(f.writes()).toHaveLength(1)
    await change('端口', '587')
    expect(button('保存 SMTP 配置').disabled).toBe(false)
    await save()
    expect(f.writes()).toHaveLength(2)
    expect(JSON.parse(f.writes()[1]![1].body as string)).toMatchObject({
      version: '2',
      encryption: 'none',
      host: 'MAIL.Example.',
      sender_name: ' Sender ',
      auto_retry_count: '0',
    })
    expect(f.wrapper.text()).toContain('未验证连接或发送')
  })
  it('does not store material in the ordinary reactive draft or DOM attributes and supports explicit clearing', async () => {
    const f = await page()
    await change('认证用户名', 'auth')
    await change('新密码（仅写入）', ' private🧩 ')
    expect(f.auth.system.smtp.material.present).toBe(true)
    expect(f.wrapper.html().includes('private🧩')).toBe(false)
    expect(JSON.stringify(f.auth.system.smtp).includes('private🧩')).toBe(false)
    expect(localStorage.length + sessionStorage.length).toBe(0)
    expect(button('保存 SMTP 配置').disabled).toBe(false)
    await click('清除新密码输入')
    expect(field('新密码（仅写入）').value).toBe('')
    expect(button('保存 SMTP 配置').disabled).toBe(true)
    await change('新密码（仅写入）', 'x'.repeat(2049))
    expect(field('新密码（仅写入）').getAttribute('aria-invalid')).toBe('true')
    await save()
    expect(f.writes()).toHaveLength(0)
  })
  it.each(['route', 'logout'] as const)(
    'restores a pending %s discard after checking/503 without restoring private input or focusing an old heading',
    async (action) => {
      const f = await page(),
        oldHeading = f.wrapper.get('h1').element
      await change('认证用户名', 'auth')
      await change('新密码（仅写入）', ' retained ')
      const pending = action === 'route' ? f.router.push('/system/users') : null
      if (action === 'logout') button('退出登录').click()
      await flushPromises()
      dialogs()
      const held = barrier<Response>()
      f.setSessionRequest(() => held.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(f.wrapper.find('.session-check').exists()).toBe(true)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(document.getElementById('app')!.inert).toBe(false)
      expect(document.documentElement.style.overflow).toBe('')
      held.resolve(problem('UNAVAILABLE'))
      await flushPromises()
      expect(button('检查当前会话').disabled).toBe(false)
      f.resetSession()
      await click('检查当前会话')
      const top = dialogs().top,
        currentHeading = f.wrapper.get('h1').element
      expect(top.contains(document.activeElement)).toBe(true)
      expect(field('新密码（仅写入）').value).toBe('')
      expect(f.auth.system.smtp.material.present).toBe(true)
      expect(f.wrapper.text()).toContain('已保留新密码输入')
      expect(oldHeading.isConnected).toBe(false)
      await click('继续编辑', top)
      expect(document.activeElement).toBe(currentHeading)
      if (pending) expect(isNavigationFailure(await pending)).toBe(true)
      expect(f.writes()).toHaveLength(0)
      expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/logout'))).toHaveLength(0)
      expect(button('保存 SMTP 配置').disabled).toBe(false)
      await save()
      expect(f.writes()).toHaveLength(1)
      expect(JSON.parse(f.writes()[0]![1].body as string).password === ' retained ').toBe(true)
      expect(f.auth.system.smtp.material.present).toBe(false)
    },
  )
  it.each([false, true])(
    'keeps the native disable trigger enabled through its dirty=%s confirmation and cancellation',
    async (dirty) => {
      const f = await page()
      if (dirty) await change('自动重试次数', '0')
      const trigger = button('停用并清除配置')
      trigger.focus()
      await click('停用并清除配置')
      const top = dialogs().top
      expect(trigger.disabled).toBe(false)
      expect(document.getElementById('app')!.inert).toBe(true)
      expect(top.contains(document.activeElement)).toBe(true)
      await click(dirty ? '继续编辑' : '取消停用', top)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(document.activeElement).toBe(trigger)
      expect(trigger.disabled).toBe(false)
      expect(f.writes()).toHaveLength(0)
      if (dirty) expect(field('自动重试次数').value).toBe('0')
    },
  )
  it('keeps both Dialog instances in order through Session recovery and lets only the top confirmation respond', async () => {
    const f = await page()
    await click('停用并清除配置')
    dialogs()
    const pending = f.router.push('/system/users')
    await flushPromises()
    const original = dialogs(2)
    expect(original.top.textContent).toContain('放弃未保存修改')
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    const restored = dialogs(2)
    expect(restored.top.contains(document.activeElement)).toBe(true)
    await click('继续编辑', restored.top)
    expect(isNavigationFailure(await pending)).toBe(true)
    const lower = dialogs().top
    expect(lower.contains(document.activeElement)).toBe(true)
    await click('取消停用', lower)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    expect(f.writes()).toHaveLength(0)
    await click('停用并清除配置')
    const leave = f.router.push('/system/users')
    await flushPromises()
    await click('放弃修改', dialogs(2).top)
    await leave
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(f.writes()).toHaveLength(0)
  })
  it('captures saved retry policy after a real dirty discard, cancels with zero write, and confirms unconfigure explicitly', async () => {
    const f = await page()
    await change('自动重试次数', '0')
    await click('停用并清除配置')
    await click('放弃修改', dialogs().top)
    const lower = dialogs().top
    expect(lower.textContent).toContain('保留自动重试 3 次')
    await click('取消停用', lower)
    expect(f.writes()).toHaveLength(0)
    await click('停用并清除配置')
    await click('确认停用并清除', dialogs().top)
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
      version: '1',
      auto_retry_count: '3',
      retry_interval_seconds: '60',
    })
    expect(f.wrapper.text()).toContain('SMTP 已停用')
    expect(button('保存重试策略').disabled).toBe(true)
  })
  it('retains a conflict draft and material until the real explicit adopt decision', async () => {
    const f = await page()
    await change('认证用户名', 'auth')
    await change('新密码（仅写入）', 'original')
    f.setMutation(async () => problem('VERSION_CONFLICT', 409))
    await save()
    expect(f.wrapper.text()).toContain('需要核对最新配置')
    expect(field('新密码（仅写入）').value).toBe('')
    expect(f.auth.system.smtp.material.present).toBe(true)
    f.setRead(async () => json({ ...settings('3'), host: 'new.example' }))
    await click('读取最新配置')
    expect(field('SMTP 主机').value).toBe('mail.example')
    expect(f.wrapper.get('form p code').text()).toBe('1')
    await click('采用最新值重新编辑')
    await click('放弃修改', dialogs().top)
    expect(field('SMTP 主机').value).toBe('new.example')
    expect(f.wrapper.get('form p code').text()).toBe('3')
    expect(f.auth.system.smtp.material.present).toBe(false)
    expect(f.writes()).toHaveLength(1)
  })
  it('uses precise success feedback, clears it for immediate new editing and separates later GET failure from confirmed applied version', async () => {
    const f = await page()
    await change('发件显示名', 'first')
    const before = f.fetch.mock.calls.length
    await save()
    expect(f.fetch.mock.calls.slice(before).map(([p, i]) => [p, i.method])).toEqual([
      ['/api/v1/system/smtp', 'PUT'],
    ])
    expect(button('SMTP 配置已保存').getAttribute('aria-label')).toBe('SMTP 配置已保存')
    await change('发件显示名', 'second')
    expect(button('保存 SMTP 配置').getAttribute('aria-label')).toBeNull()
    expect(f.writes()).toHaveLength(1)
    await click('取消修改')
    await click('放弃修改', dialogs().top)
    f.setRead(async () => problem('UNAVAILABLE'))
    await click('读取当前配置')
    expect(f.wrapper.text()).toContain('原操作已确认，当前配置读取失败')
    expect(f.wrapper.text()).toContain('applied_version：2')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.writes()).toHaveLength(1)
  })
  it('performs only explicit original replay after checking and accepts historical success with opposite current state', async () => {
    const f = await page()
    await change('认证用户名', 'auth')
    await change('新密码（仅写入）', 'retained')
    f.setMutation(async () => problem('UNAVAILABLE'))
    await save()
    expect(f.wrapper.text()).toContain('请求结果未确认')
    expect(f.auth.system.smtp.material.present).toBe(true)
    const first = f.writes()[0]![1],
      before = f.fetch.mock.calls.length
    f.setRead(async () => json(settings('4', false)))
    await click('检查当前会话与配置')
    expect(f.fetch.mock.calls.slice(before).map(([p, i]) => [p, i.method])).toEqual([
      ['/api/v1/session', 'GET'],
      ['/api/v1/system/smtp', 'GET'],
    ])
    expect(f.writes()).toHaveLength(1)
    expect(field('新密码（仅写入）').value).toBe('')
    f.setMutation(async () => json({ applied_version: '2', settings: settings('4', false) }))
    await click('重试原请求')
    const next = f.writes()[1]![1]
    expect(
      next.body === first.body && JSON.stringify(next.headers) === JSON.stringify(first.headers),
    ).toBe(true)
    expect(f.wrapper.text()).toContain('原操作已确认，当前配置已变化')
    expect(f.wrapper.text()).toContain('applied_version：2')
    expect(f.auth.system.smtp.material.present).toBe(false)
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false)
  })
  it('settles all pending old decisions false and destroys material for a same-user new Session', async () => {
    const f = await page()
    await change('认证用户名', 'auth')
    await change('新密码（仅写入）', 'old')
    const pending = f.router.push('/system/users')
    await flushPromises()
    dialogs()
    const next = sessionView()
    f.setSession({ ...next, session: { ...next.session, id: id(3) } })
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(isNavigationFailure(await pending)).toBe(true)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(f.auth.system.smtp.material.present).toBe(false)
    expect(field('认证用户名').value).toBe('')
    expect(field('新密码（仅写入）').value).toBe('')
    expect(f.writes()).toHaveLength(0)
  })
})
