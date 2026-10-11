<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiButton, UiField, UiInput, UiTextarea } from '../../components/ui'
import type { ProjectVariables } from '../../composables/useProjectVariables'
const props = defineProps<{ owner: ProjectVariables }>()
const emit = defineEmits<{ cancel: []; submit: [] }>()
const form = ref<HTMLFormElement | null>(null)
onMounted(() => form.value?.querySelector<HTMLInputElement>('input')?.focus())
</script>
<template>
  <form ref="form" class="variable-editor" :aria-label="owner.editor.mode === 'create' ? '新建普通变量' : '编辑普通变量'" @submit.prevent="emit('submit')">
    <h2>{{ owner.editor.mode === 'create' ? '新建普通变量' : '编辑普通变量' }}</h2>
    <p>普通变量按原文保存。清空值时请明确保留空输入。</p>
    <UiField label="变量名称" required v-slot="field"><UiInput :id="field.id" :model-value="owner.draft.name" :disabled="owner.busy.value || owner.pending.value" @update:model-value="owner.setField('name', $event)" /></UiField>
    <UiField label="变量描述" v-slot="field"><UiTextarea :id="field.id" :model-value="owner.draft.description" :disabled="owner.busy.value || owner.pending.value" rows="3" @update:model-value="owner.setField('description', $event)" /></UiField>
    <UiField label="变量值" v-slot="field"><UiTextarea :id="field.id" :model-value="owner.draft.value" :disabled="owner.busy.value || owner.pending.value" rows="5" @update:model-value="owner.setField('value', $event)" /></UiField>
    <p v-if="owner.editor.message" role="alert">{{ owner.editor.message }}</p>
    <div class="variable-actions"><UiButton type="submit" :disabled="!owner.canSave.value">{{ owner.editor.mode === 'create' ? '创建变量' : '保存变量' }}</UiButton><UiButton type="button" variant="secondary" :disabled="owner.busy.value || owner.pending.value" @click="emit('cancel')">取消编辑</UiButton></div>
  </form>
</template>
<style scoped>
.variable-editor { display: flex; flex-direction: column; gap: var(--space); min-width: 0; padding: var(--space); border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
.variable-editor h2, .variable-editor p { margin: 0; overflow-wrap: anywhere; }
.variable-editor p { color: var(--muted); }
.variable-actions { display: flex; flex-wrap: wrap; gap: var(--content-gap); }
</style>
