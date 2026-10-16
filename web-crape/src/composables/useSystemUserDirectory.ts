import { computed, readonly, shallowReactive, watch } from 'vue'
import type { SystemUser } from '../api/system-account'
import { AccountFailure } from '../api/client'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'

type Phase = 'waiting' | 'loading' | 'ready' | 'empty' | 'error' | 'forbidden' | 'inactive'
type Target = Readonly<{ index: number; cursor?: string }>
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const emptyRows: readonly SystemUser[] = Object.freeze([])

// Page-owned data only. Cookie authority and actual request ownership remain in
// useSession; neither a cursor nor a directory row is an account identity.
export function useSystemUserDirectory(auth: SessionController = useSession()) {
  const state = shallowReactive({
    phase: 'waiting' as Phase,
    rows: emptyRows,
    message: '',
    cursorInvalid: false,
    hasPrevious: false,
    hasNext: false,
  })
  let identity: PersonalIdentity | null = null
  let generation = 0,
    active: number | null = null,
    disposed = false,
    initial = true
  let history: (string | undefined)[] = [],
    index = 0,
    nextCursor: string | undefined
  let target: Target | null = null
  const authorized = () =>
    auth.state.phase === 'authenticated' &&
    auth.personalContext.phase === 'current' &&
    !!auth.personalContext.identity &&
    auth.state.user?.role === 'admin' &&
    !auth.system.denied
  const blocked = computed(() => auth.state.busy || state.phase === 'loading')
  function clear() {
    ++generation
    if (active !== null) auth.system.abandon()
    active = null
    history = []
    index = 0
    nextCursor = undefined
    target = null
    state.rows = emptyRows
    state.message = ''
    state.cursorInvalid = false
    state.hasPrevious = false
    state.hasNext = false
  }
  async function read(next: Target) {
    if (disposed || blocked.value || !authorized()) return
    const captured = auth.personalContext.identity
    const own = ++generation
    const current = () =>
      !disposed &&
      own === generation &&
      authorized() &&
      same(captured, auth.personalContext.identity)
    active = own
    target = next
    state.rows = emptyRows
    state.phase = 'loading'
    state.message = ''
    state.cursorInvalid = false
    state.hasPrevious = false
    state.hasNext = false
    try {
      const page = await auth.system.listUsers(
        next.cursor === undefined ? {} : { cursor: next.cursor },
      )
      if (!current()) return
      history = [...history.slice(0, next.index), next.cursor]
      index = next.index
      nextCursor = page.next_cursor
      state.rows = page.items
      state.hasPrevious = index > 0
      state.hasNext = nextCursor !== undefined
      state.phase = page.items.length ? 'ready' : 'empty'
    } catch (error) {
      if (!current()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      state.phase = 'error'
      state.cursorInvalid = e.problem?.code === 'CURSOR_INVALID'
      state.message = state.cursorInvalid
        ? '分页链接已失效，请返回首页重新加载。'
        : e.kind === 'invalid-response'
          ? '用户目录响应无法确认，请重试。'
          : e.kind === 'cancelled'
            ? '本次读取已取消或等待超时，请在请求结束后重试。'
            : '暂时无法读取用户目录，请重试。'
    } finally {
      if (active === own) active = null
    }
  }
  const stop = watch(
    () =>
      [
        auth.state.phase,
        auth.personalContext.identity,
        auth.state.user?.role,
        auth.system.denied,
        auth.state.busy,
      ] as const,
    () => {
      if (!authorized()) {
        clear()
        initial = true
        identity = auth.personalContext.identity
        state.phase = auth.state.phase === 'authenticated' ? 'forbidden' : 'inactive'
        return
      }
      if (!same(identity, auth.personalContext.identity)) {
        clear()
        identity = auth.personalContext.identity
        initial = true
      }
      // Only an initial request can wait for the existing owner. Errors and
      // explicit paging never queue or automatically retry.
      if (initial && !auth.state.busy) {
        initial = false
        void read({ index: 0 })
      }
    },
    { immediate: true, flush: 'sync' },
  )
  return {
    state: readonly(state),
    blocked,
    refresh() {
      if (blocked.value) return Promise.resolve()
      history = []
      return read({ index: 0 })
    },
    previous() {
      return state.hasPrevious
        ? read({ index: index - 1, cursor: history[index - 1] })
        : Promise.resolve()
    },
    next() {
      return state.hasNext && nextCursor !== undefined
        ? read({ index: index + 1, cursor: nextCursor })
        : Promise.resolve()
    },
    retry() {
      return state.phase === 'error' && target && !state.cursorInvalid
        ? read(target)
        : Promise.resolve()
    },
    dispose() {
      if (disposed) return
      disposed = true
      stop()
      clear()
      identity = null
      state.phase = 'inactive'
    },
  }
}
