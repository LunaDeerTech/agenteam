<script setup lang="ts">
import { computed, ref } from 'vue'
import type { MeetingContribution } from '../../api/meetings'
import { UiButton, UiDialog } from '../ui'
const props = withDefaults(
  defineProps<{
    contribution: MeetingContribution
    mutable: boolean
    canReply?: boolean
    participantName: (id: string) => string
  }>(),
  { canReply: undefined },
)
const emit = defineEmits<{
  reply: [messageID: string]
  restart: [
    contribution: MeetingContribution,
    action: 'retry-contribution' | 'regenerate-contribution',
  ]
}>()
const captured = ref<MeetingContribution | null>(null)
const action = computed(() =>
  props.contribution.status === 'failed' ? 'retry-contribution' : 'regenerate-contribution',
)
const available = computed(
  () =>
    props.contribution.kind === 'agent' &&
    !!props.contribution.execution_id &&
    !props.contribution.cancel_requested &&
    ['failed', 'completed'].includes(props.contribution.status),
)
const unchanged = computed(
  () =>
    !!captured.value &&
    props.mutable &&
    available.value &&
    captured.value.execution_id === props.contribution.execution_id &&
    captured.value.generation === props.contribution.generation &&
    captured.value.attempt === props.contribution.attempt &&
    captured.value.status === props.contribution.status,
)
const history = computed(() =>
  props.contribution.history.filter((h) => h.execution_id !== props.contribution.execution_id),
)
const labels = {
  created: '已创建',
  preparing: '准备中',
  running: '运行中',
  waiting: '等待处理',
  succeeded: '已完成',
  failed: '失败',
  cancelled: '已取消',
}
function confirm() {
  if (!unchanged.value || !captured.value) return
  const target = captured.value
  captured.value = null
  emit('restart', target, action.value)
}
function reply(messageID: string) {
  if (!props.canReply || !history.value.some((item) => item.message?.id === messageID)) return
  emit('reply', messageID)
}
</script>
<template>
  <div class="contribution-versions">
    <p
      v-if="
        contribution.message &&
        ['pending', 'waiting_for_agent', 'running'].includes(contribution.status)
      "
      class="notice"
      role="status"
    >
      新回复生成中，当前仍显示上一次成功的回复。
    </p>
    <UiButton
      v-if="available"
      variant="ghost"
      :disabled="!mutable"
      @click="captured = contribution"
    >
      {{ action === 'retry-contribution' ? '重试此 Agent' : '重新生成回复' }}
    </UiButton>
    <details v-if="history.length" class="history">
      <summary>历史版本（{{ history.length }}）</summary>
      <ol>
        <li v-for="item in history" :key="item.execution_id">
          <p class="history-heading">
            第 {{ item.generation }} 版 · 第 {{ item.attempt }} 次尝试 · {{ labels[item.status] }}
            <span v-if="item.retry_of_execution_id"> · 重试</span>
            <span v-else-if="item.regenerate_of_execution_id"> · 重新生成</span>
          </p>
          <p v-if="item.message" class="history-message">
            <template v-for="(node, index) in item.message.content_nodes" :key="index">
              <span v-if="node.type === 'text'">{{ node.text }}</span>
              <span v-else-if="node.type === 'mention'"
                >@{{ participantName(node.participant_id || '') }}</span
              >
              <span v-else>{{ node.reference.label || node.reference.id }}</span>
            </template>
          </p>
          <p v-else class="muted">这次尝试没有发布回复。</p>
          <UiButton
            v-if="item.message && canReply !== undefined"
            variant="ghost"
            :disabled="!canReply"
            :aria-label="'回复第 ' + item.generation + ' 版消息'"
            @click="reply(item.message.id)"
            >回复此版本</UiButton
          >
        </li>
      </ol>
    </details>
    <UiDialog
      :open="captured !== null"
      :title="captured?.status === 'failed' ? '重试此 Agent？' : '重新生成回复？'"
      @update:open="!$event && (captured = null)"
    >
      <p v-if="captured?.status === 'failed'">
        使用这次发言的原输入重试。其他 Agent 的回复保持不变。
      </p>
      <p v-else>
        根据当前可用的会议上下文生成新回复。成功后替换当前回复，旧版本仍可查看；其他 Agent
        不会重新发言。
      </p>
      <p>已有摘要会在下一轮正常讨论结束后更新。</p>
      <p v-if="!unchanged" class="notice" role="status">会议状态已变化，请关闭后重新读取。</p>
      <template #footer>
        <UiButton @click="captured = null">返回</UiButton>
        <UiButton variant="primary" :disabled="!unchanged" @click="confirm">{{
          captured?.status === 'failed' ? '确认重试' : '确认重新生成'
        }}</UiButton>
      </template>
    </UiDialog>
  </div>
</template>
<style scoped>
.contribution-versions {
  display: grid;
  gap: 8px;
  justify-items: start;
}
.history {
  width: 100%;
  font-size: 13px;
}
summary {
  cursor: pointer;
  color: var(--muted);
  padding: 8px 0;
}
summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
ol {
  list-style: none;
  display: grid;
  gap: 16px;
  margin: 4px 0;
  padding: 0 0 0 12px;
  border-left: 1px solid var(--border);
}
p {
  margin: 4px 0;
}
.history-heading,
.muted {
  color: var(--muted);
  font-size: 12px;
}
.history-message {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.notice {
  color: var(--warning);
}
</style>
