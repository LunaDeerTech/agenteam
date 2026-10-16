<script setup lang="ts">
import { nextTick, useId, watch } from 'vue';
import UiIcon from './UiIcon.vue';
import { useAnchoredLayer } from './internal/useAnchoredLayer';

export interface MenuItem { id: string; label: string; checked?: boolean; disabled?: boolean; icon?: string }
const props = withDefaults(defineProps<{ label: string; items: MenuItem[]; disabled?: boolean; align?: 'start' | 'end' }>(), { align: 'end' });
const emit = defineEmits<{ select: [id: string] }>();
const id = useId();
const { trigger, panel, open, target, position, show, close } = useAnchoredLayer({ align: () => props.align });
let search = '';
let searchedAt = 0;

function buttons() {
  return [...(panel.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])];
}

async function expand(last = false) {
  if (props.disabled) return;
  await show();
  if (!open.value) return;
  const choices = buttons();
  (last ? choices.at(-1) : choices[0])?.focus({ preventScroll: true });
  if (!choices.length) panel.value?.focus();
}

function select(item: MenuItem) {
  if (item.disabled) return;
  close();
  emit('select', item.id);
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault();
    event.stopPropagation();
    close();
    return;
  }
  if (event.key === 'Tab') {
    close();
    return;
  }
  const choices = buttons();
  if (!choices.length) return;
  const current = choices.indexOf(document.activeElement as HTMLButtonElement);
  let index: number | undefined;
  if (event.key === 'ArrowDown') index = (current + 1) % choices.length;
  if (event.key === 'ArrowUp') index = (current - 1 + choices.length) % choices.length;
  if (event.key === 'Home') index = 0;
  if (event.key === 'End') index = choices.length - 1;
  if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey && event.key !== ' ') {
    search = Date.now() - searchedAt > 700 ? event.key : search + event.key;
    searchedAt = Date.now();
    const query = search.toLocaleLowerCase();
    const ordered = [...choices.slice(current + 1), ...choices.slice(0, current + 1)];
    const found = ordered.find((button) => button.textContent?.trim().toLocaleLowerCase().startsWith(query));
    if (found) index = choices.indexOf(found);
  }
  if (index !== undefined) {
    event.preventDefault();
    choices[index]?.focus({ preventScroll: true });
    choices[index]?.scrollIntoView?.({ block: 'nearest' });
  }
}

watch(() => props.disabled, (disabled) => { if (disabled) close(false); });
watch(() => props.items, async () => {
  if (!open.value) return;
  await nextTick();
  if (!panel.value?.contains(document.activeElement)) buttons()[0]?.focus();
});
</script>

<template>
  <button :id="`${id}-trigger`" ref="trigger" type="button" class="ui-button ui-button--ghost ui-menu-trigger" :disabled="disabled" :aria-label="label" aria-haspopup="menu" :aria-expanded="open" :aria-controls="open ? `${id}-menu` : undefined" @click="open ? close() : expand()" @keydown.down.prevent="expand()" @keydown.up.prevent="expand(true)">
    <slot name="trigger" :open="open"><span>{{ label }}</span><UiIcon name="chevron-down" :size="14" /></slot>
  </button>
  <Teleport :to="target">
    <Transition name="ui-overlay">
      <div v-if="open" :id="`${id}-menu`" ref="panel" role="menu" :aria-labelledby="`${id}-trigger`" class="ui-popup" :style="position" tabindex="-1" :inert="!open || undefined" @keydown="onKeydown">
        <button v-for="item in items" :key="item.id" type="button" :role="item.checked === undefined ? 'menuitem' : 'menuitemradio'" :aria-checked="item.checked" :disabled="item.disabled" class="ui-popup-item" tabindex="-1" @click="select(item)">
          <UiIcon v-if="item.icon" :name="item.icon" />
          <span class="ui-popup-item-label">{{ item.label }}</span>
          <UiIcon v-if="item.checked" name="check" :size="14" />
        </button>
      </div>
    </Transition>
  </Teleport>
</template>
