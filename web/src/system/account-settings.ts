import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { AccountError, type AccountRequest } from "../account/transport";
import { type AccountCommand } from "../account/session";
import { isRecord, isVersion } from "../account/types";
import { systemCommand, systemRequest, useSystemAccess } from "./requests";

export const SETTINGS_PAGE_SIZE = 10;
const maxVersion = 9223372036854775807n;
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const encoder = new TextEncoder();
type ReadOptions = Pick<AccountRequest, "current">;

function exact(
  value: unknown,
  required: string[],
  optional: string[] = [],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    required.every((key) => key in value) &&
    Object.keys(value).every(
      (key) => required.includes(key) || optional.includes(key),
    )
  );
}
function id(value: unknown): value is string {
  return typeof value === "string" && uuid.test(value);
}
function decimal(
  value: unknown,
  minimum: number,
  maximum: number,
): value is string {
  return (
    typeof value === "string" &&
    /^(0|[1-9]\d*)$/.test(value) &&
    value.length <= 10 &&
    BigInt(value) >= BigInt(minimum) &&
    BigInt(value) <= BigInt(maximum)
  );
}
function instant(value: unknown): value is string {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value)
  )
    return false;
  const date = new Date(value);
  return (
    !Number.isNaN(date.getTime()) &&
    date.toISOString().slice(0, 23) === value.slice(0, 23)
  );
}
function text(value: unknown, maxBytes: number): value is string {
  return (
    typeof value === "string" &&
    !/[\u0000-\u001f\u007f-\u009f]/.test(value) &&
    encoder.encode(value).length <= maxBytes
  );
}
export function settingsEmailIssue(value: string): string {
  return value.length >= 3 &&
    value.length <= 254 &&
    /^[\x21-\x7e]+$/.test(value) &&
    value.indexOf("@") > 0 &&
    value.lastIndexOf("@") === value.indexOf("@") &&
    !value.endsWith("@")
    ? ""
    : "请输入完整邮箱地址，不要包含空格或显示名称。";
}
function writeVersion(value: string) {
  if (!isVersion(value) || BigInt(value) === maxVersion)
    throw new AccountError("INVALID_ARGUMENT");
}

export interface AccountSettingsValues {
  session_idle_seconds: string;
  session_absolute_seconds: string;
  password_reset_seconds: string;
  challenge_after_failures: string;
}
export interface AccountSettings extends AccountSettingsValues {
  id: string;
  version: string;
  lifetime_changes_apply_to: "newly_issued_sessions_and_tokens";
}
export const securityFields = [
  {
    key: "session_idle_seconds",
    label: "会话空闲期限（秒）",
    hint: "900–2592000 秒；不能超过绝对期限。",
    minimum: 900,
    maximum: 2592000,
  },
  {
    key: "session_absolute_seconds",
    label: "会话绝对期限（秒）",
    hint: "3600–7776000 秒。",
    minimum: 3600,
    maximum: 7776000,
  },
  {
    key: "password_reset_seconds",
    label: "密码重置链接期限（秒）",
    hint: "300–7200 秒。",
    minimum: 300,
    maximum: 7200,
  },
  {
    key: "challenge_after_failures",
    label: "登录失败挑战阈值（次）",
    hint: "1–20 次，达到阈值后需要完成登录验证。",
    minimum: 1,
    maximum: 20,
  },
] as const;
export function accountSettingsValues(
  value?: AccountSettings,
): AccountSettingsValues {
  return {
    session_idle_seconds: value?.session_idle_seconds ?? "",
    session_absolute_seconds: value?.session_absolute_seconds ?? "",
    password_reset_seconds: value?.password_reset_seconds ?? "",
    challenge_after_failures: value?.challenge_after_failures ?? "",
  };
}
export function accountSettingsIssues(
  value: AccountSettingsValues,
): Partial<Record<keyof AccountSettingsValues, string>> {
  const issues: Partial<Record<keyof AccountSettingsValues, string>> = {};
  for (const field of securityFields) {
    if (!decimal(value[field.key], field.minimum, field.maximum))
      issues[field.key] =
        `请输入 ${field.minimum}–${field.maximum} 之间的整数。`;
  }
  if (
    !issues.session_idle_seconds &&
    !issues.session_absolute_seconds &&
    BigInt(value.session_idle_seconds) > BigInt(value.session_absolute_seconds)
  )
    issues.session_idle_seconds = "空闲期限不能超过绝对期限。";
  return issues;
}
export function isAccountSettings(value: unknown): value is AccountSettings {
  return (
    exact(value, [
      "id",
      "version",
      "session_idle_seconds",
      "session_absolute_seconds",
      "password_reset_seconds",
      "challenge_after_failures",
      "lifetime_changes_apply_to",
    ]) &&
    id(value.id) &&
    isVersion(value.version) &&
    value.lifetime_changes_apply_to === "newly_issued_sessions_and_tokens" &&
    securityFields.every((field) =>
      decimal(value[field.key], field.minimum, field.maximum),
    ) &&
    BigInt(value.session_idle_seconds as string) <=
      BigInt(value.session_absolute_seconds as string)
  );
}
export function getAccountSettings(options: ReadOptions = {}) {
  return systemRequest("/system/account-settings", options, isAccountSettings);
}
export function saveAccountSettings(
  base: AccountSettings,
  values: AccountSettingsValues,
  options: ReadOptions = {},
) {
  writeVersion(base.version);
  if (Object.keys(accountSettingsIssues(values)).length)
    throw new AccountError("INVALID_ARGUMENT");
  const captured = { ...values };
  return systemCommand(
    "/system/account-settings",
    {
      ...options,
      method: "PUT",
      body: JSON.stringify({ version: base.version, ...captured }),
    },
    (value): value is AccountSettings =>
      isAccountSettings(value) &&
      value.id === base.id &&
      BigInt(value.version) === BigInt(base.version) + 1n &&
      securityFields.every((field) => value[field.key] === captured[field.key]),
    { uncertainOn: uncertainSettingsWrite },
  );
}

export interface SMTPSettings {
  id: string;
  version: string;
  configured: boolean;
  host: string;
  port: number;
  encryption: "" | "none" | "starttls" | "tls";
  username: string;
  sender_email: string;
  sender_name: string;
  credential_present: boolean;
  auto_retry_count: string;
  retry_interval_seconds: string;
}
export interface SMTPDraft {
  configured: boolean;
  host: string;
  port: string;
  encryption: string;
  username: string;
  sender_email: string;
  sender_name: string;
  credential_action: "keep" | "replace" | "remove";
  password: string;
  auto_retry_count: string;
  retry_interval_seconds: string;
}
export interface SMTPResult {
  applied_version: string;
  settings: SMTPSettings;
}
export function smtpDraft(value?: SMTPSettings): SMTPDraft {
  return {
    configured: value?.configured ?? false,
    host: value?.host ?? "",
    port: value?.port ? String(value.port) : "",
    encryption: value?.encryption ?? "",
    username: value?.username ?? "",
    sender_email: value?.sender_email ?? "",
    sender_name: value?.sender_name ?? "",
    credential_action: "keep",
    password: "",
    auto_retry_count: value?.auto_retry_count ?? "",
    retry_interval_seconds: value?.retry_interval_seconds ?? "",
  };
}
export function smtpIssues(value: SMTPDraft): Record<string, string> {
  const issues: Record<string, string> = {};
  if (!decimal(value.auto_retry_count, 0, 5))
    issues.auto_retry_count = "请输入 0–5 之间的整数。";
  if (!decimal(value.retry_interval_seconds, 10, 3600))
    issues.retry_interval_seconds = "请输入 10–3600 之间的整数。";
  if (!value.configured) return issues;
  if (!text(value.host, 253) || !value.host || /[\s/@?#]/.test(value.host))
    issues.host = "请输入主机名或 IP 地址，不要包含协议、端口或路径。";
  if (!decimal(value.port, 1, 65535))
    issues.port = "请输入 1–65535 之间的端口。";
  if (!["none", "starttls", "tls"].includes(value.encryption))
    issues.encryption = "请选择加密方式。";
  if (!text(value.username, 256))
    issues.username = "认证账号不能包含控制字符，最多 256 字节。";
  if (settingsEmailIssue(value.sender_email))
    issues.sender_email = settingsEmailIssue(value.sender_email);
  if (!text(value.sender_name, 320) || [...value.sender_name].length > 80)
    issues.sender_name = "发件人显示名最多 80 个字符，不能包含控制字符。";
  if (
    value.credential_action === "replace" &&
    (!value.password ||
      encoder.encode(value.password).length > 2048 ||
      /[\u0000\r\n]/.test(value.password))
  )
    issues.password = "请输入替换密码，最多 2048 字节且不能包含换行。";
  return issues;
}
export function isSMTPSettings(value: unknown): value is SMTPSettings {
  if (
    !exact(value, [
      "id",
      "version",
      "configured",
      "host",
      "port",
      "encryption",
      "username",
      "sender_email",
      "sender_name",
      "credential_present",
      "auto_retry_count",
      "retry_interval_seconds",
    ]) ||
    !id(value.id) ||
    !isVersion(value.version) ||
    typeof value.configured !== "boolean" ||
    typeof value.credential_present !== "boolean" ||
    !text(value.host, 253) ||
    !text(value.username, 256) ||
    !text(value.sender_email, 254) ||
    !text(value.sender_name, 320) ||
    [...value.sender_name].length > 80 ||
    !decimal(value.auto_retry_count, 0, 5) ||
    !decimal(value.retry_interval_seconds, 10, 3600) ||
    !Number.isInteger(value.port)
  )
    return false;
  if (!value.configured)
    return (
      value.host === "" &&
      value.port === 0 &&
      value.encryption === "" &&
      value.username === "" &&
      value.sender_email === "" &&
      value.sender_name === "" &&
      !value.credential_present
    );
  return (
    (value.port as number) >= 1 &&
    (value.port as number) <= 65535 &&
    value.host.length > 0 &&
    ["none", "starttls", "tls"].includes(String(value.encryption)) &&
    !settingsEmailIssue(value.sender_email)
  );
}
export function isSMTPResult(value: unknown): value is SMTPResult {
  return (
    exact(value, ["applied_version", "settings"]) &&
    isVersion(value.applied_version) &&
    isSMTPSettings(value.settings) &&
    BigInt(value.settings.version) >= BigInt(value.applied_version)
  );
}
export function getSMTPSettings(options: ReadOptions = {}) {
  return systemRequest("/system/smtp", options, isSMTPSettings);
}
function smtpResultFor(base: SMTPSettings) {
  const singleton = base.id;
  const expected = BigInt(base.version) + 1n;
  return (value: unknown): value is SMTPResult =>
    isSMTPResult(value) &&
    value.settings.id === singleton &&
    BigInt(value.applied_version) === expected;
}
export function saveSMTPSettings(
  base: SMTPSettings,
  values: SMTPDraft,
  options: ReadOptions = {},
) {
  writeVersion(base.version);
  if (Object.keys(smtpIssues(values)).length || !values.configured)
    throw new AccountError("INVALID_ARGUMENT");
  return systemCommand(
    "/system/smtp",
    {
      ...options,
      method: "PUT",
      body: JSON.stringify({
        version: base.version,
        host: values.host,
        port: Number(values.port),
        encryption: values.encryption,
        username: values.username,
        sender_email: values.sender_email,
        sender_name: values.sender_name,
        credential_action:
          values.credential_action === "remove" ? "remove" : "keep",
        ...(values.credential_action === "replace"
          ? { password: values.password }
          : {}),
        auto_retry_count: values.auto_retry_count,
        retry_interval_seconds: values.retry_interval_seconds,
      }),
    },
    smtpResultFor(base),
    { uncertainOn: uncertainSMTPWrite },
  );
}
export function unconfigureSMTP(
  base: SMTPSettings,
  policy: Pick<SMTPDraft, "auto_retry_count" | "retry_interval_seconds">,
  options: ReadOptions = {},
) {
  writeVersion(base.version);
  if (
    !decimal(policy.auto_retry_count, 0, 5) ||
    !decimal(policy.retry_interval_seconds, 10, 3600)
  )
    throw new AccountError("INVALID_ARGUMENT");
  return systemCommand(
    "/system/smtp/unconfigure",
    {
      ...options,
      method: "POST",
      body: JSON.stringify({ version: base.version, ...policy }),
    },
    smtpResultFor(base),
    { uncertainOn: uncertainSMTPWrite },
  );
}

export const deliveryPhases = {
  enqueue_pending: "等待入队",
  pending: "等待投递",
  claimed: "已领取",
  sending: "投递中",
  retry_wait: "等待自动重试",
  sent: "已完成投递",
  failed: "投递失败",
  unknown: "结果未知",
  cancelled: "已取消",
} as const;
export const deliveryResults = {
  sent: "已接受",
  failed: "失败",
  unknown: "结果未知",
  cancelled: "已取消",
} as const;
export const deliveryReasons = {
  sent: "SMTP 已接受或恢复日志已写入",
  token_invalid: "业务链接已失效",
  configuration_invalid: "邮件配置不可用",
  policy_rejected: "出站规则拒绝",
  network_failed: "网络连接失败",
  smtp_rejected: "SMTP 服务拒绝",
  timeout: "等待超时",
  cancelled: "已取消",
  unknown: "结果未知",
} as const;
export const deliveryKinds = {
  invitation: "邀请",
  password_reset: "密码重置",
  test: "测试",
} as const;
type DeliveryChannel = "smtp" | "backend_log";
export interface MailJobManagement {
  job_id: string;
  kind: keyof typeof deliveryKinds;
  phase: keyof typeof deliveryPhases;
  attempts: string;
  version: string;
  reason?: keyof typeof deliveryReasons;
  channel: DeliveryChannel;
  created_at: string;
  attempt_channel: DeliveryChannel | null;
  attempt_result: keyof typeof deliveryResults | null;
}
export interface MailJobPage {
  items: MailJobManagement[];
  next_cursor?: string;
}
export interface JobAccepted {
  job_id: string;
}
export interface JobRetryAccepted extends JobAccepted {
  version: string;
}
export type MailJobReceipt = JobRetryAccepted;
export function isMailJob(value: unknown): value is MailJobManagement {
  if (
    !exact(
      value,
      [
        "job_id",
        "kind",
        "phase",
        "attempts",
        "version",
        "channel",
        "created_at",
        "attempt_channel",
        "attempt_result",
      ],
      ["reason"],
    ) ||
    !id(value.job_id) ||
    !isVersion(value.version) ||
    !instant(value.created_at) ||
    !decimal(value.attempts, 0, 6) ||
    typeof value.kind !== "string" ||
    !Object.hasOwn(deliveryKinds, value.kind) ||
    typeof value.phase !== "string" ||
    !Object.hasOwn(deliveryPhases, value.phase) ||
    !["smtp", "backend_log"].includes(String(value.channel)) ||
    !["smtp", "backend_log", null].includes(
      value.attempt_channel as DeliveryChannel | null,
    ) ||
    !(
      value.attempt_result === null ||
      (typeof value.attempt_result === "string" &&
        Object.hasOwn(deliveryResults, value.attempt_result))
    ) ||
    !(
      value.reason === undefined ||
      (typeof value.reason === "string" &&
        Object.hasOwn(deliveryReasons, value.reason))
    )
  )
    return false;
  if (value.attempt_channel === null && value.attempt_result !== null)
    return false;
  if (
    value.attempt_channel !== null &&
    (value.attempt_channel !== value.channel || value.attempts === "0")
  )
    return false;
  if (
    value.attempt_channel === null &&
    ["claimed", "sending", "retry_wait", "sent", "failed"].includes(value.phase)
  )
    return false;
  if (
    value.phase === "enqueue_pending" &&
    (value.attempts !== "0" ||
      value.version !== "1" ||
      value.reason !== undefined ||
      value.attempt_channel !== null)
  )
    return false;
  return true;
}
export function isMailJobPage(value: unknown): value is MailJobPage {
  if (
    !exact(value, ["items"], ["next_cursor"]) ||
    !Array.isArray(value.items) ||
    value.items.length > SETTINGS_PAGE_SIZE ||
    !value.items.every(isMailJob)
  )
    return false;
  if (
    value.next_cursor !== undefined &&
    (typeof value.next_cursor !== "string" ||
      !value.next_cursor ||
      value.next_cursor.length > 8192 ||
      value.items.length !== SETTINGS_PAGE_SIZE)
  )
    return false;
  const seen = new Set<string>();
  return value.items.every(
    (item: MailJobManagement, index: number, items: MailJobManagement[]) => {
      if (seen.has(item.job_id)) return false;
      seen.add(item.job_id);
      const previous = items[index - 1];
      return (
        !previous ||
        previous.created_at > item.created_at ||
        (previous.created_at === item.created_at &&
          previous.job_id > item.job_id)
      );
    },
  );
}
export function listSMTPJobs(cursor?: string, options: ReadOptions = {}) {
  if (cursor !== undefined && (!cursor || cursor.length > 8192))
    throw new AccountError("INVALID_ARGUMENT");
  return systemRequest(
    "/system/mail-jobs/management",
    {
      ...options,
      query: {
        limit: String(SETTINGS_PAGE_SIZE),
        ...(cursor ? { cursor } : {}),
      },
    },
    isMailJobPage,
  );
}
export function getSMTPJob(jobId: string, options: ReadOptions = {}) {
  if (!id(jobId)) throw new AccountError("INVALID_ARGUMENT");
  return systemRequest(
    `/system/mail-jobs/${jobId}/management`,
    options,
    (value): value is MailJobManagement =>
      isMailJob(value) && value.job_id === jobId,
  );
}
export function testSMTP(recipient: string, options: ReadOptions = {}) {
  if (settingsEmailIssue(recipient)) throw new AccountError("INVALID_ARGUMENT");
  return systemCommand(
    "/system/smtp/test",
    {
      ...options,
      method: "POST",
      status: 202,
      body: JSON.stringify({ recipient }),
    },
    (value): value is JobAccepted =>
      exact(value, ["job_id"]) && id(value.job_id),
    { uncertainOn: uncertainSMTPWrite },
  );
}
export function retrySMTPJob(
  job: MailJobManagement,
  options: ReadOptions = {},
) {
  return retryMailJobCommand(job.job_id, job.version, options);
}
export function retryMailJobCommand(
  jobId: string,
  version: string,
  options: ReadOptions = {},
) {
  writeVersion(version);
  if (!id(jobId)) throw new AccountError("INVALID_ARGUMENT");
  return systemCommand(
    `/system/mail-jobs/${jobId}/retry`,
    {
      ...options,
      method: "POST",
      status: 202,
      body: JSON.stringify({ version }),
    },
    (value): value is JobRetryAccepted =>
      exact(value, ["job_id", "version"]) &&
      id(value.job_id) &&
      value.job_id !== jobId &&
      isVersion(value.version),
    { uncertainOn: uncertainSMTPWrite },
  );
}
export function mailJobRetryReason(job: {
  phase: string;
  attempt_result: string | null;
  version: string;
}): string {
  if (!isVersion(job.version) || BigInt(job.version) === maxVersion)
    return "此任务版本已达到上限。";
  if (!["sent", "failed", "cancelled", "unknown"].includes(job.phase))
    return "当前任务仍在处理，请先重新读取状态。";
  if (job.attempt_result === null && job.phase !== "cancelled")
    return "尚无可确认的终局尝试，暂时不能申请重试。";
  return "";
}
export function jobRetryIssue(job: MailJobManagement): string {
  const basic = mailJobRetryReason(job);
  if (basic) return basic;
  if (
    (job.attempt_channel !== null && job.attempt_result === null) ||
    (job.attempt_channel === null && job.phase !== "cancelled")
  )
    return "尚无可确认的终局尝试，暂时不能申请重试。";
  return "";
}
export function deliveryChannel(value: DeliveryChannel | null): string {
  return value === "smtp"
    ? "SMTP"
    : value === "backend_log"
      ? "后台恢复日志"
      : "尚未记录尝试渠道";
}

const knownBefore = new Set([
  "INVALID_ARGUMENT",
  "VERSION_CONFLICT",
  "INVALID_STATE",
  "RATE_LIMITED",
  "RESOURCE_DELETED",
]);
function localOrAuthorization(error: AccountError) {
  return (
    ["SERVICE_NOT_CONNECTED", "INVALID_REQUEST", "STALE_REQUEST"].includes(
      error.code,
    ) ||
    error.status === 401 ||
    error.status === 403 ||
    error.code === "CSRF_FAILED"
  );
}
export function uncertainSMTPWrite(error: AccountError): boolean {
  if (localOrAuthorization(error)) return false;
  return !(
    error.status < 500 &&
    (error.commitState === "not_started" ||
      error.commitState === "not_committed") &&
    knownBefore.has(error.code)
  );
}
function uncertainSettingsWrite(error: AccountError): boolean {
  if (localOrAuthorization(error)) return false;
  return (
    error.status >= 500 ||
    error.code === "RESOURCE_BUSY" ||
    error.code === "IDEMPOTENCY_KEY_REUSED" ||
    error.commitState === "unknown" ||
    error.commitState === "committed"
  );
}

// The command alone retains its immutable request bytes. Pages receive no key,
// CSRF or saved credential, and a later refusal never erases an earlier unknown.
export function useSettingsCommand<T>() {
  const access = useSystemAccess();
  const busy = ref(false);
  const uncertain = ref(false);
  const confirmed = ref(false);
  const replayVerified = ref(false);
  const error = shallowRef<unknown>();
  const result = shallowRef<T>();
  let command: AccountCommand<T> | undefined;
  let received: ((value: T) => void | Promise<void>) | undefined;
  let revision = 0;
  let alive = true;
  const replayable = computed(() => {
    void access.generation.value;
    return (
      uncertain.value &&
      replayVerified.value &&
      !!command?.active &&
      !(
        error.value instanceof AccountError &&
        error.value.code === "IDEMPOTENCY_KEY_REUSED"
      )
    );
  });
  function abandon() {
    revision++;
    command?.release();
    command = undefined;
    received = undefined;
    uncertain.value = false;
    replayVerified.value = false;
    confirmed.value = false;
    error.value = undefined;
    result.value = undefined;
  }
  async function execute() {
    const active = command;
    if (
      !active ||
      busy.value ||
      !active.active ||
      (uncertain.value && !replayable.value)
    )
      return;
    const captured = revision;
    busy.value = true;
    error.value = undefined;
    try {
      const value = await active.run();
      if (!alive || captured !== revision) return;
      confirmed.value = true;
      uncertain.value = false;
      result.value = value;
      command = undefined;
      active.release();
      await received?.(value);
    } catch (failure) {
      if (!alive || captured !== revision) return;
      error.value = failure;
      if (
        failure instanceof AccountError &&
        ["unknown", "committed"].includes(failure.commitState) &&
        active.active
      ) {
        uncertain.value = true;
        replayVerified.value = false;
      }
      if (!uncertain.value) {
        active.release();
        command = undefined;
      }
    } finally {
      if (alive) busy.value = false;
    }
  }
  async function submit(
    create: () => AccountCommand<T>,
    onSuccess?: (value: T) => void | Promise<void>,
  ) {
    if (busy.value || uncertain.value) return;
    abandon();
    try {
      command = create();
      received = onSuccess;
    } catch (failure) {
      error.value = failure;
      return;
    }
    await execute();
  }
  async function verify() {
    const captured = revision;
    await access.recheck();
    if (
      alive &&
      captured === revision &&
      access.allowed.value &&
      command?.active
    ) {
      replayVerified.value = true;
      return true;
    }
    return false;
  }
  watch(access.generation, abandon, { flush: "sync" });
  onBeforeUnmount(() => {
    alive = false;
    abandon();
  });
  return {
    busy,
    uncertain,
    confirmed,
    error,
    result,
    replayable,
    submit,
    retry: execute,
    verify,
    abandon,
  };
}
