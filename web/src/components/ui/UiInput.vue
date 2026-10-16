<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue';

defineOptions({ inheritAttrs: false });
const props = withDefaults(defineProps<{ id?: string; label: string; hint?: string; error?: string; disabled?: boolean; type?: string }>(), { type: 'text' });
const model = defineModel<string>({ default: '' });
const attrs = useAttrs();
const generatedId = useId();
const inputId = computed(() => props.id ?? `${generatedId}-input`);
const description = computed(() => [attrs['aria-describedby'], props.hint ? `${inputId.value}-hint` : '', props.error ? `${inputId.value}-error` : ''].filter(Boolean).join(' ') || undefined);
</script>

<template>
  <div class="ui-field">
    <label class="ui-field-label" :for="inputId">{{ label }}</label>
    <input v-bind="$attrs" :id="inputId" v-model="model" class="ui-input" :type="type" :disabled="disabled" :aria-invalid="error ? true : undefined" :aria-describedby="description" />
    <p v-if="hint" :id="`${inputId}-hint`" class="ui-field-hint">{{ hint }}</p>
    <p v-if="error" :id="`${inputId}-error`" class="ui-field-error">{{ error }}</p>
  </div>
</template>
