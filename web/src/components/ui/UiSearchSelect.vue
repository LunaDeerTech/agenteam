<script setup lang="ts">
import { nextTick, ref, useId, watch } from "vue";
import UiIcon from "./UiIcon.vue";
import UiButton from "./UiButton.vue";
import { useAnchoredLayer } from "./internal/useAnchoredLayer";

export interface SearchSelectOption {
  value: string;
  label: string;
  description?: string;
  disabledReason?: string;
}
const props = withDefaults(
  defineProps<{
    label: string;
    modelValue?: string;
    selectedLabel?: string;
    options: SearchSelectOption[];
    query?: string;
    loading?: boolean;
    error?: string;
    pageIndex?: number;
    hasPrevious?: boolean;
    hasNext?: boolean;
    disabled?: boolean;
    placeholder?: string;
    searchPlaceholder?: string;
    emptyText?: string;
  }>(),
  {
    query: "",
    pageIndex: 0,
    placeholder: "点击选择模型",
    searchPlaceholder: "搜索供应商或模型",
    emptyText: "没有匹配的模型。",
  },
);
const emit = defineEmits<{
  "update:modelValue": [value: string];
  "update:query": [value: string];
  "open-change": [value: boolean];
  previous: [];
  next: [];
  retry: [];
}>();
const id = useId();
const searchInput = ref<HTMLInputElement>();
const active = ref(-1);
const { trigger, panel, open, target, position, show, close } =
  useAnchoredLayer({
    align: () => "end",
    width: 400,
    maxHeight: 420,
    topLayer: true,
  });
const optionId = (index: number) => `${id}-option-${index}`;
function resetActive() {
  const current = props.options.findIndex(
    (o) => o.value === props.modelValue && !o.disabledReason,
  );
  active.value =
    current >= 0 ? current : props.options.findIndex((o) => !o.disabledReason);
}
async function expand() {
  if (props.disabled) return;
  resetActive();
  await show();
  if (open.value) searchInput.value?.focus({ preventScroll: true });
}
function choose(index: number) {
  const option = props.options[index];
  if (!option || option.disabledReason || props.loading || props.error) return;
  emit("update:modelValue", option.value);
  close();
}
function move(delta: number) {
  const choices = props.options.flatMap((o, i) =>
    o.disabledReason ? [] : [i],
  );
  if (!choices.length || props.loading || props.error) return;
  const current = choices.indexOf(active.value);
  active.value = choices[(current + delta + choices.length) % choices.length]!;
  void nextTick(() =>
    document
      .getElementById(optionId(active.value))
      ?.scrollIntoView?.({ block: "nearest" }),
  );
}
function search(event: Event) {
  active.value = -1;
  emit("update:query", (event.target as HTMLInputElement).value);
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    close();
  } else if (
    event.target === searchInput.value &&
    ["ArrowDown", "ArrowUp"].includes(event.key)
  ) {
    event.preventDefault();
    move(event.key === "ArrowDown" ? 1 : -1);
  } else if (
    event.target === searchInput.value &&
    event.key === "Enter" &&
    !event.isComposing
  ) {
    event.preventDefault();
    choose(active.value);
  } else if (event.key === "Tab") {
    const controls = [
      ...(panel.value?.querySelectorAll<HTMLElement>(
        "input:not(:disabled), button:not(:disabled)",
      ) ?? []),
    ];
    if (
      (event.shiftKey && event.target === controls[0]) ||
      (!event.shiftKey && event.target === controls.at(-1))
    )
      close();
  }
}
function navigate(direction: "previous" | "next") {
  if (props.loading) return;
  if (direction === "next") emit("next");
  else emit("previous");
  searchInput.value?.focus({ preventScroll: true });
}
watch(open, (value) => emit("open-change", value), { flush: "sync" });
watch(() => props.options, resetActive);
watch(
  () => props.disabled,
  (value) => {
    if (value) close(false);
  },
);
</script>

<template>
  <button
    ref="trigger"
    type="button"
    class="ui-select ui-search-select-trigger"
    :disabled="disabled"
    :aria-label="`${label}${selectedLabel ? `，当前 ${selectedLabel}` : ''}`"
    aria-haspopup="dialog"
    :aria-expanded="open"
    :aria-controls="open ? `${id}-panel` : undefined"
    @click="open ? close() : expand()"
    @keydown.down.prevent="expand()"
    @keydown.up.prevent="expand()"
  >
    <span :class="{ 'is-placeholder': !modelValue }">{{
      selectedLabel || (modelValue ? "正在核对当前模型…" : placeholder)
    }}</span>
    <UiIcon name="chevron-down" :size="14" />
  </button>
  <Teleport :to="target">
    <Transition name="ui-overlay">
      <div
        v-if="open"
        :id="`${id}-panel`"
        ref="panel"
        popover="manual"
        role="dialog"
        :aria-label="label"
        class="ui-popup ui-search-select-panel"
        :style="position"
        @keydown="onKeydown"
      >
        <div class="ui-search-select-search">
          <UiIcon name="search" :size="16" />
          <input
            ref="searchInput"
            :value="query"
            type="search"
            role="combobox"
            :aria-label="searchPlaceholder"
            :placeholder="searchPlaceholder"
            autocomplete="off"
            aria-autocomplete="list"
            aria-expanded="true"
            :aria-controls="`${id}-results`"
            :aria-activedescendant="
              active >= 0 && !loading && !error ? optionId(active) : undefined
            "
            @input="search"
          />
        </div>
        <div class="ui-search-select-results" :aria-busy="loading">
          <p v-if="loading" class="ui-popup-empty" role="status">
            正在读取模型…
          </p>
          <div v-else-if="error" class="ui-search-select-error" role="alert">
            <p>{{ error }}</p>
            <UiButton @click="emit('retry')">重新读取</UiButton>
          </div>
          <ul
            :id="`${id}-results`"
            role="listbox"
            :aria-label="label"
            :hidden="loading || !!error"
          >
            <li
              v-for="(option, index) in options"
              :id="optionId(index)"
              :key="option.value"
              role="option"
              :aria-selected="option.value === modelValue"
              :aria-disabled="!!option.disabledReason || undefined"
              :aria-describedby="
                option.disabledReason ? `${id}-reason-${index}` : undefined
              "
              class="ui-popup-item ui-select-option"
              :class="{
                'is-active': active === index,
                'is-disabled': !!option.disabledReason,
              }"
              @pointerdown.prevent
              @pointermove="!option.disabledReason && (active = index)"
              @click="choose(index)"
            >
              <span class="ui-popup-item-label"
                ><span class="ui-search-select-name">{{ option.label }}</span>
                <span
                  v-if="option.disabledReason || option.description"
                  :id="`${id}-reason-${index}`"
                  class="ui-search-select-detail"
                  >{{ option.disabledReason || option.description }}</span
                > </span
              ><UiIcon
                v-if="option.value === modelValue"
                name="check"
                :size="14"
              />
            </li>
          </ul>
          <p
            v-if="!loading && !error && !options.length"
            class="ui-popup-empty"
            role="status"
          >
            {{ emptyText }}
          </p>
        </div>
        <nav class="ui-search-select-pagination" :aria-label="`${label}分页`">
          <span>第 {{ pageIndex + 1 }} 页</span>
          <div>
            <UiButton
              :disabled="loading || !hasPrevious"
              @click="navigate('previous')"
              >上一页</UiButton
            >
            <UiButton :disabled="loading || !hasNext" @click="navigate('next')"
              >下一页</UiButton
            >
          </div>
        </nav>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-search-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  text-align: left;
}
.ui-search-select-trigger > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.ui-search-select-trigger > svg {
  flex: none;
}
.is-placeholder {
  color: var(--text-tertiary);
}
.ui-search-select-panel {
  inset: auto;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.ui-search-select-search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  flex: none;
  border-bottom: 1px solid var(--border-subtle);
}
.ui-search-select-search input {
  min-width: 0;
  width: 100%;
  border: 0;
  background: transparent;
  color: var(--text-primary);
  outline-offset: 4px;
  font: inherit;
}
.ui-search-select-results {
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  padding: 4px;
}
.ui-search-select-results ul {
  list-style: none;
  margin: 0;
  padding: 0;
}
.ui-search-select-name,
.ui-search-select-detail {
  display: block;
}
.ui-search-select-detail {
  margin-top: 4px;
  color: var(--text-secondary);
  font-size: 12px;
}
.ui-search-select-error {
  display: grid;
  gap: 12px;
  padding: 12px;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.ui-search-select-pagination {
  display: flex;
  flex: none;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-top: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  font-size: 12px;
}
.ui-search-select-pagination > div {
  display: flex;
  gap: 6px;
}
</style>
