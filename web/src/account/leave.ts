import { onBeforeUnmount, ref } from "vue";

const owners = new Set<() => boolean>();
export const leaveDialogOpen = ref(false);
let resolveLeave: ((accepted: boolean) => void) | undefined;
let pending: Promise<boolean> | undefined;

export function useLeaveProtection(dirty: () => boolean) {
  owners.add(dirty);
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (!dirty()) return;
    event.preventDefault();
    event.returnValue = "";
  };
  window.addEventListener("beforeunload", beforeUnload);
  onBeforeUnmount(() => {
    owners.delete(dirty);
    window.removeEventListener("beforeunload", beforeUnload);
  });
}

export function confirmAccountLeave() {
  if (![...owners].some((dirty) => dirty())) return Promise.resolve(true);
  if (pending) return pending;
  leaveDialogOpen.value = true;
  pending = new Promise<boolean>((resolve) => {
    resolveLeave = resolve;
  });
  return pending;
}

export function finishAccountLeave(accepted: boolean) {
  leaveDialogOpen.value = false;
  resolveLeave?.(accepted);
  resolveLeave = undefined;
  pending = undefined;
}
