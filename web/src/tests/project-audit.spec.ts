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
import {
  createProjectAuditAPI,
  auditFilterActions,
  auditFilterResourceKinds,
} from '../api/project-audit'
import { type Fetch } from '../api/client'
import { installAuthentication } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import ProjectNav from '../components/layout/ProjectNav.vue'
import ProjectAuditView from '../views/projects/ProjectAuditView.vue'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-08T12:34:56.123456Z',
  endpoint = `/api/v1/projects/${id(10)}/audit`
const view = (role: 'admin' | 'user' = 'user'): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role,
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const project = (
  number = 10,
  name = 'Demo',
  lifecycle: Project['lifecycle'] = 'active',
): Project => ({
  id: id(number),
  owner_user_id: id(1),
  name,
  normalized_name: name.toLowerCase(),
  description: 'Safe project description',
  lifecycle,
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: lifecycle === 'archived' ? time : null,
})
const record = (number = 100, projectID = id(10)) => ({
  audit_id: id(number),
  created_at: time,
  scope: 'project',
  project_id: projectID,
  actor: { kind: 'human', id: id(1) },
  action: 'secret.create',
  outcome: 'success',
  resource: { kind: 'secret', id: id(3) },
  metadata: { version: '9007199254740993', changed_fields: ['purpose', 'value'] },
  associations: { request_id: id(9) },
  summary: 'Secret created',
})
const page = (projectID = id(10)) => ({ items: [record(100, projectID)], next_cursor: null })
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
const owners: SessionController[] = [],
  routers: Router[] = [],
  releases: (() => void)[] = []
let wrapper: VueWrapper | undefined,
  narrow = false
function heldBody(value: unknown) {
  const entered = barrier(),
    joined = barrier()
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(new TextEncoder().encode(JSON.stringify(value)))
    },
    cancel() {
      entered.resolve()
      return joined.promise
    },
  })
  const release = () => joined.resolve()
  releases.push(release)
  return {
    entered,
    release,
    response: () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
  }
}
function gatedBody(value: unknown) {
  const joined = barrier()
  let controller!: ReadableStreamDefaultController<Uint8Array>,
    cancelled = 0,
    released = false
  const stream = new ReadableStream<Uint8Array>({
    start(current) {
      controller = current
      current.enqueue(new TextEncoder().encode(JSON.stringify(value)))
    },
    cancel() {
      ++cancelled
      return joined.promise
    },
  })
  const release = () => {
    if (released) return
    released = true
    if (!cancelled) controller.close()
    joined.resolve()
  }
  releases.push(release)
  return {
    release,
    cancelled: () => cancelled,
    response: () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
  }
}
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
  // Programmatic focus/DOM states only. Tab, media geometry and actual visuals
  // remain in the separately authorized real-browser representatives.
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
  target = '/owner/demo/settings/audit',
  options: {
    role?: 'user' | 'admin'
    lifecycle?: Project['lifecycle']
    name?: string
    audit?: Fetch
  } = {},
) {
  let current = view(options.role),
    currentProject = project(10, options.name ?? 'Demo', options.lifecycle)
  let session: Fetch = async () => json(current)
  let audit: Fetch =
    options.audit ??
    (async (path) =>
      json(
        path.split('?')[0]!.split('/').length === 7
          ? { ...record(100, path.split('/')[4]!), audit_id: path.split('/').at(-1)! }
          : page(path.split('/')[4]!),
      ))
  let ownerRequest: Fetch | null = null
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (/^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path)) return audit(path, init)
    if (path.startsWith('/api/v1/projects/resolve?'))
      return ownerRequest ? ownerRequest(path, init) : json(currentProject)
    if (path.startsWith('/api/v1/projects/'))
      return ownerRequest ? ownerRequest(path, init) : json(currentProject)
    if (path.startsWith('/api/v1/projects?')) return json({ items: [], next_cursor: null })
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    throw new Error(`Unexpected controlled endpoint: ${path}`)
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
    setAudit(value: Fetch) {
      audit = value
    },
    setOwner(value: Fetch | null) {
      ownerRequest = value
    },
    setSession(value: Fetch) {
      session = value
    },
    restoreSession() {
      session = async () => json(current)
    },
    setProject(value: Project) {
      currentProject = value
    },
    setView(value: SessionView) {
      current = value
    },
    reads: () =>
      fetch.mock.calls.filter(([path]) =>
        /^\/api\/v1\/projects\/[^/]+\/audit(?:[/?]|$)/.test(path),
      ),
    writes: () => fetch.mock.calls.filter(([, init]) => init.method !== 'GET'),
  }
}
function button(name: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find((element) => {
    const copy = element.cloneNode(true) as HTMLElement
    copy.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
    return (element.getAttribute('aria-label') ?? copy.textContent?.trim()) === name
  })
  if (!found) throw new Error(`Missing public button: ${name}`)
  return found
}
async function click(name: string) {
  button(name).click()
  await flushPromises()
}
function field(name: string) {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (node) => node.textContent?.trim() === name,
  )
  if (!label?.htmlFor) throw new Error(`Missing public field: ${name}`)
  return document.getElementById(label.htmlFor) as HTMLInputElement | HTMLSelectElement
}
async function change(name: string, value: string) {
  const input = field(name)
  input.value = value
  input.dispatchEvent(
    new Event(input instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }),
  )
  await flushPromises()
}
const detailButton = (number = 100) => '查看详情 ' + id(number)

describe('actual ProjectNav exact raw settings current state', () => {
  it.each([
    ['/owner/demo/settings/general', true],
    ['/owner/demo/settings/audit', true],
    ['/OWNER/DEMO/settings/audit', true],
    ['/owner/demo', false],
    ['/owner/demo/settings', false],
    ['/another/demo/settings/audit', false],
    ['/owner/other/settings/audit', false],
    ['/owner/demo/settings/audit/extra', false],
    ['/owner/demo/settings/general?cursor=x', false],
    ['/owner/demo/settings/general#fragment', false],
    ['/owner/demo/settings/%61udit', false],
    ['/owner/demo/settings/unknown', false],
    ['/owner/demo/settings/AUDIT', false],
  ])('validates %s without broad prefix matching', async (target, expected) => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
    })
    routers.push(router)
    await router.push(target as string)
    wrapper = mount(ProjectNav, {
      props: { name: 'Demo', home: '/owner/demo', settings: '/owner/demo/settings/general' },
      global: { plugins: [router] },
    })
    const link = wrapper.get('a[href="/owner/demo/settings/general"]')
    expect(link.attributes('aria-current')).toBe(expected ? 'page' : undefined)
    expect(link.text()).toBe('项目设置')
  })
  it('also rejects a malformed or non-General settings target', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
    })
    routers.push(router)
    await router.push('/owner/demo/settings/audit')
    wrapper = mount(ProjectNav, {
      props: { name: 'Demo', home: '/owner/demo', settings: '/owner/demo/settings/audit' },
      global: { plugins: [router] },
    })
    expect(wrapper.findAll('a')[1]!.attributes('aria-current')).toBeUndefined()
    await wrapper.setProps({ settings: '/owner/demo/settings/general?unsafe=true' })
    expect(wrapper.findAll('a')[1]!.attributes('aria-current')).toBeUndefined()
  })
})

describe('production App/Owner/Audit route composition', () => {
  it.each(['user', 'admin'] as const)(
    'shows %s current Owner both navigation levels and the safe audit record',
    async (role) => {
      const f = await app(undefined, { role })
      expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(true)
      expect(
        f.wrapper
          .get('nav[aria-label="项目导航"] a[href="/owner/demo/settings/general"]')
          .attributes('aria-current'),
      ).toBe('page')
      const menu = f.wrapper.get('nav[aria-label="项目设置"]')
      expect(menu.findAll('.settings-group-toggle').map((entry) => entry.text())).toEqual([
        '项目资料',
        '安全记录',
      ])
      expect(menu.get('a[href="/owner/demo/settings/audit"]').attributes('aria-current')).toBe(
        'page',
      )
      expect(f.wrapper.get('[aria-label="项目审计列表"]').text()).toContain('secret.create')
      expect(f.wrapper.text()).not.toContain('保存审计')
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('keeps General as settings default and canonicalizes a dotted direct Audit address through Resolve/Get', async () => {
    const f = await app('/OWNER/OWNER.Dot-Name/settings/audit', { name: 'Owner.Dot-Name' })
    expect(f.router.currentRoute.value.path).toBe('/owner/owner.dot-name/settings/audit')
    const calls = f.fetch.mock.calls.map(([path]) => path)
    expect(calls.findIndex((path) => path.includes('/projects/resolve?'))).toBeLessThan(
      calls.indexOf('/api/v1/projects/' + id(10)),
    )
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
    await f.router.push('/owner/owner.dot-name/settings')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/owner.dot-name/settings/general')
    expect(f.wrapper.get('#project-name').element).toHaveProperty('value', 'Owner.Dot-Name')
  })
  it.each(['/OWNER/OWNER.Dot-Name/settings/audit', '/owner/owner.dot-name/settings/audit'])(
    'retains one pending native Audit read through the canonical address %s',
    async (target) => {
      const held = gatedBody(page())
      const f = await app(target, {
        name: 'Owner.Dot-Name',
        audit: async () => held.response(),
      })
      try {
        expect(f.router.currentRoute.value.fullPath).toBe('/owner/owner.dot-name/settings/audit')
        expect(f.reads()).toHaveLength(1)
        expect(f.auth.state.busy).toBe(true)
        expect({
          cancelled: held.cancelled(),
          aborted: f.reads()[0]![1].signal?.aborted,
          stopped: f.wrapper.text().includes('本页读取已停止'),
        }).toEqual({ cancelled: 0, aborted: false, stopped: false })
        expect(f.wrapper.find('[aria-label="项目审计列表"]').exists()).toBe(false)
      } finally {
        held.release()
        await flushPromises()
      }
      expect(f.auth.state.busy).toBe(false)
      expect(f.wrapper.get('[aria-label="项目审计列表"]').text()).toContain('secret.create')
      expect(f.reads()).toHaveLength(1)
      expect(f.writes()).toHaveLength(0)
    },
  )
  it.each([
    ...['/settings/audit', '/settings/general', '/settings'].flatMap((suffix) =>
      ['?cursor=x', '?', '#fragment', '#'].map((tail) => '/owner/demo' + suffix + tail),
    ),
    '/owner/demo/settings/audit/record',
    '/owner/demo/settings/general/record',
    '/owner/demo/settings/%61udit',
    '/owner/demo/settings/%67eneral',
    '/owner/demo/%73ettings',
  ])('rejects raw route %s before Owner or Audit fetching', async (path) => {
    const f = await app(path)
    expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(false)
    expect(f.fetch.mock.calls.filter(([url]) => url.startsWith('/api/v1/projects'))).toHaveLength(0)
  })
  it.each(['archiving', 'archived'] as const)(
    'reads Audit on %s without presenting new Project editing',
    async (lifecycle) => {
      const f = await app(undefined, { lifecycle })
      expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(true)
      expect(f.wrapper.text()).toContain('当前内容只读')
      expect(f.reads()).toHaveLength(1)
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('exposes all fourteen labelled filters and complete option domains, with explicit validation and no input request', async () => {
    const f = await app()
    const labels = [
      '起始时间（含）',
      '结束时间（不含）',
      '动作',
      '结果',
      '操作者类型',
      '操作者 ID',
      '资源类型',
      '资源 ID',
      'Tool ID',
      'Execution ID',
      'Operation ID',
      'Approval ID',
      'Runner ID',
      'Agent ID',
    ]
    expect(labels.map((label) => !!field(label))).toEqual(Array(14).fill(true))
    expect([...(field('动作') as HTMLSelectElement).options].map((option) => option.value)).toEqual(
      ['', ...auditFilterActions],
    )
    expect(
      [...(field('资源类型') as HTMLSelectElement).options].map((option) => option.value),
    ).toEqual(['', ...auditFilterResourceKinds])
    await change('Tool ID', 'not-id')
    expect(f.reads()).toHaveLength(1)
    await click('应用筛选')
    const input = field('Tool ID')
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(
      document.getElementById(input.getAttribute('aria-describedby')!.split(' ').at(-1)!)
        ?.textContent,
    ).toContain('UUIDv7')
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe('not-id')
    expect(f.reads()).toHaveLength(1)
    f.setAudit(async () => json({ items: [], next_cursor: null }))
    await change('Tool ID', '')
    await change('动作', 'account.login')
    await click('应用筛选')
    expect(f.wrapper.text()).toContain('没有匹配的审计记录')
    expect(f.reads().at(-1)![0]).toContain('action=account.login')
    await click('重置筛选')
    expect(field('动作').value).toBe('')
    expect(f.reads().at(-1)![0]).toBe(endpoint + '?limit=50')
  })
  it('enters detail with an independent GET, safe fixed labels and focus, then restores the original row without refetch', async () => {
    const f = await app(),
      original = button(detailButton())
    original.focus()
    await click(detailButton())
    expect(f.reads().at(-1)![0]).toBe(endpoint + '/' + id(100))
    expect(f.wrapper.find('form[aria-label="项目审计筛选"]').exists()).toBe(false)
    expect(document.activeElement).toBe(document.getElementById('project-audit-detail-heading'))
    expect(f.wrapper.get('[aria-labelledby="project-audit-detail-heading"]').text()).toContain(
      '9007199254740993',
    )
    expect(f.wrapper.text()).toContain('变更字段')
    expect(f.wrapper.text()).toContain('purpose、value')
    expect(f.wrapper.find('pre').exists()).toBe(false)
    const count = f.reads().length
    await click('返回列表')
    expect(f.reads()).toHaveLength(count)
    expect(document.activeElement).toBe(button(detailButton()))
  })
  it('cancels a detail with its native tail still owned, and requires explicit retry after release', async () => {
    const f = await app(),
      held = heldBody(record())
    f.setAudit(async () => held.response())
    await click(detailButton())
    const cancel = button('取消读取')
    expect(cancel.disabled).toBe(false)
    await click('取消读取')
    await held.entered.promise
    expect(f.auth.state.busy).toBe(true)
    expect(f.wrapper.text()).toContain('审计详情读取失败')
    expect(button('重新读取详情').disabled).toBe(true)
    const count = f.reads().length
    held.release()
    await flushPromises()
    expect(f.reads()).toHaveLength(count)
    f.setAudit(async () => json(record()))
    await click('重新读取详情')
    expect(f.wrapper.text()).toContain('结构化证据')
  })
  it('retains an explicitly recoverable page when a later guard cancels a reused-parameter navigation', async () => {
    const f = await app(),
      held = heldBody(page())
    f.setAudit(async () => held.response())
    await click('重新读取')
    const block = f.router.beforeResolve(() => false)
    const result = await f.router.push('/owner/second/settings/audit')
    await flushPromises()
    await held.entered.promise
    expect(isNavigationFailure(result)).toBe(true)
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/audit')
    expect(f.wrapper.text()).toContain('本页读取已停止')
    held.release()
    await flushPromises()
    block()
    f.setAudit(async () => json(page()))
    await click('重试读取')
    expect(f.wrapper.find('[aria-label="项目审计列表"]').exists()).toBe(true)
  })
  it.each([
    ['/owner/second/settings/audit', 'project'],
    ['/owner/demo/settings/audit?cursor=x', 'invalid'],
    ['/owner/demo/settings/general', 'leave'],
  ] as const)(
    'retires the pending native read for %s until its actual tail ends',
    async (target, kind) => {
      const f = await app(),
        held = gatedBody(page()),
        identity = [f.auth.state.user?.id, f.auth.state.session?.id],
        expectedIdentity = kind === 'invalid' ? [undefined, undefined] : identity
      let pending = true
      f.setAudit(async (path) => {
        if (pending) {
          pending = false
          return held.response()
        }
        return json(page(path.split('/')[4]!))
      })
      await click('重新读取')
      expect(f.auth.state.busy).toBe(true)
      if (kind === 'project') f.setProject(project(11, 'Second'))
      const ownerReads = () =>
        f.fetch.mock.calls.filter(
          ([path]) => path.startsWith('/api/v1/projects') && !path.includes('/audit'),
        ).length
      const beforeOwner = ownerReads()
      await f.router.push(target)
      await flushPromises()
      try {
        expect(held.cancelled()).toBe(1)
        expect(f.reads()[1]![1].signal?.aborted).toBe(true)
        expect(f.auth.state.busy).toBe(true)
        expect(f.reads()).toHaveLength(2)
        expect(ownerReads()).toBe(beforeOwner)
        expect(f.wrapper.find('[aria-label="项目审计列表"]').exists()).toBe(false)
        expect([f.auth.state.user?.id, f.auth.state.session?.id]).toEqual(expectedIdentity)
        if (kind === 'invalid') {
          expect(f.router.currentRoute.value.name).toBe('not-found')
          expect(f.auth.state.phase).toBe('checking')
        }
      } finally {
        held.release()
        await flushPromises()
      }
      expect(f.auth.state.busy).toBe(false)
      expect([f.auth.state.user?.id, f.auth.state.session?.id]).toEqual(expectedIdentity)
      if (kind === 'project') {
        expect(f.reads()).toHaveLength(3)
        expect(f.reads().at(-1)![0]).toBe(`/api/v1/projects/${id(11)}/audit?limit=50`)
        expect(f.wrapper.get('[aria-label="项目审计列表"]').text()).toContain('secret.create')
      } else {
        expect(f.reads()).toHaveLength(2)
        expect(ownerReads()).toBe(beforeOwner)
        expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(false)
      }
      expect(f.writes()).toHaveLength(0)
    },
  )
  it('handles Project parameter navigation while clearing old rows and filters', async () => {
    const f = await app()
    await change('Tool ID', id(7))
    f.setProject(project(11, 'Second'))
    await f.router.push('/owner/second/settings/audit')
    await flushPromises()
    expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(true)
    expect(field('Tool ID').value).toBe('')
    expect(f.reads().at(-1)![0]).toBe(`/api/v1/projects/${id(11)}/audit?limit=50`)
    await click(detailButton())
    expect(f.wrapper.get('[aria-labelledby="project-audit-detail-heading"]').text()).toContain(
      id(11),
    )
  })
  it('leaves filter drafts without a new prompt but keeps the existing Owner edit confirmation', async () => {
    const f = await app()
    await change('Tool ID', id(7))
    await f.router.push('/owner/demo/settings/general')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    await f.wrapper.get('#project-description').setValue('Unsaved Owner description')
    const navigation = f.router.push('/owner/demo/settings/audit')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('放弃项目修改')
    await click('继续编辑')
    await navigation
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(f.wrapper.get('#project-description').element).toHaveProperty(
      'value',
      'Unsaved Owner description',
    )
    expect(f.writes()).toHaveLength(0)
  })
  it('destroys page state on idle pageshow Session503 and restores a fresh default view', async () => {
    const f = await app()
    await change('动作', 'secret.create')
    await click('应用筛选')
    await click(detailButton())
    const oldHeading = document.getElementById('project-audit-detail-heading')!,
      count = f.reads().length
    f.setSession(async () => problem())
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(false)
    expect(oldHeading.isConnected).toBe(false)
    expect(f.wrapper.text()).toContain('会话尚未确认')
    expect(f.reads()).toHaveLength(count)
    f.restoreSession()
    await click('检查当前会话')
    expect(f.wrapper.findComponent(ProjectAuditView).exists()).toBe(true)
    expect(field('动作').value).toBe('')
    expect(field('每页数量').value).toBe('50')
    expect(f.reads()).toHaveLength(count + 1)
    expect(f.writes()).toHaveLength(0)
  })
  it('uses the existing narrow Settings drawer with both entries and closes it on navigation', async () => {
    narrow = true
    const f = await app()
    await click('项目设置栏目')
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('基本信息')
    expect(dialog.textContent).toContain('项目审计')
    const link = dialog.querySelector<HTMLAnchorElement>('a[href="/owner/demo/settings/general"]')!
    link.click()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
