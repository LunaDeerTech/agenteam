import { afterEach, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '/workspace/agenteam/web/src/api/account'
import { AccountFailure, type Fetch, type Problem } from '/workspace/agenteam/web/src/api/client'
import { createProjectOwnerAPI, type Project } from '/workspace/agenteam/web/src/api/project-owner'
import { createSystemAccountAPI } from '/workspace/agenteam/web/src/api/system-account'
import { createSystemModelSelectionAPI } from '/workspace/agenteam/web/src/api/system-model-selection'
import { createSessionController } from '/workspace/agenteam/web/src/composables/useSession'
import { createProjectWorkspace } from '/workspace/agenteam/web/src/composables/useProjectWorkspace'

export { AccountFailure, flushPromises }
export const id = (n: number) => `01900000-0000-7000-8000-${n.toString().padStart(12, '0')}`
export const time = '2026-10-08T10:00:00.000000Z'
export const project = (override: Partial<Project> = {}): Project => ({
  id: id(10), owner_user_id: id(1), name: 'Demo', normalized_name: 'demo',
  description: 'original', lifecycle: 'active', version: '1', current_sprint_id: null,
  created_at: time, updated_at: time, archived_at: null, ...override,
})
export const session = (admin = false): SessionView => ({
  user: { id: id(1), email: 'owner@example.test', username: 'owner', display_name: '',
    role: admin ? 'admin' : 'user', theme: 'system', version: '1', initial_password_suggestion: false },
  session: { id: id(2), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
export const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: {
    'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
    'X-Request-ID': id(99),
  },
})
export const problem = (code: string, status: number, commit_state: Problem['commit_state'] = 'not_started', field_errors?: Problem['field_errors']) => json({
  type: 'urn:agenteam:problem:test', title: 'Rejected', status, code, commit_state,
  request_id: id(99), detail: '', instance: '/api/v1/projects', ...(field_errors ? { field_errors } : {}),
}, status)

const cleanups: (() => void | Promise<void>)[] = []
const releases: (() => void)[] = []
export function barrier<T>(fallback: T) {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
export function cleanup(work: () => void | Promise<void>) { cleanups.push(work) }
afterEach(async () => {
  for (const release of releases.splice(0)) release()
  await flushPromises()
  for (const work of cleanups.splice(0).reverse()) await work()
  await flushPromises()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

export async function fixture(admin = false) {
  let current = project(), view = session(admin), intercept: Fetch | undefined
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return json(view)
    if (path.endsWith('/logout')) return new Response(null, { status: 204 })
    if (path.endsWith('/bootstrap')) return json({ csrf_token: 'A'.repeat(43), challenge_modes: ['rotate'], delivery_channel: 'backend_log' })
    if (path === '/api/v1/me') return json({ user: view.user, avatar: null })
    if (path.startsWith('/api/v1/system/users')) return json({ items: [] })
    if (path.startsWith('/api/v1/projects?')) return json({ items: [{ id: current.id, name: current.name, version: current.version, lifecycle: 'active', description: current.description }], next_cursor: null })
    if (path.startsWith('/api/v1/projects/resolve?')) return json(current)
    if (path.endsWith('/commands/lookup')) return json({ state: 'committed', result: { command: 'update', project: current } })
    if (path.startsWith('/api/v1/projects/')) {
      if (init.method === 'PATCH') {
        const input = JSON.parse(init.body as string)
        current = project({ ...current, ...('name' in input ? { name: input.name, normalized_name: input.name.toLowerCase() } : {}), ...('description' in input ? { description: input.description } : {}), version: String(BigInt(current.version) + 1n) })
      }
      return json(current)
    }
    throw new Error(`Unexpected controlled endpoint ${path}`)
  }
  const fetcher = vi.fn<Fetch>((path, init) => intercept ? intercept(path, init) : normal(path, init))
  const auth = createSessionController(createAccountAPI(fetcher), createSystemAccountAPI(fetcher), undefined, undefined, undefined, createSystemModelSelectionAPI(fetcher), undefined, undefined, undefined, undefined, undefined, undefined, createProjectOwnerAPI(fetcher))
  await auth.restore()
  const replacements: string[] = []
  const workspace = createProjectWorkspace(auth, async (path) => {
    replacements.push(path)
    workspace.afterNavigation(path)
  })
  cleanup(() => { workspace.dispose(); auth.leave() })
  return {
    auth, workspace, fetcher, normal, replacements,
    intercept(next?: Fetch) { intercept = next },
    current(next: Project) { current = next },
    session(next: SessionView) { view = next },
    requests() { return fetcher.mock.calls.filter(([path]) => path.startsWith('/api/v1/projects')) },
    writes() { return fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH') },
    async open(path = '/owner/demo/settings/general') { workspace.afterNavigation(path); await flushPromises() },
  }
}
