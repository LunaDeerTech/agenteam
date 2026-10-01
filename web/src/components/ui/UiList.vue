<script setup lang="ts">
defineProps<{ items: { id: string; label: string; disabled?: boolean }[]; label: string }>()
defineEmits<{ select: [id: string] }>()
</script>
<template>
  <ul class="ui-list" :aria-label="label">
    <li v-for="item in items" :key="item.id">
      <button type="button" :disabled="item.disabled" @click="$emit('select', item.id)">
        <span>{{ item.label }}</span
        ><slot name="suffix" :item="item" />
      </button>
    </li>
  </ul>
</template>
