<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'
const props = defineProps<{ owner: ProjectTasks }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
const blankTitle = computed(() => /^\p{White_Space}*$/u.test(props.owner.sprintEdit.title))
const disabled = computed(
  () =>
    props.owner.busy.value || !props.owner.canEditSprint.value || props.owner.sprintEditStale.value,
)
onMounted(() => form.value?.querySelector<HTMLInputElement>('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="sprint-edit-form"
    aria-label="编辑 Sprint"
    @submit.prevent="emit('submit')"
  >
    <h3>编辑 Sprint</h3>
    <p>基于版本 {{ owner.sprintEdit.version }}，只保存本次编辑的字段。</p>
    <UiField label="Sprint 标题" hint="最多 256 个字符，不能全为空白。" required v-slot="field">
      <UiInput
        :id="field.id"
        :model-value="owner.sprintEdit.title"
        :disabled="disabled"
        :aria-describedby="field.describedby"
        @update:model-value="owner.touchSprintEdit('title', $event)"
      />
    </UiField>
    <UiField label="Sprint 描述" hint="删除内容后保存可清空描述；未编辑时保留原值。" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.sprintEdit.description"
        :disabled="disabled"
        :aria-describedby="field.describedby"
        rows="4"
        @update:model-value="owner.touchSprintEdit('description', $event)"
      />
    </UiField>
    <p v-if="owner.state.sprint?.state === 'completed'" role="alert">
      Sprint 已完成，不能修改标题或描述。已保留输入，可取消编辑。
    </p>
    <p v-else-if="owner.sprintEditStale.value" role="alert">
      Sprint
      已有变化，已保留原版本和输入。重新读取当前详情后，核对并明确使用当前版本；不会自动保存。
    </p>
    <p v-if="owner.sprintEdit.message" role="status">{{ owner.sprintEdit.message }}</p>
    <div v-if="owner.sprintEditStale.value" class="sprint-edit-actions">
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.sprintEditPending.value"
        @click="owner.reloadSprintEdit()"
        >重新读取 Sprint</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !owner.canEditSprint.value || !owner.sprintEdit.readVersion"
        @click="owner.adoptSprintVersion()"
        >使用当前版本继续编辑</UiButton
      >
    </div>
    <div class="sprint-edit-actions">
      <UiButton type="submit" :disabled="disabled || !owner.sprintEditDirty.value || blankTitle"
        >保存 Sprint</UiButton
      >
      <UiButton
        type="button"
        :disabled="owner.busy.value || !!owner.sprintEditPending.value"
        @click="emit('cancel')"
        >取消编辑 Sprint</UiButton
      >
    </div>
  </form>
</template>
<style scoped>
.sprint-edit-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.sprint-edit-form h3,
.sprint-edit-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.sprint-edit-form p {
  color: var(--muted);
}
.sprint-edit-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
