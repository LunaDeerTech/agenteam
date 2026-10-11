import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureRunnerMountDeclarations,
  matchingRunnerMounts,
  type AgentRunnerMounts,
  type AvailableRunner,
  type RunnerMountDeclaration,
} from '../api/agent-runner-mounts'
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

type EditField = 'name' | 'displayName' | 'tagColor' | 'description' | 'instructions'
type EditPolicyField =
  'runner_mounts' | 'model_ref' | 'inject_agents_md' | 'approval_policy' | 'approval_model_ref'
const initialEdit = () => ({
  open: false,
  agentID: '',
  version: '',
  name: '',
  displayName: '',
  tagColor: '',
  description: '',
  instructions: '',
  modelID: '',
  injectAgentsMD: 'yes' as 'yes' | 'no',
  approvalPolicy: 'default' as AgentApprovalPolicy,
  approvalModelID: '',
  runnerMounts: [] as RunnerMountDeclaration[],
  touched: {} as Partial<Record<EditField | EditPolicyField, true>>,
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
  runnerMounts: [] as RunnerMountDeclaration[],
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
    return '请核对名称、显示名称、标签颜色、模型和必填配置。名称须为 3–32 位英文字母、数字或连字符；显示名称不能全为空白，最多 256 个字符；标签颜色须为小写 #rrggbb。'
  return '未取得完整结果，请在原请求结束后重新读取。'
}
// This editor supports the current text-chat configuration profile only.
// Directory membership is a choice source; the mutation still rechecks it in Tx.
function editableModel(model: ProjectAvailableChatModel) {
  const c = model.capabilities
  return (
    !c.tool_calls &&
    !c.parallel_tool_calls &&
    !c.reasoning &&
    c.reasoning_efforts.length === 0 &&
    c.input_modalities.length === 1 &&
    c.input_modalities[0] === 'text' &&
    c.output_modalities.length === 1 &&
    c.output_modalities[0] === 'text' &&
    c.structured_output_modes.every((mode) => mode === 'text' || mode === 'json_schema')
  )
}
export function createProjectAgents(auth: SessionController, workspace: ProjectWorkspace) {
  const state = shallowReactive<{
    phase: Phase
    accessDenied: boolean
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
    runners: readonly AvailableRunner[]
    runnerNext?: string
    runnerPhase: Phase
    runnerMessage: string
    mounts: AgentRunnerMounts | null
    mountsPhase: Phase
    mountsMessage: string
  }>({
    phase: 'idle',
    accessDenied: false,
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
    runners: [],
    runnerNext: undefined,
    runnerPhase: 'idle',
    runnerMessage: '',
    mounts: null,
    mountsPhase: 'idle',
    mountsMessage: '',
  })
  const draft = reactive(initialDraft())
  const edit = reactive(initialEdit())
  const confirmation = reactive({ open: false, message: '' })
  const route = ref(''),
    reading = ref(false)
  const formInitialization = ref<object | null>(null)
  const formInitializing = computed(() => formInitialization.value !== null)
  let generation = 0,
    disposed = false,
    requested = false,
    projectKey = '',
    readKey = ''
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
    () =>
      visible.value &&
      !state.accessDenied &&
      workspace.detail.project?.lifecycle === 'active' &&
      !pending.value,
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
      (!editPolicyTouched.value || !editPolicyMessage.value) &&
      (!edit.touched.runner_mounts || (editMountsReady.value && !editMountMessage.value)) &&
      !!edit.name.trim(),
  )
  const editModels = computed(() => state.models.filter(editableModel))
  const editPolicyTouched = computed(
    () =>
      !!(edit.touched.model_ref || edit.touched.approval_policy || edit.touched.approval_model_ref),
  )
  const editPolicyMessage = computed(() => {
    if (!edit.open) return ''
    if (state.configuration?.reasoning_effort !== null)
      return '当前配置包含推理强度，暂不支持在此修改模型或审批策略。'
    if (state.modelPhase !== 'ready') return '请先完成可用模型读取，再保存模型或审批策略。'
    if (!editModels.value.some((m) => m.id === edit.modelID))
      return '原主模型未在已读取的受支持列表中。请加载更多模型，或显式选择可用模型。'
    if (
      edit.approvalPolicy === 'auto' &&
      !editModels.value.some((m) => m.id === edit.approvalModelID)
    )
      return '请选择当前可用的审批模型，再保存自动审批策略。'
    return ''
  })
  function editOptions(selected: string) {
    const options = editModels.value.map((m) => ({
      value: m.id,
      label: `${m.name} · ${m.provider_name}（${m.scope.kind === 'system' ? '系统' : '项目'}）`,
      disabled: false,
    }))
    if (selected && !options.some((option) => option.value === selected))
      options.unshift({
        value: selected,
        label: '原模型（未在已读取的受支持列表中）',
        disabled: true,
      })
    return options
  }
  const editModelOptions = computed(() => editOptions(edit.modelID))
  const editApprovalOptions = computed(() => editOptions(edit.approvalModelID))
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
  const runnerStatus = { online: '在线', offline: '离线', incompatible: '协议不兼容' }
  const runnerOptions = computed(() =>
    state.runners.map((r) => ({ value: r.id, label: `${r.name}（${runnerStatus[r.status]}）` })),
  )
  const editMountsReady = computed(
    () =>
      !!state.configuration &&
      !!state.mounts &&
      state.mountsPhase === 'ready' &&
      matchingRunnerMounts(state.configuration, state.mounts) &&
      (!edit.open || edit.version === state.mounts.agent_version),
  )
  function mountMessage(rows: readonly RunnerMountDeclaration[], editing: boolean) {
    try {
      const captured = captureRunnerMountDeclarations(rows)
      const previous = editing ? (state.mounts?.items ?? []) : []
      if (
        captured.some(
          (row) =>
            !state.runners.some((r) => r.id === row.runner_id) &&
            !previous.some((r) => r.runner_id === row.runner_id),
        )
      )
        return '请从已读取的系统 Runner 中选择；可加载更多 Runner。'
      const a = editing ? state.configuration : null
      if (
        captured.length +
          (a?.allowed_tool_ids.length ?? 0) +
          (a?.allowed_secret_variable_ids.length ?? 0) >
        256
      )
        return 'Agent 能力引用总数不能超过 256 项。'
      return ''
    } catch {
      return '请选择 Runner 并填写 1–255 位英文字母、数字、下划线、点或连字符的目录名；不能使用路径、尾点、保留名称或重复组合。'
    }
  }
  const draftMountMessage = computed(() => mountMessage(draft.runnerMounts, false))
  const editMountMessage = computed(() => mountMessage(edit.runnerMounts, true))
  function changeMounts(editing: boolean, rows: readonly RunnerMountDeclaration[]) {
    if (busy.value || !canCreate.value || rows.length > 128) return
    if (editing) {
      if (!edit.open || !canEdit.value || editStale.value || !editMountsReady.value) return
      edit.runnerMounts = rows.map((row) => ({ ...row }))
      edit.touched.runner_mounts = true
      edit.message = ''
    } else if (draft.open) draft.runnerMounts = rows.map((row) => ({ ...row }))
  }
  function readContextKey() {
    const c = workspace.currentReadContext.value
    return c
      ? `${c.identity.userID}/${c.identity.sessionID}/${c.identity.epoch}/${c.projectID}/${c.generation}/${c.readGeneration}`
      : ''
  }
  function denied(error: unknown) {
    return error instanceof AccountFailure && [401, 403, 404].includes(error.problem?.status ?? 0)
  }
  function clearDenied(error: unknown) {
    if (!denied(error)) return false
    // Retire visible observations, never the Session's original uncertain command.
    // Only a new current directory read may reopen creation after this refusal.
    state.accessDenied = true
    state.items = []
    state.next = undefined
    state.phase = 'error'
    state.message = explain(error)
    state.selectedID = null
    state.configuration = null
    state.mounts = null
    state.detailPhase = 'error'
    state.detailMessage = explain(error)
    state.mountsPhase = 'error'
    state.mountsMessage = explain(error)
    state.runners = []
    state.runnerNext = undefined
    state.runnerPhase = 'error'
    state.runnerMessage = explain(error)
    state.models = []
    state.modelNext = null
    state.modelPhase = 'error'
    state.modelMessage = explain(error)
    Object.assign(edit, initialEdit())
    Object.assign(draft, initialDraft())
    return true
  }
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
        draft.runnerMounts.length > 0 ||
        !draft.addSkills ||
        !draft.installSkill),
  )
  const canSubmit = computed(
    () =>
      canCreate.value &&
      !busy.value &&
      draft.open &&
      !!draft.name.trim() &&
      !draftMountMessage.value &&
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
    formInitialization.value = null
    Object.assign(state, {
      phase: 'idle',
      accessDenied: false,
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
      runners: [],
      runnerNext: undefined,
      runnerPhase: 'idle',
      runnerMessage: '',
      mounts: null,
      mountsPhase: 'idle',
      mountsMessage: '',
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
      p = context.projectID,
      key = readContextKey()
    reading.value = true
    try {
      const value = await work(p)
      if (live(own, p) && readContextKey() === key) accept(value)
    } catch (error) {
      if (live(own, p) && readContextKey() === key) fail(error)
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
        state.accessDenied = false
      },
      (error) => {
        clearDenied(error)
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
        clearDenied(error)
        state.modelPhase = 'error'
        state.modelMessage = explain(error)
      },
    )
  }
  async function loadRunners(more = false) {
    const cursor = more ? state.runnerNext : undefined
    if (more && !cursor) return
    await read(
      async (p) => {
        state.runnerPhase = 'loading'
        state.runnerMessage = ''
        return auth.agentConfiguration.runners(p, { limit: 50, ...(cursor ? { cursor } : {}) })
      },
      (page) => {
        const last = more ? state.runners.at(-1)?.id : undefined
        if (last && page.items.some((r) => r.id <= last)) {
          state.runnerPhase = 'error'
          state.runnerMessage = 'Runner 目录已变化，请从第一页重新读取。'
          return
        }
        state.runners = more ? [...state.runners, ...page.items] : page.items
        state.runnerNext = page.next_cursor
        state.runnerPhase = 'ready'
      },
      (error) => {
        clearDenied(error)
        state.runnerPhase = 'error'
        state.runnerMessage = explain(error)
      },
    )
  }
  async function loadConfiguration(id: string) {
    await read(
      async (p) => {
        state.detailPhase = 'loading'
        state.detailMessage = ''
        state.configuration = null
        state.mounts = null
        state.mountsPhase = 'loading'
        state.mountsMessage = ''
        const key = readContextKey(),
          own = generation
        const configuration = await auth.agentConfiguration.getConfiguration(p, id)
        if (!live(own, p) || readContextKey() !== key || state.selectedID !== id)
          throw new AccountFailure('cancelled')
        try {
          const mounts = await auth.agentConfiguration.runnerMounts(p, id)
          return { configuration, mounts, error: null }
        } catch (error) {
          // A local dependency failure must not turn existing references into [].
          if (denied(error)) throw error
          return { configuration, mounts: null, error }
        }
      },
      (value) => {
        if (state.selectedID !== id) return
        state.configuration = value.configuration
        state.detailPhase = 'ready'
        if (value.mounts && matchingRunnerMounts(value.configuration, value.mounts)) {
          state.mounts = value.mounts
          state.mountsPhase = 'ready'
          state.mountsMessage = ''
          if (
            edit.open &&
            edit.agentID === id &&
            edit.version === value.mounts.agent_version &&
            !edit.touched.runner_mounts
          )
            edit.runnerMounts = value.mounts.items.map(({ runner_id, workspace }) => ({
              runner_id,
              workspace,
            }))
        } else {
          state.mounts = null
          state.mountsPhase = 'error'
          state.mountsMessage = value.mounts
            ? '配置版本或挂载引用已变化，请重新读取；原编辑内容和版本保持不变。'
            : '未能完整读取当前 Runner 配置。原引用保持不变，请重新读取。'
        }
      },
      (error) => {
        clearDenied(error)
        state.detailPhase = 'error'
        state.detailMessage = explain(error)
        state.mounts = null
        state.mountsPhase = 'error'
      },
    )
  }
  async function pump() {
    if (!requested || busy.value || !visible.value || disposed) return
    requested = false
    const own = generation,
      p = workspace.currentReadContext.value!.projectID
    await initializeForm(async () => {
      await loadDirectory()
      if (live(own, p) && state.selectedID) await loadConfiguration(state.selectedID)
      if (live(own, p) && (draft.open || edit.open)) {
        await loadModels()
        if (live(own, p) && (draft.open || edit.open)) await loadRunners()
      }
    })
  }
  async function initializeForm(work: () => Promise<void>) {
    // Covers the whole opening/revalidation sequence, not an individual read.
    // Keep it separate from busy: read() must still admit the next owned read.
    const token = Object.freeze({})
    formInitialization.value = token
    try {
      await work()
    } finally {
      if (formInitialization.value === token) formInitialization.value = null
    }
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
    const own = generation,
      p = projectID.value!
    await initializeForm(async () => {
      await loadModels()
      if (live(own, p) && draft.open) await loadRunners()
    })
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
  async function acceptUpdate(receipt: AgentUpdateReceipt) {
    if (edit.agentID === receipt.agent.id) Object.assign(edit, initialEdit())
    state.feedback = receipt.changed ? 'Agent 配置已保存。' : '配置未变化，版本保持不变。'
    if (state.selectedID === receipt.agent.id) await loadConfiguration(receipt.agent.id)
    if (state.configuration?.id === receipt.agent.id && state.detailPhase === 'ready')
      publishSummary(state.configuration)
  }
  async function openEdit() {
    if (!canEdit.value || busy.value || !state.configuration) return
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    const a = state.configuration
    Object.assign(edit, initialEdit(), {
      open: true,
      agentID: a.id,
      version: a.version,
      name: a.name,
      displayName: a.display_name ?? '',
      tagColor: a.tag_color ?? '',
      description: a.description,
      instructions: a.instructions,
      modelID: a.model_ref,
      injectAgentsMD: a.inject_agents_md ? 'yes' : 'no',
      approvalPolicy: a.approval_policy,
      approvalModelID: a.approval_model_ref ?? '',
      runnerMounts: editMountsReady.value
        ? state.mounts!.items.map(({ runner_id, workspace }) => ({ runner_id, workspace }))
        : [],
    })
    state.feedback = ''
    const own = generation,
      p = projectID.value!
    await initializeForm(async () => {
      await loadModels()
      if (live(own, p) && edit.open) await loadRunners()
    })
  }
  function touchEdit(field: EditField, value: string) {
    if (!canEdit.value || busy.value || !edit.open || editStale.value) return
    edit[field] = value
    edit.touched[field] = true
    edit.message = ''
  }
  function touchEditPolicy(field: Exclude<EditPolicyField, 'runner_mounts'>, value: string) {
    if (!canEdit.value || busy.value || !edit.open || editStale.value) return
    if (field === 'inject_agents_md') {
      if (value !== 'yes' && value !== 'no') return
      edit.injectAgentsMD = value
    } else {
      if (state.configuration?.reasoning_effort !== null || state.modelPhase !== 'ready') return
      if (field === 'approval_policy') {
        if (!['default', 'auto', 'allow'].includes(value)) return
        edit.approvalPolicy = value as AgentApprovalPolicy
        if (value !== 'auto') edit.approvalModelID = ''
        // A strategy change always submits its matching reference in the same command.
        edit.touched.approval_model_ref = true
      } else {
        if (!editModels.value.some((m) => m.id === value)) return
        if (field === 'model_ref') edit.modelID = value
        else {
          if (edit.approvalPolicy !== 'auto') return
          edit.approvalModelID = value
          edit.touched.approval_policy = true
        }
      }
    }
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
      ...(edit.touched.runner_mounts ? { runner_mounts: edit.runnerMounts } : {}),
      ...(edit.touched.name ? { name: edit.name } : {}),
      ...(edit.touched.displayName
        ? { display_name: edit.displayName === '' ? null : edit.displayName }
        : {}),
      ...(edit.touched.tagColor ? { tag_color: edit.tagColor === '' ? null : edit.tagColor } : {}),
      ...(edit.touched.description ? { description: edit.description } : {}),
      ...(edit.touched.instructions ? { instructions: edit.instructions } : {}),
      ...(edit.touched.model_ref ? { model_ref: edit.modelID, reasoning_effort: null } : {}),
      ...(edit.touched.inject_agents_md ? { inject_agents_md: edit.injectAgentsMD === 'yes' } : {}),
      ...(edit.touched.approval_policy ? { approval_policy: edit.approvalPolicy } : {}),
      ...(edit.touched.approval_model_ref
        ? { approval_model_ref: edit.approvalPolicy === 'auto' ? edit.approvalModelID : null }
        : {}),
    })
    if (progress.value?.phase === 'rejected') auth.agentConfiguration.editRejected()
    edit.message = ''
    try {
      const value = await auth.agentConfiguration.startUpdate(p, a, {
        expected_version: edit.version,
        request,
      })
      if (live(own, p) && state.selectedID === a && !('status' in value)) await acceptUpdate(value)
    } catch (error) {
      if (!live(own, p) || state.selectedID !== a) return
      if (clearDenied(error)) return
      edit.conflict = error instanceof AccountFailure && error.problem?.code === 'VERSION_CONFLICT'
      edit.message = explain(error)
    }
  }
  async function acceptCreation(receipt: AgentCreateReceipt) {
    Object.assign(draft, initialDraft())
    state.configuration = null
    state.selectedID = receipt.agent.id
    state.feedback = 'Agent 已创建。配置与运行权限仍以实际服务校验为准。'
    await loadConfiguration(receipt.agent.id)
    if (state.configuration && state.detailPhase === 'ready') publishSummary(state.configuration)
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
      runner_mounts: draft.runnerMounts,
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
      if (live(own, p) && !('status' in value)) await acceptCreation(value)
    } catch (error) {
      if (live(own, p) && !clearDenied(error)) draft.message = explain(error)
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
        if (original.kind === 'update') await acceptUpdate(value.receipt)
        else if (value.receipt.changed) await acceptCreation({ ...value.receipt, changed: true })
      } else state.feedback = '尚未确认原操作结果，请继续保留原操作。'
    } catch (error) {
      if (live(own, p)) {
        clearDenied(error)
        state.feedback = explain(error)
      }
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
        if (readKey) {
          retire()
          readKey = ''
          state.configuration = null
          state.mounts = null
          state.items = []
          state.models = []
          state.runners = []
          state.next = undefined
          state.modelNext = null
          state.runnerNext = undefined
          state.detailPhase = 'idle'
          state.mountsPhase = 'idle'
        }
        if (!identity) {
          retire()
          clear()
          projectKey = ''
          finishConfirmation(false)
        } else if (projectKey) requested = true
        return
      }
      const nextRead = readContextKey()
      if (next !== projectKey) {
        retire()
        clear()
        projectKey = next
        requested = true
      } else if (readKey !== nextRead) {
        retire()
        requested = true
      }
      readKey = nextRead
      void pump()
    },
    { immediate: true, flush: 'sync' },
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
    runnerOptions,
    runnerStatus,
    editMountsReady,
    draftMountMessage,
    editMountMessage,
    changeMounts,
    loadRunners,
    draft,
    edit,
    editDirty,
    editStale,
    canEdit,
    canSave,
    openEdit,
    touchEdit,
    touchEditPolicy,
    editPolicyMessage,
    editModelOptions,
    editApprovalOptions,
    cancelEdit,
    submitEdit,
    visible,
    projectID,
    progress,
    pending,
    busy,
    formInitializing,
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
