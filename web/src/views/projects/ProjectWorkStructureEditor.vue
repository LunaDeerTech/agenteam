<script setup lang="ts">
import { UiButton, UiField, UiInput, UiTextarea } from '../../components/ui'

const props = defineProps<{
  kind: 'milestone' | 'sprint'
  creating: boolean
  disabled: boolean
  readOnly: boolean
  canSave: boolean
  canRead: boolean
  canAdopt: boolean
  conflict: boolean
  feedback: 'idle' | 'loading' | 'success'
  message: string
  errors: { title?: string; description?: string }
}>()
const title = defineModel<string>('title', { required: true })
const description = defineModel<string>('description', { required: true })
const emit = defineEmits<{
  save: []
  cancel: []
  readCurrent: []
  adoptCurrent: []
}>()
function submit() {
  if (!props.disabled && props.canSave && !props.readOnly) emit('save')
}
</script>

<template>
  <form class="work-structure-editor" aria-label="规划结构编辑" @submit.prevent="submit">
    <h2>{{ creating ? '新建' : '编辑' }}{{ kind === 'milestone' ? 'Milestone' : 'Sprint' }}</h2>
    <p v-if="readOnly" class="work-note">当前内容只读。</p>
    <UiField
      v-slot="field"
      label="标题"
      required
      :error="errors.title"
      hint="最多 256 个字符，且不超过 1024 字节。"
    >
      <UiInput
        :id="field.id"
        v-model="title"
        :invalid="field.invalid"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        :readonly="readOnly"
        name="work-structure-title"
        autocomplete="off"
        required
      />
    </UiField>
    <UiField
      v-slot="field"
      label="描述"
      :error="errors.description"
      hint="可留空，最多 32768 字节；保留输入的空格与换行。"
    >
      <UiTextarea
        :id="field.id"
        v-model="description"
        :invalid="field.invalid"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        :readonly="readOnly"
        name="work-structure-description"
        :rows="6"
      />
    </UiField>
    <p v-if="message" :role="conflict || Object.keys(errors).length ? 'alert' : 'status'">
      {{ message }}
    </p>
    <div class="work-actions">
      <UiButton
        v-if="!readOnly"
        type="submit"
        variant="primary"
        :disabled="disabled || !canSave"
        :state="feedback"
        loading-label="正在保存"
        success-label="已保存"
        >{{ creating ? '创建' : '保存修改' }}</UiButton
      >
      <UiButton v-if="creating" :disabled="disabled" @click="emit('cancel')">取消新建</UiButton>
      <UiButton v-if="!creating" :disabled="disabled || !canRead" @click="emit('readCurrent')"
        >读取当前值</UiButton
      >
      <UiButton v-if="conflict" :disabled="disabled || !canAdopt" @click="emit('adoptCurrent')"
        >按当前值重新编辑</UiButton
      >
    </div>
  </form>
</template>

<style scoped>
.work-structure-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
h2 {
  margin: 0;
  font-size: 16px;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.work-note {
  color: var(--muted);
}
.work-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
</style>
