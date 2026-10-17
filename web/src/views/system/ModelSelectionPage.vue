<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { UiButton } from "../../components/ui";
import CatalogCommandFeedback from "../../components/system/CatalogCommandFeedback.vue";
import ModelSelect from "../../components/system/ModelSelect.vue";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import { useLeaveProtection } from "../../account/leave";
import { AccountError, accountEnabled } from "../../account/transport";
import {
  catalogError,
  modelDisplayName,
  modelReasoningIssue,
  type ModelChoice,
  type ReasoningChoice,
  getModel,
  getProvider,
  type CatalogModel,
  type CatalogReceipt,
  type Provider,
  type ModelProviderSummary,
  type CatalogModelOption,
} from "../../system/model-catalog";
import {
  getModelSelection,
  modelPurposeIssue,
  platformPurposes,
  purposeLabels,
  purposeDescriptions,
  purposeModelTypes,
  saveModelSelection,
  type ModelPurpose,
} from "../../system/model-selection";
import { useSystemAccess } from "../../system/requests";
import { useCatalogCommand } from "../../system/useCatalogCommand";

interface Snapshot {
  id: string;
  version: string;
  values: Partial<Record<ModelPurpose, ModelChoice>>;
}
interface Reference {
  model?: CatalogModel;
  provider?: Provider | ModelProviderSummary;
  error?: unknown;
  loading?: boolean;
}
const access = useSystemAccess();
const references = ref<Record<string, Reference>>({});
let alive = true;
let referenceGeneration = 0;
let referenceSequence = 0;
const referenceOwners = new Map<string, number>();
const available = computed(() => accountEnabled && access.allowed.value);

async function resolveReference(id: string, refresh = false) {
  if (!id || (references.value[id] && !refresh) || !available.value) return;
  const owner = ++referenceSequence;
  referenceOwners.set(id, owner);
  const generation = referenceGeneration;
  const current = () =>
    alive &&
    available.value &&
    generation === referenceGeneration &&
    referenceOwners.get(id) === owner;
  references.value[id] = { loading: true };
  let model: CatalogModel | undefined;
  try {
    model = await getModel(id, current);
    const provider = await getProvider(model.provider_id, current);
    if (current()) references.value[id] = { model, provider };
  } catch (error) {
    if (current()) references.value[id] = { model, error };
  }
}
function referenceIssue(
  purpose: ModelPurpose,
  choice: ModelChoice | undefined,
  retainExisting = false,
) {
  const id = choice?.model_id;
  if (!id)
    return purpose === "reranker" || purpose === "image"
      ? undefined
      : "请选择模型";
  const value = references.value[id];
  if (!value || value.loading) return "正在核对当前模型…";
  if (value.error)
    return value.error instanceof AccountError &&
      value.error.code === "NOT_FOUND"
      ? "当前引用的模型或供应商已不存在"
      : "当前引用暂时无法核对，请重新读取";
  return value.model && value.provider
    ? modelPurposeIssue(purpose, value.model, value.provider, retainExisting) ||
        modelReasoningIssue(value.model, choice!.reasoning)
    : "当前引用不可用";
}
function referenceLabel(id: string) {
  if (!id) return "未选择";
  const value = references.value[id];
  return value?.model
    ? value.provider
      ? modelDisplayName(
          "input" in value.provider
            ? value.provider.input.name
            : value.provider.name,
          value.model.input.provider_model_id,
        )
      : value.model.input.provider_model_id
    : id;
}
function sameChoice(left?: ModelChoice, right?: ModelChoice) {
  return (
    left?.model_id === right?.model_id &&
    left?.reasoning.mode === right?.reasoning.mode &&
    (left?.reasoning.mode === "effort" ? left.reasoning.value : undefined) ===
      (right?.reasoning.mode === "effort" ? right.reasoning.value : undefined)
  );
}
function copyValues(values: Snapshot["values"]): Snapshot["values"] {
  return Object.fromEntries(
    Object.entries(values).map(([purpose, choice]) => [
      purpose,
      choice
        ? { model_id: choice.model_id, reasoning: { ...choice.reasoning } }
        : undefined,
    ]),
  );
}
function createSelection() {
  const purposes = platformPurposes;
  const baseline = shallowRef<Snapshot>();
  const latest = shallowRef<Snapshot>();
  const draft = ref<Partial<Record<ModelPurpose, ModelChoice>>>({});
  const reading = ref(false);
  const readError = shallowRef<unknown>();
  const saved = ref(false);
  const command = useCatalogCommand<CatalogReceipt>();
  let revision = 0;
  const dirty = computed(
    () =>
      !!baseline.value &&
      purposes.some(
        (purpose) =>
          !sameChoice(draft.value[purpose], baseline.value?.values[purpose]),
      ),
  );
  const conflict = computed(
    () =>
      command.error.value instanceof AccountError &&
      command.error.value.code === "VERSION_CONFLICT",
  );
  const locked = computed(
    () =>
      !available.value ||
      !baseline.value ||
      reading.value ||
      command.busy.value ||
      command.uncertain.value,
  );
  const invalid = computed(() =>
    purposes.some(
      (purpose) =>
        !!referenceIssue(
          purpose,
          draft.value[purpose],
          draft.value[purpose]?.model_id ===
            baseline.value?.values[purpose]?.model_id,
        ),
    ),
  );
  function adopt(value: Snapshot, retainDraft = false) {
    baseline.value = value;
    latest.value = undefined;
    if (!retainDraft) draft.value = copyValues(value.values);
    command.clear();
    for (const choice of Object.values(value.values))
      if (choice) void resolveReference(choice.model_id);
  }
  async function read() {
    if (
      !available.value ||
      reading.value ||
      command.busy.value ||
      command.uncertain.value
    )
      return;
    const owner = ++revision;
    const current = () => alive && available.value && owner === revision;
    reading.value = true;
    readError.value = undefined;
    try {
      const result = await getModelSelection(current);
      const value: Snapshot = {
        id: result.id,
        version: result.version,
        values: {
          embedding: result.configured?.embedding ?? undefined,
          summary: result.configured?.summary ?? undefined,
          reranker: result.configured?.reranker ?? undefined,
          image: result.configured?.image ?? undefined,
        },
      };
      if (!current()) return;
      if (baseline.value && (dirty.value || conflict.value))
        latest.value = value;
      else adopt(value);
      for (const id of new Set(
        Object.values({ ...value.values, ...draft.value }).flatMap((choice) =>
          choice ? [choice.model_id] : [],
        ),
      )) {
        if (id) void resolveReference(id, !references.value[id]?.loading);
      }
    } catch (error) {
      if (current()) readError.value = error;
    } finally {
      if (owner === revision) reading.value = false;
    }
  }
  async function save() {
    if (
      locked.value ||
      invalid.value ||
      conflict.value ||
      !baseline.value ||
      !dirty.value
    )
      return;
    const captured = { ...baseline.value, values: copyValues(draft.value) };
    saved.value = false;
    await command.submit(
      () =>
        saveModelSelection({
          id: captured.id,
          expected_version: captured.version,
          embedding: captured.values.embedding!,
          summary: captured.values.summary!,
          reranker: captured.values.reranker || null,
          image: captured.values.image || null,
        }),
      (receipt) => {
        baseline.value = { ...captured, version: receipt.version };
        draft.value = copyValues(captured.values);
        latest.value = undefined;
        saved.value = true;
      },
    );
  }
  function clear() {
    revision++;
    baseline.value = undefined;
    latest.value = undefined;
    draft.value = {};
    reading.value = false;
    readError.value = undefined;
    saved.value = false;
    command.clear();
  }
  return {
    purposes,
    baseline,
    latest,
    draft,
    reading,
    readError,
    saved,
    command,
    dirty,
    conflict,
    locked,
    invalid,
    read,
    save,
    clear,
    adoptLatest() {
      if (latest.value && !locked.value) {
        adopt(latest.value, true);
        saved.value = false;
      }
    },
    discard() {
      if (baseline.value && !locked.value) {
        adopt(latest.value || baseline.value);
        saved.value = false;
      }
    },
  };
}
const selection = createSelection();
const dirty = selection.dirty;
const editingLocked = computed(
  () =>
    !available.value ||
    selection.reading.value ||
    selection.command.busy.value ||
    selection.command.observing.value ||
    selection.command.uncertain.value,
);
const canSave = computed(
  () =>
    !editingLocked.value &&
    !!selection.baseline.value &&
    dirty.value &&
    !selection.invalid.value &&
    !selection.conflict.value,
);
function readAll() {
  if (!editingLocked.value) void selection.read();
}
function clearPurpose(purpose: ModelPurpose) {
  if (editingLocked.value || selection.locked.value) return;
  delete selection.draft.value[purpose];
  selection.saved.value = false;
}
function selectModel(
  purpose: ModelPurpose,
  model: CatalogModelOption,
  reasoning?: ReasoningChoice,
) {
  if (
    editingLocked.value ||
    selection.locked.value ||
    modelPurposeIssue(purpose, model, model.provider)
  )
    return;
  references.value[model.id] = { model, provider: model.provider };
  const previous = selection.draft.value[purpose];
  const choice =
    reasoning ??
    (previous?.model_id === model.id
      ? previous.reasoning
      : { mode: "default" });
  if (modelReasoningIssue(model, choice)) return;
  selection.draft.value[purpose] = {
    model_id: model.id,
    reasoning: { ...choice },
  };
  selection.saved.value = false;
}
watch(
  access.generation,
  () => {
    referenceGeneration++;
    referenceOwners.clear();
    references.value = {};
    selection.clear();
  },
  { flush: "sync" },
);
watch(
  available,
  (value) => {
    if (value) void selection.read();
  },
  { immediate: true },
);
useLeaveProtection(
  () =>
    selection.dirty.value ||
    selection.command.uncertain.value ||
    selection.command.busy.value,
);
onBeforeUnmount(() => {
  alive = false;
  referenceGeneration++;
  references.value = {};
  selection.clear();
});
</script>

<template>
  <div class="selection-page">
    <p v-if="!accountEnabled" class="form-feedback">
      连接账号服务后可配置平台模型用途。
    </p>
    <div v-else-if="!access.allowed.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限，页面数据已清除。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <template v-else>
      <SettingsPageActions>
        <UiButton :disabled="editingLocked" @click="readAll">重新获取</UiButton>
      </SettingsPageActions>
      <section class="selection-section" aria-label="平台模型用途">
        <div
          v-for="purpose in platformPurposes"
          :key="purpose"
          class="purpose-row"
        >
          <div class="purpose-info">
            <h3>
              {{ purposeLabels[purpose]
              }}<span
                v-if="purpose === 'reranker' || purpose === 'image'"
                class="optional"
                >可选</span
              >
            </h3>
            <p class="purpose-description">
              {{ purposeDescriptions[purpose] }}
            </p>
            <p
              v-if="
                selection.draft.value[purpose] &&
                referenceIssue(purpose, selection.draft.value[purpose])
              "
              class="reference-issue"
              role="status"
            >
              {{ referenceIssue(purpose, selection.draft.value[purpose]) }}
            </p>
          </div>
          <div class="purpose-actions">
            <ModelSelect
              :required-kind="purposeModelTypes[purpose]"
              :label="`选择${purposeLabels[purpose]}`"
              :model-value="selection.draft.value[purpose]?.model_id || ''"
              :reasoning="selection.draft.value[purpose]?.reasoning"
              allow-reasoning
              :selected-label="
                selection.baseline.value
                  ? selection.draft.value[purpose]
                    ? referenceLabel(selection.draft.value[purpose]!.model_id)
                    : undefined
                  : selection.reading.value
                    ? '正在读取配置…'
                    : '配置尚未读取。'
              "
              :disabled="editingLocked || selection.locked.value"
              :issue="
                (model) => modelPurposeIssue(purpose, model, model.provider)
              "
              @select="
                (model, reasoning) => selectModel(purpose, model, reasoning)
              "
            />
            <UiButton
              v-if="
                (purpose === 'reranker' || purpose === 'image') &&
                selection.draft.value[purpose]
              "
              :disabled="editingLocked || selection.locked.value"
              :aria-label="`清除${purposeLabels[purpose]}`"
              @click="clearPurpose(purpose)"
              >清除</UiButton
            >
          </div>
        </div>
        <p v-if="selection.readError.value" class="form-feedback" role="alert">
          读取失败。{{ catalogError(selection.readError.value) }}
        </p>
        <CatalogCommandFeedback
          :error="selection.command.error.value"
          :uncertain="selection.command.uncertain.value"
          :replayable="selection.command.replayable.value"
          :busy="selection.command.busy.value"
          :observing="selection.command.observing.value"
          :observation="selection.command.observation.value"
          :observation-error="selection.command.observationError.value"
          @observe="selection.command.observe"
          @retry="selection.command.retry"
        />
        <div v-if="selection.latest.value" class="form-feedback" role="status">
          <p>已读取最新配置，当前草稿仍保留。采用最新版本后可重新保存。</p>
          <UiButton :disabled="editingLocked" @click="selection.adoptLatest"
            >采用最新版本并保留草稿</UiButton
          >
        </div>
        <p
          v-if="selection.saved.value && !dirty"
          class="form-note"
          role="status"
        >
          已保存。
        </p>
        <div class="form-actions section-actions">
          <UiButton
            variant="primary"
            :loading="selection.command.busy.value"
            :disabled="!canSave"
            @click="selection.save"
            >保存</UiButton
          >
          <UiButton
            :disabled="editingLocked || !dirty"
            @click="selection.discard"
            >放弃修改</UiButton
          >
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.selection-page {
  max-width: 760px;
}

h3 {
  margin: 0;
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 600;
}
.purpose-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.purpose-info {
  min-width: 0;
}
.purpose-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
  width: 300px;
  align-items: center;
}
.purpose-description {
  margin: 7px 0 0;
  color: var(--text-secondary);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.optional {
  margin-left: 8px;
  color: var(--text-tertiary);
  font-size: 12px;
  font-weight: 400;
}
.reference-issue {
  margin: 6px 0 0;
  color: var(--danger);
  font-size: 12px;
}
.section-actions {
  margin-top: 18px;
}
@media (max-width: 520px) {
  .purpose-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
  }
  .purpose-actions {
    align-self: stretch;
    width: 100%;
  }
}
</style>
