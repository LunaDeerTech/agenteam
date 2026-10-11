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
import { installAuthentication } from '../router/auth'
import {
  titleSessionFixture,
  titleChild,
  titleRoot,
  titleBase,
} from './knowledge-title-search.fixture'
let wrapper: VueWrapper | undefined, router: Router | undefined
beforeEach(() => {
  vi.stubGlobal('CSS', { escape: (v: string) => v })
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  selected.auth?.leave()
  await flushPromises()
  router?.options.history.destroy()
  router = undefined
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function fixture() {
  const f = titleSessionFixture()
  selected.auth = f.auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, f.auth)
  await router.push('/owner/title-search/knowledge')
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { ...f, wrapper }
}
function button(label: string, within: ParentNode = document) {
  const found = [...within.querySelectorAll<HTMLButtonElement>('button')].find((b) => {
    const copy = b.cloneNode(true) as HTMLElement
    copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove())
    return (b.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
  })
  if (!found) throw new Error('Missing title-search control: ' + label)
  return found
}
async function search(w: VueWrapper) {
  await w.get('.desktop-tree input[type="search"]').setValue('%_!')
  await w.get('.desktop-tree form[role="search"]').trigger('submit')
  await flushPromises()
}
async function chooseRoot(w: VueWrapper) {
  await w.get(`[data-tree-id="${titleRoot().id}"]`).trigger('click')
  await flushPromises()
}
describe('Knowledge title search installed in the real App page', () => {
  it('finds an unloaded descendant, then selects through the original metadata/path/body reader', async () => {
    const f = await fixture()
    expect(f.wrapper.find(`[data-tree-id="${titleChild().id}"]`).exists()).toBe(false)
    await search(f.wrapper)
    expect(f.wrapper.get('ul[aria-label="标题查找结果"]').text()).toContain('父文档')
    button(titleChild().title).click()
    await flushPromises()
    expect(f.wrapper.get('h2').text()).toBe(titleChild().title)
    expect(f.wrapper.get('pre[aria-label="当前文档正文"]').text()).toBe('当前子文档正文 %_!')
    expect(f.fetch.mock.calls.filter(([p]) => p.includes('/search-titles?'))).toHaveLength(1)
    expect(f.fetch.mock.calls.filter(([p]) => p.includes('/children?'))).toHaveLength(1)
    expect(f.fetch.mock.calls.some(([p]) => p === `${titleBase}/${titleChild().id}`)).toBe(true)
  })
  it('keeps the existing rename confirmation and rejects a hit invalidated while confirmation is pending', async () => {
    const f = await fixture()
    await search(f.wrapper)
    await chooseRoot(f.wrapper)
    button('改名').click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('form[aria-label="文档改名"] input')!
    input.value = '未保存标题'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    button(titleChild().title).click()
    await flushPromises()
    expect(document.body.textContent).toContain('放弃本次改名？')
    await f.wrapper.get('.desktop-tree input[type="search"]').setValue('changed')
    button('放弃并继续').click()
    await flushPromises()
    expect(f.wrapper.get('h2').text()).toBe(titleRoot().title)
    expect(f.fetch.mock.calls.some(([p]) => p === `${titleBase}/${titleChild().id}`)).toBe(false)
  })
  it('preserves the actual text draft when selection is declined, then selects only after explicit discard', async () => {
    const f = await fixture()
    await search(f.wrapper)
    await chooseRoot(f.wrapper)
    button('编辑正文').click()
    await flushPromises()
    const input = document.querySelector<HTMLTextAreaElement>(
      'form[aria-label="知识库文本"] textarea',
    )!
    input.value = '未保存正文'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    button(titleChild().title).click()
    await flushPromises()
    expect(document.body.textContent).toContain('放弃本次文本编辑？')
    button('继续编辑').click()
    await flushPromises()
    expect(input.value).toBe('未保存正文')
    expect(f.wrapper.get('h2').text()).toBe(titleRoot().title)
    button(titleChild().title).click()
    await flushPromises()
    button('放弃并继续').click()
    await flushPromises()
    expect(f.wrapper.get('h2').text()).toBe(titleChild().title)
    expect(f.wrapper.get('pre').text()).toBe('当前子文档正文 %_!')
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
})
