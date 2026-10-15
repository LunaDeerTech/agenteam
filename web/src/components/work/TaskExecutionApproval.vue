<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { UiButton, UiState } from '../ui'
import type { TaskExecutionApproval } from '../../composables/useTaskExecutionApproval'
const props = defineProps<{ owner: TaskExecutionApproval }>()
const confirm = ref<{ $el: HTMLButtonElement } | null>(null)
async function choose(decision: 'approve' | 'reject') {
  if (!props.owner.choose(decision)) return
  await nextTick()
  confirm.value?.$el.focus()
}
</script>
<template>
  <section
    v-if="owner.visible.value"
    class="execution-approval"
    aria-label="执行审批"
    :aria-busy="owner.state.active !== 0"
  >
    <h5>执行审批</h5>
    <template v-if="owner.progress.value?.phase === 'uncertain'">
      <p role="status">
        原{{ owner.progress.value.decision === 'approve' ? '批准' : '拒绝' }}请求结果尚未确认。
      </p>
      <UiButton
        :disabled="owner.blocked.value || !owner.progress.value.contextValid"
        @click="owner.lookup()"
        >核对原审批决定</UiButton
      >
    </template>
    <template v-else-if="owner.progress.value?.phase === 'rejected'">
      <p>审批决定未提交，请重新读取执行后再决定。</p>
      <UiButton :disabled="owner.blocked.value" @click="owner.reread()">重新读取审批执行</UiButton>
    </template>
    <p v-else-if="owner.progress.value?.phase === 'confirmed'" role="status">
      已确认{{
        owner.progress.value.decision === 'approve' ? '批准' : '拒绝'
      }}此次操作。执行进度以刷新结果为准。
    </p>
    <template v-else>
      <p>当前执行正在等待此次工具操作的审批。</p>
      <UiButton :disabled="owner.blocked.value" @click="owner.load()">{{
        owner.state.phase === 'idle' ? '查看待审批操作' : '刷新待审批操作'
      }}</UiButton>
      <UiState v-if="owner.state.phase === 'loading'" kind="loading" title="正在读取审批请求" />
      <UiState
        v-else-if="owner.state.phase === 'error'"
        kind="error"
        title="未能读取审批请求"
        :description="owner.state.message"
      />
      <template v-else-if="owner.state.approval">
        <p>工具操作（{{ owner.state.approval.tool_name }}）</p>
        <p>本次决定仅适用于当前执行的这一次操作。</p>
        <p v-if="owner.state.approval.status !== 'pending'">
          审批已{{
            owner.state.approval.status === 'approved' ? '批准' : '拒绝'
          }}，请刷新执行详情查看进度。
        </p>
        <form
          v-else-if="owner.state.decision"
          aria-label="确认审批决定"
          @submit.prevent="owner.submit()"
        >
          <p>确认{{ owner.state.decision === 'approve' ? '批准' : '拒绝' }}此次工具操作？</p>
          <div class="approval-actions">
            <UiButton
              ref="confirm"
              type="submit"
              :variant="owner.state.decision === 'reject' ? 'danger' : 'primary'"
              :disabled="!owner.canDecide.value"
              >确认{{ owner.state.decision === 'approve' ? '批准' : '拒绝' }}此次操作</UiButton
            >
            <UiButton :disabled="owner.blocked.value" @click="owner.keep()">暂不决定</UiButton>
          </div>
        </form>
        <div v-else class="approval-actions">
          <UiButton :disabled="!owner.canDecide.value" @click="choose('approve')"
            >批准此次操作</UiButton
          >
          <UiButton variant="danger" :disabled="!owner.canDecide.value" @click="choose('reject')"
            >拒绝此次操作</UiButton
          >
        </div>
        <details>
          <summary>审批来源与操作身份</summary>
          <dl>
            <dt>执行 ID</dt>
            <dd>{{ owner.state.approval.execution_id }}</dd>
            <dt>操作 ID</dt>
            <dd>{{ owner.state.approval.operation_id }}</dd>
            <dt>审批 ID</dt>
            <dd>{{ owner.state.approval.id }}</dd>
          </dl>
        </details>
      </template>
    </template>
    <p v-if="owner.state.message && owner.state.phase !== 'error'" role="status">
      {{ owner.state.message }}
    </p>
  </section>
</template>
<style scoped>
.execution-approval,
form {
  display: grid;
  gap: var(--content-gap);
}
.execution-approval {
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
.execution-approval h5 {
  font-size: 1rem;
  margin: 0;
}
.approval-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
