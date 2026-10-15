import { afterEach, describe, expect, it, vi } from 'vitest'
import { reactive, shallowRef } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { AccountFailure } from '../api/client'
import type { ArtifactMetadata } from '../api/project-artifacts'
import type { ArtifactUploadProgress } from '../composables/artifact-upload-session'
import * as sessionModule from '../composables/useSession'
import { projectWorkspaceKey, type ProjectWorkspace } from '../composables/useProjectWorkspace'
import MeetingFileUpload from '../components/meetings/MeetingFileUpload.vue'
import {
  artifactDeferred,
  artifactID,
  artifactProject,
  artifactRecord,
} from './project-artifacts.fixture'

const wrappers: VueWrapper[] = []
afterEach(async () => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  await flushPromises()
  vi.restoreAllMocks()
})
const file = () => new File(['hello'], 'report.txt', { type: 'text/plain' })
function fixture(selected: File | null = null) {
  const context = shallowRef({
    projectID: artifactProject,
    identity: { userID: artifactID(2), sessionID: artifactID(3), epoch: 1 },
    generation: 1,
    readGeneration: 1,
  })
  const progress = shallowRef<ArtifactUploadProgress | null>(null)
  const receipt: ArtifactMetadata = {
    ...artifactRecord(),
    kind: 'user_upload',
    name: 'report.txt',
    object: { ...artifactRecord().object, byte_size: '5' },
  }
  const uploading: ArtifactUploadProgress = {
    projectID: artifactProject,
    meetingID: artifactID(7),
    name: 'report.txt',
    byteSize: '5',
    phase: 'uploading',
    observation: 'none',
    receipt: null,
    contextValid: true,
    canLookup: false,
    canCancel: false,
    canComplete: false,
  }
  const uploads = {
    get progress() {
      return progress.value
    },
    get pending() {
      return (
        !!progress.value && !['confirmed', 'cancelled', 'rejected'].includes(progress.value.phase)
      )
    },
    dismiss: vi.fn(() => true),
    start: vi.fn(
      async (
        _project: string,
        _meeting: string | null,
        _file: File,
      ): Promise<ArtifactMetadata | null> => receipt,
    ),
    lookup: vi.fn(async () => ({ state: 'pending' as const, artifact: null })),
    cancel: vi.fn(async () => ({ state: 'not_observed' as const, cleanup: null })),
    complete: vi.fn(async () => ({ artifact: receipt })),
    stop: vi.fn(),
  }
  const state = reactive({ busy: false })
  const auth = {
    state,
    artifactUploads: uploads,
  } as unknown as sessionModule.SessionController
  vi.spyOn(sessionModule, 'useSession').mockReturnValue(auth)
  const wrapper = mount(MeetingFileUpload, {
    props: { meetingId: artifactID(7), disabled: false, file: selected },
    global: {
      provide: {
        [projectWorkspaceKey as symbol]: {
          currentReadContext: context,
          detail: { project: { lifecycle: 'active' } },
        } as unknown as ProjectWorkspace,
      },
    },
  })
  wrappers.push(wrapper)
  const button = (name: string) =>
    wrapper
      .findAll<HTMLButtonElement>('button')
      .find((button) => button.find('.button-label[aria-hidden="false"]').text() === name)!
  return { wrapper, context, progress, uploads, receipt, uploading, state, button }
}
describe('Meeting file upload controls', () => {
  it('keeps a selected File local until explicit upload and emits only its published stable identity', async () => {
    const original = file(),
      f = fixture(original),
      completed = artifactDeferred<ArtifactMetadata | null>()
    f.uploads.start.mockImplementationOnce(() => {
      f.progress.value = f.uploading
      return completed.promise
    })
    expect(f.uploads.start).not.toHaveBeenCalled()
    await f.button('上传并插入').trigger('click')
    expect(f.uploads.start).toHaveBeenCalledWith(artifactProject, artifactID(7), original)
    expect(f.wrapper.emitted('choose')).toBeUndefined()
    completed.resolve(f.receipt)
    await flushPromises()
    expect(f.wrapper.emitted('choose')).toEqual([
      [{ kind: 'file', id: f.receipt.artifact_ref.artifact_id, label: 'report.txt' }],
    ])
  })
  it('keeps Unknown and cancel not_observed recoverable without inserting or enabling another upload', async () => {
    const f = fixture(file())
    f.uploads.start.mockImplementationOnce(async () => {
      f.progress.value = { ...f.uploading, phase: 'uncertain', canLookup: true, canCancel: true }
      throw new AccountFailure('transport')
    })
    await f.button('上传并插入').trigger('click')
    await flushPromises()
    expect(f.wrapper.get<HTMLInputElement>('input').element.disabled).toBe(true)
    await f.button('查询原上传').trigger('click')
    await flushPromises()
    await f.button('取消上传').trigger('click')
    await flushPromises()
    expect(f.uploads.start).toHaveBeenCalledTimes(1)
    expect(f.uploads.lookup).toHaveBeenCalledTimes(1)
    expect(f.uploads.cancel).toHaveBeenCalledTimes(1)
    expect(f.wrapper.text()).toContain('原上传结果仍待确认')
    expect(f.wrapper.text()).not.toContain('上传已取消。')
    expect(f.wrapper.emitted('choose')).toBeUndefined()
  })
  it('offers stopping the actual busy transfer without reporting the upload cancelled', async () => {
    const f = fixture()
    f.progress.value = f.uploading
    f.state.busy = true
    await flushPromises()
    await f.button('停止传输').trigger('click')
    expect(f.uploads.stop).toHaveBeenCalledTimes(1)
    expect(f.wrapper.text()).toContain('正在停止传输，上传结果仍待确认。')
    expect(f.wrapper.emitted('choose')).toBeUndefined()
  })
  it('drops a late publication after the original Meeting context changes', async () => {
    const f = fixture(file()),
      completed = artifactDeferred<ArtifactMetadata | null>()
    f.uploads.start.mockReturnValueOnce(completed.promise)
    await f.button('上传并插入').trigger('click')
    f.context.value = {
      ...f.context.value,
      identity: { ...f.context.value.identity, sessionID: artifactID(9) },
    }
    completed.resolve(f.receipt)
    await flushPromises()
    expect(f.wrapper.emitted('choose')).toBeUndefined()
    expect(f.wrapper.findAll('button').some((button) => button.text().includes('上传并插入'))).toBe(
      false,
    )
  })
})
