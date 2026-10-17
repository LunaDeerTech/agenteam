<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { UiButton, UiMenu, UiTabs } from "../../components/ui";
import SystemList from "../../components/system/SystemList.vue";
import ProviderEditor from "../../components/system/ProviderEditor.vue";
import ModelEditorDialog from "../../components/system/ModelEditorDialog.vue";
import ModelStatusSwitch from "../../components/system/ModelStatusSwitch.vue";
import ModelDiscoveryDialog from "../../components/system/ModelDiscoveryDialog.vue";
import ModelTestDialog from "../../components/system/ModelTestDialog.vue";
import CatalogDeleteDialog from "../../components/system/CatalogDeleteDialog.vue";
import {
  getModelCapabilities,
  type ProtocolCapability,
} from "../../system/model-operations";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import { useSettingsHeading } from "../../layouts/settingsHeading";
import { accountEnabled } from "../../account/transport";
import {
  catalogError,
  editableProtocols,
  getProvider,
  listModels,
  modelTypeChoices,
  type Provider,
  type CatalogModel,
} from "../../system/model-catalog";
import { useCursorList } from "../../system/useCursorList";
import { useSystemAccess } from "../../system/requests";
const route = useRoute();
const router = useRouter();
const access = useSystemAccess();
const id = String(route.params.providerId);
const provider = shallowRef<Provider>();
const reading = ref(false);
const error = shallowRef<unknown>();
const tab = ref(route.query.tab === "config" ? "config" : "models");
const configLocked = ref(false);
const lockedRows = ref(new Set<string>());
const pageLocked = computed(
  () => configLocked.value || !!lockedRows.value.size,
);
const listView = ref<{
  focusResults: (options?: { scroll?: boolean }) => Promise<void>;
}>();
function rowLocked(id: string, locked: boolean) {
  const next = new Set(lockedRows.value);
  if (locked) next.add(id);
  else next.delete(id);
  lockedRows.value = next;
}
function modelSaved(model: CatalogModel) {
  list.updateItem(
    (item) => item.id === model.id,
    (item) => ({ ...item, ...model }),
  );
}
async function modelDeleted() {
  await list.refresh({ preserve: true });
  await nextTick();
  await listView.value?.focusResults({ scroll: false });
}
const tabs = computed(() => [
  {
    value: "models",
    label: "模型",
    disabled: pageLocked.value && tab.value !== "models",
  },
  {
    value: "config",
    label: "配置",
    disabled: pageLocked.value && tab.value !== "config",
  },
]);
const modelEditing = ref(false);
const selectedModel = shallowRef<CatalogModel>();
const discovering = ref(false);
const testing = ref(false);
const deleting = ref(false);
function providerSaved(value: Provider) {
  provider.value = value;
}
const capabilities = shallowRef<ProtocolCapability[]>();
const capabilityError = shallowRef<unknown>();
const capability = computed(() =>
  capabilities.value?.find(
    (value) => value.protocol === provider.value?.input.protocol,
  ),
);
const headerActions = computed(() => [
  { id: "refresh", label: "刷新", disabled: pageLocked.value },
  {
    id: "discovery",
    label: "发现模型",
    disabled:
      !editable.value ||
      !capability.value?.discovery ||
      !provider.value?.input.enabled,
  },
  { id: "delete", label: "删除供应商", disabled: !editable.value },
]);
function headerAction(action: string) {
  if (pageLocked.value || !access.allowed.value) return;
  if (action === "refresh") {
    void load();
    void list.refresh();
  } else if (action === "discovery") {
    discovering.value = true;
  } else {
    selectedModel.value = undefined;
    deleting.value = true;
  }
}
function modelAction(model: CatalogModel, action: string) {
  if (pageLocked.value || list.pending.value || !editable.value) return;
  selectedModel.value = model;
  if (action === "edit") modelEditing.value = true;
  if (action === "test" && canTest(model)) testing.value = true;
}
function canTest(model: CatalogModel) {
  return (
    editable.value &&
    capability.value?.test &&
    capability.value.model_types.includes(model.input.type) &&
    model.input.enabled &&
    provider.value?.input.enabled
  );
}
function deleted() {
  if (selectedModel.value) {
    void list.refresh();
  } else void router.push("/system/models");
}
async function loadCapabilities() {
  const token = sequence;
  capabilityError.value = undefined;
  try {
    const value = await getModelCapabilities(
      () => alive && token === sequence && access.allowed.value,
    );
    if (token === sequence) capabilities.value = value.protocols;
  } catch (e) {
    if (token === sequence) capabilityError.value = e;
  }
}

const editable = computed(
  () =>
    access.allowed.value &&
    !!provider.value &&
    editableProtocols.includes(provider.value.input.protocol),
);
const list = useCursorList(
  (cursor, current, limit) => listModels(id, cursor, current, limit),
  { enabled: () => !!provider.value },
);
let sequence = 0;
let alive = true;
useSettingsHeading(() => ({
  title: provider.value?.input.name ?? "供应商",
  back: { to: "/system/models", label: "模型接入" },
}));
async function load() {
  if (!accountEnabled || !access.allowed.value) return;
  const owner = ++sequence;
  reading.value = true;
  error.value = undefined;
  try {
    const value = await getProvider(
      id,
      () => alive && owner === sequence && access.allowed.value,
    );
    if (owner === sequence) {
      provider.value = value;
      void loadCapabilities();
    }
  } catch (failure) {
    if (owner === sequence) error.value = failure;
  } finally {
    if (owner === sequence) reading.value = false;
  }
}
watch(
  access.generation,
  () => {
    sequence++;
    provider.value = undefined;
    configLocked.value = false;
    lockedRows.value = new Set();
    modelEditing.value =
      discovering.value =
      testing.value =
      deleting.value =
        false;
    selectedModel.value = undefined;
    capabilities.value = undefined;
    capabilityError.value = undefined;
    error.value = undefined;
  },
  { flush: "sync" },
);
watch(
  access.allowed,
  (allowed) => {
    if (allowed) void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  alive = false;
  sequence++;
});
</script>
<template>
  <div>
    <SettingsPageActions>
      <UiButton
        v-if="tab === 'models'"
        variant="primary"
        :disabled="!editable || pageLocked || list.pending.value"
        @click="
          selectedModel = undefined;
          modelEditing = true;
        "
        >添加模型</UiButton
      >
      <UiMenu
        label="更多"
        :items="headerActions"
        :disabled="!provider || reading || pageLocked || list.pending.value"
        @select="headerAction"
      />
    </SettingsPageActions>
    <p v-if="reading && !provider" class="form-note" role="status">
      正在读取供应商…
    </p>
    <div v-if="error" class="form-feedback" role="alert">
      <p>{{ catalogError(error) }}</p>
      <UiButton :loading="reading" @click="load">重新读取</UiButton>
    </div>
    <div v-if="access.denied.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <template v-if="provider">
      <p v-if="!editable" class="form-feedback">此供应商协议当前只支持查看。</p>
      <UiTabs v-model="tab" label="供应商设置" :items="tabs" v-slot="{ value }">
        <div v-show="value === 'models'">
          <div v-if="capabilityError" class="form-feedback" role="alert">
            <p>模型发现与测试能力尚未读到。</p>
            <UiButton @click="loadCapabilities">重新读取能力</UiButton>
          </div>
          <SystemList
            ref="listView"
            :list="list"
            :row-key="(item) => item.id"
            label="模型接入"
            empty-text="此供应商尚未添加模型。"
            :disabled="!access.allowed.value || pageLocked"
          >
            <template #table="{ items }"
              ><div class="model-table-scroll">
                <table class="model-table" aria-label="供应商模型">
                  <thead>
                    <tr>
                      <th>模型</th>
                      <th>类型</th>
                      <th>启用</th>
                      <th><span class="sr-only">操作</span></th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="model in items" :key="model.id">
                      <td>
                        {{ model.input.provider_model_id }}
                      </td>
                      <td>
                        <span class="model-type-tag">
                          {{
                            modelTypeChoices.find(
                              (type) => type.value === model.input.type,
                            )?.label
                          }}</span
                        >
                      </td>
                      <td
                        class="model-status-cell"
                        :class="{ 'is-active': lockedRows.has(model.id) }"
                      >
                        <ModelStatusSwitch
                          :provider="provider"
                          :model="model"
                          :disabled="
                            list.pending.value ||
                            (pageLocked && !lockedRows.has(model.id))
                          "
                          @saved="modelSaved"
                          @locked="rowLocked(model.id, $event)"
                        />
                      </td>
                      <td>
                        <div class="model-row-actions">
                          <UiButton
                            :disabled="
                              !editable || pageLocked || list.pending.value
                            "
                            @click="modelAction(model, 'edit')"
                            >编辑</UiButton
                          >
                          <UiButton
                            :disabled="
                              !canTest(model) ||
                              pageLocked ||
                              list.pending.value
                            "
                            @click="modelAction(model, 'test')"
                            >测试</UiButton
                          >
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div></template
            >
          </SystemList>
        </div>
        <ProviderEditor
          v-show="value === 'config'"
          :open="true"
          inline
          :provider="provider"
          @saved="providerSaved"
          @locked-change="configLocked = $event"
        />
      </UiTabs>
    </template>
    <template v-if="provider">
      <ModelEditorDialog
        v-model:open="modelEditing"
        :provider="provider"
        :model="selectedModel"
        @saved="list.refresh({ preserve: true })"
        @deleted="modelDeleted"
      />
      <ModelDiscoveryDialog
        v-model:open="discovering"
        :provider="provider"
        @imported="list.refresh()"
      />
      <ModelTestDialog
        v-if="selectedModel"
        v-model:open="testing"
        :provider="provider"
        :model="selectedModel"
      />
      <CatalogDeleteDialog
        v-model:open="deleting"
        :provider="provider"
        :model="selectedModel"
        @deleted="deleted"
      />
    </template>
  </div>
</template>
<style scoped>
.model-row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.model-type-tag {
  display: inline-flex;
  align-items: center;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  background: var(--surface-subtle);
  color: var(--text-secondary);
  padding: 2px 7px;
  font-size: var(--font-size-caption);
  white-space: nowrap;
}
.model-table-scroll {
  position: relative;
  overflow-x: auto;
}
.model-table {
  border-collapse: collapse;
  width: 100%;
  font-size: 13px;
  min-width: 0;
  text-align: left;
}
.model-table th {
  color: var(--text-secondary);
  font-weight: 500;
}
.model-table th,
.model-table td {
  padding: 13px 12px;
  border-bottom: 1px solid var(--border-subtle);
  overflow-wrap: anywhere;
}
.model-table th:first-child,
.model-table td:first-child {
  padding-left: 0;
}
.model-table th:last-child,
.model-table td:last-child {
  padding-right: 0;
}
.model-table th:nth-child(2),
.model-table td:nth-child(2),
.model-table th:last-child,
.model-table td:last-child {
  width: 1%;
  white-space: nowrap;
}
.model-status-cell {
  min-width: 56px;
}
@media (max-width: 600px) {
  .model-table thead {
    display: none;
  }
  .model-table tbody {
    display: block;
  }
  .model-table tr {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    padding: 16px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .model-table td,
  .model-table td:nth-child(2),
  .model-table td:last-child {
    width: auto;
    padding: 0;
    border: 0;
    min-width: 0;
  }
  .model-table td:first-child {
    grid-column: 1 / -1;
  }
  .model-status-cell {
    grid-column: 2;
    justify-self: end;
  }
  .model-status-cell.is-active {
    grid-column: 1 / -1;
    justify-self: stretch;
  }
  .model-table td:last-child {
    grid-column: 1 / -1;
  }
}
</style>
