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
import { moveFixture, id, json } from './knowledge-move.fixture'
let wrapper: VueWrapper | undefined,
  router: Router | undefined,
  fixture: Awaited<ReturnType<typeof moveFixture>> | undefined
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
  // Synthetic visibility only. Real layout, keyboard focus and themes remain PW.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await fixture?.dispose()
  fixture = undefined
  router?.options.history.destroy()
  router = undefined
  selected.auth = null
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  await flushPromises()
})
async function page() {
  const f = (fixture = await moveFixture())
  f.editor.dispose()
  f.owner.dispose()
  f.workspace.dispose()
  selected.auth = f.auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, f.auth)
  await router.push(`/owner/move/knowledge/${id(11)}`)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return f
}
function buttons(label: string, within: ParentNode = document) {
  return [...within.querySelectorAll<HTMLButtonElement>('button')].filter((node) => {
    const content = node.cloneNode(true) as HTMLElement
    content.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
    return (node.getAttribute('aria-label') ?? content.textContent?.trim()) === label
  })
}
function button(label: string, within: ParentNode = document) {
  const found = buttons(label, within)[0]
  if (!found) throw new Error(`Missing controlled button: ${label}`)
  return found
}
function dialog(title: string) {
  return [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].find((node) =>
    node.textContent?.includes(title),
  )!
}
async function click(label: string, within: ParentNode = document) {
  button(label, within).click()
  await flushPromises()
}
describe('Knowledge Move production page integration', () => {
  it('requires explicit discard of a Move selection before entering subtree deletion', async () => {
    const f = await page()
    await click('移动')
    await click('移到根层')
    await click('删除子树')
    expect(dialog('放弃本次移动？')).toBeTruthy()
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/delete-preview'))).toHaveLength(0)
    await click('继续处理', dialog('放弃本次移动？'))
    expect(dialog('移动文档')).toBeTruthy()
    expect(dialog('移动文档').textContent).toContain('目标位置：根层')
    // The new Delete request is made only after an explicit discard, through
    // its existing API. Its independently failed preview is not fabricated.
    f.intercept(async (path, init) => {
      if (path.endsWith('/delete-preview')) throw new TypeError('controlled preview failure')
      return f.normal(path, init)
    })
    await click('删除子树')
    await click('放弃并继续', dialog('放弃本次移动？'))
    expect(dialog('移动文档')).toBeUndefined()
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/delete-preview'))).toHaveLength(1)
    expect(
      f.fetch.mock.calls.filter(
        ([path]) => path.endsWith('/delete-subtree') || path.endsWith('/move'),
      ),
    ).toHaveLength(0)
  })

  it('selects a parent and saves through the real page/Session/API, then displays the current path/body', async () => {
    const f = await page()
    f.intercept(async (path, init) => {
      if (path.endsWith('/move')) {
        expect(JSON.parse(init.body as string)).toEqual({
          expected_parent_id: id(10),
          target_parent_id: id(12),
        })
        const moved = { ...f.documents.get(id(11))!, parent_document_id: id(12) }
        f.documents.set(id(11), moved)
        return json({ document: moved, changed: true })
      }
      return f.normal(path, init)
    })
    await click('移动')
    await click('选择 C 目标 为父文档')
    expect(wrapper!.find('h2').text()).toBe('B 当前文档')
    expect(dialog('移动文档').textContent).toContain('目标位置：C 目标')
    await click('确认移动')
    expect(document.body.textContent).toContain('移动已确认；已读取当前文档与位置。')
    expect(wrapper!.find('[aria-label="当前文档正文"]').text()).toBe('原正文')
    expect(wrapper!.find('.knowledge-document').text()).toContain('C 目标')
    const moveDialog = dialog('移动文档')
    expect(buttons('关闭', moveDialog)).toHaveLength(2)
    const footer = moveDialog.querySelector('footer')!
    expect(buttons('关闭', footer)).toHaveLength(1)
    await click('关闭', footer)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('keeps rename draft until its real confirmation is answered before opening Move', async () => {
    await page()
    await click('改名')
    const input = dialog('文档改名').querySelector<HTMLInputElement>('input')!
    input.value = '未保存标题'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    await click('移动')
    expect(dialog('放弃本次改名？')).toBeTruthy()
    expect(dialog('移动文档')).toBeUndefined()
    await click('继续处理', dialog('放弃本次改名？'))
    expect(input.value).toBe('未保存标题')
    await click('移动')
    await click('放弃并继续', dialog('放弃本次改名？'))
    expect(dialog('移动文档')).toBeTruthy()
  })
  it('keeps the text draft and blocks document navigation until Move selection is explicitly discarded', async () => {
    await page()
    await click('编辑正文')
    const textarea =
      document.querySelector<HTMLTextAreaElement>('textarea[aria-label="文本正文"]') ??
      document.querySelector<HTMLTextAreaElement>('textarea')!
    textarea.value = '未保存正文'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    await click('移动')
    const confirmation = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].find((d) =>
      d.textContent?.includes('放弃本次文本编辑？'),
    )!
    expect(confirmation).toBeTruthy()
    await click('继续编辑', confirmation)
    expect(textarea.value).toBe('未保存正文')
    await click('移动')
    await click(
      '放弃并继续',
      [...document.querySelectorAll<HTMLElement>('[role="dialog"]')].find((d) =>
        d.textContent?.includes('放弃本次文本编辑？'),
      )!,
    )
    await click('移到根层')
    const navigation = router!.push(`/owner/move/knowledge/${id(12)}`)
    await flushPromises()
    expect(dialog('放弃本次移动？')).toBeTruthy()
    await click('继续处理', dialog('放弃本次移动？'))
    await navigation
    expect(router!.currentRoute.value.params.document_id).toBe(id(11))
    expect(dialog('移动文档')).toBeTruthy()
  })
})
