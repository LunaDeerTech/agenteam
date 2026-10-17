<script setup lang="ts">
import { nextTick, onMounted, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { UiButton } from "../../components/ui";
import SystemList from "../../components/system/SystemList.vue";
import ProviderEditorDialog from "../../components/system/ProviderEditorDialog.vue";
import ProviderStatusSwitch from "../../components/system/ProviderStatusSwitch.vue";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import { accountEnabled } from "../../account/transport";
import { listProviders, type Provider } from "../../system/model-catalog";
import {
  rememberCatalog,
  consumeCatalog,
} from "../../system/catalog-navigation";
import { useCursorList } from "../../system/useCursorList";
import { useSystemAccess } from "../../system/requests";
const access = useSystemAccess();
const router = useRouter();
const lockedRows = ref(new Set<string>());
function rowLocked(id: string, locked: boolean) {
  const next = new Set(lockedRows.value);
  if (locked) next.add(id);
  else next.delete(id);
  lockedRows.value = next;
}
function providerSaved(provider: Provider) {
  list.updateItem(
    (item) => item.id === provider.id,
    () => provider,
  );
}
async function created(provider: Provider) {
  adding.value = false;
  await nextTick();
  void router.push(`/system/models/${provider.id}?tab=config`);
}
const page = ref<HTMLElement>();
const scrollContainer = () =>
  page.value?.closest<HTMLElement>(".settings-main");
const returned = consumeCatalog();
const list = useCursorList(listProviders, { immediate: !returned });
onMounted(async () => {
  if (!returned) return;
  await list.restore(returned.position);
  await nextTick();
  const container = scrollContainer();
  if (container) container.scrollTop = returned.scroll;
  document
    .getElementById(`provider-${returned.focusId}`)
    ?.focus({ preventScroll: true });
});
const listView = ref<{ refresh: () => Promise<void> }>();
const adding = ref(false);
function remember(id: string) {
  rememberCatalog(list.position(), id, scrollContainer()?.scrollTop ?? 0);
}
</script>
<template>
  <div ref="page">
    <SettingsPageActions
      ><UiButton
        :disabled="
          !accountEnabled || !access.allowed.value || !!lockedRows.size
        "
        :loading="list.pending.value"
        @click="listView?.refresh()"
        >刷新</UiButton
      ><UiButton
        variant="primary"
        :disabled="
          !accountEnabled || !access.allowed.value || !!lockedRows.size
        "
        @click="adding = true"
        >添加供应商</UiButton
      ></SettingsPageActions
    >
    <p v-if="!accountEnabled" class="form-feedback">
      连接账号服务后可管理模型接入。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限，目录已清除。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <h2 class="provider-list-title">供应商列表</h2>
    <SystemList
      ref="listView"
      :list="list"
      :row-key="(item) => item.id"
      label="供应商目录"
      empty-text="尚未添加供应商。"
      :disabled="!accountEnabled || !access.allowed.value || !!lockedRows.size"
    >
      <template #item="{ item }"
        ><RouterLink
          :id="`provider-${item.id}`"
          class="provider-link"
          :to="`/system/models/${item.id}`"
          :aria-disabled="!!lockedRows.size || undefined"
          @click="
            (event) => {
              if (lockedRows.size) event.preventDefault();
              else remember(item.id);
            }
          "
          >{{ item.input.name }}</RouterLink
        ><span class="form-note">{{ item.input.protocol }}</span></template
      >
      <template #actions="{ item }"
        ><ProviderStatusSwitch
          :provider="item"
          :disabled="
            list.pending.value ||
            (!!lockedRows.size && !lockedRows.has(item.id))
          "
          @saved="providerSaved"
          @locked="rowLocked(item.id, $event)"
        /><UiButton
          :disabled="
            !!lockedRows.size || list.pending.value || !access.allowed.value
          "
          @click="
            remember(item.id);
            router.push(`/system/models/${item.id}?tab=config`);
          "
          >配置</UiButton
        ></template
      >
    </SystemList>
    <ProviderEditorDialog v-model:open="adding" @saved="created" />
  </div>
</template>
<style scoped>
.provider-link {
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 550;
  text-decoration: none;
  overflow-wrap: anywhere;
}
.provider-link:hover {
  text-decoration: underline;
}
.provider-list-title {
  margin: 0 0 20px;
  font-size: 15px;
  font-weight: 550;
}
:deep(.system-list-actions) {
  align-items: flex-start;
}
@media (max-width: 480px) {
  :deep(.system-list-row) {
    flex-wrap: wrap;
  }
  :deep(.system-list-actions) {
    max-width: 100%;
    flex-direction: column;
    align-items: flex-end;
    gap: 5px;
  }
}
</style>
