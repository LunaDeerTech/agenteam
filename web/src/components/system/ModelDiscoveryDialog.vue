<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  reactive,
  ref,
  shallowReactive,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog, UiInput } from "../ui";
import ModelFormFields from "./ModelFormFields.vue";
import CatalogCommandFeedback from "./CatalogCommandFeedback.vue";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import { useCatalogCommand } from "../../system/useCatalogCommand";
import { useSystemAccess } from "../../system/requests";
import {
  catalogOperation,
  catalogError,
  modelDisplayName,
  type Provider,
  type ModelInput,
  type CatalogReceipt,
} from "../../system/model-catalog";
import {
  discoveryCommand,
  reasonLabels,
  type DiscoveryResult,
  type NetworkCommand,
} from "../../system/model-operations";
import {
  modelDraft,
  modelDraftInput,
  type ModelDraft,
} from "../../system/model-draft";
const props = defineProps<{ open: boolean; provider: Provider }>();
const emit = defineEmits<{ "update:open": [value: boolean]; imported: [] }>();
const access = useSystemAccess();
const discovery = useCommand<DiscoveryResult>();
const importing = useCatalogCommand<CatalogReceipt>();
const network = shallowRef<NetworkCommand<DiscoveryResult>>();
const result = shallowRef<DiscoveryResult>();
const selected = ref<string[]>([]);
const filter = ref("");
const drafts = shallowReactive<Record<string, ModelDraft>>(Object.create(null));
const configuring = ref(false);
function configure() {
  for (const id of selected.value)
    drafts[id] ??= reactive(modelDraft(props.provider.input.protocol));
  configuring.value = true;
}
interface ImportItem {
  id: string;
  input: ModelInput;
  status: "pending" | "saved" | "failed" | "unknown" | "skipped";
  receipt?: CatalogReceipt;
}
const batch = ref<ImportItem[]>([]);
const activeIndex = ref(-1);
const running = ref(false);
const stopped = ref(false);
const cancelling = ref(false);
const issue = ref("");
let alive = true;
let generation = 0;
const filtered = computed(
  () =>
    result.value?.items.filter((i) =>
      i.provider_model_id
        .toLocaleLowerCase()
        .includes(filter.value.trim().toLocaleLowerCase()),
    ) ?? [],
);
const locked = computed(
  () =>
    discovery.busy.value ||
    discovery.uncertain.value ||
    running.value ||
    importing.uncertain.value ||
    importing.busy.value ||
    importing.observing.value,
);
const savedCount = computed(
  () => batch.value.filter((i) => i.status === "saved").length,
);
function clear() {
  generation++;
  network.value?.release();
  network.value = undefined;
  discovery.abandon();
  importing.clear();
  result.value = undefined;
  selected.value = [];
  batch.value = [];
  filter.value = "";
  running.value = stopped.value = cancelling.value = false;
  activeIndex.value = -1;
  issue.value = "";
  for (const key of Object.keys(drafts)) delete drafts[key];
  configuring.value = false;
}
function close() {
  if (discovery.busy.value) {
    network.value?.cancel();
    cancelling.value = true;
    return;
  }
  if (running.value) {
    stopped.value = true;
    return;
  }
  if (locked.value) return;
  clear();
  emit("update:open", false);
}
async function discover() {
  if (locked.value || batch.value.length) return;
  result.value = undefined;
  selected.value = [];
  await discovery.submit(
    () => (network.value = discoveryCommand(props.provider)),
    (value) => {
      result.value = value;
      cancelling.value = false;
    },
  );
}
async function next() {
  if (!alive || stopped.value || importing.uncertain.value) return;
  const index = batch.value.findIndex(
    (item) => item.status === "pending" || item.status === "failed",
  );
  if (index < 0) {
    running.value = false;
    emit("imported");
    return;
  }
  activeIndex.value = index;
  const item = batch.value[index]!;
  const token = generation;
  running.value = true;
  await importing.submit(
    () =>
      catalogOperation("/system/models", "model.create", {
        provider_id: props.provider.id,
        input: item.input,
      }),
    (receipt) => {
      if (token !== generation) return;
      item.receipt = receipt;
      item.status = "saved";
      emit("imported");
    },
  );
  if (token !== generation) return;
  if (item.status !== "saved") {
    item.status = importing.uncertain.value ? "unknown" : "failed";
    running.value = false;
    return;
  }
  if (stopped.value) {
    running.value = false;
    return;
  }
  await next();
}
async function startImport() {
  if (locked.value || batch.value.length) return;
  if (!selected.value.length || selected.value.length > 100) {
    issue.value = "每批请选择 1–100 个模型。";
    return;
  }
  const prepared: ImportItem[] = [];
  for (const id of selected.value) {
    try {
      prepared.push({
        id,
        input: modelDraftInput(
          {
            ...drafts[id]!,
            providerModelId: id,
          },
          props.provider.input.protocol,
        ),
        status: "pending",
      });
    } catch (e) {
      issue.value = `${id}：${(e as Error).message}`;
      return;
    }
  }
  issue.value = "";
  batch.value = prepared;
  stopped.value = false;
  await next();
}
async function recover() {
  const token = generation;
  await importing.retry();
  if (token !== generation) return;
  const item = batch.value[activeIndex.value];
  if (item?.status === "saved") {
    stopped.value = false;
    await next();
  }
}
async function skipFailed() {
  if (locked.value) return;
  const item = batch.value[activeIndex.value];
  if (item?.status !== "failed") return;
  item.status = "skipped";
  importing.clear();
  stopped.value = false;
  await next();
}
async function continueImport() {
  if (locked.value) return;
  stopped.value = false;
  await next();
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
onBeforeUnmount(() => {
  alive = false;
  clear();
});
useLeaveProtection(() => props.open && locked.value);
</script>
<template>
  <UiDialog
    :open="open"
    title="发现并导入模型"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="close"
  >
    <div class="account-form">
      <p v-if="!result && !batch.length" class="form-note">
        从已保存的供应商地址获取模型 ID。
      </p>
      <p v-if="discovery.busy.value" role="status">
        {{ cancelling ? "正在停止等待，结果仍须确认…" : "正在发现模型…" }}
      </p>
      <div v-if="discovery.uncertain.value" class="form-feedback" role="alert">
        <p>结果尚未确认。恢复原操作不会再次请求供应商。</p>
        <UiButton
          :loading="discovery.busy.value"
          :disabled="!discovery.replayable.value"
          @click="discovery.retry"
          >恢复原操作</UiButton
        >
      </div>
      <p v-else-if="discovery.error.value" class="form-feedback" role="alert">
        {{ catalogError(discovery.error.value) }}
      </p>
      <p
        v-if="result && result.outcome !== 'succeeded'"
        class="form-feedback"
        role="alert"
      >
        {{ result.reason ? reasonLabels[result.reason] : "发现结果未知。" }}
      </p>
      <template v-if="result?.outcome === 'succeeded' && !batch.length"
        ><p v-if="!result.items.length" role="status">供应商未返回模型。</p>
        <template v-else
          ><UiInput v-if="!configuring" v-model="filter" label="筛选本次结果" />
          <p class="form-note">已选择 {{ selected.length }} / 100</p>
          <div v-if="!configuring" class="discovery-items">
            <label v-for="item in filtered" :key="item.provider_model_id"
              ><input
                v-model="selected"
                type="checkbox"
                :value="item.provider_model_id"
                :disabled="
                  !selected.includes(item.provider_model_id) &&
                  selected.length >= 100
                "
              /><span>{{
                modelDisplayName(provider.input.name, item.provider_model_id)
              }}</span></label
            >
          </div>
          <template v-if="configuring"
            ><details
              v-for="(id, index) in selected"
              :key="id"
              class="import-configuration"
              :open="index === 0"
            >
              <summary>{{ modelDisplayName(provider.input.name, id) }}</summary>
              <ModelFormFields
                :draft="drafts[id]!"
                :protocol="provider.input.protocol"
                bulk
              />
            </details>
            <p v-if="issue" class="form-feedback" role="alert">{{ issue }}</p>
            <UiButton variant="primary" @click="startImport"
              >导入所选模型</UiButton
            ><UiButton @click="configuring = false"
              >返回选择</UiButton
            ></template
          >
          <UiButton
            v-else
            variant="primary"
            :disabled="!selected.length"
            @click="configure"
            >配置所选模型</UiButton
          ></template
        ></template
      >
      <template v-if="batch.length"
        ><p role="status">
          已导入 {{ savedCount }} / {{ batch.length
          }}{{
            running ? (stopped ? "，完成当前项后停止。" : "，正在导入…") : ""
          }}
        </p>
        <ul class="import-results">
          <li v-for="item in batch" :key="item.id">
            <span>{{ modelDisplayName(provider.input.name, item.id) }}</span
            ><span>{{
              {
                pending: "未开始",
                saved: "已导入",
                failed: "失败",
                unknown: "结果未知",
                skipped: "已跳过",
              }[item.status]
            }}</span>
          </li>
        </ul>
        <CatalogCommandFeedback
          :error="importing.error.value"
          :uncertain="importing.uncertain.value"
          :replayable="importing.replayable.value"
          :busy="importing.busy.value"
          :observing="importing.observing.value"
          :observation="importing.observation.value"
          :observation-error="importing.observationError.value"
          @observe="importing.observe"
          @retry="recover"
        />
        <UiButton
          v-if="!locked && batch[activeIndex]?.status === 'failed'"
          @click="skipFailed"
          >跳过此项并继续</UiButton
        >
        <UiButton
          v-if="
            !locked &&
            batch.some(
              (item) => item.status === 'pending' || item.status === 'failed',
            )
          "
          @click="continueImport"
          >继续未完成项</UiButton
        ></template
      >
      <div class="form-actions">
        <UiButton v-if="!locked && !batch.length" @click="discover">{{
          result ? "重新发现" : "开始发现"
        }}</UiButton
        ><UiButton
          v-if="discovery.busy.value"
          :disabled="cancelling"
          @click="close"
          >停止等待</UiButton
        ><UiButton v-else-if="running" :disabled="stopped" @click="close"
          >停止后续导入</UiButton
        ><UiButton v-else :disabled="locked" @click="close">关闭</UiButton>
      </div>
    </div></UiDialog
  >
</template>
<style scoped>
.import-configuration {
  border-block: 1px solid var(--border-subtle);
  padding: 12px 0;
  min-width: 0;
}
.import-configuration summary {
  cursor: pointer;
  overflow-wrap: anywhere;
  margin-bottom: 16px;
  font-size: 14px;
  font-weight: 550;
}
.discovery-items {
  max-height: 240px;
  overflow: auto;
  display: grid;
  gap: 10px;
  border-block: 1px solid var(--border-subtle);
  padding: 12px 0;
}
.discovery-items label {
  display: flex;
  gap: 9px;
  align-items: center;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.import-results {
  padding: 0;
  margin: 0;
  list-style: none;
  max-height: 300px;
  overflow: auto;
}
.import-results li {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 0;
  border-bottom: 1px solid var(--border-subtle);
  font-size: 13px;
}
.import-results li span:first-child {
  overflow-wrap: anywhere;
}
.import-results li span:last-child {
  white-space: nowrap;
  color: var(--text-secondary);
}
</style>
