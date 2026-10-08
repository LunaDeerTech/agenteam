import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProjectAddress,
  captureProjectUpdate,
  type Project,
  type ProjectLifecycle,
  type ProjectListItem,
  type ProjectUpdate,
} from '../api/project-owner'
import { projectRoute } from '../router/auth'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

type Phase = 'inactive' | 'loading' | 'current' | 'read-error' | 'unavailable' | 'checking'
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const asFailure = (error: unknown) =>
  error instanceof AccountFailure ? error : new AccountFailure('transport')
const denied = (error: AccountFailure) => [401, 403, 404].includes(error.problem?.status ?? 0)
function explanation(error: AccountFailure) {
  if (error.problem?.status === 401) return '当前会话已失效，请重新登录。'
  if (error.problem?.status === 403) return '当前身份无权访问此项目。'
  if (error.problem?.status === 404) return '项目不存在或旧名称链接已失效，请返回项目列表。'
  if (error.problem?.commit_state === 'unknown') return '读取结果不确定，请明确重新读取。'
  return '未取得完整的当前信息，请在请求结束后明确重读。'
}

// App owns this single workspace across temporary RouterView removal during checking.
// Session alone retains original request bytes, keys and CSRF materials.
export function createProjectWorkspace(
  auth: SessionController = useSession(),
  replaceRoute: (path: string) => Promise<unknown> = async () => undefined,
) {
  const detail = shallowReactive<{ phase: Phase; project: Project | null; message: string }>({
    phase: 'inactive',
    project: null,
    message: '',
  })
  const list = shallowReactive<{
    phase: Phase
    items: readonly ProjectListItem[]
    message: string
    stale: boolean
    filter: 'all' | ProjectLifecycle
    limit: 25 | 50 | 100
    page: number
  }>({ phase: 'inactive', items: [], message: '', stale: false, filter: 'all', limit: 25, page: 1 })
  const draft = reactive({ name: '', description: '' })
  const baseline = reactive({ name: '', description: '' })
  const editor = reactive<{
    ready: boolean
    version: string
    conflict: boolean
    requiresRead: boolean
    fields: { name?: string; description?: string }
    message: string
  }>({ ready: false, version: '', conflict: false, requiresRead: false, fields: {}, message: '' })
  const confirmation = reactive({ open: false, title: '', message: '', label: '放弃并离开' })
  const writeMessage = ref(''),
    feedback = ref<'idle' | 'loading' | 'success'>('idle')
  const progress = computed(() => auth.projects.progress)
  const visible = computed(
    () =>
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      !!auth.personalContext.identity,
  )
  const ownerName = computed(() => (visible.value ? (auth.state.user?.username ?? '') : ''))
  const paths = computed(() => {
    if (!visible.value || !detail.project) return { home: '', settings: '' }
    try {
      const a = captureProjectAddress({
        username: ownerName.value,
        project_name: detail.project.normalized_name,
      })
      const home = `/${a.username}/${a.project_name}`
      return { home, settings: home + '/settings/general' }
    } catch {
      return { home: '', settings: '' }
    }
  })
  const blocked = computed(() => auth.state.busy || confirmation.open || !visible.value)
  const changed = computed(
    () =>
      editor.ready && (draft.name !== baseline.name || draft.description !== baseline.description),
  )
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const dirty = computed(() => changed.value || unsettled.value || editor.conflict)
  const readOnly = computed(
    () =>
      detail.project?.lifecycle !== 'active' ||
      detail.phase !== 'current' ||
      unsettled.value ||
      editor.conflict ||
      editor.requiresRead,
  )
  const canSave = computed(() => !blocked.value && !readOnly.value && editor.ready && changed.value)
  const canLookup = computed(
    () => !blocked.value && progress.value?.phase === 'uncertain' && progress.value.contextValid,
  )
  const canReplay = computed(() => !blocked.value && !!progress.value?.canRetryOriginal)
  const canAdoptCurrent = computed(
    () =>
      !blocked.value &&
      !unsettled.value &&
      !editor.requiresRead &&
      detail.phase === 'current' &&
      !!detail.project,
  )
  const previousCursors = ref<(string | undefined)[]>([]),
    nextCursor = ref<string | null>(null)
  const hasPrevious = computed(() => previousCursors.value.length > 0)
  const hasNext = computed(() => nextCursor.value !== null)
  let boundID: string | null = null
  let requestedPage: {
    cursor: string | undefined
    stack: (string | undefined)[]
    page: number
  } | null = null
  let needsIdentityRead = false
  let detailBeforeCheck: Phase = 'inactive',
    listBeforeCheck: Phase = 'inactive'
  let currentCursor: string | undefined
  let identity = auth.personalContext.identity,
    username = auth.state.user?.username ?? '',
    generation = 0,
    readGeneration = 0
  let route = '',
    mode: 'none' | 'list' | 'detail' = 'none',
    needsInitial = false,
    disposed = false,
    reading = false
  let canonicalNavigation = '',
    resolveConfirmation: ((value: boolean) => void) | null = null
  let successTimer: ReturnType<typeof setTimeout> | undefined
  const live = (own: number, captured: PersonalIdentity | null) =>
    !disposed &&
    own === generation &&
    visible.value &&
    same(captured, auth.personalContext.identity)
  function clearEditor() {
    Object.assign(draft, { name: '', description: '' })
    Object.assign(baseline, { name: '', description: '' })
    Object.assign(editor, {
      ready: false,
      version: '',
      conflict: false,
      requiresRead: false,
      fields: {},
      message: '',
    })
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const done = resolveConfirmation
    resolveConfirmation = null
    done?.(value)
  }
  function requestConfirmation(title: string, message: string, label: string) {
    if (resolveConfirmation) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message, label })
    return new Promise<boolean>((resolve) => {
      resolveConfirmation = resolve
    })
  }
  function retireRead() {
    ++readGeneration
    reading = false
    auth.projects.abandonRead()
  }
  function resetTracking() {
    auth.projects.abandon()
    clearEditor()
    writeMessage.value = ''
    feedback.value = 'idle'
    clearTimeout(successTimer)
  }
  function invalidate() {
    boundID = null
    requestedPage = null
    needsIdentityRead = false
    ++generation
    retireRead()
    resetTracking()
    finishConfirmation(false)
    Object.assign(detail, { phase: 'inactive', project: null, message: '' })
    Object.assign(list, { phase: 'inactive', items: [], message: '', stale: false, page: 1 })
    currentCursor = undefined
    nextCursor.value = null
    previousCursors.value = []
    needsInitial = mode !== 'none'
  }
  function applyCurrent() {
    if (!canAdoptCurrent.value || !detail.project) return
    if (progress.value?.phase === 'rejected') {
      auth.projects.editRejected()
      writeMessage.value = ''
    }
    const value = detail.project
    Object.assign(draft, { name: value.name, description: value.description })
    Object.assign(baseline, draft)
    Object.assign(editor, {
      ready: true,
      version: value.version,
      conflict: false,
      requiresRead: false,
      fields: {},
      message: '',
    })
  }
  async function adoptCurrent() {
    if (!canAdoptCurrent.value || !detail.project) return
    const value = detail.project,
      own = generation,
      captured = auth.personalContext.identity
    if (
      dirty.value &&
      !(await requestConfirmation(
        '采用当前项目值？',
        '当前未保存输入将被替换为本次完整读取的值。不会重发或合并旧命令。',
        '采用当前值',
      ))
    )
      return
    if (live(own, captured) && detail.project === value && canAdoptCurrent.value) applyCurrent()
  }
  function accept(value: Project, adopt: boolean) {
    if (boundID !== null && value.id !== boundID) throw new AccountFailure('invalid-response')
    boundID = value.id
    if (
      detail.project &&
      (value.id !== detail.project.id || value.owner_user_id !== detail.project.owner_user_id)
    )
      throw new AccountFailure('invalid-response')
    if (
      progress.value?.receipt?.id === value.id &&
      BigInt(value.version) < BigInt(progress.value.receipt.version)
    )
      throw new AccountFailure('invalid-response')
    detail.project = value
    detail.phase = 'current'
    detail.message = ''
    if (adopt || !editor.ready) {
      Object.assign(draft, { name: value.name, description: value.description })
      Object.assign(baseline, draft)
      Object.assign(editor, {
        ready: true,
        version: value.version,
        conflict: false,
        requiresRead: false,
        fields: {},
        message: '',
      })
    } else if (
      value.version !== editor.version ||
      value.name !== baseline.name ||
      value.description !== baseline.description
    ) {
      editor.conflict = true
      editor.message = '当前版本已变化，原草稿及版本保留。请核对当前值后明确重新编辑。'
    }
  }
  async function canonicalize() {
    const address = projectRoute(route)
    if (!address || !paths.value.home) return
    const path =
      paths.value.home + (address.suffix === '/settings' ? '/settings/general' : address.suffix)
    if (route === path) return
    canonicalNavigation = path
    try {
      await replaceRoute(path)
    } finally {
      canonicalNavigation = ''
    }
  }
  function readFailed(error: unknown) {
    const failure = asFailure(error)
    detail.phase = denied(failure) ? 'unavailable' : 'read-error'
    detail.message = explanation(failure)
    if (denied(failure)) {
      detail.project = null
      clearEditor()
    }
    if (progress.value?.phase === 'confirmed') {
      editor.requiresRead = true
      writeMessage.value = '命令已确认；当前信息读取失败。请显式重读当前信息。'
    }
  }
  async function loadDetail() {
    const address = projectRoute(route)
    if (mode !== 'detail' || !address || !visible.value || auth.state.busy || reading) return
    needsInitial = false
    const own = generation,
      captured = auth.personalContext.identity,
      read = ++readGeneration
    reading = true
    detail.phase = 'loading'
    detail.message = ''
    try {
      const resolved = await auth.projects.resolve({
        username: address.username,
        project_name: address.project_name,
      })
      if (!live(own, captured) || read !== readGeneration) return
      if (resolved.lifecycle === 'deleting') {
        detail.phase = 'unavailable'
        detail.project = null
        detail.message = '项目正在删除，内容不可用。请返回项目列表。'
        return
      }
      // Resolve is only a candidate. The stable ID Get independently authorizes publication.
      const value = await auth.projects.get(resolved.id)
      if (!live(own, captured) || read !== readGeneration) return
      accept(value, true)
      await canonicalize()
    } catch (error) {
      if (live(own, captured) && read === readGeneration) readFailed(error)
    } finally {
      if (own === generation && read === readGeneration) reading = false
    }
  }
  async function readCurrent(adopt = false) {
    if (!visible.value || auth.state.busy || reading || mode !== 'detail') return
    if (!boundID) return loadDetail()
    const own = generation,
      captured = auth.personalContext.identity,
      target = boundID,
      read = ++readGeneration
    reading = true
    detail.phase = 'loading'
    detail.message = ''
    try {
      const value = await auth.projects.get(target)
      if (!live(own, captured) || read !== readGeneration) return
      accept(value, adopt || (editor.requiresRead && !editor.ready))
      editor.requiresRead = false
      await canonicalize()
      if (progress.value?.phase === 'confirmed') writeMessage.value = '命令已确认，已读取当前项目。'
    } catch (error) {
      if (live(own, captured) && read === readGeneration) readFailed(error)
    } finally {
      if (own === generation && read === readGeneration) reading = false
    }
  }
  async function loadList(cursor: string | undefined, stack: (string | undefined)[], page: number) {
    if (mode !== 'list' || !visible.value || auth.state.busy || reading) return
    needsInitial = false
    const own = generation,
      captured = auth.personalContext.identity,
      read = ++readGeneration
    requestedPage = { cursor, stack: [...stack], page }
    reading = true
    list.phase = 'loading'
    list.message = ''
    list.stale = list.items.length > 0
    try {
      const value = await auth.projects.list({
        limit: list.limit,
        ...(list.filter === 'all' ? {} : { lifecycle: [list.filter] }),
        ...(cursor === undefined ? {} : { cursor }),
      })
      if (!live(own, captured) || read !== readGeneration) return
      Object.assign(list, { phase: 'current', items: value.items, message: '', stale: false, page })
      currentCursor = cursor
      previousCursors.value = stack
      nextCursor.value = value.next_cursor
    } catch (error) {
      if (!live(own, captured) || read !== readGeneration) return
      const failure = asFailure(error)
      list.phase = denied(failure) ? 'unavailable' : 'read-error'
      list.message = explanation(failure)
      if (denied(failure)) {
        list.items = []
        list.stale = false
        nextCursor.value = null
        previousCursors.value = []
        currentCursor = undefined
      }
    } finally {
      if (own === generation && read === readGeneration) reading = false
    }
  }
  function retryList() {
    return requestedPage
      ? loadList(requestedPage.cursor, [...requestedPage.stack], requestedPage.page)
      : loadList(currentCursor, [...previousCursors.value], list.page)
  }
  function fromFirst() {
    if (blocked.value) return
    ++generation
    retireRead()
    list.items = []
    list.stale = false
    list.page = 1
    currentCursor = undefined
    previousCursors.value = []
    nextCursor.value = null
    return loadList(undefined, [], 1)
  }
  function setFilter(value: string) {
    if (
      blocked.value ||
      !['all', 'active', 'archiving', 'archived', 'deleting'].includes(value) ||
      value === list.filter
    )
      return
    list.filter = value as typeof list.filter
    return fromFirst()
  }
  function setLimit(value: string) {
    if (blocked.value || !['25', '50', '100'].includes(value) || Number(value) === list.limit)
      return
    list.limit = Number(value) as typeof list.limit
    return fromFirst()
  }
  function next() {
    if (!blocked.value && nextCursor.value !== null)
      return loadList(nextCursor.value, [...previousCursors.value, currentCursor], list.page + 1)
  }
  function previous() {
    if (blocked.value || !hasPrevious.value) return
    const stack = [...previousCursors.value],
      cursor = stack.pop()
    return loadList(cursor, stack, list.page - 1)
  }
  function rowPath(item: ProjectListItem) {
    if (!visible.value || item.lifecycle === 'deleting') return null
    try {
      const a = captureProjectAddress({ username: ownerName.value, project_name: item.name })
      return `/${a.username}/${a.project_name}`
    } catch {
      return null
    }
  }
  function cancelEdits() {
    if (!blocked.value && !unsettled.value) applyCurrent()
  }
  async function saved(action: () => Promise<unknown>) {
    const own = generation,
      captured = auth.personalContext.identity
    feedback.value = 'loading'
    writeMessage.value = ''
    editor.fields = {}
    try {
      await action()
      if (!live(own, captured)) return
      if (progress.value?.phase === 'confirmed') {
        feedback.value = 'success'
        clearTimeout(successTimer)
        successTimer = setTimeout(() => {
          feedback.value = 'idle'
        }, 2400)
        editor.requiresRead = true
        writeMessage.value = '命令已确认，正在读取当前项目。'
        await readCurrent(true)
      } else if (progress.value?.observation === 'in_progress')
        writeMessage.value = '原命令仍在处理中，请稍后明确查证；不会自动轮询或重放。'
      else if (progress.value?.observation === 'not_observed')
        writeMessage.value = '此次未观察到原命令，不代表从未提交或已回滚。'
    } catch (error) {
      if (!live(own, captured)) return
      const failure = asFailure(error),
        p = failure.problem
      feedback.value = 'idle'
      if (denied(failure)) {
        detail.phase = 'unavailable'
        detail.project = null
        clearEditor()
        detail.message = explanation(failure)
      }
      if (progress.value?.phase === 'uncertain')
        writeMessage.value =
          p?.code === 'IDEMPOTENCY_KEY_REUSED'
            ? '原请求键发生冲突，结果不确定。不能重放；可查证或放弃本地追踪。'
            : '结果不确定。可查证原命令或按原请求重放；放弃本地追踪不会撤销服务器命令。'
      else {
        writeMessage.value = '本次请求明确拒绝，输入已保留。请核对后重新保存。'
        if (
          p?.code === 'VERSION_CONFLICT' ||
          p?.code === 'PROJECT_NOT_ACTIVE' ||
          p?.code === 'INVALID_STATE'
        ) {
          editor.conflict = true
          editor.requiresRead = true
          editor.message = '当前项目已变化，请显式重读当前值；原草稿及版本保留。'
        }
        for (const field of p?.field_errors ?? []) {
          if (field.path === '/name')
            editor.fields.name =
              field.code === 'NAME_TAKEN' ? '此名称已被占用。' : '请检查项目名称。'
          if (field.path === '/description') editor.fields.description = '请检查项目描述。'
        }
      }
    } finally {
      if (live(own, captured) && feedback.value === 'loading') feedback.value = 'idle'
    }
  }
  async function save() {
    if (!canSave.value || !detail.project) return
    let input: ProjectUpdate
    try {
      input = captureProjectUpdate({
        expected_version: editor.version,
        ...(draft.name !== baseline.name ? { name: draft.name } : {}),
        ...(draft.description !== baseline.description ? { description: draft.description } : {}),
      })
    } catch {
      editor.fields = {
        name: '名称为 1–64 位英文字母、数字、点、下划线或连字符，不能为 . 或 ..。',
        description: '描述最多 8192 UTF-8 字节，不允许制表符与换行之外的控制字符。',
      }
      return
    }
    const own = generation,
      captured = auth.personalContext.identity
    if (
      input.name !== undefined &&
      !(await requestConfirmation(
        '确认更改项目名称？',
        '旧名称链接将失效，不提供别名或自动跳转。保存后按当前项目的信息更新地址。',
        '确认保存',
      ))
    )
      return
    if (!live(own, captured) || !detail.project || auth.state.busy) return
    if (progress.value?.phase === 'rejected') auth.projects.editRejected()
    await saved(() => auth.projects.startUpdate(detail.project!.id, input))
  }
  function checkOriginal() {
    if (canLookup.value) return saved(() => auth.projects.checkOriginal())
  }
  function replayOriginal() {
    if (canReplay.value) return saved(() => auth.projects.retryOriginal())
  }
  async function abandon() {
    if (blocked.value) return
    if (
      await requestConfirmation(
        '放弃本地追踪？',
        '仅丢弃本地草稿和原命令追踪；此前请求仍可能生效，不会撤销服务器命令。',
        '放弃本地追踪',
      )
    ) {
      resetTracking()
      if (detail.project) {
        editor.requiresRead = true
        writeMessage.value = '已放弃本地追踪，请显式重读当前信息。'
      }
    }
  }
  async function confirmLeave(target?: string) {
    if (target && target === canonicalNavigation) return true
    if (!dirty.value) return !confirmation.open
    if (
      !(await requestConfirmation(
        '放弃项目修改？',
        '将放弃本地草稿和原命令追踪；此前请求仍可能生效，离开不会撤销服务器命令。',
        '放弃并离开',
      ))
    )
      return false
    resetTracking()
    return true
  }
  function afterNavigation(to: string, _from = '') {
    if (disposed) return
    const old = projectRoute(route),
      next = projectRoute(to)
    route = to
    if (to === canonicalNavigation && detail.project) return
    if (
      old &&
      next &&
      old.username === next.username &&
      old.project_name === next.project_name &&
      mode === 'detail'
    )
      return
    ++generation
    retireRead()
    resetTracking()
    boundID = null
    Object.assign(detail, { phase: 'inactive', project: null, message: '' })
    Object.assign(list, { phase: 'inactive', items: [], message: '', stale: false, page: 1 })
    currentCursor = undefined
    nextCursor.value = null
    previousCursors.value = []
    requestedPage = null
    needsIdentityRead = false
    mode = to === '/projects' ? 'list' : next ? 'detail' : 'none'
    needsInitial = mode !== 'none'
    startInitial()
  }
  function startInitial() {
    if (!needsInitial || !visible.value || auth.state.busy || disposed) return
    if (mode === 'list') void loadList(undefined, [], 1)
    if (mode === 'detail') void loadDetail()
  }
  const stop = watch(
    () =>
      [
        auth.personalContext.identity,
        auth.personalContext.phase,
        auth.state.busy,
        auth.state.user?.username,
      ] as const,
    ([nextIdentity, phase, busy, nextUsername]) => {
      if (!same(identity, nextIdentity)) {
        // null/null is stable during the initial anonymous check.
        if (identity !== null || nextIdentity !== null) invalidate()
        identity = nextIdentity
      }
      if (phase !== 'current' || !visible.value) {
        if (mode === 'detail' && detail.phase !== 'checking') {
          detailBeforeCheck = detail.phase
          detail.phase = 'checking'
        }
        if (mode === 'list' && list.phase !== 'checking') {
          listBeforeCheck = list.phase
          list.phase = 'checking'
        }
        return
      }
      if (detail.phase === 'checking') detail.phase = detailBeforeCheck
      if (list.phase === 'checking') list.phase = listBeforeCheck
      const changedUsername = username !== (nextUsername ?? '')
      username = nextUsername ?? ''
      if (changedUsername && boundID && mode === 'detail') needsIdentityRead = true
      if (!busy && needsIdentityRead && boundID && mode === 'detail') {
        needsIdentityRead = false
        void readCurrent()
      }
      if (!busy) startInitial()
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
  function dispose() {
    disposed = true
    stop()
    invalidate()
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload)
  }
  return {
    visible,
    detail,
    ownerName,
    paths,
    list,
    hasPrevious,
    hasNext,
    setFilter,
    setLimit,
    previous,
    next,
    retryList,
    fromFirst,
    rowPath,
    draft,
    editor,
    changed,
    dirty,
    readOnly,
    blocked,
    canSave,
    progress,
    canLookup,
    canReplay,
    canAdoptCurrent,
    writeMessage,
    feedback,
    save,
    cancelEdits,
    readCurrent,
    adoptCurrent,
    checkOriginal,
    replayOriginal,
    abandon,
    confirmation,
    finishConfirmation,
    confirmLeave,
    afterNavigation,
    dispose,
  }
}
export type ProjectWorkspace = ReturnType<typeof createProjectWorkspace>
export const projectWorkspaceKey: InjectionKey<ProjectWorkspace> = Symbol('project-workspace')
export function useProjectWorkspace(): ProjectWorkspace {
  const owner = inject(projectWorkspaceKey)
  if (!owner) throw new Error('Project workspace owner is missing')
  return owner
}
