import { afterEach, describe, expect, it, vi } from "vitest";
import {
  authenticatePasskey,
  creationOptions,
  decodeCredentialBytes,
  requestOptions,
  serializeCredential,
} from "../src/account/webauthn";

afterEach(() => vi.restoreAllMocks());

describe("browser Passkey boundary", () => {
  it("decodes unpadded base64url and rejects ambiguous encodings", () => {
    expect([...new Uint8Array(decodeCredentialBytes("AP_-AQ"))]).toEqual([
      0, 255, 254, 1,
    ]);
    for (const invalid of ["", "AA==", "A", "AB", "a+b/", " AA"])
      expect(() => decodeCredentialBytes(invalid)).toThrow();
  });

  it("decodes challenge and credential identities without mutating the server options", () => {
    const input = {
      challenge: "AQID",
      rpId: "example.test",
      userVerification: "required" as const,
      allowCredentials: [{ type: "public-key" as const, id: "BAU" }],
    };
    const native = requestOptions(input);
    expect([...new Uint8Array(native.challenge as ArrayBuffer)]).toEqual([
      1, 2, 3,
    ]);
    expect([
      ...new Uint8Array(native.allowCredentials![0]!.id as ArrayBuffer),
    ]).toEqual([4, 5]);
    expect(native.userVerification).toBe("required");
    expect(input.allowCredentials[0]!.id).toBe("BAU");
    const create = creationOptions({
      challenge: "AQID",
      rp: { id: "example.test", name: "agenteam" },
      user: { id: "Bgc", name: "owned", displayName: "Owned" },
      pubKeyCredParams: [{ alg: -7, type: "public-key" }],
    });
    expect([...new Uint8Array(create.user.id as ArrayBuffer)]).toEqual([6, 7]);
  });

  it("serializes the native assertion with a nullable user handle", () => {
    const value = serializeCredential({
      id: "AQID",
      rawId: new Uint8Array([1, 2, 3]).buffer,
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({}),
      response: {
        clientDataJSON: new Uint8Array([4, 5]).buffer,
        authenticatorData: new Uint8Array([6]).buffer,
        signature: new Uint8Array([7, 8]).buffer,
        userHandle: null,
      },
    } as unknown as PublicKeyCredential);
    expect(value.response).toEqual({
      clientDataJSON: "BAU",
      authenticatorData: "Bg",
      signature: "Bwg",
      userHandle: null,
    });
  });

  it("uses navigator.credentials and passes ownership cancellation through", async () => {
    const controller = new AbortController();
    const get = vi.fn().mockRejectedValue(new DOMException("", "AbortError"));
    const original = Object.getOwnPropertyDescriptor(navigator, "credentials");
    Object.defineProperty(navigator, "credentials", {
      configurable: true,
      value: { get },
    });
    try {
      await expect(
        authenticatePasskey({ challenge: "AQID" }, controller.signal),
      ).rejects.toMatchObject({ name: "AbortError" });
      expect(get.mock.calls[0]![0].signal).toBe(controller.signal);
      expect(get.mock.calls[0]![0].publicKey.allowCredentials).toBeUndefined();
    } finally {
      if (original) Object.defineProperty(navigator, "credentials", original);
      else Reflect.deleteProperty(navigator, "credentials");
    }
  });
});
