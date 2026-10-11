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
const disabled = computed(
  () => props.owner.busy.value || !props.owner.canEdit.value || props.owner.editStale.value,
)
onMounted(() => form.value?.querySelector('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="task-edit-form"
    aria-label="编辑任务"
    @submit.prevent="owner.submitEdit()"
  >
    <h3>编辑任务</h3>
    <p>只保存本次编辑的字段。描述与执行计划可以明确清空。</p>
    <UiField label="任务标题" required v-slot="field">
      <UiInput
        :id="field.id"
        :model-value="owner.edit.title"
        :disabled="disabled"
        @update:model-value="owner.touchEdit('title', $event)"
      />
    </UiField>
    <UiField label="任务类型" required v-slot="field">
      <UiSelect
        :id="field.id"
        :model-value="owner.edit.type"
        label="任务类型"
        :options="types"
        :disabled="disabled"
        @update:model-value="owner.touchEdit('type', $event)"
      />
    </UiField>
    <UiField label="优先级" required v-slot="field">
      <UiSelect
        :id="field.id"
        :model-value="owner.edit.priority"
        label="优先级"
        :options="priorities"
        :disabled="disabled"
        @update:model-value="owner.touchEdit('priority', $event)"
      />
    </UiField>
    <UiField label="任务描述" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.description"
        :disabled="disabled"
        rows="4"
        @update:model-value="owner.touchEdit('description', $event)"
      />
    </UiField>
    <UiField label="执行计划" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.plan"
        :disabled="disabled"
        rows="4"
        @update:model-value="owner.touchEdit('plan', $event)"
      />
    </UiField>
    <p v-if="owner.editStale.value" role="alert">
      任务已有新版本。请核对当前详情，取消编辑后重新填写；不会自动覆盖原版本。
    </p>
    <p v-if="owner.edit.message" role="alert">{{ owner.edit.message }}</p>
    <div class="task-edit-actions">
      <UiButton
        type="submit"
        :disabled="disabled || !owner.editDirty.value || !owner.edit.title.trim()"
        >保存任务</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.pending.value"
        @click="emit('cancel')"
        >取消编辑</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.task-edit-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.task-edit-form h3,
.task-edit-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.task-edit-form p {
  color: var(--muted);
}
.task-edit-actions {
  display: flex;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
</style>
