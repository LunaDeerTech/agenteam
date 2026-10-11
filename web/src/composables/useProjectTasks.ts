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
import { captureSprintCreateDraft, type SprintCreateReceipt } from '../api/work-sprint-lifecycle'
import { captureTaskCreateDraft, type TaskCreateDraft } from '../api/work-task-planning'
import { compareTaskTimelineEvents, type TaskTimelineEvent } from '../api/work-task-timeline'
import { captureTaskUpdateInput } from '../api/work-task-edit'
import { agentLabel, type DirectoryAgent } from '../api/agent-directory'
import { workTaskRoute } from '../router/auth'
import { type SessionController } from './useSession'
import type { createProjectWorkspace } from './useProjectWorkspace'

type Phase = 'idle' | 'loading' | 'ready' | 'error'
export type TaskTimelineState = {
  phase: Phase
  items: readonly TaskTimelineEvent[]
  next?: string
  message: string
  retryMore: boolean
}
const editFields = ['title', 'description', 'type', 'priority', 'plan'] as const
type EditField = (typeof editFields)[number]
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
    TASK_VERSION_CONFLICT: '任务已发生变化。已保留输入，请重新读取后核对。',
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
  const timeline = shallowReactive<TaskTimelineState>({
    phase: 'idle',
    items: [],
    message: '',
    retryMore: false,
  })
  function clearTimeline() {
    Object.assign(timeline, {
      phase: 'idle',
      items: [],
      next: undefined,
      message: '',
      retryMore: false,
    })
  }
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
  const edit = reactive({
    open: false,
    projectID: '',
    taskID: '',
    version: '',
    title: '',
    description: '',
    type: '' as Task['type'] | '',
    priority: '' as Task['priority'] | '',
    plan: '',
    touched: { title: false, description: false, type: false, priority: false, plan: false },
    message: '',
  })
  const sprintCreation = reactive({
    open: false,
    projectID: '',
    milestoneID: '',
    sprintID: '',
    title: '',
    description: '',
    message: '',
    needsRead: false,
  })
  let sprintCreationIdentity = ''
  const sprintStart = reactive({
    open: false,
    projectID: '',
    sprintID: '',
    version: '',
    message: '',
    needsRead: false,
    confirmed: false,
  })
  const syncingSprint = ref(false)
  let sprintStartIdentity = ''
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
  const sprintCreatePending = computed(() => {
    const p = auth.workSprintLifecycle.createProgress
    return p && ['submitting', 'uncertain'].includes(p.phase) ? p : null
  })
  const sprintCreateProgress = computed(() => {
    const p = auth.workSprintLifecycle.createProgress
    return visible.value && p?.projectID === workspace.currentReadContext.value?.projectID
      ? p
      : null
  })
  const sprintMilestoneOptions = computed(() => {
    const rows = [...state.milestones]
    if (state.milestone && !rows.some((m) => m.id === state.milestone!.id))
      rows.unshift(state.milestone)
    return rows.map((m) => ({ value: m.id, label: m.title }))
  })
  const canCreateSprint = computed(
    () =>
      visible.value &&
      state.phase === 'ready' &&
      !state.task &&
      workspace.detail.project?.lifecycle === 'active' &&
      !!sprintMilestoneOptions.value.length &&
      !pending.value &&
      !sprintCreatePending.value &&
      !sprintPending.value &&
      !sprintStart.open &&
      !sprintStart.needsRead &&
      !creation.open &&
      !edit.open &&
      !draft.action &&
      !sprintCreation.needsRead,
  )
  const sprintPending = computed(() => {
    const p = auth.workSprintLifecycle.progress
    return p && ['submitting', 'uncertain'].includes(p.phase) ? p : null
  })
  const sprintProgress = computed(() => {
    const p = auth.workSprintLifecycle.progress
    return visible.value &&
      p &&
      p.projectID === workspace.currentReadContext.value?.projectID &&
      p.sprintID === state.sprint?.id
      ? p
      : null
  })
  const sprintPendingHere = computed(() => {
    const p = sprintPending.value
    return visible.value && p?.projectID === workspace.currentReadContext.value?.projectID
      ? p
      : null
  })
  const busy = computed(
    () => auth.state.busy || reading.value || syncingSprint.value || confirmation.open,
  )
  const canStartSprint = computed(
    () =>
      visible.value &&
      state.phase === 'ready' &&
      !state.task &&
      workspace.detail.project?.lifecycle === 'active' &&
      workspace.detail.project.current_sprint_id === null &&
      state.sprint?.state === 'planned' &&
      state.sprint.project_id === workspace.currentReadContext.value?.projectID &&
      !pending.value &&
      !sprintPending.value &&
      !sprintCreatePending.value &&
      !sprintCreation.open &&
      !sprintCreation.needsRead &&
      !creation.open &&
      !edit.open &&
      !draft.action &&
      !sprintStart.needsRead &&
      sprintProgress.value?.phase !== 'confirmed',
  )
  const sprintStartStale = computed(
    () =>
      sprintStart.open &&
      (sprintStart.projectID !== workspace.currentReadContext.value?.projectID ||
        sprintStart.sprintID !== state.sprint?.id ||
        sprintStart.version !== state.sprint?.version),
  )
  const sprintReadRecovery = computed(
    () =>
      sprintStart.needsRead &&
      sameSprintLocation(
        generation,
        sprintStart.projectID,
        sprintStart.sprintID,
        sprintStartIdentity,
      ),
  )
  const readOnly = computed(
    () =>
      !visible.value ||
      workspace.detail.project?.lifecycle !== 'active' ||
      state.sprint?.state !== 'current' ||
      !state.detailCurrent ||
      !!pending.value ||
      !!sprintPending.value ||
      !!sprintCreatePending.value ||
      sprintCreation.open ||
      sprintCreation.needsRead ||
      sprintStart.open ||
      sprintStart.needsRead,
  )
  const canCreate = computed(
    () =>
      visible.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.phase === 'ready' &&
      state.sprint?.state === 'current' &&
      !state.task &&
      !pending.value &&
      !sprintPending.value &&
      !sprintCreatePending.value &&
      !sprintCreation.open &&
      !sprintCreation.needsRead &&
      !sprintStart.open &&
      !sprintStart.needsRead,
  )
  const canEdit = computed(
    () =>
      visible.value &&
      workspace.detail.project?.lifecycle === 'active' &&
      state.detailCurrent &&
      !!state.sprint &&
      ['planned', 'current'].includes(state.sprint.state) &&
      state.task?.state === 'backlog' &&
      state.task.assignee_agent_id === null &&
      !pending.value &&
      !sprintPending.value &&
      !sprintCreatePending.value &&
      !sprintCreation.open &&
      !sprintCreation.needsRead &&
      !sprintStart.open &&
      !sprintStart.needsRead,
  )
  const editDirty = computed(() => edit.open && editFields.some((field) => edit.touched[field]))
  const editStale = computed(
    () =>
      edit.open &&
      (edit.projectID !== workspace.currentReadContext.value?.projectID ||
        edit.taskID !== state.task?.id ||
        edit.version !== state.task?.version),
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
    () =>
      !!draft.comment ||
      !!draft.agentID ||
      creationDirty.value ||
      editDirty.value ||
      !!pending.value ||
      !!sprintPending.value ||
      !!sprintCreatePending.value ||
      sprintCreation.open ||
      sprintCreation.needsRead ||
      sprintStart.open,
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
  function identityKey(project: string) {
    const who = auth.personalContext.identity
    return who ? `${who.userID}/${who.sessionID}/${who.epoch}/${project}` : ''
  }
  function sameSprintLocation(own: number, project: string, sprint: string, identity: string) {
    return (
      !disposed &&
      own === generation &&
      auth.state.phase === 'authenticated' &&
      !!identity &&
      identityKey(project) === identity &&
      workTaskRoute(route.value)?.projectPath === workspace.paths.value.home &&
      workspace.detail.project?.id === project &&
      state.sprint?.id === sprint &&
      !state.task
    )
  }
  function clearSprintCreation() {
    Object.assign(sprintCreation, {
      open: false,
      projectID: '',
      milestoneID: '',
      sprintID: '',
      title: '',
      description: '',
      message: '',
      needsRead: false,
    })
    sprintCreationIdentity = ''
  }
  function clearSprintStart() {
    Object.assign(sprintStart, {
      open: false,
      projectID: '',
      sprintID: '',
      version: '',
      message: '',
      needsRead: false,
      confirmed: false,
    })
    sprintStartIdentity = ''
  }
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
  function clearEdit() {
    Object.assign(edit, {
      open: false,
      projectID: '',
      taskID: '',
      version: '',
      title: '',
      description: '',
      type: '',
      priority: '',
      plan: '',
      message: '',
      touched: { title: false, description: false, type: false, priority: false, plan: false },
    })
  }
  function retire() {
    ++generation
    requested = false
    auth.workReview.abandonRead()
  }
  function clearData() {
    clearTimeline()
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
    clearEdit()
    clearSprintStart()
    clearSprintCreation()
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
  async function loadTimeline(more = false) {
    const task = state.task
    if (!task || !state.detailCurrent) return
    const cursor = more ? timeline.next : undefined
    if (more && !cursor) return
    await read(
      async (p, step) => {
        if (!more) clearTimeline()
        timeline.phase = 'loading'
        timeline.message = ''
        timeline.retryMore = more
        return step(() =>
          auth.workReview.events(p, task.id, {
            limit: 50,
            order: 'desc',
            ...(cursor ? { cursor } : {}),
          }),
        )
      },
      (page) => {
        if (state.task?.id !== task.id) return
        // Reject inconsistent continuation atomically; never hide duplicate events.
        const before = more ? timeline.items : []
        const seen = new Set(before.map((item) => item.id))
        const chunk = [...page.items].reverse()
        for (let n = 0; n < chunk.length; n++) {
          const item = chunk[n]!
          if (
            item.project_id !== task.project_id ||
            item.task_id !== task.id ||
            seen.has(item.id) ||
            (n > 0 && compareTaskTimelineEvents(chunk[n - 1]!, item) >= 0)
          )
            throw new AccountFailure('invalid-response')
          seen.add(item.id)
        }
        if (
          (chunk.length &&
            before.length &&
            compareTaskTimelineEvents(chunk[chunk.length - 1]!, before[0]!) >= 0) ||
          (page.next_cursor && (!chunk.length || page.next_cursor === cursor))
        )
          throw new AccountFailure('invalid-response')
        timeline.items = Object.freeze([...chunk, ...before])
        timeline.next = page.next_cursor
        timeline.phase = 'ready'
      },
      (error) => {
        if (state.task?.id !== task.id) return
        if (
          error instanceof AccountFailure &&
          [401, 403, 404].includes(error.problem?.status ?? 0)
        ) {
          timeline.items = []
          timeline.next = undefined
          timeline.retryMore = false
        }
        timeline.phase = 'error'
        timeline.message =
          error instanceof AccountFailure && error.problem?.code === 'CURSOR_INVALID'
            ? '历史分页已不可用，请重新读取历史。'
            : explain(error)
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
    clearTimeline()
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
      if (requested || !visible.value || busy.value) return
      await loadTimeline()
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
  function openSprintCreation() {
    const p = workspace.currentReadContext.value?.projectID
    if (!p || busy.value || !canCreateSprint.value) return
    if (
      auth.workSprintLifecycle.progress?.phase === 'rejected' &&
      auth.workSprintLifecycle.progress.contextValid
    )
      auth.workSprintLifecycle.editRejected()
    clearSprintCreation()
    Object.assign(sprintCreation, {
      open: true,
      projectID: p,
      milestoneID: state.milestone?.id ?? '',
    })
    sprintCreationIdentity = identityKey(p)
  }
  function cancelSprintCreation() {
    if (busy.value || sprintCreatePending.value) return
    if (sprintCreateProgress.value?.phase === 'rejected' && sprintCreateProgress.value.contextValid)
      auth.workSprintLifecycle.editRejectedCreate()
    clearSprintCreation()
  }
  function sameSprintCreation(own: number, project: string, milestone: string, identity: string) {
    return (
      live(own, project) &&
      !!identity &&
      identityKey(project) === identity &&
      !state.task &&
      sprintCreation.projectID === project &&
      sprintCreation.milestoneID === milestone
    )
  }
  async function rereadCreatedSprint() {
    const p = sprintCreation.projectID,
      m = sprintCreation.milestoneID,
      sid = sprintCreation.sprintID,
      own = generation,
      identity = sprintCreationIdentity
    if (busy.value || !sprintCreation.needsRead || !sid || !sameSprintCreation(own, p, m, identity))
      return
    let selected = false
    await read(
      async (_p, step) => {
        const sprint = await step(() => auth.workReview.sprint(p, sid))
        const milestone = await step(() => auth.workReview.milestone(p, m))
        const page = await step(() => auth.workReview.sprints(p, m, { limit: 50 }))
        if (
          sprint.id !== sid ||
          sprint.project_id !== p ||
          sprint.milestone_id !== m ||
          milestone.id !== m ||
          milestone.project_id !== p
        )
          throw new AccountFailure('invalid-response')
        return { sprint, milestone, page }
      },
      (value) => {
        if (!sameSprintCreation(own, p, m, identity)) return
        // Publish actual current reads, never a synthetic list row or receipt as current state.
        state.sprint = value.sprint
        state.milestone = value.milestone
        state.sprints = value.page.items
        state.sprintNext = value.page.next_cursor
        sprintCreation.needsRead = false
        sprintCreation.open = false
        sprintCreation.message = 'Sprint 已创建；启动需单独确认。'
        selected = true
      },
      () => {
        if (sameSprintCreation(own, p, m, identity))
          sprintCreation.message =
            '创建已确认，当前 Sprint 信息读取失败。请重新读取，不要再次创建。'
      },
    )
    if (selected && sameSprintCreation(own, p, m, identity)) await selectSprint(sid)
  }
  async function acceptSprintCreation(value: SprintCreateReceipt, own: number, identity: string) {
    const p = value.sprint.project_id,
      m = value.sprint.milestone_id
    if (!sameSprintCreation(own, p, m, identity)) return
    Object.assign(sprintCreation, {
      open: false,
      sprintID: value.sprint.id,
      needsRead: true,
      message: '创建已确认，正在读取当前 Sprint。',
    })
    await rereadCreatedSprint()
  }
  async function submitSprintCreation() {
    const p = sprintCreation.projectID,
      m = sprintCreation.milestoneID,
      own = generation,
      identity = sprintCreationIdentity
    if (
      busy.value ||
      !sprintCreation.open ||
      !canCreateSprint.value ||
      !sameSprintCreation(own, p, m, identity) ||
      !sprintMilestoneOptions.value.some((row) => row.value === m)
    )
      return
    try {
      const draft = captureSprintCreateDraft({
        milestone_id: m,
        title: sprintCreation.title,
        ...(sprintCreation.description ? { description: sprintCreation.description } : {}),
      })
      if (auth.workSprintLifecycle.createProgress?.phase === 'rejected')
        auth.workSprintLifecycle.editRejectedCreate()
      const value = await auth.workSprintLifecycle.create(p, draft)
      const result = 'state' in value ? value.result : value
      if (result) await acceptSprintCreation(result, own, identity)
    } catch (error) {
      if (sameSprintCreation(own, p, m, identity))
        sprintCreation.message =
          error instanceof AccountFailure && error.kind === 'invalid-input'
            ? '请选择有效 Milestone，并填写合法标题与描述。'
            : explain(error)
    }
  }
  async function lookupSprintCreation() {
    const progress = sprintCreateProgress.value,
      own = generation
    if (busy.value || progress?.phase !== 'uncertain' || !progress.contextValid || state.task)
      return
    Object.assign(sprintCreation, {
      open: false,
      projectID: progress.projectID,
      milestoneID: progress.milestoneID,
      sprintID: progress.sprintID,
    })
    sprintCreationIdentity = identityKey(progress.projectID)
    const identity = sprintCreationIdentity
    try {
      const value = await auth.workSprintLifecycle.checkOriginalCreate()
      const result = 'state' in value ? value.result : value
      if (result) await acceptSprintCreation(result, own, identity)
    } catch (error) {
      if (sameSprintCreation(own, progress.projectID, progress.milestoneID, identity))
        sprintCreation.message = explain(error)
    }
  }
  async function rereadConfirmedCreation() {
    const progress = sprintCreateProgress.value
    if (
      busy.value ||
      progress?.phase !== 'confirmed' ||
      !progress.contextValid ||
      !progress.result ||
      state.task
    )
      return
    Object.assign(sprintCreation, {
      open: false,
      projectID: progress.projectID,
      milestoneID: progress.milestoneID,
      sprintID: progress.sprintID,
      needsRead: true,
    })
    sprintCreationIdentity = identityKey(progress.projectID)
    await rereadCreatedSprint()
  }
  function openSprintStart() {
    const p = workspace.currentReadContext.value?.projectID,
      sprint = state.sprint
    if (!p || !sprint || !canStartSprint.value || busy.value) return
    if (sprintCreateProgress.value?.phase === 'rejected' && sprintCreateProgress.value.contextValid)
      auth.workSprintLifecycle.editRejectedCreate()
    Object.assign(sprintStart, {
      open: true,
      projectID: p,
      sprintID: sprint.id,
      version: sprint.version,
      message: '',
      needsRead: false,
      confirmed: false,
    })
    sprintStartIdentity = identityKey(p)
  }
  function cancelSprintStart() {
    if (busy.value || sprintPending.value) return
    clearSprintStart()
  }
  async function rereadSprintStart() {
    const p = sprintStart.projectID,
      sid = sprintStart.sprintID,
      own = generation,
      identity = sprintStartIdentity
    if (busy.value || !sprintStart.needsRead || !sameSprintLocation(own, p, sid, identity)) return
    syncingSprint.value = true
    sprintStart.message = sprintStart.confirmed
      ? '启动已确认，正在读取当前项目信息。'
      : '正在重新读取启动条件。'
    let reload = false
    try {
      // A historical command receipt never replaces the current Project read.
      await workspace.readCurrent()
      if (!sameSprintLocation(own, p, sid, identity)) return
      if (workspace.currentReadContext.value?.projectID !== p)
        throw new AccountFailure('invalid-response')
      const sprint = await auth.workReview.sprint(p, sid)
      if (!sameSprintLocation(own, p, sid, identity) || !live(own, p)) return
      if (sprint.id !== sid || sprint.project_id !== p) throw new AccountFailure('invalid-response')
      state.sprint = sprint
      state.sprints = state.sprints.map((row) => (row.id === sid ? sprint : row))
      if (
        sprintStart.confirmed &&
        (workspace.detail.project?.current_sprint_id !== sid || sprint.state !== 'current')
      ) {
        sprintStart.message = '启动已确认，但当前 Sprint 已变化。请按当前项目信息重新选择。'
        return
      }
      sprintStart.needsRead = false
      sprintStart.message = sprintStart.confirmed
        ? 'Sprint 已启动，已确认当前 Sprint。'
        : '已重新读取启动条件，请核对后明确启动。'
      reload = true
    } catch {
      if (sameSprintLocation(own, p, sid, identity))
        sprintStart.message = sprintStart.confirmed
          ? '启动已确认，当前信息读取失败。请重新读取，不要再次启动。'
          : '启动条件读取失败，请重试读取。'
    } finally {
      syncingSprint.value = false
      if (!sameSprintLocation(own, p, sid, identity)) void pump()
    }
    if (reload && sameSprintLocation(own, p, sid, identity)) {
      await refresh()
    }
  }
  async function acceptSprintStart(project: string, sprint: string, own: number, identity: string) {
    if (!sameSprintLocation(own, project, sprint, identity)) return
    Object.assign(sprintStart, {
      open: false,
      projectID: project,
      sprintID: sprint,
      needsRead: true,
      confirmed: true,
    })
    sprintStartIdentity = identity
    await rereadSprintStart()
  }
  async function submitSprintStart() {
    const p = sprintStart.projectID,
      sid = sprintStart.sprintID,
      own = generation,
      identity = sprintStartIdentity
    if (!sprintStart.open || !canStartSprint.value || sprintStartStale.value || busy.value) return
    try {
      if (auth.workSprintLifecycle.progress?.phase === 'rejected')
        auth.workSprintLifecycle.editRejected()
      const result = await auth.workSprintLifecycle.start(p, sid, {
        expected_version: sprintStart.version,
        request: {},
      })
      const receipt = 'status' in result ? result.receipt : result
      if (!receipt || receipt.project.id !== p || receipt.sprint.id !== sid) return
      await acceptSprintStart(p, sid, own, identity)
    } catch (error) {
      if (sameSprintLocation(own, p, sid, identity)) {
        sprintStart.message =
          error instanceof AccountFailure &&
          ['VERSION_CONFLICT', 'SPRINT_VERSION_CONFLICT'].includes(error.problem?.code ?? '')
            ? 'Sprint 已发生变化，请重新读取后核对，再明确启动。'
            : '未确认 Sprint 启动。请查看原操作状态；明确拒绝后可重新读取。'
        if (auth.workSprintLifecycle.progress?.phase === 'rejected') {
          sprintStart.open = false
          sprintStart.needsRead = true
        }
      }
    }
  }
  async function rereadConfirmedSprint() {
    const p = sprintProgress.value
    if (busy.value || p?.phase !== 'confirmed' || !p.contextValid) return
    await acceptSprintStart(p.projectID, p.sprintID, generation, identityKey(p.projectID))
  }
  async function lookupSprintStart() {
    const original = sprintProgress.value,
      own = generation
    if (busy.value || original?.phase !== 'uncertain' || !original.contextValid) return
    const identity = identityKey(original.projectID)
    try {
      const result = await auth.workSprintLifecycle.checkOriginal()
      const receipt = 'status' in result ? result.receipt : result
      if (
        !receipt ||
        receipt.project.id !== original.projectID ||
        receipt.sprint.id !== original.sprintID
      )
        return
      await acceptSprintStart(original.projectID, original.sprintID, own, identity)
    } catch {
      if (sameSprintLocation(own, original.projectID, original.sprintID, identity))
        sprintStart.message = '未取得原启动结果，可在原请求结束后再次查询。'
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
    if (readOnly.value || busy.value || edit.open) return
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
    if (!p || !task || !draft.action || readOnly.value || busy.value || edit.open) return
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
    const original = progress.value,
      own = generation
    if (busy.value || !original?.contextValid || original.phase !== 'uncertain') return
    try {
      const result = await auth.workReview.checkOriginal()
      if ('status' in result && result.status !== 'committed') return
      if (!live(own, original.projectID) || state.task?.id !== original.taskID) return
      clearDraft()
      clearEdit()
      const receipt = 'status' in result ? result.receipt : result
      state.feedback =
        original.kind === 'edit' && 'changed' in receipt && !receipt.changed
          ? '已确认原保存结果，任务内容没有变化。'
          : '已确认原操作结果。'
      await refresh()
      if (original.kind === 'edit' && live(own, original.projectID) && state.phase === 'error')
        state.message = `${state.feedback} ${state.message}`
    } catch (error) {
      if (live(own, original.projectID) && state.task?.id === original.taskID)
        state.message = explain(error)
    }
  }
  function openEdit() {
    const task = state.task
    if (!task || !canEdit.value || busy.value || draft.action || edit.open) return
    clearEdit()
    Object.assign(edit, {
      projectID: task.project_id,
      taskID: task.id,
      version: task.version,
      title: task.title,
      description: task.description,
      type: task.type,
      priority: task.priority,
      plan: task.plan,
      open: true,
    })
    state.feedback = ''
  }
  function touchEdit(field: EditField, value: string) {
    if (!edit.open || busy.value || !canEdit.value || editStale.value) return
    if (field === 'type') {
      if (!['feature', 'bug', 'task', 'spike', 'chore'].includes(value)) return
      edit.type = value as Task['type']
    } else if (field === 'priority') {
      if (!['critical', 'high', 'medium', 'low'].includes(value)) return
      edit.priority = value as Task['priority']
    } else edit[field] = value
    edit.touched[field] = true
  }
  async function cancelEdit() {
    if (busy.value || pending.value) return
    if (editDirty.value && !(await confirmLeave())) return
    clearEdit()
  }
  async function submitEdit() {
    const p = workspace.currentReadContext.value?.projectID,
      task = state.task,
      own = generation
    if (
      !p ||
      !task ||
      !edit.open ||
      !canEdit.value ||
      !editDirty.value ||
      editStale.value ||
      busy.value
    )
      return
    try {
      const input = captureTaskUpdateInput({
        expected_version: edit.version,
        request: Object.fromEntries(
          editFields.filter((field) => edit.touched[field]).map((field) => [field, edit[field]]),
        ),
      })
      if (auth.workReview.progress?.phase === 'rejected') auth.workReview.editRejected()
      const result = await auth.workReview.startEdit(p, task.id, input)
      if (
        'status' in result ||
        !('changed' in result) ||
        !live(own, p) ||
        state.task?.id !== task.id
      )
        return
      clearEdit()
      const message = result.changed ? '任务已保存。' : '已确认保存，任务内容没有变化。'
      state.feedback = message
      await refresh()
      if (live(own, p) && state.phase === 'error') state.message = `${message} ${state.message}`
    } catch (error) {
      if (live(own, p) && state.task?.id === task.id) {
        edit.message =
          error instanceof AccountFailure && error.kind === 'invalid-input'
            ? '请核对标题、类型、优先级和文本长度。'
            : explain(error)
        if (auth.workReview.progress?.phase === 'rejected') state.detailCurrent = false
      }
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
    confirmation.message =
      pending.value || sprintPending.value || sprintCreatePending.value
        ? '操作结果尚未确认。离开不会重发，返回原 Sprint 或任务后可继续查询原结果。'
        : '离开将放弃尚未提交的工作输入。'
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
      clearEdit()
      clearSprintStart()
      clearSprintCreation()
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
    clearEdit()
    clearSprintStart()
    clearSprintCreation()
    clearTimeline()
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
    timeline,
    columns,
    draft,
    creation,
    edit,
    sprintStart,
    sprintCreation,
    sprintCreateProgress,
    sprintCreatePending,
    sprintMilestoneOptions,
    canCreateSprint,
    openSprintCreation,
    cancelSprintCreation,
    submitSprintCreation,
    lookupSprintCreation,
    rereadCreatedSprint,
    rereadConfirmedCreation,
    sprintPending,
    sprintPendingHere,
    sprintProgress,
    canStartSprint,
    sprintStartStale,
    sprintReadRecovery,
    openSprintStart,
    cancelSprintStart,
    submitSprintStart,
    lookupSprintStart,
    rereadSprintStart,
    rereadConfirmedSprint,
    canEdit,
    editDirty,
    editStale,
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
    loadTimeline,
    selectMilestone,
    selectSprint,
    selectTask,
    closeTask,
    chooseAction,
    submit,
    openEdit,
    touchEdit,
    cancelEdit,
    submitEdit,
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
