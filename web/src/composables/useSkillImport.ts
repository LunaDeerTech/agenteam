import { computed, ref, shallowRef, watch } from 'vue'
import { AccountFailure } from '../api/client'
import { readSkillImportFile } from '../api/skill-import'
import { useSession, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'

export function useSkillImport(
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
  refresh: () => void = () => undefined,
) {
  const open = ref(false),
    reading = ref(false),
    message = ref(''),
    fileRevision = ref(0)
  const selected = shallowRef<Readonly<{ name: string; size: number }> | null>(null)
  let text: string | null = null,
    serial = 0,
    disposed = false
  const context = workspace.currentReadContext
  const visible = computed(() => !disposed && !!context.value)
  const progress = computed(() => {
    const value = auth.skillImport.progress
    return visible.value &&
      value &&
      value.projectID === context.value?.projectID &&
      value.contextValid
      ? value
      : null
  })
  const otherPending = computed(
    () => visible.value && !!auth.skillImport.progress && !progress.value,
  )
  const canWrite = computed(() => visible.value && workspace.detail.project?.lifecycle === 'active')
  const blocked = computed(() => !visible.value || auth.state.busy || reading.value)
  const editable = computed(
    () => !otherPending.value && (!progress.value || progress.value.phase === 'rejected'),
  )
  const canSubmit = computed(
    () => !blocked.value && canWrite.value && editable.value && !!selected.value,
  )
  const detailPath = computed(() =>
    progress.value?.phase === 'confirmed'
      ? `${workspace.paths.value.home}/settings/skills/${progress.value.skillID}`
      : '',
  )
  function resetFile() {
    ++serial
    ++fileRevision.value
    reading.value = false
    selected.value = null
    text = null
  }
  const stop = watch(
    () => [context.value?.projectID, auth.personalContext.identity] as const,
    () => {
      resetFile()
      open.value = false
      message.value = ''
    },
    { flush: 'sync' },
  )
  function show() {
    if (!blocked.value) open.value = true
  }
  function close() {
    if (blocked.value) return
    open.value = false
    resetFile()
    message.value = ''
  }
  async function select(files: FileList | readonly File[]) {
    if (blocked.value || !canWrite.value || !editable.value) return
    resetFile()
    message.value = ''
    if (files.length !== 1) {
      message.value = '请选择一个名为 SKILL.md 的文件。'
      return
    }
    const own = serial,
      project = context.value?.projectID
    reading.value = true
    try {
      const file = await readSkillImportFile(files[0]!)
      if (disposed || serial !== own || context.value?.projectID !== project) return
      selected.value = Object.freeze({ name: file.name, size: file.size })
      text = file.text
    } catch {
      if (serial === own && !disposed)
        message.value =
          '文件必须名为 SKILL.md，使用 UTF-8 和 LF 换行，不含 BOM、CR 或 NUL，且不超过 256 KiB。文件内容不会被修改。'
    } finally {
      if (serial === own) reading.value = false
    }
  }
  async function act(lookup = false) {
    if (
      blocked.value ||
      !context.value ||
      (lookup ? progress.value?.phase !== 'uncertain' : !canSubmit.value)
    )
      return
    const own = serial,
      project = context.value.projectID
    message.value = ''
    try {
      if (!lookup && progress.value?.phase === 'rejected') auth.skillImport.clearSettled()
      await (lookup ? auth.skillImport.lookup() : auth.skillImport.start(project, text!))
      if (disposed || own !== serial || context.value?.projectID !== project) return
      resetFile()
      refresh()
    } catch (error) {
      if (disposed || own !== serial || context.value?.projectID !== project) return
      const failure = error instanceof AccountFailure ? error : new AccountFailure('transport')
      message.value =
        progress.value?.phase === 'uncertain'
          ? failure.problem?.code === 'RESOURCE_BUSY'
            ? '原导入仍在处理中。请稍后检查原请求，不会重新导入。'
            : failure.problem?.code === 'INVALID_STATE'
              ? '原导入未完成。请保留原请求并检查其结果，不会另建导入。'
              : '尚未取得原导入的有效结果；未查到记录也不表示没有提交。请检查原请求。'
          : '文件未被接受。请检查标准 SKILL.md 包格式和大小后重新选择文件。'
    }
  }
  function another() {
    if (blocked.value || progress.value?.phase !== 'confirmed') return
    auth.skillImport.clearSettled()
    resetFile()
    message.value = ''
  }
  return {
    open,
    reading,
    message,
    selected,
    fileRevision,
    visible,
    progress,
    otherPending,
    canWrite,
    blocked,
    editable,
    canSubmit,
    detailPath,
    show,
    close,
    select,
    act,
    another,
    dispose() {
      disposed = true
      resetFile()
      open.value = false
      message.value = ''
      stop()
    },
  }
}
export type SkillImportController = ReturnType<typeof useSkillImport>
