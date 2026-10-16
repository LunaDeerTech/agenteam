<script setup lang="ts">
import UiDialog from './UiDialog.vue'
import type { CloseReason } from './types'
const open = defineModel<boolean>('open', { default: false })
withDefaults(defineProps<{ title: string; closeOnOutside?: boolean; closeOnEscape?: boolean }>(), {
  closeOnOutside: true,
  closeOnEscape: true,
})
defineEmits<{ close: [reason: CloseReason] }>()
</script>
<template>
  <UiDialog
    v-model:open="open"
    drawer
    :title="title"
    :close-on-outside="closeOnOutside"
    :close-on-escape="closeOnEscape"
    @close="$emit('close', $event)"
    ><slot /><template v-if="$slots.footer" #footer="scope"
      ><slot name="footer" v-bind="scope" /></template
  ></UiDialog>
</template>
