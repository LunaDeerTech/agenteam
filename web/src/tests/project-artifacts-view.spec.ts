import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectArtifactsView from '../views/projects/ProjectArtifactsView.vue'
import ProjectNav from '../components/layout/ProjectNav.vue'
import { projectRoute, safeReturnTarget } from '../router/auth'
import {
  artifactBase,
  artifactContent,
  artifactDeferred,
  artifactJSON,
  artifactPage,
  artifactPath,
  artifactRecord,
  artifactSessionFixture,
  artifactText,
} from './project-artifacts.fixture'
const cleanups: (() => void)[] = []
function button(wrapper: VueWrapper, label: string) {
  const found = wrapper
    .findAll<HTMLButtonElement>('button.ui-button')
    .filter((item) => item.find('.button-label[aria-hidden="false"]').exists())
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
async function setup() {
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  const f = artifactSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation(artifactPath)
  await flushPromises()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/:username/:project_name/artifacts', component: ProjectArtifactsView },
      { path: '/elsewhere', component: { template: '<p>Elsewhere</p>' } },
    ],
  })
  await router.push(artifactPath)
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
describe('Artifact real controls and safe presentation', () => {
  it('renders text literally, focuses the selected heading and restores the original button, without exposing a write or download action', async () => {
    const f = await setup(),
      open = f.wrapper.get<HTMLButtonElement>('[aria-label="查看产物 report-100.txt"]')
    await open.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('#artifact-detail-heading').element)
    expect(f.wrapper.get('pre').text()).toBe(artifactText.trim())
    expect(f.wrapper.find('pre script').exists()).toBe(false)
    expect(f.wrapper.find('a[download], img, iframe').exists()).toBe(false)
    expect(button(f.wrapper, '下一段').element.disabled).toBe(true)
    await button(f.wrapper, '返回产物列表').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.artifact-detail').exists()).toBe(false)
    expect(document.activeElement).toBe(open.element)
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
  it('applies literal draft names only on submit, uses the actual type selector, and keeps navigation canonical', async () => {
    const f = await setup(),
      before = f.fetch.mock.calls.length
    f.intercept((path, init) =>
      path.startsWith(artifactBase + '?')
        ? Promise.resolve(artifactJSON(artifactPage([])))
        : f.normal(path, init),
    )
    await f.wrapper.get('input').setValue('Case_%')
    expect(f.fetch.mock.calls).toHaveLength(before)
    const selector = f.wrapper.get<HTMLButtonElement>('button[aria-haspopup="listbox"]')
    await selector.trigger('click')
    await flushPromises()
    const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find(
      (item) => item.textContent?.includes('用户上传'),
    )!
    expect(option).toBeDefined()
    option.click()
    await flushPromises()
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    const query = new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams
    expect(query.get('name_query')).toBe('Case_%')
    expect(query.get('kind')).toBe('user_upload')
    expect(query.has('cursor')).toBe(false)
    expect(f.wrapper.text()).toContain('暂无匹配的产物')
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    await button(f.wrapper, '清除筛选').trigger('click')
    await flushPromises()
    expect(
      new URL(f.fetch.mock.calls.at(-1)![0], 'https://owned.invalid').searchParams.has('kind'),
    ).toBe(false)
    expect(projectRoute('/Owner/Artifact-Demo/artifacts')?.path).toBe(artifactPath)
    expect(safeReturnTarget(artifactPath)).toBe(artifactPath)
    for (const path of [
      artifactPath + '/extra',
      artifactPath + '?next=bad',
      '/owner/artifact-demo/%61rtifacts',
    ])
      expect(projectRoute(path)).toBeNull()
    const nav = mount(ProjectNav, {
      props: {
        name: 'Artifact-Demo',
        home: '/owner/artifact-demo',
        settings: '/owner/artifact-demo/settings/general',
      },
      global: { plugins: [f.router] },
    })
    cleanups.unshift(() => nav.unmount())
    expect(nav.findAll('a').map((item) => item.text())).toEqual([
      'Artifact-Demo',
      '任务',
      '知识库',
      '产物',
      '项目设置',
    ])
    expect(nav.get('a[href$="/artifacts"]').attributes('aria-current')).toBe('page')
  })
  it('returns to the current page heading when closing a held read leaves the original card disabled', async () => {
    const f = await setup(),
      held = artifactDeferred<Response>()
    cleanups.unshift(() => held.resolve(artifactJSON(artifactContent())))
    f.intercept((path, init) => (path.includes('/content?') ? held.promise : f.normal(path, init)))
    const original = f.wrapper.get<HTMLButtonElement>('[aria-label="查看产物 report-100.txt"]')
    await original.trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('#artifact-detail-heading').element)
    button(f.wrapper, '返回产物列表').element.focus()
    await button(f.wrapper, '返回产物列表').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('.artifact-detail').exists()).toBe(false)
    expect(f.auth.state.busy).toBe(true)
    expect(original.element.disabled).toBe(true)
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    held.resolve(artifactJSON(artifactContent()))
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(original.element.disabled).toBe(false)
    expect(f.wrapper.find('.artifact-detail').exists()).toBe(false)
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
  })
  it('keeps non-text references as metadata and prevents a closed or left late request from reopening details or taking focus', async () => {
    const f = await setup()
    f.intercept((path, init) =>
      path.includes('/content?')
        ? Promise.resolve(
            artifactJSON({
              artifact: artifactRecord(),
              projection: 'file_ref',
              offset: '0',
              next_offset: '0',
              truncated: false,
              invalid_utf8: true,
            }),
          )
        : f.normal(path, init),
    )
    await button(f.wrapper, '查看产物').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('此类型暂不支持在线预览。')
    expect(f.wrapper.text()).toContain('不是有效的 UTF-8')
    expect(f.wrapper.find('pre, img, a[download]').exists()).toBe(false)
    const held = artifactDeferred<Response>()
    f.intercept((path, init) => (path.includes('/content?') ? held.promise : f.normal(path, init)))
    await button(f.wrapper, '重新读取').trigger('click')
    await flushPromises()
    await button(f.wrapper, '返回产物列表').trigger('click')
    await f.router.push('/elsewhere')
    held.resolve(artifactJSON(artifactContent()))
    await flushPromises()
    expect(f.wrapper.text()).toBe('Elsewhere')
    expect(document.querySelector('#artifact-detail-heading')).toBeNull()
  })
})
