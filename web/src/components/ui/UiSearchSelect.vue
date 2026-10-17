<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from "vue";
import UiIcon from "./UiIcon.vue";
import UiButton from "./UiButton.vue";
import { useAnchoredLayer } from "./internal/useAnchoredLayer";

export interface SearchSelectOption {
  value: string;
  label: string;
  description?: string;
  tags?: { label: string; title?: string }[];
  disabledReason?: string;
  details?: boolean;
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
    rich?: boolean;
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
const detailsId = ref<string>();
const detailsOption = computed(() =>
  props.options.find((option) => option.value === detailsId.value),
);
const narrowMedia = window.matchMedia("(max-width: 640px)");
const narrow = ref(narrowMedia.matches);
const inlineDetails = ref<HTMLElement>();
const {
  trigger: detailsTrigger,
  panel: detailsPanel,
  open: detailsOpen,
  target: detailsTarget,
  position: detailsPosition,
  show: showDetails,
  close: hideDetails,
} = useAnchoredLayer({
  placement: "right",
  width: 240,
  maxHeight: 380,
  topLayer: true,
  container: () => panel.value ?? "body",
});
function closeDetails(restore = true) {
  const trigger = detailsTrigger.value;
  hideDetails(false);
  detailsId.value = undefined;
  if (restore)
    void nextTick(
      () => trigger?.isConnected && trigger.focus({ preventScroll: true }),
    );
}
function resized(event: MediaQueryListEvent) {
  closeDetails(false);
  narrow.value = event.matches;
  if (open.value)
    void nextTick(() => searchInput.value?.focus({ preventScroll: true }));
}
narrowMedia.addEventListener("change", resized);
onBeforeUnmount(() => narrowMedia.removeEventListener("change", resized));
async function expandDetails(index: number) {
  const option = props.options[index];
  if (!props.rich || !option?.details || props.loading || props.error) return;
  active.value = index;
  detailsTrigger.value = document.getElementById(
    `${optionId(index)}-more`,
  ) as HTMLButtonElement;
  detailsId.value = option.value;
  if (narrow.value) await nextTick();
  else await showDetails();
  const surface = narrow.value ? inlineDetails.value : detailsPanel.value;
  (
    surface?.querySelector<HTMLElement>(
      "button:not(:disabled),input:not(:disabled)",
    ) ?? surface
  )?.focus({ preventScroll: true });
}
function detailsKeydown(event: KeyboardEvent) {
  if (event.key === "Escape" || event.key === "ArrowLeft") {
    event.preventDefault();
    event.stopPropagation();
    closeDetails();
  }
}
watch(detailsOpen, (value) => {
  if (!value && !narrow.value) detailsId.value = undefined;
});
function resetActive() {
  const current = props.options.findIndex(
    (o) => o.value === props.modelValue && (props.rich || !o.disabledReason),
  );
  active.value =
    current >= 0
      ? current
      : props.options.findIndex((o) => props.rich || !o.disabledReason);
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
    !props.rich && o.disabledReason ? [] : [i],
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
  closeDetails(false);
  active.value = -1;
  emit("update:query", (event.target as HTMLInputElement).value);
}
function onKeydown(event: KeyboardEvent) {
  if (
    detailsOption.value &&
    (narrow.value || detailsPanel.value?.contains(event.target as Node))
  )
    return;
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    close();
  } else if (
    (event.target === searchInput.value ||
      (props.rich && (event.target as HTMLElement).closest('[role="row"]'))) &&
    ["ArrowDown", "ArrowUp"].includes(event.key)
  ) {
    event.preventDefault();
    move(event.key === "ArrowDown" ? 1 : -1);
    if (props.rich && event.target !== searchInput.value)
      void nextTick(() =>
        document
          .getElementById(`${optionId(active.value)}-select`)
          ?.focus({ preventScroll: true }),
      );
  } else if (props.rich && event.key === "ArrowRight") {
    event.preventDefault();
    void expandDetails(active.value);
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
function retry() {
  if (!open.value || props.loading) return;
  if (panel.value?.contains(document.activeElement))
    searchInput.value?.focus({ preventScroll: true });
  emit("retry");
}
function navigate(direction: "previous" | "next") {
  if (props.loading) return;
  if (direction === "next") emit("next");
  else emit("previous");
  searchInput.value?.focus({ preventScroll: true });
}
watch(
  open,
  (value) => {
    if (!value) closeDetails(false);
    emit("open-change", value);
  },
  { flush: "sync" },
);
watch(
  () => props.options,
  () => {
    if (!detailsOption.value) closeDetails(false);
    resetActive();
  },
);
watch(
  () => [props.loading, props.error],
  () => {
    if (props.loading || props.error) closeDetails(false);
  },
);
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
        <div v-show="!narrow || !detailsOption" class="ui-search-select-search">
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
            :aria-haspopup="rich ? 'grid' : 'listbox'"
            aria-expanded="true"
            :aria-controls="`${id}-results`"
            :aria-activedescendant="
              active >= 0 && !loading && !error ? optionId(active) : undefined
            "
            @input="search"
          />
        </div>
        <div
          v-show="!narrow || !detailsOption"
          class="ui-search-select-results"
          :aria-busy="loading"
        >
          <p v-if="loading" class="ui-popup-empty" role="status">
            正在读取模型…
          </p>
          <div v-else-if="error" class="ui-search-select-error" role="alert">
            <p>{{ error }}</p>
            <UiButton @click="retry">重新读取</UiButton>
          </div>
          <ul
            :id="`${id}-results`"
            :role="rich ? 'grid' : 'listbox'"
            :aria-label="label"
            :hidden="loading || !!error"
          >
            <li
              v-for="(option, index) in options"
              :id="optionId(index)"
              :key="option.value"
              :role="rich ? 'row' : 'option'"
              :aria-selected="option.value === modelValue"
              :aria-disabled="(!rich && !!option.disabledReason) || undefined"
              :aria-describedby="
                option.disabledReason ? `${id}-reason-${index}` : undefined
              "
              class="ui-popup-item ui-select-option"
              :class="{
                'is-active': active === index,
                'is-disabled': !!option.disabledReason,
                'is-rich': rich,
              }"
              @pointerdown="!rich && $event.preventDefault()"
              @pointermove="
                (rich || !option.disabledReason) && (active = index)
              "
              @click="!rich && choose(index)"
            >
              <template v-if="rich">
                <div role="gridcell" class="ui-search-select-primary">
                  <button
                    :id="`${optionId(index)}-select`"
                    type="button"
                    class="ui-search-select-row-button"
                    :tabindex="active === index ? 0 : -1"
                    :aria-disabled="!!option.disabledReason || undefined"
                    :aria-describedby="
                      option.disabledReason
                        ? `${id}-reason-${index}`
                        : undefined
                    "
                    @focus="active = index"
                    @click.stop="choose(index)"
                  >
                    <span class="ui-popup-item-label">
                      <span class="ui-search-select-name">{{
                        option.label
                      }}</span>
                      <span
                        v-if="option.tags?.length"
                        class="ui-search-select-tags"
                      >
                        <span
                          v-for="tag in option.tags"
                          :key="tag.label"
                          class="ui-search-select-tag"
                          :title="tag.title"
                          >{{ tag.label }}</span
                        >
                      </span>
                      <span
                        v-if="
                          option.disabledReason ||
                          (!option.tags?.length && option.description)
                        "
                        :id="`${id}-reason-${index}`"
                        class="ui-search-select-detail"
                        >{{ option.disabledReason || option.description }}</span
                      >
                    </span>
                    <UiIcon
                      v-if="option.value === modelValue"
                      name="check"
                      :size="14"
                    />
                  </button>
                </div>
                <div
                  v-if="option.details"
                  role="gridcell"
                  class="ui-search-select-action"
                >
                  <button
                    :id="`${optionId(index)}-more`"
                    type="button"
                    class="ui-button ui-button--ghost ui-icon-button ui-search-select-more"
                    :tabindex="active === index ? 0 : -1"
                    :aria-label="`${option.label}，更多`"
                    aria-haspopup="dialog"
                    :aria-expanded="detailsId === option.value"
                    :aria-controls="
                      detailsId === option.value ? `${id}-details` : undefined
                    "
                    @focus="active = index"
                    @click.stop="expandDetails(index)"
                  >
                    <UiIcon name="more-horizontal" :size="16" />
                  </button>
                </div>
              </template>
              <span v-else class="ui-popup-item-label"
                ><span class="ui-search-select-name">{{ option.label }}</span>
                <span v-if="option.tags?.length" class="ui-search-select-tags">
                  <span
                    v-for="tag in option.tags"
                    :key="tag.label"
                    class="ui-search-select-tag"
                    :title="tag.title"
                    >{{ tag.label }}</span
                  >
                </span>
                <span
                  v-if="
                    option.disabledReason ||
                    (!option.tags?.length && option.description)
                  "
                  :id="`${id}-reason-${index}`"
                  class="ui-search-select-detail"
                  >{{ option.disabledReason || option.description }}</span
                > </span
              ><UiIcon
                v-if="!rich && option.value === modelValue"
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
        <nav
          v-show="!narrow || !detailsOption"
          class="ui-search-select-pagination"
          :aria-label="`${label}分页`"
        >
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
        <div
          v-if="narrow && detailsOption"
          :id="`${id}-details`"
          ref="inlineDetails"
          class="ui-search-select-details"
          role="dialog"
          :aria-label="`${detailsOption.label}，更多`"
          tabindex="-1"
          @keydown="detailsKeydown"
        >
          <UiButton variant="ghost" @click="closeDetails()"
            ><UiIcon name="arrow-left" :size="14" />返回模型列表</UiButton
          >
          <slot name="details" :option="detailsOption" :close="closeDetails" />
        </div>
        <Teleport :to="detailsTarget">
          <Transition name="ui-overlay">
            <div
              v-if="!narrow && detailsOpen && detailsOption"
              :id="`${id}-details`"
              ref="detailsPanel"
              popover="manual"
              role="dialog"
              :aria-label="`${detailsOption.label}，更多`"
              class="ui-popup ui-search-select-details ui-search-select-side"
              :style="detailsPosition"
              tabindex="-1"
              @keydown="detailsKeydown"
            >
              <slot
                name="details"
                :option="detailsOption"
                :close="closeDetails"
              />
            </div>
          </Transition>
        </Teleport>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-search-select-primary {
  min-width: 0;
  flex: 1;
}
.ui-search-select-action {
  flex: none;
}
.ui-search-select-row-button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.ui-search-select-row-button .ui-popup-item-label {
  flex: 1;
  min-width: 0;
}
.ui-search-select-row-button svg {
  flex: none;
}
.ui-search-select-row-button:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 3px;
  border-radius: 3px;
}
.ui-search-select-more {
  opacity: 0;
}
.is-rich:hover .ui-search-select-more,
.is-rich:focus-within .ui-search-select-more,
.is-rich.is-active .ui-search-select-more {
  opacity: 1;
}
.ui-search-select-details {
  display: grid;
  gap: 12px;
  padding: 12px;
  overflow: auto;
  overscroll-behavior: contain;
}
.ui-search-select-side {
  inset: auto;
  margin: 0;
}
@media (hover: none), (pointer: coarse) {
  .ui-search-select-more {
    opacity: 1;
  }
}
@media (max-width: 640px) {
  .ui-search-select-more {
    opacity: 1;
  }
}
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
.ui-search-select-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 5px;
}
.ui-search-select-tag {
  max-width: 100%;
  padding: 1px 5px;
  border: 1px solid var(--border-subtle);
  border-radius: 4px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
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
