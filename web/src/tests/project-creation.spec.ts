import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import type { Fetch } from '../api/client'
import {
  captureProjectCreate,
  createProjectCreationAPI,
  type ProjectCreateInput,
  type ProjectCreationResult,
} from '../api/project-creation'
import { createSessionController, type SessionController } from '../composables/useSession'
import { createProjectCreation } from '../composables/useProjectCreation'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-12T10:00:00.000001Z'
const input = { project_id: id(10), name: 'New.Project', description: '原描述\n保留空白  ' }
const ready = (projectID = id(10)): ProjectCreationResult => ({
  state: 'ready',
  project: {
    id: projectID,
    owner_user_id: id(1),
    name: 'Renamed',
    normalized_name: 'renamed',
    description: '',
    lifecycle: 'archived',
    version: '3',
    current_sprint_id: null,
    created_at: at,
    updated_at: at,
    archived_at: at,
  },
})
const accepted = (
  projectID = id(10),
  state: 'accepted' | 'initializing' | 'failed' = 'accepted',
): ProjectCreationResult => ({
  state: 'accepted',
  operation: {
    id: id(20),
    project_id: projectID,
    state,
    version: '1',
    created_at: at,
    updated_at: at,
    ...(state === 'failed' ? { safe_reason: 'dependency_unavailable' as const } : {}),
  },
  status_url: `/api/v1/project-creations/${id(20)}`,
})
const json = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
const write = { csrfToken: 'S'.repeat(43), key: 'original-project-1' }
const signal = () => new AbortController().signal
describe('Project creation API', () => {
  it('binds 201 ready to current owner and original ID, allowing a renamed archived replay', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(ready(), 201))
    expect(await createProjectCreationAPI(fetcher).create(id(1), input, write)).toEqual(ready())
    expect(fetcher.mock.calls[0]?.[0]).toBe('/api/v1/projects')
    expect(fetcher.mock.calls[0]?.[1]).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      redirect: 'error',
      body: JSON.stringify(input),
      headers: { 'Idempotency-Key': write.key, 'X-CSRF-Token': write.csrfToken },
    })
  })
  it('reads accepted and failed operations by the exact status ID without issuing a POST', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(accepted(id(10), 'failed')))
    expect(await createProjectCreationAPI(fetcher).get(id(1), id(10), id(20), signal())).toEqual(
      accepted(id(10), 'failed'),
    )
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/api/v1/project-creations/${id(20)}`)
    expect(fetcher.mock.calls[0]?.[1].method).toBe('GET')
    expect(fetcher.mock.calls[0]?.[1].headers).not.toHaveProperty('Idempotency-Key')
  })
  it('rejects mismatched statuses, unsafe operations, wrong identities and noncanonical recovery URLs', async () => {
    const a = accepted()
    if (a.state !== 'accepted') throw new Error('fixture')
    const r = ready()
    if (r.state !== 'ready') throw new Error('fixture')
    const rows = [
      json(a, 201),
      json(r, 202),
      json(a, 202),
      json({ ...a, status_url: 'https://elsewhere.test/recover' }, 202, {
        Location: 'https://elsewhere.test/recover',
      }),
      json({ ...a, operation: { ...a.operation, project_id: id(99) } }, 202, {
        Location: a.status_url,
      }),
      json({ ...a, operation: { ...a.operation, safe_reason: 'raw-provider-error' } }, 202, {
        Location: a.status_url,
      }),
      json({ ...a, operation: { ...a.operation, state: 'failed' } }, 202, {
        Location: a.status_url,
      }),
      json({ ...r, project: { ...r.project, owner_user_id: id(99) } }, 201),
      json({ ...r, project: { ...r.project, id: id(99) } }, 201),
    ]
    for (const response of rows) {
      await expect(
        createProjectCreationAPI(async () => response).create(id(1), input, write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(
      createProjectCreationAPI(async () => json(a)).get(id(1), id(10), id(21), signal()),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('validates original payload before dispatch and preserves description presence', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(ready(), 201))
    for (const patch of [
      { project_id: id(10).replace('-7000-', '-4000-') },
      { name: 'invalid space' },
      { name: '..' },
      { description: '\r' },
      { extra: 'ignored?' },
    ]) {
      await expect(
        Promise.resolve().then(() =>
          createProjectCreationAPI(fetcher).create(id(1), { ...input, ...patch }, write),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    }
    expect(fetcher).not.toHaveBeenCalled()
    expect(captureProjectCreate({ project_id: id(10), name: 'new' })).not.toHaveProperty(
      'description',
    )
    expect(
      captureProjectCreate({ project_id: id(10), name: 'new', description: '' }),
    ).toHaveProperty('description', '')
  })
})
const owners: SessionController[] = []
const releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((auth) => auth.leave())
  releases.splice(0).forEach((release) => release())
  await flushPromises()
})
async function fixture() {
  let current: SessionView = {
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
    session: { id: id(2), issued_at: at, absolute_expires_at: at, idle_expires_at: at },
    csrf_token: write.csrfToken,
  }
  let stored: ProjectCreateInput | null = null
  let mode: 'unknown' | 'accepted' | 'failed' | 'ready' = 'unknown'
  let hold: Promise<void> | null = null
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return json(current)
    if (path === '/api/v1/projects' && init.method === 'POST') {
      stored = JSON.parse(init.body as string) as ProjectCreateInput
      await hold
      if (mode === 'unknown') throw new Error('original transport result unavailable')
      if (mode === 'ready') return json(ready(stored.project_id), 201)
      const body = accepted(stored.project_id, mode)
      return json(body, 202, { Location: `/api/v1/project-creations/${id(20)}` })
    }
    if (path === `/api/v1/project-creations/${id(20)}` && stored)
      return json(
        mode === 'ready'
          ? ready(stored.project_id)
          : accepted(stored.project_id, mode === 'failed' ? 'failed' : 'initializing'),
      )
    throw new Error('Unexpected endpoint')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[19] = { projectCreation: createProjectCreationAPI(fetcher) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  return {
    auth,
    fetcher,
    stored: () => stored!,
    mode: (value: typeof mode) => {
      mode = value
    },
    replaceSession() {
      current = { ...current, session: { ...current.session, id: id(3) } }
    },
    hold() {
      let release!: () => void
      hold = new Promise<void>((resolve) => {
        release = resolve
      })
      releases.push(release)
      return release
    },
  }
}
describe('Project creation original intent', () => {
  it('keeps unknown input private and explicitly recovers only the same ID, key and complete payload', async () => {
    const f = await fixture()
    const original = { name: 'original', description: '  full\nbody  ' }
    await expect(f.auth.projectCreation.start(original)).rejects.toMatchObject({
      kind: 'transport',
    })
    original.name = 'mutated'
    expect(f.auth.projectCreation.progress).toMatchObject({
      phase: 'uncertain',
      name: 'original',
      contextValid: true,
    })
    expect(JSON.stringify(f.auth.projectCreation.progress)).not.toContain('full')
    await expect(
      f.auth.projectCreation.start({ name: 'other', description: '' }),
    ).rejects.toMatchObject({ kind: 'busy' })
    f.auth.projects.abandon()
    await f.auth.restore()
    expect(f.auth.projectCreation.progress?.canResume).toBe(true)
    f.mode('ready')
    await f.auth.projectCreation.resume()
    const posts = f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(posts).toHaveLength(2)
    expect(posts[1]?.[1].body).toBe(posts[0]?.[1].body)
    expect(posts[1]?.[1].headers).toEqual(posts[0]?.[1].headers)
    expect(JSON.parse(posts[0]![1].body as string)).toMatchObject({
      name: 'original',
      description: '  full\nbody  ',
    })
    expect(f.auth.projectCreation.progress?.phase).toBe('ready')
  })
  it('shows accepted and failed state, checks without POST and resumes the original creation on demand', async () => {
    const f = await fixture()
    f.mode('accepted')
    await f.auth.projectCreation.start({ name: 'original', description: '' })
    f.mode('failed')
    await f.auth.projectCreation.check()
    expect(f.auth.projectCreation.progress?.phase).toBe('failed')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(1)
    f.mode('ready')
    await f.auth.projectCreation.resume()
    expect(f.auth.projectCreation.progress?.result).toEqual(ready(f.stored().project_id))
  })
  it('forgets prior-session creation and keeps the original request owner until its actual return', async () => {
    const f = await fixture(),
      release = f.hold()
    const creating = f.auth.projectCreation.start({ name: 'held', description: '' })
    const failure = expect(creating).rejects.toMatchObject({ kind: 'cancelled' })
    await flushPromises()
    f.auth.leave()
    expect(f.auth.projectCreation.progress).toBeNull()
    expect(f.auth.state.busy).toBe(true)
    release()
    await failure
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    await f.auth.restore()
    await expect(f.auth.projectCreation.resume()).rejects.toMatchObject({ kind: 'invalid-input' })
  })
  it('retains recovery across form disposal and navigates using the current ready name', async () => {
    const f = await fixture(),
      refresh = vi.fn()
    let form = createProjectCreation(f.auth, refresh)
    form.show()
    form.draft.name = 'original'
    form.draft.description = 'private original'
    await form.act('create')
    form.dispose()
    form = createProjectCreation(f.auth, refresh)
    expect(form.editable.value).toBe(false)
    expect(form.progress.value?.phase).toBe('uncertain')
    f.mode('ready')
    await form.act('resume')
    expect(form.projectPath.value).toBe('/owner/renamed')
    expect(refresh).toHaveBeenCalledOnce()
    f.replaceSession()
    await f.auth.restore()
    expect(form.projectPath.value).toBe('')
    form.dispose()
  })
})
