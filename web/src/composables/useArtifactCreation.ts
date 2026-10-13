import {
  computed,
  nextTick,
  inject,
  reactive,
  ref,
  shallowRef,
  watch,
  type InjectionKey,
} from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureArtifactContent,
  type ArtifactCreationLookup,
  type ArtifactCreationResult,
  type ArtifactCancellation,
} from '../api/artifact-creation'
import { sameArtifactRef, type ArtifactMetadata } from '../api/project-artifacts'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import type { ProjectArtifactsController } from './useProjectArtifacts'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

// App lifetime preserves the form through same-Session revalidation. Original
// request bodies, keys and CSRF remain private to the Session controller.
export function createArtifactCreation(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const route = ref(''),
    reader = shallowRef<ProjectArtifactsController | null>(null)
  const bound = shallowRef<Context | null>(null)
  const focusTarget = shallowRef<(() => HTMLElement | null) | null>(null)
  const state = reactive({
    open: false,
    name: '',
    description: '',
    media: 'text/plain' as 'text/plain' | 'text/markdown',
    content: '',
    message: '',
    field: '',
    writing: false,
    confirmed: false,
    cancelled: false,
    needsRead: false,
    confirming: false,
    denied: false,
  })
  let revision = 0,
    disposed = false,
    suspended = false,
    readRequested = false,
    boundPath = ''
  let receipt: ArtifactMetadata | null = null
  let trigger: HTMLElement | null = null
  let answer: ((value: boolean) => void) | null = null
  const context = computed(() => {
    const c = workspace.currentReadContext.value
    return !disposed &&
      c &&
      route.value === workspace.paths.value.home + '/artifacts' &&
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
    const value = auth.artifactCreation.progress
    return bound.value?.projectID === value?.projectID ? value : null
  })
  const pending = computed(() => ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''))
  const busy = computed(() => auth.state.busy || state.writing || !!reader.value?.reading.value)
  const blocked = computed(
    () => !sameContext(bound.value, context.value) || busy.value || state.confirming,
  )
  const canOpen = computed(
    () =>
      !!context.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !busy.value &&
      !state.open &&
      !state.confirming &&
      !pending.value &&
      !['submitting', 'uncertain'].includes(auth.knowledgeCommands.progress?.phase ?? '') &&
      !['submitting', 'uncertain'].includes(auth.knowledgeText.progress?.phase ?? '') &&
      !['submitting', 'uncertain'].includes(auth.knowledgeDelete.progress?.phase ?? ''),
  )
  const dirty = computed(
    () =>
      pending.value ||
      (state.open &&
        !state.confirmed &&
        !state.cancelled &&
        (state.name !== '' ||
          state.description !== '' ||
          state.content !== '' ||
          state.media !== 'text/plain')),
  )
  const canSave = computed(
    () =>
      !blocked.value &&
      state.open &&
      !state.denied &&
      !pending.value &&
      !state.confirmed &&
      !state.cancelled &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canLookup = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      progress.value?.phase === 'uncertain' &&
      progress.value.contextValid,
  )
  const canRetry = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !!progress.value?.canRetryOriginal &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canCancel = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !!progress.value?.canCancel &&
      ['active', 'archived'].includes(workspace.detail.project?.lifecycle ?? ''),
  )
  const live = (serial: number, c: Context) =>
    revision === serial && sameContext(c, context.value) && sameContext(c, bound.value)
  function clear() {
    ++revision
    bound.value = null
    auth.artifactCreation.abandonLocal()
    receipt = null
    trigger = null
    boundPath = ''
    suspended = false
    readRequested = false
    Object.assign(state, {
      open: false,
      name: '',
      description: '',
      media: 'text/plain',
      content: '',
      message: '',
      field: '',
      writing: false,
      confirmed: false,
      cancelled: false,
      needsRead: false,
      confirming: false,
      denied: false,
    })
    const done = answer
    answer = null
    done?.(false)
  }
  async function confirmLeave() {
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
  function openCreate() {
    if (!canOpen.value || !context.value) return
    clear()
    bound.value = context.value
    boundPath = route.value
    trigger =
      typeof document !== 'undefined' && document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    state.open = true
  }
  async function close() {
    const original = trigger,
      c = context.value,
      path = route.value
    if (!(await confirmLeave())) return false
    const serial = revision
    await nextTick()
    // UiDialog safely falls back while the original trigger is still disabled
    // in the previous DOM patch. Restore it only after that same close has
    // enabled it; a new route, context, dialog or user focus wins instead.
    const fallback = focusTarget.value?.()
    const activeDialog = [...document.querySelectorAll('[role="dialog"]')].some(
      (panel) => !panel.closest('[inert], [aria-hidden="true"]'),
    )
    if (
      original?.isConnected &&
      !original.matches(':disabled') &&
      !state.open &&
      !original.closest('[inert], [aria-hidden="true"]') &&
      !activeDialog &&
      revision === serial &&
      route.value === path &&
      sameContext(c, context.value) &&
      fallback &&
      document.activeElement === fallback
    )
      original.focus({ preventScroll: true })
    return true
  }
  function rereadCreated() {
    if (blocked.value || !receipt || !reader.value || !state.needsRead) return
    // Reserve before the synchronous read-state watchers can run again.
    readRequested = true
    readRequested = reader.value.refreshCreated(receipt)
    state.message = readRequested
      ? '创建已确认，正在读取当前产物列表与正文。'
      : '创建已确认，请在当前读取结束后重读产物。'
  }
  async function perform(action: 'create' | 'lookup' | 'retry' | 'cancel') {
    if (
      !(action === 'create'
        ? canSave.value
        : action === 'lookup'
          ? canLookup.value
          : action === 'retry'
            ? canRetry.value
            : canCancel.value) ||
      !bound.value
    )
      return
    const c = bound.value,
      serial = revision
    state.message = ''
    state.field = ''
    // Validate before allocating a private original intent. Invalid drafts stay editable.
    let input
    if (action === 'create') {
      try {
        input = captureArtifactContent({
          name: state.name,
          media_type: state.media,
          content: state.content,
          ...(state.description === '' ? {} : { description: state.description }),
        })
      } catch {
        state.field = '请检查名称、描述与正文大小；输入按原样保留。'
        return
      }
    }
    state.writing = true
    try {
      const value: ArtifactCreationResult | ArtifactCreationLookup | ArtifactCancellation =
        await (action === 'create'
          ? auth.artifactCreation.start(c.projectID, input!)
          : action === 'lookup'
            ? auth.artifactCreation.checkOriginal()
            : action === 'retry'
              ? auth.artifactCreation.retryOriginal()
              : auth.artifactCreation.cancelOriginal())
      if (!live(serial, c)) return
      if (!('state' in value) || value.state === 'committed') {
        receipt = value.artifact
        state.confirmed = true
        state.needsRead = true
        readRequested = false
        state.message = '创建已确认，等待读取当前产物。'
      } else if (value.state === 'cancelled') {
        state.cancelled = true
        state.message = '原创建已取消，清理已完成。'
      } else
        state.message =
          value.state === 'not_observed'
            ? '尚未观察到原创建。可明确重试原请求；这不表示此前一定未提交。'
            : value.state === 'pending'
              ? '原创建仍在处理中，请稍后明确查询结果。'
              : '已请求取消，清理尚未完成。请稍后明确查询结果。'
    } catch (error) {
      if (!live(serial, c)) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([401, 403, 404, 410].includes(failure.problem?.status ?? 0)) {
        clear()
        state.denied = true
        state.message = '当前项目不可访问，输入和原请求查询入口已清除。'
      } else
        state.message =
          progress.value?.phase === 'uncertain'
            ? '创建结果尚不确定，请查询原创建结果；不会自动重发或取消。'
            : '当前操作尚未开始，请在其他操作结束后重试。'
    } finally {
      if (revision === serial) state.writing = false
    }
    if (live(serial, c) && state.needsRead) rereadCreated()
  }
  const stop = watch(
    () =>
      [
        auth.state.phase,
        auth.personalContext.identity,
        auth.personalContext.phase,
        context.value,
        route.value,
        workspace.detail.phase,
        busy.value,
        reader.value?.creationRead.phase,
        reader.value?.creationRead.ref,
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
        const sameProjectRead =
          workspace.detail.project?.id === bound.value.projectID &&
          ['loading', 'checking', 'read-error'].includes(workspace.detail.phase)
        // A current Project re-read temporarily removes read authority. Retain
        // private recovery material for that same identity/path/P, never enable
        // commands from the retained Project value while its read is unconfirmed.
        if (
          route.value === boundPath &&
          auth.personalContext.phase === 'current' &&
          ((suspended && workspace.detail.phase !== 'unavailable') || sameProjectRead)
        )
          return
        clear()
        return
      }
      if (c.projectID !== bound.value.projectID) {
        clear()
        return
      }
      if (suspended || !sameContext(c, bound.value)) {
        bound.value = c
        suspended = false
      }
      if (state.confirmed && reader.value?.page.phase === 'unavailable') {
        clear()
        state.confirmed = true
        state.denied = true
        state.message = '创建已确认；当前产物不可访问，编辑内容已清除。'
        return
      }
      if (!state.needsRead || !receipt || !reader.value || busy.value) return
      if (!readRequested) {
        rereadCreated()
        return
      }
      const read = reader.value.creationRead
      if (read.ref && sameArtifactRef(read.ref, receipt.artifact_ref) && read.phase === 'ready') {
        state.needsRead = false
        state.message = '创建已确认；已读取当前产物列表与正文。'
      } else if (read.phase === 'error' || read.phase === 'idle') {
        state.message = '创建已确认；当前读取未完成，请重读产物，不要再次创建。'
      }
    },
    { flush: 'sync' },
  )
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  if (typeof window !== 'undefined') window.addEventListener('beforeunload', beforeUnload)
  return {
    state,
    visible,
    fallbackFocus: computed(() => (visible.value ? (focusTarget.value?.() ?? null) : null)),
    progress,
    pending,
    busy,
    blocked,
    canOpen,
    dirty,
    canSave,
    canLookup,
    canRetry,
    canCancel,
    openCreate,
    confirmLeave,
    confirmDiscard,
    rereadCreated,
    create: () => perform('create'),
    lookup: () => perform('lookup'),
    retry: () => perform('retry'),
    cancel: () => perform('cancel'),
    close,
    afterNavigation(path: string) {
      route.value = path
    },
    attachReader(value: ProjectArtifactsController, fallback?: () => HTMLElement | null) {
      reader.value = value
      focusTarget.value = fallback ?? null
      // Revalidation can remount the view. An unrequested read may resume once;
      // an observed read failure still needs an explicit user action.
      return () => {
        if (reader.value === value) {
          reader.value = null
          focusTarget.value = null
        }
      }
    },
    dispose() {
      if (disposed) return
      clear()
      disposed = true
      stop()
      reader.value = null
      focusTarget.value = null
      if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload)
    },
  }
}
export type ArtifactCreationEditor = ReturnType<typeof createArtifactCreation>
export const artifactCreationKey: InjectionKey<ArtifactCreationEditor> = Symbol('artifact-creation')
export function useArtifactCreation() {
  const value = inject(artifactCreationKey)
  if (!value) throw new Error('Artifact creation owner is unavailable')
  return value
}
