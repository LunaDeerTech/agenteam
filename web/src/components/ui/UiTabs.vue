<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue';

export interface TabItem { value: string; label: string; disabled?: boolean }
const props = defineProps<{ label: string; items: TabItem[] }>();
const model = defineModel<string>({ required: true });
const id = useId();
const tablist = ref<HTMLElement | null>(null);
const current = computed(() => props.items.find((item) => item.value === model.value && !item.disabled)?.value ?? props.items.find((item) => !item.disabled)?.value);
const tabId = (value: string) => `${id}-tab-${encodeURIComponent(value)}`;

async function onKeydown(event: KeyboardEvent) {
  const enabled = props.items.filter((item) => !item.disabled);
  if (!enabled.length) return;
  const selected = enabled.findIndex((item) => item.value === current.value);
  let next: number | undefined;
  if (event.key === 'ArrowRight') next = (selected + 1) % enabled.length;
  if (event.key === 'ArrowLeft') next = (selected - 1 + enabled.length) % enabled.length;
  if (event.key === 'Home') next = 0;
  if (event.key === 'End') next = enabled.length - 1;
  if (next === undefined) return;
  event.preventDefault();
  const item = enabled[next];
  if (!item) return;
  model.value = item.value;
  await nextTick();
  const target = document.getElementById(tabId(item.value));
  target?.focus({ preventScroll: true });
  target?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
}
</script>

<template>
  <div class="ui-tabs">
    <div ref="tablist" class="ui-tablist" role="tablist" :aria-label="label" @keydown="onKeydown">
      <button v-for="item in items" :id="tabId(item.value)" :key="item.value" type="button" role="tab" class="ui-tab" :aria-selected="item.value === current" :aria-controls="item.value === current ? `${id}-panel` : undefined" :tabindex="item.value === current ? 0 : -1" :disabled="item.disabled" @click="model = item.value">{{ item.label }}</button>
    </div>
    <div v-if="current !== undefined" :id="`${id}-panel`" role="tabpanel" :aria-labelledby="tabId(current)" class="ui-tabpanel" tabindex="0"><slot :value="current" /></div>
  </div>
</template>
