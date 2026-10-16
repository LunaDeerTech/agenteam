<script setup lang="ts">
import { nextTick, watch } from "vue";
import { RouterView, useRoute } from "vue-router";
import AppShell from "./layouts/AppShell.vue";
import AccountShell from "./layouts/AccountShell.vue";

const route = useRoute();
watch(
  () => route.fullPath,
  async (_next, previous) => {
    if (!previous) return;
    await nextTick();
    requestAnimationFrame(() =>
      document.getElementById("page-title")?.focus({ preventScroll: true }),
    );
  },
);
</script>

<template>
  <component :is="route.meta.layout === 'account' ? AccountShell : AppShell">
    <RouterView />
  </component>
</template>
