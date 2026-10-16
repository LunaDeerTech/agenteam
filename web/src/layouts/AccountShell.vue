<script setup lang="ts">
import { RouterLink, useRoute } from "vue-router";
import { computed } from "vue";
import BrandLogo from "./BrandLogo.vue";
import PageHeading from "./PageHeading.vue";
const route = useRoute();
const panel = computed(
  () => route.name === "login" || route.name === "forgot-password",
);
</script>

<template>
  <div class="account-shell" :class="{ 'account-shell--panel': panel }">
    <header v-if="!panel" class="account-brand-header">
      <RouterLink to="/" aria-label="agenteam 首页">
        <BrandLogo class="account-logo" />
      </RouterLink>
    </header>
    <main id="main" tabindex="-1" :class="{ 'account-panel': panel }">
      <div v-if="panel" class="account-panel-brand">
        <RouterLink to="/" aria-label="agenteam 首页"
          ><BrandLogo class="account-logo"
        /></RouterLink>
      </div>
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
.account-brand-header {
  padding: 20px 28px;
}
.account-brand-header a {
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
.account-shell--panel {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 48px 24px;
}
main.account-panel {
  width: 100%;
  max-width: 440px;
  margin: 0;
  padding: 36px;
  border: 1px solid var(--border-subtle);
  border-radius: 12px;
  background: var(--surface-content);
  box-shadow: 0 12px 40px rgb(0 0 0 / 5%);
}
.account-panel-brand {
  margin-bottom: 28px;
}
.account-panel-brand a {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
}
@media (max-width: 760px) {
  .account-brand-header {
    padding: 18px 22px;
  }
  main {
    margin-top: 48px;
  }
  .account-shell--panel {
    padding: 28px 20px;
  }
  main.account-panel {
    padding: 28px 24px;
  }
}
</style>
