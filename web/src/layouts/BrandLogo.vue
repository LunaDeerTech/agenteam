<script setup lang="ts">
import horizontalLight from "../assets/brand/agenteam-horizontal-light.svg";
import horizontalDark from "../assets/brand/agenteam-horizontal-dark.svg";
import iconLight from "../assets/brand/agenteam-icon-512-light.svg";
import iconDark from "../assets/brand/agenteam-icon-512-dark.svg";
import { useTheme } from "../composables/useTheme";

defineProps<{ compactOnMobile?: boolean }>();
const { resolvedTheme } = useTheme();
</script>

<template>
  <picture
    class="brand-logo"
    :class="{ 'brand-logo--compact': compactOnMobile }"
    aria-hidden="true"
  >
    <source
      v-if="compactOnMobile"
      media="(max-width: 480px)"
      :srcset="resolvedTheme === 'dark' ? iconDark : iconLight"
    />
    <img
      :src="resolvedTheme === 'dark' ? horizontalDark : horizontalLight"
      width="1600"
      height="420"
      alt=""
    />
  </picture>
</template>

<style scoped>
.brand-logo {
  display: block;
  width: 132px;
  height: 35px;
  flex: none;
}
img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
@media (max-width: 480px) {
  .brand-logo--compact {
    width: 36px;
    height: 36px;
  }
}
</style>
