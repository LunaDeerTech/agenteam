import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSkillOwnerAPI } from '../api/skill-owner'
import { type Fetch } from '../api/client'
import { createSessionController, type SessionController } from '../composables/useSession'
import * as sessionModule from '../composables/useSession'
import {
  createProjectWorkspace,
  projectWorkspaceKey,
  type ProjectWorkspace,
} from '../composables/useProjectWorkspace'
import { useSkillOwner, type SkillOwnerController } from '../composables/useSkillOwner'
import ProjectSkillsView from '../views/projects/ProjectSkillsView.vue'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-10T12:00:00.123456Z',
  base = `/api/v1/projects/${id(10)}/skills`
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
const metadata = (n = 20) => ({
  id: id(n),
  project_id: id(10),
  name: 'Add Skills',
  normalized_name: 'add-skills',
  description: '真实接口的受控值',
  protected: true,
  current_revision: '1',
  version: '1',
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': id(99),
    },
  })
const problem = (status: number) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code: status === 401 ? 'UNAUTHENTICATED' : status === 409 ? 'INVALID_STATE' : 'NOT_FOUND',
      instance: '/api/v1',
      request_id: id(99),
      commit_state: 'not_started',
    },
    status,
  )
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const pages: SkillOwnerController[] = [],
  workspaces: ProjectWorkspace[] = [],
  owners: SessionController[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  for (const page of pages.splice(0)) page.dispose()
  for (const release of releases.splice(0)) release()
  await flushPromises()
  for (const workspace of workspaces.splice(0)) workspace.dispose()
  for (const owner of owners.splice(0)) owner.leave()
  await flushPromises()
})
async function fixture(lifecycle: Project['lifecycle'] = 'active', bind = true) {
  const project: Project = {
    id: id(10),
    owner_user_id: id(1),
    name: 'Demo',
    normalized_name: 'demo',
    description: '',
    lifecycle,
    version: '1',
    current_sprint_id: null,
    created_at: time,
    updated_at: time,
    archived_at: lifecycle === 'archived' ? time : null,
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    if (path === '/api/v1/session') return json(session)
    if (path === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${id(10)}`)
      return json(project)
    if (path === base) return json({ items: [metadata()] })
    if (path === `${base}/${id(20)}`) return json(metadata())
    if (path === `${base}/${id(21)}`) return json(metadata(21))
    throw new Error('unexpected controlled request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[16] = { skills: createSkillOwnerAPI(fetch) }
  const auth = createSessionController(...args)
  owners.push(auth)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspaces.push(workspace)
  if (bind) {
    workspace.afterNavigation('/owner/demo/settings/skills')
    await flushPromises()
  }
  const location = shallowRef<{ projectPath: string; skillID: string | null }>({
    projectPath: '/owner/demo',
    skillID: null,
  })
  const page = useSkillOwner(auth, workspace, location)
  pages.push(page)
  await flushPromises()
  return {
    auth,
    workspace,
    location,
    page,
    fetch,
    normal,
    intercept(value?: Fetch) {
      intercept = value
    },
  }
}

describe('Skills current Human owner and page lifetime', () => {
  it('reads the directory for an absent optional route ID through the actual view and owner', async () => {
    const f = await fixture()
    // Retire the fixture's state-only page before mounting the actual view.
    // Only the singleton lookup and network are controlled; the Session,
    // Workspace, route parser, view and Skills controller are the real code.
    f.page.dispose()
    f.fetch.mockClear()
    const singleton = vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
    const path = '/owner/demo/settings/skills'
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/:username/:project_name/settings/skills/:skill_id?',
          component: ProjectSkillsView,
        },
      ],
    })
    await router.push(path)
    await router.isReady()
    const wrapper = mount(RouterView, {
      attachTo: document.body,
      global: { plugins: [router], provide: { [projectWorkspaceKey as symbol]: f.workspace } },
    })
    try {
      await flushPromises()
      expect(router.currentRoute.value.params.skill_id).toBe('')
      expect(wrapper.findAll('[aria-label="技能目录"] li')).toHaveLength(1)
      expect(wrapper.get('h2').text()).toBe('Add Skills')
      await wrapper.get(`a[href="${path}/${id(20)}"]`).trigger('click')
      await flushPromises()
      expect(wrapper.get('h1').text()).toBe('技能详情')
      expect(wrapper.get('.skill-facts').text()).toContain(id(20))
      await wrapper.get(`a[href="${path}"]`).trigger('click')
      await flushPromises()
      expect(wrapper.get('h1').text()).toBe('项目技能库')
      expect(wrapper.findAll('[aria-label="技能目录"] li')).toHaveLength(1)
      expect(
        f.fetch.mock.calls
          .filter(([request]) => request.startsWith(base))
          .map(([request]) => request),
      ).toEqual([base, `${base}/${id(20)}`, base])
      expect(f.auth.state.busy).toBe(false)
    } finally {
      wrapper.unmount()
      await flushPromises()
      router.options.history.destroy()
      singleton.mockRestore()
      document.body.innerHTML = ''
    }
  })
  it.each(['active', 'archived'] as const)(
    'reads directory and detail in %s through the original Session owner',
    async (lifecycle) => {
      const f = await fixture(lifecycle)
      expect(f.page.state.phase).toBe('ready')
      expect(f.page.state.items).toEqual([metadata()])
      f.location.value = { projectPath: '/owner/demo', skillID: id(20) }
      await flushPromises()
      expect(f.page.state.detail).toEqual(metadata())
      expect(f.auth.state.busy).toBe(false)
      expect(
        f.fetch.mock.calls.filter(([path]) => path.startsWith(base)).map(([path]) => path),
      ).toEqual([base, `${base}/${id(20)}`])
    },
  )
  it('does not issue a Skill read before current Workspace authorization', async () => {
    const f = await fixture('active', false)
    expect(f.page.visible.value).toBe(false)
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith(base))).toBe(false)
    f.workspace.afterNavigation('/owner/demo/settings/skills')
    await flushPromises()
    expect(f.page.state.items).toHaveLength(1)
  })
  it.each([403, 404])('clears a refused current directory and detail after %s', async (status) => {
    const f = await fixture()
    f.intercept(async () => problem(status))
    f.page.refresh()
    await flushPromises()
    expect(f.page.state.items).toHaveLength(0)
    expect(f.page.state.phase).toBe('unavailable')
    f.intercept(undefined)
    f.location.value = { projectPath: '/owner/demo', skillID: id(20) }
    await flushPromises()
    expect(f.page.state.detail).not.toBeNull()
    f.intercept(async () => problem(status))
    f.page.refresh()
    await flushPromises()
    expect(f.page.state.detail).toBeNull()
    expect(f.auth.state.phase).toBe('authenticated')
  })
  it('shows unpublished state as an error and recovers only through explicit reread', async () => {
    const f = await fixture()
    f.intercept(async () => problem(409))
    f.page.refresh()
    await flushPromises()
    expect(f.page.state.phase).toBe('error')
    expect(f.page.state.message).toContain('尚未就绪')
    expect(f.page.state.items).toHaveLength(0)
    f.intercept(undefined)
    f.page.refresh()
    await flushPromises()
    expect(f.page.state.phase).toBe('ready')
  })
  it('keeps the Cookie owner busy until the original cancelled reader actually joins', async () => {
    const f = await fixture(),
      tail = deferred<void>()
    releases.push(() => tail.resolve())
    f.intercept(
      async () =>
        new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(new TextEncoder().encode('{"items":['))
            },
            cancel() {
              return tail.promise
            },
          }),
          { headers: { 'Content-Type': 'application/json' } },
        ),
    )
    f.page.refresh()
    await flushPromises()
    f.page.cancel()
    await flushPromises()
    expect(f.auth.state.busy).toBe(true)
    const count = f.fetch.mock.calls.length
    f.page.refresh()
    expect(f.fetch.mock.calls.length).toBe(count)
    tail.resolve()
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.page.state.phase).toBe('error')
    f.intercept(undefined)
    f.page.refresh()
    await flushPromises()
    expect(f.page.state.items).toEqual([metadata()])
  })
  it('rejects a late old 401 without invalidating the next selection', async () => {
    const f = await fixture(),
      old = deferred<Response>()
    releases.push(() => old.resolve(problem(401)))
    f.intercept((path, init) => (path === `${base}/${id(20)}` ? old.promise : f.normal(path, init)))
    f.location.value = { projectPath: '/owner/demo', skillID: id(20) }
    await flushPromises()
    f.location.value = { projectPath: '/owner/demo', skillID: id(21) }
    old.resolve(problem(401))
    await flushPromises()
    expect(f.auth.state.phase).toBe('authenticated')
    expect(f.page.state.detail?.id).toBe(id(21))
    expect(f.auth.state.busy).toBe(false)
  })
})
