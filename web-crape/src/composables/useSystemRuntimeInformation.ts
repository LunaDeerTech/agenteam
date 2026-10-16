import { computed, readonly, shallowReactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import type { SystemRuntimeInformation } from '../api/system-runtime-information'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

export type RuntimeInformationPhase =
  'waiting' | 'loading' | 'ready' | 'error' | 'forbidden' | 'inactive'
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch

// A page instance owns only its observation. The shared Session controller keeps
// actual Cookie ownership until transport and cancellation have both finished.
export function useSystemRuntimeInformation(auth: SessionController = useSession()) {
  const state = shallowReactive({
    phase: 'waiting' as RuntimeInformationPhase,
    observation: null as SystemRuntimeInformation | null,
    message: '',
  })
  const identity = auth.personalContext.identity
  let generation = 0,
    initial = true,
    disposed = false,
    retired = false,
    active: number | null = null
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied &&
    same(identity, auth.personalContext.identity)
  const live = () => !disposed && !retired && authorized()
  const blocked = computed(() => !live() || auth.state.busy || state.phase === 'loading')
  const reading = computed(() => state.phase === 'waiting' || state.phase === 'loading')
  function retireRead() {
    ++generation
    const wasActive = active !== null
    active = null
    if (wasActive) auth.system.runtimeInformation.abandon()
  }
  function invalidate() {
    retired = true
    initial = false
    retireRead()
    state.observation = null
    state.message = ''
    state.phase =
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      same(identity, auth.personalContext.identity) &&
      (auth.state.user?.role === 'user' || auth.system.denied)
        ? 'forbidden'
        : 'inactive'
  }
  async function refresh() {
    if (blocked.value) return
    initial = false
    const own = ++generation,
      capturedIdentity = auth.personalContext.identity
    const current = () =>
      live() && generation === own && same(capturedIdentity, auth.personalContext.identity)
    active = own
    state.observation = null
    state.message = ''
    state.phase = 'loading'
    try {
      const value = await auth.system.runtimeInformation.get()
      if (!current()) return
      state.observation = value
      state.phase = 'ready'
    } catch (error) {
      if (!current()) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      state.observation = null
      state.message =
        failure.kind === 'cancelled'
          ? '本次读取已取消或等待超时，请在请求结束后重新读取。'
          : failure.kind === 'invalid-response'
            ? '运行信息响应无法确认，请重新读取。'
            : '暂时无法读取运行信息，请重新读取。'
      state.phase = 'error'
    } finally {
      if (active === own) active = null
    }
  }
  const stop = watch(
    () =>
      [
        auth.state.phase,
        auth.personalContext.phase,
        auth.personalContext.identity,
        auth.state.user?.role,
        auth.system.denied,
        auth.state.busy,
      ] as const,
    () => {
      if (disposed || retired) return
      if (!authorized()) {
        invalidate()
        return
      }
      if (initial && !auth.state.busy) {
        initial = false
        void refresh()
      }
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    blocked,
    reading,
    isCurrent: live,
    refresh,
    cancel() {
      if (!live() || !reading.value) return
      initial = false
      retireRead()
      state.observation = null
      state.message = '本次读取已取消，请在请求结束后重新读取。'
      state.phase = 'error'
    },
    leave() {
      if (!live()) return
      initial = false
      retireRead()
      state.observation = null
      state.message = '本页读取已停止，可显式重新读取。'
      state.phase = 'error'
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      invalidate()
      state.phase = 'inactive'
    },
  }
}
export type SystemRuntimeInformationController = ReturnType<typeof useSystemRuntimeInformation>
