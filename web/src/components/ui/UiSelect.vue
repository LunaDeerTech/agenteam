<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import UiPopover from './UiPopover.vue'
import UiIcon from './UiIcon.vue'
import { optionKeys } from '../../composables/useOptionKeys'
import type { ChoiceOption } from './types'
const model = defineModel<string>({ default: '' })
const props = defineProps<{
  options: ChoiceOption[]
  label: string
  disabled?: boolean
  invalid?: boolean
  id?: string
}>()
const open = ref(false)
const listId = useId()
const selected = computed(() => props.options.find((o) => o.value === model.value))
</script>
<template>
  <UiPopover
    :id="listId"
    :trigger-id="id"
    v-model:open="open"
    class="ui-select"
    :label="label"
    :aria-label="label"
    :aria-invalid="invalid || undefined"
    :disabled="disabled"
    role="listbox"
    match-width
    @keydown="optionKeys"
    ><template #trigger
      ><span class="truncate">{{ selected?.label || '请选择' }}</span
      ><UiIcon name="chevron" class="select-chevron" /></template
    ><template #default="{ close }"
      ><button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="menu-item"
        role="option"
        :aria-selected="model === option.value"
        :data-autofocus="model === option.value && !option.disabled ? '' : undefined"
        :disabled="option.disabled"
        @click="
          () => {
            model = option.value
            close()
          }
        "
      >
        <span>{{ option.label }}</span
        ><UiIcon v-if="model === option.value" name="check" />
      </button>
      <p v-if="!options.length" class="popover-copy">没有可选项</p></template
    ></UiPopover
  >
</template>
