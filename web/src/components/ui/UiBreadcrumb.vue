<script setup lang="ts">
defineProps<{ items: { label: string; to?: string }[] }>()
</script>
<template>
  <nav aria-label="面包屑">
    <ol class="ui-breadcrumb">
      <li v-for="(item, index) in items" :key="index">
        <span v-if="index" aria-hidden="true">/</span
        ><RouterLink v-if="item.to && index !== items.length - 1" :to="item.to">{{
          item.label
        }}</RouterLink
        ><span v-else :aria-current="index === items.length - 1 ? 'page' : undefined">{{
          item.label
        }}</span>
      </li>
    </ol>
  </nav>
</template>
