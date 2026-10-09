import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProjectVariableCommand,
  newProjectVariableID,
  type ProjectVariable,
  type ProjectVariableCommand,
  type ProjectVariableReceipt,
  type ProjectVariableSummary,
  type ProjectVariableUpdate,
} from '../api/project-variables'
import { projectRoute } from '../router/auth'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  a.projectID === b.projectID &&
  sameIdentity(a.identity, b.identity) &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
const failure = (e: unknown) => (e instanceof AccountFailure ? e : new AccountFailure('transport'))
const blank = () => ({ name: '', description: '', value: '' })
function explanation(error: AccountFailure) {
  switch (error.problem?.code) {
    case 'VERSION_CONFLICT':
      return '版本已变化。草稿已保留，请读取当前信息后明确选择采用或保留输入。'
    case 'CURSOR_INVALID':
    case 'CURSOR_STALE':
      return '分页位置已失效。已保留旧观察，请从第一页重新读取。'
    case 'PROJECT_NOT_ACTIVE':
      return '项目当前不允许这项操作；历史回执与当前状态分别显示。'
    case 'NOT_FOUND':
      return '当前对象不存在或不可访问；这不证明原操作已执行。'
    case 'FORBIDDEN':
      return '当前身份无权操作此项目。'
    case 'SESSION_REVOKED':
    case 'UNAUTHENTICATED':
    case 'CSRF_FAILED':
      return '当前登录上下文已失效，请重新检查会话。'
    case 'IDEMPOTENCY_KEY_REUSED':
      return '原请求标识发生冲突，不能重放或自动换标识。'
    case 'RESOURCE_BUSY':
      return '名称已占用或项目容量暂不可用，请检查输入和当前信息。'
  }
  return error.kind === 'invalid-input'
    ? '请按字段规则检查输入，内容不会自动修剪或改写。'
    : '未取得可确认的完整结果。请在当前请求结束后明确读取或查询原操作。'
}
export function createProjectVariables(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const route = ref(''),
    disposed = ref(false),
    boundID = ref<string | null>(null)
  const scope = computed(() => projectRoute(route.value)?.suffix === '/settings/variables')
  const context = computed(() =>
    scope.value && !disposed.value ? workspace.currentReadContext.value : null,
  )
  const visible = computed(
    () =>
      !!context.value &&
      ['active', 'archiving', 'archived'].includes(workspace.detail.project?.lifecycle ?? ''),
  )
  const currentProject = computed(() => (visible.value ? workspace.detail.project : null))
  const progress = computed(() => {
    const p = auth.projectVariables.progress
    return p?.projectID === boundID.value ? p : null
  })
  const pending = computed(
    () =>
      !!progress.value &&
      progress.value.phase !== 'confirmed' &&
      progress.value.phase !== 'rejected',
  )
  const canMutate = computed(() => visible.value && currentProject.value?.lifecycle === 'active')
  const confirmation = reactive({ open: false, title: '', message: '', label: '' })
  let confirmResult: ((accepted: boolean) => void) | null = null
  const sessionBusy = computed(() => auth.state.busy)
  const blocked = computed(() => !visible.value || auth.state.busy || confirmation.open)
  const page = shallowReactive({
    phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error',
    items: [] as readonly ProjectVariableSummary[],
    stale: false,
    message: '',
    cursorInvalid: false,
    page: 1,
    hasPrevious: false,
    hasNext: false,
  })
  let cursor: string | undefined, nextCursor: string | undefined
  let previous: (string | undefined)[] = [],
    readRevision = 0,
    needsInitial = false
  const editor = shallowReactive({
    open: false,
    mode: 'create' as 'create' | 'edit',
    phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error',
    targetID: '',
    version: '',
    original: null as ProjectVariable | null,
    review: null as ProjectVariable | null,
    requiresRead: false,
    conflict: false,
    missing: false,
    message: '',
    fields: {} as Record<string, string>,
  })
  const draft = reactive(blank()),
    baseline = reactive(blank())
  const changed = computed(
    () =>
      editor.open &&
      (editor.mode === 'create'
        ? Object.values(draft).some(Boolean)
        : ['name', 'description', 'value'].some(
            (k) => draft[k as keyof typeof draft] !== baseline[k as keyof typeof draft],
          )),
  )
  const dirty = computed(() => changed.value || pending.value || editor.conflict)
  const message = ref(''),
    feedback = ref<'idle' | 'loading' | 'success'>('idle')
  const canSave = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      !auth.projectVariables.progress &&
      editor.open &&
      editor.phase === 'ready' &&
      !editor.requiresRead &&
      !editor.conflict &&
      changed.value,
  )
  const canDelete = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      !auth.projectVariables.progress &&
      editor.mode === 'edit' &&
      editor.phase === 'ready' &&
      !editor.requiresRead &&
      !editor.conflict &&
      !changed.value &&
      !!editor.original,
  )
  const canLookup = computed(
    () => !blocked.value && !!progress.value?.contextValid && !progress.value.keyConflict,
  )
  const canReplay = computed(() => !blocked.value && !!progress.value?.canReplayOriginal)
  let trackedIdentity = auth.personalContext.identity,
    lastContext: Context | null = null
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const done = confirmResult
    confirmResult = null
    done?.(value)
  }
  function confirmAction(title: string, text: string, label: string) {
    if (confirmResult) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message: text, label })
    return new Promise<boolean>((resolve) => {
      confirmResult = resolve
    })
  }
  function retireReads() {
    ++readRevision
    auth.projectVariables.abandonReads()
    if (page.phase === 'loading') page.phase = 'inactive'
    if (editor.phase === 'loading') {
      editor.phase = editor.original ? 'ready' : 'error'
      editor.requiresRead = true
    }
  }
  function clearEditor() {
    Object.assign(editor, {
      open: false,
      phase: 'inactive',
      mode: 'create',
      targetID: '',
      version: '',
      original: null,
      review: null,
      requiresRead: false,
      conflict: false,
      missing: false,
      fields: {},
      message: '',
    })
    Object.assign(draft, blank())
    Object.assign(baseline, blank())
    feedback.value = 'idle'
  }
  function clearAll() {
    retireReads()
    auth.projectVariables.abandonPending()
    clearEditor()
    finishConfirmation(false)
    Object.assign(page, {
      phase: 'inactive',
      items: [],
      stale: false,
      message: '',
      cursorInvalid: false,
      page: 1,
      hasPrevious: false,
      hasNext: false,
    })
    cursor = nextCursor = undefined
    previous = []
    message.value = ''
    boundID.value = null
  }
  const live = (captured: Context, revision: number) =>
    revision === readRevision &&
    visible.value &&
    sameContext(captured, context.value) &&
    !disposed.value
  async function loadPage(position: {
    cursor?: string
    previous: (string | undefined)[]
    page: number
  }) {
    const captured = context.value
    if (blocked.value || !captured) return false
    const own = ++readRevision
    page.phase = 'loading'
    page.message = ''
    try {
      const result = await auth.projectVariables.list(captured.projectID, {
        limit: 50,
        ...(position.cursor === undefined ? {} : { cursor: position.cursor }),
      })
      if (!live(captured, own)) return false
      cursor = position.cursor
      previous = [...position.previous]
      nextCursor = result.next_cursor
      Object.assign(page, {
        phase: 'ready',
        items: result.items,
        stale: false,
        cursorInvalid: false,
        page: position.page,
        hasPrevious: previous.length > 0,
        hasNext: nextCursor !== undefined,
      })
      return true
    } catch (e) {
      if (live(captured, own)) {
        const error = failure(e)
        Object.assign(page, {
          phase: 'error',
          stale: page.items.length > 0,
          message: explanation(error),
          cursorInvalid: ['CURSOR_INVALID', 'CURSOR_STALE'].includes(error.problem?.code ?? ''),
        })
      }
      return false
    }
  }
  async function readSelected() {
    const captured = context.value,
      target = editor.targetID
    if (blocked.value || !captured || !target || editor.mode !== 'edit') return false
    const own = ++readRevision,
      preserve = changed.value || editor.conflict || editor.requiresRead || !!progress.value
    editor.phase = 'loading'
    editor.message = ''
    editor.missing = false
    try {
      const value = await auth.projectVariables.get(captured.projectID, target)
      if (!live(captured, own) || editor.targetID !== target) return false
      editor.phase = 'ready'
      editor.requiresRead = false
      if (preserve) {
        editor.review = value
        editor.conflict = true
      } else adopt(value)
      return true
    } catch (e) {
      if (live(captured, own) && editor.targetID === target) {
        editor.phase = 'error'
        editor.requiresRead = true
        editor.message = explanation(failure(e))
        editor.missing = failure(e).problem?.code === 'NOT_FOUND'
      }
      return false
    }
  }
  function adopt(value: ProjectVariable, keepDraft = false) {
    editor.original = value
    editor.review = null
    editor.version = value.version
    editor.targetID = value.id
    editor.conflict = false
    editor.requiresRead = false
    editor.missing = false
    editor.phase = 'ready'
    editor.mode = 'edit'
    Object.assign(baseline, {
      name: value.name,
      description: value.description,
      value: value.value,
    })
    if (!keepDraft) Object.assign(draft, baseline)
  }
  function adoptCurrent(keepDraft = false) {
    if (blocked.value || !editor.review || progress.value || editor.requiresRead) return false
    adopt(editor.review, keepDraft)
    return true
  }
  async function discardEditor(title = '放弃变量修改？') {
    if (
      (dirty.value || progress.value) &&
      !(await confirmAction(
        title,
        '未保存草稿和本地原操作材料将被丢弃。这不会撤销服务器操作；刷新或关闭页面也不会保留恢复材料。',
        '放弃本地材料',
      ))
    )
      return false
    auth.projectVariables.abandonPending()
    clearEditor()
    return true
  }
  async function select(targetID: string) {
    if (blocked.value || !(await discardEditor())) return false
    Object.assign(editor, { open: true, mode: 'edit', targetID })
    return readSelected()
  }
  async function createNew() {
    if (blocked.value || !canMutate.value || !(await discardEditor())) return false
    try {
      Object.assign(editor, {
        open: true,
        mode: 'create',
        targetID: newProjectVariableID(),
        phase: 'ready',
      })
      return true
    } catch {
      message.value = '无法生成安全的操作标识，请检查浏览器环境。'
      return false
    }
  }
  function command(): ProjectVariableCommand {
    const projectID = context.value!.projectID
    if (editor.mode === 'create')
      return captureProjectVariableCommand({
        kind: 'create',
        projectID,
        request: { variable_id: editor.targetID, ...draft },
      })
    const request: { name?: string; description?: string; value?: string } = {}
    for (const field of ['name', 'description', 'value'] as const)
      if (draft[field] !== baseline[field]) request[field] = draft[field]
    return captureProjectVariableCommand({
      kind: 'update',
      projectID,
      targetID: editor.targetID,
      expectedVersion: editor.version,
      request: request as ProjectVariableUpdate,
    })
  }
  function receive(receipt: ProjectVariableReceipt) {
    page.stale = true
    feedback.value = 'success'
    message.value = '原操作已确认。历史回执保留；当前目录需要明确重读。'
    if (receipt.command === 'project.variable.delete') {
      clearEditor()
      feedback.value = 'success'
    } else {
      editor.open = true
      adopt(receipt.variable)
      editor.requiresRead = true
      editor.message = '此处为历史回执内容。要开始新的修改，请结束本地追踪并重新读取当前变量。'
    }
  }
  async function mutate(value: ProjectVariableCommand) {
    const captured = context.value
    if (!captured) return false
    feedback.value = 'loading'
    message.value = ''
    editor.fields = {}
    try {
      const result = await auth.projectVariables.start(value)
      if (!sameContext(captured, context.value) || !visible.value) return false
      receive(result)
      return true
    } catch (e) {
      if (
        sameIdentity(captured.identity, auth.personalContext.identity) &&
        boundID.value === captured.projectID
      ) {
        const error = failure(e)
        feedback.value = 'idle'
        message.value = explanation(error)
        if (error.problem?.code === 'VERSION_CONFLICT') {
          editor.conflict = true
          editor.requiresRead = true
        }
        for (const field of error.problem?.field_errors ?? []) {
          const name = /^\/request\/(name|description|value)$/.exec(field.path)?.[1]
          const texts: Record<string, string> = {
            RESERVED_NAME: '该名称保留给系统使用。',
            NAME_CONFLICT: '该名称已被占用。',
            INVALID_VALUE: '请检查此字段。',
            TOO_LONG: '此字段超过允许的字节长度。',
          }
          if (name && texts[field.code])
            editor.fields = { ...editor.fields, [name]: texts[field.code]! }
        }
      }
      return false
    }
  }
  async function save() {
    if (!canSave.value) return false
    try {
      return await mutate(command())
    } catch (e) {
      editor.message = explanation(failure(e))
      return false
    }
  }
  async function deleteSelected() {
    if (!canDelete.value) return false
    const value = captureProjectVariableCommand({
        kind: 'delete',
        projectID: context.value!.projectID,
        targetID: editor.targetID,
        expectedVersion: editor.version,
      }),
      captured = context.value
    if (
      !(await confirmAction(
        '删除普通变量？',
        `从当前目录移除 ${draft.name}（${editor.targetID}，版本 ${editor.version}）。历史操作回执仍可能保留旧值。`,
        '删除变量',
      ))
    )
      return false
    if (!sameContext(captured, context.value) || !canDelete.value) return false
    return mutate(value)
  }
  async function lookupOriginal() {
    if (!canLookup.value) return false
    const captured = context.value
    try {
      const result = await auth.projectVariables.lookupOriginal()
      if (!sameContext(captured, context.value) || !visible.value) return false
      if (result.status === 'committed') receive(result.receipt)
      else
        message.value =
          result.status === 'in_progress'
            ? '已观察到原意图，尚未取得完成回执；不会自动重放。'
            : '本次未观察到原操作。这不证明未提交，也不会自动重放。'
      return true
    } catch (e) {
      if (sameContext(captured, context.value)) message.value = explanation(failure(e))
      return false
    }
  }
  async function replayOriginal() {
    if (!canReplay.value) return false
    const captured = context.value
    try {
      const result = await auth.projectVariables.replayOriginal()
      if (!sameContext(captured, context.value) || !visible.value) return false
      receive(result)
      return true
    } catch (e) {
      if (sameContext(captured, context.value)) message.value = explanation(failure(e))
      return false
    }
  }
  async function abandonPending() {
    if (blocked.value) return false
    if (
      progress.value &&
      !(await confirmAction(
        '结束本地操作追踪？',
        '将丢弃原请求的本地恢复材料，不撤销服务器操作。草稿仍保留；历史回执不会作为当前版本。',
        '结束追踪',
      ))
    )
      return false
    auth.projectVariables.abandonPending()
    feedback.value = 'idle'
    message.value = ''
    if (editor.mode === 'edit' && editor.open) editor.requiresRead = true
    return true
  }
  function editRejected() {
    if (blocked.value || !auth.projectVariables.editRejected()) return false
    feedback.value = 'idle'
    return true
  }
  async function readOriginalTarget() {
    const original = progress.value
    if (blocked.value || !original) return false
    if (
      editor.open &&
      editor.targetID !== original.targetID &&
      changed.value &&
      !(await confirmAction(
        '切换到原操作对象？',
        '将放弃另一对象的本地草稿，保留原操作历史。',
        '切换对象',
      ))
    )
      return false
    if (editor.targetID !== original.targetID) clearEditor()
    Object.assign(editor, {
      open: true,
      mode: 'edit',
      targetID: original.targetID,
      requiresRead: true,
    })
    return readSelected()
  }
  async function confirmLeave(target?: string) {
    if (target && projectRoute(target)?.path === projectRoute(route.value)?.path) return true
    // Owner canonicalization changes only this same, currently proven stable Project's address.
    if (
      target &&
      context.value?.projectID === boundID.value &&
      projectRoute(target)?.path === workspace.paths.value.home + '/settings/variables'
    )
      return true
    if (!dirty.value && !progress.value) {
      retireReads()
      return true
    }
    if (
      !(await confirmAction(
        '离开变量管理？',
        '未保存草稿和本地恢复材料将被丢弃，不撤销服务器操作。',
        '放弃并离开',
      ))
    )
      return false
    auth.projectVariables.abandonPending()
    clearEditor()
    retireReads()
    return true
  }
  function afterNavigation(to: string, _from: string) {
    route.value = to
  }
  const stop = watch(
    () => [context.value, auth.personalContext.identity, auth.state.busy, scope.value] as const,
    ([next, identity, busy]) => {
      if (disposed.value) return
      if (!sameIdentity(trackedIdentity, identity)) {
        clearAll()
        trackedIdentity = identity
        lastContext = null
      }
      if (!next || !visible.value) {
        if (lastContext) {
          retireReads()
          page.stale = page.items.length > 0
          if (editor.open) editor.requiresRead = true
        }
        lastContext = null
        return
      }
      if (boundID.value && boundID.value !== next.projectID) clearAll()
      if (!boundID.value) {
        boundID.value = next.projectID
        needsInitial = true
      } else if (lastContext && !sameContext(lastContext, next)) {
        retireReads()
        page.stale = page.items.length > 0
        if (editor.open) editor.requiresRead = true
      }
      lastContext = next
      if (needsInitial && !busy) {
        needsInitial = false
        void loadPage({ previous: [], page: 1 })
      }
    },
    { flush: 'sync', immediate: true },
  )
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value || progress.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', beforeUnload)
  function dispose() {
    if (disposed.value) return
    disposed.value = true
    stop()
    clearAll()
    window.removeEventListener('beforeunload', beforeUnload)
  }
  return {
    visible,
    sessionBusy,
    currentProject,
    canMutate,
    blocked,
    dirty,
    pending,
    progress,
    message,
    feedback,
    page,
    editor,
    draft,
    changed,
    canSave,
    canDelete,
    canLookup,
    canReplay,
    confirmation,
    firstPage: () => loadPage({ previous: [], page: 1 }),
    reloadPage: () => loadPage({ cursor, previous, page: page.page }),
    nextPage: () =>
      !page.cursorInvalid && nextCursor !== undefined
        ? loadPage({ cursor: nextCursor, previous: [...previous, cursor], page: page.page + 1 })
        : Promise.resolve(false),
    previousPage: () =>
      !page.cursorInvalid && previous.length
        ? loadPage({
            cursor: previous.at(-1),
            previous: previous.slice(0, -1),
            page: page.page - 1,
          })
        : Promise.resolve(false),
    select,
    createNew,
    closeEditor: () => (blocked.value ? Promise.resolve(false) : discardEditor()),
    readSelected,
    adoptCurrent,
    save,
    deleteSelected,
    lookupOriginal,
    readOriginalTarget,
    replayOriginal,
    abandonPending,
    editRejected,
    readOwner: () => workspace.readCurrent(),
    confirm: () => finishConfirmation(true),
    cancelConfirmation: () => finishConfirmation(false),
    confirmLeave,
    afterNavigation,
    dispose,
  }
}
export type ProjectVariables = ReturnType<typeof createProjectVariables>
export const projectVariablesKey: InjectionKey<ProjectVariables> = Symbol('ProjectVariables')
export function useProjectVariables() {
  const value = inject(projectVariablesKey)
  if (!value) throw new Error('Project Variables require the App owner')
  return value
}
