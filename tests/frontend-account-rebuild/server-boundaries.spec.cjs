const { test, expect } = require("./owned-fixture.cjs");

test("real Account API requires a session and distinguishes browser from session CSRF", async ({
  page,
  owned,
}) => {
  // Run against the nonce-verified TLS provider, without application UI state.
  // Return only classifications and booleans, never credentials or CSRF values.
  const observed = await page.evaluate(async (credentials) => {
    async function request(path, method = "GET", body, csrf) {
      const headers = { Accept: "application/json" };
      if (body !== undefined) {
        headers["Content-Type"] = "application/json";
        headers["Idempotency-Key"] = crypto.randomUUID();
      }
      if (csrf) headers["X-CSRF-Token"] = csrf;
      const response = await fetch(`/api/v1${path}`, {
        method,
        headers,
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const value = response.status === 204 ? undefined : await response.json();
      return { status: response.status, value };
    }
    const anonymous = [];
    for (const path of ["/me", "/me/preferences", "/me/avatar"])
      anonymous.push((await request(path)).status);
    const bootstrap = await request("/auth/bootstrap");
    const rejectedLogin = await request("/sessions/login", "POST", credentials);
    const login = await request(
      "/sessions/login",
      "POST",
      credentials,
      bootstrap.value.csrf_token,
    );
    const session = await request("/session");
    const before = await request("/me");
    const edit = {
      version: before.value.user.version,
      display_name: "must not be saved",
    };
    const missing = await request("/me", "PATCH", edit);
    const wrongContext = await request(
      "/me",
      "PATCH",
      edit,
      bootstrap.value.csrf_token,
    );
    const after = await request("/me");
    const logout = await request(
      "/sessions/logout",
      "POST",
      {},
      session.value.csrf_token,
    );
    const ended = await request("/me");
    return {
      anonymous,
      bootstrap: bootstrap.status,
      loginWithoutCSRF: rejectedLogin.status,
      login: login.status,
      session: session.status,
      profile: before.status,
      missing: { status: missing.status, code: missing.value.code },
      wrongContext: {
        status: wrongContext.status,
        code: wrongContext.value.code,
      },
      unchanged:
        before.value.user.version === after.value.user.version &&
        before.value.user.display_name === after.value.user.display_name,
      logout: logout.status,
      afterLogout: ended.status,
    };
  }, owned.credentials);
  expect(observed).toEqual({
    anonymous: [401, 401, 401],
    bootstrap: 200,
    loginWithoutCSRF: 403,
    login: 200,
    session: 200,
    profile: 200,
    missing: { status: 403, code: "CSRF_FAILED" },
    wrongContext: { status: 403, code: "CSRF_FAILED" },
    unchanged: true,
    logout: 204,
    afterLogout: 401,
  });
});
