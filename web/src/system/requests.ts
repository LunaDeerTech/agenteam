import { computed, readonly, ref, watch } from "vue";
import {
  authenticatedRequest,
  sessionCommand,
  useSession,
  type AccountCommand,
} from "../account/session";
import { AccountError, type AccountRequest } from "../account/transport";

const session = useSession();
const denied = ref(false);
const generation = ref(0);
const role = ref(session.user.value?.role);
const allowed = computed(
  () => session.user.value?.role === "admin" && !denied.value,
);
watch(
  session.generation,
  () => {
    generation.value++;
    denied.value = false;
  },
  { flush: "sync" },
);
watch(
  () => session.user.value,
  (user) => {
    if (!user || user.role === role.value) return;
    role.value = user.role;
    generation.value++;
  },
  { flush: "sync" },
);

function context(requested?: () => boolean) {
  if (!allowed.value) throw new AccountError("FORBIDDEN", 403);
  const captured = generation.value;
  return () =>
    captured === generation.value &&
    !denied.value &&
    role.value === "admin" &&
    (!requested || requested());
}
function rejected(error: unknown, current: () => boolean) {
  if (current() && error instanceof AccountError && error.status === 403) {
    denied.value = true;
    generation.value++;
  }
}

export function useSystemAccess() {
  return {
    allowed,
    denied: readonly(denied),
    generation: readonly(generation),
    async recheck() {
      await session.refresh();
      if (session.user.value?.role === "admin") {
        denied.value = false;
      }
    },
  };
}

export async function systemRequest<T>(
  path: string,
  options: AccountRequest = {},
  validate?: (value: unknown) => value is T,
): Promise<T> {
  const current = context(options.current);
  try {
    return await authenticatedRequest(path, { ...options, current }, validate);
  } catch (error) {
    rejected(error, current);
    throw error;
  }
}

export function systemCommand<T>(
  path: string,
  options: AccountRequest,
  validate?: (value: unknown) => value is T,
  policy?: { uncertainOn?: (error: AccountError) => boolean },
): AccountCommand<T> {
  const current = context(options.current);
  const command = sessionCommand(path, { ...options, current }, validate);
  return {
    get key() {
      return command.key;
    },
    get active() {
      return command.active;
    },
    get confirmed() {
      return command.confirmed;
    },
    release: () => command.release(),
    async run(onConfirmed) {
      try {
        return await command.run(onConfirmed);
      } catch (error) {
        rejected(error, current);
        if (
          error instanceof AccountError &&
          error.commitState !== "committed" &&
          policy?.uncertainOn?.(error)
        )
          throw new AccountError(
            error.code,
            error.status,
            "unknown",
            error.fields,
          );
        throw error;
      }
    },
  };
}
