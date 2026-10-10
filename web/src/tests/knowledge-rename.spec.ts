import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeOwnerAPI } from '../api/knowledge-owner'
import type { Fetch } from '../api/client'
import { installAuthentication, projectRoute, safeReturnTarget } from '../router/auth'
import App from '../App.vue'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-10T12:00:00.123456Z',
  base = `/api/v1/projects/${id(10)}/knowledge/documents`
const session: SessionView = {
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
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
}
const project: Project = {
  id: id(10),
  owner_user_id: id(1),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: null,
}
const row = (n = 20, version = '1') => ({
  id: id(n),
  project_id: id(10),
  parent_document_id: n === 21 ? id(20) : null,
  title: n === 21 ? '子文档' : '父文档',
  content_version: version,
  source_kind: 'text',
  media_type: 'text/markdown',
  status: 'active',
  indexing_status: 'failed',
  created_by: { kind: 'human', user_id: id(1) },
  created_at: time,
  updated_at: time,
})
const content = (
  n = 20,
  text = '当前 <img src=x onerror=alert(1)> 正文',
  version = '1',
  offset = '0',
  truncated = false,
) => ({
  document: row(n, version),
  text: {
    text,
    next_byte_offset: (
      BigInt(offset) + BigInt(new TextEncoder().encode(text).byteLength)
    ).toString(),
    truncated,
  },
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const problem = (status: number, code: string) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: '',
      detail: '',
      status,
      code,
      instance: base,
      request_id: id(99),
      commit_state: 'not_started',
    },
    status,
  )
let wrapper: VueWrapper | undefined, router: Router | undefined
const releases: (() => void)[] = []
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
  // Controlled layout only: actual viewport and rendered geometry remain browser work.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  for (const release of releases.splice(0)) release()
  await flushPromises()
  router?.options.history.destroy()
  router = undefined
  selected.auth?.leave()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function page(path = '/owner/demo/knowledge', response?: (url: URL) => Response | undefined) {
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (path === '/api/v1/session') return json(session)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${id(10)}`)
      return json(project)
    const replacement = response?.(url)
    if (replacement) return replacement
    if (url.pathname === `${base}/children`)
      return json({
        items: url.searchParams.get('parent_document_id') === 'null' ? [row()] : [row(21)],
      })
    const n = url.pathname.includes(id(21)) ? 21 : 20
    if (url.pathname.endsWith('/ancestors')) return json({ items: n === 21 ? [row()] : [] })
    if (url.pathname.endsWith('/content')) return json(content(n))
    if (url.pathname === `${base}/${id(n)}`) return json({ active: row(n) })
    throw new Error('Unexpected controlled request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  args[16] = { knowledgeCommands: createKnowledgeCommandsAPI(fetch) }
  const auth = (selected.auth = createSessionController(...args))
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, auth)
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return {
    auth,
    fetch,
    normal,
    wrapper,
    router,
    intercept(value: Fetch) {
      intercept = value
    },
  }
}
function button(text: string, within: ParentNode = document) {
  const found = [...within.querySelectorAll<HTMLButtonElement>('button')].find((entry) => {
    const content = entry.cloneNode(true) as HTMLElement
    content.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
    return (entry.getAttribute('aria-label') ?? content.textContent?.trim()) === text
  })
  if (!found) throw new Error(`Missing button: ${text}`)
  return found
}
function tree(idValue = id(20), within: ParentNode = document) {
  const found = within.querySelector<HTMLElement>(`[data-tree-id="${idValue}"]`)
  if (!found) throw new Error('Missing tree item')
  return found
}
async function clickParent() {
  tree().click()
  await flushPromises()
}

describe('Knowledge rename production page', () => {
  it('edits through the real page, Session and API then displays current title/body/tree', async () => {
    const f = await page()
    await clickParent()
    let updated = false
    const doc = () => ({
      ...row(),
      title: updated ? '新文档标题' : '父文档',
      content_version: updated ? '2' : '1',
    })
    f.intercept(async (path, init) => {
      const url = new URL(path, 'https://owned.invalid')
      if (path.endsWith('/rename')) {
        updated = true
        expect(JSON.parse(init.body as string)).toEqual({
          expected_version: '1',
          title: '新文档标题',
        })
        return json({ document: doc() })
      }
      if (url.pathname.endsWith('/children')) return json({ items: [doc()] })
      if (url.pathname.endsWith('/ancestors')) return json({ items: [] })
      if (url.pathname.endsWith('/content')) return json({ ...content(), document: doc() })
      if (url.pathname === `${base}/${id(20)}`) return json({ active: doc() })
      return f.normal(path, init)
    })
    button('改名').click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[autocomplete="off"]')!
    input.value = '新文档标题'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    button('保存标题').click()
    await flushPromises()
    expect(document.body.textContent).toContain('改名已确认；已读取当前文档。')
    expect(f.wrapper.find('h2').text()).toBe('新文档标题')
    expect(tree().textContent).toContain('新文档标题')
    expect(f.wrapper.find('[aria-label="当前文档正文"]').text()).toContain('当前 <img')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/rename'))).toHaveLength(1)
    button('关闭', document.querySelector('[role="dialog"]')!).click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
  it('requires an explicit discard for same-component document route changes', async () => {
    const f = await page()
    await clickParent()
    button('改名').click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[autocomplete="off"]')!
    input.value = '保留的草稿'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const first = f.router.push(`/owner/demo/knowledge/${id(21)}`)
    await flushPromises()
    expect(document.body.textContent).toContain('放弃本次改名？')
    button('继续处理').click()
    await first
    await flushPromises()
    expect(f.router.currentRoute.value.fullPath).toBe('/owner/demo/knowledge')
    expect(input.value).toBe('保留的草稿')
    const second = f.router.push(`/owner/demo/knowledge/${id(21)}`)
    await flushPromises()
    button('放弃并继续').click()
    await second
    await flushPromises()
    expect(f.router.currentRoute.value.fullPath).toBe(`/owner/demo/knowledge/${id(21)}`)
    expect(f.wrapper.find('h2').text()).toBe('子文档')
    expect(f.fetch.mock.calls.some(([path]) => path.endsWith('/rename'))).toBe(false)
  })
})
