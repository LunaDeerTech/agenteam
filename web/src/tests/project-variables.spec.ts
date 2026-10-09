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
import { createAccountAPI } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createProjectVariablesAPI } from '../api/project-variables'
import { type Fetch } from '../api/client'
import {
  projectRoute,
  safeReturnTarget,
  installAuthentication,
  installProjectNavigation,
  installProjectModelSettingsNavigation,
  installProjectVariablesNavigation,
} from '../router/auth'
import App from '../App.vue'
import ProjectWorkspaceView from '../views/projects/ProjectWorkspaceView.vue'
import ProjectSettingsView from '../views/projects/ProjectSettingsView.vue'
import ProjectVariablesView from '../views/projects/ProjectVariablesView.vue'
import { useTheme } from '../composables/useTheme'
const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-09T12:00:00.000001Z',
  projectID = id(10),
  target = id(20),
  path = '/owner/project.name/settings/variables'
const view = {
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
  session: { id: id(2), issued_at: at, idle_expires_at: at, absolute_expires_at: at },
  csrf_token: 'S'.repeat(43),
}
const project = {
  id: projectID,
  owner_user_id: id(1),
  name: 'project.name',
  normalized_name: 'project.name',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
}
const row = {
  id: target,
  project_id: projectID,
  type: 'variable',
  name: 'CUSTOM',
  description: 'safe description',
  version: '1',
  created_at: at,
  updated_at: at,
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })
let wrapper: VueWrapper | undefined
async function settle() {
  for (let i = 0; i < 4; i++) await flushPromises()
}
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
  // DOM focus bookkeeping only; actual geometry and keyboard acceptance require the browser fixture.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  selected.auth?.leave()
  selected.auth = null
  document.body.innerHTML = ''
  useTheme().setTheme('system')
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function fixture() {
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') return json(view)
    if (url.endsWith('/logout')) return new Response(null, { status: 204 })
    if (url.includes('/variables')) {
      if (init.method === 'GET')
        return json(url.includes('?') ? { items: [row] } : { ...row, value: 'ordinary text' })
      const body = JSON.parse(String(init.body)),
        request = body.request
      return json({
        command: 'project.variable.create',
        changed: true,
        variable: {
          ...row,
          id: request.variable_id,
          name: request.name,
          description: request.description,
          value: request.value,
        },
        event_id: id(80),
        audit_id: id(81),
      })
    }
    if (url.startsWith('/api/v1/projects/')) return json(project)
    throw new Error('unexpected fixture request')
  })
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
    undefined,
    undefined,
    createProjectVariablesAPI(fetcher),
  )
  await selected.auth.restore()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/projects',
        name: 'projects',
        component: { template: '<p>Projects</p>' },
        meta: { authentication: true, protected: true },
      },
      {
        path: '/login',
        name: 'login',
        component: { template: '<p>Login</p>' },
        meta: { authentication: true },
      },
      {
        path: '/:username/:project_name',
        component: ProjectWorkspaceView,
        meta: { authentication: true, protected: true, projectWorkspace: true },
        children: [
          {
            path: 'settings',
            component: ProjectSettingsView,
            children: [
              { path: 'variables', name: 'project-variables', component: ProjectVariablesView },
              { path: 'general', component: { template: '<p>General</p>' } },
            ],
          },
        ],
      },
      { path: '/:pathMatch(.*)*', name: 'not-found', component: { template: '<p>Not found</p>' } },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await settle()
  return { router, fetcher, auth: selected.auth, wrapper }
}
function button(label: string) {
  const value = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(
    (b) =>
      (b.getAttribute('aria-label') === label ||
        b.querySelector('.button-label')?.textContent?.trim() === label) &&
      !b.closest('[inert]'),
  )
  if (!value) throw new Error('button not found: ' + label)
  return value
}
function input(label: string) {
  const l = Array.from(document.querySelectorAll('label')).find(
    (v) => v.textContent?.replace(' *', '') === label,
  )
  if (!l) throw new Error('label not found: ' + label)
  return document.getElementById(l.htmlFor) as HTMLInputElement | HTMLTextAreaElement
}
async function set(label: string, value: string) {
  const el = input(label)
  el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

describe('Variables exact route, Owner menu and actual page consumers', () => {
  it.each([
    '/owner/p/settings/variables',
    '/OWNER/project.name/settings/variables',
    '/owner/.dot/settings/variables',
  ])('accepts only the canonical variables suffix %s', (value) => {
    expect(projectRoute(value)?.suffix).toBe('/settings/variables')
    expect(safeReturnTarget(value)).toBe(value.toLowerCase())
  })
  it.each([
    '/owner/p/settings/variables/',
    '/owner/p/settings/variables?',
    '/owner/p/settings/variables?x=1',
    '/owner/p/settings/variables#x',
    '/owner/p/settings/%76ariables',
    '/owner/p/settings/Variables',
    '/owner/p/settings/variables/123',
    '/owner/../settings/variables',
    '/api/p/settings/variables',
  ])('rejects malformed raw path %s', (value) => {
    expect(projectRoute(value)).toBeNull()
    expect(safeReturnTarget(value)).toBe('/')
  })
  it('keeps Owner then Model then Variables guard order and cancels without an afterNavigation publication', async () => {
    const auth = createSessionController({
      ...createAccountAPI(),
      getSession: async () => view as never,
    })
    await auth.restore()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path, component: {}, meta: { projectWorkspace: true } },
        { path: '/to', component: {} },
      ],
    })
    installAuthentication(router, auth)
    await router.push(path)
    const events: string[] = [],
      model = {
        confirmLeave: async () => {
          events.push('model')
          return true
        },
        afterNavigation: () => events.push('model-after'),
      }
    // Owner participates only when leaving a Project route, preserving its old gate.
    const variable = {
      confirmLeave: async () => {
        events.push('variables')
        return false
      },
      afterNavigation: () => events.push('variables-after'),
    }
    installProjectModelSettingsNavigation(router, model)
    installProjectVariablesNavigation(router, variable)
    const stops = installProjectNavigation(router, {
      confirmLeave: async () => {
        events.push('owner')
        return true
      },
      afterNavigation: () => events.push('owner-after'),
    })
    const failed = await router.push('/to')
    expect(isNavigationFailure(failed)).toBe(true)
    expect(events).toEqual(['owner', 'model', 'variables'])
    stops()
    router.options.history.destroy()
    auth.leave()
  })
  it('renders the real Settings leaf and uses summaries until an explicit detail read', async () => {
    const f = await fixture()
    expect(document.querySelector('h1')?.textContent).toBe('Variables')
    expect(document.querySelector(`a[href="${path}"]`)?.textContent).toContain('Variables')
    expect(document.body.textContent).not.toContain('ordinary text')
    expect(f.fetcher.mock.calls.filter(([p]) => p.includes('/variables'))).toHaveLength(1)
    button('查看变量 CUSTOM').click()
    await settle()
    expect(input('值').value).toBe('ordinary text')
    expect(button('保存修改').disabled).toBe(true)
    expect(document.querySelector('table')?.textContent).not.toContain('ordinary text')
    expect(document.querySelector('nav[aria-label="变量分页"]')?.textContent).toContain('第 1 页')
  })
  it('submits actual captured create from controls with inline success and original recovery controls', async () => {
    const f = await fixture()
    button('新建变量').click()
    await flushPromises()
    await set('名称', 'BROWSER_CUSTOM')
    await set('值', '')
    button('创建变量').click()
    await settle()
    const call = f.fetcher.mock.calls.find(
      ([p, init]) => p.endsWith('/variables') && init.method === 'POST',
    )
    expect(JSON.parse(String(call?.[1].body))).toMatchObject({
      request: { name: 'BROWSER_CUSTOM', description: '', value: '' },
    })
    expect(document.querySelector('[aria-label="已确认历史回执"]')?.textContent).toContain(
      'BROWSER_CUSTOM',
    )
    expect(button('查询原操作').disabled).toBe(false)
    expect(button('重放原操作').disabled).toBe(false)
    expect(document.querySelector('[role="status"]')?.textContent).not.toContain('private')
  })
  it('logout asks Variables after existing guards, cancellation keeps draft and restores trigger focus', async () => {
    const f = await fixture()
    button('新建变量').click()
    await flushPromises()
    await set('名称', 'DRAFT')
    const logout = button('退出登录')
    logout.focus()
    logout.click()
    await settle()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('离开变量管理？')
    button('取消').click()
    await settle()
    expect(input('名称').value).toBe('DRAFT')
    expect(f.fetcher.mock.calls.some(([p]) => p.endsWith('/logout'))).toBe(false)
    expect(f.router.currentRoute.value.fullPath).toBe(path)
    expect(document.activeElement).toBe(logout)
  })
  it('invalid raw query redirects before any new Variables read', async () => {
    const f = await fixture(),
      count = f.fetcher.mock.calls.filter(([p]) => p.includes('/variables')).length
    await f.router.push(path + '?x=1')
    await settle()
    expect(f.router.currentRoute.value.name).toBe('not-found')
    expect(f.fetcher.mock.calls.filter(([p]) => p.includes('/variables'))).toHaveLength(count)
  })
})
