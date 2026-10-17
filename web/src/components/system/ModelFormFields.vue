<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import {
  UiButton,
  UiCheckboxGroup,
  UiIcon,
  UiIconButton,
  UiInput,
  UiSelect,
} from "../ui";
import type { ModelDraft } from "../../system/model-draft";
import type { Protocol } from "../../system/model-catalog";
const props = defineProps<{
  draft: ModelDraft;
  protocol: Protocol;
  bulk?: boolean;
  typeLocked?: boolean;
}>();
const modalityOptions = [
  { value: "text", label: "文本", icon: "text", disabled: true },
  { value: "image", label: "图像", icon: "image" },
  { value: "file", label: "文件", icon: "file" },
];
const chat = computed(() => props.draft.type === "chat");
const inputModalities = computed({
  get: () => [
    "text",
    ...props.draft.inputModalities
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value && value !== "text"),
  ],
  set: (values: string[]) => {
    props.draft.inputModalities = [
      "text",
      ...values.filter((value) => value !== "text"),
    ].join(", ");
  },
});
const levels = ref<HTMLElement>();
const addLevel = ref<HTMLElement>();
async function appendLevel() {
  if (props.draft.reasoningEfforts.length >= 32) return;
  props.draft.reasoningEfforts.push({ name: "", value: "" });
  await nextTick();
  levels.value
    ?.querySelector<HTMLElement>(".reasoning-row:last-child input")
    ?.focus();
}
async function removeLevel(index: number) {
  props.draft.reasoningEfforts.splice(index, 1);
  await nextTick();
  const rows = levels.value?.querySelectorAll<HTMLElement>(".reasoning-row");
  const row = rows?.[Math.min(index, rows.length - 1)];
  if (row) row.querySelector<HTMLInputElement>("input")?.focus();
  else addLevel.value?.querySelector<HTMLButtonElement>("button")?.focus();
}
</script>
<template>
  <div class="model-fields">
    <template v-if="!bulk">
      <UiInput v-model="draft.name" label="模型名称" required maxlength="128" />
      <UiInput
        v-model="draft.providerModelId"
        label="模型 ID"
        required
        maxlength="256"
      />
    </template>
    <UiSelect
      v-model="draft.type"
      label="模型类型"
      :disabled="typeLocked"
      :options="[
        { value: 'chat', label: '对话' },
        { value: 'embedding', label: '向量' },
      ]"
    />
    <label class="model-check"
      ><input v-model="draft.enabled" type="checkbox" />启用模型</label
    >
    <fieldset v-if="draft.type" class="capability-fields">
      <legend>模型能力</legend>
      <div class="model-grid">
        <div v-if="chat" class="modality-input">
          <UiCheckboxGroup
            v-model="inputModalities"
            label="输入模态"
            :options="modalityOptions"
          />
          <p class="model-hint">
            图像和文件仅记录模型能力，当前调用仅支持文本。
          </p>
        </div>
        <div v-else class="fixed-modality">
          <span class="fixed-label">输入模态</span>
          <span class="fixed-value"><UiIcon name="text" :size="14" />文本</span>
        </div>
        <div class="fixed-modality">
          <span class="fixed-label">输出模态</span>
          <span class="fixed-value"
            ><UiIcon :name="chat ? 'text' : 'vector'" :size="14" />{{
              chat ? "文本" : "向量"
            }}</span
          >
        </div>
      </div>
      <template v-if="chat">
        <div class="model-checks">
          <label class="model-check"
            ><input v-model="draft.toolCalls" type="checkbox" />工具调用</label
          >
          <label class="model-check"
            ><input
              v-model="draft.parallelToolCalls"
              type="checkbox"
            />并行工具调用</label
          >
          <label class="model-check"
            ><input v-model="draft.streaming" type="checkbox" />流式输出</label
          >
        </div>
        <label class="model-check"
          ><input
            v-model="draft.structuredOutput"
            type="checkbox"
          />支持结构化输出（JSON Schema）</label
        >
        <label class="model-check"
          ><input v-model="draft.reasoning" type="checkbox" />支持思考</label
        >
        <section
          v-if="draft.reasoning"
          class="reasoning-levels"
          aria-label="思考等级"
        >
          <p class="model-hint">
            名称用于显示，值发送给模型；不添加等级时不可调节思考强度。
          </p>
          <div ref="levels" class="reasoning-rows">
            <div
              v-for="(effort, index) in draft.reasoningEfforts"
              :key="index"
              class="reasoning-row"
            >
              <UiInput
                v-model="effort.name"
                :label="`等级名称 ${index + 1}`"
                required
              />
              <UiInput
                v-model="effort.value"
                :label="`等级值 ${index + 1}`"
                required
                maxlength="32"
                autocomplete="off"
                spellcheck="false"
              />
              <UiIconButton
                :label="`删除等级 ${index + 1}`"
                icon="close"
                @click="removeLevel(index)"
              />
            </div>
          </div>
          <div ref="addLevel">
            <UiButton
              :disabled="draft.reasoningEfforts.length >= 32"
              @click="appendLevel"
              >添加等级</UiButton
            >
          </div>
        </section>
      </template>
    </fieldset>
    <details v-if="draft.type" class="model-advanced">
      <summary>高级选项</summary>
      <div class="model-grid">
        <UiInput
          v-model="draft.contextLength"
          label="上下文长度（可选）"
          inputmode="numeric"
          placeholder="未知时留空"
        />
        <UiInput
          v-if="chat"
          v-model="draft.maxOutput"
          label="最大输出（可选）"
          inputmode="numeric"
          placeholder="未知时留空"
        />
      </div>
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
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  align-items: start;
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
.model-check input {
  accent-color: var(--accent);
}
.fixed-modality {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.fixed-label {
  font-size: 13px;
  font-weight: 500;
}
.fixed-value {
  display: flex;
  align-items: center;
  gap: 7px;
  min-height: var(--control-height);
  padding: 7px 10px;
  box-sizing: border-box;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  font-size: 13px;
}
.model-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--text-tertiary);
  line-height: 1.5;
}
.reasoning-levels {
  display: grid;
  gap: 12px;
}
.reasoning-levels > .model-hint {
  margin: 0;
}
.reasoning-rows {
  display: grid;
  gap: 12px;
}
.reasoning-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  align-items: end;
  gap: 10px;
}
.model-advanced {
  font-size: 13px;
}
.model-advanced summary {
  cursor: pointer;
  color: var(--text-secondary);
}
.model-advanced .model-grid {
  margin-top: 14px;
}
@media (max-width: 480px) {
  .model-grid {
    grid-template-columns: 1fr;
  }
  .reasoning-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .reasoning-row > :first-child {
    grid-column: 1;
  }
  .reasoning-row > :nth-child(2) {
    grid-column: 1;
  }
  .reasoning-row > :last-child {
    grid-column: 2;
    grid-row: 2;
  }
}
</style>
