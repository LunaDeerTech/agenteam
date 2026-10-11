<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../ui'
import type { ProjectAgents } from '../../composables/useProjectAgents'
const props = defineProps<{ owner: ProjectAgents }>()
const emit = defineEmits<{ cancel: [] }>()
const form = ref<HTMLFormElement | null>(null)
const disabled = computed(
  () => props.owner.busy.value || !props.owner.canEdit.value || props.owner.editStale.value,
)
onMounted(() => form.value?.querySelector('input')?.focus())
</script>
<template>
  <form
    ref="form"
    class="agent-edit-form"
    aria-label="编辑 Agent 配置"
    @submit.prevent="owner.submitEdit()"
  >
    <h2>编辑 Agent 配置</h2>
    <p>基于版本 {{ owner.edit.version }}，只保存本次编辑的字段。描述和指令可以明确清空。</p>
    <UiField label="Agent 名称" required v-slot="field">
      <UiInput
        :id="field.id"
        :model-value="owner.edit.name"
        :disabled="disabled"
        @update:model-value="owner.touchEdit('name', $event)"
      />
    </UiField>
    <UiField label="Agent 描述" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.description"
        :disabled="disabled"
        rows="4"
        @update:model-value="owner.touchEdit('description', $event)"
      />
    </UiField>
    <UiField label="指令" v-slot="field">
      <UiTextarea
        :id="field.id"
        :model-value="owner.edit.instructions"
        :disabled="disabled"
        rows="6"
        @update:model-value="owner.touchEdit('instructions', $event)"
      />
    </UiField>
    <p v-if="owner.editStale.value" role="alert">
      配置已有新版本。请取消编辑并重新读取，核对后再保存；不会自动替换原版本。
    </p>
    <p v-if="owner.edit.message" role="alert">{{ owner.edit.message }}</p>
    <div class="agent-edit-actions">
      <UiButton type="submit" :disabled="!owner.canSave.value">保存配置</UiButton>
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
.agent-edit-form {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.agent-edit-form h2,
.agent-edit-form p {
  margin: 0;
  overflow-wrap: anywhere;
}
.agent-edit-form p {
  color: var(--muted);
}
.agent-edit-actions {
  display: flex;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
</style>
