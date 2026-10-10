import { computed, onScopeDispose, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import { newSecretID, type SecretCommand, type SecretMetadata } from '../api/project-secrets'
import { useSession, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type ReadTask =
  | Readonly<{ kind: 'list'; cursor?: string }>
  | Readonly<{ kind: 'detail'; id: string; adopt?: boolean }>
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration &&
  a.identity.userID === b.identity.userID &&
  a.identity.sessionID === b.identity.sessionID &&
  a.identity.epoch === b.identity.epoch
const sameIdentity = (a: Context['identity'] | null, b: Context['identity'] | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
export function secretMessage(error: unknown): string {
  const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
  if (e.kind === 'invalid-input')
    return '请检查名称、描述和值的格式与长度。值输入不会保留，请重新输入。'
  if (e.problem?.code === 'VERSION_CONFLICT')
    return '版本已变化。请重新读取并明确采用当前版本，再重新输入值。'
  if (e.problem?.code === 'IDEMPOTENCY_KEY_REUSED')
    return '原请求标识发生冲突，不能重发。请重新读取当前信息后明确放弃本地追踪。'
  if (e.problem?.status === 401) return '当前会话已失效，请重新确认身份。'
  if (e.problem?.status === 403 || e.problem?.status === 404)
    return '当前 Secret 不可访问，原展示已清除。'
  if (e.problem?.code === 'NAME_CONFLICT') return '名称已被占用，请修改名称。'
  if (e.kind === 'busy') return '正在等待原请求实际结束，请稍后再操作。'
  return '请求未能完整确认。请查看命令状态或重新读取；不会自动重发。'
}
export function useProjectSecrets(
  location: Readonly<Ref<string>>,
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const scope = shallowRef<Context | null>(null)
  const list = shallowReactive<{
    phase: 'loading' | 'ready' | 'error' | 'unavailable'
    items: readonly SecretMetadata[]
    cursor?: string
    message: string
  }>({ phase: 'loading', items: [], message: '' })
  const detail = shallowReactive<{
    phase: 'idle' | 'loading' | 'ready' | 'error' | 'unavailable'
    value: SecretMetadata | null
    message: string
  }>({ phase: 'idle', value: null, message: '' })
  const editor = shallowReactive({
    open: false,
    kind: 'create' as 'create' | 'update' | 'delete',
    name: '',
    description: '',
    baseline: null as SecretMetadata | null,
    conflict: false,
    clearValue: 0,
  })
  const state = shallowReactive({ message: '', saved: false, leaveOpen: false, active: false })
  let generation = 0,
    queue: ReadTask[] = [],
    disposed = false
  let resolveLeave: ((allowed: boolean) => void) | null = null
  const context = computed(() => {
    const current = workspace.currentReadContext.value
    return location.value === workspace.paths.value.home + '/settings/secrets' ? current : null
  })
  const visible = computed(() => sameContext(scope.value, context.value))
  const busy = computed(() => auth.state.busy || state.active)
  const progress = computed(() => {
    const p = auth.secrets.progress,
      current = context.value
    return current &&
      p &&
      sameIdentity(p.identity, current.identity) &&
      p.target.projectID === current.projectID
      ? p
      : null
  })
  const unresolved = computed(
    () => progress.value?.phase === 'uncertain' || progress.value?.phase === 'submitting',
  )
  const canWrite = computed(
    () =>
      visible.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      !workspace.readOnly.value &&
      !busy.value &&
      !unresolved.value,
  )
  const dirty = computed(() => editor.open || unresolved.value)
  function emptyList() {
    list.items = []
    list.cursor = undefined
  }
  function emptyDetail() {
    detail.value = null
    detail.phase = 'idle'
    detail.message = ''
  }
  function clearEditor() {
    editor.open = false
    editor.name = ''
    editor.description = ''
    editor.baseline = null
    editor.conflict = false
    ++editor.clearValue
  }
  function retireRead() {
    ++generation
    queue = []
    auth.secrets.abandonRead()
  }
  function protectedFailure(error: unknown) {
    const status = error instanceof AccountFailure ? error.problem?.status : undefined
    if (status === 401 || status === 403 || status === 404) {
      emptyList()
      emptyDetail()
      list.phase = 'unavailable'
      clearEditor()
      return true
    }
    return false
  }
  function enqueue(task: ReadTask) {
    if (!visible.value) return
    queue.push(task)
    void drain()
  }
  async function drain() {
    if (disposed || busy.value || !visible.value || queue.length === 0) return
    const task = queue.shift()!,
      captured = scope.value!,
      own = generation
    const current = () => !disposed && own === generation && sameContext(captured, context.value)
    state.active = true
    if (task.kind === 'list') {
      list.phase = 'loading'
      list.message = ''
    } else {
      detail.phase = 'loading'
      detail.value = null
      detail.message = ''
    }
    try {
      if (task.kind === 'list') {
        const page = await auth.secrets.list(captured.projectID, {
          limit: 50,
          ...(task.cursor ? { cursor: task.cursor } : {}),
        })
        if (!current()) return
        if (task.cursor) {
          const ids = new Set(list.items.map((v) => v.id)),
            names = new Set(list.items.map((v) => v.name))
          if (
            page.items.some((v) => ids.has(v.id) || names.has(v.name)) ||
            (list.items.length &&
              page.items.length &&
              list.items[list.items.length - 1]!.name >= page.items[0]!.name)
          )
            throw new AccountFailure('invalid-response')
        }
        list.items = Object.freeze(task.cursor ? [...list.items, ...page.items] : [...page.items])
        list.cursor = page.next_cursor
        list.phase = 'ready'
      } else {
        const value = await auth.secrets.get(captured.projectID, task.id)
        if (!current()) return
        const receipt = progress.value?.receipt
        if (
          receipt &&
          'variable' in receipt &&
          receipt.variable.id === value.id &&
          BigInt(value.version) < BigInt(receipt.variable.version)
        )
          throw new AccountFailure('invalid-response')
        detail.value = value
        detail.phase = 'ready'
        // A fresh DTO is displayed for review; it never silently rebases an existing edit.
      }
    } catch (error) {
      if (!current()) return
      const unavailable = protectedFailure(error)
      if (task.kind === 'list') {
        emptyList()
        list.phase = unavailable ? 'unavailable' : 'error'
        list.message = secretMessage(error)
      } else {
        detail.value = null
        detail.phase = unavailable ? 'unavailable' : 'error'
        detail.message = secretMessage(error)
      }
    } finally {
      state.active = false
      void drain()
    }
  }
  const stops = [
    watch(
      context,
      (next) => {
        if (sameContext(scope.value, next)) return
        retireRead()
        scope.value = next
        emptyList()
        emptyDetail()
        clearEditor()
        state.message = ''
        state.saved = false
        if (next) {
          list.phase = 'loading'
          queue.push({ kind: 'list' })
          void drain()
        }
      },
      { immediate: true, flush: 'sync' },
    ),
    watch(
      () => auth.state.busy,
      () => void drain(),
    ),
  ]
  function refresh() {
    if (busy.value || !visible.value) return
    emptyList()
    enqueue({ kind: 'list' })
  }
  function select(row: SecretMetadata) {
    if (!busy.value && visible.value && !editor.open) enqueue({ kind: 'detail', id: row.id })
  }
  function open(kind: 'create' | 'update' | 'delete') {
    if (!canWrite.value || (kind !== 'create' && detail.phase !== 'ready')) return
    const baseline = kind === 'create' ? null : detail.value
    editor.kind = kind
    editor.baseline = baseline
    editor.name = baseline?.name ?? ''
    editor.description = baseline?.description ?? ''
    editor.conflict = false
    editor.open = true
    ++editor.clearValue
    state.message = ''
    state.saved = false
  }
  async function refreshConfirmed(target: string, deleted: boolean) {
    emptyList()
    emptyDetail()
    clearEditor()
    if (!deleted) enqueue({ kind: 'detail', id: target })
    enqueue({ kind: 'list' })
  }
  async function submit(value: string | undefined) {
    ++editor.clearValue
    if (!canWrite.value || !editor.open || editor.conflict) return
    const captured = scope.value!,
      own = generation,
      baseline = editor.baseline
    let command: SecretCommand | null =
      editor.kind === 'create'
        ? {
            kind: 'create',
            projectID: captured.projectID,
            request: {
              variable_id: newSecretID(),
              name: editor.name,
              description: editor.description,
              value: value ?? '',
            },
          }
        : editor.kind === 'delete' && baseline
          ? {
              kind: 'delete',
              projectID: captured.projectID,
              targetID: baseline.id,
              expectedVersion: baseline.version,
            }
          : baseline
            ? {
                kind: 'update',
                projectID: captured.projectID,
                targetID: baseline.id,
                expectedVersion: baseline.version,
                request: {
                  ...(editor.name !== baseline.name ? { name: editor.name } : {}),
                  ...(editor.description !== baseline.description
                    ? { description: editor.description }
                    : {}),
                  ...(value === undefined ? {} : { value }),
                },
              }
            : null
    value = undefined
    if (!command) return
    const current = () => !disposed && own === generation && sameContext(captured, context.value)
    try {
      const attempt = auth.secrets.execute(command)
      command = null
      const receipt = await attempt
      if (!current()) return
      state.saved = true
      state.message = '命令已确认，正在重新读取当前信息。'
      await refreshConfirmed(
        'variable' in receipt ? receipt.variable.id : receipt.deleted.id,
        'deleted' in receipt,
      )
    } catch (error) {
      if (!current()) return
      state.message = secretMessage(error)
      if (error instanceof AccountFailure && error.problem?.code === 'VERSION_CONFLICT')
        editor.conflict = true
      protectedFailure(error)
    } finally {
      command = null
    }
  }
  async function lookup() {
    if (busy.value || !visible.value || progress.value?.phase !== 'uncertain') return
    const captured = scope.value!,
      own = generation
    try {
      const result = await auth.secrets.lookup()
      if (disposed || own !== generation || !sameContext(captured, context.value)) return
      if (result.status === 'committed') {
        state.saved = true
        state.message = '已查到此命令的历史回执；正在重新读取当前信息。'
        await refreshConfirmed(
          'variable' in result.receipt ? result.receipt.variable.id : result.receipt.deleted.id,
          'deleted' in result.receipt,
        )
      } else state.message = '尚未观察到回执，这不表示回滚或未执行。可稍后继续查证。'
    } catch (error) {
      if (own === generation && sameContext(captured, context.value)) {
        state.message = secretMessage(error)
        protectedFailure(error)
      }
    }
  }
  function adopt() {
    if (
      busy.value ||
      !editor.conflict ||
      detail.phase !== 'ready' ||
      !detail.value ||
      detail.value === editor.baseline ||
      detail.value.id !== editor.baseline?.id
    )
      return
    editor.baseline = detail.value
    editor.conflict = false
    ++editor.clearValue
    state.message = '已采用重新读取的版本。请核对修改并重新输入需要替换的值。'
  }
  function confirmLeave(): Promise<boolean> {
    if (!dirty.value) return Promise.resolve(true)
    if (resolveLeave) return Promise.resolve(false)
    state.leaveOpen = true
    return new Promise((resolve) => {
      resolveLeave = resolve
    })
  }
  function resolveNavigation(allowed: boolean) {
    state.leaveOpen = false
    if (allowed) {
      retireRead()
      clearEditor()
      auth.secrets.abandon()
    }
    const resolve = resolveLeave
    resolveLeave = null
    resolve?.(allowed)
  }
  async function closeEditor() {
    if (await confirmLeave()) clearEditor()
  }
  function dispose() {
    disposed = true
    stops.forEach((stop) => stop())
    retireRead()
    clearEditor()
    resolveLeave?.(false)
    resolveLeave = null
  }
  onScopeDispose(dispose)
  return {
    list,
    detail,
    editor,
    state,
    visible,
    busy,
    progress,
    unresolved,
    canWrite,
    dirty,
    refresh,
    select,
    open,
    submit,
    lookup,
    adopt,
    confirmLeave,
    resolveNavigation,
    closeEditor,
    more: () => {
      if (!busy.value && list.cursor) enqueue({ kind: 'list', cursor: list.cursor })
    },
    reread: () => {
      if (!busy.value && editor.baseline)
        enqueue({ kind: 'detail', id: editor.baseline.id, adopt: true })
    },
    readOwner: () => workspace.readCurrent(),
  }
}
