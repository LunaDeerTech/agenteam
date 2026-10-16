<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue';
import UiIconButton from './UiIconButton.vue';
import { lockPageScroll } from './internal/scrollLock';

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  side?: 'center' | 'left';
  closeOnOutside?: boolean;
  closeOnEscape?: boolean;
}>(), { side: 'center', closeOnOutside: true, closeOnEscape: true });
type CloseReason = 'escape' | 'outside' | 'action';
const emit = defineEmits<{ 'update:open': [value: boolean]; close: [reason: CloseReason] }>();
const id = useId();
const dialog = ref<HTMLDialogElement | null>(null);
let previousFocus: HTMLElement | null = null;
let releaseScroll: (() => void) | undefined;
let pointerStartedOutside = false;

function requestClose(reason: CloseReason = 'action') {
  emit('update:open', false);
  emit('close', reason);
}

function restoreFocus() {
  if (previousFocus?.isConnected && !previousFocus.closest('[inert]') && !previousFocus.matches(':disabled')) {
    previousFocus.focus({ preventScroll: true });
  }
  previousFocus = null;
}

function closeDialog() {
  dialog.value?.close();
  releaseScroll?.();
  releaseScroll = undefined;
  restoreFocus();
}

watch(() => props.open, async (open) => {
  await nextTick();
  if (!dialog.value || props.open !== open) return;
  if (open && !dialog.value.open) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    releaseScroll = lockPageScroll();
    dialog.value.showModal();
  } else if (!open && dialog.value.open) {
    closeDialog();
  }
}, { immediate: true });

function onCancel(event: Event) {
  event.preventDefault();
  if (props.closeOnEscape) requestClose('escape');
}

function isOutside(event: MouseEvent | PointerEvent) {
  if (event.target !== dialog.value || !dialog.value) return false;
  const bounds = dialog.value.getBoundingClientRect();
  return event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom;
}

function onClick(event: MouseEvent) {
  if (pointerStartedOutside && isOutside(event) && props.closeOnOutside) requestClose('outside');
  pointerStartedOutside = false;
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog.value) return;
  const focusable = [...dialog.value.querySelectorAll<HTMLElement>('a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])')]
    .filter((element) => !element.closest('[inert], [hidden]') && element.getClientRects().length > 0);
  const first = focusable[0];
  const last = focusable.at(-1);
  if (!first) {
    event.preventDefault();
    dialog.value.focus();
  } else if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

onBeforeUnmount(closeDialog);
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" :aria-labelledby="`${id}-title`" class="ui-dialog" :class="`ui-dialog--${side}`" tabindex="-1" @cancel="onCancel" @keydown="onKeydown" @pointerdown="pointerStartedOutside = isOutside($event)" @click="onClick">
      <header class="ui-dialog-header">
        <h2 :id="`${id}-title`" class="ui-dialog-title">{{ title }}</h2>
        <UiIconButton label="关闭" icon="close" @click="requestClose()" />
      </header>
      <div class="ui-dialog-body"><slot :close="requestClose" /></div>
      <footer v-if="$slots.footer" class="ui-dialog-footer"><slot name="footer" :close="requestClose" /></footer>
    </dialog>
  </Teleport>
</template>
