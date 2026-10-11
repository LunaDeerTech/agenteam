import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import type { AgentConfiguration, AgentCreateFields } from '../api/agent-configuration'
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
