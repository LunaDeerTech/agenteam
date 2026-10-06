import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import UiDialog from '../components/ui/UiDialog.vue'
import UiDrawer from '../components/ui/UiDrawer.vue'

const mounted: VueWrapper[] = []
let host: HTMLDivElement

beforeEach(() => {
  host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  document.body.style.overflow = 'auto'
  // jsdom has no layout. This only exposes existing focusable nodes to useLayer;
  // default pointer focus behavior is covered by the real Chromium harness.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [{ width: 10, height: 10 }] as unknown as DOMRectList,
  )
})

afterEach(() => {
  for (const wrapper of mounted.splice(0).reverse()) wrapper.unmount()
  host.remove()
  document.body.style.overflow = ''
  vi.restoreAllMocks()
})

function surface(component: typeof UiDialog | typeof UiDrawer, outside = true, escape = true) {
  const wrapper = mount(
    defineComponent({
      components: { Surface: component },
      setup: () => ({ open: ref(false), nested: ref(false), outside, escape }),
      template: `
        <button data-launch @click="open = true">Open</button>
        <Surface v-model:open="open" title="Outer" :close-on-outside="outside" :close-on-escape="escape">
          <input data-input data-autofocus />
          <button data-inside>Inside</button><a data-link href="#inside">Link</a><span data-text>Text</span>
          <button data-nested-launch @click="nested = true">Nested</button>
          <Surface v-model:open="nested" title="Nested"><input data-nested-input data-autofocus /></Surface>
          <template #footer="{ close }"><button data-action @click="close">Done</button></template>
        </Surface>`,
    }),
    { attachTo: host },
  )
  mounted.push(wrapper)
  return wrapper
}

async function open(wrapper: VueWrapper) {
  const trigger = wrapper.get('[data-launch]')
  // jsdom does not focus a button when trigger('click') dispatches its event.
  ;(trigger.element as HTMLElement).focus()
  await trigger.trigger('click')
  await nextTick()
  return trigger.element
}

function pointer(target: Element) {
  const event = new PointerEvent('pointerdown', { bubbles: true, cancelable: true, button: 0 })
  target.dispatchEvent(event)
  return event
}

describe.each([
  ['dialog', UiDialog],
  ['drawer', UiDrawer],
] as const)('%s outside-close contract', (_, component) => {
  it('cancels only an allowed top overlay event and emits one outside close', async () => {
    const wrapper = surface(component)
    const trigger = await open(wrapper)
    const panel = wrapper.getComponent(component)
    expect(host.inert).toBe(true)
    expect(document.body.style.overflow).toBe('hidden')
    const event = pointer(document.querySelector('.ui-overlay')!)
    expect(event.defaultPrevented).toBe(true)
    await nextTick()
    expect(panel.emitted('update:open')).toEqual([[false]])
    expect(panel.emitted('close')).toEqual([['outside']])
    expect(document.querySelector('[role=dialog]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(host.inert).toBe(false)
    expect(document.body.style.overflow).toBe('auto')
  })

  it('does not cancel or close disabled outside, and retains Escape/action policy', async () => {
    const wrapper = surface(component, false, false)
    await open(wrapper)
    const panel = wrapper.getComponent(component)
    expect(pointer(document.querySelector('.ui-overlay')!).defaultPrevented).toBe(false)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(panel.emitted('close')).toBeUndefined()
    expect(panel.emitted('update:open')).toBeUndefined()
    expect(document.querySelector('[role=dialog]')).not.toBeNull()
    document.querySelector<HTMLButtonElement>('[data-action]')!.click()
    await nextTick()
    expect(panel.emitted('update:open')).toEqual([[false]])
    expect(panel.emitted('close')).toEqual([['action']])
  })

  it('leaves panel input, button, link and text pointer defaults untouched', async () => {
    const wrapper = surface(component)
    await open(wrapper)
    const panel = wrapper.getComponent(component)
    for (const selector of ['[data-input]', '[data-inside]', '[data-link]', '[data-text]']) {
      expect(pointer(document.querySelector(selector)!).defaultPrevented).toBe(false)
    }
    await nextTick()
    expect(panel.emitted('update:open')).toBeUndefined()
    expect(panel.emitted('close')).toBeUndefined()
    expect(document.querySelector('[role=dialog]')).not.toBeNull()
  })

  it('retains Escape, close-button and footer-action reasons and restoration', async () => {
    const wrapper = surface(component)
    const panel = wrapper.getComponent(component)
    for (const action of ['escape', 'button', 'footer']) {
      const trigger = await open(wrapper)
      if (action === 'escape') {
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      } else {
        document
          .querySelector<HTMLButtonElement>(
            action === 'button' ? '.dialog-header button' : '[data-action]',
          )!
          .click()
      }
      await nextTick()
      expect(document.querySelector('[role=dialog]')).toBeNull()
      expect(document.activeElement).toBe(trigger)
    }
    expect(panel.emitted('close')).toEqual([['escape'], ['action'], ['action']])
    expect(panel.emitted('update:open')).toEqual([[false], [false], [false]])
  })

  it('ignores a synthetic non-top overlay and restores each nested trigger in order', async () => {
    const wrapper = surface(component)
    const trigger = await open(wrapper)
    const nestedTrigger = document.querySelector<HTMLButtonElement>('[data-nested-launch]')!
    nestedTrigger.focus()
    nestedTrigger.click()
    await nextTick()
    const overlays = document.querySelectorAll('.ui-overlay')
    expect(overlays).toHaveLength(2)
    // A covered lower overlay is not physically reachable; this is a gate test.
    expect(pointer(overlays[0]!).defaultPrevented).toBe(false)
    expect(document.querySelectorAll('[role=dialog]')).toHaveLength(2)
    expect(wrapper.getComponent(component).emitted('close')).toBeUndefined()
    expect(pointer(overlays[1]!).defaultPrevented).toBe(true)
    await nextTick()
    expect(document.querySelectorAll('[role=dialog]')).toHaveLength(1)
    expect(document.activeElement).toBe(nestedTrigger)
    expect(host.inert).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.querySelector('[role=dialog]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(host.inert).toBe(false)
  })
})
