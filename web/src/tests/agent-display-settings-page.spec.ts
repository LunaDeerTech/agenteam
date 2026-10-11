import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import { captureAgentUpdateInput, type AgentConfiguration } from '../api/agent-configuration'
import type { AgentConfigurationProgress, SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import {
  createProjectAgents,
  projectAgentsKey,
  type ProjectAgents,
} from '../composables/useProjectAgents'
import ProjectAgentsView from '../views/projects/ProjectAgentsView.vue'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const initial: AgentConfiguration = {
  id: id(2),
  project_id: id(1),
  name: 'Review-Agent',
  normalized_name: 'review-agent',
  display_name: '原显示名称',
  tag_color: '#123abc',
  description: '原描述',
  instructions: '原指令',
  model_ref: id(3),
  reasoning_effort: null,
  inject_agents_md: true,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
  version: '1',
  created_at: time,
  updated_at: time,
}
const summary = (a: AgentConfiguration) => ({
  id: a.id,
  project_id: a.project_id,
  name: a.name,
  display_name: a.display_name,
  tag_color: a.tag_color,
  description: a.description,
  version: a.version,
  created_at: a.created_at,
  updated_at: a.updated_at,
})
const mounted: VueWrapper[] = [],
  owners: ProjectAgents[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (value: string) => value })
})
afterEach(async () => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount())
  owners.splice(0).forEach((owner) => owner.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})
async function page() {
  // Controlled typed service results test publication and controls. These are
  // not database authority or a substitute for the later native browser chain.
  let current = { ...initial }
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const progress = shallowRef<AgentConfigurationProgress | null>(null)
  type Port = SessionController['agentConfiguration']
  const api = {
    list: vi.fn<Port['list']>(async () => ({ items: [summary(current)] })),
    getConfiguration: vi.fn<Port['getConfiguration']>(async () => ({ ...current })),
    runners: vi.fn<Port['runners']>(async () => ({ items: [] })),
    runnerMounts: vi.fn<Port['runnerMounts']>(async () => ({
      project_id: current.project_id,
      agent_id: current.id,
      agent_version: current.version,
      items: [],
    })),
    // Display edits remain available when the unrelated model directory fails.
    models: vi.fn<Port['models']>(async () => {
      throw new AccountFailure('transport')
    }),
    startCreate: vi.fn<Port['startCreate']>(async () => {
      throw new Error('unexpected create')
    }),
    startUpdate: vi.fn<Port['startUpdate']>(async (_p, _a, value) => {
      const captured = captureAgentUpdateInput(value)
      current = {
        ...current,
        ...captured.request,
        version: String(BigInt(captured.expected_version) + 1n),
      }
      return { agent: { ...current }, changed: true, event_ids: [id(40)] }
    }),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => ({
      status: 'not_observed',
      receipt: null,
    })),
    abandonRead: vi.fn(),
    editRejected: vi.fn(),
    get progress() {
      return progress.value
    },
  } satisfies Port
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const context = shallowRef({ identity, projectID: id(1), generation: 1, readGeneration: 1 })
  const auth = {
    state: authState,
    personalContext: shallowReactive({ identity }),
    agentConfiguration: api,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project: { id: id(1), lifecycle: 'active' } },
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectAgents(auth, workspace)
  owners.push(owner)
  owner.afterNavigation('/owner/demo/settings/agents')
  const wrapper = mount(ProjectAgentsView, {
    attachTo: document.body,
    global: { provide: { [projectAgentsKey as symbol]: owner } },
  })
  mounted.push(wrapper)
  await flushPromises()
  await wrapper.get('.agent-entry').trigger('click')
  await flushPromises()
  await click('编辑配置')
  return {
    owner,
    api,
    progress,
    authState,
    setCurrent: (value: AgentConfiguration) => {
      current = value
    },
  }
}
const body = () => new DOMWrapper(document.body)
function visibleLabel(element: Element) {
  const copy = element.cloneNode(true) as Element
  copy.querySelectorAll('[aria-hidden="true"]').forEach((hidden) => hidden.remove())
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim() ?? ''
}
function button(label: string) {
  const found = body()
    .findAll<HTMLButtonElement>('button')
    .find((item) => visibleLabel(item.element) === label)
  expect(found, `missing button ${label}`).toBeDefined()
  return found!
}
async function click(label: string) {
  await button(label).trigger('click')
  await flushPromises()
}
function field(label: string) {
  const labelNode = body()
    .findAll('label[for]')
    .find((item) => visibleLabel(item.element) === label)
  expect(labelNode, `missing field ${label}`).toBeDefined()
  return new DOMWrapper(document.getElementById(labelNode!.attributes('for')!) as HTMLInputElement)
}
const editor = () => body().find('form[aria-label="编辑 Agent 配置"]')

describe('Agent display settings with real shared controls', () => {
  it('prefills both fields, changes one without rewriting others, and reads newer current metadata on explicit refresh', async () => {
    const f = await page()
    expect(field('显示名称').element.value).toBe('原显示名称')
    expect(field('标签颜色').element.value).toBe('#123abc')
    expect(document.activeElement).toBe(field('Agent 名称').element)
    expect(button('保存配置').element.disabled).toBe(true)
    expect(f.owner.state.modelPhase).toBe('error')
    await field('显示名称').setValue('  e\u0301 助手😀  ')
    expect(button('保存配置').element.disabled).toBe(false)
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: { display_name: '  e\u0301 助手😀  ' },
    })
    expect(f.owner.state.items[0]).toMatchObject({
      display_name: '  e\u0301 助手😀  ',
      tag_color: '#123abc',
    })
    expect(f.owner.state.items[0]).not.toHaveProperty('instructions')
    expect(f.api.list).toHaveBeenCalledTimes(1)
    expect(f.api.getConfiguration).toHaveBeenCalledTimes(2)
    f.setCurrent({
      ...initial,
      display_name: '服务端当前显示名称',
      tag_color: '#abcdef',
      version: '3',
    })
    await click('刷新 Agent 目录')
    expect(f.api.list).toHaveBeenLastCalledWith(id(1), { limit: 50 })
    expect(f.api.list).toHaveBeenCalledTimes(2)
    expect(f.api.getConfiguration).toHaveBeenLastCalledWith(id(1), id(2))
    expect(f.api.getConfiguration).toHaveBeenCalledTimes(3)
    expect(body().get('section[aria-label="Agent 目录"]').text()).toContain('服务端当前显示名称')
    expect(body().get('section[aria-label="Agent 配置"]').text()).toContain('#abcdef')
    await click('编辑配置')
    expect(field('显示名称').element.value).toBe('服务端当前显示名称')
    expect(field('标签颜色').element.value).toBe('#abcdef')
    expect(f.owner.edit.version).toBe('3')
    expect(f.api.startCreate).not.toHaveBeenCalled()
  })

  it('sets a canonical color by itself then explicitly clears both fields and falls back to the Agent name', async () => {
    const f = await page()
    await field('标签颜色').setValue('#a0b1c2')
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(id(1), id(2), {
      expected_version: '1',
      request: { tag_color: '#a0b1c2' },
    })
    await click('编辑配置')
    for (const label of ['显示名称', '标签颜色']) {
      const control = field(label)
      expect(
        document.getElementById(control.attributes('aria-describedby')!)?.textContent,
      ).toContain('删除内容后保存可清空')
      await control.setValue('')
    }
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(id(1), id(2), {
      expected_version: '2',
      request: { display_name: null, tag_color: null },
    })
    expect(body().get('.agent-entry strong').text()).toBe('Review-Agent')
    expect(f.owner.state.configuration).toMatchObject({
      display_name: null,
      tag_color: null,
      version: '3',
    })
    await click('刷新 Agent 目录')
    expect(f.api.getConfiguration).toHaveBeenCalledTimes(4)
    expect(f.owner.state.items[0]).toMatchObject({
      display_name: null,
      tag_color: null,
      version: '3',
    })
    await click('编辑配置')
    expect(field('显示名称').element.value).toBe('')
    expect(field('标签颜色').element.value).toBe('')
    expect(button('保存配置').element.disabled).toBe(true)
  })

  it('keeps whitespace and uppercase input as rejected drafts instead of silently clearing or normalizing', async () => {
    const f = await page()
    await field('显示名称').setValue('   ')
    await click('保存配置')
    expect(editor().exists()).toBe(true)
    expect(field('显示名称').element.value).toBe('   ')
    expect(editor().text()).toContain('显示名称不能全为空白')
    expect(f.owner.state.configuration?.display_name).toBe('原显示名称')
    await field('显示名称').setValue('有效显示名称')
    await field('标签颜色').setValue('#ABCDEF')
    await click('保存配置')
    expect(field('标签颜色').element.value).toBe('#ABCDEF')
    expect(editor().text()).toContain('标签颜色须为小写 #rrggbb')
    expect(f.owner.state.configuration?.tag_color).toBe('#123abc')
    expect(f.owner.edit.version).toBe('1')
  })

  it('retains conflicting drafts and restores focus only after confirmed cancellation', async () => {
    const f = await page()
    await field('显示名称').setValue('未保存名称')
    await field('标签颜色').setValue('')
    f.api.startUpdate.mockRejectedValueOnce(
      new AccountFailure('problem', {
        type: 'urn:agenteam:problem:test',
        title: 'Conflict',
        status: 409,
        detail: '',
        instance: '/',
        code: 'VERSION_CONFLICT',
        request_id: id(99),
        commit_state: 'not_committed',
      }),
    )
    await click('保存配置')
    expect(field('显示名称').element.value).toBe('未保存名称')
    expect(field('标签颜色').element.value).toBe('')
    expect(field('显示名称').element.disabled).toBe(true)
    expect(button('保存配置').element.disabled).toBe(true)
    expect(f.owner.edit.version).toBe('1')
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: { display_name: '未保存名称', tag_color: null },
    })
    await click('取消编辑')
    expect(editor().exists()).toBe(true)
    f.owner.finishConfirmation(false)
    await flushPromises()
    expect(field('显示名称').element.value).toBe('未保存名称')
    await click('取消编辑')
    f.owner.finishConfirmation(true)
    await flushPromises()
    expect(editor().exists()).toBe(false)
    expect(document.activeElement).toBe(button('编辑配置').element)
    expect(f.api.startUpdate).toHaveBeenCalledTimes(1)
  })

  it('locks both fields during a busy request and exposes only original Lookup after Unknown', async () => {
    const f = await page()
    await field('显示名称').setValue('恢复名称')
    await field('标签颜色').setValue('')
    f.authState.busy = true
    await flushPromises()
    expect(field('显示名称').element.disabled).toBe(true)
    expect(field('标签颜色').element.disabled).toBe(true)
    f.authState.busy = false
    await flushPromises()
    f.api.startUpdate.mockImplementationOnce(async () => {
      f.progress.value = {
        kind: 'update',
        projectID: id(1),
        agentID: id(2),
        phase: 'uncertain',
        receipt: null,
        observation: 'none',
        contextValid: true,
      }
      throw new AccountFailure('transport')
    })
    await click('保存配置')
    expect(editor().exists()).toBe(false)
    expect(body().get('section[aria-label="Agent 保存结果恢复"]').text()).toContain('不要重复保存')
    await click('查询原保存结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.api.startUpdate).toHaveBeenCalledTimes(1)
    expect(f.owner.edit.displayName).toBe('恢复名称')
    expect(f.owner.edit.tagColor).toBe('')
    expect(f.owner.edit.version).toBe('1')
  })
})
