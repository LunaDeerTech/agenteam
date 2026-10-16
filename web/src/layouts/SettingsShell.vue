<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { UiDialog, UiIconButton } from "../components/ui";
import {
  systemSettings,
  projectSettings,
  personalSettings,
} from "../router/routes";
import PageHeading from "./PageHeading.vue";
import SettingsNavigation from "./SettingsNavigation.vue";

const route = useRoute();
const directoryOpen = ref(false);
const scope = computed(() => route.meta.settingsScope || "system");
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
        <template #actions
          ><UiIconButton
            class="settings-directory-trigger"
            icon="menu"
            :label="`打开${title}目录`"
            @click="directoryOpen = true"
        /></template>
      </PageHeading>
      <div class="settings-page-body"><slot /></div>
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
  margin: 0 11px 25px;
  font-size: 13px;
  font-weight: 550;
  line-height: 1.5;
}
.settings-main {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  padding: 45px 48px 64px;
}
.settings-page-body {
  max-width: 800px;
  min-height: 240px;
}
.settings-directory-trigger {
  display: none;
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
    padding: 28px 19px 48px;
  }
  .settings-directory-trigger {
    display: inline-flex;
  }
}
</style>
