import { router } from "./index";
export function safeReturnPath(value: unknown): string {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\?#\u0000-\u0020\u007f]/.test(value)
  )
    return "/";
  const resolved = router.resolve(value);
  if (
    !resolved.matched.length ||
    resolved.name === "not-found" ||
    resolved.meta.layout === "account"
  )
    return "/";
  return resolved.path;
}
