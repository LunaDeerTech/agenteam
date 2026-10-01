<script setup lang="ts">
import UiIcon from './UiIcon.vue'
import UiSpinner from './UiSpinner.vue'
import type { ButtonState } from './types'
withDefaults(
  defineProps<{
    variant?: 'default' | 'primary' | 'ghost' | 'danger'
    state?: ButtonState
    disabled?: boolean
    loadingLabel?: string
    successLabel?: string
    type?: 'button' | 'submit' | 'reset'
    icon?: boolean
  }>(),
  {
    variant: 'default',
    state: 'idle',
    type: 'button',
    loadingLabel: '处理中',
    successLabel: '已完成',
  },
)
</script>
<template>
  <button
    :type="type"
    class="ui-button"
    :class="[variant, { 'icon-only': icon }]"
    :disabled="disabled || state === 'loading'"
    :aria-busy="state === 'loading' || undefined"
    :aria-label="
      state !== 'idle' && !icon ? (state === 'loading' ? loadingLabel : successLabel) : undefined
    "
  >
    <span class="button-content">
      <span
        class="button-label"
        :class="{ 'button-hidden': state !== 'idle' }"
        :aria-hidden="state !== 'idle'"
        ><slot
      /></span>
      <span
        v-if="!icon"
        class="button-label"
        :class="{ 'button-hidden': state !== 'loading' }"
        :aria-hidden="state !== 'loading'"
        :role="state === 'loading' ? 'status' : undefined"
        ><UiSpinner />{{ loadingLabel }}</span
      >
      <span
        v-if="!icon"
        class="button-label"
        :class="{ 'button-hidden': state !== 'success' }"
        :aria-hidden="state !== 'success'"
        :role="state === 'success' ? 'status' : undefined"
        ><UiIcon name="check" />{{ successLabel }}</span
      >
      <span v-if="icon && state === 'loading'" class="button-label"><UiSpinner /></span>
      <span v-if="icon && state === 'success'" class="button-label"><UiIcon name="check" /></span>
    </span>
  </button>
</template>
