<script setup lang="ts">
import { UiButton, UiState } from '../../components/ui'
import ProjectSchedulerForm from '../../components/projects/ProjectSchedulerForm.vue'
import { useProjectScheduler } from '../../composables/useProjectScheduler'
const owner = useProjectScheduler()
const { state, current, draft, visible, progress, busy, readOnly, pending, canAdopt } = owner
</script>
<template>
  <section class="scheduler-settings" aria-labelledby="scheduler-heading">
    <header class="page-heading">
      <h1 id="scheduler-heading">调度配置</h1>
      <p class="meta">管理自动调度与同时运行的数量。</p>
    </header>
    <template v-if="visible">
      <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取调度配置" />
      <dl v-if="current" class="scheduler-facts">
        <div>
          <dt>当前自动调度</dt>
          <dd>{{ current.scheduler_enabled ? '已启用' : '已暂停' }}</dd>
        </div>
        <div>
          <dt>当前并发上限</dt>
          <dd>{{ current.scheduler_max_concurrency ?? '不限制' }}</dd>
        </div>
      </dl>
      <ProjectSchedulerForm
        v-if="draft.expected_version && !state.denied"
        :draft="draft"
        :busy="busy"
        :read-only="readOnly"
        :locked="pending || state.conflict || state.needsRead"
        @submit="owner.submit"
        @cancel="owner.cancelEdits"
      />
      <p v-if="state.message" role="status">{{ state.message }}</p>
      <template v-if="progress?.phase === 'uncertain'">
        <h3>保存结果尚未确认</h3>
        <p v-if="progress.observation === 'in_progress'">原操作仍在处理中，请稍后再次查证。</p>
        <p v-else-if="progress.observation === 'not_observed'">
          此次未观察到原操作，不能据此判断它未提交。
        </p>
        <p>查证只确认原操作的历史结果；当前配置仍需重新读取。</p>
        <div class="scheduler-actions">
          <UiButton :disabled="busy || !progress.contextValid" @click="owner.lookupOriginal"
            >查证原操作</UiButton
          >
          <UiButton
            :disabled="busy || readOnly || !progress.canRetryOriginal"
            @click="owner.replayOriginal"
            >按原请求重放</UiButton
          >
          <UiButton :disabled="busy" variant="ghost" @click="owner.abandonOriginal"
            >放弃本地追踪</UiButton
          >
        </div>
      </template>
      <template v-if="state.conflict">
        <p>原输入及版本已保留。先读取当前配置，核对后才能采用当前版本再次保存。</p>
        <UiButton :disabled="!canAdopt" @click="owner.adoptCurrentVersion"
          >采用当前版本并保留输入</UiButton
        >
      </template>
      <UiButton :disabled="busy || pending || state.denied" @click="owner.readCurrent"
        >重新读取当前配置</UiButton
      >
    </template>
    <UiState v-else kind="loading" title="正在确认当前项目与会话" />
  </section>
</template>
<style scoped>
.meta {
  color: var(--muted);
}
.scheduler-facts {
  padding: 16px 0;
  border-block: 1px solid var(--border);
}
.scheduler-settings {
  display: grid;
  gap: var(--section-gap);
  min-width: 0;
}
.scheduler-settings h1,
.scheduler-settings h3,
.scheduler-settings p {
  margin: 0;
  overflow-wrap: anywhere;
}
.scheduler-facts {
  display: grid;
  gap: var(--content-gap);
  margin: 0;
}
.scheduler-facts > div {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
.scheduler-facts dt {
  color: var(--muted);
}
.scheduler-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.scheduler-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
