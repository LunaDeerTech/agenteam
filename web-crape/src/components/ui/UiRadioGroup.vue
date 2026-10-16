<script setup lang="ts">
import { useId } from 'vue'
import type { ChoiceOption } from './types'
const model = defineModel<string>({ default: '' })
const props = defineProps<{
  options: ChoiceOption[]
  legend: string
  name?: string
  disabled?: boolean
}>()
const group = props.name || useId()
</script>
<template>
  <fieldset class="ui-radio-group" :disabled="disabled">
    <legend>{{ legend }}</legend>
    <div class="ui-row">
      <label
        v-for="option in options"
        :key="option.value"
        class="ui-choice"
        :class="{ disabled: option.disabled || disabled }"
        ><input
          v-model="model"
          type="radio"
          :name="group"
          :value="option.value"
          :disabled="option.disabled"
        /><span>{{ option.label }}</span></label
      >
    </div>
  </fieldset>
</template>
