import { isRecord, isUser, type AccountUser } from "../account/types";
import { systemRequest } from "./requests";
import type { CursorPage } from "./useCursorList";

export const isInstant = (value: unknown): value is string =>
  typeof value === "string" &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value) &&
  Number.isFinite(Date.parse(value)) &&
  new Date(value).toISOString().slice(0, 23) === value.slice(0, 23);
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
    Object.keys(value).every(
      (key) => key === "items" || key === "next_cursor",
    ) &&
    Array.isArray(value.items) &&
    value.items.length <= 100 &&
    value.items.every(item) &&
    (value.next_cursor === undefined ||
      (typeof value.next_cursor === "string" &&
        value.next_cursor.length > 0 &&
        new TextEncoder().encode(value.next_cursor).length <= 8192))
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
          Object.keys(item).every((key) =>
            [
              "id",
              "email",
              "username",
              "display_name",
              "role",
              "theme",
              "version",
              "initial_password_suggestion",
              "created_at",
            ].includes(key),
          ) &&
          isInstant((item as unknown as Record<string, unknown>).created_at),
      ) && value.items.length <= 10,
  );
}
export function formatInstant(value: string) {
  return value.slice(0, 19).replace("T", " ") + " UTC";
}
