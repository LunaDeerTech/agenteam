import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectSecretsView from '../views/projects/ProjectSecretsView.vue'
import ProjectSettingsView from '../views/projects/ProjectSettingsView.vue'
import { projectRoute, safeReturnTarget } from '../router/auth'
import {
  secretBase,
  secretDeferred,
  secretID,
  secretJSON,
  secretMetadata,
  secretPath,
  secretSessionFixture,
} from './project-secrets.fixture'

const cleanups: (() => void)[] = []
function findButton(wrapper: VueWrapper, label: string) {
  const button = wrapper
    .findAll<HTMLButtonElement>('button.ui-button')
    .find((item) => item.get('.button-label[aria-hidden="false"]').text() === label)
  expect(button, `visible button ${label}`).toBeDefined()
  return button!
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
  const f = secretSessionFixture()
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation(secretPath)
  await flushPromises()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/:username/:project_name/settings',
        component: ProjectSettingsView,
        children: [
          { path: 'secrets', component: ProjectSecretsView },
          ...[
            'general',
            'agents',
            'variables',
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
  await router.push(secretPath)
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
describe('Project Secrets read page', () => {
  it('exposes safe names/descriptions, explicitly reads detail, restores focus and exposes the authorized write entry points without exposing values', async () => {
    const f = await setup()
    expect(f.wrapper.text()).toContain('SERVICE_TOKEN')
    const button = findButton(f.wrapper, 'SERVICE_TOKEN')
    expect(button.exists()).toBe(true)
    button.element.focus()
    await button.trigger('click')
    await flushPromises()
    expect(f.fetch.mock.calls.some(([path]) => path === secretBase + '/' + secretID(10))).toBe(true)
    expect(f.wrapper.text()).toContain('9007199254740993')
    expect(f.wrapper.get('[aria-label="项目 Secrets"] h2').element).toBe(document.activeElement)
    expect(button.attributes('aria-pressed')).toBe('true')
    const actions = f.wrapper
      .findAll('button.ui-button')
      .map((button) => button.get('.button-label[aria-hidden="false"]').text())
    expect(actions).toContain('创建 Secret')
    expect(actions).toContain('编辑名称与描述')
    expect(actions).toContain('替换 Secret 值')
    expect(actions).toContain('删除 Secret')
    for (const text of ['授权']) expect(actions.some((label) => label.includes(text))).toBe(false)
    for (const text of ['授权 Agent', '有效', '无授权'])
      expect(f.wrapper.text()).not.toContain(text)
    expect(f.wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(f.wrapper.find('textarea').exists()).toBe(false)
    const refresh = findButton(f.wrapper, '重新读取详情')
    await refresh.trigger('click')
    await flushPromises()
    expect(
      f.fetch.mock.calls.filter(([path]) => path === secretBase + '/' + secretID(10)),
    ).toHaveLength(2)
    await findButton(f.wrapper, '返回目录').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('[aria-label="项目 Secrets"] h2').exists()).toBe(false)
    expect(document.activeElement).toBe(button.element)
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
    expect(
      f.wrapper
        .findAll('a')
        .some((link) => link.attributes('href') === secretPath && link.text().includes('安全目录')),
    ).toBe(true)
  })
  it('does not steal focus after a delayed selection when another element now owns focus', async () => {
    const f = await setup(),
      pending = secretDeferred<Response>()
    cleanups.unshift(() => pending.resolve(secretJSON(secretMetadata())))
    f.intercept((path, init) =>
      path === secretBase + '/' + secretID(10) ? pending.promise : f.normal(path, init),
    )
    const select = findButton(f.wrapper, 'SERVICE_TOKEN')
    select.element.focus()
    await select.trigger('click')
    await flushPromises()
    expect(select.attributes('disabled')).toBeDefined()
    const heading = f.wrapper.get('[aria-label="项目 Secrets"] h2')
    expect(heading.text()).toBe('Secret 详情')
    expect(document.activeElement).toBe(heading.element)
    expect(f.wrapper.text()).not.toContain('9007199254740993')
    const other = document.createElement('button')
    document.body.append(other)
    cleanups.unshift(() => other.remove())
    other.focus()
    pending.resolve(secretJSON(secretMetadata()))
    await flushPromises()
    expect(document.activeElement).toBe(other)
    expect(f.wrapper.get('[aria-label="项目 Secrets"] h2').element).toBe(heading.element)
    expect(heading.text()).toBe('SERVICE_TOKEN')
    expect(f.wrapper.text()).toContain('9007199254740993')
  })
  it('cancels and clears its page instance on navigation', async () => {
    const f = await setup(),
      pending = secretDeferred<Response>()
    cleanups.unshift(() => pending.resolve(secretJSON(secretMetadata())))
    f.intercept((path, init) =>
      path === secretBase + '/' + secretID(10) ? pending.promise : f.normal(path, init),
    )
    await findButton(f.wrapper, 'SERVICE_TOKEN').trigger('click')
    await flushPromises()
    const other = document.createElement('button')
    document.body.append(other)
    cleanups.unshift(() => other.remove())
    other.focus()
    await f.router.push('/elsewhere')
    pending.resolve(secretJSON(secretMetadata()))
    await flushPromises()
    expect(f.wrapper.text()).not.toContain('9007199254740993')
    expect(f.wrapper.find('[aria-label="项目 Secrets"] h2').exists()).toBe(false)
    expect(document.activeElement).toBe(other)
    expect(f.auth.state.busy).toBe(false)
  })
  it('allowlists only the exact project Secrets route', () => {
    expect(projectRoute(secretPath)?.suffix).toBe('/settings/secrets')
    expect(safeReturnTarget(secretPath)).toBe(secretPath)
    for (const invalid of [
      secretPath + '/',
      secretPath + '/' + secretID(10),
      secretPath + '?value=x',
      secretPath + '#value',
      '/api/demo/settings/secrets',
      secretPath.replace('secrets-demo', '%73ecrets-demo'),
    ]) {
      expect(projectRoute(invalid)).toBeNull()
      expect(safeReturnTarget(invalid)).toBe('/')
    }
  })
})
