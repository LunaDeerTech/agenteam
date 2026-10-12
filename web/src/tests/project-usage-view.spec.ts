import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectUsageView from '../views/projects/ProjectUsageView.vue'
import ProjectSettingsView from '../views/projects/ProjectSettingsView.vue'
import ProjectNav from '../components/layout/ProjectNav.vue'
import { dailyAsOf, dailyPage } from './project-usage-summary.fixture'
import { projectRoute, safeReturnTarget } from '../router/auth'
import {
  usageBase,
  usageDeferred,
  usageID,
  usageJSON,
  usageNow,
  usagePage,
  usagePath,
  usageProblem,
  usageRecord,
  usageSessionFixture,
} from './project-usage.fixture'

const cleanups: (() => void)[] = []
function button(wrapper: VueWrapper, label: string) {
  const found = wrapper
    .findAll<HTMLButtonElement>('button.ui-button')
    .find((item) => item.get('.button-label[aria-hidden="false"]').text() === label)
  expect(found, `visible button ${label}`).toBeDefined()
  return found!
}
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function setup() {
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.spyOn(Date, 'now').mockReturnValue(usageNow)
  const f = usageSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation(usagePath)
  await flushPromises()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/:username/:project_name/settings',
        component: ProjectSettingsView,
        children: [
          { path: 'model-usage', component: ProjectUsageView },
          ...[
            'general',
            'agents',
            'variables',
            'secrets',
            'skills',
            'scheduler',
            'audit',
            'model-providers',
            'available-models',
          ].map((path) => ({ path, component: { template: '<p>Other settings</p>' } })),
        ],
      },
      { path: '/elsewhere', component: { template: '<p>Elsewhere</p>' } },
    ],
  })
  await router.push(usagePath)
  const wrapper = mount(RouterView, {
    attachTo: document.body,
    global: { plugins: [router], provide: { [projectWorkspaceKey as symbol]: workspace } },
  })
  cleanups.push(() => {
    wrapper.unmount()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  return { ...f, workspace, router, wrapper }
}
describe('Project usage read page', () => {
  it('shows daily server totals and each denominator without summing the current list, with keyboard focus and an empty state', async () => {
    const f = await setup()
    f.intercept((path, init) =>
      path.includes('/model-usage/summary?')
        ? Promise.resolve(usageJSON(dailyPage()))
        : f.normal(path, init),
    )
    await button(f.wrapper, '按日汇总').trigger('click')
    await flushPromises()
    expect(button(f.wrapper, '按日汇总').attributes('aria-pressed')).toBe('true')
    expect(document.activeElement).toBe(f.wrapper.get('.usage-page > h2').element)
    const summary = f.wrapper.get('[aria-label="按日用量汇总"]')
    expect(summary.text()).toContain(dailyAsOf)
    expect(summary.text()).toContain('已确认调用51')
    expect(summary.text()).toContain('派发情况未知2')
    const rows = summary.findAll('tbody tr')
    expect(rows).toHaveLength(6)
    expect(rows[0]!.findAll('td').map((cell) => cell.text())).toEqual([
      '9007199254740993',
      '50',
      '1',
    ])
    expect(rows[1]!.findAll('td').map((cell) => cell.text())).toEqual(['0', '49', '2'])
    expect(rows[2]!.findAll('td').map((cell) => cell.text())).toEqual([
      '9223372036854775807',
      '1',
      '50',
    ])
    expect(rows[3]!.findAll('td').map((cell) => cell.text())).toEqual(['未知', '0', '51'])
    expect(f.wrapper.find('.usage-list').exists()).toBe(false)
    expect(f.wrapper.find('[aria-label="调用记录分页"]').exists()).toBe(false)
    f.intercept((path, init) =>
      path.includes('/model-usage/summary?')
        ? Promise.resolve(usageJSON(dailyPage([])))
        : f.normal(path, init),
    )
    await button(f.wrapper, '刷新按日汇总').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('本次时间范围内没有用量汇总。')
    expect(f.wrapper.findAll('.usage-day')).toHaveLength(0)
    await button(f.wrapper, '调用明细').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.usage-list').exists()).toBe(true)
    expect(f.wrapper.find('.usage-daily').exists()).toBe(false)
  })
  it('shows exact historical values and local details, preserves null versus zero, and restores selection focus', async () => {
    const f = await setup(),
      select = f.wrapper.get<HTMLButtonElement>('.usage-select')
    expect(f.wrapper.text()).toContain('9007199254740993')
    expect(f.wrapper.text()).toContain('历史 Provider')
    const facts = f.wrapper
      .get('.usage-tokens')
      .findAll('dd')
      .map((v) => v.text())
    expect(facts).toEqual(['9007199254740993', '0', '未知'])
    const count = f.fetch.mock.calls.length
    select.element.focus()
    await select.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('.usage-detail h2').element)
    expect(f.wrapper.text()).toContain('9223372036854775807')
    expect(f.wrapper.text()).toContain('当前不可引用')
    expect(f.wrapper.text()).toContain('safe-provider-request:1')
    expect(f.fetch.mock.calls).toHaveLength(count)
    await button(f.wrapper, '返回调用列表').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.usage-detail').exists()).toBe(false)
    expect(document.activeElement).toBe(select.element)
    expect(f.wrapper.findAll('input,textarea').length).toBe(0)
    expect(
      f.fetch.mock.calls.every(
        ([path, init]) => init.method === 'GET' && !path.includes('/summary'),
      ),
    ).toBe(true)
    expect(
      f.wrapper
        .findAll('a')
        .some((a) => a.attributes('href') === usagePath && a.text() === '调用记录'),
    ).toBe(true)
  })
  it('paginates with visible controls and heading focus, then refreshes to an empty current read', async () => {
    const f = await setup(),
      first = Array.from({ length: 50 }, (_, i) => usageRecord(200 - i))
    let empty = false
    f.intercept((path, init) =>
      !path.startsWith(usageBase + '?')
        ? f.normal(path, init)
        : Promise.resolve(
            usageJSON(
              empty
                ? usagePage([])
                : path.includes('cursor=')
                  ? usagePage([usageRecord(150)])
                  : usagePage(first, 'next'),
            ),
          ),
    )
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    expect(button(f.wrapper, '上一页').element.disabled).toBe(true)
    await button(f.wrapper, '下一页').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('[aria-label="调用记录分页"]').text()).toContain('第 2 页')
    expect(document.activeElement).toBe(f.wrapper.get('.usage-page > h2').element)
    expect(button(f.wrapper, '下一页').element.disabled).toBe(true)
    await button(f.wrapper, '上一页').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('[aria-label="调用记录分页"]').text()).toContain('第 1 页')
    empty = true
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('本次时间范围内没有调用记录')
    expect(f.wrapper.findAll('.usage-select')).toHaveLength(0)
  })
  it('shows loading/cancel and safe unavailable recovery without displaying the previous detail', async () => {
    const f = await setup(),
      held = usageDeferred<Response>()
    cleanups.unshift(() => held.resolve(usageJSON(usagePage())))
    await f.wrapper.get('.usage-select').trigger('click')
    f.intercept((path, init) =>
      path.startsWith(usageBase + '?') ? held.promise : f.normal(path, init),
    )
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.usage-detail').exists()).toBe(false)
    expect(f.wrapper.text()).toContain('正在读取调用记录')
    await button(f.wrapper, '停止读取').trigger('click')
    await flushPromises()
    held.resolve(usageJSON(usagePage()))
    await flushPromises()
    expect(f.wrapper.text()).toContain('读取已停止')
    f.intercept((path, init) =>
      path.startsWith(usageBase + '?') ? Promise.resolve(usageProblem(403)) : f.normal(path, init),
    )
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('不存在或不可访问')
    expect(f.wrapper.text()).not.toContain('历史模型')
    f.intercept()
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('历史模型')
  })
  it('clears on route departure and registers only the approved settings entry', async () => {
    const f = await setup()
    expect(projectRoute(usagePath)?.suffix).toBe('/settings/model-usage')
    expect(safeReturnTarget(usagePath)).toBe(usagePath)
    expect(projectRoute(usagePath + '/summary')).toBeNull()
    const nav = mount(ProjectNav, {
      props: {
        name: 'Usage-Demo',
        home: '/owner/usage-demo',
        settings: '/owner/usage-demo/settings/general',
      },
      global: { plugins: [f.router] },
    })
    cleanups.unshift(() => nav.unmount())
    expect(
      nav
        .findAll('a')
        .find((a) => a.text() === '项目设置')
        ?.attributes('aria-current'),
    ).toBe('page')
    await f.wrapper.get('.usage-select').trigger('click')
    await f.router.push('/elsewhere')
    await flushPromises()
    expect(f.wrapper.text()).toBe('Elsewhere')
    expect(f.wrapper.text()).not.toContain(usageID(100))
  })
})
