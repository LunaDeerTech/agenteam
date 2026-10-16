let captured: { kind: "invite" | "reset-password"; token: string } | undefined;

// Imported before router creation. Link capabilities stay in memory and never
// enter route state, storage, a request URL, diagnostics, or a rendered field.
const kind = window.location.pathname.split("/").filter(Boolean).at(-1);
if (kind === "invite" || kind === "reset-password") {
  const fragment = window.location.hash.slice(1);
  let cleared = false;
  try {
    window.history.replaceState(null, "", window.location.pathname);
    cleared = true;
  } catch {
    captured = undefined;
  }
  const params = new URLSearchParams(fragment);
  const token = params.get("token") ?? fragment;
  if (
    cleared &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}$/.test(
      token,
    )
  )
    captured = { kind, token };
}

export function takeAccountLink(
  kind: "invite" | "reset-password",
): string | undefined {
  const token = captured?.kind === kind ? captured.token : undefined;
  captured = undefined;
  return token;
}
