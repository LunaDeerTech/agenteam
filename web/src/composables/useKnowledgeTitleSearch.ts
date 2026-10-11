import { computed, readonly, shallowReactive, shallowRef, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import { compareKnowledgeDocuments } from '../api/knowledge-owner'
import {
  captureKnowledgeTitleQuery,
  type KnowledgeTitleHit,
  type KnowledgeTitleQuery,
  type KnowledgeTitleSearchPort,
} from '../api/knowledge-title-search'
import type { ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
export type KnowledgeTitlePhase = 'idle' | 'loading' | 'ready' | 'empty' | 'error' | 'unavailable'
const sameContext = (a: Context | null, b: Context | null) =>
  !!a &&
  !!b &&
  a.projectID === b.projectID &&
  a.generation === b.generation &&
  a.readGeneration === b.readGeneration &&
  a.identity.userID === b.identity.userID &&
  a.identity.sessionID === b.identity.sessionID &&
  a.identity.epoch === b.identity.epoch

// This controller owns observations only. Its port must use the existing
// Session knowledgeRead owner; cancel never releases the underlying fetch tail.
export function useKnowledgeTitleSearch(
  port: KnowledgeTitleSearchPort,
  context: Readonly<Ref<Context | null>>,
  blocked: Readonly<Ref<boolean>>,
  pageSize = 50,
) {
  const state = shallowReactive({
    draft: '',
    query: null as KnowledgeTitleQuery | null,
    items: Object.freeze([]) as readonly KnowledgeTitleHit[],
    nextCursor: undefined as string | undefined,
    phase: 'idle' as KnowledgeTitlePhase,
    message: '',
    field: '',
  })
  const disposed = shallowRef(false)
  let revision = 0
  const active = shallowRef<{ revision: number; context: Context } | null>(null)
  const cursors = new Set<string>()
  const visible = computed(() => !disposed.value && context.value !== null)
  const busy = computed(() => active.value !== null)
  const disabled = computed(() => !visible.value || blocked.value || busy.value)
  const canMore = computed(() => !disabled.value && !!state.query && !!state.nextCursor)
  function retire() {
    ++revision
    const original = active.value
    active.value = null
    if (original) port.abandon()
  }
  function clear() {
    retire()
    state.draft = ''
    state.query = null
    state.items = Object.freeze([])
    state.nextCursor = undefined
    state.phase = 'idle'
    state.message = ''
    state.field = ''
    cursors.clear()
  }
  function setDraft(value: string) {
    if (!visible.value || value === state.draft) return
    clear()
    state.draft = value
  }
  async function read(more: boolean) {
    if (disabled.value || !context.value || (more && !canMore.value)) return
    let query: KnowledgeTitleQuery
    try {
      query = captureKnowledgeTitleQuery(
        more
          ? { ...state.query!, cursor: state.nextCursor }
          : { title_query: state.draft, limit: pageSize },
      )
    } catch {
      state.field = '请输入有效标题文字；保留原空格，不接受控制字符或过长查询。'
      return
    }
    if (!more) {
      state.items = Object.freeze([])
      state.nextCursor = undefined
      cursors.clear()
    }
    state.query = query
    state.field = ''
    state.message = ''
    state.phase = 'loading'
    const original = { revision: ++revision, context: context.value }
    active.value = original
    const current = () =>
      !disposed.value &&
      active.value === original &&
      revision === original.revision &&
      sameContext(original.context, context.value)
    try {
      const page = await port.searchTitles(original.context.projectID, query)
      if (!current()) return
      if (page.next_cursor && (page.next_cursor === query.cursor || cursors.has(page.next_cursor)))
        throw new AccountFailure('invalid-response')
      const merged = new Map(state.items.map((item) => [item.document.id, item]))
      for (const item of page.items) merged.set(item.document.id, item)
      state.items = Object.freeze(
        [...merged.values()].sort((a, b) => compareKnowledgeDocuments(a.document, b.document)),
      )
      state.nextCursor = page.next_cursor
      if (query.cursor) cursors.add(query.cursor)
      state.phase = state.items.length ? 'ready' : 'empty'
    } catch (error) {
      if (!current()) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      const denied = [401, 403, 404].includes(e.problem?.status ?? 0)
      if (denied) {
        state.draft = ''
        state.query = null
        state.items = Object.freeze([])
        state.nextCursor = undefined
        cursors.clear()
      }
      state.phase = denied ? 'unavailable' : 'error'
      state.message = denied
        ? '当前项目不存在或不可访问。'
        : e.kind === 'cancelled'
          ? '查找已停止，请明确重新查找。'
          : '未取得完整的查找结果，请重新查找。'
    } finally {
      if (active.value === original) active.value = null
    }
  }
  const stop = watch(
    context,
    (next, previous) => {
      if (!sameContext(next, previous)) clear()
    },
    { flush: 'sync' },
  )
  return {
    state: readonly(state),
    visible,
    busy,
    disabled,
    canMore,
    setDraft,
    search: () => read(false),
    more: () => read(true),
    clear,
    selection(id: string) {
      if (disabled.value || !state.items.some((item) => item.document.id === id)) return null
      const selectedRevision = revision,
        selectedContext = context.value
      return () =>
        !disabled.value &&
        selectedRevision === revision &&
        sameContext(selectedContext, context.value) &&
        state.items.some((item) => item.document.id === id)
    },
    cancel() {
      if (!active.value) return
      retire()
      state.phase = 'error'
      state.message = '查找已停止，请在原读取结束后重新查找。'
    },
    dispose() {
      disposed.value = true
      stop()
      clear()
    },
  }
}
export type KnowledgeTitleSearch = ReturnType<typeof useKnowledgeTitleSearch>
