import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { createAccountAPI } from '../api/account'
import type { Fetch } from '../api/client'
import { createProjectArtifactsAPI, type ArtifactMetadata } from '../api/project-artifacts'
import TaskExecutionArtifacts from '../components/work/TaskExecutionArtifacts.vue'
import { createSessionController, type SessionController } from '../composables/useSession'
import {
  createTaskExecutionArtifacts,
  type TaskExecutionArtifactScope,
} from '../composables/useTaskExecutionArtifacts'
import { at, id, json, p, session, t } from './task-execution-fixture'

const wrappers: VueWrapper[] = [],
  disposals: (() => void)[] = [],
  releases: (() => void)[] = []
afterEach(async () => {
  releases.splice(0).forEach((release) => release())
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  disposals.splice(0).forEach((dispose) => dispose())
  await flushPromises()
  vi.restoreAllMocks()
})
const body = '<script>literal result</script>\ncount=4, sum=20, mean=5'
function artifact(text = body): ArtifactMetadata {
  return {
    artifact_ref: { project_id: p, artifact_id: id(301), file_id: id(302) },
    kind: 'generated',
    name: 'result.txt',
    object: {
      media_type: 'text/plain',
      byte_size: String(new TextEncoder().encode(text).length),
      sha256: 'sha256:' + 'a'.repeat(64),
    },
    source_execution_id: id(100),
    source_operation_id: id(303),
    created_by_agent_id: id(7),
    created_at: at,
    version: '1',
  }
}
function content(item = artifact(), text = body, offset = '0', truncated = false) {
  return {
    artifact: item,
    projection: 'text',
    text,
    offset,
    next_offset: String(BigInt(offset) + BigInt(new TextEncoder().encode(text).length)),
    truncated,
  }
}
function fixture() {
  const scope = shallowRef<TaskExecutionArtifactScope | null>({
    projectID: p,
    taskID: t,
    executionID: id(100),
    key: `task/${t}/${id(100)}`,
  })
  let handler: Fetch = async (url) =>
    url.includes('/content?') ? json(content()) : json({ items: [artifact()] })
  const fetch = vi.fn<Fetch>((url, init) => handler(url, init)),
    api = createProjectArtifactsAPI(fetch),
    signal = new AbortController().signal
  const owner = createTaskExecutionArtifacts(scope, shallowRef(false), {
    list: (project, query) => api.list(project, query, signal),
    read: (ref, query) => api.read(ref, query, signal),
  })
  const wrapper = mount(TaskExecutionArtifacts, { props: { owner } })
  wrappers.push(wrapper)
  disposals.push(owner.dispose)
  async function click(label: string) {
    const button = wrapper.findAll('button').find((button) => {
      const copy = button.element.cloneNode(true) as Element
      copy.querySelectorAll('[aria-hidden="true"]').forEach((child) => child.remove())
      return (copy.getAttribute('aria-label') ?? copy.textContent?.trim()) === label
    })
    expect(button, label).toBeDefined()
    await button!.trigger('click')
    await flushPromises()
  }
  return {
    scope,
    owner,
    wrapper,
    fetch,
    click,
    intercept: (next: Fetch) => {
      handler = next
    },
  }
}

describe('Task execution成果读取', () => {
  it('reads only the selected execution artifacts and renders literal content with original provenance', async () => {
    const f = fixture()
    await f.click('查看执行成果')
    const request = new URL(f.fetch.mock.calls[0]![0], 'https://owned.invalid')
    expect(request.pathname).toBe(`/api/v1/projects/${p}/artifacts`)
    expect(Object.fromEntries(request.searchParams)).toEqual({ execution_id: id(100), limit: '50' })
    await f.click('result.txt')
    expect(f.wrapper.find('pre').text()).toBe(body)
    expect(f.wrapper.find('script').exists()).toBe(false)
    expect(f.wrapper.text()).toContain(id(100))
    expect(f.wrapper.text()).toContain(id(303))
    expect(f.wrapper.text()).toContain('接受或退回任务仍需明确提交审核操作')
    expect(f.fetch.mock.calls.every(([, init]) => init.method === 'GET')).toBe(true)
  })
  it('rejects another execution and never shows its contents', async () => {
    const f = fixture()
    f.intercept(async () => json({ items: [{ ...artifact(), source_execution_id: id(101) }] }))
    await f.click('查看执行成果')
    expect(f.wrapper.text()).toContain('未能读取执行成果')
    expect(f.wrapper.text()).not.toContain('result.txt')
    expect(f.fetch).toHaveBeenCalledTimes(1)
  })
  it('retires a late original read when the selected execution changes', async () => {
    const f = fixture()
    let finish!: (value: Response) => void
    const response = new Promise<Response>((resolve) => {
      finish = resolve
    })
    releases.push(() => finish(json({ items: [artifact()] })))
    f.intercept(async () => response)
    await f.click('查看执行成果')
    f.scope.value = { projectID: p, taskID: t, executionID: id(101), key: `task/${t}/${id(101)}` }
    finish(json({ items: [artifact()] }))
    await flushPromises()
    expect(f.wrapper.text()).not.toContain('result.txt')
    expect(f.wrapper.text()).toContain('查看执行成果')
  })
  it('reads bounded content pages and rejects a changed artifact before publishing the next page', async () => {
    const f = fixture(),
      text = 'x'.repeat(8200),
      item = artifact(text)
    f.intercept(async (url) => {
      if (!url.includes('/content?')) return json({ items: [item] })
      if (url.includes('offset=0&')) return json(content(item, text.slice(0, 8192), '0', true))
      return json(content({ ...item, version: '2' }, text.slice(8192), '8192'))
    })
    await f.click('查看执行成果')
    await f.click('result.txt')
    expect(f.wrapper.find('pre').text()).toHaveLength(8192)
    await f.click('下一段成果内容')
    expect(f.wrapper.find('pre').exists()).toBe(false)
    expect(f.wrapper.text()).toContain('未能读取成果内容')
    expect(f.fetch.mock.calls.at(-1)?.[0]).toContain('offset=8192&limit=8192')
  })
  it('keeps abandoned Artifact HTTP work under the existing Task Session request owner until its real tail ends', async () => {
    let finish!: (response: Response) => void
    const response = new Promise<Response>((resolve) => {
      finish = resolve
    })
    releases.push(() => finish(json({ items: [artifact()] })))
    let pending = true
    const fetch: Fetch = async (url) =>
      url === '/api/v1/session'
        ? json(session())
        : pending
          ? response
          : json({ items: [artifact()] })
    const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
    args[19] = { projectArtifacts: createProjectArtifactsAPI(fetch) }
    const auth: SessionController = createSessionController(...args)
    disposals.push(() => auth.leave())
    await auth.restore()
    const first = auth.workReview
      .executionArtifacts(p, { execution_id: id(100) })
      .catch((error) => error)
    await flushPromises()
    auth.workReview.abandonRead()
    expect(await first).toMatchObject({ kind: 'cancelled' })
    expect(auth.state.busy).toBe(true)
    await expect(
      auth.workReview.executionArtifacts(p, { execution_id: id(100) }),
    ).rejects.toMatchObject({ kind: 'busy' })
    finish(json({ items: [artifact()] }))
    await flushPromises()
    pending = false
    expect(auth.state.busy).toBe(false)
    expect(
      (await auth.workReview.executionArtifacts(p, { execution_id: id(100) })).items[0]
        ?.source_execution_id,
    ).toBe(id(100))
  })
})
