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
import { createSystemOutboundPolicy } from '../composables/useSystemOutboundPolicy'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import { createSystemAccountSecurityAPI } from '../api/system-account-security'
import { createSystemSMTPSettingsAPI } from '../api/system-smtp-settings'
import { createSystemSMTPDeliveryAPI } from '../api/system-smtp-delivery'
import { createSystemOutboundPolicyAPI, type OutboundPolicy } from '../api/system-outbound-policy'
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemSMTPSettingsView from '../views/system/SystemSMTPSettingsView.vue'
import SystemOutboundPolicyView from '../views/system/SystemOutboundPolicyView.vue'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const view = (): SessionView => ({
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
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const settings = (configured = true) => ({
  id: id(10),
  version: '1',
  configured,
  host: configured ? 'mail.example' : '',
  port: configured ? 25 : 0,
  encryption: configured ? 'none' : '',
  username: '',
  sender_email: configured ? 'sender@example.test' : '',
  sender_name: '',
  credential_present: false,
  auto_retry_count: '0',
  retry_interval_seconds: '10',
})
const endpoint = '/api/v1/system/outbound-policy'
const policy = (version = '1'): OutboundPolicy => ({
  version,
  rules: [{ cidr: '10.0.0.0/8', ports: [443], allow_http: false }],
})
const receipt = (version = '2', count = '1') => ({
  version,
  rule_count: count,
  audit_id: id(40),
  created_at: time,
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
      detail: '<private error>',
      instance: endpoint,
      request_id: id(9),
      commit_state: 'not_started',
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
const disposers: (() => void)[] = []
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
  // jsdom DOM/state only; native pointer defaults, Tab and geometry remain browser checks.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  for (const dispose of disposers.splice(0)) dispose()
  selected.auth?.leave()
  selected.auth = null
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})
async function fixture(initial = view()) {
  let current = initial,
    session: Fetch = async () => json(current),
    configuration: Fetch = async () => json(settings())
  let currentPolicy = policy(),
    outbound: Fetch = async (_, init) => {
      if (init.method === 'GET') return json(currentPolicy)
      const body = JSON.parse(init.body as string)
      const version = String(BigInt(body.expected_version) + 1n)
      currentPolicy = { version, rules: body.rules }
      return json(receipt(version, String(body.rules.length)))
    }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === endpoint) return outbound(path, init)
    if (path.includes('/mail-jobs/')) return json({ items: [] })
    if (path.startsWith('/api/v1/system/smtp')) return configuration(path, init)
    if (path.startsWith('/api/v1/system/users?')) return json({ items: [] })
    if (path.endsWith('/logout')) {
      session = async () => problem('SESSION_REVOKED', 401)
      return new Response(null, { status: 204 })
    }
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    throw new Error('Unexpected controlled route')
  })
  const auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    createSystemModelSelectionAPI(fetch),
    createSystemAccountSecurityAPI(fetch),
    createSystemSMTPSettingsAPI(fetch),
    createSystemSMTPDeliveryAPI(fetch),
    createSystemOutboundPolicyAPI(fetch),
  )
  selected.auth = auth
  disposers.push(() => auth.leave())
  await auth.restore()
  return {
    auth,
    fetch,
    setOutbound(value: Fetch) {
      outbound = value
    },
    setConfiguration(value: Fetch) {
      configuration = value
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
    writes() {
      return fetch.mock.calls.filter(([p, i]) => p === endpoint && i.method === 'PUT')
    },
    reads() {
      return fetch.mock.calls.filter(([p, i]) => p === endpoint && i.method === 'GET')
    },
  }
}
async function controller() {
  const f = await fixture(),
    page = createSystemOutboundPolicy(f.auth)
  disposers.push(page.dispose)
  page.afterNavigation('/system/outbound-policy', '')
  page.attach()
  await flushPromises()
  return { ...f, page }
}
async function app(initial = view(), path = '/system/outbound-policy') {
  const f = await fixture(initial)
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
          { path: 'outbound-policy', component: SystemOutboundPolicyView },
          ...['invitations', 'providers', 'models', 'model-selection', 'account-security'].map(
            (path) => ({ path, component: { template: '<p>Accepted leaf</p>' } }),
          ),
        ],
      },
    ],
  })
  installAuthentication(router, f.auth)
  await router.push(path)
  await router.isReady()
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  wrapper = mount(App, { attachTo: host, global: { plugins: [router] } })
  await flushPromises()
  return { ...f, router, wrapper }
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
  const node = field(label)
  node.value = value
  node.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function submit(label = '保存规则') {
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
  return { layers, top }
}

describe('Outbound page early real App navigation boundaries', () => {
  it('leaves an inactive clean domain without reading outbound or disrupting SMTP sections', async () => {
    const f = await app(view(), '/system/smtp')
    expect(f.reads()).toHaveLength(0)
    expect(f.wrapper.find('[aria-label="SMTP 测试与任务"]').exists()).toBe(false)
    expect(await f.router.push('/settings/profile')).toBeUndefined()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/settings/profile')
    await click('退出登录')
    expect(f.router.currentRoute.value.path).toBe('/login')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/logout'))).toHaveLength(1)
    expect(f.reads()).toHaveLength(0)
  })

  it('allows a new ordinary-user logout through the real App without any system write', async () => {
    const ordinary = view()
    ordinary.user.role = 'user'
    const f = await app(ordinary)
    expect(f.wrapper.get('h1').text()).toBe('无权访问系统设置')
    expect(f.reads()).toHaveLength(0)
    await click('退出登录')
    expect(f.router.currentRoute.value.path).toBe('/login')
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.auth.system.outboundPolicy.progress).toBeNull()
    expect(button('登录').disabled).toBe(false)
    expect(f.writes()).toHaveLength(0)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/logout'))).toHaveLength(1)
  })

  it.each(['ordinary', 'current403'] as const)(
    'allows a fresh route from a stable clean %s identity',
    async (kind) => {
      const current = view()
      if (kind === 'ordinary') current.user.role = 'user'
      const f = await app(current)
      if (kind === 'current403') {
        f.setOutbound(async () => problem('FORBIDDEN', 403))
        await click('读取当前规则')
        expect(f.auth.system.denied).toBe(true)
      }
      expect(f.wrapper.get('h1').text()).toBe('无权访问系统设置')
      expect(await f.router.push('/settings/profile')).toBeUndefined()
      await flushPromises()
      expect(f.router.currentRoute.value.path).toBe('/settings/profile')
      expect(f.writes()).toHaveLength(0)
      const count = f.reads().length
      await flushPromises()
      expect(f.reads()).toHaveLength(count)
    },
  )

  it('reaches login from a fresh public current-state check after 401 and bootstrap, without replaying', async () => {
    const f = await app()
    await change('规则 1 指定端口', '8443')
    f.setOutbound(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await submit()
    expect(f.auth.system.outboundPolicy.progress?.phase).toBe('uncertain')
    expect(f.writes()).toHaveLength(1)
    const before = f.fetch.mock.calls.length
    f.setSessionRequest(async () => problem('SESSION_REVOKED', 401))
    await click('检查当前会话与规则')
    expect(f.fetch.mock.calls.slice(before, before + 2).map(([path]) => path)).toEqual([
      '/api/v1/session',
      '/api/v1/auth/bootstrap',
    ])
    expect(f.router.currentRoute.value.path).toBe('/login')
    expect(f.auth.state.phase).toBe('anonymous')
    expect(f.auth.system.outboundPolicy.progress).toBeNull()
    expect(f.writes()).toHaveLength(1)
    for (const selector of ['#login-email', '#login-password']) {
      const input = f.wrapper.get<HTMLInputElement>(selector).element
      expect(input.disabled || input.readOnly || input.value !== '').toBe(false)
    }
    expect(button('登录').disabled).toBe(false)
  })

  it.each(['route', 'logout'] as const)(
    'preserves an old %s decision across checking/503 and same-identity recovery',
    async (action) => {
      const f = await app()
      await change('规则 1 指定端口', '8443')
      const pending = action === 'route' ? f.router.push('/settings/profile') : null
      if (action === 'logout') button('退出登录').click()
      await flushPromises()
      dialogs()
      const held = barrier<Response>()
      f.setSessionRequest(() => held.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(f.auth.personalContext.phase).toBe('checking')
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(document.querySelector('#app')?.getAttribute('aria-hidden')).toBeNull()
      held.resolve(problem('DEPENDENCY_UNAVAILABLE', 503))
      await flushPromises()
      expect(button('检查当前会话').disabled).toBe(false)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      f.resetSession()
      await click('检查当前会话')
      const top = dialogs().top
      expect(field('规则 1 指定端口').value).toBe('8443')
      await click('继续编辑', top)
      if (pending) expect(isNavigationFailure(await pending)).toBe(true)
      expect(f.router.currentRoute.value.path).toBe('/system/outbound-policy')
      expect(f.writes()).toHaveLength(0)
      expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/logout'))).toHaveLength(0)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      const next = f.router.push('/settings/profile')
      await flushPromises()
      await click('放弃修改', dialogs().top)
      await next
      expect(f.router.currentRoute.value.path).toBe('/settings/profile')
    },
  )

  it.each(['new-session', 'role-loss', 'anonymous'] as const)(
    'never gives an old pending decision new clean authority after %s',
    async (kind) => {
      const f = await app()
      await change('规则 1 指定端口', '8443')
      const pending = f.router.push('/settings/profile')
      await flushPromises()
      dialogs()
      if (kind === 'anonymous') f.setSessionRequest(async () => problem('SESSION_REVOKED', 401))
      else {
        const next = view()
        if (kind === 'new-session') next.session.id = id(3)
        else {
          next.user.role = 'user'
          next.user.version = '2'
        }
        f.setSession(next)
      }
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(isNavigationFailure(await pending)).toBe(true)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(f.writes()).toHaveLength(0)
      expect(f.router.currentRoute.value.path).toBe(
        kind === 'anonymous' ? '/login' : '/system/outbound-policy',
      )
      if (kind === 'new-session') expect(field('规则 1 指定端口').value).toBe('443')
    },
  )
})

describe('Outbound drafts, reads and historical command results', () => {
  it('uses explicit row defaults, rejects invalid fields without PUT and captures sorted numeric ports', async () => {
    const f = await controller(),
      p = f.page
    expect(p.changed.value).toBe(false)
    const added = p.addRule()!
    expect(p.draft.find((row) => row.id === added)).toMatchObject({
      cidr: '',
      mode: 'specific',
      ports: '',
      allow_http: false,
    })
    expect(p.canSave.value).toBe(false)
    await p.save()
    expect(f.writes()).toHaveLength(0)
    p.updateRule(added, { cidr: 'fd00::/8', ports: '443 80' })
    expect(p.validation.value.errors[added]?.ports).toBeTruthy()
    p.updateRule(added, { ports: '0443' })
    expect(p.canSave.value).toBe(false)
    p.updateRule(added, { ports: '8443, 443', allow_http: true })
    expect(p.canSave.value).toBe(true)
    await p.save()
    expect(f.writes()).toHaveLength(1)
    expect(JSON.parse(f.writes()[0]![1].body as string)).toEqual({
      expected_version: '1',
      rules: [policy().rules[0], { cidr: 'fd00::/8', ports: [443, 8443], allow_http: true }],
    })
    expect(p.editor.version).toBe('2')
    expect(p.changed.value).toBe(false)
    expect(p.dirty.value).toBe(false)
    p.updateRule(p.draft[1]!.id, { mode: 'all', ports: '' })
    expect(p.validation.value.rules?.[1]?.ports).toBe('all')
    expect(p.feedback.value).toBe('idle')
  })

  it('retains submitted draft and last observation after receipt plus failed GET, and retries only the read', async () => {
    const f = await controller(),
      p = f.page
    p.updateRule(p.draft[0]!.id, { ports: '8443' })
    f.setOutbound(async (_, init) =>
      init.method === 'PUT' ? json(receipt()) : problem('DEPENDENCY_UNAVAILABLE', 503),
    )
    await p.save()
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.observation.phase).toBe('error')
    expect(p.observation.value?.version).toBe('1')
    expect(p.draft[0]?.ports).toBe('8443')
    expect(p.editor.version).toBe('1')
    expect(p.state.requiresRead).toBe(true)
    expect(p.canSave.value).toBe(false)
    const reads = f.reads().length
    const retry = p.refresh()
    expect(p.confirmation.open).toBe(false)
    await retry
    expect(f.reads()).toHaveLength(reads + 1)
    expect(f.writes()).toHaveLength(1)
    expect(p.draft[0]?.ports).toBe('8443')
    f.setOutbound(async () => json(policy('19')))
    await p.refresh()
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.observation.value?.version).toBe('19')
    expect(p.editor.version).toBe('19')
    expect(p.changed.value).toBe(false)
  })

  it('keeps current version 19 separate from original receipt 2 and original bytes', async () => {
    const f = await controller(),
      p = f.page
    p.updateRule(p.draft[0]!.id, { ports: '8443' })
    f.setOutbound(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await p.save()
    const original = f.writes()[0]![1]
    f.setOutbound(async (_, init) => (init.method === 'PUT' ? json(receipt()) : json(policy('19'))))
    await p.checkOriginal()
    expect(p.observation.value?.version).toBe('19')
    expect(p.editor.version).toBe('1')
    expect(p.state.confirmed).toBeNull()
    expect(p.progress.value?.phase).toBe('uncertain')
    const reads = f.reads().length
    await p.retryOriginal()
    expect(f.reads()).toHaveLength(reads + 1)
    expect(p.state.confirmed?.version).toBe('2')
    expect(p.observation.value?.version).toBe('19')
    expect(p.editor.version).toBe('19')
    expect(p.changed.value).toBe(false)
    expect(
      f.writes()[1]![1].body === original.body &&
        JSON.stringify(f.writes()[1]![1].headers) === JSON.stringify(original.headers),
    ).toBe(true)
  })

  it('requires an explicit conflict review and adoption before capturing a new version and key', async () => {
    const f = await controller(),
      p = f.page
    p.updateRule(p.draft[0]!.id, { ports: '8443' })
    f.setOutbound(async (_, init) =>
      init.method === 'PUT' ? problem('VERSION_CONFLICT', 409) : json(policy('2')),
    )
    await p.save()
    expect(p.editor.conflict).toBe(true)
    await p.readLatest()
    expect(p.editor.version).toBe('1')
    expect(p.draft[0]?.ports).toBe('8443')
    expect(f.writes()).toHaveLength(1)
    const adopt = p.adoptLatest()
    expect(p.confirmation.open).toBe(true)
    p.finishConfirmation(true)
    await adopt
    expect(p.editor.version).toBe('2')
    expect(p.draft[0]?.ports).toBe('443')
    p.updateRule(p.draft[0]!.id, { ports: '9443' })
    f.setOutbound(async (_, init) =>
      init.method === 'PUT' ? json(receipt('3')) : json(policy('3')),
    )
    await p.save()
    const writes = f.writes()
    expect(JSON.parse(writes[1]![1].body as string).expected_version).toBe('2')
    expect(
      new Headers(writes[0]![1].headers).get('Idempotency-Key') ===
        new Headers(writes[1]![1].headers).get('Idempotency-Key'),
    ).toBe(false)
  })

  it('retires a cancelled read synchronously, retains the last valid view and waits for actual cancel', async () => {
    const f = await controller(),
      p = f.page,
      tail = barrier(),
      cancel = vi.fn(() => tail.promise)
    f.setOutbound(
      async () =>
        new Response(new ReadableStream<Uint8Array>({ cancel }), {
          headers: { 'Content-Type': 'application/json' },
        }),
    )
    const reading = p.readLatest()
    await flushPromises()
    expect(p.observation.phase).toBe('loading')
    p.cancelRead()
    expect(p.observation.phase).toBe('error')
    expect(p.observation.value?.version).toBe('1')
    const count = f.reads().length
    try {
      await reading
      expect(f.auth.state.busy).toBe(true)
      expect(p.canSave.value).toBe(false)
      await p.refresh()
      expect(f.reads()).toHaveLength(count)
    } finally {
      tail.resolve()
      await flushPromises()
    }
    expect(f.auth.state.busy).toBe(false)
    expect(p.observation.phase).toBe('error')
    expect(f.reads()).toHaveLength(count)
    f.setOutbound(async () => json(policy()))
    await p.refresh()
    expect(p.observation.phase).toBe('ready')
    expect(f.reads()).toHaveLength(count + 1)
  })

  it('does not automatically restart a detached first read after its ignored fetch returns', async () => {
    const f = await fixture(),
      held = barrier<Response>(),
      p = createSystemOutboundPolicy(f.auth)
    disposers.push(p.dispose)
    f.setOutbound(() => held.promise)
    p.afterNavigation('/system/outbound-policy', '')
    p.attach()
    await flushPromises()
    p.detach()
    expect(p.observation.phase).toBe('error')
    p.attach()
    try {
      expect(f.auth.state.busy).toBe(true)
      expect(f.reads()).toHaveLength(1)
    } finally {
      held.resolve(json(policy()))
      await flushPromises()
    }
    expect(f.reads()).toHaveLength(1)
    expect(p.observation.phase).toBe('error')
    expect(p.observation.value).toBeNull()
    f.setOutbound(async () => json(policy()))
    await p.refresh()
    expect(f.reads()).toHaveLength(2)
  })

  it('lets an initial never-dispatched read wait once for the existing Session owner', async () => {
    const f = await fixture(),
      held = barrier<Response>()
    f.setSessionRequest(() => held.promise)
    const restored = f.auth.restore()
    const p = createSystemOutboundPolicy(f.auth)
    disposers.push(p.dispose)
    p.afterNavigation('/system/outbound-policy', '')
    p.attach()
    await flushPromises()
    expect(f.reads()).toHaveLength(0)
    held.resolve(json(view()))
    await restored
    await flushPromises()
    expect(f.reads()).toHaveLength(1)
    expect(p.observation.phase).toBe('ready')
    await flushPromises()
    expect(f.reads()).toHaveLength(1)
  })

  it('retains an independent unknown request while cancelling its explicit observation', async () => {
    const f = await controller(),
      p = f.page,
      tail = barrier(),
      cancel = vi.fn(() => tail.promise)
    p.updateRule(p.draft[0]!.id, { ports: '8443' })
    f.setOutbound(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await p.save()
    const original = f.writes()[0]![1]
    f.setOutbound(
      async () =>
        new Response(new ReadableStream<Uint8Array>({ cancel }), {
          headers: { 'Content-Type': 'application/json' },
        }),
    )
    const checked = p.checkOriginal()
    await flushPromises()
    p.cancelRead()
    expect(p.observation.phase).toBe('error')
    try {
      await checked
      expect(p.progress.value?.phase).toBe('uncertain')
      expect(p.progress.value?.canRetryOriginal).toBe(false)
    } finally {
      tail.resolve()
      await flushPromises()
    }
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(p.progress.value?.canRetryOriginal).toBe(true)
    f.setOutbound(async (_, init) => (init.method === 'PUT' ? json(receipt()) : json(policy('2'))))
    await p.retryOriginal()
    expect(f.writes()[1]![1].body === original.body).toBe(true)
  })

  it('publishes no late receipt or follow-up GET after a real leave invalidates the draft', async () => {
    const f = await controller(),
      p = f.page,
      held = barrier<Response>()
    p.updateRule(p.draft[0]!.id, { ports: '8443' })
    f.setOutbound(() => held.promise)
    const save = p.save()
    await flushPromises()
    p.afterNavigation('/settings/profile', '/system/outbound-policy')
    p.detach()
    const reads = f.reads().length
    held.resolve(json(receipt()))
    await save
    await flushPromises()
    expect(f.reads()).toHaveLength(reads)
    expect(p.state.confirmed).toBeNull()
    expect(p.draft).toHaveLength(0)
  })

  it('reacts to adoption of maximum version and does not grant a new clean leave during failed checking', async () => {
    const f = await controller(),
      p = f.page
    f.setOutbound(async () => json(policy('9223372036854775807')))
    await p.refresh()
    expect(p.maximumVersion.value).toBe(true)
    expect(p.addRule()).toBeNull()
    expect(p.canSave.value).toBe(false)
    f.setSessionRequest(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.personalContext.phase).toBe('checking')
    expect(await p.confirmLeave()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('enforces the 256-row local cap and keeps all an explicit choice', async () => {
    const f = await controller(),
      p = f.page
    for (let i = 1; i < 256; i++) expect(p.addRule()).not.toBeNull()
    expect(p.draft).toHaveLength(256)
    expect(p.addRule()).toBeNull()
    expect(
      p.draft
        .slice(1)
        .every((row) => row.mode === 'specific' && row.ports === '' && !row.allow_http),
    ).toBe(true)
    await p.save()
    expect(f.writes()).toHaveLength(0)
  })
})

describe('Outbound structured View and public navigation composition', () => {
  it('has nine leaves in four groups, preserves users default and validates the twelfth exact return', async () => {
    const f = await app(view(), '/system')
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(
      f.wrapper
        .get('nav[aria-label="系统设置"]')
        .findAll('a')
        .map((node) => node.text()),
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
    expect(f.wrapper.findAll('.settings-group-toggle')).toHaveLength(4)
    expect(safeReturnTarget('/system/outbound-policy')).toBe('/system/outbound-policy')
    for (const value of [
      '/system/outbound-policy?x=1',
      '/system/outbound-policy#x',
      '/system/outbound-policy/',
      '/system/outbound-policy/extra',
      ['/system/outbound-policy'],
    ])
      expect(safeReturnTarget(value)).toBe('/')
    await f.wrapper.get('a[href="/system/outbound-policy"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('出站规则')
    expect(f.reads()).toHaveLength(1)
    expect(f.writes()).toHaveLength(0)
  })

  it('adds and removes local rows with local focus, and requires an explicit all selection', async () => {
    const f = await app()
    await click('添加规则')
    expect(document.activeElement).toBe(field('规则 2 CIDR'))
    expect(field('规则 2 指定端口').value).toBe('')
    expect(button('保存规则').disabled).toBe(true)
    await change('规则 2 CIDR', 'fd00::/8')
    await click('规则 2 端口范围')
    await click('全部端口（all）')
    expect(button('保存规则').disabled).toBe(false)
    expect(f.writes()).toHaveLength(0)
    await click('删除规则 2')
    expect(document.activeElement).toBe(field('规则 1 CIDR'))
    expect(button('保存规则').disabled).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })

  it('keeps one resident confirmation component and returns to its current local heading after same-Session remount', async () => {
    const f = await app()
    const originalView = f.wrapper.getComponent(SystemOutboundPolicyView)
    const dialogBefore = originalView.findComponent({ name: 'UiDialog' }).vm.$.uid
    await change('规则 1 指定端口', '8443')
    const trigger = button('取消修改')
    trigger.focus()
    await click('取消修改')
    expect(trigger.disabled).toBe(false)
    await click('继续编辑', dialogs().top)
    expect(document.activeElement).toBe(trigger)
    expect(originalView.findComponent({ name: 'UiDialog' }).vm.$.uid).toBe(dialogBefore)
    await click('取消修改')
    dialogs()
    const held = barrier<Response>()
    f.setSessionRequest(() => held.promise)
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(trigger.isConnected).toBe(false)
    expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
    held.resolve(json(view()))
    await flushPromises()
    const heading = f.wrapper.get('h1').element
    const top = dialogs().top
    expect(top.contains(document.activeElement)).toBe(true)
    await click('继续编辑', top)
    expect(document.activeElement).toBe(heading)
    expect(f.writes()).toHaveLength(0)
  })

  it('shows retained confirmed fields through GET failure without inheriting success on the next edit', async () => {
    const f = await app()
    await change('规则 1 指定端口', '8443')
    f.setOutbound(async (_, init) =>
      init.method === 'PUT' ? json(receipt()) : problem('DEPENDENCY_UNAVAILABLE', 503),
    )
    await submit()
    expect(f.wrapper.text()).toContain('保存已确认，当前规则读取失败')
    expect(field('规则 1 指定端口').value).toBe('8443')
    expect(field('规则 1 指定端口').disabled).toBe(true)
    expect(f.wrapper.get('[aria-label="本次保存的历史确认"]').text()).toContain('版本 2')
    f.setOutbound(async () => json(policy('19')))
    await click('读取当前规则')
    expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
    expect(f.writes()).toHaveLength(1)
    await change('规则 1 指定端口', '9443')
    expect(button('保存规则').disabled).toBe(false)
    expect(button('保存规则').getAttribute('aria-label')).not.toBe('出站规则已保存')
    expect(f.writes()).toHaveLength(1)
  })
})
