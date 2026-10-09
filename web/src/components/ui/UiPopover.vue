<script setup lang="ts">
import { computed, ref } from 'vue'
import { useFloating, autoUpdate, offset, flip, shift, size } from '@floating-ui/vue'
import { useLayer } from '../../composables/useLayer'
import type { CloseReason } from './types'
defineOptions({ inheritAttrs: false })
const open = defineModel<boolean>('open', { default: false })
const props = withDefaults(
  defineProps<{
    label: string
    disabled?: boolean
    role?: 'menu' | 'listbox' | 'dialog'
    matchWidth?: boolean
    id?: string
    triggerId?: string
  }>(),
  { role: 'dialog' },
)
const emit = defineEmits<{ close: [reason: CloseReason]; keydown: [event: KeyboardEvent] }>()
const anchor = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const middleware = computed(() => [
  offset(6),
  flip({ padding: 8 }),
  shift({ padding: 8 }),
  size({
    padding: 8,
    apply({ rects, availableHeight, elements }) {
      Object.assign(elements.floating.style, {
        maxHeight: `${Math.max(0, availableHeight)}px`,
        ...(props.matchWidth ? { width: `${rects.reference.width}px` } : {}),
      })
    },
  }),
])
const { floatingStyles } = useFloating(anchor, panel, {
  placement: 'bottom-start',
  strategy: 'fixed',
  middleware,
  whileElementsMounted: autoUpdate,
})
function close(reason: CloseReason) {
  open.value = false
  emit('close', reason)
}
const { zIndex } = useLayer(open, panel, close, false, anchor)
function leave(el: Element) {
  ;(el as HTMLElement).inert = true
  el.setAttribute('aria-hidden', 'true')
}
function keydown(e: KeyboardEvent) {
  if (e.key === 'Tab') close('action')
  emit('keydown', e)
}
</script>
<template>
  <button
    v-bind="$attrs"
    :id="triggerId"
    ref="anchor"
    type="button"
    class="ui-button popover-trigger"
    :disabled="disabled"
    :aria-expanded="open"
    :aria-haspopup="role"
    :aria-controls="open ? id : undefined"
    @click="open = !open"
    @keydown="
      (e) => {
        if (['ArrowDown', 'ArrowUp'].includes(e.key)) {
          e.preventDefault()
          open = true
        }
      }
    "
  >
    <slot name="trigger">{{ label }}</slot></button
  ><Teleport to="body"
    ><Transition name="popover" @before-leave="leave"
      ><div
        v-if="open"
        :id="id"
        ref="panel"
        :style="[floatingStyles, { zIndex }]"
        :role="role"
        :aria-label="label"
        tabindex="-1"
        class="ui-popover"
        @keydown="keydown"
      >
        <slot :close="() => close('action')" /></div></Transition
  ></Teleport>
</template>
