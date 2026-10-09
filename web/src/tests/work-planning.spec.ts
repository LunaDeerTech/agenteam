import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProjectWorkStructureEditor from '../views/projects/ProjectWorkStructureEditor.vue'
import ProjectWorkTaskEditor from '../views/projects/ProjectWorkTaskEditor.vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createSessionController } from '../composables/useSession'
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
