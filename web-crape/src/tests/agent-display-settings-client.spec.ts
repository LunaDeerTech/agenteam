import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import type { Fetch } from '../api/client'
import {
  captureAgentUpdateInput,
  createAgentConfigurationAPI,
  type AgentConfiguration,
  type AgentUpdateFields,
} from '../api/agent-configuration'
import { createSessionController, type SessionController } from '../composables/useSession'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  agent = id(2),
  time = '2026-10-11T10:00:00.000000Z'
const path = `/api/v1/projects/${project}/agents/${agent}/configuration`
const write = { csrfToken: 'S'.repeat(43), key: 'agent-display/original' }
const configuration = (fields: AgentUpdateFields = {}, version = '2'): AgentConfiguration => ({
  id: agent,
  project_id: project,
  name: 'Review-Agent',
  normalized_name: 'review-agent',
  display_name: '原显示名称',
  tag_color: '#123abc',
  description: '',
  instructions: '',
  model_ref: id(3),
  reasoning_effort: null,
  inject_agents_md: true,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
  version,
  created_at: time,
  updated_at: time,
  ...fields,
})
const receipt = (fields: AgentUpdateFields, changed = true, version = changed ? '2' : '1') => ({
  agent: configuration(fields, version),
  changed,
  event_ids: changed ? [id(4)] : [],
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
const owners: SessionController[] = []
afterEach(async () => {
  owners.splice(0).forEach((owner) => owner.leave())
  await flushPromises()
})

describe('Agent display metadata closed update contract', () => {
  it('preserves omission, exact Unicode values and null in immutable captured updates', () => {
    const text = '  e\u0301 助手😀  '
    const draft = { expected_version: '7', request: { display_name: text } }
    const captured = captureAgentUpdateInput(draft)
    draft.request.display_name = 'later'
    expect(captured).toEqual({ expected_version: '7', request: { display_name: text } })
    expect(Object.isFrozen(captured.request)).toBe(true)
    expect(Object.hasOwn(captured.request, 'tag_color')).toBe(false)
    expect(
      captureAgentUpdateInput({ expected_version: '7', request: { tag_color: null } }),
    ).toEqual({ expected_version: '7', request: { tag_color: null } })
    expect(
      captureAgentUpdateInput({
        expected_version: '7',
        request: { display_name: null, tag_color: '#abcdef' },
      }).request,
    ).toEqual({ display_name: null, tag_color: '#abcdef' })
    expect(
      captureAgentUpdateInput({
        expected_version: '7',
        request: { display_name: '😀'.repeat(256) },
      }).request.display_name,
    ).toBe('😀'.repeat(256))
  })

  it('rejects blank, control, malformed Unicode, oversized names and noncanonical colors before fetch', () => {
    const fetcher = vi.fn<Fetch>()
    const api = createAgentConfigurationAPI(fetcher)
    for (const request of [
      { display_name: '' },
      { display_name: ' \t ' },
      { display_name: '\u0085' },
      { display_name: 'name\nline' },
      { display_name: '\ud800' },
      { display_name: 'x'.repeat(257) },
      { display_name: '😀'.repeat(257) },
      { display_name: undefined },
      { tag_color: '' },
      { tag_color: '#ABCDEF' },
      { tag_color: ' #123abc' },
      { tag_color: '#123abc ' },
      { tag_color: '#abc' },
      { tag_color: undefined },
      { displayName: 'alias' },
    ])
      expect(() =>
        api.update(project, agent, { expected_version: '1', request } as never, write),
      ).toThrowError('invalid-input')
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('checks each explicit display field in changed, no-op and Lookup receipts', async () => {
    const input = {
      expected_version: '1',
      request: { display_name: '  新助手  ', tag_color: '#abc123' },
    }
    const clear = { expected_version: '2', request: { display_name: null, tag_color: null } }
    const replies = [json(receipt(input.request)), json(receipt(clear.request, true, '3'))]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createAgentConfigurationAPI(fetcher)
    expect(await api.update(project, agent, input, write)).toEqual(receipt(input.request))
    expect(await api.update(project, agent, clear, write)).toEqual(
      receipt(clear.request, true, '3'),
    )
    expect(fetcher.mock.calls.map(([, init]) => JSON.parse(init.body as string))).toEqual([
      input,
      clear,
    ])
    expect(fetcher.mock.calls.every(([, init]) => init.method === 'PATCH')).toBe(true)
    expect(
      await createAgentConfigurationAPI(async () =>
        json(receipt({ display_name: null }, false)),
      ).update(project, agent, { expected_version: '1', request: { display_name: null } }, write),
    ).toMatchObject({ changed: false, event_ids: [], agent: { version: '1', display_name: null } })
    for (const wrong of [{ display_name: 'different' }, { tag_color: '#abcdef' }]) {
      const invalid = receipt({ ...input.request, ...wrong })
      await expect(
        createAgentConfigurationAPI(async () => json(invalid)).update(project, agent, input, write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
      await expect(
        createAgentConfigurationAPI(async () =>
          json({ status: 'committed', receipt: invalid }),
        ).lookupUpdate(project, agent, input, write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    await expect(
      createAgentConfigurationAPI(async () =>
        json(receipt({ display_name: 'ignored clear' }, true, '3')),
      ).update(project, agent, clear, write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('keeps the actual Session original key, Agent, version and nullable patch through Unknown', async () => {
    const current: SessionView = {
      user: {
        id: id(8),
        email: 'owner@example.test',
        username: 'owner',
        display_name: 'Owner',
        role: 'user',
        theme: 'system',
        version: '1',
        initial_password_suggestion: false,
      },
      session: { id: id(9), issued_at: time, absolute_expires_at: time, idle_expires_at: time },
      csrf_token: write.csrfToken,
    }
    let status: 'not_observed' | 'in_progress' | 'committed' = 'not_observed'
    const fetcher = vi.fn<Fetch>(async (url, init) => {
      if (url === '/api/v1/session') return json(current)
      if (url === path)
        return json(
          {
            type: 'urn:agenteam:problem:test',
            title: 'Unknown',
            status: 503,
            detail: '',
            instance: path,
            code: 'COMMIT_UNKNOWN',
            request_id: id(99),
            commit_state: 'unknown',
          },
          503,
        )
      if (url === `/api/v1/projects/${project}/agent-commands/lookup`) {
        const original = JSON.parse(init.body as string)
        return json({
          status,
          receipt:
            status === 'committed' ? receipt(original.request, true, '9007199254740994') : null,
        })
      }
      throw new Error(`Unexpected path ${url}`)
    })
    const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
    args[19] = { agentConfiguration: createAgentConfigurationAPI(fetcher) }
    const auth = createSessionController(...args)
    owners.push(auth)
    await auth.restore()
    const draft = {
      expected_version: '9007199254740993',
      request: { display_name: '  原助手😀  ', tag_color: null as string | null },
    }
    const expected = structuredClone(draft)
    const saving = auth.agentConfiguration.startUpdate(project, agent, draft)
    draft.request.display_name = 'later'
    draft.request.tag_color = '#ffffff'
    draft.expected_version = '2'
    await expect(saving).rejects.toMatchObject({ problem: { code: 'COMMIT_UNKNOWN' } })
    const first = fetcher.mock.calls.find(([, init]) => init.method === 'PATCH')![1]
    expect(JSON.parse(first.body as string)).toEqual(expected)
    await auth.restore()
    await expect(auth.agentConfiguration.startUpdate(project, agent, draft)).rejects.toMatchObject({
      kind: 'busy',
    })
    for (const next of ['not_observed', 'in_progress', 'committed'] as const) {
      status = next
      expect(await auth.agentConfiguration.checkOriginal()).toMatchObject({ status })
      const lookup = fetcher.mock.calls.at(-1)![1]
      expect(JSON.parse(lookup.body as string)).toEqual({
        command: 'agent.update',
        target_id: agent,
        ...expected,
      })
      expect(lookup.headers).toMatchObject({
        'Idempotency-Key': (first.headers as Record<string, string>)['Idempotency-Key'],
      })
    }
    expect(fetcher.mock.calls.filter(([, init]) => init.method === 'PATCH')).toHaveLength(1)
    expect(auth.agentConfiguration.progress).toMatchObject({
      phase: 'confirmed',
      receipt: { agent: { ...expected.request, version: '9007199254740994' } },
    })
  })
})
