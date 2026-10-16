import {
  accountRequest,
  accountErrorMessage,
  AccountError,
  type AccountRequest,
} from "./transport";
import {
  anonymousRequest,
  authenticatedRequest,
  sessionCommand,
} from "./session";
import { isRecord, isVersion } from "./types";
import type { CreationOptionsJSON, RequestOptionsJSON } from "./webauthn";

export interface PasskeyCapabilities {
  available: boolean;
  reason: "available" | "secure_origin_required" | "domain_required";
  rp_id: string | null;
}
export interface PasskeyItem {
  id: string;
  label: string;
  created_at: string;
  last_used_at: string | null;
}
export interface PasskeyList {
  version: string;
  email_verified: boolean;
  email_initialization_required: boolean;
  items: PasskeyItem[];
}
export interface PasskeyAssertionOptions {
  operation_id: string;
  expires_at: string;
  public_key: RequestOptionsJSON;
}
export type VerificationIntent =
  | { purpose: "change_email" }
  | { purpose: "manage_passkeys"; action: "register" }
  | {
      purpose: "manage_passkeys";
      action: "rename" | "delete";
      passkey_id: string;
    };
export interface PasskeyGrant {
  grant_id: string;
  purpose: "manage_passkeys" | "change_email";
  expires_at: string;
}
export interface MailCodeRequest {
  request_id: string;
  expires_at: string;
  resend_after: string;
}
export interface PasskeyReceipt {
  version: string;
  id: string;
  created_at: string;
}
export interface EmailChangeReceipt {
  version: string;
  email: string;
  created_at: string;
}
export interface PasskeyCreationOptions {
  operation_id: string;
  expires_at: string;
  public_key: CreationOptionsJSON;
}
const uuid = (value: unknown): value is string =>
  typeof value === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    value,
  );
const instant = (value: unknown): value is string =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value));
function exact(
  value: unknown,
  keys: string[],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => key in value)
  );
}
export function isPasskeyCapabilities(
  value: unknown,
): value is PasskeyCapabilities {
  return (
    exact(value, ["available", "reason", "rp_id"]) &&
    typeof value.available === "boolean" &&
    (value.available
      ? value.reason === "available" &&
        typeof value.rp_id === "string" &&
        !!value.rp_id
      : ["secure_origin_required", "domain_required"].includes(
          String(value.reason),
        ) && value.rp_id === null)
  );
}
export function isPasskeyList(value: unknown): value is PasskeyList {
  if (
    !exact(value, [
      "version",
      "email_verified",
      "email_initialization_required",
      "items",
    ]) ||
    !isVersion(value.version) ||
    typeof value.email_verified !== "boolean" ||
    typeof value.email_initialization_required !== "boolean" ||
    (value.email_verified && value.email_initialization_required) ||
    !Array.isArray(value.items) ||
    value.items.length > 20
  )
    return false;
  const ids = new Set<string>();
  return value.items.every((item) => {
    if (
      !exact(item, ["id", "label", "created_at", "last_used_at"]) ||
      !uuid(item.id) ||
      ids.has(item.id) ||
      typeof item.label !== "string" ||
      passkeyLabelError(item.label) !== "" ||
      item.label !== item.label.trim() ||
      !instant(item.created_at) ||
      !(item.last_used_at === null || instant(item.last_used_at))
    )
      return false;
    ids.add(item.id);
    return true;
  });
}
export function isAssertionOptions(
  value: unknown,
): value is PasskeyAssertionOptions {
  if (
    !exact(value, ["operation_id", "expires_at", "public_key"]) ||
    !uuid(value.operation_id) ||
    !instant(value.expires_at) ||
    !isRecord(value.public_key)
  )
    return false;
  const key = value.public_key;
  return (
    typeof key.challenge === "string" &&
    /^[A-Za-z0-9_-]+$/.test(key.challenge) &&
    typeof key.rpId === "string" &&
    !!key.rpId &&
    key.userVerification === "required" &&
    (key.allowCredentials === undefined ||
      (Array.isArray(key.allowCredentials) &&
        key.allowCredentials.every(
          (item) =>
            isRecord(item) &&
            item.type === "public-key" &&
            typeof item.id === "string" &&
            /^[A-Za-z0-9_-]+$/.test(item.id),
        )))
  );
}
export function getPasskeyCapabilities(options: AccountRequest = {}) {
  return accountRequest(
    "/auth/passkeys/capabilities",
    options,
    isPasskeyCapabilities,
  );
}
export function getPasskeys(options: AccountRequest = {}) {
  return authenticatedRequest("/me/passkeys", options, isPasskeyList);
}
export function getPasskeyLoginOptions(options: AccountRequest = {}) {
  return anonymousRequest(
    "/auth/passkeys/login/options",
    { ...options, method: "POST", body: "{}" },
    isAssertionOptions,
  );
}
export function passkeyUnavailableMessage(
  capability?: PasskeyCapabilities,
): string {
  if (capability?.reason === "domain_required")
    return "Passkey 需要通过域名访问，仍可使用密码。";
  if (capability?.reason === "secure_origin_required")
    return "Passkey 需要安全连接，仍可使用密码。";
  return "当前浏览器不支持 Passkey，仍可使用密码。";
}
export function passkeyErrorMessage(error: unknown): string {
  if (error instanceof AccountError) {
    if (
      error.fields.some(
        (field) =>
          field.path === "/code" && field.code === "INVALID_OR_EXPIRED",
      )
    )
      return "验证码错误或已过期，请重新获取。";
    if (error.code === "INVALID_STATE")
      return "验证已过期或不再适用，请重新验证。";
    if (error.code === "EMAIL_NOT_VERIFIED")
      return "当前邮箱尚未验证，请联系管理员完成邮箱初始化。";
    return accountErrorMessage(error);
  }
  if (error instanceof DOMException && error.name === "InvalidStateError")
    return "此 Passkey 已添加，请使用其他验证器。";
  return "Passkey 验证未完成，请重试。";
}
export function mailRequestErrorMessage(error: unknown): string {
  return error instanceof AccountError && error.code === "INVALID_STATE"
    ? "暂时无法发送验证码，请联系管理员检查 SMTP 配置。"
    : passkeyErrorMessage(error);
}
export function mailCodeError(value: string): string {
  return /^\d{6}$/.test(value) ? "" : "请输入 6 位数字验证码。";
}

export function passkeyLabelError(value: string): string {
  const label = value.trim();
  return !label ||
    [...label].length > 64 ||
    new TextEncoder().encode(label).length > 256 ||
    /\p{Cc}/u.test(label)
    ? "名称需为 1–64 个字符。"
    : "";
}
export function isPasskeyGrant(value: unknown): value is PasskeyGrant {
  return (
    exact(value, ["grant_id", "purpose", "expires_at"]) &&
    uuid(value.grant_id) &&
    ["manage_passkeys", "change_email"].includes(String(value.purpose)) &&
    instant(value.expires_at)
  );
}
export function isMailCodeRequest(value: unknown): value is MailCodeRequest {
  return (
    exact(value, ["request_id", "expires_at", "resend_after"]) &&
    uuid(value.request_id) &&
    instant(value.expires_at) &&
    instant(value.resend_after)
  );
}
export function isPasskeyReceipt(value: unknown): value is PasskeyReceipt {
  return (
    exact(value, ["version", "id", "created_at"]) &&
    isVersion(value.version) &&
    uuid(value.id) &&
    instant(value.created_at)
  );
}
export function isEmailChangeReceipt(
  value: unknown,
): value is EmailChangeReceipt {
  return (
    exact(value, ["version", "email", "created_at"]) &&
    isVersion(value.version) &&
    typeof value.email === "string" &&
    !!value.email &&
    instant(value.created_at)
  );
}
function isCreationOptions(value: unknown): value is PasskeyCreationOptions {
  if (
    !exact(value, ["operation_id", "expires_at", "public_key"]) ||
    !uuid(value.operation_id) ||
    !instant(value.expires_at) ||
    !isRecord(value.public_key)
  )
    return false;
  const key = value.public_key;
  return (
    typeof key.challenge === "string" &&
    /^[A-Za-z0-9_-]+$/.test(key.challenge) &&
    isRecord(key.rp) &&
    typeof key.rp.id === "string" &&
    !!key.rp.id &&
    typeof key.rp.name === "string" &&
    isRecord(key.user) &&
    typeof key.user.id === "string" &&
    /^[A-Za-z0-9_-]+$/.test(key.user.id) &&
    typeof key.user.name === "string" &&
    typeof key.user.displayName === "string" &&
    Array.isArray(key.pubKeyCredParams) &&
    key.pubKeyCredParams.length > 0 &&
    key.pubKeyCredParams.every(
      (parameter) =>
        isRecord(parameter) &&
        parameter.type === "public-key" &&
        Number.isInteger(parameter.alg),
    ) &&
    isRecord(key.authenticatorSelection) &&
    key.authenticatorSelection.userVerification === "required"
  );
}
export function getVerificationOptions(
  intent: VerificationIntent,
  options: AccountRequest = {},
) {
  return authenticatedRequest(
    "/me/passkeys/reauth/options",
    { ...options, method: "POST", body: JSON.stringify(intent) },
    isAssertionOptions,
  );
}
export function completeVerification(
  operation_id: string,
  credential: unknown,
  purpose: PasskeyGrant["purpose"],
) {
  return sessionCommand(
    "/me/passkeys/reauth/complete",
    { method: "POST", body: JSON.stringify({ operation_id, credential }) },
    (value): value is PasskeyGrant =>
      isPasskeyGrant(value) && value.purpose === purpose,
  );
}
export function requestRecoveryCode(password: string) {
  return sessionCommand(
    "/me/passkeys/recovery/request",
    { method: "POST", body: JSON.stringify({ password }), status: 202 },
    isMailCodeRequest,
  );
}
export function confirmRecoveryCode(request_id: string, code: string) {
  return sessionCommand(
    "/me/passkeys/recovery/confirm",
    { method: "POST", body: JSON.stringify({ request_id, code }) },
    (value): value is PasskeyGrant =>
      isPasskeyGrant(value) && value.purpose === "manage_passkeys",
  );
}
export function resendRecoveryCode(request_id: string) {
  return sessionCommand(
    "/me/passkeys/recovery/resend",
    { method: "POST", body: JSON.stringify({ request_id }), status: 202 },
    isMailCodeRequest,
  );
}
export function getRegistrationOptions(
  grant_id: string,
  label: string,
  expected_version: string,
  options: AccountRequest = {},
) {
  return authenticatedRequest(
    "/me/passkeys/registration/options",
    {
      ...options,
      method: "POST",
      body: JSON.stringify({ grant_id, label: label.trim(), expected_version }),
    },
    isCreationOptions,
  );
}
export function completeRegistration(
  operation_id: string,
  credential: unknown,
) {
  return sessionCommand(
    "/me/passkeys/registration/complete",
    { method: "POST", body: JSON.stringify({ operation_id, credential }) },
    isPasskeyReceipt,
  );
}
export function changePasskey(
  id: string,
  expected_version: string,
  grant_id: string,
  label?: string,
) {
  if (!uuid(id)) throw new AccountError("INVALID_ARGUMENT");
  return sessionCommand(
    `/me/passkeys/${id}`,
    {
      method: label === undefined ? "DELETE" : "PATCH",
      body: JSON.stringify({
        expected_version,
        grant_id,
        ...(label === undefined ? {} : { label: label.trim() }),
      }),
    },
    (value): value is PasskeyReceipt =>
      isPasskeyReceipt(value) && value.id === id,
  );
}
export function requestEmailChange(
  grant_id: string,
  email: string,
  expected_version: string,
) {
  return sessionCommand(
    "/me/email-change/request",
    {
      method: "POST",
      body: JSON.stringify({ grant_id, email, expected_version }),
      status: 202,
    },
    isMailCodeRequest,
  );
}
export function resendEmailChange(request_id: string) {
  return sessionCommand(
    "/me/email-change/resend",
    { method: "POST", body: JSON.stringify({ request_id }), status: 202 },
    isMailCodeRequest,
  );
}
export function confirmEmailChange(request_id: string, code: string) {
  return sessionCommand(
    "/me/email-change/confirm",
    { method: "POST", body: JSON.stringify({ request_id, code }) },
    isEmailChangeReceipt,
  );
}
export function requestEmailRecovery(authorization: string, password: string) {
  return sessionCommand(
    "/me/email-initialization/request",
    {
      method: "POST",
      body: JSON.stringify({ authorization, password }),
      status: 202,
    },
    isMailCodeRequest,
  );
}
export function resendEmailRecovery(request_id: string) {
  return sessionCommand(
    "/me/email-initialization/resend",
    { method: "POST", body: JSON.stringify({ request_id }), status: 202 },
    isMailCodeRequest,
  );
}
export function confirmEmailRecovery(request_id: string, code: string) {
  return sessionCommand(
    "/me/email-initialization/confirm",
    { method: "POST", body: JSON.stringify({ request_id, code }) },
    isEmailChangeReceipt,
  );
}
