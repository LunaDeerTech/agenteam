<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: [] }>()
const form = ref<HTMLFormElement | null>(null)
const types = [
  { value: 'feature', label: '功能' },
  { value: 'bug', label: '缺陷' },
  { value: 'task', label: '任务' },
  { value: 'spike', label: '调研' },
  { value: 'chore', label: '维护' },
]
const priorities = [
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
]
const disabled = computed(() => props.owner.busy.value || !props.owner.canCreate.value)
const canSubmit = computed(
  () =>
    !disabled.value &&
    !!props.owner.creation.title.trim() &&
    !!props.owner.creation.type &&
    !!props.owner.creation.priority,
)
function selectType(value: string) {
  if (types.some((item) => item.value === value))
    props.owner.creation.type = value as typeof props.owner.creation.type
}
function selectPriority(value: string) {
  if (priorities.some((item) => item.value === value))
    props.owner.creation.priority = value as typeof props.owner.creation.priority
}
onMounted(() => form.value?.querySelector('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="task-create-form"
    aria-label="新建任务"
    @submit.prevent="owner.submitCreation()"
  >
    <h3>新建任务</h3>
    <p>任务将创建到当前 Sprint 的待规划列。创建确认后，再选择 Agent 加入待执行。</p>
    <UiField label="任务标题" required v-slot="field"
      ><UiInput :id="field.id" v-model="owner.creation.title" :disabled="disabled"
    /></UiField>
    <div class="task-create-properties">
      <UiField label="任务类型" required v-slot="field"
        ><UiSelect
          :id="field.id"
          :model-value="owner.creation.type"
          label="任务类型"
          :options="types"
          :disabled="disabled"
          @update:model-value="selectType"
      /></UiField>
      <UiField label="优先级" required v-slot="field"
        ><UiSelect
          :id="field.id"
          :model-value="owner.creation.priority"
          label="优先级"
          :options="priorities"
          :disabled="disabled"
          @update:model-value="selectPriority"
      /></UiField>
    </div>
    <UiField label="任务描述" v-slot="field"
      ><UiTextarea
        :id="field.id"
        v-model="owner.creation.description"
        :disabled="disabled"
        rows="4"
    /></UiField>
    <UiField label="执行计划" v-slot="field"
      ><UiTextarea :id="field.id" v-model="owner.creation.plan" :disabled="disabled" rows="4"
    /></UiField>
    <p v-if="owner.creation.message" role="alert">{{ owner.creation.message }}</p>
    <div class="task-create-actions">
      <UiButton type="submit" :disabled="!canSubmit">确认创建任务</UiButton
      ><UiButton type="button" :disabled="disabled" @click="emit('cancel')">取消创建</UiButton>
    </div>
  </form>
</template>
<style scoped>
.task-create-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.task-create-form h3,
.task-create-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.task-create-form p {
  color: var(--muted);
}
.task-create-properties {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space);
}
.task-create-actions {
  display: flex;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
@media (max-width: 640px) {
  .task-create-properties {
    grid-template-columns: 1fr;
  }
}
</style>
