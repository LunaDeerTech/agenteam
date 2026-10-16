export type RequestOptionsJSON = Omit<
  PublicKeyCredentialRequestOptions,
  "challenge" | "allowCredentials"
> & {
  challenge: string;
  allowCredentials?: Array<
    Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }
  >;
};

export type CreationOptionsJSON = Omit<
  PublicKeyCredentialCreationOptions,
  "challenge" | "user" | "excludeCredentials"
> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
  excludeCredentials?: RequestOptionsJSON["allowCredentials"];
};

export function decodeCredentialBytes(value: string): ArrayBuffer {
  if (!value || !/^[A-Za-z0-9_-]+$/.test(value) || value.length % 4 === 1)
    throw new Error("INVALID_WEBAUTHN_OPTIONS");
  const decoded = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  const bytes = Uint8Array.from(decoded, (character) =>
    character.charCodeAt(0),
  );
  if (encodeCredentialBytes(bytes.buffer) !== value)
    throw new Error("INVALID_WEBAUTHN_OPTIONS");
  return bytes.buffer;
}

export function encodeCredentialBytes(value: ArrayBuffer): string {
  let binary = "";
  for (const byte of new Uint8Array(value)) binary += String.fromCharCode(byte);
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

export function browserSupportsPasskeys(): boolean {
  return (
    window.isSecureContext === true &&
    typeof PublicKeyCredential !== "undefined" &&
    typeof navigator.credentials?.get === "function" &&
    typeof navigator.credentials?.create === "function"
  );
}

function descriptor(
  value: NonNullable<RequestOptionsJSON["allowCredentials"]>[number],
) {
  return { ...value, id: decodeCredentialBytes(value.id) };
}

export function requestOptions(
  value: RequestOptionsJSON,
): PublicKeyCredentialRequestOptions {
  return {
    ...value,
    challenge: decodeCredentialBytes(value.challenge),
    allowCredentials: value.allowCredentials?.map(descriptor),
  };
}

export function creationOptions(
  value: CreationOptionsJSON,
): PublicKeyCredentialCreationOptions {
  return {
    ...value,
    challenge: decodeCredentialBytes(value.challenge),
    user: { ...value.user, id: decodeCredentialBytes(value.user.id) },
    excludeCredentials: value.excludeCredentials?.map(descriptor),
  };
}

export function serializeCredential(credential: PublicKeyCredential) {
  const response = credential.response;
  const common = {
    id: credential.id,
    rawId: encodeCredentialBytes(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
  if ("attestationObject" in response) {
    const attestation = response as AuthenticatorAttestationResponse;
    return {
      ...common,
      response: {
        clientDataJSON: encodeCredentialBytes(attestation.clientDataJSON),
        attestationObject: encodeCredentialBytes(attestation.attestationObject),
        transports: attestation.getTransports?.() ?? [],
      },
    };
  }
  const assertion = response as AuthenticatorAssertionResponse;
  return {
    ...common,
    response: {
      clientDataJSON: encodeCredentialBytes(assertion.clientDataJSON),
      authenticatorData: encodeCredentialBytes(assertion.authenticatorData),
      signature: encodeCredentialBytes(assertion.signature),
      userHandle: assertion.userHandle
        ? encodeCredentialBytes(assertion.userHandle)
        : null,
    },
  };
}

export async function authenticatePasskey(
  value: RequestOptionsJSON,
  signal: AbortSignal,
) {
  const credential = await navigator.credentials.get({
    publicKey: requestOptions(value),
    signal,
  });
  if (!credential || credential.type !== "public-key")
    throw new Error("PASSKEY_NOT_RETURNED");
  return serializeCredential(credential as PublicKeyCredential);
}

export async function createPasskey(
  value: CreationOptionsJSON,
  signal: AbortSignal,
) {
  const credential = await navigator.credentials.create({
    publicKey: creationOptions(value),
    signal,
  });
  if (!credential || credential.type !== "public-key")
    throw new Error("PASSKEY_NOT_RETURNED");
  return serializeCredential(credential as PublicKeyCredential);
}

export function passkeyCancelled(error: unknown): boolean {
  return (
    error instanceof DOMException &&
    ["AbortError", "NotAllowedError"].includes(error.name)
  );
}
