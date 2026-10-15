import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createProjectOwnerAPI } from '../api/project-owner'
import { createProjectMCPAPI } from '../api/project-mcp'
import { createMCPCredentialAPI } from '../api/project-mcp-credentials'
import type { Fetch } from '../api/client'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import { projectRoute, safeReturnTarget } from '../router/auth'
import ProjectMCPView from '../views/projects/ProjectMCPView.vue'
import {
  schedulerID as id,
  schedulerJSON as json,
  schedulerSession,
} from './project-scheduler.fixture'

const p = id(10),
  configID = id(20),
  connectionID = id(21)
const base = `/api/v1/projects/${p}/mcp`
const cleanups: (() => void)[] = []
afterEach(async () => {
  cleanups
    .splice(0)
    .reverse()
    .forEach((close) => close())
  await flushPromises()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
function button(wrapper: Pick<VueWrapper, 'findAll'>, label: string) {
  const result = wrapper.findAll<HTMLButtonElement>('button').find((item) => {
    const labelSpan = item.find('.button-label[aria-hidden="false"]')
    return (labelSpan.exists() ? labelSpan.text() : item.attributes('aria-label')) === label
  })
  expect(result, `button ${label}`).toBeDefined()
  return result!
}
async function fixture(initialConnected = false, scheme: 'none' | 'bearer' | 'basic' = 'none') {
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener() {},
    removeEventListener() {},
  }))
  const config = {
    id: configID,
    scope: 'project',
    project_id: p,
    version: '1',
    runtime_revision: '1',
    name: 'Owned service',
    description: 'Project tool provider',
    endpoint: 'https://mcp.example.test/mcp',
    authentication: { scheme },
  }
  const connection = {
    id: connectionID,
    project_id: p,
    config_id: configID,
    version: '1',
    config_revision: '1',
    state: 'connected',
    enabled: true,
    tools_state: 'ready',
    resources_state: 'ready',
    tools_revision: '1',
    resources_revision: '1',
  }
  let connected = initialConnected
  let intercept: Fetch | undefined
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return json(schedulerSession)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${p}`)
      return json({
        id: p,
        owner_user_id: schedulerSession.user.id,
        name: 'Demo',
        normalized_name: 'demo',
        description: '',
        lifecycle: 'active',
        version: '1',
        current_sprint_id: null,
        created_at: schedulerSession.session.issued_at,
        updated_at: schedulerSession.session.issued_at,
        archived_at: null,
      })
    if (path === `${base}/configs`)
      return json(init.method === 'GET' ? { items: [config] } : { config })
    if (path === `${base}/connections`) return json({ items: connected ? [connection] : [] })
    if (path === `${base}/credentials`)
      return json({ credential_id: id(40), purpose: 'mcp', version: '1', deleted: false })
    if (path === `${base}/connections/connect`) {
      connected = true
      return json({ connection })
    }
    if (path === `${base}/connections/${connectionID}/disable`) {
      connection.enabled = false
      connection.version = '2'
      return json({ connection })
    }
    if (path.endsWith('/tools'))
      return json({
        items: [
          {
            tool_id: id(22),
            spec_revision: '1',
            config_id: configID,
            connection_id: connectionID,
            remote_name: 'search',
            active: true,
            default_enabled: false,
            definition: {
              stable_key: `mcp:${configID}:search`,
              name: 'search',
              description: '<script>untrusted tool</script>',
              input_schema: { type: 'object', properties: {} },
            },
          },
        ],
      })
    if (path.endsWith('/resources'))
      return json({
        items: [
          {
            id: id(23),
            connection_id: connectionID,
            kind: 'resource',
            uri: 'file:///provider/notes',
            name: 'Notes',
            title: '',
            description: '',
            mime_type: 'text/plain',
            revision: '1',
          },
        ],
      })
    throw new Error('unexpected controlled MCP request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof sessionModule.createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = { mcp: createProjectMCPAPI(fetch), mcpCredentials: createMCPCredentialAPI(fetch) }
  const auth = sessionModule.createSessionController(...args)
  await auth.restore()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(auth)
  const workspace = createProjectWorkspace(auth)
  workspace.afterNavigation('/owner/demo/settings/mcp')
  await flushPromises()
  cleanups.push(() => {
    workspace.dispose()
    auth.leave()
  })
  function page() {
    const wrapper = mount(ProjectMCPView, {
      attachTo: document.body,
      global: { provide: { [projectWorkspaceKey as symbol]: workspace } },
    })
    cleanups.push(() => wrapper.unmount())
    return wrapper
  }
  return {
    auth,
    workspace,
    fetch,
    normal,
    connection,
    page,
    intercept(value: Fetch) {
      intercept = value
    },
  }
}

describe('Project MCP real Session page interactions', () => {
  it('shows stopped refresh as unknown and enables explicit action only after lookup', async () => {
    const f = await fixture(true),
      w = f.page()
    f.intercept(async (path, init) => {
      if (path.endsWith('/refresh')) throw new Error('original response lost')
      if (path.endsWith('/commands/lookup')) return json({ state: 'stopped' })
      return f.normal(path, init)
    })
    await flushPromises()
    await button(w, '刷新发现').trigger('click')
    await flushPromises()
    expect(button(w, '刷新发现').element.disabled).toBe(true)
    await button(w, '查询原操作').trigger('click')
    await flushPromises()
    expect(w.find('[aria-label="已停止的连接操作"]').text()).toContain('远端结果无法确认')
    expect(f.auth.projectMCP.progress?.phase).toBe('stopped')
    expect(button(w, '刷新发现').element.disabled).toBe(false)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/refresh'))).toHaveLength(1)
  })
  it('saves a static credential without echoing it and connects using its original receipt', async () => {
    const f = await fixture(false, 'bearer'),
      w = f.page()
    await flushPromises()
    await button(w, '设置连接凭据').trigger('click')
    await flushPromises()
    const dialog = new DOMWrapper(document.querySelector('[role="dialog"]')!)
    await dialog.find('input[type="password"]').setValue('owned-token')
    await dialog.find('form').trigger('submit')
    await flushPromises()
    expect(document.querySelector('input[type="password"]')).toBeNull()
    expect(w.text()).not.toContain('owned-token')
    expect(JSON.stringify(f.auth.projectMCP.credentials.progress)).not.toContain('owned-token')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/credentials'))).toHaveLength(1)
    await button(w, '使用已保存凭据连接').trigger('click')
    await flushPromises()
    const request = f.fetch.mock.calls.find(([path]) => path.endsWith('/connect'))!
    expect(JSON.parse(request[1].body as string)).toEqual({
      config_id: configID,
      expected_version: '0',
      credential_id: id(40),
    })
    expect(w.text()).toContain('已连接')
  })
  it('keeps an unknown credential operation across remount and only queries its original receipt', async () => {
    const f = await fixture(false, 'basic')
    f.intercept(async (path, init) => {
      if (path.endsWith('/credentials')) throw new Error('lost original response')
      if (path.endsWith('/credential-commands/lookup'))
        return json({
          observed: true,
          result: { credential_id: id(40), purpose: 'mcp', version: '1', deleted: false },
        })
      return f.normal(path, init)
    })
    let w = f.page()
    await flushPromises()
    await button(w, '设置连接凭据').trigger('click')
    await flushPromises()
    const dialog = new DOMWrapper(document.querySelector('[role="dialog"]')!)
    await dialog.find('input[autocomplete="off"]').setValue('owner')
    await dialog.find('input[type="password"]').setValue('owned-password')
    await dialog.find('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('凭据保存结果尚未确认')
    expect(document.querySelector('input[type="password"]')).toBeNull()
    w.unmount()
    w = f.page()
    await flushPromises()
    await button(w, '查询原凭据操作').trigger('click')
    await flushPromises()
    expect(button(w, '使用已保存凭据连接').element.disabled).toBe(false)
    const calls = f.fetch.mock.calls.filter(
      ([path]) => path.endsWith('/credentials') || path.endsWith('/credential-commands/lookup'),
    )
    expect(calls).toHaveLength(2)
    expect(JSON.parse(calls[0]![1].body as string)).toEqual({ value: btoa('owner:owned-password') })
    expect(calls[1]![1].body).toBe('{"kind":"create"}')
    expect(new Headers(calls[0]![1].headers).get('Idempotency-Key')).toBe(
      new Headers(calls[1]![1].headers).get('Idempotency-Key'),
    )
  })
  it('connects and rereads actual discovery, renders provider metadata as text, then disables', async () => {
    const f = await fixture()
    const w = f.page()
    await flushPromises()
    expect(w.text()).toContain('未连接')
    await button(w, '连接服务').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('已连接')
    expect(w.text()).toContain('search')
    expect(w.text()).toContain('<script>untrusted tool</script>')
    expect(w.find('script').exists()).toBe(false)
    expect(w.text()).toContain('file:///provider/notes')
    expect(w.find('a[href^="file:"]').exists()).toBe(false)
    await button(w, '停用连接').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('连接已停用，目录仅供查看')
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/connect'))).toHaveLength(1)
    expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/disable'))).toHaveLength(1)
  })
  it('retains the original unknown request across page remount and never resends it', async () => {
    const f = await fixture()
    let lookup = 0
    f.intercept(async (path, init) => {
      if (path.endsWith('/connect')) throw new Error('lost original response')
      if (path.endsWith('/commands/lookup'))
        return json(
          ++lookup === 1
            ? { state: 'not_found' }
            : { state: 'complete', result: { connection: f.connection } },
        )
      return f.normal(path, init)
    })
    let w = f.page()
    await flushPromises()
    await button(w, '连接服务').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('操作结果尚未确认')
    expect(button(w, '连接服务').element.disabled).toBe(true)
    w.unmount()
    w = f.page()
    await flushPromises()
    expect(w.text()).toContain('操作结果尚未确认')
    await button(w, '查询原操作').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('不能据此判断它未提交')
    await button(w, '查询原操作').trigger('click')
    await flushPromises()
    expect(f.auth.projectMCP.progress?.phase).toBe('confirmed')
    const writes = f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')
    expect(writes.filter(([path]) => path.endsWith('/connect'))).toHaveLength(1)
    expect(
      new Set(writes.map(([, init]) => new Headers(init.headers).get('Idempotency-Key'))).size,
    ).toBe(1)
  })
  it('opens the real bounded config form and exposes only implemented authentication', async () => {
    const f = await fixture(),
      w = f.page()
    await flushPromises()
    await button(w, '添加服务').trigger('click')
    await flushPromises()
    const dialog = new DOMWrapper(document.querySelector('[role="dialog"]')!)
    expect(dialog.text()).toContain('添加 MCP 服务')
    expect(dialog.text()).toContain('认证方式无认证')
    expect(dialog.find('input[type="password"]').exists()).toBe(false)
    expect(dialog.text()).not.toContain('OAuth')
    await button(dialog, '取消').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.fetch.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
  })
  it('admits only the canonical MCP settings address', () => {
    expect(projectRoute('/owner/demo/settings/mcp')?.suffix).toBe('/settings/mcp')
    expect(safeReturnTarget('/owner/demo/settings/mcp')).toBe('/owner/demo/settings/mcp')
    for (const path of [
      '/owner/demo/settings/mcp?x=1',
      '/owner/demo/settings/mcp/extra',
      '/owner/demo/settings/%6dcp',
    ])
      expect(projectRoute(path)).toBeNull()
  })
})
