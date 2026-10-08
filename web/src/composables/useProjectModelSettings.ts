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
import {
  captureProjectConfigurationCommand,
  captureProjectModelID,
  type ProjectAvailableChatModel,
  type ProjectChatProtocol,
  type ProjectConfigurationCommand,
  type ProjectModel,
  type ProjectModelCapabilities,
  type ProjectModelPage,
  type ProjectModelPageQuery,
  type ProjectModelWriteInput,
  type ProjectProvider,
  type ProjectProviderWriteInput,
} from '../api/project-models'
import {
  captureProjectCredentialTarget,
  captureProjectCredentialValue,
  type ProjectCredentialMetadata,
} from '../api/project-model-credentials'
import { projectRoute } from '../router/auth'
import {
  useSession,
  type PersonalIdentity,
  type SessionController,
  type ProjectModelSettingsProgress,
} from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type PageKind = 'providers' | 'models' | 'available'
type Phase = 'inactive' | 'waiting' | 'loading' | 'ready' | 'empty' | 'error'
type EditorPhase = 'inactive' | 'loading' | 'ready' | 'error'
type Limit = 25 | 50 | 100
type Fields = Readonly<Record<string, string>>
export type ProjectProviderDraft = {
  name: string
  protocol: ProjectChatProtocol
  base_url: string
  enabled: boolean
  credential_ref: string
}
export type ProjectModelDraft = {
  name: string
  provider_model_id: string
  enabled: boolean
  tool_calls: boolean
  parallel_tool_calls: boolean
  streaming: boolean
  reasoning: boolean
  input_modalities: string[]
  output_modalities: string[]
  structured_output_modes: string[]
  context_length: string
  max_output: string
}
type PageState<T> = {
  phase: Phase
  items: readonly T[]
  stale: boolean
  message: string
  cursorInvalid: boolean
  limit: Limit
  page: number
  hasPrevious: boolean
  hasNext: boolean
}
type EditorState<T> = {
  open: boolean
  mode: 'create' | 'edit'
  phase: EditorPhase
  target: string | null
  original: T | null
  review: T | null
  version: string
  fields: Fields
  message: string
  supported: boolean
  conflict: boolean
  requiresRead: boolean
}
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
const initialProvider = (): ProjectProviderDraft => ({
  name: '',
  protocol: 'openai-chat-completions',
  base_url: '',
  enabled: true,
  credential_ref: '',
})
const initialModel = (): ProjectModelDraft => ({
  name: '',
  provider_model_id: '',
  enabled: true,
  tool_calls: false,
  parallel_tool_calls: false,
  streaming: true,
  reasoning: false,
  input_modalities: ['text'],
  output_modalities: ['text'],
  structured_output_modes: ['text'],
  context_length: '',
  max_output: '',
})
const initialEditor = <T>(): EditorState<T> => ({
  open: false,
  mode: 'create',
  phase: 'inactive',
  target: null,
  original: null,
  review: null,
  version: '',
  fields: Object.freeze({}),
  message: '',
  supported: true,
  conflict: false,
  requiresRead: false,
})
function providerDraft(value: ProjectProvider): ProjectProviderDraft {
  return {
    name: value.input.name,
    protocol: value.input.protocol,
    base_url: value.input.base_url,
    enabled: value.input.enabled,
    credential_ref: value.input.credential_ref ?? '',
  }
}
function modelDraft(value: ProjectModel): ProjectModelDraft {
  const c = value.input.capabilities
  return {
    name: value.input.name,
    provider_model_id: value.input.provider_model_id,
    enabled: value.input.enabled,
    tool_calls: c.tool_calls,
    parallel_tool_calls: c.parallel_tool_calls,
    streaming: c.streaming,
    reasoning: c.reasoning,
    input_modalities: [...c.input_modalities],
    output_modalities: [...c.output_modalities],
    structured_output_modes: [...c.structured_output_modes],
    context_length: c.context_length ?? '',
    max_output: c.max_output ?? '',
  }
}
function providerInput(value: ProjectProviderDraft): ProjectProviderWriteInput {
  return {
    name: value.name,
    protocol: value.protocol,
    base_url: value.base_url,
    enabled: value.enabled,
    credential_ref: value.credential_ref === '' ? null : value.credential_ref,
    options: {},
  }
}
function modelInput(value: ProjectModelDraft): ProjectModelWriteInput {
  return {
    name: value.name,
    provider_model_id: value.provider_model_id,
    type: 'chat',
    enabled: value.enabled,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: value.tool_calls,
      parallel_tool_calls: value.parallel_tool_calls,
      streaming: value.streaming,
      reasoning: value.reasoning,
      input_modalities: [...value.input_modalities] as ProjectModelCapabilities['input_modalities'],
      output_modalities: [
        ...value.output_modalities,
      ] as ProjectModelCapabilities['output_modalities'],
      structured_output_modes: [
        ...value.structured_output_modes,
      ] as ProjectModelCapabilities['structured_output_modes'],
      reasoning_efforts: [],
      context_length: value.context_length === '' ? null : value.context_length,
      max_output: value.max_output === '' ? null : value.max_output,
    },
  }
}
function supportedProvider(value: ProjectProvider): boolean {
  try {
    captureProjectConfigurationCommand({
      kind: 'provider.update',
      id: value.id,
      expected_version: value.version,
      input: value.input as ProjectProviderWriteInput,
    })
    return true
  } catch {
    return false
  }
}
function supportedModel(value: ProjectModel, provider: ProjectProvider): boolean {
  try {
    captureProjectConfigurationCommand({
      kind: 'model.update',
      id: value.id,
      provider_id: value.provider_id,
      protocol: provider.input.protocol,
      expected_version: value.version,
      input: value.input as ProjectModelWriteInput,
    })
    return true
  } catch {
    return false
  }
}
function readMessage(e: AccountFailure): string {
  if (e.problem?.code === 'CURSOR_INVALID') return '分页游标已不可用。请明确从第一页重新读取。'
  if (e.kind === 'cancelled') return '本次读取已取消或等待超时。请求结束后可明确重读。'
  if (e.problem?.status === 403 || e.problem?.status === 404)
    return '当前项目或目标权限尚不能确认，请先重新读取项目。'
  return '未取得完整的当前信息，请明确重读。'
}
function writeExplanation(
  e: AccountFailure,
  progress: ProjectModelSettingsProgress | null,
): string {
  if (progress?.keyConflict)
    return '原请求标识发生冲突，不能重放或自动换标识。可以明确放弃本地追踪；这不会撤销服务端命令。'
  if (progress?.phase === 'uncertain')
    return '原请求结果尚未严格确认。查证仅提供历史观察；只有原请求重放的完整成功响应才能确认本次写入。'
  if (e.problem?.code === 'DEPENDENCY_UNBOUND')
    return '仍被使用的模型暂不能在此删除。没有执行引用迁移。'
  if (e.problem?.code === 'RESOURCE_BUSY')
    return '对象仍被引用或使用，当前操作被拒绝。请明确处理引用后再操作。'
  if (e.problem?.code === 'VERSION_CONFLICT')
    return '版本已变化，输入已保留。请显式重读并核对当前值。'
  if (e.problem?.code === 'INVALID_STATE' || e.problem?.code === 'PROJECT_NOT_ACTIVE')
    return '当前状态不允许此操作，输入已保留。请显式重读当前信息。'
  if (e.problem?.code === 'CAPABILITY_UNSUPPORTED')
    return '当前协议不支持这些配置声明，请检查能力选项。'
  if (e.kind === 'invalid-input') return '请检查输入格式和当前操作条件。'
  return '本次操作未完成，原输入已保留。请检查当前信息后明确操作。'
}

// One App-lifetime coordinator. Session owns every request and original material;
// this owner only binds UI observations/drafts to the current Project read.
export function createProjectModelSettings(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  let identity = auth.personalContext.identity,
    route = '',
    generation = 0,
    needsInitial = false
  const scope = shallowRef<Context | null>(null),
    boundID = ref<string | null>(null)
  const pageMode = ref<'none' | 'providers' | 'available'>('none'),
    disposed = ref(false),
    activeRead = ref(false),
    gateRead = shallowRef<Context | null>(null)
  const providerBaseline = ref(''),
    modelBaseline = ref('')
  const providerReviewContext = shallowRef<Context | null>(null),
    modelReviewContext = shallowRef<Context | null>(null),
    credentialMetadataContext = shallowRef<Context | null>(null)
  let resolveConfirmation: ((value: boolean) => void) | null = null
  let submitted: Readonly<{
    kind: 'provider' | 'model' | 'credential'
    form: string
    credentialRef: string
  }> | null = null
  let acceptedReceipt: ProjectModelSettingsProgress['receipt'] = null
  const provider = shallowReactive(initialEditor<ProjectProvider>()),
    model = shallowReactive(initialEditor<ProjectModel>())
  const providerForm = reactive(initialProvider()),
    modelForm = reactive(initialModel())
  const modelProvider = shallowRef<ProjectProvider | null>(null),
    modelReviewProvider = shallowRef<ProjectProvider | null>(null)
  const credential = shallowReactive({
    open: false,
    phase: 'inactive' as EditorPhase,
    mode: 'create' as 'create' | 'manage',
    reference: '',
    metadata: null as ProjectCredentialMetadata | null,
    fields: Object.freeze({}) as Fields,
    message: '',
    conflict: false,
    requiresRead: false,
  })
  const credentialValue = ref('')
  const preparedCredential = ref<Readonly<{
    credential_id: string
    version: string
    bound: boolean
  }> | null>(null)
  const deletion = shallowReactive({
    open: false,
    kind: 'provider' as 'provider' | 'model' | 'credential',
    target: '',
    name: '',
    version: '',
    replacement: 'unset',
    message: '',
  })
  const confirmation = reactive({ open: false, title: '', message: '', label: '确认' })
  const modelsOpen = ref(false),
    message = ref(''),
    feedback = ref<'idle' | 'loading' | 'success'>('idle')
  const progress = computed(() => {
    const value = auth.projectModelSettings.progress
    return value?.projectID === boundID.value ? value : null
  })
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    sameIdentity(identity, auth.personalContext.identity) &&
    auth.state.user?.id === identity?.userID &&
    auth.state.session?.id === identity?.sessionID
  const visible = computed(
    () =>
      !disposed.value &&
      pageMode.value !== 'none' &&
      authorized() &&
      !!scope.value &&
      sameContext(scope.value, workspace.currentReadContext.value) &&
      gateRead.value === null,
  )
  const currentProject = computed(() =>
    visible.value &&
    workspace.detail.phase === 'current' &&
    workspace.detail.project?.id === boundID.value
      ? workspace.detail.project
      : null,
  )
  const viewContext = computed(() => (visible.value ? scope.value : null))
  const canMutate = computed(() => currentProject.value?.lifecycle === 'active')
  const pending = computed(
    () => progress.value?.phase === 'submitting' || progress.value?.phase === 'uncertain',
  )
  const blocked = computed(
    () => !visible.value || auth.state.busy || activeRead.value || confirmation.open,
  )
  const providerDirty = computed(
    () => provider.phase !== 'inactive' && JSON.stringify(providerForm) !== providerBaseline.value,
  )
  const modelDirty = computed(
    () => model.phase !== 'inactive' && JSON.stringify(modelForm) !== modelBaseline.value,
  )
  const dirty = computed(
    () =>
      providerDirty.value ||
      modelDirty.value ||
      credentialValue.value !== '' ||
      pending.value ||
      (!!preparedCredential.value && !preparedCredential.value.bound) ||
      provider.conflict ||
      model.conflict ||
      credential.conflict,
  )
  const canLookup = computed(
    () =>
      !blocked.value &&
      !!progress.value?.contextValid &&
      progress.value.phase === 'uncertain' &&
      !progress.value.keyConflict,
  )
  const canReplay = computed(
    () =>
      !blocked.value &&
      !!progress.value?.canRetryOriginal &&
      (progress.value.domain === 'configuration' || canMutate.value),
  )
  const canSaveProvider = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      provider.open &&
      provider.phase === 'ready' &&
      provider.supported &&
      !provider.conflict &&
      !provider.requiresRead &&
      !pending.value &&
      providerDirty.value,
  )
  const canSaveModel = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      model.open &&
      model.phase === 'ready' &&
      model.supported &&
      !!modelProvider.value &&
      !model.conflict &&
      !model.requiresRead &&
      !pending.value &&
      modelDirty.value,
  )
  const canAdoptProvider = computed(
    () =>
      !blocked.value &&
      !pending.value &&
      !!provider.review &&
      sameContext(providerReviewContext.value, scope.value),
  )
  const canAdoptModel = computed(
    () =>
      !blocked.value &&
      !pending.value &&
      !!model.review &&
      !!modelReviewProvider.value &&
      sameContext(modelReviewContext.value, scope.value),
  )
  const canCreateCredential = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      !pending.value &&
      credential.mode === 'create' &&
      !credential.conflict &&
      !credential.requiresRead &&
      credentialValue.value !== '',
  )
  const canRotateCredential = computed(
    () =>
      !blocked.value &&
      canMutate.value &&
      !pending.value &&
      credential.mode === 'manage' &&
      credentialValue.value !== '' &&
      !!credential.metadata &&
      sameContext(credentialMetadataContext.value, scope.value) &&
      !credential.conflict &&
      !credential.requiresRead,
  )

  function pager<T>(
    kind: PageKind,
    read: (projectID: string, query: ProjectModelPageQuery) => Promise<ProjectModelPage<T>>,
  ) {
    const state = shallowReactive<PageState<T>>({
      phase: 'inactive',
      items: Object.freeze([]),
      stale: false,
      message: '',
      cursorInvalid: false,
      limit: 25,
      page: 1,
      hasPrevious: false,
      hasNext: false,
    })
    let cursors: (string | undefined)[] = [undefined],
      index = 0,
      next: string | null = null
    let requested = { index: 0, cursor: undefined as string | undefined }
    async function load(target = requested) {
      if (blocked.value || !scope.value) return false
      const context = scope.value,
        own = ++generation
      activeRead.value = true
      requested = { ...target }
      state.phase = 'loading'
      state.message = ''
      state.cursorInvalid = false
      const current = () =>
        !disposed.value && own === generation && visible.value && sameContext(context, scope.value)
      try {
        const result = await read(context.projectID, {
          limit: state.limit,
          ...(target.cursor === undefined ? {} : { cursor: target.cursor }),
        })
        if (!current()) return false
        cursors = [...cursors.slice(0, target.index), target.cursor]
        index = target.index
        next = result.next_cursor
        Object.assign(state, {
          phase: result.items.length ? 'ready' : 'empty',
          items: result.items,
          stale: false,
          message: '',
          page: index + 1,
          hasPrevious: index > 0,
          hasNext: next !== null,
        })
        return true
      } catch (error) {
        if (!current()) return false
        const e = failure(error)
        Object.assign(state, {
          phase: 'error',
          stale: state.items.length > 0,
          message: readMessage(e),
          cursorInvalid: e.problem?.code === 'CURSOR_INVALID',
        })
        denyCurrent(e, context)
        return false
      } finally {
        if (own === generation) activeRead.value = false
      }
    }
    function invalidate() {
      Object.assign(state, {
        phase: 'waiting',
        items: Object.freeze([]),
        stale: false,
        message: '',
        cursorInvalid: false,
        page: 1,
        hasPrevious: false,
        hasNext: false,
      })
      cursors = [undefined]
      index = 0
      next = null
      requested = { index: 0, cursor: undefined }
    }
    return {
      state,
      kind,
      load,
      invalidate,
      retry: () => load(),
      fromFirst: () => load({ index: 0, cursor: undefined }),
      next: () =>
        state.hasNext && next !== null
          ? load({ index: index + 1, cursor: next })
          : Promise.resolve(false),
      previous: () =>
        state.hasPrevious
          ? load({ index: index - 1, cursor: cursors[index - 1] })
          : Promise.resolve(false),
      setLimit(value: Limit) {
        if (blocked.value || ![25, 50, 100].includes(value)) return Promise.resolve(false)
        state.limit = value
        return load({ index: 0, cursor: undefined })
      },
    }
  }
  const providers = pager('providers', auth.projectModelSettings.listProviders),
    models = pager('models', auth.projectModelSettings.listModels),
    available = pager('available', auth.projectModelSettings.listAvailableChatModels)
  function retireReads() {
    ++generation
    activeRead.value = false
    auth.projectModelSettings.abandonReads()
  }
  function hideObservations() {
    retireReads()
    providers.invalidate()
    models.invalidate()
    available.invalidate()
    provider.review = null
    model.review = null
    providerReviewContext.value = modelReviewContext.value = credentialMetadataContext.value = null
    modelReviewProvider.value = null
    credential.metadata = null
    finishConfirmation(false)
  }
  function finishConfirmation(answer: boolean) {
    confirmation.open = false
    const resolve = resolveConfirmation
    resolveConfirmation = null
    resolve?.(answer)
  }
  function ask(title: string, description: string, label = '确认') {
    if (resolveConfirmation || !visible.value) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message: description, label })
    return new Promise<boolean>((resolve) => {
      resolveConfirmation = resolve
    })
  }
  function resetProvider() {
    Object.assign(provider, initialEditor<ProjectProvider>())
    Object.assign(providerForm, initialProvider())
    providerBaseline.value = ''
    providerReviewContext.value = null
  }
  function resetModel() {
    Object.assign(model, initialEditor<ProjectModel>())
    Object.assign(modelForm, initialModel())
    modelBaseline.value = ''
    modelProvider.value = modelReviewProvider.value = null
    modelReviewContext.value = null
  }
  function resetCredential() {
    Object.assign(credential, {
      open: false,
      phase: 'inactive',
      mode: 'create',
      reference: '',
      metadata: null,
      fields: Object.freeze({}),
      message: '',
      conflict: false,
      requiresRead: false,
    })
    credentialValue.value = ''
    credentialMetadataContext.value = null
  }
  // Local draft/intent retirement cannot authorize a cached Owner context.
  function clearAll() {
    hideObservations()
    auth.projectModelSettings.abandonPending()
    resetProvider()
    resetModel()
    resetCredential()
    preparedCredential.value = null
    deletion.open = false
    deletion.message = ''
    modelsOpen.value = false
    message.value = ''
    feedback.value = 'idle'
    submitted = null
    acceptedReceipt = null
  }
  function denyCurrent(e: AccountFailure, context: Context) {
    if (
      (e.problem?.status === 403 || e.problem?.status === 404) &&
      sameContext(context, scope.value)
    ) {
      gateRead.value = context
      message.value = '当前项目或目标权限尚不能确认。请显式重新读取项目后再继续。'
      hideObservations()
    }
  }
  function errorFields(e: AccountFailure): Fields {
    const result: Record<string, string> = {}
    for (const field of e.problem?.field_errors ?? []) {
      const key = field.path.replace(/^\/input\//, '').replace(/^\//, '')
      if (
        [
          'name',
          'base_url',
          'protocol',
          'credential_ref',
          'provider_model_id',
          'capabilities',
          'value',
          'expected_version',
          'replacement',
        ].includes(key)
      )
        result[key] = '请检查此字段及当前版本。'
    }
    return Object.freeze(result)
  }
  function editRejected() {
    if (progress.value?.phase === 'rejected') auth.projectModelSettings.editRejected()
  }
  function fillProvider(value: ProjectProvider) {
    Object.assign(providerForm, providerDraft(value))
    providerBaseline.value = JSON.stringify(providerForm)
    Object.assign(provider, {
      mode: 'edit',
      phase: 'ready',
      target: value.id,
      original: value,
      review: null,
      version: value.version,
      supported: supportedProvider(value),
      conflict: false,
      requiresRead: false,
      fields: Object.freeze({}),
      message: '',
    })
    providerReviewContext.value = null
  }
  function fillModel(value: ProjectModel, parent: ProjectProvider) {
    Object.assign(modelForm, modelDraft(value))
    modelBaseline.value = JSON.stringify(modelForm)
    modelProvider.value = parent
    Object.assign(model, {
      mode: 'edit',
      phase: 'ready',
      target: value.id,
      original: value,
      review: null,
      version: value.version,
      supported: supportedModel(value, parent),
      conflict: false,
      requiresRead: false,
      fields: Object.freeze({}),
      message: '',
    })
    modelReviewProvider.value = null
    modelReviewContext.value = null
  }
  async function discardForReplacement(kind: 'provider' | 'model') {
    const changed = kind === 'provider' ? providerDirty.value : modelDirty.value
    if (pending.value || blocked.value) return false
    if (!changed) return true
    const context = scope.value
    const answer = await ask(
      '放弃当前表单修改？',
      '尚未保存的输入会被清除；已确认的服务端命令不会撤销。',
      '放弃修改',
    )
    return answer && sameContext(context, scope.value) && visible.value
  }
  async function newProvider() {
    const context = scope.value
    if (
      !canMutate.value ||
      !(await discardForReplacement('provider')) ||
      !sameContext(context, scope.value) ||
      !canMutate.value
    )
      return false
    editRejected()
    resetProvider()
    provider.open = true
    provider.phase = 'ready'
    providerBaseline.value = JSON.stringify(providerForm)
    return true
  }
  async function readProvider(target = provider.target, preserveDraft = false) {
    if (blocked.value || !scope.value || !target) return false
    const context = scope.value
    if (!preserveDraft && !(await discardForReplacement('provider'))) return false
    if (!visible.value || !sameContext(context, scope.value)) return false
    const own = ++generation,
      wanted = captureID(target)
    if (!wanted) return false
    activeRead.value = true
    provider.open = true
    provider.phase = 'loading'
    provider.message = ''
    provider.review = null
    providerReviewContext.value = null
    const current = () =>
      !disposed.value && generation === own && visible.value && sameContext(context, scope.value)
    try {
      const value = await auth.projectModelSettings.getProvider(context.projectID, wanted)
      if (!current()) return false
      if (
        preserveDraft &&
        provider.target === wanted &&
        (providerDirty.value || provider.conflict || pending.value)
      ) {
        provider.review = value
        providerReviewContext.value = context
        provider.phase = 'ready'
        provider.requiresRead = false
      } else fillProvider(value)
      return true
    } catch (error) {
      if (current()) {
        const e = failure(error)
        provider.phase = 'error'
        provider.message = readMessage(e)
        denyCurrent(e, context)
      }
      return false
    } finally {
      if (generation === own) activeRead.value = false
    }
  }
  function captureID(value: string) {
    try {
      return captureProjectModelID(value)
    } catch {
      message.value = '请输入合法的稳定 ID。'
      return null
    }
  }
  async function newModel(providerID: string) {
    const context = scope.value
    if (
      !context ||
      !canMutate.value ||
      !(await discardForReplacement('model')) ||
      !sameContext(context, scope.value) ||
      !canMutate.value
    )
      return false
    const target = captureID(providerID)
    if (!target) return false
    const own = ++generation
    activeRead.value = true
    try {
      const parent = await auth.projectModelSettings.getProvider(context.projectID, target)
      if (generation !== own || !visible.value || !sameContext(context, scope.value)) return false
      editRejected()
      resetModel()
      model.open = true
      model.phase = 'ready'
      modelProvider.value = parent
      modelBaseline.value = JSON.stringify(modelForm)
      return true
    } catch (error) {
      if (generation === own && sameContext(context, scope.value)) {
        const e = failure(error)
        message.value = readMessage(e)
        denyCurrent(e, context)
      }
      return false
    } finally {
      if (generation === own) activeRead.value = false
    }
  }
  async function readModel(target = model.target, preserveDraft = false) {
    if (blocked.value || !scope.value || !target) return false
    const context = scope.value
    if (!preserveDraft && !(await discardForReplacement('model'))) return false
    if (!visible.value || !sameContext(context, scope.value)) return false
    const own = ++generation,
      wanted = captureID(target)
    if (!wanted) return false
    activeRead.value = true
    model.open = true
    model.phase = 'loading'
    model.message = ''
    model.review = null
    modelReviewContext.value = null
    const current = () =>
      !disposed.value && own === generation && visible.value && sameContext(context, scope.value)
    try {
      const value = await auth.projectModelSettings.getModel(context.projectID, wanted)
      if (!current()) return false
      const parent = await auth.projectModelSettings.getProvider(
        context.projectID,
        value.provider_id,
      )
      if (!current()) return false
      if (
        preserveDraft &&
        model.target === wanted &&
        (modelDirty.value || model.conflict || pending.value)
      ) {
        model.review = value
        modelReviewProvider.value = parent
        modelReviewContext.value = context
        model.phase = 'ready'
        model.requiresRead = false
      } else fillModel(value, parent)
      return true
    } catch (error) {
      if (current()) {
        const e = failure(error)
        model.phase = 'error'
        model.message = readMessage(e)
        denyCurrent(e, context)
      }
      return false
    } finally {
      if (generation === own) activeRead.value = false
    }
  }
  async function adoptProvider() {
    if (!canAdoptProvider.value || !provider.review) return false
    const value = provider.review,
      context = scope.value
    if (
      !(await ask(
        '使用当前 Provider 值？',
        '这会替换当前表单输入和编辑版本，不会自动保存。',
        '使用当前值',
      ))
    )
      return false
    if (!visible.value || !sameContext(context, scope.value) || provider.review !== value)
      return false
    editRejected()
    fillProvider(value)
    message.value = ''
    return true
  }
  async function adoptModel() {
    if (!canAdoptModel.value || !model.review || !modelReviewProvider.value) return false
    const value = model.review,
      parent = modelReviewProvider.value,
      context = scope.value
    if (
      !(await ask(
        '使用当前 Model 值？',
        '这会替换当前表单输入和编辑版本，不会自动保存。',
        '使用当前值',
      ))
    )
      return false
    if (!visible.value || !sameContext(context, scope.value) || model.review !== value) return false
    editRejected()
    fillModel(value, parent)
    message.value = ''
    return true
  }
  function invalidateAfterWrite(kind: ProjectModelSettingsProgress['kind']) {
    if (kind.startsWith('provider.')) providers.state.stale = providers.state.items.length > 0
    if (kind.startsWith('model.')) models.state.stale = models.state.items.length > 0
    if (kind.includes('.')) available.state.stale = available.state.items.length > 0
  }
  function acceptConfirmation(
    value: NonNullable<ProjectModelSettingsProgress['receipt']>,
    kind: ProjectModelSettingsProgress['kind'],
  ) {
    if (acceptedReceipt === value) return
    acceptedReceipt = value
    invalidateAfterWrite(kind)
    feedback.value = 'success'
    message.value = '本次写入已严格确认。当前列表和详情需要明确重读。'
    if ('credential_id' in value) {
      credentialValue.value = ''
      credential.fields = Object.freeze({})
      credential.conflict = credential.requiresRead = false
      if (kind === 'create') {
        preparedCredential.value = Object.freeze({
          credential_id: value.credential_id,
          version: value.version,
          bound: false,
        })
        credential.reference = value.credential_id
        credential.mode = 'manage'
        credential.metadata = null
        credentialMetadataContext.value = null
        credential.message =
          '凭据已创建，尚未绑定。请明确选择到 Provider 表单，再单独保存 Provider。'
      } else {
        credential.metadata = null
        credentialMetadataContext.value = null
        credential.message = value.deleted
          ? '凭据删除已确认。'
          : '凭据轮换已确认，请明确重读安全 metadata。'
      }
    } else if (kind.startsWith('provider.')) {
      credentialMetadataContext.value = null
      credential.metadata = null
      if (credential.mode === 'manage') {
        credential.requiresRead = true
        credential.message = 'Provider 写入已确认。操作凭据前请明确重读安全 metadata。'
      }
      if (kind === 'provider.delete') {
        provider.open = false
        provider.phase = 'inactive'
        providerBaseline.value = JSON.stringify(providerForm)
        deletion.open = false
      } else {
        provider.target = value.resource_id
        provider.version = value.version
        provider.mode = 'edit'
        if (submitted?.kind === 'provider') providerBaseline.value = submitted.form
        provider.original = null
        provider.requiresRead = true
        provider.conflict = false
        provider.fields = Object.freeze({})
        if (
          submitted?.kind === 'provider' &&
          preparedCredential.value?.credential_id === submitted.credentialRef
        )
          preparedCredential.value = Object.freeze({ ...preparedCredential.value, bound: true })
      }
    } else {
      if (kind === 'model.delete') {
        model.open = false
        model.phase = 'inactive'
        modelBaseline.value = JSON.stringify(modelForm)
        deletion.open = false
      } else {
        model.target = value.resource_id
        model.version = value.version
        model.mode = 'edit'
        if (submitted?.kind === 'model') modelBaseline.value = submitted.form
        model.original = null
        model.requiresRead = true
        model.conflict = false
        model.fields = Object.freeze({})
      }
    }
  }
  async function mutate(
    kind: 'provider' | 'model' | 'credential',
    work: () => Promise<NonNullable<ProjectModelSettingsProgress['receipt']>>,
  ) {
    if (blocked.value || !canMutate.value || pending.value || !scope.value) return false
    const context = scope.value
    try {
      editRejected()
      feedback.value = 'loading'
      message.value = ''
      submitted = Object.freeze({
        kind,
        form:
          kind === 'provider'
            ? JSON.stringify(providerForm)
            : kind === 'model'
              ? JSON.stringify(modelForm)
              : '',
        credentialRef: kind === 'provider' ? providerForm.credential_ref : '',
      })
      const pendingWrite = work()
      if (kind === 'credential') credentialValue.value = ''
      const value = await pendingWrite
      if (!visible.value || !sameContext(context, scope.value)) return false
      const current = progress.value
      if (current?.phase === 'confirmed') acceptConfirmation(value, current.kind)
      return true
    } catch (error) {
      if (
        !sameIdentity(context.identity, auth.personalContext.identity) ||
        boundID.value !== context.projectID
      )
        return false
      const e = failure(error)
      feedback.value = 'idle'
      message.value = writeExplanation(e, progress.value)
      const editor = kind === 'provider' ? provider : kind === 'model' ? model : credential
      editor.fields = errorFields(e)
      editor.message = message.value
      if (
        progress.value?.phase === 'rejected' &&
        ['VERSION_CONFLICT', 'INVALID_STATE', 'PROJECT_NOT_ACTIVE'].includes(e.problem?.code ?? '')
      ) {
        editor.conflict = editor.requiresRead = true
        if (kind === 'provider') {
          provider.review = null
          providerReviewContext.value = null
        }
        if (kind === 'model') {
          model.review = null
          modelReviewContext.value = null
        }
        if (kind === 'credential') credentialMetadataContext.value = null
      }
      denyCurrent(e, context)
      return false
    }
  }
  async function saveProvider() {
    if (!canSaveProvider.value || !scope.value) return false
    try {
      const input = providerInput(providerForm),
        command: ProjectConfigurationCommand =
          provider.mode === 'create'
            ? { kind: 'provider.create', input }
            : {
                kind: 'provider.update',
                id: provider.target!,
                expected_version: provider.version,
                input,
              }
      const captured = captureProjectConfigurationCommand(command),
        project = scope.value.projectID
      provider.fields = Object.freeze({})
      return mutate('provider', () =>
        captured.kind === 'provider.create'
          ? auth.projectModelSettings.createProvider(project, captured.input)
          : auth.projectModelSettings.updateProvider(
              project,
              captured.id,
              captured.expected_version,
              captured.input,
            ),
      )
    } catch {
      provider.fields = Object.freeze({ name: '请检查名称、协议、URL 和凭据 ID。' })
      provider.message = '输入不合法，未发送请求。'
      return false
    }
  }
  async function saveModel() {
    if (!canSaveModel.value || !scope.value || !modelProvider.value) return false
    try {
      const context = {
          provider_id: modelProvider.value.id,
          protocol: modelProvider.value.input.protocol,
        },
        input = modelInput(modelForm)
      const captured = captureProjectConfigurationCommand(
          model.mode === 'create'
            ? { kind: 'model.create', ...context, input }
            : {
                kind: 'model.update',
                ...context,
                id: model.target!,
                expected_version: model.version,
                input,
              },
        ),
        project = scope.value.projectID
      model.fields = Object.freeze({})
      return mutate('model', () =>
        captured.kind === 'model.create'
          ? auth.projectModelSettings.createModel(project, context, captured.input)
          : auth.projectModelSettings.updateModel(
              project,
              { ...context, id: captured.id },
              captured.expected_version,
              captured.input,
            ),
      )
    } catch {
      model.fields = Object.freeze({ name: '请检查名称、原生型号和能力选项。' })
      model.message = '输入不符合当前 chat 协议，未发送请求。'
      return false
    }
  }
  async function openCredential(mode: 'create' | 'manage', reference = '') {
    if (blocked.value || pending.value || (mode === 'create' && !canMutate.value)) return false
    if (credentialValue.value !== '') {
      const context = scope.value
      if (
        !(await ask(
          '放弃尚未提交的凭据材料？',
          '输入只在本地内存中使用，不会自动写入。',
          '放弃材料',
        )) ||
        !sameContext(context, scope.value)
      )
        return false
    }
    resetCredential()
    credential.open = true
    credential.mode = mode
    credential.phase = 'ready'
    credential.reference = reference
    if (mode === 'manage' && reference) return readCredential()
    return true
  }
  async function readCredential() {
    if (blocked.value || !scope.value) return false
    const target = captureID(credential.reference)
    if (!target) {
      credential.fields = Object.freeze({ reference: '请输入合法的 Credential ID。' })
      return false
    }
    if (credentialValue.value !== '' && credential.metadata?.credential_id !== target) {
      const context = scope.value
      if (
        !(await ask('更换凭据目标？', '更换目标会清除尚未提交的材料。', '更换目标')) ||
        !sameContext(context, scope.value)
      )
        return false
      credentialValue.value = ''
    }
    const context = scope.value,
      own = ++generation
    activeRead.value = true
    credential.phase = 'loading'
    credential.fields = Object.freeze({})
    credentialMetadataContext.value = null
    try {
      const value = await auth.projectModelSettings.getCredentialMetadata(context.projectID, target)
      if (own !== generation || !visible.value || !sameContext(context, scope.value)) return false
      credential.metadata = value
      credentialMetadataContext.value = context
      credential.phase = 'ready'
      credential.requiresRead = false
      credential.message = ''
      // A fresh metadata read alone does not rebase a rejected material command.
      return true
    } catch (error) {
      if (own === generation && sameContext(context, scope.value)) {
        const e = failure(error)
        credential.metadata = null
        credential.phase = 'error'
        credential.message = readMessage(e)
        denyCurrent(e, context)
      }
      return false
    } finally {
      if (own === generation) activeRead.value = false
    }
  }
  async function adoptCredentialMetadata() {
    if (
      blocked.value ||
      pending.value ||
      !credential.metadata ||
      !sameContext(credentialMetadataContext.value, scope.value)
    )
      return false
    const context = scope.value,
      value = credential.metadata
    if (
      !(await ask(
        '采用当前凭据版本？',
        '只采用安全 metadata 的版本；不会读取材料或自动提交。请重新明确输入新材料。',
        '采用当前版本',
      ))
    )
      return false
    if (!sameContext(context, scope.value) || credential.metadata !== value || !visible.value)
      return false
    editRejected()
    credential.conflict = credential.requiresRead = false
    credentialValue.value = ''
    credential.message = ''
    return true
  }
  async function createCredential() {
    if (!canCreateCredential.value || !scope.value) return false
    try {
      const value = captureProjectCredentialValue(credentialValue.value),
        project = scope.value.projectID
      return mutate('credential', () => auth.projectModelSettings.createCredential(project, value))
    } catch {
      credential.fields = Object.freeze({ value: '新材料应为 1–65536 个 UTF-8 字节。' })
      return false
    }
  }
  async function rotateCredential() {
    if (!canRotateCredential.value || !scope.value || !credential.metadata) return false
    try {
      const value = captureProjectCredentialValue(credentialValue.value),
        target = captureProjectCredentialTarget({
          kind: 'update',
          credential_id: credential.metadata.credential_id,
          expected_version: credential.metadata.version,
        }),
        project = scope.value.projectID
      return mutate('credential', () =>
        auth.projectModelSettings.updateCredential(
          project,
          target.credential_id,
          target.expected_version,
          value,
        ),
      )
    } catch {
      credential.fields = Object.freeze({ value: '请检查材料和当前凭据版本。' })
      return false
    }
  }
  function usePreparedCredential() {
    if (
      blocked.value ||
      pending.value ||
      !canMutate.value ||
      !provider.open ||
      !preparedCredential.value
    )
      return false
    providerForm.credential_ref = preparedCredential.value.credential_id
    message.value = '凭据 ID 已放入 Provider 草稿。请再明确保存 Provider。'
    return true
  }
  async function openDelete(kind: 'provider' | 'model' | 'credential') {
    if (blocked.value || pending.value || !canMutate.value) return false
    const record =
      kind === 'provider'
        ? provider.original
        : kind === 'model'
          ? model.original
          : credential.metadata
    if (
      !record ||
      (kind === 'provider' && (provider.requiresRead || provider.conflict)) ||
      (kind === 'model' && (model.requiresRead || model.conflict)) ||
      (kind === 'credential' &&
        (!sameContext(credentialMetadataContext.value, scope.value) ||
          credential.requiresRead ||
          credential.conflict))
    )
      return false
    Object.assign(deletion, {
      open: true,
      kind,
      target: 'id' in record ? record.id : record.credential_id,
      name: 'input' in record ? record.input.name : record.credential_id,
      version: record.version,
      replacement: 'unset',
      message: '',
    })
    if (kind === 'model' && available.state.phase !== 'ready' && available.state.phase !== 'empty')
      await available.fromFirst()
    return true
  }
  async function deleteSelected() {
    if (!deletion.open || blocked.value || pending.value || !canMutate.value || !scope.value)
      return false
    const project = scope.value.projectID,
      target = deletion.target,
      version = deletion.version
    try {
      if (deletion.kind === 'provider') {
        captureProjectConfigurationCommand({
          kind: 'provider.delete',
          id: target,
          expected_version: version,
        })
        return mutate('provider', () =>
          auth.projectModelSettings.deleteProvider(project, target, version),
        )
      }
      if (deletion.kind === 'model') {
        if (deletion.replacement === 'unset') {
          deletion.message = '请明确选择无替代或一个可用 chat 模型。'
          return false
        }
        const replacement = deletion.replacement === '' ? null : deletion.replacement
        if (
          replacement !== null &&
          (available.state.stale ||
            !available.state.items.some((item) => item.id === replacement && item.id !== target))
        ) {
          deletion.message = '请从当前已读可用目录明确选择候选。'
          return false
        }
        captureProjectConfigurationCommand({
          kind: 'model.delete',
          id: target,
          expected_version: version,
          replacement,
        })
        return mutate('model', () =>
          auth.projectModelSettings.deleteModel(project, target, version, replacement),
        )
      }
      captureProjectCredentialTarget({
        kind: 'delete',
        credential_id: target,
        expected_version: version,
      })
      const result = await mutate('credential', () =>
        auth.projectModelSettings.deleteCredential(project, target, version),
      )
      if (result) deletion.open = false
      return result
    } catch {
      deletion.message = '目标、版本或候选不合法，未发送请求。'
      return false
    }
  }
  async function lookupOriginal() {
    if (!canLookup.value || !scope.value) return false
    const context = scope.value
    try {
      if (progress.value?.domain === 'configuration')
        await auth.projectModelSettings.lookupConfiguration()
      else await auth.projectModelSettings.lookupCredential()
      if (!visible.value || !sameContext(context, scope.value)) return false
      message.value =
        progress.value?.observation === 'observed'
          ? '查证观察到了历史回执，尚未严格确认本次完整输入。不会自动推进或清除原请求。'
          : '本次未观察到历史回执；这不是回滚证明，原请求仍待决。'
      return true
    } catch (error) {
      if (
        sameIdentity(context.identity, auth.personalContext.identity) &&
        boundID.value === context.projectID
      ) {
        const e = failure(error)
        message.value = '本次查证失败，原请求仍待决。'
        denyCurrent(e, context)
      }
      return false
    }
  }
  async function replayOriginal() {
    if (!canReplay.value || !scope.value) return false
    const context = scope.value
    try {
      const value = await auth.projectModelSettings.retryOriginal()
      if (
        !visible.value ||
        !sameContext(context, scope.value) ||
        progress.value?.phase !== 'confirmed'
      )
        return false
      acceptConfirmation(value, progress.value.kind)
      return true
    } catch (error) {
      if (
        sameIdentity(context.identity, auth.personalContext.identity) &&
        boundID.value === context.projectID
      ) {
        const e = failure(error)
        message.value = writeExplanation(e, progress.value)
        denyCurrent(e, context)
      }
      return false
    }
  }
  async function abandonPending() {
    if (blocked.value || !progress.value || !scope.value) return false
    const context = scope.value
    if (
      !(await ask(
        '放弃本地原请求追踪？',
        '这不会撤销服务端命令，也不证明未执行。原凭据材料和重放能力会被清除。',
        '放弃追踪',
      ))
    )
      return false
    if (!visible.value || !sameContext(context, scope.value)) return false
    auth.projectModelSettings.abandonPending()
    credentialValue.value = ''
    message.value = '已放弃本地追踪；未撤销服务端命令。'
    return true
  }
  async function closeProvider() {
    const context = scope.value
    if (!(await discardForReplacement('provider'))) return false
    if (!visible.value || !sameContext(context, scope.value)) return false
    resetProvider()
    return true
  }
  async function closeModel() {
    const context = scope.value
    if (!(await discardForReplacement('model'))) return false
    if (!visible.value || !sameContext(context, scope.value)) return false
    resetModel()
    return true
  }
  async function closeCredential() {
    if (blocked.value) return false
    const context = scope.value
    if (
      credentialValue.value !== '' &&
      (!(await ask(
        '放弃尚未提交的凭据材料？',
        '已确认或待决的服务端命令不会因此撤销。',
        '放弃材料',
      )) ||
        !sameContext(context, scope.value))
    )
      return false
    credentialValue.value = ''
    credential.open = false
    return true
  }
  function closeDelete() {
    if (blocked.value) return
    deletion.open = false
  }
  async function showModels() {
    if (blocked.value) return false
    modelsOpen.value = true
    if (models.state.phase === 'waiting' || models.state.phase === 'inactive')
      return models.fromFirst()
    return true
  }
  function invalidateCurrent() {
    // Workspace may restore its cached context after same-identity checking.
    // Losing this scope requires a newer successful Owner Get before reuse.
    if (scope.value) gateRead.value = scope.value
    scope.value = null
    hideObservations()
    needsInitial = pageMode.value !== 'none'
  }
  function bindContext() {
    if (disposed.value) return
    const currentIdentity = auth.personalContext.identity
    if ((identity || currentIdentity) && !sameIdentity(identity, currentIdentity)) {
      identity = currentIdentity
      gateRead.value = null
      clearAll()
      scope.value = null
      boundID.value = null
      needsInitial = pageMode.value !== 'none'
    }
    const current = workspace.currentReadContext.value
    if (!current || !authorized() || pageMode.value === 'none') {
      if (scope.value) invalidateCurrent()
      if (
        workspace.detail.phase === 'unavailable' &&
        currentIdentity &&
        sameIdentity(identity, currentIdentity)
      ) {
        clearAll()
        boundID.value = null
      }
      return
    }
    if (boundID.value !== null && boundID.value !== current.projectID) clearAll()
    if (gateRead.value !== null) {
      const prior = gateRead.value
      if (
        sameIdentity(prior.identity, current.identity) &&
        prior.projectID === current.projectID &&
        current.readGeneration <= prior.readGeneration
      )
        return
      gateRead.value = null
    }
    if (!sameContext(scope.value, current)) {
      hideObservations()
      scope.value = current
      boundID.value = current.projectID
      needsInitial = true
    }
    const completed = progress.value
    if (visible.value && completed?.phase === 'confirmed' && completed.receipt)
      acceptConfirmation(completed.receipt, completed.kind)
    if (needsInitial && visible.value && !auth.state.busy && !activeRead.value) {
      needsInitial = false
      void (pageMode.value === 'providers' ? providers.fromFirst() : available.fromFirst())
    }
  }
  const stop = watch(
    () =>
      [
        auth.personalContext.identity,
        auth.personalContext.phase,
        auth.state.phase,
        auth.state.busy,
        workspace.currentReadContext.value,
        workspace.detail.phase,
        auth.projectModelSettings.progress?.receipt,
      ] as const,
    bindContext,
    { flush: 'sync' },
  )
  async function confirmLeave(target?: string) {
    if (disposed.value || pageMode.value === 'none') return true
    const currentPath = projectRoute(route),
      targetPath = projectRoute(target)
    if (targetPath && currentPath && targetPath.path === currentPath.path) return true
    if (auth.state.busy || activeRead.value) return false
    if (!dirty.value) return true
    const context = scope.value
    if (
      !(await ask(
        '离开项目模型设置？',
        '未保存输入、未绑定候选及原请求追踪会被清除；已执行命令不会撤销。',
        '放弃并离开',
      ))
    )
      return false
    if (!sameContext(context, scope.value) || !visible.value) return false
    clearAll()
    return true
  }
  function afterNavigation(to: string, _from = '') {
    const parsed = projectRoute(to),
      next =
        parsed?.suffix === '/settings/model-providers'
          ? 'providers'
          : parsed?.suffix === '/settings/available-models'
            ? 'available'
            : 'none'
    const prior = projectRoute(route)
    if (prior && parsed && prior.path === parsed.path && pageMode.value === next) {
      route = to
      bindContext()
      return
    }
    if (
      route &&
      (next !== pageMode.value ||
        prior?.username !== parsed?.username ||
        prior?.project_name !== parsed?.project_name)
    ) {
      clearAll()
      scope.value = null
      boundID.value = null
    }
    route = to
    pageMode.value = next
    needsInitial = next !== 'none'
    bindContext()
  }
  async function readOwner() {
    if (auth.state.busy || confirmation.open) return
    await workspace.readCurrent()
    bindContext()
  }
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  if (typeof window !== 'undefined') window.addEventListener('beforeunload', beforeUnload)
  function dispose() {
    if (disposed.value) return
    disposed.value = true
    stop()
    gateRead.value = null
    clearAll()
    scope.value = null
    boundID.value = null
    if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload)
  }
  return {
    visible,
    viewContext,
    currentProject,
    canMutate,
    blocked,
    dirty,
    pending,
    progress,
    message,
    feedback,
    providers,
    models,
    available,
    modelsOpen,
    showModels,
    provider,
    providerForm,
    providerDirty,
    canSaveProvider,
    canAdoptProvider,
    newProvider,
    readProvider,
    saveProvider,
    adoptProvider,
    closeProvider,
    model,
    modelForm,
    modelProvider,
    modelReviewProvider,
    modelDirty,
    canSaveModel,
    canAdoptModel,
    newModel,
    readModel,
    saveModel,
    adoptModel,
    closeModel,
    credential,
    credentialValue,
    preparedCredential,
    canCreateCredential,
    canRotateCredential,
    openCredential,
    readCredential,
    adoptCredentialMetadata,
    createCredential,
    rotateCredential,
    closeCredential,
    usePreparedCredential,
    deletion,
    openDelete,
    deleteSelected,
    closeDelete,
    canLookup,
    canReplay,
    lookupOriginal,
    replayOriginal,
    abandonPending,
    readOwner,
    confirmation,
    confirm: () => finishConfirmation(true),
    cancelConfirmation: () => finishConfirmation(false),
    confirmLeave,
    afterNavigation,
    dispose,
  }
}
export type ProjectModelSettings = ReturnType<typeof createProjectModelSettings>
export const projectModelSettingsKey: InjectionKey<ProjectModelSettings> =
  Symbol('ProjectModelSettings')
export function useProjectModelSettings() {
  const owner = inject(projectModelSettingsKey)
  if (!owner) throw new Error('Project model settings require the App owner')
  return owner
}
