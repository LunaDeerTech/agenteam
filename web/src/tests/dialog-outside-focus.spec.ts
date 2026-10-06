import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import UiDialog from '../components/ui/UiDialog.vue'
import UiDrawer from '../components/ui/UiDrawer.vue'
import UiPopover from '../components/ui/UiPopover.vue'

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

function recoverySurface(
  component: typeof UiDialog | typeof UiDrawer,
  mutation = '',
  initiallyOpen = false,
) {
  const wrapper = mount(
    defineComponent({
      components: { Surface: component, UiDialog },
      setup() {
        const mountedBoth = ref(true)
        async function remount() {
          mountedBoth.value = false
          await nextTick()
          mountedBoth.value = true
        }
        return {
          lower: ref(initiallyOpen),
          confirmation: ref(initiallyOpen),
          lowerMounted: ref(true),
          invalid: ref(false),
          mountedBoth,
          remount,
          mutation,
        }
      },
      template: `
        <button data-launch @click="lower = true">Open lower</button>
        <template v-if="mountedBoth">
        <Surface v-if="lowerMounted" v-model:open="lower" title="Remaining">
          <fieldset :disabled="invalid && mutation === 'fieldset'">
            <div :hidden="invalid && mutation === 'hidden'"
              :inert="invalid && mutation === 'inert' ? '' : undefined"
              :contenteditable="invalid && mutation === 'editable-child' ? 'true' : undefined"
              :aria-hidden="invalid && mutation === 'aria-hidden' ? 'true' : undefined"
              :style="invalid ? { display: mutation === 'display' ? 'none' : undefined,
                visibility: mutation === 'visibility' ? 'hidden' : mutation === 'collapse' ? 'collapse' : undefined } : {}">
              <component :is="['non-focusable', 'editable-child', 'editable-host'].includes(mutation) ? 'span' : 'button'"
                v-if="!(invalid && mutation === 'disconnected')" data-recovery-trigger
                :tabindex="invalid && ['non-focusable', 'editable-child', 'editable-host'].includes(mutation) ? undefined : 0"
                :contenteditable="invalid && mutation === 'editable-host' ? 'true' : undefined"
                :disabled="invalid && mutation === 'disabled'" @click="confirmation = true">Confirm</component>
            </div>
          </fieldset>
          <input data-remaining-input />
        </Surface>
        <UiDialog v-model:open="confirmation" title="Confirmation">
          <input data-confirmation-input data-autofocus />
          <button data-invalidate @click="invalid = true">Invalidate old trigger</button>
          <button data-remove-lower @click="lowerMounted = false">Remove lower</button>
          <button data-remount @click="remount">Remount both</button>
        </UiDialog>
        </template>`,
    }),
    { attachTo: host },
  )
  mounted.push(wrapper)
  return wrapper
}

async function openConfirmation(wrapper: VueWrapper) {
  await open(wrapper)
  const trigger = document.querySelector<HTMLElement>('[data-recovery-trigger]')!
  trigger.focus() // jsdom click dispatch has no native pointer focus.
  trigger.click()
  await flushPromises()
  expect(document.activeElement).toBe(document.querySelector('[data-confirmation-input]'))
  return trigger
}

async function escapeTop() {
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await nextTick()
}

function remainingPanel() {
  const panel = document.querySelector<HTMLElement>('.ui-dialog')!
  expect(panel.inert).toBe(false)
  expect(panel.contains(document.activeElement)).toBe(true)
  expect(host.inert).toBe(true)
  expect(document.body.style.overflow).toBe('hidden')
  return panel
}

describe.each([
  ['dialog', UiDialog],
  ['drawer', UiDrawer],
] as const)('%s modal restoration', (_, component) => {
  it('prefers the exact still-valid trigger over another focusable', async () => {
    const trigger = await openConfirmation(recoverySurface(component))
    await escapeTop()
    remainingPanel()
    expect(document.activeElement).toBe(trigger)
  })

  it.each(['body', 'background', 'disconnected'])(
    'restores within the lower modal when simultaneous open captured %s',
    async (source) => {
      const outside = document.createElement('button')
      host.append(outside)
      if (source !== 'body') outside.focus()
      recoverySurface(component, '', true)
      await nextTick()
      if (source === 'disconnected') outside.remove()
      await escapeTop()
      const panel = remainingPanel()
      expect(document.activeElement).toBe(panel.querySelector('.dialog-header button'))
      expect(document.activeElement).not.toBe(outside)
    },
  )

  it.each([
    'disabled',
    'fieldset',
    'hidden',
    'inert',
    'aria-hidden',
    'display',
    'visibility',
    'collapse',
    'disconnected',
    'non-focusable',
    'editable-child',
    'no-layout',
    'empty-region',
  ])('rejects the now-%s trigger and finds a legal modal target', async (mutation) => {
    const trigger = await openConfirmation(recoverySurface(component, mutation))
    document.querySelector<HTMLButtonElement>('[data-invalidate]')!.click()
    await nextTick()
    if (mutation === 'editable-child') {
      // jsdom does not expose isContentEditable; the browser suite proves this
      // inherited DOM state. Its native focus() still refuses this plain span.
      Object.defineProperty(trigger, 'isContentEditable', { configurable: true, value: true })
    }
    if (mutation === 'no-layout')
      Object.defineProperty(trigger, 'getClientRects', {
        configurable: true,
        value: () => [] as unknown as DOMRectList,
      })
    if (mutation === 'empty-region')
      Object.defineProperty(trigger, 'getClientRects', {
        configurable: true,
        value: () => [{ width: 0, height: 0 }] as unknown as DOMRectList,
      })
    await escapeTop()
    const panel = remainingPanel()
    expect(document.activeElement).toBe(panel.querySelector('.dialog-header button'))
    expect(document.activeElement).not.toBe(trigger)
  })

  it('uses the remaining panel when all its controls are disabled', async () => {
    await openConfirmation(recoverySurface(component))
    const lower = document.querySelector<HTMLElement>('.ui-dialog')!
    lower.querySelectorAll<HTMLInputElement | HTMLButtonElement>('input,button').forEach((node) => {
      node.disabled = true
    })
    await escapeTop()
    expect(remainingPanel()).toBe(lower)
    expect(document.activeElement).toBe(lower)
  })

  it('preserves a contenteditable host that really accepts focus', async () => {
    const trigger = await openConfirmation(recoverySurface(component, 'editable-host'))
    document.querySelector<HTMLButtonElement>('[data-invalidate]')!.click()
    await nextTick()
    Object.defineProperty(trigger, 'isContentEditable', { configurable: true, value: true })
    expect(trigger.getAttribute('tabindex')).toBeNull()
    expect(trigger.getAttribute('contenteditable')).toBe('true')
    await escapeTop()
    remainingPanel()
    expect(document.activeElement).toBe(trigger)
  })

  it.each(['next-control', 'panel'])('continues after a focus no-op to the %s', async (target) => {
    await openConfirmation(recoverySurface(component, 'disabled'))
    document.querySelector<HTMLButtonElement>('[data-invalidate]')!.click()
    await nextTick()
    const lower = document.querySelector<HTMLElement>('.ui-dialog')!
    const header = lower.querySelector<HTMLButtonElement>('.dialog-header button')!
    const next = lower.querySelector<HTMLInputElement>('[data-remaining-input]')!
    const firstFocus = vi.spyOn(header, 'focus').mockImplementation(() => {})
    if (target === 'panel') vi.spyOn(next, 'focus').mockImplementation(() => {})
    await escapeTop()
    expect(firstFocus).toHaveBeenCalledTimes(1)
    expect(remainingPanel()).toBe(lower)
    expect(document.activeElement).toBe(target === 'panel' ? lower : next)
  })

  it('restores inside the new lower panel after both real components remount open', async () => {
    const trigger = await openConfirmation(recoverySurface(component))
    document.querySelector<HTMLButtonElement>('[data-remount]')!.click()
    await flushPromises()
    expect(trigger.isConnected).toBe(false)
    expect(document.querySelectorAll('.ui-dialog')).toHaveLength(2)
    expect(document.activeElement).toBe(document.querySelector('[data-confirmation-input]'))
    await escapeTop()
    remainingPanel()
  })

  it('does not focus any background target when the allowed panel is unavailable', async () => {
    await openConfirmation(recoverySurface(component))
    document.querySelector<HTMLElement>('.ui-dialog')!.hidden = true
    const focus = vi.spyOn(HTMLElement.prototype, 'focus')
    await escapeTop()
    expect(focus).not.toHaveBeenCalled()
    expect(host.inert).toBe(true)
    expect(document.body.style.overflow).toBe('hidden')
  })

  it('does not steal focus when a non-top modal unmounts', async () => {
    await openConfirmation(recoverySurface(component))
    const active = document.activeElement
    const focus = vi.spyOn(HTMLElement.prototype, 'focus')
    document.querySelector<HTMLButtonElement>('[data-remove-lower]')!.click()
    await nextTick()
    expect(document.querySelectorAll('.ui-dialog')).toHaveLength(1)
    expect(document.activeElement).toBe(active)
    expect(focus).not.toHaveBeenCalled()
    expect(host.inert).toBe(true)
  })
})

it('never restores into an inert lower modal outside the remaining highest modal', async () => {
  const wrapper = mount(
    defineComponent({
      components: { UiDialog },
      setup: () => ({ first: ref(false), second: ref(false), third: ref(false) }),
      template: `<button data-launch @click="first = true">First</button>
        <UiDialog v-model:open="first" title="First"><button data-stack @click="second = true; third = true">Open two</button></UiDialog>
        <UiDialog v-model:open="second" title="Second"><input data-second /></UiDialog>
        <UiDialog v-model:open="third" title="Third"><input data-third data-autofocus /></UiDialog>`,
    }),
    { attachTo: host },
  )
  mounted.push(wrapper)
  await open(wrapper)
  const trigger = document.querySelector<HTMLButtonElement>('[data-stack]')!
  trigger.focus()
  trigger.click()
  await flushPromises()
  await escapeTop()
  const panels = document.querySelectorAll<HTMLElement>('.ui-dialog')
  expect(panels).toHaveLength(2)
  expect(panels[0]!.inert).toBe(true)
  expect(panels[1]!.contains(document.activeElement)).toBe(true)
  expect(document.activeElement).not.toBe(trigger)
})

it.each(['valid', 'disabled', 'panel-no-focus'])(
  'restores in the highest allowed popover, preserving valid-trigger priority (%s)',
  async (state) => {
    const wrapper = mount(
      defineComponent({
        components: { UiDialog, UiPopover },
        setup: () => ({
          lower: ref(false),
          popover: ref(false),
          confirmation: ref(false),
          invalid: ref(false),
        }),
        template: `<button data-launch @click="lower = true">Lower</button>
          <UiDialog v-model:open="lower" title="Lower">
            <UiPopover v-model:open="popover" label="Actions" data-popover-anchor>
              <input data-popover-first /><button data-popover-confirm :disabled="invalid" @click="confirmation = true">Confirm</button>
            </UiPopover>
          </UiDialog>
          <UiDialog v-model:open="confirmation" title="Confirmation"><button data-invalidate @click="invalid = true">Invalidate</button></UiDialog>`,
      }),
      { attachTo: host },
    )
    mounted.push(wrapper)
    await open(wrapper)
    document.querySelector<HTMLButtonElement>('[data-popover-anchor]')!.click()
    await flushPromises()
    const trigger = document.querySelector<HTMLButtonElement>('[data-popover-confirm]')!
    trigger.focus()
    trigger.click()
    await flushPromises()
    if (state !== 'valid') document.querySelector<HTMLButtonElement>('[data-invalidate]')!.click()
    await nextTick()
    if (state === 'panel-no-focus') {
      document.querySelector<HTMLInputElement>('[data-popover-first]')!.disabled = true
      vi.spyOn(document.querySelector<HTMLElement>('.ui-popover')!, 'focus').mockImplementation(
        () => {},
      )
    }
    await escapeTop()
    expect(document.activeElement).toBe(
      state === 'valid'
        ? trigger
        : document.querySelector(
            state === 'disabled' ? '[data-popover-first]' : '.dialog-header button',
          ),
    )
    expect(document.querySelector<HTMLElement>('.ui-popover')!.inert).toBe(false)
    expect(host.inert).toBe(true)
  },
)

function pageHeading() {
  const heading = document.createElement('h1')
  heading.tabIndex = -1
  heading.textContent = 'Current page'
  host.append(heading)
  return heading
}

async function fallbackDialog(target: HTMLElement | null | undefined, trigger?: HTMLElement) {
  trigger?.focus() // jsdom has no pointer default focus.
  const wrapper = mount(UiDialog, {
    attachTo: host,
    props: { open: true, title: 'Page confirmation', fallbackFocus: target },
    slots: { default: '<input data-autofocus data-page-confirmation />' },
  })
  mounted.push(wrapper)
  await nextTick()
  expect(document.activeElement).toBe(
    [...document.querySelectorAll('[data-page-confirmation]')].at(-1),
  )
  return wrapper
}

function invalidatePageTarget(target: HTMLElement, state: string) {
  if (state === 'disconnected') target.remove()
  else if (state === 'disabled') (target as HTMLButtonElement).disabled = true
  else if (state === 'fieldset') {
    const fieldset = document.createElement('fieldset')
    target.replaceWith(fieldset)
    fieldset.append(target)
    fieldset.disabled = true
  } else if (state === 'hidden') target.hidden = true
  else if (state === 'hidden-ancestor') {
    const parent = document.createElement('div')
    target.replaceWith(parent)
    parent.append(target)
    parent.hidden = true
  } else if (state === 'inert') target.setAttribute('inert', '')
  else if (state === 'aria-hidden') target.setAttribute('aria-hidden', 'true')
  else if (state === 'display') target.style.display = 'none'
  else if (state === 'visibility' || state === 'collapse')
    target.style.visibility = state === 'visibility' ? 'hidden' : 'collapse'
  else if (state === 'no-layout' || state === 'empty-region')
    Object.defineProperty(target, 'getClientRects', {
      configurable: true,
      value: () =>
        (state === 'no-layout' ? [] : [{ width: 0, height: 0 }]) as unknown as DOMRectList,
    })
  else if (state === 'focus-no-op') vi.spyOn(target, 'focus').mockImplementation(() => {})
  else if (state === 'editable-child') {
    target.removeAttribute('tabindex')
    Object.defineProperty(target, 'isContentEditable', { configurable: true, value: true })
  }
}

describe('UiDialog explicit page fallback', () => {
  it('reads a local ref that becomes available after an initially open mount', async () => {
    const wrapper = mount(
      defineComponent({
        components: { UiDialog },
        setup: () => ({ open: ref(true), target: ref<HTMLElement | null>(null) }),
        template: `<h1 ref="target" tabindex="-1" data-current-title>Current page</h1>
          <UiDialog v-model:open="open" title="Confirmation" :fallback-focus="target">
            <input data-autofocus /><button data-normal-close @click="open = false">Close</button>
          </UiDialog>`,
      }),
      { attachTo: host },
    )
    mounted.push(wrapper)
    expect(wrapper.getComponent(UiDialog).props('fallbackFocus')).toBeNull()
    await nextTick()
    const heading = wrapper.get('[data-current-title]').element as HTMLElement
    expect(wrapper.getComponent(UiDialog).props('fallbackFocus')).toBe(heading)
    document.querySelector<HTMLButtonElement>('[data-normal-close]')!.click()
    await nextTick()
    expect(document.activeElement).toBe(heading)
    expect(host.inert).toBe(false)
    expect(document.body.style.overflow).toBe('auto')
    expect(wrapper.findComponent(UiDialog).exists()).toBe(true)
  })

  it('uses the latest replacement ref and leaves the old local node disconnected', async () => {
    const wrapper = mount(
      defineComponent({
        components: { UiDialog },
        setup: () => ({
          open: ref(true),
          replacement: ref(false),
          target: ref<HTMLElement | null>(null),
        }),
        template: `<h1 v-if="!replacement" ref="target" tabindex="-1" data-title>Old local title</h1>
          <h2 v-else ref="target" tabindex="-1" data-title>New local title</h2>
          <UiDialog v-model:open="open" title="Confirmation" :fallback-focus="target">
            <button data-replace @click="replacement = true">Replace</button>
            <button data-normal-close @click="open = false">Close</button>
          </UiDialog>`,
      }),
      { attachTo: host },
    )
    mounted.push(wrapper)
    await nextTick()
    const old = wrapper.get('[data-title]').element
    document.querySelector<HTMLButtonElement>('[data-replace]')!.click()
    await nextTick()
    const current = wrapper.get('[data-title]').element
    expect(old.isConnected).toBe(false)
    expect(current).not.toBe(old)
    document.querySelector<HTMLButtonElement>('[data-normal-close]')!.click()
    await nextTick()
    expect(document.activeElement).toBe(current)
  })

  it('prefers the exact valid trigger and uses preventScroll', async () => {
    const heading = pageHeading()
    const trigger = document.createElement('button')
    host.append(trigger)
    const wrapper = await fallbackDialog(heading, trigger)
    const fallbackFocus = vi.spyOn(heading, 'focus')
    const originalFocus = vi.spyOn(trigger, 'focus')
    await wrapper.setProps({ open: false })
    expect(document.activeElement).toBe(trigger)
    expect(originalFocus).toHaveBeenCalledExactlyOnceWith({ preventScroll: true })
    expect(fallbackFocus).not.toHaveBeenCalled()
  })

  it.each([
    'disabled',
    'fieldset',
    'hidden',
    'hidden-ancestor',
    'inert',
    'aria-hidden',
    'display',
    'visibility',
    'collapse',
    'disconnected',
    'no-layout',
    'empty-region',
    'focus-no-op',
  ])('rejects the now-%s trigger and focuses the explicit local target', async (state) => {
    const heading = pageHeading()
    const trigger = document.createElement('button')
    host.append(trigger)
    const wrapper = await fallbackDialog(heading, trigger)
    invalidatePageTarget(trigger, state)
    const focused = vi.spyOn(heading, 'focus')
    await wrapper.setProps({ open: false })
    expect(document.activeElement).toBe(heading)
    expect(focused).toHaveBeenCalledExactlyOnceWith({ preventScroll: true })
  })

  it.each([
    'null',
    'undefined',
    'disabled',
    'fieldset',
    'hidden',
    'hidden-ancestor',
    'inert',
    'aria-hidden',
    'display',
    'visibility',
    'collapse',
    'disconnected',
    'no-layout',
    'empty-region',
    'focus-no-op',
    'body',
    'html',
    'container',
    'cross-document',
    'editable-child',
  ])('does not replace an unusable %s fallback with an arbitrary page control', async (state) => {
    const unrelated = document.createElement('button')
    host.append(unrelated)
    const unrelatedFocus = vi.spyOn(unrelated, 'focus')
    let target: HTMLElement | null | undefined = document.createElement(
      ['container', 'editable-child'].includes(state) ? 'span' : 'button',
    )
    host.append(target)
    if (state === 'null') target = null
    else if (state === 'undefined') target = undefined
    else if (state === 'body') target = document.body
    else if (state === 'html') target = document.documentElement
    else if (state === 'cross-document') {
      const foreign = document.implementation.createHTMLDocument('Other document')
      target = foreign.createElement('button')
      foreign.body.append(target)
    }
    if (target) invalidatePageTarget(target, state)
    const wrapper = await fallbackDialog(target)
    const before = document.activeElement
    await wrapper.setProps({ open: false })
    expect(document.activeElement).not.toBe(before)
    if (target && !['body', 'html'].includes(state)) expect(document.activeElement).not.toBe(target)
    expect(unrelatedFocus).not.toHaveBeenCalled()
    expect(document.activeElement).not.toBe(unrelated)
  })

  it('does not call BODY/HTML focus in the explicit fallback branch', async () => {
    for (const target of [document.body, document.documentElement]) {
      const wrapper = await fallbackDialog(target)
      const bodyFocus = vi.spyOn(document.body, 'focus')
      const htmlFocus = vi.spyOn(document.documentElement, 'focus')
      await wrapper.setProps({ open: false })
      expect(bodyFocus).not.toHaveBeenCalled()
      expect(htmlFocus).not.toHaveBeenCalled()
      bodyFocus.mockRestore()
      htmlFocus.mockRestore()
      wrapper.unmount()
    }
  })

  it('does not try the explicit fallback during direct unmount', async () => {
    const heading = pageHeading()
    const wrapper = await fallbackDialog(heading)
    const focus = vi.spyOn(heading, 'focus')
    wrapper.unmount()
    await nextTick()
    expect(focus).not.toHaveBeenCalled()
    expect(document.activeElement).not.toBe(heading)
    expect(host.inert).toBe(false)
  })

  it('preserves original valid-trigger restoration on unmount', async () => {
    const heading = pageHeading()
    const trigger = document.createElement('button')
    host.append(trigger)
    const wrapper = await fallbackDialog(heading, trigger)
    const focus = vi.spyOn(heading, 'focus')
    wrapper.unmount()
    expect(document.activeElement).toBe(trigger)
    expect(focus).not.toHaveBeenCalled()
  })

  it('keeps remaining modal scope and does not restore a non-top removed layer', async () => {
    const wrapper = mount(
      defineComponent({
        components: { UiDialog },
        setup: () => ({
          lower: ref(true),
          upper: ref(true),
          target: ref<HTMLElement | null>(null),
        }),
        template: `<h1 ref="target" tabindex="-1" data-title>Background title</h1>
          <UiDialog v-model:open="lower" title="Lower" :fallback-focus="target"><input data-lower /></UiDialog>
          <UiDialog v-model:open="upper" title="Upper" :fallback-focus="target">
            <input data-upper data-autofocus />
            <button data-remove-lower @click="lower = false">Remove lower</button>
            <button data-close-upper @click="upper = false">Close upper</button>
          </UiDialog>`,
      }),
      { attachTo: host },
    )
    mounted.push(wrapper)
    await nextTick()
    const heading = wrapper.get('[data-title]').element as HTMLElement
    const focus = vi.spyOn(heading, 'focus')
    document.querySelector<HTMLButtonElement>('[data-close-upper]')!.click()
    await nextTick()
    expect(document.querySelector('.ui-dialog')!.contains(document.activeElement)).toBe(true)
    expect(focus).not.toHaveBeenCalled()
    wrapper.unmount()

    const currentHeading = pageHeading()
    const currentFocus = vi.spyOn(currentHeading, 'focus')
    const lower = await fallbackDialog(currentHeading)
    const upper = await fallbackDialog(currentHeading)
    const active = document.activeElement
    await lower.setProps({ open: false })
    expect(document.activeElement).toBe(active)
    expect(currentFocus).not.toHaveBeenCalled()
    upper.unmount()
  })
})
