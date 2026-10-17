<script setup lang="ts">
import { nextTick, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { UiButton } from "../../components/ui";
import SystemList from "../../components/system/SystemList.vue";
import ProviderEditorDialog from "../../components/system/ProviderEditorDialog.vue";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import { accountEnabled } from "../../account/transport";
import { listProviders } from "../../system/model-catalog";
import {
  rememberCatalog,
  consumeCatalog,
} from "../../system/catalog-navigation";
import { useCursorList } from "../../system/useCursorList";
import { useSystemAccess } from "../../system/requests";
const access = useSystemAccess();
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
        :disabled="!accountEnabled || !access.allowed.value"
        :loading="list.pending.value"
        @click="listView?.refresh()"
        >刷新</UiButton
      ><UiButton
        variant="primary"
        :disabled="!accountEnabled || !access.allowed.value"
        @click="adding = true"
        >添加供应商</UiButton
      ></SettingsPageActions
    >
    <p v-if="!accountEnabled" class="form-feedback">
      连接账号服务后可管理模型目录。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限，目录已清除。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <SystemList
      ref="listView"
      :list="list"
      :row-key="(item) => item.id"
      label="供应商目录"
      empty-text="尚未添加供应商。"
      :disabled="!accountEnabled || !access.allowed.value"
    >
      <template #item="{ item }"
        ><RouterLink
          :id="`provider-${item.id}`"
          class="provider-link"
          :to="`/system/models/${item.id}`"
          @click="remember(item.id)"
          >{{ item.input.name }}</RouterLink
        ><span class="form-note">{{ item.input.protocol }}</span></template
      >
      <template #actions="{ item }"
        ><span class="provider-state">{{
          item.input.enabled ? "已启用" : "已停用"
        }}</span
        ><span class="provider-state">{{
          item.input.credential_ref ? "已配置凭据" : "未配置凭据"
        }}</span></template
      >
    </SystemList>
    <ProviderEditorDialog v-model:open="adding" @saved="list.first()" />
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
.provider-state {
  color: var(--text-secondary);
  font-size: 12px;
  white-space: nowrap;
}
@media (max-width: 480px) {
  :deep(.system-list-actions) {
    flex-direction: column;
    align-items: flex-end;
    gap: 5px;
  }
}
</style>
