import { sessionIdentity } from "../account/session";
import type { CursorPosition } from "./useCursorList";
let saved:
  | {
      identity: string;
      position: CursorPosition;
      focusId: string;
      scroll: number;
    }
  | undefined;
const identity = () => JSON.stringify(sessionIdentity());
export function rememberCatalog(
  position: CursorPosition,
  focusId: string,
  scroll: number,
) {
  saved = { identity: identity(), position, focusId, scroll };
}
export function consumeCatalog() {
  const current = saved;
  saved = undefined;
  return current?.identity === identity() ? current : undefined;
}
