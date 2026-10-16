import { computed, reactive, watch } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureProjectSecretUpdate,
  captureProjectSecretValue,
  type ProjectSecretReceipt,
} from '../api/project-secret-commands'
import type { ProjectSecretMetadata } from '../api/project-secrets'
import { useSession, type SessionController } from './useSession'
import { useProjectWorkspace, type ProjectWorkspace } from './useProjectWorkspace'
import type { ProjectSecretsController } from './useProjectSecrets'

type Mode = 'create' | 'metadata' | 'replace'
export function useProjectSecretEditor(
  reader: ProjectSecretsController,
  auth: SessionController = useSession(),
  workspace: ProjectWorkspace = useProjectWorkspace(),
) {
  // No material belongs in this reactive draft, baseline, public progress or Session.
  const state = reactive({
    open: false,
    mode: 'create' as Mode,
    name: '',
    description: '',
    targetID: '',
    version: '',
    message: '',
    conflict: false,
    reading: false,
    materialRevision: 0,
    formRevision: 0,
  })
  const deletion = reactive({ open: false, name: '', version: '' })
  let deleteTarget: ProjectSecretMetadata | null = null
  let base: ProjectSecretMetadata | null = null,
    disposed = false,
    generation = 0
  const key = computed(() => {
    const context = workspace.currentReadContext.value
    if (disposed || !reader.visible.value || !context) return ''
    const identity = context.identity
    return `${context.projectID}/${identity.userID}/${identity.sessionID}/${identity.epoch}/${context.generation}/${context.readGeneration}`
  })
  const visible = computed(() => key.value !== '' && reader.page.phase !== 'unavailable')
  const progress = computed(() => {
    const value = auth.projectSecretCommands.progress
    return visible.value &&
      value?.identity.projectID === workspace.currentReadContext.value?.projectID
      ? value
      : null
  })
  const unresolved = computed(() =>
    ['submitting', 'uncertain'].includes(progress.value?.phase ?? ''),
  )
  const blocked = computed(
    () => !visible.value || auth.state.busy || state.reading || reader.blocked.value,
  )
  const writable = computed(() => visible.value && workspace.detail.project?.lifecycle === 'active')
  const canOpen = computed(
    () =>
      !blocked.value &&
      writable.value &&
      !deletion.open &&
      !unresolved.value &&
      !auth.projectSecretCommands.progress,
  )
  const canDelete = computed(
    () =>
      canOpen.value &&
      !state.open &&
      reader.detail.phase === 'ready' &&
      !!reader.detail.value &&
      reader.detail.value.id === reader.selected.value &&
      !(state.conflict && base === reader.detail.value),
  )
  function cancelDelete() {
    deletion.open = false
    deletion.name = ''
    deletion.version = ''
    deleteTarget = null
  }
  const canSave = computed(
    () =>
      state.open &&
      !blocked.value &&
      writable.value &&
      !state.conflict &&
      !unresolved.value &&
      (state.mode !== 'metadata' ||
        (!!base && (state.name !== base.name || state.description !== base.description))),
  )
  const canLookup = computed(
    () => !blocked.value && progress.value?.phase === 'uncertain' && !!progress.value.contextValid,
  )
  function clear(message = '') {
    cancelDelete()
    ++generation
    ++state.materialRevision
    ++state.formRevision
    Object.assign(state, {
      open: false,
      name: '',
      description: '',
      targetID: '',
      version: '',
      message,
      conflict: false,
      reading: false,
    })
    base = null
  }
  function close() {
    clear()
    auth.projectSecretCommands.abandon()
    const pending = auth.projectSecretCommands.progress
    if (!auth.state.busy && pending?.phase === 'rejected' && pending.contextValid)
      auth.projectSecretCommands.editRejected()
  }
  const stop = watch(
    [key, () => reader.page.phase],
    ([now, phase], [previous]) => {
      if (now !== previous || phase === 'unavailable') close()
    },
    { flush: 'sync' },
  )
  function open(mode: Mode) {
    if (!canOpen.value) return
    const value = reader.detail.value
    if (
      mode !== 'create' &&
      (reader.detail.phase !== 'ready' || !value || value.id !== reader.selected.value)
    )
      return
    clear()
    base = mode === 'create' ? null : value
    Object.assign(state, {
      open: true,
      mode,
      name: base?.name ?? '',
      description: base?.description ?? '',
      targetID: base?.id ?? '',
      version: base?.version ?? '',
    })
  }
  function failure(error: unknown) {
    const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
    if ([401, 403, 404, 410].includes(e.problem?.status ?? 0)) {
      clear('当前 Secret 不存在或不可访问。')
      reader.deny()
      return
    }
    if (progress.value?.phase === 'uncertain') {
      state.message =
        progress.value.identity.kind === 'delete'
          ? '删除结果尚未确认。只能检查原请求，不能再次删除。'
          : '提交结果尚未确认。材料已清除；只能检查原请求，不能重复提交。'
      return
    }
    state.conflict = state.mode !== 'create' && e.problem?.code === 'VERSION_CONFLICT'
    state.message = state.conflict
      ? '记录已经变化。请重新读取当前版本后检查草稿；替换材料需要重新输入。'
      : e.problem?.code === 'RESOURCE_BUSY'
        ? '名称已被占用或当前资源不可修改。请检查名称和草稿后重新提交；材料需要重新输入。'
        : e.kind === 'invalid-input'
          ? '请检查名称、描述和 Secret 值的格式。'
          : '本次提交未完成。已输入的材料不会保留，请检查后重新输入。'
  }
  function confirmed(target: string, deleted = false) {
    clear(
      deleted
        ? 'Secret 已删除，正在独立重新读取目录。历史记录保留。'
        : '请求已确认，正在独立读取当前记录。',
    )
    auth.projectSecretCommands.finishConfirmed()
    if (deleted) reader.refreshAfterDelete()
    else reader.refreshAfterWrite(target)
  }
  function acceptReceipt(receipt: ProjectSecretReceipt) {
    if (receipt.command === 'project.secret_variable.delete') confirmed(receipt.deleted.id, true)
    else confirmed(receipt.variable.id)
  }
  function requestDelete() {
    if (!canDelete.value) return
    const current = reader.detail.value!
    clear()
    deleteTarget = current
    Object.assign(deletion, { open: true, name: current.name, version: current.version })
  }
  async function confirmDelete() {
    const original = deleteTarget
    if (
      !deletion.open ||
      !original ||
      blocked.value ||
      !writable.value ||
      auth.projectSecretCommands.progress ||
      reader.detail.value !== original ||
      reader.selected.value !== original.id
    ) {
      cancelDelete()
      return
    }
    const capturedKey = key.value,
      serial = generation
    cancelDelete()
    try {
      const result = await auth.projectSecretCommands.delete(
        original.project_id,
        original.id,
        original.version,
      )
      if (serial !== generation || capturedKey !== key.value || !visible.value) return
      if (!('status' in result)) acceptReceipt(result)
    } catch (error) {
      if (serial !== generation || capturedKey !== key.value || !visible.value) return
      const e = error instanceof AccountFailure ? error : new AccountFailure('transport')
      if ([401, 403, 404, 410].includes(e.problem?.status ?? 0)) {
        failure(e)
        return
      }
      if (progress.value?.phase === 'uncertain') {
        state.message = '删除结果尚未确认。只能检查原请求，不能再次删除。'
        return
      }
      state.conflict = e.problem?.code === 'VERSION_CONFLICT'
      base = original
      state.message = state.conflict
        ? '记录已经变化。请重新读取详情，再确认要删除的版本。'
        : e.problem?.code === 'RESOURCE_BUSY'
          ? 'Secret 仍被引用，无法删除。请先解除引用后再重试。'
          : '删除未完成。当前记录保留，请检查后重试。'
      if (progress.value?.phase === 'rejected' && progress.value.contextValid)
        auth.projectSecretCommands.editRejected()
    }
  }
  async function submit(takeMaterial: () => string) {
    if (!canSave.value) return
    const projectID = workspace.currentReadContext.value!.projectID,
      capturedKey = key.value,
      serial = generation
    let value: string | undefined
    try {
      // Validate safe fields before consuming the private material input.
      if (state.mode !== 'replace')
        captureProjectSecretUpdate({ name: state.name, description: state.description })
      if (state.mode !== 'metadata') value = captureProjectSecretValue(takeMaterial())
      ++state.materialRevision
      if (progress.value?.phase === 'rejected') auth.projectSecretCommands.editRejected()
      const operation =
        state.mode === 'create'
          ? auth.projectSecretCommands.create(projectID, {
              name: state.name,
              description: state.description,
              value: value!,
            })
          : auth.projectSecretCommands.update(
              projectID,
              state.targetID,
              state.version,
              state.mode === 'replace'
                ? { value: value! }
                : {
                    ...(state.name !== base?.name ? { name: state.name } : {}),
                    ...(state.description !== base?.description
                      ? { description: state.description }
                      : {}),
                  },
            )
      value = undefined
      const result = await operation
      if (serial !== generation || capturedKey !== key.value || !visible.value) return
      if (!('status' in result)) acceptReceipt(result)
    } catch (error) {
      if (serial === generation && capturedKey === key.value && visible.value) failure(error)
    } finally {
      value = undefined
    }
  }
  async function lookup() {
    if (!canLookup.value) return
    const capturedKey = key.value,
      serial = generation
    try {
      const result = await auth.projectSecretCommands.lookup()
      if (serial !== generation || capturedKey !== key.value || !visible.value) return
      if ('status' in result && result.status === 'committed') acceptReceipt(result.receipt)
      else state.message = '尚未观察到原请求提交；这不证明它已回滚。请稍后再次检查原请求。'
    } catch (error) {
      if (serial === generation && capturedKey === key.value && visible.value) failure(error)
    }
  }
  async function readCurrent() {
    if (blocked.value || !state.open || state.mode === 'create' || unresolved.value) return
    const capturedKey = key.value,
      serial = generation,
      target = state.targetID
    ++state.materialRevision
    state.reading = true
    try {
      const value = await auth.projectSecrets.get(
        workspace.currentReadContext.value!.projectID,
        target,
      )
      if (serial !== generation || capturedKey !== key.value || !visible.value) return
      base = value
      state.version = value.version
      state.conflict = false
      state.message = '已读取当前版本，名称与描述草稿保留；请确认后再提交。'
    } catch (error) {
      if (serial === generation && capturedKey === key.value && visible.value) failure(error)
    } finally {
      if (serial === generation) state.reading = false
    }
  }
  return {
    state,
    deletion,
    canDelete,
    requestDelete,
    cancelDelete,
    confirmDelete,
    visible,
    progress,
    blocked,
    canOpen,
    canSave,
    canLookup,
    open,
    close,
    submit,
    lookup,
    readCurrent,
    readConfirmed() {
      const pending = progress.value
      if (!blocked.value && pending?.phase === 'confirmed' && pending.contextValid)
        confirmed(pending.identity.targetID, pending.identity.kind === 'delete')
    },
    dispose() {
      if (disposed) return
      stop()
      close()
      disposed = true
    },
  }
}
export type ProjectSecretEditor = ReturnType<typeof useProjectSecretEditor>
