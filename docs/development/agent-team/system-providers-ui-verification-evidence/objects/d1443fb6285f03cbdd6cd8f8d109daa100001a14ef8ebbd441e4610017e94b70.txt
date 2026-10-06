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
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import PersonalSettingsView from '../views/settings/PersonalSettingsView.vue'
import ProfileSettings from '../views/settings/ProfileSettings.vue'
import AppearanceSettings from '../views/settings/AppearanceSettings.vue'
import PasswordSettings from '../views/settings/PasswordSettings.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
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
const rows = (count = 25) =>
  Array.from({ length: count }, (_, n) => ({
    ...user,
    id: id(100 - n),
    email: `user${100 - n}@example.com`,
    username: `username${100 - n}`,
    display_name: '',
    role: 'user',
    created_at: time,
  }))
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(3),
    },
  })
const problem = (code: string, status: number) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Error',
      status,
      detail: '<script>raw private detail</script>',
      instance: '/api/v1/system/users',
      code,
      request_id: id(3),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => {
    resolve = next
  })
  return { promise, resolve }
}
let wrapper: VueWrapper | undefined
let narrow = false
beforeEach(() => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: narrow && query.includes('760px'),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  selected.auth?.leave()
  narrow = false
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})
async function page(
  path = '/system',
  role: 'admin' | 'user' = 'admin',
  anonymous = false,
  list: Fetch = async () => json({ items: rows(), next_cursor: 'next-a' }),
) {
  let signedIn = !anonymous
  let current: SessionView = {
    user: { ...user, role },
    session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
  const fetch = vi.fn<Fetch>(async (path) => {
    if (path === '/api/v1/session')
      return signedIn ? json(current) : problem('UNAUTHENTICATED', 401)
    if (path.startsWith('/api/v1/system/users?')) return list(path, {})
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
    if (path === '/api/v1/me') return json({ user: current.user, avatar: null })
    throw new Error('Unexpected fixture path')
  })
  selected.auth = createSessionController(createAccountAPI(fetch), createSystemAccountAPI(fetch))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView, meta: { authentication: true, protected: true } },
      { path: '/login', name: 'login', component: LoginView, meta: { authentication: true } },
      {
        path: '/settings',
        component: PersonalSettingsView,
        redirect: '/settings/profile',
        meta: { authentication: true, protected: true },
        children: [
          { path: 'profile', component: ProfileSettings },
          { path: 'appearance', component: AppearanceSettings },
          { path: 'password', component: PasswordSettings },
        ],
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
        children: [{ path: 'users', component: SystemUsersView }],
      },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return {
    wrapper,
    router,
    auth: selected.auth,
    fetch,
    setList(next: Fetch) {
      list = next
    },
    setRole(role: 'admin' | 'user') {
      current = { ...current, user: { ...current.user, role, version: '2' } }
    },
    directoryCalls() {
      return fetch.mock.calls.filter(([p]) => p.startsWith('/api/v1/system/users?'))
    },
  }
}
function button(label: string) {
  const result = [...document.querySelectorAll('button')].find(
    (node) =>
      (
        node.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? node.textContent
      )?.trim() === label,
  )
  if (!result) throw new Error('Missing visible button: ' + label)
  return result
}
async function click(label: string) {
  button(label).click()
  await flushPromises()
}

describe('System user directory page and shared shells', () => {
  it('opens users from the two-leaf administrator entry with separately labelled fields', async () => {
    const p = await page('/')
    await p.wrapper.get('nav[aria-label="系统导航"] a[href="/system"]').trigger('click')
    await flushPromises()
    expect(p.router.currentRoute.value.path).toBe('/system/users')
    expect(p.wrapper.get('h1').text()).toBe('用户')
    expect(p.wrapper.find('nav[aria-label="个人设置"]').exists()).toBe(false)
    const menu = p.wrapper.get('nav[aria-label="系统设置"]')
    expect(menu.text()).toContain('用户与邀请')
    expect(menu.findAll('a').map((a) => a.text())).toEqual(['用户', '待注册邀请', 'Providers'])
    expect(menu.get('a').attributes('aria-current')).toBe('page')
    expect(menu.findAll('button').map((b) => b.text())).not.toContain('退出登录')
    expect(p.wrapper.findAll('thead th').map((th) => th.text())).toEqual([
      '邮箱',
      '用户名',
      '显示名',
      '角色',
      '注册时间（UTC）',
    ])
    const cells = p.wrapper.get('tbody tr').findAll('td')
    expect(cells.map((cell) => cell.text()).slice(0, 4)).toEqual([
      'user100@example.com',
      'username100',
      'user100@example.com',
      '普通用户',
    ])
    expect(cells[4]!.get('time').attributes()).toMatchObject({
      datetime: time,
      'aria-label': time,
      title: time,
    })
    expect(cells[4]!.text()).toBe('2026-10-06 12:34:56')
    expect(p.wrapper.get('tbody').findAll('a')).toHaveLength(0)
    expect(p.wrapper.text()).toContain('本页 25 位用户')
    expect(p.directoryCalls()).toHaveLength(1)
    expect(document.activeElement).toBe(p.wrapper.get('h1').element)
  })
  it('shows a stable direct-link denial for a normal user and rechecks permission explicitly', async () => {
    const p = await page('/system', 'user')
    expect(p.router.currentRoute.value.path).toBe('/system/users')
    expect(p.wrapper.text()).toContain('无权访问系统设置')
    expect(p.wrapper.find('a[href="/system"]').exists()).toBe(false)
    expect(p.wrapper.find('table').exists()).toBe(false)
    expect(p.directoryCalls()).toHaveLength(0)
    p.setRole('admin')
    await click('重新检查权限')
    expect(p.wrapper.get('h1').text()).toBe('用户')
    expect(p.directoryCalls()).toHaveLength(1)
  })
  it('preserves directory text including the real layout fixture display-name trailing space', async () => {
    const row = { ...rows(1)[0]!, display_name: '系统目录长名称 Mixed '.repeat(4) }
    const p = await page('/system', 'admin', false, async () => json({ items: [row] }))
    const cells = p.wrapper.get('tbody tr').findAll('td')
    expect(cells.map((cell) => cell.element.textContent)).toEqual([
      row.email,
      row.username,
      row.display_name,
      '普通用户',
      '2026-10-06 12:34:56',
    ])
    // The previous browser observer trimmed all cells, changing this valid
    // display name before comparing it with the unchanged database value.
    expect(cells[2]!.element.textContent!.trim()).not.toBe(row.display_name)
  })
  it('retains five distinct field labels and full long record text in the narrow layout', async () => {
    narrow = true
    const row = {
      ...rows(1)[0]!,
      email: `directory-${'long'.repeat(20)}@example.com`,
      username: 'directory-fixture-0123456789',
      display_name: '系统目录长名称 Mixed '.repeat(4),
    }
    const p = await page('/system', 'admin', false, async () => json({ items: [row] }))
    const cells = p.wrapper.get('tbody tr').findAll('td')
    expect(cells.map((cell) => cell.attributes('data-label'))).toEqual([
      '邮箱',
      '用户名',
      '显示名',
      '角色',
      '注册时间（UTC）',
    ])
    expect(cells.slice(0, 3).map((cell) => cell.element.textContent)).toEqual([
      row.email,
      row.username,
      row.display_name,
    ])
    expect(cells[4]!.get('time').attributes('datetime')).toBe(time)
    expect(button('系统设置栏目')).toBeTruthy()
  })
  it('shows loading, failure and empty states without stale rows or raw error text', async () => {
    const held = barrier<Response>()
    const p = await page('/system', 'admin', false, () => held.promise)
    expect(p.wrapper.text()).toContain('正在读取用户目录')
    expect(p.wrapper.find('table').exists()).toBe(false)
    expect(button('刷新').disabled).toBe(true)
    held.resolve(json({ items: rows(), next_cursor: 'next-a' }))
    await flushPromises()
    const failure = barrier<Response>()
    p.setList(() => failure.promise)
    button('下一页').click()
    await flushPromises()
    expect(p.wrapper.find('table').exists()).toBe(false)
    failure.resolve(problem('DEPENDENCY_UNAVAILABLE', 503))
    await flushPromises()
    expect(p.wrapper.text()).toContain('用户目录读取失败')
    expect(p.wrapper.text()).not.toContain('暂无用户')
    expect(p.wrapper.text()).not.toContain('raw private')
    p.setList(async () => json({ items: [] }))
    await click('重试')
    expect(p.wrapper.text()).toContain('暂无用户')
    expect(p.wrapper.text()).toContain('本页 0 位用户')
    expect(p.wrapper.text()).not.toContain('创建')
  })
  it('clears the table and navigation on current FORBIDDEN without changing the User role', async () => {
    const p = await page()
    p.setList(async () => problem('FORBIDDEN', 403))
    await click('刷新')
    expect(p.wrapper.text()).toContain('无权访问系统设置')
    expect(p.wrapper.find('table').exists()).toBe(false)
    expect(p.wrapper.find('a[href="/system"]').exists()).toBe(false)
    expect(p.auth.state.user?.role).toBe('admin')
    expect(p.auth.state.phase).toBe('authenticated')
  })
  it('offers an explicit first-page recovery for a bad cursor', async () => {
    const p = await page()
    p.setList(async () => problem('CURSOR_INVALID', 400))
    await click('下一页')
    expect(p.directoryCalls()).toHaveLength(2)
    expect(p.wrapper.text()).toContain('返回首页重新加载')
    p.setList(async () => json({ items: rows(1) }))
    await click('返回首页重新加载')
    expect(p.directoryCalls().at(-1)![0]).toBe('/api/v1/system/users?limit=25')
    expect(button('上一页').disabled).toBe(true)
  })
  it('keeps the personal draft before revalidation, then discards only after confirmation', async () => {
    const p = await page('/settings/profile')
    const display = p.wrapper.findAll('input').find((input) => input.element.value === 'Admin')!
    await display.setValue('未保存 Draft')
    const before = p.fetch.mock.calls.length
    const navigation = p.router.push('/system')
    await flushPromises()
    expect(document.body.textContent).toContain('放弃未保存修改？')
    expect(p.fetch.mock.calls.length).toBe(before)
    await click('继续编辑')
    await navigation
    expect(p.router.currentRoute.value.path).toBe('/settings/profile')
    expect(display.element.value).toBe('未保存 Draft')
    const accepted = p.router.push('/system')
    await flushPromises()
    await click('放弃修改')
    await accepted
    await flushPromises()
    expect(p.router.currentRoute.value.path).toBe('/system/users')
    expect(p.wrapper.find('nav[aria-label="个人设置"]').exists()).toBe(false)
    await click('退出登录')
    expect(p.fetch.mock.calls.some(([path]) => path.endsWith('/logout'))).toBe(true)
  })
  it('keeps the mobile drawer keyboard accessible and restores its trigger focus', async () => {
    narrow = true
    const p = await page()
    const trigger = button('系统设置栏目')
    trigger.focus()
    await click('系统设置栏目')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(document.querySelector('[role="dialog"]')?.contains(document.activeElement)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    await click('系统设置栏目')
    const leaf = document.querySelector(
      '[role="dialog"] a[href="/system/users"]',
    ) as HTMLAnchorElement
    leaf.click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(p.wrapper.get('h1').text()).toBe('用户')
  })
  it('retains a reachable paging control after success or a disabled last-page action', async () => {
    const p = await page()
    const next = button('下一页')
    next.focus()
    await click('下一页')
    expect(document.activeElement).toBe(next)
    p.setList(async () => json({ items: rows(1) }))
    await click('下一页')
    expect(document.activeElement).toBe(button('刷新'))
  })
  it('restores only the exact system leaf after real controller login', async () => {
    for (const value of [
      '/system',
      '/system/users?x=1',
      '/system/users#x',
      '/system/models',
      '//example.com/system/users',
      ['/system/users'],
    ])
      expect(safeReturnTarget(value)).toBe('/')
    expect(safeReturnTarget('/system/users')).toBe('/system/users')
    const p = await page('/system/users', 'admin', true)
    expect(p.router.currentRoute.value.name).toBe('login')
    expect(p.router.currentRoute.value.query.return).toBe('/system/users')
    await p.wrapper.get('input[type="email"]').setValue('admin@example.com')
    await p.wrapper.get('input[type="password"]').setValue('Original password 123')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.router.currentRoute.value.path).toBe('/system/users')
    expect(p.wrapper.get('h1').text()).toBe('用户')
  })
  it('registers the production system parent, lazy leaf and inherited authorization metadata', async () => {
    await page('/')
    const { router } = await import('../router')
    await router.push('/system')
    expect(router.currentRoute.value.path).toBe('/system/users')
    expect(router.currentRoute.value.meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
    expect(
      router
        .getRoutes()
        .filter((route) => route.meta.navigation)
        .map((route) => route.path),
    ).toEqual(['/system'])
    router.options.history.destroy()
  })
})
