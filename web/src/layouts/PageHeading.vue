<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>

<template>
  <header class="page-heading">
    <div>
      <h1 id="page-title" tabindex="-1">{{ title }}</h1>
      <p v-if="description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="page-heading-actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.page-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:24px; margin:0 0 32px; }
.page-heading > div:first-child { min-width:0; }
h1 { margin:0; color:var(--text-primary); font-size:24px; line-height:1.4; font-weight:600; letter-spacing:-.025em; overflow-wrap:anywhere; }
h1:focus { outline:none; }
p { margin:8px 0 0; color:var(--text-secondary); font-size:13px; line-height:1.7; overflow-wrap:anywhere; }
.page-heading-actions { flex:none; }
@media(max-width:760px) { .page-heading { gap:12px; margin-bottom:28px; } h1 { font-size:22px; } }
</style>
