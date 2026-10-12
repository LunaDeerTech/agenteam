import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import {
  createProjectSecretCommandsAPI,
  type ProjectSecretDeleteReceipt,
} from '../api/project-secret-commands'
import type { Fetch } from '../api/client'
import * as sessionModule from '../composables/useSession'
import { createProjectWorkspace, projectWorkspaceKey } from '../composables/useProjectWorkspace'
import ProjectSecretsView from '../views/projects/ProjectSecretsView.vue'
import {
  secretWriteFixture,
  secretBase,
  secretPath,
  secretID,
  secretProject,
  secretTime,
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
function tombstone(version = '2'): ProjectSecretDeleteReceipt {
  return {
    command: 'project.secret_variable.delete',
    changed: true,
    deleted: {
      id: secretID(10),
      project_id: secretProject,
      type: 'secret',
      version,
      deleted_at: secretTime,
    },
    event_id: secretID(80),
    audit_id: secretID(81),
  }
}
function button(wrapper: VueWrapper, name: string) {
  const result = wrapper
    .findAll<HTMLButtonElement>('button')
    .find(
      (b) =>
        b.element.querySelector('.button-label[aria-hidden="false"]')?.textContent?.trim() === name,
    )
  expect(result, name).toBeDefined()
  return result!
}
async function dialogAction(name: string) {
  const target = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(
    (b) => b.querySelector('.button-label[aria-hidden="false"]')?.textContent?.trim() === name,
  )
  expect(target, name).toBeDefined()
  target!.click()
  await flushPromises()
}
async function setup() {
  vi.stubGlobal('matchMedia', (media: string) => ({
    matches: false,
    media,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  const f = secretWriteFixture()
  let removed = false,
    cut = false,
    busy = false,
    notObserved = false,
    directoryError = false
  let committed: ProjectSecretDeleteReceipt | null = null
  const deletes: { key: string; body: unknown; target: string }[] = []
  const lookups: { key: string; body: unknown }[] = []
  f.intercept(async (path, init) => {
    const url = new URL(path, 'https://owned.invalid')
    if (init.method === 'DELETE') {
      deletes.push({
        key: new Headers(init.headers).get('Idempotency-Key')!,
        body: JSON.parse(String(init.body)),
        target: url.pathname,
      })
      if (busy) return secretProblem(409, 'RESOURCE_BUSY')
      if (
        (deletes.at(-1)!.body as { expected_version: string }).expected_version !==
        f.current.version
      )
        return secretProblem(409, 'VERSION_CONFLICT')
      committed = tombstone(String(BigInt(f.current.version) + 1n))
      removed = true
      if (cut) {
        cut = false
        throw new TypeError('Controlled response unavailable')
      }
      return secretJSON(committed)
    }
    if (url.pathname === secretBase + '/commands/lookup') {
      lookups.push({
        key: new Headers(init.headers).get('Idempotency-Key')!,
        body: JSON.parse(String(init.body)),
      })
      return secretJSON(
        committed && !notObserved
          ? { status: 'committed', receipt: committed }
          : { status: 'not_observed', receipt: null },
      )
    }
    if (url.pathname === secretBase) {
      if (directoryError) return secretProblem(503)
      return secretJSON({ items: removed ? [] : [f.current] })
    }
    return f.normal(path, init)
  })
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
  await button(wrapper, f.current.name).trigger('click')
  await flushPromises()
  return {
    ...f,
    wrapper,
    router,
    deletes,
    lookups,
    cut: () => {
      cut = true
    },
    busy: (value: boolean) => {
      busy = value
    },
    notObserved: (value: boolean) => {
      notObserved = value
    },
    directoryError: (value: boolean) => {
      directoryError = value
    },
  }
}

describe('Secret logical deletion through the original API and Session', () => {
  it('binds DELETE and Lookup to the same target/version/key and parses only a safe tombstone', async () => {
    const version = '9007199254740993',
      key = 'original-secret-delete',
      signal = new AbortController().signal
    let response: unknown = tombstone('9007199254740994')
    const fetch = vi.fn<Fetch>(async () => secretJSON(response)),
      api = createProjectSecretCommandsAPI(fetch)
    const receipt = await api.delete(
      secretProject,
      secretID(10),
      version,
      'S'.repeat(43),
      key,
      signal,
    )
    expect(receipt).toEqual(tombstone('9007199254740994'))
    expect(Object.isFrozen(receipt.deleted)).toBe(true)
    expect(fetch.mock.calls[0]![0]).toBe(secretBase + '/' + secretID(10))
    expect(fetch.mock.calls[0]![1].method).toBe('DELETE')
    expect(JSON.parse(String(fetch.mock.calls[0]![1].body))).toEqual({ expected_version: version })
    response = { status: 'committed', receipt }
    await api.lookup(
      {
        kind: 'delete',
        projectID: secretProject,
        targetID: secretID(10),
        expectedVersion: version,
      },
      'S'.repeat(43),
      key,
      signal,
    )
    expect(fetch.mock.calls[1]![0]).toBe(secretBase + '/commands/lookup')
    expect(new Headers(fetch.mock.calls[1]![1].headers).get('Idempotency-Key')).toBe(key)
    expect(JSON.parse(String(fetch.mock.calls[1]![1].body))).toEqual({
      command: 'project.secret_variable.delete',
      target_id: secretID(10),
      expected_version: version,
    })
  })
  it('rejects unsafe, foreign or wrong-version delete receipts without accepting material fields', async () => {
    let response: unknown
    const fetch = vi.fn<Fetch>(async () => secretJSON(response)),
      api = createProjectSecretCommandsAPI(fetch)
    for (const bad of [
      { ...tombstone(), variable: { value: 'not-a-delete-field' } },
      { ...tombstone(), changed: false },
      { ...tombstone(), event_id: null },
      { ...tombstone(), deleted: { ...tombstone().deleted, project_id: secretID(77) } },
      { ...tombstone(), deleted: { ...tombstone().deleted, id: secretID(78) } },
      { ...tombstone(), deleted: { ...tombstone().deleted, material_digest: 'forbidden' } },
      tombstone('1'),
    ]) {
      response = bad
      await expect(
        api.delete(
          secretProject,
          secretID(10),
          '1',
          'S'.repeat(43),
          'original',
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const count = fetch.mock.calls.length
    expect(() =>
      api.delete(
        secretProject,
        secretID(10),
        '0',
        'S'.repeat(43),
        'original',
        new AbortController().signal,
      ),
    ).toThrow()
    expect(fetch.mock.calls).toHaveLength(count)
  })
  it('cancels with no write and restores the original enabled delete trigger after DOM updates', async () => {
    const f = await setup(),
      trigger = button(f.wrapper, '删除 Secret')
    trigger.element.focus()
    await trigger.trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'SERVICE_TOKEN（版本 1）',
    )
    expect(trigger.element.disabled).toBe(true)
    expect(f.wrapper.find('textarea.secret-material').exists()).toBe(false)
    await dialogAction('取消')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(trigger.element.disabled).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(f.deletes).toHaveLength(0)
  })
  it('preserves referenced metadata on Busy then independently rereads the directory after deletion', async () => {
    const f = await setup()
    f.busy(true)
    await button(f.wrapper, '删除 Secret').trigger('click')
    await dialogAction('确认删除')
    expect(f.wrapper.text()).toContain('Secret 仍被引用，无法删除')
    expect(f.wrapper.get('.secret-detail h2').text()).toBe('SERVICE_TOKEN')
    expect(button(f.wrapper, '删除 Secret').element.disabled).toBe(false)
    expect(f.auth.projectSecretCommands.progress).toBeNull()
    f.busy(false)
    f.directoryError(true)
    const readsBefore = f.fetch.mock.calls.filter(
      ([path, init]) => path === secretBase + '/' + secretID(10) && init.method === 'GET',
    ).length
    await button(f.wrapper, '删除 Secret').trigger('click')
    await dialogAction('确认删除')
    expect(f.deletes).toHaveLength(2)
    expect(f.deletes[1]!.key).not.toBe(f.deletes[0]!.key)
    expect(f.deletes[1]!.body).toEqual({ expected_version: '1' })
    expect(f.wrapper.find('.secret-detail').exists()).toBe(false)
    expect(f.wrapper.text()).toContain('Secrets 目录暂不可用')
    expect(f.wrapper.text()).not.toContain('SERVICE_TOKEN')
    expect(
      f.fetch.mock.calls.filter(
        ([path, init]) => path === secretBase + '/' + secretID(10) && init.method === 'GET',
      ),
    ).toHaveLength(readsBefore)
    f.directoryError(false)
    await button(f.wrapper, '重新读取目录').trigger('click')
    await flushPromises()
    expect(f.wrapper.text()).toContain('项目 Secrets 目录暂无条目。')
  })
  it('keeps Unknown and not-observed on the original Lookup without issuing another DELETE', async () => {
    const f = await setup()
    f.cut()
    await button(f.wrapper, '删除 Secret').trigger('click')
    await dialogAction('确认删除')
    const original = f.auth.projectSecretCommands.progress!
    expect(original.phase).toBe('uncertain')
    expect(original.identity).toEqual({
      kind: 'delete',
      projectID: secretProject,
      targetID: secretID(10),
      expectedVersion: '1',
    })
    expect(
      Object.keys(original).some((key) => ['value', 'body', 'digest', 'request'].includes(key)),
    ).toBe(false)
    expect(button(f.wrapper, '删除 Secret').element.disabled).toBe(true)
    f.notObserved(true)
    await button(f.wrapper, '检查原请求').trigger('click')
    await flushPromises()
    expect(f.auth.projectSecretCommands.progress?.phase).toBe('uncertain')
    expect(f.wrapper.text()).toContain('这不证明它已回滚')
    f.notObserved(false)
    await button(f.wrapper, '检查原请求').trigger('click')
    await flushPromises()
    expect(f.deletes).toHaveLength(1)
    expect(f.lookups).toEqual(
      [1, 2].map(() => ({
        key: original.key,
        body: {
          command: 'project.secret_variable.delete',
          target_id: secretID(10),
          expected_version: '1',
        },
      })),
    )
    expect(f.wrapper.text()).toContain('项目 Secrets 目录暂无条目。')
    expect(f.wrapper.find('.secret-detail').exists()).toBe(false)
  })
  it('requires a new independent detail read and confirmation after a version conflict', async () => {
    const f = await setup()
    f.setCurrent({ ...f.current, version: '2' })
    await button(f.wrapper, '删除 Secret').trigger('click')
    await dialogAction('确认删除')
    expect(f.wrapper.text()).toContain('请重新读取详情')
    expect(button(f.wrapper, '删除 Secret').element.disabled).toBe(true)
    await button(f.wrapper, '重新读取详情').trigger('click')
    await flushPromises()
    expect(button(f.wrapper, '删除 Secret').element.disabled).toBe(false)
    await button(f.wrapper, '删除 Secret').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('版本 2')
    await dialogAction('确认删除')
    expect(f.deletes.map((v) => v.body)).toEqual([
      { expected_version: '1' },
      { expected_version: '2' },
    ])
  })
  it('retires an open delete confirmation when leaving the original project route', async () => {
    const f = await setup()
    await button(f.wrapper, '删除 Secret').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await f.router.push('/elsewhere')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(f.deletes).toHaveLength(0)
    expect(f.auth.projectSecretCommands.progress).toBeNull()
  })
})
