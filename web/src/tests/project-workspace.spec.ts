import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
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
import { createProjectCreationAPI } from '../api/project-creation'
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
  // Geometry supports focus behavior here; real layout is verified in the browser.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
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
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[12] = createProjectOwnerAPI(fetcher)
  args[19] = { projectCreation: createProjectCreationAPI(fetcher) }
  selected.auth = createSessionController(...args)
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
    let stopNavigation = () => {}
    const navigation = new Promise<unknown>((resolve) => {
      stopNavigation = f.router.afterEach((_to, _from, failure) => resolve(failure))
    })
    onTestFinished(() => stopNavigation())
    await f.wrapper.find('a[href="/owner/demo"]').trigger('click')
    expect(await navigation).toBeUndefined()
    stopNavigation()
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
    ).toEqual([
      '基本信息',
      '目录与配置',
      '变量目录',
      '安全目录',
      '项目技能库',
      '调度配置',
      '项目审计',
      'Providers',
      '可用模型',
      '调用记录',
    ])
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

function creationDialog() {
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
  if (!dialog) throw new Error('Missing creation dialog')
  return dialog
}
async function fillCreation(name = 'New.Project', description = '保留原描述  ') {
  button('新建项目').focus()
  button('新建项目').click()
  await flushPromises()
  await new DOMWrapper(creationDialog().querySelector('input')!).setValue(name)
  await new DOMWrapper(creationDialog().querySelector('textarea')!).setValue(description)
}
async function submitCreation() {
  await new DOMWrapper(creationDialog().querySelector('form')!).trigger('submit')
  await flushPromises()
}
function created(body: string) {
  const input = JSON.parse(body)
  return new Response(
    JSON.stringify({
      state: 'ready',
      project: {
        ...value(),
        id: input.project_id,
        name: input.name,
        normalized_name: input.name.toLowerCase(),
        description: input.description,
      },
    }),
    { status: 201, headers: { 'Content-Type': 'application/json' } },
  )
}

describe('Project list creation dialog with the real Session owner', () => {
  it('opens a separate modal, focuses the name, keeps backdrop clicks inside and restores focus on Escape', async () => {
    const f = await page()
    const trigger = button('新建项目')
    trigger.focus()
    trigger.click()
    await flushPromises()
    expect(f.wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(creationDialog().querySelector('input'))
    await new DOMWrapper(document.querySelector('.ui-overlay')!).trigger('pointerdown')
    expect(creationDialog()).toBeTruthy()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })
  it('preserves invalid input and description so the user can correct it before the first request', async () => {
    const f = await page()
    await fillCreation('invalid name', '描述与空格  ')
    await submitCreation()
    expect(creationDialog().querySelector('[role="alert"]')?.textContent).toContain('名称需为')
    expect(creationDialog().querySelector('input')?.value).toBe('invalid name')
    expect(creationDialog().querySelector('textarea')?.value).toBe('描述与空格  ')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
    f.intercept(async (path, init) =>
      path === '/api/v1/projects' && init.method === 'POST'
        ? created(init.body as string)
        : f.normal(path, init),
    )
    await new DOMWrapper(creationDialog().querySelector('input')!).setValue('corrected')
    await submitCreation()
    expect(creationDialog().textContent).toContain('项目已完成初始化')
    expect(creationDialog().querySelector('a')?.getAttribute('href')).toBe('/owner/corrected')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
  })
  it('keeps the modal open while submitting, prevents duplicate creation and exposes the confirmed project', async () => {
    const f = await page()
    let release!: () => void
    const hold = new Promise<void>((resolve) => {
      release = resolve
    })
    onTestFinished(() => release())
    f.intercept(async (path, init) => {
      if (path === '/api/v1/projects' && init.method === 'POST') {
        await hold
        return created(init.body as string)
      }
      return f.normal(path, init)
    })
    await fillCreation()
    await submitCreation()
    expect(creationDialog().textContent).toContain('正在创建项目')
    expect(button('返回项目列表').disabled).toBe(true)
    button('关闭').click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(creationDialog()).toBeTruthy()
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
    release()
    await vi.waitFor(() => expect(creationDialog().textContent).toContain('项目已完成初始化'))
    expect(creationDialog().querySelector('a')?.getAttribute('href')).toBe('/owner/new.project')
    button('返回项目列表').click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(button('新建项目'))
  })
  it('preserves a rejected name and description until the user edits and resubmits', async () => {
    const f = await page()
    f.intercept(async (path, init) =>
      path === '/api/v1/projects' && init.method === 'POST'
        ? json(
            {
              type: 'urn:agenteam:problem:test',
              title: 'Conflict',
              status: 409,
              detail: '',
              instance: path,
              code: 'RESOURCE_BUSY',
              request_id: id(99),
              commit_state: 'not_started',
              field_errors: [{ path: '/name', code: 'NAME_TAKEN' }],
            },
            409,
          )
        : f.normal(path, init),
    )
    await fillCreation('taken', '不能丢失的描述')
    await submitCreation()
    expect(creationDialog().querySelector('[role="alert"]')?.textContent).toContain(
      '项目名称已被使用',
    )
    expect(creationDialog().querySelector('input')?.value).toBe('taken')
    expect(creationDialog().querySelector('textarea')?.value).toBe('不能丢失的描述')
    f.intercept(async (path, init) =>
      path === '/api/v1/projects' && init.method === 'POST'
        ? created(init.body as string)
        : f.normal(path, init),
    )
    await new DOMWrapper(creationDialog().querySelector('input')!).setValue('available')
    await submitCreation()
    expect(creationDialog().textContent).toContain('项目已完成初始化')
    const posts = f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(posts).toHaveLength(2)
    expect(JSON.parse(posts[1]![1].body as string)).toMatchObject({
      name: 'available',
      description: '不能丢失的描述',
    })
  })
  it('reopens an unknown result and resumes only the original request', async () => {
    const f = await page()
    f.intercept(async (path, init) => {
      if (path === '/api/v1/projects' && init.method === 'POST') throw new Error('lost response')
      return f.normal(path, init)
    })
    await fillCreation('original', '完整原始内容  ')
    await submitCreation()
    expect(creationDialog().textContent).toContain('创建结果尚未确认')
    button('返回项目列表').click()
    await flushPromises()
    expect(document.activeElement).toBe(button('查看创建状态'))
    button('查看创建状态').click()
    await flushPromises()
    expect(creationDialog().querySelector('input')).toBeNull()
    expect(creationDialog().textContent).toContain('original')
    f.intercept(async (path, init) =>
      path === '/api/v1/projects' && init.method === 'POST'
        ? created(init.body as string)
        : f.normal(path, init),
    )
    button('继续原创建').click()
    await vi.waitFor(() => expect(creationDialog().textContent).toContain('项目已完成初始化'))
    const posts = f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(posts).toHaveLength(2)
    expect(posts[1]![1].body).toBe(posts[0]![1].body)
    expect(new Headers(posts[1]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(posts[0]![1].headers).get('Idempotency-Key'),
    )
  })
})
