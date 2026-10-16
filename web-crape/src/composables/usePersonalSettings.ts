import {
  computed,
  inject,
  reactive,
  readonly,
  ref,
  shallowRef,
  watch,
  type InjectionKey,
} from 'vue'
import type {
  AvatarMedia,
  AvatarMetadata,
  PasswordInput,
  PreferencesView,
  ProfileInput,
  ProfileView,
  Theme,
} from '../api/account'
import { AccountFailure } from '../api/client'
import {
  useSession,
  type PersonalIdentity,
  type PersonalMutationResult,
  type SessionController,
} from './useSession'

export type SettingsSection = 'profile' | 'appearance' | 'password'
type SectionStatus =
  'idle' | 'loading' | 'saving' | 'uncertain' | 'confirmed' | 'error' | 'confirming-session'
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const uncertain = (e: AccountFailure) =>
  ['transport', 'cancelled', 'invalid-response'].includes(e.kind) ||
  e.problem?.commit_state === 'unknown' ||
  ['COMMIT_UNKNOWN', 'RESOURCE_BUSY'].includes(e.problem?.code ?? '')
const blank = () => ({
  status: 'idle' as SectionStatus,
  message: '',
  fields: {} as Record<string, string>,
})
const metadataEqual = (a: AvatarMetadata | null | undefined, b: AvatarMetadata) =>
  a?.media_type === b.media_type && a.byte_size === b.byte_size && a.sha256 === b.sha256

// Created once by App. It survives a checking-only RouterView unmount, and has no
// Session/token/key or second request lane. All authority stays in useSession.
export function createPersonalSettings(auth: SessionController = useSession()) {
  const identity = shallowRef<PersonalIdentity | null>(auth.personalContext.identity)
  const savedProfile = shallowRef<ProfileView | null>(null)
  const savedPreferences = shallowRef<PreferencesView | null>(null)
  const profileDraft = reactive({ username: '', display_name: '', version: '' })
  const profileBase = reactive({ username: '', display_name: '', version: '' })
  const appearanceDraft = reactive({ theme: 'system' as Theme, version: '' })
  const appearanceBase = reactive({ theme: 'system' as Theme, version: '' })
  const password = reactive({
    current_password: '',
    new_password: '',
    confirmation: '',
    version: '',
  })
  const sections = reactive({ profile: blank(), appearance: blank(), password: blank() })
  const candidate = shallowRef<File | null>(null)
  let candidateVersion = ''
  const candidateURL = ref(''),
    avatarURL = ref('')
  const avatarState = reactive({ loading: false, error: '', message: '' })
  const mutation = ref<SettingsSection | null>(null)
  const unresolved = ref<SettingsSection | null>(null)
  const requiresReload = ref(false)
  const confirmation = reactive({ open: false, title: '', message: '', label: '' })
  let confirmResult: ((accepted: boolean) => void) | null = null
  let revision = 0,
    imageRevision = 0
  let disposed = false
  let passwordFeedback: {
    identity: PersonalIdentity
    origin: PersonalIdentity
    requestGeneration: number
  } | null = null
  const profileDirty = computed(
    () =>
      profileDraft.username !== profileBase.username ||
      profileDraft.display_name !== profileBase.display_name,
  )
  const appearanceDirty = computed(() => appearanceDraft.theme !== appearanceBase.theme)
  const passwordDirty = computed(
    () => !!(password.current_password || password.new_password || password.confirmation),
  )
  const dirty = computed(
    () =>
      profileDirty.value ||
      appearanceDirty.value ||
      passwordDirty.value ||
      !!candidate.value ||
      !!unresolved.value ||
      !!mutation.value,
  )
  const locked = computed(
    () => auth.state.busy || !!mutation.value || !!unresolved.value || requiresReload.value,
  )
  const profileVersionChanged = computed(
    () =>
      !!savedProfile.value &&
      !!profileDraft.version &&
      savedProfile.value.user.version !== profileDraft.version,
  )
  const appearanceVersionChanged = computed(
    () =>
      !!savedPreferences.value &&
      !!appearanceDraft.version &&
      savedPreferences.value.version !== appearanceDraft.version,
  )
  function revoke(url: typeof candidateURL) {
    if (url.value) URL.revokeObjectURL(url.value)
    url.value = ''
  }
  function clearCandidate() {
    candidate.value = null
    candidateVersion = ''
    revoke(candidateURL)
  }
  function clearPassword() {
    password.current_password = ''
    password.new_password = ''
    password.confirmation = ''
  }
  function resetProfile() {
    const user = savedProfile.value?.user
    Object.assign(profileDraft, {
      username: user?.username ?? '',
      display_name: user?.display_name ?? '',
      version: user?.version ?? '',
    })
    Object.assign(profileBase, profileDraft)
  }
  function resetAppearance() {
    const value = savedPreferences.value
    Object.assign(appearanceDraft, {
      theme: value?.theme ?? 'system',
      version: value?.version ?? '',
    })
    Object.assign(appearanceBase, appearanceDraft)
    if (identity.value) auth.personal.clearThemePreview(identity.value)
  }
  function resetDrafts() {
    resetProfile()
    resetAppearance()
    clearCandidate()
    clearPassword()
    password.version = savedProfile.value?.user.version ?? auth.state.user?.version ?? ''
    unresolved.value = null
  }
  function resetOwner() {
    ++revision
    ++imageRevision
    if (identity.value) auth.personal.clearThemePreview(identity.value)
    savedProfile.value = null
    savedPreferences.value = null
    resetDrafts()
    revoke(avatarURL)
    Object.assign(avatarState, { loading: false, error: '', message: '' })
    for (const section of Object.values(sections)) Object.assign(section, blank())
    mutation.value = null
    passwordFeedback = null
    requiresReload.value = false
    finishConfirmation(false)
  }
  const current = (captured: PersonalIdentity | null, own: number) =>
    !disposed &&
    revision === own &&
    same(captured, identity.value) &&
    same(captured, auth.personalContext.identity) &&
    auth.personalContext.phase === 'current'
  function acceptPreferences(value: PreferencesView) {
    if (savedPreferences.value && BigInt(value.version) < BigInt(savedPreferences.value.version))
      return
    const editing = appearanceDirty.value
    savedPreferences.value = Object.freeze({ ...value })
    if (!editing) resetAppearance()
  }
  function acceptProfile(value: ProfileView) {
    if (
      value.user.id !== identity.value?.userID ||
      (savedProfile.value && BigInt(value.user.version) < BigInt(savedProfile.value.user.version))
    )
      return
    const editing = profileDirty.value
    savedProfile.value = value
    if (!editing) resetProfile()
    acceptPreferences({ version: value.user.version, theme: value.user.theme })
    if (!passwordDirty.value) password.version = value.user.version
  }
  function showError(section: SettingsSection, error: unknown, write = false) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    const target = sections[section]
    target.fields = {}
    const code = e.problem?.code
    target.status = write && (uncertain(e) || unresolved.value === section) ? 'uncertain' : 'error'
    if (write && (uncertain(e) || code === 'IDEMPOTENCY_KEY_REUSED')) unresolved.value = section
    target.message =
      target.status === 'uncertain'
        ? '请求结果未确认。可检查当前事实、重试原请求，或明确放弃本次操作；此前提交仍可能生效。'
        : code === 'VERSION_CONFLICT'
          ? '资料版本已变化。输入仍保留，请明确重新加载后再编辑。'
          : code === 'IDEMPOTENCY_KEY_REUSED'
            ? '原请求标识与输入冲突，请保留原请求或明确放弃后重新加载。'
            : code === 'PAYLOAD_TOO_LARGE'
              ? '头像不能超过 5 MiB。'
              : code === 'UNSUPPORTED_MEDIA_TYPE'
                ? '仅支持静态 JPG、PNG、WebP 图片。'
                : e.kind === 'busy'
                  ? '上一请求尚未结束，请稍后再操作。'
                  : e.kind === 'invalid-input'
                    ? '请检查输入格式。'
                    : '操作未完成，请检查当前登录状态或重试。'
    for (const item of e.problem?.field_errors ?? []) {
      const name = item.path.slice(1)
      const labels: Record<string, string> = {
        username: '用户名不可用，请检查格式或换一个名称。',
        display_name: '显示名最多 80 个字符、320 字节，不能含控制字符。',
        current_password: '当前密码不正确。',
        new_password: '新密码不符合要求，请使用更强的密码。',
        confirmation: '两次新密码不一致。',
      }
      if (labels[name]) target.fields[name] = labels[name]
      else if (item.path === '/password')
        target.message = '密码校验失败，请检查当前密码和新密码规则。'
    }
  }
  async function loadAvatar() {
    const captured = identity.value,
      own = revision,
      image = ++imageRevision
    const expected = savedProfile.value?.avatar
    revoke(avatarURL)
    avatarState.error = ''
    if (expected === null) {
      avatarState.loading = false
      return
    }
    if (!expected) {
      avatarState.error = '当前头像资料尚未取得。'
      return
    }
    avatarState.loading = true
    try {
      const result = await auth.personal.readAvatar()
      if (!current(captured, own) || image !== imageRevision) return
      if (!metadataEqual(savedProfile.value?.avatar, result.metadata)) {
        avatarState.error = '头像已变化，请重新加载。'
        return
      }
      avatarURL.value = URL.createObjectURL(result.blob)
    } catch (error) {
      if (current(captured, own) && image === imageRevision)
        avatarState.error =
          error instanceof AccountFailure && error.kind === 'busy'
            ? '上一请求尚未结束，暂时无法读取头像。'
            : '头像读取失败，请重新加载。'
    } finally {
      if (current(captured, own) && image === imageRevision) avatarState.loading = false
    }
  }
  async function load(section: SettingsSection, images = section === 'profile') {
    const captured = identity.value,
      own = revision
    if (sections[section].status === 'loading' || auth.state.busy) return
    const passwordConfirmed = section === 'password' && ownsPasswordProgress()
    let readError = ''
    sections[section].status = 'loading'
    if (!passwordConfirmed) sections[section].message = ''
    try {
      if (section === 'appearance') {
        const value = await auth.personal.getPreferences()
        if (!current(captured, own)) return
        acceptPreferences(value)
      } else {
        const value = await auth.personal.getProfile()
        if (!current(captured, own)) return
        acceptProfile(value)
      }
      requiresReload.value = false
      sections[section].status = 'idle'
      if (images) await loadAvatar()
    } catch (error) {
      if (current(captured, own)) {
        showError(section, error)
        readError = sections[section].message
      }
    } finally {
      // A profile read can refresh the version, but cannot erase the independent
      // confirmed password command or turn its Session confirmation into a write retry.
      if (section === 'password' && current(captured, own)) showPasswordProgress(readError)
    }
  }
  function localError(section: SettingsSection, fields: Record<string, string>) {
    sections[section].status = 'error'
    sections[section].fields = fields
    sections[section].message = '请检查标注的输入。'
  }
  async function mutate(section: SettingsSection, work: () => Promise<PersonalMutationResult>) {
    if (locked.value || auth.personalContext.phase !== 'current') return
    const captured = identity.value,
      own = revision
    mutation.value = section
    Object.assign(sections[section], { status: 'saving', fields: {}, message: '' })
    try {
      const result = await work()
      if (!current(captured, own)) return
      await applyMutation(result)
    } catch (error) {
      if (current(captured, own)) showError(section, error, true)
    } finally {
      if (revision === own) mutation.value = null
    }
  }
  async function applyMutation(result: PersonalMutationResult) {
    unresolved.value = null
    if (result.kind === 'profile' || result.kind === 'avatar-put') {
      acceptProfile(result.value)
      sections.profile.status = 'confirmed'
      if (result.kind === 'profile') {
        resetProfile()
        sections.profile.message = '资料已保存。'
      } else {
        clearCandidate()
        avatarState.message = '头像上传已确认。'
      }
      await loadAvatar()
    } else if (result.kind === 'preferences') {
      acceptPreferences(result.value)
      resetAppearance()
      sections.appearance.status = 'confirmed'
      sections.appearance.message = '主题已保存。'
    } else if (result.kind === 'avatar-delete') {
      clearCandidate()
      revoke(avatarURL)
      avatarState.message = '头像移除命令已确认，正在读取当前资料。'
      sections.profile.status = 'confirmed'
      const captured = identity.value,
        own = revision
      try {
        const latest = await auth.personal.getProfile()
        if (!current(captured, own)) return
        acceptProfile(latest)
        avatarState.message = '头像移除命令已确认；下方显示当前头像状态。'
        await loadAvatar()
      } catch {
        if (current(captured, own))
          avatarState.error = '移除已确认，但当前资料读取失败，请重新加载。'
      }
    } else if (result.kind === 'password') {
      clearPassword()
      sections.password.status = result.value.sessionConfirmed ? 'confirmed' : 'error'
      sections.password.message = result.value.sessionConfirmed
        ? '密码已修改，当前登录已更新，其他旧登录已失效。'
        : '密码修改已确认，但新会话尚未确认。请检查当前会话，不要再次提交改密。'
    }
  }
  async function saveProfile() {
    const fields: Record<string, string> = {}
    if (
      profileDraft.username !== profileBase.username &&
      !/^[A-Za-z0-9](?:[A-Za-z0-9-]{1,30}[A-Za-z0-9])$/.test(profileDraft.username)
    )
      fields.username = '用户名须为 3–32 位英文字母、数字或中间连字符。'
    if (
      [...profileDraft.display_name].length > 80 ||
      new TextEncoder().encode(profileDraft.display_name).byteLength > 320 ||
      /[\x00-\x1f\x7f-\x9f]/.test(profileDraft.display_name)
    )
      fields.display_name = '显示名最多 80 个字符、320 字节，不能含控制字符。'
    if (Object.keys(fields).length) {
      localError('profile', fields)
      return
    }
    if (!profileDirty.value || !profileDraft.version) return
    const value = {
      version: profileDraft.version,
      ...(profileDraft.username === profileBase.username
        ? {}
        : { username: profileDraft.username }),
      ...(profileDraft.display_name === profileBase.display_name
        ? {}
        : { display_name: profileDraft.display_name }),
    } as ProfileInput
    await mutate('profile', async () => ({
      kind: 'profile',
      value: await auth.personal.updateProfile(value),
    }))
  }
  function selectAvatar(file: File | null) {
    if (locked.value) return
    clearCandidate()
    avatarState.message = ''
    if (!file) return
    if (
      !['image/jpeg', 'image/png', 'image/webp'].includes(file.type) ||
      file.size === 0 ||
      file.size > 5 * 1024 * 1024
    ) {
      avatarState.error = '请选择不超过 5 MiB 的静态 JPG、PNG 或 WebP 图片。'
      return
    }
    avatarState.error = ''
    candidate.value = file
    candidateVersion = savedProfile.value?.user.version ?? ''
    candidateURL.value = URL.createObjectURL(file)
  }
  async function uploadAvatar() {
    const file = candidate.value,
      version = candidateVersion
    if (!file || !version) return
    await mutate('profile', async () => ({
      kind: 'avatar-put',
      value: await auth.personal.putAvatar({ version, file, mediaType: file.type as AvatarMedia }),
    }))
  }
  async function removeAvatar(version: string) {
    if (!version || savedProfile.value?.avatar === null) return
    await mutate('profile', async () => {
      await auth.personal.deleteAvatar({ version })
      return { kind: 'avatar-delete' }
    })
  }
  function chooseTheme(value: Theme) {
    if (locked.value || !identity.value) return
    appearanceDraft.theme = value
    auth.personal.previewTheme(identity.value, value)
  }
  async function saveTheme() {
    if (!appearanceDirty.value || !appearanceDraft.version) return
    const value = { version: appearanceDraft.version, theme: appearanceDraft.theme }
    await mutate('appearance', async () => ({
      kind: 'preferences',
      value: await auth.personal.setPreferences(value),
    }))
  }
  async function savePassword() {
    const fields: Record<string, string> = {}
    if (
      !password.current_password ||
      [...password.current_password].length > 512 ||
      new TextEncoder().encode(password.current_password).byteLength > 512
    )
      fields.current_password = '请输入当前密码，最多 512 字节。'
    for (const name of ['new_password', 'confirmation'] as const) {
      const value = password[name]
      if (
        [...value].length < 15 ||
        [...value].length > 128 ||
        new TextEncoder().encode(value).byteLength > 512
      )
        fields[name] = '须为 15–128 个字符，最多 512 字节。'
    }
    if (password.new_password !== password.confirmation) fields.confirmation = '两次新密码不一致。'
    if (Object.keys(fields).length) {
      localError('password', fields)
      return
    }
    const value: PasswordInput = { ...password }
    await mutate('password', async () => ({
      kind: 'password',
      value: await auth.personal.changePassword(value),
    }))
  }
  async function retryOriginal() {
    const section = unresolved.value
    if (!section || auth.state.busy || mutation.value) return
    const captured = identity.value,
      own = revision
    mutation.value = section
    try {
      const result = await auth.personal.retryOriginal()
      if (current(captured, own)) await applyMutation(result)
    } catch (error) {
      if (current(captured, own)) showError(section, error, true)
    } finally {
      if (revision === own) mutation.value = null
    }
  }
  async function checkCurrent(section: SettingsSection) {
    if (auth.state.busy) return
    await auth.restore()
    if (auth.personalContext.phase !== 'current') return
    await load(section)
    if (unresolved.value === section) {
      sections[section].status = 'uncertain'
      sections[section].message =
        '当前资料已检查；它不能证明原请求是否完成。仍可重试原请求或明确放弃。'
    }
  }
  function requestConfirmation(title: string, message: string, label: string): Promise<boolean> {
    if (confirmResult) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message, label })
    return new Promise((resolve) => {
      confirmResult = resolve
    })
  }
  function finishConfirmation(accepted: boolean) {
    confirmation.open = false
    const done = confirmResult
    confirmResult = null
    done?.(accepted)
  }
  function discard() {
    ++revision
    ++imageRevision
    auth.personal.abandon()
    resetDrafts()
    mutation.value = null
    for (const section of Object.values(sections)) Object.assign(section, blank())
  }
  async function confirmLeave() {
    if (!dirty.value) return true
    const answer = await requestConfirmation(
      '放弃未保存修改？',
      '未保存的资料、头像选择、主题预览和密码输入将清除。在途或未确认的提交仍可能生效。',
      '放弃修改',
    )
    if (answer) discard()
    return answer
  }
  async function abandonOperation() {
    if (
      await requestConfirmation(
        '放弃本次页面操作？',
        '此前提交仍可能生效。这里只放弃页面的确认和编辑，不会取消服务端提交；之后须重新加载才能开始新操作。',
        '放弃并清空',
      )
    ) {
      discard()
      requiresReload.value = true
    }
  }
  async function reload(section: SettingsSection) {
    if (dirty.value && !(await confirmLeave())) return
    await load(section)
  }
  function afterNavigation(to: string, from: string) {
    if (to === from) return
    if (from.startsWith('/settings') && !to.startsWith('/settings')) {
      discard()
      revoke(avatarURL)
      savedProfile.value = null
      savedPreferences.value = null
    }
  }
  const stopIdentity = watch(
    () => [auth.personalContext.phase, auth.personalContext.identity] as const,
    ([phase, value]) => {
      if (phase === 'checking') return
      if (phase === 'invalid' || !same(identity.value, value)) {
        const progress = auth.personal.passwordProgress
        const feedback = passwordFeedback
        const keepPassword =
          !!progress &&
          !!feedback &&
          ownsPasswordProgress() &&
          same(identity.value, progress.identity) &&
          value?.userID === identity.value?.userID
        resetOwner()
        identity.value = value
        if (keepPassword && value) {
          // Only this command's first A -> B transition inherits its safe
          // feedback. A later B -> C epoch retires it even for the same user.
          passwordFeedback = { ...feedback, identity: value }
          sections.password.status =
            progress.phase === 'confirmed' ? 'confirmed' : 'confirming-session'
          sections.password.message = '密码修改已确认，正在确认当前登录。'
        }
      }
    },
    { flush: 'sync' },
  )
  const stopUser = watch(
    () => auth.state.user,
    (user) => {
      if (!user || user.id !== identity.value?.userID) return
      if (savedProfile.value && BigInt(user.version) >= BigInt(savedProfile.value.user.version))
        acceptProfile({ user, avatar: savedProfile.value.avatar })
      else acceptPreferences({ version: user.version, theme: user.theme })
      if (!passwordDirty.value) password.version = user.version
    },
    { flush: 'sync', immediate: true },
  )
  function ownsPasswordProgress() {
    const progress = auth.personal.passwordProgress
    return (
      !!progress &&
      !!passwordFeedback &&
      same(passwordFeedback.identity, identity.value) &&
      same(passwordFeedback.origin, progress.identity) &&
      passwordFeedback.requestGeneration === progress.requestGeneration
    )
  }
  function showPasswordProgress(readError = '') {
    const progress = auth.personal.passwordProgress
    if (!progress || !ownsPasswordProgress()) return
    sections.password.status = readError
      ? 'error'
      : progress.phase === 'confirming-session'
        ? 'confirming-session'
        : progress.phase === 'confirmed'
          ? 'confirmed'
          : 'error'
    const confirmed =
      progress.phase === 'confirmed'
        ? '密码已修改，当前登录已更新，其他旧登录已失效。'
        : progress.phase === 'confirming-session'
          ? '密码修改已确认，正在确认新会话。'
          : '密码修改已确认，但新会话尚未确认。请检查当前会话，不要再次提交改密。'
    sections.password.message = readError ? `${confirmed} ${readError}` : confirmed
  }
  const stopPassword = watch(
    () => auth.personal.passwordProgress,
    (progress) => {
      if (!progress) {
        passwordFeedback = null
        return
      }
      if (identity.value && same(progress.identity, identity.value))
        passwordFeedback = {
          identity: identity.value,
          origin: progress.identity,
          requestGeneration: progress.requestGeneration,
        }
      if (!ownsPasswordProgress()) return
      clearPassword()
      showPasswordProgress()
    },
    { flush: 'sync' },
  )
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', beforeUnload)
  function dispose() {
    if (disposed) return
    disposed = true
    auth.personal.abandon()
    resetOwner()
    stopIdentity()
    stopUser()
    stopPassword()
    window.removeEventListener('beforeunload', beforeUnload)
  }
  return {
    auth,
    savedProfile: readonly(savedProfile),
    savedPreferences: readonly(savedPreferences),
    profileDraft,
    appearanceDraft,
    password,
    sections: readonly(sections),
    avatarState: readonly(avatarState),
    candidate: readonly(candidate),
    candidateURL: readonly(candidateURL),
    avatarURL: readonly(avatarURL),
    confirmation: readonly(confirmation),
    profileDirty,
    appearanceDirty,
    passwordDirty,
    dirty,
    locked,
    profileVersionChanged,
    appearanceVersionChanged,
    mutation: readonly(mutation),
    unresolved: readonly(unresolved),
    requiresReload: readonly(requiresReload),
    load,
    saveProfile,
    selectAvatar,
    uploadAvatar,
    removeAvatar,
    chooseTheme,
    saveTheme,
    savePassword,
    retryOriginal,
    checkCurrent,
    resetProfile,
    resetAppearance,
    clearCandidate,
    clearPassword,
    confirmLeave,
    finishConfirmation,
    abandonOperation,
    reload,
    afterNavigation,
    dispose,
  }
}
export type PersonalSettings = ReturnType<typeof createPersonalSettings>
export const personalSettingsKey: InjectionKey<PersonalSettings> = Symbol(
  'personal settings draft owner',
)
export function usePersonalSettings() {
  const owner = inject(personalSettingsKey)
  if (!owner) throw new Error('personal settings owner is missing')
  return owner
}
