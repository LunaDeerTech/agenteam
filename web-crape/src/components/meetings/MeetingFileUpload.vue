<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { AccountFailure } from '../../api/client'
import { captureArtifactUploadFile } from '../../api/artifact-upload'
import type { ArtifactMetadata } from '../../api/project-artifacts'
import type { MeetingReference } from '../../api/meetings'
import { useSession } from '../../composables/useSession'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { UiButton } from '../ui'
const props = defineProps<{ meetingId: string | null; disabled: boolean; file?: File | null }>()
const emit = defineEmits<{ choose: [reference: MeetingReference] }>()
const auth = useSession(),
  workspace = useProjectWorkspace()
const selected = shallowRef<File | null>(null),
  message = ref(''),
  working = ref(false),
  stopped = ref(false)
let revision = 0,
  disposed = false
const context = computed(() => workspace.currentReadContext.value)
const scope = computed(() => {
  const c = context.value
  return c
    ? JSON.stringify([
        c.projectID,
        c.identity.userID,
        c.identity.sessionID,
        c.identity.epoch,
        c.generation,
        c.readGeneration,
        props.meetingId,
      ])
    : ''
})
const current = computed(() => {
  const progress = auth.artifactUploads.progress
  return progress &&
    progress.projectID === context.value?.projectID &&
    progress.meetingID === props.meetingId
    ? progress
    : null
})
const foreign = computed(() => auth.artifactUploads.pending && !current.value)
const busy = computed(() => auth.state.busy || working.value)
const canSelect = computed(
  () =>
    !!scope.value &&
    !props.disabled &&
    !busy.value &&
    !auth.artifactUploads.pending &&
    workspace.detail.project?.lifecycle === 'active',
)
const status = computed(() => {
  if (stopped.value && busy.value) return '正在停止传输，上传结果仍待确认。'
  const p = current.value
  if (!p) return ''
  if (p.phase === 'beginning') return '正在准备上传…'
  if (p.phase === 'uploading') return '正在上传文件…'
  if (p.phase === 'publishing') return '正在完成上传…'
  if (p.phase === 'confirmed') return '文件已上传，可以插入消息。'
  if (p.phase === 'cancelled') return '上传已取消。'
  if (p.phase === 'uploaded') return '文件已传输，完成上传后即可插入。'
  if (p.phase === 'cancelling') return '正在取消上传，请查询确认结果。'
  if (p.phase === 'uncertain')
    return p.observation === 'pending'
      ? '原上传仍在处理中，请查询确认结果。'
      : '原上传结果仍待确认，请继续查询或请求取消。'
  return '这次上传未开始，请检查文件后再试。'
})
function select(file: File | null) {
  if (!canSelect.value || !file) return
  try {
    captureArtifactUploadFile(file)
    if (!auth.artifactUploads.dismiss()) return
    selected.value = file
    message.value = ''
    stopped.value = false
  } catch {
    message.value = '请选择名称有效且不超过 1 GiB 的文件。'
  }
}
function changed(event: Event) {
  const input = event.target as HTMLInputElement
  select(input.files?.length === 1 ? input.files[0]! : null)
  input.value = ''
}
function choose(artifact: ArtifactMetadata) {
  if (
    props.disabled ||
    !scope.value ||
    artifact.artifact_ref.project_id !== context.value?.projectID
  )
    return
  emit('choose', { kind: 'file', id: artifact.artifact_ref.artifact_id, label: artifact.name })
}
async function act(action: 'start' | 'lookup' | 'complete' | 'cancel') {
  const c = context.value,
    captured = scope.value
  if (!c || !captured || busy.value || foreign.value) return
  const p = current.value
  if (
    action === 'start'
      ? !canSelect.value || !selected.value
      : action === 'lookup'
        ? !p?.canLookup
        : action === 'complete'
          ? !p?.canComplete || props.disabled || workspace.detail.project?.lifecycle !== 'active'
          : !p?.canCancel
  )
    return
  const serial = ++revision
  const live = () => !disposed && serial === revision && captured === scope.value
  working.value = true
  message.value = ''
  stopped.value = false
  try {
    if (action === 'start') {
      const artifact = await auth.artifactUploads.start(
        c.projectID,
        props.meetingId,
        selected.value!,
      )
      if (live() && artifact) choose(artifact)
    } else {
      const result = await auth.artifactUploads[action]()
      if (live() && 'artifact' in result && result.artifact) choose(result.artifact)
    }
  } catch (error) {
    if (!live()) return
    const failure = error instanceof AccountFailure ? error : null
    message.value =
      failure?.kind === 'invalid-input'
        ? '请检查所选文件。'
        : auth.artifactUploads.pending
          ? '请先查询原上传的结果。'
          : '暂时无法上传文件，请稍后再试。'
  } finally {
    if (live()) {
      working.value = false
      selected.value = null
    }
  }
}
function stop() {
  stopped.value = true
  auth.artifactUploads.stop()
}
watch(
  () => props.file,
  (value) => {
    if (value) select(value)
  },
  { immediate: true },
)
watch(
  scope,
  () => {
    ++revision
    working.value = false
    selected.value = null
    message.value = ''
    stopped.value = false
  },
  { flush: 'sync' },
)
onScopeDispose(() => {
  disposed = true
  ++revision
  selected.value = null
})
</script>
<template>
  <section class="meeting-file-upload" aria-label="上传会议文件">
    <p v-if="foreign" role="status">另一个页面有上传待确认，请返回原页面处理。</p>
    <template v-else>
      <label class="file-label"
        >选择上传文件<input type="file" :disabled="!canSelect" @change="changed"
      /></label>
      <p v-if="selected">{{ selected.name }} · {{ selected.size }} 字节</p>
      <p v-else-if="current" class="muted">{{ current.name }} · {{ current.byteSize }} 字节</p>
      <p v-if="status" role="status">{{ status }}</p>
      <p v-if="message" class="notice" role="status">{{ message }}</p>
      <div class="upload-actions">
        <UiButton v-if="selected" variant="primary" :disabled="!canSelect" @click="act('start')"
          >上传并插入</UiButton
        >
        <UiButton v-if="current?.phase === 'uploading'" :disabled="stopped" @click="stop"
          >停止传输</UiButton
        >
        <UiButton v-if="current?.canLookup" :disabled="busy" @click="act('lookup')"
          >查询原上传</UiButton
        >
        <UiButton
          v-if="current?.canComplete"
          variant="primary"
          :disabled="busy || disabled || workspace.detail.project?.lifecycle !== 'active'"
          @click="act('complete')"
          >完成上传并插入</UiButton
        >
        <UiButton v-if="current?.canCancel" :disabled="busy" @click="act('cancel')"
          >取消上传</UiButton
        >
        <UiButton
          v-if="current?.receipt"
          variant="primary"
          :disabled="busy || disabled"
          @click="choose(current.receipt)"
          >插入已上传文件</UiButton
        >
      </div>
    </template>
  </section>
</template>
<style scoped>
.meeting-file-upload,
.file-label {
  display: grid;
  gap: 12px;
}
.upload-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.muted {
  color: var(--muted);
  font-size: 12px;
}
.notice {
  color: var(--warning);
}
input {
  max-width: 100%;
  font: inherit;
}
</style>
