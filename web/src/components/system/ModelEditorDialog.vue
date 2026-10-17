<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  reactive,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog } from "../ui";
import ModelFormFields from "./ModelFormFields.vue";
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import { useSystemAccess } from "../../system/requests";
import { AccountError } from "../../account/transport";
import { useLeaveProtection } from "../../account/leave";
import { modelDraft, modelDraftInput } from "../../system/model-draft";
import {
  catalogOperation,
  catalogError,
  getModel,
  type Provider,
  type CatalogModel,
  type CatalogReceipt,
} from "../../system/model-catalog";
const props = defineProps<{
  open: boolean;
  provider: Provider;
  model?: CatalogModel;
}>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  saved: [value: CatalogModel];
}>();
const access = useSystemAccess();
const write = useCatalogCommand<CatalogReceipt>();
const draft = reactive(modelDraft(props.provider.input.protocol));
const baseline = shallowRef<CatalogModel>();
const initial = ref("");
const discarding = ref(false);
const reading = ref(false);
const receipt = shallowRef<CatalogReceipt>();
const canonical = shallowRef<CatalogModel>();
const error = shallowRef<unknown>();
const issue = ref("");
let owner = 0;
let alive = true;
const dirty = computed(
  () => props.open && !receipt.value && JSON.stringify(draft) !== initial.value,
);
const locked = computed(
  () =>
    write.busy.value ||
    write.uncertain.value ||
    write.observing.value ||
    reading.value,
);
const conflict = computed(
  () =>
    write.error.value instanceof AccountError &&
    write.error.value.code === "VERSION_CONFLICT",
);
function clear() {
  owner++;
  write.clear();
  error.value = undefined;
  issue.value = "";
  receipt.value = canonical.value = undefined;
  reading.value = discarding.value = false;
  Object.assign(draft, modelDraft(props.provider.input.protocol));
  baseline.value = undefined;
  initial.value = JSON.stringify(draft);
}
function close(force = false) {
  if (locked.value) return;
  if (dirty.value && !force) {
    discarding.value = true;
    return;
  }
  clear();
  emit("update:open", false);
}
async function readSaved() {
  if (!receipt.value || reading.value) return;
  const token = owner;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getModel(
      receipt.value.resource_id,
      () => alive && props.open && token === owner,
    );
    if (token === owner) {
      canonical.value = value;
      emit("saved", value);
    }
  } catch (e) {
    if (token === owner) error.value = e;
  } finally {
    if (token === owner) reading.value = false;
  }
}
async function readLatest() {
  if (!baseline.value || locked.value) return;
  const token = owner;
  reading.value = true;
  try {
    const value = await getModel(
      baseline.value.id,
      () => alive && props.open && token === owner,
    );
    if (token === owner) {
      baseline.value = value;
      write.clear();
    }
  } catch (e) {
    if (token === owner) error.value = e;
  } finally {
    if (token === owner) reading.value = false;
  }
}
async function save() {
  if (locked.value || receipt.value || conflict.value || !access.allowed.value)
    return;
  issue.value = "";
  let input;
  try {
    input = modelDraftInput(draft, props.provider.input.protocol);
  } catch (e) {
    issue.value = (e as Error).message;
    return;
  }
  const current = baseline.value;
  await write.submit(
    () =>
      catalogOperation(
        current ? `/system/models/${current.id}` : "/system/models",
        current ? "model.update" : "model.create",
        current
          ? { expected_version: current.version, input }
          : { provider_id: props.provider.id, input },
        current ? "PUT" : "POST",
        current?.id,
      ),
    async (result) => {
      receipt.value = result;
      await readSaved();
    },
  );
}
watch(
  () => props.open,
  (open) => {
    clear();
    if (open) {
      baseline.value = props.model;
      Object.assign(
        draft,
        modelDraft(props.provider.input.protocol, props.model?.input),
      );
      initial.value = JSON.stringify(draft);
    }
  },
  { immediate: true },
);
watch(
  access.generation,
  () => {
    clear();
    emit("update:open", false);
  },
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
useLeaveProtection(() => props.open && (dirty.value || locked.value));
</script>
<template>
  <UiDialog
    :open="open"
    :title="
      discarding ? '放弃尚未保存的编辑？' : model ? '编辑模型' : '添加模型'
    "
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="close()"
  >
    <div v-if="discarding" class="account-form">
      <p class="form-note">尚未保存的模型配置将被丢弃。</p>
      <div class="form-actions">
        <UiButton @click="discarding = false">继续编辑</UiButton
        ><UiButton variant="danger" @click="close(true)">放弃修改</UiButton>
      </div>
    </div>
    <div v-else-if="receipt" class="account-form">
      <p role="status">
        {{ canonical ? "模型已保存。" : "保存已完成，最新模型尚未读回。" }}
      </p>
      <div v-if="error" class="form-feedback" role="alert">
        {{ catalogError(error)
        }}<UiButton :loading="reading" @click="readSaved">重新读取</UiButton>
      </div>
      <UiButton :disabled="reading" @click="close(true)">完成</UiButton>
    </div>
    <form v-else class="account-form" @submit.prevent="save">
      <fieldset :disabled="locked" class="model-editor-fields">
        <ModelFormFields :draft="draft" :protocol="provider.input.protocol" />
      </fieldset>
      <p v-if="issue" role="alert" class="form-feedback">{{ issue }}</p>
      <p v-if="error" role="alert" class="form-feedback">
        {{ catalogError(error) }}
      </p>
      <CatalogCommandFeedback
        :error="write.error.value"
        :uncertain="write.uncertain.value"
        :replayable="write.replayable.value"
        :busy="write.busy.value"
        :observing="write.observing.value"
        :observation="write.observation.value"
        :observation-error="write.observationError.value"
        @observe="write.observe"
        @retry="write.retry"
      />
      <UiButton v-if="conflict" :loading="reading" @click="readLatest"
        >读取最新版本，保留草稿</UiButton
      >
      <div class="form-actions">
        <UiButton
          variant="primary"
          type="submit"
          :loading="write.busy.value"
          :disabled="locked || conflict"
          >保存</UiButton
        ><UiButton :disabled="locked" @click="close()">取消</UiButton>
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.model-editor-fields {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
</style>
