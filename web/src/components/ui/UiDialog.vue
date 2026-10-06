<script setup lang="ts">
import { ref, useId } from 'vue'
import { useLayer } from '../../composables/useLayer'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import type { CloseReason } from './types'
const open = defineModel<boolean>('open', { default: false })
const props = withDefaults(
  defineProps<{
    title: string
    drawer?: boolean
    closeOnOutside?: boolean
    closeOnEscape?: boolean
  }>(),
  { closeOnOutside: true, closeOnEscape: true },
)
const emit = defineEmits<{ close: [reason: CloseReason] }>()
const panel = ref<HTMLElement | null>(null)
const id = useId()
function close(reason: CloseReason) {
  if (
    (reason === 'escape' && !props.closeOnEscape) ||
    (reason === 'outside' && !props.closeOnOutside)
  )
    return
  open.value = false
  emit('close', reason)
}
const layer = useLayer(open, panel, close, true)
function outsidePointerDown(event: PointerEvent) {
  if (!layer.isTop() || !props.closeOnOutside) return
  event.preventDefault()
  close('outside')
}
function beforeLeave(el: Element) {
  ;(el as HTMLElement).inert = true
  el.setAttribute('aria-hidden', 'true')
}
</script>
<template>
  <Teleport to="body"
    ><Transition :name="drawer ? 'drawer' : 'dialog'" @before-leave="beforeLeave"
      ><div
        v-if="open"
        class="ui-overlay"
        :class="{ 'drawer-overlay': drawer }"
        @pointerdown.self="outsidePointerDown"
      >
        <section
          ref="panel"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="id"
          tabindex="-1"
          class="ui-dialog"
          :class="{ 'ui-drawer': drawer }"
        >
          <header class="dialog-header">
            <h2 :id="id">{{ title }}</h2>
            <UiButton icon variant="ghost" aria-label="关闭" @click="close('action')"
              ><UiIcon name="close"
            /></UiButton>
          </header>
          <div class="dialog-content"><slot /></div>
          <footer v-if="$slots.footer" class="dialog-footer">
            <slot name="footer" :close="() => close('action')" />
          </footer>
        </section></div></Transition
  ></Teleport>
</template>
