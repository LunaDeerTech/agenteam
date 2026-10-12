import { createMemoryHistory, createRouter } from 'vue-router'
import {
  installAuthentication,
  installProjectVariablesNavigation,
  projectRoute,
  safeReturnTarget,
} from '../router/auth'
import ProjectVariablesView from '../views/projects/ProjectVariablesView.vue'
import { UiButton, UiDialog } from '../components/ui'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed, defineComponent, h, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { ProjectVariable, ProjectVariableReceipt } from '../api/project-variables'
import {
  createProjectVariables,
  projectVariablesKey,
  type ProjectVariables,
} from '../composables/useProjectVariables'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const p = id(1),
  v = id(2),
  at = '2026-10-11T10:00:00.000001Z',
  route = '/owner/demo/settings/variables'
const data = (version = '1'): ProjectVariable => ({
  id: v,
  project_id: p,
  type: 'variable',
  name: 'ORDINARY',
  description: 'Description',
  value: 'original\ntext',
  version,
  created_at: at,
  updated_at: at,
})
type Port = SessionController['projectVariables']
const owners: ProjectVariables[] = [],
  wrappers: VueWrapper[] = []
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
  vi.stubGlobal('CSS', { escape: (v: string) => v })
})
afterEach(async () => {
  wrappers.splice(0).forEach((w) => w.unmount())
  owners.splice(0).forEach((o) => o.dispose())
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function fixture(existing = true) {
  const identity = { userID: id(8), sessionID: id(9), epoch: 1 }
  const personal = shallowReactive({
    phase: 'current',
    identity: identity as typeof identity | null,
  })
  const state = shallowReactive({
    phase: 'authenticated',
    busy: false,
    user: { id: id(8) },
    session: { id: id(9) },
  })
  const context = shallowRef<ProjectWorkspace['currentReadContext']['value']>({
    projectID: p,
    identity,
    generation: 1,
    readGeneration: 1,
  })
  const home = shallowRef('/owner/demo'),
    project = shallowReactive<{ project: Project | null }>({
      project: {
        id: p,
        owner_user_id: id(8),
        name: 'Demo',
        normalized_name: 'demo',
        description: '',
        lifecycle: 'active',
        version: '1',
        current_sprint_id: null,
        created_at: at,
        updated_at: at,
        archived_at: null,
      },
    })
  const progress = shallowRef<Port['progress']>(null),
    log: string[] = []
  let stored: ProjectVariable | null = existing ? data() : null
  const publish = (
    kind: 'create' | 'update' | 'delete',
    value: ProjectVariableReceipt | null,
    phase: 'confirmed' | 'uncertain' | 'rejected',
  ) => {
    progress.value = {
      projectID: p,
      targetID: v,
      kind,
      phase,
      receipt: value,
      observation: value ? 'committed' : 'none',
      contextValid: true,
    }
  }
  const receipt = (kind: 'create' | 'update'): ProjectVariableReceipt => ({
    command: kind === 'create' ? 'project.variable.create' : 'project.variable.update',
    changed: true,
    variable: stored!,
    event_id: id(20),
    audit_id: id(21),
  })
  const port = {
    get progress() {
      return progress.value
    },
    list: vi.fn<Port['list']>(async () => {
      log.push('list')
      if (!stored) return { items: [] }
      const { value: _value, ...summary } = stored
      return { items: [summary] }
    }),
    get: vi.fn<Port['get']>(async () => {
      log.push('get')
      if (!stored) throw new Error('Missing fixture variable')
      return stored
    }),
    create: vi.fn<Port['create']>(async (_p, draft) => {
      log.push('create')
      stored = { ...data(), ...draft }
      const r = receipt('create')
      publish('create', r, 'confirmed')
      return r
    }),
    update: vi.fn<Port['update']>(async (_p, _v, expected, patch) => {
      log.push('update')
      stored = { ...stored!, ...patch, version: String(BigInt(expected) + 1n) }
      const r = receipt('update')
      publish('update', r, 'confirmed')
      return r
    }),
    delete: vi.fn<Port['delete']>(async (_p, _v, expected) => {
      log.push('delete')
      stored = null
      const r = {
        command: 'project.variable.delete' as const,
        changed: true as const,
        deleted: {
          id: v,
          project_id: p,
          type: 'variable' as const,
          version: String(BigInt(expected) + 1n),
          deleted_at: at,
        },
        event_id: id(20),
        audit_id: id(21),
      }
      publish('delete', r, 'confirmed')
      return r
    }),
    checkOriginal: vi.fn<Port['checkOriginal']>(async () => {
      if (!stored) throw new Error('No original')
      const r = receipt('create')
      publish('create', r, 'confirmed')
      return { status: 'committed', receipt: r }
    }),
    finishConfirmed: vi.fn(() => {
      progress.value = null
    }),
    editRejected: vi.fn(() => {
      progress.value = null
    }),
    abandonReads: vi.fn(),
    abandonPending: vi.fn(() => {
      progress.value = null
    }),
  } satisfies Port
  const auth = {
    state,
    personalContext: personal,
    projectVariables: port,
  } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: project,
    paths: computed(() => ({ home: home.value })),
  } as unknown as ProjectWorkspace
  const owner = createProjectVariables(auth, workspace)
  owners.push(owner)
  owner.afterNavigation(route)
  await flushPromises()
  return {
    owner,
    auth,
    port,
    state,
    personal,
    context,
    home,
    project,
    log,
    progress,
    setStored(value: ProjectVariable) {
      stored = value
    },
    publish,
  }
}
function visibleText(element: Element) {
  const clone = element.cloneNode(true) as Element
  clone.querySelectorAll('[aria-hidden="true"]').forEach((e) => e.remove())
  return clone.textContent?.replace(/\s+/g, ' ').trim() ?? ''
}
function button(text: string) {
  const item = [...document.querySelectorAll('button')].find(
    (e) => e.getAttribute('aria-label') === text || visibleText(e) === text,
  )
  if (!item) throw new Error(`Missing visible button: ${text}`)
  return new DOMWrapper(item)
}
async function page(existing = false) {
  const f = await fixture(existing)
  const Host = defineComponent({
    setup() {
      return () =>
        h('main', [
          h(ProjectVariablesView),
          h(
            UiDialog,
            {
              open: f.owner.confirmation.open,
              title: f.owner.confirmation.title,
              'onUpdate:open': (value: boolean) => {
                if (!value) f.owner.finishConfirmation(false)
              },
            },
            {
              default: () => h('p', f.owner.confirmation.message),
              footer: () => [
                h(UiButton, { onClick: () => f.owner.finishConfirmation(false) }, () => '取消'),
                h(
                  UiButton,
                  { onClick: () => f.owner.finishConfirmation(true) },
                  () => f.owner.confirmation.label,
                ),
              ],
            },
          ),
        ])
    },
  })
  const wrapper = mount(Host, {
    attachTo: document.body,
    global: { provide: { [projectVariablesKey as symbol]: f.owner } },
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { ...f, wrapper }
}
describe('ordinary Project Variable page', () => {
  it('requires explicit deletion confirmation and removes the tombstone without reading the deleted target', async () => {
    const f = await page(true)
    await button('ORDINARY').trigger('click')
    await flushPromises()
    const deleteTrigger = button('删除变量').element as HTMLButtonElement
    deleteTrigger.focus()
    await button('删除变量').trigger('click')
    await flushPromises()
    expect(deleteTrigger.disabled).toBe(true)
    expect(document.body.textContent).toContain('ORDINARY（版本 1）')
    expect(document.body.textContent).toContain('历史操作记录仍保留')
    expect(f.port.delete).not.toHaveBeenCalled()
    await button('取消').trigger('click')
    await flushPromises()
    expect(f.owner.detail.variable?.id).toBe(v)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(deleteTrigger.disabled).toBe(false)
    expect(document.activeElement).toBe(deleteTrigger)
    await button('删除变量').trigger('click')
    await flushPromises()
    await button('确认删除变量').trigger('click')
    await flushPromises()
    expect(f.port.delete).toHaveBeenCalledExactlyOnceWith(p, v, '1')
    expect(f.port.get).toHaveBeenCalledTimes(1)
    expect(f.port.list).toHaveBeenCalledTimes(2)
    expect(f.wrapper.text()).toContain('已确认删除，并重新读取当前目录。')
    expect(f.wrapper.text()).toContain('项目暂无普通变量。')
    expect(f.wrapper.find('.variable-detail').exists()).toBe(false)
    expect(f.owner.progress.value).toBeNull()
    expect(document.activeElement).toBe(button('新建变量').element)
  })
  it('retains conflict input and field presence until explicit read, version adoption and a new save confirmation', async () => {
    const f = await page(true)
    f.port.update.mockImplementationOnce(async () => {
      f.publish('update', null, 'rejected')
      throw new AccountFailure('problem', {
        type: 'urn:agenteam:problem:test',
        title: 'Conflict',
        status: 409,
        detail: '',
        instance: '/',
        code: 'VERSION_CONFLICT',
        request_id: id(90),
        commit_state: 'not_committed',
      })
    })
    await button('ORDINARY').trigger('click')
    await flushPromises()
    await button('编辑变量').trigger('click')
    await flushPromises()
    await f.wrapper.findAll('textarea')[1]!.setValue('')
    await button('保存变量').trigger('click')
    await flushPromises()
    await button('确认保存变量').trigger('click')
    await flushPromises()
    expect(f.owner.editor).toMatchObject({ open: true, targetID: v, version: '1' })
    expect(f.owner.draft).toEqual({ name: 'ORDINARY', description: 'Description', value: '' })
    expect(f.port.get).toHaveBeenCalledTimes(1)
    expect(button('保存变量').attributes('disabled')).toBeDefined()
    f.setStored({
      ...data('2'),
      name: 'CURRENT',
      description: 'Current description',
      value: 'current value',
    })
    await button('重新读取当前变量').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('.variable-value').text()).toBe('current value')
    expect(f.owner.editor.version).toBe('1')
    expect(f.owner.draft).toEqual({ name: 'ORDINARY', description: 'Description', value: '' })
    expect(f.port.update).toHaveBeenCalledTimes(1)
    await button('保留输入并采用当前版本').trigger('click')
    await flushPromises()
    expect(f.owner.editor.version).toBe('2')
    expect(f.port.update).toHaveBeenCalledTimes(1)
    await button('保存变量').trigger('click')
    await flushPromises()
    expect(f.port.update).toHaveBeenCalledTimes(1)
    await button('确认保存变量').trigger('click')
    await flushPromises()
    expect(f.port.update).toHaveBeenNthCalledWith(1, p, v, '1', { value: '' })
    expect(f.port.update).toHaveBeenNthCalledWith(2, p, v, '2', { value: '' })
    expect(f.owner.detail.variable).toMatchObject({
      name: 'CURRENT',
      description: 'Current description',
      value: '',
      version: '3',
    })
  })
  it('uses real fields and confirmation, current detail and explicit empty update with focus restoration', async () => {
    const f = await page()
    await button('新建变量').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(f.wrapper.get('input').element)
    await button('取消编辑').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(button('新建变量').element)
    await button('新建变量').trigger('click')
    await flushPromises()
    await f.wrapper.get('input').setValue('ORDINARY')
    await f.wrapper.findAll('textarea')[0]!.setValue('Description')
    await f.wrapper.findAll('textarea')[1]!.setValue('complete\nplain text')
    await button('创建变量').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('确认创建普通变量？')
    expect(f.port.create).not.toHaveBeenCalled()
    await button('确认创建变量').trigger('click')
    await flushPromises()
    expect(f.port.create).toHaveBeenCalledExactlyOnceWith(p, {
      name: 'ORDINARY',
      description: 'Description',
      value: 'complete\nplain text',
    })
    expect(f.wrapper.get('.variable-value').text()).toBe('complete\nplain text')
    expect(document.activeElement).toBe(f.wrapper.get('.variable-detail h2').element)
    expect(f.wrapper.get('[aria-label="变量目录"]').text()).not.toContain('plain text')
    await button('编辑变量').trigger('click')
    await flushPromises()
    await f.wrapper.findAll('textarea')[1]!.setValue('')
    await button('保存变量').trigger('click')
    await flushPromises()
    await button('确认保存变量').trigger('click')
    await flushPromises()
    expect(f.port.update).toHaveBeenCalledExactlyOnceWith(p, v, '1', { value: '' })
    expect(f.wrapper.get('.variable-value').text()).toBe('（空值）')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(f.wrapper.get('.variable-detail h2').element)
  })
  it('hides editable plaintext during Unknown and offers only original lookup before current reread', async () => {
    const f = await page()
    f.port.create.mockImplementationOnce(async (_p, draft) => {
      f.setStored({ ...data(), ...draft })
      f.publish('create', null, 'uncertain')
      throw new AccountFailure('transport')
    })
    await button('新建变量').trigger('click')
    await flushPromises()
    await f.wrapper.get('input').setValue('ORDINARY')
    await f.wrapper.findAll('textarea')[1]!.setValue('pending plain text')
    await button('创建变量').trigger('click')
    await flushPromises()
    await button('确认创建变量').trigger('click')
    await flushPromises()
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.wrapper.text()).not.toContain('pending plain text')
    expect(button('新建变量').attributes('disabled')).toBeDefined()
    expect(button('重新读取').attributes('disabled')).toBeDefined()
    await button('查询原操作').trigger('click')
    await flushPromises()
    expect(f.port.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.port.create).toHaveBeenCalledTimes(1)
    expect(f.wrapper.get('.variable-value').text()).toBe('pending plain text')
    f.owner.afterNavigation('/other/demo/settings/variables')
    await flushPromises()
    expect(f.wrapper.text()).not.toContain('pending plain text')
    expect(f.owner.draft.value).toBe('')
  })
  it('confirms the original Variables scope before Session restoration and clears it only after accepted navigation', async () => {
    const f = await fixture()
    const home = defineComponent({ render: () => h('div', 'Project route') })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: route,
          component: home,
          meta: { authentication: true, protected: true, projectWorkspace: true },
        },
        {
          path: '/owner/demo/settings/general',
          component: home,
          meta: { authentication: true, protected: true, projectWorkspace: true },
        },
      ],
    })
    const restore = vi.fn(async () => null)
    f.auth.restore = restore
    installAuthentication(router, f.auth)
    await router.push(route)
    const stop = installProjectVariablesNavigation(router, f.owner)
    try {
      await f.owner.select(v)
      f.owner.openEdit()
      f.owner.setField('value', 'unsubmitted plain text')
      restore.mockClear()
      const first = router.push('/owner/demo/settings/general')
      await flushPromises()
      expect(f.owner.confirmation.open).toBe(true)
      expect(restore).not.toHaveBeenCalled()
      f.owner.finishConfirmation(false)
      await first
      expect(router.currentRoute.value.fullPath).toBe(route)
      expect(f.owner.draft.value).toBe('unsubmitted plain text')
      const second = router.push('/owner/demo/settings/general')
      await flushPromises()
      f.owner.finishConfirmation(true)
      await second
      expect(restore).toHaveBeenCalledTimes(1)
      expect(f.owner.detail.variable).toBeNull()
      expect(f.owner.draft.value).toBe('')
      expect(projectRoute(route)?.suffix).toBe('/settings/variables')
      expect(safeReturnTarget('/OWNER/DeMo/settings/variables')).toBe(route)
      for (const suffix of ['/child', '?value=x', '/', '%2f'])
        expect(safeReturnTarget(route + suffix)).toBe('/')
    } finally {
      stop()
    }
  })
})
