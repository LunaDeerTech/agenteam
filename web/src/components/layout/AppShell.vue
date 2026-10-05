<script setup lang="ts">
import SystemNav from './SystemNav.vue'
</script>
<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">跳转到内容</a
    ><SystemNav><slot name="account" /></SystemNav>
    <main id="main-content" tabindex="-1" class="app-content"><slot /></main>
  </div>
</template>
