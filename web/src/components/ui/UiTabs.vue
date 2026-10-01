<script setup lang="ts">
import { nextTick, onMounted, onBeforeUnmount, ref, useId, watch } from 'vue'
import type { ChoiceOption } from './types'
const model = defineModel<string>({ default: '' })
const props = defineProps<{ items: ChoiceOption[]; label: string }>()
const id = useId()
const root = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const direction = ref(1)
const indicator = ref({ width: '0px', transform: 'translateX(0px)', opacity: 0 })
let observer: ResizeObserver | undefined
function measure() {
  const element = root.value?.querySelector<HTMLElement>('[aria-selected="true"]')
  if (!element || !root.value) {
    indicator.value.opacity = 0
    return
  }
  const buttonRect = element.getBoundingClientRect()
  const rootRect = root.value.getBoundingClientRect()
  indicator.value = {
    width: `${buttonRect.width}px`,
    transform: `translateX(${buttonRect.left - rootRect.left + root.value.scrollLeft}px)`,
    opacity: 1,
  }
}
function observeSize() {
  observer?.disconnect()
  if (typeof ResizeObserver !== 'undefined' && root.value) {
    observer = new ResizeObserver(measure)
    observer.observe(root.value)
    root.value.querySelectorAll('button').forEach((button) => observer!.observe(button))
  }
  measure()
}
onMounted(observeSize)
onBeforeUnmount(() => observer?.disconnect())
watch(
  () => props.items,
  async () => {
    await nextTick()
    observeSize()
  },
  { deep: true },
)
watch(
  model,
  async (value, previous) => {
    direction.value =
      props.items.findIndex((item) => item.value === value) >=
      props.items.findIndex((item) => item.value === previous)
        ? 1
        : -1
    await nextTick()
    measure()
  },
  { flush: 'sync' },
)
function retire(element: Element) {
  const oldPanel = element as HTMLElement
  const restore = oldPanel.contains(document.activeElement)
  oldPanel.inert = true
  oldPanel.setAttribute('aria-hidden', 'true')
  oldPanel.removeAttribute('id')
  if (restore) void nextTick(() => panel.value?.focus({ preventScroll: true }))
}
async function keydown(e: KeyboardEvent) {
  const items = props.items.filter((i) => !i.disabled)
  let index = items.findIndex((i) => i.value === model.value)
  if (e.key === 'ArrowRight') index = (index + 1) % items.length
  else if (e.key === 'ArrowLeft') index = (index - 1 + items.length) % items.length
  else if (e.key === 'Home') index = 0
  else if (e.key === 'End') index = items.length - 1
  else return
  e.preventDefault()
  if (!items[index]) return
  model.value = items[index].value
  await nextTick()
  const selected = root.value?.querySelector<HTMLElement>('[aria-selected="true"]')
  selected?.focus()
  selected?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
}
</script>
<template>
  <div>
    <div ref="root" class="ui-tabs" role="tablist" :aria-label="label" @keydown="keydown">
      <button
        v-for="item in items"
        :id="id + '-tab-' + item.value"
        :key="item.value"
        type="button"
        role="tab"
        :disabled="item.disabled"
        :aria-selected="model === item.value"
        :aria-controls="id + '-panel'"
        :tabindex="model === item.value ? 0 : -1"
        @click="model = item.value"
      >
        {{ item.label }}
      </button>
      <span class="tab-indicator" :style="indicator" aria-hidden="true" />
    </div>
    <div class="tab-viewport">
      <Transition :name="direction > 0 ? 'tab-forward' : 'tab-backward'" @before-leave="retire">
        <div
          :id="id + '-panel'"
          :key="model"
          ref="panel"
          role="tabpanel"
          :aria-labelledby="id + '-tab-' + model"
          tabindex="0"
          class="tab-panel"
        >
          <slot :value="model" />
        </div>
      </Transition>
    </div>
  </div>
</template>
