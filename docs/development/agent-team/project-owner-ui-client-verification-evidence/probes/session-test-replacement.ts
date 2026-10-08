// Test-only dependency replacement. Pure return parsing must never call this.
export function useSession(): never {
  throw new Error('SESSION_BEHAVIOR_OUTSIDE_THIS_FROZEN_TEST')
}
export type SessionController = never
