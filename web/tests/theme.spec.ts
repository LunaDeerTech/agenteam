import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  initTheme,
  THEME_STORAGE_KEY,
  useTheme,
} from "../src/composables/useTheme";

function systemPreference(dark: boolean) {
  const events = new EventTarget();
  let matches = dark;
  const media = {
    get matches() {
      return matches;
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener: events.addEventListener.bind(events),
    removeEventListener: events.removeEventListener.bind(events),
  } as MediaQueryList;
  vi.stubGlobal("matchMedia", () => media);
  return (next: boolean) => {
    matches = next;
    events.dispatchEvent(new Event("change"));
  };
}

let dispose: (() => void) | undefined;
beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});
afterEach(() => {
  dispose?.();
  dispose = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("local appearance preference", () => {
  it("follows system changes until an explicit choice, then resumes on system", () => {
    const changeSystem = systemPreference(false);
    dispose = initTheme();
    const appearance = useTheme();
    expect(document.documentElement.dataset.theme).toBe("light");
    changeSystem(true);
    expect(document.documentElement.dataset.theme).toBe("dark");
    appearance.setTheme("light");
    changeSystem(false);
    changeSystem(true);
    expect(appearance.theme.value).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    appearance.setTheme("system");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
  });

  it("restores a saved choice on a fresh initialization and ignores invalid values", () => {
    systemPreference(true);
    dispose = initTheme();
    useTheme().setTheme("light");
    dispose();
    dispose = initTheme();
    expect(useTheme().theme.value).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    dispose();
    localStorage.setItem(THEME_STORAGE_KEY, "unsupported");
    dispose = initTheme();
    expect(useTheme().theme.value).toBe("system");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("keeps appearance usable when browser storage rejects both reads and writes", () => {
    systemPreference(true);
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage unavailable", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("Storage unavailable", "QuotaExceededError");
    });
    expect(() => {
      dispose = initTheme();
    }).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(() => useTheme().setTheme("light")).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("accepts appearance changes from another tab and stops reacting after disposal", () => {
    const changeSystem = systemPreference(false);
    dispose = initTheme();
    window.dispatchEvent(
      new StorageEvent("storage", { key: THEME_STORAGE_KEY, newValue: "dark" }),
    );
    expect(document.documentElement.dataset.theme).toBe("dark");
    window.dispatchEvent(
      new StorageEvent("storage", { key: "unrelated", newValue: "light" }),
    );
    expect(document.documentElement.dataset.theme).toBe("dark");
    window.dispatchEvent(
      new StorageEvent("storage", { key: null, newValue: null }),
    );
    expect(document.documentElement.dataset.theme).toBe("light");
    dispose();
    dispose = undefined;
    changeSystem(true);
    window.dispatchEvent(
      new StorageEvent("storage", { key: THEME_STORAGE_KEY, newValue: "dark" }),
    );
    expect(document.documentElement.dataset.theme).toBe("light");
  });
});
