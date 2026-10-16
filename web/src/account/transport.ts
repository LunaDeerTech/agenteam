import { isRecord } from "./types";

export type CommitState =
  "not_started" | "committed" | "not_committed" | "unknown";
export class AccountError extends Error {
  constructor(
    readonly code: string,
    readonly status = 0,
    readonly commitState: CommitState = "not_committed",
    readonly fields: ReadonlyArray<{ path: string; code: string }> = [],
  ) {
    super(code);
    this.name = "AccountError";
  }
}

const loopback = new Set(["localhost", "127.0.0.1", "[::1]"]);
export const accountEnabled =
  import.meta.env.VITE_ACCOUNT_ENABLED === "true" &&
  (window.location.protocol === "https:" ||
    loopback.has(window.location.hostname));
const apiBase = `${import.meta.env.BASE_URL}api/v1`;
let tail: Promise<unknown> = Promise.resolve();

export interface AccountRequest {
  method?: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  body?: string | Blob;
  csrf?: string;
  key?: string;
  headers?: Record<string, string>;
  current?: () => boolean;
  binary?: boolean;
  status?: number;
  query?: Readonly<{ cursor?: string; limit?: string | number }>;
}

export interface AccountImage {
  blob: Blob;
  etag: string | null;
  contentLength: string | null;
}

async function readBody(
  response: Response,
  limit: number,
): Promise<ArrayBuffer[]> {
  const reader = response.body?.getReader();
  if (!reader) return [];
  const chunks: ArrayBuffer[] = [];
  let length = 0;
  try {
    for (;;) {
      const part = await reader.read();
      if (part.done) return chunks;
      length += part.value.byteLength;
      if (length > limit) {
        await reader.cancel();
        throw new Error("response_limit");
      }
      chunks.push(part.value.slice().buffer);
    }
  } finally {
    reader.releaseLock();
  }
}

// Keep ownership until the response body has drained. Even a rejected read can
// carry Set-Cookie, which the browser applies before application callbacks.
async function sendAccount<T>(
  path: string,
  options: AccountRequest = {},
  validate?: (value: unknown) => value is T,
): Promise<T> {
  if (!accountEnabled) throw new AccountError("SERVICE_NOT_CONNECTED");
  if (options.current && !options.current())
    throw new AccountError("STALE_REQUEST");
  if (!path.startsWith("/") || path.includes("?") || path.includes("#"))
    throw new AccountError("INVALID_REQUEST");
  const method = options.method ?? "GET";
  const query = new URLSearchParams();
  for (const [name, value] of Object.entries(options.query ?? {})) {
    if (value === undefined) continue;
    if (
      method !== "GET" ||
      !["cursor", "limit"].includes(name) ||
      !String(value) ||
      new TextEncoder().encode(String(value)).length > 8192 ||
      (name === "limit" &&
        (!/^[1-9]\d{0,2}$/.test(String(value)) || Number(value) > 100))
    )
      throw new AccountError("INVALID_REQUEST");
    query.set(name, String(value));
  }
  const suffix = query.size ? `?${query.toString()}` : "";
  if (
    typeof options.body === "string" &&
    new TextEncoder().encode(options.body).length >
      (method === "PUT" && path === "/system/outbound-policy"
        ? 1048576
        : method === "PUT" && path === "/system/smtp"
          ? 32768
          : 16384)
  )
    throw new AccountError("INVALID_ARGUMENT");
  const mutation = !!options.key;
  const headers = new Headers({
    Accept: options.binary
      ? "image/png,image/jpeg,image/webp"
      : "application/json",
    ...options.headers,
  });
  if (typeof options.body === "string")
    headers.set("Content-Type", "application/json");
  if (options.csrf) headers.set("X-CSRF-Token", options.csrf);
  if (options.key) headers.set("Idempotency-Key", options.key);
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 20000);
  try {
    const response = await fetch(`${apiBase}${path}${suffix}`, {
      method,
      headers,
      body: options.body,
      signal: controller.signal,
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
    });
    const mediaType = response.headers
      .get("Content-Type")
      ?.split(";")[0]
      ?.trim();
    const parts = await readBody(
      response,
      response.ok && options.binary ? 5 * 1024 * 1024 : 1024 * 1024,
    );
    let value: unknown;
    if (response.ok && options.binary) {
      const blob = new Blob(parts, { type: mediaType });
      if (
        blob.size > 5 * 1024 * 1024 ||
        !["image/png", "image/jpeg", "image/webp"].includes(blob.type)
      )
        throw new AccountError("INVALID_RESPONSE");
      value = {
        blob,
        etag: response.headers.get("ETag"),
        contentLength: response.headers.get("Content-Length"),
      } satisfies AccountImage;
    } else {
      const text = await new Blob(parts).text();
      if (
        response.status !== 204 &&
        mediaType !==
          (response.ok ? "application/json" : "application/problem+json")
      )
        throw new AccountError(
          "INVALID_RESPONSE",
          response.status,
          mutation ? "unknown" : "not_committed",
        );
      try {
        value = text ? JSON.parse(text) : undefined;
      } catch {
        throw new AccountError(
          "INVALID_RESPONSE",
          response.status,
          mutation ? "unknown" : "not_committed",
        );
      }
    }
    if (options.current && !options.current())
      throw new AccountError(
        "STALE_REQUEST",
        0,
        mutation ? "unknown" : "not_committed",
      );
    if (!response.ok) {
      if (!isRecord(value) || typeof value.code !== "string")
        throw new AccountError(
          "INVALID_RESPONSE",
          response.status,
          mutation ? "unknown" : "not_committed",
        );
      const commit = value.commit_state;
      const fields = Array.isArray(value.field_errors)
        ? value.field_errors.filter(
            (field): field is { path: string; code: string } =>
              isRecord(field) &&
              typeof field.path === "string" &&
              typeof field.code === "string",
          )
        : [];
      throw new AccountError(
        value.code,
        response.status,
        commit === "not_started" ||
          commit === "committed" ||
          commit === "not_committed"
          ? commit
          : "unknown",
        fields,
      );
    }
    if (response.status !== (options.status ?? 200))
      throw new AccountError(
        "INVALID_RESPONSE",
        response.status,
        mutation ? "unknown" : "not_committed",
      );
    if (validate && !validate(value))
      throw new AccountError(
        "INVALID_RESPONSE",
        response.status,
        mutation ? "unknown" : "not_committed",
      );
    return value as T;
  } catch (error) {
    if (error instanceof AccountError) throw error;
    throw new AccountError(
      "NETWORK_ERROR",
      0,
      mutation ? "unknown" : "not_committed",
    );
  } finally {
    window.clearTimeout(timeout);
  }
}

export type AccountSender = typeof sendAccount;

export function accountSequence<T>(
  operation: (send: AccountSender) => Promise<T>,
): Promise<T> {
  const result = tail.then(() => operation(sendAccount));
  tail = result.catch(() => undefined);
  return result;
}

export function accountRequest<T>(
  path: string,
  options: AccountRequest = {},
  validate?: (value: unknown) => value is T,
): Promise<T> {
  return accountSequence((send) => send(path, options, validate));
}

export function accountErrorMessage(error: unknown): string {
  if (!(error instanceof AccountError)) return "暂时无法完成操作，请稍后重试。";
  const messages: Record<string, string> = {
    SERVICE_NOT_CONNECTED: "账号服务尚未连接，暂时无法操作。",
    NETWORK_ERROR: "连接中断，请检查网络后重试。",
    INVALID_RESPONSE: "暂时无法确认服务响应，请重试。",
    UNAUTHENTICATED: "登录信息无效或已过期，请重新登录。",
    SESSION_REVOKED: "登录已失效，请重新登录。",
    FORBIDDEN: "你目前无法执行此操作。",
    CSRF_FAILED: "页面验证已过期，请重新载入后再试。",
    ORIGIN_DENIED: "当前访问地址无法连接账号服务。",
    INVALID_ARGUMENT: "请检查填写内容后再试。",
    VERSION_CONFLICT: "资料已发生变化。请重新读取，再确认你的修改。",
    RATE_LIMITED: "操作过于频繁，请稍后重试。",
    CHALLENGE_REQUIRED: "请先完成验证，再继续登录。",
    CHALLENGE_INVALID: "验证未通过或已过期，请重新验证。",
    RESOURCE_DELETED: "链接已失效，请申请新的链接。",
    NOT_FOUND: "链接不存在或已失效。",
    PAYLOAD_TOO_LARGE: "图片不能超过 5 MB。",
    UNSUPPORTED_MEDIA_TYPE: "请选择 JPEG、PNG 或 WebP 图片。",
  };
  return error.commitState === "unknown"
    ? "操作结果暂时无法确认。请保留当前页面，使用原请求继续确认。"
    : (messages[error.code] ?? "暂时无法完成操作，请稍后重试。");
}
