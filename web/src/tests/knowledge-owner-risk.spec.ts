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
import { installAuthentication } from '../router/auth'
import App from '../App.vue'

// Independent controlled-network risks, not a real Logout/Owner-transfer/PG test.
// App supplies the production Workspace; only its network and layout are controlled.
const uuid = (n: number) => `01980000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const instant = '2026-10-10T12:00:00.123456Z'
function session(n = 2): SessionView {
  return {
    user: {
      id: uuid(1),
      email: 'reader@example.test',
      username: 'reader',
      display_name: 'Reader',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: {
      id: uuid(n),
      issued_at: instant,
      idle_expires_at: instant,
      absolute_expires_at: instant,
    },
    csrf_token: 'R'.repeat(43),
  }
}
function project(n: number): Project {
  return {
    id: uuid(n),
    owner_user_id: uuid(1),
    name: n === 10 ? 'First' : 'Second',
    normalized_name: n === 10 ? 'first' : 'second',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: instant,
    updated_at: instant,
    archived_at: null,
  }
}
const base = (n: number) => `/api/v1/projects/${uuid(n)}/knowledge/documents`
const doc = (n: number) => ({
  id: uuid(n + 100),
  project_id: uuid(n),
  parent_document_id: null,
  title: n === 10 ? 'First private title' : 'Second private title',
  content_version: '1',
  source_kind: 'text',
  media_type: 'text/plain',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: uuid(1) },
  created_at: instant,
  updated_at: instant,
})
const bytes = (n: number, text = `Visible body ${n}`) => ({
  document: doc(n),
  text: { text, next_byte_offset: String(new TextEncoder().encode(text).length), truncated: false },
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status === 200 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': uuid(999),
    },
  })
function latch() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release }
}

let view: VueWrapper | undefined, router: Router | undefined
const gates: (() => void)[] = []
beforeEach(() => {
  vi.stubGlobal('CSS', { escape: (value: string) => value })
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener() {},
    removeEventListener() {},
  }))
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})
afterEach(async () => {
  view?.unmount()
  view = undefined
  for (const release of gates.splice(0)) release()
  await flushPromises()
  expect(selected.auth?.state.busy).toBe(false)
  router?.options.history.destroy()
  router = undefined
  selected.auth = null
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function fixture() {
  let currentSession = session()
  let intercept: ((path: string, init: RequestInit) => Response | undefined) | undefined
  const fetch = vi.fn<Fetch>(async (path, init) => {
    const replacement = intercept?.(path, init)
    if (replacement) return replacement
    const url = new URL(path, 'https://controlled.invalid')
    if (path === '/api/v1/session') return json(currentSession)
    if (url.pathname === '/api/v1/projects/resolve') {
      const name = url.searchParams.get('project_name')
      if (url.searchParams.get('username') === 'reader' && (name === 'first' || name === 'second'))
        return json(project(name === 'first' ? 10 : 20))
    }
    for (const n of [10, 20]) {
      if (path === `/api/v1/projects/${uuid(n)}`) return json(project(n))
      if (path === `${base(n)}/children?parent_document_id=null&limit=50`)
        return json({ items: [doc(n)] })
      if (path === `${base(n)}/${uuid(n + 100)}`) return json({ active: doc(n) })
      if (path === `${base(n)}/${uuid(n + 100)}/ancestors`) return json({ items: [] })
      if (path === `${base(n)}/${uuid(n + 100)}/content?byte_offset=0&max_bytes=65536`)
        return json(bytes(n))
    }
    throw new Error('Unexpected independent controlled request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  const auth = (selected.auth = createSessionController(...args))
  const { router: source } = await import('../router')
  source.options.history.destroy()
  router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, auth)
  await router.push('/reader/first/knowledge')
  await router.isReady()
  view = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  expect(auth.state.phase).toBe('authenticated')
  expect(view.find(`[data-tree-id="${uuid(110)}"]`).exists()).toBe(true)
  return {
    auth,
    fetch,
    view,
    router,
    intercept(value: typeof intercept) {
      intercept = value
    },
    session(value: SessionView) {
      currentSession = value
    },
  }
}
async function select(n: number) {
  await view!.get(`[data-tree-id="${uuid(n + 100)}"]`).trigger('click')
  await flushPromises()
}
function reread() {
  const buttons = view!.findAll('button').filter((entry) => entry.text().includes('从开头重读'))
  expect(buttons).toHaveLength(1)
  return buttons[0]!.trigger('click')
}

function heldBody(kind: 'reader' | 'outer') {
  const gate = latch(),
    entered = latch(),
    returned = latch()
  gates.push(gate.release)
  let cancelCalls = 0,
    outerCalls = 0
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(JSON.stringify(bytes(10, 'MUST NEVER PUBLISH'))))
      if (kind === 'outer') controller.close()
    },
    async cancel() {
      ++cancelCalls
      entered.release()
      await gate.promise
      returned.release()
    },
  })
  const originalCancel = stream.cancel.bind(stream)
  stream.cancel = async (reason) => {
    ++outerCalls
    await originalCancel(reason)
    if (kind === 'outer') {
      entered.release()
      await gate.promise
      returned.release()
    }
  }
  return {
    response: new Response(stream, {
      headers: { 'Content-Type': 'application/json', 'X-Request-ID': uuid(999) },
    }),
    entered: entered.promise,
    returned: returned.promise,
    release: gate.release,
    counts: () => ({ cancelCalls, outerCalls }),
  }
}

describe('Independent Knowledge authority and original transport retirement risks', () => {
  it('removes already displayed private observations on a current revoked-session response', async () => {
    const f = await fixture()
    await select(10)
    expect(f.view.get('pre[aria-label="当前文档正文"]').text()).toBe('Visible body 10')
    let rejected = 0
    f.intercept((path) => {
      if (!path.startsWith(base(10))) return undefined
      ++rejected
      return json(
        {
          type: 'urn:agenteam:problem:session-revoked',
          title: 'Session revoked',
          detail: 'Sign in again.',
          status: 401,
          code: 'SESSION_REVOKED',
          instance: new URL(path, 'https://controlled.invalid').pathname,
          request_id: uuid(999),
          commit_state: 'not_committed',
        },
        401,
      )
    })
    await reread()
    await flushPromises()
    expect(rejected).toBe(1)
    expect(f.auth.state.phase).toBe('unavailable')
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.auth.state.busy).toBe(false)
    expect(f.view.find('[data-tree-id]').exists()).toBe(false)
    expect(f.view.find('pre[aria-label="当前文档正文"]').exists()).toBe(false)
    expect(f.view.text()).not.toContain('First private title')
    expect(f.view.text()).not.toContain('Visible body 10')
    const calls = f.fetch.mock.calls.length
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(calls)
  })

  it('retains the original reader cancellation across Project navigation before the new Project can read', async () => {
    const f = await fixture(),
      held = heldBody('reader')
    let signal: AbortSignal | null | undefined
    f.intercept((path, init) => {
      if (path === `${base(10)}/${uuid(110)}/content?byte_offset=0&max_bytes=65536`) {
        signal = init.signal
        return held.response
      }
    })
    await select(10)
    expect(f.auth.state.busy).toBe(true)
    await f.router.push('/reader/second/knowledge')
    await held.entered
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    expect(held.counts()).toEqual({ cancelCalls: 1, outerCalls: 0 })
    expect(f.auth.state.busy).toBe(true)
    expect(f.view.text()).not.toContain('First private title')
    expect(f.view.text()).not.toContain('MUST NEVER PUBLISH')
    const heldCalls = f.fetch.mock.calls.length
    expect(
      f.fetch.mock.calls.some(
        ([path]) => path.includes('project_name=second') || path.startsWith(base(20)),
      ),
    ).toBe(false)
    await flushPromises()
    expect(f.fetch.mock.calls).toHaveLength(heldCalls)
    held.release()
    await held.returned
    await flushPromises()
    expect(held.counts()).toEqual({ cancelCalls: 1, outerCalls: 1 })
    expect(f.auth.state.busy).toBe(false)
    await select(20)
    expect(f.view.get('pre[aria-label="当前文档正文"]').text()).toBe('Visible body 20')
    expect(f.view.text()).not.toContain('MUST NEVER PUBLISH')
  })

  it('retains the original outer cancellation across identity retirement before restoring a new Session', async () => {
    const f = await fixture(),
      held = heldBody('outer')
    f.intercept((path) =>
      path === `${base(10)}/${uuid(110)}/content?byte_offset=0&max_bytes=65536`
        ? held.response
        : undefined,
    )
    await select(10)
    await held.entered
    expect(held.counts()).toEqual({ cancelCalls: 0, outerCalls: 1 })
    expect(f.auth.state.busy).toBe(true)
    expect(f.view.text()).not.toContain('MUST NEVER PUBLISH')
    const before = f.fetch.mock.calls.length
    f.session(session(3))
    f.auth.leave()
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.personalContext.identity).toBeNull()
    expect(f.auth.state.busy).toBe(true)
    expect(f.fetch.mock.calls).toHaveLength(before)
    expect(f.view.find('[data-tree-id]').exists()).toBe(false)
    expect(f.view.text()).not.toContain('First private title')
    expect(f.view.text()).not.toContain('MUST NEVER PUBLISH')
    held.release()
    await held.returned
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    f.intercept(undefined)
    await f.auth.restore()
    await flushPromises()
    expect(f.auth.personalContext.identity?.sessionID).toBe(uuid(3))
    expect(f.auth.state.phase).toBe('authenticated')
    await select(10)
    expect(f.view.get('pre[aria-label="当前文档正文"]').text()).toBe('Visible body 10')
    expect(f.view.text()).not.toContain('MUST NEVER PUBLISH')
    expect(held.counts()).toEqual({ cancelCalls: 0, outerCalls: 1 })
  })
})
