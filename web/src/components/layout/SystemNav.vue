<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSession } from '../../composables/useSession'
import AppBrand from './AppBrand.vue'
const router = useRouter()
const route = useRoute()
const auth = useSession()
const entries = computed(() =>
  router
    .getRoutes()
    .filter(
      (r) =>
        r.meta.navigation &&
        (!r.meta.systemAdmin ||
          (auth.state.phase === 'authenticated' &&
            auth.state.user?.role === 'admin' &&
            !auth.system.denied)),
    )
    .sort((a, b) => a.meta.navigation!.order - b.meta.navigation!.order),
)
function current(path: string) {
  return path === '/projects'
    ? route.meta.projectWorkspace === true
    : route.matched.some((entry) => entry.path === path)
}
</script>
<template>
  <header class="system-nav">
    <RouterLink class="brand" to="/" aria-label="agenteam 入口"><AppBrand /></RouterLink>
    <nav aria-label="系统导航">
      <RouterLink
        v-for="entry in entries"
        :key="entry.path"
        :to="entry.path"
        :aria-current="current(entry.path) ? 'page' : undefined"
        >{{ entry.meta.navigation?.label }}</RouterLink
      >
    </nav>
    <slot />
  </header>
</template>
