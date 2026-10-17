<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { UiButton, UiDialog } from "../../components/ui";
import CatalogCommandFeedback from "../../components/system/CatalogCommandFeedback.vue";
import SystemList from "../../components/system/SystemList.vue";
import { useLeaveProtection } from "../../account/leave";
import { AccountError, accountEnabled } from "../../account/transport";
import {
  catalogError,
  getModel,
  getProvider,
  listModels,
  listProviders,
  type CatalogModel,
  type CatalogReceipt,
  type Provider,
} from "../../system/model-catalog";
import {
  getMeetingSummarySelection,
  getModelSelection,
  modelPurposeIssue,
  platformPurposes,
  purposeLabels,
  saveMeetingSummarySelection,
  saveModelSelection,
  type ModelPurpose,
} from "../../system/model-selection";
import { useSystemAccess } from "../../system/requests";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import { useCursorList } from "../../system/useCursorList";

interface Snapshot {
  id: string;
  version: string;
  values: Partial<Record<ModelPurpose, string>>;
}
interface Reference {
  model?: CatalogModel;
  provider?: Provider;
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
  id: string,
  retainExisting = false,
) {
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
    ? modelPurposeIssue(purpose, value.model, value.provider, retainExisting)
    : "当前引用不可用";
}
function referenceLabel(id: string) {
  if (!id) return "未选择";
  const value = references.value[id];
  return value?.model
    ? `${value.model.input.name}${value.provider ? ` · ${value.provider.input.name}` : ""}`
    : id;
}
function createGroup(summary: boolean) {
  const purposes: ModelPurpose[] = summary
    ? ["meeting_summary"]
    : [...platformPurposes];
  const baseline = shallowRef<Snapshot>();
  const latest = shallowRef<Snapshot>();
  const draft = ref<Partial<Record<ModelPurpose, string>>>({});
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
          (draft.value[purpose] || "") !==
          (baseline.value?.values[purpose] || ""),
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
          draft.value[purpose] || "",
          draft.value[purpose] === baseline.value?.values[purpose],
        ),
    ),
  );
  function adopt(value: Snapshot, retainDraft = false) {
    baseline.value = value;
    latest.value = undefined;
    if (!retainDraft) draft.value = { ...value.values };
    command.clear();
    for (const id of Object.values(value.values))
      if (id) void resolveReference(id);
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
      let value: Snapshot;
      if (summary) {
        const result = await getMeetingSummarySelection(current);
        value = {
          id: result.id,
          version: result.version,
          values: { meeting_summary: result.model || "" },
        };
      } else {
        const result = await getModelSelection(current);
        value = {
          id: result.id,
          version: result.version,
          values: {
            embedding: result.configured?.embedding || "",
            memory: result.configured?.memory || "",
            reranker: result.configured?.reranker || "",
            image: result.configured?.image || "",
          },
        };
      }
      if (!current()) return;
      if (baseline.value && (dirty.value || conflict.value))
        latest.value = value;
      else adopt(value);
      for (const id of new Set(
        Object.values({ ...value.values, ...draft.value }),
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
    const captured = { ...baseline.value, values: { ...draft.value } };
    saved.value = false;
    await command.submit(
      () =>
        summary
          ? saveMeetingSummarySelection({
              id: captured.id,
              expected_version: captured.version,
              model: captured.values.meeting_summary!,
            })
          : saveModelSelection({
              id: captured.id,
              expected_version: captured.version,
              embedding: captured.values.embedding!,
              memory: captured.values.memory!,
              reranker: captured.values.reranker || null,
              image: captured.values.image || null,
            }),
      (receipt) => {
        baseline.value = { ...captured, version: receipt.version };
        draft.value = { ...captured.values };
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
    summary,
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
const platform = createGroup(false);
const summary = createGroup(true);
const groups = [platform, summary];
const picking = ref<ModelPurpose>();
const provider = shallowRef<Provider>();
const pickerOpen = computed({
  get: () => !!picking.value,
  set: (value) => {
    if (!value) picking.value = undefined;
  },
});
const providers = useCursorList(listProviders, {
  enabled: () => !!picking.value && !provider.value,
});
const models = useCursorList(
  (cursor, current, limit) =>
    listModels(provider.value?.id, cursor, current, limit),
  { enabled: () => !!picking.value && !!provider.value },
);
function openPicker(purpose: ModelPurpose) {
  const group = purpose === "meeting_summary" ? summary : platform;
  if (group.locked.value) return;
  provider.value = undefined;
  providers.reset();
  models.reset();
  picking.value = purpose;
}
function pickProvider(value: Provider) {
  models.reset();
  provider.value = value;
}
function selectModel(model: CatalogModel) {
  const purpose = picking.value;
  if (
    !purpose ||
    !provider.value ||
    modelPurposeIssue(purpose, model, provider.value)
  )
    return;
  const group = purpose === "meeting_summary" ? summary : platform;
  if (group.locked.value) return;
  references.value[model.id] = { model, provider: provider.value };
  group.draft.value[purpose] = model.id;
  group.saved.value = false;
  picking.value = undefined;
}
watch(
  access.generation,
  () => {
    referenceGeneration++;
    referenceOwners.clear();
    references.value = {};
    picking.value = undefined;
    provider.value = undefined;
    groups.forEach((group) => group.clear());
  },
  { flush: "sync" },
);
watch(
  available,
  (value) => {
    if (value) groups.forEach((group) => void group.read());
  },
  { immediate: true },
);
useLeaveProtection(() =>
  groups.some(
    (group) =>
      group.dirty.value ||
      group.command.uncertain.value ||
      group.command.busy.value,
  ),
);
onBeforeUnmount(() => {
  alive = false;
  referenceGeneration++;
  references.value = {};
  groups.forEach((group) => group.clear());
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
      <section
        v-for="group in groups"
        :key="String(group.summary)"
        class="selection-section"
        :aria-label="group.summary ? '会议摘要' : '平台模型用途'"
      >
        <div class="section-heading">
          <h2>{{ group.summary ? "会议摘要" : "平台模型用途" }}</h2>
          <UiButton
            :loading="group.reading.value"
            :disabled="
              group.command.busy.value || group.command.uncertain.value
            "
            @click="group.read"
            >重新读取</UiButton
          >
        </div>
        <p v-if="group.reading.value" class="form-note" role="status">
          正在读取配置…
        </p>
        <p v-if="group.readError.value" class="form-feedback" role="alert">
          读取失败。{{ catalogError(group.readError.value) }}
        </p>
        <template v-if="group.baseline.value">
          <div
            v-for="purpose in group.purposes"
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
              <p class="selected-name">
                {{ referenceLabel(group.draft.value[purpose] || "") }}
              </p>
              <p
                v-if="
                  group.draft.value[purpose] &&
                  referenceIssue(purpose, group.draft.value[purpose] || '')
                "
                class="reference-issue"
                role="status"
              >
                {{ referenceIssue(purpose, group.draft.value[purpose] || "") }}
              </p>
              <p v-if="purpose === 'memory'" class="form-note">
                需要支持 json_schema 的对话模型。
              </p>
            </div>
            <div class="purpose-actions">
              <UiButton
                :disabled="group.locked.value"
                :aria-label="`选择${purposeLabels[purpose]}`"
                @click="openPicker(purpose)"
                >选择模型</UiButton
              >
              <UiButton
                v-if="
                  (purpose === 'reranker' || purpose === 'image') &&
                  group.draft.value[purpose]
                "
                :disabled="group.locked.value"
                :aria-label="`清除${purposeLabels[purpose]}`"
                @click="
                  group.draft.value[purpose] = '';
                  group.saved.value = false;
                "
                >清除</UiButton
              >
            </div>
          </div>
          <div v-if="group.latest.value" class="form-feedback" role="status">
            <p>已读取最新配置，当前草稿仍保留。采用最新版本后可重新保存。</p>
            <UiButton :disabled="group.locked.value" @click="group.adoptLatest"
              >采用最新版本并保留草稿</UiButton
            >
          </div>
          <CatalogCommandFeedback
            :error="group.command.error.value"
            :uncertain="group.command.uncertain.value"
            :replayable="group.command.replayable.value"
            :busy="group.command.busy.value"
            :observing="group.command.observing.value"
            :observation="group.command.observation.value"
            :observation-error="group.command.observationError.value"
            @observe="group.command.observe"
            @retry="group.command.retry"
          />
          <p v-if="group.saved.value" class="form-note" role="status">
            {{
              group.summary ? "会议摘要用途已保存。" : "平台模型用途已保存。"
            }}
          </p>
          <div class="form-actions section-actions">
            <UiButton
              variant="primary"
              :loading="group.command.busy.value"
              :disabled="
                group.locked.value ||
                !group.dirty.value ||
                group.invalid.value ||
                group.conflict.value
              "
              @click="group.save"
              >{{
                group.summary ? "保存会议摘要用途" : "保存平台模型用途"
              }}</UiButton
            >
            <UiButton
              :disabled="group.locked.value || !group.dirty.value"
              @click="group.discard"
              >放弃修改</UiButton
            >
          </div>
        </template>
      </section>
    </template>
    <UiDialog
      v-model:open="pickerOpen"
      :title="picking ? `选择${purposeLabels[picking]}` : '选择模型'"
    >
      <template v-if="picking">
        <template v-if="!provider">
          <p class="form-note">先选择供应商。</p>
          <SystemList
            :list="providers"
            :row-key="(item) => item.id"
            label="供应商"
            empty-text="尚未添加供应商。"
            :disabled="!available"
          >
            <template #item="{ item }"
              ><span>{{ item.input.name }}</span
              ><span class="form-note"
                >{{ item.input.protocol
                }}{{ item.input.enabled ? "" : " · 已停用" }}</span
              ></template
            >
            <template #actions="{ item }"
              ><UiButton
                :disabled="!item.input.enabled"
                @click="pickProvider(item)"
                >选择供应商</UiButton
              ></template
            >
          </SystemList>
        </template>
        <template v-else>
          <div class="section-heading">
            <h3>{{ provider.input.name }}</h3>
            <UiButton @click="provider = undefined">更换供应商</UiButton>
          </div>
          <SystemList
            :list="models"
            :row-key="(item) => item.id"
            label="模型"
            empty-text="此供应商尚未添加模型。"
            :disabled="!available"
          >
            <template #item="{ item }"
              ><span>{{ item.input.name }}</span
              ><span class="form-note">{{
                modelPurposeIssue(picking, item, provider) ||
                item.input.provider_model_id
              }}</span></template
            >
            <template #actions="{ item }"
              ><UiButton
                :disabled="!!modelPurposeIssue(picking, item, provider)"
                @click="selectModel(item)"
                >使用此模型</UiButton
              ></template
            >
          </SystemList>
        </template>
      </template>
    </UiDialog>
  </div>
</template>

<style scoped>
.selection-page {
  max-width: 760px;
}
.selection-section + .selection-section {
  margin-top: 34px;
  padding-top: 28px;
  border-top: 1px solid var(--border-subtle);
}
.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 10px;
}
h2,
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
}
.selected-name {
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
    align-self: flex-end;
  }
}
</style>
