export type ThemePreference = "light" | "dark" | "system";
export interface AccountUser {
  id: string;
  email: string;
  username: string;
  display_name: string;
  role: "admin" | "user";
  theme: ThemePreference;
  version: string;
  initial_password_suggestion: boolean;
}
export interface AccountSession {
  id: string;
  issued_at: string;
  absolute_expires_at: string;
  idle_expires_at: string;
}
export interface SessionResponse {
  user: AccountUser;
  session: AccountSession;
  csrf_token: string;
}
export interface BootstrapResponse {
  csrf_token: string;
  challenge_modes: string[];
  delivery_channel: "smtp" | "backend_log";
}
export interface ProfileResponse {
  user: AccountUser;
  avatar: {
    media_type: "image/jpeg" | "image/png" | "image/webp";
    byte_size: string;
    sha256: string;
  } | null;
}
export interface PreferencesResponse {
  version: string;
  theme: ThemePreference;
}

const idPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const instantPattern = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/;

export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
export function isVersion(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[1-9]\d{0,18}$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
  );
}
export function isTheme(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}
export function isUser(value: unknown): value is AccountUser {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    idPattern.test(value.id) &&
    [value.email, value.username, value.display_name].every(
      (item) => typeof item === "string",
    ) &&
    (value.role === "admin" || value.role === "user") &&
    isTheme(value.theme) &&
    isVersion(value.version) &&
    typeof value.initial_password_suggestion === "boolean"
  );
}
export function isSession(value: unknown): value is SessionResponse {
  if (
    !isRecord(value) ||
    !isUser(value.user) ||
    !isRecord(value.session) ||
    typeof value.csrf_token !== "string" ||
    !value.csrf_token
  )
    return false;
  const session = value.session;
  return (
    typeof session.id === "string" &&
    idPattern.test(session.id) &&
    [
      session.issued_at,
      session.absolute_expires_at,
      session.idle_expires_at,
    ].every((item) => typeof item === "string" && instantPattern.test(item))
  );
}
export function isBootstrap(value: unknown): value is BootstrapResponse {
  return (
    isRecord(value) &&
    typeof value.csrf_token === "string" &&
    !!value.csrf_token &&
    Array.isArray(value.challenge_modes) &&
    value.challenge_modes.every((mode) => typeof mode === "string") &&
    (value.delivery_channel === "smtp" ||
      value.delivery_channel === "backend_log")
  );
}
export function isProfile(value: unknown): value is ProfileResponse {
  if (!isRecord(value) || !isUser(value.user)) return false;
  if (value.avatar === null) return true;
  const avatar = value.avatar;
  return (
    isRecord(avatar) &&
    ["image/jpeg", "image/png", "image/webp"].includes(
      String(avatar.media_type),
    ) &&
    typeof avatar.byte_size === "string" &&
    /^\d{1,19}$/.test(avatar.byte_size) &&
    typeof avatar.sha256 === "string" &&
    /^sha256:[0-9a-f]{64}$/.test(avatar.sha256)
  );
}
export function isPreferences(value: unknown): value is PreferencesResponse {
  return isRecord(value) && isVersion(value.version) && isTheme(value.theme);
}
