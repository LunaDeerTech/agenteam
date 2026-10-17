<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiSwitch } from "../ui";
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { AccountError } from "../../account/transport";
import { useLeaveProtection } from "../../account/leave";
import { useSystemAccess } from "../../system/requests";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import {
  catalogError,
  editableProtocols,
  getProvider,
  providerOperation,
  type CatalogReceipt,
  type Provider,
} from "../../system/model-catalog";
const props = defineProps<{ provider: Provider; disabled?: boolean }>();
const emit = defineEmits<{
  saved: [provider: Provider];
  locked: [value: boolean];
}>();
const container = ref<HTMLElement>();
async function withFocus(action: () => void | Promise<unknown>) {
  const ownedFocus = container.value?.contains(document.activeElement);
  try {
    await action();
  } finally {
    await nextTick();
    if (
      alive &&
      ownedFocus &&
      (!document.activeElement || document.activeElement === document.body)
    )
      container.value
        ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
        ?.focus({ preventScroll: true });
  }
}
const access = useSystemAccess();
const command = useCatalogCommand<CatalogReceipt>();
const baseline = shallowRef(props.provider);
const desired = ref<boolean>();
const receipt = shallowRef<CatalogReceipt>();
const reading = ref(false);
const readError = shallowRef<unknown>();
let alive = true;
let generation = 0;
const conflict = computed(
  () =>
    command.error.value instanceof AccountError &&
    command.error.value.code === "VERSION_CONFLICT",
);
const locked = computed(
  () =>
    command.busy.value ||
    command.observing.value ||
    command.uncertain.value ||
    reading.value ||
    !!receipt.value ||
    desired.value !== undefined,
);
const unavailable = computed(
  () =>
    !access.allowed.value ||
    !editableProtocols.includes(baseline.value.input.protocol),
);
function reset() {
  generation++;
  desired.value = undefined;
  receipt.value = undefined;
  reading.value = false;
  readError.value = undefined;
  command.clear();
}
async function read(saved: boolean) {
  if (reading.value || !access.allowed.value) return;
  const owner = generation;
  reading.value = true;
  readError.value = undefined;
  try {
    const value = await getProvider(
      baseline.value.id,
      () => alive && owner === generation && access.allowed.value,
    );
    if (owner !== generation) return;
    baseline.value = value;
    if (saved) reset();
    else command.clear();
    emit("saved", value);
  } catch (error) {
    if (owner === generation) readError.value = error;
  } finally {
    if (owner === generation) reading.value = false;
  }
}
async function apply() {
  if (
    unavailable.value ||
    props.disabled ||
    desired.value === undefined ||
    command.busy.value ||
    command.uncertain.value ||
    command.observing.value ||
    conflict.value ||
    receipt.value ||
    reading.value
  )
    return;
  const input = { ...baseline.value.input, enabled: desired.value };
  await command.submit(
    () => providerOperation(input, baseline.value),
    async (result) => {
      receipt.value = result;
      await read(true);
    },
  );
}
function toggle(value: boolean) {
  if (locked.value || unavailable.value || props.disabled) return;
  desired.value = value;
  void withFocus(apply);
}
function cancel() {
  if (
    command.busy.value ||
    command.uncertain.value ||
    command.observing.value ||
    receipt.value ||
    reading.value
  )
    return;
  reset();
}
watch(
  () => props.provider,
  (value) => {
    if (!locked.value) baseline.value = value;
  },
);
watch(locked, (value) => emit("locked", value), { immediate: true });
watch(access.generation, reset, { flush: "sync" });
useLeaveProtection(() => locked.value);
onBeforeUnmount(() => {
  alive = false;
  reset();
  emit("locked", false);
});
</script>

<template>
  <div ref="container" class="provider-status">
    <UiSwitch
      :model-value="baseline.input.enabled"
      :label="`启用${baseline.input.name}`"
      :disabled="
        disabled ||
        unavailable ||
        command.uncertain.value ||
        command.observing.value ||
        conflict ||
        !!receipt ||
        (desired !== undefined && !command.busy.value)
      "
      :loading="command.busy.value || reading"
      @update:model-value="toggle"
    />
    <span
      v-if="command.busy.value || reading"
      class="form-note"
      role="status"
      >{{ reading ? "正在读取…" : "正在保存…" }}</span
    >
    <div v-if="receipt && readError" class="form-feedback" role="alert">
      <p>状态已保存，最新配置尚未读回。</p>
      <UiButton :loading="reading" @click="withFocus(() => read(true))"
        >重新读取</UiButton
      >
    </div>
    <template v-else>
      <CatalogCommandFeedback
        :error="command.error.value"
        :uncertain="command.uncertain.value"
        :replayable="command.replayable.value"
        :busy="command.busy.value"
        :observing="command.observing.value"
        :observation="command.observation.value"
        :observation-error="command.observationError.value"
        @observe="withFocus(command.observe)"
        @retry="withFocus(command.retry)"
      />
      <p v-if="readError" class="form-feedback" role="alert">
        {{ catalogError(readError) }}
      </p>
      <div
        v-if="
          desired !== undefined &&
          !command.busy.value &&
          !command.uncertain.value &&
          !command.observing.value
        "
        class="form-actions"
      >
        <UiButton
          v-if="conflict"
          :loading="reading"
          @click="withFocus(() => read(false))"
          >读取最新版本</UiButton
        >
        <UiButton v-else :disabled="reading" @click="withFocus(apply)"
          >应用更改</UiButton
        >
        <UiButton :disabled="reading" @click="withFocus(cancel)"
          >取消更改</UiButton
        >
      </div>
    </template>
  </div>
</template>

<style scoped>
.provider-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 100%;
}
.provider-status .form-feedback,
.provider-status .form-actions {
  width: 100%;
  box-sizing: border-box;
}
.provider-status .form-feedback {
  max-width: 360px;
  margin: 0;
}
.provider-status p {
  margin: 0;
}
</style>
