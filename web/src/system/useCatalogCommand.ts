import { onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useCommand } from "../account/useCommand";
import { useSystemAccess } from "./requests";
import type { CatalogOperation } from "./model-catalog";

export function useCatalogCommand<T>() {
  const state = useCommand<T>();
  const access = useSystemAccess();
  const operation = shallowRef<CatalogOperation<T>>();
  const observing = ref(false);
  const observation = ref<"found" | "not_found">();
  const observationError = shallowRef<unknown>();
  let generation = 0;
  function clear() {
    generation++;
    operation.value?.command.release();
    operation.value = undefined;
    observing.value = false;
    observation.value = undefined;
    observationError.value = undefined;
    state.abandon();
  }
  async function submit(
    create: () => CatalogOperation<T>,
    done: (value: T) => void | Promise<void>,
    confirmed?: () => void,
  ) {
    if (state.busy.value || state.uncertain.value || observing.value) return;
    observation.value = undefined;
    observationError.value = undefined;
    await state.submit(
      () => {
        operation.value = create();
        return operation.value.command;
      },
      done,
      confirmed,
    );
  }
  async function observe() {
    const active = operation.value;
    if (
      !active?.command.active ||
      !state.uncertain.value ||
      state.busy.value ||
      observing.value
    )
      return;
    const owner = ++generation;
    observing.value = true;
    observationError.value = undefined;
    try {
      const result = await active.observe();
      if (
        owner === generation &&
        operation.value === active &&
        active.command.active
      )
        observation.value = result ? "found" : "not_found";
    } catch (failure) {
      if (owner === generation && operation.value === active)
        observationError.value = failure;
    } finally {
      if (owner === generation) observing.value = false;
    }
  }
  watch(access.generation, clear, { flush: "sync" });
  onBeforeUnmount(clear);
  return {
    ...state,
    submit,
    observing,
    observation,
    observationError,
    observe,
    clear,
  };
}
