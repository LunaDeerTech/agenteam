<script setup lang="ts">
defineProps<{ code: string; plain?: boolean }>()
</script>
<template>
  <pre class="ui-code" :class="{ plain }"><code>{{ code }}</code></pre>
</template>
