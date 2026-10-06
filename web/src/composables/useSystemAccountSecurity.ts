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
import {
  captureAccountSecurityUpdate,
  type AccountSecuritySettings,
  type AccountSecurityValues,
} from '../api/system-account-security'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

export const accountSecurityFields = [
  {
    key: 'session_idle_seconds',
    label: '会话空闲期限',
    unit: '秒',
    minimum: 900,
    maximum: 2592000,
    hint: '900–2592000 秒，不大于会话绝对期限。',
  },
  {
    key: 'session_absolute_seconds',
    label: '会话绝对期限',
    unit: '秒',
    minimum: 3600,
    maximum: 7776000,
    hint: '3600–7776000 秒，不小于会话空闲期限。',
  },
  {
    key: 'password_reset_seconds',
    label: '密码重置有效期',
    unit: '秒',
    minimum: 300,
    maximum: 7200,
    hint: '300–7200 秒；邀请有效期固定为 24 小时。',
  },
  {
    key: 'challenge_after_failures',
    label: '触发挑战的失败次数',
    unit: '次',
    minimum: 1,
    maximum: 20,
    hint: '1–20 次，不能关闭挑战。',
  },
] as const
type Field = keyof AccountSecurityValues
type Draft = Record<Field, string>
const emptyDraft = (): Draft => ({
  session_idle_seconds: '',
  session_absolute_seconds: '',
  password_reset_seconds: '',
  challenge_after_failures: '',
})
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
function fieldValues(value: AccountSecurityValues): Draft {
  return Object.fromEntries(accountSecurityFields.map(({ key }) => [key, value[key]])) as Draft
}

// This state survives a temporary RouterView unmount. Only Session owns retry bytes/materials.
export function createSystemAccountSecurity(auth: SessionController = useSession()) {
  const api = auth.system.accountSecurity
  const observation = shallowReactive({
    phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error',
    value: null as AccountSecuritySettings | null,
    message: '',
  })
  const draft = reactive(emptyDraft())
  const editor = reactive({ ready: false, version: '', conflict: false, message: '' })
  const state = shallowReactive({
    confirmed: null as AccountSecuritySettings | null,
    writeMessage: '',
    requiresRead: false,
  })
  const confirmation = reactive({ open: false, title: '', message: '' })
  const feedback = ref<'idle' | 'loading' | 'success'>('idle'),
    checking = ref(false)
  const progress = computed(() => api.progress)
  const blocked = computed(() => auth.state.busy || checking.value)
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const locked = computed(
    () => unsettled.value || editor.conflict || state.requiresRead || !editor.ready,
  )
  let base: AccountSecuritySettings | null = null,
    baseline = JSON.stringify(emptyDraft())
  let identity = auth.personalContext.identity,
    revision = 0,
    readRevision = 0,
    reading = false,
    attached = false,
    activeRoute = false,
    disposed = false,
    initial = true
  let timer: ReturnType<typeof setTimeout> | undefined
  let confirmationResult: ((value: boolean) => void) | null = null
  const changed = computed(() => editor.ready && JSON.stringify(draft) !== baseline)
  const dirty = computed(() => changed.value || editor.conflict || unsettled.value)
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    !!auth.personalContext.identity &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied
  const current = (own: number, captured: PersonalIdentity | null) =>
    !disposed &&
    activeRoute &&
    own === revision &&
    authorized() &&
    same(captured, auth.personalContext.identity)
  const errors = computed(() => {
    const result: Partial<Record<Field, string>> = {}
    if (!editor.ready) return result
    for (const field of accountSecurityFields) {
      const value = draft[field.key]
      if (
        !/^[1-9][0-9]*$/.test(value) ||
        value.length > 19 ||
        BigInt(value) < BigInt(field.minimum) ||
        BigInt(value) > BigInt(field.maximum)
      )
        result[field.key] =
          `请输入 ${field.minimum}–${field.maximum} 的整数${field.unit === '秒' ? '秒数' : '次数'}，不加空格或前导零。`
    }
    if (
      !result.session_idle_seconds &&
      !result.session_absolute_seconds &&
      BigInt(draft.session_idle_seconds) > BigInt(draft.session_absolute_seconds)
    ) {
      result.session_idle_seconds = '空闲期限不能大于绝对期限。'
      result.session_absolute_seconds = '绝对期限不能小于空闲期限。'
    }
    return result
  })
  const maximumVersion = computed(() => !!base && base.version === '9223372036854775807')
  const canSave = computed(
    () =>
      !!base &&
      changed.value &&
      !blocked.value &&
      !locked.value &&
      !maximumVersion.value &&
      observation.phase === 'ready' &&
      !Object.keys(errors.value).length &&
      progress.value?.phase !== 'rejected',
  )
  function stopFeedback() {
    clearTimeout(timer)
    timer = undefined
    feedback.value = 'idle'
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const done = confirmationResult
    confirmationResult = null
    done?.(value)
  }
  function requestConfirmation(title: string) {
    if (confirmationResult) return Promise.resolve(false)
    Object.assign(confirmation, {
      open: true,
      title,
      message:
        '将放弃本地草稿和本次请求的追踪；此前保存仍可能生效。放弃修改不会撤销服务器已经保存的配置。',
    })
    return new Promise<boolean>((resolve) => {
      confirmationResult = resolve
    })
  }
  function clearRead() {
    ++readRevision
    if (reading) api.abandonRead()
    reading = false
  }
  function clearEditor() {
    base = null
    baseline = JSON.stringify(emptyDraft())
    Object.assign(draft, emptyDraft())
    Object.assign(editor, { ready: false, version: '', conflict: false, message: '' })
  }
  function adopt(value: AccountSecuritySettings) {
    base = value
    Object.assign(draft, fieldValues(value))
    baseline = JSON.stringify(draft)
    Object.assign(editor, { ready: true, version: value.version, conflict: false, message: '' })
    state.requiresRead = false
  }
  function discard(requireRead: boolean) {
    ++revision
    clearRead()
    api.abandon()
    clearEditor()
    stopFeedback()
    state.writeMessage = ''
    state.requiresRead = requireRead
  }
  function invalidate() {
    discard(false)
    Object.assign(observation, { phase: 'inactive', value: null, message: '' })
    state.confirmed = null
    checking.value = false
    initial = true
    finishConfirmation(false)
  }
  function observe(value: AccountSecuritySettings, initialize: boolean) {
    Object.assign(observation, { phase: 'ready', value, message: '' })
    if (base && value.version !== base.version) {
      editor.conflict = true
      editor.message = '当前观察版本已变化。原草稿和编辑版本保持不变，请核对后明确采用最新值。'
    }
    if (!base && initialize && !unsettled.value) adopt(value)
  }
  function readMessage(error: unknown) {
    return error instanceof AccountFailure && error.kind === 'cancelled'
      ? '读取已取消或等待超时，请在当前请求结束后明确重试。'
      : '未取得完整的当前配置，请明确重新读取。保留的草稿不代表当前服务器配置。'
  }
  async function readCurrent(initialize = true): Promise<AccountSecuritySettings | null> {
    if (!attached || !authorized() || blocked.value) return null
    initial = false
    const own = revision,
      captured = auth.personalContext.identity,
      read = ++readRevision
    reading = true
    const valid = () => attached && current(own, captured) && read === readRevision
    Object.assign(observation, { phase: 'loading', value: null, message: '' })
    try {
      const value = await api.get()
      if (!valid()) return null
      observe(value, initialize)
      return value
    } catch (error) {
      if (valid())
        Object.assign(observation, { phase: 'error', value: null, message: readMessage(error) })
      return null
    } finally {
      if (read === readRevision) reading = false
    }
  }
  async function confirmDiscard(title = '放弃未保存修改？') {
    if (!dirty.value) return true
    const own = revision,
      captured = auth.personalContext.identity
    return (await requestConfirmation(title)) && current(own, captured)
  }
  async function confirmLeave() {
    if (!(await confirmDiscard())) return false
    if (!authorized() || disposed || !activeRoute) return !dirty.value
    discard(unsettled.value)
    return true
  }
  function errorMessage(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (progress.value?.phase === 'uncertain')
      return e.problem?.code === 'IDEMPOTENCY_KEY_REUSED'
        ? '原请求标识与输入冲突。原请求保持不变，请明确放弃后重新读取。'
        : '保存结果未确认。请检查当前会话与配置，再显式重试原请求；当前配置读取不能确认原请求。'
    if (e.problem?.code === 'VERSION_CONFLICT') {
      editor.conflict = true
      return '配置版本冲突。四项草稿和原编辑版本已保留，请读取最新配置，核对后明确采用最新值重新编辑。'
    }
    if (e.problem?.code === 'INVALID_ARGUMENT')
      return '请核对四项字段的整数格式、范围及期限关系，修改后再保存。'
    return '保存未完成，草稿已保留。请检查字段或明确重新读取当前配置。'
  }
  async function mutate(work: () => Promise<AccountSecuritySettings>) {
    const own = revision,
      captured = auth.personalContext.identity
    stopFeedback()
    feedback.value = 'loading'
    state.writeMessage = ''
    state.confirmed = null
    editor.message = ''
    try {
      const result = await work()
      if (!current(own, captured)) return
      state.confirmed = result
      clearEditor()
      state.requiresRead = true
      feedback.value = 'success'
      state.writeMessage = '账号安全配置已保存。'
      timer = setTimeout(() => {
        if (current(own, captured)) feedback.value = 'idle'
      }, 2400)
      const value = await readCurrent()
      if (!current(own, captured)) return
      if (!value)
        state.writeMessage = '保存已确认，当前配置读取失败。请仅重新读取当前配置，不要重复保存。'
    } catch (error) {
      if (!current(own, captured)) return
      feedback.value = 'idle'
      state.writeMessage = errorMessage(error)
    }
  }
  async function resume() {
    if (
      disposed ||
      !attached ||
      !activeRoute ||
      !authorized() ||
      blocked.value ||
      !initial ||
      state.requiresRead
    )
      return
    initial = false
    await readCurrent(!base)
  }
  function afterNavigation(to: string, from: string) {
    const isPage = (path: string) => path.split(/[?#]/, 1)[0] === '/system/account-security'
    if (isPage(from) && !isPage(to)) {
      activeRoute = false
      invalidate()
    } else if (isPage(to)) activeRoute = true
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
      void resume()
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
    observation: readonly(observation),
    draft: readonly(draft),
    editor: readonly(editor),
    state: readonly(state),
    confirmation: readonly(confirmation),
    progress,
    feedback,
    checking,
    blocked,
    locked,
    changed,
    dirty,
    errors,
    maximumVersion,
    canSave,
    attach() {
      attached = true
      activeRoute = true
      void resume()
    },
    detach() {
      attached = false
      clearRead()
      initial = true
    },
    afterNavigation,
    confirmLeave,
    finishConfirmation,
    updateField(field: Field, value: string) {
      if (
        !accountSecurityFields.some(({ key }) => key === field) ||
        !authorized() ||
        blocked.value ||
        locked.value ||
        draft[field] === value
      )
        return
      if (progress.value?.phase === 'rejected') api.abandon()
      stopFeedback()
      state.writeMessage = ''
      draft[field] = value
    },
    save() {
      if (!canSave.value || !base) return Promise.resolve()
      let captured
      try {
        captured = captureAccountSecurityUpdate({ version: base.version, ...draft })
      } catch {
        editor.message = '请核对四项字段的整数格式、范围及期限关系。'
        return Promise.resolve()
      }
      return mutate(() => api.start(captured))
    },
    async cancel() {
      const hadUnsettled = unsettled.value
      if (!(await confirmDiscard())) return
      if (!authorized() || disposed || !activeRoute) return
      discard(hadUnsettled)
      if (!hadUnsettled && observation.phase === 'ready' && observation.value)
        adopt(observation.value)
      else state.requiresRead = true
    },
    async refresh() {
      if (blocked.value || !(await confirmDiscard())) return
      if (!authorized() || disposed || !activeRoute || blocked.value) return
      discard(false)
      await readCurrent()
    },
    readLatest() {
      return !unsettled.value && !blocked.value ? readCurrent(false) : Promise.resolve(null)
    },
    async adoptLatest() {
      if (blocked.value || unsettled.value || observation.phase !== 'ready' || !observation.value)
        return
      const value = observation.value
      if (!(await confirmDiscard('放弃草稿并采用最新值？'))) return
      if (!authorized() || disposed || !activeRoute || blocked.value || observation.value !== value)
        return
      discard(false)
      adopt(value)
    },
    async checkOriginal() {
      if (blocked.value || progress.value?.phase !== 'uncertain') return
      const own = revision,
        captured = auth.personalContext.identity
      checking.value = true
      try {
        const value = await api.checkOriginal()
        if (current(own, captured)) {
          observe(value, false)
          state.writeMessage =
            '已读取当前配置；这不能确认原请求是否接受。请显式重试原请求以取得历史确认。'
        }
      } catch (error) {
        if (current(own, captured)) {
          Object.assign(observation, { phase: 'error', value: null, message: readMessage(error) })
          state.writeMessage =
            '原请求结果仍未确认。当前会话确认后可显式重试原请求；配置读取失败不证明保存未接受。'
        }
      } finally {
        if (own === revision) {
          checking.value = false
          initial = false
        }
      }
    },
    retryOriginal() {
      return !blocked.value && progress.value?.canRetryOriginal
        ? mutate(() => api.retryOriginal())
        : Promise.resolve()
    },
    async abandonOperation() {
      const own = revision,
        captured = auth.personalContext.identity
      if ((await requestConfirmation('放弃本次操作的追踪？')) && current(own, captured)) {
        discard(true)
        state.writeMessage = '已放弃本地追踪。此前保存仍可能生效，请在当前请求结束后明确读取配置。'
      }
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
export type SystemAccountSecurityController = ReturnType<typeof createSystemAccountSecurity>
export const systemAccountSecurityKey: InjectionKey<SystemAccountSecurityController> =
  Symbol('system-account-security')
export function useSystemAccountSecurity() {
  const page = inject(systemAccountSecurityKey)
  if (!page) throw new Error('Account security requires the App controller')
  return page
}
