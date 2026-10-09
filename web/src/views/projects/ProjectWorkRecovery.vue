<script setup lang="ts">
import { computed } from 'vue'
import { UiButton } from '../../components/ui'
import { useProjectWorkPlanning } from '../../composables/useProjectWorkPlanning'
const work = useProjectWorkPlanning()
const receipt = computed(() => work.progress.value?.receipt)
const historic = computed(() => {
  const value = receipt.value
  if (!value) return null
  return value.domain === 'structure'
    ? (value.value.milestone ?? value.value.sprint)
    : value.value.task
})
const phase = computed(
  () =>
    ({
      submitting: '正在提交原命令',
      uncertain: '原命令结果不确定',
      rejected: '原命令已明确拒绝',
      confirmed: '原命令已确认',
    })[work.progress.value?.phase ?? 'uncertain'],
)
</script>
<template>
  <section v-if="work.progress.value" class="work-recovery" aria-label="原命令恢复">
    <h2>{{ phase }}</h2>
    <p v-if="work.recoveryMessage.value" role="status">{{ work.recoveryMessage.value }}</p>
    <p v-if="work.progress.value.observation === 'in_progress'">
      本次查证仍在处理中；不会自动轮询或重放。
    </p>
    <p v-if="work.progress.value.observation === 'not_observed'">
      本次未观察到原命令，不代表从未提交或已经回滚。
    </p>
    <p v-if="work.progress.value.failure" role="alert">
      最近一次请求未成功完成。已确认的历史回执仍保留；不确定结果仍需查证。
    </p>
    <dl v-if="historic">
      <dt>原命令历史标题</dt>
      <dd>{{ historic.title }}</dd>
      <dt>历史版本</dt>
      <dd>{{ historic.version }}</dd>
      <dt>对象 ID</dt>
      <dd>{{ historic.id }}</dd>
      <template v-if="receipt?.domain === 'blocker'"
        ><dt>历史阻塞记录 ID</dt>
        <dd>{{ receipt.value.blocker.id }}</dd></template
      >
    </dl>
    <p v-if="historic" class="muted">
      这是原命令的历史回执；详情区通过另一请求读取当前内容，两者可能不同。
    </p>
    <div class="actions">
      <UiButton
        :disabled="work.blocked.value || !work.progress.value.canLookup"
        @click="work.checkOriginal"
        >查证原命令</UiButton
      >
      <UiButton
        :disabled="work.blocked.value || !work.progress.value.canReplay"
        @click="work.retryOriginal"
        >按原请求重放</UiButton
      >
      <UiButton variant="ghost" :disabled="work.blocked.value" @click="work.abandonOriginal"
        >放弃本地追踪</UiButton
      >
    </div>
  </section>
</template>
<style scoped>
.work-recovery {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--space);
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
h2 {
  margin: 0;
  font-size: 16px;
}
p,
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
dl {
  margin: 0;
  display: grid;
  grid-template-columns: minmax(100px, auto) minmax(0, 1fr);
  gap: var(--content-gap);
}
dt,
.muted {
  color: var(--muted);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
