import { computed, reactive, shallowRef, watch } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureKnowledgeID,
  compareKnowledgeDocuments,
  type KnowledgeDocument,
} from '../api/knowledge-owner'
import { captureKnowledgeMove } from '../api/knowledge-move'
import type { KnowledgeOwner } from './useKnowledgeOwner'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch

export function useKnowledgeMove(
  owner: KnowledgeOwner,
  workspace: ProjectWorkspace = useProjectWorkspace(),
  auth: SessionController = useSession(),
) {
  const state = reactive({
    open: false,
    title: '',
    expectedParent: null as string | null,
    targetParent: null as string | null,
    targetLabel: '',
    targetReady: false,
    conflict: false,
    needsCurrent: false,
    denied: false,
    message: '',
    field: '',
    confirming: false,
    recovering: false,
    parent: null as string | null,
    path: [] as readonly KnowledgeDocument[],
    items: [] as readonly KnowledgeDocument[],
    nextCursor: undefined as string | undefined,
    pickerPhase: 'idle' as 'idle' | 'loading' | 'ready' | 'empty' | 'error',
    pickerMessage: '',
  })
  type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
  let bound: (Context & { document: string }) | null = null
  let baseline: KnowledgeDocument | null = null,
    rejectedObservation: KnowledgeDocument | null = null
  let revision = 0,
    readRevision = 0,
    suspended = false,
    disposed = false
  const activeRead = shallowRef<number | null>(null)
  const cursors = new Set<string>()
  let currentReadRequested = false
  let answer: ((value: boolean) => void) | null = null
  const progress = computed(() => {
    const p = auth.knowledgeMove.progress
    return p && p.projectID === bound?.projectID && p.documentID === bound.document ? p : null
  })
  const live = () => {
    const c = workspace.currentReadContext.value
    return (
      !disposed &&
      !!bound &&
      !!c &&
      owner.visible.value &&
      sameIdentity(bound.identity, c.identity) &&
      c.projectID === bound.projectID &&
      (state.recovering ||
        (c.generation === bound.generation &&
          c.readGeneration === bound.readGeneration &&
          owner.state.selected === bound.document))
    )
  }
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const dirty = computed(
    () =>
      (!state.needsCurrent && state.targetParent !== state.expectedParent) ||
      state.conflict ||
      unsettled.value,
  )
  const blocked = computed(
    () =>
      !live() ||
      auth.state.busy ||
      owner.busy.value ||
      activeRead.value !== null ||
      state.confirming,
  )
  const canOpen = computed(
    () =>
      !owner.blocked.value &&
      owner.state.metadataPhase === 'ready' &&
      !!owner.state.document &&
      workspace.detail.phase === 'current' &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canChoose = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !unsettled.value &&
      !state.needsCurrent &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canSave = computed(
    () =>
      canChoose.value &&
      !!baseline &&
      state.targetReady &&
      !state.conflict &&
      state.targetParent !== state.expectedParent,
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
  function retirePicker() {
    ++readRevision
    if (activeRead.value !== null) auth.knowledge.abandon()
    activeRead.value = null
  }
  function clear() {
    ++revision
    retirePicker()
    currentReadRequested = false
    bound = null
    baseline = null
    rejectedObservation = null
    cursors.clear()
    Object.assign(state, {
      open: false,
      title: '',
      expectedParent: null,
      targetParent: null,
      targetLabel: '',
      targetReady: false,
      conflict: false,
      needsCurrent: false,
      denied: false,
      message: '',
      field: '',
      confirming: false,
      recovering: false,
      parent: null,
      path: [],
      items: [],
      nextCursor: undefined,
      pickerPhase: 'idle',
      pickerMessage: '',
    })
    answer?.(false)
    answer = null
  }
  function denyCurrent(invalidate = true) {
    retirePicker()
    baseline = null
    rejectedObservation = null
    currentReadRequested = false
    cursors.clear()
    Object.assign(state, {
      denied: true,
      title: '',
      items: [],
      path: [],
      parent: null,
      expectedParent: null,
      targetParent: null,
      targetLabel: '',
      targetReady: false,
      nextCursor: undefined,
      pickerPhase: 'idle',
      pickerMessage: '',
      field: '',
      message: '当前文档或目录不存在或不可访问。',
    })
    if (invalidate && bound) owner.invalidateCurrent(bound.projectID, bound.document)
    // Keep the Session original intent, if uncertain. A rejected read is not
    // evidence that an earlier command was not committed.
  }
  function bind(value: KnowledgeDocument) {
    const c = workspace.currentReadContext.value
    if (!c || c.projectID !== value.project_id) return false
    bound = { ...c, document: value.id }
    baseline = value
    rejectedObservation = null
    suspended = false
    state.recovering = false
    state.expectedParent = value.parent_document_id
    state.title = value.title
    return true
  }
  function abandon() {
    if (
      bound &&
      auth.knowledgeMove.progress?.projectID === bound.projectID &&
      auth.knowledgeMove.progress.documentID === bound.document
    )
      auth.knowledgeMove.abandon()
    clear()
  }
  function recoverPending() {
    const p = auth.knowledgeMove.progress,
      identity = auth.personalContext.identity
    const c = workspace.currentReadContext.value
    if (
      !identity ||
      !p?.contextValid ||
      !['submitting', 'uncertain'].includes(p.phase) ||
      !c ||
      c.projectID !== p.projectID
    )
      return false
    bound = { ...c, identity, document: p.documentID }
    baseline = null
    state.open = true
    state.recovering = true
    state.needsCurrent = true
    state.message = '原移动结果尚不确定，请查询原结果或明确放弃。'
    return true
  }
  function open() {
    if (unsettled.value) {
      state.open = true
      return
    }
    if (!canOpen.value || !owner.state.document) return
    clear()
    if (!bind(owner.state.document)) return
    state.targetParent = state.expectedParent
    state.targetReady = state.expectedParent === null
    state.targetLabel =
      state.expectedParent === null
        ? '根层'
        : (owner.state.ancestors.items.at(-1)?.title ?? '当前父文档')
    state.open = true
    void browse(null)
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

  // Candidate reads use the same Session owner, with their own publication
  // generation. They never select a document in the content reader.
  async function inspectTarget(target: string, current: () => boolean) {
    if (target === bound!.document) throw new AccountFailure('invalid-input')
    const head = await auth.knowledge.get(bound!.projectID, target)
    if (!current()) throw new AccountFailure('cancelled')
    if (!('active' in head)) throw new AccountFailure('invalid-input')
    const ancestors = await auth.knowledge.ancestors(bound!.projectID, target)
    if (!current()) throw new AccountFailure('cancelled')
    if (
      ancestors.some((item) => item.id === bound!.document) ||
      (ancestors.at(-1)?.id ?? null) !== head.active.parent_document_id
    )
      throw new AccountFailure('invalid-input')
    return { document: head.active, path: [...ancestors, head.active] }
  }
  async function readPicker(action: 'browse' | 'choose', value: string | null, more = false) {
    if (!canChoose.value || !bound || (more && (!state.nextCursor || value !== state.parent)))
      return
    let target: string | null
    try {
      target = value === null ? null : captureKnowledgeID(value)
    } catch {
      return
    }
    const serial = ++readRevision,
      own = revision,
      project = bound.projectID
    const current = () =>
      activeRead.value === serial && readRevision === serial && revision === own && live()
    activeRead.value = serial
    state.field = ''
    state.pickerMessage = ''
    state.pickerPhase = 'loading'
    try {
      const inspected = target === null ? null : await inspectTarget(target, current)
      if (!current()) return
      if (action === 'choose') {
        state.targetParent = target
        state.targetLabel = inspected?.document.title ?? '根层'
        state.targetReady = true
        state.pickerPhase = state.items.length ? 'ready' : 'empty'
        return
      }
      const cursor = more ? state.nextCursor : undefined
      const page = await auth.knowledge.children(project, target, {
        limit: 50,
        ...(cursor ? { cursor } : {}),
      })
      if (!current()) return
      if (
        page.next_cursor &&
        (page.next_cursor === cursor || (more && cursors.has(page.next_cursor)))
      )
        throw new AccountFailure('invalid-response')
      if (!more) cursors.clear()
      if (cursor) cursors.add(cursor)
      const items = new Map((more ? state.items : []).map((item) => [item.id, item]))
      for (const item of page.items) items.set(item.id, item)
      state.items = [...items.values()].sort(compareKnowledgeDocuments)
      state.parent = target
      state.path = inspected?.path ?? []
      state.nextCursor = page.next_cursor
      state.pickerPhase = state.items.length ? 'ready' : 'empty'
    } catch (error) {
      if (!current()) return
      state.pickerPhase = 'error'
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([401, 403, 404, 410].includes(e.problem?.status ?? 0)) {
        denyCurrent()
        return
      }
      state.pickerMessage =
        e.kind === 'invalid-input'
          ? '不能选择自己或后代；文档位置也可能已变化，请重新选择。'
          : '未取得完整的当前目录或路径，请重新读取。'
      // A failed candidate is never an approved destination.
      if (action === 'choose') state.targetReady = false
    } finally {
      if (activeRead.value === serial) activeRead.value = null
    }
  }
  const browse = (target: string | null, more = false) => readPicker('browse', target, more)
  const choose = (target: string | null) => readPicker('choose', target)
  async function perform(action: 'save' | 'lookup' | 'replay') {
    if (
      !live() ||
      (action === 'save'
        ? !canSave.value
        : action === 'lookup'
          ? !canLookup.value
          : !canReplay.value)
    )
      return
    const own = revision,
      original = {
        project: bound!.projectID,
        document: bound!.document,
        parent: baseline?.parent_document_id ?? null,
      }
    state.field = ''
    state.message = ''
    try {
      let pending
      if (action === 'save') {
        const input = captureKnowledgeMove({
          expected_parent_id: state.expectedParent,
          target_parent_id: state.targetParent,
        })
        if (progress.value?.phase === 'rejected') auth.knowledgeMove.editRejected()
        pending = auth.knowledgeMove.startMove(original.project, original.document, input)
      } else
        pending =
          action === 'lookup'
            ? auth.knowledgeMove.checkOriginal()
            : auth.knowledgeMove.retryOriginal()
      const result = await pending
      if (revision !== own || !live()) return
      if ('state' in result && result.state !== 'committed') {
        state.message =
          result.state === 'in_progress'
            ? '原移动仍在处理中，请稍后查询。'
            : '尚未观察到原移动结果，可按原请求重放。'
        return
      }
      const receipt = 'state' in result ? result.receipt.document : result.document
      state.conflict = false
      state.needsCurrent = true
      state.message = '移动已确认，正在重新读取当前位置与目录。'
      // Recovery may have lost the old picker cache: invalidate all observed
      // branches, then fetch both receipt/current positions through the reader.
      currentReadRequested = owner.refreshAfterMove(receipt, original.parent)
      if (!currentReadRequested) state.message = '移动已确认；当前读取未完成，请明确重读。'
    } catch (error) {
      if (revision !== own || !live()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([403, 404, 410].includes(e.problem?.status ?? 0)) {
        denyCurrent()
      } else if (progress.value?.phase === 'rejected') {
        rejectedObservation = owner.state.document
        state.conflict = true
        state.message = '本次移动未提交。选择已保留，请重读并明确采用当前位置。'
      } else if (e.kind === 'invalid-input') state.field = '请重新选择有效的父文档。'
      else state.message = '移动结果尚不确定，请查询原结果；不会自动重发。'
    }
  }
  function adoptCurrent() {
    if (!canAdopt.value || !owner.state.document) return
    if (progress.value?.phase === 'rejected') auth.knowledgeMove.editRejected()
    const target = state.targetParent,
      label = state.targetLabel,
      preserve = state.conflict
    bind(owner.state.document)
    state.targetParent = preserve ? target : state.expectedParent
    state.targetLabel = preserve
      ? label
      : state.expectedParent === null
        ? '根层'
        : (owner.state.ancestors.items.at(-1)?.title ?? '当前父文档')
    state.targetReady = state.targetParent === null
    state.conflict = false
    state.needsCurrent = false
    state.message = ''
    state.field = ''
    if (preserve && target !== null) void choose(target)
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
        auth.knowledgeMove.progress?.phase,
      ] as const,
    () => {
      if (disposed) return
      if (!bound) {
        recoverPending()
        return
      }
      if (!sameIdentity(bound.identity, auth.personalContext.identity)) {
        abandon()
        return
      }
      if (auth.personalContext.phase === 'checking') {
        suspended = true
        retirePicker()
        return
      }
      if (suspended) {
        suspended = false
        clear()
        recoverPending()
        return
      }
      if (
        owner.state.selected === bound.document &&
        ['unavailable', 'deleted'].includes(owner.state.metadataPhase)
      ) {
        denyCurrent(false)
        return
      }
      const c = workspace.currentReadContext.value
      if (
        (c && c.projectID !== bound.projectID) ||
        (workspace.detail.project && workspace.detail.project.id !== bound.projectID)
      ) {
        abandon()
        return
      }
      if (
        unsettled.value &&
        workspace.detail.project?.id === bound.projectID &&
        (!c ||
          (c.projectID === bound.projectID &&
            (c.generation !== bound.generation ||
              c.readGeneration !== bound.readGeneration ||
              owner.state.selected !== bound.document)))
      ) {
        // A current Project reread (including archival) retires observations,
        // never the original unresolved command. Lookup remains available.
        state.recovering = true
        retirePicker()
        return
      }
      if (
        !state.recovering &&
        (!c ||
          c.projectID !== bound.projectID ||
          c.generation !== bound.generation ||
          c.readGeneration !== bound.readGeneration ||
          owner.state.selected !== bound.document)
      ) {
        abandon()
        return
      }
      if (progress.value?.phase === 'confirmed' && currentReadRequested && !owner.busy.value) {
        state.message =
          owner.state.metadataPhase === 'ready' &&
          owner.state.document?.id === bound.document &&
          owner.state.ancestors.phase === 'ready'
            ? '移动已确认；已读取当前文档与位置。'
            : '移动已确认；当前读取未完成，请明确重读。'
      }
    },
    { flush: 'post', immediate: true },
  )
  return {
    state,
    progress,
    dirty,
    blocked,
    canOpen,
    canChoose,
    canSave,
    canLookup,
    canReplay,
    canAdopt,
    open,
    close,
    permitNavigation,
    confirmDiscard,
    browse,
    choose,
    adoptCurrent,
    save: () => perform('save'),
    lookup: () => perform('lookup'),
    replay: () => perform('replay'),
    reread() {
      if (live() && !blocked.value && !unsettled.value) {
        if (owner.state.selected !== bound!.document) owner.select(bound!.document)
        else owner.retryDocument()
        currentReadRequested = true
      }
    },
    dispose() {
      if (disposed) return
      if (auth.personalContext.phase === 'checking') clear()
      else abandon()
      disposed = true
      stop()
    },
  }
}
export type KnowledgeMove = ReturnType<typeof useKnowledgeMove>
