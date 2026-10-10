import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import App from '../App.vue'
import { installAuthentication, projectRoute } from '../router/auth'
import { base, barrier, fixture, json, metadata, project, receipt } from './project-secrets-fixture'
let wrapper: VueWrapper | undefined, router: Router | undefined
beforeEach(() =>
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  ),
)
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  router?.options.history.destroy()
  router = undefined
  selected.auth?.leave()
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function page(archived = false) {
  const f = fixture()
  if (archived)
    f.project({ ...project(), lifecycle: 'archived', archived_at: metadata().created_at })
  selected.auth = f.auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, f.auth)
  await router.push('/owner/demo/settings/secrets')
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { ...f, router, wrapper }
}
function button(label: string, root: ParentNode = document): HTMLButtonElement {
  const match = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) => (node.getAttribute('aria-label') ?? node.textContent?.trim()) === label,
  )
  if (!match) throw new Error('Missing UI action: ' + label)
  return match
}
function field(label: string): HTMLInputElement | HTMLTextAreaElement {
  const node = [...document.querySelectorAll('label')].find(
    (node) => node.textContent?.trim().replace(/\s*\*$/, '') === label,
  )
  const input = node?.htmlFor ? document.getElementById(node.htmlFor) : null
  if (!(input instanceof HTMLInputElement || input instanceof HTMLTextAreaElement))
    throw new Error('Missing field: ' + label)
  return input
}
async function fill(label: string, value: string) {
  const node = field(label)
  node.value = value
  node.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function click(label: string, root: ParentNode = document) {
  button(label, root).click()
  await flushPromises()
}
const topDialog = () => [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].at(-1)!
describe('Project Owner Secret settings through real Router, Session and Workspace', () => {
  it('reads safe metadata and performs create, metadata update and delete without revealing values', async () => {
    const f = await page()
    expect(
      document.querySelector('nav[aria-label="项目导航"] a[aria-current="page"]')?.textContent,
    ).toBe('项目设置')
    expect(document.body.textContent).toContain('TOKEN')
    await click('创建 Secret')
    await fill('名称', 'NEW_TOKEN')
    await fill('描述', 'Safe')
    await fill('新的 Secret 值', 'LOCAL-ONE-TIME-CANARY')
    document
      .querySelector<HTMLFormElement>('#secret-editor')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    expect(field('新的 Secret 值').value).toBe('')
    await flushPromises()
    expect(document.body.innerHTML).not.toContain('LOCAL-ONE-TIME-CANARY')
    expect(document.body.textContent).toContain('NEW_TOKEN')
    expect(document.body.textContent).toContain('命令已确认')
    await click('编辑 Secret')
    await fill('描述', 'Updated safe')
    await click('保存')
    await flushPromises()
    const patch = f.fetch.mock.calls.find(([, init]) => init.method === 'PATCH')!
    expect(JSON.parse(patch[1].body as string)).toMatchObject({
      expected_version: '1',
      request: { description: 'Updated safe' },
    })
    expect(JSON.parse(patch[1].body as string).request).not.toHaveProperty('value')
    await click('删除 Secret')
    await click('确认删除')
    expect(document.body.textContent).not.toContain('NEW_TOKEN')
    expect(
      f.fetch.mock.calls
        .filter(([, init]) => ['POST', 'PATCH', 'DELETE'].includes(init.method!))
        .map(([, init]) => init.method),
    ).toEqual(['POST', 'PATCH', 'DELETE'])
  })
  it('clears input while a write is still held, blocks duplication, and checks Unknown without replay', async () => {
    const f = await page(),
      hold = barrier<Response>()
    f.intercept(async (url, init) =>
      url === base && init.method === 'POST' ? hold.promise : f.normal(url, init),
    )
    await click('创建 Secret')
    await fill('名称', 'NEW')
    await fill('新的 Secret 值', 'NEVER-STORE-CANARY')
    await click('保存')
    expect(field('新的 Secret 值').value).toBe('')
    expect(button('保存').disabled).toBe(true)
    expect(f.auth.state.busy).toBe(true)
    hold.resolve(new Response('broken', { headers: { 'Content-Type': 'application/json' } }))
    await flushPromises()
    expect(document.body.innerHTML).not.toContain('NEVER-STORE-CANARY')
    expect(topDialog().textContent).toContain('结果尚未确认')
    f.intercept()
    await click('查证原命令', topDialog())
    expect(f.auth.secrets.progress).toMatchObject({
      phase: 'uncertain',
      observation: 'not_observed',
    })
    expect(f.fetch.mock.calls.filter(([url]) => url === base)).toHaveLength(1)
    expect(
      f.fetch.mock.calls.find(([url]) => url.endsWith('/commands/lookup'))![1].body,
    ).not.toContain('NEVER-STORE-CANARY')
  })
  it('confirms local discard before global Session restore and joins the actual navigation', async () => {
    const f = await page()
    await click('创建 Secret')
    await fill('名称', 'UNSAVED')
    await fill('新的 Secret 值', 'DISCARD-CANARY')
    const before = f.fetch.mock.calls.filter(([url]) => url === '/api/v1/session').length
    const first = f.router.push('/owner/demo/settings/general')
    await flushPromises()
    expect(topDialog().textContent).toContain('放弃本地编辑')
    expect(f.fetch.mock.calls.filter(([url]) => url === '/api/v1/session')).toHaveLength(before)
    await click('继续留在此页', topDialog())
    await first
    expect(f.router.currentRoute.value.fullPath).toBe('/owner/demo/settings/secrets')
    expect(field('新的 Secret 值').value).toBe('DISCARD-CANARY')
    const next = f.router.push('/owner/demo/settings/general')
    await flushPromises()
    await click('确认放弃', topDialog())
    await next
    await flushPromises()
    expect(f.router.currentRoute.value.fullPath).toBe('/owner/demo/settings/general')
    expect(document.body.innerHTML).not.toContain('DISCARD-CANARY')
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })
  it('shows archived safe metadata with disabled creation and retains strict route rejection', async () => {
    await page(true)
    expect(button('创建 Secret').disabled).toBe(true)
    expect(document.body.textContent).toContain('TOKEN')
    expect(projectRoute('/owner/demo/settings/secrets')).toMatchObject({
      suffix: '/settings/secrets',
    })
    for (const path of [
      '/owner/demo/settings/secrets/',
      '/owner/demo/settings/%73ecrets',
      '/owner/demo/settings/secrets?x=1',
      '/owner/demo/settings/secret',
    ])
      expect(projectRoute(path)).toBeNull()
  })
})
