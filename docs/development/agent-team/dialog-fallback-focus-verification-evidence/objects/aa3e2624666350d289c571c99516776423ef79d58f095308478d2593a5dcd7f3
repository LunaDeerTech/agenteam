import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'
import type { CloseReason } from '../components/ui/types'
interface Layer {
  panel: Ref<HTMLElement | null>
  trigger: HTMLElement | null
  modal: boolean
  close: (reason: CloseReason) => void
}
const layers: Layer[] = []
const selector =
  'button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex="-1"])'
export function focusable(root: HTMLElement) {
  return Array.from(root.querySelectorAll<HTMLElement>(selector)).filter(
    (el) => !el.closest('[inert]') && el.getClientRects().length,
  )
}
function restorationVisible(element: HTMLElement) {
  if (
    !element.isConnected ||
    !Array.from(element.getClientRects()).some((rect) => rect.width > 0 && rect.height > 0)
  )
    return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    if (
      node.inert ||
      node.hasAttribute('inert') ||
      node.hidden ||
      node.getAttribute('aria-hidden')?.toLowerCase() === 'true'
    )
      return false
    const style = getComputedStyle(node)
    if (
      style.display === 'none' ||
      style.visibility === 'hidden' ||
      style.visibility === 'collapse'
    )
      return false
  }
  return true
}
function restorationTarget(element: HTMLElement) {
  const tabindex = element.getAttribute('tabindex')
  return (
    restorationVisible(element) &&
    !element.matches(':disabled') &&
    (element.tabIndex >= 0 ||
      (tabindex !== null && /^[+-]?\d+$/.test(tabindex.trim())) ||
      element.isContentEditable)
  )
}
function restoreFocus(element: HTMLElement) {
  if (!restorationTarget(element)) return false
  element.focus({ preventScroll: true })
  return element.ownerDocument.activeElement === element
}
function restorePageFocus(element: HTMLElement | null) {
  if (
    !element ||
    element.ownerDocument !== document ||
    element === document.body ||
    element === document.documentElement
  )
    return false
  return restoreFocus(element)
}
function restoreWithinModal(trigger: HTMLElement | null, allowed: Layer[]) {
  if (
    trigger &&
    allowed.some((layer) => {
      const panel = layer.panel.value
      return panel && restorationVisible(panel) && panel.contains(trigger)
    }) &&
    restoreFocus(trigger)
  )
    return
  for (const layer of [...allowed].reverse()) {
    const panel = layer.panel.value
    if (!panel || !restorationVisible(panel)) continue
    for (const target of [...focusable(panel), panel]) {
      if (target !== trigger && restoreFocus(target)) return
    }
  }
}
let previousInert = false
let previousOverflow = ''
function syncBackground() {
  const app = document.getElementById('app')
  const modal = layers.some((l) => l.modal)
  if (app) app.inert = modal || previousInert
  document.body.style.overflow = modal ? 'hidden' : previousOverflow
  const lastModal = layers.map((l) => l.modal).lastIndexOf(true)
  layers.forEach((layer, index) => {
    const panel = layer.panel.value
    if (!panel) return
    panel.inert = index < lastModal
    if (panel.inert) panel.setAttribute('aria-hidden', 'true')
    else panel.removeAttribute('aria-hidden')
  })
}
function keydown(event: KeyboardEvent) {
  const top = layers.at(-1)
  if (!top) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopImmediatePropagation()
    top.close('escape')
    return
  }
  const modal = [...layers].reverse().find((l) => l.modal)
  if (event.key !== 'Tab') return
  if (!top.modal) top.close('action')
  if (!modal) return
  const start = layers.indexOf(modal)
  const nodes = layers.slice(start).flatMap((l) => (l.panel.value ? focusable(l.panel.value) : []))
  if (!nodes.length) {
    event.preventDefault()
    modal.panel.value?.focus()
    return
  }
  const index = nodes.indexOf(document.activeElement as HTMLElement)
  if (event.shiftKey && index <= 0) {
    event.preventDefault()
    nodes.at(-1)?.focus()
  } else if (!event.shiftKey && (index === nodes.length - 1 || index === -1)) {
    event.preventDefault()
    nodes[0]?.focus()
  }
}
function pointerdown(event: PointerEvent) {
  const top = layers.at(-1)
  if (!top || top.modal) return
  if (
    !top.panel.value?.contains(event.target as Node) &&
    !top.trigger?.contains(event.target as Node)
  )
    top.close('outside')
}
export function useLayer(
  open: Ref<boolean>,
  panel: Ref<HTMLElement | null>,
  close: (reason: CloseReason) => void,
  modal = false,
  trigger?: Ref<HTMLElement | null>,
  fallbackFocus?: Readonly<Ref<HTMLElement | null | undefined>>,
) {
  let record: Layer | undefined
  function remove(restore = true, allowFallback = false) {
    if (!record) return
    const active = record
    const index = layers.indexOf(active)
    const wasTop = index === layers.length - 1
    if (active.panel.value) {
      active.panel.value.inert = true
      active.panel.value.setAttribute('aria-hidden', 'true')
    }
    layers.splice(index, 1)
    record = undefined
    syncBackground()
    if (restore && wasTop) {
      const lastModal = layers.map((layer) => layer.modal).lastIndexOf(true)
      if (lastModal === -1) {
        const fallback = allowFallback ? fallbackFocus?.value : undefined
        if (fallback) {
          if (!restorePageFocus(active.trigger) && fallback !== active.trigger)
            restorePageFocus(fallback)
        } else if (active.trigger?.isConnected) active.trigger.focus({ preventScroll: true })
      } else restoreWithinModal(active.trigger, layers.slice(lastModal))
    }
    if (!layers.length) {
      document.removeEventListener('keydown', keydown, true)
      document.removeEventListener('pointerdown', pointerdown, true)
    }
  }
  watch(
    open,
    async (value) => {
      if (!value) {
        remove(true, true)
        return
      }
      if (!layers.length) {
        previousInert = document.getElementById('app')?.inert || false
        previousOverflow = document.body.style.overflow
        document.addEventListener('keydown', keydown, true)
        document.addEventListener('pointerdown', pointerdown, true)
      }
      record = {
        panel,
        trigger: trigger?.value || (document.activeElement as HTMLElement),
        close,
        modal,
      }
      layers.push(record)
      syncBackground()
      await nextTick()
      syncBackground()
      if (open.value && record)
        (
          panel.value?.querySelector<HTMLElement>('[data-autofocus]') ||
          (panel.value && focusable(panel.value)[0]) ||
          panel.value
        )?.focus()
    },
    { flush: 'sync', immediate: true },
  )
  onBeforeUnmount(() => remove())
  return { isTop: () => layers.at(-1) === record }
}
