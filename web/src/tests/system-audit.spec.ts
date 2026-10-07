import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, isNavigationFailure } from 'vue-router'
import type { SessionController } from '../composables/useSession'
import type { SystemAuditController, AuditFilterDraft } from '../composables/useSystemAudit'
const selected = vi.hoisted(() => ({
  auth: null as SessionController | null,
  instances: [] as {
    controller: SystemAuditController
    initialDraft: AuditFilterDraft
    initialLimit: string | undefined
  }[],
}))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
vi.mock('../composables/useSystemAudit', async (original) => {
  const actual = await original<typeof import('../composables/useSystemAudit')>()
  return {
    ...actual,
    useSystemAudit: (...args: Parameters<typeof actual.useSystemAudit>) => {
      const controller = actual.useSystemAudit(...args)
      selected.instances.push({
        controller,
        initialDraft: { ...controller.draft },
        initialLimit: controller.state.applied.limit,
      })
      return controller
    },
  }
})
import { createSessionController } from '../composables/useSession'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import { createSystemAccountSecurityAPI } from '../api/system-account-security'
import { createSystemSMTPSettingsAPI } from '../api/system-smtp-settings'
import { createSystemSMTPDeliveryAPI } from '../api/system-smtp-delivery'
import { createSystemOutboundPolicyAPI } from '../api/system-outbound-policy'
import {
  createSystemAuditAPI,
  auditFilterActions,
  auditFilterResourceKinds,
} from '../api/system-audit'
import { type Fetch } from '../api/client'
import { router as productionRouter } from '../router'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import SystemAuditView from '../views/system/SystemAuditView.vue'

const endpoint = '/api/v1/system/audit'
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-07T00:00:00.123456Z'
const view = (session = 2, role: 'admin' | 'user' = 'admin'): SessionView => ({
  user: {
    id: id(1),
    email: 'admin@example.test',
    username: 'admin',
    display_name: 'Admin',
    role,
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(session), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const record = (n = 100) => ({
  audit_id: id(n),
  created_at: time,
  scope: 'system',
  actor: { kind: 'human', id: id(1) },
  action: 'secret.create',
  outcome: 'success',
  resource: { kind: 'secret', id: id(2) },
  metadata: { version: '1', changed_fields: ['value'] },
  associations: { request_id: id(8) },
  summary: 'Secret created',
})
const page = (n = 100, count = 1, cursor: string | null = null) => ({
  items: Array.from({ length: count }, (_, i) => record(n - i)),
  next_cursor: cursor,
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problem = (code = 'DEPENDENCY_UNAVAILABLE', status = 503) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      status,
      code,
      detail: '<private diagnostic>',
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
const releases: (() => void)[] = []
function heldBody(value: unknown, status = 200) {
  const entered = barrier(),
    joined = barrier()
  const cancel = vi.fn(() => {
    entered.resolve()
    return joined.promise
  })
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(JSON.stringify(value)))
    },
    cancel,
  })
  const release = () => joined.resolve()
  releases.push(release)
  return {
    entered,
    cancel,
    stream,
    release,
    response: () =>
      new Response(stream, {
        status,
        headers: {
          'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
          'X-Request-ID': id(9),
        },
      }),
  }
}
let wrapper: VueWrapper | undefined
const owners: SessionController[] = []
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
  // DOM focus/state only. Pointer defaults, Tab, geometry and visual layouts remain real-browser checks.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  for (const release of releases.splice(0)) release()
  wrapper?.unmount()
  wrapper = undefined
  for (const owner of owners) owner.leave()
  await flushPromises()
  expect(owners.every((owner) => !owner.state.busy)).toBe(true)
  owners.length = 0
  selected.auth = null
  selected.instances.length = 0
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
  useTheme().setTheme('system')
})
async function app(initial = view(), target = '/system/audit') {
  const observations: {
    method: string
    path: string
    phase: string | undefined
    personalPhase: string | undefined
    role: string | undefined
    route: string
  }[] = []
  let currentRoute = () => ''
  let current = initial,
    session: Fetch = async () => json(current)
  let audit: Fetch = async (path) =>
    json(path.startsWith(endpoint + '/') ? record(Number.parseInt(path.slice(-12), 16)) : page())
  const fetch = vi.fn<Fetch>(async (path, init) => {
    observations.push({
      method: init.method!,
      path,
      phase: selected.auth?.state.phase,
      personalPhase: selected.auth?.personalContext.phase,
      role: selected.auth?.state.user?.role,
      route: currentRoute(),
    })
    if (path === '/api/v1/session') return session(path, init)
    if (path === endpoint || path.startsWith(endpoint + '?') || path.startsWith(endpoint + '/'))
      return audit(path, init)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) {
      session = async () => problem('SESSION_REVOKED', 401)
      return new Response(null, { status: 204 })
    }
    if (path.startsWith('/api/v1/system/users')) return json({ items: [] })
    if (path === '/api/v1/system/smtp')
      return json({
        id: id(10),
        version: '1',
        configured: false,
        host: '',
        port: 0,
        encryption: '',
        username: '',
        sender_email: '',
        sender_name: '',
        credential_present: false,
        auto_retry_count: '0',
        retry_interval_seconds: '10',
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
    createSystemAuditAPI(fetch),
  )
  selected.auth = auth
  owners.push(auth)
  await auth.restore()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: productionRouter.options.routes,
  })
  currentRoute = () => router.currentRoute.value.path
  let blockNavigation = false
  router.beforeEach(() => (blockNavigation ? false : true))
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
    observations,
    setAudit(value: Fetch) {
      audit = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    setSession(value: SessionView) {
      current = value
    },
    resetSession() {
      session = async () => json(current)
    },
    blockNavigation(value: boolean) {
      blockNavigation = value
    },
    reads() {
      return fetch.mock.calls.filter(
        ([path]) =>
          path === endpoint || path.startsWith(endpoint + '?') || path.startsWith(endpoint + '/'),
      )
    },
    sessionReads() {
      return fetch.mock.calls.filter(([path]) => path === '/api/v1/session')
    },
    writes() {
      return fetch.mock.calls.filter(([, init]) => init.method !== 'GET')
    },
  }
}
function button(label: string, root: ParentNode = document) {
  const result = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) =>
      node.getAttribute('aria-label') === label ||
      (
        node.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? node.textContent
      )?.trim() === label,
  )
  expect(result, `button ${label}`).toBeTruthy()
  return result!
}
async function click(label: string) {
  button(label).click()
  await flushPromises()
}
function field(label: string) {
  const node = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (node) => node.textContent?.trim() === label,
  )
  expect(node, `field ${label}`).toBeTruthy()
  return document.getElementById(node!.htmlFor) as HTMLInputElement | HTMLSelectElement
}
async function change(label: string, value: string) {
  const input = field(label)
  input.value = value
  input.dispatchEvent(
    new Event(input instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
  )
  await flushPromises()
}
async function apply() {
  const submitter = button('应用筛选')
  submitter.form!.dispatchEvent(
    new SubmitEvent('submit', { bubbles: true, cancelable: true, submitter }),
  )
  await flushPromises()
}
const detailButton = (n = 100) => '查看详情 ' + id(n)

describe('Audit page uses the production App, route records, authentication guard and factory', () => {
  it('has one protected Audit leaf, nine leaves/four groups and thirteen exact return targets', async () => {
    const f = await app()
    expect(f.router.resolve('/system/audit').matched.at(-1)?.path).toBe('/system/audit')
    expect(f.router.currentRoute.value.meta.systemAdmin).toBe(true)
    const toggles = f.wrapper.findAll('.settings-menu .settings-group-toggle')
    expect(toggles).toHaveLength(4)
    for (const toggle of toggles)
      if (toggle.attributes('aria-expanded') === 'false') await toggle.trigger('click')
    expect(f.wrapper.findAll('.settings-menu a').map((node) => node.attributes('href'))).toEqual([
      '/system/users',
      '/system/invitations',
      '/system/providers',
      '/system/models',
      '/system/model-selection',
      '/system/audit',
      '/system/account-security',
      '/system/smtp',
      '/system/outbound-policy',
    ])
    const targets = [
      '/',
      '/settings/profile',
      '/settings/appearance',
      '/settings/password',
      '/system/users',
      '/system/invitations',
      '/system/providers',
      '/system/models',
      '/system/model-selection',
      '/system/account-security',
      '/system/smtp',
      '/system/outbound-policy',
      '/system/audit',
    ]
    for (const target of targets) expect(safeReturnTarget(target)).toBe(target)
    for (const value of [
      '/system/audit?x=1',
      '/system/audit#x',
      '/system/audit/',
      '/system/audit/extra',
      ['/system/audit'],
    ])
      expect(safeReturnTarget(value)).toBe('/')
    expect(f.reads()).toHaveLength(1)
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })

  it('disposes the whole page on idle pageshow/Session503 and mounts a fresh default instance on same-identity recovery', async () => {
    const f = await app()
    await change('起始时间（含）', '2026-10-07T00:00:00Z')
    await apply()
    await click(detailButton())
    expect(f.wrapper.find('[aria-labelledby="audit-detail-heading"]').exists()).toBe(true)
    const oldHeading = document.getElementById('audit-detail-heading')!,
      reads = f.reads().length
    const session = barrier<Response>()
    releases.push(() => session.resolve(problem()))
    f.setSessionRequest(() => session.promise)
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
    expect(f.wrapper.find('.session-check').exists()).toBe(true)
    expect(!oldHeading.isConnected && f.reads().length === reads).toBe(true)
    session.resolve(problem())
    await flushPromises()
    expect(f.wrapper.text()).toContain('会话尚未确认')
    const recovery = button('检查当前会话')
    recovery.focus()
    await flushPromises()
    expect(document.activeElement).toBe(recovery)
    f.resetSession()
    await click('检查当前会话')
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(true)
    expect(field('起始时间（含）').value).toBe('')
    expect(field('每页数量').value).toBe('50')
    expect(f.reads().length).toBe(reads + 1)
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
    expect(f.wrapper.find('[aria-labelledby="audit-detail-heading"]').exists()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('ignores busy pageshow, keeps cancelled reads in error after actual tail, and requires explicit refresh', async () => {
    const f = await app(),
      held = heldBody(page())
    f.setAudit(async () => held.response())
    const sessions = f.sessionReads().length
    await click('刷新第一页')
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(f.sessionReads()).toHaveLength(sessions)
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(true)
    await click('取消读取')
    await held.entered.promise
    expect(f.wrapper.text()).toContain('系统审计读取失败')
    expect(f.auth.state.busy && button('刷新第一页').disabled).toBe(true)
    const reads = f.reads().length
    held.release()
    await flushPromises()
    expect(!f.auth.state.busy && !held.stream.locked).toBe(true)
    expect(f.reads()).toHaveLength(reads)
    expect(f.wrapper.text()).toContain('系统审计读取失败')
    f.setAudit(async () => json(page()))
    await click('刷新第一页')
    expect(f.reads()).toHaveLength(reads + 1)
    expect(f.writes()).toHaveLength(0)
  })

  it('retires the local read before a later guard rejects navigation, then explicitly refreshes the surviving applied filter', async () => {
    const f = await app()
    await change('动作', 'secret.create')
    await apply()
    const held = heldBody(page()),
      reads = f.reads().length
    f.setAudit(async () => held.response())
    await click('刷新第一页')
    f.blockNavigation(true)
    expect(isNavigationFailure(await f.router.push('/'))).toBe(true)
    await held.entered.promise
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/system/audit')
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(true)
    expect(f.wrapper.text()).toContain('本页读取已停止')
    expect(field('动作').value).toBe('secret.create')
    held.release()
    await flushPromises()
    expect(!f.auth.state.busy && !held.stream.locked).toBe(true)
    expect(f.reads()).toHaveLength(reads + 1)
    f.setAudit(async () => json(page()))
    await click('刷新第一页')
    expect(new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams.get('action')).toBe(
      'secret.create',
    )
    expect(f.reads()).toHaveLength(reads + 2)
    expect(f.writes()).toHaveLength(0)
  })

  it('unmounts during an actual old cancel tail, and only the latest newly mounted Audit instance reads once after join', async () => {
    const f = await app(),
      held = heldBody(page())
    f.setAudit(async () => held.response())
    await click('刷新第一页')
    await f.router.push('/system/users')
    await flushPromises()
    await held.entered.promise
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
    const reads = f.reads().length
    f.setAudit(async () => json(page()))
    await f.router.push('/system/audit')
    await flushPromises()
    expect(f.wrapper.text()).toContain('等待当前请求结束')
    expect(f.reads()).toHaveLength(reads)
    held.release()
    await flushPromises()
    expect(f.reads()).toHaveLength(reads + 1)
    expect(f.fetch.mock.calls.filter(([p]) => p.startsWith('/api/v1/system/users'))).toHaveLength(0)
    expect(!f.auth.state.busy && !held.stream.locked).toBe(true)
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
  })

  it('clears detail and drafts for a new Session and shows no Audit controls after current administrator loss', async () => {
    const f = await app()
    await change('Tool ID', id(80))
    await click(detailButton())
    f.setSession(view(3))
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(field('Tool ID').value).toBe('')
    expect(f.wrapper.find('[aria-labelledby="audit-detail-heading"]').exists()).toBe(false)
    const count = f.reads().length
    f.setSession(view(3, 'user'))
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
    expect(f.wrapper.get('h1').text()).toBe('无权访问系统设置')
    expect(f.reads()).toHaveLength(count)
    expect(f.writes()).toHaveLength(0)
  })

  it('allows fresh anonymous login navigation after Audit401 while destroying all private page state', async () => {
    const f = await app()
    await change('Tool ID', id(80))
    f.setSessionRequest(async () => problem('SESSION_REVOKED', 401))
    f.setAudit(async () => problem('SESSION_REVOKED', 401))
    await click('刷新第一页')
    await flushPromises()
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
    // The explicit Session check runs the real anonymous bootstrap and then the login guard.
    await click('检查当前会话')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/login')
    expect(f.wrapper.find('#login-email').exists()).toBe(true)
    expect(f.wrapper.find('#audit-filter-tool_id').exists()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('handles current403 using the system permission view and leaves the read-only page without an added confirmation', async () => {
    const f = await app()
    const original = selected.instances[0]!.controller
    await change('Tool ID', id(80))
    expect(original.draft.tool_id).toBe(id(80))
    f.setAudit(async () => problem('FORBIDDEN', 403))
    await click('刷新第一页')
    expect(f.wrapper.get('h1').text()).toBe('无权访问系统设置')
    expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
    expect(!original.isCurrent() && original.draft.tool_id === '').toBe(true)
    const reads = f.reads().length
    const checkpoint = f.observations.length
    const held = heldBody(
      {
        type: 'urn:agenteam:problem:test',
        title: 'Failure',
        status: 403,
        code: 'FORBIDDEN',
        detail: '',
        instance: endpoint,
        request_id: id(9),
        commit_state: 'not_started',
      },
      403,
    )
    f.setAudit(async () => held.response())
    try {
      await f.router.push('/')
      await flushPromises()
      await held.entered.promise
      expect(f.router.currentRoute.value.path).toBe('/')
      const sequence = f.observations.slice(checkpoint)
      process.stdout.write('audit-current403-navigation ' + JSON.stringify(sequence) + '\n')
      expect(sequence.map(({ method, path, route }) => ({ method, path, route }))).toEqual([
        { method: 'GET', path: '/api/v1/session', route: '/system/audit' },
        { method: 'GET', path: endpoint + '?limit=50', route: '/system/audit' },
      ])
      expect(
        sequence.at(-1)?.phase === 'authenticated' &&
          sequence.at(-1)?.personalPhase === 'current' &&
          sequence.at(-1)?.role === 'admin',
      ).toBe(true)
      expect(selected.instances).toHaveLength(2)
      const fresh = selected.instances[1]!
      expect(
        fresh.controller !== original &&
          Object.entries(fresh.initialDraft).every(
            ([key, value]) => value === (key === 'limit' ? '50' : ''),
          ) &&
          fresh.initialLimit === '50',
      ).toBe(true)
      expect(
        !fresh.controller.isCurrent() && fresh.controller.state.list.phase === 'inactive',
      ).toBe(true)
      expect(f.reads()).toHaveLength(reads + 1)
      expect(held.cancel).toHaveBeenCalledTimes(1)
      expect(f.auth.state.busy && !f.auth.system.denied).toBe(true)
      expect(f.wrapper.findComponent(SystemAuditView).exists()).toBe(false)
      expect(button('退出登录').disabled).toBe(true)
    } finally {
      held.release()
      await flushPromises()
    }
    expect(!f.auth.state.busy && !held.stream.locked && !f.auth.system.denied).toBe(true)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.observations).toHaveLength(checkpoint + 2)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.writes()).toHaveLength(0)
  })
})

describe('Audit visible filters, complete inline observations and local focus', () => {
  it('uses only the visited cursor and explicit first-page actions, including strict expired-cursor recovery and reset', async () => {
    const f = await app()
    await change('每页数量', '1')
    f.setAudit(async () => json(page(100, 1, 'opaque.page-2')))
    await apply()
    f.setAudit(async () => json(page(90)))
    await click('下一页')
    expect(new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams.get('cursor')).toBe(
      'opaque.page-2',
    )
    expect(f.wrapper.text()).toContain('第 2 页')
    f.setAudit(async () => json(page(100, 1, 'opaque.page-2')))
    await click('上一页')
    expect(new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams.has('cursor')).toBe(
      false,
    )
    expect(f.wrapper.text()).toContain('第 1 页')
    f.setAudit(async () => problem('CURSOR_INVALID', 400))
    await click('下一页')
    const count = f.reads().length
    expect(f.wrapper.text()).toContain('分页链接已失效，返回第一页')
    expect(f.wrapper.find('[aria-label="系统审计列表"]').exists()).toBe(false)
    expect(button('上一页').disabled && button('下一页').disabled).toBe(true)
    await flushPromises()
    expect(f.reads()).toHaveLength(count)
    f.setAudit(async () => json(page(80)))
    await click('返回第一页')
    expect(new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams.has('cursor')).toBe(
      false,
    )
    await change('动作', 'project.archive.accepted')
    await click('重置筛选')
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
    expect(field('动作').value === '' && field('每页数量').value === '50').toBe(true)
    expect(f.wrapper.text()).not.toContain('筛选已修改，尚未应用')
    expect(f.writes()).toHaveLength(0)
  })

  it.each([
    ['起始时间（含）', '2026-02-30T00:00:00Z'],
    ['Tool ID', id(7).toUpperCase().replace('01970000', '0197ABCD')],
    ['每页数量', '01'],
    ['每页数量', '1e2'],
    ['每页数量', '201'],
  ])(
    'keeps the complete old observation and sends no request for invalid %s',
    async (label, value) => {
      const f = await app(),
        count = f.reads().length
      await change(label, value)
      await apply()
      expect(f.reads()).toHaveLength(count)
      const input = field(label)
      expect(input.getAttribute('aria-invalid')).toBe('true')
      expect(
        document.getElementById(input.getAttribute('aria-describedby')!)?.getAttribute('role'),
      ).toBe('alert')
      expect(input.value).toBe(value)
      expect(document.activeElement).toBe(input)
      expect(f.wrapper.find('[aria-label="系统审计列表"]').exists()).toBe(true)
      expect(f.writes()).toHaveLength(0)
    },
  )

  it('publishes no partial table for an invalid final row and explicitly retries a failed read without exposing Problem details', async () => {
    const f = await app()
    f.setAudit(async () =>
      json({
        items: [
          record(100),
          {
            ...record(99),
            metadata: {
              version: '1',
              changed_fields: ['value'],
              extra: '<img src=x onerror=alert(1)>',
            },
          },
        ],
        next_cursor: null,
      }),
    )
    await click('刷新第一页')
    expect(f.wrapper.text()).toContain('系统审计读取失败')
    expect(f.wrapper.find('[aria-label="系统审计列表"]').exists()).toBe(false)
    expect(f.wrapper.find('img').exists()).toBe(false)
    f.setAudit(async () => problem())
    await click('重试读取')
    expect(f.wrapper.text()).not.toContain('<private diagnostic>')
    expect(f.wrapper.text()).not.toContain('没有匹配的审计记录')
    const count = f.reads().length
    await flushPromises()
    expect(f.reads()).toHaveLength(count)
    f.setAudit(async () => json(page(100, 0)))
    await click('重试读取')
    expect(f.wrapper.text()).toContain('没有匹配的审计记录')
    expect(f.reads()).toHaveLength(count + 1)
    expect(f.writes()).toHaveLength(0)
  })

  it('cancels unfinished detail and returns to the prior page with a local heading fallback while the old tail is busy', async () => {
    const f = await app(),
      held = heldBody(record())
    f.setAudit(async () => held.response())
    await click(detailButton())
    const oldHeading = document.getElementById('audit-detail-heading')!
    expect(f.wrapper.text()).toContain('正在读取审计详情')
    expect(f.wrapper.text()).not.toContain('结构化证据')
    expect(document.activeElement).toBe(oldHeading)
    const count = f.reads().length
    try {
      await click('返回列表')
      await held.entered.promise
      expect(!oldHeading.isConnected && f.auth.state.busy).toBe(true)
      expect(f.reads()).toHaveLength(count)
      expect(f.wrapper.find('[aria-label="系统审计列表"]').exists()).toBe(true)
      expect(document.activeElement).toBe(document.getElementById('audit-list-heading'))
      expect(button(detailButton()).disabled).toBe(true)
    } finally {
      held.release()
      await flushPromises()
    }
    expect(!f.auth.state.busy && !held.stream.locked).toBe(true)
    expect(document.activeElement).toBe(document.getElementById('audit-list-heading'))
    expect(f.reads()).toHaveLength(count)
    expect(f.writes()).toHaveLength(0)
  })

  it('merges legal missing details without probing a different scope and re-reads only the captured target', async () => {
    const f = await app()
    f.setAudit(async () => problem('NOT_FOUND', 404))
    await click(detailButton())
    expect(f.wrapper.text()).toContain('记录不存在或不可访问')
    const count = f.reads().length
    expect(f.wrapper.find('[aria-label="系统审计列表"]').exists()).toBe(false)
    f.setAudit(async () => json(record()))
    await click('重新读取详情')
    expect(f.reads()).toHaveLength(count + 1)
    expect(f.reads().at(-1)![0]).toBe(endpoint + '/' + id(100))
    expect(f.wrapper.text()).toContain('结构化证据')
    expect(document.activeElement).toBe(document.getElementById('audit-detail-heading'))
    expect(
      f.fetch.mock.calls.every(([path]) => path === '/api/v1/session' || path.startsWith(endpoint)),
    ).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })

  it('shows a Project-only actor filter as an explicit legal empty observation without local omission', async () => {
    const f = await app()
    await change('操作者类型', 'agent_run')
    await change('操作者 ID', id(30))
    f.setAudit(async () => json(page(100, 0)))
    await apply()
    const params = new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams
    expect(params.get('actor_kind')).toBe('agent_run')
    expect(params.get('actor_id')).toBe(id(30))
    expect(f.wrapper.text()).toContain('没有匹配的审计记录')
    expect(f.wrapper.find('[aria-invalid="true"]').exists()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('provides all fourteen visible controls and all 53/25 legal options, applies only explicit input and keeps invalid drafts', async () => {
    const f = await app()
    const labels = [
      '起始时间（含）',
      '结束时间（不含）',
      '操作者类型',
      '操作者 ID',
      '动作',
      '结果',
      '资源类型',
      '资源 ID',
      'Tool ID',
      'Execution ID',
      'Operation ID',
      'Approval ID',
      'Runner ID',
      'Agent ID',
    ]
    for (const label of labels) expect(field(label).isConnected).toBe(true)
    expect(
      [...(field('动作') as HTMLSelectElement).options]
        .map((option) => option.value)
        .filter(Boolean),
    ).toEqual([...auditFilterActions])
    expect(
      [...(field('资源类型') as HTMLSelectElement).options]
        .map((option) => option.value)
        .filter(Boolean),
    ).toEqual([...auditFilterResourceKinds])
    expect([...document.querySelectorAll('legend')].map((node) => node.textContent)).toEqual([
      '时间与事件',
      '操作者与资源',
      '关联 ID',
    ])
    const before = f.reads().length
    await change('起始时间（含）', '2026-10-07T08:00:00.123456+08:00')
    await change('结束时间（不含）', '2026-10-07T01:00:00Z')
    await change('操作者类型', 'human')
    await change('操作者 ID', id(1))
    await change('动作', 'artifact.read')
    await change('结果', 'unknown')
    await change('资源类型', 'project')
    await change('资源 ID', id(3))
    for (const label of labels.slice(8)) await change(label, id(20))
    await change('每页数量', '2')
    expect(f.reads()).toHaveLength(before)
    expect(f.wrapper.text()).toContain('筛选已修改，尚未应用')
    f.setAudit(async () => json(page(100, 0)))
    await apply()
    const query = new URL(f.reads().at(-1)![0], 'https://app.invalid').searchParams
    expect([...query.keys()].sort()).toEqual(
      [
        'from',
        'to',
        'actor_kind',
        'actor_id',
        'action',
        'outcome',
        'resource_kind',
        'resource_id',
        'tool_id',
        'execution_id',
        'operation_id',
        'approval_id',
        'runner_id',
        'agent_id',
        'limit',
      ].sort(),
    )
    expect(query.get('from')).toBe(time)
    expect(query.get('action')).toBe('artifact.read')
    expect(f.wrapper.text()).toContain('没有匹配的审计记录')
    await change('操作者类型', 'service')
    await apply()
    expect(f.reads()).toHaveLength(before + 1)
    const input = field('操作者类型')
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(input.getAttribute('aria-describedby')).toBeTruthy()
    expect(document.activeElement).toBe(input)
    expect(field('操作者 ID').value).toBe(id(1))
    expect(f.wrapper.text()).toContain('没有匹配的审计记录')
    const beforeUnload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(beforeUnload)
    expect(beforeUnload.defaultPrevented).toBe(false)
    expect(f.router.currentRoute.value.fullPath).toBe('/system/audit')
    expect(f.writes()).toHaveLength(0)
  })

  it('opens strict typed details without URLs or related reads and restores the new row button without re-fetching the list', async () => {
    const f = await app()
    await click(detailButton())
    expect(f.router.currentRoute.value.fullPath).toBe('/system/audit')
    expect(f.reads().at(-1)![0]).toBe(endpoint + '/' + id(100))
    const detail = f.wrapper.get('[aria-labelledby="audit-detail-heading"]')
    expect(detail.text()).toContain('变更字段')
    expect(detail.text()).toContain('value')
    expect(detail.text()).toContain('Secret created')
    expect(detail.text()).toContain('Request ID')
    expect(detail.find('pre').exists() || detail.find('a').exists()).toBe(false)
    expect(document.activeElement).toBe(document.getElementById('audit-detail-heading'))
    expect(f.wrapper.find('[aria-label="系统审计筛选"]').exists()).toBe(false)
    const count = f.reads().length
    await click('返回列表')
    expect(f.reads()).toHaveLength(count)
    expect(document.activeElement).toBe(button(detailButton()))
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(
      f.fetch.mock.calls.every(([path]) => path === '/api/v1/session' || path.startsWith(endpoint)),
    ).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })
})
