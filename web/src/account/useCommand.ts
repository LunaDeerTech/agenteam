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
  let generation = 0;
  let success: ((value: T) => void | Promise<void>) | undefined;
  let onConfirmed: (() => void) | undefined;
  const replayable = computed(
    () => uncertain.value && !!command.value?.active && !confirmed.value,
  );
  watch(
    () => command.value?.active,
    (active) => {
      if (active === false && !command.value?.confirmed) {
        command.value?.release();
        command.value = undefined;
        busy.value = false;
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
        if (!alive || command.value !== active) return;
        confirmed.value = true;
        uncertain.value = false;
        onConfirmed?.();
      });
      if (alive && command.value === active) await success?.(result);
    } catch (failure) {
      if (!alive || command.value !== active) return;
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
      if (alive && command.value === active) busy.value = false;
    }
  }
  async function submit(
    create: () => AccountCommand<T> | Promise<AccountCommand<T>>,
    received?: (value: T) => void | Promise<void>,
    cleared?: () => void,
  ) {
    if (busy.value || uncertain.value) return;
    const owner = ++generation;
    error.value = undefined;
    confirmed.value = false;
    busy.value = true;
    try {
      command.value?.release();
      const created = await create();
      if (!alive || owner !== generation) {
        created.release();
        return;
      }
      command.value = created;
      success = received;
      onConfirmed = cleared;
    } catch (failure) {
      if (alive && owner === generation) error.value = failure;
      return;
    } finally {
      if (alive && owner === generation) busy.value = false;
    }
    if (!alive || owner !== generation) {
      command.value?.release();
      return;
    }
    await execute();
  }
  function abandon() {
    generation++;
    busy.value = false;
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
