<script setup lang="ts">
import { computed, provide, shallowReadonly, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { UiDialog, UiIconButton } from "../components/ui";
import {
  systemSettings,
  projectSettings,
  personalSettings,
} from "../router/routes";
import PageHeading from "./PageHeading.vue";
import SettingsNavigation from "./SettingsNavigation.vue";
import { settingsActionsTarget } from "./settingsActions";

const route = useRoute();
const directoryOpen = ref(false);
const actionsTarget = ref<HTMLElement>();
provide(settingsActionsTarget, shallowReadonly(actionsTarget));
const scope = computed(() => route.meta.settingsScope || "system");
const wideBody = computed(
  () =>
    scope.value === "system" &&
    ["users", "invitations", "smtp", "outbound-policy"].includes(
      String(route.meta.settingKey),
    ),
);
const title = computed(
  () =>
    ({ system: "系统设置", personal: "个人设置", project: "项目设置" })[
      scope.value
    ],
);
const pages = computed(
  () =>
    ({
      system: systemSettings,
      personal: personalSettings,
      project: projectSettings,
    })[scope.value],
);
watch(
  () => route.fullPath,
  () => {
    directoryOpen.value = false;
  },
);
</script>

<template>
  <div class="settings-shell" :data-settings-scope="scope">
    <aside class="settings-sidebar">
      <h2>{{ title }}</h2>
      <SettingsNavigation :pages="pages" :scope="scope" :label="title" />
    </aside>
    <section class="settings-main" aria-labelledby="page-title">
      <PageHeading
        :title="route.meta.title || ''"
        :description="route.meta.description"
      >
        <template #actions>
          <div class="settings-heading-actions">
            <div ref="actionsTarget" class="settings-page-actions" />
            <UiIconButton
              class="settings-directory-trigger"
              icon="menu"
              :label="`打开${title}目录`"
              @click="directoryOpen = true"
            />
          </div>
        </template>
      </PageHeading>
      <div
        class="settings-page-body"
        :class="{ 'settings-page-body--wide': wideBody }"
      >
        <slot />
      </div>
    </section>
    <UiDialog v-model:open="directoryOpen" :title="title" side="left">
      <SettingsNavigation
        :pages="pages"
        :scope="scope"
        :label="`${title}目录`"
        @navigate="directoryOpen = false"
      />
    </UiDialog>
  </div>
</template>

<style scoped>
.settings-shell {
  display: grid;
  grid-template-columns: var(--sidebar-width) minmax(0, 1fr);
  min-height: 0;
  flex: 1;
}
.settings-sidebar {
  min-height: 0;
  padding: 28px 17px 36px;
  background: var(--surface-page);
  border-right: 1px solid var(--border-subtle);
  overflow-y: auto;
}
.settings-sidebar h2 {
  margin: 0 11px 22px;
  font-size: 12px;
  font-weight: 600;
  line-height: 1.5;
}
.settings-main {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  scroll-padding-block: 12px;
  padding: 43px 48px 60px;
}
.settings-page-body {
  max-width: 760px;
  min-height: 240px;
}
.settings-page-body--wide {
  max-width: none;
}
.settings-directory-trigger {
  display: none;
}
.settings-heading-actions,
.settings-page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.settings-page-actions:empty {
  display: none;
}
@media (max-width: 1050px) {
  .settings-main {
    padding: 38px 32px 48px;
  }
}
@media (max-width: 760px) {
  .settings-shell {
    display: flex;
  }
  .settings-sidebar {
    display: none;
  }
  .settings-main {
    flex: 1;
    padding: 27px 19px 35px;
  }
  .settings-directory-trigger {
    display: inline-flex;
  }
}
</style>
