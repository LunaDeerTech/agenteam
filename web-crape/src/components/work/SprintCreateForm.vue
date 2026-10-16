<script setup lang="ts">
import { computed, useId } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks; fallbackFocus?: HTMLElement | null }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const formID = useId()
const disabled = computed(() => props.owner.busy.value || !props.owner.canCreateSprint.value)
const canSubmit = computed(
  () =>
    !disabled.value &&
    !!props.owner.sprintCreation.title.trim() &&
    props.owner.sprintMilestoneOptions.value.some(
      (m) => m.value === props.owner.sprintCreation.milestoneID,
    ),
)
function chooseMilestone(value: string) {
  if (!disabled.value && props.owner.sprintMilestoneOptions.value.some((m) => m.value === value))
    props.owner.sprintCreation.milestoneID = value
}
</script>
<template>
  <UiDialog
    :open="owner.sprintCreation.open && !owner.sprintCreatePending.value"
    title="新建 Sprint"
    :close-on-outside="false"
    :close-on-escape="!owner.busy.value"
    :fallback-focus="fallbackFocus"
    @update:open="!$event && emit('cancel')"
  >
    <form
      :id="formID"
      class="sprint-create-form"
      aria-label="新建 Sprint"
      @submit.prevent="emit('submit')"
    >
      <p>创建到所选 Milestone，初始状态为未启动。已有当前 Sprint 时仍可规划后续 Sprint。</p>
      <UiField label="所属 Milestone" required v-slot="field"
        ><UiSelect
          :id="field.id"
          :model-value="owner.sprintCreation.milestoneID"
          label="所属 Milestone"
          :options="owner.sprintMilestoneOptions.value"
          :disabled="disabled"
          @update:model-value="chooseMilestone"
      /></UiField>
      <UiButton
        v-if="owner.state.milestoneNext"
        type="button"
        :disabled="disabled"
        @click="owner.loadMilestones(true)"
        >加载更多 Milestone</UiButton
      >
      <UiField label="Sprint 标题" required v-slot="field"
        ><UiInput
          data-autofocus
          :id="field.id"
          v-model="owner.sprintCreation.title"
          :disabled="disabled"
      /></UiField>
      <UiField label="Sprint 描述" v-slot="field"
        ><UiTextarea
          :id="field.id"
          v-model="owner.sprintCreation.description"
          :disabled="disabled"
          rows="4"
      /></UiField>
      <p v-if="owner.sprintCreation.message" role="alert">{{ owner.sprintCreation.message }}</p>
    </form>
    <template #footer>
      <div class="sprint-create-actions">
        <UiButton type="submit" :form="formID" variant="primary" :disabled="!canSubmit"
          >确认创建 Sprint</UiButton
        ><UiButton type="button" :disabled="disabled" @click="emit('cancel')"
          >取消创建 Sprint</UiButton
        >
      </div>
    </template>
  </UiDialog>
</template>
<style scoped>
.sprint-create-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.sprint-create-form h3,
.sprint-create-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.sprint-create-form p {
  color: var(--muted);
}
.sprint-create-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
