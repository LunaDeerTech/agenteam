<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(() => props.owner.busy.value || !props.owner.canCreateMilestone.value)
const canSubmit = computed(() => !disabled.value && !!props.owner.milestoneCreation.title.trim())
onMounted(() => form.value?.querySelector<HTMLElement>('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="milestone-create-form"
    aria-label="新建 Milestone"
    @submit.prevent="emit('submit')"
  >
    <h3>新建 Milestone</h3>
    <p>创建 Milestone 后，可单独规划 Sprint。</p>
    <UiField label="Milestone 标题" required v-slot="field"
      ><UiInput :id="field.id" v-model="owner.milestoneCreation.title" :disabled="disabled"
    /></UiField>
    <UiField label="Milestone 描述" v-slot="field"
      ><UiTextarea
        :id="field.id"
        v-model="owner.milestoneCreation.description"
        :disabled="disabled"
        rows="4"
    /></UiField>
    <p v-if="owner.milestoneCreation.message" role="alert">{{ owner.milestoneCreation.message }}</p>
    <div class="milestone-create-actions">
      <UiButton type="submit" :disabled="!canSubmit">确认创建 Milestone</UiButton
      ><UiButton type="button" :disabled="disabled" @click="emit('cancel')"
        >取消创建 Milestone</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.milestone-create-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
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
