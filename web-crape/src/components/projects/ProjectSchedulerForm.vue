<script setup lang="ts">
import { computed, ref } from 'vue'
import { UiButton, UiCheckbox, UiField, UiInput, UiSwitch } from '../ui'
import {
  captureProjectSchedulerUpdate,
  type ProjectSchedulerDraft,
  type ProjectSchedulerUpdate,
} from '../../api/project-scheduler'

const props = defineProps<{
  draft: ProjectSchedulerDraft
  busy?: boolean
  locked?: boolean
  readOnly?: boolean
}>()
const emit = defineEmits<{ submit: [input: ProjectSchedulerUpdate]; cancel: [] }>()
const attempted = ref(false)
const disabled = computed(() => props.busy || props.locked || props.readOnly)
const input = computed(() => {
  try {
    return captureProjectSchedulerUpdate({
      expected_version: props.draft.expected_version,
      scheduler_enabled: props.draft.enabled,
      scheduler_max_concurrency: props.draft.unlimited ? null : props.draft.limit,
    })
  } catch {
    return null
  }
})
const error = computed(() =>
  attempted.value && !input.value && !props.draft.unlimited
    ? '请输入 1–9223372036854775807 之间的整数，不含空格或前导零。'
    : '',
)
function submit() {
  if (disabled.value) return
  attempted.value = true
  if (input.value) emit('submit', input.value)
}
</script>

<template>
  <form class="scheduler-form" aria-label="调度配置" @submit.prevent="submit">
    <UiSwitch v-model="draft.enabled" name="scheduler-enabled" :disabled="disabled">
      启用自动调度
    </UiSwitch>
    <p class="scheduler-hint">
      暂停只阻止新的自动调度，不会终止已有执行；启用后不会自动启动尚未开始的 Sprint。
    </p>
    <UiCheckbox v-model="draft.unlimited" name="scheduler-unlimited" :disabled="disabled">
      不限制并发数量
    </UiCheckbox>
    <UiField
      v-if="!draft.unlimited"
      v-slot="field"
      label="并发上限"
      required
      :error="error"
      hint="设置同时运行的最大数量。"
    >
      <UiInput
        :id="field.id"
        v-model="draft.limit"
        name="scheduler-limit"
        type="text"
        inputmode="numeric"
        autocomplete="off"
        :disabled="disabled"
        :invalid="field.invalid"
        :aria-describedby="field.describedby"
      />
    </UiField>
    <p v-if="readOnly" role="status">当前项目只读，无法修改调度配置。</p>
    <div class="scheduler-actions">
      <UiButton type="submit" variant="primary" :disabled="disabled">
        {{ busy ? '正在保存调度配置' : '保存调度配置' }}
      </UiButton>
      <UiButton :disabled="disabled" variant="ghost" @click="emit('cancel')"> 取消修改 </UiButton>
    </div>
  </form>
</template>

<style scoped>
.scheduler-form {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.scheduler-hint {
  margin: 0;
  color: var(--muted);
  overflow-wrap: anywhere;
}
.scheduler-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--content-gap);
}
</style>
