<script setup lang="ts">
import { ref, useId } from 'vue'
import { useFloating, autoUpdate, offset, flip, shift } from '@floating-ui/vue'
defineProps<{ text: string }>()
const id = useId()
const anchor = ref<HTMLElement | null>(null)
const tip = ref<HTMLElement | null>(null)
const hovered = ref(false)
const focused = ref(false)
const dismissed = ref(false)
const { floatingStyles } = useFloating(anchor, tip, {
  strategy: 'fixed',
  placement: 'top-start',
  middleware: [offset(6), flip({ padding: 8 }), shift({ padding: 8 })],
  whileElementsMounted: autoUpdate,
})
</script>
<template>
  <span
    class="ui-tooltip-wrap"
    @mouseenter="
      () => {
        hovered = true
        dismissed = false
      }
    "
    @mouseleave="hovered = false"
    ><button
      ref="anchor"
      type="button"
      class="ui-button ghost"
      :aria-describedby="id"
      @focus="
        () => {
          focused = true
          dismissed = false
        }
      "
      @blur="focused = false"
      @keydown.esc.stop="dismissed = true"
    >
      <slot /></button
    ><span
      v-if="(hovered || focused) && !dismissed"
      :id="id"
      ref="tip"
      :style="floatingStyles"
      role="tooltip"
      class="ui-tooltip"
      >{{ text }}</span
    ></span
  >
</template>
