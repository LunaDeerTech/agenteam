<script setup lang="ts">
import { UiButton, UiState } from '../ui'
import type { TaskExecutions } from '../../composables/useTaskExecutions'
defineProps<{ owner: TaskExecutions }>()
defineEmits<{ load: [event: MouseEvent] }>()
</script>
<template>
  <section class="execution-output" aria-label="最终输出" :aria-busy="owner.state.outputPhase === 'loading'">
    <div class="output-heading">
      <h5>最终输出</h5>
      <UiButton v-if="owner.state.outputPhase !== 'error'" :disabled="owner.blocked.value" @click="$emit('load', $event)">{{ owner.state.outputPhase === 'idle' ? '加载最终输出' : '刷新最终输出' }}</UiButton>
    </div>
    <p class="output-note">读取此执行已保存的最终结果。</p>
    <UiState v-if="owner.state.outputPhase === 'loading'" kind="loading" title="正在读取最终输出" />
    <UiState v-else-if="owner.state.outputPhase === 'error'" kind="error" title="未能读取最终输出" :description="owner.state.outputMessage">
      <UiButton :disabled="owner.blocked.value" @click="$emit('load', $event)">重试最终输出</UiButton>
    </UiState>
    <template v-else-if="owner.state.output">
      <UiState v-if="owner.state.output.availability === 'pending'" kind="empty" title="尚未生成最终输出" description="此执行尚未结束，可稍后刷新。" />
      <UiState v-else-if="owner.state.output.availability === 'not_recorded'" kind="empty" title="未记录最终输出" description="此执行已结束，但没有已保存的最终输出记录。" />
      <template v-else>
        <template v-for="item in owner.state.output.snapshot?.items" :key="item.id">
          <pre v-if="item.data.content.length" class="output-text" tabindex="0" aria-label="最终输出正文">{{ item.data.content }}</pre>
          <UiState v-else kind="empty" title="最终输出为空文本" description="执行成功，已保存的最终文本为空。" />
        </template>
        <UiState v-if="!owner.state.output.snapshot?.items.length" kind="empty" title="执行已结束，无最终文本" description="结束状态和原因见上方执行详情。" />
      </template>
    </template>
  </section>
</template>
<style scoped>
.execution-output { display: flex; flex-direction: column; gap: var(--content-gap); min-width: 0; border-top: 1px solid var(--border); padding-top: var(--space); }
.output-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--content-gap); flex-wrap: wrap; }
.output-heading h5 { font-size: 1rem; margin: 0; }
.output-note { color: var(--muted); font-size: 0.875rem; margin: 0; }
.output-text { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; tab-size: 4; font: inherit; line-height: 1.6; min-width: 0; }
</style>
