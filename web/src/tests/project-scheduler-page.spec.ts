import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../App.vue'
import ProjectWorkspaceView from '../views/projects/ProjectWorkspaceView.vue'
import ProjectSettingsView from '../views/projects/ProjectSettingsView.vue'
import ProjectSchedulerSettings from '../views/projects/ProjectSchedulerSettings.vue'
import * as sessionModule from '../composables/useSession'
import { installAuthentication, projectRoute, safeReturnTarget } from '../router/auth'
import { schedulerFixture, schedulerBase } from './project-scheduler.fixture'
let wrapper: VueWrapper | undefined
let fixture: Awaited<ReturnType<typeof schedulerFixture>> | undefined
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await fixture?.close()
  fixture = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
describe('Scheduler App and Settings route', () => {
  it('uses the actual App-owned controller to navigate, pause, save and reread through the unique Session', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: false,
        media: query,
        addEventListener() {},
        removeEventListener() {},
      })),
    )
    fixture = await schedulerFixture(false)
    vi.spyOn(sessionModule, 'useSession').mockReturnValue(fixture.auth)
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
        {
          path: '/:username/:project_name',
          component: ProjectWorkspaceView,
          meta: { protected: true, authentication: true, projectWorkspace: true },
          children: [
            {
              path: 'settings',
              component: ProjectSettingsView,
              children: [
                { path: 'general', component: { template: '<p>项目资料</p>' } },
                { path: 'scheduler', component: ProjectSchedulerSettings },
              ],
            },
          ],
        },
      ],
    })
    installAuthentication(router, fixture.auth)
    try {
      await router.push('/owner/demo/settings/scheduler')
      wrapper = mount(App, { global: { plugins: [router] } })
      await flushPromises()
      expect(wrapper.find('a[href="/owner/demo/settings/scheduler"]').exists()).toBe(true)
      expect(wrapper.get('#scheduler-heading').text()).toBe('调度配置')
      await wrapper.get('input[role="switch"]').setValue(false)
      await wrapper.get('form[aria-label="调度配置"]').trigger('submit')
      await flushPromises()
      expect(wrapper.text()).toContain('已暂停')
      expect(wrapper.text()).toContain('操作已确认，并已读取当前调度配置')
      expect(
        fixture.fetch.mock.calls.filter(([p, i]) => p === schedulerBase && i.method === 'PATCH'),
      ).toHaveLength(1)
      expect(
        fixture.fetch.mock.calls.filter(([p, i]) => p === schedulerBase && i.method === 'GET'),
      ).toHaveLength(2)
    } finally {
      wrapper?.unmount()
      wrapper = undefined
    }
  })
  it('admits only the canonical Scheduler settings suffix and safe return path', () => {
    expect(projectRoute('/owner/demo/settings/scheduler')?.suffix).toBe('/settings/scheduler')
    expect(safeReturnTarget('/owner/demo/settings/scheduler')).toBe(
      '/owner/demo/settings/scheduler',
    )
    for (const path of [
      '/owner/demo/settings/scheduler?x=1',
      '/owner/demo/settings/scheduler/extra',
      '/owner/demo/settings/%73cheduler',
    ])
      expect(projectRoute(path)).toBeNull()
  })
})
