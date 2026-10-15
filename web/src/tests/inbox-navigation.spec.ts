import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ProjectNav from '../components/layout/ProjectNav.vue'
import { projectRoute, safeReturnTarget } from '../router/auth'

describe('Inbox canonical Project navigation', () => {
  it('accepts only the project Inbox leaf and preserves existing Meeting and OAuth routes', () => {
    expect(projectRoute('/owner/demo/inbox')?.suffix).toBe('/inbox')
    expect(safeReturnTarget('/owner/demo/inbox')).toBe('/owner/demo/inbox')
    expect(safeReturnTarget('/mcp/oauth/callback')).toBe('/mcp/oauth/callback')
    expect(projectRoute('/owner/demo/meetings/019a0000-0000-7000-8000-000000000001')).not.toBeNull()
    for (const path of [
      '/owner/demo/inbox/other',
      '/owner/demo/inbox?from=other',
      '/owner/demo/%69nbox',
      '/owner/demo/inbox#item',
    ])
      expect(projectRoute(path)).toBeNull()
  })
  it('marks only the current project Inbox link and retains an explicit project return path', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
    })
    await router.push('/owner/demo/inbox')
    await router.isReady()
    const wrapper = mount(ProjectNav, {
      props: { name: 'Demo', home: '/owner/demo', settings: '/owner/demo/settings/general' },
      global: { plugins: [router] },
    })
    try {
      expect(wrapper.findAll('a[aria-current="page"]').map((link) => link.text())).toEqual([
        '收件箱',
      ])
      expect(wrapper.get('a.project-name').attributes('href')).toBe('/owner/demo')
      await router.push('/owner/other/inbox')
      await flushPromises()
      expect(wrapper.find('a[aria-current="page"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      router.options.history.destroy()
    }
  })
})
