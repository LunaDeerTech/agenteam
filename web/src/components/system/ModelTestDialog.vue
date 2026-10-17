<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
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
function clear() {
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
  if (locked.value) return;
  result.value = undefined;
  cancelling.value = false;
  await state.submit(
    () => (command.value = modelTestCommand(props.provider, props.model)),
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
      <p class="form-note">使用已保存配置发送一次最小测试请求。</p>
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
        <UiButton v-if="!locked" variant="primary" @click="run">{{
          result ? "重新测试" : "开始测试"
        }}</UiButton
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
</style>
