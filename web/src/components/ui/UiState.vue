<script setup lang="ts">
import UiSpinner from './UiSpinner.vue'
defineProps<{ kind: 'loading' | 'empty' | 'error'; title: string; description?: string }>()
</script>
<template>
  <div
    class="ui-state"
    :role="kind === 'error' ? 'alert' : 'status'"
    :aria-busy="kind === 'loading' || undefined"
  >
    <h3>
      <UiSpinner v-if="kind === 'loading'" /><span v-else-if="kind === 'error'" aria-hidden="true"
        >!</span
      >{{ title }}
    </h3>
    <p v-if="description" class="meta">{{ description }}</p>
    <slot />
  </div>
</template>
