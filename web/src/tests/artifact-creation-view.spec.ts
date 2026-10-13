import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import App from '../App.vue'
import { installAuthentication } from '../router/auth'
import {
  artifactBase,
  artifactContent,
  artifactJSON,
  artifactPage,
  artifactPath,
  artifactProblem,
  createdRecord,
  creationSessionFixture,
} from './artifact-creation.fixture'
import type { ArtifactContentInput } from '../api/artifact-creation'
let wrapper: VueWrapper | undefined, router: Router | undefined
beforeEach(() => {
  vi.stubGlobal('CSS', { escape: (value: string) => value })
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  // Only controlled focus geometry; real viewport/theme acceptance is browser work.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await flushPromises()
  router?.options.history.destroy()
  router = undefined
  selected.auth?.leave()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
function button(label: string, within: ParentNode = document) {
  const value = [...within.querySelectorAll<HTMLButtonElement>('button')].find((el) => {
    const clone = el.cloneNode(true) as HTMLElement
    clone.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
    return (el.getAttribute('aria-label') ?? clone.textContent?.trim()) === label
  })
  if (!value) throw new Error(`Missing button ${label}`)
  return value
}
async function click(label: string, within: ParentNode = document) {
  await new DOMWrapper(button(label, within)).trigger('click')
  await flushPromises()
}
function dialog() {
  return document.querySelector<HTMLElement>('[role="dialog"]')!
}
async function field(label: string, value: string) {
  const element = [...dialog().querySelectorAll('label')].find((item) =>
    item.textContent?.includes(label),
  )!
  await new DOMWrapper(document.getElementById(element.htmlFor) as HTMLInputElement).setValue(value)
}
async function fixture() {
  const f = creationSessionFixture()
  selected.auth = f.auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, f.auth)
  await router.push(artifactPath)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await vi.waitFor(() => expect(button('新建文本产物').disabled).toBe(false))
  return { ...f, router }
}
const input: ArtifactContentInput = {
  name: '  output.txt  ',
  media_type: 'text/plain',
  content: ' <script>literal</script> 中文\n ',
}
async function fill() {
  button('新建文本产物').focus()
  await click('新建文本产物')
  await field('产物名称', input.name)
  await field('正文', input.content)
}
async function submit() {
  await new DOMWrapper(dialog().querySelector('form')!).trigger('submit')
}
describe('Artifact creation actual App, controls and navigation', () => {
  it('creates from the actual page, shows independent read confirmation, renders text safely and restores trigger focus', async () => {
    const f = await fixture(),
      row = createdRecord(input)
    f.intercept((path, init) =>
      path.endsWith('/create-content')
        ? Promise.resolve(artifactJSON({ artifact: row }))
        : path.startsWith(artifactBase + '?')
          ? Promise.resolve(artifactJSON(artifactPage([row])))
          : path.includes('/content?')
            ? Promise.resolve(artifactJSON(artifactContent(row, input.content)))
            : f.normal(path, init),
    )
    await fill()
    await submit()
    await vi.waitFor(() => expect(dialog().textContent).toContain('已读取当前产物列表与正文'))
    expect([...dialog().querySelectorAll('textarea')].every((node) => node.disabled)).toBe(true)
    // Real transitions retain a child dialog while beforeLeave hides/inerts
    // its overlay ancestor. That retired panel must not block trigger focus.
    const retiring = document.createElement('div')
    retiring.setAttribute('aria-hidden', 'true')
    retiring.setAttribute('inert', '')
    retiring.innerHTML = '<section role="dialog">Retiring surface</section>'
    document.body.append(retiring)
    await click('返回产物', dialog())
    retiring.remove()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('pre')?.textContent).toBe(input.content)
    expect(document.querySelector('pre script')).toBeNull()
    expect(document.activeElement).toBe(button('新建文本产物'))
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
  })
  it('asks before route Session restore, preserves declined navigation, and explicitly retries only an observed original request', async () => {
    const f = await fixture()
    f.intercept((path, init) =>
      path.endsWith('/create-content')
        ? Promise.resolve(artifactProblem(503))
        : f.normal(path, init),
    )
    await fill()
    await submit()
    await vi.waitFor(() => expect(button('查询原创建结果').disabled).toBe(false))
    const sessionReads = f.fetch.mock.calls.filter(([path]) => path === '/api/v1/session').length
    const navigation = f.router.push('/')
    await vi.waitFor(() => expect(document.body.textContent).toContain('放弃本次产物编辑？'))
    expect(f.fetch.mock.calls.filter(([path]) => path === '/api/v1/session')).toHaveLength(
      sessionReads,
    )
    await click('继续编辑')
    await navigation
    expect(f.router.currentRoute.value.fullPath).toBe(artifactPath)
    expect((dialog().querySelectorAll('textarea')[1] as HTMLTextAreaElement).value).toBe(
      input.content,
    )
    f.intercept((path, init) =>
      path.endsWith('/lookup')
        ? Promise.resolve(artifactJSON({ state: 'not_observed', artifact: null }))
        : f.normal(path, init),
    )
    await click('查询原创建结果')
    await vi.waitFor(() => expect(button('重试原创建请求').disabled).toBe(false))
    expect(dialog().textContent).not.toContain('取消原创建')
    const row = createdRecord(input)
    f.intercept((path, init) =>
      path.endsWith('/create-content')
        ? Promise.resolve(artifactJSON({ artifact: row }))
        : path.startsWith(artifactBase + '?')
          ? Promise.resolve(artifactJSON(artifactPage([row])))
          : path.includes('/content?')
            ? Promise.resolve(artifactJSON(artifactContent(row, input.content)))
            : f.normal(path, init),
    )
    await click('重试原创建请求')
    await vi.waitFor(() => expect(dialog().textContent).toContain('已读取当前产物列表与正文'))
    const commands = f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(commands.map(([path]) => path)).toEqual([
      artifactBase + '/create-content',
      artifactBase + '/creation-commands/lookup',
      artifactBase + '/create-content',
    ])
    expect(new Set(commands.map(([, init]) => init.body)).size).toBe(1)
    expect(
      new Set(commands.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
  })
  it('offers explicit pending cancellation and keeps cleanup pending distinct from a cancelled result', async () => {
    const f = await fixture()
    let observation = 'pending'
    f.intercept((path, init) =>
      path.endsWith('/create-content')
        ? Promise.resolve(artifactProblem(503))
        : path.endsWith('/lookup')
          ? Promise.resolve(artifactJSON({ state: observation, artifact: null }))
          : path.endsWith('/cancel')
            ? Promise.resolve(artifactJSON({ state: 'cancel_requested', cleanup: 'pending' }))
            : f.normal(path, init),
    )
    await fill()
    await submit()
    await vi.waitFor(() => expect(button('查询原创建结果').disabled).toBe(false))
    expect(dialog().textContent).not.toContain('取消原创建')
    await click('查询原创建结果')
    await vi.waitFor(() => expect(button('取消原创建').disabled).toBe(false))
    await click('取消原创建')
    await vi.waitFor(() => expect(dialog().textContent).toContain('清理尚未完成'))
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(3)
    observation = 'cancelled'
    await click('查询原创建结果')
    await vi.waitFor(() => expect(dialog().textContent).toContain('原创建已取消，清理已完成'))
    await click('关闭', dialog().querySelector('.dialog-footer')!)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
