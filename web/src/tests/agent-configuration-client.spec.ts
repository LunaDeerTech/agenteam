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
    allowed_secret_variable_ids: [],
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
  allowed_secret_variable_ids: [],
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

describe('Owner Agent configuration closed client', () => {
  it('creates its immutable original intent and reads the full configuration with real default-tool allowance', async () => {
    const value = original(),
      captured = captureAgentCreateInput(value)
    expect(captured).toEqual(value)
    expect(Object.hasOwn(captured.request, 'install_skill_enabled')).toBe(false)
    expect(Object.hasOwn(captured.request, 'tag_color')).toBe(false)
    expect(Object.hasOwn(captured.request, 'display_name')).toBe(true)
    expect(Object.isFrozen(captured.request)).toBe(true)
    expect(Object.isFrozen(captured.request.allowed_tool_ids)).toBe(true)
    const fetcher = vi.fn<Fetch>(async (_path, init) =>
      json(init.method === 'GET' ? configuration() : receipt()),
    )
    const api = createAgentConfigurationAPI(fetcher)
    const created = await api.create(project, captured, write)
    expect(created).toEqual(receipt())
    expect(Object.isFrozen(created.agent)).toBe(true)
    expect(Object.isFrozen(created.agent.allowed_tool_ids)).toBe(true)
    expect(Object.isFrozen(created.event_ids)).toBe(true)
    const signal = new AbortController().signal
    const current = await api.getConfiguration(project, agent, signal)
    expect(current).toEqual(created.agent)
    expect(Object.keys(current)).toHaveLength(20)
    expect(fetcher.mock.calls[0]![0]).toBe(`/api/v1/projects/${project}/agents`)
    expect(fetcher.mock.calls[0]![1]).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
    })
    expect(fetcher.mock.calls[0]![1].headers).toMatchObject({
      'X-CSRF-Token': write.csrfToken,
      'Idempotency-Key': write.key,
    })
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual(value)
    expect(fetcher.mock.calls[1]![0]).toBe(
      `/api/v1/projects/${project}/agents/${agent}/configuration`,
    )
    expect(fetcher.mock.calls[1]![1]).toMatchObject({ method: 'GET', signal })
    expect(fetcher.mock.calls[1]![1].body).toBeUndefined()

    const disabled = captureAgentCreateInput({
      request: {
        ...value.request,
        install_skill_enabled: false,
        add_skills_enabled: false,
        instructions: 'x'.repeat(32768),
      },
    })
    const disabledReceipt = {
      ...receipt(),
      agent: {
        ...configuration(),
        instructions: disabled.request.instructions,
        allowed_tool_ids: [],
      },
    }
    const send = vi.fn<Fetch>(async () => json(disabledReceipt))
    expect(await createAgentConfigurationAPI(send).create(project, disabled, write)).toEqual(
      disabledReceipt,
    )
    expect(JSON.parse(send.mock.calls[0]![1].body as string)).toEqual(disabled)
    // Explicitly selecting the Registry's default Tool does not require a duplicate.
    const selected = captureAgentCreateInput({
      request: { ...value.request, allowed_tool_ids: [install] },
    })
    expect(await api.create(project, selected, write)).toEqual(receipt())
    for (const tools of [[], [install, id(31)]]) {
      const bad = createAgentConfigurationAPI(async () =>
        json({ ...receipt(), agent: { ...configuration(), allowed_tool_ids: tools } }),
      )
      await expect(bad.create(project, captured, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })

  it('keeps the exact original request and key for Lookup after Unknown without issuing another Create', async () => {
    const failure = {
      type: 'urn:agenteam:problem:test',
      title: 'Unable to confirm',
      status: 503,
      detail: 'safe failure',
      instance: `/api/v1/projects/${project}/agents`,
      code: 'COMMIT_UNKNOWN',
      request_id: id(90),
      commit_state: 'unknown',
    }
    const mutable = { request: { ...original().request, allowed_mount_ids: [id(20)] } }
    const captured = captureAgentCreateInput(mutable)
    const committed = { ...receipt(), agent: { ...configuration(), allowed_mount_ids: [id(20)] } }
    mutable.request.allowed_mount_ids.push(id(21))
    mutable.request.instructions = 'later draft'
    const replies = [
      new Response(JSON.stringify(failure), {
        status: 503,
        headers: {
          'Content-Type': 'application/problem+json',
          'X-Request-ID': id(90),
        },
      }),
      json({ status: 'not_observed', receipt: null }),
      json({ status: 'in_progress', receipt: null }),
      json({ status: 'committed', receipt: committed }),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createAgentConfigurationAPI(fetcher)
    await expect(api.create(project, captured, write)).rejects.toMatchObject({
      kind: 'problem',
      problem: failure,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
    for (const status of ['not_observed', 'in_progress', 'committed']) {
      expect(await api.lookup(project, captured, write)).toEqual({
        status,
        receipt: status === 'committed' ? committed : null,
      })
    }
    for (const [path, init] of fetcher.mock.calls.slice(1)) {
      expect(path).toBe(`/api/v1/projects/${project}/agent-commands/lookup`)
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': write.csrfToken,
        'Idempotency-Key': write.key,
      })
      expect(JSON.parse(init.body as string)).toEqual({ command: 'agent.create', ...captured })
      expect(JSON.parse(init.body as string)).not.toHaveProperty('target_id')
      expect(JSON.parse(init.body as string)).not.toHaveProperty('expected_version')
    }
    expect(fetcher).toHaveBeenCalledTimes(4)
    await expect(
      createAgentConfigurationAPI(async () => json({ status: 'committed', receipt: null })).lookup(
        project,
        captured,
        write,
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    const raw = JSON.stringify(failure).replace(
      '"commit_state":"unknown"',
      '"commit_state":"unknown","commit_state":"not_started"',
    )
    await expect(
      createAgentConfigurationAPI(
        async () =>
          new Response(raw, {
            status: 503,
            headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(90) },
          }),
      ).create(project, captured, write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('rejects malformed or mismatched fields and duplicate wire members without broadening creation authority', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(receipt()))
    const api = createAgentConfigurationAPI(fetcher)
    for (const fields of [
      { agent_id: '01900000-0000-4000-8000-000000000002' },
      { name: 'Admin' },
      { install_skill_enabled: null },
      { inject_agents_md: undefined },
      { allowed_tool_ids: [id(31), install] },
      { allowed_mount_ids: [id(20), id(20)] },
      { allowed_mount_ids: Array(1) },
      { approval_policy: 'auto' },
      { display_name: ' ' },
      { instructions: '\ud800' },
      { instructions: 'x'.repeat(32769) },
      { lifecycle: 'active' },
    ])
      expect(() =>
        captureAgentCreateInput({ request: { ...original().request, ...fields } }),
      ).toThrowError('invalid-input')
    await expect(
      Promise.resolve().then(() =>
        api.create(project, { ...original(), expected_version: '1' } as never, write),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      Promise.resolve().then(() =>
        api.create(project, original(), { ...write, key: 'invalid key' }),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
    for (const fields of [
      { project_id: id(99) },
      { normalized_name: 'Review-Agent' },
      { version: 1 },
      { allowed_mount_ids: null },
      { lifecycle: 'deleting' },
      { created_at: '2026-10-12T12:00:00.000000Z' },
      { runtime_authorized: true },
    ])
      await expect(
        createAgentConfigurationAPI(async () =>
          json({ ...configuration(), ...fields }),
        ).getConfiguration(project, agent, new AbortController().signal),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const raw = JSON.stringify(configuration()).replace(
      `"id":"${agent}"`,
      `"id":"${id(99)}","id":"${agent}"`,
    )
    await expect(
      createAgentConfigurationAPI(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      ).getConfiguration(project, agent, new AbortController().signal),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const changed of [
      { changed: false },
      { event_ids: [] },
      { agent: { ...configuration(), version: '2' } },
    ]) {
      await expect(
        createAgentConfigurationAPI(async () => json({ ...receipt(), ...changed })).create(
          project,
          original(),
          write,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
  })

  it('updates only captured fields and distinguishes exact large-version changes from no-op clears', async () => {
    const value = {
      expected_version: '9007199254740993',
      request: { name: 'Edited-Agent', description: '' },
    }
    const captured = captureAgentUpdateInput(value)
    value.request.name = 'Later-Agent'
    value.request.description = 'later draft'
    expect(captured).toEqual({
      expected_version: '9007199254740993',
      request: { name: 'Edited-Agent', description: '' },
    })
    expect(Object.hasOwn(captured.request, 'instructions')).toBe(false)
    expect(Object.isFrozen(captured)).toBe(true)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const changed = {
      agent: {
        ...configuration(),
        name: 'Edited-Agent',
        normalized_name: 'edited-agent',
        description: '',
        version: '9007199254740994',
      },
      changed: true,
      event_ids: [id(41)],
    }
    const noop = {
      agent: { ...configuration(), instructions: '', version: '9223372036854775807' },
      changed: false,
      event_ids: [],
    }
    const replies = [changed, noop]
    const fetcher = vi.fn<Fetch>(async () => json(replies.shift()!))
    const api = createAgentConfigurationAPI(fetcher)
    const signal = new AbortController().signal
    const options = { ...write, key: 'original-agent-update/1', signal }
    const result = await api.update(project, agent, captured, options)
    expect(result).toEqual(changed)
    expect(result.agent.instructions).toBe(original().request.instructions)
    expect(Object.isFrozen(result)).toBe(true)
    expect(Object.isFrozen(result.agent.allowed_tool_ids)).toBe(true)
    expect(Object.isFrozen(result.event_ids)).toBe(true)
    const clear = captureAgentUpdateInput({
      expected_version: '9223372036854775807',
      request: { instructions: '' },
    })
    expect(
      await api.update(project, agent, clear, { ...options, key: 'noop-agent-update/1' }),
    ).toEqual(noop)
    for (const [path, init] of fetcher.mock.calls) {
      expect(path).toBe(`/api/v1/projects/${project}/agents/${agent}/configuration`)
      expect(init).toMatchObject({
        method: 'PATCH',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal,
      })
    }
    expect(fetcher.mock.calls[0]![1].headers).toMatchObject({
      'X-CSRF-Token': options.csrfToken,
      'Idempotency-Key': options.key,
    })
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual(captured)
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toEqual(clear)
    expect(fetcher).toHaveBeenCalledTimes(2)
  })

  it('looks up the exact original Update after Unknown without another PATCH and preserves conflict semantics', async () => {
    const failure = {
      type: 'urn:agenteam:problem:test',
      title: 'Unable to confirm',
      status: 503,
      detail: 'safe failure',
      instance: `/api/v1/projects/${project}/agents/${agent}/configuration`,
      code: 'COMMIT_UNKNOWN',
      request_id: id(91),
      commit_state: 'unknown',
    }
    const problem = (body: typeof failure) =>
      new Response(JSON.stringify(body), {
        status: body.status,
        headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': body.request_id },
      })
    const value = {
      expected_version: '8',
      request: { description: '  原意图\n保持原字节  ', instructions: '' },
    }
    const captured = captureAgentUpdateInput(value)
    const committed = {
      agent: { ...configuration(), ...captured.request, version: '9' },
      changed: true,
      event_ids: [id(42)],
    }
    value.expected_version = '9'
    value.request.description = 'later draft'
    const replies = [
      problem(failure),
      json({ status: 'not_observed', receipt: null }),
      json({ status: 'in_progress', receipt: null }),
      json({ status: 'committed', receipt: committed }),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createAgentConfigurationAPI(fetcher)
    const options = { ...write, key: 'uncertain-agent-update/1' }
    await expect(api.update(project, agent, captured, options)).rejects.toMatchObject({
      kind: 'problem',
      problem: failure,
    })
    expect(fetcher).toHaveBeenCalledTimes(1)
    for (const status of ['not_observed', 'in_progress', 'committed']) {
      expect(await api.lookupUpdate(project, agent, captured, options)).toEqual({
        status,
        receipt: status === 'committed' ? committed : null,
      })
    }
    expect(fetcher.mock.calls[0]![1].method).toBe('PATCH')
    for (const [path, init] of fetcher.mock.calls.slice(1)) {
      expect(path).toBe(`/api/v1/projects/${project}/agent-commands/lookup`)
      expect(init.method).toBe('POST')
      expect(init.headers).toMatchObject({
        'X-CSRF-Token': options.csrfToken,
        'Idempotency-Key': options.key,
      })
      expect(JSON.parse(init.body as string)).toEqual({
        command: 'agent.update',
        target_id: agent,
        expected_version: '8',
        request: { description: '  原意图\n保持原字节  ', instructions: '' },
      })
    }
    expect(fetcher).toHaveBeenCalledTimes(4)
    const conflict = {
      ...failure,
      status: 409,
      code: 'VERSION_CONFLICT',
      commit_state: 'not_started',
    }
    const denied = vi.fn<Fetch>(async () => problem(conflict))
    await expect(
      createAgentConfigurationAPI(denied).update(project, agent, captured, options),
    ).rejects.toMatchObject({ kind: 'problem', problem: conflict })
    expect(denied).toHaveBeenCalledTimes(1)
  })

  it('rejects invalid Update presence and receipts that disagree with the original identity, patch or event shape', async () => {
    const value = { expected_version: '8', request: { description: '' } }
    for (const malformed of [
      { ...value, request: {} },
      { ...value, request: { description: null } },
      { ...value, request: { instructions: undefined } },
      { ...value, request: { name: '' } },
      { ...value, request: { model_reference: model } },
      { ...value, expected_version: 8 },
      { ...value, expected_version: '08' },
    ])
      expect(() => captureAgentUpdateInput(malformed)).toThrowError('invalid-input')
    const valid = {
      agent: { ...configuration(), version: '9' },
      changed: true,
      event_ids: [id(43)],
    }
    const fetcher = vi.fn<Fetch>(async () => json(valid))
    await expect(
      Promise.resolve().then(() =>
        createAgentConfigurationAPI(fetcher).update(project, agent, value, {
          ...write,
          key: 'invalid key',
        }),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
    for (const malformed of [
      { ...valid, agent: { ...valid.agent, id: id(99) } },
      { ...valid, agent: { ...valid.agent, project_id: id(99) } },
      { ...valid, agent: { ...valid.agent, version: '8' } },
      { ...valid, agent: { ...valid.agent, description: 'ignored clear' } },
      { ...valid, event_ids: [] },
      { ...valid, changed: false, agent: { ...valid.agent, version: '8' } },
    ])
      await expect(
        createAgentConfigurationAPI(async () => json(malformed)).update(
          project,
          agent,
          value,
          write,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    const raw = JSON.stringify(valid).replace('"changed":true', '"changed":false,"changed":true')
    await expect(
      createAgentConfigurationAPI(
        async () => new Response(raw, { headers: { 'Content-Type': 'application/json' } }),
      ).update(project, agent, value, write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
    for (const malformed of [
      { status: 'committed', receipt: null },
      { status: 'in_progress', receipt: valid },
      { status: 'committed', receipt: { ...valid, agent: { ...valid.agent, version: '10' } } },
    ])
      await expect(
        createAgentConfigurationAPI(async () => json(malformed)).lookupUpdate(
          project,
          agent,
          value,
          write,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })

  it('preserves model and approval changes as one immutable update and recovers an explicit approval clear by the original Lookup', async () => {
    const value = {
      expected_version: '9',
      request: {
        model_ref: id(7),
        reasoning_effort: null,
        inject_agents_md: false,
        approval_policy: 'auto',
        approval_model_ref: model,
      },
    }
    const captured = captureAgentUpdateInput(value)
    value.request.model_ref = id(8)
    expect(Object.isFrozen(captured.request)).toBe(true)
    expect(captured.request.model_ref).toBe(id(7))
    expect(Object.hasOwn(captured.request, 'instructions')).toBe(false)
    const saved = {
      agent: { ...configuration(), ...captured.request, version: '10' },
      changed: true,
      event_ids: [id(44)],
    }
    const cleared = {
      agent: {
        ...saved.agent,
        approval_policy: 'default',
        approval_model_ref: null,
        version: '11',
      },
      changed: true,
      event_ids: [id(45)],
    }
    const failure = {
      type: 'urn:agenteam:problem:test',
      title: 'Unable to confirm',
      status: 503,
      detail: '',
      instance: '/',
      code: 'COMMIT_UNKNOWN',
      request_id: id(91),
      commit_state: 'unknown',
    }
    const replies = [
      json(saved),
      new Response(JSON.stringify(failure), {
        status: 503,
        headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(91) },
      }),
      json({ status: 'committed', receipt: cleared }),
    ]
    const fetcher = vi.fn<Fetch>(async () => replies.shift()!)
    const api = createAgentConfigurationAPI(fetcher)
    expect(await api.update(project, agent, captured, write)).toEqual(saved)
    const clear = captureAgentUpdateInput({
      expected_version: '10',
      request: { approval_policy: 'default', approval_model_ref: null },
    })
    const originalWrite = { ...write, key: 'approval-clear/original' }
    await expect(api.update(project, agent, clear, originalWrite)).rejects.toMatchObject({
      kind: 'problem',
      problem: { code: 'COMMIT_UNKNOWN' },
    })
    expect(await api.lookupUpdate(project, agent, clear, originalWrite)).toEqual({
      status: 'committed',
      receipt: cleared,
    })
    expect(fetcher.mock.calls.map(([, init]) => init.method)).toEqual(['PATCH', 'PATCH', 'POST'])
    expect(JSON.parse(fetcher.mock.calls[0]![1].body as string)).toEqual(captured)
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toEqual(clear)
    expect(JSON.parse(fetcher.mock.calls[2]![1].body as string)).toEqual({
      command: 'agent.update',
      target_id: agent,
      ...clear,
    })
    for (const [, init] of fetcher.mock.calls.slice(1))
      expect(init.headers).toMatchObject({ 'Idempotency-Key': originalWrite.key })
  })

  it('rejects unsupported policy input and any successful receipt that ignores an explicit model, false or null', async () => {
    const fetcher = vi.fn<Fetch>()
    const api = createAgentConfigurationAPI(fetcher)
    for (const request of [
      { model_ref: null },
      { inject_agents_md: null },
      { reasoning_effort: 'high' },
      { reasoning_effort: undefined },
      { approval_policy: 'auto', approval_model_ref: null },
      { approval_policy: 'default', approval_model_ref: model },
      { approval_model_ref: undefined },
    ])
      expect(() =>
        api.update(project, agent, { expected_version: '1', request } as never, write),
      ).toThrowError('invalid-input')
    expect(fetcher).not.toHaveBeenCalled()
    const input = captureAgentUpdateInput({
      expected_version: '1',
      request: {
        model_ref: id(7),
        reasoning_effort: null,
        inject_agents_md: false,
        approval_policy: 'allow',
        approval_model_ref: null,
      },
    })
    const exact = { ...configuration(), ...input.request, version: '2' }
    for (const ignored of [
      { model_ref: model },
      { inject_agents_md: true },
      { reasoning_effort: 'high' },
      { approval_policy: 'auto', approval_model_ref: model },
    ])
      await expect(
        createAgentConfigurationAPI(async () =>
          json({
            agent: { ...exact, ...ignored },
            changed: true,
            event_ids: [id(46)],
          }),
        ).update(project, agent, input, write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})
