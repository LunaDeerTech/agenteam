<script setup lang="ts">
import ProviderEditor from "./ProviderEditor.vue";
import type { Provider } from "../../system/model-catalog";
defineProps<{ open: boolean; provider?: Provider }>();
defineEmits<{ "update:open": [value: boolean]; saved: [provider: Provider] }>();
</script>
<template>
  <ProviderEditor
    :open="open"
    :provider="provider"
    @update:open="$emit('update:open', $event)"
    @saved="$emit('saved', $event)"
  />
</template>
