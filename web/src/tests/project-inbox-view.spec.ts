import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectInboxView from '../views/projects/ProjectInboxView.vue'
import {
  inboxBase,
  inboxDecision,
  inboxDeferred,
  inboxFixture,
  inboxItem,
  inboxJSON,
  inboxPage,
  inboxPath,
  inboxProblem,
} from './project-inbox.fixture'

const cleanups: (() => void)[] = []
afterEach(async () => {
  cleanups.splice(0).forEach((close) => close())
  await flushPromises()
  vi.restoreAllMocks()
})
async function setup() {
  const f = inboxFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation(inboxPath)
  await flushPromises()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/:username/:project_name/inbox', component: ProjectInboxView },
      {
        path: '/:username/:project_name/meetings/:meeting_id',
        component: { template: '<h1>Meeting source</h1>' },
      },
      { path: '/:username/:project_name', component: { template: '<h1>Project</h1>' } },
    ],
  })
  await router.push(inboxPath)
  await router.isReady()
  const wrapper = mount(RouterView, {
    attachTo: document.body,
    global: { plugins: [router], provide: { [projectWorkspaceKey as symbol]: workspace } },
  })
  cleanups.push(() => {
    wrapper.unmount()
    workspace.dispose()
    f.auth.leave()
    router.options.history.destroy()
  })
  await flushPromises()
  return { ...f, router, workspace, wrapper }
}
describe('Inbox visible proposal navigation', () => {
  it('renders the original reason literally and navigates to its Meeting without making a review command', async () => {
    const f = await setup()
    expect(f.wrapper.get('h1').text()).toBe('收件箱')
    expect(f.wrapper.find('script').exists()).toBe(false)
    expect(f.wrapper.text()).toContain(inboxItem().summary)
    const link = f.wrapper.get('a.inbox-open')
    expect(link.attributes('href')).toBe('/owner/artifact-demo/meetings/' + inboxItem().source_id)
    expect(f.wrapper.text()).not.toContain('标记已处理')
    await link.trigger('click')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe(
      '/owner/artifact-demo/meetings/' + inboxItem().source_id,
    )
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
  it('shows loading, handled-empty and permission failure distinctly and restores focus after changing the filter', async () => {
    const f = await setup(),
      held = inboxDeferred<Response>()
    f.intercept((path, init) => (path.startsWith(inboxBase) ? held.promise : f.normal(path, init)))
    await f.wrapper.get('input[value="resolved"]').setValue()
    expect(f.wrapper.text()).toContain('正在读取收件箱')
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    held.resolve(inboxJSON(inboxPage([])))
    await flushPromises()
    expect(f.wrapper.text()).toContain('暂无已处理事项')
    f.intercept((path, init) =>
      path.startsWith(inboxBase) ? Promise.resolve(inboxProblem(403)) : f.normal(path, init),
    )
    const refresh = f.wrapper
      .findAll('button')
      .find((button) => button.text().includes('刷新收件箱'))!
    await refresh.trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('收件箱不可用')
    expect(f.wrapper.text()).not.toContain('暂无已处理')
    expect(f.wrapper.find('a.inbox-open').exists()).toBe(false)
  })
  it('offers answering a Decision at its Meeting and retains handled navigation without a local answer action', async () => {
    const f = await setup()
    f.intercept((path, init) =>
      path.startsWith(inboxBase)
        ? Promise.resolve(
            inboxJSON(
              inboxPage([
                inboxDecision(200, path.includes('status=resolved') ? 'resolved' : 'open'),
              ]),
            ),
          )
        : f.normal(path, init),
    )
    const refresh = f.wrapper
      .findAll('button')
      .find((button) =>
        button.findAll('[aria-hidden="false"]').some((label) => label.text() === '刷新收件箱'),
      )
    expect(refresh?.exists()).toBe(true)
    await refresh!.trigger('click')
    await flushPromises()
    expect(f.wrapper.get('a.inbox-open').text()).toBe('回答决议')
    expect(f.wrapper.get('a.inbox-open').attributes('href')).toBe(
      '/owner/artifact-demo/meetings/' + inboxDecision().source_context.meeting_id,
    )
    await f.wrapper.get('input[value="resolved"]').setValue()
    await flushPromises()
    expect(f.wrapper.get('a.inbox-open').text()).toBe('查看决议')
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
})
