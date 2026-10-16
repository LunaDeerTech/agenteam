const { randomBytes } = require("node:crypto");
const {
  test,
  expect,
  api,
  login,
  fillPrivate,
  select,
  safeForward,
} = require("./owned-fixture.cjs");
const endpoint = "/system/outbound-policy/proxy";
const routePattern = "**/api/v1/system/outbound-policy/proxy";

async function saveProxy(page, receipts) {
  const next = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/v1${endpoint}` &&
      response.request().method() === "PUT",
  );
  await page.getByRole("button", { name: "保存代理设置", exact: true }).click();
  const response = await next;
  expect(response.status()).toBe(200);
  const receipt = receipts.get(response.request().headers()["idempotency-key"]);
  if (!receipt) throw new Error("PROXY_FORWARDED_RECEIPT_UNAVAILABLE");
  expect(
    Object.keys(receipt).sort().join() === "audit_id,created_at,version",
  ).toBe(true);
  await expect(
    page.getByRole("status").filter({ hasText: "代理设置已保存。" }),
  ).toBeVisible();
  await expect
    .poll(
      async () =>
        (await page.getByLabel("密码", { exact: true }).inputValue()) === "",
    )
    .toBe(true);
  return receipt;
}
function safeProjection(value) {
  return (
    Object.keys(value).sort().join() === "gateway,mode,version" &&
    value.gateway &&
    Object.keys(value.gateway).sort().join() ===
      "credential_present,host,port,protocol,username"
  );
}

test("owned proxy keeps, replaces and removes credentials and recovers only its accepted original write", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  if (process.env.NODE_EXTRA_CA_CERTS !== owned.certificate_ca_file)
    throw new Error("OWNED_OUTBOUND_NODE_CA_REQUIRED");
  const gateway = owned.proxy_gateway;
  if (
    !gateway ||
    !["http", "https", "socks5"].includes(gateway.protocol) ||
    typeof gateway.host !== "string" ||
    !gateway.host ||
    !Number.isInteger(gateway.port)
  )
    throw new Error("OWNED_OUTBOUND_PROXY_GATEWAY_REQUIRED");
  const secrets = [0, 1, 2].map(() => randomBytes(24).toString("base64url"));
  const puts = [];
  page.on("request", (request) => {
    if (
      new URL(request.url()).pathname === `/api/v1${endpoint}` &&
      request.method() === "PUT"
    )
      puts.push({
        key: request.headers()["idempotency-key"],
        body: request.postData(),
      });
  });
  await login(page, owned);
  // Capture the genuine server receipt before forwarding its unchanged body.
  // Chromium's later response-body retrieval failed in run-06 despite a 200
  // and the UI's following GET; never replace this with a fabricated receipt.
  const receipts = new Map();
  await page.route(
    routePattern,
    safeForward(async (route) => {
      if (route.request().method() !== "PUT") return route.continue();
      const response = await route.fetch();
      if (response.status() === 200)
        receipts.set(
          route.request().headers()["idempotency-key"],
          await response.json(),
        );
      await route.fulfill({ response });
    }),
  );
  await page.getByRole("tab", { name: "代理设置", exact: true }).click();
  await expect(
    page.getByRole("combobox", { name: "连接方式", exact: true }),
  ).toBeEnabled();
  await select(page, "连接方式", "使用代理");
  await select(
    page,
    "代理协议",
    { http: "HTTP CONNECT", https: "HTTPS CONNECT", socks5: "SOCKS5" }[
      gateway.protocol
    ],
  );
  await fillPrivate(page.getByLabel("代理主机", { exact: true }), gateway.host);
  await page.getByLabel("代理端口", { exact: true }).fill(String(gateway.port));
  await page.getByLabel("用户名", { exact: true }).fill("owned-browser");
  await fillPrivate(page.getByLabel("密码", { exact: true }), secrets[0]);
  const first = await saveProxy(page, receipts);
  const stored = await api(page, endpoint);
  expect(stored.status).toBe(200);
  expect(
    safeProjection(stored.value) &&
      stored.value.gateway.credential_present &&
      stored.value.version === first.version,
  ).toBe(true);
  expect(
    secrets.every((secret) => !JSON.stringify(stored.value).includes(secret)),
  ).toBe(true);
  await page.reload();
  await page.getByRole("tab", { name: "代理设置", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await page.getByLabel("密码", { exact: true }).inputValue()) === "",
    )
    .toBe(true);

  // Changing mode with an empty password must keep the gateway/credential.
  await select(page, "连接方式", "直连");
  await saveProxy(page, receipts);
  const kept = await api(page, endpoint);
  const keepBody = JSON.parse(puts.at(-1).body);
  expect(
    kept.value.mode === "direct" &&
      kept.value.gateway.credential_present &&
      keepBody.credential_action === "keep" &&
      !Object.hasOwn(keepBody, "password"),
  ).toBe(true);
  await fillPrivate(page.getByLabel("密码", { exact: true }), secrets[1]);
  await saveProxy(page, receipts);
  const replaceBody = JSON.parse(puts.at(-1).body);
  expect(
    replaceBody.credential_action === "keep" &&
      replaceBody.password === secrets[1],
  ).toBe(true);

  // Real backend accepts the first write; only its browser response is lost.
  // The subsequent real same-key replay returns the original receipt. Its
  // following GET is independently dropped to distinguish confirmation/read.
  const attempts = [];
  let committedVersion;
  let loseNextRead = false;
  const recoveryHandler = safeForward(async (route) => {
    if (route.request().method() === "PUT") {
      attempts.push({
        key: route.request().headers()["idempotency-key"],
        body: route.request().postData(),
      });
      const response = await route.fetch();
      if (response.status() !== 200) throw new Error();
      const receipt = await response.json();
      if (attempts.length === 1) {
        committedVersion = receipt.version;
        await route.abort("connectionfailed");
      } else {
        loseNextRead = true;
        await route.fulfill({ response });
      }
    } else if (loseNextRead && route.request().method() === "GET") {
      loseNextRead = false;
      await route.abort("connectionfailed");
    } else await route.continue();
  });
  await page.route(routePattern, recoveryHandler);
  await select(page, "连接方式", "使用代理");
  await fillPrivate(page.getByLabel("密码", { exact: true }), secrets[2]);
  await page.getByRole("button", { name: "保存代理设置", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("原保存结果尚未确认");
  await expect(
    page.getByRole("tab", { name: "访问规则", exact: true }),
  ).toBeDisabled();
  await expect
    .poll(
      async () =>
        (await page.getByLabel("密码", { exact: true }).inputValue()) === "",
    )
    .toBe(true);
  expect(attempts.length).toBe(1);
  await page.getByRole("button", { name: "检查当前状态", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  expect(attempts.length).toBe(1);
  await page.getByRole("button", { name: "重试原请求", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "保存已确认，当前配置读取失败",
  );
  expect(attempts.length).toBe(2);
  expect(
    attempts[0].key === attempts[1].key &&
      attempts[0].body === attempts[1].body &&
      JSON.parse(attempts[1].body).password === secrets[2],
  ).toBe(true);
  await page.getByRole("button", { name: "重新读取配置", exact: true }).click();
  await expect(
    page.getByRole("combobox", { name: "连接方式", exact: true }),
  ).toBeEnabled();
  await expect
    .poll(
      async () =>
        (await page.getByLabel("密码", { exact: true }).inputValue()) === "",
    )
    .toBe(true);
  expect(attempts.length).toBe(2);
  await page.unroute(routePattern, recoveryHandler);
  const recovered = await api(page, endpoint);
  expect(
    recovered.value.version === committedVersion &&
      recovered.value.gateway.credential_present &&
      safeProjection(recovered.value),
  ).toBe(true);

  await page.getByRole("button", { name: "移除身份认证", exact: true }).click();
  await page
    .getByRole("dialog", { name: "移除代理身份认证", exact: true })
    .getByRole("button", { name: "移除身份认证", exact: true })
    .click();
  await select(page, "连接方式", "直连");
  await saveProxy(page, receipts);
  const removed = await api(page, endpoint);
  const removeBody = JSON.parse(puts.at(-1).body);
  expect(
    removed.value.mode === "direct" &&
      !removed.value.gateway.credential_present &&
      removed.value.gateway.username === "" &&
      removeBody.credential_action === "remove" &&
      !Object.hasOwn(removeBody, "password"),
  ).toBe(true);
});
