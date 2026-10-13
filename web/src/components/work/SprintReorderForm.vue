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
    !props.owner.canReorderSprint.value ||
    props.owner.sprintReorderStale.value,
)
const options = computed(() =>
  props.owner.sprintCandidates.items.map((m) => ({
    value: m.id,
    label: m.title,
  })),
)
const modes = [
  { value: 'before', label: '移到指定 Sprint 之前' },
  { value: 'tail', label: '移到组尾' },
]
onMounted(() => heading.value?.focus())
</script>
<template>
  <form class="sprint-reorder-form" aria-label="调整 Sprint 顺序" @submit.prevent="emit('submit')">
    <h3 ref="heading" tabindex="-1">调整 Sprint 顺序</h3>
    <p>只调整当前 Milestone 内的顺序，不改变 Sprint 内容、任务或当前迭代。</p>
    <UiField label="排序位置" v-slot="field">
      <UiSelect
        :id="field.id"
        label="排序位置"
        :options="modes"
        :model-value="owner.sprintReorder.mode"
        :disabled="disabled"
        @update:model-value="owner.setSprintReorderMode($event)"
      />
    </UiField>
    <template v-if="owner.sprintReorder.mode === 'before'">
      <UiField label="目标 Sprint" hint="当前 Sprint 将移到所选对象之前。" v-slot="field">
        <UiSelect
          :id="field.id"
          label="目标 Sprint"
          :options="options"
          :model-value="owner.sprintReorder.beforeID"
          :disabled="disabled || owner.sprintCandidates.phase !== 'ready'"
          @update:model-value="owner.chooseSprintBefore($event)"
        />
      </UiField>
      <p v-if="owner.sprintCandidates.phase === 'loading'" role="status">正在读取候选 Sprint。</p>
      <p v-if="owner.sprintCandidates.phase === 'ready' && !options.length">
        本页没有其他 Sprint，可继续翻页或选择组尾。
      </p>
      <p v-if="owner.sprintCandidates.message" role="status">
        {{ owner.sprintCandidates.message }}
      </p>
      <div class="sprint-reorder-actions" aria-label="排序候选分页">
        <template v-if="owner.sprintCandidates.phase !== 'stale'">
          <UiButton
            type="button"
            :disabled="disabled || owner.sprintCandidates.page === 0"
            @click="owner.loadSprintCandidates(owner.sprintCandidates.page - 1)"
            >上一页候选</UiButton
          >
          <span>第 {{ owner.sprintCandidates.page + 1 }} 页</span>
          <UiButton
            type="button"
            :disabled="disabled || !owner.sprintCandidates.next"
            @click="owner.loadSprintCandidates(owner.sprintCandidates.page + 1)"
            >下一页候选</UiButton
          >
        </template>
        <UiButton
          v-if="owner.sprintCandidates.phase === 'stale'"
          type="button"
          :disabled="disabled"
          @click="owner.loadSprintCandidates(0, true)"
          >从首页重新读取</UiButton
        >
        <UiButton
          v-if="owner.sprintCandidates.phase === 'error'"
          type="button"
          :disabled="disabled"
          @click="owner.loadSprintCandidates(owner.sprintCandidates.page)"
          >重新读取本页</UiButton
        >
      </div>
    </template>
    <p v-else>将当前 Sprint 移到所属 Milestone 的 Sprint 列表末尾。</p>
    <p v-if="owner.sprintReorder.beforeID && owner.sprintReorder.mode === 'before'">
      移到“{{ owner.sprintReorder.beforeTitle }}”之前。
    </p>
    <p v-if="owner.state.sprint?.state === 'completed'" role="alert">
      Sprint 已完成，不能调整顺序。原选择已保留，可取消排序。
    </p>
    <p v-else-if="owner.sprintReorderStale.value" role="alert">
      Sprint 已发生变化，原版本和排序选择已保留。请重新读取并核对，再明确采用当前版本。
    </p>
    <p v-if="owner.sprintReorder.message" role="status">{{ owner.sprintReorder.message }}</p>
    <div v-if="owner.sprintReorderStale.value" class="sprint-reorder-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.sprintReorderPending.value"
        @click="owner.reloadSprintReorder()"
        >重新读取排序对象</UiButton
      >
      <UiButton
        type="button"
        :disabled="
          owner.busy.value || !owner.canReorderSprint.value || !owner.sprintReorder.readVersion
        "
        @click="owner.adoptSprintReorderVersion()"
        >使用当前版本继续排序</UiButton
      >
    </div>
    <div class="sprint-reorder-actions">
      <UiButton
        type="submit"
        variant="primary"
        :disabled="disabled || !owner.sprintReorderReady.value"
        >确认调整顺序</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.sprintReorderPending.value"
        @click="emit('cancel')"
        >取消调整顺序</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.sprint-reorder-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.sprint-reorder-form h3,
.sprint-reorder-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.sprint-reorder-form p {
  color: var(--muted);
}
.sprint-reorder-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
</style>
