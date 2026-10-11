import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type {
  AgentConfiguration,
  AgentCreateFields,
  AgentUpdateInput,
  AgentUpdateReceipt,
} from '../api/agent-configuration'
import type { ProjectAvailableChatModel } from '../api/project-models'
import type { SessionController, AgentConfigurationProgress } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectAgents, type ProjectAgents } from '../composables/useProjectAgents'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const time = '2026-10-11T10:00:00.000000Z'
const entry = (p = id(1), n = 2) => ({
  id: id(n),
  project_id: p,
  name: 'Review-Agent',
  display_name: null,
  tag_color: null,
  description: '目录描述',
  version: '1',
  created_at: time,
  updated_at: time,
})
const configuration = (p = id(1), n = 2): AgentConfiguration => ({
  ...entry(p, n),
  normalized_name: 'review-agent',
  instructions: '配置中的指令',
  inject_agents_md: true,
  model_ref: id(3),
  reasoning_effort: null,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [id(4)],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
})
const model: ProjectAvailableChatModel = {
  id: id(3),
  provider_id: id(5),
  scope: { kind: 'system' },
  name: 'Actual Chat',
  provider_name: 'Actual Provider',
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
const textModel: ProjectAvailableChatModel = {
  ...model,
  capabilities: {
    ...model.capabilities,
    tool_calls: false,
    reasoning: false,
    reasoning_efforts: [],
  },
}
const secondModel: ProjectAvailableChatModel = { ...textModel, id: id(7), name: 'Second Chat' }
const owners: ProjectAgents[] = [],
  release: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((owner) => owner.dispose())
  release.splice(0).forEach((done) => done())
  await flushPromises()
})
// Controlled typed reads isolate page publication; they are not authority or database substitutes.
async function fixture() {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<{
    identity: typeof identity
    projectID: string
    generation: number
    readGeneration: number
  } | null>({ identity, projectID: id(1), generation: 1, readGeneration: 1 })
  const authState = shallowReactive({ phase: 'authenticated', busy: false })
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  const progress = shallowRef<AgentConfigurationProgress | null>(null)
  const project = shallowReactive({ id: id(1), lifecycle: 'active' })
  const api = {
    list: vi.fn(async (p: string, q: { cursor?: string }) => ({
      items: [entry(p, q.cursor ? 6 : 2)],
      next_cursor: q.cursor ? undefined : 'second-page',
    })),
    getConfiguration: vi.fn(async (p: string, a: string) => ({ ...configuration(p), id: a })),
    models: vi.fn(async () => ({ items: [model], next_cursor: null })),
    startCreate: vi.fn(async (p: string, value: Omit<AgentCreateFields, 'agent_id'>) => ({
      changed: true as const,
      event_ids: [id(50)],
      agent: { ...configuration(p, 40), ...value, id: id(40) },
    })),
    startUpdate: vi.fn(
      async (p: string, a: string, value: AgentUpdateInput): Promise<AgentUpdateReceipt> => ({
        changed: true,
        event_ids: [id(51)],
        agent: {
          ...configuration(p),
          ...value.request,
          id: a,
          version: String(BigInt(value.expected_version) + 1n),
        },
      }),
    ),
    checkOriginal: vi.fn(async () => ({ status: 'not_observed' as const, receipt: null })),
    editRejected: vi.fn(),
    abandonRead: vi.fn(),
    get progress() {
      return progress.value
    },
  }
  const auth = {
    state: authState,
    personalContext: personal,
    agentConfiguration: api,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project },
    paths: computed(() => ({ home: project.id === id(1) ? '/owner/demo' : '/owner/second' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectAgents(auth, workspace)
  owners.push(owner)
  owner.afterNavigation('/owner/demo/settings/agents')
  await flushPromises()
  return { owner, api, context, personal, project, authState, progress, identity }
}

describe('Project Agent settings publication', () => {
  it('uses the safe paged directory and selected real ID before reading full configuration', async () => {
    const f = await fixture()
    expect(f.api.list).toHaveBeenCalledWith(id(1), { limit: 50 })
    expect(f.api.getConfiguration).not.toHaveBeenCalled()
    expect(f.owner.agentLabel(f.owner.state.items[0]!)).toBe('Review-Agent')
    expect(f.owner.state.items[0]).not.toHaveProperty('instructions')
    await f.owner.selectAgent(id(99))
    expect(f.api.getConfiguration).not.toHaveBeenCalled()
    await f.owner.loadDirectory(true)
    expect(f.api.list).toHaveBeenLastCalledWith(id(1), { limit: 50, cursor: 'second-page' })
    expect(f.owner.state.items.map((a) => a.id)).toEqual([id(2), id(6)])
    await f.owner.selectAgent(id(6))
    expect(f.api.getConfiguration).toHaveBeenCalledExactlyOnceWith(id(1), id(6))
    expect(f.owner.state.configuration).toMatchObject({ id: id(6), instructions: '配置中的指令' })
    expect(f.api.models).not.toHaveBeenCalled()
  })

  it('rejects old Project detail publication and keeps recovery attached only to the original Project', async () => {
    const f = await fixture()
    let finish!: () => void
    f.api.getConfiguration.mockImplementationOnce(
      async () =>
        new Promise<AgentConfiguration>((resolve) => {
          finish = () => resolve(configuration())
          release.push(finish)
        }),
    )
    const reading = f.owner.selectAgent(id(2))
    f.progress.value = {
      kind: 'create',
      projectID: id(1),
      agentID: id(40),
      phase: 'uncertain',
      receipt: null,
      observation: 'none',
      contextValid: true,
    }
    expect(f.owner.progress.value?.agentID).toBe(id(40))
    f.project.id = id(11)
    f.context.value = { identity: f.identity, projectID: id(11), generation: 2, readGeneration: 2 }
    f.owner.afterNavigation('/owner/second/settings/agents')
    await flushPromises()
    finish()
    await reading
    await flushPromises()
    expect(f.owner.state.configuration).toBeNull()
    expect(f.owner.progress.value).toBeNull()
    expect(f.owner.pending.value?.projectID).toBe(id(1))
    expect(f.owner.canCreate.value).toBe(false)
    await f.owner.lookup()
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
    expect(f.api.startCreate).not.toHaveBeenCalled()
    f.personal.identity = null
    f.context.value = null
    await flushPromises()
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.state.items).toEqual([])
  })

  it('requires a real model and explicit policies, preserves defaults, and disables archived creation', async () => {
    const f = await fixture()
    await f.owner.openCreation()
    expect(f.api.models).toHaveBeenCalledExactlyOnceWith(id(1), { limit: 50 })
    f.owner.draft.name = 'Blank-Agent'
    expect(f.owner.canSubmit.value).toBe(false)
    f.owner.selectModel(id(99))
    expect(f.owner.draft.modelID).toBe('')
    f.owner.selectModel(model.id)
    f.owner.draft.reasoningEffort = 'high'
    f.owner.draft.injectAgentsMD = 'yes'
    f.owner.chooseApproval('auto')
    expect(f.owner.canSubmit.value).toBe(false)
    f.owner.draft.approvalModelID = model.id
    expect(f.owner.canSubmit.value).toBe(true)
    f.owner.chooseApproval('default')
    expect(f.owner.draft.approvalModelID).toBe('')
    await f.owner.submitCreation()
    const [p, fields] = f.api.startCreate.mock.calls[0]!
    expect(p).toBe(id(1))
    expect(fields).toMatchObject({
      name: 'Blank-Agent',
      model_ref: model.id,
      reasoning_effort: 'high',
      inject_agents_md: true,
      approval_policy: 'default',
      allowed_tool_ids: [],
      allowed_mount_ids: [],
      allowed_secret_variable_ids: [],
    })
    for (const absent of [
      'agent_id',
      'approval_model_ref',
      'add_skills_enabled',
      'install_skill_enabled',
    ])
      expect(Object.hasOwn(fields, absent)).toBe(false)
    expect(f.owner.state.selectedID).toBe(id(40))
    f.project.lifecycle = 'archived'
    expect(f.owner.canCreate.value).toBe(false)
    await f.owner.openCreation()
    expect(f.owner.draft.open).toBe(false)
    expect(f.api.startCreate).toHaveBeenCalledTimes(1)
  })
})

describe('Agent edit generation and version gates', () => {
  it('captures only touched fields and requires explicit discard and a fresh read after a version conflict', async () => {
    const f = await fixture()
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    expect(f.owner.canSave.value).toBe(false)
    f.owner.touchEdit('description', '')
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
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: { description: '' },
    })
    expect(f.owner.edit.description).toBe('')
    expect(f.owner.edit.version).toBe('1')
    expect(f.owner.editStale.value).toBe(true)
    expect(f.owner.canSave.value).toBe(false)
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenCalledTimes(1)
    expect(f.api.getConfiguration).toHaveBeenCalledTimes(1)
    f.api.getConfiguration.mockResolvedValueOnce({ ...configuration(), version: '2' })
    const refresh = f.owner.refresh()
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.finishConfirmation(false)
    await refresh
    expect(f.owner.edit.open).toBe(true)
    const confirmedRefresh = f.owner.refresh()
    f.owner.finishConfirmation(true)
    await confirmedRefresh
    await flushPromises()
    expect(f.owner.edit.open).toBe(false)
    expect(f.owner.state.configuration?.version).toBe('2')
    await f.owner.openEdit()
    f.owner.touchEdit('name', 'Review-Agent')
    f.api.startUpdate.mockResolvedValueOnce({
      changed: false,
      event_ids: [],
      agent: { ...configuration(), version: '2' },
    })
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(id(1), id(2), {
      expected_version: '2',
      request: { name: 'Review-Agent' },
    })
    expect(f.owner.state.feedback).toBe('配置未变化，版本保持不变。')
    expect(f.owner.state.items[0]).not.toHaveProperty('instructions')
    expect(f.api.startCreate).not.toHaveBeenCalled()
  })

  it('confirms dirty Agent switches and ignores an edit result after the Project generation has retired', async () => {
    const f = await fixture()
    await f.owner.loadDirectory(true)
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    f.owner.touchEdit('instructions', '')
    const denied = f.owner.selectAgent(id(6))
    expect(f.owner.confirmation.open).toBe(true)
    f.owner.finishConfirmation(false)
    await denied
    expect(f.owner.state.selectedID).toBe(id(2))
    const allowed = f.owner.selectAgent(id(6))
    f.owner.finishConfirmation(true)
    await allowed
    expect(f.owner.state.selectedID).toBe(id(6))
    expect(f.owner.edit.open).toBe(false)
    await f.owner.openEdit()
    f.owner.touchEdit('name', 'Later-Agent')
    let finish!: () => void
    f.api.startUpdate.mockImplementationOnce(
      async () =>
        new Promise<AgentUpdateReceipt>((resolve) => {
          finish = () =>
            resolve({
              changed: true,
              event_ids: [id(51)],
              agent: { ...configuration(id(1), 6), name: 'Later-Agent', version: '2' },
            })
          release.push(finish)
        }),
    )
    const saving = f.owner.submitEdit()
    f.project.id = id(11)
    f.context.value = { identity: f.identity, projectID: id(11), generation: 2, readGeneration: 2 }
    f.owner.afterNavigation('/owner/second/settings/agents')
    await flushPromises()
    finish()
    await saving
    expect(f.owner.state.configuration).toBeNull()
    expect(f.owner.edit.open).toBe(false)
    expect(f.owner.state.feedback).toBe('')
    expect(f.owner.state.items.every((item) => item.project_id === id(11))).toBe(true)
    expect(f.api.startUpdate).toHaveBeenCalledTimes(1)
  })

  it('selects current supported models and saves the model, injection and approval pair atomically', async () => {
    const f = await fixture()
    f.api.models.mockResolvedValue({
      items: [textModel, secondModel, { ...model, id: id(12) }],
      next_cursor: null,
    })
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    expect(f.api.models).toHaveBeenCalledExactlyOnceWith(id(1), { limit: 50 })
    expect(f.owner.editModelOptions.value.map((v) => v.value)).toEqual([id(3), id(7)])
    f.owner.touchEditPolicy('model_ref', id(12))
    expect(f.owner.edit.modelID).toBe(id(3))
    expect(f.owner.editDirty.value).toBe(false)
    f.owner.touchEditPolicy('model_ref', id(7))
    f.owner.touchEditPolicy('inject_agents_md', 'no')
    f.owner.touchEditPolicy('approval_policy', 'auto')
    expect(f.owner.canSave.value).toBe(false)
    await f.owner.submitEdit()
    expect(f.api.startUpdate).not.toHaveBeenCalled()
    f.owner.touchEditPolicy('approval_model_ref', id(3))
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: {
        model_ref: id(7),
        reasoning_effort: null,
        inject_agents_md: false,
        approval_policy: 'auto',
        approval_model_ref: id(3),
      },
    })
    expect(f.owner.state.configuration).toMatchObject({
      version: '2',
      model_ref: id(7),
      approval_model_ref: id(3),
    })
    await f.owner.openEdit()
    f.owner.touchEditPolicy('approval_policy', 'default')
    expect(f.owner.edit.approvalModelID).toBe('')
    f.api.startUpdate.mockResolvedValueOnce({
      agent: {
        ...f.owner.state.configuration!,
        version: '3',
        approval_policy: 'default',
        approval_model_ref: null,
      },
      changed: true,
      event_ids: [id(52)],
    })
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenLastCalledWith(id(1), id(2), {
      expected_version: '2',
      request: { approval_policy: 'default', approval_model_ref: null },
    })
    expect(f.api.startUpdate).toHaveBeenCalledTimes(2)
    expect(f.owner.state.configuration).toMatchObject({
      model_ref: id(7),
      inject_agents_md: false,
      approval_model_ref: null,
    })
    expect(f.api.startCreate).not.toHaveBeenCalled()
    expect(f.api.checkOriginal).not.toHaveBeenCalled()
  })

  it('does not rewrite a selected model or original version when the directory changes, and rejects a late directory from another Project', async () => {
    const f = await fixture()
    f.api.models.mockResolvedValue({ items: [textModel, secondModel], next_cursor: null })
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    f.owner.touchEditPolicy('model_ref', id(7))
    f.api.models.mockResolvedValueOnce({ items: [textModel], next_cursor: null })
    await f.owner.loadModels()
    expect(f.owner.edit.modelID).toBe(id(7))
    expect(f.owner.edit.version).toBe('1')
    expect(f.owner.editModelOptions.value[0]).toMatchObject({ value: id(7), disabled: true })
    expect(f.owner.canSave.value).toBe(false)
    await f.owner.submitEdit()
    expect(f.api.startUpdate).not.toHaveBeenCalled()
    await f.owner.loadModels()
    expect(f.owner.canSave.value).toBe(true)
    let finish!: () => void
    f.api.models.mockImplementationOnce(
      async () =>
        new Promise<{ items: ProjectAvailableChatModel[]; next_cursor: null }>((resolve) => {
          finish = () => resolve({ items: [textModel, secondModel], next_cursor: null })
          release.push(finish)
        }),
    )
    const reading = f.owner.loadModels()
    f.project.id = id(11)
    f.context.value = { identity: f.identity, projectID: id(11), generation: 2, readGeneration: 2 }
    f.owner.afterNavigation('/owner/second/settings/agents')
    await flushPromises()
    finish()
    await reading
    await flushPromises()
    expect(f.owner.edit.open).toBe(false)
    expect(f.owner.state.models).toEqual([])
    expect(f.api.startUpdate).not.toHaveBeenCalled()
  })
})
