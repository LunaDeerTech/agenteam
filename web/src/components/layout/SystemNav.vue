<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
const router = useRouter()
const entries = computed(() =>
  router
    .getRoutes()
    .filter((r) => r.meta.navigation)
    .sort((a, b) => a.meta.navigation!.order - b.meta.navigation!.order),
)
</script>
<template>
  <header class="system-nav">
    <RouterLink class="brand" to="/" aria-label="agenteam 入口"
      ><span class="brand-mark" aria-hidden="true">a</span>agenteam</RouterLink
    >
    <nav aria-label="系统导航">
      <RouterLink v-for="entry in entries" :key="entry.path" :to="entry.path">{{
        entry.meta.navigation?.label
      }}</RouterLink>
    </nav>
    <slot />
  </header>
</template>
