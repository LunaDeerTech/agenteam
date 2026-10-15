import { afterEach, describe, expect, it, vi } from 'vitest'
import { shallowRef } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MCPOAuthCallbackView from '../views/MCPOAuthCallbackView.vue'
import type { MCPOAuthProgress } from '../composables/mcp-oauth-session'

const host = vi.hoisted(() => ({ current: null as unknown, replace: vi.fn() }))
vi.mock('../composables/useSession', () => ({ useSession: () => host.current }))
vi.mock('vue-router', () => ({ useRouter: () => ({ replace: host.replace }) }))
let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  host.replace.mockReset()
})
function setup(unknown: boolean, lookupPhase: 'confirmed' | 'stopped' = 'confirmed') {
  const progress = shallowRef<MCPOAuthProgress>({
    project: 'project',
    returnPath: '/owner/project/settings/mcp',
    phase: 'completing',
    observation: 'none',
    contextValid: true,
  })
  const oauth = {
    get progress() {
      return progress.value
    },
    captureReturn: vi.fn(),
    completeReturn: vi.fn(async () => {
      progress.value = { ...progress.value, phase: unknown ? 'uncertain' : 'confirmed' }
      if (unknown) throw new Error('private-code-must-not-render')
    }),
    lookup: vi.fn(async () => {
      progress.value = {
        ...progress.value,
        phase: lookupPhase,
        observation: lookupPhase === 'stopped' ? 'stopped' : 'complete',
      }
    }),
  }
  host.current = { projectMCP: { oauth } }
  return oauth
}
describe('MCP OAuth callback visible recovery', () => {
  it('shows confirmation and returns to the original project settings', async () => {
    const oauth = setup(false)
    wrapper = mount(MCPOAuthCallbackView, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('连接 MCP 服务')
    expect(document.activeElement).toBe(wrapper.get('h1').element)
    expect(wrapper.get('[role="status"]').text()).toContain('授权已完成')
    const button = wrapper.findAll('button').find((v) => v.text().includes('返回 MCP 设置'))!
    await button.trigger('click')
    expect(host.replace).toHaveBeenCalledExactlyOnceWith('/owner/project/settings/mcp')
    expect(oauth.completeReturn).toHaveBeenCalledTimes(1)
    expect(oauth.lookup).not.toHaveBeenCalled()
  })
  it('queries the original operation on Unknown and never offers another exchange', async () => {
    const oauth = setup(true)
    wrapper = mount(MCPOAuthCallbackView)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('授权结果尚未确认')
    expect(wrapper.text()).not.toContain('private-code')
    expect(wrapper.text()).not.toContain('重试授权')
    const lookup = wrapper.findAll('button').find((v) => v.text().includes('查询原操作'))!
    await lookup.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('授权已完成')
    expect(oauth.lookup).toHaveBeenCalledTimes(1)
    expect(oauth.completeReturn).toHaveBeenCalledTimes(1)
  })
  it('explains a stopped original operation without claiming a remote outcome or retrying it', async () => {
    const oauth = setup(true, 'stopped')
    wrapper = mount(MCPOAuthCallbackView)
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((v) => v.text().includes('查询原操作'))!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('原授权操作已停止')
    expect(wrapper.text()).toContain('远端结果无法确认')
    expect(wrapper.text()).not.toContain('授权已完成')
    expect(wrapper.text()).not.toContain('查询原操作')
    await wrapper
      .findAll('button')
      .find((v) => v.text().includes('返回 MCP 设置'))!
      .trigger('click')
    expect(host.replace).toHaveBeenCalledExactlyOnceWith('/owner/project/settings/mcp')
    expect(oauth.completeReturn).toHaveBeenCalledTimes(1)
    expect(oauth.lookup).toHaveBeenCalledTimes(1)
  })
})
