import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { type AccountCommand } from "../account/session";
import { AccountError, accountEnabled } from "../account/transport";
import { useSettingsCommand } from "./account-settings";
import { useSystemAccess } from "./requests";

/** One versioned editor: observations never silently overwrite a dirty draft. */
export function useOutboundDraft<
  T extends { version: string },
  D,
  R extends { version: string },
>(config: {
  read: (options: { current: () => boolean }) => Promise<T>;
  draft: (value?: T) => D;
  write: (
    base: T,
    draft: D,
    options: { current: () => boolean },
  ) => AccountCommand<R>;
  clearSecret?: (draft: D) => void;
}) {
  const access = useSystemAccess();
  const baseline = shallowRef<T>();
  const latest = shallowRef<T>();
  const draft = ref(config.draft()) as import("vue").Ref<D>;
  const reading = ref(false);
  const readError = shallowRef<unknown>();
  const command = useSettingsCommand<R>();
  let revision = 0;
  let alive = true;
  let minimumVersion = 0n;
  const dirty = computed(
    () =>
      !!baseline.value &&
      JSON.stringify(draft.value) !==
        JSON.stringify(config.draft(baseline.value)),
  );
  const changed = computed(
    () =>
      !!latest.value &&
      !!baseline.value &&
      latest.value.version !== baseline.value.version,
  );
  const conflict = computed(
    () =>
      command.error.value instanceof AccountError &&
      command.error.value.code === "VERSION_CONFLICT",
  );
  const locked = computed(
    () =>
      !accountEnabled ||
      !access.allowed.value ||
      !baseline.value ||
      reading.value ||
      command.busy.value ||
      command.uncertain.value,
  );
  function adopt(value: T) {
    baseline.value = value;
    draft.value = config.draft(value);
  }
  async function load(replace = false) {
    if (reading.value || !accountEnabled || !access.allowed.value) return;
    const captured = ++revision;
    const generation = access.generation.value;
    const current = () =>
      alive && captured === revision && generation === access.generation.value;
    reading.value = true;
    readError.value = undefined;
    try {
      const value = await config.read({ current });
      if (!current()) return;
      if (BigInt(value.version) < minimumVersion)
        throw new AccountError("INVALID_RESPONSE");
      if (
        !latest.value ||
        BigInt(value.version) >= BigInt(latest.value.version)
      )
        latest.value = value;
      if (!baseline.value || replace) adopt(latest.value!);
    } catch (error) {
      if (current()) readError.value = error;
    } finally {
      if (captured === revision) reading.value = false;
    }
  }
  async function submit() {
    if (locked.value || conflict.value || !dirty.value || !baseline.value)
      return;
    const generation = access.generation.value;
    await command.submit(
      () => {
        const intent = config.write(baseline.value!, draft.value, {
          current: () => alive && generation === access.generation.value,
        });
        config.clearSecret?.(draft.value);
        return intent;
      },
      async (receipt) => {
        // Strict original receipt confirms the write; only a fresh GET is editable.
        baseline.value = undefined;
        draft.value = config.draft();
        minimumVersion = BigInt(receipt.version);
        await load(true);
      },
    );
  }
  function reset() {
    revision++;
    reading.value = false;
    readError.value = undefined;
    baseline.value = undefined;
    latest.value = undefined;
    minimumVersion = 0n;
    draft.value = config.draft();
  }
  function cancel() {
    command.abandon();
    if (baseline.value) adopt(baseline.value);
  }
  function adoptLatest() {
    if (latest.value) {
      command.abandon();
      adopt(latest.value);
    }
  }
  async function abandon() {
    command.abandon();
    reset();
    await load(true);
  }
  watch(access.generation, reset, { flush: "sync" });
  onBeforeUnmount(() => {
    alive = false;
    reset();
  });
  return {
    baseline,
    latest,
    draft,
    reading,
    readError,
    command,
    dirty,
    changed,
    conflict,
    locked,
    load,
    submit,
    cancel,
    adoptLatest,
    abandon,
  };
}
