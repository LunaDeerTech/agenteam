<script setup lang="ts">
import { useId } from 'vue'
const props = defineProps<{
  label: string
  id?: string
  hint?: string
  error?: string
  required?: boolean
}>()
const generated = useId()
const controlId = props.id || generated
</script>
<template>
  <div class="ui-field">
    <label :for="controlId">{{ label }}<span v-if="required" aria-hidden="true"> *</span></label
    ><slot
      :id="controlId"
      :invalid="!!error"
      :describedby="error || hint ? controlId + '-note' : undefined"
    />
    <p
      v-if="error || hint"
      :id="controlId + '-note'"
      class="field-note"
      :class="{ 'error-text': error }"
      :role="error ? 'alert' : undefined"
    >
      {{ error || hint }}
    </p>
  </div>
</template>
