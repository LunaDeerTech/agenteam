import { Blob as NodeBlob } from "node:buffer";
import { mount } from "@vue/test-utils";
import { h } from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const user = {
  id: "019a2222-2222-7222-8222-222222222222",
  email: "synthetic@example.invalid",
  username: "synthetic",
  display_name: "Synthetic",
  role: "admin",
  theme: "system",
  version: "1",
  initial_password_suggestion: false,
};
const identity = {
  user,
  session: {
    id: "019a1111-1111-7111-8111-111111111111",
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "synthetic-context",
};
const configured = {
  version: "9",
  mode: "proxy" as const,
  gateway: {
    protocol: "https" as const,
    host: "proxy.example.invalid",
    port: 8443,
    username: "synthetic",
    credential_present: true,
  },
};
const receipt = {
  version: "10",
  audit_id: "019a3333-3333-7333-8333-333333333333",
  created_at: "2026-10-16T12:00:00.000001Z",
};
const policy = {
  version: "9",
  rule_mode: "allowlist" as const,
  rules: [{ target: "10.0.0.0/8", ports: [443] }],
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status < 400 ? "application/json" : "application/problem+json",
    },
  });
let dispose: (() => void) | undefined;
let theme: (() => void) | undefined;
beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  localStorage.clear();
  theme = (await import("../src/composables/useTheme")).initTheme();
});
afterEach(() => {
  dispose?.();
  dispose = undefined;
  theme?.();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

it("canonicalizes IP and DNS targets while requiring canonical CIDRs and unambiguous wildcard syntax", async () => {
  const { canonicalRuleTarget } = await import("../src/system/outbound-policy");
  for (const [input, expected] of [
    ["8.8.8.8", "8.8.8.8/32"],
    ["10.1.2.3", "10.1.2.3/32"],
    ["127.0.0.1", "127.0.0.1/32"],
    ["169.254.169.254", "169.254.169.254/32"],
    ["::1", "::1/128"],
    ["2001:DB8:0000::1", "2001:db8::1/128"],
    ["FD12:0:0:0:0:0:0:1", "fd12::1/128"],
    ["8.8.8.0/24", "8.8.8.0/24"],
    ["10.0.0.0/8", "10.0.0.0/8"],
    ["2001:db8::/32", "2001:db8::/32"],
    ["fd12::/64", "fd12::/64"],
    ["0.0.0.0/0", "0.0.0.0/0"],
    ["::/0", "::/0"],
    ["API.Example.Invalid.", "api.example.invalid"],
    ["*.Example.Invalid.", "*.example.invalid"],
  ] as const)
    expect(canonicalRuleTarget(input), input).toBe(expected);
  for (const input of [
    "10.1.0.0/8",
    "8.8.8.1/24",
    "fd12::1/64",
    "2001:DB8::/32",
    "2001:0db8::/32",
    "0.0.0.0/00",
    "10.0.0.0/33",
    "::/129",
    "10.00.0.1",
    "256.0.0.1",
    "fd00:::",
    "::ffff:10.0.0.1",
    "::ffff:a00:1/128",
    "fe80::1%eth0",
    "[2001:db8::1]",
    "https://example.invalid",
    "example.invalid/path",
    "example.invalid:443",
    "user@example.invalid",
    "例子.invalid",
    "bad_label.invalid",
    "example..invalid",
    "example.invalid..",
    "-example.invalid",
    `${"a".repeat(64)}.invalid`,
    "*",
    "**.example.invalid",
    "api.*.example.invalid",
    "*.*.example.invalid",
    "*.8.8.8.8",
  ])
    expect(canonicalRuleTarget(input), input).toBeUndefined();
});

it("requires an explicit rule mode, validates port bounds and detects normalized duplicates", async () => {
  const { parsePorts, ruleIssues, ruleDrafts } =
    await import("../src/system/outbound-policy");
  expect(ruleDrafts()).toEqual({ ruleMode: "", rules: [] });
  expect(ruleIssues(ruleDrafts()).rule_mode).toBeDefined();
  for (const ruleMode of ["allowlist", "denylist"] as const)
    expect(ruleIssues({ ruleMode, rules: [] })).toEqual({});
  const draft = ruleDrafts(policy);
  expect(draft).toEqual({
    ruleMode: "allowlist",
    rules: [{ target: "10.0.0.0/8", ports: "443" }],
  });
  draft.rules[0]!.target = "192.168.0.0/16";
  expect(policy.rules[0]!.target).toBe("10.0.0.0/8");
  expect(parsePorts("all")).toBe("all");
  expect(parsePorts("587, 443")).toEqual([443, 587]);
  const ports = Array.from({ length: 256 }, (_, index) => index + 1);
  expect(parsePorts(ports.join(","))).toEqual(ports);
  expect(parsePorts([...ports, 257].join(","))).toBeUndefined();
  for (const value of ["0", "443,443", "1e3", "65536", "1-5", ""])
    expect(parsePorts(value)).toBeUndefined();
  expect(
    ruleIssues({
      ruleMode: "allowlist",
      rules: [
        { target: "10.0.0.1", ports: "587, 443" },
        { target: "10.0.0.1/32", ports: "443, 587" },
      ],
    })["1.target"],
  ).toBeDefined();
});

it("accepts both strict policy modes and exactly 512 rules while rejecting legacy and malformed DTOs", async () => {
  const { isOutboundPolicy, ruleDrafts, ruleIssues } =
    await import("../src/system/outbound-policy");
  for (const rule_mode of ["allowlist", "denylist"])
    expect(isOutboundPolicy({ ...policy, rule_mode, rules: [] })).toBe(true);
  const rules = Array.from({ length: 512 }, (_, index) => ({
    target: `10.${Math.floor(index / 256)}.${index % 256}.0/24`,
    ports: "all" as const,
  }));
  const largest = { ...policy, rules };
  expect(isOutboundPolicy(largest)).toBe(true);
  expect(ruleIssues(ruleDrafts(largest))).toEqual({});
  const tooMany = {
    ...policy,
    rules: [...rules, { target: "192.168.0.0/16", ports: "all" as const }],
  };
  expect(isOutboundPolicy(tooMany)).toBe(false);
  expect(ruleIssues(ruleDrafts(tooMany)).rules).toBeDefined();
  for (const value of [
    { version: policy.version, rules: policy.rules },
    { ...policy, rule_mode: "" },
    { ...policy, rule_mode: "proxy" },
    { ...policy, allow_http: false },
    {
      ...policy,
      rules: [{ ...policy.rules[0], allow_http: false }],
    },
    { ...policy, rules: [{ cidr: "10.0.0.0/8", ports: [443] }] },
    { ...policy, rules: [{ target: "10.1.0.0/8", ports: [443] }] },
    { ...policy, rules: [{ target: "10.0.0.1", ports: [443] }] },
    { ...policy, rules: [{ target: "API.example.invalid", ports: [443] }] },
    { ...policy, rules: [{ target: "10.0.0.0/8", ports: [587, 443] }] },
    { ...policy, rules: [{ target: "10.0.0.0/8", ports: [443, 443] }] },
    { ...policy, rules: [{ target: "10.0.0.0/8", ports: [] }] },
    { ...policy, rules: [...policy.rules, ...policy.rules] },
  ])
    expect(isOutboundPolicy(value)).toBe(false);
});

it("does not accept secret projections or reuse an old credential at another gateway", async () => {
  const { proxyDraft, proxyIssues, isOutboundProxy, canonicalGatewayHost } =
    await import("../src/system/outbound-policy");
  expect(isOutboundProxy(configured)).toBe(true);
  expect(canonicalGatewayHost("Corporate-Gateway.")).toBe("corporate-gateway");
  expect(canonicalGatewayHost("FD00:0:0:0:0:0:0:1")).toBe("fd00::1");
  for (const host of [
    "https://proxy.example.invalid",
    "proxy:8080",
    "user@proxy",
    "127.1",
    "01.2.3.4",
    "proxy.0x7f",
    "代理.example",
    "[fd00::1]",
  ])
    expect(canonicalGatewayHost(host)).toBeUndefined();
  expect(
    isOutboundProxy({
      ...configured,
      gateway: { ...configured.gateway, password: "not-a-read-field" },
    }),
  ).toBe(false);
  const draft = proxyDraft(configured);
  draft.host = "other.example.invalid";
  draft.mode = "direct";
  expect(proxyIssues(draft, configured).password).toBeDefined();
  draft.password = "synthetic replacement";
  expect(proxyIssues(draft, configured)).toEqual({});
  draft.password = "";
  draft.removeCredential = true;
  draft.username = "";
  expect(proxyIssues(draft, configured)).toEqual({});
});

it("allows a legitimate large rules write only at the exact rules endpoint, and rejects above its bound", async () => {
  const saved = { ...receipt, rule_count: "512" };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(saved));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const { accountRequest } = await import("../src/account/transport");
  const { ruleDrafts, saveOutboundPolicy } =
    await import("../src/system/outbound-policy");
  const largest = {
    ...policy,
    rules: Array.from({ length: 512 }, (_, index) => ({
      target: `10.${Math.floor(index / 256)}.${index % 256}.0/24`,
      ports: Array.from({ length: 256 }, (_, port) => 40000 + port),
    })),
  };
  const body = JSON.stringify({
    expected_version: policy.version,
    rule_mode: largest.rule_mode,
    rules: largest.rules,
  });
  expect(body.length).toBeGreaterThan(524288);
  expect(body.length).toBeLessThanOrEqual(1048576);
  const command = saveOutboundPolicy(policy, ruleDrafts(largest));
  try {
    await expect(command.run()).resolves.toEqual(saved);
  } finally {
    command.release();
  }
  expect(fetcher.mock.calls[1]![0]).toBe("/api/v1/system/outbound-policy");
  expect(JSON.parse(fetcher.mock.calls[1]![1].body)).toEqual(JSON.parse(body));
  await expect(
    accountRequest("/system/outbound-policy", {
      method: "PUT",
      body: " ".repeat(1048577),
    }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  await expect(
    accountRequest("/system/outbound-policy/proxy", { method: "PUT", body }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  expect(fetcher).toHaveBeenCalledTimes(2);
});

async function mountRuleEditor() {
  const api = await import("../src/system/outbound-policy");
  const { useOutboundDraft } = await import("../src/system/useOutboundDraft");
  let state!: ReturnType<
    typeof useOutboundDraft<
      import("../src/system/outbound-policy").OutboundPolicy,
      import("../src/system/outbound-policy").PolicyDraft,
      import("../src/system/outbound-policy").OutboundReceipt
    >
  >;
  const wrapper = mount({
    setup() {
      state = useOutboundDraft({
        read: api.getOutboundPolicy,
        draft: api.ruleDrafts,
        write: api.saveOutboundPolicy,
      });
      return () => h("div");
    },
  });
  dispose = () => wrapper.unmount();
  return state;
}

it("replays the original mode and rules together after an unknown, then recovers a failed confirmation read without another write", async () => {
  const saved = { ...receipt, rule_count: "1" };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(policy))
    .mockRejectedValueOnce(new Error("synthetic lost response"))
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(saved))
    .mockRejectedValueOnce(new Error("synthetic read failure"));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const state = await mountRuleEditor();
  await state.load();
  state.draft.value.ruleMode = "denylist";
  expect(state.dirty.value).toBe(true);
  expect(state.draft.value.rules).toEqual([
    { target: "10.0.0.0/8", ports: "443" },
  ]);
  state.draft.value.rules = [{ target: "192.168.1.7", ports: "587, 443" }];
  await state.submit();
  expect(state.command.uncertain.value).toBe(true);
  expect(state.locked.value).toBe(true);
  expect(state.draft.value).toEqual({
    ruleMode: "denylist",
    rules: [{ target: "192.168.1.7", ports: "587, 443" }],
  });
  // The pending command owns immutable bytes even if its source draft changes.
  state.draft.value.ruleMode = "allowlist";
  state.draft.value.rules[0]!.target = "10.0.0.0/8";
  await state.submit();
  expect(fetcher).toHaveBeenCalledTimes(3);
  await state.command.verify();
  await state.command.retry();
  expect(state.command.confirmed.value).toBe(true);
  expect(state.command.uncertain.value).toBe(false);
  expect(state.readError.value).toBeDefined();
  expect(state.baseline.value).toBeUndefined();
  expect(state.draft.value).toEqual({ ruleMode: "", rules: [] });
  expect(state.locked.value).toBe(true);
  const writes = fetcher.mock.calls.filter(
    ([, options]) => options.method === "PUT",
  );
  expect(writes).toHaveLength(2);
  expect(JSON.parse(writes[0]![1].body)).toEqual({
    expected_version: "9",
    rule_mode: "denylist",
    rules: [{ target: "192.168.1.7/32", ports: [443, 587] }],
  });
  expect(writes[0]![1].body).toBe(writes[1]![1].body);
  const key = writes[0]![1].headers.get("Idempotency-Key");
  expect(key).toBeTruthy();
  expect(writes[1]![1].headers.get("Idempotency-Key")).toBe(key);
  await state.command.retry();
  await state.submit();
  expect(fetcher).toHaveBeenCalledTimes(6);
  fetcher.mockResolvedValueOnce(
    json({
      version: "10",
      rule_mode: "denylist",
      rules: [{ target: "192.168.1.7/32", ports: [443, 587] }],
    }),
  );
  await state.load();
  expect(state.baseline.value?.version).toBe("10");
  expect(state.draft.value).toEqual({
    ruleMode: "denylist",
    rules: [{ target: "192.168.1.7/32", ports: "443, 587" }],
  });
  expect(state.readError.value).toBeUndefined();
  expect(state.dirty.value).toBe(false);
  expect(
    fetcher.mock.calls.filter(([, options]) => options.method === "PUT"),
  ).toHaveLength(2);
});

it("keeps a conflicting mode and rules draft until the user explicitly adopts the latest policy", async () => {
  const latest = {
    version: "10",
    rule_mode: "allowlist",
    rules: [{ target: "172.16.0.0/12", ports: "all" }],
  };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(policy))
    .mockResolvedValueOnce(
      json({ code: "VERSION_CONFLICT", commit_state: "not_committed" }, 409),
    )
    .mockResolvedValueOnce(json(latest));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const state = await mountRuleEditor();
  await state.load();
  const draft = {
    ruleMode: "denylist" as const,
    rules: [{ target: "192.168.0.0/16", ports: "443" }],
  };
  state.draft.value = {
    ruleMode: draft.ruleMode,
    rules: draft.rules.map((rule) => ({ ...rule })),
  };
  await state.submit();
  expect(state.conflict.value).toBe(true);
  expect(state.command.uncertain.value).toBe(false);
  expect(state.draft.value).toEqual(draft);
  await state.load();
  expect(state.latest.value).toEqual(latest);
  expect(state.baseline.value).toEqual(policy);
  expect(state.draft.value).toEqual(draft);
  expect(state.changed.value).toBe(true);
  expect(state.dirty.value).toBe(true);
  await state.submit();
  expect(
    fetcher.mock.calls.filter(([, options]) => options.method === "PUT"),
  ).toHaveLength(1);
  state.adoptLatest();
  expect(state.baseline.value).toEqual(latest);
  expect(state.draft.value).toEqual({
    ruleMode: "allowlist",
    rules: [{ target: "172.16.0.0/12", ports: "all" }],
  });
  expect(state.conflict.value).toBe(false);
  expect(state.dirty.value).toBe(false);
});

it("retains a private original proxy intent across an unknown and never rewrites a confirmed command after a failed read", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(configured))
    .mockRejectedValueOnce(new Error("synthetic lost response"))
    .mockResolvedValueOnce(json(identity))
    .mockResolvedValueOnce(json(receipt))
    .mockRejectedValueOnce(new Error("synthetic read failure"));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const api = await import("../src/system/outbound-policy");
  const { useOutboundDraft } = await import("../src/system/useOutboundDraft");
  let state!: ReturnType<
    typeof useOutboundDraft<
      import("../src/system/outbound-policy").OutboundProxy,
      import("../src/system/outbound-policy").ProxyDraft,
      import("../src/system/outbound-policy").ProxyReceipt
    >
  >;
  const wrapper = mount({
    setup() {
      state = useOutboundDraft({
        read: api.getOutboundProxy,
        draft: api.proxyDraft,
        write: api.saveOutboundProxy,
        clearSecret: (draft) => {
          draft.password = "";
        },
      });
      return () => h("div");
    },
  });
  dispose = () => wrapper.unmount();
  await state.load();
  state.draft.value.password = "synthetic-only replacement";
  await state.submit();
  expect(state.command.uncertain.value).toBe(true);
  expect(state.draft.value.password).toBe("");
  expect(state.locked.value).toBe(true);
  await state.command.verify();
  await state.command.retry();
  expect(state.command.confirmed.value).toBe(true);
  expect(state.command.uncertain.value).toBe(false);
  expect(state.readError.value).toBeDefined();
  const writes = fetcher.mock.calls.filter(
    ([, options]) => options.method === "PUT",
  );
  expect(writes).toHaveLength(2);
  expect(writes[0]![1].body).toBe(writes[1]![1].body);
  expect(writes[0]![1].headers.get("Idempotency-Key")).toBe(
    writes[1]![1].headers.get("Idempotency-Key"),
  );
  fetcher.mockResolvedValueOnce(json({ ...configured, version: "10" }));
  await state.load();
  expect(
    fetcher.mock.calls.filter(([, options]) => options.method === "PUT"),
  ).toHaveLength(2);
  expect(state.baseline.value?.version).toBe("10");
});
