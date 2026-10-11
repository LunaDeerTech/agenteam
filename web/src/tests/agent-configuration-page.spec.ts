import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { AgentCreateReceipt, AgentUpdateReceipt } from '../api/agent-configuration'
import type { DirectoryAgent } from '../api/agent-directory'
import type { ProjectAvailableChatModel } from '../api/project-models'
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
const projectID = id(1)
const model: ProjectAvailableChatModel = {
  id: id(3),
  provider_id: id(5),
  scope: { kind: 'system' },
  name: 'Available Chat',
  provider_name: 'Configured Provider',
  version: '1',
  capabilities: {
    tool_calls: true,
    parallel_tool_calls: false,
    streaming: true,
    reasoning: true,
    input_modalities: ['text'],
    output_modalities: ['text'],
    reasoning_efforts: ['low', 'high'],
    structured_output_modes: ['text'],
    context_length: null,
    max_output: null,
  },
}
const existing: DirectoryAgent = {
  id: id(2),
  project_id: projectID,
  name: 'Existing-Agent',
  display_name: '已有助手',
  tag_color: null,
  description: '原目录描述',
  version: '1',
  created_at: time,
  updated_at: time,
}
const receipt: AgentCreateReceipt = {
  changed: true,
  event_ids: [id(50)],
  agent: {
    ...existing,
    id: id(40),
    name: 'Builder-Agent',
    normalized_name: 'builder-agent',
    display_name: '构建助手',
    description: '目录可见描述',
    instructions: '仅配置区域显示的固定指令',
    inject_agents_md: false,
    model_ref: model.id,
    reasoning_effort: 'high',
    approval_policy: 'auto',
    approval_model_ref: model.id,
    allowed_tool_ids: [id(4)],
    allowed_mount_ids: [],
    allowed_secret_variable_ids: [],
    lifecycle: 'active',
  },
}
const mounted: VueWrapper[] = []
const owners: ProjectAgents[] = []

beforeEach(() => {
  // Supply jsdom geometry for the real shared controls; do not replace the controls.
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
  vi.restoreAllMocks()
})

async function page() {
  // Typed Session results isolate the page contract. Actual Session ownership,
  // HTTP decoding, authorization and default initialization have separate tests.
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef({ identity, projectID, generation: 1, readGeneration: 1 })
  const progress = shallowRef<AgentConfigurationProgress | null>(null)
  type AgentPort = SessionController['agentConfiguration']
  const api = {
    list: vi.fn<AgentPort['list']>(async () => ({ items: [existing] })),
    getConfiguration: vi.fn<AgentPort['getConfiguration']>(async (_p, a) => ({
      ...receipt.agent,
      ...existing,
      id: a,
      normalized_name: existing.name.toLowerCase(),
    })),
    models: vi.fn<AgentPort['models']>(async () => ({ items: [model], next_cursor: null })),
    startCreate: vi.fn<AgentPort['startCreate']>(async () => receipt),
    startUpdate: vi.fn<AgentPort['startUpdate']>(async () => {
      throw new Error('unexpected configuration update')
    }),
    checkOriginal: vi.fn<AgentPort['checkOriginal']>(async () => {
      throw new Error('unexpected original-result lookup')
    }),
    abandonRead: vi.fn(),
    editRejected: vi.fn(),
    get progress() {
      return progress.value
    },
  } satisfies AgentPort
  const auth = {
    state: shallowReactive({ phase: 'authenticated', busy: false }),
    personalContext: shallowReactive({ identity }),
    agentConfiguration: api,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project: { id: projectID, lifecycle: 'active' } },
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
  return { owner, api, progress }
}

const body = () => new DOMWrapper(document.body)
function visibleLabel(element: Element) {
  const copy = element.cloneNode(true) as Element
  for (const hidden of copy.querySelectorAll('[aria-hidden="true"]')) hidden.remove()
  return copy.getAttribute('aria-label') ?? copy.textContent?.trim() ?? ''
}
function findButton(label: string) {
  return body()
    .findAll<HTMLButtonElement>('button')
    .find((item) => visibleLabel(item.element) === label)
}
function button(label: string) {
  const found = findButton(label)
  expect(found, `missing button: ${label}`).toBeDefined()
  return found!
}
async function click(label: string) {
  await button(label).trigger('click')
  await flushPromises()
}
function field(label: string) {
  const target = body()
    .findAll('label[for]')
    .find((item) => visibleLabel(item.element) === label)
  expect(target, `missing field: ${label}`).toBeDefined()
  const element = document.getElementById(target!.attributes('for')!)
  expect(element).toBeInstanceOf(HTMLElement)
  return new DOMWrapper(element as HTMLInputElement | HTMLTextAreaElement)
}
function checkbox(label: string) {
  const target = body()
    .findAll('label')
    .find((item) => visibleLabel(item.element) === label)
  expect(target, `missing checkbox: ${label}`).toBeDefined()
  return target!.get<HTMLInputElement>('input[type="checkbox"]')
}
async function choose(label: string, option: string) {
  await body().get(`button[aria-label="${label}"]`).trigger('click')
  await flushPromises()
  const selected = body()
    .get(`[role="listbox"][aria-label="${label}"]`)
    .findAll('[role="option"]')
    .find((item) => visibleLabel(item.element) === option)
  expect(selected, `missing model or policy choice: ${option}`).toBeDefined()
  await selected!.trigger('click')
  await flushPromises()
}
async function fillCreation() {
  await field('Agent 名称').setValue('Builder-Agent')
  await field('显示名称').setValue('构建助手')
  await field('Agent 描述').setValue('目录可见描述')
  await field('指令').setValue(receipt.agent.instructions)
  expect(button('确认创建 Agent').element.disabled).toBe(true)
  await choose('主模型', 'Available Chat · Configured Provider（系统）')
  await choose('推理强度', 'high')
  expect(button('确认创建 Agent').element.disabled).toBe(true)
  await choose('AGENTS.md 注入', '不注入 AGENTS.md')
  await choose('审批策略', '模型自动审批')
  expect(button('确认创建 Agent').element.disabled).toBe(true)
  await choose('审批模型', 'Available Chat · Configured Provider（系统）')
  expect(checkbox('添加默认 Skills').element.checked).toBe(true)
  expect(checkbox('启用 Install Skill 工具').element.checked).toBe(true)
  await checkbox('添加默认 Skills').setValue(false)
  expect(button('确认创建 Agent').element.disabled).toBe(false)
}

describe('Agent configuration page with real UI controls', () => {
  it('waits for parent cancellation, then creates explicit configuration and displays the safe directory and full receipt', async () => {
    const f = await page()
    await click('创建 Agent')
    expect(document.activeElement).toBe(field('Agent 名称').element)
    await field('Agent 名称').setValue('Unsaved-Agent')
    await click('取消创建')
    expect(body().find('form[aria-label="创建 Agent"]').exists()).toBe(true)
    expect(findButton('创建 Agent')).toBeUndefined()
    expect(f.api.startCreate).not.toHaveBeenCalled()
    // App owns the confirmation dialog. Resolve its actual public callback only
    // after checking that the page has not discarded the draft or restored focus.
    f.owner.finishConfirmation(true)
    await flushPromises()
    expect(body().find('form[aria-label="创建 Agent"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button('创建 Agent').element)

    await click('创建 Agent')
    await fillCreation()
    await click('确认创建 Agent')
    expect(f.api.startCreate).toHaveBeenCalledExactlyOnceWith(projectID, {
      name: 'Builder-Agent',
      display_name: '构建助手',
      description: '目录可见描述',
      instructions: receipt.agent.instructions,
      model_ref: model.id,
      reasoning_effort: 'high',
      inject_agents_md: false,
      approval_policy: 'auto',
      approval_model_ref: model.id,
      allowed_tool_ids: [],
      allowed_mount_ids: [],
      allowed_secret_variable_ids: [],
      add_skills_enabled: false,
    })
    expect(body().find('form[aria-label="创建 Agent"]').exists()).toBe(false)
    const directory = body().get('section[aria-label="Agent 目录"]')
    expect(directory.text()).toContain('构建助手')
    expect(directory.text()).not.toContain(receipt.agent.instructions)
    expect(directory.text()).not.toContain(id(4))
    const detail = body().get('section[aria-label="Agent 配置"]')
    expect(detail.findAll('dt')).toHaveLength(20)
    expect(detail.text()).toContain(receipt.agent.instructions)
    expect(detail.text()).toContain(id(4))
    expect(detail.text()).toContain('模型自动审批')
    expect(detail.text()).toContain('目录和配置本身不授予执行权限。')
    expect(detail.find('input, textarea, select').exists()).toBe(false)
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
  })

  it('hides creation after Unknown and only queries the original result while its context remains valid', async () => {
    const f = await page()
    const original: AgentConfigurationProgress = {
      kind: 'create',
      projectID,
      agentID: receipt.agent.id,
      phase: 'uncertain',
      receipt: null,
      observation: 'none',
      contextValid: true,
    }
    f.api.startCreate.mockImplementationOnce(async () => {
      f.progress.value = original
      throw new AccountFailure('transport')
    })
    f.api.checkOriginal.mockResolvedValueOnce({ status: 'not_observed', receipt: null })
    await click('创建 Agent')
    await fillCreation()
    await click('确认创建 Agent')
    expect(body().get('section[aria-label="Agent 创建结果恢复"]').text()).toContain(
      '创建结果待确认。请查询原操作结果，不要重复创建。',
    )
    expect(body().find('form[aria-label="创建 Agent"]').exists()).toBe(false)
    expect(findButton('创建 Agent')).toBeUndefined()
    expect(findButton('确认创建 Agent')).toBeUndefined()
    f.progress.value = { ...original, contextValid: false }
    await flushPromises()
    expect(button('查询原创建结果').element.disabled).toBe(true)
    await click('查询原创建结果')
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
    f.progress.value = original
    await flushPromises()
    await click('查询原创建结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(1)
    expect(body().text()).toContain('尚未确认原操作结果，请继续保留原操作。')
    expect(findButton('创建 Agent')).toBeUndefined()
    f.api.checkOriginal.mockImplementationOnce(async () => {
      f.progress.value = { ...original, phase: 'confirmed', receipt, observation: 'committed' }
      return { status: 'committed', receipt }
    })
    await click('查询原创建结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.api.startCreate).toHaveBeenCalledTimes(1)
    expect(body().find('section[aria-label="Agent 创建结果恢复"]').exists()).toBe(false)
    expect(body().text()).toContain('Agent 已创建。配置与运行权限仍以实际服务校验为准。')
    expect(body().get('section[aria-label="Agent 配置"]').text()).toContain(
      receipt.agent.instructions,
    )
    expect(body().find('form[aria-label="创建 Agent"]').exists()).toBe(false)
    expect(button('创建 Agent').element.disabled).toBe(false)
  })
})

describe('Agent text configuration editing with real controls', () => {
  it('uses actual model choices and saves automatic approval then clears its reference when returning to default', async () => {
    const f = await page()
    const primary: ProjectAvailableChatModel = {
      ...model,
      name: 'Original Chat',
      capabilities: {
        ...model.capabilities,
        tool_calls: false,
        reasoning: false,
        reasoning_efforts: [],
      },
    }
    const replacement: ProjectAvailableChatModel = {
      ...primary,
      id: id(7),
      name: 'Replacement Chat',
    }
    const initial = {
      ...receipt.agent,
      ...existing,
      normalized_name: existing.name.toLowerCase(),
      model_ref: primary.id,
      inject_agents_md: true,
      reasoning_effort: null,
      approval_policy: 'default' as const,
      approval_model_ref: null,
    }
    f.api.getConfiguration.mockResolvedValueOnce(initial)
    f.api.models.mockResolvedValue({
      items: [primary, replacement, { ...model, id: id(12), name: 'Unsupported Reasoning' }],
      next_cursor: null,
    })
    await body().get('.agent-entry').trigger('click')
    await flushPromises()
    await click('编辑配置')
    expect(document.activeElement).toBe(field('Agent 名称').element)
    expect(f.api.models).toHaveBeenCalledExactlyOnceWith(projectID, { limit: 50 })
    expect(field('主模型').text()).toContain('Original Chat')
    await choose('主模型', 'Replacement Chat · Configured Provider（系统）')
    await choose('AGENTS.md 注入', '不注入 AGENTS.md')
    await choose('审批策略', '模型自动审批')
    expect(button('保存配置').element.disabled).toBe(true)
    await choose('审批模型', 'Original Chat · Configured Provider（系统）')
    const updated: AgentUpdateReceipt = {
      agent: {
        ...initial,
        model_ref: id(7),
        inject_agents_md: false,
        approval_policy: 'auto',
        approval_model_ref: primary.id,
        version: '2',
      },
      changed: true,
      event_ids: [id(53)],
    }
    f.api.startUpdate.mockResolvedValueOnce(updated)
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(projectID, existing.id, {
      expected_version: '1',
      request: {
        model_ref: id(7),
        reasoning_effort: null,
        inject_agents_md: false,
        approval_policy: 'auto',
        approval_model_ref: primary.id,
      },
    })
    expect(f.owner.state.configuration).toEqual(updated.agent)
    await click('编辑配置')
    expect(field('主模型').text()).toContain('Replacement Chat')
    expect(field('审批模型').text()).toContain('Original Chat')
    await choose('审批策略', '默认审批')
    expect(
      body()
        .findAll('label')
        .some((item) => visibleLabel(item.element) === '审批模型'),
    ).toBe(false)
    f.api.startUpdate.mockResolvedValueOnce({
      agent: {
        ...updated.agent,
        approval_policy: 'default',
        approval_model_ref: null,
        version: '3',
      },
      changed: true,
      event_ids: [id(54)],
    })
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(projectID, existing.id, {
      expected_version: '2',
      request: { approval_policy: 'default', approval_model_ref: null },
    })
    expect(f.api.startUpdate).toHaveBeenCalledTimes(2)
    expect(f.owner.state.configuration).toMatchObject({
      model_ref: id(7),
      inject_agents_md: false,
      version: '3',
    })
    expect(f.api.startCreate).not.toHaveBeenCalled()
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
  })

  it('restores focus after confirmed cancellation and explicitly saves touched text or a no-op', async () => {
    const f = await page()
    await body().get('.agent-entry').trigger('click')
    await flushPromises()
    await click('编辑配置')
    expect(document.activeElement).toBe(field('Agent 名称').element)
    expect(button('保存配置').element.disabled).toBe(true)
    await field('Agent 描述').setValue('尚未保存')
    await click('取消编辑')
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(true)
    expect(f.api.startUpdate).not.toHaveBeenCalled()
    f.owner.finishConfirmation(true)
    await flushPromises()
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(document.activeElement).toBe(button('编辑配置').element)
    await click('编辑配置')
    await field('Agent 名称').setValue('Changed-Agent')
    await field('Agent 描述').setValue('')
    await field('指令').setValue('')
    const saved: AgentUpdateReceipt = {
      changed: true,
      event_ids: [id(52)],
      agent: {
        ...receipt.agent,
        ...existing,
        name: 'Changed-Agent',
        normalized_name: 'changed-agent',
        description: '',
        instructions: '',
        version: '2',
      },
    }
    f.api.startUpdate.mockResolvedValueOnce(saved)
    expect(f.api.startUpdate).not.toHaveBeenCalled()
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(projectID, existing.id, {
      expected_version: '1',
      request: { name: 'Changed-Agent', description: '', instructions: '' },
    })
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(body().text()).toContain('Agent 配置已保存。')
    expect(f.owner.state.configuration).toEqual(saved.agent)
    expect(f.owner.state.items[0]).toMatchObject({
      name: 'Changed-Agent',
      description: '',
      version: '2',
    })
    expect(f.owner.state.items[0]).not.toHaveProperty('instructions')
    await click('编辑配置')
    await field('Agent 名称').setValue('Temporary-Agent')
    await field('Agent 名称').setValue('Changed-Agent')
    f.api.startUpdate.mockResolvedValueOnce({ ...saved, changed: false, event_ids: [] })
    await click('保存配置')
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(projectID, existing.id, {
      expected_version: '2',
      request: { name: 'Changed-Agent' },
    })
    expect(body().text()).toContain('配置未变化，版本保持不变。')
    expect(f.api.startCreate).not.toHaveBeenCalled()
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
  })

  it('hides the editor after an uncertain save and permits only the original result query', async () => {
    const f = await page()
    await body().get('.agent-entry').trigger('click')
    await flushPromises()
    await click('编辑配置')
    await field('指令').setValue('replacement')
    const original: AgentConfigurationProgress = {
      kind: 'update',
      projectID,
      agentID: existing.id,
      phase: 'uncertain',
      receipt: null,
      observation: 'none',
      contextValid: true,
    }
    f.api.startUpdate.mockImplementationOnce(async () => {
      f.progress.value = original
      throw new AccountFailure('transport')
    })
    await click('保存配置')
    expect(body().find('form[aria-label="编辑 Agent 配置"]').exists()).toBe(false)
    expect(body().get('section[aria-label="Agent 保存结果恢复"]').text()).toContain(
      '保存结果待确认。请查询原操作结果，不要重复保存。',
    )
    expect(button('编辑配置').element.disabled).toBe(true)
    expect(findButton('创建 Agent')).toBeUndefined()
    f.progress.value = { ...original, contextValid: false }
    await flushPromises()
    expect(button('查询原保存结果').element.disabled).toBe(true)
    await click('查询原保存结果')
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
    f.progress.value = original
    f.api.checkOriginal.mockResolvedValueOnce({ status: 'not_observed', receipt: null })
    await flushPromises()
    await click('查询原保存结果')
    expect(body().text()).toContain('尚未确认原操作结果，请继续保留原操作。')
    const saved: AgentUpdateReceipt = {
      changed: true,
      event_ids: [id(52)],
      agent: {
        ...receipt.agent,
        ...existing,
        normalized_name: existing.name.toLowerCase(),
        instructions: 'replacement',
        version: '2',
      },
    }
    f.api.checkOriginal.mockImplementationOnce(async () => {
      f.progress.value = {
        ...original,
        phase: 'confirmed',
        receipt: saved,
        observation: 'committed',
      }
      return { status: 'committed', receipt: saved }
    })
    await click('查询原保存结果')
    expect(f.api.checkOriginal).toHaveBeenCalledTimes(2)
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(projectID, existing.id, {
      expected_version: '1',
      request: { instructions: 'replacement' },
    })
    expect(f.owner.state.configuration).toEqual(saved.agent)
    expect(body().find('section[aria-label="Agent 保存结果恢复"]').exists()).toBe(false)
    expect(f.api.startCreate).not.toHaveBeenCalled()
  })
})
