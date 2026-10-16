import { computed, onBeforeUnmount, ref, shallowRef } from "vue";
import { AccountError } from "./transport";
import type { AccountCommand } from "./session";

export function useCommand<T>() {
  const busy = ref(false);
  const uncertain = ref(false);
  const confirmed = ref(false);
  const error = shallowRef<unknown>();
  let command: AccountCommand<T> | undefined;
  let alive = true;
  let success: ((value: T) => void | Promise<void>) | undefined;
  let onConfirmed: (() => void) | undefined;
  const replayable = computed(
    () => uncertain.value && !!command?.active && !confirmed.value,
  );
  async function execute() {
    if (!command || busy.value) return;
    busy.value = true;
    error.value = undefined;
    try {
      const result = await command.run(() => {
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
        !command.confirmed &&
        failure instanceof AccountError &&
        failure.commitState === "unknown"
      )
        uncertain.value = true;
      if (!uncertain.value && !command.confirmed) command.release();
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
      command?.release();
      command = await create();
      success = received;
      onConfirmed = cleared;
    } catch (failure) {
      if (alive) error.value = failure;
      return;
    } finally {
      if (alive) busy.value = false;
    }
    if (!alive) {
      command?.release();
      return;
    }
    await execute();
  }
  function abandon() {
    command?.release();
    command = undefined;
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
