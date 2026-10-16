import { afterEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectSecretsView from '../views/projects/ProjectSecretsView.vue'
import {
  secretWriteFixture,
  secretBase,
  secretPath,
  secretDeferred,
  secretJSON,
  secretProblem,
} from './project-secret-write.fixture'
const cleanups: (() => void)[] = []
afterEach(async () => {
  for (const close of cleanups.splice(0)) close()
  await flushPromises()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
const body = () => new DOMWrapper(document.body)
const activeDialog = () =>
  new DOMWrapper(document.querySelector<HTMLElement>('[role="dialog"]') ?? document.body)
function button(w: Pick<VueWrapper, 'findAll'>, label: string) {
  const candidates = activeDialog().findAll<HTMLButtonElement>('button')
  const value = candidates.find(
    (item) => item.find('.button-label[aria-hidden="false"]').text() === label,
  )
  expect(value, `button ${label}`).toBeDefined()
  return value!
}
function field(w: VueWrapper, label: string) {
  const value = activeDialog()
    .findAll('label')
    .find((item) => item.text().replace(/\s*\*$/, '') === label)
  expect(value, `field ${label}`).toBeDefined()
  return activeDialog().get<HTMLInputElement | HTMLTextAreaElement>('#' + value!.attributes('for'))
}
async function paste(w: VueWrapper, value: string) {
  const target = field(w, 'Secret 值'),
    event = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { getData: () => value } })
  target.element.dispatchEvent(event)
  await flushPromises()
  return target.element as HTMLTextAreaElement
}
async function setup(empty = false) {
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  const f = secretWriteFixture()
  if (empty)
    f.intercept((path, init) =>
      new URL(path, 'https://owned.invalid').pathname === secretBase &&
      init.method === 'GET' &&
      f.writes.length === 0
        ? Promise.resolve(secretJSON({ items: [] }))
        : f.normal(path, init),
    )
  await f.auth.restore()
  const workspace = createProjectWorkspace(f.auth)
  workspace.afterNavigation(secretPath)
  await flushPromises()
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(f.auth)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/:username/:project_name/settings/secrets', component: ProjectSecretsView },
      { path: '/elsewhere', component: { template: '<p>Elsewhere</p>' } },
    ],
  })
  await router.push(secretPath)
  const wrapper = mount(RouterView, {
    attachTo: document.body,
    global: { plugins: [router], provide: { [projectWorkspaceKey as symbol]: workspace } },
  })
  cleanups.push(() => {
    wrapper.unmount()
    workspace.dispose()
    f.auth.leave()
  })
  await flushPromises()
  return { ...f, wrapper, workspace, router }
}
async function select(f: Awaited<ReturnType<typeof setup>>) {
  await button(f.wrapper, f.current.name).trigger('click')
  await flushPromises()
}
describe('Secret creation and explicit replacement through the real page and Session', () => {
  it.each(['create', 'replace'] as const)(
    'exposes required accessible controls and all scoped browser steps from the %s form',
    async (mode) => {
      const f = await setup(mode === 'create')
      const catalog = f.wrapper.get('ul[aria-label="Secrets 目录"]')
      expect(catalog.findAll('li')).toHaveLength(mode === 'create' ? 0 : 1)
      if (mode === 'create') {
        expect(f.wrapper.text()).toContain('项目 Secrets 目录暂无条目。')
        await button(f.wrapper, '创建 Secret').trigger('click')
      } else {
        await select(f)
        expect(f.wrapper.get('.secret-detail h2').text()).toBe(f.current.name)
        await button(f.wrapper, '替换 Secret 值').trigger('click')
      }
      await flushPromises()
      const label = mode === 'create' ? 'Secret 名称' : 'Secret 值'
      const control = field(f.wrapper, label).element
      expect(document.activeElement === control).toBe(true)
      // getByLabel's full label text includes this marker; textbox accessible
      // naming excludes the aria-hidden child. Keep the actual focus check.
      expect(control.labels?.[0]?.textContent?.trim()).toBe(label + ' *')
      expect(control.labels?.[0]?.querySelector('[aria-hidden="true"]')?.textContent?.trim()).toBe(
        '*',
      )
      const form = body().get('form')
      expect(form.attributes('aria-label')).toBe(
        mode === 'create' ? '创建 Secret' : '替换 Secret 值',
      )
      if (mode === 'create') {
        await field(f.wrapper, 'Secret 名称').setValue('BROWSER_SECRET')
        await field(f.wrapper, '描述').setValue('创建说明')
        const first = await paste(f.wrapper, 'synthetic-create\r\n\tsecond')
        expect(button(form, '创建 Secret').attributes('disabled')).toBeUndefined()
        await form.trigger('submit')
        await flushPromises()
        expect(first.value).toBe('')
        expect(body().find('form').exists()).toBe(false)
        expect(f.wrapper.get('.secret-detail h2').text()).toBe('BROWSER_SECRET')
        expect(f.wrapper.get('.secret-detail dd').text()).toBe('1')
        await button(f.wrapper, '编辑名称与描述').trigger('click')
        await flushPromises()
        expect(body().get('form').attributes('aria-label')).toBe('编辑名称与描述')
        expect(body().find('textarea.secret-material').exists()).toBe(false)
        await field(f.wrapper, 'Secret 名称').setValue('RENAMED_BROWSER_SECRET')
        await field(f.wrapper, '描述').setValue('更新后的安全说明')
        expect(button(body().get('form'), '保存修改').attributes('disabled')).toBeUndefined()
        await body().get('form').trigger('submit')
        await flushPromises()
        expect(f.wrapper.get('.secret-detail h2').text()).toBe('RENAMED_BROWSER_SECRET')
        expect(f.wrapper.get('.secret-detail dd').text()).toBe('2')
        await button(f.wrapper, '替换 Secret 值').trigger('click')
        await flushPromises()
        expect(field(f.wrapper, 'Secret 值').element).toBe(document.activeElement)
      }
      const replacement = await paste(f.wrapper, 'synthetic-replacement\r\n\tsecond')
      if (mode === 'replace') f.cutNext()
      expect(button(body().get('form'), '确认替换').attributes('disabled')).toBeUndefined()
      await body().get('form').trigger('submit')
      await flushPromises()
      expect(replacement.value).toBe('')
      if (mode === 'replace') {
        expect(button(body().get('form'), '确认替换').attributes('disabled')).toBeDefined()
        expect(button(f.wrapper, '检查原请求').attributes('disabled')).toBeUndefined()
        await button(f.wrapper, '检查原请求').trigger('click')
        await flushPromises()
      }
      expect(body().find('form').exists()).toBe(false)
      expect(f.wrapper.get('.secret-detail h2').text()).toBe(
        mode === 'create' ? 'RENAMED_BROWSER_SECRET' : f.current.name,
      )
      expect(f.wrapper.get('.secret-detail dd').text()).toBe(mode === 'create' ? '3' : '2')
      expect(f.writes).toHaveLength(mode === 'create' ? 3 : 1)
      expect(f.lookups).toHaveLength(mode === 'create' ? 0 : 1)
    },
  )
  it.each([0, 1])(
    'retains the other original line ending when deleting displayed newline %s',
    async (index) => {
      const f = await setup()
      await select(f)
      await button(f.wrapper, '替换 Secret 值').trigger('click')
      await flushPromises()
      const target = await paste(f.wrapper, '\r\n\n')
      target.setSelectionRange(index, index + 1)
      target.dispatchEvent(
        new InputEvent('beforeinput', { bubbles: true, inputType: 'deleteContentBackward' }),
      )
      target.value = '\n'
      target.setSelectionRange(index, index)
      target.dispatchEvent(
        new InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }),
      )
      await body().get('form').trigger('submit')
      await flushPromises()
      expect(f.writes).toHaveLength(1)
      expect(f.writes[0]!.body.request.value === (index === 0 ? '\n' : '\r\n')).toBe(true)
    },
  )
  it('allows correcting a create name conflict without an impossible current-version read', async () => {
    const f = await setup()
    await button(f.wrapper, '创建 Secret').trigger('click')
    await flushPromises()
    await field(f.wrapper, 'Secret 名称').setValue('COLLISION')
    const old = await paste(f.wrapper, 'synthetic-collision')
    f.intercept((path, init) =>
      init.method === 'POST'
        ? Promise.resolve(secretProblem(409, 'RESOURCE_BUSY'))
        : f.normal(path, init),
    )
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(old.value).toBe('')
    expect(field(f.wrapper, 'Secret 名称').element.value).toBe('COLLISION')
    await field(f.wrapper, 'Secret 名称').setValue('AVAILABLE')
    await paste(f.wrapper, 'synthetic-reentered')
    expect(button(body().get('form'), '创建 Secret').attributes('disabled')).toBeUndefined()
    f.intercept()
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(f.writes).toHaveLength(1)
    expect(f.wrapper.get('.secret-detail h2').text()).toBe('AVAILABLE')
  })
  it('creates as ordinary Owner, preserves CRLF paste across a later edit, clears old DOM before dispatch and reads current metadata', async () => {
    const f = await setup()
    expect(f.auth.state.user?.role).toBe('user')
    await button(f.wrapper, '创建 Secret').trigger('click')
    await flushPromises()
    expect(field(f.wrapper, 'Secret 名称').element).toBe(document.activeElement)
    await field(f.wrapper, 'Secret 名称').setValue('CREATED_SECRET')
    await field(f.wrapper, '描述').setValue('安全说明')
    const raw = ' \tfirst\r\nsecond\rthird\n ',
      original = await paste(f.wrapper, raw)
    original.value += 'tail'
    original.dispatchEvent(new Event('input', { bubbles: true }))
    await body().get('form[aria-label="创建 Secret"]').trigger('submit')
    expect(original.value).toBe('')
    await flushPromises()
    expect(f.writes).toHaveLength(1)
    expect(f.writes[0]!.body.request.value === raw + 'tail').toBe(true)
    expect(JSON.stringify(f.auth.projectSecretCommands.progress).includes('first')).toBe(false)
    const reads = f.fetch.mock.calls
      .filter(([, init]) => init.method === 'GET')
      .map(([path]) => path)
    expect(reads).toContain(secretBase + '/' + f.writes[0]!.body.request.variable_id)
    expect(reads.filter((path) => path.startsWith(secretBase + '?'))).toHaveLength(2)
    expect(body().find('form').exists()).toBe(false)
    expect(f.wrapper.get('.secret-detail h2').text()).toBe('CREATED_SECRET')
  })
  it('omits material for metadata editing, then explicitly replaces it with an independent versioned command', async () => {
    const f = await setup()
    await select(f)
    await button(f.wrapper, '编辑名称与描述').trigger('click')
    await flushPromises()
    expect(body().find('textarea.secret-material').exists()).toBe(false)
    await field(f.wrapper, '描述').setValue('')
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(f.writes[0]!.body).toEqual({ expected_version: '1', request: { description: '' } })
    await button(f.wrapper, '替换 Secret 值').trigger('click')
    await flushPromises()
    const old = await paste(f.wrapper, '\t\r\n ')
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(old.value).toBe('')
    expect(f.writes[1]!.body.expected_version).toBe('2')
    expect(Object.keys(f.writes[1]!.body.request)).toEqual(['value'])
    expect(f.writes[1]!.body.request.value === '\t\r\n ').toBe(true)
    expect(f.writes[1]!.key).not.toBe(f.writes[0]!.key)
  })
  it('checks only original identity after a lost update and displays an independent newer current GET', async () => {
    const f = await setup()
    await select(f)
    await button(f.wrapper, '替换 Secret 值').trigger('click')
    await flushPromises()
    const old = await paste(f.wrapper, 'synthetic-unknown')
    f.cutNext()
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(old.value).toBe('')
    expect(button(f.wrapper, '确认替换').attributes('disabled')).toBeDefined()
    f.setCurrent({ ...f.current, version: '3', name: 'CURRENT_NAME' })
    await button(f.wrapper, '检查原请求').trigger('click')
    await flushPromises()
    expect(f.writes).toHaveLength(1)
    expect(f.lookups[0]).toEqual({
      key: f.writes[0]!.key,
      body: {
        command: 'project.secret_variable.update',
        target_id: f.current.id,
        expected_version: '1',
      },
    })
    expect(f.wrapper.get('.secret-detail h2').text()).toBe('CURRENT_NAME')
    expect(f.wrapper.get('.secret-facts').text()).toContain('3')
  })
  it('requires a fresh version after conflict while preserving only the safe draft', async () => {
    const f = await setup()
    await select(f)
    await button(f.wrapper, '编辑名称与描述').trigger('click')
    await flushPromises()
    await field(f.wrapper, '描述').setValue('我的草稿')
    f.setCurrent({ ...f.current, version: '2', description: '另一作者' })
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(button(f.wrapper, '保存修改').attributes('disabled')).toBeDefined()
    await button(f.wrapper, '重新读取当前版本').trigger('click')
    await flushPromises()
    expect(field(f.wrapper, '描述').element.value).toBe('我的草稿')
    await body().get('form').trigger('submit')
    await flushPromises()
    expect(f.writes[1]!.body).toEqual({
      expected_version: '2',
      request: { description: '我的草稿' },
    })
  })
  it('clears the original material DOM during actual same-Session checking and on route retirement', async () => {
    const f = await setup()
    await select(f)
    await button(f.wrapper, '替换 Secret 值').trigger('click')
    await flushPromises()
    const old = await paste(f.wrapper, 'synthetic-checking'),
      check = secretDeferred<Response>()
    f.intercept((path, init) => (path === '/api/v1/session' ? check.promise : f.normal(path, init)))
    const checking = f.auth.restore()
    await flushPromises()
    expect(old.value).toBe('')
    expect(body().find('form').exists()).toBe(false)
    check.resolve(secretJSON(f.session))
    await checking
    await flushPromises()
    f.intercept()
    f.workspace.afterNavigation(secretPath)
    await flushPromises()
    expect(f.workspace.detail.phase).toBe('current')
    expect(f.wrapper.text()).toContain(f.current.name)
    await select(f)
    await button(f.wrapper, '替换 Secret 值').trigger('click')
    await flushPromises()
    const departed = await paste(f.wrapper, 'synthetic-leaving')
    await f.router.push('/elsewhere')
    await flushPromises()
    expect(departed.value).toBe('')
    expect(f.writes).toHaveLength(0)
  })
  it('retires current denied material/observations and permits only original Lookup after access is read again', async () => {
    const f = await setup()
    await select(f)
    await button(f.wrapper, '替换 Secret 值').trigger('click')
    await flushPromises()
    const old = await paste(f.wrapper, 'synthetic-denied')
    f.cutNext()
    await body().get('form').trigger('submit')
    await flushPromises()
    const key = f.auth.projectSecretCommands.progress!.key
    f.intercept((path, init) =>
      path.endsWith('/commands/lookup')
        ? Promise.resolve(secretProblem(404, 'NOT_FOUND'))
        : f.normal(path, init),
    )
    await button(f.wrapper, '检查原请求').trigger('click')
    await flushPromises()
    expect(old.value).toBe('')
    expect(body().find('form').exists()).toBe(false)
    expect(f.wrapper.find('.secret-detail').exists()).toBe(false)
    expect(f.auth.projectSecretCommands.progress).toMatchObject({ key, phase: 'uncertain' })
    f.intercept()
    await button(f.wrapper, '重新读取目录').trigger('click')
    await flushPromises()
    await button(f.wrapper, '检查原请求').trigger('click')
    await flushPromises()
    expect(f.writes).toHaveLength(1)
    expect(f.lookups[0]!.key).toBe(key)
  })
})
