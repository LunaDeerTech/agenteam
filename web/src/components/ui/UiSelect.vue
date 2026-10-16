<script setup lang="ts">
import { computed, nextTick, ref, useAttrs, useId, watch } from "vue";
import UiIcon from "./UiIcon.vue";
import { useAnchoredLayer } from "./internal/useAnchoredLayer";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}
defineOptions({ inheritAttrs: false });
const props = withDefaults(
  defineProps<{
    id?: string;
    label: string;
    options: SelectOption[];
    placeholder?: string;
    disabled?: boolean;
    hint?: string;
    error?: string;
  }>(),
  { placeholder: "请选择" },
);
const model = defineModel<string>({ default: "" });
const attrs = useAttrs();
const id = useId();
const inputId = computed(() => props.id ?? `${id}-select`);
const description = computed(
  () =>
    [
      attrs["aria-describedby"],
      props.hint ? `${id}-hint` : "",
      props.error ? `${id}-error` : "",
    ]
      .filter(Boolean)
      .join(" ") || undefined,
);
const selected = computed(() =>
  props.options.find((option) => option.value === model.value),
);
const active = ref(-1);
const { trigger, panel, open, target, position, show, close } =
  useAnchoredLayer({ align: () => "start", matchWidth: true });
let search = "";
let searchedAt = 0;
const optionId = (index: number) => `${id}-option-${index}`;

function moveTo(index: number) {
  active.value = index;
  nextTick(() =>
    document
      .getElementById(optionId(index))
      ?.scrollIntoView?.({ block: "nearest" }),
  );
}

async function expand() {
  if (props.disabled) return;
  const current = props.options.findIndex(
    (option) => option.value === model.value && !option.disabled,
  );
  active.value =
    current >= 0
      ? current
      : props.options.findIndex((option) => !option.disabled);
  await show();
  if (open.value) moveTo(active.value);
}

function choose(index: number) {
  const option = props.options[index];
  if (!option || option.disabled) return;
  model.value = option.value;
  close();
}

async function onKeydown(event: KeyboardEvent) {
  if (event.key === "Tab") {
    close(false);
    return;
  }
  if (event.key === "Escape" && open.value) {
    event.preventDefault();
    event.stopPropagation();
    close();
    return;
  }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    if (open.value) choose(active.value);
    else await expand();
    return;
  }
  if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    if (!open.value) {
      await expand();
      if (event.key === "ArrowDown" || event.key === "ArrowUp") return;
    }
    const enabled = props.options
      .map((option, index) => (option.disabled ? -1 : index))
      .filter((index) => index >= 0);
    if (!enabled.length) return;
    const current = enabled.indexOf(active.value);
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? enabled.length - 1
          : (current + (event.key === "ArrowDown" ? 1 : -1) + enabled.length) %
            enabled.length;
    moveTo(enabled[next]!);
    return;
  }
  if (
    event.key.length === 1 &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  ) {
    event.preventDefault();
    if (!open.value) await expand();
    search = Date.now() - searchedAt > 700 ? event.key : search + event.key;
    searchedAt = Date.now();
    const found = props.options.findIndex(
      (option) =>
        !option.disabled &&
        option.label.toLocaleLowerCase().startsWith(search.toLocaleLowerCase()),
    );
    if (found >= 0) moveTo(found);
  }
}

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) close(false);
  },
);
watch(
  () => props.options,
  () => {
    if (props.options[active.value]?.disabled || !props.options[active.value])
      active.value = props.options.findIndex((option) => !option.disabled);
  },
);
</script>

<template>
  <div class="ui-field">
    <label :id="`${id}-label`" :for="inputId" class="ui-field-label">{{
      label
    }}</label>
    <button
      v-bind="$attrs"
      :id="inputId"
      ref="trigger"
      type="button"
      class="ui-select"
      role="combobox"
      :disabled="disabled"
      :aria-labelledby="`${id}-label`"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? `${id}-listbox` : undefined"
      :aria-activedescendant="
        open && active >= 0 ? optionId(active) : undefined
      "
      :aria-invalid="error ? true : undefined"
      :aria-describedby="description"
      @click="open ? close() : expand()"
      @keydown="onKeydown"
    >
      <span class="ui-select-value" :class="{ 'is-placeholder': !selected }">{{
        selected?.label ?? placeholder
      }}</span>
      <UiIcon name="chevron-down" :size="14" />
    </button>
    <p v-if="hint" :id="`${id}-hint`" class="ui-field-hint">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="ui-field-error">{{ error }}</p>
  </div>
  <Teleport :to="target">
    <Transition name="ui-overlay">
      <div
        v-if="open"
        :id="`${id}-listbox`"
        ref="panel"
        role="listbox"
        :aria-labelledby="`${id}-label`"
        class="ui-popup"
        :style="position"
        :inert="!open || undefined"
      >
        <div
          v-for="(option, index) in options"
          :id="optionId(index)"
          :key="option.value"
          role="option"
          :aria-selected="option.value === model"
          :aria-disabled="option.disabled || undefined"
          class="ui-popup-item ui-select-option"
          :class="{
            'is-active': active === index,
            'is-disabled': option.disabled,
          }"
          @pointerdown.prevent
          @pointermove="!option.disabled && (active = index)"
          @click="choose(index)"
        >
          <span class="ui-popup-item-label">{{ option.label }}</span
          ><UiIcon v-if="option.value === model" name="check" :size="14" />
        </div>
        <p v-if="!options.length" class="ui-popup-empty">无可选项</p>
      </div>
    </Transition>
  </Teleport>
</template>
