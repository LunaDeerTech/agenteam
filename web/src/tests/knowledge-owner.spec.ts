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

describe('Knowledge production page with the original Session and Project workspace', () => {
  it('starts unselected, expands without selecting, and reads a selectable parent as safe text', async () => {
    const f = await page()
    expect(f.auth.state.user?.role).toBe('user')
    expect(f.wrapper.text()).toContain('选择一篇文档')
    expect(
      f.wrapper
        .get('nav[aria-label="项目导航"] a[href="/owner/demo/knowledge"]')
        .attributes('aria-current'),
    ).toBe('page')
    expect(f.fetch.mock.calls.filter(([url]) => url.startsWith(base)).map(([url]) => url)).toEqual([
      `${base}/children?parent_document_id=null&limit=50`,
    ])
    tree().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await flushPromises()
    expect(tree().getAttribute('aria-expanded')).toBe('true')
    expect(tree(id(21)).getAttribute('aria-level')).toBe('2')
    expect(f.wrapper.text()).toContain('选择一篇文档')
    tree().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(f.wrapper.get('pre[aria-label="当前文档正文"]').text()).toBe(content().text.text)
    expect(f.wrapper.find('.canonical-text img').exists()).toBe(false)
    expect(f.wrapper.text()).toContain('索引失败')
    expect(button('从开头重读').disabled).toBe(false)
    expect(button('重读文档树').disabled).toBe(false)
    expect(f.auth.state.busy).toBe(false)
  })
  it('opens a document deep link using ancestors as a path without inventing a child page', async () => {
    const f = await page(`/owner/demo/knowledge/${id(21)}`)
    expect(f.wrapper.get('h2').text()).toBe('子文档')
    expect(f.wrapper.get('nav[aria-label="面包屑"]').text()).toContain('父文档')
    expect(f.wrapper.find(`[data-tree-id="${id(21)}"]`).exists()).toBe(false)
    expect(f.fetch.mock.calls.filter(([url]) => url.includes('/children?'))).toHaveLength(1)
    button('读取子文档').click()
    await flushPromises()
    expect(tree(id(21))).toBeTruthy()
    expect(f.wrapper.get('h2').text()).toBe('子文档')
  })
  it.each(['deleted', 'empty', 'file'] as const)(
    'shows %s independently without parser or download work',
    async (kind) => {
      const file = { ...row(), source_kind: 'file', media_type: 'application/pdf' }
      const f = await page(`/owner/demo/knowledge/${id(20)}`, (url) => {
        if (url.pathname === `${base}/${id(20)}` && kind === 'deleted')
          return json({
            deleted: { id: id(20), project_id: id(10), content_version: '2', deleted_at: time },
          })
        if (url.pathname === `${base}/${id(20)}` && kind === 'file') return json({ active: file })
        if (url.pathname.endsWith('/content'))
          return json(
            kind === 'file'
              ? { document: file, unavailable: 'dependency_unbound' }
              : content(20, ''),
          )
      })
      expect(f.wrapper.text()).toContain(
        kind === 'deleted'
          ? '文档已删除'
          : kind === 'empty'
            ? '正文为空'
            : '此文档暂不支持正文读取',
      )
      expect(f.wrapper.find('pre').exists()).toBe(false)
      expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
      if (kind === 'deleted')
        expect(
          f.fetch.mock.calls.some(
            ([url]) => url.endsWith('/ancestors') || url.includes('/content?'),
          ),
        ).toBe(false)
    },
  )
  it('shows local denial and a working explicit retry without changing the current identity', async () => {
    const f = await page()
    f.intercept(async (path, init) =>
      path === `${base}/${id(20)}` ? problem(403, 'FORBIDDEN') : f.normal(path, init),
    )
    await clickParent()
    expect(f.wrapper.text()).toContain('文档不可用')
    expect(f.wrapper.find('pre').exists()).toBe(false)
    expect(f.auth.state.phase).toBe('authenticated')
    expect(button('重新读取文档').disabled).toBe(false)
    f.intercept(f.normal)
    button('重新读取文档').click()
    await flushPromises()
    expect(f.wrapper.get('pre').text()).toBe(content().text.text)
  })
  it('moves by the returned byte offset and clears the old segment when its version changes', async () => {
    const f = await page(`/owner/demo/knowledge/${id(20)}`, (url) => {
      if (!url.pathname.endsWith('/content')) return
      const offset = url.searchParams.get('byte_offset')!
      return json(
        offset === '0'
          ? content(20, 'a'.repeat(65536), '1', offset, true)
          : content(20, '新版本', '2', offset),
      )
    })
    expect(button('下一段').disabled).toBe(false)
    button('下一段').click()
    await flushPromises()
    expect(f.fetch.mock.calls.at(-1)?.[0]).toBe(
      `${base}/${id(20)}/content?byte_offset=65536&max_bytes=65536`,
    )
    expect(f.wrapper.text()).toContain('文档版本已变化')
    expect(f.wrapper.find('pre').exists()).toBe(false)
    expect(button('从开头重读').disabled).toBe(false)
  })
  it('closes the document drawer on Escape and returns focus to its trigger', async () => {
    await page()
    const trigger = button('文档树')
    trigger.focus()
    trigger.click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    trigger.click()
    await flushPromises()
    tree(id(20), document.querySelector('[role="dialog"]')!).click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.querySelector('pre')).not.toBeNull()
    expect(document.activeElement).toBe(trigger)
  })
  it('keeps the real Cookie lane busy after Stop until the original response tail returns', async () => {
    const f = await page()
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    releases.push(release)
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode(JSON.stringify(content())))
        c.close()
      },
    })
    const cancel = body.cancel.bind(body)
    body.cancel = async (reason) => {
      await cancel(reason)
      await held
    }
    f.intercept(async (path, init) =>
      path.includes('/content?')
        ? new Response(body, { headers: { 'Content-Type': 'application/json' } })
        : f.normal(path, init),
    )
    await clickParent()
    expect(f.wrapper.find('pre').exists()).toBe(false)
    button('停止本次读取').click()
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    expect(button('从开头重读').disabled).toBe(true)
    expect(f.wrapper.find('pre').exists()).toBe(false)
    release()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(button('从开头重读').disabled).toBe(false)
    expect(f.wrapper.find('pre').exists()).toBe(false)
  })
})

describe('Knowledge strict return leaves', () => {
  it('accepts only canonical knowledge leaves while preserving existing Project settings', () => {
    expect(safeReturnTarget(`/Owner/Demo/knowledge/${id(20)}`)).toBe(
      `/owner/demo/knowledge/${id(20)}`,
    )
    expect(safeReturnTarget('/owner/demo/knowledge')).toBe('/owner/demo/knowledge')
    expect(safeReturnTarget('/owner/demo/settings/general')).toBe('/owner/demo/settings/general')
    for (const path of [
      `/owner/demo/knowledge/${id(20)}?x=1`,
      `/owner/demo/knowledge/${id(20)}#x`,
      `/owner/demo/knowledge/%30${id(20).slice(1)}`,
      '/owner/demo/knowledge/',
      '/owner/demo/knowledge/new',
      `/owner/demo/knowledge/${id(20)}/content`,
      '/owner/demo/knowledge/01970000-0000-4000-8000-000000000020',
    ]) {
      expect(projectRoute(path)).toBeNull()
      expect(safeReturnTarget(path)).toBe('/')
    }
  })
})
