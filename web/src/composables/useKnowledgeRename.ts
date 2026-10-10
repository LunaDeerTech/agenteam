import { computed, reactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import { captureKnowledgeRename } from '../api/knowledge-commands'
import type { KnowledgeDocument } from '../api/knowledge-owner'
import type { KnowledgeOwner } from './useKnowledgeOwner'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch

export function useKnowledgeRename(
  owner: KnowledgeOwner,
  workspace: ProjectWorkspace = useProjectWorkspace(),
  auth: SessionController = useSession(),
) {
  const state = reactive({
    open: false,
    title: '',
    baseline: '',
    version: '',
    conflict: false,
    needsCurrent: false,
    denied: false,
    message: '',
    field: '',
    confirming: false,
    recovering: false,
  })
  let baseline: KnowledgeDocument | null = null
  let rejectedObservation: KnowledgeDocument | null = null
  let bound: {
    identity: PersonalIdentity
    project: string
    document: string
    generation: number
    readGeneration: number
  } | null = null
  let revision = 0,
    suspended = false,
    disposed = false
  let answer: ((value: boolean) => void) | null = null
  const progress = computed(() => {
    const p = auth.knowledgeCommands.progress
    return p && p.projectID === bound?.project && p.documentID === bound.document ? p : null
  })
  const live = () => {
    const c = workspace.currentReadContext.value
    return (
      !disposed &&
      !!bound &&
      !!c &&
      same(bound.identity, c.identity) &&
      c.projectID === bound.project &&
      (state.recovering ||
        (c.generation === bound.generation &&
          c.readGeneration === bound.readGeneration &&
          owner.state.selected === bound.document)) &&
      owner.visible.value
    )
  }
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const dirty = computed(() => state.title !== state.baseline || state.conflict || unsettled.value)
  const blocked = computed(() => auth.state.busy || owner.busy.value || state.confirming || !live())
  const canOpen = computed(
    () =>
      !owner.blocked.value &&
      owner.state.metadataPhase === 'ready' &&
      !!owner.state.document &&
      workspace.detail.phase === 'current' &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canSave = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !state.conflict &&
      !state.needsCurrent &&
      !unsettled.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !!baseline &&
      state.title !== state.baseline,
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
      !state.denied &&
      !unsettled.value &&
      owner.state.metadataPhase === 'ready' &&
      owner.state.document?.id === bound?.document &&
      (!state.conflict || owner.state.document !== rejectedObservation),
  )

  function clear() {
    ++revision
    baseline = null
    rejectedObservation = null
    bound = null
    Object.assign(state, {
      open: false,
      title: '',
      baseline: '',
      version: '',
      conflict: false,
      needsCurrent: false,
      denied: false,
      message: '',
      field: '',
      confirming: false,
      recovering: false,
    })
    answer?.(false)
    answer = null
  }
  function bind(value: KnowledgeDocument) {
    const c = workspace.currentReadContext.value
    if (!c || c.projectID !== value.project_id) return false
    bound = {
      identity: c.identity,
      project: c.projectID,
      document: value.id,
      generation: c.generation,
      readGeneration: c.readGeneration,
    }
    suspended = false
    state.recovering = false
    rejectedObservation = null
    baseline = value
    state.baseline = value.title
    state.version = value.content_version
    return true
  }
  function abandon() {
    if (
      bound &&
      auth.knowledgeCommands.progress?.projectID === bound.project &&
      auth.knowledgeCommands.progress.documentID === bound.document
    )
      auth.knowledgeCommands.abandon()
    clear()
  }
  // Recover only the public target/identity. Session retains the private input,
  // CSRF and key; no current metadata is required to discard or look up it.
  function recoverPending() {
    const p = auth.knowledgeCommands.progress,
      identity = auth.personalContext.identity
    if (!identity || !p?.contextValid || !['submitting', 'uncertain'].includes(p.phase))
      return false
    const c = workspace.currentReadContext.value
    bound = {
      identity,
      project: p.projectID,
      document: p.documentID,
      generation: c?.generation ?? -1,
      readGeneration: c?.readGeneration ?? -1,
    }
    baseline = null
    state.recovering = true
    state.open = true
    state.needsCurrent = true
    state.message = '原改名结果尚不确定，请查询原结果或明确放弃。'
    return true
  }
  function open() {
    if (unsettled.value) {
      state.open = true
      return
    }
    if (!canOpen.value || !owner.state.document) return
    if (!bind(owner.state.document)) return
    state.title = state.baseline
    state.open = true
    state.field = ''
    state.message = ''
    state.denied = false
    if (progress.value?.phase === 'uncertain')
      state.message = '原改名结果尚不确定，请先查询原结果。'
  }
  async function permitNavigation() {
    if (!bound || !dirty.value) {
      abandon()
      return true
    }
    if (answer) return false
    state.confirming = true
    const accepted = await new Promise<boolean>((resolve) => {
      answer = resolve
    })
    state.confirming = false
    if (accepted) abandon()
    return accepted
  }
  function confirmDiscard(value: boolean) {
    const resolve = answer
    answer = null
    resolve?.(value)
  }
  async function close() {
    if (await permitNavigation()) state.open = false
  }
  async function perform(action: 'save' | 'lookup' | 'replay') {
    if (
      !live() ||
      (action === 'save'
        ? !baseline || !canSave.value
        : action === 'lookup'
          ? !canLookup.value
          : !canReplay.value)
    )
      return
    const own = revision,
      original = {
        project: bound!.project,
        document: bound!.document,
        parent: baseline?.parent_document_id ?? null,
      }
    state.field = ''
    state.message = ''
    try {
      let pending
      if (action === 'save') {
        const input = captureKnowledgeRename({
          expected_version: state.version,
          title: state.title,
        })
        if (progress.value?.phase === 'rejected') auth.knowledgeCommands.editRejected()
        pending = auth.knowledgeCommands.startRename(original.project, original.document, input)
      } else
        pending =
          action === 'lookup'
            ? auth.knowledgeCommands.checkOriginal()
            : auth.knowledgeCommands.retryOriginal()
      const result = await pending
      if (revision !== own || !live()) return
      if ('state' in result && result.state !== 'committed') {
        state.message =
          result.state === 'in_progress'
            ? '原改名仍在处理中，请稍后查询原结果。'
            : '尚未观察到原改名结果，可按原请求重放。'
        return
      }
      state.conflict = false
      state.needsCurrent = true
      // The receipt records this command only. A fresh Get establishes the next baseline.
      const receipt = 'state' in result ? result.receipt.document : result.document
      state.baseline = state.title
      state.message = '改名已确认，正在重新读取当前文档与目录。'
      owner.refreshAfterRename(receipt, original.parent)
    } catch (error) {
      if (revision !== own || !live()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (e.problem?.status === 403 || e.problem?.status === 404) {
        state.denied = true
        state.title = ''
        state.baseline = ''
        baseline = null
        state.message = '当前文档不存在或不可访问。'
        owner.invalidateCurrent(original.project, original.document)
      } else if (e.kind === 'invalid-input')
        state.field = '标题须为 1–512 个字符，不能包含控制字符。'
      else if (progress.value?.phase === 'rejected') {
        rejectedObservation = owner.state.document
        state.conflict = true
        state.message =
          e.problem?.code === 'VERSION_CONFLICT'
            ? '文档已变化。草稿已保留，请重读后明确采用当前版本。'
            : '本次改名未提交，请确认当前文档后再编辑。'
      } else state.message = '改名结果尚不确定，请查询原结果；不会自动重发。'
    }
  }
  function adoptCurrent() {
    if (!canAdopt.value || !owner.state.document) return
    if (progress.value?.phase === 'rejected') auth.knowledgeCommands.editRejected()
    const draft = state.title
    bind(owner.state.document)
    state.title = state.conflict ? draft : state.baseline
    state.conflict = false
    state.needsCurrent = false
    state.message = ''
    state.field = ''
  }
  const stop = watch(
    () =>
      [
        auth.personalContext.phase,
        auth.personalContext.identity,
        workspace.currentReadContext.value,
        owner.state.selected,
        owner.state.document,
        owner.state.metadataPhase,
        owner.busy.value,
        auth.knowledgeCommands.progress?.phase,
      ] as const,
    () => {
      if (disposed) return
      if (!bound) {
        recoverPending()
        return
      }
      if (!same(bound.identity, auth.personalContext.identity)) {
        abandon()
        return
      }
      // A same-session check keeps Session's original unknown intent. Do not
      // confuse the temporarily missing read workspace with a new identity.
      if (auth.personalContext.phase === 'checking') {
        suspended = true
        return
      }
      if (suspended) {
        suspended = false
        clear()
        recoverPending()
        return
      }
      if (state.recovering) return
      const c = workspace.currentReadContext.value
      if (
        !c ||
        c.projectID !== bound.project ||
        c.generation !== bound.generation ||
        c.readGeneration !== bound.readGeneration ||
        owner.state.selected !== bound.document
      ) {
        abandon()
        return
      }
      if (progress.value?.phase === 'confirmed' && !owner.busy.value) {
        const current = owner.state.document
        state.message =
          owner.state.metadataPhase === 'ready' &&
          current &&
          BigInt(current.content_version) >= BigInt(progress.value.receipt!.content_version)
            ? '改名已确认；已读取当前文档。'
            : '改名已确认；当前读取未完成，请明确重读。'
      }
    },
    { flush: 'post', immediate: true },
  )
  function dispose() {
    if (disposed) return
    // App may temporarily unmount this view for same-session checking.
    if (auth.personalContext.phase === 'checking') clear()
    else abandon()
    disposed = true
    stop()
  }
  return {
    state,
    progress,
    dirty,
    blocked,
    canOpen,
    canSave,
    canLookup,
    canReplay,
    canAdopt,
    open,
    close,
    permitNavigation,
    confirmDiscard,
    save: () => perform('save'),
    lookup: () => perform('lookup'),
    replay: () => perform('replay'),
    adoptCurrent,
    reread: () => {
      if (live() && !blocked.value && !unsettled.value) {
        if (owner.state.selected !== bound!.document) owner.select(bound!.document)
        else owner.retryDocument()
      }
    },
    dispose,
  }
}
export type KnowledgeRename = ReturnType<typeof useKnowledgeRename>
