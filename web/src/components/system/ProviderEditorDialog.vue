<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { UiButton, UiDialog, UiInput, UiSelect } from "../ui";
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { AccountError } from "../../account/transport";
import { useLeaveProtection } from "../../account/leave";
import { useSystemAccess } from "../../system/requests";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import {
  catalogError,
  credentialOperation,
  getCredential,
  getProvider,
  providerOperation,
  protocolChoices,
  editableProtocols,
  type CatalogReceipt,
  type CredentialReceipt,
  type CredentialMetadata,
  type Provider,
  type ProviderInput,
  type Protocol,
} from "../../system/model-catalog";
const props = defineProps<{ open: boolean; provider?: Provider }>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  saved: [provider: Provider];
}>();
const access = useSystemAccess();
const baseline = shallowRef<Provider>();
const name = ref("");
const protocol = ref<Protocol>("openai-compatible");
const baseUrl = ref("");
const enabled = ref(true);
const options = ref("{}");
const credential = ref("");
const credentialNeedsReentry = ref(false);
const detachCredential = ref(false);
const credentialSaved = shallowRef<CredentialReceipt>();
const credentialMetadata = shallowRef<CredentialMetadata>();
const receipt = shallowRef<CatalogReceipt>();
const canonical = shallowRef<Provider>();
const error = shallowRef<unknown>();
const fieldError = ref("");
const reading = ref(false);
const discarding = ref(false);
const initial = ref("");
const secretWrite = useCatalogCommand<CredentialReceipt>();
const providerWrite = useCatalogCommand<CatalogReceipt>();
let alive = true;
let revision = 0;
const snapshot = () =>
  JSON.stringify([
    name.value,
    protocol.value,
    baseUrl.value,
    enabled.value,
    options.value,
    detachCredential.value,
  ]);
const dirty = computed(
  () =>
    !receipt.value &&
    (snapshot() !== initial.value ||
      !!credential.value ||
      !!credentialSaved.value),
);
const locked = computed(
  () =>
    reading.value ||
    secretWrite.busy.value ||
    secretWrite.observing.value ||
    secretWrite.uncertain.value ||
    providerWrite.busy.value ||
    providerWrite.observing.value ||
    providerWrite.uncertain.value,
);
const conflict = computed(
  () =>
    providerWrite.error.value instanceof AccountError &&
    providerWrite.error.value.code === "VERSION_CONFLICT",
);
const secretConflict = computed(
  () =>
    !secretWrite.uncertain.value &&
    secretWrite.error.value instanceof AccountError &&
    secretWrite.error.value.code === "VERSION_CONFLICT" &&
    ["not_started", "not_committed"].includes(
      secretWrite.error.value.commitState,
    ),
);
watch([secretConflict, secretWrite.uncertain], ([conflicted, uncertain]) => {
  if (conflicted || uncertain) credential.value = "";
  if (conflicted) credentialNeedsReentry.value = true;
});
async function readLatestCredential() {
  if (locked.value || !baseline.value?.input.credential_ref) return;
  const token = revision;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getCredential(
      baseline.value.input.credential_ref,
      () => alive && props.open && token === revision,
    );
    if (token === revision) {
      credentialMetadata.value = value;
      secretWrite.clear();
    }
  } catch (e) {
    if (token === revision) error.value = e;
  } finally {
    if (token === revision) reading.value = false;
  }
}
function clear() {
  revision++;
  credential.value = "";
  credentialNeedsReentry.value = false;
  detachCredential.value = false;
  credentialSaved.value = undefined;
  credentialMetadata.value = undefined;
  receipt.value = undefined;
  canonical.value = undefined;
  baseline.value = undefined;
  name.value = baseUrl.value = "";
  options.value = "{}";
  fieldError.value = "";
  error.value = undefined;
  reading.value = discarding.value = false;
  secretWrite.clear();
  providerWrite.clear();
}
function initialize() {
  clear();
  baseline.value = props.provider;
  name.value = props.provider?.input.name ?? "";
  protocol.value = props.provider?.input.protocol ?? "openai-compatible";
  baseUrl.value = props.provider?.input.base_url ?? "";
  enabled.value = props.provider?.input.enabled ?? true;
  options.value = JSON.stringify(props.provider?.input.options ?? {}, null, 2);
  initial.value = snapshot();
}
function close(force = false) {
  if (locked.value) return;
  if (!force && dirty.value) {
    discarding.value = true;
    return;
  }
  clear();
  emit("update:open", false);
}
async function readSaved() {
  const id = receipt.value?.resource_id;
  if (!id || reading.value) return;
  const owner = revision;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getProvider(
      id,
      () => alive && owner === revision && props.open,
    );
    if (owner !== revision) return;
    canonical.value = value;
    emit("saved", value);
  } catch (failure) {
    if (owner === revision) error.value = failure;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
async function readLatest() {
  if (!baseline.value || locked.value) return;
  const owner = revision;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getProvider(
      baseline.value.id,
      () => alive && owner === revision && props.open,
    );
    if (owner !== revision) return;
    baseline.value = value;
    providerWrite.clear();
  } catch (failure) {
    if (owner === revision) error.value = failure;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
function input(): ProviderInput | undefined {
  fieldError.value = "";
  let extra: unknown;
  try {
    extra = JSON.parse(options.value);
  } catch {
    fieldError.value = "供应商选项必须是 JSON 对象。";
    return;
  }
  if (!extra || typeof extra !== "object" || Array.isArray(extra)) {
    fieldError.value = "供应商选项必须是 JSON 对象。";
    return;
  }
  if (!name.value.trim() || [...name.value.trim()].length > 128) {
    fieldError.value = "名称需为 1–128 个字符。";
    return;
  }
  if (!editableProtocols.includes(protocol.value)) {
    fieldError.value = "此协议当前只支持查看。";
    return;
  }
  try {
    const url = new URL(baseUrl.value.trim());
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      throw new Error();
  } catch {
    fieldError.value = "请输入不含凭据、查询或片段的 HTTP / HTTPS 地址。";
    return;
  }
  if (new TextEncoder().encode(credential.value).length > 65536) {
    fieldError.value = "凭据过长。";
    return;
  }
  return {
    name: name.value.trim(),
    protocol: protocol.value,
    base_url: baseUrl.value.trim(),
    enabled: enabled.value,
    options: extra as Record<string, unknown>,
    credential_ref: detachCredential.value
      ? null
      : (credentialSaved.value?.credential_id ??
        baseline.value?.input.credential_ref ??
        null),
  };
}
async function writeProvider(draft: ProviderInput) {
  const value = {
    ...draft,
    credential_ref:
      credentialSaved.value?.credential_id ?? draft.credential_ref,
  };
  await providerWrite.submit(
    () => providerOperation(value, baseline.value),
    async (result) => {
      receipt.value = result;
      await readSaved();
    },
  );
}
async function save() {
  if (
    locked.value ||
    receipt.value ||
    conflict.value ||
    secretConflict.value ||
    !access.allowed.value
  )
    return;
  const draft = input();
  if (!draft) return;
  error.value = undefined;
  if (detachCredential.value || !credential.value || credentialSaved.value) {
    await writeProvider(draft);
    return;
  }
  const owner = revision;
  let existing: CredentialMetadata | undefined = credentialMetadata.value;
  if (baseline.value?.input.credential_ref && !existing) {
    reading.value = true;
    try {
      existing = credentialMetadata.value = await getCredential(
        baseline.value.input.credential_ref,
        () => alive && owner === revision && props.open,
      );
    } catch (failure) {
      if (owner === revision) error.value = failure;
      return;
    } finally {
      if (owner === revision) reading.value = false;
    }
  }
  if (owner !== revision || !props.open) return;
  await secretWrite.submit(
    () => credentialOperation(credential.value, existing),
    async (result) => {
      credentialSaved.value = result;
      await writeProvider(draft);
    },
    () => {
      credential.value = "";
    },
  );
}
watch(
  () => props.open,
  (open) => {
    if (open) initialize();
    else clear();
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
useLeaveProtection(() => props.open && (dirty.value || locked.value));
onBeforeUnmount(() => {
  alive = false;
  clear();
});
</script>
<template>
  <UiDialog
    :open="open"
    :title="provider ? '编辑供应商' : '添加供应商'"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="
      (value) => {
        if (!value) close();
      }
    "
  >
    <div v-if="discarding" class="account-form">
      <p>
        {{
          credentialSaved
            ? "凭据已保存，关闭不会撤销该结果。放弃剩余编辑？"
            : "放弃尚未保存的编辑？"
        }}
      </p>
      <div class="form-actions">
        <UiButton @click="discarding = false">继续编辑</UiButton
        ><UiButton variant="danger" @click="close(true)">放弃编辑</UiButton>
      </div>
    </div>
    <div v-else-if="receipt" class="account-form">
      <p role="status">供应商已保存。</p>
      <div v-if="error" role="alert" class="form-feedback">
        <p>保存已完成，最新配置尚未读回。</p>
        <UiButton :loading="reading" @click="readSaved">重新读取</UiButton>
      </div>
      <UiButton :disabled="reading" @click="close(true)">完成</UiButton>
    </div>
    <form v-else class="account-form" @submit.prevent="save">
      <fieldset :disabled="locked">
        <UiInput v-model="name" label="供应商名称" maxlength="128" required />
        <UiSelect
          v-model="protocol"
          label="服务类型"
          :options="protocolChoices"
          :disabled="!!provider"
        />
        <UiInput
          v-model="baseUrl"
          label="服务地址"
          placeholder="https://api.example.com/v1"
          required
        />
        <UiInput
          v-model="credential"
          label="API 凭据（可选）"
          type="password"
          autocomplete="new-password"
          :disabled="!!credentialSaved || detachCredential || secretConflict"
          :hint="
            secretConflict
              ? '凭据未更新。读取最新版本后，请重新填写。'
              : credentialNeedsReentry
                ? '请重新填写要替换的凭据；留空保留已保存凭据。'
                : baseline?.input.credential_ref
                  ? '留空保留已保存凭据。'
                  : '凭据仅写入，不回显。'
          "
        />
        <label
          v-if="baseline?.input.credential_ref && !credentialSaved"
          class="catalog-check"
          ><input
            v-model="detachCredential"
            type="checkbox"
          />解除凭据关联（保留凭据）</label
        >
        <label class="catalog-check"
          ><input v-model="enabled" type="checkbox" />启用供应商</label
        >
        <details class="catalog-advanced">
          <summary>高级选项</summary>
          <label
            >供应商选项（JSON）<textarea
              v-model="options"
              spellcheck="false"
              rows="4"
            />
          </label>
        </details>
      </fieldset>
      <p v-if="fieldError" class="form-feedback" role="alert">
        {{ fieldError }}
      </p>
      <p v-if="error" class="form-feedback" role="alert">
        {{ catalogError(error) }}
      </p>
      <p v-if="credentialSaved" class="form-feedback" role="status">
        凭据已保存；供应商配置尚未完成。
      </p>
      <CatalogCommandFeedback
        :error="secretWrite.error.value"
        :uncertain="secretWrite.uncertain.value"
        :replayable="secretWrite.replayable.value"
        :busy="secretWrite.busy.value"
        :observing="secretWrite.observing.value"
        :observation="secretWrite.observation.value"
        :observation-error="secretWrite.observationError.value"
        @observe="secretWrite.observe"
        @retry="secretWrite.retry"
      />
      <CatalogCommandFeedback
        :error="providerWrite.error.value"
        :uncertain="providerWrite.uncertain.value"
        :replayable="providerWrite.replayable.value"
        :busy="providerWrite.busy.value"
        :observing="providerWrite.observing.value"
        :observation="providerWrite.observation.value"
        :observation-error="providerWrite.observationError.value"
        @observe="providerWrite.observe"
        @retry="providerWrite.retry"
      />
      <UiButton
        v-if="secretConflict"
        :loading="reading"
        @click="readLatestCredential"
        >读取凭据最新版本</UiButton
      >
      <UiButton v-if="conflict" :loading="reading" @click="readLatest"
        >读取最新版本，保留草稿</UiButton
      >
      <div class="form-actions">
        <UiButton
          variant="primary"
          type="submit"
          :loading="secretWrite.busy.value || providerWrite.busy.value"
          :disabled="locked || conflict || secretConflict"
          >保存</UiButton
        ><UiButton :disabled="locked" @click="close()">取消</UiButton>
      </div>
    </form>
  </UiDialog>
</template>
<style scoped>
.catalog-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.catalog-advanced {
  font-size: 13px;
}
.catalog-advanced summary {
  cursor: pointer;
  color: var(--text-secondary);
}
.catalog-advanced label {
  display: grid;
  gap: 8px;
  margin-top: 12px;
}
textarea {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 8px;
  color: var(--text-primary);
  background: var(--surface-content);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  font-family: monospace;
  resize: vertical;
}
</style>
