import { computed, reactive, readonly, ref, shallowReactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import { type SavedModel, type SelectionReceipt } from '../api/system-model-selection'
import {
  captureMeetingSummaryCommand,
  type MeetingSummaryCommand,
  type MeetingSummaryState,
} from '../api/system-meeting-summary'
import type { Provider, ProviderModel } from '../api/system-providers'
import {
  useSession,
  type PersonalIdentity,
  type SessionController,
  type MeetingSummaryRead,
} from './useSession'

type SelectionPurpose = 'summary'
const selectionPurposes = ['summary'] as const
type Phase = 'inactive' | 'loading' | 'ready' | 'empty' | 'error'
type Draft = Record<SelectionPurpose, string | null>
type PageTarget = Readonly<{ index: number; cursor?: string }>
type PageKind = 'providers' | 'models'
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const emptyDraft = (): Draft => ({ summary: null })
function pageState<T>() {
  return shallowReactive({
    phase: 'inactive' as Phase,
    rows: Object.freeze([]) as readonly T[],
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
  })
}
function cursorState() {
  return {
    history: [] as (string | undefined)[],
    index: 0,
    next: null as string | null,
    target: { index: 0 } as PageTarget,
  }
}
function referenceState() {
  return shallowReactive({
    id: null as string | null,
    phase: 'inactive' as Phase,
    value: null as SavedModel | null,
    message: '',
  })
}
function choiceState() {
  return shallowReactive({
    mode: 'providers' as PageKind,
    providers: pageState<Provider>(),
    models: pageState<ProviderModel>(),
    provider: shallowReactive({
      phase: 'inactive' as Phase,
      value: null as Provider | null,
      message: '',
    }),
    providerID: null as string | null,
  })
}
export function meetingSummaryReason(value: SavedModel): string {
  if (!value.provider.input.enabled) return 'Provider 已停用'
  if (!value.model.input.enabled) return 'Model 已停用'
  if (value.model.input.type !== 'chat') return '会议 Summary 需要 chat Model'
  return ''
}
// This branch owns only Summary observations and drafts. The existing page owns
// the sole confirmation host; Session retains this branch's private original intent.
export function createMeetingSummarySettings(
  auth: SessionController = useSession(),
  requestConfirmation: (title: string, message?: string) => Promise<boolean>,
  canResume: () => boolean,
) {
  const api = auth.system.selection.meetingSummary
  const selection = shallowReactive({
    phase: 'inactive' as Phase,
    value: null as MeetingSummaryState | null,
    message: '',
  })
  const references = {
    summary: referenceState(),
  }
  const choices = {
    summary: choiceState(),
  }
  const cursors = Object.fromEntries(
    selectionPurposes.map((purpose) => [
      purpose,
      { providers: cursorState(), models: cursorState() },
    ]),
  ) as Record<SelectionPurpose, Record<PageKind, ReturnType<typeof cursorState>>>
  const draft = reactive(emptyDraft())
  const draftDetails = shallowReactive<Record<SelectionPurpose, SavedModel | null>>({
    summary: null,
  })
  const editor = reactive({ open: false, version: '', message: '' })
  const state = shallowReactive({
    writeMessage: '',
    confirmed: null as SelectionReceipt | null,
    requiresReload: false,
  })
  const activePurpose = ref<SelectionPurpose | null>(null)
  const feedback = ref<'idle' | 'loading' | 'success'>('idle'),
    checking = ref(false)
  const progress = computed(() => api.progress)
  const blocked = computed(() => auth.state.busy || checking.value)
  const locked = computed(
    () =>
      checking.value ||
      state.requiresReload ||
      ['submitting', 'uncertain', 'rejected'].includes(progress.value?.phase ?? ''),
  )
  let base: MeetingSummaryState | null = null,
    baseline = JSON.stringify(emptyDraft())
  let identity = auth.personalContext.identity,
    revision = 0,
    writeGeneration = 0,
    choiceRevision = 0,
    attached = false,
    activeRoute = false,
    disposed = false,
    initial = true
  let timer: ReturnType<typeof setTimeout> | undefined
  let pendingChoice: {
    revision: number
    purpose: SelectionPurpose
    work: () => Promise<unknown>
  } | null = null
  const reads = new Map<MeetingSummaryRead, number>(),
    activeReads = new Set<MeetingSummaryRead>()
  const changed = computed(() => editor.open && JSON.stringify(draft) !== baseline)
  const dirty = computed(
    () =>
      changed.value ||
      ['submitting', 'uncertain', 'rejected'].includes(progress.value?.phase ?? ''),
  )
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
  const canSave = computed(
    () =>
      editor.open &&
      !!base &&
      changed.value &&
      !blocked.value &&
      !locked.value &&
      selectionPurposes.every((purpose) => {
        const target = draft[purpose],
          value = draftDetails[purpose]
        if (target === null) return false
        return !!value && value.model.id === target && !meetingSummaryReason(value)
      }),
  )
  function stopFeedback() {
    clearTimeout(timer)
    timer = undefined
    feedback.value = 'idle'
  }
  function resetPage(purpose: SelectionPurpose, kind: PageKind) {
    Object.assign(choices[purpose][kind], {
      phase: 'inactive',
      rows: Object.freeze([]),
      message: '',
      cursorInvalid: false,
      hasPrevious: false,
      hasNext: false,
    })
    cursors[purpose][kind] = cursorState()
  }
  function resetChoice(purpose: SelectionPurpose) {
    resetPage(purpose, 'providers')
    resetPage(purpose, 'models')
    Object.assign(choices[purpose], { mode: 'providers', providerID: null })
    Object.assign(choices[purpose].provider, { phase: 'inactive', value: null, message: '' })
  }
  function cancelRead(scope: MeetingSummaryRead) {
    reads.set(scope, (reads.get(scope) ?? 0) + 1)
    if (activeReads.has(scope)) api.abandonRead(scope)
    activeReads.delete(scope)
  }
  function clearChoices() {
    ++choiceRevision
    pendingChoice = null
    for (const scope of [
      'meeting-summary-providers',
      'meeting-summary-provider',
      'meeting-summary-models',
    ] as const)
      cancelRead(scope)
  }
  function clearReads() {
    clearChoices()
    cancelRead('meeting-summary-state')
    cancelRead('meeting-summary-reference')
    for (const purpose of selectionPurposes) resetChoice(purpose)
  }
  function readContext(scope: MeetingSummaryRead) {
    const own = revision,
      captured = auth.personalContext.identity,
      generation = (reads.get(scope) ?? 0) + 1
    reads.set(scope, generation)
    activeReads.add(scope)
    return {
      valid: () => attached && current(own, captured) && reads.get(scope) === generation,
      finish: () => {
        if (reads.get(scope) === generation) activeReads.delete(scope)
      },
    }
  }
  function readFailure(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    return e.problem?.code === 'CURSOR_INVALID'
      ? '分页链接已失效，请明确返回首页重新加载。'
      : e.problem?.code === 'NOT_FOUND'
        ? '当前引用详情不存在。保存的引用仍保留，请明确更换或重新读取。'
        : e.kind === 'cancelled'
          ? '读取已取消或等待超时，请在请求结束后明确重试。'
          : '读取失败，尚未获得完整的当前详情。请明确重试。'
  }
  function resetReferences(value: MeetingSummaryState | null) {
    for (const purpose of selectionPurposes) {
      const target = value?.model ?? null
      Object.assign(references[purpose], {
        id: target,
        phase: target ? 'loading' : 'empty',
        value: null,
        message: '',
      })
    }
  }
  function refreshDraftEvidence() {
    if (!editor.open) return
    for (const purpose of selectionPurposes) {
      const observed = references[purpose]
      if (draft[purpose] && draft[purpose] === observed.id)
        draftDetails[purpose] = observed.phase === 'ready' ? observed.value : null
    }
  }
  function observeProvider(target: string, provider: Provider | null, message = '') {
    // A completed exact observation applies to every known pair for this ID.
    // A different protocol needs a new complete Model parse before reuse.
    const replace = (pair: SavedModel | null): SavedModel | null =>
      pair?.provider.id !== target
        ? pair
        : provider && pair.provider.input.protocol === provider.input.protocol
          ? Object.freeze({ model: pair.model, provider })
          : null
    for (const purpose of selectionPurposes) {
      const reference = references[purpose]
      if (reference.value?.provider.id === target) {
        const value = replace(reference.value)
        Object.assign(reference, {
          phase: value ? 'ready' : 'error',
          value,
          message: value ? '' : message || 'Provider 协议已变化，请明确重读完整引用详情。',
        })
      }
      draftDetails[purpose] = replace(draftDetails[purpose])
    }
  }
  function observeModel(pair: SavedModel) {
    for (const purpose of selectionPurposes) {
      if (references[purpose].id === pair.model.id)
        Object.assign(references[purpose], { phase: 'ready', value: pair, message: '' })
      if (editor.open && draft[purpose] === pair.model.id) draftDetails[purpose] = pair
    }
  }
  async function readReferences(value: MeetingSummaryState) {
    const own = revision,
      captured = auth.personalContext.identity
    const ids = [
      ...new Set(
        selectionPurposes
          .map((purpose) => value.model)
          .filter((value): value is string => typeof value === 'string'),
      ),
    ]
    // Only the bounded saved IDs are expanded. Each call yields its own Provider.
    for (const target of ids) {
      if (!attached || !current(own, captured) || selection.value !== value || blocked.value) break
      const op = readContext('meeting-summary-reference')
      try {
        const pair = await api.getSavedModel(target)
        if (!op.valid() || selection.value !== value) return
        observeProvider(pair.provider.id, pair.provider)
        observeModel(pair)
      } catch (error) {
        if (!op.valid() || selection.value !== value) return
        for (const purpose of selectionPurposes)
          if (references[purpose].id === target)
            Object.assign(references[purpose], {
              phase: 'error',
              value: null,
              message: readFailure(error),
            })
      } finally {
        op.finish()
      }
    }
    if (!attached || !current(own, captured) || selection.value !== value) return
    for (const purpose of selectionPurposes)
      if (references[purpose].phase === 'loading')
        Object.assign(references[purpose], {
          phase: 'error',
          message: '读取尚未完成，请在请求结束后明确重读。',
        })
    refreshDraftEvidence()
  }
  async function readCurrent(): Promise<MeetingSummaryState | null> {
    if (!attached || !authorized() || blocked.value) return null
    const op = readContext('meeting-summary-state')
    Object.assign(selection, { phase: 'loading', value: null, message: '' })
    resetReferences(null)
    let value: MeetingSummaryState
    try {
      value = await api.get()
      if (!op.valid()) return null
      Object.assign(selection, { phase: 'ready', value })
      resetReferences(value)
    } catch (error) {
      if (op.valid()) Object.assign(selection, { phase: 'error', message: readFailure(error) })
      return null
    } finally {
      op.finish()
    }
    await readReferences(value)
    return op.valid() && selection.value === value ? value : null
  }
  async function readPage(purpose: SelectionPurpose, kind: PageKind, target: PageTarget) {
    if (
      !attached ||
      !authorized() ||
      blocked.value ||
      activePurpose.value !== purpose ||
      !editor.open
    )
      return false
    const choice = choices[purpose],
      selected = choice.provider.value
    if (kind === 'models' && !selected) return false
    const epoch = choiceRevision,
      op = readContext(
        kind === 'providers' ? 'meeting-summary-providers' : 'meeting-summary-models',
      )
    const page = choice[kind],
      cursor = cursors[purpose][kind]
    cursor.target = target
    Object.assign(page, {
      phase: 'loading',
      rows: Object.freeze([]),
      message: '',
      cursorInvalid: false,
      hasPrevious: false,
      hasNext: false,
    })
    try {
      const query = target.cursor === undefined ? {} : { cursor: target.cursor }
      const result =
        kind === 'providers'
          ? await api.listProviders(query)
          : await api.listModels({
              provider_id: selected!.id,
              protocol: selected!.input.protocol,
              ...query,
            })
      if (
        !op.valid() ||
        epoch !== choiceRevision ||
        activePurpose.value !== purpose ||
        (kind === 'models' && choice.provider.value !== selected)
      )
        return false
      if (kind === 'providers')
        for (const provider of result.items as readonly Provider[])
          observeProvider(provider.id, provider)
      else
        for (const model of result.items as readonly ProviderModel[])
          observeModel(Object.freeze({ model, provider: selected! }))
      cursor.history = [...cursor.history.slice(0, target.index), target.cursor]
      cursor.index = target.index
      cursor.next = result.next_cursor
      Object.assign(page, {
        phase: result.items.length ? 'ready' : 'empty',
        rows: result.items,
        hasPrevious: target.index > 0,
        hasNext: result.next_cursor !== null,
      })
      return true
    } catch (error) {
      if (op.valid() && epoch === choiceRevision && activePurpose.value === purpose)
        Object.assign(page, {
          phase: 'error',
          message: readFailure(error),
          cursorInvalid:
            error instanceof AccountFailure && error.problem?.code === 'CURSOR_INVALID',
        })
      return false
    } finally {
      op.finish()
    }
  }
  async function readProvider(purpose: SelectionPurpose, target: string) {
    if (
      !attached ||
      !authorized() ||
      blocked.value ||
      activePurpose.value !== purpose ||
      !editor.open
    )
      return false
    const choice = choices[purpose],
      epoch = choiceRevision,
      op = readContext('meeting-summary-provider')
    Object.assign(choice.provider, { phase: 'loading', value: null, message: '' })
    try {
      const value = await api.getProvider(target)
      if (
        !op.valid() ||
        epoch !== choiceRevision ||
        activePurpose.value !== purpose ||
        choice.providerID !== target
      )
        return false
      observeProvider(target, value)
      Object.assign(choice.provider, { phase: 'ready', value })
      return true
    } catch (error) {
      if (op.valid() && epoch === choiceRevision && activePurpose.value === purpose) {
        observeProvider(target, null, readFailure(error))
        Object.assign(choice.provider, { phase: 'error', message: readFailure(error) })
      }
      return false
    } finally {
      op.finish()
    }
  }
  function clearBusiness() {
    clearChoices()
    activePurpose.value = null
    base = null
    Object.assign(editor, { open: false, version: '', message: '' })
    Object.assign(draft, emptyDraft())
    for (const purpose of selectionPurposes) draftDetails[purpose] = null
    baseline = JSON.stringify(emptyDraft())
  }
  function discard(reload = false) {
    ++revision
    api.abandon()
    // This revision retires every outstanding observation in the current batch.
    // Settle it here; old continuations must never publish into a later batch.
    const message = '读取已取消，请在请求结束后明确重读。'
    if (selection.phase === 'loading')
      Object.assign(selection, { phase: 'error', value: null, message })
    for (const purpose of selectionPurposes)
      if (references[purpose].phase === 'loading')
        Object.assign(references[purpose], { phase: 'error', value: null, message })
    clearBusiness()
    stopFeedback()
    state.writeMessage = ''
    state.requiresReload = reload
  }
  function invalidate() {
    discard()
    clearReads()
    Object.assign(selection, { phase: 'inactive', value: null, message: '' })
    resetReferences(null)
    state.confirmed = null
    initial = true
  }
  async function confirmLeave() {
    if (!dirty.value) return true
    const own = revision,
      captured = auth.personalContext.identity
    const capturedState = navigationStamp()
    const uncertain = ['submitting', 'uncertain'].includes(progress.value?.phase ?? '')
    if (
      !(await requestConfirmation(
        '放弃会议 Summary 修改？',
        '仅放弃会议 Summary 草稿和请求跟踪；此前保存仍可能生效，四项平台用途保持。',
      )) ||
      !current(own, captured) ||
      capturedState !== navigationStamp()
    )
      return false
    discard(uncertain)
    return true
  }
  async function guarded(work: () => void | Promise<unknown>) {
    if (!(await confirmLeave()) || disposed || !activeRoute || !authorized()) return
    await work()
  }
  function errorMessage(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (progress.value?.phase === 'uncertain')
      return e.problem?.code === 'IDEMPOTENCY_KEY_REUSED'
        ? '原请求标识与输入冲突。请明确放弃后重新读取；不能改写原请求。'
        : '保存结果未确认。请检查原请求，再显式重试原请求；此前保存仍可能生效。'
    if (e.problem?.code === 'VERSION_CONFLICT')
      return '配置版本已变化。草稿已保留，请明确读取最新配置并核对后再保存。'
    if (
      ['NOT_FOUND', 'INVALID_STATE', 'CAPABILITY_UNSUPPORTED', 'RESOURCE_DELETED'].includes(
        e.problem?.code ?? '',
      )
    )
      return '所选 Model 或 Provider 当前不可用。草稿已保留，请重新读取并核对会议 Summary。'
    return '保存未完成，草稿已保留。请重新读取并核对会议 Summary。'
  }
  async function mutate(work: () => Promise<SelectionReceipt>) {
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
      clearBusiness()
      state.requiresReload = false
      feedback.value = 'success'
      state.writeMessage = '会议 Summary 配置已保存。'
      timer = setTimeout(() => {
        if (current(own, captured)) feedback.value = 'idle'
      }, 2400)
      clearReads()
      const value = await readCurrent()
      if (!current(own, captured)) return
      if (!value)
        state.writeMessage =
          '会议 Summary 配置已保存，当前配置读取失败。请仅重新读取，不要重复保存。'
      else if (selectionPurposes.some((purpose) => references[purpose].phase === 'error'))
        state.writeMessage = '会议 Summary 配置已保存，部分引用详情尚未读取成功。请明确重读。'
    } catch (error) {
      if (!current(own, captured)) return
      feedback.value = 'idle'
      state.writeMessage = errorMessage(error)
    }
  }
  async function resume() {
    if (disposed || !attached || !activeRoute || !authorized() || blocked.value || !canResume())
      return
    if (initial) {
      initial = false
      const value = await readCurrent()
      if (value && editor.open && base && value.version !== base.version) {
        state.requiresReload = true
        editor.message = '当前配置版本已变化；原草稿和原请求保持不变，请明确核对。'
      }
      return
    }
    const pending = pendingChoice
    if (!pending) return
    pendingChoice = null
    if (
      pending.revision === choiceRevision &&
      activePurpose.value === pending.purpose &&
      editor.open
    )
      await pending.work()
  }
  function scheduleChoice(purpose: SelectionPurpose, work: () => Promise<unknown>) {
    pendingChoice = { revision: choiceRevision, purpose, work }
    void resume()
  }
  function afterNavigation(to: string, from: string) {
    const isPage = (path: string) => path.split(/[?#]/, 1)[0] === '/system/model-selection'
    if (isPage(from) && !isPage(to)) {
      activeRoute = false
      invalidate()
    } else if (isPage(to)) activeRoute = true
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
      void resume()
    },
  )
  function navigationStamp() {
    return JSON.stringify([
      revision,
      writeGeneration,
      editor.open,
      editor.version,
      draft,
      progress.value,
      state.requiresReload,
    ])
  }
  return {
    auth,
    selection: readonly(selection),
    references: readonly(references),
    choices: readonly(choices),
    draft: readonly(draft),
    draftDetails: readonly(draftDetails),
    editor: readonly(editor),
    state: readonly(state),
    activePurpose: readonly(activePurpose),
    progress,
    feedback,
    checking,
    blocked,
    locked,
    changed,
    dirty,
    canSave,
    attach() {
      attached = true
      activeRoute = true
      void resume()
    },
    detach() {
      attached = false
      // Preserve the completed observation across a checking-only unmount.
      clearReads()
      const message = '读取已取消，请在请求结束后明确重读。'
      if (selection.phase === 'loading')
        Object.assign(selection, { phase: 'error', value: null, message })
      if (references.summary.phase === 'loading')
        Object.assign(references.summary, { phase: 'error', value: null, message })
    },
    afterNavigation,
    confirmLeave,
    resume,
    navigationStamp,
    discard,
    openEditor() {
      if (
        blocked.value ||
        !authorized() ||
        selection.phase !== 'ready' ||
        !selection.value ||
        state.requiresReload ||
        editor.open
      )
        return
      stopFeedback()
      api.abandon()
      state.writeMessage = ''
      state.confirmed = null
      base = selection.value
      Object.assign(draft, { summary: base.model })
      baseline = JSON.stringify(draft)
      for (const purpose of selectionPurposes)
        draftDetails[purpose] =
          references[purpose].phase === 'ready' ? references[purpose].value : null
      Object.assign(editor, { open: true, version: base.version, message: '' })
    },
    closeEditor() {
      return guarded(() => {
        clearBusiness()
        api.abandon()
      })
    },
    refresh() {
      return guarded(async () => {
        if (blocked.value) return
        clearBusiness()
        clearReads()
        state.requiresReload = false
        initial = false
        await readCurrent()
      })
    },
    async retryReference(purpose: SelectionPurpose) {
      if (!attached || !authorized() || blocked.value) return
      const value = selection.value,
        target = value?.model
      if (!target || !value) return
      const op = readContext('meeting-summary-reference')
      for (const key of selectionPurposes)
        if (references[key].id === target)
          Object.assign(references[key], { phase: 'loading', value: null, message: '' })
      refreshDraftEvidence()
      try {
        const pair = await api.getSavedModel(target)
        if (!op.valid() || selection.value !== value) return
        observeProvider(pair.provider.id, pair.provider)
        observeModel(pair)
      } catch (error) {
        if (op.valid() && selection.value === value)
          for (const key of selectionPurposes)
            if (references[key].id === target)
              Object.assign(references[key], {
                phase: 'error',
                value: null,
                message: readFailure(error),
              })
      } finally {
        op.finish()
        if (op.valid()) refreshDraftEvidence()
      }
    },
    choosePurpose(purpose: SelectionPurpose) {
      if (!selectionPurposes.includes(purpose) || !editor.open || locked.value || !authorized())
        return
      clearChoices()
      activePurpose.value = purpose
      resetChoice(purpose)
      scheduleChoice(purpose, () => readPage(purpose, 'providers', { index: 0 }))
    },
    returnToPurposes() {
      clearChoices()
      activePurpose.value = null
    },
    selectProvider(value: Provider) {
      const purpose = activePurpose.value
      if (
        !purpose ||
        !editor.open ||
        locked.value ||
        blocked.value ||
        !choices[purpose].providers.rows.some((row) => row === value)
      )
        return
      clearChoices()
      const choice = choices[purpose]
      choice.providerID = value.id
      choice.mode = 'models'
      resetPage(purpose, 'models')
      const epoch = choiceRevision
      scheduleChoice(purpose, async () => {
        if (await readProvider(purpose, value.id))
          if (epoch === choiceRevision) await readPage(purpose, 'models', { index: 0 })
      })
    },
    backToProviders() {
      const purpose = activePurpose.value
      if (!purpose || locked.value) return
      clearChoices()
      resetChoice(purpose)
      scheduleChoice(purpose, () => readPage(purpose, 'providers', { index: 0 }))
    },
    retryProvider() {
      const purpose = activePurpose.value
      if (!purpose || blocked.value || locked.value) return
      const target = choices[purpose].providerID,
        epoch = choiceRevision
      if (target)
        scheduleChoice(purpose, async () => {
          if (await readProvider(purpose, target))
            if (epoch === choiceRevision) await readPage(purpose, 'models', { index: 0 })
        })
    },
    pageAction(kind: PageKind, action: 'previous' | 'next' | 'retry' | 'refresh') {
      const purpose = activePurpose.value
      if (!purpose || blocked.value || locked.value) return
      const cursor = cursors[purpose][kind],
        page = choices[purpose][kind]
      if (action === 'previous' && page.hasPrevious)
        return readPage(purpose, kind, {
          index: cursor.index - 1,
          cursor: cursor.history[cursor.index - 1],
        })
      if (action === 'next' && cursor.next !== null)
        return readPage(purpose, kind, { index: cursor.index + 1, cursor: cursor.next })
      if (action === 'refresh' || (action === 'retry' && page.cursorInvalid))
        return readPage(purpose, kind, { index: 0 })
      if (action === 'retry') return readPage(purpose, kind, cursor.target)
    },
    candidateReason(value: ProviderModel) {
      const purpose = activePurpose.value,
        provider = purpose ? choices[purpose].provider.value : null
      return purpose && provider
        ? meetingSummaryReason({ model: value, provider })
        : '尚未读取完整 Provider'
    },
    selectModel(value: ProviderModel) {
      const purpose = activePurpose.value,
        choice = purpose ? choices[purpose] : null,
        provider = choice?.provider.value
      if (
        !purpose ||
        !choice ||
        !provider ||
        locked.value ||
        blocked.value ||
        !choice.models.rows.some((row) => row === value)
      )
        return
      const pair = Object.freeze({ model: value, provider })
      if (meetingSummaryReason(pair)) return
      draft[purpose] = value.id
      draftDetails[purpose] = pair
      clearChoices()
      activePurpose.value = null
    },
    save() {
      if (!canSave.value || !base) return Promise.resolve()
      let captured: MeetingSummaryCommand
      try {
        captured = captureMeetingSummaryCommand({
          kind: 'model.selection.update',
          id: base.id,
          expected_version: base.version,
          model: draft.summary,
        } as MeetingSummaryCommand)
      } catch {
        editor.message = '请选择已完整核实的 enabled System chat Model，会议 Summary 不提供清空。'
        return Promise.resolve()
      }
      ++writeGeneration
      return mutate(() => api.start(captured))
    },
    async review() {
      if (
        blocked.value ||
        !editor.open ||
        ['submitting', 'uncertain'].includes(progress.value?.phase ?? '')
      )
        return
      const own = revision,
        captured = auth.personalContext.identity
      if (
        !(await requestConfirmation(
          '读取最新配置并核对？',
          '保留Summary 草稿 ID，读取当前配置版本。未核实的草稿引用需要明确重新选择；核对后才可创建新的保存请求。',
        )) ||
        !current(own, captured)
      )
        return
      api.abandon()
      stopFeedback()
      state.requiresReload = true
      clearReads()
      const value = await readCurrent()
      if (!value || !current(own, captured) || !editor.open) return
      base = value
      editor.version = value.version
      baseline = JSON.stringify({ summary: value.model })
      for (const purpose of selectionPurposes)
        draftDetails[purpose] =
          draft[purpose] === references[purpose].id && references[purpose].phase === 'ready'
            ? references[purpose].value
            : null
      state.requiresReload = false
      state.writeMessage = ''
      editor.message = '已读取当前配置。请核对Summary 草稿，并重新选择尚未确认的引用后保存。'
    },
    async checkOriginal() {
      if (blocked.value) return
      const own = revision,
        captured = auth.personalContext.identity
      checking.value = true
      try {
        await api.checkOriginal()
        if (current(own, captured))
          state.writeMessage = '原请求历史已读取，这不确认本次输入已接受。请显式重试原请求。'
      } catch {
        if (current(own, captured))
          state.writeMessage = '原请求历史未能读取；若当前会话已确认，仍可显式重试原请求。'
      } finally {
        checking.value = false
        void resume()
      }
    },
    retryOriginal() {
      return !blocked.value && progress.value?.canRetryOriginal
        ? mutate(() => api.retryOriginal())
        : Promise.resolve()
    },
    async abandonOperation() {
      const own = revision,
        captured = auth.personalContext.identity
      if ((await requestConfirmation('放弃本次请求的追踪？')) && current(own, captured)) {
        discard(true)
        state.writeMessage = '已放弃本地追踪。此前保存仍可能生效，请重新读取当前配置。'
      }
    },
    dispose() {
      if (disposed) return
      disposed = true
      stopIdentity()
      invalidate()
    },
  }
}
export type MeetingSummarySettingsController = ReturnType<typeof createMeetingSummarySettings>
