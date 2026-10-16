import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { createAccountAPI } from '../api/account'
import { createSystemRunnersAPI } from '../api/system-runners'
import { safeReturnTarget } from '../router/auth'
import type { Fetch } from '../api/client'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemRunnersView from '../views/system/SystemRunnersView.vue'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const session = {
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
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const row = () => ({
  id: id(3),
  name: 'Fixture Runner',
  description: '',
  tags: [] as string[],
  root_path: '/owned/workspace',
  version: '1',
  credential_generation: '1',
  status: 'offline',
  public_key_fingerprint: null,
  enrolled_at: null,
  last_seen_at: null,
  last_hello: null,
  created_at: time,
  updated_at: time,
})
const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(90),
    },
  })
const unknown = () =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      detail: '',
      status: 503,
      code: 'COMMIT_UNKNOWN',
      instance: '/',
      request_id: id(90),
      commit_state: 'unknown',
    },
    503,
  )
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
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  selected.auth?.leave()
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
function button(name: string, root: ParentNode = document) {
  const el = Array.from(root.querySelectorAll<HTMLButtonElement>('button')).find(
    (b) => b.querySelector('.button-label')?.textContent?.trim() === name,
  )
  expect(el, `button ${name}`).toBeTruthy()
  return el!
}
function field(name: string) {
  const label = Array.from(document.querySelectorAll('label')).find((l) =>
    l.textContent?.trim().startsWith(name),
  )
  expect(label, `field ${name}`).toBeTruthy()
  const input = document.getElementById(label!.htmlFor) as HTMLInputElement | HTMLTextAreaElement
  expect(input).toBeTruthy()
  return new DOMWrapper(input)
}
async function click(name: string, root: ParentNode = document) {
  button(name, root).click()
  await flushPromises()
}
async function page(role = 'admin') {
  let item = row(),
    exists = false,
    cut = false,
    committed = false
  const receipt = (command: string, changed = true) => ({
    command_id: id(10),
    command,
    runner: { ...item },
    changed,
  })
  const fetcher = vi.fn<Fetch>(async (url, init) => {
    if (url === '/api/v1/session') return json({ ...session, user: { ...session.user, role } })
    if (init.method === 'GET')
      return json(url.includes('?') ? { items: exists ? [item] : [], next: null } : item)
    const input = JSON.parse(init.body as string)
    if (url.endsWith('/lookup'))
      return json(
        committed
          ? { state: 'committed', receipt: receipt('runner.create'), token_available: false }
          : { state: 'not_observed', token_available: false },
      )
    const create = !!input.runner_id
    if (create) {
      item = {
        ...item,
        id: input.runner_id,
        name: input.name,
        description: input.description,
        tags: input.tags,
        root_path: input.root_path,
      }
      exists = true
      if (cut) return unknown()
    } else {
      item = {
        ...item,
        name: input.name ?? item.name,
        description: input.description ?? item.description,
        tags: input.tags ?? item.tags,
        version: String(BigInt(input.expected_version) + 1n),
      }
    }
    const command = create
      ? 'runner.create'
      : url.endsWith('/enrollment')
        ? 'runner.enrollment.issue'
        : url.endsWith('/revoke')
          ? 'runner.revoke'
          : 'runner.update'
    const material = create || command === 'runner.enrollment.issue'
    return json({
      receipt: receipt(command),
      token_available: material,
      ...(material
        ? {
            enrollment_token: 'A'.repeat(43),
            expires_at: new Date(Date.now() + 600000).toISOString().replace('Z', '000Z'),
          }
        : {}),
    })
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { systemRunners: createSystemRunnersAPI(fetcher) }
  selected.auth = createSessionController(...args)
  await selected.auth.restore()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/system',
        component: SystemSettingsView,
        children: [{ path: 'runners', component: SystemRunnersView }],
      },
      { path: '/away', component: { template: '<h1>Other page</h1>' } },
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ],
  })
  await router.push('/system/runners')
  await router.isReady()
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  wrapper = mount(RouterView, { attachTo: host, global: { plugins: [router] } })
  await flushPromises()
  return {
    router,
    fetcher,
    get item() {
      return item
    },
    cut() {
      cut = true
    },
    commitLookup() {
      committed = true
    },
  }
}
async function createForm() {
  await click('创建 Runner')
  expect(document.activeElement).toBe(field('Runner 名称').element)
  await field('Runner 名称').setValue('Fixture Runner')
  await field('描述').setValue('Configured without a device connection.')
  await field('标签').setValue('two\none')
  await field('根目录').setValue('/owned/workspace')
}
async function submit() {
  const form = document.querySelector('form')!
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
}
describe('System Runner management real Vue components', () => {
  it('shows the system menu and manages metadata with one-time material explicitly closed before another operation', async () => {
    const f = await page()
    expect(safeReturnTarget('/system/runners')).toBe('/system/runners')
    expect(document.body.textContent).toContain('设备列表')
    await createForm()
    await submit()
    const token = document.querySelector<HTMLInputElement>('input[aria-label="登记 token"]')!
    expect(token.type).toBe('password')
    expect(token.value.length).toBe(43)
    expect(document.body.textContent).not.toContain('A'.repeat(43))
    const copy = vi.fn(async (_value: string) => {})
    vi.stubGlobal('navigator', { clipboard: { writeText: copy } })
    await click('复制登记材料')
    expect(copy).toHaveBeenCalledWith(token.value)
    await click('关闭登记材料')
    expect(token.value).toBe('')
    expect(document.querySelector('[aria-label="登记 token"]')).toBeNull()
    await click('编辑 Runner')
    expect(document.activeElement).toBe(field('Runner 名称').element)
    expect(field('根目录').attributes('readonly')).toBeDefined()
    await field('描述').setValue('Updated description')
    await submit()
    expect(document.body.textContent).toContain('Updated description')
    const writes = f.fetcher.mock.calls.filter(([, i]) => i.method !== 'GET')
    expect(writes).toHaveLength(2)
    expect(JSON.parse(writes[0]![1].body as string).tags).toEqual(['one', 'two'])
    expect(JSON.parse(writes[1]![1].body as string)).not.toHaveProperty('root_path')
    await click('撤销 Runner 凭据')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('旧密钥')
    await click('取消')
    expect(f.fetcher.mock.calls.filter(([, i]) => i.method !== 'GET')).toHaveLength(2)
  })
  it('keeps Unknown recovery usable inside the editor and never creates again or invents old material', async () => {
    const f = await page()
    f.cut()
    await createForm()
    await submit()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('只检查原请求')
    await click('检查原请求')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    f.commitLookup()
    await click('检查原请求')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('[aria-label="登记 token"]')).toBeNull()
    expect(document.body.textContent).toContain('历史回执不含登记材料')
    expect(
      f.fetcher.mock.calls.filter(([u, i]) => i.method === 'POST' && !u.endsWith('/lookup')),
    ).toHaveLength(1)
    await click('重新签发登记材料')
    await click('确认重新签发')
    expect(
      document.querySelector<HTMLInputElement>('[aria-label="登记 token"]')?.value.length,
    ).toBe(43)
  })
  it('confirms dirty route departure and clears the form after explicit discard', async () => {
    const f = await page()
    await createForm()
    const moving = f.router.push('/away')
    await flushPromises()
    expect(document.body.textContent).toContain('放弃未保存的修改？')
    await click('继续编辑')
    await moving
    expect(f.router.currentRoute.value.path).toBe('/system/runners')
    const leaving = f.router.push('/away')
    await flushPromises()
    await click('放弃修改')
    await leaving
    await flushPromises()
    expect(document.querySelector('form')).toBeNull()
    expect(document.body.textContent).toContain('Other page')
  })
  it('clears existing token DOM on route departure and never exposes the page for ordinary users', async () => {
    const f = await page()
    await createForm()
    await submit()
    const token = document.querySelector<HTMLInputElement>('[aria-label="登记 token"]')!
    await f.router.push('/away')
    await flushPromises()
    expect(token.value).toBe('')
    expect(document.querySelector('[aria-label="登记 token"]')).toBeNull()
    wrapper!.unmount()
    wrapper = undefined
    selected.auth!.leave()
    document.body.innerHTML = ''
    const ordinary = await page('user')
    expect(document.body.textContent).toContain('需要系统管理员权限')
    expect(
      ordinary.fetcher.mock.calls.filter(([u]) => u.startsWith('/api/v1/system/runners')),
    ).toHaveLength(0)
  })
})
