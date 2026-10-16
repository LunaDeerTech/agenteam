const http = require("node:http");

function privateControl(runtime) {
  let url;
  try {
    url = new URL(runtime.smtp_control);
  } catch {
    throw new Error("OWNED_SMTP_CONTROL_REQUIRED");
  }
  const address = url.hostname.split(".").map(Number);
  const privateIPv4 =
    address.length === 4 &&
    address.every(
      (part) => Number.isInteger(part) && part >= 0 && part <= 255,
    ) &&
    (address[0] === 10 ||
      (address[0] === 172 && address[1] >= 16 && address[1] <= 31) ||
      (address[0] === 192 && address[1] === 168));
  if (
    !privateIPv4 ||
    url.protocol !== "http:" ||
    url.port !== "9000" ||
    url.pathname !== "/" ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    !/^[a-f0-9]{32}$/.test(runtime.smtp_nonce || "") ||
    !/^[a-f0-9]{32}$/.test(runtime.smtp_endpoint_id || "")
  )
    throw new Error("OWNED_SMTP_CONTROL_INVALID");
  return url;
}

// Test-only private control channel. Never call this through product fetch or
// record the returned MIME, URL or token in a reporter, screenshot or trace.
async function readLastMail(runtime) {
  const url = privateControl(runtime);
  url.pathname = `/message/${runtime.smtp_endpoint_id}`;
  return new Promise((resolve, reject) => {
    const request = http.get(
      url,
      { headers: { "X-Fixture-Nonce": runtime.smtp_nonce }, timeout: 5000 },
      (response) => {
        if (response.statusCode === 404) {
          response.resume();
          resolve(undefined);
          return;
        }
        if (
          response.statusCode !== 200 ||
          response.headers["content-type"]?.split(";")[0] !== "message/rfc822"
        ) {
          response.resume();
          reject(new Error("OWNED_SMTP_MESSAGE_UNAVAILABLE"));
          return;
        }
        const chunks = [];
        let size = 0;
        response.on("data", (chunk) => {
          size += chunk.length;
          if (size > 65536) {
            request.destroy();
            reject(new Error("OWNED_SMTP_MESSAGE_LIMIT"));
          } else chunks.push(chunk);
        });
        response.on("end", () =>
          resolve(Buffer.concat(chunks).toString("utf8")),
        );
        response.on("error", () =>
          reject(new Error("OWNED_SMTP_MESSAGE_READ_FAILED")),
        );
      },
    );
    request.on("timeout", () => request.destroy());
    request.on("error", () =>
      reject(new Error("OWNED_SMTP_MESSAGE_READ_FAILED")),
    );
  });
}

function accountLinkFromMail(raw, { origin, recipient, purpose }) {
  if (!raw) return undefined;
  const separator = raw.indexOf("\r\n\r\n");
  if (separator < 0) throw new Error("OWNED_MAIL_FORMAT_INVALID");
  const headers = raw.slice(0, separator).replace(/\r\n[ \t]+/g, " ");
  const to = headers.match(/^To:\s*(.+)$/im)?.[1];
  if (!to || !(to === recipient || to.endsWith(`<${recipient}>`)))
    return undefined;
  let body = raw.slice(separator + 4);
  if (/^Content-Transfer-Encoding:\s*base64\s*$/im.test(headers))
    body = Buffer.from(body.replace(/\s/g, ""), "base64").toString("utf8");
  else if (
    /^Content-Transfer-Encoding:\s*quoted-printable\s*$/im.test(headers)
  ) {
    body = body
      .replace(/=\r\n/g, "")
      .replace(/=([A-Fa-f0-9]{2})/g, (_match, value) =>
        String.fromCharCode(parseInt(value, 16)),
      );
  }
  if (purpose !== "invite" && purpose !== "reset-password")
    throw new Error("OWNED_MAIL_PURPOSE_INVALID");
  const links = body.match(/https:\/\/[^\s<>"']+/g) || [];
  for (const value of links) {
    let url;
    try {
      url = new URL(value);
    } catch {
      continue;
    }
    if (url.origin !== origin || url.pathname !== `/${purpose}` || url.search)
      continue;
    if (
      !/^#[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}$/.test(
        url.hash,
      )
    )
      continue;
    return url.href;
  }
  return undefined;
}

module.exports = { readLastMail, accountLinkFromMail };
