import {
  computed,
  inject,
  readonly,
  shallowReactive,
  watch,
  ref,
  nextTick,
  onMounted,
  onUnmounted,
  type InjectionKey,
} from 'vue'
import { AccountFailure } from '../api/client'
import type { DeliveryChannel, InvitationInput, ResetInput } from '../api/account'
import type { EntryMutationResult, SessionController } from './useSession'
import {
  accountEntryMode,
  installAccountLinkCapture,
  type AccountEntryMode,
  type AccountLinkCapture,
} from '../router/account-link'

type Phase =
  | 'idle'
  | 'preparing'
  | 'ready'
  | 'failed'
  | 'missing'
  | 'invalid'
  | 'forbidden'
  | 'submitting'
  | 'uncertain'
  | 'success'
  | 'confirming-session'
  | 'session-unconfirmed'
type Draft = {
  email: string
  username: string
  display_name: string
  password: string
  confirmation: string
}

export function createAccountEntry(
  auth: SessionController,
  links: AccountLinkCapture = installAccountLinkCapture(),
) {
  const state = shallowReactive({
    mode: null as AccountEntryMode | null,
    phase: 'idle' as Phase,
    notice: '',
    fields: {} as Record<string, string>,
    requestID: '',
    email: '',
    expiresAt: '',
    channel: null as DeliveryChannel | null,
  })
  const draft = shallowReactive<Draft>({
    email: '',
    username: '',
    display_name: '',
    password: '',
    confirmation: '',
  })
  const confirmation = shallowReactive({ open: false, message: '' })
  let token: string | null = null,
    revision = 0,
    activePath = '',
    disposed = false
  let confirmationPromise: Promise<boolean> | null = null,
    resolveConfirmation: ((value: boolean) => void) | null = null
  let replacing = false
  const locked = computed(
    () =>
      auth.state.busy || ['submitting', 'uncertain', 'confirming-session'].includes(state.phase),
  )
  const dirty = computed(
    () =>
      state.phase === 'uncertain' ||
      (!['success', 'confirming-session', 'session-unconfirmed'].includes(state.phase) &&
        (state.mode === 'forgot'
          ? !!draft.email
          : !!(draft.username || draft.display_name || draft.password || draft.confirmation))),
  )
  const loginTarget = computed(() =>
    auth.state.phase === 'authenticated' ? '/login?switch=1' : '/login',
  )
  function clearSecrets() {
    token = null
    draft.password = ''
    draft.confirmation = ''
  }
  function clearDraft() {
    clearSecrets()
    draft.email = ''
    draft.username = ''
    draft.display_name = ''
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const resolve = resolveConfirmation
    confirmationPromise = null
    resolveConfirmation = null
    resolve?.(value)
  }
  async function confirmLeave(): Promise<boolean> {
    if (auth.state.busy) {
      state.notice = '当前请求尚未结束，请等待后再离开。'
      return false
    }
    if (!dirty.value) return true
    if (!confirmationPromise) {
      confirmation.message =
        state.phase === 'uncertain'
          ? '原请求结果仍未知。放弃会清除本页的重试材料，不代表服务器取消或回滚。'
          : '本页有尚未提交的输入。放弃后需要重新打开原链接。'
      confirmation.open = true
      confirmationPromise = new Promise((resolve) => {
        resolveConfirmation = resolve
      })
    }
    return confirmationPromise
  }
  function error(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    state.requestID = e.problem?.request_id ?? ''
    const fields: Record<string, string> = {}
    for (const field of e.problem?.field_errors ?? []) {
      const name = field.path.slice(1)
      if (name === 'username') fields.username = '请更换用户名；名称可能无效、保留或已被使用。'
      if (name === 'display_name') fields.display_name = '显示名最多 80 个字符，不能含控制字符。'
      if (name === 'email') fields.email = '请输入完整的邮箱地址，不要添加首尾空格。'
      if (name === 'password' || name === 'new_password')
        fields.password = '密码未被接受，请检查长度或更换更安全的密码。'
      if (name === 'confirmation') fields.confirmation = '两次密码必须完全一致。'
    }
    state.fields = fields
    if (auth.entry.progress?.phase === 'uncertain') {
      state.phase = 'uncertain'
      state.notice = auth.entry.progress.contextValid
        ? '原请求结果仍未确认。只能重试原请求，或明确放弃。'
        : '原请求结果仍未知，原上下文已失效，无法重放。请明确放弃后继续。'
    } else if (e.problem?.status === 410 && e.problem.code === 'RESOURCE_DELETED') {
      state.phase = 'invalid'
      clearSecrets()
      state.notice =
        state.mode === 'invitation'
          ? '邀请链接不可用，请联系管理员。'
          : '重置链接不可用，请重新申请找回密码。'
    } else if (e.problem?.code === 'FORBIDDEN' && state.mode === 'invitation') {
      state.phase = 'forbidden'
      state.email = ''
      clearSecrets()
      state.notice = '当前账号不能兑换此邀请。切换账号后，请重新打开原邀请链接。'
    } else {
      state.phase = state.mode === 'forgot' || state.expiresAt ? 'ready' : 'failed'
      state.notice =
        e.problem?.code === 'CSRF_FAILED'
          ? '公开入口上下文已失效，请重新检查。'
          : e.problem?.status === 401
            ? '当前会话已失效，请重新检查。'
            : e.problem?.status === 429
              ? '请求过于频繁，请稍后明确重试。'
              : e.kind === 'busy'
                ? '当前还有请求尚未结束，请等待后再试。'
                : e.kind === 'invalid-input'
                  ? '请检查输入格式；密码允许中文和空格，不能自动去除空格。'
                  : Object.keys(fields).length
                    ? '请检查标出的字段。'
                    : '暂时无法完成请求，请重试。'
    }
  }
  async function prepare() {
    if (!state.mode || disposed || locked.value) return
    const captured = revision
    state.notice = ''
    state.requestID = ''
    state.fields = {}
    state.phase = 'preparing'
    try {
      const result = await auth.entry.prepare()
      if (captured !== revision || disposed) return
      state.channel = result.deliveryChannel
      if (state.mode === 'invitation' && token) {
        const inspection = await auth.entry.inspectInvitation({ token })
        if (captured !== revision || disposed) return
        state.email = inspection.email
        state.expiresAt = inspection.expires_at
      } else if (state.mode === 'reset' && token) {
        const inspection = await auth.entry.inspectPasswordReset({ token })
        if (captured !== revision || disposed) return
        state.expiresAt = inspection.expires_at
      } else if (state.mode !== 'forgot') {
        state.phase = 'missing'
        state.notice = '请重新打开原链接。'
        return
      }
      state.phase = 'ready'
    } catch (e) {
      if (captured === revision && !disposed) error(e)
    }
  }
  function activate(path: string, mode: AccountEntryMode) {
    ++revision
    auth.entry.abandon()
    finishConfirmation(false)
    clearDraft()
    activePath = path
    state.mode = mode
    state.phase = 'idle'
    state.notice = ''
    state.fields = {}
    state.requestID = ''
    state.email = ''
    state.expiresAt = ''
    state.channel = null
    if (mode !== 'forgot') {
      const captured = links.take(mode)
      if (captured.kind !== 'valid') {
        state.phase = captured.kind === 'missing' ? 'missing' : 'invalid'
        state.notice =
          captured.kind === 'unsafe'
            ? '无法安全清除链接地址，请关闭此页后重新打开原链接。'
            : captured.kind === 'missing'
              ? '请重新打开原链接。'
              : '链接格式无效，请重新打开完整的原链接。'
        return
      }
      token = captured.token
    }
    void prepare()
  }
  function afterNavigation(to: string, from: string) {
    if (disposed) return
    const path = to.split(/[?#]/)[0] ?? ''
    const mode = accountEntryMode(path)
    if (mode) {
      if (path !== activePath || (from && to !== from)) activate(path, mode)
    } else if (accountEntryMode(from.split(/[?#]/)[0] ?? '') || activePath) {
      ++revision
      auth.entry.abandon()
      clearDraft()
      finishConfirmation(false)
      state.mode = null
      state.phase = 'idle'
      activePath = ''
      links.discard()
    }
  }
  const stopLinks = links.subscribe(async () => {
    if (disposed || replacing || links.pendingMode !== state.mode) return
    replacing = true
    const captured = revision
    try {
      if (!(await confirmLeave()) || captured !== revision || disposed || auth.state.busy) {
        links.discard()
        return
      }
      const mode = links.pendingMode
      if (mode) activate(activePath, mode)
    } finally {
      replacing = false
    }
  })
  const stopProgress = watch(
    () => auth.entry.progress,
    (progress) => {
      if (!state.mode || !progress) return
      if (
        progress.kind === 'reset-complete' &&
        ['confirming-session', 'confirmed', 'session-unconfirmed'].includes(progress.phase)
      ) {
        clearSecrets()
        state.phase = progress.phase === 'confirmed' ? 'success' : progress.phase
        state.notice =
          progress.phase === 'confirming-session'
            ? '密码重置已确认，正在检查当前会话。'
            : progress.phase === 'session-unconfirmed'
              ? '密码重置已确认，但当前会话尚未确认。请检查会话，不要再次提交重置。'
              : '密码已重置，请重新登录。'
      }
    },
    { flush: 'sync' },
  )
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  function pageHidden() {
    if (!state.mode) return
    ++revision
    auth.entry.abandon()
    clearDraft()
    links.discard()
    finishConfirmation(false)
    state.phase = state.mode === 'forgot' ? 'failed' : 'missing'
    state.notice = state.mode === 'forgot' ? '请重新检查后申请恢复方式。' : '请重新打开原链接。'
  }
  window.addEventListener('beforeunload', beforeUnload)
  window.addEventListener('pagehide', pageHidden)
  function validate(): boolean {
    const fields: Record<string, string> = {}
    if (state.mode === 'forgot') {
      if (
        draft.email.length > 254 ||
        !/^[\x21-\x7e]+$/.test(draft.email) ||
        !/^[^@<>(),;:]+@[^@<>(),;:]+$/.test(draft.email)
      )
        fields.email = '请输入完整的邮箱地址，不要添加首尾空格。'
    } else {
      if (state.mode === 'invitation') {
        if (!/^[A-Za-z0-9](?:[A-Za-z0-9-]{1,30}[A-Za-z0-9])$/.test(draft.username))
          fields.username = '用户名为 3–32 个英文字母、数字或中间连字符。'
        if (
          [...draft.display_name].length > 80 ||
          new TextEncoder().encode(draft.display_name).length > 320 ||
          /[\x00-\x1f\x7f-\x9f]/.test(draft.display_name)
        )
          fields.display_name = '显示名最多 80 个字符，不能含控制字符。'
      }
      if (
        [...draft.password].length < 15 ||
        [...draft.password].length > 128 ||
        new TextEncoder().encode(draft.password).length > 512
      )
        fields.password = '密码应为 15–128 个字符，最多 512 字节。'
      if (draft.confirmation !== draft.password) fields.confirmation = '两次密码必须完全一致。'
    }
    state.fields = fields
    if (Object.keys(fields).length) state.notice = '请检查标出的字段。'
    return !Object.keys(fields).length
  }
  function accepted(result: EntryMutationResult) {
    state.fields = {}
    state.requestID = ''
    if (result.kind === 'reset-request') {
      state.channel = result.value.delivery_channel
      state.notice = '如果该邮箱可用，将按当前渠道提供恢复方式。'
      state.phase = 'success'
    } else if (result.kind === 'invitation') {
      clearSecrets()
      draft.username = ''
      draft.display_name = ''
      state.phase = 'success'
      state.notice = '账号已创建，请返回登录。'
    } else {
      clearSecrets()
      state.phase = result.value.session.kind === 'unconfirmed' ? 'session-unconfirmed' : 'success'
      state.notice =
        result.value.session.kind === 'unconfirmed'
          ? '密码重置已确认，但当前会话尚未确认。请检查会话，不要再次提交重置。'
          : '密码已重置，请重新登录。'
    }
  }
  async function submit(values: Partial<Draft>) {
    if (
      disposed ||
      locked.value ||
      !['ready', 'success'].includes(state.phase) ||
      (state.phase === 'success' && state.mode !== 'forgot')
    )
      return
    // Values are captured synchronously from the native form before the first await.
    for (const name of ['email', 'username', 'display_name', 'password', 'confirmation'] as const)
      if (typeof values[name] === 'string') draft[name] = values[name]
    if (!validate()) return
    const captured = revision,
      mode = state.mode
    const invitation: InvitationInput | null =
      mode === 'invitation' && token
        ? Object.freeze({
            token,
            username: draft.username,
            display_name: draft.display_name,
            password: draft.password,
            confirmation: draft.confirmation,
          })
        : null
    const reset: ResetInput | null =
      mode === 'reset' && token
        ? Object.freeze({ token, new_password: draft.password, confirmation: draft.confirmation })
        : null
    const email = Object.freeze({ email: draft.email })
    state.phase = 'submitting'
    state.notice = ''
    state.requestID = ''
    try {
      const result: EntryMutationResult =
        mode === 'forgot'
          ? { kind: 'reset-request', value: await auth.entry.requestPasswordReset(email) }
          : invitation
            ? { kind: 'invitation', value: await auth.entry.redeemInvitation(invitation) }
            : reset
              ? { kind: 'reset-complete', value: await auth.entry.completePasswordReset(reset) }
              : (() => {
                  throw new AccountFailure('invalid-input')
                })()
      if (captured === revision && !disposed) accepted(result)
    } catch (e) {
      if (captured === revision && !disposed) error(e)
    }
  }
  async function retryOriginal() {
    if (disposed || auth.state.busy || state.phase !== 'uncertain') return
    const captured = revision
    try {
      const result = await auth.entry.retryOriginal()
      if (captured === revision && !disposed) accepted(result)
    } catch (e) {
      if (captured === revision && !disposed) error(e)
    }
  }
  async function checkSession() {
    if (state.phase !== 'session-unconfirmed' || auth.state.busy) return
    const captured = revision
    try {
      await auth.entry.prepare()
      if (captured !== revision || disposed) return
      state.phase = 'success'
      state.notice = '密码已重置，请返回登录。'
    } catch {
      if (captured === revision) state.notice = '密码重置已确认，当前会话仍未确认，请稍后检查。'
    }
  }
  async function abandon() {
    if (!(await confirmLeave()) || auth.state.busy || disposed) return
    const mode = state.mode
    if (mode) {
      links.discard()
      activate(activePath, mode)
    }
  }
  return {
    state: readonly(state),
    draft,
    confirmation: readonly(confirmation),
    locked,
    dirty,
    loginTarget,
    get canRetryOriginal() {
      return auth.entry.progress?.canRetryOriginal ?? false
    },
    prepare,
    submit,
    retryOriginal,
    checkSession,
    abandon,
    confirmLeave,
    finishConfirmation,
    afterNavigation,
    dispose() {
      disposed = true
      ++revision
      stopLinks()
      stopProgress()
      window.removeEventListener('beforeunload', beforeUnload)
      window.removeEventListener('pagehide', pageHidden)
      links.dispose()
      auth.entry.abandon()
      clearDraft()
      finishConfirmation(false)
    },
  }
}
export type AccountEntry = ReturnType<typeof createAccountEntry>
export const accountEntryKey: InjectionKey<AccountEntry> = Symbol('account-entry')
export function useAccountEntry() {
  const owner = inject(accountEntryKey)
  if (!owner) throw new Error('Account entry owner is unavailable')
  return owner
}

// Native controls include autofill and the final keystroke before Vue has rendered.
export function accountEntryFormValues(form: HTMLFormElement) {
  const data = new FormData(form)
  const result: Partial<Draft> = {}
  for (const name of ['email', 'username', 'display_name', 'password', 'confirmation'] as const) {
    const value = data.get(name)
    if (typeof value === 'string') result[name] = value
  }
  return result
}

export function useAccountEntryForm(owner: AccountEntry) {
  const heading = ref<HTMLElement | null>(null),
    form = ref<HTMLFormElement | null>(null)
  let focusRevision = 0,
    alive = true
  const moved = () => {
    ++focusRevision
  }
  onMounted(() => {
    heading.value?.focus()
    document.addEventListener('focusin', moved)
    document.addEventListener('pointerdown', moved)
  })
  onUnmounted(() => {
    alive = false
    document.removeEventListener('focusin', moved)
    document.removeEventListener('pointerdown', moved)
  })
  async function submit() {
    if (!form.value) return
    const values = accountEntryFormValues(form.value),
      revision = focusRevision
    await owner.submit(values)
    await nextTick()
    if (!alive || revision !== focusRevision) return
    const invalid = form.value?.querySelector<HTMLElement>('[aria-invalid="true"]')
    if (invalid) invalid.focus()
    else if (owner.state.notice)
      heading.value?.parentElement?.querySelector<HTMLElement>('[role="alert"]')?.focus()
  }
  return { heading, form, submit }
}
