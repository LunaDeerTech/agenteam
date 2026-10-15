<script setup lang="ts">
import { computed, onScopeDispose, reactive, watch } from 'vue'
import { AccountFailure } from '../../api/client'
import {
  sameArtifactRef,
  type ArtifactContent,
  type ArtifactMetadata,
} from '../../api/project-artifacts'
import { useSession } from '../../composables/useSession'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { UiButton, UiDialog } from '../ui'
const props = defineProps<{ open: boolean; meetingId: string; artifactId: string; label: string }>()
const emit = defineEmits<{ 'update:open': [open: boolean] }>()
const auth = useSession(),
  workspace = useProjectWorkspace()
const state = reactive({
  phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error' | 'unavailable',
  message: '',
  metadata: null as ArtifactMetadata | null,
  content: null as ArtifactContent | null,
  index: 0,
})
const offsets: string[] = ['0']
let revision = 0,
  ownsRead = false,
  disposed = false
const scope = computed(() => {
  const context = workspace.currentReadContext.value
  return context && props.open
    ? JSON.stringify([
        context.projectID,
        context.identity.userID,
        context.identity.sessionID,
        context.identity.epoch,
        context.generation,
        context.readGeneration,
        props.meetingId,
        props.artifactId,
      ])
    : ''
})
const busy = computed(() => auth.state.busy || state.phase === 'loading')
const canNext = computed(
  () =>
    state.content?.projection === 'text' &&
    state.content.truncated &&
    BigInt(state.content.next_offset) > BigInt(state.content.offset) &&
    state.index < 1023,
)
function abandon() {
  ++revision
  if (ownsRead) auth.projectArtifacts.abandon()
  ownsRead = false
}
async function load(offset = '0', index = 0) {
  const context = workspace.currentReadContext.value,
    captured = scope.value
  if (disposed || !captured || !context || state.phase === 'loading') return
  if (auth.state.busy) {
    state.phase = 'error'
    state.message = '正在完成当前操作，请稍后读取文件。'
    return
  }
  const serial = ++revision,
    live = () => !disposed && revision === serial && scope.value === captured
  state.phase = 'loading'
  state.message = ''
  state.content = null
  try {
    if (!state.metadata) {
      const request = auth.projectArtifacts.metadata(context.projectID, props.artifactId)
      ownsRead = auth.state.busy
      let metadata: ArtifactMetadata
      try {
        metadata = await request
      } finally {
        if (live()) ownsRead = false
      }
      if (!live()) return
      if (
        metadata.artifact_ref.project_id !== context.projectID ||
        metadata.artifact_ref.artifact_id !== props.artifactId
      )
        throw new AccountFailure('invalid-response')
      state.metadata = metadata
    }
    const target = state.metadata.artifact_ref
    if (auth.state.busy) throw new AccountFailure('busy')
    const request = auth.projectArtifacts.read(target, { offset, limit: 8192 })
    ownsRead = auth.state.busy
    let content: ArtifactContent
    try {
      content = await request
    } finally {
      if (live()) ownsRead = false
    }
    if (!live()) return
    if (!sameArtifactRef(content.artifact.artifact_ref, target))
      throw new AccountFailure('invalid-response')
    state.metadata = content.artifact
    state.content = content
    state.index = index
    state.phase = 'ready'
    if (
      content.projection === 'text' &&
      content.truncated &&
      content.next_offset === content.offset
    )
      state.message = '本次未取得更多正文，可以重新读取。'
  } catch (error) {
    if (!live()) return
    state.content = null
    const failure = error instanceof AccountFailure ? error : null
    const unavailable =
      failure?.problem &&
      (failure.problem.status === 403 ||
        failure.problem.status === 404 ||
        failure.problem.code === 'RESOURCE_DELETED')
    state.phase = unavailable ? 'unavailable' : 'error'
    if (unavailable) state.metadata = null
    state.message = unavailable
      ? '此文件已不可用。历史引用仍保留。'
      : '暂时无法读取文件，请稍后重试。'
  }
}
function next() {
  if (busy.value || !canNext.value || !state.content) return
  offsets[state.index + 1] = state.content.next_offset
  void load(state.content.next_offset, state.index + 1)
}
function previous() {
  if (!busy.value && state.index > 0) void load(offsets[state.index - 1]!, state.index - 1)
}
watch(
  scope,
  (value, before) => {
    abandon()
    offsets.splice(0, offsets.length, '0')
    Object.assign(state, { phase: 'idle', message: '', metadata: null, content: null, index: 0 })
    if (value && !before) void load()
    else if (value !== before && before) emit('update:open', false)
  },
  { immediate: true, flush: 'sync' },
)
onScopeDispose(() => {
  disposed = true
  abandon()
})
</script>
<template>
  <UiDialog :open="open" :title="label || '文件引用'" @update:open="emit('update:open', $event)">
    <section class="file-preview" aria-label="会议文件内容">
      <p v-if="state.phase === 'loading'" role="status">正在读取文件…</p>
      <p v-if="state.message" class="notice" role="status">{{ state.message }}</p>
      <template v-if="state.metadata">
        <p class="muted">
          {{ state.metadata.name }} · {{ state.metadata.object.media_type }} ·
          {{ state.metadata.object.byte_size }} 字节
        </p>
        <template v-if="state.content?.projection === 'text'">
          <pre v-if="state.content.text" tabindex="0" aria-label="文件正文">{{
            state.content.text
          }}</pre>
          <p v-else>正文为空。</p>
          <p v-if="state.content.truncated" class="muted">当前显示部分正文。</p>
          <div class="file-pages">
            <UiButton :disabled="busy || state.index === 0" @click="previous">上一段</UiButton
            ><UiButton :disabled="busy || !canNext" @click="next">下一段</UiButton>
          </div>
        </template>
        <p v-else-if="state.content">此类型暂不支持在线预览，可以查看文件信息。</p>
      </template>
      <UiButton
        v-if="state.phase === 'error' || state.phase === 'ready'"
        :disabled="busy"
        @click="load(offsets[state.index], state.index)"
        >重新读取</UiButton
      >
    </section>
    <template #footer><UiButton @click="emit('update:open', false)">返回会议</UiButton></template>
  </UiDialog>
</template>
<style scoped>
.file-preview {
  display: grid;
  gap: 12px;
  min-width: 0;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
pre {
  margin: 0;
  padding: 12px;
  max-height: 50vh;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border: 1px solid var(--border);
  border-radius: 6px;
  font: 13px/1.6 var(--font-mono);
}
.file-pages {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.muted {
  color: var(--muted);
  font-size: 12px;
}
.notice {
  color: var(--warning);
}
</style>
