import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import ProjectNav from '../components/layout/ProjectNav.vue'
import SystemNav from '../components/layout/SystemNav.vue'

vi.mock('../composables/useSession', () => ({
  useSession: () => ({
    state: { phase: 'authenticated', user: { role: 'admin' } },
    system: { denied: false },
  }),
}))
let wrapper: VueWrapper | undefined
let router: Router | undefined
const page = { template: '<div />' }
afterEach(() => {
  wrapper?.unmount()
  router?.options.history.destroy()
})
async function navigation(path: string, system = false) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: page },
      {
        path: '/projects',
        component: page,
        meta: { navigation: { label: '项目', order: 10 }, projectWorkspace: true },
      },
      {
        path: '/system',
        component: page,
        meta: { navigation: { label: '系统设置', order: 20 }, systemAdmin: true },
        children: [{ path: ':section', component: page }],
      },
      {
        path: '/:username/:project_name/:tail(.*)*',
        component: page,
        meta: { projectWorkspace: true },
      },
    ],
  })
  await router.push(path)
  await router.isReady()
  wrapper = system
    ? mount(SystemNav, { global: { plugins: [router] } })
    : mount(ProjectNav, {
        props: { name: 'Demo', home: '/owner/demo', settings: '/owner/demo/settings/general' },
        global: { plugins: [router] },
      })
  return wrapper
}

describe('project navigation keeps the current module visible', () => {
  it.each([
    'general',
    'agents',
    'variables',
    'secrets',
    'skills',
    'scheduler',
    'audit',
    'model-providers',
    'available-models',
    'model-usage',
  ])('marks Project settings current on its %s leaf', async (leaf) => {
    const view = await navigation(`/owner/demo/settings/${leaf}`)
    expect(view.findAll('a[aria-current="page"]').map((entry) => entry.text())).toEqual([
      '项目设置',
    ])
  })
  it('moves selection between sections and never borrows another Project identity', async () => {
    const view = await navigation('/owner/demo/settings/skills')
    await router!.push('/owner/demo/knowledge')
    await flushPromises()
    expect(view.findAll('a[aria-current="page"]').map((entry) => entry.text())).toEqual(['知识库'])
    await router!.push('/owner/other/settings/skills')
    await flushPromises()
    expect(view.find('a[aria-current="page"]').exists()).toBe(false)
  })
  it('keeps the system Project entry selected throughout a Project workspace', async () => {
    const view = await navigation('/owner/demo/settings/skills', true)
    expect(view.find('nav a[aria-current="page"]').text()).toBe('项目')
    await router!.push('/system/users')
    await flushPromises()
    expect(view.find('nav a[aria-current="page"]').text()).toBe('系统设置')
    await router!.push('/')
    await flushPromises()
    expect(view.find('nav a[aria-current="page"]').exists()).toBe(false)
  })
})
