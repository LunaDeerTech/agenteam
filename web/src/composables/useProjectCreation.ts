import { computed, reactive, ref, watch } from 'vue'
import { AccountFailure } from '../api/client'
import { captureProjectAddress } from '../api/project-owner'
import { useSession, type SessionController } from './useSession'

// Request identity and recovery survive view removal in Session, not in the draft.
export function createProjectCreation(
  auth: SessionController = useSession(),
  refresh: () => unknown = () => undefined,
) {
  const open = ref(false)
  const draft = reactive({ name: '', description: '' })
  const message = ref('')
  const active = ref(true)
  let revision = 0
  const visible = computed(
    () =>
      active.value &&
      auth.state.phase === 'authenticated' &&
      auth.personalContext.phase === 'current' &&
      !!auth.personalContext.identity,
  )
  const progress = computed(() => (visible.value ? auth.projectCreation.progress : null))
  const blocked = computed(() => !visible.value || auth.state.busy)
  const editable = computed(() => !progress.value || progress.value.phase === 'rejected')
  const projectPath = computed(() => {
    const result = progress.value?.result
    if (
      progress.value?.phase !== 'ready' ||
      result?.state !== 'ready' ||
      !auth.state.user ||
      result.project.owner_user_id !== auth.state.user.id
    )
      return ''
    try {
      const address = captureProjectAddress({
        username: auth.state.user.username,
        project_name: result.project.normalized_name,
      })
      return `/${address.username}/${address.project_name}`
    } catch {
      return ''
    }
  })
  let identity = auth.personalContext.identity
  const stop = watch(
    () => auth.personalContext.identity,
    (next) => {
      if (
        identity?.userID !== next?.userID ||
        identity?.sessionID !== next?.sessionID ||
        identity?.epoch !== next?.epoch
      ) {
        ++revision
        open.value = false
        draft.name = draft.description = message.value = ''
      }
      identity = next
    },
    { flush: 'sync' },
  )
  function show() {
    if (!blocked.value) open.value = true
  }
  function close() {
    if (blocked.value) return
    open.value = false
    message.value = ''
    if (editable.value) draft.name = draft.description = ''
  }
  async function act(action: 'create' | 'resume' | 'check') {
    if (blocked.value) return
    const own = revision
    message.value = ''
    try {
      if (action === 'create') {
        if (!editable.value) return
        if (progress.value?.phase === 'rejected') auth.projectCreation.editRejected()
      }
      const result = await (action === 'create'
        ? auth.projectCreation.start({ name: draft.name, description: draft.description })
        : action === 'resume'
          ? auth.projectCreation.resume()
          : auth.projectCreation.check())
      if (result.state === 'ready' && visible.value && own === revision) {
        draft.name = draft.description = ''
        await refresh()
      }
    } catch (error) {
      if (!visible.value || own !== revision) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      message.value =
        failure.kind === 'invalid-input'
          ? '名称需为 1–64 个字母、数字、点、下划线或连字符；不能只有点。描述最多 8192 字节。'
          : failure.problem?.field_errors?.some(
                (field) => field.path === '/name' && field.code === 'NAME_TAKEN',
              )
            ? '项目名称已被使用，请修改后再提交。'
            : '未取得完整创建结果。请核对下方状态，不要另建重复项目。'
    }
  }
  function another() {
    if (blocked.value || progress.value?.phase !== 'ready') return
    auth.projectCreation.clearReady()
    draft.name = draft.description = message.value = ''
  }
  return {
    open,
    draft,
    message,
    progress,
    visible,
    blocked,
    editable,
    projectPath,
    show,
    close,
    act,
    another,
    dispose() {
      ++revision
      active.value = false
      draft.name = draft.description = message.value = ''
      stop()
    },
  }
}
export type ProjectCreation = ReturnType<typeof createProjectCreation>
