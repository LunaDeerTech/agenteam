import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProjectWorkStructureEditor from '../views/projects/ProjectWorkStructureEditor.vue'
import ProjectWorkTaskEditor from '../views/projects/ProjectWorkTaskEditor.vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createSessionController } from '../composables/useSession'
import type { SessionController } from '../composables/useSession'
import { createAccountAPI } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createWorkPlanningAPI } from '../api/work-planning'
import type { Fetch } from '../api/client'
const selectedSession = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => {
  const source = await original<typeof import('../composables/useSession')>()
  return { ...source, useSession: () => selectedSession.auth ?? source.useSession() }
})
import App from '../App.vue'
import {
  installAuthentication,
  installProjectModelSettingsNavigation,
  installProjectNavigation,
  installProjectWorkPlanningNavigation,
  projectRoute,
  safeReturnTarget,
  workPlanningRoute,
} from '../router/auth'

const id = '01900000-0000-7000-8000-000000000010'
const home = '/owner/owner.dot-name'
const cleanup: (() => void)[] = []
afterEach(() => {
  cleanup
    .splice(0)
    .reverse()
    .forEach((close) => close())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  selectedSession.auth = null
  document.body.innerHTML = ''
})

describe('Work planning production route boundary', () => {
  it.each([
    ['', 'explore', null],
    [`/milestones/${id}`, 'milestone', id],
    [`/sprints/${id}`, 'sprint', id],
    [`/tasks/${id}`, 'task', id],
  ] as const)('retains the explicit Explore destination %s after login', (tail, kind, target) => {
    const input = `/OwNeR/Owner.Dot-Name/tasks/explore${tail}`
    const path = `${home}/tasks/explore${tail}`
    expect(projectRoute(input)?.path).toBe(path)
    expect(safeReturnTarget(input)).toBe(path)
    expect(workPlanningRoute(input)).toEqual({ home, path, kind, id: target })
  })

  it.each([
    '/tasks',
    '/tasks/',
    '/tasks/explore/',
    '/tasks/Explore',
    '/tasks/explore?',
    '/tasks/explore?cursor=private',
    '/tasks/explore#draft',
    '/tasks/explore/../settings',
    '/tasks/explore/%74asks/' + id,
    '/tasks/explore/tasks/' + id.replace('-7000-', '-4000-'),
    '/tasks/explore/tasks/' + id.toUpperCase().replace('000010', '00001A'),
    '/tasks/explore/tasks/' + id + '/',
    '/tasks/explore/tasks/' + id + '/extra',
    '/tasks/explore/task/' + id,
    '/tasks/explore//tasks/' + id,
    '/tasks/explore\\tasks\\' + id,
    '/tasks/explore/tasks/null',
    '/tasks/explore/tasks/' + id.replace('-8000-', '-c000-'),
  ])('rejects unsafe or unimplemented Work path %s', (tail) => {
    expect(projectRoute(home + tail)).toBeNull()
    expect(workPlanningRoute(home + tail)).toBeNull()
    expect(safeReturnTarget(home + tail)).toBe('/')
  })

  it('keeps existing Project and static return paths distinct', () => {
    for (const suffix of [
      '',
      '/settings/general',
      '/settings/audit',
      '/settings/model-providers',
    ]) {
      expect(safeReturnTarget(home + suffix)).toBe(home + suffix)
      expect(workPlanningRoute(home + suffix)).toBeNull()
    }
    for (const path of ['/projects', '/settings/profile', '/system/providers'])
      expect(safeReturnTarget(path)).toBe(path)
    expect(projectRoute('/api/project/tasks/explore')).toBeNull()
    expect(projectRoute('/owner/../tasks/explore')).toBeNull()
  })
})

async function navigation() {
  const auth = createSessionController()
  const component = { template: '<div />' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/owner/:project/:pathMatch(.*)*', component, meta: { projectWorkspace: true } },
      { path: '/outside', component },
      { path: '/:pathMatch(.*)*', name: 'not-found', component },
    ],
  })
  cleanup.push(
    () => router.options.history.destroy(),
    () => auth.leave(),
  )
  await router.push(home + '/tasks/explore')
  installAuthentication(router, auth)
  return router
}
function owner(router: Router, calls: string[], name: string, answer: () => Promise<boolean>) {
  const hook = {
    confirmLeave: async () => {
      calls.push(name)
      return answer()
    },
    afterNavigation: () => calls.push(name + '-after'),
  }
  const install =
    name === 'owner'
      ? installProjectNavigation
      : name === 'model'
        ? installProjectModelSettingsNavigation
        : installProjectWorkPlanningNavigation
  cleanup.push(install(router, hook))
}

describe('Work planning joins existing navigation ownership', () => {
  it('asks Owner, Model, then Work and publishes only completed navigation', async () => {
    const router = await navigation()
    const calls: string[] = []
    for (const name of ['owner', 'model', 'work']) owner(router, calls, name, async () => true)
    await router.push(home + '/tasks/explore/tasks/' + id)
    expect(calls).toEqual(['owner', 'model', 'work', 'owner-after', 'model-after', 'work-after'])
    expect(router.currentRoute.value.path).toBe(home + '/tasks/explore/tasks/' + id)
  })

  it('stops at a Model cancellation without asking or clearing Work', async () => {
    const router = await navigation()
    const calls: string[] = []
    owner(router, calls, 'owner', async () => true)
    owner(router, calls, 'model', async () => false)
    owner(router, calls, 'work', async () => true)
    await router.push('/outside')
    expect(calls).toEqual(['owner', 'model'])
    expect(router.currentRoute.value.path).toBe(home + '/tasks/explore')
  })

  it('waits for the Work choice and preserves the current route on cancellation', async () => {
    const router = await navigation()
    const calls: string[] = []
    let decide!: (value: boolean) => void
    owner(router, calls, 'owner', async () => true)
    owner(router, calls, 'model', async () => true)
    owner(router, calls, 'work', () => new Promise<boolean>((resolve) => (decide = resolve)))
    const pending = router.push('/outside')
    await flushPromises()
    expect(calls).toEqual(['owner', 'model', 'work'])
    expect(router.currentRoute.value.path).toBe(home + '/tasks/explore')
    decide(false)
    await pending
    expect(calls).toEqual(['owner', 'model', 'work'])
    expect(router.currentRoute.value.path).toBe(home + '/tasks/explore')
  })

  it('does not let retiring an old controller unregister a replacement', async () => {
    const router = await navigation()
    const retired = vi.fn(async () => true)
    const stopRetired = installProjectWorkPlanningNavigation(router, {
      confirmLeave: retired,
      afterNavigation: vi.fn(),
    })
    const current = vi.fn(async () => false)
    cleanup.push(
      installProjectWorkPlanningNavigation(router, {
        confirmLeave: current,
        afterNavigation: vi.fn(),
      }),
    )
    stopRetired()
    await router.push('/outside')
    expect(retired).not.toHaveBeenCalled()
    expect(current).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe(home + '/tasks/explore')
  })
})

const editorProps = {
  creating: true,
  disabled: false,
  readOnly: false,
  canSave: true,
  canRead: true,
  canAdopt: false,
  conflict: false,
  feedback: 'idle' as const,
  message: '',
  errors: {},
  title: '原始标题',
  description: '',
}

describe('Work planning public editor interactions', () => {
  it('preserves exact structure text and emits only an explicit save', async () => {
    const wrapper = mount(ProjectWorkStructureEditor, {
      props: { ...editorProps, kind: 'milestone' },
    })
    cleanup.push(() => wrapper.unmount())
    await wrapper.get('input[name="work-structure-title"]').setValue('  新标题 🧭  ')
    await wrapper.get('textarea').setValue('  第一行\n第二行\t ')
    expect(wrapper.emitted('update:title')).toEqual([['  新标题 🧭  ']])
    expect(wrapper.emitted('update:description')).toEqual([['  第一行\n第二行\t ']])
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toEqual([[]])
  })

  it('does not turn an Enter/form submission into a disabled write', async () => {
    const wrapper = mount(ProjectWorkStructureEditor, {
      props: { ...editorProps, kind: 'sprint', canSave: false },
    })
    cleanup.push(() => wrapper.unmount())
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ canSave: true, readOnly: true })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.find('button[type="submit"]').exists()).toBe(false)
    expect(wrapper.get('input').attributes('readonly')).toBeDefined()
  })

  it('does not invent Task defaults and preserves Plan as inert original text', async () => {
    const wrapper = mount(ProjectWorkTaskEditor, {
      props: { ...editorProps, type: '', priority: '', plan: '' },
    })
    cleanup.push(() => wrapper.unmount())
    expect(wrapper.text()).toContain('请选择类型')
    expect(wrapper.text()).toContain('请选择优先级')
    expect(wrapper.emitted('update:type')).toBeUndefined()
    expect(wrapper.emitted('update:priority')).toBeUndefined()
    const original = '  <script>doNotExecute()</script>\n**原文**\t '
    await wrapper.get('textarea[name="work-task-plan"]').setValue(original)
    expect(wrapper.emitted('update:plan')).toEqual([[original]])
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('keeps conflict reread separate from explicitly adopting current values', async () => {
    const wrapper = mount(ProjectWorkTaskEditor, {
      props: {
        ...editorProps,
        creating: false,
        type: 'task',
        priority: 'medium',
        plan: '草稿',
        conflict: true,
        canAdopt: true,
        message: '当前版本已变化。',
      },
    })
    cleanup.push(() => wrapper.unmount())
    expect(wrapper.get('[role="alert"]').text()).toBe('当前版本已变化。')
    await wrapper
      .findAll('button')
      .find(
        (button) =>
          button.element.querySelector('[aria-hidden="false"]')?.textContent?.trim() ===
          '读取当前值',
      )!
      .trigger('click')
    expect(wrapper.emitted('readCurrent')).toEqual([[]])
    expect(wrapper.emitted('adoptCurrent')).toBeUndefined()
    expect(wrapper.emitted('save')).toBeUndefined()
    await wrapper
      .findAll('button')
      .find(
        (button) =>
          button.element.querySelector('[aria-hidden="false"]')?.textContent?.trim() ===
          '按当前值重新编辑',
      )!
      .trigger('click')
    expect(wrapper.emitted('adoptCurrent')).toEqual([[]])
    expect(wrapper.get<HTMLTextAreaElement>('textarea[name="work-task-plan"]').element.value).toBe(
      '草稿',
    )
  })
})

async function productionPage(path = home + '/tasks/explore/milestones/' + id) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  const at = '2026-10-09T10:00:00.000000Z'
  const projectID = id.replace('000010', '000011'),
    userID = id.replace('000010', '000001')
  const base = '/api/v1/projects/' + projectID
  const project = {
    id: projectID,
    owner_user_id: userID,
    name: 'Owner.Dot-Name',
    normalized_name: 'owner.dot-name',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: at,
    updated_at: at,
    archived_at: null,
  }
  let milestone = {
    id,
    project_id: projectID,
    title: 'Initial milestone',
    description: 'Original description',
    manual_rank: '8'.repeat(32),
    version: '1',
    created_at: at,
    updated_at: at,
  }
  const json = (value: unknown) =>
    new Response(JSON.stringify(value), {
      status: 200,
      headers: {
        'Content-Type': 'application/json',
        'X-Request-ID': id.replace('000010', '000099'),
      },
    })
  let intercept: Fetch | undefined
  const normal: Fetch = async (url, init) => {
    if (url === '/api/v1/session')
      return json({
        user: {
          id: userID,
          email: 'owner@example.test',
          username: 'owner',
          display_name: 'Owner',
          role: 'user',
          theme: 'system',
          version: '1',
          initial_password_suggestion: false,
        },
        session: {
          id: id.replace('000010', '000002'),
          issued_at: at,
          absolute_expires_at: at,
          idle_expires_at: at,
        },
        csrf_token: 'S'.repeat(43),
      })
    if (url === '/api/v1/sessions/logout') return new Response(null, { status: 204 })
    if (url === '/api/v1/auth/bootstrap')
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (url.startsWith('/api/v1/projects/resolve?') || url === base) return json(project)
    if (url.startsWith('/api/v1/projects?')) return json({ items: [] })
    if (url.startsWith(base + '/milestones?')) {
      const { description: _description, ...summary } = milestone
      return json({ items: [summary] })
    }
    if (url === base + '/milestones/' + id && init.method === 'GET') return json(milestone)
    if (url === base + '/milestones/' + id && init.method === 'PATCH') {
      const input = JSON.parse(init.body as string) as {
        request: { title?: string; description?: string }
      }
      milestone = {
        ...milestone,
        ...input.request,
        version: String(BigInt(milestone.version) + 1n),
      }
      return json({
        command: 'work.milestone.update',
        changed: true,
        milestone,
        sprint: null,
        event_id: id.replace('000010', '000020'),
      })
    }
    throw new Error('Unexpected production-page fixture request')
  }
  const fetcher = vi.fn<Fetch>((url, init) =>
    intercept ? intercept(url, init) : normal(url, init),
  )
  const auth = createSessionController(
    createAccountAPI(fetcher),
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    undefined,
    createProjectOwnerAPI(fetcher),
    undefined,
    undefined,
    createWorkPlanningAPI(fetcher),
  )
  selectedSession.auth = auth
  const { router: source } = await import('../router')
  source.options.history.destroy()
  const router = createRouter({ history: createMemoryHistory(), routes: source.options.routes })
  installAuthentication(router, auth)
  await router.push(path)
  await router.isReady()
  const wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  cleanup.push(() => {
    wrapper.unmount()
    router.options.history.destroy()
    auth.leave()
  })
  await flushPromises()
  await flushPromises()
  return {
    wrapper,
    router,
    auth,
    fetcher,
    project,
    base,
    json,
    normal,
    intercept(value: Fetch) {
      intercept = value
    },
  }
}
function publicButton(label: string) {
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find((value) => {
    const copy = value.cloneNode(true) as HTMLElement
    copy.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
    return (value.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
  })
  if (!button) throw new Error('Missing public button: ' + label)
  return button
}
describe('Work production App and router composition', () => {
  it('explicitly refreshes the same Project ID and preserves the Work draft and cancelable Owner guard', async () => {
    const f = await productionPage()
    await f.wrapper.find('input[name="work-structure-title"]').setValue('Keep through rename')
    Object.assign(f.project, { name: 'Renamed', normalized_name: 'renamed', version: '2' })
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe(home + '/tasks/explore/milestones/' + id)
    const before = f.fetcher.mock.calls.length
    publicButton('刷新项目信息').click()
    await flushPromises()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/renamed/tasks/explore/milestones/' + id)
    expect(f.fetcher.mock.calls.slice(before).filter(([url]) => url === f.base)).toHaveLength(1)
    expect(f.fetcher.mock.calls.slice(before).some(([url]) => url.includes('/resolve?'))).toBe(
      false,
    )
    expect(
      f.wrapper.find<HTMLInputElement>('input[name="work-structure-title"]').element.value,
    ).toBe('Keep through rename')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    const leaving = f.router.push('/owner/renamed/settings/general')
    await flushPromises()
    expect(document.body.textContent).toContain('放弃项目修改？')
    publicButton('继续编辑').click()
    await leaving
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/owner/renamed/tasks/explore/milestones/' + id)
    expect(
      f.wrapper.find<HTMLInputElement>('input[name="work-structure-title"]').element.value,
    ).toBe('Keep through rename')
    expect(f.fetcher.mock.calls.some(([, init]) => init.method === 'PATCH')).toBe(false)
  })
  it('does not publish Work data or issue child reads after explicit Project refresh loses authority', async () => {
    const f = await productionPage()
    await f.wrapper.find('input[name="work-structure-title"]').setValue('Private Work input')
    f.intercept(async (url, init) =>
      url === f.base
        ? new Response(
            JSON.stringify({
              type: 'urn:agenteam:problem:not-found',
              title: 'Not found',
              detail: '',
              instance: f.base,
              status: 404,
              code: 'NOT_FOUND',
              commit_state: 'not_started',
              request_id: id.replace('000010', '000099'),
            }),
            {
              status: 404,
              headers: {
                'Content-Type': 'application/problem+json',
                'X-Request-ID': id.replace('000010', '000099'),
              },
            },
          )
        : f.normal(url, init),
    )
    const before = f.fetcher.mock.calls.length
    publicButton('刷新项目信息').click()
    await flushPromises()
    await flushPromises()
    expect(document.body.textContent).toContain('项目不可用')
    expect(f.wrapper.find('#work-planning-title').exists()).toBe(false)
    expect(document.body.textContent).not.toContain('Private Work input')
    expect(f.fetcher.mock.calls.slice(before).map(([url]) => url)).toEqual([f.base])
  })
  it('retires a pending explicit Project refresh on navigation and rejects its late publication', async () => {
    const f = await productionPage()
    let signal: AbortSignal | null | undefined
    let finish: (() => void) | undefined
    f.intercept(async (url, init) => {
      if (url === f.base) {
        signal = init.signal
        return new Promise<Response>((resolve) => {
          finish = () =>
            resolve(f.json({ ...f.project, name: 'Late', normalized_name: 'late', version: '2' }))
        })
      }
      return f.normal(url, init)
    })
    publicButton('刷新项目信息').click()
    await flushPromises()
    expect(finish).toBeDefined()
    expect(document.body.textContent).toContain('正在读取项目')
    await f.router.push('/projects')
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    const before = f.fetcher.mock.calls.length
    finish!()
    await flushPromises()
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/projects')
    expect(f.wrapper.find('#work-planning-title').exists()).toBe(false)
    expect(document.body.textContent).not.toContain('Late')
    expect(f.fetcher.mock.calls.slice(before).some(([url]) => url.startsWith(f.base + '/'))).toBe(
      false,
    )
  })
  it('renders the explicit planning entry and saves through the actual Session facade', async () => {
    const f = await productionPage()
    expect(f.wrapper.find('nav[aria-label="项目导航"] a[aria-current="page"]').text()).toBe(
      '任务规划',
    )
    expect(f.wrapper.find('#work-planning-title').text()).toBe('任务规划')
    await f.wrapper.find('input[name="work-structure-title"]').setValue('Updated title')
    await f.wrapper.find('form[aria-label="规划结构编辑"]').trigger('submit')
    await flushPromises()
    expect(f.wrapper.find('section[aria-label="原命令恢复"]').text()).toContain('原命令已确认')
    expect(f.wrapper.find('section[aria-label="当前读取内容"]').text()).toContain('Updated title')
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
  })
  it('asks the Work owner before logout and retains draft after canceling', async () => {
    const f = await productionPage()
    await f.wrapper.find('input[name="work-structure-title"]').setValue('Keep this draft')
    publicButton('退出登录').click()
    await flushPromises()
    expect(document.body.textContent).toContain('放弃任务规划修改？')
    expect(f.fetcher.mock.calls.some(([url]) => url.endsWith('/logout'))).toBe(false)
    publicButton('继续编辑').click()
    await flushPromises()
    expect(
      f.wrapper.find<HTMLInputElement>('input[name="work-structure-title"]').element.value,
    ).toBe('Keep this draft')
    expect(f.auth.state.phase).toBe('authenticated')
  })
  it('does not register the unimplemented default Kanban path or request Work data there', async () => {
    const f = await productionPage(home + '/tasks')
    expect(f.router.currentRoute.value.name).toBe('not-found')
    expect(
      f.fetcher.mock.calls.some(([url]) => /\/(milestones|sprints|tasks)(\/|\?)/.test(url)),
    ).toBe(false)
    expect(f.wrapper.find('#work-planning-title').exists()).toBe(false)
  })
})
