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
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { AccountError } from "../../account/transport";
import { useLeaveProtection } from "../../account/leave";
import { useSystemAccess } from "../../system/requests";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import {
  catalogError,
  credentialOperation,
  editableProtocols,
  getCredential,
  getProvider,
  providerOperation,
  type CatalogReceipt,
  type CredentialMetadata,
  type CredentialReceipt,
  type Provider,
} from "../../system/model-catalog";
const props = defineProps<{ provider: Provider; disabled?: boolean }>();
const emit = defineEmits<{
  updated: [value: Provider];
  active: [value: boolean];
  closed: [deleted: boolean];
}>();
const access = useSystemAccess();
const triggerContainer = ref<HTMLElement>();
const open = ref(false);
const target = ref<string>();
const baseline = shallowRef<Provider>();
const metadata = shallowRef<CredentialMetadata>();
const unlinked = ref(false);
const deleted = ref(false);
const reading = ref(false);
const error = shallowRef<unknown>();
const changed = ref(false);
const abandon = ref(false);
const unlinkWrite = useCatalogCommand<CatalogReceipt>();
const deleteWrite = useCatalogCommand<CredentialReceipt>();
let alive = true;
let revision = 0;
const locked = computed(
  () =>
    reading.value ||
    unlinkWrite.busy.value ||
    unlinkWrite.uncertain.value ||
    unlinkWrite.observing.value ||
    deleteWrite.busy.value ||
    deleteWrite.uncertain.value ||
    deleteWrite.observing.value,
);
const pending = computed(() => unlinked.value && !deleted.value);
const readonly = computed(
  () =>
    !access.allowed.value ||
    !editableProtocols.includes(props.provider.input.protocol),
);
const conflict = computed(() => {
  const value = unlinked.value
    ? deleteWrite.error.value
    : unlinkWrite.error.value;
  return value instanceof AccountError && value.code === "VERSION_CONFLICT";
});
const available = computed(
  () =>
    !readonly.value &&
    (pending.value ||
      (!props.disabled && !!props.provider.input.credential_ref)),
);
function clear() {
  revision++;
  unlinkWrite.clear();
  deleteWrite.clear();
  target.value = undefined;
  baseline.value = undefined;
  metadata.value = undefined;
  unlinked.value =
    deleted.value =
    reading.value =
    changed.value =
    abandon.value =
      false;
  error.value = undefined;
}
async function readProvider() {
  const owner = revision;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getProvider(
      props.provider.id,
      () => alive && owner === revision && access.allowed.value,
    );
    if (owner === revision) emit("updated", value);
  } catch (failure) {
    if (owner === revision) error.value = failure;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
async function load() {
  if (locked.value || !target.value || readonly.value) return;
  const owner = revision;
  reading.value = true;
  error.value = undefined;
  changed.value = false;
  try {
    const current = () => alive && owner === revision && access.allowed.value;
    const provider = await getProvider(props.provider.id, current);
    if (!current()) return;
    if (!unlinked.value && provider.input.credential_ref !== target.value) {
      changed.value = true;
      return;
    }
    const secret = await getCredential(target.value, current);
    if (!current()) return;
    baseline.value = provider;
    metadata.value = secret;
    if (unlinked.value) deleteWrite.clear();
    else unlinkWrite.clear();
  } catch (failure) {
    if (owner === revision) error.value = failure;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
async function begin() {
  if (!available.value || locked.value) return;
  if (!pending.value) {
    clear();
    target.value = props.provider.input.credential_ref ?? undefined;
  }
  open.value = true;
  if (!pending.value) await load();
}
async function close() {
  if (locked.value || (deleted.value && error.value)) return;
  const completed = deleted.value;
  open.value = false;
  abandon.value = false;
  if (!pending.value) clear();
  await nextTick();
  // UiDialog closes after its own nextTick watcher; restore only once it is no longer modal.
  await nextTick();
  emit("closed", completed);
  if (
    alive &&
    available.value &&
    (!document.activeElement ||
      document.activeElement === document.body ||
      !!document.activeElement.closest("dialog:not([open])"))
  )
    triggerContainer.value
      ?.querySelector<HTMLButtonElement>("button")
      ?.focus({ preventScroll: true });
}
async function abandonDelete() {
  if (locked.value || !pending.value) return;
  open.value = false;
  clear();
  await nextTick();
  await nextTick();
  emit("closed", false);
}
async function removeSecret() {
  if (
    readonly.value ||
    !metadata.value ||
    deleted.value ||
    deleteWrite.busy.value ||
    deleteWrite.uncertain.value ||
    deleteWrite.observing.value
  )
    return;
  await deleteWrite.submit(
    () => credentialOperation(undefined, metadata.value!, true),
    async () => {
      deleted.value = true;
      await readProvider();
    },
  );
  if (alive && pending.value) await readProvider();
}
async function confirm() {
  if (
    locked.value ||
    readonly.value ||
    changed.value ||
    error.value ||
    conflict.value ||
    !baseline.value ||
    !metadata.value
  )
    return;
  if (unlinked.value) {
    await removeSecret();
    return;
  }
  await unlinkWrite.submit(
    () =>
      providerOperation(
        { ...baseline.value!.input, credential_ref: null },
        baseline.value,
      ),
    async () => {
      unlinked.value = true;
      await removeSecret();
    },
  );
}
watch(
  [open, pending, locked],
  () => emit("active", open.value || pending.value || locked.value),
  { immediate: true },
);
watch(
  access.generation,
  () => {
    open.value = false;
    clear();
  },
  { flush: "sync" },
);
useLeaveProtection(() => pending.value || locked.value);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
</script>
<template>
  <div ref="triggerContainer" class="credential-delete">
    <UiButton :disabled="!available || locked" @click="begin"
      >删除凭据</UiButton
    >
    <UiDialog
      :open="open"
      title="删除凭据"
      :close-on-escape="!locked && !(deleted && error)"
      :close-on-outside="!locked && !(deleted && error)"
      @update:open="close"
    >
      <div class="account-form">
        <template v-if="abandon">
          <p>当前供应商已不使用此凭据，凭据尚未删除。结束本次删除？</p>
          <div class="form-actions">
            <UiButton @click="abandon = false">继续处理</UiButton
            ><UiButton @click="abandonDelete">结束删除</UiButton>
          </div>
        </template>
        <template v-else-if="deleted">
          <p role="status">凭据已删除。</p>
          <div v-if="error" class="form-feedback" role="alert">
            <p>删除已完成，最新供应商配置尚未读回。</p>
            <UiButton :loading="reading" @click="readProvider"
              >重新读取</UiButton
            >
          </div>
          <UiButton :disabled="reading || !!error" @click="close"
            >完成</UiButton
          >
        </template>
        <template v-else>
          <p v-if="unlinked" role="status">
            {{
              deleteWrite.uncertain.value
                ? "关联已解除，凭据删除结果尚未确认。"
                : "当前供应商已不使用此凭据，凭据尚未删除。"
            }}
          </p>
          <p v-else>
            解除当前供应商的关联并删除此凭据？其他配置仍在使用时，凭据不会删除。
          </p>
          <p v-if="reading" role="status">正在读取最新状态…</p>
          <p v-if="changed" role="alert" class="form-feedback">
            供应商关联的凭据已更改。请取消后重新发起删除。
          </p>
          <div v-if="error" class="form-feedback" role="alert">
            <p>{{ catalogError(error) }}</p>
            <UiButton
              :loading="reading"
              @click="unlinked ? readProvider() : load()"
              >重新读取</UiButton
            >
          </div>
          <CatalogCommandFeedback
            :error="unlinkWrite.error.value"
            :uncertain="unlinkWrite.uncertain.value"
            :replayable="unlinkWrite.replayable.value"
            :busy="unlinkWrite.busy.value"
            :observing="unlinkWrite.observing.value"
            :observation="unlinkWrite.observation.value"
            :observation-error="unlinkWrite.observationError.value"
            @observe="unlinkWrite.observe"
            @retry="unlinkWrite.retry"
          />
          <CatalogCommandFeedback
            :error="deleteWrite.error.value"
            :uncertain="deleteWrite.uncertain.value"
            :replayable="deleteWrite.replayable.value"
            :busy="deleteWrite.busy.value"
            :observing="deleteWrite.observing.value"
            :observation="deleteWrite.observation.value"
            :observation-error="deleteWrite.observationError.value"
            @observe="deleteWrite.observe"
            @retry="deleteWrite.retry"
          />
          <UiButton v-if="conflict" :disabled="locked" @click="load"
            >读取最新版本</UiButton
          >
          <div class="form-actions">
            <UiButton
              variant="danger"
              :disabled="locked || !!error || changed || conflict || !metadata"
              @click="confirm"
              >{{ unlinked ? "继续删除" : "确认删除" }}</UiButton
            >
            <UiButton :disabled="locked" @click="close">{{
              unlinked ? "关闭" : "取消"
            }}</UiButton>
            <UiButton v-if="unlinked" :disabled="locked" @click="abandon = true"
              >结束删除…</UiButton
            >
          </div>
        </template>
      </div>
    </UiDialog>
  </div>
</template>
