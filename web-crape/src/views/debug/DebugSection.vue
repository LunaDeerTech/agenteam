<script setup lang="ts">
defineProps<{ id: string; title: string; description?: string }>()
</script>
<template>
  <section :id="id" tabindex="-1" class="debug-section">
    <header>
      <h2>{{ title }}</h2>
      <p v-if="description" class="meta">{{ description }}</p>
    </header>
    <div class="ui-stack"><slot /></div>
  </section>
</template>
