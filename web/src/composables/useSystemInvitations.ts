import {
  computed,
  inject,
  reactive,
  readonly,
  ref,
  shallowReactive,
  watch,
  type InjectionKey,
} from 'vue'
import { AccountFailure } from '../api/client'
import type {
  Invitation,
  InvitationDelivery,
  InvitationMutationResult,
} from '../api/system-invitations'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

type Target = Readonly<{ index: number; cursor?: string }>
type CommandKind = InvitationMutationResult['kind']
const empty: readonly Invitation[] = Object.freeze([])
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
export function invitationRetryReason(delivery: InvitationDelivery): string {
  if (['enqueue_pending', 'pending', 'claimed', 'sending', 'retry_wait'].includes(delivery.phase))
    return '当前投递周期尚未结束。'
  if (delivery.phase === 'unknown' && delivery.attempt_result === null)
    return '结果未知，尚无已结束尝试的记录。'
  if (delivery.channel !== null && delivery.attempt_result === null)
    return '本次尝试尚未报告结束结果。'
  return ''
}

// App owns page drafts; useSession alone owns Cookie requests and immutable
// command material. Temporary RouterView detach is not navigation or discard.
export function createSystemInvitations(auth: SessionController = useSession()) {
  const state = shallowReactive({
    phase: 'inactive' as 'inactive' | 'waiting' | 'loading' | 'ready' | 'empty' | 'error',
    rows: empty,
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
    writeMessage: '',
    emailError: '',
    requiresReload: false,
  })
  const draft = reactive({ email: '' })
  const dialogs = shallowReactive({
    create: false,
    revoke: null as Readonly<{ id: string; version: string; email: string }> | null,
    retry: null as Readonly<{ job_id: string; version: string; email: string }> | null,
  })
  const confirmation = reactive({ open: false, title: '', message: '', label: '' })
  const feedback = reactive({
    kind: null as CommandKind | null,
    target: '',
    state: 'idle' as 'idle' | 'loading' | 'success',
  })
  const checking = ref(false)
  const progress = computed(() => auth.system.invitationProgress)
  const dirty = computed(
    () =>
      !!draft.email ||
      progress.value?.phase === 'submitting' ||
      progress.value?.phase === 'uncertain',
  )
  const blocked = computed(() => auth.state.busy || checking.value || state.phase === 'loading')
  const locked = computed(
    () =>
      blocked.value ||
      state.requiresReload ||
      ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  let identity = auth.personalContext.identity
  let revision = 0,
    readGeneration = 0,
    activeRead: number | null = null
  let attached = false,
    activeRoute = false,
    disposed = false,
    initial = true
  let history: (string | undefined)[] = [],
    index = 0,
    nextCursor: string | undefined
  let target: Target | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let confirmResult: ((value: boolean) => void) | null = null
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    !!auth.personalContext.identity &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied
  const current = (own: number, captured: PersonalIdentity | null) =>
    !disposed &&
    activeRoute &&
    revision === own &&
    authorized() &&
    same(captured, auth.personalContext.identity)
  function clearRead() {
    ++readGeneration
    if (activeRead !== null) auth.system.abandonInvitationRead()
    activeRead = null
    history = []
    index = 0
    nextCursor = undefined
    target = null
    Object.assign(state, {
      rows: empty,
      message: '',
      cursorInvalid: false,
      hasPrevious: false,
      hasNext: false,
    })
  }
  function stopFeedback() {
    clearTimeout(timer)
    timer = undefined
    Object.assign(feedback, { kind: null, target: '', state: 'idle' })
  }
  function clearDraft() {
    draft.email = ''
    dialogs.create = false
    dialogs.revoke = null
    dialogs.retry = null
    state.emailError = ''
  }
  function discard(requireRead = false) {
    ++revision
    auth.system.abandonInvitations()
    clearDraft()
    stopFeedback()
    state.writeMessage = ''
    state.requiresReload = requireRead
  }
  function invalidate() {
    discard()
    clearRead()
    initial = true
    state.phase = 'inactive'
    finishConfirmation(false)
  }
  async function read(next: Target) {
    if (disposed || !activeRoute || !authorized() || auth.state.busy || state.phase === 'loading')
      return
    initial = false
    const own = revision,
      captured = auth.personalContext.identity,
      generation = ++readGeneration
    const valid = () => current(own, captured) && generation === readGeneration
    activeRead = generation
    target = next
    Object.assign(state, {
      phase: 'loading',
      rows: empty,
      message: '',
      cursorInvalid: false,
      hasPrevious: false,
      hasNext: false,
    })
    try {
      const page = await auth.system.listInvitations(
        next.cursor === undefined ? {} : { cursor: next.cursor },
      )
      if (!valid()) return
      history = [...history.slice(0, next.index), next.cursor]
      index = next.index
      nextCursor = page.next_cursor
      Object.assign(state, {
        rows: page.items,
        hasPrevious: index > 0,
        hasNext: nextCursor !== undefined,
        phase: page.items.length ? 'ready' : 'empty',
        requiresReload: false,
      })
    } catch (error) {
      if (!valid()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      state.phase = 'error'
      state.cursorInvalid = e.problem?.code === 'CURSOR_INVALID'
      state.message = state.cursorInvalid
        ? '分页链接已失效，请返回首页重新加载。'
        : e.kind === 'cancelled'
          ? '本次读取已取消或等待超时，请在请求结束后重试。'
          : e.kind === 'invalid-response'
            ? '邀请列表响应无法确认，请重试。'
            : '暂时无法读取邀请列表，请重试。'
    } finally {
      if (activeRead === generation) activeRead = null
    }
  }
  async function refresh() {
    if (blocked.value) return
    history = []
    await read({ index: 0 })
  }
  function showFailure(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    const code = e.problem?.code
    state.writeMessage =
      progress.value?.phase === 'uncertain'
        ? code === 'IDEMPOTENCY_KEY_REUSED'
          ? '原请求标识与输入冲突，原请求仍未确认；请保留原请求或明确放弃后重新加载。'
          : '请求结果未确认。此前提交仍可能生效；可检查当前状态、重试原请求或明确放弃。'
        : code === 'VERSION_CONFLICT'
          ? '记录版本已变化，请重新加载后再操作。'
          : code === 'RESOURCE_DELETED' || code === 'TOKEN_INVALID'
            ? '该邀请已不可用，请重新加载列表。'
            : code === 'RATE_LIMITED'
              ? '操作过于频繁，请稍后重试。'
              : e.kind === 'busy'
                ? '上一请求尚未结束，请等待后再操作。'
                : e.kind === 'invalid-input' || code === 'INVALID_ARGUMENT'
                  ? '请检查输入或重新加载当前记录。'
                  : '操作未完成，请检查当前状态或重试。'
    if (e.problem?.field_errors?.some((item) => item.path === '/email'))
      state.emailError = '邮箱格式不正确或已被占用，请检查后重试。'
  }
  async function mutate(
    kind: CommandKind,
    targetID: string,
    work: () => Promise<InvitationMutationResult>,
  ) {
    const own = revision,
      captured = auth.personalContext.identity
    stopFeedback()
    Object.assign(feedback, { kind, target: targetID, state: 'loading' })
    state.writeMessage = ''
    state.emailError = ''
    try {
      const result = await work()
      if (!current(own, captured)) return
      clearDraft()
      state.writeMessage = {
        create: '邀请请求已接受',
        resend: '重发请求已接受',
        retry: '重试投递请求已接受',
        revoke: '邀请已撤销',
      }[result.kind]
      feedback.state = 'success'
      timer = setTimeout(() => {
        if (current(own, captured)) feedback.state = 'idle'
      }, 2400)
      // A valid receipt is final for this command. A later GET is independent.
      history = []
      await read({ index: 0 })
    } catch (error) {
      if (!current(own, captured)) return
      feedback.state = 'idle'
      showFailure(error)
    }
  }
  function requestConfirmation(title: string, message: string, label: string) {
    if (confirmResult) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message, label })
    return new Promise<boolean>((resolve) => {
      confirmResult = resolve
    })
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const done = confirmResult
    confirmResult = null
    done?.(value)
  }
  async function confirmLeave() {
    if (!dirty.value) return true
    const hadIntent =
      progress.value?.phase === 'submitting' || progress.value?.phase === 'uncertain'
    const accepted = await requestConfirmation(
      '放弃未保存修改？',
      '邮箱输入将清除；此前提交仍可能生效。这里只停止页面追踪，不会取消服务端提交。',
      '放弃修改',
    )
    if (accepted) discard(hadIntent)
    return accepted
  }
  async function closeDialog() {
    if (!(await confirmLeave())) return
    clearDraft()
  }
  async function abandonOperation() {
    if (
      await requestConfirmation(
        '放弃本次操作？',
        '此前提交仍可能生效。清空后必须成功重新读取列表，才能开始新的邀请操作。',
        '放弃并清空',
      )
    )
      discard(true)
  }
  async function checkCurrent() {
    if (blocked.value) return
    checking.value = true
    try {
      await auth.restore()
      if (!authorized() || !activeRoute) return
      await read({ index: 0 })
      if (progress.value?.phase === 'uncertain')
        state.writeMessage = '当前状态已检查；列表不是原请求的确认。仍可重试原请求或明确放弃。'
    } finally {
      checking.value = false
    }
  }
  function afterNavigation(to: string, from: string) {
    const isPage = (value: string) => value.split(/[?#]/, 1)[0] === '/system/invitations'
    if (isPage(from) && !isPage(to)) {
      activeRoute = false
      invalidate()
    } else if (isPage(to)) activeRoute = true
  }
  function initialRead() {
    if (
      !disposed &&
      attached &&
      activeRoute &&
      initial &&
      authorized() &&
      !auth.state.busy &&
      !checking.value
    )
      void read({ index: 0 })
  }
  const stopIdentity = watch(
    () =>
      [
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.user?.role,
        auth.system.denied,
        auth.state.busy,
      ] as const,
    ([phase, value]) => {
      if (phase === 'checking') return
      if (
        phase === 'invalid' ||
        !same(identity, value) ||
        auth.state.user?.role !== 'admin' ||
        auth.system.denied
      ) {
        invalidate()
        identity = value
      }
      initialRead()
    },
  )
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', beforeUnload)
  return {
    auth,
    state: readonly(state),
    draft,
    dialogs: readonly(dialogs),
    feedback: readonly(feedback),
    confirmation: readonly(confirmation),
    progress,
    dirty,
    blocked,
    locked,
    checking,
    attach() {
      attached = true
      activeRoute = true
      initialRead()
    },
    detach() {
      attached = false
      clearRead()
      initial = true
      state.phase = 'waiting'
    },
    afterNavigation,
    confirmLeave,
    finishConfirmation,
    closeDialog,
    abandonOperation,
    checkCurrent,
    refresh,
    openCreate() {
      if (!locked.value) {
        dialogs.create = true
        state.emailError = ''
        state.writeMessage = ''
      }
    },
    openRevoke(row: Invitation) {
      if (!locked.value)
        dialogs.revoke = Object.freeze({ id: row.id, version: row.version, email: row.email })
    },
    openRetry(row: Invitation) {
      if (!locked.value && !invitationRetryReason(row.latest_delivery))
        dialogs.retry = Object.freeze({
          job_id: row.latest_delivery.job_id,
          version: row.latest_delivery.version,
          email: row.email,
        })
    },
    create() {
      if (locked.value) return Promise.resolve()
      if (!draft.email || draft.email.length > 254) {
        state.emailError = '请输入邮箱，最多 254 个字符。'
        return Promise.resolve()
      }
      const email = draft.email
      return mutate('create', '', () => auth.system.createInvitation({ email }))
    },
    resend(row: Invitation) {
      if (locked.value) return Promise.resolve()
      const captured = { id: row.id, version: row.version }
      return mutate('resend', row.id, () => auth.system.resendInvitation(captured))
    },
    revoke() {
      if (locked.value || !dialogs.revoke) return Promise.resolve()
      const { id, version } = dialogs.revoke
      return mutate('revoke', id, () => auth.system.revokeInvitation({ id, version }))
    },
    retryDelivery() {
      if (locked.value || !dialogs.retry) return Promise.resolve()
      const { job_id, version } = dialogs.retry
      return mutate('retry', job_id, () => auth.system.retryDelivery({ job_id, version }))
    },
    retryOriginal() {
      const kind = progress.value?.kind
      if (!kind || !progress.value?.canRetryOriginal || blocked.value) return Promise.resolve()
      return mutate(kind, feedback.target, () => auth.system.retryInvitationOriginal())
    },
    previous() {
      return state.hasPrevious && !blocked.value
        ? read({ index: index - 1, cursor: history[index - 1] })
        : Promise.resolve()
    },
    next() {
      return state.hasNext && nextCursor !== undefined && !blocked.value
        ? read({ index: index + 1, cursor: nextCursor })
        : Promise.resolve()
    },
    retryRead() {
      return state.phase === 'error' && target && !state.cursorInvalid && !blocked.value
        ? read(target)
        : Promise.resolve()
    },
    dispose() {
      if (disposed) return
      disposed = true
      stopIdentity()
      invalidate()
      window.removeEventListener('beforeunload', beforeUnload)
    },
  }
}
export type SystemInvitationsController = ReturnType<typeof createSystemInvitations>
export const systemInvitationsKey: InjectionKey<SystemInvitationsController> =
  Symbol('system-invitations')
export function useSystemInvitations() {
  const value = inject(systemInvitationsKey)
  if (!value) throw new Error('System invitations require the App controller')
  return value
}
