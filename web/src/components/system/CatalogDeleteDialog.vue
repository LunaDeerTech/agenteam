<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { UiButton, UiDialog } from "../ui";
import ModelSelect from "./ModelSelect.vue";
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import { useSystemAccess } from "../../system/requests";
import { useLeaveProtection } from "../../account/leave";
import {
  getDeletionImpact,
  type DeletionImpact,
} from "../../system/model-operations";
import {
  catalogOperation,
  credentialOperation,
  catalogError,
  modelDisplayName,
  getCredential,
  getProvider,
  getModel,
  type Provider,
  type CatalogModel,
  type CatalogModelOption,
  type CatalogReceipt,
  type CredentialMetadata,
  type CredentialReceipt,
} from "../../system/model-catalog";
const props = defineProps<{
  open: boolean;
  provider: Provider;
  model?: CatalogModel;
  credential?: string;
}>();
const emit = defineEmits<{ "update:open": [value: boolean]; deleted: [] }>();
const access = useSystemAccess();
const write = useCatalogCommand<CatalogReceipt | CredentialReceipt>();
const impact = shallowRef<DeletionImpact>();
const currentModel = shallowRef<CatalogModel>();
const currentProvider = shallowRef<Provider>();
const secret = shallowRef<CredentialMetadata>();
const reading = ref(false);
const error = shallowRef<unknown>();
const saved = ref(false);
const replacement = shallowRef<CatalogModelOption>();
let alive = true;
let revision = 0;
const locked = computed(
  () =>
    reading.value ||
    write.busy.value ||
    write.uncertain.value ||
    write.observing.value,
);
const blocked = computed(
  () =>
    !!error.value ||
    (!secret.value && !currentProvider.value && !currentModel.value) ||
    (!!props.model &&
      (!impact.value ||
        !!impact.value.delete_blocker ||
        (impact.value.replacement_requirement === "required" &&
          !replacement.value))),
);
const title = computed(() =>
  props.credential ? "删除凭据" : props.model ? "删除模型" : "删除供应商",
);
function replacementIssue(item: CatalogModelOption) {
  if (item.id === currentModel.value?.id) return "不能替换为当前模型";
  if (item.input.type !== currentModel.value?.input.type)
    return "模型类型不适用于此替换";
  if (!item.provider.enabled) return "供应商已停用";
  if (!item.input.enabled) return "模型已停用";
  if (
    impact.value?.reference_groups.some((g) => g.role === "summary") &&
    !item.input.capabilities.structured_output_modes.includes("json_schema")
  )
    return "总结模型需要 json_schema 能力";
  return undefined;
}
function clear() {
  revision++;
  write.clear();
  impact.value = undefined;
  currentModel.value = undefined;
  currentProvider.value = undefined;
  secret.value = undefined;
  error.value = undefined;
  saved.value = false;
  reading.value = false;
  replacement.value = undefined;
}
function close() {
  if (locked.value) return;
  clear();
  emit("update:open", false);
}
async function load() {
  if (locked.value) return;
  const token = revision;
  reading.value = true;
  error.value = undefined;
  write.clear();
  try {
    const current = () => alive && props.open && token === revision;
    if (props.credential)
      secret.value = await getCredential(props.credential, current);
    else if (props.model) {
      const model = await getModel(props.model.id, current);
      const value = await getDeletionImpact(model, current);
      if (current()) {
        currentModel.value = model;
        impact.value = value;
      }
    } else
      currentProvider.value = await getProvider(props.provider.id, current);
  } catch (e) {
    if (token === revision) error.value = e;
  } finally {
    if (token === revision) reading.value = false;
  }
}
async function remove() {
  if (locked.value || blocked.value || !access.allowed.value) return;
  await write.submit(
    () => {
      if (secret.value)
        return credentialOperation(undefined, secret.value, true);
      if (currentModel.value)
        return catalogOperation(
          `/system/models/${currentModel.value.id}`,
          "model.delete",
          {
            expected_version: currentModel.value.version,
            replacement: replacement.value?.id ?? null,
          },
          "DELETE",
          currentModel.value.id,
        );
      const provider = currentProvider.value!;
      return catalogOperation(
        `/system/model-providers/${provider.id}`,
        "provider.delete",
        { expected_version: provider.version },
        "DELETE",
        provider.id,
      );
    },
    () => {
      saved.value = true;
      emit("deleted");
    },
  );
}
watch(
  () => props.open,
  (open) => {
    clear();
    if (open) void load();
  },
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
useLeaveProtection(() => props.open && locked.value);
</script>
<template>
  <UiDialog
    :open="open"
    :title="title"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="close"
  >
    <div v-if="saved" class="account-form">
      <p role="status">删除已完成。</p>
      <UiButton @click="close">完成</UiButton>
    </div>
    <div v-else class="account-form">
      <p class="model-reference">
        {{
          credential
            ? "仅删除独立凭据，不会自动解除任何配置关联。"
            : model
              ? `删除模型“${modelDisplayName(provider.input.name, model.input.provider_model_id)}”？`
              : `删除供应商“${provider.input.name}”？模型和独立凭据不会自动删除。`
        }}
      </p>
      <p v-if="reading" role="status">正在读取最新状态…</p>
      <p v-if="error" class="form-feedback" role="alert">
        {{ catalogError(error) }}
      </p>
      <template v-if="impact"
        ><p>当前共有 {{ impact.reference_count }} 处引用。</p>
        <ul v-if="impact.reference_groups.length">
          <li
            v-for="group in impact.reference_groups"
            :key="`${group.owner_kind}:${group.role}`"
          >
            {{ group.owner_kind }} / {{ group.role }}：{{ group.count }}
          </li>
        </ul>
        <p v-if="impact.delete_blocker" class="form-feedback" role="alert">
          当前引用尚不支持替换，暂时无法删除。
        </p>
        <div v-else-if="impact.replacement_requirement !== 'none'">
          <p class="form-note">
            {{
              impact.replacement_requirement === "required"
                ? "请选择替代模型。"
                : "可选择替代模型；不选则清空这些用途。"
            }}
          </p>
          <ModelSelect
            v-if="currentModel"
            :required-kind="currentModel.input.type"
            label="选择替代模型"
            :model-value="replacement?.id"
            :selected-label="
              replacement
                ? modelDisplayName(
                    replacement.provider.name,
                    replacement.input.provider_model_id,
                  )
                : undefined
            "
            :disabled="locked"
            :issue="replacementIssue"
            @select="replacement = $event"
          />
          <UiButton
            v-if="replacement && impact.replacement_requirement === 'optional'"
            @click="replacement = undefined"
            >清空替代模型</UiButton
          >
        </div></template
      >
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
      <UiButton
        v-if="error || (write.error.value && !write.uncertain.value)"
        :loading="reading"
        :disabled="locked"
        @click="load"
        >重新读取</UiButton
      >
      <div class="form-actions">
        <UiButton
          variant="danger"
          :disabled="locked || blocked"
          :loading="write.busy.value"
          @click="remove"
          >确认删除</UiButton
        ><UiButton :disabled="locked" @click="close">取消</UiButton>
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
