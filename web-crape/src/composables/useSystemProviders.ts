import {
  computed,
  inject,
  reactive,
  readonly,
  ref,
  shallowReactive,
  watch,
  type InjectionKey,
} from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProviderCommand,
  type Provider,
  type ProviderInput,
  type ProviderModel,
  type ProviderProtocol,
  type ProviderReceipt,
  type CredentialMetadata,
} from '../api/system-providers'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

type Phase = 'inactive' | 'loading' | 'ready' | 'empty' | 'error'
type PageTarget = Readonly<{ index: number; cursor?: string }>
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const emptyProviders: readonly Provider[] = Object.freeze([]),
  emptyModels: readonly ProviderModel[] = Object.freeze([])

// App lifetime owns ordinary draft fields and confirmation. The Session owner
// alone retains write-only material and complete immutable command intentions.
export function createSystemProviders(auth: SessionController = useSession()) {
  const api = auth.system.providers
  const list = shallowReactive({
    phase: 'inactive' as Phase,
    rows: emptyProviders,
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
  })
  const detail = shallowReactive({
    phase: 'inactive' as Phase,
    value: null as Provider | null,
    message: '',
  })
  const models = shallowReactive({
    phase: 'inactive' as Phase,
    rows: emptyModels,
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
    firstEmpty: false,
  })
  const metadata = shallowReactive({
    phase: 'inactive' as 'inactive' | 'loading' | 'unconfigured' | 'ready' | 'missing' | 'error',
    value: null as CredentialMetadata | null,
    message: '',
  })
  const editor = shallowReactive({
    mode: null as 'create' | 'edit' | null,
    message: '',
    reviewed: false,
    currentVersion: '',
  })
  const draft = reactive({
    name: '',
    protocol: '' as ProviderProtocol | '',
    base_url: '',
    enabled: '' as '' | 'true' | 'false',
  })
  const state = shallowReactive({
    writeMessage: '',
    confirmed: null as ProviderReceipt | null,
    requiresReload: false,
    materialMessage: '',
  })
  const dialogs = shallowReactive({
    deletion: null as Readonly<{ id: string; version: string; name: string }> | null,
  })
  const confirmation = reactive({ open: false, title: '', message: '' })
  const feedback = ref<'idle' | 'loading' | 'success'>('idle'),
    checking = ref(false)
  const progress = computed(() => api.progress),
    material = computed(() => api.material)
  let base: Provider | null = null,
    selectedID: string | null = null
  let identity = auth.personalContext.identity,
    revision = 0,
    attached = false,
    activeRoute = false,
    disposed = false,
    initial = true
  const readGenerations = { list: 0, detail: 0, models: 0, metadata: 0 }
  const activeReads = new Set<keyof typeof readGenerations>()
  let listHistory: (string | undefined)[] = [],
    modelHistory: (string | undefined)[] = [],
    listIndex = 0,
    modelIndex = 0
  let nextList: string | null = null,
    nextModels: string | null = null,
    listTarget: PageTarget = { index: 0 },
    modelTarget: PageTarget = { index: 0 }
  let timer: ReturnType<typeof setTimeout> | undefined,
    confirmResult: ((value: boolean) => void) | null = null
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    !!auth.personalContext.identity &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied
  const current = (own: number, captured: PersonalIdentity | null) =>
    !disposed &&
    activeRoute &&
    own === revision &&
    authorized() &&
    same(captured, auth.personalContext.identity)
  const changed = computed(() =>
    editor.mode === 'create'
      ? !!draft.name || !!draft.protocol || !!draft.base_url || draft.enabled !== ''
      : editor.mode === 'edit' &&
        !!base &&
        (draft.name !== base.input.name ||
          draft.base_url !== base.input.base_url ||
          draft.protocol !== base.input.protocol ||
          draft.enabled !== String(base.input.enabled)),
  )
  const dirty = computed(
    () =>
      changed.value ||
      material.value.present ||
      material.value.invalid ||
      ['submitting', 'uncertain', 'rejected'].includes(progress.value?.phase ?? ''),
  )
  const blocked = computed(() => auth.state.busy || checking.value)
  const locked = computed(
    () =>
      blocked.value ||
      state.requiresReload ||
      progress.value?.phase === 'submitting' ||
      progress.value?.phase === 'uncertain' ||
      (progress.value?.phase === 'rejected' && !editor.reviewed),
  )
  const canDelete = computed(
    () => !!detail.value && models.phase === 'empty' && models.firstEmpty && !locked.value,
  )
  function stopFeedback() {
    clearTimeout(timer)
    timer = undefined
    feedback.value = 'idle'
  }
  function finishConfirmation(value: boolean) {
    confirmation.open = false
    const done = confirmResult
    confirmResult = null
    done?.(value)
  }
  function clearDraft() {
    base = null
    Object.assign(draft, { name: '', protocol: '', base_url: '', enabled: '' })
    Object.assign(editor, { mode: null, message: '', reviewed: false, currentVersion: '' })
    dialogs.deletion = null
    state.materialMessage = ''
  }
  function discard(reload = false) {
    ++revision
    api.abandon()
    clearDraft()
    stopFeedback()
    state.writeMessage = ''
    state.confirmed = null
    state.requiresReload = reload
  }
  function clearReads() {
    for (const key of Object.keys(readGenerations) as (keyof typeof readGenerations)[]) {
      ++readGenerations[key]
      if (activeReads.has(key)) api.abandonRead(`provider-${key}`)
    }
    activeReads.clear()
    listHistory = []
    modelHistory = []
    listIndex = modelIndex = 0
    nextList = nextModels = null
    Object.assign(list, {
      phase: 'inactive',
      rows: emptyProviders,
      message: '',
      cursorInvalid: false,
      hasPrevious: false,
      hasNext: false,
    })
    clearSelectedRead()
  }
  function clearSelectedRead() {
    Object.assign(detail, { phase: 'inactive', value: null, message: '' })
    Object.assign(models, {
      phase: 'inactive',
      rows: emptyModels,
      message: '',
      cursorInvalid: false,
      firstEmpty: false,
      hasPrevious: false,
      hasNext: false,
    })
    Object.assign(metadata, { phase: 'inactive', value: null, message: '' })
    modelHistory = []
    modelIndex = 0
    nextModels = null
  }
  function invalidate() {
    discard()
    clearReads()
    selectedID = null
    initial = true
    finishConfirmation(false)
  }
  function readContext(part: keyof typeof readGenerations) {
    const own = revision,
      captured = auth.personalContext.identity,
      generation = ++readGenerations[part]
    activeReads.add(part)
    return {
      valid: () => attached && current(own, captured) && generation === readGenerations[part],
      finish: () => {
        if (generation === readGenerations[part]) activeReads.delete(part)
      },
    }
  }
  function readFailure(error: unknown, label: string) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    return e.problem?.code === 'CURSOR_INVALID'
      ? '分页链接已失效，请返回首页重新加载。'
      : e.kind === 'cancelled'
        ? '读取已取消或等待超时，请在请求结束后重试。'
        : `${label}读取失败，请重试。`
  }
  async function readList(target: PageTarget) {
    if (!attached || !authorized() || blocked.value) return
    initial = false
    listTarget = target
    const operation = readContext('list')
    Object.assign(list, {
      phase: 'loading',
      rows: emptyProviders,
      message: '',
      cursorInvalid: false,
      hasNext: false,
      hasPrevious: false,
    })
    try {
      const page = await api.list(target.cursor === undefined ? {} : { cursor: target.cursor })
      if (!operation.valid()) return
      listHistory = [...listHistory.slice(0, target.index), target.cursor]
      listIndex = target.index
      nextList = page.next_cursor
      Object.assign(list, {
        rows: page.items,
        phase: page.items.length ? 'ready' : 'empty',
        hasPrevious: listIndex > 0,
        hasNext: nextList !== null,
      })
      state.requiresReload = false
    } catch (error) {
      if (operation.valid())
        Object.assign(list, {
          phase: 'error',
          message: readFailure(error, 'Provider列表'),
          cursorInvalid:
            error instanceof AccountFailure && error.problem?.code === 'CURSOR_INVALID',
        })
    } finally {
      operation.finish()
    }
  }
  async function readModels(target: PageTarget) {
    const provider = detail.value
    if (!provider || !attached || !authorized() || blocked.value) return
    modelTarget = target
    const operation = readContext('models')
    Object.assign(models, {
      phase: 'loading',
      rows: emptyModels,
      message: '',
      cursorInvalid: false,
      firstEmpty: false,
      hasPrevious: false,
      hasNext: false,
    })
    try {
      const page = await api.models({
        provider_id: provider.id,
        protocol: provider.input.protocol,
        ...(target.cursor === undefined ? {} : { cursor: target.cursor }),
      })
      if (!operation.valid() || detail.value?.id !== provider.id) return
      modelHistory = [...modelHistory.slice(0, target.index), target.cursor]
      modelIndex = target.index
      nextModels = page.next_cursor
      Object.assign(models, {
        rows: page.items,
        phase: page.items.length ? 'ready' : 'empty',
        firstEmpty: target.index === 0 && page.items.length === 0,
        hasPrevious: modelIndex > 0,
        hasNext: nextModels !== null,
      })
    } catch (error) {
      if (operation.valid())
        Object.assign(models, {
          phase: 'error',
          message: readFailure(error, 'Models'),
          cursorInvalid:
            error instanceof AccountFailure && error.problem?.code === 'CURSOR_INVALID',
        })
    } finally {
      operation.finish()
    }
  }
  async function readMetadata() {
    const provider = detail.value
    if (!provider || !attached || !authorized() || blocked.value) return
    const ref = provider.input.credential_ref
    Object.assign(metadata, {
      phase: ref === null ? 'unconfigured' : 'loading',
      value: null,
      message: '',
    })
    if (ref === null) return
    const operation = readContext('metadata')
    try {
      const value = await api.metadata(ref)
      if (
        operation.valid() &&
        detail.value?.id === provider.id &&
        detail.value.input.credential_ref === ref
      )
        Object.assign(metadata, { phase: 'ready', value })
    } catch (error) {
      if (operation.valid()) {
        const missing = error instanceof AccountFailure && error.problem?.status === 404
        Object.assign(metadata, {
          phase: missing ? 'missing' : 'error',
          message: missing ? '引用不存在或不可读取' : '凭据状态读取失败',
        })
      }
    } finally {
      operation.finish()
    }
  }
  function populateEditor(value: Provider) {
    base = value
    Object.assign(draft, {
      name: value.input.name,
      protocol: value.input.protocol,
      base_url: value.input.base_url,
      enabled: String(value.input.enabled),
    })
    Object.assign(editor, {
      mode: 'edit',
      message: '',
      reviewed: false,
      currentVersion: value.version,
    })
  }
  async function readSelected(target: string, editing = false) {
    if (!attached || !authorized() || blocked.value) return
    selectedID = target
    clearSelectedRead()
    const operation = readContext('detail')
    detail.phase = 'loading'
    try {
      const value = await api.get(target)
      if (!operation.valid() || selectedID !== target) return
      Object.assign(detail, { phase: 'ready', value })
      if (editing) populateEditor(value)
      await readModels({ index: 0 })
      if (operation.valid() && !blocked.value) await readMetadata()
    } catch (error) {
      if (operation.valid())
        Object.assign(detail, { phase: 'error', message: readFailure(error, '当前配置') })
    } finally {
      operation.finish()
    }
  }
  function requestConfirmation(title: string) {
    if (confirmResult) return Promise.resolve(false)
    Object.assign(confirmation, {
      open: true,
      title,
      message:
        '输入和本次操作的本地追踪将清除。此前请求仍可能生效；已创建的新凭据可能尚未绑定。这里不会撤销写入或删除凭据。',
    })
    return new Promise<boolean>((resolve) => {
      confirmResult = resolve
    })
  }
  async function confirmLeave() {
    if (!dirty.value) return true
    const hadIntent = !!progress.value && progress.value.phase !== 'confirmed'
    const accepted = await requestConfirmation('放弃未保存修改？')
    if (accepted) discard(hadIntent)
    return accepted
  }
  async function guarded(work: () => void | Promise<void>) {
    if (!(await confirmLeave()) || disposed || !activeRoute || !authorized()) return
    await work()
  }
  function ordinaryInput(): ProviderInput {
    if (!draft.protocol || draft.enabled === '') throw new AccountFailure('invalid-input')
    return {
      name: draft.name,
      protocol: draft.protocol,
      base_url: draft.base_url,
      enabled: draft.enabled === 'true',
      credential_ref:
        progress.value?.credential?.credential_id ?? base?.input.credential_ref ?? null,
      options: {},
    }
  }
  function showFailure(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    state.writeMessage =
      progress.value?.phase === 'uncertain'
        ? e.problem?.code === 'IDEMPOTENCY_KEY_REUSED'
          ? '原请求标识与输入冲突，原意图已锁定。请明确放弃后重新读取。'
          : '本次结果尚未确认。请检查原请求，再显式重试原请求；此前请求仍可能生效。'
        : progress.value?.credential
          ? '新凭据已创建，Provider尚未确认保存。保留新凭据引用；明确读取当前配置并核对后可重新保存。'
          : e.kind === 'invalid-input' || e.problem?.code === 'INVALID_ARGUMENT'
            ? '请检查名称、协议、URL、启用状态与材料长度，输入不会自动改写。'
            : e.problem?.code === 'VERSION_CONFLICT'
              ? '当前版本已变化，请明确读取当前配置并核对输入。'
              : '操作未完成，请检查当前配置。'
  }
  async function mutate(work: () => Promise<ProviderReceipt>) {
    const own = revision,
      captured = auth.personalContext.identity
    stopFeedback()
    feedback.value = 'loading'
    state.writeMessage = ''
    editor.message = ''
    try {
      const result = await work()
      if (!current(own, captured)) return
      state.confirmed = result
      clearDraft()
      feedback.value = 'success'
      state.writeMessage =
        result.kind === 'provider.delete'
          ? 'Provider 已删除，凭据保持不变。'
          : 'Provider 配置已保存。此结果仅确认配置，不证明连接可用。'
      timer = setTimeout(() => {
        if (current(own, captured)) feedback.value = 'idle'
      }, 2400)
      selectedID = result.kind === 'provider.delete' ? null : result.resource_id
      clearSelectedRead()
      await readList({ index: 0 })
      if (current(own, captured) && !blocked.value && selectedID) await readSelected(selectedID)
      if (current(own, captured) && (list.phase === 'error' || detail.phase === 'error'))
        state.writeMessage = '操作已确认，当前配置读取失败。请仅重读当前配置。'
    } catch (error) {
      if (!current(own, captured)) return
      feedback.value = 'idle'
      showFailure(error)
    }
  }
  async function initialRead() {
    if (!disposed && attached && activeRoute && initial && authorized() && !blocked.value) {
      const own = revision,
        captured = auth.personalContext.identity,
        selected = selectedID
      await readList({ index: 0 })
      if (current(own, captured) && attached && !blocked.value && selected)
        await readSelected(selected)
    }
  }
  function afterNavigation(to: string, from: string) {
    const page = (path: string) => path.split(/[?#]/, 1)[0] === '/system/providers'
    if (page(from) && !page(to)) {
      activeRoute = false
      invalidate()
    } else if (page(to)) activeRoute = true
  }
  const stopIdentity = watch(
    () =>
      [
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.user?.role,
        auth.system.denied,
        auth.state.busy,
      ] as const,
    ([phase, value]) => {
      if (phase === 'checking') return
      if (
        phase === 'invalid' ||
        !same(identity, value) ||
        auth.state.user?.role !== 'admin' ||
        auth.system.denied
      ) {
        invalidate()
        identity = value
      }
      void initialRead()
    },
  )
  function beforeUnload(event: BeforeUnloadEvent) {
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }
  window.addEventListener('beforeunload', beforeUnload)
  return {
    auth,
    list: readonly(list),
    detail: readonly(detail),
    models: readonly(models),
    metadata: readonly(metadata),
    editor: readonly(editor),
    dialogs: readonly(dialogs),
    state: readonly(state),
    confirmation: readonly(confirmation),
    draft,
    feedback,
    checking,
    progress,
    material,
    changed,
    dirty,
    blocked,
    locked,
    canDelete,
    attach() {
      attached = true
      activeRoute = true
      void initialRead()
    },
    detach() {
      attached = false
      clearReads()
      initial = true
    },
    afterNavigation,
    confirmLeave,
    finishConfirmation,
    closeEditor() {
      return guarded(clearDraft)
    },
    closeDelete() {
      return guarded(() => {
        dialogs.deletion = null
      })
    },
    refresh() {
      return guarded(async () => {
        if (blocked.value) return
        selectedID = null
        clearSelectedRead()
        await readList({ index: 0 })
      })
    },
    select(value: Provider) {
      return guarded(() => readSelected(value.id))
    },
    back() {
      return guarded(() => {
        selectedID = null
        clearSelectedRead()
      })
    },
    previous() {
      return guarded(() =>
        !blocked.value && list.hasPrevious
          ? readList({ index: listIndex - 1, cursor: listHistory[listIndex - 1] })
          : undefined,
      )
    },
    next() {
      return guarded(() =>
        !blocked.value && nextList !== null
          ? readList({ index: listIndex + 1, cursor: nextList })
          : undefined,
      )
    },
    retryList() {
      return guarded(() =>
        !blocked.value ? readList(list.cursorInvalid ? { index: 0 } : listTarget) : undefined,
      )
    },
    previousModels() {
      return guarded(() =>
        !blocked.value && models.hasPrevious
          ? readModels({ index: modelIndex - 1, cursor: modelHistory[modelIndex - 1] })
          : undefined,
      )
    },
    nextModels() {
      return guarded(() =>
        !blocked.value && nextModels !== null
          ? readModels({ index: modelIndex + 1, cursor: nextModels })
          : undefined,
      )
    },
    retryModels() {
      return guarded(() =>
        !blocked.value ? readModels(models.cursorInvalid ? { index: 0 } : modelTarget) : undefined,
      )
    },
    retryDetail() {
      return guarded(() => (!blocked.value && selectedID ? readSelected(selectedID) : undefined))
    },
    retryMetadata: readMetadata,
    openCreate() {
      return guarded(() => {
        if (locked.value) return
        stopFeedback()
        clearDraft()
        editor.mode = 'create'
        state.writeMessage = ''
        state.confirmed = null
        api.abandon()
      })
    },
    openEdit() {
      return guarded(() => {
        if (locked.value || !detail.value) return
        stopFeedback()
        const target = detail.value.id
        api.abandon()
        state.writeMessage = ''
        return readSelected(target, true)
      })
    },
    openDelete() {
      if (canDelete.value && detail.value) {
        stopFeedback()
        api.abandon()
        state.writeMessage = ''
        dialogs.deletion = Object.freeze({
          id: detail.value.id,
          version: detail.value.version,
          name: detail.value.input.name,
        })
      }
    },
    setMaterial(value: string) {
      try {
        api.setMaterial(value)
        state.materialMessage = ''
      } catch {
        state.materialMessage = '新凭据必须是合法Unicode文本，最多 65536 UTF-8 字节。'
      }
    },
    save() {
      if (
        locked.value ||
        !editor.mode ||
        (!changed.value && !material.value.present && !editor.reviewed)
      )
        return Promise.resolve()
      try {
        const input = ordinaryInput()
        if (editor.reviewed) return mutate(() => api.rebase(input))
        const command = captureProviderCommand(
          editor.mode === 'create'
            ? { kind: 'provider.create', input }
            : { kind: 'provider.update', id: base!.id, expected_version: base!.version, input },
          material.value.present,
        )
        return mutate(() => api.start(command))
      } catch (error) {
        showFailure(error)
        return Promise.resolve()
      }
    },
    remove() {
      const target = dialogs.deletion
      if (!target || !canDelete.value || locked.value) return Promise.resolve()
      return mutate(() =>
        api.start({ kind: 'provider.delete', id: target.id, expected_version: target.version }),
      )
    },
    async reviewConflict() {
      if (blocked.value || !progress.value?.canRebase) return
      const own = revision,
        captured = auth.personalContext.identity
      try {
        const latest = await api.readForRebase()
        if (!current(own, captured)) return
        base = latest
        editor.reviewed = true
        editor.currentVersion = latest.version
        editor.message = '已读取当前版本。请核对保留的普通输入，再明确保存；不会再次创建凭据。'
      } catch {
        if (current(own, captured)) editor.message = '当前配置读取失败，尚不能重新保存。'
      }
    },
    async checkOriginal() {
      if (blocked.value || progress.value?.phase !== 'uncertain') return
      checking.value = true
      const own = revision,
        captured = auth.personalContext.identity
      try {
        const found = await api.checkOriginal()
        if (current(own, captured))
          state.writeMessage = found
            ? '已观察到历史回执，当前对象或绑定仍未确认。请显式重试原请求以核对原输入。'
            : '本次未观察到历史回执；不代表此前请求未提交。可显式重试原请求或放弃。'
      } catch {
        if (current(own, captured))
          state.writeMessage = '本次查证失败，不代表此前请求未提交。请保留原请求或明确放弃。'
      } finally {
        checking.value = false
        void initialRead()
      }
    },
    retryOriginal() {
      return !blocked.value && progress.value?.canRetryOriginal
        ? mutate(() => api.retryOriginal())
        : Promise.resolve()
    },
    async abandonOperation() {
      if (await requestConfirmation('放弃本次操作？')) discard(true)
    },
    dispose() {
      if (disposed) return
      disposed = true
      stopIdentity()
      invalidate()
      window.removeEventListener('beforeunload', beforeUnload)
    },
  }
}
export type SystemProvidersController = ReturnType<typeof createSystemProviders>
export const systemProvidersKey: InjectionKey<SystemProvidersController> =
  Symbol('system-providers')
export function useSystemProviders() {
  const value = inject(systemProvidersKey)
  if (!value) throw new Error('System Providers require the App controller')
  return value
}
