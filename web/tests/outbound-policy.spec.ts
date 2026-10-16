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

it("normalizes private network input and rejects public, mapped and ambiguous rule values", async () => {
  const { canonicalPrivateCidr, parsePorts, ruleIssues, isOutboundPolicy } =
    await import("../src/system/outbound-policy");
  expect(canonicalPrivateCidr("10.2.3.4/8")).toBe("10.0.0.0/8");
  expect(canonicalPrivateCidr("FD12:0000:0000:0000:ABCD::1/64")).toBe(
    "fd12::/64",
  );
  for (const value of [
    "127.0.0.1/32",
    "10.0.0.0/7",
    "172.15.0.0/16",
    "192.168.0.0/15",
    "::ffff:10.0.0.1/128",
    "fd00:::/64",
    "10.00.0.1/32",
  ])
    expect(canonicalPrivateCidr(value)).toBeUndefined();
  expect(parsePorts("587, 443")).toEqual([443, 587]);
  for (const value of ["0", "443,443", "1e3", "65536", "1-5", ""])
    expect(parsePorts(value)).toBeUndefined();
  expect(
    ruleIssues([
      { cidr: "10.1.0.0/8", ports: "443", allow_http: false },
      { cidr: "10.0.0.0/8", ports: "443", allow_http: false },
    ])["1.cidr"],
  ).toBeDefined();
  expect(
    isOutboundPolicy({
      version: "1",
      rules: [{ cidr: "10.0.0.0/8", ports: [587, 443], allow_http: false }],
    }),
  ).toBe(false);
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
  const fetcher = vi.fn().mockImplementation(async () => json({ saved: true }));
  vi.stubGlobal("fetch", fetcher);
  const { accountRequest } = await import("../src/account/transport");
  const body = JSON.stringify({
    expected_version: "1",
    rules: Array.from({ length: 256 }, (_, index) => ({
      cidr: `10.${index}.0.0/16`,
      ports: Array.from({ length: 256 }, (_, port) => 40000 + port),
      allow_http: false,
    })),
  });
  expect(body.length).toBeGreaterThan(16384);
  await expect(
    accountRequest("/system/outbound-policy", { method: "PUT", body }),
  ).resolves.toEqual({ saved: true });
  await expect(
    accountRequest("/system/outbound-policy", {
      method: "PUT",
      body: " ".repeat(1048577),
    }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  await expect(
    accountRequest("/system/outbound-policy/proxy", { method: "PUT", body }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  expect(fetcher).toHaveBeenCalledTimes(1);
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
