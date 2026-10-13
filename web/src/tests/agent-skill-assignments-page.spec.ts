import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProjectAgentsView from '../views/projects/ProjectAgentsView.vue'
import { projectAgentsKey } from '../composables/useProjectAgents'
import { base, pageFixture, receipt } from './agent-skill-assignments.fixture'
const clean: (() => void)[] = [],
  mounted: VueWrapper[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  mounted.splice(0).forEach((w) => w.unmount())
  clean.splice(0).forEach((dispose) => dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function setup() {
  const f = await pageFixture()
  clean.push(f.dispose)
  const wrapper = mount(ProjectAgentsView, {
    attachTo: document.body,
    global: { provide: { [projectAgentsKey as symbol]: f.owner } },
  })
  mounted.push(wrapper)
  const button = (label: string) => {
    const b = wrapper.findAll('button').find((b) => b.text() === label)
    if (!b) throw new Error(`Missing button ${label}`)
    return b
  }
  await button('管理 Skills').trigger('click')
  await flushPromises()
  return { ...f, wrapper, button }
}
describe('Agent Skills settings user flow', () => {
  it('confirms assign, disable, enable and exact-grant removal with fresh reads and focus restoration', async () => {
    const f = await setup()
    expect(f.wrapper.text()).toContain('此 Agent 尚未分配技能')
    expect(f.button('编辑配置').attributes('disabled')).toBeDefined()
    await f.wrapper.get('button[aria-label="选择项目技能"]').trigger('click')
    await flushPromises()
    const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find((e) =>
      e.textContent?.includes('Skill-30'),
    )!
    await new DOMWrapper(option).trigger('click')
    await f.button('分配技能').trigger('click')
    await f.button('保留当前分配').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.button('分配技能').element)
    // Cancellation returns to the selected candidate without submitting it.
    await f.button('分配技能').trigger('click')
    await f.button('确认技能操作').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('技能分配已保存')
    expect(document.activeElement?.textContent).toBe('Agent Skills')
    expect(f.owner.state.configuration?.version).toBe('2')
    await f.button('禁用技能').trigger('click')
    await f.button('确认技能操作').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('已禁用')
    expect(f.owner.skills.state.items[0]?.enabled).toBe(false)
    await f.button('启用技能').trigger('click')
    await f.button('确认技能操作').trigger('click')
    await flushPromises()
    const currentGrant = f.owner.skills.state.items[0]!.id
    await f.button('撤销分配').trigger('click')
    await f.button('确认技能操作').trigger('click')
    await flushPromises()
    expect(JSON.parse(f.calls('/remove')[0]![1].body as string)).toEqual({
      expected_version: '4',
      request: { assignment_id: currentGrant },
    })
    expect(f.wrapper.text()).toContain('已撤销分配，技能包保留')
    expect(f.owner.skills.state.items).toEqual([])
    expect(f.calls('/assign')).toHaveLength(3)
    await f.button('关闭技能管理').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.button('管理 Skills').element)
    expect(f.button('编辑配置').attributes('disabled')).toBeUndefined()
  })
  it('shows only original Lookup after an uncertain operation and never repeats the write', async () => {
    const f = await setup()
    f.setIntercept((path) => {
      if (path === base + '/assign') throw new TypeError('lost response')
    })
    await f.wrapper.get('button[aria-label="选择项目技能"]').trigger('click')
    await flushPromises()
    await new DOMWrapper(
      Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find((e) =>
        e.textContent?.includes('Skill-30'),
      )!,
    ).trigger('click')
    await f.button('分配技能').trigger('click')
    await f.button('确认技能操作').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('form[aria-label="确认技能分配操作"]').exists()).toBe(false)
    await f.button('查询原技能操作').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('尚未观察到原操作结果')
    f.state.last = receipt()
    f.state.config = { ...f.state.config, version: '2' }
    f.state.sequence = '2'
    f.state.rows = [f.state.last.assignment!]
    await f.button('查询原技能操作').trigger('click')
    await flushPromises()
    expect(f.calls('/assign')).toHaveLength(1)
    expect(f.calls('/commands/lookup')).toHaveLength(2)
    expect(f.wrapper.text()).toContain('已启用')
    expect(document.activeElement?.textContent).toBe('Agent Skills')
  })
})
