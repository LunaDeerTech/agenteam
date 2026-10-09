<script setup lang="ts">
import { UiButton, UiField, UiInput, UiSelect, UiTextarea } from '../../components/ui'

const props = defineProps<{
  creating: boolean
  disabled: boolean
  readOnly: boolean
  canSave: boolean
  canRead: boolean
  canAdopt: boolean
  conflict: boolean
  feedback: 'idle' | 'loading' | 'success'
  message: string
  errors: { title?: string; description?: string; type?: string; priority?: string; plan?: string }
}>()
const title = defineModel<string>('title', { required: true })
const description = defineModel<string>('description', { required: true })
const type = defineModel<string>('type', { required: true })
const priority = defineModel<string>('priority', { required: true })
const plan = defineModel<string>('plan', { required: true })
const emit = defineEmits<{
  save: []
  cancel: []
  readCurrent: []
  adoptCurrent: []
}>()
const types = [
  { value: '', label: '请选择类型', disabled: true },
  { value: 'feature', label: '功能' },
  { value: 'bug', label: '缺陷' },
  { value: 'task', label: '任务' },
  { value: 'spike', label: '调研' },
  { value: 'chore', label: '维护' },
]
const priorities = [
  { value: '', label: '请选择优先级', disabled: true },
  { value: 'critical', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
]
function submit() {
  if (!props.disabled && props.canSave && !props.readOnly) emit('save')
}
</script>

<template>
  <form class="work-task-editor" aria-label="Task 规划编辑" @submit.prevent="submit">
    <h2>{{ creating ? '新建 Task' : '编辑 Task' }}</h2>
    <p v-if="creating" class="work-note">新 Task 保存在 backlog，暂不指派 Agent。</p>
    <p v-if="readOnly" class="work-note">当前 Task 只读。</p>
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
        name="work-task-title"
        autocomplete="off"
        required
      />
    </UiField>
    <UiField
      v-slot="field"
      label="描述"
      :error="errors.description"
      hint="可留空，最多 32768 字节。"
    >
      <UiTextarea
        :id="field.id"
        v-model="description"
        :invalid="field.invalid"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        :readonly="readOnly"
        name="work-task-description"
        :rows="4"
      />
    </UiField>
    <div class="work-task-properties">
      <UiField v-slot="field" label="类型" required :error="errors.type">
        <UiSelect
          :id="field.id"
          v-model="type"
          label="Task 类型"
          :options="types"
          :invalid="field.invalid"
          :aria-describedby="field.describedby"
          :disabled="disabled || readOnly"
        />
      </UiField>
      <UiField v-slot="field" label="优先级" required :error="errors.priority">
        <UiSelect
          :id="field.id"
          v-model="priority"
          label="Task 优先级"
          :options="priorities"
          :invalid="field.invalid"
          :aria-describedby="field.describedby"
          :disabled="disabled || readOnly"
        />
      </UiField>
    </div>
    <UiField
      v-slot="field"
      label="Plan"
      :error="errors.plan"
      hint="可留空，最多 32768 字节；保留原文，不执行 HTML 或 Markdown。"
    >
      <UiTextarea
        :id="field.id"
        v-model="plan"
        :invalid="field.invalid"
        :aria-describedby="field.describedby"
        :disabled="disabled"
        :readonly="readOnly"
        name="work-task-plan"
        :rows="10"
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
        >{{ creating ? '创建 Task' : '保存修改' }}</UiButton
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
.work-task-editor {
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
.work-task-properties {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space);
}
.work-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--content-gap);
}
@media (max-width: 560px) {
  .work-task-properties {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
