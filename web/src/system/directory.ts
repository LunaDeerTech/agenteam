import { isRecord, isUser, type AccountUser } from "../account/types";
import { systemRequest } from "./requests";
import type { CursorPage } from "./useCursorList";

export const isInstant = (value: unknown): value is string =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value);
export const isId = (value: unknown): value is string =>
  typeof value === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    value,
  );
export interface SystemUser extends AccountUser {
  created_at: string;
}
export function isCursorPage<T>(
  value: unknown,
  item: (value: unknown) => value is T,
): value is CursorPage<T> {
  return (
    isRecord(value) &&
    Array.isArray(value.items) &&
    value.items.length <= 100 &&
    value.items.every(item) &&
    (value.next_cursor === undefined ||
      (typeof value.next_cursor === "string" &&
        value.next_cursor.length > 0 &&
        value.next_cursor.length <= 8192))
  );
}
export function listUsers(cursor?: string, current?: () => boolean) {
  return systemRequest(
    "/system/users",
    { query: { limit: 10, cursor }, current },
    (value): value is CursorPage<SystemUser> =>
      isCursorPage(
        value,
        (item): item is SystemUser =>
          isUser(item) &&
          isInstant((item as unknown as Record<string, unknown>).created_at),
      ),
  );
}
export function formatInstant(value: string) {
  return value.slice(0, 16).replace("T", " ") + " UTC";
}
