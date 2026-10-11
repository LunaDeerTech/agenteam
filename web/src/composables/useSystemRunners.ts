import { computed, inject, reactive, ref, shallowReactive, watch, type InjectionKey } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureRunnerCommand,
  runnerTextOrder,
  type RunnerCommand,
  type RunnerCreate,
  type RunnerMutation,
  type RunnerLookup,
  type SystemRunner,
} from '../api/system-runners'
import { useSession, type PersonalIdentity, type SessionController } from './useSession'
const same = (a: PersonalIdentity | null, b: PersonalIdentity | null) =>
  !!a && !!b && a.userID === b.userID && a.sessionID === b.sessionID && a.epoch === b.epoch
const empty = () => ({ name: '', description: '', tags: '', root_path: '' })
export function createSystemRunners(auth: SessionController = useSession()) {
  const api = auth.system.runners
  const state = shallowReactive({
    phase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error',
    rows: [] as readonly SystemRunner[],
    next: null as string | null,
    selected: null as string | null,
    detail: null as SystemRunner | null,
    detailPhase: 'inactive' as 'inactive' | 'loading' | 'ready' | 'error',
    message: '',
    detailMessage: '',
    writeMessage: '',
  })
  const draft = reactive(empty()),
    editor = reactive({
      mode: null as 'create' | 'edit' | null,
      target: '',
      version: '',
      conflict: false,
      message: '',
    })
  const business = reactive({
    kind: null as 'enrollment' | 'revoke' | null,
    version: '',
    target: '',
  })
  const confirmation = reactive({ open: false })
  // This short-lived view memory is not put in the Session owner or any persistent store.
  const material = shallowReactive({ token: '', expires: '', target: '', revision: 0 })
  const attached = ref(false),
    reading = ref(false),
    acting = ref(false),
    progress = computed(() => api.progress)
  const authorized = computed(
    () =>
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      !!auth.personalContext.identity &&
      auth.state.user?.role === 'admin' &&
      !auth.system.denied,
  )
  const visible = computed(() => attached.value && authorized.value)
  const blocked = computed(() => auth.state.busy || reading.value || acting.value)
  const unsettled = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const locked = computed(() => blocked.value || unsettled.value || !visible.value)
  let generation = 0,
    baseline = '',
    pendingConfirmation: ((answer: boolean) => void) | null = null,
    timer: ReturnType<typeof setTimeout> | undefined
  let identity = auth.personalContext.identity,
    disposed = false
  const dirty = computed(() => editor.mode !== null && JSON.stringify(draft) !== baseline)
  const current = (g: number, c: PersonalIdentity | null) =>
    !disposed && visible.value && generation === g && same(c, auth.personalContext.identity)
  function clearMaterial() {
    clearTimeout(timer)
    timer = undefined
    material.token = ''
    material.expires = ''
    material.target = ''
    ++material.revision
  }
  function clearVisible() {
    ++generation
    clearMaterial()
    api.cancel()
    state.rows = []
    state.next = null
    state.detail = null
    state.selected = null
    state.phase = 'inactive'
    state.detailPhase = 'inactive'
    state.message = ''
    state.detailMessage = ''
    state.writeMessage = ''
    editor.mode = null
    editor.conflict = false
    editor.message = ''
    Object.assign(draft, empty())
    business.kind = null
    pendingConfirmation?.(false)
    pendingConfirmation = null
    confirmation.open = false
  }
  function fail(error: unknown): string {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if (e.problem?.status === 401 || e.problem?.status === 403) {
      clearVisible()
      return '当前身份不能管理 Runner，请重新检查权限。'
    }
    if (e.problem?.status === 404) {
      clearMaterial()
      state.detail = null
      state.detailPhase = 'error'
      editor.mode = null
      business.kind = null
      Object.assign(draft, empty())
      return '该 Runner 当前不可访问，请重新读取目录。'
    }
    if (e.kind === 'cancelled') return '读取已停止，请明确重新读取。'
    if (e.problem?.code === 'VERSION_CONFLICT')
      return '配置版本已变化。保留草稿，请读取当前版本后明确采用。'
    if (e.kind === 'invalid-input') return '请按字段要求填写，名称、标签和根目录不自动修剪。'
    return '操作未完成，请重新读取或检查原请求。'
  }
  async function readList(append = false) {
    if (!visible.value || blocked.value) return
    const g = generation,
      c = auth.personalContext.identity,
      after = append ? state.next : null
    if (append && !after) return
    reading.value = true
    state.message = ''
    if (!append) state.phase = 'loading'
    try {
      const page = await api.list({ limit: 50, ...(after ? { after_id: after } : {}) })
      if (!current(g, c)) return
      if (
        append &&
        state.rows.length &&
        page.items.length &&
        state.rows.at(-1)!.id >= page.items[0]!.id
      )
        throw new AccountFailure('invalid-response')
      state.rows = Object.freeze(append ? [...state.rows, ...page.items] : [...page.items])
      state.next = page.next
      state.phase = 'ready'
    } catch (e) {
      if (current(g, c)) {
        state.message = fail(e)
        state.phase = append && state.rows.length ? 'ready' : 'error'
      }
    } finally {
      reading.value = false
    }
  }
  async function readDetail(target: string) {
    const g = generation,
      c = auth.personalContext.identity
    state.detailPhase = 'loading'
    state.detailMessage = ''
    state.detail = null
    try {
      const value = await api.get(target)
      if (!current(g, c) || state.selected !== target) return
      state.detail = value
      state.detailPhase = 'ready'
    } catch (e) {
      if (current(g, c)) {
        state.detailMessage = fail(e)
        state.detailPhase = 'error'
      }
    }
  }
  async function refreshDetail() {
    if (!state.selected || locked.value) return
    reading.value = true
    try {
      await readDetail(state.selected)
    } finally {
      reading.value = false
    }
  }
  async function confirmDiscard() {
    if (!dirty.value) return true
    if (pendingConfirmation) return false
    confirmation.open = true
    return new Promise<boolean>((resolve) => {
      pendingConfirmation = resolve
    })
  }
  function finishConfirmation(answer: boolean) {
    confirmation.open = false
    const resolve = pendingConfirmation
    pendingConfirmation = null
    resolve?.(answer)
  }
  async function select(target: string) {
    if (locked.value || !(await confirmDiscard())) return
    clearMaterial()
    editor.mode = null
    state.selected = target
    reading.value = true
    try {
      await readDetail(target)
    } finally {
      reading.value = false
    }
  }
  function newID() {
    const b = new Uint8Array(16)
    crypto.getRandomValues(b)
    let t = BigInt(Date.now())
    for (let i = 5; i >= 0; i--) {
      b[i] = Number(t & 255n)
      t >>= 8n
    }
    b[6] = (b[6]! & 15) | 0x70
    b[8] = (b[8]! & 63) | 0x80
    const h = Array.from(b, (n) => n.toString(16).padStart(2, '0')).join('')
    return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
  }
  function openCreate() {
    if (locked.value) return
    clearMaterial()
    Object.assign(draft, empty())
    baseline = JSON.stringify(draft)
    editor.target = newID()
    editor.version = ''
    editor.conflict = false
    editor.message = ''
    editor.mode = 'create'
  }
  function openEdit() {
    if (locked.value || state.detailPhase !== 'ready' || !state.detail) return
    clearMaterial()
    const d = state.detail
    Object.assign(draft, {
      name: d.name,
      description: d.description,
      tags: d.tags.join('\n'),
      root_path: d.root_path,
    })
    baseline = JSON.stringify(draft)
    editor.target = d.id
    editor.version = d.version
    editor.conflict = false
    editor.message = ''
    editor.mode = 'edit'
  }
  async function closeEditor() {
    if (blocked.value || !(await confirmDiscard())) return
    editor.mode = null
    editor.conflict = false
    Object.assign(draft, empty())
  }
  function draftCommand(): RunnerCommand {
    const tags = draft.tags === '' ? [] : draft.tags.split('\n').sort(runnerTextOrder)
    const request =
      editor.mode === 'create'
        ? {
            runner_id: editor.target,
            name: draft.name,
            description: draft.description,
            tags,
            root_path: draft.root_path,
          }
        : {
            expected_version: editor.version,
            name: draft.name,
            description: draft.description,
            tags,
          }
    return captureRunnerCommand({
      command: editor.mode === 'create' ? 'runner.create' : 'runner.update',
      target: editor.target,
      request,
    })
  }
  const errors = computed(() => {
    if (!editor.mode) return ''
    try {
      draftCommand()
      return ''
    } catch {
      return '名称需1–128字节，描述至多4096字节；标签每行一项且不重复；根目录须为绝对规范路径。'
    }
  })
  const canSave = computed(
    () => !locked.value && editor.mode !== null && !editor.conflict && !errors.value,
  )
  function showMaterial(result: RunnerMutation) {
    clearMaterial()
    if (!result.token_available) return
    const delay = Date.parse(result.expires_at) - Date.now()
    if (!Number.isFinite(delay) || delay <= 0) {
      state.writeMessage = '登记材料已过期，请明确重新签发。'
      return
    }
    material.token = result.enrollment_token
    material.expires = result.expires_at
    material.target = result.receipt.runner.id
    timer = setTimeout(clearMaterial, Math.min(delay, 2147483647))
  }
  async function confirmed(
    result: RunnerMutation | RunnerLookup,
    g: number,
    c: PersonalIdentity | null,
  ) {
    if (!current(g, c)) return
    if ('state' in result && result.state === 'not_observed') {
      state.writeMessage = '尚未观察到原命令提交；这不证明回滚。请继续检查原请求，不要重复创建。'
      return
    }
    editor.mode = null
    editor.conflict = false
    Object.assign(draft, empty())
    business.kind = null
    state.selected = result.receipt.runner.id
    if ('state' in result) {
      clearMaterial()
      state.writeMessage =
        '原命令已确认。历史回执不含登记材料；需要时请读取当前配置后明确重新签发。'
    } else {
      showMaterial(result)
      if (!material.token) state.writeMessage = '命令已确认，正在重新读取当前配置。'
    }
    // Receipt is historical. Only this independent GET supplies editable facts.
    await readDetail(result.receipt.runner.id)
  }
  async function execute(command: RunnerCommand) {
    if (locked.value) return
    const g = generation,
      c = auth.personalContext.identity
    acting.value = true
    clearMaterial()
    state.writeMessage = ''
    editor.message = ''
    try {
      await confirmed(await api.start(command), g, c)
    } catch (e) {
      if (current(g, c)) {
        const message = fail(e)
        state.writeMessage = message
        editor.message = message
        if (e instanceof AccountFailure && e.problem?.code === 'VERSION_CONFLICT')
          editor.conflict = true
      }
    } finally {
      acting.value = false
    }
  }
  async function save() {
    if (!canSave.value) return
    try {
      await execute(draftCommand())
    } catch (e) {
      editor.message = fail(e)
    }
  }
  function openBusiness(kind: 'enrollment' | 'revoke') {
    if (locked.value || state.detailPhase !== 'ready' || !state.detail) return
    clearMaterial()
    business.kind = kind
    business.target = state.detail.id
    business.version = state.detail.version
    state.writeMessage = ''
  }
  async function confirmBusiness() {
    if (!business.kind || locked.value) return
    await execute({
      command: business.kind === 'enrollment' ? 'runner.enrollment.issue' : 'runner.revoke',
      target: business.target,
      request: { expected_version: business.version },
    })
  }
  async function checkOriginal() {
    if (
      blocked.value ||
      !visible.value ||
      progress.value?.phase !== 'uncertain' ||
      !progress.value.contextValid
    )
      return
    acting.value = true
    clearMaterial()
    const g = generation,
      c = auth.personalContext.identity
    try {
      await confirmed(await api.checkOriginal(), g, c)
    } catch (e) {
      if (current(g, c)) state.writeMessage = fail(e)
    } finally {
      acting.value = false
    }
  }
  async function rebase() {
    if (locked.value || editor.mode !== 'edit' || !editor.conflict) return
    reading.value = true
    const g = generation,
      c = auth.personalContext.identity,
      target = editor.target
    try {
      const d = await api.get(target)
      if (current(g, c) && editor.target === target) {
        state.selected = target
        state.detail = d
        state.detailPhase = 'ready'
        editor.version = d.version
        editor.conflict = false
        editor.message = '已采用当前版本，原草稿保留；请检查后再次保存。'
      }
    } catch (e) {
      if (current(g, c)) editor.message = fail(e)
    } finally {
      reading.value = false
    }
  }
  function attach() {
    if (disposed) return
    attached.value = true
  }
  function detach() {
    attached.value = false
    clearVisible()
  }
  async function confirmLeave() {
    if (blocked.value) return false
    return confirmDiscard()
  }
  const stop = watch(
    () =>
      [attached.value, authorized.value, auth.state.busy, auth.personalContext.identity] as const,
    () => {
      const now = auth.personalContext.identity
      if (!authorized.value || !same(identity, now)) {
        clearVisible()
        identity = now
      }
      if (
        visible.value &&
        !auth.state.busy &&
        !reading.value &&
        !acting.value &&
        state.phase === 'inactive'
      )
        void readList()
    },
    { flush: 'sync' },
  )
  function dispose() {
    disposed = true
    stop()
    detach()
  }
  return {
    auth,
    state,
    draft,
    editor,
    business,
    confirmation,
    material,
    progress,
    visible,
    blocked,
    locked,
    unsettled,
    dirty,
    errors,
    canSave,
    attach,
    detach,
    dispose,
    confirmLeave,
    finishConfirmation,
    readList,
    refreshDetail,
    select,
    openCreate,
    openEdit,
    closeEditor,
    save,
    rebase,
    openBusiness,
    confirmBusiness,
    checkOriginal,
    clearMaterial,
    cancel: () => {
      clearMaterial()
      api.cancel()
    },
  }
}
export type SystemRunnersController = ReturnType<typeof createSystemRunners>
const key: InjectionKey<SystemRunnersController> = Symbol('system-runners')
export { key as systemRunnersKey }
export function useSystemRunners() {
  const value = inject(key)
  if (!value) throw new Error('System Runner page context required')
  return value
}
