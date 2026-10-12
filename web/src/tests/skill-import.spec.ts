import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSkillOwnerAPI } from '../api/skill-owner'
import {
  createSkillImportAPI,
  captureSkillImport,
  readSkillImportFile,
  type SkillImportInput,
} from '../api/skill-import'
import { type Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useSkillOwner } from '../composables/useSkillOwner'
import { useSkillImport } from '../composables/useSkillImport'
import SkillImportForm from '../components/projects/SkillImportForm.vue'

const id = (n: number) => `019a0000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const at = '2026-10-12T12:00:00.000001Z',
  base = `/api/v1/projects/${id(10)}/skills`
const text =
  '---\nname: Existing Package\ndescription: Literal: # source\n---\n# Original body\n  keep this whitespace  \n'
const receipt = (skillID: string) => ({ skill_id: skillID, revision: '1', version: '1' })
const input = (value = text): SkillImportInput =>
  captureSkillImport({
    request: {
      skill_id: id(20),
      mode: 'create',
      source: { kind: 'text_files', files: [{ path: 'SKILL.md', utf8_text: value }] },
    },
  })
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(90),
    },
  })
const problem = (code: string, status = 409) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code,
      instance: '/api/v1',
      request_id: id(90),
      commit_state: 'not_started',
    },
    status,
  )
function file(value: string | Uint8Array, name = 'SKILL.md') {
  const bytes = typeof value === 'string' ? new TextEncoder().encode(value) : value
  const result = new File([bytes as BlobPart], name)
  // JSDOM lacks Blob.arrayBuffer. Keep the real File bytes/name/size and provide
  // only that browser primitive, without bypassing decoding or file validation.
  Object.defineProperty(result, 'arrayBuffer', {
    configurable: true,
    value: async () => bytes.slice().buffer,
  })
  return result
}
describe('Skill package import API', () => {
  it('reads the existing file byte for byte without parsing or generating metadata', async () => {
    expect(await readSkillImportFile(file(text))).toEqual({
      name: 'SKILL.md',
      size: new TextEncoder().encode(text).length,
      text,
    })
    const notAStandardPackage = 'server validates this metadata, not the browser'
    expect((await readSkillImportFile(file(notAStandardPackage))).text).toBe(notAStandardPackage)
  })
  it('rejects unsupported names, malformed UTF-8, BOM, CR, NUL and oversized input rather than repairing it', async () => {
    for (const candidate of [
      file(text, 'other.md'),
      file(''),
      file('\ufeff' + text),
      file(text.replace('\n', '\r\n')),
      file(text + '\0'),
      file(new Uint8Array([0xc3, 0x28])),
      file('x'.repeat(256 * 1024 + 1)),
    ])
      await expect(readSkillImportFile(candidate)).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(() => input('\ud800')).toThrow()
    expect(() => input('\u0001'.repeat(256 * 1024))).toThrow()
  })
  it('sends the identical complete create input and original key to Lookup with strict bound receipts', async () => {
    const fetcher = vi.fn<Fetch>(async () => json(receipt(id(20))))
    const api = createSkillImportAPI(fetcher),
      write = { csrfToken: 'S'.repeat(43), key: 'skill-original-key' }
    await api.install(id(10), input(), write)
    await api.lookup(id(10), input(), write)
    expect(fetcher.mock.calls.map(([path]) => path)).toEqual([base, base + '/commands/lookup'])
    for (const [, init] of fetcher.mock.calls)
      expect(init).toMatchObject({
        method: 'POST',
        body: JSON.stringify(input()),
        credentials: 'same-origin',
        redirect: 'error',
        headers: { 'Idempotency-Key': write.key, 'X-CSRF-Token': write.csrfToken },
      })
    for (const bad of [
      { ...receipt(id(21)) },
      { ...receipt(id(20)), revision: '2' },
      { ...receipt(id(20)), version: 1 },
      { ...receipt(id(20)), text },
      { ...receipt(id(20)), project_id: id(10) },
    ])
      await expect(
        createSkillImportAPI(async () => json(bad)).install(id(10), input(), write),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    await expect(
      createSkillImportAPI(
        async () =>
          new Response('{"skill_id":"' + id(20) + '","revision":"1","version":"1","version":"1"}', {
            headers: { 'Content-Type': 'application/json' },
          }),
      ).install(id(10), input(), write),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
})

const cleanups: (() => void)[] = []
afterEach(async () => {
  cleanups
    .splice(0)
    .reverse()
    .forEach((cleanup) => cleanup())
  await flushPromises()
  vi.restoreAllMocks()
})
async function fixture() {
  let current: SessionView = {
    user: {
      id: id(1),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: { id: id(2), issued_at: at, idle_expires_at: at, absolute_expires_at: at },
    csrf_token: 'S'.repeat(43),
  }
  const project: Project = {
    id: id(10),
    owner_user_id: id(1),
    name: 'Demo',
    normalized_name: 'demo',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: at,
    updated_at: at,
    archived_at: null,
  }
  let mode = 'ready',
    held: Promise<void> | null = null,
    stored: SkillImportInput | null = null,
    installed = false
  const fetcher = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return json(current)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${id(10)}`)
      return json(project)
    if (path === base || path === base + '/commands/lookup') {
      stored = JSON.parse(init.body as string) as SkillImportInput
      await held
      if (mode === 'unknown') throw new Error('original response unavailable')
      if (mode === 'missing') return problem('NOT_FOUND', 404)
      if (mode === 'reserved') return problem('RESOURCE_BUSY')
      if (mode === 'failed') return problem('INVALID_STATE')
      if (mode === 'invalid') return problem('INVALID_ARGUMENT', 400)
      installed = true
      return json(receipt(stored.request.skill_id))
    }
    if (path === base + '/catalog?limit=25')
      return json({
        items: installed
          ? [
              {
                id: stored!.request.skill_id,
                project_id: id(10),
                name: 'Server Name',
                normalized_name: 'server-name',
                description: 'Server metadata',
                protected: false,
                current_revision: '1',
                version: '1',
              },
            ]
          : [],
      })
    throw new Error('unexpected request')
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetcher)]
  args[12] = createProjectOwnerAPI(fetcher)
  args[19] = { skillImport: createSkillImportAPI(fetcher), skills: createSkillOwnerAPI(fetcher) }
  const auth = createSessionController(...args)
  cleanups.push(() => auth.leave())
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  cleanups.push(() => workspace.dispose())
  workspace.afterNavigation('/owner/demo/settings/skills')
  await flushPromises()
  const location = shallowRef({ projectPath: '/owner/demo', skillID: null })
  const reader = useSkillOwner(auth, workspace, location)
  cleanups.push(() => reader.dispose())
  await flushPromises()
  const importer = useSkillImport(auth, workspace, () => reader.refresh())
  cleanups.push(() => importer.dispose())
  return {
    auth,
    workspace,
    importer,
    reader,
    fetcher,
    stored: () => stored!,
    mode(value: string) {
      mode = value
    },
    replaceIdentity() {
      current = { ...current, session: { ...current.session, id: id(3) } }
    },
    hold() {
      let release!: () => void
      held = new Promise<void>((resolve) => {
        release = resolve
      })
      cleanups.push(release)
      return release
    },
  }
}
describe('Skill package import original intent', () => {
  it('selects a real file, cancels without writing, then imports and rereads the catalog rather than using receipt metadata', async () => {
    const f = await fixture()
    f.importer.show()
    const wrapper = mount(SkillImportForm, {
      props: { owner: f.importer, onClose: f.importer.close },
      global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
    })
    cleanups.push(() => wrapper.unmount())
    const choose = async () => {
      const element = wrapper.get('input[type="file"]')
      Object.defineProperty(element.element, 'files', { configurable: true, value: [file(text)] })
      await element.trigger('change')
      await flushPromises()
    }
    await choose()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Original body')
    const cancel = wrapper
      .findAll('button')
      .find((button) => button.find('.button-label[aria-hidden="false"]').text() === '取消')!
    await cancel.trigger('click')
    expect(f.importer.selected.value).toBeNull()
    expect(f.fetcher.mock.calls.filter(([, init]) => init.method === 'POST')).toHaveLength(0)
    f.importer.show()
    await flushPromises()
    await choose()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.stored().request.source.files[0].utf8_text).toBe(text)
    expect(f.reader.state.items[0]?.name).toBe('Server Name')
    expect(f.importer.progress.value?.phase).toBe('confirmed')
    expect(wrapper.get('a').attributes('href')).toBe(
      '/owner/demo/settings/skills/' + f.stored().request.skill_id,
    )
    expect(JSON.stringify(f.auth.skillImport.progress)).not.toContain('Original body')
  })
  it('retains one original input through Unknown, missing, reserved and failed Lookups without a second Install', async () => {
    const f = await fixture()
    f.mode('unknown')
    await expect(f.auth.skillImport.start(id(10), text)).rejects.toMatchObject({
      kind: 'transport',
    })
    for (const mode of ['missing', 'reserved', 'failed']) {
      f.mode(mode)
      await expect(f.auth.skillImport.lookup()).rejects.toMatchObject({ kind: 'problem' })
      expect(f.auth.skillImport.progress?.phase).toBe('uncertain')
      expect(() => f.auth.skillImport.clearSettled()).toThrow()
      await expect(f.auth.skillImport.start(id(10), 'another')).rejects.toMatchObject({
        kind: 'busy',
      })
    }
    f.workspace.afterNavigation('/owner/other/settings/skills')
    expect(f.importer.progress.value).toBeNull()
    await flushPromises()
    await f.auth.restore()
    f.mode('ready')
    await f.auth.skillImport.lookup()
    const calls = f.fetcher.mock.calls.filter(
      ([path]) => path === base || path === base + '/commands/lookup',
    )
    expect(calls.filter(([path]) => path === base)).toHaveLength(1)
    expect(calls).toHaveLength(5)
    for (const [, init] of calls) {
      expect(init.body).toBe(calls[0]![1].body)
      expect(init.headers).toEqual(calls[0]![1].headers)
    }
    expect(f.auth.skillImport.progress?.phase).toBe('confirmed')
  })
  it('only clears an explicitly rejected initial input and discards late file selection on a project change', async () => {
    const f = await fixture()
    f.mode('invalid')
    await expect(f.auth.skillImport.start(id(10), text)).rejects.toMatchObject({ kind: 'problem' })
    expect(f.auth.skillImport.progress?.phase).toBe('rejected')
    f.auth.skillImport.clearSettled()
    const selected = file(text)
    let release!: (value: ArrayBuffer) => void
    Object.defineProperty(selected, 'arrayBuffer', {
      value: () =>
        new Promise<ArrayBuffer>((resolve) => {
          release = resolve
        }),
      configurable: true,
    })
    const choosing = f.importer.select([selected])
    f.workspace.afterNavigation('/owner/other/settings/skills')
    release(new TextEncoder().encode(text).buffer)
    await choosing
    expect(f.importer.selected.value).toBeNull()
  })
  it('keeps the original request owner until actual return and prevents a previous Session publishing its receipt', async () => {
    const f = await fixture(),
      release = f.hold()
    const original = f.auth.skillImport.start(id(10), text).catch((error: unknown) => error)
    await flushPromises()
    f.auth.leave()
    expect(f.auth.skillImport.progress).toBeNull()
    expect(f.auth.state.busy).toBe(true)
    release()
    await original
    await flushPromises()
    expect(f.auth.state.busy).toBe(false)
    expect(f.auth.skillImport.progress).toBeNull()
  })
})
