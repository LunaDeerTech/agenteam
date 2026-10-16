<script setup lang="ts">
import { inject } from "vue";
import { settingsActionsTarget } from "./settingsActions";

const target = inject(settingsActionsTarget);
</script>

<template>
  <Teleport v-if="target" :to="target"><slot /></Teleport>
</template>
