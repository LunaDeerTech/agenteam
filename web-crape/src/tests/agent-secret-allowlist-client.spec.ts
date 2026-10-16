import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import {
  captureAgentCreateInput,
  captureAgentUpdateInput,
  createAgentConfigurationAPI,
  type AgentCreateInput,
} from '../api/agent-configuration'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const project = id(1),
  agent = id(2),
  model = id(3),
  install = id(30)
const instant = '2026-10-11T12:00:00.000000Z'
const write = { csrfToken: 'S'.repeat(43), key: 'original-agent-create/1' }
const original = (): AgentCreateInput => ({
  request: {
    agent_id: agent,
    name: 'Review-Agent',
    model_ref: model,
    inject_agents_md: false,
    approval_policy: 'default',
    allowed_tool_ids: [],
    allowed_mount_ids: [],
    allowed_secret_variable_ids: [id(20)],
    description: '',
    display_name: null,
    instructions: '  保留配置与换行\nDo not normalize.  ',
  },
})
const configuration = () => ({
  id: agent,
  project_id: project,
  name: 'Review-Agent',
  normalized_name: 'review-agent',
  display_name: null,
  tag_color: null,
  description: '',
  instructions: original().request.instructions,
  inject_agents_md: false,
  model_ref: model,
  reasoning_effort: null,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [install],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [id(20)],
  lifecycle: 'active',
  version: '1',
  created_at: instant,
  updated_at: instant,
})
const receipt = () => ({ agent: configuration(), changed: true, event_ids: [id(40)] })
const json = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  })

describe('Agent Secret allowlist presence and original receipts', () => {
  it('captures explicit empty and sorted sets immutably while preserving omitted fields', () => {
    const ids = [id(20), id(21)]
    const input = captureAgentUpdateInput({
      expected_version: '1',
      request: { allowed_secret_variable_ids: ids },
    })
    ids.pop()
    expect(input.request.allowed_secret_variable_ids).toEqual([id(20), id(21)])
    expect(Object.isFrozen(input.request.allowed_secret_variable_ids)).toBe(true)
    expect(
      captureAgentUpdateInput({
        expected_version: '1',
        request: { allowed_secret_variable_ids: [] },
      }).request,
    ).toEqual({ allowed_secret_variable_ids: [] })
    expect(
      captureAgentUpdateInput({ expected_version: '1', request: { description: 'metadata only' } })
        .request,
    ).not.toHaveProperty('allowed_secret_variable_ids')
    for (const value of [
      null,
      Array(1),
      [id(20), id(20)],
      [id(21), id(20)],
      [id(171).toUpperCase()],
      Array.from({ length: 129 }, (_, n) => id(100 + n)),
    ])
      expect(() =>
        captureAgentUpdateInput({
          expected_version: '1',
          request: { allowed_secret_variable_ids: value },
        }),
      ).toThrow()
  })

  it('requires exact original IDs in both update and Lookup receipts, including explicit clearing', async () => {
    for (const ids of [[id(21)], []]) {
      const input = captureAgentUpdateInput({
        expected_version: '1',
        request: { allowed_secret_variable_ids: ids },
      })
      const result = {
        agent: { ...configuration(), allowed_secret_variable_ids: ids, version: '2' },
        changed: true,
        event_ids: [id(40)],
      }
      const send = vi.fn<Fetch>(async (_path, init) =>
        json(init.method === 'PATCH' ? result : { status: 'committed', receipt: result }),
      )
      const api = createAgentConfigurationAPI(send)
      expect(await api.update(project, agent, input, write)).toEqual(result)
      expect(await api.lookupUpdate(project, agent, input, write)).toEqual({
        status: 'committed',
        receipt: result,
      })
      expect(JSON.parse(send.mock.calls[0]![1].body as string)).toEqual(input)
      expect(JSON.parse(send.mock.calls[1]![1].body as string)).toEqual({
        command: 'agent.update',
        target_id: agent,
        ...input,
      })
      expect(new Headers(send.mock.calls[1]![1].headers).get('Idempotency-Key')).toBe(write.key)
      const wrong = { ...result, agent: { ...result.agent, allowed_secret_variable_ids: [id(20)] } }
      const bad = createAgentConfigurationAPI(async (_path, init) =>
        json(init.method === 'PATCH' ? wrong : { status: 'committed', receipt: wrong }),
      )
      await expect(bad.update(project, agent, input, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
      await expect(bad.lookupUpdate(project, agent, input, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })

  it('creates with a nonempty set and keeps current reads distinct from the original receipt', async () => {
    const input = captureAgentCreateInput(original())
    const current = { ...configuration(), version: '2', allowed_secret_variable_ids: [id(21)] }
    const api = createAgentConfigurationAPI(async (_url, init) =>
      json(init.method === 'GET' ? current : receipt()),
    )
    expect((await api.create(project, input, write)).agent.allowed_secret_variable_ids).toEqual([
      id(20),
    ])
    expect(
      (await api.getConfiguration(project, agent, new AbortController().signal))
        .allowed_secret_variable_ids,
    ).toEqual([id(21)])
    expect(input.request.allowed_secret_variable_ids).toEqual([id(20)])
  })
})
