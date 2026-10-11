import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import { captureProjectVariableCreate, captureProjectVariableUpdate, type ProjectVariable, type ProjectVariableSummary, type ProjectVariableUpdate, type ProjectVariableReceipt, type ProjectVariableLookup } from '../api/project-variables'
import { projectRoute } from '../router/auth'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

type Context = NonNullable<ProjectWorkspace['currentReadContext']['value']>
type Fields = { name: string; description: string; value: string }
const blank = (): Fields => ({ name: '', description: '', value: '' })
const sameIdentity = (a: PersonalIdentity | null, b: PersonalIdentity | null) => !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const sameContext = (a: Context | null, b: Context | null) => !!a && !!b && a.projectID === b.projectID && sameIdentity(a.identity, b.identity) && a.generation === b.generation && a.readGeneration === b.readGeneration
const asFailure = (error: unknown) => error instanceof AccountFailure ? error : new AccountFailure('transport')
function explanation(error: AccountFailure) {
  if (error.problem?.code === 'VERSION_CONFLICT') return '版本已变化，输入已保留。请重新读取当前变量，核对后明确采用当前版本。'
  if (['CURSOR_INVALID', 'CURSOR_STALE'].includes(error.problem?.code ?? '')) return '目录分页已失效，请从第一页重新读取。'
  if ([401, 403, 404].includes(error.problem?.status ?? 0)) return '当前变量不存在或不可访问，页面内容已清除。'
  if (error.problem?.code === 'PROJECT_NOT_ACTIVE') return '项目当前不允许修改普通变量。'
  if (error.problem?.code === 'IDEMPOTENCY_KEY_REUSED') return '原请求标识发生冲突，请查询原操作；不会自动改用新标识。'
  return error.kind === 'invalid-input' ? '请检查名称和内容格式；输入不会自动修剪。' : '未取得完整结果，请明确重读或查询原操作。'
}
export function createProjectVariables(auth: SessionController = useSession(), workspace: ProjectWorkspace = useProjectWorkspace()) {
  const route = ref(''), disposed = ref(false), denied = ref(false)
  const context = computed(() => {
    const address = projectRoute(route.value), current = workspace.currentReadContext.value
    return !disposed.value && auth.state.user?.id === auth.personalContext.identity?.userID && auth.state.session?.id === auth.personalContext.identity?.sessionID && address?.suffix === '/settings/variables' && `/${address.username}/${address.project_name}` === workspace.paths.value.home && current && workspace.detail.project?.id === current.projectID && auth.state.phase === 'authenticated' && auth.personalContext.phase === 'current' && sameIdentity(current.identity, auth.personalContext.identity) ? current : null
  })
  const visible = computed(() => !!context.value && ['active', 'archiving', 'archived'].includes(workspace.detail.project?.lifecycle ?? ''))
  const canMutate = computed(() => visible.value && !denied.value && workspace.detail.project?.lifecycle === 'active')
  const page = shallowReactive({ phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error', items: [] as readonly ProjectVariableSummary[], nextCursor: undefined as string | undefined, message: '', stale: false })
  const detail = shallowReactive({ phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error', variable: null as ProjectVariable | null, message: '' })
  const editor = reactive({ open: false, mode: 'create' as 'create' | 'update', version: '', targetID: '', message: '', conflict: false })
  const draft = reactive(blank()), touched = reactive(new Set<keyof Fields>())
  const selected = ref(''), message = ref(''), reading = ref(false), writing = ref(false), needsRead = ref(false)
  const progress = computed(() => auth.projectVariables.progress?.projectID === context.value?.projectID ? auth.projectVariables.progress : null)
  const pending = computed(() => progress.value?.phase === 'uncertain' || progress.value?.phase === 'submitting')
  const busy = computed(() => auth.state.busy || reading.value || writing.value)
  const confirmation = reactive({ open: false, title: '', message: '', label: '' })
  let confirmResult: ((ok: boolean) => void) | null = null
  let revision = 0, bound: Context | null = null, identity = auth.personalContext.identity
  let requested = false
  let committed: { id: string; version: string; context: Context } | null = null
  let checking: Context | null = null
  const dirty = computed(() => editor.open && (touched.size > 0 || Object.values(draft).some(Boolean)) || !!pending.value || needsRead.value)
  const canSave = computed(() => {
    if (!canMutate.value || busy.value || confirmation.open || !editor.open || editor.conflict || !!progress.value) return false
    try {
      if (editor.mode === 'create') captureProjectVariableCreate({ variable_id: '01900000-0000-7000-8000-000000000001', ...draft })
      else captureProjectVariableUpdate(patch())
      return true
    } catch { return false }
  })
  const canAdoptVersion = computed(() => canMutate.value && !busy.value && !pending.value && editor.open && editor.conflict && detail.phase === 'ready' && detail.variable?.id === editor.targetID)
  function patch(): ProjectVariableUpdate {
    return Object.freeze(Object.fromEntries([...touched].map(k => [k, draft[k]])))
  }
  function setField(field: keyof Fields, value: string) { if (!pending.value && !writing.value) { draft[field] = value; touched.add(field) } }
  function finishConfirmation(ok: boolean) { confirmation.open = false; const done = confirmResult; confirmResult = null; done?.(ok) }
  function ask(title: string, text: string, label: string) {
    if (confirmResult) return Promise.resolve(false)
    Object.assign(confirmation, { open: true, title, message: text, label })
    return new Promise<boolean>(resolve => { confirmResult = resolve })
  }
  function clearEditor() { Object.assign(editor, { open: false, mode: 'create', version: '', targetID: '', message: '', conflict: false }); Object.assign(draft, blank()); touched.clear() }
  function clearValues() { page.items = []; page.nextCursor = undefined; page.phase = 'inactive'; page.message = ''; page.stale = false; detail.variable = null; detail.phase = 'inactive'; detail.message = ''; clearEditor(); selected.value = ''; message.value = '' }
  function retire() { ++revision; reading.value = false; auth.projectVariables.abandonReads() }
  function clearScope(abandon: boolean) { retire(); clearValues(); requested = false; committed = null; checking = null; needsRead.value = false; writing.value = false; finishConfirmation(false); if (abandon) auth.projectVariables.abandonPending() }
  const live = (c: Context, serial: number) => visible.value && serial === revision && sameContext(c, context.value)
  function deny(e: AccountFailure) {
    if ([401, 403, 404].includes(e.problem?.status ?? 0)) { clearScope(true); denied.value = true; page.phase = 'error'; page.message = explanation(e); return true }
    return false
  }
  async function readPage(next?: string) {
    const c = context.value
    if (!c || !visible.value || denied.value || busy.value || pending.value) return
    const serial = ++revision
    reading.value = true; page.phase = 'loading'; page.message = ''; page.items = []; page.nextCursor = undefined
    try {
      const result = await auth.projectVariables.list(c.projectID, { limit: 50, ...(next === undefined ? {} : { cursor: next }) })
      if (!live(c, serial)) return
      page.items = result.items; page.nextCursor = result.next_cursor; page.phase = 'ready'; page.stale = false;
    } catch (error) {
      if (!live(c, serial)) return
      const e = asFailure(error)
      if (!deny(e)) { page.phase = 'error'; page.message = explanation(e) }
    } finally { if (serial === revision) reading.value = false }
  }
  async function select(id: string) {
    if (!context.value || denied.value || busy.value || pending.value) return
    if (editor.open && !(await cancelEdit())) return
    const c = context.value
    if (!c) return
    const serial = ++revision
    selected.value = id; detail.variable = null; detail.phase = 'loading'; detail.message = ''; reading.value = true
    try {
      const result = await auth.projectVariables.get(c.projectID, id)
      if (live(c, serial) && selected.value === id) { detail.variable = result; detail.phase = 'ready' }
    } catch (error) { if (live(c, serial)) { const e = asFailure(error); if (!deny(e)) { detail.phase = 'error'; detail.message = explanation(e) } } }
    finally { if (serial === revision) reading.value = false }
  }
  async function refresh() {
    if (busy.value || pending.value) return
    if (denied.value) { await workspace.readCurrent(); return }
    if (needsRead.value) return rereadCommitted()
    if (editor.conflict) return rereadConflict()
    if (editor.open && !(await cancelEdit())) return
    const id = selected.value, c = context.value
    detail.variable = null; detail.phase = 'inactive'; detail.message = ''
    await readPage()
    if (id && sameContext(c, context.value) && page.phase === 'ready') await select(id)
  }
  async function openCreate() {
    if (!canMutate.value || busy.value || pending.value || needsRead.value) return
    if (editor.open && !(await cancelEdit())) return
    if (auth.projectVariables.progress?.phase === 'rejected') auth.projectVariables.editRejected()
    clearEditor(); editor.open = true; editor.mode = 'create'
  }
  function openEdit() {
    const variable = detail.variable
    if (!canMutate.value || busy.value || pending.value || needsRead.value || editor.open || !variable || detail.phase !== 'ready') return
    if (auth.projectVariables.progress?.phase === 'rejected') auth.projectVariables.editRejected()
    clearEditor(); Object.assign(editor, { open: true, mode: 'update', targetID: variable.id, version: variable.version }); Object.assign(draft, { name: variable.name, description: variable.description, value: variable.value })
  }
  async function cancelEdit() {
    if (writing.value || pending.value) return false
    if (touched.size > 0 && !(await ask('放弃变量输入？', '离开后将清除当前尚未提交的输入。', '放弃输入'))) return false
    clearEditor(); return true
  }
  async function accept(value: ProjectVariableReceipt | ProjectVariableLookup, c: Context, serial: number) {
    if (!live(c, serial)) return
    const receipt = 'status' in value ? value.receipt : value
    if (!receipt) { message.value = '原操作尚未确认，请稍后再次查询原操作。'; return }
    // Keep only the locator/version floor outside Session. Historical plaintext is not current data.
    committed = { id: receipt.variable.id, version: receipt.variable.version, context: c }
    needsRead.value = true; clearEditor(); detail.variable = null; detail.phase = 'inactive'; page.stale = true
  }
  async function submit() {
    if (!canSave.value || !context.value) return
    const c = context.value, mode = editor.mode, targetID = editor.targetID, expected = editor.version
    const input = mode === 'create' ? Object.freeze({ ...draft }) : patch()
    if (!(await ask(mode === 'create' ? '确认创建普通变量？' : '确认保存普通变量？', '仅保存本次明确提交的字段；值不会自动修剪。', mode === 'create' ? '确认创建变量' : '确认保存变量'))) return
    if (!sameContext(c, context.value) || !canMutate.value || busy.value) return
    const serial = ++revision
    writing.value = true; editor.message = ''; message.value = ''
    try {
      const value = mode === 'create'
        ? await auth.projectVariables.create(c.projectID, input as Fields)
        : await auth.projectVariables.update(c.projectID, targetID, expected, input)
      await accept(value, c, serial)
    } catch (error) {
      if (live(c, serial)) {
        const e = asFailure(error)
        if (!deny(e)) {
          editor.message = explanation(e); message.value = explanation(e)
          if (progress.value?.phase === 'rejected') {
            // A definite rejection may retire the command, never the user's input or presence.
            auth.projectVariables.editRejected()
            editor.conflict = mode === 'update' && e.problem?.code === 'VERSION_CONFLICT'
            detail.variable = null; detail.phase = 'error'; detail.message = explanation(e)
          }
        }
      }
    } finally { if (serial === revision) writing.value = false }
    if (live(c, serial) && needsRead.value) await rereadCommitted()
  }
  async function rereadConflict() {
    const c = context.value, target = editor.targetID
    if (!c || !canMutate.value || busy.value || pending.value || !editor.open || !editor.conflict) return
    const serial = ++revision
    reading.value = true; detail.variable = null; detail.phase = 'loading'; detail.message = ''
    try {
      const value = await auth.projectVariables.get(c.projectID, target)
      if (live(c, serial) && editor.targetID === target) {
        detail.variable = value; detail.phase = 'ready'
        editor.message = '已读取当前值。输入与原版本仍保留；核对下方详情后可明确采用当前版本，再确认保存。'
      }
    } catch (error) { if (live(c, serial)) { const e = asFailure(error); if (!deny(e)) { detail.phase = 'error'; detail.message = explanation(e) } } }
    finally { if (serial === revision) reading.value = false }
  }
  function adoptCurrentVersion() {
    if (!canAdoptVersion.value || !detail.variable) return
    editor.version = detail.variable.version; editor.conflict = false
    editor.message = '已采用当前版本并保留输入。只有明确修改的字段会在再次确认后保存。'; message.value = ''
  }
  async function lookupOriginal() {
    const c = context.value
    if (!c || busy.value || progress.value?.phase !== 'uncertain' || !progress.value.contextValid) return
    const serial = ++revision; writing.value = true
    try { await accept(await auth.projectVariables.checkOriginal(), c, serial) }
    catch (error) { if (live(c, serial)) { const e = asFailure(error); if (!deny(e)) message.value = explanation(e) } }
    finally { if (serial === revision) writing.value = false }
    if (live(c, serial) && needsRead.value) await rereadCommitted()
  }
  async function rereadCommitted() {
    const original = committed, c = context.value
    if (!original || !c || !sameContext(original.context, c) || busy.value) return
    const serial = ++revision; reading.value = true; message.value = '操作已确认，正在读取当前变量与目录。'
    try {
      const variable = await auth.projectVariables.get(c.projectID, original.id)
      if (!live(c, serial) || committed !== original) return
      if (BigInt(variable.version) < BigInt(original.version)) throw new AccountFailure('invalid-response')
      const list = await auth.projectVariables.list(c.projectID, { limit: 50 })
      if (!live(c, serial) || committed !== original) return
      detail.variable = variable; detail.phase = 'ready'; detail.message = ''; selected.value = variable.id
      page.items = list.items; page.nextCursor = list.next_cursor; page.phase = 'ready'; page.stale = false; page.message = ''
      needsRead.value = false; committed = null; auth.projectVariables.finishConfirmed(); message.value = '已确认保存，并重新读取当前变量与目录。'
    } catch (error) { if (live(c, serial)) { const e = asFailure(error); if (!deny(e)) { detail.variable = null; detail.phase = 'error'; detail.message = explanation(e); message.value = '操作已确认，但当前读取未完成。请重读，不要再次提交。' } } }
    finally { if (serial === revision) reading.value = false }
  }
  function pump() { if (requested && visible.value && !busy.value && !pending.value) { requested = false; void readPage() } }
  const stop = watch(() => [context.value, auth.personalContext.identity, auth.state.busy, workspace.paths.value.home, auth.state.phase, workspace.detail.phase] as const, ([next, nextIdentity]) => {
    if (disposed.value) return
    if (!sameIdentity(identity, nextIdentity)) { clearScope(true); identity = nextIdentity; bound = null }
    if (checking && (workspace.detail.phase === 'unavailable' || (next && checking.projectID !== next.projectID))) clearScope(true)
    if (!sameContext(bound, next)) {
      const previous = bound
      bound = next
      const address = projectRoute(route.value)
      const sameRoute = address?.suffix === '/settings/variables' && `/${address.username}/${address.project_name}` === workspace.paths.value.home
      if (previous) {
        if (!next && auth.state.phase === 'checking' && sameIdentity(previous.identity, nextIdentity)) {
          // Session retains the original Unknown bytes/key while authority is being checked.
          // Page plaintext is hidden/cleared; only a matching current scope can expose Lookup.
          checking = previous; retire(); clearValues(); finishConfirmation(false); writing.value = false
        }
        else if (!sameRoute || (next && previous.projectID !== next.projectID)) clearScope(true)
        else { retire(); clearValues(); finishConfirmation(false); writing.value = false }
      }
      if (next) {
        checking = null
        denied.value = false
        if (committed && committed.context.projectID === next.projectID && sameIdentity(committed.context.identity, next.identity)) committed = { ...committed, context: next }
        requested = !needsRead.value; pump()
      }
    } else pump()
  }, { immediate: true, flush: 'sync' })
  async function confirmLeave(target?: string) {
    if (target === route.value) return true
    if (dirty.value && !(await ask('离开普通变量页面？', '未确认的原操作不会重发；离开将清除页面输入和结果。', '离开并清除'))) return false
    clearScope(true); return true
  }
  return { visible, canMutate, page, detail, editor, draft, selected, message, progress, pending, busy, needsRead, canSave, canAdoptVersion, confirmation,
    setField, readPage, select, refresh, openCreate, openEdit, cancelEdit, submit, lookupOriginal, rereadCommitted, rereadConflict, adoptCurrentVersion, finishConfirmation, confirmLeave,
    nextPage() { if (page.nextCursor && !page.stale) return readPage(page.nextCursor) },
    afterNavigation(to: string, _from = '') { if (to !== route.value) { clearScope(true); bound = null; route.value = to } },
    dispose() { disposed.value = true; stop(); clearScope(true) },
  }
}
export type ProjectVariables = ReturnType<typeof createProjectVariables>
export const projectVariablesKey: InjectionKey<ProjectVariables> = Symbol('project-variables')
export function useProjectVariables() { const owner = inject(projectVariablesKey); if (!owner) throw new Error('Project Variables owner is unavailable'); return owner }
