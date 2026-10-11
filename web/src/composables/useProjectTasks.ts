import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  taskStates,
  type TaskState,
  type Task,
  type TaskSummary,
  type MilestoneSummary,
  type Milestone,
  type SprintSummary,
  type Sprint,
  type Blocker,
  type Page,
  type ReviewInput,
} from '../api/work-review'
import { captureTaskCreateDraft, type TaskCreateDraft } from '../api/work-task-planning'
import { agentLabel, type DirectoryAgent } from '../api/agent-directory'
import { workTaskRoute } from '../router/auth'
import { type SessionController } from './useSession'
import type { createProjectWorkspace } from './useProjectWorkspace'

type Phase = 'idle' | 'loading' | 'ready' | 'error'
export type TaskColumn = {
  phase: Phase
  items: readonly TaskSummary[]
  next?: string
  message: string
}
function emptyColumn(): TaskColumn {
  return { phase: 'idle', items: [], message: '' }
}
function explain(error: unknown): string {
  const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
  if (e.problem?.status === 401 || e.problem?.code === 'CSRF_FAILED')
    return '会话已失效，请重新登录。'
  if (e.problem?.status === 403 || e.problem?.status === 404)
    return '当前对象不可用或无权访问，请重新选择。'
  const messages: Record<string, string> = {
    VERSION_CONFLICT: '任务已发生变化。已保留输入，请重新读取后核对。',
    COMMENT_REQUIRED: '请填写评审说明。',
    TASK_ASSIGNEE_REQUIRED: '请选择接收此任务的 Agent。',
    RESOURCE_BUSY: '任务有尚未结束的操作，请稍后重新读取。',
    DEPENDENCY_UNBOUND: '当前任务条件尚不支持此操作，请重新读取后确认。',
    INVALID_STATE: '任务当前状态不允许此操作，请重新读取。',
    TASK_BLOCKERS_UNRESOLVED: '任务仍有未解除的阻塞。',
  }
  return messages[e.problem?.code ?? ''] ?? '未取得完整结果，请在当前请求结束后重新读取。'
}
export function createProjectTasks(
  auth: SessionController,
  workspace: ReturnType<typeof createProjectWorkspace>,
  navigate: (path: string) => Promise<unknown>,
) {
  const state = shallowReactive<{
    phase: Phase
    message: string
    milestones: readonly MilestoneSummary[]
    milestoneNext?: string
    milestone: Milestone | null
    sprints: readonly SprintSummary[]
    sprintNext?: string
    sprint: Sprint | null
    task: Task | null
    blockers: readonly Blocker[]
    blockerNext?: string
    blockerPhase: Phase
    agents: readonly DirectoryAgent[]
    agentNext?: string
    agentMessage: string
    detailCurrent: boolean
    feedback: string
  }>({
    phase: 'idle',
    message: '',
    milestones: [],
    milestone: null,
    sprints: [],
    sprint: null,
    task: null,
    blockers: [],
    blockerPhase: 'idle',
    agents: [],
    agentMessage: '',
    detailCurrent: false,
    feedback: '',
  })
  const columns = reactive(
    Object.fromEntries(taskStates.map((s) => [s, emptyColumn()])) as Record<TaskState, TaskColumn>,
  )
  const names = shallowReactive(new Map<string, DirectoryAgent>())
  const draft = reactive<{
    action: ReviewInput['request']['target_state'] | ''
    agentID: string
    comment: string
  }>({ action: '', agentID: '', comment: '' })
  const creation = reactive<{
    open: boolean
    title: string
    description: string
    plan: string
    type: TaskCreateDraft['type'] | ''
    priority: TaskCreateDraft['priority'] | ''
    message: string
  }>({ open: false, title: '', description: '', plan: '', type: '', priority: '', message: '' })
  const confirmation = reactive({ open: false, message: '' })
  let resolveLeave: ((value: boolean) => void) | null = null
  const reading = ref(false)
  const route = ref('')
  let manualMilestone: string | null = null
  let projectKey = '',
    generation = 0,
    requested = false,
    disposed = false
  const visible = computed(
    () =>
      !!workspace.currentReadContext.value &&
      !!workTaskRoute(route.value) &&
      workTaskRoute(route.value)?.projectPath === workspace.paths.value.home &&
      auth.state.phase === 'authenticated',
  )
  const pending = computed(() => {
    const p = auth.workReview.progress
    return p && ['submitting', 'uncertain'].includes(p.phase) ? p : null
  })
  const progress = computed(() => {
    const p = auth.workReview.progress
    if (!p || p.kind === 'create') return null
    return p.projectID === workspace.currentReadContext.value?.projectID &&
      p.taskID === state.task?.id
      ? p
      : null
  })
  const creationPending = computed(() => {
    const p = pending.value
    return visible.value &&
      p?.kind === 'create' &&
      p.projectID === workspace.currentReadContext.value?.projectID
      ? p
      : null
  })
  const creationProgress = computed(() => {
    const p = auth.workReview.progress
    return p?.kind === 'create' &&
      p.projectID === workspace.currentReadContext.value?.projectID &&
      p.sprintID === state.sprint?.id
      ? p
      : null
  })
  const busy = computed(() => auth.state.busy || reading.value || confirmation.open)
  const readOnly = computed(
    () =>
      !visible.value ||
      workspace.detail.project?.lifecycle !== 'active' ||
      state.sprint?.state !== 'current' ||
      !state.detailCurrent ||
      !!pending.value,
  )
  const canCreate = computed(
    () =>
      visible.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.phase === 'ready' &&
      state.sprint?.state === 'current' &&
      !state.task &&
      !pending.value,
  )
  const creationDirty = computed(
    () =>
      !!creation.title ||
      !!creation.description ||
      !!creation.plan ||
      !!creation.type ||
      !!creation.priority,
  )
  const dirty = computed(
    () => !!draft.comment || !!draft.agentID || creationDirty.value || !!pending.value,
  )
  const agentOptions = computed(() =>
    state.agents.map((a) => ({
      value: a.id,
      label: agentLabel(a) === a.name ? a.name : `${agentLabel(a)} (${a.name})`,
    })),
  )
  const name = (id: string | null) =>
    id === null ? '未指派' : names.has(id) ? agentLabel(names.get(id)!) : 'Agent 信息不可用'
  const projectPath = () => workTaskRoute(route.value)?.projectPath ?? ''
  const live = (own: number, p: string) =>
    !disposed &&
    own === generation &&
    visible.value &&
    workspace.currentReadContext.value?.projectID === p
  function clearDraft() {
    draft.action = ''
    draft.agentID = ''
    draft.comment = ''
  }
  function clearCreation() {
    Object.assign(creation, {
      open: false,
      title: '',
      description: '',
      plan: '',
      type: '',
      priority: '',
      message: '',
    })
  }
  function retire() {
    ++generation
    requested = false
    auth.workReview.abandonRead()
  }
  function clearData() {
    manualMilestone = null
    state.phase = 'idle'
    state.message = ''
    state.milestones = []
    state.milestone = null
    state.milestoneNext = undefined
    state.sprints = []
    state.sprint = null
    state.sprintNext = undefined
    state.task = null
    state.detailCurrent = false
    state.blockers = []
    state.blockerNext = undefined
    state.blockerPhase = 'idle'
    state.agents = []
    state.agentNext = undefined
    state.agentMessage = ''
    state.feedback = ''
    names.clear()
    for (const s of taskStates) Object.assign(columns[s], emptyColumn())
    clearDraft()
    clearCreation()
  }
  async function read<T>(
    fn: (project: string, step: <V>(work: () => Promise<V>) => Promise<V>) => Promise<T>,
    accept: (value: T) => void,
    fail: (error: unknown) => void = (error) => {
      state.message = explain(error)
    },
  ) {
    const context = workspace.currentReadContext.value
    if (!context || busy.value || !visible.value) return
    const own = generation,
      p = context.projectID
    reading.value = true
    async function step<V>(work: () => Promise<V>): Promise<V> {
      if (!live(own, p)) throw new AccountFailure('cancelled')
      const v = await work()
      if (!live(own, p)) throw new AccountFailure('cancelled')
      return v
    }
    try {
      const value = await fn(p, step)
      if (live(own, p)) accept(value)
    } catch (error) {
      if (live(own, p)) fail(error)
    } finally {
      reading.value = false
      void pump()
    }
  }
  async function hydrateNames(
    items: readonly TaskSummary[],
    p: string,
    step: <V>(work: () => Promise<V>) => Promise<V>,
  ) {
    for (const id of new Set(
      items.map((t) => t.assignee_agent_id).filter((v): v is string => v !== null),
    )) {
      if (names.has(id)) continue
      try {
        names.set(id, await step(() => auth.workReview.agent(p, id)))
      } catch (error) {
        if (error instanceof AccountFailure && [401, 403].includes(error.problem?.status ?? 0))
          throw error
      }
    }
  }
  async function loadColumn(s: TaskState, more = false) {
    const sprint = state.sprint
    if (!sprint) return
    const old = columns[s],
      cursor = more ? old.next : undefined
    if (more && !cursor) return
    await read(
      async (p, step) => {
        old.phase = 'loading'
        const page = await step(() =>
          auth.workReview.tasks(p, {
            sprint_id: sprint.id,
            state: s,
            limit: 50,
            ...(cursor ? { cursor } : {}),
          }),
        )
        await hydrateNames(page.items, p, step)
        return page
      },
      (page) => {
        const previous = more ? old.items : []
        if (page.items.some((row) => previous.some((x) => x.id === row.id))) {
          old.phase = 'error'
          old.message = '列表已变化，请重新读取。'
          return
        }
        old.items = [...previous, ...page.items]
        old.next = page.next_cursor
        old.phase = 'ready'
        old.message = ''
      },
      (error) => {
        old.phase = 'error'
        old.message = explain(error)
      },
    )
  }
  async function loadAgents(more = false) {
    const cursor = more ? state.agentNext : undefined
    if (more && !cursor) return
    await read(
      async (p, step) =>
        step(() => auth.workReview.agents(p, { limit: 50, ...(cursor ? { cursor } : {}) })),
      (page) => {
        const previous = more ? state.agents : []
        if (page.items.some((row) => previous.some((a) => a.id === row.id))) {
          state.agentMessage = '目录已变化，请重新读取。'
          return
        }
        state.agents = [...previous, ...page.items]
        state.agentNext = page.next_cursor
        state.agentMessage = ''
        for (const a of page.items) names.set(a.id, a)
      },
      (error) => {
        state.agentMessage = explain(error)
      },
    )
  }
  async function loadMilestones(more = false) {
    const cursor = more ? state.milestoneNext : undefined
    if (more && !cursor) return
    await read(
      async (p, step) =>
        step(() => auth.workReview.milestones(p, { limit: 50, ...(cursor ? { cursor } : {}) })),
      (page) => {
        state.milestones = merge(state.milestones, page, more)
        state.milestoneNext = page.next_cursor
      },
    )
  }
  async function loadSprints(more = false) {
    const milestone = state.milestone
    if (!milestone) return
    const cursor = more ? state.sprintNext : undefined
    if (more && !cursor) return
    await read(
      async (p, step) =>
        step(() =>
          auth.workReview.sprints(p, milestone.id, { limit: 50, ...(cursor ? { cursor } : {}) }),
        ),
      (page) => {
        state.sprints = merge(state.sprints, page, more)
        state.sprintNext = page.next_cursor
      },
    )
  }
  function merge<T extends { id: string }>(
    old: readonly T[],
    page: Page<T>,
    more: boolean,
  ): readonly T[] {
    if (more && page.items.some((x) => old.some((y) => x.id === y.id)))
      throw new AccountFailure('invalid-response')
    return more ? [...old, ...page.items] : page.items
  }
  async function loadBlockers(more = false) {
    const task = state.task
    if (!task) return
    const cursor = more ? state.blockerNext : undefined
    if (more && !cursor) return
    await read(
      async (p, step) => {
        state.blockerPhase = 'loading'
        return step(() =>
          auth.workReview.blockers(p, task.id, { limit: 50, ...(cursor ? { cursor } : {}) }),
        )
      },
      (page) => {
        state.blockers = merge(state.blockers, page, more)
        state.blockerNext = page.next_cursor
        state.blockerPhase = 'ready'
      },
      (error) => {
        state.blockerPhase = 'error'
        state.message = explain(error)
      },
    )
  }
  async function refresh() {
    requested = true
    await pump()
  }
  async function pump() {
    if (!requested || disposed || busy.value || !visible.value) return
    requested = false
    const selected = workTaskRoute(route.value)
    if (!selected) return
    state.phase = 'loading'
    state.message = ''
    state.detailCurrent = false
    await read(
      async (p, step) => {
        const task =
          selected.kind === 'task' ? await step(() => auth.workReview.task(p, selected.id!)) : null
        const sid =
          task?.sprint_id ??
          (selected.kind === 'sprint'
            ? selected.id
            : manualMilestone
              ? null
              : workspace.detail.project?.current_sprint_id)
        const sprint = sid ? await step(() => auth.workReview.sprint(p, sid)) : null
        const milestone = sprint
          ? await step(() => auth.workReview.milestone(p, sprint.milestone_id))
          : manualMilestone
            ? await step(() => auth.workReview.milestone(p, manualMilestone!))
            : null
        if (
          task &&
          (!sprint ||
            !milestone ||
            task.milestone_id !== milestone.id ||
            task.sprint_id !== sprint.id)
        )
          throw new AccountFailure('invalid-response')
        const milestones = await step(() => auth.workReview.milestones(p, { limit: 50 }))
        const sprints = milestone
          ? await step(() => auth.workReview.sprints(p, milestone.id, { limit: 50 }))
          : null
        const agents = await step(() => auth.workReview.agents(p, { limit: 50 }))
        return { task, sprint, milestone, milestones, sprints, agents }
      },
      (value) => {
        const changed = state.sprint?.id !== value.sprint?.id
        state.task = value.task
        state.sprint = value.sprint
        state.milestone = value.milestone
        state.milestones = value.milestones.items
        state.milestoneNext = value.milestones.next_cursor
        state.sprints = value.sprints?.items ?? []
        state.sprintNext = value.sprints?.next_cursor
        state.agents = value.agents.items
        state.agentNext = value.agents.next_cursor
        names.clear()
        for (const a of value.agents.items) names.set(a.id, a)
        state.phase = 'ready'
        state.detailCurrent = !!value.task
        state.blockers = []
        state.blockerNext = undefined
        if (changed) for (const s of taskStates) Object.assign(columns[s], emptyColumn())
      },
      (error) => {
        state.phase = 'error'
        state.message = explain(error)
        state.task = null
        state.detailCurrent = false
      },
    )
    // The read callbacks publish the phase after the asynchronous request returns.
    if ((state.phase as Phase) !== 'ready' || requested || !visible.value) return
    if (state.task) {
      await loadBlockers()
      if (state.task?.assignee_agent_id && !names.has(state.task.assignee_agent_id)) {
        const tid = state.task.assignee_agent_id
        await read(
          async (p, step) => step(() => auth.workReview.agent(p, tid)),
          (agent) => names.set(agent.id, agent),
          () => undefined,
        )
      }
    }
    for (const s of taskStates) {
      if (requested || !visible.value || busy.value) return
      await loadColumn(s)
    }
  }
  async function selectMilestone(id: string) {
    if (busy.value) return
    const target = `${projectPath()}/tasks`
    if (route.value === target) {
      if (!(await confirmLeave())) return
      manualMilestone = id
      clearDraft()
      await refresh()
      return
    }
    const previous = manualMilestone
    manualMilestone = id
    try {
      await navigate(target)
    } finally {
      if (route.value !== target) manualMilestone = previous
    }
  }
  async function selectSprint(id: string) {
    if (!busy.value) await navigate(`${projectPath()}/tasks/sprints/${id}`)
  }
  async function selectTask(id: string) {
    if (!busy.value) await navigate(`${projectPath()}/tasks/${id}`)
  }
  async function closeTask() {
    if (state.sprint && !busy.value)
      await navigate(`${projectPath()}/tasks/sprints/${state.sprint.id}`)
  }
  function chooseAction(action: ReviewInput['request']['target_state']) {
    if (readOnly.value || busy.value) return
    const task = state.task
    if (
      !task ||
      !(
        (task.state === 'in_progress' && action === 'in_review') ||
        (task.state === 'backlog' && action === 'todo') ||
        (task.state === 'in_review' && ['todo', 'done'].includes(action))
      )
    )
      return
    clearDraft()
    draft.action = action
    state.feedback = ''
  }
  async function submit() {
    const p = workspace.currentReadContext.value?.projectID,
      task = state.task
    if (!p || !task || !draft.action || readOnly.value || busy.value) return
    try {
      if (auth.workReview.progress?.phase === 'rejected') auth.workReview.editRejected()
      if (task.state === 'backlog' && draft.action === 'todo') {
        await auth.workReview.startReady(p, task.id, {
          expected_version: task.version,
          request: {
            target_state: 'todo',
            assignee_agent_id: draft.agentID,
            ...(draft.comment === '' ? {} : { comment: draft.comment }),
          },
        })
      } else {
        const input: ReviewInput = {
          expected_version: task.version,
          request: {
            target_state: draft.action,
            comment: draft.comment,
            ...(draft.action === 'done' ? {} : { assignee_agent_id: draft.agentID }),
          },
        }
        await auth.workReview.startTransfer(p, task.id, input)
      }
      if (
        visible.value &&
        workspace.currentReadContext.value?.projectID === p &&
        state.task?.id === task.id
      ) {
        clearDraft()
        state.feedback = '任务状态已更新。'
        await refresh()
      }
    } catch (error) {
      if (visible.value) {
        state.message = explain(error)
        if (auth.workReview.progress?.phase === 'rejected') state.detailCurrent = false
      }
    }
  }
  async function lookup() {
    if (busy.value || !progress.value?.contextValid || progress.value.phase !== 'uncertain') return
    try {
      const result = await auth.workReview.checkOriginal()
      if ('status' in result && result.status !== 'committed') return
      clearDraft()
      state.feedback = '已确认原操作结果。'
      await refresh()
    } catch (error) {
      state.message = explain(error)
    }
  }
  function openCreation() {
    if (!canCreate.value || busy.value) return
    clearDraft()
    creation.open = true
    creation.message = ''
  }
  async function cancelCreation() {
    if (busy.value || pending.value) return
    if (creationDirty.value && !(await confirmLeave())) return
    clearCreation()
  }
  async function revealCreated(project: string, task: Task) {
    if (!visible.value || workspace.currentReadContext.value?.projectID !== project) return
    clearCreation()
    await navigate(`${projectPath()}/tasks/${task.id}`)
    if (visible.value && workspace.currentReadContext.value?.projectID === project)
      state.feedback = '任务已创建，可选择 Agent 加入待执行。'
  }
  async function submitCreation() {
    const p = workspace.currentReadContext.value?.projectID,
      sprint = state.sprint,
      own = generation
    if (!p || !sprint || !creation.open || !canCreate.value || busy.value) return
    try {
      const value = captureTaskCreateDraft({
        sprint_id: sprint.id,
        title: creation.title,
        description: creation.description,
        type: creation.type,
        priority: creation.priority,
        plan: creation.plan,
      })
      if (auth.workReview.progress?.phase === 'rejected') auth.workReview.editRejected()
      const result = await auth.workReview.startCreate(p, value)
      if (!('status' in result) && live(own, p)) await revealCreated(p, result.task)
    } catch (error) {
      if (live(own, p))
        creation.message =
          error instanceof AccountFailure && error.kind === 'invalid-input'
            ? '请填写合法的标题、类型和优先级，并核对描述与计划长度。'
            : explain(error)
    }
  }
  async function lookupCreation() {
    const p = creationProgress.value,
      own = generation
    if (busy.value || p?.phase !== 'uncertain' || !p.contextValid) return
    try {
      const result = await auth.workReview.checkOriginal()
      if ('status' in result && result.status === 'committed' && live(own, p.projectID))
        await revealCreated(p.projectID, result.receipt.task)
    } catch (error) {
      if (live(own, p.projectID)) creation.message = explain(error)
    }
  }
  function confirmLeave(target?: string): Promise<boolean> {
    if (target === route.value || !dirty.value) return Promise.resolve(true)
    confirmation.message = pending.value
      ? '操作结果尚未确认。离开不会重发，返回原 Sprint 或任务后可继续查询原结果。'
      : '离开将放弃尚未提交的任务输入。'
    confirmation.open = true
    return new Promise((resolve) => {
      resolveLeave = resolve
    })
  }
  function finishLeave(yes: boolean) {
    confirmation.open = false
    const resolve = resolveLeave
    resolveLeave = null
    if (yes) {
      clearDraft()
      clearCreation()
    }
    resolve?.(yes)
  }
  function afterNavigation(to: string) {
    if (to === route.value) return
    retire()
    route.value = to
    if (workTaskRoute(to)?.kind !== 'root') manualMilestone = null
    clearDraft()
    clearCreation()
    state.task = null
    state.detailCurrent = false
    state.feedback = ''
    if (workTaskRoute(to)) {
      requested = true
      void pump()
    }
  }
  const stop = watch(
    () =>
      [workspace.currentReadContext.value, auth.state.busy, auth.personalContext.identity] as const,
    () => {
      const context = workspace.currentReadContext.value,
        identity = auth.personalContext.identity
      const next = identity
        ? `${identity.userID}/${identity.sessionID}/${identity.epoch}/${context?.projectID ?? ''}`
        : ''
      if (!context) {
        if (!identity) {
          retire()
          clearData()
          projectKey = ''
        }
        return
      }
      if (next !== projectKey) {
        retire()
        clearData()
        projectKey = next
        requested = true
      }
      void pump()
    },
    { flush: 'post', immediate: true },
  )
  function dispose() {
    disposed = true
    retire()
    stop()
    finishLeave(false)
    clearData()
  }
  return {
    state,
    columns,
    draft,
    creation,
    creationPending,
    creationProgress,
    canCreate,
    confirmation,
    visible,
    busy,
    readOnly,
    progress,
    pending,
    agentOptions,
    name,
    refresh,
    loadColumn,
    loadAgents,
    loadMilestones,
    loadSprints,
    loadBlockers,
    selectMilestone,
    selectSprint,
    selectTask,
    closeTask,
    chooseAction,
    submit,
    openCreation,
    cancelCreation,
    submitCreation,
    lookupCreation,
    lookup,
    confirmLeave,
    afterNavigation,
    confirm: () => finishLeave(true),
    cancelConfirmation: () => finishLeave(false),
    dispose,
  }
}
export type ProjectTasks = ReturnType<typeof createProjectTasks>
export const projectTasksKey: InjectionKey<ProjectTasks> = Symbol('project-tasks')
export function useProjectTasks() {
  const owner = inject(projectTasksKey)
  if (!owner) throw new Error('Task owner is missing')
  return owner
}
