import { computed, readonly, ref, shallowRef } from "vue";
import { applyAccountTheme } from "../composables/useTheme";
import {
  AccountError,
  accountEnabled,
  accountRequest,
  accountSequence,
  type AccountRequest,
  type AccountSender,
} from "./transport";
import {
  isBootstrap,
  isRecord,
  isSession,
  type AccountUser,
  type BootstrapResponse,
  type SessionResponse,
} from "./types";

export type SessionStatus =
  "checking" | "anonymous" | "authenticated" | "unavailable";
const status = ref<SessionStatus>(accountEnabled ? "checking" : "anonymous");
const current = shallowRef<SessionResponse>();
const problem = shallowRef<unknown>();
const confirmedAction = ref<"password" | undefined>();
const generation = ref(0);
let browser: BootstrapResponse | undefined;
const browserGeneration = ref(0);
let checking: Promise<void> | undefined;
let initialized = !accountEnabled;

function publish(value: SessionResponse) {
  const changed =
    current.value?.user.id !== value.user.id ||
    current.value?.session.id !== value.session.id;
  if (changed) generation.value++;
  current.value = value;
  status.value = "authenticated";
  problem.value = undefined;
  confirmedAction.value = undefined;
  if (changed) applyAccountTheme(value.user.theme);
}

function forgetIdentity() {
  generation.value++;
  current.value = undefined;
  status.value = "anonymous";
  problem.value = undefined;
  applyAccountTheme("system");
}

function rejected(error: unknown, capturedGeneration: number) {
  if (capturedGeneration !== generation.value) return;
  if (error instanceof AccountError && error.status === 401) forgetIdentity();
  else if (error instanceof AccountError && error.code === "CSRF_FAILED") {
    generation.value++;
    status.value = "unavailable";
    problem.value = error;
  }
}

export function sessionIdentity() {
  return {
    generation: generation.value,
    userId: current.value?.user.id,
    sessionId: current.value?.session.id,
  };
}

export function useSession() {
  return {
    status: readonly(status),
    problem: readonly(problem),
    confirmedAction: readonly(confirmedAction),
    generation: readonly(generation),
    hasIdentity: computed(() => !!current.value),
    user: computed(() =>
      status.value === "authenticated" ? current.value?.user : undefined,
    ),
    refresh: refreshSession,
  };
}

async function readSession(send: AccountSender, capturedGeneration: number) {
  const value = await send(
    "/session",
    { current: () => capturedGeneration === generation.value },
    isSession,
  );
  publish(value);
}

export function refreshSession(): Promise<void> {
  if (!accountEnabled) return Promise.resolve();
  if (checking) return checking;
  const capturedGeneration = generation.value;
  status.value = "checking";
  problem.value = undefined;
  const operation = accountSequence(async (send) => {
    try {
      await readSession(send, capturedGeneration);
    } catch (error) {
      if (capturedGeneration !== generation.value) return;
      if (error instanceof AccountError && error.status === 401)
        forgetIdentity();
      else {
        status.value = "unavailable";
        problem.value = error;
      }
    }
  });
  checking = operation.finally(() => {
    initialized = true;
    checking = undefined;
  });
  return checking;
}

export async function ensureSession() {
  if (!initialized || checking) await (checking ?? refreshSession());
}

async function ensureBrowser() {
  if (browser) return { value: browser, generation: browserGeneration.value };
  return accountSequence(async (send) => {
    if (!browser) {
      browser = await send("/auth/bootstrap", {}, isBootstrap);
      browserGeneration.value++;
    }
    return { value: browser, generation: browserGeneration.value };
  });
}

export async function anonymousRequest<T>(
  path: string,
  options: AccountRequest,
  validate?: (value: unknown) => value is T,
): Promise<T> {
  const context = await ensureBrowser();
  const identityGeneration = generation.value;
  const requestedCurrent = options.current;
  const valid = () =>
    generation.value === identityGeneration &&
    browser === context.value &&
    browserGeneration.value === context.generation &&
    (!requestedCurrent || requestedCurrent());
  try {
    return await accountRequest(
      path,
      { ...options, csrf: context.value.csrf_token, current: valid },
      validate,
    );
  } catch (error) {
    if (
      valid() &&
      error instanceof AccountError &&
      error.code === "CSRF_FAILED"
    ) {
      browser = undefined;
      browserGeneration.value++;
    }
    throw error;
  }
}

function authenticatedContext() {
  if (status.value !== "authenticated" || !current.value)
    throw new AccountError("UNAUTHENTICATED", 401);
  const snapshot = current.value;
  const capturedGeneration = generation.value;
  return {
    csrf: snapshot.csrf_token,
    userId: snapshot.user.id,
    generation: capturedGeneration,
    current: () =>
      generation.value === capturedGeneration &&
      current.value?.session.id === snapshot.session.id,
  };
}

export async function authenticatedRequest<T>(
  path: string,
  options: AccountRequest = {},
  validate?: (value: unknown) => value is T,
): Promise<T> {
  const context = authenticatedContext();
  const requestedCurrent = options.current;
  try {
    return await accountRequest(
      path,
      {
        ...options,
        csrf: context.csrf,
        current: () =>
          context.current() && (!requestedCurrent || requestedCurrent()),
      },
      (value): value is T =>
        (!validate || validate(value)) && sameAccount(value, context.userId),
    );
  } catch (error) {
    rejected(error, context.generation);
    throw error;
  }
}

export interface AccountCommand<T> {
  readonly key: string;
  readonly confirmed: boolean;
  readonly active: boolean;
  run(onConfirmed?: () => void): Promise<T>;
  release(): void;
}

type CommandContext = {
  csrf: string;
  current: () => boolean;
  generation: number;
  anonymous?: boolean;
};

function command<T>(
  path: string,
  options: AccountRequest,
  context: CommandContext,
  validate?: (value: unknown) => value is T,
  after?: (send: AccountSender, value: T) => Promise<void>,
): AccountCommand<T> {
  let body = options.body;
  const metadata = { ...options };
  delete metadata.body;
  const key = options.key ?? crypto.randomUUID();
  let released = false;
  let running = false;
  let confirmed = false;
  return {
    key,
    get confirmed() {
      return confirmed;
    },
    get active() {
      return !released && context.current();
    },
    async run(onConfirmed) {
      if (released || running || confirmed || !context.current())
        throw new AccountError("STALE_REQUEST");
      running = true;
      try {
        return await accountSequence(async (send) => {
          const value = await send<T>(
            path,
            {
              ...metadata,
              body,
              key,
              csrf: context.csrf,
              current: () => !released && context.current(),
            },
            validate,
          );
          confirmed = true;
          body = undefined;
          onConfirmed?.();
          if (after) await after(send, value);
          return value;
        });
      } catch (error) {
        if (!context.anonymous) rejected(error, context.generation);
        else if (
          context.current() &&
          error instanceof AccountError &&
          error.code === "CSRF_FAILED"
        ) {
          browser = undefined;
          browserGeneration.value++;
        }
        throw error;
      } finally {
        running = false;
      }
    },
    release() {
      released = true;
      body = undefined;
    },
  };
}

export function sessionCommand<T>(
  path: string,
  options: AccountRequest,
  validate?: (value: unknown) => value is T,
): AccountCommand<T> {
  const context = authenticatedContext();
  const identityCurrent = context.current;
  context.current = () =>
    identityCurrent() && (!options.current || options.current());
  return command(
    path,
    options,
    context,
    (value): value is T =>
      (!validate || validate(value)) && sameAccount(value, context.userId),
  );
}
function sameAccount(value: unknown, userId: string) {
  return (
    !isRecord(value) ||
    !("user" in value) ||
    (isRecord(value.user) && value.user.id === userId)
  );
}

export async function browserCommand<T>(
  path: string,
  options: AccountRequest,
  validate?: (value: unknown) => value is T,
): Promise<AccountCommand<T>> {
  const context = await ensureBrowser();
  const identityGeneration = generation.value;
  return command(
    path,
    options,
    {
      csrf: context.value.csrf_token,
      generation: context.generation,
      anonymous: true,
      current: () =>
        generation.value === identityGeneration &&
        browser === context.value &&
        browserGeneration.value === context.generation,
    },
    validate,
  );
}

async function confirmReplacement(send: AccountSender) {
  status.value = "checking";
  try {
    await readSession(send, generation.value);
    initialized = true;
  } catch (error) {
    if (error instanceof AccountError && error.status === 401) forgetIdentity();
    else {
      status.value = "unavailable";
      problem.value = error;
    }
    throw error;
  }
}

export async function loginCommand(
  email: string,
  password: string,
  options: { key?: string; challengePass?: string } = {},
): Promise<AccountCommand<unknown>> {
  const context = await ensureBrowser();
  const identityGeneration = generation.value;
  return command(
    "/sessions/login",
    {
      method: "POST",
      key: options.key,
      body: JSON.stringify({
        email,
        password,
        ...(options.challengePass
          ? { challenge_pass: options.challengePass }
          : {}),
      }),
    },
    {
      csrf: context.value.csrf_token,
      generation: context.generation,
      anonymous: true,
      current: () =>
        generation.value === identityGeneration &&
        browser === context.value &&
        browserGeneration.value === context.generation,
    },
    (value): value is unknown =>
      isRecord(value) &&
      isSession({ ...value, csrf_token: "pending-confirmation" }) &&
      value.next_path === "/",
    confirmReplacement,
  );
}

export function passwordCommand(body: {
  version: string;
  current_password: string;
  new_password: string;
  confirmation: string;
}): AccountCommand<unknown> {
  return command(
    "/me/change-password",
    { method: "POST", body: JSON.stringify(body) },
    authenticatedContext(),
    (value): value is unknown =>
      isRecord(value) && value.completed === true && value.next_path === "/",
    async (send) => {
      confirmedAction.value = "password";
      await confirmReplacement(send);
    },
  );
}

export function logoutCommand(): AccountCommand<void> {
  return command<void>(
    "/sessions/logout",
    { method: "POST", body: "{}", status: 204 },
    authenticatedContext(),
    undefined,
    async () => {
      confirmedAction.value = undefined;
      forgetIdentity();
    },
  );
}

export function updateSessionUser(user: AccountUser) {
  if (!current.value || current.value.user.id !== user.id) return;
  if (BigInt(user.version) < BigInt(current.value.user.version)) return;
  current.value = { ...current.value, user };
}
