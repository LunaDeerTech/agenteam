import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { AccountError } from "./transport";
import type { AccountCommand } from "./session";

export function useCommand<T>() {
  const busy = ref(false);
  const uncertain = ref(false);
  const confirmed = ref(false);
  const error = shallowRef<unknown>();
  const command = shallowRef<AccountCommand<T>>();
  let alive = true;
  let success: ((value: T) => void | Promise<void>) | undefined;
  let onConfirmed: (() => void) | undefined;
  const replayable = computed(
    () => uncertain.value && !!command.value?.active && !confirmed.value,
  );
  watch(
    () => command.value?.active,
    (active) => {
      if (active === false) {
        command.value?.release();
        command.value = undefined;
      }
    },
    { flush: "sync" },
  );
  async function execute() {
    const active = command.value;
    if (!active || busy.value) return;
    busy.value = true;
    error.value = undefined;
    try {
      const result = await active.run(() => {
        if (!alive) return;
        confirmed.value = true;
        uncertain.value = false;
        onConfirmed?.();
      });
      if (alive) await success?.(result);
    } catch (failure) {
      if (!alive) return;
      error.value = failure;
      if (
        !active.confirmed &&
        failure instanceof AccountError &&
        (failure.commitState === "unknown" ||
          failure.commitState === "committed")
      )
        uncertain.value = true;
      if (!uncertain.value && !active.confirmed) active.release();
    } finally {
      if (alive) busy.value = false;
    }
  }
  async function submit(
    create: () => AccountCommand<T> | Promise<AccountCommand<T>>,
    received?: (value: T) => void | Promise<void>,
    cleared?: () => void,
  ) {
    if (busy.value || uncertain.value) return;
    error.value = undefined;
    confirmed.value = false;
    busy.value = true;
    try {
      command.value?.release();
      command.value = await create();
      success = received;
      onConfirmed = cleared;
    } catch (failure) {
      if (alive) error.value = failure;
      return;
    } finally {
      if (alive) busy.value = false;
    }
    if (!alive) {
      command.value?.release();
      return;
    }
    await execute();
  }
  function abandon() {
    command.value?.release();
    command.value = undefined;
    uncertain.value = false;
    error.value = undefined;
    confirmed.value = false;
    success = undefined;
    onConfirmed = undefined;
  }
  onBeforeUnmount(() => {
    alive = false;
    abandon();
  });
  return {
    busy,
    uncertain,
    confirmed,
    error,
    replayable,
    submit,
    retry: execute,
    abandon,
  };
}
