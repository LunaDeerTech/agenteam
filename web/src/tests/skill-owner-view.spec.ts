import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive } from 'vue'
import { createMemoryHistory, createRouter, RouterView, type Router } from 'vue-router'
import type { SkillOwnerController } from '../composables/useSkillOwner'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
const supplied = vi.hoisted(() => ({
  page: null as SkillOwnerController | null,
  workspace: null as ProjectWorkspace | null,
}))
vi.mock('../composables/useSkillOwner', () => ({ useSkillOwner: () => supplied.page! }))
vi.mock('../composables/useProjectWorkspace', () => ({
  useProjectWorkspace: () => supplied.workspace!,
}))
import ProjectSkillsView from '../views/projects/ProjectSkillsView.vue'
import { projectRoute, safeReturnTarget } from '../router/auth'

const target = '01970000-0000-7000-8000-000000000020'
const skill = {
  id: target,
  project_id: '01970000-0000-7000-8000-000000000010',
  name: 'Add Skills',
  normalized_name: 'add-skills',
  description: '<img src=x onerror=alert(1)>\n仅展示文本',
  protected: true,
  current_revision: '9007199254740993',
  version: '2',
}
const path = '/owner/demo/settings/skills'
let wrapper: VueWrapper | undefined, router: Router | undefined
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await flushPromises()
  router?.options.history.destroy()
  router = undefined
  document.body.innerHTML = ''
})
async function fixture() {
  // This view-only port is controlled. The separate state tests use actual
  // Session, Workspace, transport and API; no DOM result claims authorization.
  const state = shallowReactive({
    phase: 'ready',
    items: [skill],
    detail: null as typeof skill | null,
    message: '',
  })
  const active = shallowReactive({ visible: true, blocked: false, reading: false })
  const refresh = vi.fn(),
    cancel = vi.fn()
  supplied.page = {
    state,
    visible: computed(() => active.visible),
    blocked: computed(() => active.blocked),
    reading: computed(() => active.reading),
    busy: computed(() => active.blocked),
    refresh,
    cancel,
    dispose: vi.fn(),
  } as unknown as SkillOwnerController
  supplied.workspace = {
    paths: computed(() => ({ home: '/owner/demo', settings: '/owner/demo/settings/general' })),
  } as unknown as ProjectWorkspace
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/:username/:project_name/settings/skills/:skill_id?', component: ProjectSkillsView },
    ],
  })
  await router.push(path)
  await router.isReady()
  wrapper = mount(RouterView, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { state, active, refresh, cancel }
}

describe('Skills view visible interactions', () => {
  it('opens and returns from a detail without rendering description markup', async () => {
    const f = await fixture()
    expect(wrapper!.text()).toContain('项目技能库')
    expect(wrapper!.text()).toContain('受保护')
    expect(wrapper!.find('img').exists()).toBe(false)
    await wrapper!.get(`a[href="${path}/${target}"]`).trigger('click')
    await flushPromises()
    f.state.detail = skill
    f.state.phase = 'loading'
    await flushPromises()
    f.state.phase = 'ready'
    await flushPromises()
    expect(wrapper!.text()).toContain('9007199254740993')
    expect(wrapper!.text()).toContain('记录版本')
    expect(document.activeElement?.textContent).toBe('技能详情')
    await wrapper!.get(`a[href="${path}"]`).trigger('click')
    await flushPromises()
    expect(wrapper!.get('h1').text()).toBe('项目技能库')
  })
  it('offers explicit read/stop actions and hides values when access is unavailable', async () => {
    const f = await fixture()
    const reread = wrapper!
      .findAll('button')
      .find((button) => button.find('[aria-hidden="false"]').text() === '重新读取')!
    await reread.trigger('click')
    expect(f.refresh).toHaveBeenCalledOnce()
    f.active.blocked = true
    f.active.reading = true
    await flushPromises()
    expect(reread.attributes('disabled')).toBeDefined()
    await wrapper!
      .findAll('button')
      .find((button) => button.find('[aria-hidden="false"]').text() === '停止读取')!
      .trigger('click')
    expect(f.cancel).toHaveBeenCalledOnce()
    f.state.phase = 'unavailable'
    f.state.items = []
    f.state.message = '当前技能不存在或不可访问。'
    await flushPromises()
    expect(wrapper!.text()).toContain('不可访问')
    expect(wrapper!.text()).not.toContain('Add Skills')
    f.active.visible = false
    await flushPromises()
    expect(wrapper!.text()).toContain('正在确认技能库访问身份')
  })
})

describe('Skills canonical project route', () => {
  it('accepts only the directory and canonical UUID7 detail', () => {
    for (const value of [path, `${path}/${target}`]) {
      expect(projectRoute(value)?.path).toBe(value)
      expect(safeReturnTarget(value)).toBe(value)
    }
    for (const value of [
      `${path}?`,
      `${path}#x`,
      `${path}/`,
      `${path}/unknown`,
      `${path}/%30${target.slice(1)}`,
      `${path}/../general`,
      `${path}\\x`,
      `${path}/${target.replace('-7000-', '-6000-')}`,
    ]) {
      expect(projectRoute(value)).toBeNull()
      expect(safeReturnTarget(value)).toBe('/')
    }
  })
})
