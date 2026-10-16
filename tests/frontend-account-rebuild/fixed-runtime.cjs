// Local routing for the one task-owned r2 origin. This never tests public frp.
// Keep the standalone cookie-race probe unchanged; share only this fixture's
// narrow routing/certificate adapter with new Vue browser cases.
const fs = require("node:fs");
const http = require("node:http");
const net = require("node:net");
const { createHash, X509Certificate } = require("node:crypto");

const publicOrigin = "https://154.12.86.233:18443";
const publicTarget = "154.12.86.233:18443";
const certificateSPKI = (certificate) =>
  createHash("sha256")
    .update(certificate.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");

function validatePublicCertificate(runtime, leaf) {
  try {
    const ca = new X509Certificate(
      fs.readFileSync(runtime.certificate_ca_file),
    );
    if (
      runtime.origin !== publicOrigin ||
      !Number.isFinite(Date.parse(runtime.expires_at)) ||
      Date.parse(runtime.expires_at) <= Date.now() ||
      !leaf.checkIP("154.12.86.233") ||
      !ca.ca ||
      !leaf.checkIssued(ca) ||
      !leaf.verify(ca.publicKey) ||
      Date.parse(ca.validFrom) > Date.now() ||
      Date.parse(ca.validTo) <= Date.now()
    )
      throw new Error();
  } catch {
    throw new Error("OWNED_PUBLIC_CERTIFICATE_OR_EXPIRY_INVALID");
  }
}

async function startFixedRelay() {
  const sockets = new Set();
  const server = http.createServer((_request, response) => {
    response.writeHead(403);
    response.end();
  });
  server.on("connection", (socket) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
    socket.on("error", () => socket.destroy());
    socket.setTimeout(30000, () => socket.destroy());
  });
  server.on("connect", (request, client, head) => {
    if (request.url !== publicTarget) {
      client.end("HTTP/1.1 403 Forbidden\r\n\r\n");
      return;
    }
    const upstream = net.connect({ host: "127.0.0.1", port: 5254 });
    sockets.add(upstream);
    upstream.on("close", () => {
      sockets.delete(upstream);
      client.destroy();
    });
    client.on("close", () => upstream.destroy());
    upstream.on("error", () => {
      client.destroy();
      upstream.destroy();
    });
    upstream.once("connect", () => {
      client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head.length) upstream.write(head);
      client.pipe(upstream);
      upstream.pipe(client);
    });
  });
  await new Promise((resolve, reject) => {
    server.once("error", () => reject(new Error("OWNED_RELAY_START_FAILED")));
    server.listen(0, "127.0.0.1", resolve);
  });
  return {
    proxy: { server: `http://127.0.0.1:${server.address().port}` },
    async close() {
      for (const socket of sockets) socket.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

// Enable before navigation. Missing certificate events fail closed before any
// credentials, even when a locally read certificate matches the manifest pin.
async function observeOwnedCertificate(page, runtime) {
  const cdp = await page.context().newCDPSession(page);
  let observed;
  cdp.on("Security.visibleSecurityStateChanged", (event) => {
    observed =
      event.visibleSecurityState?.certificateSecurityState?.certificate;
  });
  await cdp.send("Network.enable");
  await cdp.send("Security.enable");
  return {
    verify() {
      let matches = false;
      try {
        matches =
          !!observed?.length &&
          certificateSPKI(
            new X509Certificate(Buffer.from(observed[0], "base64")),
          ) === runtime.certificate_spki;
      } catch {
        // Do not include the certificate or CDP payload in failure output.
      }
      if (matches !== true)
        throw new Error("OWNED_BROWSER_CERTIFICATE_MISMATCH");
    },
    close: () => cdp.detach(),
  };
}

module.exports = {
  publicOrigin,
  validatePublicCertificate,
  startFixedRelay,
  observeOwnedCertificate,
};
