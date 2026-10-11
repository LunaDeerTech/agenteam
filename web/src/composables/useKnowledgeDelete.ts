import { computed, reactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import type { KnowledgeDeletePreview } from '../api/knowledge-delete'
import type { KnowledgeDocument } from '../api/knowledge-owner'
import type { KnowledgeOwner } from './useKnowledgeOwner'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch

export function useKnowledgeDelete(
  owner: KnowledgeOwner,
  workspace: ProjectWorkspace = useProjectWorkspace(),
  auth: SessionController = useSession(),
) {
  const state = reactive({
    open: false,
    loading: false,
    accepted: false,
    denied: false,
    confirming: false,
    recovering: false,
    title: '',
    message: '',
    nodes: [] as readonly KnowledgeDocument[],
    expiresAt: '',
  })
  // This capability never enters reactive/public state, DOM or persistence.
  let preview: KnowledgeDeletePreview | null = null
  let bound: {
    identity: PersonalIdentity
    project: string
    root: string
    parent: string | null
    generation: number
    readGeneration: number
  } | null = null
  let revision = 0,
    disposed = false,
    suspended = false
  let answer: ((value: boolean) => void) | null = null
  const progress = computed(() => {
    const p = auth.knowledgeDelete.progress
    return p?.projectID === bound?.project && p?.documentID === bound?.root ? p : null
  })
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const live = () => {
    const c = workspace.currentReadContext.value
    return (
      !disposed &&
      !!bound &&
      !!c &&
      owner.visible.value &&
      same(c.identity, bound.identity) &&
      c.projectID === bound.project &&
      (state.recovering ||
        (c.generation === bound.generation &&
          c.readGeneration === bound.readGeneration &&
          owner.state.selected === bound.root))
    )
  }
  const blocked = computed(
    () => !live() || auth.state.busy || owner.busy.value || state.loading || state.confirming,
  )
  const canOpen = computed(
    () =>
      !owner.blocked.value &&
      !!owner.state.document &&
      owner.state.metadataPhase === 'ready' &&
      workspace.detail.phase === 'current' &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canPreview = computed(
    () =>
      !blocked.value &&
      !state.denied &&
      !unsettled.value &&
      progress.value?.phase !== 'confirmed' &&
      workspace.detail.project?.lifecycle === 'active',
  )
  const canDelete = computed(() => canPreview.value && state.nodes.length > 0 && state.accepted)
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
  function clearPreview() {
    preview = null
    state.nodes = Object.freeze([])
    state.expiresAt = ''
    state.accepted = false
  }
  function clear() {
    ++revision
    auth.knowledgeDelete.cancelPreview()
    clearPreview()
    bound = null
    Object.assign(state, {
      open: false,
      loading: false,
      denied: false,
      confirming: false,
      recovering: false,
      title: '',
      message: '',
    })
    answer?.(false)
    answer = null
  }
  function abandon() {
    if (
      bound &&
      auth.knowledgeDelete.progress?.projectID === bound.project &&
      auth.knowledgeDelete.progress.documentID === bound.root
    )
      auth.knowledgeDelete.abandon()
    clear()
  }
  function recover() {
    const p = auth.knowledgeDelete.progress,
      identity = auth.personalContext.identity
    if (!identity || !p?.contextValid || !['submitting', 'uncertain'].includes(p.phase)) return
    const c = workspace.currentReadContext.value
    bound = {
      identity,
      project: p.projectID,
      root: p.documentID,
      parent: null,
      generation: c?.generation ?? -1,
      readGeneration: c?.readGeneration ?? -1,
    }
    state.recovering = true
    state.open = true
    state.message = '原删除结果尚不确定，请查询原结果；不会自动重发。'
  }
  function deny() {
    if (!bound || state.denied) return
    clearPreview()
    state.denied = true
    state.title = ''
    state.message = '当前文档不存在或不可访问。'
    owner.invalidateCurrent(bound.project, bound.root)
  }
  async function loadPreview() {
    if (!canPreview.value || !bound) return
    if (progress.value?.phase === 'rejected') auth.knowledgeDelete.editRejected()
    const own = ++revision,
      target = bound
    clearPreview()
    state.loading = true
    state.message = ''
    try {
      const result = await auth.knowledgeDelete.preview(target.project, target.root)
      if (revision !== own || !live()) return
      preview = result
      state.nodes = result.nodes
      state.title = result.nodes.find((node) => node.id === target.root)!.title
      state.expiresAt = result.expires_at
      state.message = '请核对完整删除范围，再明确确认。'
    } catch (error) {
      if (revision !== own || !live()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (e.problem?.status === 403 || e.problem?.status === 404) deny()
      else state.message = '删除预览未完成，请重新读取；未发送删除。'
    } finally {
      if (revision === own) state.loading = false
    }
  }
  async function open() {
    if (unsettled.value) {
      state.open = true
      return
    }
    if (!canOpen.value || !owner.state.document) return
    const c = workspace.currentReadContext.value!,
      document = owner.state.document
    abandon()
    bound = {
      identity: c.identity,
      project: c.projectID,
      root: document.id,
      parent: document.parent_document_id,
      generation: c.generation,
      readGeneration: c.readGeneration,
    }
    state.title = document.title
    state.open = true
    await loadPreview()
  }
  async function perform(action: 'delete' | 'lookup' | 'replay') {
    if (
      !bound ||
      !live() ||
      (action === 'delete'
        ? !canDelete.value
        : action === 'lookup'
          ? !canLookup.value
          : !canReplay.value)
    )
      return
    if (action === 'delete' && (!preview || Date.parse(preview.expires_at) <= Date.now())) {
      clearPreview()
      state.message = '预览已过期，请重新预览并确认删除范围。'
      return
    }
    const own = revision,
      target = bound
    state.message = ''
    try {
      const pending =
        action === 'delete'
          ? auth.knowledgeDelete.startDelete(
              target.project,
              target.root,
              { confirmation_token: preview!.confirmation_token },
              preview!.nodes.map((node) => node.id),
            )
          : action === 'lookup'
            ? auth.knowledgeDelete.checkOriginal()
            : auth.knowledgeDelete.retryOriginal()
      // Session now owns the full original capability. Never create a new one
      // while its result remains uncertain.
      clearPreview()
      const result = await pending
      if (revision !== own || !live()) return
      if ('state' in result && result.state !== 'committed') {
        state.message =
          result.state === 'in_progress'
            ? '原删除仍在处理中，请稍后查询原结果。'
            : '尚未观察到原删除结果，可显式按原请求重放。'
        return
      }
      const receipt = 'state' in result ? result.receipt : result
      state.message = receipt.cleanup_pending
        ? '删除已确认，后台清理待完成。正在重新读取当前文档和目录。'
        : '删除已确认。正在重新读取当前文档和目录。'
      owner.refreshAfterDelete(target.project, receipt.root_id, receipt.deleted_ids, target.parent)
    } catch (error) {
      if (revision !== own || !live()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if (e.problem?.status === 403 || e.problem?.status === 404) deny()
      else if (progress.value?.phase === 'rejected')
        state.message = '本次删除未提交。请重新预览当前范围并再次确认。'
      else state.message = '删除结果尚不确定，请查询原结果；不会自动重发。'
    }
  }
  async function permitNavigation() {
    if (!unsettled.value) {
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
  const stop = watch(
    () =>
      [
        auth.personalContext.phase,
        auth.personalContext.identity,
        workspace.currentReadContext.value,
        owner.state.selected,
        owner.state.metadataPhase,
        owner.busy.value,
        auth.knowledgeDelete.progress?.phase,
      ] as const,
    () => {
      if (disposed) return
      if (!bound) {
        recover()
        return
      }
      if (!same(bound.identity, auth.personalContext.identity)) {
        abandon()
        return
      }
      if (auth.personalContext.phase === 'checking') {
        suspended = true
        ++revision
        auth.knowledgeDelete.cancelPreview()
        clearPreview()
        state.loading = false
        return
      }
      if (suspended) {
        suspended = false
        clear()
        recover()
        return
      }
      const c = workspace.currentReadContext.value
      if (!c) {
        if (unsettled.value) {
          clearPreview()
          state.title = ''
          state.recovering = true
          return
        }
        clear()
        return
      }
      if (c.projectID !== bound.project) {
        abandon()
        return
      }
      if (
        !state.recovering &&
        (c.generation !== bound.generation ||
          c.readGeneration !== bound.readGeneration ||
          owner.state.selected !== bound.root)
      ) {
        if (unsettled.value) {
          clearPreview()
          state.title = ''
          state.recovering = true
        } else {
          abandon()
          return
        }
      }
      if (progress.value?.phase === 'confirmed' && !owner.busy.value) {
        const cleanup = progress.value.receipt?.cleanup_pending ? '后台清理待完成。' : ''
        state.message =
          owner.state.selected === bound.root && owner.state.metadataPhase === 'deleted'
            ? `删除已确认；已读取当前删除状态。${cleanup}`
            : `删除已确认；当前读取未完成，请明确重读。${cleanup}`
      } else if (owner.state.metadataPhase === 'unavailable' && !owner.busy.value) deny()
    },
    { flush: 'post', immediate: true },
  )
  return {
    state,
    progress,
    unsettled,
    blocked,
    canOpen,
    canPreview,
    canDelete,
    canLookup,
    canReplay,
    open,
    loadPreview,
    remove: () => perform('delete'),
    lookup: () => perform('lookup'),
    replay: () => perform('replay'),
    permitNavigation,
    confirmDiscard(value: boolean) {
      const resolve = answer
      answer = null
      resolve?.(value)
    },
    async close() {
      if (await permitNavigation()) state.open = false
    },
    reread() {
      if (bound && live() && !blocked.value && !unsettled.value)
        owner.refreshAfterDelete(
          bound.project,
          bound.root,
          progress.value?.receipt?.deleted_ids ?? [bound.root],
          bound.parent,
        )
    },
    dispose() {
      if (disposed) return
      if (
        auth.personalContext.phase === 'checking' ||
        (unsettled.value &&
          !workspace.currentReadContext.value &&
          same(bound?.identity ?? null, auth.personalContext.identity))
      )
        clear()
      else abandon()
      disposed = true
      stop()
    },
  }
}
export type KnowledgeDelete = ReturnType<typeof useKnowledgeDelete>
