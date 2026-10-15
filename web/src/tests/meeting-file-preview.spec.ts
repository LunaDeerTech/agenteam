import { afterEach, describe, expect, it, vi } from 'vitest'
import { reactive, shallowRef } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AccountFailure } from '../api/client'
import type {
  ArtifactContent,
  ArtifactMetadata,
  ArtifactReadQuery,
  ArtifactRef,
} from '../api/project-artifacts'
import * as sessionModule from '../composables/useSession'
import { projectWorkspaceKey, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import MeetingFilePreview from '../components/meetings/MeetingFilePreview.vue'
import {
  artifactDeferred,
  artifactID,
  artifactProject,
  artifactRecord,
  artifactText,
} from './project-artifacts.fixture'

const wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  await flushPromises()
  vi.restoreAllMocks()
  document.body.replaceChildren()
})
function fixture() {
  const context = shallowRef({
    projectID: artifactProject,
    identity: { userID: artifactID(2), sessionID: artifactID(3), epoch: 1 },
    generation: 1,
    readGeneration: 1,
  })
  const metadata = artifactRecord()
  const content: ArtifactContent = {
    artifact: metadata,
    projection: 'text',
    text: artifactText,
    offset: '0',
    next_offset: '40',
    truncated: true,
    invalid_utf8: false,
  }
  const artifacts = {
    metadata: vi.fn(
      async (_project: string, _artifact: string): Promise<ArtifactMetadata> => metadata,
    ),
    read: vi.fn(
      async (_ref: ArtifactRef, _query: ArtifactReadQuery): Promise<ArtifactContent> => content,
    ),
    abandon: vi.fn(),
  }
  const state = reactive({ busy: false })
  const auth = {
    state,
    projectArtifacts: artifacts,
  } as unknown as sessionModule.SessionController
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(auth)
  const wrapper = mount(MeetingFilePreview, {
    props: {
      open: false,
      meetingId: artifactID(7),
      artifactId: metadata.artifact_ref.artifact_id,
      label: metadata.name,
    },
    attachTo: document.body,
    global: {
      stubs: { Teleport: true },
      provide: {
        [projectWorkspaceKey as symbol]: {
          currentReadContext: context,
        } as unknown as ProjectWorkspace,
      },
    },
  })
  wrappers.push(wrapper)
  const open = async () => {
    await wrapper.setProps({ open: true })
    await flushPromises()
  }
  const button = (name: string) =>
    wrapper
      .findAll<HTMLButtonElement>('button')
      .find(
        (button) =>
          button.find('.button-label[aria-hidden="false"]').exists() &&
          button.get('.button-label[aria-hidden="false"]').text() === name,
      )!
  return { wrapper, open, button, context, metadata, content, artifacts, state }
}
describe('Meeting original file reference preview', () => {
  it('resolves the stable Artifact ID before reading and pages with the original complete Ref', async () => {
    const f = fixture()
    await f.open()
    expect(f.artifacts.metadata).toHaveBeenCalledWith(
      artifactProject,
      f.metadata.artifact_ref.artifact_id,
    )
    expect(f.artifacts.read).toHaveBeenCalledWith(f.metadata.artifact_ref, {
      offset: '0',
      limit: 8192,
    })
    expect(f.wrapper.get('pre').text()).toBe(artifactText.trim())
    expect(f.wrapper.find('script').exists()).toBe(false)
    f.artifacts.read.mockResolvedValueOnce({
      ...f.content,
      text: 'Last paragraph',
      offset: '40',
      next_offset: '54',
      truncated: false,
    })
    await f.button('下一段').trigger('click')
    await flushPromises()
    expect(f.artifacts.read).toHaveBeenLastCalledWith(f.metadata.artifact_ref, {
      offset: '40',
      limit: 8192,
    })
    expect(f.artifacts.metadata).toHaveBeenCalledTimes(1)
    expect(f.button('下一段').element.disabled).toBe(true)
    await f.button('上一段').trigger('click')
    await flushPromises()
    expect(f.artifacts.read).toHaveBeenLastCalledWith(f.metadata.artifact_ref, {
      offset: '0',
      limit: 8192,
    })
    await f.button('返回会议').trigger('click')
    expect(f.wrapper.emitted('update:open')).toEqual([[false]])
  })
  it('shows metadata and the actual file_ref limitation without inventing a download or preview URL', async () => {
    const f = fixture()
    f.artifacts.read.mockResolvedValueOnce({
      ...f.content,
      projection: 'file_ref',
      text: '',
      next_offset: '0',
      truncated: false,
    })
    await f.open()
    expect(f.wrapper.text()).toContain('此类型暂不支持在线预览，可以查看文件信息。')
    expect(f.wrapper.text()).toContain(f.metadata.name)
    expect(f.wrapper.find('img, iframe, a[download], pre').exists()).toBe(false)
  })
  it('marks only a current denied read unavailable and preserves the historical reference', async () => {
    const f = fixture()
    f.artifacts.metadata.mockRejectedValueOnce(
      new AccountFailure('problem', {
        type: 'about:blank',
        title: 'Not found',
        status: 404,
        detail: '',
        instance: '',
        code: 'NOT_FOUND',
        request_id: artifactID(99),
        commit_state: 'not_started',
      }),
    )
    await f.open()
    expect(f.wrapper.text()).toContain('此文件已不可用。历史引用仍保留。')
    expect(f.artifacts.read).not.toHaveBeenCalled()
    expect(f.wrapper.props('artifactId')).toBe(f.metadata.artifact_ref.artifact_id)
    expect(f.wrapper.emitted('update:open')).toBeUndefined()
  })
  it('retires its own read on a scope change and ignores the late metadata', async () => {
    const f = fixture(),
      held = artifactDeferred<ArtifactMetadata>()
    f.artifacts.metadata.mockImplementationOnce(() => {
      f.state.busy = true
      return held.promise
    })
    await f.open()
    f.context.value = { ...f.context.value, projectID: artifactID(8) }
    await flushPromises()
    expect(f.artifacts.abandon).toHaveBeenCalledTimes(1)
    expect(f.wrapper.emitted('update:open')).toEqual([[false]])
    held.resolve(f.metadata)
    await flushPromises()
    expect(f.artifacts.read).not.toHaveBeenCalled()
    expect(f.wrapper.find('pre').exists()).toBe(false)
  })
})
