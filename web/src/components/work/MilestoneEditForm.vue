<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(
  () =>
    props.owner.busy.value ||
    !props.owner.canEditMilestone.value ||
    props.owner.milestoneEditStale.value,
)
onMounted(() => form.value?.querySelector<HTMLInputElement>('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="milestone-edit-form"
    aria-label="编辑 Milestone"
    @submit.prevent="emit('submit')"
  >
    <h3>编辑 Milestone</h3>
    <p>基于版本 {{ owner.milestoneEdit.version }}，只保存本次编辑的字段。</p>
    <UiField label="Milestone 标题" hint="最多 256 个字符，不能全为空白。" required v-slot="field">
      <UiInput
        :id="field.id"
        :model-value="owner.milestoneEdit.title"
        :disabled="disabled"
        :aria-describedby="field.describedby"
        @update:model-value="owner.touchMilestoneEdit('title', $event)"
      />
    </UiField>
    <UiField
      label="Milestone 描述"
      hint="删除内容后保存可清空描述；未编辑时保留原值。"
      v-slot="field"
    >
      <UiTextarea
        :id="field.id"
        :model-value="owner.milestoneEdit.description"
        :disabled="disabled"
        :aria-describedby="field.describedby"
        rows="4"
        @update:model-value="owner.touchMilestoneEdit('description', $event)"
      />
    </UiField>
    <p v-if="owner.milestoneEditStale.value" role="alert">
      Milestone
      已有变化，已保留原版本和输入。重新读取当前详情后，核对并明确使用当前版本；不会自动保存。
    </p>
    <p v-if="owner.milestoneEdit.message" role="status">{{ owner.milestoneEdit.message }}</p>
    <div v-if="owner.milestoneEditStale.value" class="milestone-edit-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.milestoneEditPending.value"
        @click="owner.reloadMilestoneEdit()"
        >重新读取 Milestone</UiButton
      >
      <UiButton
        type="button"
        :disabled="
          owner.busy.value || !owner.canEditMilestone.value || !owner.milestoneEdit.readVersion
        "
        @click="owner.adoptMilestoneVersion()"
        >使用当前版本继续编辑</UiButton
      >
    </div>
    <div class="milestone-edit-actions">
      <UiButton
        type="submit"
        variant="primary"
        :disabled="disabled || !owner.milestoneEditDirty.value || !owner.milestoneEdit.title.trim()"
        >保存 Milestone</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.milestoneEditPending.value"
        @click="emit('cancel')"
        >取消编辑 Milestone</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.milestone-edit-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  width: 100%;
  max-width: 800px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
}
.milestone-edit-form h3,
.milestone-edit-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.milestone-edit-form p {
  color: var(--muted);
}
.milestone-edit-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
