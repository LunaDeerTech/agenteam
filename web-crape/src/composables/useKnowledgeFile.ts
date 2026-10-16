import { computed, reactive, ref, watch } from 'vue'
import { AccountFailure } from '../api/client'
import {
  encodeKnowledgeFile,
  knowledgeFileMaximumBytes,
  type KnowledgeFileMedia,
  type KnowledgeFileLookup,
  type KnowledgeFileResult,
} from '../api/knowledge-file'
import type { KnowledgeDocument } from '../api/knowledge-owner'
import { newTaskID } from '../api/work-task-planning'
import type { KnowledgeOwner } from './useKnowledgeOwner'
import type { ProjectWorkspace } from './useProjectWorkspace'
import type { SessionController } from './useSession'
import type { createKnowledgeFileSession } from './knowledge-file-session'

type Files = ReturnType<typeof createKnowledgeFileSession>
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const same = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration &&
  a.identity.userID === b.identity.userID &&
  a.identity.sessionID === b.identity.sessionID &&
  a.identity.epoch === b.identity.epoch
const mediaByExtension: Record<string, KnowledgeFileMedia> = {
  txt: 'text/plain',
  md: 'text/markdown',
  markdown: 'text/markdown',
  pdf: 'application/pdf',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
}
export function useKnowledgeFile(
  owner: KnowledgeOwner,
  workspace: ProjectWorkspace,
  auth: SessionController,
  files: Files,
) {
  const state = reactive({
    open: false,
    mode: 'create' as 'create' | 'update',
    title: '',
    filename: '',
    parentID: null as string | null,
    parentLabel: '根层',
    targetID: '',
    version: '',
    media: 'application/pdf' as KnowledgeFileMedia,
    bytes: '',
    hasFile: false,
    reading: false,
    message: '',
    conflict: false,
    confirming: false,
  })
  const preview = ref('')
  let downloadURL = '',
    bound: Context | null = null,
    serial = 0,
    disposed = false
  let answer: ((value: boolean) => void) | null = null
  const context = computed(() =>
    !disposed && owner.visible.value ? workspace.currentReadContext.value : null,
  )
  const progress = computed(() =>
    files.progress?.projectID === context.value?.projectID ? files.progress : null,
  )
  const pending = computed(
    () => progress.value?.phase === 'uncertain' || progress.value?.phase === 'submitting',
  )
  const busy = computed(() => auth.state.busy || owner.busy.value || state.reading)
  const canOpen = computed(
    () =>
      !!context.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !busy.value &&
      !pending.value &&
      !state.confirming,
  )
  const canSave = computed(
    () =>
      canOpen.value &&
      same(bound, context.value) &&
      state.open &&
      state.hasFile &&
      !state.conflict &&
      (state.mode === 'update' || (state.title.length > 0 && [...state.title].length <= 512)),
  )
  const canLookup = computed(
    () => !busy.value && progress.value?.phase === 'uncertain' && progress.value.contextValid,
  )
  const canReplay = computed(
    () =>
      canLookup.value &&
      !!progress.value?.canRetryOriginal &&
      workspace.detail.project?.lifecycle === 'active',
  )
  function revoke() {
    if (preview.value) URL.revokeObjectURL(preview.value)
    if (downloadURL) URL.revokeObjectURL(downloadURL)
    preview.value = ''
    downloadURL = ''
  }
  function clearDraft() {
    ++serial
    bound = null
    Object.assign(state, {
      open: false,
      title: '',
      filename: '',
      bytes: '',
      hasFile: false,
      reading: false,
      conflict: false,
      targetID: '',
      version: '',
      parentID: null,
      parentLabel: '根层',
    })
  }
  async function permitNavigation() {
    if (busy.value && (state.open || pending.value)) {
      state.message = '请等待本次请求结束后再离开。'
      return false
    }
    if (!state.hasFile && !state.title && !pending.value) {
      clearDraft()
      revoke()
      return true
    }
    if (answer) return false
    state.confirming = true
    const accepted = await new Promise<boolean>((resolve) => {
      answer = resolve
    })
    state.confirming = false
    if (!accepted || !files.abandon()) return false
    clearDraft()
    revoke()
    return true
  }
  function confirmDiscard(value: boolean) {
    const done = answer
    answer = null
    done?.(value)
  }
  async function open(parent: KnowledgeDocument | null = null, replace = false) {
    if (
      (replace && (!parent || parent.source_kind !== 'file')) ||
      !canOpen.value ||
      !(await permitNavigation()) ||
      !context.value ||
      (parent && parent.project_id !== context.value.projectID)
    )
      return
    bound = context.value
    Object.assign(state, {
      open: true,
      mode: replace ? 'update' : 'create',
      title: replace ? (parent?.title ?? '') : '',
      targetID: replace ? parent!.id : newTaskID(),
      version: replace ? parent!.content_version : '',
      parentID: replace ? parent!.parent_document_id : (parent?.id ?? null),
      parentLabel: replace ? '' : (parent?.title ?? '根层'),
      message: '',
    })
  }
  async function choose(file: File | undefined) {
    if (!file || !same(bound, context.value) || pending.value || busy.value) return
    const media = mediaByExtension[file.name.split('.').pop()?.toLowerCase() ?? '']
    if (!media) {
      state.message = '请选择 TXT、Markdown、PDF 或 DOCX 文件。'
      return
    }
    if (file.size > knowledgeFileMaximumBytes) {
      state.message = '当前支持不超过 1 MiB 的文件，请选择较小的文件。'
      return
    }
    const current = ++serial,
      captured = bound
    state.reading = true
    state.message = ''
    try {
      const bytes = new Uint8Array(await file.arrayBuffer())
      if (current !== serial || !same(captured, context.value)) return
      if (bytes.length !== file.size || bytes.length > knowledgeFileMaximumBytes)
        throw new AccountFailure('invalid-input')
      state.bytes = encodeKnowledgeFile(bytes)
      state.filename = file.name
      state.media = media
      state.hasFile = true
      if (state.mode === 'create' && state.title === '') state.title = file.name
    } catch {
      if (current === serial) state.message = '未能读取所选文件，请重新选择。'
    } finally {
      if (current === serial) state.reading = false
    }
  }
  function failed(error: unknown) {
    const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if ([401, 403, 404, 410].includes(failure.problem?.status ?? 0)) {
      const current = context.value,
        target = owner.state.document?.id ?? state.targetID
      if (current && target) owner.invalidateCurrent(current.projectID, target)
      clearDraft()
      revoke()
      files.abandon()
      state.message = '当前文档或项目不可访问。'
      return
    }
    state.conflict = failure.problem?.code === 'VERSION_CONFLICT'
    state.message = pending.value
      ? '上传结果尚未确认，请查询原上传结果。'
      : state.conflict
        ? '文档已变化。请重新读取当前文档后，关闭此窗口并重新选择文件替换。'
        : failure.problem?.status === 413
          ? '文件超过当前 1 MiB 上限，未保存。'
          : '本次文件操作未完成，请检查输入后重试。'
  }
  function accept(result: KnowledgeFileResult | KnowledgeFileLookup, captured: Context) {
    if (!same(captured, context.value)) return
    if ('state' in result && result.state !== 'committed') {
      state.message =
        result.state === 'not_observed'
          ? '尚未观察到原上传，可明确重放原请求。'
          : '原上传仍在处理中，请稍后再次查询。'
      return
    }
    const document = 'state' in result ? result.receipt.document : result.document
    const previousParent = state.parentID
    clearDraft()
    state.message = '文件已保存，后台正在更新索引。'
    owner.refreshAfterText(document, previousParent)
  }
  async function save() {
    if (!canSave.value || !bound) return
    const captured = bound
    try {
      const result = await files.start(
        captured.projectID,
        state.mode === 'create'
          ? {
              command: 'create',
              document_id: state.targetID,
              request: {
                parent_document_id: state.parentID,
                title: state.title,
                media_type: state.media,
                bytes_base64: state.bytes,
              },
            }
          : {
              command: 'update',
              document_id: state.targetID,
              request: {
                expected_version: state.version,
                media_type: state.media,
                bytes_base64: state.bytes,
              },
            },
      )
      accept(result, captured)
    } catch (error) {
      if (same(captured, context.value)) failed(error)
    }
  }
  async function recover(replay = false) {
    const captured = context.value
    if (
      !captured ||
      busy.value ||
      !progress.value?.contextValid ||
      (replay && workspace.detail.project?.lifecycle !== 'active')
    )
      return
    try {
      accept(await (replay ? files.replay() : files.lookup()), captured)
    } catch (error) {
      if (same(captured, context.value)) failed(error)
    }
  }
  async function source(showPreview = false) {
    const captured = context.value,
      doc = owner.state.document
    if (!captured || !doc || busy.value) return
    const current = ++serial
    state.message = ''
    try {
      const result = await files.source(captured.projectID, doc.id, doc.content_version)
      if (
        current !== serial ||
        !same(captured, context.value) ||
        owner.state.document?.id !== doc.id ||
        owner.state.document.content_version !== doc.content_version
      )
        return
      const url = URL.createObjectURL(
        new Blob([new Uint8Array(result.bytes)], { type: result.document.media_type }),
      )
      if (showPreview) {
        if (result.document.media_type !== 'application/pdf') {
          URL.revokeObjectURL(url)
          return
        }
        if (preview.value) URL.revokeObjectURL(preview.value)
        preview.value = url
      } else {
        if (downloadURL) URL.revokeObjectURL(downloadURL)
        downloadURL = url
        const link = globalThis.document.createElement('a')
        link.href = url
        const extension =
          Object.entries(mediaByExtension).find(
            ([, media]) => media === result.document.media_type,
          )?.[0] ?? 'bin'
        const title = result.document.title.replace(/[\\/\u0000-\u001f]/g, '_')
        link.download = title.toLowerCase().endsWith('.' + extension)
          ? title
          : title + '.' + extension
        globalThis.document.body.append(link)
        link.click()
        link.remove()
      }
    } catch (error) {
      if (current === serial && same(captured, context.value)) failed(error)
    }
  }
  const stop = watch(
    () => [context.value, owner.state.document?.id, owner.state.document?.content_version] as const,
    ([next]) => {
      ++serial
      revoke()
      if (bound && !same(bound, next)) {
        clearDraft()
        confirmDiscard(false)
      }
    },
  )
  return {
    state,
    preview,
    progress,
    pending,
    busy,
    canOpen,
    canSave,
    canLookup,
    canReplay,
    open,
    choose,
    save,
    recover,
    source,
    permitNavigation,
    confirmDiscard,
    close: permitNavigation,
    dispose() {
      disposed = true
      stop()
      ++serial
      revoke()
      files.abandonRead()
      confirmDiscard(false)
      clearDraft()
    },
  }
}
export type KnowledgeFileEditor = ReturnType<typeof useKnowledgeFile>
