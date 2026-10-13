<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiSelect } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const heading = ref<HTMLHeadingElement | null>(null)
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canReorderTask.value ||
    props.owner.taskReorderStale.value,
)
const options = computed(() =>
  props.owner.taskCandidates.items.map((m) => ({
    value: m.id,
    label: m.title,
  })),
)
const modes = [
  { value: 'before', label: '移到指定任务之前' },
  { value: 'tail', label: '移到组尾' },
]
onMounted(() => heading.value?.focus())
</script>
<template>
  <form class="task-reorder-form" aria-label="调整任务顺序" @submit.prevent="emit('submit')">
    <h3 ref="heading" tabindex="-1">调整任务顺序</h3>
    <p>只调整同一 Sprint、待规划状态和优先级分组内的顺序。候选不受看板搜索条件影响。</p>
    <UiField label="排序位置" v-slot="field">
      <UiSelect
        :id="field.id"
        label="排序位置"
        :options="modes"
        :model-value="owner.taskReorder.mode"
        :disabled="disabled"
        @update:model-value="owner.setTaskReorderMode($event)"
      />
    </UiField>
    <template v-if="owner.taskReorder.mode === 'before'">
      <UiField label="目标任务" hint="当前任务将移到所选对象之前。" v-slot="field">
        <UiSelect
          :id="field.id"
          label="目标任务"
          :options="options"
          :model-value="owner.taskReorder.beforeID"
          :disabled="disabled || owner.taskCandidates.phase !== 'ready'"
          @update:model-value="owner.chooseTaskBefore($event)"
        />
      </UiField>
      <p v-if="owner.taskCandidates.phase === 'loading'" role="status">正在读取候选任务。</p>
      <p v-if="owner.taskCandidates.phase === 'ready' && !options.length">
        本页没有其他任务，可继续翻页或选择组尾。
      </p>
      <p v-if="owner.taskCandidates.message" role="status">
        {{ owner.taskCandidates.message }}
      </p>
      <div class="task-reorder-actions" aria-label="排序候选分页">
        <template v-if="owner.taskCandidates.phase !== 'stale'">
          <UiButton
            type="button"
            :disabled="disabled || owner.taskCandidates.page === 0"
            @click="owner.loadTaskCandidates(owner.taskCandidates.page - 1)"
            >上一页候选</UiButton
          >
          <span>第 {{ owner.taskCandidates.page + 1 }} 页</span>
          <UiButton
            type="button"
            :disabled="disabled || !owner.taskCandidates.next"
            @click="owner.loadTaskCandidates(owner.taskCandidates.page + 1)"
            >下一页候选</UiButton
          >
        </template>
        <UiButton
          v-if="owner.taskCandidates.phase === 'stale'"
          type="button"
          :disabled="disabled"
          @click="owner.loadTaskCandidates(0, true)"
          >从首页重新读取</UiButton
        >
        <UiButton
          v-if="owner.taskCandidates.phase === 'error'"
          type="button"
          :disabled="disabled"
          @click="owner.loadTaskCandidates(owner.taskCandidates.page)"
          >重新读取本页</UiButton
        >
      </div>
    </template>
    <p v-else>将当前任务移到同一 Sprint、待规划状态和优先级分组的末尾。</p>
    <p v-if="owner.taskReorder.beforeID && owner.taskReorder.mode === 'before'">
      移到“{{ owner.taskReorder.beforeTitle }}”之前。
    </p>
    <p
      v-if="
        !owner.canReorderTask.value &&
        !owner.busy.value &&
        !owner.pending.value &&
        owner.state.detailCurrent
      "
      role="alert"
    >
      当前项目、Sprint 或任务状态不允许排序。原选择已保留，可取消排序。
    </p>
    <p v-else-if="owner.taskReorderStale.value" role="alert">
      任务已发生变化，原版本和排序选择已保留。请重新读取并核对，再明确采用当前版本。
    </p>
    <p v-if="owner.taskReorder.message" role="status">{{ owner.taskReorder.message }}</p>
    <div v-if="owner.taskReorderStale.value" class="task-reorder-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="owner.reloadTaskReorder()"
        >重新读取排序对象</UiButton
      >
      <UiButton
        type="button"
        :disabled="
          owner.busy.value || !owner.canReorderTask.value || !owner.taskReorder.readVersion
        "
        @click="owner.adoptTaskReorderVersion()"
        >使用当前版本继续排序</UiButton
      >
    </div>
    <div class="task-reorder-actions">
      <UiButton
        type="submit"
        variant="primary"
        :disabled="disabled || !owner.taskReorderReady.value"
        >确认调整顺序</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="emit('cancel')"
        >取消调整顺序</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.task-reorder-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.task-reorder-form h3,
.task-reorder-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.task-reorder-form p {
  color: var(--muted);
}
.task-reorder-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
</style>
