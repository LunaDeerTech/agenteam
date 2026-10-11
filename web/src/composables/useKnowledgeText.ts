import { computed, inject, reactive, ref, shallowRef, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  type KnowledgeTextMedia,
  type KnowledgeTextResult,
  type KnowledgeTextLookup,
} from '../api/knowledge-text'
import type { KnowledgeDocument, KnowledgeContent } from '../api/knowledge-owner'
import type { KnowledgeOwner } from './useKnowledgeOwner'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { projectRoute } from '../router/auth'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type FullText = KnowledgeContent & {
  text: { text: string; next_byte_offset: string; truncated: boolean }
}
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

// App owns this controller, so same-Session checks can unmount the view without
// losing a draft. The Session alone owns request bodies, CSRF and original keys.
export function createKnowledgeText(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const route = ref(''),
    reader = shallowRef<KnowledgeOwner | null>(null)
  const state = reactive({
    open: false,
    mode: 'create' as 'create' | 'update',
    parentID: null as string | null,
    parentLabel: '根层',
    targetID: '',
    title: '',
    media: 'text/plain' as KnowledgeTextMedia,
    text: '',
    version: '',
    message: '',
    field: '',
    conflict: false,
    denied: false,
    reading: false,
    writing: false,
    saved: false,
    needsRead: false,
    confirming: false,
  })
  const current = shallowRef<FullText | null>(null)
  const bound = shallowRef<Context | null>(null)
  let revision = 0,
    suspended = false,
    disposed = false
  let baseline: { text: string; media: KnowledgeTextMedia } | null = null
  let committed: KnowledgeDocument | null = null,
    previousParent: string | null = null,
    readRequested = false
  let answer: ((value: boolean) => void) | null = null
  const context = computed(() => {
    const address = projectRoute(route.value),
      c = workspace.currentReadContext.value
    return !disposed &&
      address &&
      (address.suffix === '/knowledge' || address.suffix.startsWith('/knowledge/')) &&
      `/${address.username}/${address.project_name}` === workspace.paths.value.home &&
      c &&
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      sameIdentity(c.identity, auth.personalContext.identity) &&
      auth.state.user?.id === c.identity.userID &&
      auth.state.session?.id === c.identity.sessionID
      ? c
      : null
  })
  const visible = computed(() => !!context.value)
  const progress = computed(() => {
    const p = auth.knowledgeText.progress
    return p && bound.value?.projectID === p.projectID ? p : null
  })
  const pending = computed(() => ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''))
  const busy = computed(
    () => auth.state.busy || state.reading || state.writing || !!reader.value?.busy.value,
  )
  const blocked = computed(
    () =>
      !context.value ||
      !bound.value ||
      !sameContext(bound.value, context.value) ||
      busy.value ||
      state.confirming,
  )
  const canOpen = computed(
    () =>
      !!context.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !busy.value &&
      !state.confirming &&
      !['submitting', 'uncertain'].includes(auth.knowledgeCommands.progress?.phase ?? '') &&
      !pending.value &&
      !state.needsRead,
  )
  const dirty = computed(
    () =>
      pending.value ||
      state.conflict ||
      state.needsRead ||
      (state.open &&
        !state.saved &&
        (state.mode === 'create'
          ? state.title !== '' || state.text !== '' || state.media !== 'text/plain'
          : !!baseline && (state.text !== baseline.text || state.media !== baseline.media))),
  )
  const canSave = computed(
    () =>
      !blocked.value &&
      state.open &&
      !state.denied &&
      !state.conflict &&
      !state.saved &&
      !pending.value &&
      !state.needsRead &&
      workspace.detail.project?.lifecycle === 'active' &&
      (state.mode === 'create' ||
        (!!baseline && (state.text !== baseline.text || state.media !== baseline.media))),
  )
  const canLookup = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      progress.value?.phase === 'uncertain' &&
      progress.value.contextValid,
  )
  const canReplay = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !!progress.value?.canRetryOriginal &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canAdopt = computed(
    () =>
      !blocked.value &&
      !pending.value &&
      state.conflict &&
      !!current.value &&
      current.value.document.id === state.targetID &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const live = (serial: number, c: Context) =>
    revision === serial && sameContext(c, context.value) && sameContext(bound.value, c)
  function clear(abandon = true) {
    ++revision
    bound.value = null
    if (state.reading) auth.knowledge.abandon()
    if (abandon) auth.knowledgeText.abandon()
    baseline = null
    committed = null
    current.value = null
    suspended = false
    readRequested = false
    Object.assign(state, {
      open: false,
      mode: 'create',
      parentID: null,
      parentLabel: '根层',
      targetID: '',
      title: '',
      media: 'text/plain',
      text: '',
      version: '',
      message: '',
      field: '',
      conflict: false,
      denied: false,
      reading: false,
      writing: false,
      saved: false,
      needsRead: false,
      confirming: false,
    })
    const done = answer
    answer = null
    done?.(false)
  }
  function clearDenied() {
    const confirmed = state.saved
    clear()
    state.saved = confirmed
    state.denied = true
    state.message = confirmed
      ? '保存已确认；当前文档或项目不可访问，编辑内容已清除。'
      : '当前文档或项目不可访问，编辑内容已清除。'
  }
  function denied(error: AccountFailure) {
    if (![401, 403, 404, 410].includes(error.problem?.status ?? 0)) return false
    const project = bound.value?.projectID,
      target = state.targetID
    clearDenied()
    if (project && target) reader.value?.invalidateCurrent(project, target)
    return true
  }
  async function permitNavigation() {
    if (!dirty.value) {
      clear()
      return true
    }
    if (answer) return false
    state.confirming = true
    const accepted = await new Promise<boolean>((resolve) => {
      answer = resolve
    })
    state.confirming = false
    if (accepted) clear()
    return accepted
  }
  function confirmDiscard(value: boolean) {
    const done = answer
    answer = null
    done?.(value)
  }
  async function openCreate(parent: KnowledgeDocument | null = null) {
    if (!canOpen.value || !(await permitNavigation()) || !context.value) return
    if (parent && parent.project_id !== context.value.projectID) return
    bound.value = context.value
    Object.assign(state, {
      open: true,
      mode: 'create',
      parentID: parent?.id ?? null,
      parentLabel: parent?.title ?? '根层',
    })
  }
  async function openEdit(document: KnowledgeDocument) {
    if (
      !canOpen.value ||
      document.source_kind !== 'text' ||
      !(await permitNavigation()) ||
      !context.value ||
      document.project_id !== context.value.projectID
    )
      return
    bound.value = context.value
    Object.assign(state, {
      open: true,
      mode: 'update',
      targetID: document.id,
      title: document.title,
      parentID: document.parent_document_id,
      reading: true,
    })
    const serial = revision,
      c = bound.value
    try {
      const value = await auth.knowledgeText.readCurrent(c.projectID, document.id)
      if (!live(serial, c)) return
      baseline = { text: value.text.text, media: value.document.media_type as KnowledgeTextMedia }
      Object.assign(state, {
        title: value.document.title,
        text: value.text.text,
        media: baseline.media,
        version: value.document.content_version,
        parentID: value.document.parent_document_id,
      })
    } catch (error) {
      if (live(serial, c)) {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (!denied(e)) state.message = '未读取到可编辑的完整当前文本，请关闭后重新读取。'
      }
    } finally {
      if (revision === serial) state.reading = false
    }
  }
  function setParent(parent: KnowledgeDocument | null) {
    if (
      blocked.value ||
      pending.value ||
      state.mode !== 'create' ||
      state.saved ||
      (parent && parent.project_id !== bound.value?.projectID)
    )
      return
    state.parentID = parent?.id ?? null
    state.parentLabel = parent?.title ?? '根层'
  }
  function rereadSaved() {
    if (blocked.value || !committed || !reader.value || !state.needsRead) return
    // Reserve the read before synchronous observer changes can re-enter this watcher.
    readRequested = true
    readRequested = reader.value.refreshAfterText(committed, previousParent)
    if (readRequested) state.message = '保存已确认，正在读取当前文档、正文与目录。'
  }
  async function perform(action: 'save' | 'lookup' | 'replay') {
    if (
      !(action === 'save'
        ? canSave.value
        : action === 'lookup'
          ? canLookup.value
          : canReplay.value) ||
      !bound.value
    )
      return
    const c = bound.value,
      serial = revision
    state.field = ''
    state.message = ''
    try {
      let operation: Promise<KnowledgeTextResult | KnowledgeTextLookup>
      if (action === 'save') {
        if (progress.value?.phase === 'rejected') auth.knowledgeText.editRejected()
        operation =
          state.mode === 'create'
            ? auth.knowledgeText.startCreate(c.projectID, {
                parent_document_id: state.parentID,
                title: state.title,
                media_type: state.media,
                text: state.text,
              })
            : auth.knowledgeText.startReplace(c.projectID, state.targetID, {
                expected_version: state.version,
                media_type: state.media,
                text: state.text,
              })
      } else
        operation =
          action === 'lookup'
            ? auth.knowledgeText.checkOriginal()
            : auth.knowledgeText.retryOriginal()
      state.writing = true
      const result = await operation
      if (!live(serial, c)) return
      if ('state' in result && result.state !== 'committed') {
        state.message =
          result.state === 'in_progress'
            ? '原保存仍在处理中，请稍后查询。'
            : '尚未观察到原保存，可明确按原请求重放；这不表示此前未提交。'
        return
      }
      committed = 'state' in result ? result.receipt.document : result.document
      previousParent = state.parentID
      state.targetID = committed.id
      state.saved = true
      state.conflict = false
      state.needsRead = true
      readRequested = false
      state.message = '保存已确认，等待读取当前文档。'
    } catch (error) {
      if (!live(serial, c)) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (denied(e)) return
      if (e.kind === 'invalid-input')
        state.field = '请检查标题、文本格式与内容大小；输入不会自动修剪。'
      else if (progress.value?.phase === 'rejected') {
        if (e.problem?.code === 'VERSION_CONFLICT') {
          state.conflict = true
          current.value = null
          state.message = '版本已变化，输入和原版本已保留。请重读当前正文，核对后明确采用当前版本。'
        } else state.message = '本次保存未提交，输入已保留，可修改后再次明确保存。'
      } else state.message = '保存结果尚不确定，请查询原保存；不会自动重发。'
    } finally {
      if (revision === serial) state.writing = false
    }
    if (live(serial, c) && state.needsRead) rereadSaved()
  }
  async function rereadConflict() {
    if (blocked.value || !state.conflict || !bound.value || pending.value) return
    const c = bound.value,
      serial = revision
    state.reading = true
    current.value = null
    try {
      const value = await auth.knowledgeText.readCurrent(c.projectID, state.targetID)
      if (live(serial, c)) {
        current.value = value
        state.message = '已读取当前正文；输入和原版本保持，核对后可明确采用当前版本。'
      }
    } catch (error) {
      if (live(serial, c)) {
        const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
        if (!denied(e)) state.message = '当前正文读取失败，草稿仍保留，请明确重读。'
      }
    } finally {
      if (revision === serial) state.reading = false
    }
  }
  function adoptVersion() {
    if (!canAdopt.value || !current.value) return
    if (progress.value?.phase === 'rejected') auth.knowledgeText.editRejected()
    state.version = current.value.document.content_version
    state.title = current.value.document.title
    state.conflict = false
    baseline = {
      text: current.value.text.text,
      media: current.value.document.media_type as KnowledgeTextMedia,
    }
    state.message = '已采用当前版本，保留输入；再次明确保存才会覆盖正文。'
  }
  const stop = watch(
    () =>
      [
        auth.personalContext.identity,
        auth.personalContext.phase,
        context.value,
        route.value,
        reader.value?.busy.value,
        reader.value?.state.document,
        reader.value?.state.metadataPhase,
        reader.value?.state.ancestors.phase,
        reader.value?.state.content,
        workspace.detail.phase,
        auth.knowledgeText.progress?.phase,
      ] as const,
    () => {
      if (disposed || !bound.value) return
      if (!sameIdentity(bound.value.identity, auth.personalContext.identity)) {
        clear()
        return
      }
      if (auth.state.phase === 'checking' || auth.personalContext.phase === 'checking') {
        suspended = true
        return
      }
      const c = context.value
      if (!c) {
        if (
          suspended &&
          auth.personalContext.phase === 'current' &&
          workspace.detail.phase !== 'unavailable' &&
          projectRoute(route.value)?.suffix.startsWith('/knowledge')
        )
          return
        clear()
        return
      }
      if (c.projectID !== bound.value.projectID) {
        clear()
        return
      }
      if (suspended || !sameContext(bound.value, c)) {
        bound.value = c
        suspended = false
      }
      if (
        reader.value &&
        state.targetID &&
        reader.value.state.selected === state.targetID &&
        (['unavailable', 'deleted'].includes(reader.value.state.metadataPhase) ||
          ['unavailable', 'deleted'].includes(reader.value.state.ancestors.phase) ||
          ['unavailable', 'deleted'].includes(reader.value.state.content.phase))
      ) {
        clearDenied()
        return
      }
      if (state.needsRead && committed && reader.value && !busy.value) {
        if (!readRequested) {
          rereadSaved()
          return
        }
        const doc = reader.value.state.document,
          content = reader.value.state.content
        if (
          doc?.id === committed.id &&
          BigInt(doc.content_version) >= BigInt(committed.content_version) &&
          content.phase === 'ready' &&
          content.value?.document.content_version === doc.content_version &&
          'text' in content.value &&
          reader.value.state.ancestors.phase === 'ready' &&
          [null, previousParent, committed.parent_document_id, doc.parent_document_id].every(
            (parent) => ['ready', 'empty'].includes(reader.value!.level(parent).phase),
          )
        ) {
          state.needsRead = false
          state.message = '保存已确认；已读取当前文档、正文与目录。'
        } else state.message = '保存已确认；当前读取未完成，请明确重读，不要再次保存。'
      }
    },
    { flush: 'sync' },
  )
  const parents = computed(() => [
    ...new Map(
      Object.values(reader.value?.state.levels ?? {})
        .flatMap((level) => level.items)
        .map((doc) => [doc.id, doc]),
    ).values(),
  ])
  return {
    state,
    current,
    parents,
    visible,
    busy,
    blocked,
    progress,
    pending,
    dirty,
    canOpen,
    canSave,
    canLookup,
    canReplay,
    canAdopt,
    openCreate,
    openEdit,
    setParent,
    permitNavigation,
    confirmDiscard,
    rereadSaved,
    rereadConflict,
    adoptVersion,
    save: () => perform('save'),
    lookup: () => perform('lookup'),
    replay: () => perform('replay'),
    async close() {
      await permitNavigation()
    },
    confirmLeave: permitNavigation,
    afterNavigation(path: string) {
      route.value = path
    },
    attachReader(value: KnowledgeOwner) {
      reader.value = value
      if (state.needsRead) readRequested = false
      return () => {
        if (reader.value === value) reader.value = null
      }
    },
    dispose() {
      if (disposed) return
      clear()
      disposed = true
      stop()
      reader.value = null
    },
  }
}
export type KnowledgeTextEditor = ReturnType<typeof createKnowledgeText>
export const knowledgeTextKey: InjectionKey<KnowledgeTextEditor> = Symbol('knowledge-text')
export function useKnowledgeText() {
  const value = inject(knowledgeTextKey)
  if (!value) throw new Error('Knowledge text owner is unavailable')
  return value
}
