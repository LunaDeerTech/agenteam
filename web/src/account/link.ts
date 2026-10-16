type LinkKind = "invite" | "reset-password";
let captured: { kind: LinkKind; token: string } | undefined;
let consumer:
  { kind: LinkKind; accept: (token: string | undefined) => void } | undefined;
const tokenPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}$/;
function capture() {
  const fragment = window.location.hash.slice(1);
  if (!fragment) return;
  const path = window.location.pathname;
  const kind =
    path === "/invite"
      ? "invite"
      : path === "/reset-password"
        ? "reset-password"
        : undefined;
  captured = undefined;
  try {
    window.history.replaceState(null, "", path);
  } catch {
    if (kind && consumer?.kind === kind) consumer.accept(undefined);
    return;
  }
  if (!kind) return;
  const value = fragment.startsWith("token=")
    ? new URLSearchParams(fragment).get("token")
    : fragment;
  const token = value && tokenPattern.test(value) ? value : undefined;
  if (consumer?.kind === kind) consumer.accept(token);
  else if (token) captured = { kind, token };
}
// Runs before createWebHistory; later links use the same clear-first boundary.
capture();
window.addEventListener("hashchange", capture);
window.addEventListener("popstate", capture);
export function takeAccountLink(kind: LinkKind) {
  const token = captured?.kind === kind ? captured.token : undefined;
  captured = undefined;
  return token;
}
export function ownAccountLink(
  kind: LinkKind,
  accept: (token: string | undefined) => void,
) {
  const owner = { kind, accept };
  consumer = owner;
  return () => {
    if (consumer === owner) consumer = undefined;
    if (captured?.kind === kind) captured = undefined;
  };
}
if (import.meta.hot)
  import.meta.hot.dispose(() => {
    window.removeEventListener("hashchange", capture);
    window.removeEventListener("popstate", capture);
    captured = undefined;
    consumer = undefined;
  });
