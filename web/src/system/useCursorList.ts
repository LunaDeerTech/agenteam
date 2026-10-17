import {
  computed,
  onBeforeUnmount,
  readonly,
  ref,
  shallowRef,
  watch,
} from "vue";
import { accountEnabled } from "../account/transport";
import { useSystemAccess } from "./requests";
import { isPageSize, type PageSize } from "./pagination";

export interface CursorPage<T> {
  items: T[];
  next_cursor?: string;
}
export interface CursorPosition {
  pageSize: PageSize;
  index: number;
  cursors: Array<string | undefined>;
}
export interface CursorListOptions {
  enabled?: () => boolean;
  immediate?: boolean;
}
export interface CursorReadOptions {
  preserve?: boolean;
}

export function useCursorList<T>(
  read: (
    cursor: string | undefined,
    current: () => boolean,
    limit: number,
  ) => Promise<CursorPage<T>>,
  options: CursorListOptions = {},
) {
  const access = useSystemAccess();
  const enabled = computed(
    () =>
      accountEnabled && access.allowed.value && (options.enabled?.() ?? true),
  );
  const items = shallowRef<T[]>([]);
  const pageSize = ref<PageSize>(10);
  const cursors = ref<Array<string | undefined>>([undefined]);
  const index = ref(0);
  const nextCursor = ref<string>();
  const loading = ref(false);
  const refreshing = ref(false);
  const pending = computed(() => loading.value || refreshing.value);
  const loaded = ref(false);
  const error = shallowRef<unknown>();
  let alive = true;
  let sequence = 0;
  let target: { page: number; cursor?: string } = { page: 0 };
  function cancel() {
    sequence++;
    loading.value = false;
    refreshing.value = false;
  }
  function clear() {
    cancel();
    items.value = [];
    cursors.value = [undefined];
    index.value = 0;
    nextCursor.value = undefined;
    loaded.value = false;
    error.value = undefined;
    target = { page: 0 };
  }
  function reset() {
    clear();
    pageSize.value = 10;
  }
  async function load(
    page: number,
    cursor: string | undefined,
    request: CursorReadOptions = {},
  ) {
    if (!alive || !enabled.value || pending.value) return;
    const owner = ++sequence;
    const current = () => alive && owner === sequence && enabled.value;
    const preserve = request.preserve === true && loaded.value;
    target = { page, cursor };
    refreshing.value = preserve;
    loading.value = !preserve;
    error.value = undefined;
    if (!preserve) {
      items.value = [];
      loaded.value = false;
    }
    try {
      const value = await read(cursor, current, pageSize.value);
      if (!current()) return;
      items.value = value.items;
      index.value = page;
      cursors.value = [...cursors.value.slice(0, page), cursor];
      nextCursor.value = value.next_cursor;
      loaded.value = true;
    } catch (failure) {
      if (current()) error.value = failure;
    } finally {
      if (alive && owner === sequence) {
        loading.value = false;
        refreshing.value = false;
      }
    }
  }
  watch(access.generation, reset, { flush: "sync" });
  watch(
    enabled,
    (value) => {
      if (!value) cancel();
      else if (options.immediate !== false)
        void load(index.value, cursors.value[index.value]);
    },
    { immediate: true, flush: "sync" },
  );
  onBeforeUnmount(() => {
    alive = false;
    clear();
  });
  return {
    items,
    loading,
    refreshing,
    pending,
    loaded,
    error,
    index,
    pageSize: readonly(pageSize),
    nextCursor,
    cancel,
    reset,
    position(): CursorPosition {
      return {
        pageSize: pageSize.value,
        index: index.value,
        cursors: [...cursors.value],
      };
    },
    async restore(position: CursorPosition) {
      if (
        !isPageSize(position.pageSize) ||
        !Number.isInteger(position.index) ||
        position.index < 0 ||
        position.index >= position.cursors.length ||
        position.cursors[0] !== undefined ||
        position.cursors.some(
          (v, i) =>
            i > 0 &&
            (typeof v !== "string" ||
              !v ||
              new TextEncoder().encode(v).length > 8192),
        )
      )
        return;
      clear();
      pageSize.value = position.pageSize;
      cursors.value = [...position.cursors];
      index.value = position.index;
      await load(position.index, position.cursors[position.index]);
    },
    updateItem(matches: (item: T) => boolean, update: (item: T) => T) {
      if (!alive || !enabled.value) return;
      items.value = items.value.map((item) =>
        matches(item) ? update(item) : item,
      );
    },
    async setPageSize(value: string | number) {
      const size = typeof value === "string" ? Number(value) : value;
      if (
        !isPageSize(size) ||
        (typeof value === "string" && String(size) !== value) ||
        size === pageSize.value
      )
        return;
      clear();
      pageSize.value = size;
      await load(0, undefined);
    },
    reload: (request?: CursorReadOptions) =>
      load(target.page, target.cursor, request),
    refresh: (request?: CursorReadOptions) =>
      load(index.value, cursors.value[index.value], request),
    first: (request?: CursorReadOptions) => load(0, undefined, request),
    previous: () =>
      index.value > 0
        ? load(index.value - 1, cursors.value[index.value - 1])
        : Promise.resolve(),
    next: () =>
      nextCursor.value
        ? load(index.value + 1, nextCursor.value)
        : Promise.resolve(),
  };
}
export type CursorList<T> = ReturnType<typeof useCursorList<T>>;
