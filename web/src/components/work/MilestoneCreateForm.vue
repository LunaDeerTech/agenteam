<script setup lang="ts">
import { computed, useId } from 'vue'
import { UiButton, UiDialog, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks; fallbackFocus?: HTMLElement | null }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const formID = useId()
const disabled = computed(() => props.owner.busy.value || !props.owner.canCreateMilestone.value)
const canSubmit = computed(() => !disabled.value && !!props.owner.milestoneCreation.title.trim())
</script>
<template>
  <UiDialog
    :open="owner.milestoneCreation.open && !owner.milestonePending.value"
    title="新建 Milestone"
    :close-on-outside="false"
    :close-on-escape="!owner.busy.value"
    :fallback-focus="fallbackFocus"
    @update:open="!$event && emit('cancel')"
  >
    <form
      :id="formID"
      class="milestone-create-form"
      aria-label="新建 Milestone"
      @submit.prevent="emit('submit')"
    >
      <p>创建 Milestone 后，可单独规划 Sprint。</p>
      <UiField label="Milestone 标题" required v-slot="field"
        ><UiInput
          data-autofocus
          :id="field.id"
          v-model="owner.milestoneCreation.title"
          :disabled="disabled"
      /></UiField>
      <UiField label="Milestone 描述" v-slot="field"
        ><UiTextarea
          :id="field.id"
          v-model="owner.milestoneCreation.description"
          :disabled="disabled"
          rows="4"
      /></UiField>
      <p v-if="owner.milestoneCreation.message" role="alert">
        {{ owner.milestoneCreation.message }}
      </p>
    </form>
    <template #footer>
      <div class="milestone-create-actions">
        <UiButton type="submit" :form="formID" variant="primary" :disabled="!canSubmit"
          >确认创建 Milestone</UiButton
        ><UiButton type="button" :disabled="disabled" @click="emit('cancel')"
          >取消创建 Milestone</UiButton
        >
      </div>
    </template>
  </UiDialog>
</template>
<style scoped>
.milestone-create-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.milestone-create-form h3,
.milestone-create-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.milestone-create-form p {
  color: var(--muted);
}
.milestone-create-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
