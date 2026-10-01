<script setup lang="ts">
import { useId } from 'vue'
import UiIcon from './UiIcon.vue'
const open = defineModel<boolean>('open', { default: false })
withDefaults(defineProps<{ label: string; maxHeight?: string }>(), {
  maxHeight: 'min(320px, 40dvh)',
})
const id = useId()
</script>
<template>
  <div class="ui-collapse">
    <button
      type="button"
      class="collapse-trigger"
      :aria-expanded="open"
      :aria-controls="id"
      @click="open = !open"
    >
      <UiIcon name="chevron" :class="{ rotated: open }" />{{ label }}
    </button>
    <div
      :id="id"
      class="collapse-grid"
      :class="{ expanded: open }"
      :inert="!open"
      :aria-hidden="!open"
    >
      <div class="collapse-inner">
        <div
          class="collapse-content"
          :style="{ maxHeight }"
          role="region"
          :aria-label="label"
          :tabindex="open ? 0 : -1"
        >
          <slot />
        </div>
      </div>
    </div>
  </div>
</template>
