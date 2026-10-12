import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { Project } from '../api/project-owner'
import type { ProjectVariable, ProjectVariableReceipt } from '../api/project-variables'
import { createProjectVariables, type ProjectVariables } from '../composables/useProjectVariables'
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
  releases: (() => void)[] = [],
  tails: Promise<unknown>[] = []
afterEach(async () => {
  owners.splice(0).forEach((o) => o.dispose())
  releases.splice(0).forEach((f) => f())
  await Promise.allSettled(tails.splice(0))
  await flushPromises()
})
function deferred<T>(fallback: T) {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  releases.push(() => resolve(fallback))
  return { promise, resolve }
}
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
async function save(owner: ProjectVariables) {
  const pending = owner.submit()
  tails.push(pending)
  await flushPromises()
  expect(owner.confirmation.open).toBe(true)
  owner.finishConfirmation(true)
  await pending
}
describe('ordinary Project Variable current owner state', () => {
  it('keeps an unknown deletion pending through not-observed lookup, then accepts only its tombstone', async () => {
    const f = await fixture()
    await f.owner.select(v)
    f.port.delete.mockImplementationOnce(async () => {
      f.publish('delete', null, 'uncertain')
      throw new AccountFailure('transport')
    })
    const removing = f.owner.deleteVariable()
    tails.push(removing)
    await flushPromises()
    f.owner.finishConfirmation(true)
    await removing
    expect(f.owner.pending.value).toBe(true)
    expect(f.owner.page.items).toHaveLength(1)
    expect(f.owner.canDelete.value).toBe(false)
    f.port.checkOriginal.mockResolvedValueOnce({ status: 'not_observed', receipt: null })
    await f.owner.lookupOriginal()
    await f.owner.deleteVariable()
    await f.owner.refresh()
    expect(f.port.delete).toHaveBeenCalledTimes(1)
    expect(f.owner.pending.value).toBe(true)
    expect(f.port.get).toHaveBeenCalledTimes(1)
    const tombstone = {
      command: 'project.variable.delete' as const,
      changed: true as const,
      deleted: { id: v, project_id: p, type: 'variable' as const, version: '2', deleted_at: at },
      event_id: id(20),
      audit_id: id(21),
    }
    f.port.checkOriginal.mockImplementationOnce(async () => {
      f.publish('delete', tombstone, 'confirmed')
      return { status: 'committed', receipt: tombstone }
    })
    f.port.list.mockResolvedValueOnce({ items: [] })
    await f.owner.lookupOriginal()
    expect(f.owner.message.value).toBe('已确认删除，并重新读取当前目录。')
    expect(f.owner.page.items).toEqual([])
    expect(f.port.delete).toHaveBeenCalledTimes(1)
    expect(f.port.get).toHaveBeenCalledTimes(1)
    expect(f.owner.pending.value).toBe(false)
  })
  it.each([
    ['VERSION_CONFLICT', 409],
    ['NOT_FOUND', 404],
  ] as const)(
    'does not reinterpret delete %s as success or retry the command',
    async (code, status) => {
      const f = await fixture()
      await f.owner.select(v)
      f.port.delete.mockImplementationOnce(async () => {
        f.publish('delete', null, 'rejected')
        throw new AccountFailure('problem', {
          type: 'urn:agenteam:problem:test',
          title: 'Rejected',
          status,
          detail: '',
          instance: '/',
          code,
          request_id: id(90),
          commit_state: 'not_committed',
        })
      })
      const removing = f.owner.deleteVariable()
      tails.push(removing)
      await flushPromises()
      f.owner.finishConfirmation(true)
      await removing
      expect(f.port.delete).toHaveBeenCalledExactlyOnceWith(p, v, '1')
      expect(f.owner.message.value).not.toContain('已确认删除')
      expect(f.owner.canDelete.value).toBe(false)
      expect(f.port.finishConfirmed).not.toHaveBeenCalled()
      await f.owner.deleteVariable()
      expect(f.port.delete).toHaveBeenCalledTimes(1)
    },
  )
  it('does not send a confirmed delete after the selected Project scope retires', async () => {
    const f = await fixture()
    await f.owner.select(v)
    const removing = f.owner.deleteVariable()
    tails.push(removing)
    await flushPromises()
    f.owner.afterNavigation('/owner/other/settings/variables')
    f.owner.finishConfirmation(true)
    await removing
    expect(f.port.delete).not.toHaveBeenCalled()
    expect(f.owner.detail.variable).toBeNull()
  })
  it('keeps input and clear-field presence after a definite non-conflict rejection without automatic retry', async () => {
    const f = await fixture()
    await f.owner.select(v)
    f.owner.openEdit()
    f.owner.setField('description', '')
    f.owner.setField('value', 'pending input')
    f.port.update.mockImplementationOnce(async () => {
      f.publish('update', null, 'rejected')
      throw new AccountFailure('problem', {
        type: 'urn:agenteam:problem:test',
        title: 'Rejected',
        status: 400,
        detail: '',
        instance: '/',
        code: 'INVALID_INPUT',
        request_id: id(90),
        commit_state: 'not_committed',
      })
    })
    await save(f.owner)
    expect(f.owner.editor).toMatchObject({ open: true, targetID: v, version: '1' })
    expect(f.owner.draft).toEqual({ name: 'ORDINARY', description: '', value: 'pending input' })
    expect(f.port.update).toHaveBeenCalledTimes(1)
    expect(f.port.get).toHaveBeenCalledTimes(1)
    f.owner.setField('value', 'corrected input')
    await save(f.owner)
    expect(f.port.update).toHaveBeenNthCalledWith(2, p, v, '1', {
      description: '',
      value: 'corrected input',
    })
  })
  it.each([401, 403, 404])(
    'clears retained conflict plaintext on a current %s reread denial',
    async (status) => {
      const f = await fixture()
      await f.owner.select(v)
      f.owner.openEdit()
      f.owner.setField('value', 'pending input')
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
      await save(f.owner)
      expect(f.owner.draft.value).toBe('pending input')
      f.port.get.mockRejectedValueOnce(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:test',
          title: 'Denied',
          status,
          detail: '',
          instance: '/',
          code: 'FORBIDDEN',
          request_id: id(91),
          commit_state: 'not_started',
        }),
      )
      await f.owner.rereadConflict()
      f.owner.adoptCurrentVersion()
      await f.owner.submit()
      expect(f.owner.editor).toMatchObject({
        open: false,
        targetID: '',
        version: '',
        conflict: false,
      })
      expect(f.owner.draft).toEqual({ name: '', description: '', value: '' })
      expect(f.owner.detail.variable).toBeNull()
      expect(f.owner.page.items).toEqual([])
      expect(f.owner.canSave.value).toBe(false)
      expect(f.port.update).toHaveBeenCalledTimes(1)
      expect(f.port.abandonPending).toHaveBeenCalled()
    },
  )
  it('publishes only a real post-command Get/List and sends only touched clear fields', async () => {
    const f = await fixture(false)
    await f.owner.openCreate()
    f.owner.setField('name', 'ORDINARY')
    f.owner.setField('description', 'Description')
    f.owner.setField('value', 'original\ntext')
    const held = deferred(data())
    f.port.get.mockImplementationOnce(() => held.promise)
    const saving = f.owner.submit()
    tails.push(saving)
    await flushPromises()
    f.owner.finishConfirmation(true)
    await flushPromises()
    expect(f.owner.needsRead.value).toBe(true)
    expect(f.owner.detail.variable).toBeNull()
    expect(f.port.finishConfirmed).not.toHaveBeenCalled()
    held.resolve(data())
    await saving
    expect(f.log).toEqual(['list', 'create', 'list']) // held Get is the controlled original current read
    expect(f.port.get).toHaveBeenCalledWith(p, v)
    expect(f.owner.detail.variable).toEqual(data())
    expect(Object.hasOwn(f.owner.page.items[0]!, 'value')).toBe(false)
    f.owner.openEdit()
    f.owner.setField('value', '')
    await save(f.owner)
    expect(f.port.update).toHaveBeenCalledExactlyOnceWith(p, v, '1', { value: '' })
    expect(f.owner.detail.variable).toMatchObject({ value: '', version: '2' })
    expect(f.port.finishConfirmed).toHaveBeenCalledTimes(2)
  })
  it('keeps Unknown read-only until exact lookup then rereads and never resends create', async () => {
    const f = await fixture(false)
    f.port.create.mockImplementationOnce(async (_p, draft) => {
      f.setStored({ ...data(), ...draft })
      f.publish('create', null, 'uncertain')
      throw new AccountFailure('transport')
    })
    await f.owner.openCreate()
    f.owner.setField('name', 'ORDINARY')
    f.owner.setField('value', 'original\ntext')
    await save(f.owner)
    expect(f.owner.pending.value).toBe(true)
    expect(f.owner.canSave.value).toBe(false)
    expect(f.owner.detail.variable).toBeNull()
    await f.owner.submit()
    await f.owner.openCreate()
    expect(f.port.create).toHaveBeenCalledTimes(1)
    await f.owner.lookupOriginal()
    expect(f.port.checkOriginal).toHaveBeenCalledTimes(1)
    expect(f.port.create).toHaveBeenCalledTimes(1)
    expect(f.owner.detail.phase).toBe('ready')
  })
  it('clears plaintext on route and identity changes and discards an original late detail', async () => {
    const f = await fixture(),
      held = deferred(data())
    f.port.get.mockImplementationOnce(() => held.promise)
    const read = f.owner.select(v)
    tails.push(read)
    await flushPromises()
    f.owner.afterNavigation('/other/demo/settings/variables')
    held.resolve(data())
    await read
    expect(f.owner.visible.value).toBe(false)
    expect(f.owner.detail.variable).toBeNull()
    expect(f.owner.page.items).toEqual([])
    expect(f.port.abandonReads).toHaveBeenCalled()
    f.owner.afterNavigation(route)
    await flushPromises()
    await f.owner.select(v)
    f.owner.openEdit()
    expect(f.owner.draft.value).toBe(data().value)
    f.personal.identity = { userID: id(8), sessionID: id(30), epoch: 2 }
    await flushPromises()
    expect(f.owner.draft).toEqual({ name: '', description: '', value: '' })
    expect(f.owner.detail.variable).toBeNull()
    expect(f.owner.progress.value).toBeNull()
    expect(f.port.abandonPending).toHaveBeenCalled()
  })
  it('clears current plaintext on an actual Owner denial and does not turn a failed directory into empty ready', async () => {
    const f = await fixture()
    await f.owner.select(v)
    f.port.list.mockRejectedValueOnce(
      new AccountFailure('problem', {
        type: 'urn:agenteam:problem:test',
        title: 'Denied',
        status: 403,
        detail: '',
        instance: '/',
        code: 'FORBIDDEN',
        request_id: id(90),
        commit_state: 'not_started',
      }),
    )
    await f.owner.refresh()
    expect(f.owner.page.phase).toBe('error')
    expect(f.owner.page.items).toEqual([])
    expect(f.owner.detail.variable).toBeNull()
    expect(f.owner.draft.value).toBe('')
    expect(f.port.get).toHaveBeenCalledTimes(1)
    expect(f.port.abandonPending).toHaveBeenCalled()
  })
})
