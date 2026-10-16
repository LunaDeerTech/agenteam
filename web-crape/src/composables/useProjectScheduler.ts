import { computed, inject, reactive, ref, shallowRef, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProjectSchedulerUpdate,
  type ProjectSchedulerConfiguration,
  type ProjectSchedulerDraft,
  type ProjectSchedulerLookup,
  type ProjectSchedulerReceipt,
  type ProjectSchedulerUpdate,
} from '../api/project-scheduler'
import { projectRoute } from '../router/auth'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  a === b ||
  (!!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch)
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration
const failure = (e: unknown) => (e instanceof AccountFailure ? e : new AccountFailure('transport'))

// App owns drafts across same-Session checks; Session alone holds original keys,
// CSRF and immutable request bytes. Navigation/identity retirement clears both.
export function createProjectScheduler(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  const route = ref(''),
    bound = shallowRef<Context | null>(null)
  const current = shallowRef<ProjectSchedulerConfiguration | null>(null)
  const draft = reactive<ProjectSchedulerDraft>({
    expected_version: '',
    enabled: false,
    unlimited: true,
    limit: '',
  })
  const state = reactive({
    phase: 'waiting' as 'waiting' | 'loading' | 'ready' | 'error',
    reading: false,
    writing: false,
    conflict: false,
    needsRead: false,
    denied: false,
    message: '',
  })
  const confirmation = reactive({ open: false, title: '', message: '', label: '' })
  let answer: ((yes: boolean) => void) | null = null
  let baseline: ProjectSchedulerUpdate | null = null
  let identity = auth.personalContext.identity,
    revision = 0,
    disposed = false,
    requested = false
  const context = computed(() => {
    const address = projectRoute(route.value),
      c = workspace.currentReadContext.value
    return !disposed &&
      address?.suffix === '/settings/scheduler' &&
      `/${address.username}/${address.project_name}` === workspace.paths.value.home &&
      c &&
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      sameIdentity(c.identity, auth.personalContext.identity)
      ? c
      : null
  })
  const visible = computed(() => !!context.value && sameContext(bound.value, context.value))
  const readOnly = computed(() => workspace.detail.project?.lifecycle !== 'active')
  const progress = computed(() => {
    const p = auth.projectScheduler.progress
    return p && p.projectID === bound.value?.projectID ? p : null
  })
  const pending = computed(() => ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''))
  const busy = computed(
    () => auth.state.busy || state.reading || state.writing || confirmation.open,
  )
  const canSave = computed(
    () =>
      visible.value &&
      !!draft.expected_version &&
      !busy.value &&
      !readOnly.value &&
      !pending.value &&
      !state.needsRead &&
      !state.conflict &&
      !state.denied,
  )
  const dirty = computed(
    () =>
      !!baseline &&
      (draft.enabled !== baseline.scheduler_enabled ||
        (draft.unlimited ? null : draft.limit) !== baseline.scheduler_max_concurrency),
  )
  const canAdopt = computed(
    () =>
      visible.value &&
      state.conflict &&
      !!current.value &&
      !busy.value &&
      !readOnly.value &&
      !pending.value,
  )
  function finishConfirmation(yes: boolean) {
    confirmation.open = false
    const resolve = answer
    answer = null
    resolve?.(yes)
  }
  function ask(title: string, message: string, label: string) {
    if (answer) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message, label })
    return new Promise<boolean>((resolve) => {
      answer = resolve
    })
  }
  function clearDraft() {
    Object.assign(draft, { expected_version: '', enabled: false, unlimited: true, limit: '' })
    baseline = null
  }
  function retireRead() {
    ++revision
    state.reading = false
    auth.projectScheduler.abandonRead()
  }
  function clear() {
    retireRead()
    requested = false
    state.writing = false
    current.value = null
    clearDraft()
    Object.assign(state, {
      phase: 'waiting',
      conflict: false,
      needsRead: false,
      denied: false,
      message: '',
    })
    finishConfirmation(false)
    auth.projectScheduler.abandon()
  }
  function take(value: ProjectSchedulerConfiguration) {
    baseline = Object.freeze({
      expected_version: value.version,
      scheduler_enabled: value.scheduler_enabled,
      scheduler_max_concurrency: value.scheduler_max_concurrency,
    })
    Object.assign(draft, {
      expected_version: value.version,
      enabled: value.scheduler_enabled,
      unlimited: value.scheduler_max_concurrency === null,
      limit: value.scheduler_max_concurrency ?? '',
    })
  }
  const live = (c: Context, serial: number) =>
    visible.value && serial === revision && sameContext(c, context.value)
  function denied(e: AccountFailure) {
    if (![401, 403, 404].includes(e.problem?.status ?? 0)) return false
    retireRead()
    requested = false
    state.writing = false
    current.value = null
    clearDraft()
    auth.projectScheduler.abandon()
    Object.assign(state, {
      phase: 'error',
      conflict: false,
      needsRead: false,
      denied: true,
      message: '当前项目或调度配置不可访问，请重新确认访问权限。',
    })
    return true
  }
  async function readCurrent() {
    const c = context.value
    if (!c || !visible.value || busy.value || pending.value || state.denied) return
    const serial = ++revision,
      confirmed = progress.value?.phase === 'confirmed' ? progress.value.receipt : null
    state.reading = true
    state.phase = 'loading'
    current.value = null
    try {
      const value = await auth.projectScheduler.get(c.projectID)
      if (!live(c, serial)) return
      if (confirmed && BigInt(value.version) < BigInt(confirmed.version))
        throw new AccountFailure('invalid-response')
      current.value = value
      state.phase = 'ready'
      if (state.needsRead) {
        state.needsRead = false
        state.conflict = false
        take(value)
        auth.projectScheduler.finishConfirmed()
        state.message = '操作已确认，并已读取当前调度配置。'
      } else if (!draft.expected_version) {
        take(value)
        state.message = ''
      } else if (state.conflict || value.version !== draft.expected_version) {
        state.conflict = true
        state.message = '已读取当前配置。原输入与版本仍保留，请核对后明确采用当前版本。'
      } else state.message = '已读取当前调度配置，未改变未保存的输入。'
    } catch (error) {
      if (!live(c, serial)) return
      if (!denied(failure(error))) {
        state.phase = 'error'
        state.message = state.needsRead
          ? '操作已确认，但当前配置读取未完成。请重新读取，不要再次提交。'
          : '未能读取当前调度配置，请重新读取。'
      }
    } finally {
      if (serial === revision) state.reading = false
      pump()
    }
  }
  function accept(
    value: ProjectSchedulerReceipt | ProjectSchedulerLookup,
    c: Context,
    serial: number,
  ) {
    if (!live(c, serial)) return
    if ('state' in value && value.state !== 'committed') {
      state.message = '原操作尚未确认，请稍后再次查证。'
      return
    }
    state.needsRead = true
    current.value = null
    state.phase = 'waiting'
    state.conflict = false
    state.message = '原操作已确认，当前配置仍需重新读取。'
  }
  async function execute(action: () => Promise<ProjectSchedulerReceipt | ProjectSchedulerLookup>) {
    const c = context.value
    if (!c) return
    const serial = ++revision
    state.writing = true
    state.message = ''
    current.value = null
    try {
      accept(await action(), c, serial)
    } catch (error) {
      if (live(c, serial)) {
        const e = failure(error)
        if (!denied(e)) {
          if (progress.value?.phase === 'rejected') {
            state.conflict = e.problem?.code === 'VERSION_CONFLICT'
            auth.projectScheduler.editRejected()
            state.message = state.conflict
              ? '配置已变化，原输入和版本已保留。请先重新读取当前配置。'
              : '本次修改未保存，原输入已保留。请检查后重新提交。'
          } else state.message = '尚未确认保存结果。请查证原操作，不要重复提交。'
        }
      }
    } finally {
      if (serial === revision) state.writing = false
    }
    if (live(c, serial) && state.needsRead) await readCurrent()
  }
  async function submit(value: ProjectSchedulerUpdate) {
    if (!canSave.value || !context.value) return
    const captured = captureProjectSchedulerUpdate(value)
    const original = captureProjectSchedulerUpdate({
      expected_version: draft.expected_version,
      scheduler_enabled: draft.enabled,
      scheduler_max_concurrency: draft.unlimited ? null : draft.limit,
    })
    if (JSON.stringify(captured) !== JSON.stringify(original)) return
    const project = context.value.projectID
    await execute(() => auth.projectScheduler.startUpdate(project, captured))
  }
  async function lookupOriginal() {
    if (
      !visible.value ||
      busy.value ||
      progress.value?.phase !== 'uncertain' ||
      !progress.value.contextValid
    )
      return
    await execute(() => auth.projectScheduler.checkOriginal())
  }
  async function replayOriginal() {
    if (!visible.value || busy.value || readOnly.value || !progress.value?.canRetryOriginal) return
    await execute(() => auth.projectScheduler.retryOriginal())
  }
  function adoptCurrentVersion() {
    if (!canAdopt.value || !current.value) return
    draft.expected_version = current.value.version
    baseline = Object.freeze({
      expected_version: current.value.version,
      scheduler_enabled: current.value.scheduler_enabled,
      scheduler_max_concurrency: current.value.scheduler_max_concurrency,
    })
    state.conflict = false
    state.message = '已采用当前版本，保留你的输入。再次保存会提交这份配置。'
  }
  async function cancelEdits() {
    if (busy.value || pending.value || state.needsRead || !visible.value) return
    if (dirty.value && !(await ask('取消调度修改？', '将丢弃尚未保存的配置输入。', '取消修改')))
      return
    if (!visible.value) return
    if (current.value) {
      take(current.value)
      state.conflict = false
      state.message = ''
    } else {
      clearDraft()
      state.conflict = false
      await readCurrent()
    }
  }
  async function abandonOriginal() {
    if (!visible.value || busy.value || progress.value?.phase !== 'uncertain') return
    if (
      !(await ask(
        '放弃本地追踪？',
        '只清除本地输入和追踪，不会撤销服务器上可能已经提交的修改。',
        '放弃追踪',
      ))
    )
      return
    clear()
    bound.value = context.value
  }
  function pump() {
    if (requested && visible.value && !busy.value && !pending.value && !state.denied) {
      requested = false
      void readCurrent()
    }
  }
  const stop = watch(
    () =>
      [
        context.value,
        auth.personalContext.identity,
        auth.state.busy,
        auth.state.phase,
        workspace.detail.phase,
      ] as const,
    ([next, nextIdentity]) => {
      if (disposed) return
      if (!sameIdentity(identity, nextIdentity)) {
        clear()
        bound.value = null
        identity = nextIdentity
      }
      const previous = bound.value
      if (!next) {
        if (previous) {
          // Checking or a same-Project revalidation hides the page but never
          // discards its original draft/Unknown key. Actual denial clears it.
          retireRead()
          current.value = null
          state.phase = 'waiting'
          if (workspace.detail.phase === 'unavailable') {
            clear()
            bound.value = null
          }
        }
        return
      }
      if (!sameContext(previous, next)) {
        const retained =
          previous &&
          previous.projectID === next.projectID &&
          sameIdentity(previous.identity, next.identity)
        if (!retained) clear()
        bound.value = next
        requested = !pending.value && !state.conflict && !state.denied
      }
      pump()
    },
    { immediate: true, flush: 'sync' },
  )
  async function confirmLeave(target?: string) {
    if (target === route.value) return true
    if (
      (dirty.value || pending.value || state.needsRead) &&
      !(await ask(
        '离开调度配置？',
        '未确认的修改不会自动重发。离开会清除本地输入和追踪，不会撤销服务器修改。',
        '离开并清除',
      ))
    )
      return false
    clear()
    return true
  }
  return {
    state,
    draft,
    current,
    visible,
    readOnly,
    progress,
    pending,
    busy,
    canSave,
    canAdopt,
    confirmation,
    submit,
    readCurrent,
    cancelEdits,
    lookupOriginal,
    replayOriginal,
    adoptCurrentVersion,
    abandonOriginal,
    confirmLeave,
    finishConfirmation,
    afterNavigation(to: string) {
      if (to !== route.value) {
        clear()
        bound.value = null
        route.value = to
      }
    },
    dispose() {
      disposed = true
      stop()
      clear()
      bound.value = null
    },
  }
}
export type ProjectScheduler = ReturnType<typeof createProjectScheduler>
export const projectSchedulerKey: InjectionKey<ProjectScheduler> = Symbol('project-scheduler')
export function useProjectScheduler() {
  const owner = inject(projectSchedulerKey)
  if (!owner) throw new Error('Project Scheduler owner is unavailable')
  return owner
}
