import { AccountError, type AccountRequest } from "../account/transport";
import { isRecord, isVersion } from "../account/types";
import { isInstant } from "./directory";
import { systemCommand, systemRequest } from "./requests";

export interface OutboundRule {
  cidr: string;
  ports: "all" | number[];
  allow_http: boolean;
}
export interface OutboundPolicy {
  version: string;
  rules: OutboundRule[];
}
export interface OutboundReceipt {
  version: string;
  rule_count: string;
  audit_id: string;
  created_at: string;
}
export interface RuleDraft {
  cidr: string;
  ports: string;
  allow_http: boolean;
}
type ReadOptions = Pick<AccountRequest, "current">;
export const exact = (
  value: unknown,
  keys: string[],
): value is Record<string, unknown> =>
  isRecord(value) &&
  Object.keys(value).length === keys.length &&
  keys.every((key) => key in value);

/** Parse only a private network prefix; normalize masked addresses for editing. */
export function canonicalPrivateCidr(input: string): string | undefined {
  const match = /^([^/]+)\/(0|[1-9]\d{0,2})$/.exec(input.trim());
  if (!match) return;
  const address = match[1]!;
  const bits = Number(match[2]);
  if (!address.includes(":")) {
    const octets = address.split(".");
    if (
      bits > 32 ||
      octets.length !== 4 ||
      !octets.every(
        (part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255,
      )
    )
      return;
    const values = octets.map(Number);
    if (!(
      (values[0] === 10 && bits >= 8) ||
      (values[0] === 172 &&
        values[1]! >= 16 &&
        values[1]! <= 31 &&
        bits >= 12) ||
      (values[0] === 192 && values[1] === 168 && bits >= 16)
    ))
      return;
    let number = values.reduce(
      (result, part) => (result << 8n) | BigInt(part),
      0n,
    );
    number = (number >> BigInt(32 - bits)) << BigInt(32 - bits);
    return (
      [24, 16, 8, 0]
        .map((shift) => Number((number >> BigInt(shift)) & 255n))
        .join(".") + `/${bits}`
    );
  }
  if (bits < 7 || bits > 128 || !/^[0-9a-f:]+$/i.test(address)) return;
  const halves = address.split("::");
  if (halves.length > 2) return;
  const parts = (value: string) => (value ? value.split(":") : []);
  const left = parts(halves[0]!);
  const right = halves.length === 2 ? parts(halves[1]!) : [];
  if (![...left, ...right].every((part) => /^[0-9a-f]{1,4}$/i.test(part)))
    return;
  const count = left.length + right.length;
  if (
    (halves.length === 1 && count !== 8) ||
    (halves.length === 2 && count >= 8)
  )
    return;
  const words = [...left, ...Array<string>(8 - count).fill("0"), ...right].map(
    (part) => parseInt(part, 16),
  );
  if ((words[0]! & 0xfe00) !== 0xfc00) return;
  let number = words.reduce(
    (result, part) => (result << 16n) | BigInt(part),
    0n,
  );
  number = (number >> BigInt(128 - bits)) << BigInt(128 - bits);
  const network = Array.from({ length: 8 }, (_, index) =>
    Number((number >> BigInt((7 - index) * 16)) & 65535n),
  );
  let start = -1;
  let length = 1;
  for (let index = 0; index < 8;) {
    if (network[index] !== 0) {
      index++;
      continue;
    }
    const beginning = index;
    while (index < 8 && network[index] === 0) index++;
    if (index - beginning > length) {
      start = beginning;
      length = index - beginning;
    }
  }
  const formatted = network.map((part) => part.toString(16));
  const canonical =
    start < 0
      ? formatted.join(":")
      : `${formatted.slice(0, start).join(":")}::${formatted.slice(start + length).join(":")}`;
  return `${canonical}/${bits}`;
}

export function parsePorts(input: string): OutboundRule["ports"] | undefined {
  if (input.trim().toLowerCase() === "all") return "all";
  const tokens = input.trim().split(/[,，\s]+/);
  if (
    !tokens.length ||
    tokens.length > 256 ||
    !tokens.every(
      (part) => /^[1-9]\d{0,4}$/.test(part) && Number(part) <= 65535,
    )
  )
    return;
  const ports = tokens.map(Number);
  if (new Set(ports).size !== ports.length) return;
  return ports.sort((left, right) => left - right);
}
export function ruleDrafts(policy?: OutboundPolicy): RuleDraft[] {
  return (
    policy?.rules.map((rule) => ({
      ...rule,
      ports: rule.ports === "all" ? "all" : rule.ports.join(", "),
    })) ?? []
  );
}
export function ruleIssues(drafts: RuleDraft[]): Record<string, string> {
  const issues: Record<string, string> = {};
  if (drafts.length > 256) issues.rules = "最多可设置 256 条规则。";
  const seen = new Set<string>();
  drafts.forEach((draft, index) => {
    const cidr = canonicalPrivateCidr(draft.cidr);
    const ports = parsePorts(draft.ports);
    if (!cidr)
      issues[`${index}.cidr`] = "请输入 RFC1918 或 IPv6 ULA 私网 CIDR。";
    if (!ports)
      issues[`${index}.ports`] =
        "填写 all 或 1–65535 的端口，以逗号分隔，最多 256 个且不能重复。";
    if (cidr && ports) {
      const key = JSON.stringify([cidr, ports, draft.allow_http]);
      if (seen.has(key)) issues[`${index}.cidr`] = "此规则与另一条规则重复。";
      seen.add(key);
    }
  });
  return issues;
}
export function isOutboundRule(value: unknown): value is OutboundRule {
  return (
    exact(value, ["cidr", "ports", "allow_http"]) &&
    typeof value.cidr === "string" &&
    canonicalPrivateCidr(value.cidr) === value.cidr &&
    typeof value.allow_http === "boolean" &&
    (value.ports === "all" ||
      (Array.isArray(value.ports) &&
        value.ports.length > 0 &&
        value.ports.length <= 256 &&
        value.ports.every(
          (port, index, ports) =>
            Number.isInteger(port) &&
            port >= 1 &&
            port <= 65535 &&
            (index === 0 || ports[index - 1] < port),
        )))
  );
}
export function isOutboundPolicy(value: unknown): value is OutboundPolicy {
  return (
    exact(value, ["version", "rules"]) &&
    isVersion(value.version) &&
    Array.isArray(value.rules) &&
    value.rules.length <= 256 &&
    value.rules.every(isOutboundRule) &&
    new Set(
      value.rules.map((rule) =>
        JSON.stringify([rule.cidr, rule.ports, rule.allow_http]),
      ),
    ).size === value.rules.length
  );
}
export function getOutboundPolicy(options: ReadOptions = {}) {
  return systemRequest("/system/outbound-policy", options, isOutboundPolicy);
}
export function saveOutboundPolicy(
  base: OutboundPolicy,
  drafts: RuleDraft[],
  options: ReadOptions = {},
) {
  if (
    !isVersion(base.version) ||
    BigInt(base.version) === 9223372036854775807n ||
    Object.keys(ruleIssues(drafts)).length
  )
    throw new AccountError("INVALID_ARGUMENT");
  const rules = drafts.map((draft) => ({
    cidr: canonicalPrivateCidr(draft.cidr)!,
    ports: parsePorts(draft.ports)!,
    allow_http: draft.allow_http,
  }));
  if (new TextEncoder().encode(JSON.stringify(rules)).length > 524288)
    throw new AccountError("INVALID_ARGUMENT");
  return systemCommand(
    "/system/outbound-policy",
    {
      ...options,
      method: "PUT",
      body: JSON.stringify({ expected_version: base.version, rules }),
    },
    (value): value is OutboundReceipt =>
      exact(value, ["version", "rule_count", "audit_id", "created_at"]) &&
      isVersion(value.version) &&
      BigInt(value.version) === BigInt(base.version) + 1n &&
      value.rule_count === String(rules.length) &&
      typeof value.audit_id === "string" &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        value.audit_id,
      ) &&
      isInstant(value.created_at),
  );
}

export interface ProxyGateway {
  protocol: "http" | "https" | "socks5";
  host: string;
  port: number;
  username: string;
  credential_present: boolean;
}
export interface OutboundProxy {
  version: string;
  mode: "direct" | "proxy";
  gateway: ProxyGateway | null;
}
export interface ProxyReceipt {
  version: string;
  audit_id: string;
  created_at: string;
}
export interface ProxyDraft {
  mode: "direct" | "proxy";
  protocol: "http" | "https" | "socks5";
  host: string;
  port: string;
  username: string;
  password: string;
  removeCredential: boolean;
}
const bytes = (value: string) => new TextEncoder().encode(value).length;
const clean = (value: string) =>
  !/\p{Cc}/u.test(value) &&
  new TextDecoder().decode(new TextEncoder().encode(value)) === value;
export function canonicalGatewayHost(value: string): string | undefined {
  if (
    !value ||
    bytes(value) > 253 ||
    !/^[\x21-\x7e]+$/.test(value) ||
    /[/@?#%\\\[\]]/.test(value)
  )
    return;
  if (value.includes(":")) {
    try {
      return new URL(`https://[${value}]/`).hostname.slice(1, -1);
    } catch {
      return;
    }
  }
  if (/^\d+(\.\d+){3}$/.test(value)) {
    if (
      !value
        .split(".")
        .every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
    )
      return;
    return value;
  }
  const host = value.toLowerCase().replace(/\.$/, "");
  const labels = host.split(".");
  if (
    !labels.every((label) =>
      /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label),
    ) ||
    /^(?:\d+|0x[0-9a-f]*)$/.test(labels.at(-1)!)
  )
    return;
  return host;
}
export function proxyDraft(value?: OutboundProxy): ProxyDraft {
  return {
    mode: value?.mode ?? "direct",
    protocol: value?.gateway?.protocol ?? "https",
    host: value?.gateway?.host ?? "",
    port: value?.gateway ? String(value.gateway.port) : "",
    username: value?.gateway?.username ?? "",
    password: "",
    removeCredential: false,
  };
}
export function proxyIssues(
  draft: ProxyDraft,
  base?: OutboundProxy,
): Record<string, string> {
  const issues: Record<string, string> = {};
  const hasGateway =
    draft.mode === "proxy" ||
    !!base?.gateway ||
    !!draft.host ||
    !!draft.port ||
    !!draft.username ||
    !!draft.password;
  if (!["direct", "proxy"].includes(draft.mode))
    issues.mode = "请选择连接方式。";
  if (hasGateway) {
    if (!["http", "https", "socks5"].includes(draft.protocol))
      issues.protocol = "请选择代理协议。";
    if (!canonicalGatewayHost(draft.host))
      issues.host = "填写代理主机名或 IP 地址，不包含协议、路径或用户信息。";
    if (!/^[1-9]\d{0,4}$/.test(draft.port) || Number(draft.port) > 65535)
      issues.port = "请输入 1–65535 的端口。";
    if (
      bytes(draft.username) > 255 ||
      !clean(draft.username) ||
      (draft.protocol !== "socks5" && draft.username.includes(":"))
    )
      issues.username = "用户名不能包含控制字符；HTTP 代理用户名不能包含冒号。";
    if (
      draft.password &&
      (bytes(draft.password) > 255 ||
        /[\0\r\n]/.test(draft.password) ||
        new TextDecoder().decode(new TextEncoder().encode(draft.password)) !==
          draft.password)
    )
      issues.password = "密码最多 255 字节，不能包含空字符或换行。";
    if (draft.removeCredential && (draft.username || draft.password))
      issues.username = "移除身份认证时，请清空用户名和密码。";
    if (
      !!draft.username !==
      !!(
        draft.password ||
        (!draft.removeCredential && base?.gateway?.credential_present)
      )
    )
      issues.password = "用户名与代理密码需要同时配置。";
    const old = base?.gateway;
    if (
      old?.credential_present &&
      !draft.removeCredential &&
      !draft.password &&
      (old.protocol !== draft.protocol ||
        old.host !== canonicalGatewayHost(draft.host) ||
        old.port !== Number(draft.port) ||
        old.username !== draft.username)
    )
      issues.password = "代理地址或用户名已改变，请填写新密码或移除身份认证。";
  }
  return issues;
}
export function isOutboundProxy(value: unknown): value is OutboundProxy {
  if (
    !exact(value, ["version", "mode", "gateway"]) ||
    !isVersion(value.version) ||
    !["direct", "proxy"].includes(String(value.mode))
  )
    return false;
  if (value.gateway === null) return value.mode === "direct";
  const gateway = value.gateway;
  return (
    exact(gateway, [
      "protocol",
      "host",
      "port",
      "username",
      "credential_present",
    ]) &&
    ["http", "https", "socks5"].includes(String(gateway.protocol)) &&
    typeof gateway.host === "string" &&
    canonicalGatewayHost(gateway.host) === gateway.host &&
    Number.isInteger(gateway.port) &&
    Number(gateway.port) > 0 &&
    Number(gateway.port) <= 65535 &&
    typeof gateway.username === "string" &&
    bytes(gateway.username) <= 255 &&
    clean(gateway.username) &&
    (gateway.protocol === "socks5" || !gateway.username.includes(":")) &&
    typeof gateway.credential_present === "boolean" &&
    !!gateway.username === gateway.credential_present
  );
}
export function getOutboundProxy(options: ReadOptions = {}) {
  return systemRequest(
    "/system/outbound-policy/proxy",
    options,
    isOutboundProxy,
  );
}
export function saveOutboundProxy(
  base: OutboundProxy,
  draft: ProxyDraft,
  options: ReadOptions = {},
) {
  if (
    !isVersion(base.version) ||
    BigInt(base.version) === 9223372036854775807n ||
    Object.keys(proxyIssues(draft, base)).length
  )
    throw new AccountError("INVALID_ARGUMENT");
  const gateway = draft.host
    ? {
        protocol: draft.protocol,
        host: canonicalGatewayHost(draft.host)!,
        port: Number(draft.port),
        username: draft.username,
      }
    : null;
  const body = JSON.stringify({
    expected_version: base.version,
    mode: draft.mode,
    gateway,
    credential_action: draft.removeCredential || !gateway ? "remove" : "keep",
    ...(draft.password ? { password: draft.password } : {}),
  });
  return systemCommand(
    "/system/outbound-policy/proxy",
    { ...options, method: "PUT", body },
    (value): value is ProxyReceipt =>
      exact(value, ["version", "audit_id", "created_at"]) &&
      isVersion(value.version) &&
      BigInt(value.version) === BigInt(base.version) + 1n &&
      typeof value.audit_id === "string" &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        value.audit_id,
      ) &&
      isInstant(value.created_at),
  );
}
