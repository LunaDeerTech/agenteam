<script setup lang="ts">
import { RouterLink, useRoute } from "vue-router";
import BrandLogo from "./BrandLogo.vue";
import PageHeading from "./PageHeading.vue";
const route = useRoute();
</script>

<template>
  <div class="account-shell">
    <header>
      <RouterLink to="/" aria-label="agenteam 首页">
        <BrandLogo class="account-logo" />
      </RouterLink>
    </header>
    <main id="main" tabindex="-1">
      <PageHeading
        :title="route.meta.title || ''"
        :description="route.meta.description"
      /><slot />
    </main>
  </div>
</template>

<style scoped>
.account-shell {
  min-height: 100dvh;
  background: var(--surface-page);
}
header {
  padding: 20px 28px;
}
header a {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  text-decoration: none;
}
.account-logo {
  width: 152px;
  height: 40px;
}
main {
  max-width: 420px;
  margin: 64px auto 0;
  padding: 0 24px 64px;
}
@media (max-width: 760px) {
  header {
    padding: 18px 22px;
  }
  main {
    margin-top: 48px;
  }
}
</style>
