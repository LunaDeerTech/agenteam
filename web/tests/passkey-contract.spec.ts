import { describe, expect, it } from "vitest";
import {
  isAssertionOptions,
  isPasskeyCapabilities,
  isPasskeyList,
  mailCodeError,
  passkeyLabelError,
} from "../src/account/passkeys";

const item = {
  id: "019a0000-0000-7000-8000-000000000001",
  label: "工作电脑",
  created_at: "2026-10-16T12:00:00.000000Z",
  last_used_at: null,
};

describe("Passkey server contract", () => {
  it("does not turn unavailable or malformed capability responses into a usable feature", () => {
    expect(
      isPasskeyCapabilities({
        available: false,
        reason: "domain_required",
        rp_id: null,
      }),
    ).toBe(true);
    expect(
      isPasskeyCapabilities({
        available: true,
        reason: "domain_required",
        rp_id: "localhost",
      }),
    ).toBe(false);
    expect(
      isPasskeyCapabilities({
        available: true,
        reason: "available",
        rp_id: null,
      }),
    ).toBe(false);
    expect(
      isPasskeyCapabilities({
        available: false,
        reason: "domain_required",
        rp_id: null,
        enabled: true,
      }),
    ).toBe(false);
  });
  it("rejects duplicate or unsafe list projections rather than rendering a misleading credential list", () => {
    const list = {
      version: "1",
      email_verified: true,
      email_initialization_required: false,
      items: [item],
    };
    expect(isPasskeyList(list)).toBe(true);
    expect(isPasskeyList({ ...list, email_verified: false })).toBe(true);
    expect(
      isPasskeyList({ ...list, email_initialization_required: true }),
    ).toBe(false);
    expect(
      isPasskeyList({ version: "1", email_verified: false, items: [] }),
    ).toBe(false);
    expect(isPasskeyList({ ...list, version: "0" })).toBe(false);
    expect(isPasskeyList({ ...list, items: [item, item] })).toBe(false);
    expect(
      isPasskeyList({
        ...list,
        items: [{ ...item, public_key: "not-a-list-field" }],
      }),
    ).toBe(false);
    expect(
      isPasskeyList({
        ...list,
        items: Array.from({ length: 21 }, (_, index) => ({
          ...item,
          id: `019a0000-0000-7000-8000-${String(index).padStart(12, "0")}`,
        })),
      }),
    ).toBe(false);
  });
  it("requires user verification even when the server supplies otherwise usable assertion options", () => {
    const options = {
      operation_id: item.id,
      expires_at: item.created_at,
      public_key: {
        challenge: "AQID",
        rpId: "localhost",
        userVerification: "required",
      },
    };
    expect(isAssertionOptions(options)).toBe(true);
    expect(
      isAssertionOptions({
        ...options,
        public_key: { ...options.public_key, userVerification: "preferred" },
      }),
    ).toBe(false);
    expect(
      isAssertionOptions({
        ...options,
        public_key: { ...options.public_key, challenge: "AA==" },
      }),
    ).toBe(false);
  });
  it("accepts Unicode labels within the contract and only six ASCII OTP digits", () => {
    expect(passkeyLabelError("  我的电脑  ")).toBe("");
    expect(passkeyLabelError("🔑".repeat(64))).toBe("");
    expect(passkeyLabelError("🔑".repeat(65))).not.toBe("");
    for (const code of [
      "12345",
      "1234567",
      "１２３４５６",
      "123 56",
      " 123456",
    ])
      expect(mailCodeError(code)).not.toBe("");
    expect(mailCodeError("012345")).toBe("");
  });
});
