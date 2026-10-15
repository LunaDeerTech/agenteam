import { describe, expect, it, vi } from 'vitest'
import {
  createProjectMCPAPI,
  parseMCPConnection,
  parseMCPLookup,
  parseMCPResource,
  type MCPConfigWrite,
} from '../api/project-mcp'
import { type Fetch } from '../api/client'

const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1),
  configID = id(2),
  connectionID = id(3)
const base = `/api/v1/projects/${project}/mcp`
const write = {
  csrfToken: 'S'.repeat(43),
  key: 'mcp.original:key',
  signal: new AbortController().signal,
}
const input: MCPConfigWrite = {
  expected_version: '0',
  input: {
    name: 'Owned MCP',
    description: '',
    endpoint: 'https://mcp.example.test/mcp',
    authentication: { scheme: 'none' },
  },
}
const config = {
  ...input.input,
  id: configID,
  scope: 'project',
  project_id: project,
  version: '9007199254740993',
  runtime_revision: '1',
}
const connection = {
  id: connectionID,
  project_id: project,
  config_id: configID,
  version: '2',
  config_revision: '1',
  state: 'connected',
  enabled: true,
  protocol_revision: '2026-07-28',
  tools_state: 'ready',
  resources_state: 'error',
  tools_revision: '1',
  resources_revision: '0',
  last_error_code: 'mcp_protocol_error',
}
const resource = {
  id: id(4),
  connection_id: connectionID,
  kind: 'resource',
  uri: 'file:///owned/note.txt',
  name: 'Note',
  title: '<script>untrusted</script>',
  description: '',
  mime_type: 'text/plain',
  revision: '1',
  byte_size: '9007199254740993',
}
const tool = {
  tool_id: id(5),
  spec_revision: '1',
  config_id: configID,
  connection_id: connectionID,
  remote_name: 'echo',
  default_enabled: false,
  active: true,
  definition: {
    stable_key: `mcp:${configID}:echo`,
    name: 'echo',
    description: '',
    input_schema: { type: 'object', properties: {} },
  },
}
const json = (v: unknown) =>
  new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } })

describe('MCP original account transport', () => {
  it('accepts stopped without a result only for the original refresh lookup', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ state: 'stopped' }))
    const api = createProjectMCPAPI(fetch)
    await expect(api.lookup(project, 'connection.refresh', write)).resolves.toEqual({
      state: 'stopped',
    })
    expect(fetch.mock.calls[0]![0]).toBe(`${base}/commands/lookup`)
    expect(fetch.mock.calls[0]![1].body).toBe('{"command":"connection.refresh"}')
    for (const value of [
      { state: 'stopped', result: { connection } },
      { state: 'stopped', result: null },
    ])
      expect(() => parseMCPLookup(value, project, 'connection.refresh')).toThrow()
    expect(() => parseMCPLookup({ state: 'stopped' }, project, 'connection.connect')).toThrow()
  })
  it('uses all thirteen exact routes and preserves original write keys and decimal strings', async () => {
    const fetch = vi.fn<Fetch>(async (url, init) => {
      if (url.endsWith('/commands/lookup')) return json({ state: 'pending' })
      if (init.method === 'GET')
        return json({
          items: url.endsWith('/configs')
            ? [config]
            : url.endsWith('/tools')
              ? [tool]
              : url.endsWith('/resources')
                ? [resource]
                : [connection],
        })
      if (url.includes('/configs'))
        return json(init.method === 'DELETE' ? { deleted: true } : { config })
      return json({ connection })
    })
    const api = createProjectMCPAPI(fetch)
    expect((await api.configs(project, write.signal))[0]?.version).toBe('9007199254740993')
    await api.createConfig(project, input, write)
    await api.updateConfig(
      project,
      configID,
      { ...input, expected_version: '9007199254740993' },
      write,
    )
    await api.deleteConfig(project, configID, '9007199254740993', write)
    await api.connections(project, write.signal)
    await api.connect(project, { config_id: configID, expected_version: '0' }, write)
    for (const action of ['refresh', 'enable', 'disable', 'disconnect'] as const)
      await api.changeConnection(project, connectionID, action, '2', write)
    await api.tools(project, connectionID, write.signal)
    expect((await api.resources(project, connectionID, write.signal))[0]?.byte_size).toBe(
      resource.byte_size,
    )
    await api.lookup(project, 'connection.connect', write)
    expect(fetch.mock.calls.map(([url, init]) => [url, init.method])).toEqual([
      [`${base}/configs`, 'GET'],
      [`${base}/configs`, 'POST'],
      [`${base}/configs/${configID}`, 'PUT'],
      [`${base}/configs/${configID}`, 'DELETE'],
      [`${base}/connections`, 'GET'],
      [`${base}/connections/connect`, 'POST'],
      ...['refresh', 'enable', 'disable', 'disconnect'].map((action) => [
        `${base}/connections/${connectionID}/${action}`,
        'POST',
      ]),
      [`${base}/connections/${connectionID}/tools`, 'GET'],
      [`${base}/connections/${connectionID}/resources`, 'GET'],
      [`${base}/commands/lookup`, 'POST'],
    ])
    for (const [, init] of fetch.mock.calls) {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        signal: write.signal,
      })
      if (init.method !== 'GET') {
        expect(new Headers(init.headers).get('X-CSRF-Token')).toBe(write.csrfToken)
        expect(new Headers(init.headers).get('Idempotency-Key')).toBe(write.key)
      }
    }
    expect(fetch.mock.calls[2]![1].body).toContain('"expected_version":"9007199254740993"')
    expect(fetch.mock.calls[12]![1].body).toBe('{"command":"connection.connect"}')
  })
  it('keeps partial discovery and pending lookup facts without inventing success or replay', () => {
    const result = parseMCPConnection(connection, project)
    expect(result.state).toBe('connected')
    expect(result.resources_state).toBe('error')
    expect(parseMCPLookup({ state: 'not_found' }, project, 'connection.connect')).toEqual({
      state: 'not_found',
    })
    expect(() =>
      parseMCPLookup({ state: 'pending', result: { connection } }, project, 'connection.connect'),
    ).toThrow()
    expect(() =>
      parseMCPLookup(
        { state: 'complete', result: { deleted: true } },
        project,
        'connection.connect',
      ),
    ).toThrow()
  })
  it('rejects cross-project or mismatched target responses and internal fields', async () => {
    expect(() => parseMCPConnection({ ...connection, project_id: id(99) }, project)).toThrow()
    expect(() =>
      parseMCPConnection({ ...connection, runtime_binding_id: id(99) }, project),
    ).toThrow()
    expect(() => parseMCPResource({ ...resource, connection_id: id(99) }, connectionID)).toThrow()
    const api = createProjectMCPAPI(async () => json({ connection: { ...connection, id: id(99) } }))
    await expect(
      api.changeConnection(project, connectionID, 'disable', '2', write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('never fetches unsafe or unsupported input and captures original input before async work', async () => {
    const fetch = vi.fn<Fetch>(async () => json({ config }))
    const api = createProjectMCPAPI(fetch)
    for (const endpoint of [
      'http://mcp.example.test',
      'https://secret@mcp.example.test',
      'https://mcp.example.test/#secret',
      'https://mcp.example.test/\nsecret',
    ]) {
      expect(() =>
        api.createConfig(project, { ...input, input: { ...input.input, endpoint } }, write),
      ).toThrow()
    }
    for (const authentication of [
      { scheme: 'unknown-authentication' },
      { scheme: 'custom_header', header_name: 'Authorization' },
      { scheme: 'bearer', token: 'secret' },
    ]) {
      expect(() =>
        api.createConfig(
          project,
          { ...input, input: { ...input.input, authentication } } as MCPConfigWrite,
          write,
        ),
      ).toThrow()
    }
    expect(() =>
      api.connect(
        project,
        { config_id: configID, expected_version: '0', credential_id: 'secret' },
        write,
      ),
    ).toThrow()
    expect(fetch).not.toHaveBeenCalled()
    const mutable = { ...input, input: { ...input.input } }
    const pending = api.createConfig(project, mutable, write)
    mutable.input.name = 'changed after submit'
    await pending
    expect(fetch.mock.calls[0]![1].body).toBe(JSON.stringify(input))
  })
  it('rejects duplicate JSON members, duplicate directory IDs, oversize and numeric versions', async () => {
    for (const value of [
      '{"items":[],"items":[]}',
      JSON.stringify({ items: [connection, connection] }),
      JSON.stringify({ items: [{ ...connection, version: 2 }] }),
      ' '.repeat(8 * 1024 * 1024 + 1) + '{}',
    ]) {
      const api = createProjectMCPAPI(
        async () => new Response(value, { headers: { 'Content-Type': 'application/json' } }),
      )
      await expect(api.connections(project, write.signal)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('does not retry unknown writes or read resource URI content from the browser', async () => {
    const fetch = vi.fn<Fetch>(async () => {
      throw new Error('private provider detail')
    })
    const api = createProjectMCPAPI(fetch)
    await expect(
      api.connect(project, { config_id: configID, expected_version: '0' }, write),
    ).rejects.toMatchObject({ kind: 'transport' })
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(parseMCPResource(resource, connectionID).uri).toBe('file:///owned/note.txt')
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
