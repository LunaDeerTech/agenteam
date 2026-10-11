import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { type Fetch } from '../api/client'
import { installAuthentication } from '../router/auth'
import App from '../App.vue'

const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const time = '2026-10-08T10:00:00.000000Z'
const view: SessionView = {
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
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const value = (): Project => ({
  id: id(10),
  owner_user_id: id(1),
  name: 'Demo',
  normalized_name: 'demo',
  description: 'Description',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: null,
})
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
let wrapper: VueWrapper | undefined, activeRouter: Router | undefined
beforeEach(() => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  activeRouter?.options.history.destroy()
  activeRouter = undefined
  selected.auth?.leave()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function page(
  path = '/projects',
  options: {
    anonymous?: boolean
    lifecycle?: Project['lifecycle']
    username?: string
    admin?: boolean
  } = {},
) {
  let signedIn = !options.anonymous,
    current = {
      ...value(),
      ...(options.lifecycle
        ? {
            lifecycle: options.lifecycle,
            archived_at: options.lifecycle === 'archived' ? time : null,
          }
        : {}),
    }
  let session: SessionView = {
    ...view,
    user: {
      ...view.user,
      username: options.username ?? 'owner',
      role: options.admin ? 'admin' : 'user',
    },
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (url, init) => {
    if (url === '/api/v1/session')
      return signedIn
        ? json(session)
        : json(
            {
              type: 'urn:agenteam:problem:test',
              title: '',
              detail: '',
              instance: url,
              code: 'UNAUTHENTICATED',
              status: 401,
              request_id: id(99),
              commit_state: 'not_started',
            },
            401,
          )
    if (url.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (url.endsWith('/login')) {
      signedIn = true
      return json({ user: session.user, session: session.session, next_path: '/' })
    }
    if (url.endsWith('/logout')) {
      signedIn = false
      return new Response(null, { status: 204 })
    }
    if (url.startsWith('/api/v1/projects?'))
      return json({
        items: [
          {
            id: current.id,
            name: current.name,
            description: current.description,
            lifecycle: current.lifecycle,
            version: current.version,
          },
        ],
        next_cursor: null,
      })
    if (url.includes('/projects/resolve?')) return json(current)
    if (url.endsWith('/commands/lookup'))
      return json({ state: 'committed', result: { command: 'update', project: current } })
    if (init.method === 'PATCH') {
      const update = JSON.parse(init.body as string)
      current = {
        ...current,
        ...('name' in update
          ? { name: update.name, normalized_name: update.name.toLowerCase() }
          : {}),
        ...('description' in update ? { description: update.description } : {}),
        version: String(BigInt(current.version) + 1n),
      }
      return json(current)
    }
    if (url.startsWith('/api/v1/projects/')) return json(current)
    throw new Error('Unexpected request')
  }
  const fetcher = vi.fn<Fetch>((url, init) =>
    intercept ? intercept(url, init) : normal(url, init),
  )
  selected.auth = createSessionController(
    createAccountAPI(fetcher),
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
    createProjectOwnerAPI(fetcher),
  )
  const { router: source } = await import('../router')
  source.options.history.destroy()
  const router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  activeRouter = router
  installAuthentication(router, selected.auth)
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return {
    wrapper,
    router,
    fetcher,
    auth: selected.auth,
    normal,
    intercept(next?: Fetch) {
      intercept = next
    },
    session(next: SessionView) {
      session = next
    },
  }
}
function button(text: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('button')].find((entry) => {
    const content = entry.cloneNode(true) as HTMLElement
    content.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
    return (entry.getAttribute('aria-label') ?? content.textContent?.trim()) === text
  })
  if (!found) throw new Error(`Missing public button: ${text}`)
  return found
}

describe('Project production routes and public interactions', () => {
  it('shows Project before System settings with a separate Project navigation', async () => {
    const f = await page('/owner/demo', { admin: true })
    expect(
      f.wrapper
        .find('nav[aria-label="系统导航"]')
        .findAll('a')
        .map((link) => link.text()),
    ).toEqual(['项目', '系统设置'])
    expect(f.wrapper.find('nav[aria-label="项目导航"]').text()).toContain('Demo')
    expect(f.wrapper.find('nav[aria-label="项目导航"]').text()).toContain('项目设置')
    expect(f.wrapper.text()).not.toContain('会议')
    expect(f.wrapper.find('h1').text()).toBe('Demo')
  })
  it('enters from the real list and keeps General as the default alongside Audit and model leaves', async () => {
    const f = await page()
    await f.wrapper.find('a[href="/owner/demo"]').trigger('click')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo')
    await f.router.push('/owner/demo/settings')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(f.wrapper.find('input#project-name').element).toHaveProperty('value', 'Demo')
    expect(
      f.wrapper
        .find('nav[aria-label="项目设置"]')
        .findAll('a')
        .map((link) => link.text()),
    ).toEqual(['基本信息', '项目审计', 'Providers', '可用模型'])
  })
  it('retains URL and input when a Project navigation is cancelled, then explicitly discards', async () => {
    const f = await page('/owner/demo/settings/general')
    await f.wrapper.find('textarea#project-description').setValue('dirty description')
    await f.wrapper.find('a[href="/projects"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('放弃项目修改')
    button('继续编辑').click()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(f.wrapper.find('textarea').element).toHaveProperty('value', 'dirty description')
    // Join this navigation itself: the destination may still be loading its
    // lazy component after the confirmation and one microtask flush.
    let removeNavigation = () => {}
    const navigation = new Promise<{ to: string; from: string; failed: boolean }>((resolve) => {
      removeNavigation = f.router.afterEach((to, from, failure) => {
        removeNavigation()
        resolve({ to: to.fullPath, from: from.fullPath, failed: !!failure })
      })
    })
    onTestFinished(() => removeNavigation())
    try {
      await f.wrapper.find('a[href="/projects"]').trigger('click')
      await flushPromises()
      button('放弃并离开').click()
      expect(await navigation).toEqual({
        to: '/projects',
        from: '/owner/demo/settings/general',
        failed: false,
      })
      expect(f.router.currentRoute.value.path).toBe('/projects')
      expect(f.fetcher.mock.calls.some(([, init]) => init.method === 'PATCH')).toBe(false)
    } finally {
      removeNavigation()
    }
  })
  it('saving a description requires one explicit action and reports button success', async () => {
    const f = await page('/owner/demo/settings/general')
    expect(button('保存修改').disabled).toBe(true)
    await f.wrapper.find('textarea').setValue('new text')
    button('保存修改').click()
    await flushPromises()
    expect(f.wrapper.text()).toContain('命令已确认')
    expect(f.wrapper.find('textarea').element).toHaveProperty('value', 'new text')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
    expect(f.wrapper.text()).toContain('已确认保存')
  })
  it('shows archived fields read-only and leaves out unbound actions', async () => {
    const f = await page('/owner/demo/settings/general', { lifecycle: 'archived' })
    expect(f.wrapper.text()).toContain('项目已归档')
    expect(f.wrapper.find('textarea').attributes('readonly')).toBeDefined()
    expect(button('保存修改').disabled).toBe(true)
    expect(f.wrapper.text()).not.toContain('永久删除')
    expect(f.wrapper.text()).not.toContain('更换 Owner')
  })
  it.each(['admin', 'projects', 'forgot-password', 'reset-password'])(
    'accepts legitimate Owner %s without displacing account namespaces',
    async (username) => {
      const f = await page(`/${username}/demo/settings`, { username })
      expect(f.router.currentRoute.value.path).toBe(`/${username}/demo/settings/general`)
      expect(f.wrapper.find('textarea').exists()).toBe(true)
      expect(
        f.fetcher.mock.calls.some(([path]) =>
          path.includes(`/resolve?username=${username}&project_name=demo`),
        ),
      ).toBe(true)
    },
  )
  it.each([
    '/api/v1',
    '/system/unknown',
    '/settings/unknown',
    '/owner/%64emo/settings',
    '/owner/demo/settings?token=private',
    '/owner/demo#private',
  ])('keeps invalid/reserved raw route out of Project requests: %s', async (path) => {
    const f = await page(path)
    expect(f.router.currentRoute.value.name).toBe('not-found')
    expect(f.fetcher.mock.calls.some(([path]) => path.startsWith('/api/v1/projects'))).toBe(false)
  })
  it('logs in through a dynamic return and performs fresh Resolve/Get', async () => {
    const f = await page('/owner/demo/settings/general', { anonymous: true })
    expect(f.router.currentRoute.value.name).toBe('login')
    expect(f.router.currentRoute.value.query.return).toBe('/owner/demo/settings/general')
    expect(f.fetcher.mock.calls.some(([path]) => path.startsWith('/api/v1/projects'))).toBe(false)
    await f.wrapper.find('#login-email').setValue('owner@example.test')
    await f.wrapper.find('#login-password').setValue('correct owner password')
    await f.wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/demo/settings/general')
    expect(f.wrapper.find('textarea').exists()).toBe(true)
  })
})
