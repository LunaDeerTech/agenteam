<script setup lang="ts">
import { UiInput, UiSelect } from "../ui";
import type { ModelDraft } from "../../system/model-draft";
import type { Protocol } from "../../system/model-catalog";
defineProps<{ draft: ModelDraft; protocol: Protocol; bulk?: boolean }>();
</script>
<template>
  <div class="model-fields">
    <template v-if="!bulk"
      ><UiInput
        v-model="draft.name"
        label="模型名称"
        required
        maxlength="128" /><UiInput
        v-model="draft.providerModelId"
        label="模型 ID"
        required
        maxlength="256"
    /></template>
    <UiSelect
      v-model="draft.type"
      label="模型类型"
      :options="[
        { value: 'chat', label: '对话' },
        { value: 'embedding', label: '向量' },
      ]"
    />
    <label class="model-check"
      ><input v-model="draft.enabled" type="checkbox" />启用模型</label
    >
    <fieldset class="capability-fields">
      <legend>模型能力</legend>
      <div class="model-grid">
        <UiInput
          v-model="draft.inputModalities"
          label="输入模态"
          placeholder="例如 text, image"
          hint="text、image、file 或 vector，以英文逗号分隔。"
        /><UiInput
          v-model="draft.outputModalities"
          label="输出模态"
          placeholder="例如 text 或 vector"
        />
      </div>
      <div class="model-checks">
        <label class="model-check"
          ><input v-model="draft.toolCalls" type="checkbox" />工具调用</label
        ><label class="model-check"
          ><input
            v-model="draft.parallelToolCalls"
            type="checkbox"
          />并行工具调用</label
        ><label class="model-check"
          ><input v-model="draft.streaming" type="checkbox" />流式输出</label
        ><label class="model-check"
          ><input v-model="draft.reasoning" type="checkbox" />推理</label
        >
      </div>
      <UiInput
        v-model="draft.structuredOutputModes"
        label="结构化输出"
        placeholder="text, json_schema"
      />
      <UiInput
        v-if="draft.reasoning"
        v-model="draft.reasoningEfforts"
        label="推理档位"
        placeholder="例如 low, medium, high"
      />
      <div class="model-grid">
        <UiInput
          v-model="draft.contextLength"
          label="上下文长度（可选）"
          inputmode="numeric"
          placeholder="未知时留空"
        /><UiInput
          v-model="draft.maxOutput"
          label="最大输出（可选）"
          inputmode="numeric"
          placeholder="未知时留空"
        />
      </div>
    </fieldset>
    <details class="model-advanced">
      <summary>高级选项</summary>
      <label
        >参数（JSON）<textarea
          v-model="draft.parameters"
          rows="4"
          spellcheck="false"
        /></label
      ><label
        >请求体覆盖（JSON）<textarea
          v-model="draft.requestOverwrite"
          rows="4"
          spellcheck="false"
        /></label
      ><label
        >请求头覆盖（JSON）<textarea
          v-model="draft.headerOverwrite"
          rows="4"
          spellcheck="false"
        />
      </label>
    </details>
  </div>
</template>
<style scoped>
.model-fields,
.capability-fields {
  display: grid;
  gap: 18px;
}
.capability-fields {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.capability-fields legend {
  padding: 0;
  margin-bottom: 14px;
  font-size: 14px;
  font-weight: 550;
}
.model-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.model-checks {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
}
.model-check {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
}
.model-advanced {
  font-size: 13px;
}
.model-advanced summary {
  cursor: pointer;
  color: var(--text-secondary);
}
.model-advanced label {
  display: grid;
  gap: 8px;
  margin-top: 14px;
}
.model-advanced textarea {
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  min-height: 80px;
  border: 1px solid var(--border-default);
  border-radius: var(--radius-md);
  padding: 10px;
  background: var(--bg-surface);
  color: var(--text-primary);
  font: 12px var(--font-mono);
}
@media (max-width: 480px) {
  .model-grid {
    grid-template-columns: 1fr;
  }
}
</style>
