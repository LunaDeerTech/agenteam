<script setup lang="ts">
import { UiButton, UiState } from '../ui'
import type { TaskExecutionArtifacts } from '../../composables/useTaskExecutionArtifacts'
defineProps<{ owner: TaskExecutionArtifacts }>()
</script>
<template>
  <section
    v-if="owner.visible.value"
    class="execution-artifacts"
    aria-label="执行成果"
    :aria-busy="owner.state.active !== 0"
  >
    <div class="artifact-actions">
      <h5>执行成果</h5>
      <UiButton :disabled="owner.blocked.value" @click="owner.load()">
        {{ owner.state.phase === 'idle' ? '查看执行成果' : '刷新执行成果' }}
      </UiButton>
    </div>
    <p>成果属于当前选中的执行；接受或退回任务仍需明确提交审核操作。</p>
    <UiState v-if="owner.state.phase === 'loading'" kind="loading" title="正在读取执行成果" />
    <UiState
      v-else-if="owner.state.phase === 'error'"
      kind="error"
      title="未能读取执行成果"
      :description="owner.state.message"
    />
    <UiState
      v-else-if="owner.state.phase === 'ready' && !owner.state.items.length"
      kind="empty"
      title="此次执行尚无已发布成果"
    />
    <ul v-if="owner.state.items.length" class="artifact-list">
      <li v-for="item in owner.state.items" :key="item.artifact_ref.artifact_id">
        <UiButton
          :disabled="owner.blocked.value"
          :aria-pressed="
            owner.state.selected?.artifact_ref.artifact_id === item.artifact_ref.artifact_id
          "
          @click="owner.select(item.artifact_ref)"
          >{{ item.name }}</UiButton
        >
        <span>{{ item.object.media_type }} · {{ item.object.byte_size }} 字节</span>
      </li>
    </ul>
    <UiButton v-if="owner.state.next" :disabled="owner.blocked.value" @click="owner.load(true)"
      >更多执行成果</UiButton
    >
    <section v-if="owner.state.selected" aria-label="成果内容">
      <h6>{{ owner.state.selected.name }}</h6>
      <UiState
        v-if="owner.state.contentPhase === 'loading'"
        kind="loading"
        title="正在读取成果内容"
      />
      <UiState
        v-else-if="owner.state.contentPhase === 'error'"
        kind="error"
        title="未能读取成果内容"
        :description="owner.state.contentMessage"
      >
        <UiButton
          :disabled="owner.blocked.value"
          @click="owner.select(owner.state.selected.artifact_ref)"
          >重新读取成果内容</UiButton
        >
      </UiState>
      <template v-else-if="owner.state.content">
        <pre v-if="owner.state.content.projection === 'text'" class="artifact-content">{{
          owner.state.content.text
        }}</pre>
        <p v-else>此文件没有可显示的文本预览。</p>
        <div v-if="owner.state.content.projection === 'text'" class="artifact-actions">
          <span
            >字节 {{ owner.state.content.offset }}–{{ owner.state.content.next_offset }} /
            {{ owner.state.content.artifact.object.byte_size }}</span
          >
          <UiButton
            v-if="owner.state.pageIndex > 0"
            :disabled="owner.blocked.value"
            @click="owner.page('previous')"
            >上一段成果内容</UiButton
          >
          <UiButton
            v-if="owner.state.content.truncated"
            :disabled="owner.blocked.value"
            @click="owner.page('next')"
            >下一段成果内容</UiButton
          >
        </div>
        <details>
          <summary>成果来源与校验信息</summary>
          <dl>
            <dt>执行 ID</dt>
            <dd>{{ owner.state.content.artifact.source_execution_id }}</dd>
            <dt>操作 ID</dt>
            <dd>{{ owner.state.content.artifact.source_operation_id ?? '未提供' }}</dd>
            <dt>成果 ID</dt>
            <dd>{{ owner.state.content.artifact.artifact_ref.artifact_id }}</dd>
            <dt>SHA-256</dt>
            <dd>{{ owner.state.content.artifact.object.sha256 }}</dd>
          </dl>
        </details>
      </template>
    </section>
  </section>
</template>
<style scoped>
.execution-artifacts {
  display: grid;
  gap: var(--content-gap);
  border-top: 1px solid var(--border);
  padding-top: var(--space);
  min-width: 0;
}
.artifact-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.artifact-list {
  list-style: none;
  padding: 0;
  display: grid;
  gap: var(--content-gap);
}
.artifact-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
.artifact-content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 28rem;
  overflow: auto;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
