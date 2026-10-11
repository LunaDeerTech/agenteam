<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(() => props.owner.busy.value || !props.owner.canCreateSprint.value)
const canSubmit = computed(() => !disabled.value && !!props.owner.sprintCreation.title.trim() && props.owner.sprintMilestoneOptions.value.some((m) => m.value === props.owner.sprintCreation.milestoneID))
function chooseMilestone(value: string) {
  if (!disabled.value && props.owner.sprintMilestoneOptions.value.some((m) => m.value === value)) props.owner.sprintCreation.milestoneID = value
}
onMounted(() => form.value?.querySelector<HTMLElement>('button[role="combobox"], input')?.focus())
</script>
<template>
  <form ref="form" class="sprint-create-form" aria-label="新建 Sprint" @submit.prevent="emit('submit')">
    <h3>新建 Sprint</h3>
    <p>创建到所选 Milestone，初始状态为未启动。已有当前 Sprint 时仍可规划后续 Sprint。</p>
    <UiField label="所属 Milestone" required v-slot="field"><UiSelect :id="field.id" :model-value="owner.sprintCreation.milestoneID" label="所属 Milestone" :options="owner.sprintMilestoneOptions.value" :disabled="disabled" @update:model-value="chooseMilestone" /></UiField>
    <UiButton v-if="owner.state.milestoneNext" type="button" :disabled="disabled" @click="owner.loadMilestones(true)">加载更多 Milestone</UiButton>
    <UiField label="Sprint 标题" required v-slot="field"><UiInput :id="field.id" v-model="owner.sprintCreation.title" :disabled="disabled" /></UiField>
    <UiField label="Sprint 描述" v-slot="field"><UiTextarea :id="field.id" v-model="owner.sprintCreation.description" :disabled="disabled" rows="4" /></UiField>
    <p v-if="owner.sprintCreation.message" role="alert">{{ owner.sprintCreation.message }}</p>
    <div class="sprint-create-actions"><UiButton type="submit" :disabled="!canSubmit">确认创建 Sprint</UiButton><UiButton type="button" :disabled="disabled" @click="emit('cancel')">取消创建 Sprint</UiButton></div>
  </form>
</template>
<style scoped>
.sprint-create-form { display: flex; flex-direction: column; gap: var(--space); min-width: 0; padding: var(--space); border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
.sprint-create-form h3, .sprint-create-form p { margin: 0; overflow-wrap: anywhere; }
.sprint-create-form p { color: var(--muted); }
.sprint-create-actions { display: flex; flex-wrap: wrap; gap: var(--content-gap); }
</style>
