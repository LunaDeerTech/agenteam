<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import UiButton from '../../components/ui/UiButton.vue'
import UiDialog from '../../components/ui/UiDialog.vue'
import UiField from '../../components/ui/UiField.vue'
import UiInput from '../../components/ui/UiInput.vue'
import UiSelect from '../../components/ui/UiSelect.vue'
import UiCheckbox from '../../components/ui/UiCheckbox.vue'
import UiState from '../../components/ui/UiState.vue'
import { useSystemModels } from '../../composables/useSystemModels'
const page = useSystemModels()
const { editor, draft, state, progress, blocked, locked, feedback, confirmation } = page
const form = ref<HTMLFormElement | null>(null)
let alive = true
onBeforeUnmount(() => {
  alive = false
})
const inputModalities = computed(() =>
  editor.type === 'chat' ? ['text', 'image', 'file', 'vector'] : ['text'],
)
const outputModalities = computed(() =>
  editor.type === 'chat'
    ? ['text']
    : editor.type === 'embedding'
      ? ['vector']
      : editor.type === 'image_generation'
        ? ['image']
        : [],
)
const structured = computed(() =>
  editor.provider?.input.protocol === 'openai-chat-completions'
    ? ['text', 'json_schema']
    : ['text'],
)
function toggle(
  field: 'input_modalities' | 'output_modalities' | 'structured_output_modes',
  value: string,
  checked: boolean,
) {
  draft[field] = checked
    ? [...draft[field].filter((item) => item !== value), value]
    : draft[field].filter((item) => item !== value)
}
function operable(node: HTMLElement) {
  if (!node.isConnected || node.matches(':disabled') || !node.getClientRects().length) return false
  for (let current: HTMLElement | null = node; current; current = current.parentElement)
    if (current.inert || current.hidden || current.getAttribute('aria-hidden') === 'true')
      return false
  return true
}
async function perform(work: () => Promise<void>, event: Event) {
  const target = event instanceof SubmitEvent ? event.submitter : event.currentTarget
  await work()
  if (!alive || !form.value?.isConnected) return
  await nextTick()
  if (!alive || !editor.mode || confirmation.open || blocked.value) return
  const focus =
    target instanceof HTMLElement && target.tagName !== 'FORM' && operable(target)
      ? target
      : form.value?.querySelector<HTMLElement>('button:not(:disabled), input:not(:disabled)')
  if (focus && operable(focus)) focus.focus()
}
</script>
<template>
  <UiDialog
    :open="editor.mode !== null"
    :title="editor.mode === 'create' ? '创建 Model' : '编辑 Model'"
    @update:open="!$event && page.closeEditor()"
  >
    <form ref="form" class="model-editor" @submit.prevent="perform(page.save, $event)">
      <p class="meta">保存配置不会调用 Provider，也不表示已经验证运行或连接。</p>
      <UiField v-slot="field" label="Provider" hint="创建后不可迁移归属。">
        <UiInput
          :id="field.id"
          :model-value="editor.provider?.input.name ?? ''"
          readonly
          :aria-describedby="field.describedby"
        />
      </UiField>
      <UiField v-slot="field" label="类型" hint="由 Provider 协议决定，创建后不可修改。">
        <UiInput
          :id="field.id"
          :model-value="editor.type"
          readonly
          :aria-describedby="field.describedby"
        />
      </UiField>
      <p class="meta">
        协议：{{ editor.provider?.input.protocol
        }}<span v-if="editor.version">；当前核对版本：{{ editor.version }}</span>
      </p>
      <UiField v-slot="field" label="名称" required hint="1–128 个 Unicode 字符，不自动修整。">
        <UiInput
          :id="field.id"
          v-model="draft.name"
          :disabled="locked"
          autocomplete="off"
          :aria-describedby="field.describedby"
        />
      </UiField>
      <UiField
        v-slot="field"
        label="原生 Model ID"
        required
        hint="1–256 UTF-8 字节，按原文保存，不补全。"
      >
        <UiInput
          :id="field.id"
          v-model="draft.provider_model_id"
          :disabled="locked"
          autocomplete="off"
          spellcheck="false"
          :aria-describedby="field.describedby"
        />
      </UiField>
      <UiField v-slot="field" label="启用状态" required>
        <UiSelect
          :id="field.id"
          :model-value="draft.enabled"
          :options="[
            { value: 'true', label: '启用' },
            { value: 'false', label: '禁用' },
          ]"
          label="启用状态"
          :disabled="locked"
          @update:model-value="draft.enabled = $event as 'true' | 'false'"
        />
      </UiField>
      <fieldset v-if="editor.type === 'chat'" :disabled="locked">
        <legend>能力声明</legend>
        <UiCheckbox v-model="draft.tool_calls" :disabled="locked">工具调用</UiCheckbox>
        <UiCheckbox v-model="draft.parallel_tool_calls" :disabled="locked"
          >并行工具调用（需要工具调用）</UiCheckbox
        >
        <UiCheckbox v-model="draft.streaming" :disabled="locked">流式输出</UiCheckbox>
        <UiCheckbox v-model="draft.reasoning" :disabled="locked">推理</UiCheckbox>
      </fieldset>
      <fieldset :disabled="locked">
        <legend>输入模态</legend>
        <UiCheckbox
          v-for="value in inputModalities"
          :key="value"
          :model-value="draft.input_modalities.includes(value)"
          :disabled="locked"
          @update:model-value="toggle('input_modalities', value, $event)"
          >{{ value }}</UiCheckbox
        >
      </fieldset>
      <fieldset v-if="outputModalities.length" :disabled="locked">
        <legend>输出模态</legend>
        <UiCheckbox
          v-for="value in outputModalities"
          :key="value"
          :model-value="draft.output_modalities.includes(value)"
          :disabled="locked"
          @update:model-value="toggle('output_modalities', value, $event)"
          >{{ value }}</UiCheckbox
        >
      </fieldset>
      <p v-else class="meta">reranker 不声明输出模态。</p>
      <fieldset v-if="editor.type === 'chat'" :disabled="locked">
        <legend>结构化输出声明</legend>
        <UiCheckbox
          v-for="value in structured"
          :key="value"
          :model-value="draft.structured_output_modes.includes(value)"
          :disabled="locked"
          @update:model-value="toggle('structured_output_modes', value, $event)"
          >{{ value }}</UiCheckbox
        >
      </fieldset>
      <div class="capacities">
        <UiField v-slot="field" label="上下文容量" hint="可留空；填写正 int64 十进制整数。">
          <UiInput
            :id="field.id"
            v-model="draft.context_length"
            inputmode="numeric"
            :disabled="locked"
            :aria-describedby="field.describedby"
          />
        </UiField>
        <UiField v-slot="field" label="最大输出容量" hint="可留空；不得超过已填的上下文容量。">
          <UiInput
            :id="field.id"
            v-model="draft.max_output"
            inputmode="numeric"
            :disabled="locked"
            :aria-describedby="field.describedby"
          />
        </UiField>
      </div>
      <p class="meta">
        当前 parameters、request_overwrite、header_overwrite 仅支持空对象，reasoning_efforts
        仅支持空数组。能力声明不表示外部服务已经验证支持。
      </p>
      <p v-if="editor.message" role="status">{{ editor.message }}</p>
      <UiState
        v-if="progress?.phase === 'uncertain'"
        kind="error"
        title="请求结果未确认"
        :description="state.writeMessage"
      >
        <div class="model-actions">
          <UiButton :disabled="blocked" @click="perform(page.checkOriginal, $event)"
            >检查原请求</UiButton
          >
          <UiButton
            :disabled="blocked || !progress.canRetryOriginal"
            @click="perform(page.retryOriginal, $event)"
            >重试原请求</UiButton
          >
          <UiButton variant="ghost" @click="page.abandonOperation">放弃本次操作</UiButton>
        </div>
      </UiState>
      <p v-else-if="state.writeMessage" role="alert">{{ state.writeMessage }}</p>
      <UiButton
        v-if="progress?.phase === 'rejected'"
        :disabled="blocked"
        @click="perform(page.reviewEditor, $event)"
        >{{ editor.mode === 'create' ? '重新编辑并核对' : '读取当前版本并核对' }}</UiButton
      >
      <div class="model-actions">
        <UiButton
          type="submit"
          :disabled="locked || (!page.changed.value && !editor.reviewed)"
          :state="feedback === 'loading' ? 'loading' : 'idle'"
          >保存 Model 配置</UiButton
        >
        <UiButton variant="ghost" @click="page.closeEditor">取消</UiButton>
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.model-editor {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.model-editor p {
  overflow-wrap: anywhere;
}
.model-editor fieldset {
  display: grid;
  gap: 10px;
  min-width: 0;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.model-editor legend {
  max-width: 100%;
  overflow-wrap: anywhere;
}
.model-editor :deep(.ui-choice) {
  min-width: 0;
  white-space: normal;
  overflow-wrap: anywhere;
}
.model-editor :deep(.ui-input),
.model-editor :deep(.ui-select) {
  width: 100%;
  min-width: 0;
}
.capacities {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.model-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.model-actions :deep(button) {
  max-width: 100%;
  white-space: normal;
}
@media (max-width: 700px) {
  .capacities {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
