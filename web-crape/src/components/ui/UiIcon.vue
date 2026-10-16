<script setup lang="ts">
withDefaults(
  defineProps<{
    name: 'check' | 'chevron' | 'close' | 'search' | 'file' | 'more' | 'arrow-left' | 'arrow-right'
  }>(),
  { name: 'chevron' },
)
const paths = {
  check: 'M5 12l4 4L19 6',
  chevron: 'M8 5l7 7-7 7',
  close: 'M6 6l12 12M18 6L6 18',
  search: 'M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
  file: 'M14 2H5v20h14V7l-5-5v5h5M8 12h8M8 16h8',
  more: 'M5 12h.01M12 12h.01M19 12h.01',
  'arrow-left': 'M14 5l-7 7 7 7',
  'arrow-right': 'M10 5l7 7-7 7',
}
</script>
<template>
  <svg
    class="ui-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[name]" />
  </svg>
</template>
