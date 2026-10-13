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
    !props.owner.canReorderMilestone.value ||
    props.owner.milestoneReorderStale.value,
)
const options = computed(() =>
  props.owner.milestoneCandidates.items.map((m) => ({
    value: m.id,
    label: m.title,
  })),
)
const modes = [
  { value: 'before', label: '移到指定 Milestone 之前' },
  { value: 'tail', label: '移到组尾' },
]
onMounted(() => heading.value?.focus())
</script>
<template>
  <form
    class="milestone-reorder-form"
    aria-label="调整 Milestone 顺序"
    @submit.prevent="emit('submit')"
  >
    <h3 ref="heading" tabindex="-1">调整 Milestone 顺序</h3>
    <p>调整当前项目中的位置，不改变 Milestone 内容或所属 Sprint。</p>
    <UiField label="排序位置" v-slot="field">
      <UiSelect
        :id="field.id"
        label="排序位置"
        :options="modes"
        :model-value="owner.milestoneReorder.mode"
        :disabled="disabled"
        @update:model-value="owner.setMilestoneReorderMode($event)"
      />
    </UiField>
    <template v-if="owner.milestoneReorder.mode === 'before'">
      <UiField label="目标 Milestone" hint="当前 Milestone 将移到所选对象之前。" v-slot="field">
        <UiSelect
          :id="field.id"
          label="目标 Milestone"
          :options="options"
          :model-value="owner.milestoneReorder.beforeID"
          :disabled="disabled || owner.milestoneCandidates.phase !== 'ready'"
          @update:model-value="owner.chooseMilestoneBefore($event)"
        />
      </UiField>
      <p v-if="owner.milestoneCandidates.phase === 'loading'" role="status">
        正在读取候选 Milestone。
      </p>
      <p v-if="owner.milestoneCandidates.phase === 'ready' && !options.length">
        本页没有其他 Milestone，可继续翻页或选择组尾。
      </p>
      <p v-if="owner.milestoneCandidates.message" role="status">
        {{ owner.milestoneCandidates.message }}
      </p>
      <div class="milestone-reorder-actions" aria-label="排序候选分页">
        <template v-if="owner.milestoneCandidates.phase !== 'stale'">
          <UiButton
            type="button"
            :disabled="disabled || owner.milestoneCandidates.page === 0"
            @click="owner.loadMilestoneCandidates(owner.milestoneCandidates.page - 1)"
            >上一页候选</UiButton
          >
          <span>第 {{ owner.milestoneCandidates.page + 1 }} 页</span>
          <UiButton
            type="button"
            :disabled="disabled || !owner.milestoneCandidates.next"
            @click="owner.loadMilestoneCandidates(owner.milestoneCandidates.page + 1)"
            >下一页候选</UiButton
          >
        </template>
        <UiButton
          v-if="owner.milestoneCandidates.phase === 'stale'"
          type="button"
          :disabled="disabled"
          @click="owner.loadMilestoneCandidates(0, true)"
          >从首页重新读取</UiButton
        >
        <UiButton
          v-if="owner.milestoneCandidates.phase === 'error'"
          type="button"
          :disabled="disabled"
          @click="owner.loadMilestoneCandidates(owner.milestoneCandidates.page)"
          >重新读取本页</UiButton
        >
      </div>
    </template>
    <p v-else>将当前 Milestone 移到本项目 Milestone 列表的末尾。</p>
    <p v-if="owner.milestoneReorder.beforeID && owner.milestoneReorder.mode === 'before'">
      移到“{{ owner.milestoneReorder.beforeTitle }}”之前。
    </p>
    <p v-if="owner.milestoneReorderStale.value" role="alert">
      Milestone 已发生变化，原版本和排序选择已保留。请重新读取并核对，再明确采用当前版本。
    </p>
    <p v-if="owner.milestoneReorder.message" role="status">{{ owner.milestoneReorder.message }}</p>
    <div v-if="owner.milestoneReorderStale.value" class="milestone-reorder-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.milestoneReorderPending.value"
        @click="owner.reloadMilestoneReorder()"
        >重新读取排序对象</UiButton
      >
      <UiButton
        type="button"
        :disabled="
          owner.busy.value ||
          !owner.canReorderMilestone.value ||
          !owner.milestoneReorder.readVersion
        "
        @click="owner.adoptMilestoneReorderVersion()"
        >使用当前版本继续排序</UiButton
      >
    </div>
    <div class="milestone-reorder-actions">
      <UiButton
        type="submit"
        variant="primary"
        :disabled="disabled || !owner.milestoneReorderReady.value"
        >确认调整顺序</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.milestoneReorderPending.value"
        @click="emit('cancel')"
        >取消调整顺序</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.milestone-reorder-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.milestone-reorder-form h3,
.milestone-reorder-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.milestone-reorder-form p {
  color: var(--muted);
}
.milestone-reorder-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
</style>
