import { onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { accountEnabled } from "../account/transport";
import { useSystemAccess } from "./requests";

export interface CursorPage<T> {
  items: T[];
  next_cursor?: string;
}

export function useCursorList<T>(
  read: (
    cursor: string | undefined,
    current: () => boolean,
  ) => Promise<CursorPage<T>>,
) {
  const access = useSystemAccess();
  const items = shallowRef<T[]>([]);
  const cursors = ref<Array<string | undefined>>([undefined]);
  const index = ref(0);
  const nextCursor = ref<string>();
  const loading = ref(false);
  const loaded = ref(false);
  const error = shallowRef<unknown>();
  let alive = true;
  let sequence = 0;
  function clear() {
    sequence++;
    items.value = [];
    cursors.value = [undefined];
    index.value = 0;
    nextCursor.value = undefined;
    loaded.value = false;
    loading.value = false;
    error.value = undefined;
  }
  async function load(page = index.value, cursor = cursors.value[page]) {
    if (!accountEnabled || !access.allowed.value || loading.value) return;
    const owner = ++sequence;
    loading.value = true;
    error.value = undefined;
    try {
      const value = await read(cursor, () => alive && owner === sequence);
      if (!alive || owner !== sequence) return;
      items.value = value.items;
      index.value = page;
      cursors.value = [...cursors.value.slice(0, page), cursor];
      nextCursor.value = value.next_cursor;
      loaded.value = true;
    } catch (failure) {
      if (alive && owner === sequence) error.value = failure;
    } finally {
      if (alive && owner === sequence) loading.value = false;
    }
  }
  watch(access.generation, clear, { flush: "sync" });
  watch(
    access.allowed,
    (value) => {
      if (value) void load();
    },
    { immediate: true },
  );
  onBeforeUnmount(() => {
    alive = false;
    clear();
  });
  return {
    items,
    loading,
    loaded,
    error,
    index,
    nextCursor,
    reload: () => load(),
    first: () => load(0, undefined),
    previous: () =>
      index.value > 0 ? load(index.value - 1) : Promise.resolve(),
    next: () =>
      nextCursor.value
        ? load(index.value + 1, nextCursor.value)
        : Promise.resolve(),
  };
}
