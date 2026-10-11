import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import { agentLabel, type DirectoryAgent } from '../api/agent-directory'
import type {
  AgentApprovalPolicy,
  AgentConfiguration,
  AgentCreateFields,
  AgentCreateReceipt,
  AgentUpdateFields,
  AgentUpdateReceipt,
} from '../api/agent-configuration'
import type { ProjectAvailableChatModel } from '../api/project-models'
import { projectRoute } from '../router/auth'
import type { SessionController } from './useSession'
import type { ProjectWorkspace } from './useProjectWorkspace'

type EditField = 'name' | 'description' | 'instructions'
const initialEdit = () => ({
  open: false,
  agentID: '',
  version: '',
  name: '',
  description: '',
  instructions: '',
  touched: {} as Partial<Record<EditField, true>>,
  conflict: false,
  message: '',
})
type Phase = 'idle' | 'loading' | 'ready' | 'error'
const initialDraft = () => ({
  open: false,
  name: '',
  displayName: '',
  tagColor: '',
  description: '',
  instructions: '',
  modelID: '',
  reasoningEffort: '',
  injectAgentsMD: '' as '' | 'yes' | 'no',
  approvalPolicy: '' as '' | AgentApprovalPolicy,
  approvalModelID: '',
  addSkills: true,
  installSkill: true,
  message: '',
})
function explain(error: unknown) {
  const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
  if (e.problem?.status === 401 || e.problem?.code === 'CSRF_FAILED')
    return '会话已失效，请重新登录。'
  if ([403, 404].includes(e.problem?.status ?? 0)) return '当前项目或 Agent 不可用，或无权访问。'
  if (e.problem?.code === 'VERSION_CONFLICT')
    return 'Agent 配置已有新版本。请取消编辑并重新读取，核对后再保存。'
  if (e.problem?.code === 'CURSOR_INVALID') return '目录已变化，请从第一页重新读取。'
  if (e.kind === 'invalid-input')
    return '请核对名称、模型和必填配置。名称须为 3–32 位英文字母、数字或连字符。'
  return '未取得完整结果，请在原请求结束后重新读取。'
}
export function createProjectAgents(auth: SessionController, workspace: ProjectWorkspace) {
  const state = shallowReactive<{
    phase: Phase
    items: readonly DirectoryAgent[]
    next?: string
    message: string
    selectedID: string | null
    configuration: AgentConfiguration | null
    detailPhase: Phase
    detailMessage: string
    models: readonly ProjectAvailableChatModel[]
    modelNext: string | null
    modelPhase: Phase
    modelMessage: string
    feedback: string
  }>({
    phase: 'idle',
    items: [],
    message: '',
    selectedID: null,
    configuration: null,
    detailPhase: 'idle',
    detailMessage: '',
    models: [],
    modelNext: null,
    modelPhase: 'idle',
    modelMessage: '',
    feedback: '',
  })
  const draft = reactive(initialDraft())
  const edit = reactive(initialEdit())
  const confirmation = reactive({ open: false, message: '' })
  const route = ref(''),
    reading = ref(false)
  let generation = 0,
    disposed = false,
    requested = false,
    projectKey = ''
  let leaveResult: ((yes: boolean) => void) | null = null
  const visible = computed(() => {
    const r = projectRoute(route.value)
    return (
      !!workspace.currentReadContext.value &&
      r?.suffix === '/settings/agents' &&
      `/${r.username}/${r.project_name}` === workspace.paths.value.home &&
      auth.state.phase === 'authenticated'
    )
  })
  const projectID = computed(() => workspace.currentReadContext.value?.projectID ?? null)
  const progress = computed(() => {
    const p = auth.agentConfiguration.progress
    return visible.value && p?.projectID === workspace.currentReadContext.value?.projectID
      ? p
      : null
  })
  const pending = computed(() => {
    const p = auth.agentConfiguration.progress
    return p && ['submitting', 'uncertain'].includes(p.phase) ? p : null
  })
  const busy = computed(() => reading.value || auth.state.busy || confirmation.open)
  const canCreate = computed(
    () => visible.value && workspace.detail.project?.lifecycle === 'active' && !pending.value,
  )
  const canEdit = computed(
    () =>
      canCreate.value &&
      state.detailPhase === 'ready' &&
      !!state.configuration &&
      state.configuration.id === state.selectedID &&
      !draft.open,
  )
  const editDirty = computed(() => edit.open && Object.keys(edit.touched).length > 0)
  const editStale = computed(() => {
    const a = state.configuration
    return (
      edit.conflict || (edit.open && (!a || a.id !== edit.agentID || a.version !== edit.version))
    )
  })
  const canSave = computed(
    () =>
      canEdit.value &&
      !busy.value &&
      edit.open &&
      editDirty.value &&
      !editStale.value &&
      !!edit.name.trim(),
  )
  const model = computed(() => state.models.find((m) => m.id === draft.modelID))
  const modelOptions = computed(() =>
    state.models.map((m) => ({
      value: m.id,
      label: `${m.name} · ${m.provider_name}（${m.scope.kind === 'system' ? '系统' : '项目'}）`,
    })),
  )
  const effortOptions = computed(() => [
    { value: '', label: '使用模型默认值' },
    ...(model.value?.capabilities.reasoning_efforts ?? []).map((value) => ({
      value,
      label: value,
    })),
  ])
  const dirty = computed(
    () =>
      draft.open &&
      (!!draft.name ||
        !!draft.displayName ||
        !!draft.tagColor ||
        !!draft.description ||
        !!draft.instructions ||
        !!draft.modelID ||
        !!draft.injectAgentsMD ||
        !!draft.approvalPolicy ||
        !draft.addSkills ||
        !draft.installSkill),
  )
  const canSubmit = computed(
    () =>
      canCreate.value &&
      !busy.value &&
      draft.open &&
      !!draft.name.trim() &&
      state.modelPhase === 'ready' &&
      !!model.value &&
      !!draft.injectAgentsMD &&
      !!draft.approvalPolicy &&
      (!draft.reasoningEffort ||
        model.value.capabilities.reasoning_efforts.includes(draft.reasoningEffort)) &&
      (draft.approvalPolicy !== 'auto' || state.models.some((m) => m.id === draft.approvalModelID)),
  )
  function live(own: number, p: string) {
    return (
      !disposed &&
      generation === own &&
      visible.value &&
      workspace.currentReadContext.value?.projectID === p
    )
  }
  function retire() {
    ++generation
    requested = false
    auth.agentConfiguration.abandonRead()
  }
  function clear() {
    Object.assign(state, {
      phase: 'idle',
      items: [],
      next: undefined,
      message: '',
      selectedID: null,
      configuration: null,
      detailPhase: 'idle',
      detailMessage: '',
      models: [],
      modelNext: null,
      modelPhase: 'idle',
      modelMessage: '',
      feedback: '',
    })
    Object.assign(draft, initialDraft())
    Object.assign(edit, initialEdit())
  }
  async function read<T>(
    work: (p: string) => Promise<T>,
    accept: (v: T) => void,
    fail: (e: unknown) => void,
  ) {
    const context = workspace.currentReadContext.value
    if (!context || !visible.value || busy.value) return
    const own = generation,
      p = context.projectID
    reading.value = true
    try {
      const value = await work(p)
      if (live(own, p)) accept(value)
    } catch (error) {
      if (live(own, p)) fail(error)
    } finally {
      reading.value = false
      void pump()
    }
  }
  async function loadDirectory(more = false) {
    const cursor = more ? state.next : undefined
    if (more && !cursor) return
    await read(
      async (p) => {
        state.phase = 'loading'
        state.message = ''
        return auth.agentConfiguration.list(p, { limit: 50, ...(cursor ? { cursor } : {}) })
      },
      (page) => {
        state.items = more ? [...state.items, ...page.items] : page.items
        state.next = page.next_cursor
        state.phase = 'ready'
      },
      (error) => {
        state.phase = 'error'
        state.message = explain(error)
      },
    )
  }
  async function loadModels(more = false) {
    const cursor = more ? state.modelNext : null
    if (more && !cursor) return
    await read(
      async (p) => {
        state.modelPhase = 'loading'
        state.modelMessage = ''
        return auth.agentConfiguration.models(p, { limit: 50, ...(cursor ? { cursor } : {}) })
      },
      (page) => {
        state.models = more ? [...state.models, ...page.items] : page.items
        state.modelNext = page.next_cursor
        state.modelPhase = 'ready'
      },
      (error) => {
        state.modelPhase = 'error'
        state.modelMessage = explain(error)
      },
    )
  }
  async function loadConfiguration(id: string) {
    await read(
      async (p) => {
        state.detailPhase = 'loading'
        state.detailMessage = ''
        state.configuration = null
        return auth.agentConfiguration.getConfiguration(p, id)
      },
      (value) => {
        if (state.selectedID !== id) return
        state.configuration = value
        state.detailPhase = 'ready'
      },
      (error) => {
        state.detailPhase = 'error'
        state.detailMessage = explain(error)
      },
    )
  }
  async function pump() {
    if (!requested || busy.value || !visible.value || disposed) return
    requested = false
    const own = generation,
      p = workspace.currentReadContext.value!.projectID
    await loadDirectory()
    if (live(own, p) && state.selectedID) await loadConfiguration(state.selectedID)
    if (live(own, p) && draft.open) await loadModels()
  }
  async function refresh() {
    if (busy.value || !visible.value) return
    if (edit.open && !(await cancelEdit())) return
    requested = true
    void pump()
  }
  async function selectAgent(id: string) {
    if (busy.value || !visible.value || !state.items.some((a) => a.id === id)) return
    if (state.selectedID === id) return
    const own = generation,
      p = projectID.value!
    if (draft.open && !(await cancelCreation())) return
    if (edit.open && !pending.value && !(await cancelEdit())) return
    if (progress.value?.kind === 'update' && progress.value.phase === 'uncertain') {
      if (!(await ask('保存结果尚未确认。切换后仍须查询原 Agent 的原操作，不能重新保存。'))) return
      Object.assign(edit, initialEdit())
    }
    if (!live(own, p)) return
    retire()
    state.feedback = ''
    state.selectedID = id
    await loadConfiguration(id)
  }
  async function openCreation() {
    if (!canCreate.value || busy.value) return
    if (edit.open && !(await cancelEdit())) return
    if (!canCreate.value || busy.value) return
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    Object.assign(draft, initialDraft(), { open: true })
    state.selectedID = null
    state.configuration = null
    state.feedback = ''
    await loadModels()
  }
  function selectModel(id: string) {
    if (busy.value || !canCreate.value || !state.models.some((m) => m.id === id)) return
    draft.modelID = id
    if (!model.value?.capabilities.reasoning_efforts.includes(draft.reasoningEffort))
      draft.reasoningEffort = ''
  }
  function chooseApproval(value: string) {
    if (!['default', 'auto', 'allow'].includes(value) || busy.value || !canCreate.value) return
    draft.approvalPolicy = value as AgentApprovalPolicy
    if (value !== 'auto') draft.approvalModelID = ''
  }
  async function cancelCreation() {
    if (busy.value || pending.value) return false
    if (dirty.value && !(await ask('放弃尚未提交的 Agent 配置？'))) return false
    Object.assign(draft, initialDraft())
    return true
  }
  function publishSummary(a: AgentConfiguration) {
    const summary: DirectoryAgent = Object.freeze({
      id: a.id,
      project_id: a.project_id,
      name: a.name,
      display_name: a.display_name,
      tag_color: a.tag_color,
      description: a.description,
      version: a.version,
      created_at: a.created_at,
      updated_at: a.updated_at,
    })
    state.items = state.items.some((v) => v.id === a.id)
      ? state.items.map((v) => (v.id === a.id ? summary : v))
      : [summary, ...state.items]
  }
  function acceptUpdate(receipt: AgentUpdateReceipt) {
    if (edit.agentID === receipt.agent.id) Object.assign(edit, initialEdit())
    if (state.selectedID === receipt.agent.id) {
      state.configuration = receipt.agent
      state.detailPhase = 'ready'
      state.detailMessage = ''
    }
    publishSummary(receipt.agent)
    state.feedback = receipt.changed ? 'Agent 配置已保存。' : '配置未变化，版本保持不变。'
  }
  function openEdit() {
    if (!canEdit.value || busy.value || !state.configuration) return
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    const a = state.configuration
    Object.assign(edit, initialEdit(), {
      open: true,
      agentID: a.id,
      version: a.version,
      name: a.name,
      description: a.description,
      instructions: a.instructions,
    })
    state.feedback = ''
  }
  function touchEdit(field: EditField, value: string) {
    if (!canEdit.value || busy.value || !edit.open || editStale.value) return
    edit[field] = value
    edit.touched[field] = true
    edit.message = ''
  }
  async function cancelEdit() {
    if (busy.value || pending.value) return false
    const own = generation,
      p = projectID.value
    if (editDirty.value && !(await ask('放弃尚未保存的 Agent 修改？'))) return false
    if (!p || !live(own, p)) return false
    Object.assign(edit, initialEdit())
    return true
  }
  async function submitEdit() {
    if (!canSave.value || !projectID.value) return
    const own = generation,
      p = projectID.value,
      a = edit.agentID
    const request: AgentUpdateFields = Object.freeze({
      ...(edit.touched.name ? { name: edit.name } : {}),
      ...(edit.touched.description ? { description: edit.description } : {}),
      ...(edit.touched.instructions ? { instructions: edit.instructions } : {}),
    })
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    edit.message = ''
    try {
      const value = await auth.agentConfiguration.startUpdate(p, a, {
        expected_version: edit.version,
        request,
      })
      if (live(own, p) && state.selectedID === a && !('status' in value)) acceptUpdate(value)
    } catch (error) {
      if (!live(own, p) || state.selectedID !== a) return
      edit.conflict = error instanceof AccountFailure && error.problem?.code === 'VERSION_CONFLICT'
      edit.message = explain(error)
    }
  }
  function acceptCreation(receipt: AgentCreateReceipt) {
    Object.assign(draft, initialDraft())
    state.configuration = receipt.agent
    state.selectedID = receipt.agent.id
    state.detailPhase = 'ready'
    state.detailMessage = ''
    state.feedback = 'Agent 已创建。配置与运行权限仍以实际服务校验为准。'
    publishSummary(receipt.agent)
  }
  async function submitCreation() {
    if (!canSubmit.value) return
    const context = workspace.currentReadContext.value!,
      own = generation,
      p = context.projectID
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    draft.message = ''
    const fields: Omit<AgentCreateFields, 'agent_id'> = {
      name: draft.name,
      model_ref: draft.modelID,
      inject_agents_md: draft.injectAgentsMD === 'yes',
      approval_policy: draft.approvalPolicy as AgentApprovalPolicy,
      allowed_tool_ids: [],
      allowed_mount_ids: [],
      allowed_secret_variable_ids: [],
      ...(draft.displayName ? { display_name: draft.displayName } : {}),
      ...(draft.tagColor ? { tag_color: draft.tagColor } : {}),
      ...(draft.description ? { description: draft.description } : {}),
      ...(draft.instructions ? { instructions: draft.instructions } : {}),
      ...(draft.reasoningEffort ? { reasoning_effort: draft.reasoningEffort } : {}),
      ...(draft.approvalPolicy === 'auto' ? { approval_model_ref: draft.approvalModelID } : {}),
      ...(draft.addSkills ? {} : { add_skills_enabled: false }),
      ...(draft.installSkill ? {} : { install_skill_enabled: false }),
    }
    try {
      const value = await auth.agentConfiguration.startCreate(p, fields)
      if (live(own, p) && !('status' in value)) acceptCreation(value)
    } catch (error) {
      if (live(own, p)) draft.message = explain(error)
    }
  }
  async function lookup() {
    const original = progress.value
    if (busy.value || !original?.contextValid || original.phase !== 'uncertain') return
    const own = generation,
      p = original.projectID
    state.feedback = ''
    try {
      const value = await auth.agentConfiguration.checkOriginal()
      if (!live(own, p)) return
      if ('status' in value && value.status === 'committed') {
        if (original.kind === 'update') acceptUpdate(value.receipt)
        else if (value.receipt.changed) acceptCreation({ ...value.receipt, changed: true })
      } else state.feedback = '尚未确认原操作结果，请继续保留原操作。'
    } catch (error) {
      if (live(own, p)) state.feedback = explain(error)
    }
  }
  function ask(message: string) {
    if (confirmation.open) return Promise.resolve(false)
    confirmation.open = true
    confirmation.message = message
    return new Promise<boolean>((resolve) => {
      leaveResult = resolve
    })
  }
  function finishConfirmation(yes: boolean) {
    confirmation.open = false
    const finish = leaveResult
    leaveResult = null
    finish?.(yes)
  }
  async function confirmLeave(target?: string) {
    if (!visible.value || target === route.value) return true
    if (busy.value) return false
    const uncertain = progress.value?.phase === 'uncertain' && progress.value.contextValid
    if (!dirty.value && !editDirty.value && !uncertain) return true
    const yes = await ask(
      uncertain
        ? '操作结果尚未确认。离开后仍需回到原项目查询原操作，不能重新提交。'
        : '放弃尚未提交的 Agent 配置并离开？',
    )
    if (yes) {
      Object.assign(draft, initialDraft())
      Object.assign(edit, initialEdit())
    }
    return yes
  }
  function afterNavigation(to: string) {
    if (to === route.value) return
    retire()
    route.value = to
    clear()
    if (projectRoute(to)?.suffix === '/settings/agents') {
      requested = true
      void pump()
    }
  }
  const stop = watch(
    () =>
      [workspace.currentReadContext.value, auth.personalContext.identity, auth.state.busy] as const,
    () => {
      const context = workspace.currentReadContext.value,
        identity = auth.personalContext.identity
      const next = identity
        ? `${identity.userID}/${identity.sessionID}/${identity.epoch}/${context?.projectID ?? ''}`
        : ''
      if (!context) {
        if (!identity) {
          retire()
          clear()
          projectKey = ''
          finishConfirmation(false)
        } else if (projectKey) requested = true
        return
      }
      if (next !== projectKey) {
        retire()
        clear()
        projectKey = next
        requested = true
      }
      void pump()
    },
    { immediate: true, flush: 'post' },
  )
  function dispose() {
    disposed = true
    retire()
    stop()
    finishConfirmation(false)
    clear()
  }
  return {
    state,
    draft,
    edit,
    editDirty,
    editStale,
    canEdit,
    canSave,
    openEdit,
    touchEdit,
    cancelEdit,
    submitEdit,
    visible,
    projectID,
    progress,
    pending,
    busy,
    canCreate,
    canSubmit,
    dirty,
    modelOptions,
    effortOptions,
    confirmation,
    agentLabel,
    refresh,
    loadDirectory,
    loadModels,
    loadConfiguration,
    selectAgent,
    openCreation,
    selectModel,
    chooseApproval,
    cancelCreation,
    submitCreation,
    lookup,
    confirmLeave,
    afterNavigation,
    finishConfirmation,
    dispose,
  }
}
export type ProjectAgents = ReturnType<typeof createProjectAgents>
export const projectAgentsKey: InjectionKey<ProjectAgents> = Symbol('project-agents')
export function useProjectAgents() {
  const owner = inject(projectAgentsKey)
  if (!owner) throw new Error('Agent settings owner is missing')
  return owner
}
