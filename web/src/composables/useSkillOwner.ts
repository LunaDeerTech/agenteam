import { computed, readonly, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import { captureSkillID, type SkillMetadata } from '../api/skill-owner'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export type SkillLocation = Readonly<{ projectPath: string; skillID: string | null }>
type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Phase = 'waiting' | 'loading' | 'ready' | 'error' | 'unavailable'
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  sameIdentity(a.identity, b.identity) &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration

export function useSkillOwner(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  location: Readonly<Ref<SkillLocation>> = computed(() => ({
    projectPath: workspace.paths.value.home,
    skillID: null,
  })),
) {
  const state = shallowReactive({
    phase: 'waiting' as Phase,
    items: Object.freeze([]) as readonly SkillMetadata[],
    detail: null as SkillMetadata | null,
    message: '',
  })
  const identity = auth.personalContext.identity
  let scope: Context | null = null,
    route: SkillLocation | null = null,
    generation = 0,
    disposed = false
  const queue = shallowReactive<{ active: number | null; pending: SkillLocation | null }>({
    active: null,
    pending: null,
  })
  const authorized = () =>
    !disposed &&
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    sameIdentity(identity, auth.personalContext.identity) &&
    auth.state.user?.id === identity?.userID &&
    auth.state.session?.id === identity?.sessionID
  const live = () =>
    authorized() &&
    sameContext(scope, workspace.currentReadContext.value) &&
    location.value.projectPath !== '' &&
    location.value.projectPath === workspace.paths.value.home
  const visible = computed(live)
  const reading = computed(() => queue.active !== null)
  const busy = computed(() => auth.state.busy || reading.value || queue.pending !== null)
  const blocked = computed(() => !live() || busy.value)
  function clear() {
    state.items = Object.freeze([])
    state.detail = null
    state.message = ''
  }
  function retire() {
    ++generation
    queue.pending = null
    const previous = queue.active
    queue.active = null
    if (previous !== null) auth.skills.abandon()
  }
  function schedule(next: SkillLocation) {
    retire()
    clear()
    state.phase = 'waiting'
    queue.pending = next
    pump()
  }
  function pump() {
    if (!live() || !scope || queue.active !== null || !queue.pending || auth.state.busy) return
    const task = queue.pending,
      captured = scope,
      serial = ++generation
    queue.pending = null
    queue.active = serial
    state.phase = 'loading'
    void execute(task, captured, serial)
  }
  async function execute(task: SkillLocation, captured: Context, serial: number) {
    const current = () =>
      live() &&
      serial === generation &&
      sameContext(captured, scope) &&
      task.projectPath === location.value.projectPath &&
      task.skillID === location.value.skillID
    try {
      if (task.skillID === null) {
        const result = await auth.skills.list(captured.projectID)
        if (!current()) return
        state.items = result.items
      } else {
        const result = await auth.skills.get(captured.projectID, captureSkillID(task.skillID))
        if (!current()) return
        state.detail = result
      }
      state.phase = 'ready'
    } catch (error) {
      if (!current()) return
      clear()
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const unavailable = [401, 403, 404].includes(e.problem?.status ?? 0)
      state.phase = unavailable ? 'unavailable' : 'error'
      state.message = unavailable
        ? '当前技能不存在或不可访问。'
        : e.problem?.status === 409
          ? '项目技能库尚未就绪，请稍后重新读取。'
          : e.kind === 'cancelled'
            ? '读取已停止，请明确重读。'
            : e.kind === 'invalid-response'
              ? '未取得有效的完整响应，请重新读取。'
              : '本次读取失败，请重新读取。'
    } finally {
      if (queue.active === serial) queue.active = null
      pump()
    }
  }
  const stop = watch(
    () =>
      [
        workspace.currentReadContext.value,
        workspace.paths.value.home,
        location.value,
        auth.state.phase,
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.busy,
      ] as const,
    ([context, home, next]) => {
      if (disposed) return
      if (!authorized() || !context || !home || next.projectPath !== home) {
        if (scope || queue.pending || queue.active !== null) {
          scope = null
          route = null
          retire()
          clear()
          state.phase = 'waiting'
        }
        return
      }
      if (
        !sameContext(scope, context) ||
        route?.projectPath !== next.projectPath ||
        route?.skillID !== next.skillID
      ) {
        scope = context
        route = next
        schedule(next)
      } else pump()
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    visible,
    reading,
    busy,
    blocked,
    refresh() {
      if (!blocked.value) schedule(location.value)
    },
    cancel() {
      retire()
      clear()
      state.phase = 'error'
      state.message = '读取已停止，请在原请求结束后明确重读。'
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      retire()
      clear()
      scope = null
    },
  }
}
export type SkillOwnerController = ReturnType<typeof useSkillOwner>
