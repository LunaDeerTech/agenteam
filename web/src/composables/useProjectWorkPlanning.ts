import {
  computed,
  inject,
  reactive,
  ref,
  shallowReactive,
  shallowRef,
  watch,
  type InjectionKey,
} from 'vue'
import { AccountFailure } from '../api/client'
import { captureWorkPlanningCommand, newWorkPlanningID } from '../api/work-planning'
import type {
  WorkMilestone,
  WorkMilestoneSummary,
  WorkSprint,
  WorkSprintSummary,
  WorkTask,
  WorkTaskSummary,
  WorkTaskQuery,
  WorkPage,
  WorkTaskBlocker,
  WorkPlanningCommand,
  WorkPlanningReceipt,
  WorkTaskType,
  WorkTaskPriority,
  WorkTaskUpdateRequest,
  WorkBlockerAddRequest,
} from '../api/work-planning'
import { workPlanningRoute } from '../router/auth'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Address = NonNullable<ReturnType<typeof workPlanningRoute>>
type Kind = 'milestone' | 'sprint' | 'task'
type Form = { title: string; description: string; type: string; priority: string; plan: string }
const emptyForm = (): Form => ({ title: '', description: '', type: '', priority: '', plan: '' })
type Phase = 'inactive' | 'loading' | 'ready' | 'empty' | 'error'
export type WorkPlanningPage<T> = {
  phase: Phase
  items: readonly T[]
  message: string
  stale: boolean
  page: number
  cursor: string | undefined
  next: string | undefined
  previous: (string | undefined)[]
  invalid: boolean
}
function page<T>(): WorkPlanningPage<T> {
  return shallowReactive({
    phase: 'inactive',
    items: [],
    message: '',
    stale: false,
    page: 1,
    cursor: undefined,
    next: undefined,
    previous: [],
    invalid: false,
  })
}
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
const failure = (value: unknown) =>
  value instanceof AccountFailure ? value : new AccountFailure('transport')
function explanation(value: unknown) {
  const problem = failure(value).problem
  if (problem?.status === 401) return '当前会话已失效，请重新登录。'
  if (problem?.status === 403 || problem?.status === 404)
    return '当前身份无法读取此对象，请重新确认项目访问权限。'
  if (problem?.code === 'CURSOR_STALE' || problem?.code === 'CURSOR_INVALID')
    return '分页已失效，请明确从首页重新读取。'
  return '未取得完整的当前内容，请在请求结束后明确重新读取。'
}

// App retains the controller while Session checking temporarily removes its view.
// Original mutation bytes and Cookie ownership belong exclusively to Session.
export function createProjectWorkPlanning(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  replaceRoute: (path: string) => Promise<unknown> = async () => undefined,
) {
  const address = shallowRef<Address | null>(null)
  const detail = shallowReactive<{
    phase: Phase
    message: string
    milestone: WorkMilestone | null
    sprint: WorkSprint | null
    task: WorkTask | null
  }>({ phase: 'inactive', message: '', milestone: null, sprint: null, task: null })
  const milestones = page<WorkMilestoneSummary>()
  const sprints = shallowReactive(new Map<string, WorkPlanningPage<WorkSprintSummary>>())
  const tasks = shallowReactive(new Map<string, WorkPlanningPage<WorkTaskSummary>>())
  const blockers = page<WorkTaskBlocker>()
  const blockerStatus = ref<'unresolved' | 'resolved' | 'all'>('unresolved')
  const filters = reactive<{
    text: string
    type: '' | NonNullable<WorkTaskQuery['type']>
    priority: '' | NonNullable<WorkTaskQuery['priority']>
  }>({ text: '', type: '', priority: '' })
  const expanded = ref<string[]>([])
  const treeOpen = ref(false)
  const reading = ref(false)
  const confirmation = reactive({ open: false, title: '', message: '', label: '放弃并离开' })
  const progress = computed(() => auth.workPlanning.progress)
  const context = computed<Context | null>(() => {
    const current = workspace.currentReadContext.value
    return address.value && current && workspace.paths.value.home === address.value.home
      ? current
      : null
  })
  const visible = computed(
    () =>
      !!context.value &&
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current',
  )
  const blocked = computed(
    () => !visible.value || auth.state.busy || reading.value || confirmation.open,
  )
  const readOnly = computed(
    () => !context.value || workspace.detail.project?.lifecycle !== 'active',
  )
  const selected = computed(() => {
    const value = address.value
    return value && value.id ? `${value.kind}:${value.id}` : ''
  })
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const draft = reactive(emptyForm()),
    baseline = reactive(emptyForm())
  const editor = reactive<{
    open: boolean
    kind: Kind
    creating: boolean
    targetID: string
    parentID: string
    version: string
    conflict: boolean
    requiresRead: boolean
    errors: Partial<Record<keyof Form, string>>
    message: string
    feedback: 'idle' | 'loading' | 'success'
  }>({
    open: false,
    kind: 'milestone',
    creating: false,
    targetID: '',
    parentID: '',
    version: '',
    conflict: false,
    requiresRead: false,
    errors: {},
    message: '',
    feedback: 'idle',
  })
  const changed = computed(
    () =>
      editor.open &&
      Object.keys(draft).some((key) => draft[key as keyof Form] !== baseline[key as keyof Form]),
  )
  const blockerDraft = reactive({
    type: '' as '' | 'rely_on' | 'waiting_for_human',
    description: '',
    relatedTaskID: '',
    resolvingID: '',
    resolution: '',
    message: '',
  })
  const dependencyPage = page<WorkTaskSummary>()
  const dependencyText = ref('')
  const blockerChanged = computed(
    () =>
      !!(
        blockerDraft.type ||
        blockerDraft.description ||
        blockerDraft.relatedTaskID ||
        blockerDraft.resolvingID ||
        blockerDraft.resolution
      ),
  )
  const reorder = reactive({ open: false, beforeID: '', message: '' })
  const reorderPage = page<WorkMilestoneSummary | WorkSprintSummary | WorkTaskSummary>()
  const dirty = computed(
    () => changed.value || unsettled.value || editor.conflict || blockerChanged.value,
  )
  const editableTask = computed(
    () =>
      !!detail.task &&
      detail.task.state === 'backlog' &&
      detail.task.assignee_agent_id === null &&
      detail.sprint?.state !== 'completed',
  )
  const editorReadOnly = computed(
    () =>
      readOnly.value ||
      unsettled.value ||
      editor.conflict ||
      editor.requiresRead ||
      (editor.kind !== 'milestone' && detail.sprint?.state === 'completed') ||
      (editor.kind === 'task' && !editor.creating && !editableTask.value),
  )
  const canSave = computed(
    () =>
      !blocked.value && !editorReadOnly.value && editor.open && (editor.creating || changed.value),
  )
  const canAdopt = computed(
    () =>
      !blocked.value &&
      !unsettled.value &&
      !editor.requiresRead &&
      detail.phase === 'ready' &&
      !editor.creating,
  )
  const canBlock = computed(
    () =>
      !blocked.value &&
      !readOnly.value &&
      !unsettled.value &&
      editableTask.value &&
      detail.phase === 'ready' &&
      !editor.requiresRead &&
      !changed.value &&
      !editor.conflict,
  )
  const recoveryMessage = ref('')
  let feedbackTimer: ReturnType<typeof setTimeout> | undefined
  let readContext: Context | null = null
  let detailNeedsRead = false
  function resetEditor() {
    if (feedbackTimer) clearTimeout(feedbackTimer)
    Object.assign(draft, emptyForm())
    Object.assign(baseline, emptyForm())
    Object.assign(editor, {
      open: false,
      creating: false,
      targetID: '',
      parentID: '',
      version: '',
      conflict: false,
      requiresRead: false,
      errors: {},
      message: '',
      feedback: 'idle',
    })
    Object.assign(blockerDraft, {
      type: '',
      description: '',
      relatedTaskID: '',
      resolvingID: '',
      resolution: '',
      message: '',
    })
    Object.assign(reorder, { open: false, beforeID: '', message: '' })
    clearPage(dependencyPage)
    clearPage(reorderPage)
    dependencyText.value = ''
    recoveryMessage.value = ''
  }
  function currentObject() {
    return editor.kind === 'task'
      ? detail.task
      : editor.kind === 'sprint'
        ? detail.sprint
        : detail.milestone
  }
  function adoptObject(value: WorkMilestone | WorkSprint | WorkTask) {
    const form: Form = {
      title: value.title,
      description: value.description,
      type: 'type' in value ? value.type : '',
      priority: 'priority' in value ? value.priority : '',
      plan: 'plan' in value ? value.plan : '',
    }
    Object.assign(draft, form)
    Object.assign(baseline, form)
    Object.assign(editor, {
      open: true,
      creating: false,
      targetID: value.id,
      version: value.version,
      parentID:
        'sprint_id' in value ? value.sprint_id : 'milestone_id' in value ? value.milestone_id : '',
      conflict: false,
      requiresRead: false,
      errors: {},
      message: '',
    })
  }
  function publishSelection(value: {
    milestone: WorkMilestone | null
    sprint: WorkSprint | null
    task: WorkTask | null
  }) {
    const oldTaskVersion = detail.task?.version
    const oldTaskID = detail.task?.id
    Object.assign(detail, value, {
      phase: address.value?.kind === 'explore' ? 'empty' : 'ready',
      message: '',
    })
    detailNeedsRead = false
    if (oldTaskID !== value.task?.id || oldTaskVersion !== value.task?.version) clearPage(blockers)
    const kind = address.value?.kind
    if (!kind || kind === 'explore' || editor.creating) return
    const object = value[kind]
    if (!object) return
    if (!editor.open || editor.targetID !== object.id) {
      editor.kind = kind
      adoptObject(object)
    } else if (
      object.version !== editor.version &&
      (changed.value || unsettled.value || editor.conflict)
    ) {
      editor.conflict = true
      editor.requiresRead = false
      editor.message = '当前版本已变化，原草稿保留。请对照当前值后明确重新编辑。'
    } else if (!changed.value && !unsettled.value && !editor.conflict) adoptObject(object)
    else editor.requiresRead = false
  }
  let generation = 0,
    disposed = false,
    initial = false,
    boundProject: string | null = null
  let identity = auth.personalContext.identity
  let canonicalNavigation = ''
  let answer: ((value: boolean) => void) | null = null
  const live = (own: number, captured: Context) =>
    !disposed && own === generation && sameContext(captured, context.value)
  function clearPage<T>(target: WorkPlanningPage<T>) {
    Object.assign(target, page<T>())
  }
  function clearSelection() {
    Object.assign(detail, {
      phase: 'inactive',
      message: '',
      milestone: null,
      sprint: null,
      task: null,
    })
    clearPage(blockers)
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const settle = answer
    answer = null
    settle?.(value)
  }
  function clearAll() {
    ++generation
    auth.workPlanning.abandon()
    reading.value = false
    resetEditor()
    clearSelection()
    clearPage(milestones)
    sprints.clear()
    tasks.clear()
    expanded.value = []
    treeOpen.value = false
    boundProject = null
    Object.assign(filters, { text: '', type: '', priority: '' })
    blockerStatus.value = 'unresolved'
    finishConfirmation(false)
  }
  async function runRead<T>(
    work: (captured: Context, current: () => boolean) => Promise<T>,
    publish: (value: T) => void,
    failed: (error: unknown) => void,
  ) {
    const captured = context.value
    if (!captured || auth.state.busy || reading.value || disposed) return
    const own = generation
    reading.value = true
    try {
      const value = await work(captured, () => live(own, captured))
      if (live(own, captured)) publish(value)
    } catch (error) {
      if (live(own, captured)) failed(error)
    } finally {
      if (own === generation) reading.value = false
    }
  }
  async function selection(value: Address, captured: Context, current: () => boolean) {
    const api = auth.workPlanning,
      project = captured.projectID
    let milestone: WorkMilestone | null = null,
      sprint: WorkSprint | null = null,
      task: WorkTask | null = null
    const ensureCurrent = () => {
      if (!current()) throw new AccountFailure('invalid-response')
    }
    if (value.kind === 'task' && value.id) {
      task = await api.getTask(project, value.id)
      ensureCurrent()
      sprint = await api.getSprint(project, task.sprint_id)
      ensureCurrent()
      if (sprint.milestone_id !== task.milestone_id) throw new AccountFailure('invalid-response')
      milestone = await api.getMilestone(project, sprint.milestone_id)
      ensureCurrent()
    } else if (value.kind === 'sprint' && value.id) {
      sprint = await api.getSprint(project, value.id)
      ensureCurrent()
      milestone = await api.getMilestone(project, sprint.milestone_id)
      ensureCurrent()
    } else if (value.kind === 'milestone' && value.id) {
      milestone = await api.getMilestone(project, value.id)
      ensureCurrent()
    }
    return { milestone, sprint, task }
  }
  function selectionFailed(error: unknown) {
    editor.requiresRead = true
    detail.phase = 'error'
    detail.message = explanation(error)
    if ([401, 403, 404].includes(failure(error).problem?.status ?? 0)) {
      detail.milestone = null
      detail.sprint = null
      detail.task = null
    }
  }
  async function loadSelection() {
    const value = address.value
    if (!value || blocked.value) return
    detail.phase = 'loading'
    detail.message = ''
    await runRead(
      (captured, current) => selection(value, captured, current),
      publishSelection,
      selectionFailed,
    )
  }
  async function loadPage<T>(
    target: WorkPlanningPage<T>,
    request: (captured: Context, cursor: string | undefined) => Promise<WorkPage<T>>,
    direction: 'first' | 'next' | 'previous',
  ) {
    if (blocked.value || (target.invalid && direction !== 'first')) return
    if (
      (direction === 'next' && !target.next) ||
      (direction === 'previous' && !target.previous.length)
    )
      return
    const cursor =
      direction === 'next'
        ? target.next
        : direction === 'previous'
          ? target.previous.at(-1)
          : undefined
    const previous =
      direction === 'first'
        ? []
        : direction === 'next'
          ? [...target.previous, target.cursor]
          : target.previous.slice(0, -1)
    const index =
      direction === 'first' ? 1 : direction === 'next' ? target.page + 1 : target.page - 1
    target.phase = 'loading'
    target.message = ''
    await runRead(
      (captured) => request(captured, cursor),
      (result) => {
        Object.assign(target, {
          items: result.items,
          next: result.next_cursor,
          cursor,
          previous,
          page: index,
          phase: result.items.length ? 'ready' : 'empty',
          message: '',
          stale: false,
          invalid: false,
        })
      },
      (error) => {
        target.phase = 'error'
        target.stale = target.items.length > 0
        target.message = explanation(error)
        if (['CURSOR_STALE', 'CURSOR_INVALID'].includes(failure(error).problem?.code ?? '')) {
          target.invalid = true
          target.next = undefined
          target.previous = []
        }
        if ([401, 403, 404].includes(failure(error).problem?.status ?? 0)) target.items = []
      },
    )
  }
  function loadMilestones(direction: 'first' | 'next' | 'previous' = 'first') {
    return loadPage(
      milestones,
      (captured, cursor) =>
        auth.workPlanning.listMilestones(captured.projectID, {
          limit: 50,
          ...(cursor ? { cursor } : {}),
        }),
      direction,
    )
  }
  function sprintPage(id: string) {
    if (!sprints.has(id)) sprints.set(id, page<WorkSprintSummary>())
    return sprints.get(id)!
  }
  function taskPage(id: string) {
    if (!tasks.has(id)) tasks.set(id, page<WorkTaskSummary>())
    return tasks.get(id)!
  }
  function loadSprints(id: string, direction: 'first' | 'next' | 'previous' = 'first') {
    return loadPage(
      sprintPage(id),
      (captured, cursor) =>
        auth.workPlanning.listSprints(captured.projectID, {
          milestone_id: id,
          limit: 50,
          ...(cursor ? { cursor } : {}),
        }),
      direction,
    )
  }
  function loadTasks(id: string, direction: 'first' | 'next' | 'previous' = 'first') {
    const query: WorkTaskQuery = {
      sprint_id: id,
      state: 'backlog',
      assignee_agent_id: null,
      limit: 50,
      ...(filters.text ? { text: filters.text } : {}),
      ...(filters.type ? { type: filters.type } : {}),
      ...(filters.priority ? { priority: filters.priority } : {}),
    }
    return loadPage(
      taskPage(id),
      (captured, cursor) =>
        auth.workPlanning.listTasks(captured.projectID, {
          ...query,
          ...(cursor ? { cursor } : {}),
        }),
      direction,
    )
  }
  function loadBlockers(direction: 'first' | 'next' | 'previous' = 'first') {
    const task = detail.task
    if (!task) return
    const status = blockerStatus.value
    return loadPage(
      blockers,
      (captured, cursor) =>
        auth.workPlanning.listTaskBlockers(captured.projectID, task.id, {
          limit: 50,
          status,
          ...(cursor ? { cursor } : {}),
        }),
      direction,
    )
  }
  async function applyFilters() {
    if (blocked.value) return
    tasks.clear()
    if (detail.sprint) await loadTasks(detail.sprint.id)
  }
  async function setBlockerStatus(value: 'unresolved' | 'resolved' | 'all') {
    if (blocked.value || value === blockerStatus.value) return
    blockerStatus.value = value
    clearPage(blockers)
    await loadBlockers()
  }
  function invalidate<T>(target: WorkPlanningPage<T>) {
    if (target.phase === 'inactive') return
    target.invalid = true
    target.stale = target.items.length > 0
    target.next = undefined
    target.previous = []
    target.message = '内容可能已变化，请从首页重新读取。'
  }
  function invalidateReceipt(receipt: WorkPlanningReceipt) {
    if (receipt.domain === 'structure') {
      if (receipt.value.milestone) invalidate(milestones)
      if (receipt.value.sprint) {
        const rows = sprints.get(receipt.value.sprint.milestone_id)
        if (rows) invalidate(rows)
      }
    } else {
      const task = receipt.value.task,
        rows = tasks.get(task.sprint_id)
      if (rows) invalidate(rows)
      if (detail.task?.id === task.id) clearPage(blockers)
    }
    invalidate(reorderPage)
  }
  function validateForm() {
    const errors: Partial<Record<keyof Form, string>> = {}
    const size = (text: string) => new TextEncoder().encode(text).length
    if (
      !draft.title ||
      /^\p{White_Space}*$/u.test(draft.title) ||
      /[\p{Cs}\p{Cc}]/u.test(draft.title) ||
      [...draft.title].length > 256 ||
      size(draft.title) > 1024
    )
      errors.title = '请输入含非空白的标题，最多 256 个字符和 1024 字节，不含控制字符。'
    for (const key of (editor.kind === 'task' ? ['description', 'plan'] : ['description']) as (
      'description' | 'plan'
    )[]) {
      if (
        /\p{Cs}/u.test(draft[key]) ||
        /\p{Cc}/u.test(draft[key].replace(/[\t\r\n]/g, '')) ||
        size(draft[key]) > 32768
      )
        errors[key] = '最多 32768 字节，可包含换行与制表符，不含其它控制字符。'
    }
    if (editor.kind === 'task') {
      if (!['feature', 'bug', 'task', 'spike', 'chore'].includes(draft.type))
        errors.type = '请选择类型。'
      if (!['low', 'medium', 'high', 'critical'].includes(draft.priority))
        errors.priority = '请选择优先级。'
    }
    editor.errors = errors
    return Object.keys(errors).length === 0
  }
  async function askDiscard(force = false) {
    if (!dirty.value && !force) return true
    if (answer) return false
    Object.assign(confirmation, {
      open: true,
      title: '放弃任务规划修改？',
      message: '将放弃本地草稿与原命令追踪；此前请求仍可能生效，放弃不会撤销服务器命令。',
      label: '放弃本地修改',
    })
    return new Promise<boolean>((resolve) => {
      answer = resolve
    })
  }
  async function beginCreate(kind: Kind) {
    if (blocked.value || readOnly.value || unsettled.value) return
    const captured = context.value,
      own = generation
    if (
      !captured ||
      (kind === 'sprint' && !detail.milestone) ||
      (kind === 'task' && (!detail.sprint || detail.sprint.state === 'completed'))
    )
      return
    if (!(await askDiscard()) || !live(own, captured) || auth.state.busy) return
    auth.workPlanning.abandon()
    resetEditor()
    Object.assign(editor, {
      open: true,
      creating: true,
      kind,
      parentID: kind === 'sprint' ? detail.milestone!.id : kind === 'task' ? detail.sprint!.id : '',
    })
  }
  async function cancelCreate() {
    if (blocked.value || !editor.creating) return
    const captured = context.value,
      own = generation
    if (!captured || !(await askDiscard()) || !live(own, captured)) return
    auth.workPlanning.abandon()
    resetEditor()
    publishSelection({ milestone: detail.milestone, sprint: detail.sprint, task: detail.task })
  }
  async function adoptCurrent() {
    if (!canAdopt.value) return
    const captured = context.value,
      own = generation
    if (!captured || !(await askDiscard()) || !live(own, captured) || auth.state.busy) return
    const current = currentObject()
    if (!current) return
    auth.workPlanning.abandon()
    adoptObject(current)
  }
  function rejected(error: unknown) {
    const problem = failure(error).problem
    if (progress.value?.phase === 'uncertain') {
      recoveryMessage.value = '原命令结果不确定，请查证或明确按原请求重放。'
    } else if (failure(error).kind === 'invalid-input')
      editor.message = '输入不符合规划要求，请检查后重试。'
    else editor.message = '本次请求未能完成，请查看错误并保留原草稿。'
    if (problem?.code === 'VERSION_CONFLICT') {
      editor.conflict = true
      editor.requiresRead = true
      editor.message = '版本冲突，原草稿与版本已保留。请先读取当前值。'
    }
    for (const field of problem?.field_errors ?? []) {
      const key = field.path.replace(/^request\./, '')
      if (['title', 'description', 'type', 'priority', 'plan'].includes(key))
        editor.errors[key as keyof Form] = '此字段不符合要求，请检查输入。'
    }
  }
  async function refreshAfterReceipt(
    receipt: WorkPlanningReceipt,
    captured: Context,
    own: number,
    applySaved = false,
  ) {
    if (!live(own, captured)) return
    invalidateReceipt(receipt)
    recoveryMessage.value = '原命令已确认；正在独立读取当前内容。'
    editor.feedback = 'success'
    if (feedbackTimer) clearTimeout(feedbackTimer)
    feedbackTimer = setTimeout(() => {
      editor.feedback = 'idle'
    }, 2400)
    if (editor.creating) {
      const value =
        receipt.domain === 'structure'
          ? (receipt.value.milestone ?? receipt.value.sprint)
          : receipt.value.task
      if (value) {
        const kind: Kind =
          receipt.domain === 'structure'
            ? receipt.value.milestone
              ? 'milestone'
              : 'sprint'
            : 'task'
        // A receipt confirms only its original fields; the selected route performs a separate Get.
        resetEditor()
        editor.kind = kind
        canonicalNavigation = targetPath(kind, value.id)
        try {
          await replaceRoute(canonicalNavigation)
        } finally {
          canonicalNavigation = ''
        }
      }
    } else {
      editor.requiresRead = true
      await loadSelection()
      if (!live(own, captured)) return
      if (detail.phase === 'ready' && applySaved) {
        const value = currentObject()
        if (value) adoptObject(value)
      }
    }
    if (live(own, captured))
      recoveryMessage.value =
        detail.phase === 'error'
          ? '原命令已确认，当前内容读取失败。可明确重新读取。'
          : '原命令已确认；历史回执与当前内容分别读取。'
  }
  async function execute(command: WorkPlanningCommand) {
    const captured = context.value,
      own = generation
    if (!captured || blocked.value || readOnly.value || unsettled.value) return
    let immutable: WorkPlanningCommand
    try {
      immutable = captureWorkPlanningCommand(command)
    } catch (error) {
      rejected(error)
      return
    }
    editor.feedback = 'loading'
    editor.message = ''
    recoveryMessage.value = ''
    try {
      const receipt = await auth.workPlanning.start(immutable)
      if (live(own, captured)) await refreshAfterReceipt(receipt, captured, own, true)
    } catch (error) {
      if (live(own, captured)) rejected(error)
    } finally {
      if (live(own, captured) && editor.feedback === 'loading') editor.feedback = 'idle'
    }
  }
  async function save() {
    if (!canSave.value || !validateForm()) return
    const projectID = context.value!.projectID,
      expected_version = editor.version,
      targetID = editor.targetID
    const text = { title: draft.title, description: draft.description }
    let command: WorkPlanningCommand
    try {
      if (editor.creating) {
        const id = newWorkPlanningID()
        if (editor.kind === 'milestone')
          command = {
            domain: 'structure',
            projectID,
            command: 'work.milestone.create',
            request: { milestone_id: id, ...text },
          }
        else if (editor.kind === 'sprint')
          command = {
            domain: 'structure',
            projectID,
            command: 'work.sprint.create',
            request: { sprint_id: id, milestone_id: editor.parentID, ...text },
          }
        else
          command = {
            domain: 'task',
            projectID,
            command: 'work.task.create',
            request: {
              task_id: id,
              sprint_id: editor.parentID,
              ...text,
              type: draft.type as WorkTaskType,
              priority: draft.priority as WorkTaskPriority,
              plan: draft.plan,
            },
          }
      } else {
        const request: WorkTaskUpdateRequest = {
          ...(draft.title !== baseline.title ? { title: draft.title } : {}),
          ...(draft.description !== baseline.description ? { description: draft.description } : {}),
          ...(editor.kind === 'task' && draft.type !== baseline.type
            ? { type: draft.type as WorkTaskType }
            : {}),
          ...(editor.kind === 'task' && draft.priority !== baseline.priority
            ? { priority: draft.priority as WorkTaskPriority }
            : {}),
          ...(editor.kind === 'task' && draft.plan !== baseline.plan ? { plan: draft.plan } : {}),
        }
        if (editor.kind === 'task')
          command = {
            domain: 'task',
            projectID,
            command: 'work.task.update',
            targetID,
            expected_version,
            request,
          }
        else
          command = {
            domain: 'structure',
            projectID,
            command: editor.kind === 'milestone' ? 'work.milestone.update' : 'work.sprint.update',
            targetID,
            expected_version,
            request,
          }
      }
    } catch {
      editor.message = '无法安全准备此次命令，请稍后重试。'
      return
    }
    await execute(command)
  }
  async function checkOriginal() {
    if (blocked.value || !progress.value?.canLookup) return
    const captured = context.value!,
      own = generation
    try {
      await auth.workPlanning.checkOriginal()
      if (!live(own, captured)) return
      const result = progress.value
      if (result?.phase === 'confirmed' && result.receipt)
        await refreshAfterReceipt(result.receipt, captured, own)
      else
        recoveryMessage.value =
          result?.observation === 'in_progress'
            ? '原命令仍在处理中。稍后可明确再次查证。'
            : '本次未观察到原命令，结果仍不确定。可明确查证或按原请求重放。'
    } catch (error) {
      if (live(own, captured)) rejected(error)
    }
  }
  async function retryOriginal() {
    if (blocked.value || !progress.value?.canReplay) return
    const captured = context.value!,
      own = generation
    try {
      const receipt = await auth.workPlanning.retryOriginal()
      if (live(own, captured)) await refreshAfterReceipt(receipt, captured, own)
    } catch (error) {
      if (live(own, captured)) rejected(error)
    }
  }
  async function abandonOriginal() {
    if (blocked.value || !progress.value) return
    const captured = context.value!,
      own = generation
    if (!(await askDiscard(true)) || !live(own, captured)) return
    auth.workPlanning.abandon()
    if (editor.open && !editor.creating) {
      editor.requiresRead = true
      editor.conflict = true
    }
    recoveryMessage.value = '已放弃本地追踪，不撤销服务器命令。'
  }
  async function loadDependencies(direction: 'first' | 'next' | 'previous' = 'first') {
    const text = dependencyText.value
    await loadPage(
      dependencyPage,
      (captured, cursor) =>
        auth.workPlanning.listTasks(captured.projectID, {
          limit: 50,
          ...(text ? { text } : {}),
          ...(cursor ? { cursor } : {}),
        }),
      direction,
    )
  }
  async function addBlocker() {
    if (!canBlock.value || !detail.task) return
    blockerDraft.message = ''
    if (
      !blockerDraft.type ||
      (blockerDraft.type === 'rely_on' &&
        (!blockerDraft.relatedTaskID || blockerDraft.relatedTaskID === detail.task.id))
    ) {
      blockerDraft.message = '请选择阻塞类型；依赖任务必须是本项目中的其它任务。'
      return
    }
    const task = detail.task
    try {
      const request: WorkBlockerAddRequest =
        blockerDraft.type === 'waiting_for_human'
          ? {
              type: 'waiting_for_human' as const,
              metadata: {},
              blocker_id: newWorkPlanningID(),
              description: blockerDraft.description,
            }
          : {
              type: 'rely_on' as const,
              metadata: { related_task_id: blockerDraft.relatedTaskID },
              blocker_id: newWorkPlanningID(),
              description: blockerDraft.description,
            }
      await execute({
        domain: 'blocker',
        projectID: context.value!.projectID,
        taskID: task.id,
        expected_version: task.version,
        command: 'work.task.blocker.add',
        request,
      })
      if (progress.value?.phase === 'confirmed') {
        Object.assign(blockerDraft, { type: '', description: '', relatedTaskID: '' })
        await loadBlockers()
      } else blockerDraft.message = editor.message || recoveryMessage.value
    } catch {
      blockerDraft.message = '请检查阻塞说明与依赖任务，保留原文后重试。'
    }
  }
  function beginResolve(id: string) {
    if (
      !canBlock.value ||
      !blockers.items.some((item) => item.id === id && item.resolved_at === null)
    )
      return
    blockerDraft.resolvingID = id
    blockerDraft.resolution = ''
    blockerDraft.message = ''
  }
  async function resolveBlocker() {
    if (!canBlock.value || !detail.task || !blockerDraft.resolvingID) return
    const value = blockerDraft.resolution
    if (value !== '' && /^\p{White_Space}*$/u.test(value)) {
      blockerDraft.message = '备注不能只有空白；不填写备注时请完全清空。'
      return
    }
    await execute({
      domain: 'blocker',
      projectID: context.value!.projectID,
      taskID: detail.task.id,
      expected_version: detail.task.version,
      command: 'work.task.blocker.resolve',
      request: {
        blocker_id: blockerDraft.resolvingID,
        resolution_comment: value === '' ? null : value,
      },
    })
    if (progress.value?.phase === 'confirmed') {
      blockerDraft.resolvingID = ''
      blockerDraft.resolution = ''
      await loadBlockers()
    } else blockerDraft.message = editor.message || recoveryMessage.value
  }
  async function loadReorder(direction: 'first' | 'next' | 'previous' = 'first') {
    if (!reorder.open || !editor.open || editor.creating) return
    const kind = editor.kind,
      current = currentObject()
    if (!current) return
    await loadPage(
      reorderPage,
      (captured, cursor) => {
        const query = { limit: 50, ...(cursor ? { cursor } : {}) }
        if (kind === 'milestone') return auth.workPlanning.listMilestones(captured.projectID, query)
        if (kind === 'sprint')
          return auth.workPlanning.listSprints(captured.projectID, {
            ...query,
            milestone_id: (current as WorkSprint).milestone_id,
          })
        const task = current as WorkTask
        return auth.workPlanning.listTasks(captured.projectID, {
          ...query,
          sprint_id: task.sprint_id,
          state: task.state,
          priority: task.priority,
        })
      },
      direction,
    )
  }
  async function openReorder() {
    if (blocked.value || editorReadOnly.value || !editor.open || editor.creating || changed.value)
      return
    reorder.open = true
    reorder.beforeID = ''
    reorder.message = ''
    clearPage(reorderPage)
    await loadReorder()
  }
  async function saveReorder(tail: boolean) {
    if (
      blocked.value ||
      editorReadOnly.value ||
      !reorder.open ||
      changed.value ||
      reorderPage.invalid ||
      !['ready', 'empty'].includes(reorderPage.phase)
    )
      return
    const current = currentObject()
    if (!current) return
    const before_id = reorder.beforeID
    if (
      !tail &&
      (!before_id ||
        before_id === current.id ||
        !reorderPage.items.some((item) => item.id === before_id))
    ) {
      reorder.message = '请在当前同组页选择其它对象。'
      return
    }
    const common = {
      projectID: context.value!.projectID,
      targetID: current.id,
      expected_version: editor.version,
    }
    const request = tail ? {} : { before_id }
    const command: WorkPlanningCommand =
      editor.kind === 'milestone'
        ? { domain: 'structure', command: 'work.milestone.reorder', ...common, request }
        : editor.kind === 'sprint'
          ? {
              domain: 'structure',
              command: 'work.sprint.reorder',
              ...common,
              request: { milestone_id: (current as WorkSprint).milestone_id, ...request },
            }
          : { domain: 'task', command: 'work.task.reorder', ...common, request }
    await execute(command)
    if (progress.value?.phase === 'confirmed') reorder.open = false
  }
  async function expand(nodes: string[]) {
    if (blocked.value) return
    const added = nodes.filter((key) => !expanded.value.includes(key))
    expanded.value = [...nodes]
    for (const key of added) {
      const [kind, id] = key.split(':')
      if (!id || blocked.value) continue
      if (kind === 'milestone' && sprintPage(id).phase === 'inactive') await loadSprints(id)
      if (kind === 'sprint' && taskPage(id).phase === 'inactive') await loadTasks(id)
    }
  }
  function targetPath(kind: 'milestone' | 'sprint' | 'task', id: string) {
    const path = `${workspace.paths.value.home}/tasks/explore/${kind}s/${id}`
    return workPlanningRoute(path)?.path ?? ''
  }
  async function startInitial() {
    if (!initial || blocked.value || !address.value) return
    initial = false
    const own = generation
    if (milestones.phase === 'inactive') await loadMilestones()
    if (own !== generation || blocked.value || !address.value) return
    if (address.value.kind === 'explore') {
      const current = workspace.detail.project?.current_sprint_id
      if (current) {
        canonicalNavigation = targetPath('sprint', current)
        try {
          await replaceRoute(canonicalNavigation)
        } finally {
          canonicalNavigation = ''
        }
        return
      }
      detail.phase = 'empty'
      detail.message = '当前没有正在进行的 Sprint，请选择。'
    } else await loadSelection()
  }
  async function confirmLeave(target?: string) {
    if (target === canonicalNavigation && target) return true
    if (!dirty.value) return !confirmation.open
    if (answer) return false
    Object.assign(confirmation, {
      open: true,
      title: '放弃任务规划修改？',
      message: '将放弃本地草稿与原命令追踪；此前请求仍可能生效，离开不会撤销服务器命令。',
      label: '放弃并离开',
    })
    if (
      !(await new Promise<boolean>((resolve) => {
        answer = resolve
      }))
    )
      return false
    auth.workPlanning.abandon()
    resetEditor()
    return true
  }
  function afterNavigation(to: string, _from = '') {
    if (disposed) return
    const next = workPlanningRoute(to)
    if (address.value?.path === next?.path) return
    ++generation
    auth.workPlanning.abandonRead()
    reading.value = false
    address.value = next
    resetEditor()
    clearSelection()
    initial = !!next
    if (!next) {
      clearAll()
      return
    }
    void startInitial()
  }
  const stop = watch(
    () =>
      [
        auth.personalContext.identity,
        workspace.currentReadContext.value,
        auth.state.busy,
        auth.personalContext.phase,
      ] as const,
    ([nextIdentity]) => {
      if (!sameIdentity(identity, nextIdentity) && (identity !== null || nextIdentity !== null))
        clearAll()
      identity = nextIdentity
      const current = context.value
      if (readContext && !sameContext(readContext, current)) {
        ++generation
        auth.workPlanning.abandonRead()
        reading.value = false
        if (detail.phase === 'loading' || detail.phase === 'ready') {
          detail.phase = 'error'
          detail.message = '项目读取资格已重新确认，请读取当前值。'
          detailNeedsRead = true
          editor.requiresRead = true
        }
        invalidate(milestones)
        for (const target of sprints.values()) invalidate(target)
        for (const target of tasks.values()) invalidate(target)
        invalidate(blockers)
        invalidate(dependencyPage)
        invalidate(reorderPage)
      }
      const restored = !readContext && !!current
      readContext = current
      if (current && current.projectID !== boundProject) {
        clearPage(milestones)
        sprints.clear()
        tasks.clear()
        clearSelection()
        expanded.value = []
        boundProject = current.projectID
        initial = !!address.value
      }
      if (current && !auth.state.busy) {
        if (initial) void startInitial()
        else if (restored && detailNeedsRead && !reading.value) void loadSelection()
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
  if (typeof window !== 'undefined') window.addEventListener('beforeunload', beforeUnload)
  function dispose() {
    disposed = true
    stop()
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload)
    clearAll()
  }
  return {
    visible,
    draft,
    editor,
    changed,
    editorReadOnly,
    canSave,
    canAdopt,
    editableTask,
    canBlock,
    blockerDraft,
    dependencyPage,
    dependencyText,
    reorder,
    reorderPage,
    recoveryMessage,
    beginCreate,
    cancelCreate,
    adoptCurrent,
    save,
    checkOriginal,
    retryOriginal,
    abandonOriginal,
    loadDependencies,
    addBlocker,
    beginResolve,
    resolveBlocker,
    loadReorder,
    openReorder,
    saveReorder,
    blocked,
    readOnly,
    address,
    detail,
    milestones,
    sprints,
    tasks,
    blockers,
    filters,
    blockerStatus,
    expanded,
    treeOpen,
    selected,
    progress,
    dirty,
    confirmation,
    loadMilestones,
    loadSprints,
    loadTasks,
    loadBlockers,
    loadSelection,
    applyFilters,
    setBlockerStatus,
    expand,
    targetPath,
    confirmLeave,
    afterNavigation,
    confirm: () => finishConfirmation(true),
    cancelConfirmation: () => finishConfirmation(false),
    dispose,
  }
}
export type ProjectWorkPlanning = ReturnType<typeof createProjectWorkPlanning>
export const projectWorkPlanningKey: InjectionKey<ProjectWorkPlanning> =
  Symbol('project-work-planning')
export function useProjectWorkPlanning() {
  const value = inject(projectWorkPlanningKey)
  if (!value) throw new Error('Project work planning owner unavailable')
  return value
}
