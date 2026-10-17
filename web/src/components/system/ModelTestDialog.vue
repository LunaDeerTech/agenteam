<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog } from "../ui";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import { useSystemAccess } from "../../system/requests";
import {
  catalogError,
  modelDisplayName,
  type Provider,
  type CatalogModel,
} from "../../system/model-catalog";
import {
  modelTestCommand,
  reasonLabels,
  type TestResult,
  type NetworkCommand,
} from "../../system/model-operations";
import {
  attachmentBytes,
  modelTestMediaTypes,
  readModelTestFiles,
  validateModelTestInput,
  type ModelTestAttachment,
} from "../../system/model-test-input";
const props = defineProps<{
  open: boolean;
  provider: Provider;
  model: CatalogModel;
}>();
const emit = defineEmits<{ "update:open": [value: boolean] }>();
const access = useSystemAccess();
const state = useCommand<TestResult>();
const command = shallowRef<NetworkCommand<TestResult>>();
const result = shallowRef<TestResult>();
const cancelling = ref(false);
const locked = computed(() => state.busy.value || state.uncertain.value);
const chat = computed(() => props.model.input.type === "chat");
const text = ref("请简要说明收到的内容。");
const attachments = shallowRef<ModelTestAttachment[]>([]);
const reading = ref(false);
const inputError = ref("");
const fileInput = ref<HTMLInputElement>();
const selectFilesButton = ref<InstanceType<typeof UiButton>>();
const attachmentList = ref<HTMLUListElement>();
let fileGeneration = 0;
const acceptedTypes = computed(() =>
  modelTestMediaTypes
    .filter((type) =>
      props.model.input.capabilities.input_modalities.includes(
        type.startsWith("image/") ? "image" : "file",
      ),
    )
    .join(","),
);
async function chooseFiles(event: Event) {
  const input = event.target as HTMLInputElement;
  const files = [...(input.files ?? [])];
  input.value = "";
  if (!files.length || locked.value || reading.value) return;
  const owner = ++fileGeneration;
  reading.value = true;
  inputError.value = "";
  try {
    const added = await readModelTestFiles(
      files,
      attachments.value,
      props.model,
      () => owner === fileGeneration && props.open,
    );
    if (owner === fileGeneration) attachments.value = added;
  } catch (error) {
    if (owner === fileGeneration)
      inputError.value =
        error instanceof Error ? error.message : "无法读取文件，请重新选择。";
  } finally {
    if (owner === fileGeneration) reading.value = false;
  }
}
async function removeAttachment(index: number, event: MouseEvent) {
  if (locked.value || reading.value) return;
  const trigger = event.currentTarget;
  const restoreFocus = document.activeElement === trigger;
  const owner = fileGeneration;
  attachments.value = attachments.value.filter((_, at) => at !== index);
  inputError.value = "";
  await nextTick();
  if (
    !restoreFocus ||
    owner !== fileGeneration ||
    !props.open ||
    locked.value ||
    reading.value
  )
    return;
  const active = document.activeElement;
  if (
    active &&
    active !== document.body &&
    active !== trigger &&
    active.isConnected
  )
    return;
  const target =
    attachmentList.value?.querySelectorAll<HTMLButtonElement>("button")[
      Math.min(index, attachments.value.length - 1)
    ] ?? selectFilesButton.value?.$el;
  if (target instanceof HTMLElement && !target.matches(":disabled"))
    target.focus();
}
function clear() {
  fileGeneration++;
  reading.value = false;
  text.value = "请简要说明收到的内容。";
  attachments.value = [];
  inputError.value = "";
  command.value?.release();
  command.value = undefined;
  state.abandon();
  result.value = undefined;
  cancelling.value = false;
}
function close() {
  if (state.busy.value) {
    command.value?.cancel();
    cancelling.value = true;
    return;
  }
  if (state.uncertain.value) return;
  clear();
  emit("update:open", false);
}
async function run() {
  if (locked.value || reading.value) return;
  const input = chat.value
    ? { text: text.value, attachments: [...attachments.value] }
    : undefined;
  inputError.value = input
    ? (validateModelTestInput(input, props.model) ?? "")
    : "";
  if (inputError.value) return;
  result.value = undefined;
  cancelling.value = false;
  await state.submit(
    () =>
      (command.value = modelTestCommand(props.provider, props.model, input)),
    (value) => {
      result.value = value;
      cancelling.value = false;
    },
  );
}
watch(() => props.open, clear);
watch(
  access.generation,
  () => {
    clear();
    emit("update:open", false);
  },
  { flush: "sync" },
);
onBeforeUnmount(clear);
useLeaveProtection(() => props.open && locked.value);
</script>
<template>
  <UiDialog
    :open="open"
    title="测试模型"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="close"
  >
    <div class="account-form">
      <p class="model-reference">
        {{
          modelDisplayName(provider.input.name, model.input.provider_model_id)
        }}
      </p>
      <p class="form-note">
        {{
          chat
            ? "使用已保存的模型配置测试；点击开始测试后发送文本和附件。"
            : "使用已保存配置发送一次最小测试请求。"
        }}
      </p>
      <fieldset v-if="chat" class="test-input" :disabled="locked || reading">
        <label class="test-text"
          ><span>测试文本</span
          ><textarea
            v-model="text"
            class="ui-input"
            rows="3"
            aria-describedby="test-text-limit"
          />
        </label>
        <p id="test-text-limit" class="form-note">
          最多 16 KiB；也可仅发送附件。
        </p>
        <template v-if="acceptedTypes">
          <div class="test-files">
            <input
              ref="fileInput"
              type="file"
              hidden
              aria-label="添加附件"
              multiple
              :accept="acceptedTypes"
              :disabled="attachments.length >= 4"
              @change="chooseFiles"
            />
            <UiButton
              ref="selectFilesButton"
              :disabled="attachments.length >= 4"
              @click="fileInput?.click()"
              >选择附件</UiButton
            >
          </div>
          <p class="form-note">
            最多 4 个附件，共 8 MiB。图片、PDF 单个 4 MiB；TXT、Markdown、DOCX
            单个 1 MiB，并提取文字。GIF 仅支持静态图像。
          </p>
        </template>
        <ul
          v-if="attachments.length"
          ref="attachmentList"
          class="test-attachments"
          aria-label="已选附件"
        >
          <li v-for="(item, index) in attachments" :key="index">
            <span class="attachment-info"
              ><span>{{ item.name }}</span
              ><small
                >{{ Math.ceil(attachmentBytes(item) / 1024) }} KiB</small
              ></span
            >
            <UiButton
              :aria-label="`移除附件 ${item.name}`"
              @click="removeAttachment(index, $event)"
              >移除</UiButton
            >
          </li>
        </ul>
      </fieldset>
      <p v-if="reading" role="status">正在读取本地文件…</p>
      <p v-if="inputError" class="form-feedback" role="alert">
        {{ inputError }}
      </p>
      <p v-if="state.busy.value" role="status">
        {{ cancelling ? "正在停止等待，操作结果仍须确认…" : "正在测试…" }}
      </p>
      <div v-if="result" class="form-feedback" role="status">
        <p>
          {{
            result.outcome === "succeeded"
              ? "本次测试成功。"
              : result.reason
                ? reasonLabels[result.reason]
                : "本次测试结果未知。"
          }}
        </p>
        <p>耗时 {{ result.duration_ms }} ms</p>
        <template v-if="result.answer_text !== null">
          <h3 class="answer-title">模型回答</h3>
          <p class="model-answer">{{ result.answer_text }}</p>
          <p v-if="result.answer_truncated" class="form-note">
            回答较长，仅显示前 8 KiB 内容。
          </p>
        </template>
      </div>
      <div v-if="state.uncertain.value" class="form-feedback" role="alert">
        <p>结果尚未确认。恢复原操作不会再次发送测试请求。</p>
        <UiButton
          :loading="state.busy.value"
          :disabled="!state.replayable.value"
          @click="state.retry"
          >恢复原操作</UiButton
        >
      </div>
      <p v-else-if="state.error.value" class="form-feedback" role="alert">
        {{ catalogError(state.error.value) }}
      </p>
      <div class="form-actions">
        <UiButton
          v-if="!locked"
          variant="primary"
          :disabled="reading"
          @click="run"
          >{{ result ? "重新测试" : "开始测试" }}</UiButton
        ><UiButton v-if="state.busy.value" :disabled="cancelling" @click="close"
          >停止等待</UiButton
        ><UiButton v-else :disabled="state.uncertain.value" @click="close"
          >关闭</UiButton
        >
      </div>
    </div></UiDialog
  >
</template>

<style scoped>
.model-reference {
  min-width: 0;
  overflow-wrap: anywhere;
}
.test-input {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.test-text,
.test-files {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
}
.test-text textarea {
  resize: vertical;
  min-height: 88px;
}
.test-files {
  justify-items: start;
}
.test-attachments {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.test-attachments li {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}
.attachment-info {
  display: grid;
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}
.attachment-info small {
  color: var(--text-secondary);
}
.answer-title {
  margin: var(--space-3) 0 var(--space-2);
  font-size: inherit;
}
.model-answer {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
