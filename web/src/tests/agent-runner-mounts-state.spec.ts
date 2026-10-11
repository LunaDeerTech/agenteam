import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { AgentConfiguration, AgentUpdateInput } from '../api/agent-configuration'
import type { AgentRunnerMounts } from '../api/agent-runner-mounts'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectAgents, type ProjectAgents } from '../composables/useProjectAgents'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const base: AgentConfiguration = {
  id: id(2),
  project_id: id(1),
  name: 'Test-Agent',
  normalized_name: 'test-agent',
  display_name: null,
  tag_color: null,
  description: 'original',
  instructions: '',
  inject_agents_md: true,
  model_ref: id(3),
  reasoning_effort: null,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [id(5)],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
  version: '1',
  created_at: '2026-10-11T10:00:00.000000Z',
  updated_at: '2026-10-11T10:00:00.000000Z',
}
const runner = {
  id: id(4),
  name: 'System worker',
  description: 'Safe description',
  version: '1',
  status: 'offline' as const,
}
const mapping = (version = '1'): AgentRunnerMounts => ({
  project_id: id(1),
  agent_id: id(2),
  agent_version: version,
  items: [{ mount_id: id(5), runner_id: id(4), workspace: 'original.dir', name: 'Legacy alias' }],
})
const model = {
  id: id(3),
  provider_id: id(7),
  scope: { kind: 'system' as const },
  name: 'Text',
  provider_name: 'Provider',
  version: '1',
  capabilities: {
    tool_calls: false,
    parallel_tool_calls: false,
    streaming: true,
    reasoning: false,
    input_modalities: ['text' as const],
    output_modalities: ['text' as const],
    reasoning_efforts: [],
    structured_output_modes: ['text' as const],
    context_length: null,
    max_output: null,
  },
}
const owners: ProjectAgents[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  owners.splice(0).forEach((o) => o.dispose())
  releases.splice(0).forEach((r) => r())
  await flushPromises()
})
async function fixture() {
  let current = { ...base },
    mounts = mapping()
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const context = shallowRef<{
    identity: typeof identity
    projectID: string
    generation: number
    readGeneration: number
  } | null>({ identity, projectID: id(1), generation: 1, readGeneration: 1 })
  const personal = shallowReactive<{ identity: typeof identity | null }>({ identity })
  type Port = SessionController['agentConfiguration']
  const api = {
    list: vi.fn<Port['list']>(async () => ({ items: [current] })),
    getConfiguration: vi.fn<Port['getConfiguration']>(async () => current),
    runnerMounts: vi.fn<Port['runnerMounts']>(async () => mounts),
    runners: vi.fn<Port['runners']>(async () => ({ items: [runner] })),
    models: vi.fn<Port['models']>(async () => ({ items: [model], next_cursor: null })),
    startCreate: vi.fn<Port['startCreate']>(async () => ({
      agent: current,
      changed: true,
      event_ids: [id(50)],
    })),
    startUpdate: vi.fn<Port['startUpdate']>(async (_p, _a, input) => {
      current = {
        ...current,
        ...input.request,
        version: String(BigInt(input.expected_version) + 1n),
      }
      if (input.request.runner_mounts !== undefined) {
        current = {
          ...current,
          allowed_mount_ids: input.request.runner_mounts.length ? [id(5)] : [],
        }
        mounts = {
          ...mounts,
          items: input.request.runner_mounts.map((r) => ({
            ...r,
            mount_id: id(5),
            name: 'Legacy alias',
          })),
        }
      }
      mounts = { ...mounts, agent_version: current.version }
      return { agent: current, changed: true, event_ids: [id(51)] }
    }),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => ({
      status: 'not_observed',
      receipt: null,
    })),
    abandonRead: vi.fn(),
    editRejected: vi.fn(),
    progress: null,
  } satisfies Port
  const auth = {
    state: shallowReactive({ phase: 'authenticated', busy: false }),
    personalContext: personal,
    agentConfiguration: api,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project: { id: id(1), lifecycle: 'active' } },
    paths: computed(() => ({ home: '/owner/project' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectAgents(auth, workspace)
  owners.push(owner)
  owner.afterNavigation('/owner/project/settings/agents')
  await flushPromises()
  return {
    owner,
    api,
    context,
    personal,
    identity,
    setCurrent: (value: AgentConfiguration) => {
      current = value
    },
    setMounts: (value: AgentRunnerMounts) => {
      mounts = value
    },
  }
}
describe('Agent Runner current configuration and drafts', () => {
  it('submits the offline Runner declaration and publishes fresh Get results instead of the receipt', async () => {
    const f = await fixture()
    await f.owner.openCreation()
    f.owner.draft.name = 'Test-Agent'
    f.owner.selectModel(id(3))
    f.owner.draft.injectAgentsMD = 'yes'
    f.owner.chooseApproval('default')
    f.owner.changeMounts(false, [{ runner_id: runner.id, workspace: 'new.dir' }])
    expect(f.owner.canSubmit.value).toBe(true)
    f.setCurrent({ ...base, version: '2' })
    f.setMounts(mapping('2'))
    f.api.startCreate.mockResolvedValueOnce({ agent: base, changed: true, event_ids: [id(50)] })
    await f.owner.submitCreation()
    const [, fields] = f.api.startCreate.mock.calls[0]!
    expect(fields.runner_mounts).toEqual([{ runner_id: id(4), workspace: 'new.dir' }])
    expect(fields).not.toHaveProperty('allowed_mount_ids')
    expect(f.api.getConfiguration).toHaveBeenCalledExactlyOnceWith(id(1), id(2))
    expect(f.api.runnerMounts).toHaveBeenCalledExactlyOnceWith(id(1), id(2))
    expect(f.owner.state.configuration?.version).toBe('2')
    expect(f.owner.state.mounts?.agent_version).toBe('2')
    expect(f.owner.state.mounts?.items[0]!.workspace).toBe('original.dir')
  })
  it('refuses mismatched reads and preserves edited rows and original version across a later current read', async () => {
    const f = await fixture()
    f.setMounts(mapping('2'))
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    expect(f.owner.editMountsReady.value).toBe(false)
    expect(f.owner.state.mounts).toBeNull()
    f.owner.changeMounts(true, [])
    expect(f.owner.edit.touched.runner_mounts).toBeUndefined()
    f.setMounts(mapping())
    await f.owner.loadConfiguration(id(2))
    expect(f.owner.edit.runnerMounts).toEqual([{ runner_id: id(4), workspace: 'original.dir' }])
    f.owner.changeMounts(true, [{ runner_id: id(4), workspace: 'my.draft' }])
    f.setCurrent({ ...base, version: '2' })
    f.setMounts(mapping('2'))
    await f.owner.loadConfiguration(id(2))
    expect(f.owner.edit.version).toBe('1')
    expect(f.owner.edit.runnerMounts[0]!.workspace).toBe('my.draft')
    expect(f.owner.canSave.value).toBe(false)
    await f.owner.submitEdit()
    expect(f.api.startUpdate).not.toHaveBeenCalled()
  })
  it('keeps legacy unmapped references when editing metadata but current entity refusal blocks every edit', async () => {
    const f = await fixture()
    f.api.runnerMounts.mockRejectedValue(new AccountFailure('transport'))
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    f.owner.touchEdit('description', 'changed')
    expect(f.owner.editMountsReady.value).toBe(false)
    expect(f.owner.canSave.value).toBe(true)
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: { description: 'changed' },
    })
    expect(f.owner.state.configuration?.allowed_mount_ids).toEqual([id(5)])
    expect(f.owner.state.mounts).toBeNull()
    await f.owner.openEdit()
    f.owner.touchEdit('description', 'private draft')
    f.api.runnerMounts.mockRejectedValueOnce(
      new AccountFailure('problem', {
        type: 'urn:test',
        title: 'Not found',
        status: 404,
        detail: '',
        instance: '/',
        code: 'NOT_FOUND',
        request_id: id(90),
        commit_state: 'not_started',
      }),
    )
    await f.owner.loadConfiguration(id(2))
    expect(f.owner.state.configuration).toBeNull()
    expect(f.owner.edit.open).toBe(false)
    expect(f.owner.canSave.value).toBe(false)
  })
  it('explicitly clears all rows, but untouched settings omit the declaration', async () => {
    const f = await fixture()
    await f.owner.selectAgent(id(2))
    await f.owner.openEdit()
    f.owner.changeMounts(true, [])
    await f.owner.submitEdit()
    expect(f.api.startUpdate).toHaveBeenCalledExactlyOnceWith(id(1), id(2), {
      expected_version: '1',
      request: { runner_mounts: [] },
    })
    expect(f.owner.state.mounts?.items).toEqual([])
    expect(f.owner.state.configuration?.allowed_mount_ids).toEqual([])
    await f.owner.openEdit()
    f.owner.touchEdit('description', 'ordinary')
    await f.owner.submitEdit()
    expect((f.api.startUpdate.mock.calls[1]![2] as AgentUpdateInput).request).not.toHaveProperty(
      'runner_mounts',
    )
  })
  it('retires late candidate reads and hides observations during checking without replacing a local draft', async () => {
    const f = await fixture()
    await f.owner.openCreation()
    f.owner.draft.name = 'kept draft'
    let release!: (v: { items: (typeof runner)[] }) => void
    f.api.runners.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve
          releases.push(() => resolve({ items: [] }))
        }),
    )
    const pending = f.owner.loadRunners()
    f.context.value = null
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.state.runners).toEqual([])
    expect(f.owner.draft.name).toBe('kept draft')
    release({ items: [runner] })
    await pending
    expect(f.owner.state.runners).toEqual([])
    f.personal.identity = { ...f.identity, sessionID: id(19), epoch: 2 }
    f.context.value = {
      identity: f.personal.identity,
      projectID: id(1),
      generation: 2,
      readGeneration: 2,
    }
    await flushPromises()
    expect(f.owner.draft.open).toBe(false)
    expect(f.owner.draft.name).toBe('')
    expect(f.api.startCreate).not.toHaveBeenCalled()
  })
})

describe('current-scope refusal across Agent settings reads', () => {
  it('retires the creation draft on current write refusal and cannot resubmit it', async () => {
    const f = await fixture()
    await f.owner.openCreation()
    f.owner.draft.name = 'New-Agent'
    f.owner.selectModel(id(3))
    f.owner.draft.injectAgentsMD = 'yes'
    f.owner.chooseApproval('default')
    f.owner.changeMounts(false, [{ runner_id: runner.id, workspace: 'private.dir' }])
    expect(f.owner.canSubmit.value).toBe(true)
    f.api.startCreate.mockRejectedValueOnce(
      new AccountFailure('problem', {
        type: 'urn:test',
        title: 'Forbidden',
        status: 403,
        detail: '',
        instance: '/',
        code: 'FORBIDDEN',
        request_id: id(90),
        commit_state: 'not_started',
      }),
    )
    await f.owner.submitCreation()
    expect(f.owner.draft.open).toBe(false)
    expect(f.owner.draft.runnerMounts).toEqual([])
    expect(f.owner.canCreate.value).toBe(false)
    await f.owner.submitCreation()
    expect(f.api.startCreate).toHaveBeenCalledTimes(1)
  })
  it.each(['models', 'list', 'runners'] as const)(
    'retires observations and write capability after current %s refusal',
    async (port) => {
      const f = await fixture()
      await f.owner.selectAgent(id(2))
      await f.owner.openEdit()
      f.owner.touchEdit('description', 'private draft')
      f.owner.changeMounts(true, [{ runner_id: id(4), workspace: 'private.dir' }])
      f.api[port].mockRejectedValueOnce(
        new AccountFailure('problem', {
          type: 'urn:test',
          title: 'Forbidden',
          status: 403,
          detail: '',
          instance: '/',
          code: 'FORBIDDEN',
          request_id: id(90),
          commit_state: 'not_started',
        }),
      )
      await (port === 'models'
        ? f.owner.loadModels()
        : port === 'list'
          ? f.owner.loadDirectory()
          : f.owner.loadRunners())
      expect(f.owner.canSave.value).toBe(false)
      expect(f.owner.canCreate.value).toBe(false)
      expect(f.owner.state.configuration).toBeNull()
      expect(f.owner.state.mounts).toBeNull()
      expect(f.owner.state.items).toEqual([])
      expect(f.owner.edit.open).toBe(false)
      await f.owner.submitEdit()
      expect(f.api.startUpdate).not.toHaveBeenCalled()
      await f.owner.refresh()
      await flushPromises()
      expect(f.owner.canCreate.value).toBe(true)
    },
  )
  it.each(['models', 'list', 'runners'] as const)(
    'preserves the current draft on local %s unavailability',
    async (port) => {
      const f = await fixture()
      await f.owner.selectAgent(id(2))
      await f.owner.openEdit()
      f.owner.touchEdit('description', 'kept draft')
      f.api[port].mockRejectedValueOnce(
        new AccountFailure('problem', {
          type: 'urn:test',
          title: 'Unavailable',
          status: 503,
          detail: '',
          instance: '/',
          code: 'DEPENDENCY_UNBOUND',
          request_id: id(90),
          commit_state: 'not_started',
        }),
      )
      await (port === 'models'
        ? f.owner.loadModels()
        : port === 'list'
          ? f.owner.loadDirectory()
          : f.owner.loadRunners())
      expect(f.owner.edit.description).toBe('kept draft')
      expect(f.owner.edit.version).toBe('1')
      expect(f.owner.state.mounts?.items[0]!.workspace).toBe('original.dir')
      expect(f.owner.canSave.value).toBe(true)
    },
  )
})
