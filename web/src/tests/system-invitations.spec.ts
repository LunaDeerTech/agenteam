import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
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
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemInvitationsView from '../views/system/SystemInvitationsView.vue'
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
const row = (n = 100) => ({
  id: id(n),
  email: `invite${n}@example.com`,
  version: '2',
  created_at: time,
  expires_at: time,
  latest_delivery: {
    job_id: id(n + 300),
    accepted_at: time,
    phase: 'sent',
    attempts: '1',
    version: '7',
    channel: 'backend_log',
    attempt_result: 'sent',
    reason: 'sent',
  },
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problem = (code: string, status: number, fields = false) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      status,
      code,
      detail: '<script>private raw detail</script>',
      instance: '/api/v1/system/invitations',
      request_id: id(9),
      commit_state: 'not_started',
      ...(fields ? { field_errors: [{ path: '/email', code: 'EMAIL_INVALID' }] } : {}),
    },
    status,
  )
function barrier<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
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
  // jsdom has no layout; browser tests own visibility/geometry and real focus.
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
  path = '/system/invitations',
  role: 'admin' | 'user' = 'admin',
  anonymous = false,
) {
  let current: SessionView = {
    user: { ...user, role },
    session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
  let signedIn = !anonymous,
    list: Fetch = async () => json({ items: [row()] }),
    session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
  let mutation: Fetch = async (p) =>
    p.endsWith('/revoke')
      ? new Response(null, { status: 204 })
      : p.endsWith('/retry')
        ? json({ job_id: id(500), version: '9' }, 202)
        : json({ id: id(100), job_id: id(500), version: '3' }, p.endsWith('/resend') ? 202 : 201)
  const fetch = vi.fn<Fetch>(async (p, init) => {
    if (p === '/api/v1/session') return session(p, init)
    if (p.startsWith('/api/v1/system/invitations?')) return list(p, init)
    if (p.startsWith('/api/v1/system/users?')) return json({ items: [] })
    if (p.startsWith('/api/v1/system/')) return mutation(p, init)
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
    throw new Error('Unexpected private fixture path')
  })
  selected.auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
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
    setMutation(value: Fetch) {
      mutation = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    resetSessionRequest() {
      session = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
    },
    posts() {
      return fetch.mock.calls.filter(
        ([p, init]) => p.startsWith('/api/v1/system/') && init.method === 'POST',
      )
    },
    reads() {
      return fetch.mock.calls.filter(([p]) => p.startsWith('/api/v1/system/invitations?'))
    },
  }
}
function button(label: string, within: ParentNode = document) {
  const buttons = [...within.querySelectorAll<HTMLButtonElement>('button')]
  const b = buttons.find(
    (b) =>
      (
        b.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? b.textContent
      )?.trim() === label,
  )
  if (!b) throw new Error('Missing visible control: ' + label)
  return b
}
const dialog = () =>
  document.querySelectorAll<HTMLElement>('[role="dialog"]')[
    document.querySelectorAll('[role="dialog"]').length - 1
  ]!
async function click(label: string, within?: ParentNode) {
  button(label, within).click()
  await flushPromises()
}
async function email(value: string) {
  const el = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
  el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
function sameSession(): SessionView {
  return {
    user,
    session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
}
async function pageshowChecking(f: Awaited<ReturnType<typeof page>>, response: Promise<Response>) {
  f.setSessionRequest(() => response)
  window.dispatchEvent(new Event('pageshow'))
  await flushPromises()
  expect(f.wrapper.text()).toContain('正在确认会话')
  expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
  expect(document.getElementById('app')!.inert).toBe(false)
  expect(document.body.style.overflow).not.toBe('hidden')
}
function pendingConfirmation() {
  const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  expect(layers).toHaveLength(2)
  const [business, confirmation] = layers
  expect(confirmation!.textContent).toContain('放弃未保存修改')
  expect(confirmation!.getAttribute('aria-hidden')).toBeNull()
  expect(confirmation!.inert).toBe(false)
  expect(business!.inert).toBe(true)
  expect(business!.querySelector<HTMLInputElement>('input')!.value).toBe('pending@example.com')
  return { business: business!, confirmation: confirmation! }
}

describe('real App, router and invitations controller composition', () => {
  it('keeps the original /system destination and opens the second exact menu leaf', async () => {
    const f = await page('/system')
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    await f.wrapper.get('nav[aria-label="系统设置"] a[href="/system/invitations"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('待注册邀请')
    expect(f.wrapper.get('nav[aria-label="系统设置"] [aria-current="page"]').text()).toBe(
      '待注册邀请',
    )
    expect(f.wrapper.findAll('.settings-group-toggle')).toHaveLength(4)
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
    expect(f.reads()).toHaveLength(1)
    expect(f.wrapper.findAll('th').map((n) => n.text())).toEqual([
      '邮箱',
      '创建时间（UTC）',
      '到期时间（UTC）',
      '最近一次投递任务',
      '操作',
    ])
    expect(f.wrapper.findAll('tbody time').map((n) => n.attributes('datetime'))).toEqual([
      time,
      time,
      time,
    ])
    expect(f.wrapper.text()).toContain('后台恢复日志')
    expect(f.wrapper.text()).toContain('不保证已进入收件箱')
    expect(f.wrapper.findAll('tbody a')).toHaveLength(0)
  })
  it('keeps ordinary users denied at the same URL without any invitation request', async () => {
    const f = await page('/system/invitations', 'user')
    expect(f.router.currentRoute.value.path).toBe('/system/invitations')
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(f.wrapper.find('a[href="/system"]').exists()).toBe(false)
    expect(f.reads()).toHaveLength(0)
    expect(f.posts()).toHaveLength(0)
  })
  it('returns from login only to the sixth exact safe leaf', async () => {
    for (const target of [
      '/system',
      '/system/invitations?x=1',
      '/system/invitations#x',
      '/system/mail-jobs',
      '//bad/system/invitations',
      ['/system/invitations'],
    ])
      expect(safeReturnTarget(target)).toBe('/')
    const f = await page('/system/invitations', 'admin', true)
    expect(f.router.currentRoute.value.query.return).toBe('/system/invitations')
    await f.wrapper.get('input[type="email"]').setValue('admin@example.com')
    await f.wrapper.get('input[type="password"]').setValue('Original password 123')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('待注册邀请')
    const { router } = await import('../router')
    expect(router.resolve('/system/invitations').meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
    router.options.history.destroy()
  })
  it('sends raw text email, keeps safe inline business errors, then preserves receipt success after a failed list', async () => {
    const f = await page()
    await click('邀请用户')
    await email(' Name@Example.com ')
    expect(document.querySelector('input')?.getAttribute('type')).toBe('text')
    f.setMutation(async () => problem('INVALID_ARGUMENT', 422, true))
    await click('发送邀请')
    expect(f.posts()[0]![1].body).toBe('{"email":" Name@Example.com "}')
    expect(dialog().textContent).toContain('邮箱格式不正确或已被占用')
    expect(dialog().textContent).not.toContain('private raw')
    await email('new@example.com')
    f.setMutation(async () => json({ id: id(100), job_id: id(500), version: '1' }, 201))
    f.setList(async () => problem('DEPENDENCY_UNAVAILABLE', 503))
    await click('发送邀请')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.wrapper.text()).toContain('邀请请求已接受')
    expect(f.wrapper.text()).toContain('操作已确认，列表读取失败')
    expect(f.wrapper.text()).not.toContain('请求结果未确认')
    expect(f.wrapper.find('table').exists()).toBe(false)
    f.setList(async () => json({ items: [] }))
    await click('重新读取列表')
    expect(f.posts()).toHaveLength(2)
    expect(f.wrapper.text()).toContain('暂无待注册邀请')
  })
  it.each([true, false])(
    'restores a rejected form submission to its actionable submit control (%s)',
    async (explicitSubmitter) => {
      const f = await page(),
        held = barrier<Response>()
      await click('邀请用户')
      await email(' invalid@example.com ')
      const submit = button('发送邀请', dialog())
      submit.focus()
      f.setMutation(() => held.promise)
      submit.form!.dispatchEvent(
        new SubmitEvent('submit', {
          bubbles: true,
          cancelable: true,
          submitter: explicitSubmitter ? submit : null,
        }),
      )
      await flushPromises()
      expect(submit.disabled).toBe(true)
      // Browsers blur a control once disabled; jsdom does not model that step.
      document.body.tabIndex = -1
      document.body.focus()
      document.body.removeAttribute('tabindex')
      expect(document.activeElement).toBe(document.body)
      held.resolve(problem('INVALID_ARGUMENT', 400, true))
      await flushPromises()
      expect(dialog().textContent).toContain('邮箱格式不正确或已被占用')
      expect(submit.disabled).toBe(false)
      expect(document.activeElement).toBe(submit)
    },
  )
  it.each([
    ['create', '邀请用户', '发送邀请', '邀请请求已接受'],
    ['revoke', '撤销邀请', '确认撤销', '撤销邀请'],
    ['retry', '重试投递', '确认重试投递', '重试投递请求已接受'],
  ] as const)(
    'keeps a fresh %s confirmation independent of the previous success feedback',
    async (kind, open, submit, reopen) => {
      const f = await page()
      let rows = [row(100), row(99)].map((item) => ({
        ...item,
        latest_delivery: {
          ...item.latest_delivery,
          phase: 'cancelled',
          channel: null,
          attempt_result: null,
          attempts: '0',
          reason: 'cancelled',
        },
      }))
      f.setList(async () => json({ items: rows }))
      if (kind === 'revoke')
        f.setMutation(async () => {
          rows = rows.slice(1)
          return new Response(null, { status: 204 })
        })
      await click('刷新')
      await click(open)
      if (kind === 'create') await email('first@example.com')
      await click(submit, dialog())
      expect(document.querySelector('[role="dialog"]')).toBeNull()
      expect(f.posts()).toHaveLength(1)
      await click(reopen)
      const next = button(submit, dialog())
      expect(next.disabled).toBe(false)
      expect(next.getAttribute('aria-label')).toBeNull()
      expect(next.getAttribute('aria-busy')).toBeNull()
      expect(f.posts()).toHaveLength(1)
    },
  )
  it('confirms revocation using captured invitation version and cancel sends nothing', async () => {
    const f = await page()
    await click('撤销邀请')
    expect(dialog().textContent).toContain(row().email)
    expect(dialog().textContent).toContain('链接将失效')
    await click('取消', dialog())
    expect(f.posts()).toHaveLength(0)
    await click('撤销邀请')
    f.setList(async () => json({ items: [] }))
    await click('确认撤销')
    expect(f.posts()[0]![0]).toBe(`/api/v1/system/invitations/${id(100)}/revoke`)
    expect(f.posts()[0]![1].body).toBe('{"version":"2"}')
    expect(f.wrapper.text()).toContain('邀请已撤销')
    expect(document.activeElement).toBe(button('邀请用户'))
  })
  it('keeps phase and actual attempt result independent and enables legal cancelled/null retry with job version', async () => {
    const f = await page()
    const value = row()
    f.setList(async () =>
      json({
        items: [
          {
            ...value,
            latest_delivery: {
              ...value.latest_delivery,
              phase: 'retry_wait',
              channel: 'smtp',
              attempt_result: 'unknown',
              reason: 'unknown',
            },
          },
        ],
      }),
    )
    await click('刷新')
    expect(f.wrapper.text()).toContain('等待自动重试')
    expect(f.wrapper.text()).toContain('本次尝试结果')
    expect(f.wrapper.text()).toContain('结果未知')
    expect(button('重试投递').disabled).toBe(true)
    f.setList(async () =>
      json({
        items: [
          {
            ...value,
            latest_delivery: {
              ...value.latest_delivery,
              phase: 'cancelled',
              channel: null,
              attempt_result: null,
              attempts: '0',
            },
          },
        ],
      }),
    )
    await click('刷新')
    expect(button('重试投递').disabled).toBe(false)
    await click('重试投递')
    expect(dialog().textContent).toContain('可能造成重复投递')
    await click('确认重试投递')
    expect(f.posts()[0]![0]).toBe(`/api/v1/system/mail-jobs/${id(400)}/retry`)
    expect(f.posts()[0]![1].body).toBe('{"version":"7"}')
  })
  it('keeps the dirty modal through actual App checking unmount and restores the same email', async () => {
    const f = await page(),
      held = barrier<Response>()
    await click('邀请用户')
    await email('unsaved@example.com')
    f.setSessionRequest(() => held.promise)
    const restore = f.auth.restore()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.wrapper.text()).toContain('正在确认会话')
    held.resolve(
      json({
        user,
        session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
        csrf_token: 'S'.repeat(43),
      }),
    )
    await restore
    await flushPromises()
    expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
      'unsaved@example.com',
    )
    const cancel = button('取消', dialog())
    cancel.focus()
    await click('取消', dialog())
    const overlays = [...document.querySelectorAll('.ui-overlay')]
    expect(overlays).toHaveLength(2)
    expect(overlays.at(-1)?.textContent).toContain('放弃未保存修改')
    await click('继续编辑', dialog())
    expect(document.activeElement).toBe(cancel)
    await click('取消', dialog())
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.activeElement).toBe(cancel)
    await click('取消', dialog())
    await click('放弃修改', dialog())
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('makes recovery available inside an uncertain modal and replays one original command after checking an empty list', async () => {
    const f = await page()
    await click('邀请用户')
    await email('uncertain@example.com')
    f.setMutation(async () => {
      throw new Error('lost private response')
    })
    await click('发送邀请')
    expect(dialog().textContent).toContain('请求结果未确认')
    expect(button('重试原请求', dialog()).disabled).toBe(true)
    f.setList(async () => json({ items: [] }))
    const reads = f.reads().length
    await click('检查当前状态', dialog())
    expect(f.reads()).toHaveLength(reads + 1)
    expect(button('重试原请求', dialog()).disabled).toBe(false)
    f.setMutation(async () => json({ id: id(100), job_id: id(500), version: '1' }, 201))
    await click('重试原请求', dialog())
    expect(f.posts()[1]![1].body).toBe(f.posts()[0]![1].body)
    expect(f.posts()[1]![1].headers).toEqual(f.posts()[0]![1].headers)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it.each([false, true])(
    'cohosts a pending close confirmation across pageshow checking and failure recovery (%s)',
    async (failFirst) => {
      const f = await page(),
        held = barrier<Response>()
      await click('邀请用户')
      await email('pending@example.com')
      await click('取消', dialog())
      pendingConfirmation()
      await pageshowChecking(f, held.promise)
      if (failFirst) {
        held.resolve(problem('DEPENDENCY_UNAVAILABLE', 503))
        await flushPromises()
        expect(f.wrapper.text()).toContain('会话尚未确认')
        expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
        expect(document.getElementById('app')!.inert).toBe(false)
        expect(button('检查当前会话').disabled).toBe(false)
        f.resetSessionRequest()
        await click('检查当前会话')
      } else {
        f.resetSessionRequest()
        held.resolve(json(sameSession()))
        await flushPromises()
      }
      const restored = pendingConfirmation()
      expect(f.posts()).toHaveLength(0)
      await click('继续编辑', restored.confirmation)
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(restored.business.inert).toBe(false)
      expect(restored.business.contains(document.activeElement)).toBe(true)
      expect(restored.business.querySelector<HTMLInputElement>('input')!.value).toBe(
        'pending@example.com',
      )
      await click('取消', restored.business)
      await click('放弃修改', dialog())
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(f.posts()).toHaveLength(0)
    },
  )
  it.each([
    ['route', false],
    ['route', true],
    ['logout', false],
    ['logout', true],
  ] as const)('settles a pending %s once after checking with discard=%s', async (kind, discard) => {
    const f = await page(),
      held = barrier<Response>()
    await click('邀请用户')
    await email('pending@example.com')
    let navigation: Promise<unknown> | undefined
    if (kind === 'route') navigation = f.router.push('/system/users')
    else button('退出登录').click()
    await flushPromises()
    pendingConfirmation()
    await pageshowChecking(f, held.promise)
    f.resetSessionRequest()
    held.resolve(json(sameSession()))
    await flushPromises()
    const restored = pendingConfirmation()
    expect(f.posts()).toHaveLength(0)
    await click(discard ? '放弃修改' : '继续编辑', restored.confirmation)
    await navigation
    await flushPromises()
    const logouts = f.fetch.mock.calls.filter(
      ([path, init]) => path.endsWith('/logout') && init.method === 'POST',
    )
    expect(logouts).toHaveLength(kind === 'logout' && discard ? 1 : 0)
    expect(f.router.currentRoute.value.path).toBe(
      discard ? (kind === 'route' ? '/system/users' : '/login') : '/system/invitations',
    )
    if (discard) expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    else {
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
      expect(dialog().querySelector<HTMLInputElement>('input')!.value).toBe('pending@example.com')
    }
    expect(f.posts()).toHaveLength(0)
  })
  it.each(['forbidden', 'new-session', 'revoked', 'dispose'] as const)(
    'ends a pending navigation with false and clears confirmation on %s',
    async (kind) => {
      const f = await page()
      await click('邀请用户')
      await email('pending@example.com')
      const navigation = f.router.push('/system/users')
      await flushPromises()
      pendingConfirmation()
      if (kind === 'dispose') {
        f.wrapper.unmount()
        wrapper = undefined
      } else {
        const value = sameSession()
        if (kind === 'forbidden') value.user = { ...value.user, role: 'user' }
        if (kind === 'new-session') value.session = { ...value.session, id: id(50) }
        f.setSessionRequest(async () =>
          kind === 'revoked' ? problem('SESSION_REVOKED', 401) : json(value),
        )
        window.dispatchEvent(new Event('pageshow'))
        await flushPromises()
      }
      await expect(navigation).resolves.toBeTruthy()
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
      expect(document.getElementById('app')!.inert).toBe(false)
      expect(f.posts()).toHaveLength(0)
      expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/logout'))).toHaveLength(0)
    },
  )
  it('confirms menu/top navigation and logout before any Session check, then discards only after consent', async () => {
    const f = await page()
    await click('邀请用户')
    await email('dirty@example.com')
    const before = f.fetch.mock.calls.length,
      navigation = f.router.push('/system/users')
    await flushPromises()
    expect(dialog().textContent).toContain('放弃未保存修改')
    expect(f.fetch.mock.calls).toHaveLength(before)
    await click('继续编辑', dialog())
    await navigation
    expect(f.router.currentRoute.value.path).toBe('/system/invitations')
    button('退出登录').click()
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(before)
    await click('继续编辑', dialog())
    const accepted = f.router.push('/settings/profile')
    await flushPromises()
    await click('放弃修改', dialog())
    await accepted
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('个人资料')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await f.router.push('/system/invitations')
    await flushPromises()
    await click('邀请用户')
    expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe('')
  })
  it.each(['escape', 'backdrop', 'close'] as const)(
    'routes dirty %s modal closing through the same confirmation',
    async (kind) => {
      await page()
      await click('邀请用户')
      await email('dirty@example.com')
      if (kind === 'escape')
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      else if (kind === 'backdrop')
        document
          .querySelector('.ui-overlay')!
          .dispatchEvent(new Event('pointerdown', { bubbles: true }))
      else (dialog().querySelector('button[aria-label="关闭"]') as HTMLButtonElement).click()
      await flushPromises()
      expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
      await click('继续编辑', dialog())
      expect(document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
        'dirty@example.com',
      )
      await click('取消', dialog())
      await click('放弃修改', dialog())
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    },
  )
  it('uses safe current denial to clear open drafts and both system menu leaves', async () => {
    const f = await page()
    await click('邀请用户')
    await email('private-draft@example.com')
    f.setMutation(async () => problem('FORBIDDEN', 403))
    await click('发送邀请')
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.wrapper.find('nav[aria-label="系统设置"]').exists()).toBe(false)
    expect(f.auth.state.user?.role).toBe('admin')
  })
  it('retains full long field text and five narrow-layout labels without merging columns', async () => {
    const f = await page(),
      value = { ...row(), email: 'long'.repeat(55) + '@example.com' }
    f.setList(async () => json({ items: [value] }))
    await click('刷新')
    expect(f.wrapper.get('tbody td').element.textContent).toBe(value.email)
    expect(f.wrapper.findAll('tbody td').map((c) => c.attributes('data-label'))).toEqual([
      '邮箱',
      '创建时间（UTC）',
      '到期时间（UTC）',
      '最近一次投递任务',
      '操作',
    ])
    expect(f.wrapper.findAll('tbody time').every((n) => n.attributes('aria-label') === time)).toBe(
      true,
    )
  })
})
