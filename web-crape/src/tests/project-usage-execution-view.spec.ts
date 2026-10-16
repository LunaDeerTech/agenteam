import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import type { ProjectUsageRecord } from '../api/project-usage'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectUsageView from '../views/projects/ProjectUsageView.vue'
import { dailyPage } from './project-usage-summary.fixture'
import {
  usageBase,
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
const button = (wrapper: VueWrapper, label: string) => {
  const found = wrapper
    .findAll<HTMLButtonElement>('button.ui-button')
    .find((item) => item.get('.button-label[aria-hidden="false"]').text() === label)
  expect(found, label).toBeDefined()
  return found!
}
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function setup(record: ProjectUsageRecord = usageRecord()) {
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
  f.intercept((path, init) =>
    path.startsWith(usageBase)
      ? Promise.resolve(usageJSON(path.includes('/summary?') ? dailyPage() : usagePage([record])))
      : f.normal(path, init),
  )
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/:username/:project_name/settings/model-usage', component: ProjectUsageView },
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
  return { ...f, wrapper }
}

describe('Execution filter visible interactions', () => {
  it('applies from the selected detail, carries the filter into daily totals, and clears with heading focus', async () => {
    const f = await setup()
    await f.wrapper.get('.usage-select').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('.usage-detail h2').element)
    const action = button(f.wrapper, '查看同一次执行的调用')
    action.element.focus()
    await action.trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.usage-detail').exists()).toBe(false)
    expect(f.wrapper.get('[aria-label="执行筛选"]').text()).toContain('当前执行：' + usageID(21))
    expect(document.activeElement).toBe(f.wrapper.get('.usage-page > h2').element)
    const scoped = new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams
    expect(scoped.get('execution_id')).toBe(usageID(21))
    expect(scoped.has('cursor')).toBe(false)
    await f.wrapper.get('.usage-select').trigger('click')
    expect(button(f.wrapper, '查看同一次执行的调用').element.disabled).toBe(true)
    await button(f.wrapper, '返回调用列表').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('.usage-select').element)
    await button(f.wrapper, '按日汇总').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('[aria-label="按日用量汇总"]').text()).toContain('已确认调用51')
    expect(
      new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams.get(
        'execution_id',
      ),
    ).toBe(usageID(21))
    await button(f.wrapper, '清除执行筛选').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('[aria-label="执行筛选"]').exists()).toBe(false)
    expect(button(f.wrapper, '按日汇总').attributes('aria-pressed')).toBe('true')
    expect(document.activeElement).toBe(f.wrapper.get('.usage-page > h2').element)
    const cleared = new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams
    expect(cleared.has('execution_id')).toBe(false)
    expect(cleared.get('from')).toBe(scoped.get('from'))
    expect(cleared.get('to')).toBe(scoped.get('to'))
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })

  it('offers no execution action for an unassociated call and removes filter detail on rejected current access', async () => {
    const f = await setup({
      ...usageRecord(),
      consumer_kind: 'tool',
      purpose: 'approval_auto',
      agent_id: null,
      execution_id: null,
    })
    await f.wrapper.get('.usage-select').trigger('click')
    expect(
      f.wrapper
        .findAll('.usage-facts > div')
        .find((item) => item.get('dt').text() === 'Execution ID')!
        .get('dd')
        .text(),
    ).toBe('无关联')
    expect(
      f.wrapper.findAll('button').some((item) => item.text().includes('查看同一次执行的调用')),
    ).toBe(false)
    f.intercept()
    await button(f.wrapper, '刷新调用记录').trigger('click')
    await flushPromises()
    await f.wrapper.get('.usage-select').trigger('click')
    f.intercept((path, init) =>
      path.startsWith(usageBase) ? Promise.resolve(usageProblem(403)) : f.normal(path, init),
    )
    await button(f.wrapper, '查看同一次执行的调用').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('当前调用记录不存在或不可访问')
    expect(f.wrapper.find('[aria-label="执行筛选"]').exists()).toBe(false)
    expect(f.wrapper.find('.usage-detail').exists()).toBe(false)
    expect(f.wrapper.find('.usage-list').exists()).toBe(false)
    expect(f.wrapper.text()).not.toContain(usageID(21))
  })
})
