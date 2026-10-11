import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { shallowRef } from 'vue'
import KnowledgeTitleSearch from '../components/knowledge/KnowledgeTitleSearch.vue'
import {
  useKnowledgeTitleSearch,
  type KnowledgeTitleSearch as Controller,
} from '../composables/useKnowledgeTitleSearch'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import { titleDocument as doc, titleID as id, titleProject } from './knowledge-title-search.fixture'
const active: { wrapper: VueWrapper; page: Controller }[] = []
afterEach(() => {
  for (const { wrapper, page } of active.splice(0)) {
    wrapper.unmount()
    page.dispose()
  }
})
function fixture() {
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    projectID: titleProject,
    generation: 1,
    readGeneration: 1,
    identity: { userID: id(2), sessionID: id(3), epoch: 1 },
  })
  const blocked = shallowRef(false)
  const port = {
    searchTitles: vi.fn(async () => ({
      items: [{ document: doc(11, '%_! <script>', id(10)), ancestors: [doc(10, '根文档')] }],
    })),
    abandon: vi.fn(),
  }
  const page = useKnowledgeTitleSearch(port, context, blocked)
  const wrapper = mount(KnowledgeTitleSearch, { props: { search: page, selected: null } })
  active.push({ wrapper, page })
  return { wrapper, page, port, blocked }
}
describe('Knowledge title search user controls', () => {
  it('submits explicit literal input and renders a selectable text-only hit with its path', async () => {
    const { wrapper, port } = fixture()
    await wrapper.get('input[type="search"]').setValue('%_!')
    expect(port.searchTitles).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('ul').text()).toContain('%_! <script>')
    expect(wrapper.get('ul').text()).toContain('根文档')
    expect(wrapper.find('script').exists()).toBe(false)
    await wrapper.get('li button').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[id(11)]])
    const clear = wrapper.findAll('button').find((button) => button.text().startsWith('清空查找'))!
    await clear.trigger('click')
    expect(wrapper.find('ul').exists()).toBe(false)
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
  })
  it('blocks submit in the handler while another original request owns Session', async () => {
    const { wrapper, blocked, port } = fixture()
    await wrapper.get('input').setValue('文字')
    blocked.value = true
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(port.searchTitles).not.toHaveBeenCalled()
    blocked.value = false
    await wrapper.get('input').setValue('')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toContain('请输入有效标题文字')
    expect(port.searchTitles).not.toHaveBeenCalled()
  })
})
