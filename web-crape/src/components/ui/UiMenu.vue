<script setup lang="ts">
import { ref, useId } from 'vue'
import UiPopover from './UiPopover.vue'
import { optionKeys } from '../../composables/useOptionKeys'
import type { MenuAction } from './types'
defineProps<{ label: string; actions: MenuAction[]; disabled?: boolean }>()
const emit = defineEmits<{ select: [id: string] }>()
const open = ref(false)
const id = useId()
</script>
<template>
  <UiPopover
    :id="id"
    v-model:open="open"
    :label="label"
    :disabled="disabled"
    role="menu"
    @keydown="optionKeys"
    ><template #trigger
      ><slot>{{ label }}</slot></template
    ><template #default="{ close }"
      ><button
        v-for="action in actions"
        :key="action.id"
        class="menu-item"
        :class="{ 'error-text': action.danger }"
        type="button"
        role="menuitem"
        :disabled="action.disabled"
        @click="
          () => {
            emit('select', action.id)
            close()
          }
        "
      >
        {{ action.label }}
      </button></template
    ></UiPopover
  >
</template>
