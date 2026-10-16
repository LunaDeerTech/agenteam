const {
  test,
  expect,
  api,
  loginHTTP,
  closeSession,
} = require("./owned-fixture.cjs");

test("owned real authorization and CSRF failures leave identity and configuration intact", async ({
  page,
  owned,
}) => {
  // This stays on the certificate/nonce-gated document. These are genuine API
  // authority checks, separate from any synthetic UI 403 presentation evidence.
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const rules = await api(page, "/system/outbound-policy");
  const proxy = await api(page, "/system/outbound-policy/proxy");
  expect(rules.status).toBe(200);
  expect(proxy.status).toBe(200);
  const rejected = await api(
    page,
    "/system/outbound-policy",
    "PUT",
    {
      expected_version: rules.value.version,
      rules: rules.value.rules,
    },
    { omitCSRF: true },
  );
  expect(rejected.status).toBe(403);
  const gateway = proxy.value.gateway;
  const proxyRejected = await api(
    page,
    "/system/outbound-policy/proxy",
    "PUT",
    {
      expected_version: proxy.value.version,
      mode: proxy.value.mode,
      gateway: gateway
        ? {
            protocol: gateway.protocol,
            host: gateway.host,
            port: gateway.port,
            username: gateway.username,
          }
        : null,
      credential_action: gateway ? "keep" : "remove",
    },
    { omitCSRF: true },
  );
  expect(proxyRejected.status).toBe(403);
  expect(
    (await api(page, "/system/outbound-policy")).value.version ===
      rules.value.version,
  ).toBe(true);
  expect(
    (await api(page, "/system/outbound-policy/proxy")).value.version ===
      proxy.value.version,
  ).toBe(true);
  await closeSession(page);
  expect(await loginHTTP(page, owned.regular_credentials)).toBe(200);
  const before = await api(page, "/session");
  expect(before.status).toBe(200);
  expect(before.value.user.role === "user").toBe(true);
  expect((await api(page, "/system/outbound-policy")).status).toBe(403);
  expect((await api(page, "/system/outbound-policy/proxy")).status).toBe(403);
  const after = await api(page, "/session");
  expect(after.status).toBe(200);
  expect(
    before.value.user.id === after.value.user.id &&
      before.value.session.id === after.value.session.id,
  ).toBe(true);
});
