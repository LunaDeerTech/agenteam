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
import { fixture, id, lookup, json, token } from './knowledge-delete.fixture'
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
async function page() {
  const f = fixture()
  selected.auth = f.auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, f.auth)
  await router.push(`/owner/delete/knowledge/${id(20)}`)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { ...f, wrapper }
}
function button(label: string, parent: ParentNode = document) {
  const matches = [...parent.querySelectorAll<HTMLButtonElement>('button')].filter((b) => {
    const copy = b.cloneNode(true) as HTMLElement
    copy.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove())
    return (b.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
  })
  expect(matches).toHaveLength(1)
  return matches[0]!
}
async function openPreview() {
  button('删除子树').click()
  await vi.waitFor(() =>
    expect(document.querySelector('ul[aria-label="完整删除范围"]')?.textContent).toContain(
      '子文档',
    ),
  )
}
async function confirm() {
  const checkbox = document.querySelector<HTMLInputElement>(
    'form[aria-label="确认删除子树"] input[type="checkbox"]',
  )!
  checkbox.checked = true
  checkbox.dispatchEvent(new Event('change', { bubbles: true }))
  await flushPromises()
  button('确认删除').click()
  await flushPromises()
}
describe('Knowledge subtree deletion installed in App', () => {
  it('requires the explicit checkbox and shows a current tombstone without rendering the token or old body', async () => {
    const f = await page()
    button('删除子树').focus()
    await openPreview()
    expect(button('确认删除').disabled).toBe(true)
    expect(document.body.innerHTML).not.toContain(token)
    expect(document.querySelectorAll('ul[aria-label="完整删除范围"] li')).toHaveLength(2)
    await confirm()
    expect(document.body.textContent).toContain('已读取当前删除状态')
    expect(f.wrapper.find('pre[aria-label="当前文档正文"]').exists()).toBe(false)
    expect(f.wrapper.find(`[data-tree-id="${id(20)}"]`).exists()).toBe(false)
    expect(f.wrapper.find(`[data-tree-id="${id(22)}"]`).exists()).toBe(true)
    const dialog = document.querySelector('[role="dialog"]')!
    button('关闭', dialog.querySelector('.dialog-footer') ?? dialog).click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(document.querySelector('.knowledge-document > h2'))
    const heading = document.querySelector('.knowledge-document h3')!
    expect(heading.textContent?.trim()).toBe('!文档已删除')
    const accessibleHeading = heading.cloneNode(true) as HTMLElement
    accessibleHeading.querySelectorAll('[aria-hidden="true"]').forEach((node) => node.remove())
    expect(accessibleHeading.textContent?.trim()).toBe('文档已删除')

    const trigger = button('文档树')
    trigger.focus()
    trigger.click()
    await flushPromises()
    const drawer = document.querySelector('[role="dialog"]')!
    expect(drawer.querySelector('.dialog-header h2')?.textContent).toBe('文档树')
    const tree = drawer.querySelector('[role="tree"][aria-label="知识库文档"]')!
    expect(tree.querySelector(`[data-tree-id="${id(20)}"]`)).toBeNull()
    expect(tree.querySelector(`[data-tree-id="${id(21)}"]`)).toBeNull()
    expect(tree.querySelector(`[data-tree-id="${id(22)}"]`)?.getAttribute('role')).toBe('treeitem')
    drawer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    trigger.click()
    await flushPromises()
    const sibling = document.querySelector<HTMLElement>(
      `[role="dialog"] [role="tree"] [data-tree-id="${id(22)}"]`,
    )!
    sibling.focus()
    expect(document.activeElement).toBe(sibling)
    sibling.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('.knowledge-document > h2')?.textContent).toBe('保留旁支')
    expect(document.querySelector('pre.canonical-text')?.textContent).toBe('正文')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/delete-subtree'))).toHaveLength(1)
  })
  it('retains the actual rename draft when deletion is declined, and loads a preview only after explicit discard', async () => {
    const f = await page()
    button('改名').click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('form[aria-label="文档改名"] input')!
    input.value = '未保存标题'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    button('删除子树').click()
    await flushPromises()
    expect(document.body.textContent).toContain('放弃本次改名？')
    button('继续处理').click()
    await flushPromises()
    expect(input.value).toBe('未保存标题')
    expect(f.fetch.mock.calls.some(([p]) => p.endsWith('/delete-preview'))).toBe(false)
    button('删除子树').click()
    await flushPromises()
    button('放弃并继续').click()
    await vi.waitFor(() =>
      expect(document.querySelector('ul[aria-label="完整删除范围"]')).not.toBeNull(),
    )
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-preview'))).toHaveLength(1)
  })
  it('keeps Unknown explicit, queries the original full intent, and issues no second delete', async () => {
    const f = await page()
    await openPreview()
    f.intercept(async (path, init) => {
      if (path.endsWith('/delete-subtree')) {
        f.setDeleted()
        throw new TypeError('lost receipt')
      }
      if (path.endsWith('/commands/lookup')) return json(lookup())
      return f.normal(path, init)
    })
    await confirm()
    expect(document.body.textContent).toContain('删除结果尚不确定')
    expect(document.body.textContent).not.toContain('删除已确认')
    const original = f.fetch.mock.calls.find(([p]) => p.endsWith('/delete-subtree'))![1]
    button('查询原删除结果').click()
    await flushPromises()
    expect(document.body.textContent).toContain('已读取当前删除状态')
    const check = f.fetch.mock.calls.find(([p]) => p.endsWith('/commands/lookup'))![1]
    expect(new Headers(check.headers).get('Idempotency-Key')).toBe(
      new Headers(original.headers).get('Idempotency-Key'),
    )
    expect(JSON.parse(check.body as string).request).toEqual(JSON.parse(original.body as string))
    expect(f.fetch.mock.calls.filter(([p]) => p.endsWith('/delete-subtree'))).toHaveLength(1)
  })
})
