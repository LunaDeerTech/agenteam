import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
  type CSSProperties,
} from "vue";

export function useAnchoredLayer(
  options: { align?: () => "start" | "end"; matchWidth?: boolean } = {},
) {
  const trigger = ref<HTMLButtonElement | null>(null);
  const panel = ref<HTMLElement | null>(null);
  const open = ref(false);
  const target = ref<HTMLElement | string>("body");
  const position = ref<CSSProperties>({ visibility: "hidden" });
  let observer: ResizeObserver | undefined;

  function updatePosition() {
    if (!open.value || !trigger.value || !panel.value) return;
    const anchor = trigger.value.getBoundingClientRect();
    const visual = window.visualViewport;
    const viewportWidth = visual?.width ?? window.innerWidth;
    const viewportHeight = visual?.height ?? window.innerHeight;
    const offsetLeft = visual?.offsetLeft ?? 0;
    const offsetTop = visual?.offsetTop ?? 0;
    const gap = 6;
    const edge = 8;
    const width = Math.max(
      0,
      Math.min(
        options.matchWidth ? anchor.width : 208,
        viewportWidth - edge * 2,
      ),
    );
    const below = viewportHeight + offsetTop - anchor.bottom - gap - edge;
    const above = anchor.top - offsetTop - gap - edge;
    const flip =
      below < Math.min(panel.value.scrollHeight, 240) && above > below;
    const maxHeight = Math.max(0, flip ? above : below);
    const height = Math.min(panel.value.scrollHeight, maxHeight);
    const idealLeft =
      options.align?.() === "start" ? anchor.left : anchor.right - width;
    position.value = {
      position: "fixed",
      left: `${Math.max(offsetLeft + edge, Math.min(idealLeft, offsetLeft + viewportWidth - width - edge))}px`,
      top: `${flip ? anchor.top - gap - height : anchor.bottom + gap}px`,
      width: `${width}px`,
      maxHeight: `${maxHeight}px`,
      transformOrigin: flip ? "bottom center" : "top center",
    };
  }

  async function show() {
    target.value = trigger.value?.closest("dialog") ?? "body";
    position.value = { visibility: "hidden" };
    open.value = true;
    await nextTick();
    if (open.value) updatePosition();
  }

  function close(restoreFocus = true) {
    if (!open.value) return;
    open.value = false;
    if (restoreFocus && trigger.value?.isConnected)
      trigger.value.focus({ preventScroll: true });
  }

  function outside(event: Event) {
    const path = event.composedPath();
    if (
      !path.includes(trigger.value as EventTarget) &&
      !path.includes(panel.value as EventTarget)
    )
      close(false);
  }

  function cleanup() {
    document.removeEventListener("pointerdown", outside, true);
    document.removeEventListener("focusin", outside);
    window.removeEventListener("resize", updatePosition);
    window.removeEventListener("scroll", updatePosition, true);
    window.visualViewport?.removeEventListener("resize", updatePosition);
    window.visualViewport?.removeEventListener("scroll", updatePosition);
    observer?.disconnect();
  }

  watch(open, async (value) => {
    cleanup();
    if (!value) return;
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("focusin", outside);
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    window.visualViewport?.addEventListener("resize", updatePosition);
    window.visualViewport?.addEventListener("scroll", updatePosition);
    await nextTick();
    if (!open.value) return;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(updatePosition);
      if (trigger.value) observer.observe(trigger.value);
      if (panel.value) observer.observe(panel.value);
    }
  });
  onBeforeUnmount(cleanup);

  return {
    trigger,
    panel,
    open,
    target,
    position: computed(() => position.value),
    show,
    close,
  };
}
