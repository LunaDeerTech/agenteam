import { isRecord, isVersion } from "../account/types";
import { isCursorPage, isId, isInstant } from "./directory";
import { systemCommand, systemRequest } from "./requests";
import type { CursorPage } from "./useCursorList";
import { AccountError } from "../account/transport";
import { isPageSize } from "./pagination";

const phases = [
  "enqueue_pending",
  "pending",
  "claimed",
  "sending",
  "retry_wait",
  "sent",
  "failed",
  "unknown",
  "cancelled",
] as const;
const reasons = [
  "sent",
  "token_invalid",
  "configuration_invalid",
  "policy_rejected",
  "network_failed",
  "smtp_rejected",
  "timeout",
  "cancelled",
  "unknown",
] as const;
export interface InvitationDelivery {
  job_id: string;
  accepted_at: string;
  phase: (typeof phases)[number];
  attempts: string;
  version: string;
  channel: "smtp" | "backend_log" | null;
  attempt_result: "sent" | "failed" | "unknown" | "cancelled" | null;
  reason?: (typeof reasons)[number];
}
export interface Invitation {
  id: string;
  email: string;
  version: string;
  created_at: string;
  expires_at: string;
  latest_delivery: InvitationDelivery;
}
export interface InvitationReceipt {
  id: string;
  job_id: string;
  version: string;
}
const onlyKeys = (value: Record<string, unknown>, keys: readonly string[]) =>
  Object.keys(value).every((key) => keys.includes(key));
export function isInvitationDelivery(
  value: unknown,
): value is InvitationDelivery {
  return (
    isRecord(value) &&
    onlyKeys(value, [
      "job_id",
      "accepted_at",
      "phase",
      "attempts",
      "version",
      "channel",
      "attempt_result",
      "reason",
    ]) &&
    isId(value.job_id) &&
    isInstant(value.accepted_at) &&
    phases.includes(value.phase as InvitationDelivery["phase"]) &&
    typeof value.attempts === "string" &&
    /^[0-6]$/.test(value.attempts) &&
    isVersion(value.version) &&
    [null, "smtp", "backend_log"].includes(value.channel as null) &&
    [null, "sent", "failed", "unknown", "cancelled"].includes(
      value.attempt_result as null,
    ) &&
    (value.reason === undefined ||
      reasons.includes(
        value.reason as InvitationDelivery["reason"] & string,
      )) &&
    !(value.channel === null && value.attempt_result !== null) &&
    !(value.channel !== null && value.attempts === "0") &&
    !(
      value.channel === null &&
      ["claimed", "sending", "retry_wait", "sent", "failed"].includes(
        String(value.phase),
      )
    ) &&
    (value.phase !== "enqueue_pending" ||
      (value.attempts === "0" &&
        value.version === "1" &&
        value.reason === undefined &&
        value.channel === null))
  );
}
export function isInvitation(value: unknown): value is Invitation {
  return (
    isRecord(value) &&
    onlyKeys(value, [
      "id",
      "email",
      "version",
      "created_at",
      "expires_at",
      "latest_delivery",
    ]) &&
    isId(value.id) &&
    typeof value.email === "string" &&
    value.email.length > 0 &&
    value.email.length <= 254 &&
    isVersion(value.version) &&
    isInstant(value.created_at) &&
    isInstant(value.expires_at) &&
    isInvitationDelivery(value.latest_delivery)
  );
}
export function listInvitations(
  cursor?: string,
  current?: () => boolean,
  limit = 10,
) {
  if (!isPageSize(limit)) throw new AccountError("INVALID_ARGUMENT");
  return systemRequest(
    "/system/invitations",
    { query: { limit, cursor }, current },
    (value): value is CursorPage<Invitation> =>
      isCursorPage(value, isInvitation, limit),
  );
}
function receipt(value: unknown): value is InvitationReceipt {
  return (
    isRecord(value) &&
    onlyKeys(value, ["id", "job_id", "version"]) &&
    isId(value.id) &&
    isId(value.job_id) &&
    isVersion(value.version)
  );
}
export function createInvitation(email: string, current?: () => boolean) {
  return systemCommand(
    "/system/invitations",
    { method: "POST", status: 201, body: JSON.stringify({ email }), current },
    receipt,
  );
}
export function resendInvitation(
  invitation: Invitation,
  current?: () => boolean,
) {
  return systemCommand(
    `/system/invitations/${invitation.id}/resend`,
    {
      method: "POST",
      status: 202,
      body: JSON.stringify({ version: invitation.version }),
      current,
    },
    (value): value is InvitationReceipt =>
      receipt(value) && value.id === invitation.id,
  );
}
export function revokeInvitation(
  invitation: Invitation,
  current?: () => boolean,
) {
  return systemCommand(
    `/system/invitations/${invitation.id}/revoke`,
    {
      method: "POST",
      status: 204,
      body: JSON.stringify({ version: invitation.version }),
      current,
    },
    (value): value is undefined => value === undefined,
  );
}
export const deliveryLabel: Record<InvitationDelivery["phase"], string> = {
  enqueue_pending: "等待入队",
  pending: "等待处理",
  claimed: "正在准备",
  sending: "正在尝试发送",
  retry_wait: "等待自动重试",
  sent: "发送已确认",
  failed: "投递失败",
  unknown: "投递结果未知",
  cancelled: "已取消",
};
export const attemptLabel = {
  sent: "发送已确认",
  failed: "尝试失败",
  unknown: "结果未知",
  cancelled: "已取消",
};
export function deliveryText(delivery: InvitationDelivery, attempt = false) {
  const state = attempt ? delivery.attempt_result : delivery.phase;
  if (state === "sent")
    return delivery.channel === "smtp" ? "邮件服务器已接收" : "恢复日志已写入";
  return attempt
    ? state
      ? attemptLabel[state as keyof typeof attemptLabel]
      : "尚未记录"
    : deliveryLabel[delivery.phase];
}
export const reasonLabel: Record<
  NonNullable<InvitationDelivery["reason"]>,
  string
> = {
  sent: "发送端已确认",
  token_invalid: "业务链接已失效",
  configuration_invalid: "发送配置无效",
  policy_rejected: "出站规则拒绝",
  network_failed: "网络连接失败",
  smtp_rejected: "邮件服务器拒绝",
  timeout: "请求超时",
  cancelled: "已取消",
  unknown: "原因未知",
};
