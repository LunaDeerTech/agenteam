<script setup lang="ts" generic="T">
import { nextTick, ref } from "vue";
import { UiButton } from "../ui";
import PageSizeSelect from "./PageSizeSelect.vue";
import { AccountError, accountErrorMessage } from "../../account/transport";
import type { CursorList } from "../../system/useCursorList";
const props = defineProps<{
  list: CursorList<T>;
  rowKey: (item: T) => string;
  label: string;
  resultsLabel?: string;
  paginationLabel?: string;
  emptyText: string;
  disabled?: boolean;
  pageSizeDisabled?: boolean;
}>();
const results = ref<HTMLElement>();
const failure = ref<HTMLElement>();
async function focusResults(options: { scroll?: boolean } = {}) {
  await nextTick();
  const target = props.list.error.value ? failure.value : results.value;
  target?.focus({ preventScroll: options.scroll === false });
  if (options.scroll !== false) target?.scrollIntoView({ block: "start" });
}
async function navigate(action: () => Promise<void>) {
  if (props.disabled || props.list.pending.value) return;
  await action();
  await nextTick();
  if (!props.disabled) await focusResults();
}
const refresh = () => navigate(() => props.list.first());
const invalidCursor = () =>
  props.list.error.value instanceof AccountError &&
  props.list.error.value.code === "CURSOR_INVALID";
defineExpose({ navigate, focusResults, refresh });
</script>

<template>
  <div class="system-list">
    <div class="system-list-status">
      <p class="form-note" role="status" aria-live="polite">
        {{
          list.loading.value
            ? "正在读取…"
            : list.loaded.value
              ? `第 ${list.index.value + 1} 页，已读取 ${list.items.value.length} 条。${list.refreshing.value ? "正在更新…" : ""}`
              : "尚未读取"
        }}
      </p>
      <PageSizeSelect
        :model-value="list.pageSize.value"
        :disabled="disabled || pageSizeDisabled"
        @update:model-value="list.setPageSize"
      />
    </div>
    <div
      v-if="list.error.value"
      ref="failure"
      class="form-feedback system-list-error"
      role="alert"
      tabindex="-1"
    >
      <p>{{ accountErrorMessage(list.error.value) }}</p>
      <slot name="error-extra" :error="list.error.value" />
      <div class="form-actions">
        <UiButton
          :disabled="disabled"
          :loading="list.pending.value"
          @click="navigate(invalidCursor() ? list.first : list.reload)"
          >{{ invalidCursor() ? "返回第一页" : "重新读取" }}</UiButton
        >
        <UiButton
          v-if="!invalidCursor() && list.index.value > 0"
          :disabled="disabled || list.pending.value"
          @click="refresh"
          >返回第一页</UiButton
        >
      </div>
    </div>
    <div
      ref="results"
      tabindex="-1"
      class="system-list-results"
      :aria-label="resultsLabel || `${label}结果`"
      :aria-busy="list.pending.value"
    >
      <p
        v-if="list.loaded.value && !list.items.value.length"
        class="system-list-empty"
      >
        {{ emptyText }}
      </p>
      <slot
        v-if="list.items.value.length && $slots.table"
        name="table"
        :items="list.items.value"
      />
      <ul
        v-else-if="list.items.value.length"
        class="system-list-rows"
        :aria-label="label"
      >
        <li
          v-for="item in list.items.value"
          :key="rowKey(item)"
          class="system-list-row"
        >
          <div class="system-list-object">
            <slot name="item" :item="item" />
          </div>
          <div v-if="$slots.actions" class="system-list-actions">
            <slot name="actions" :item="item" />
          </div>
        </li>
      </ul>
    </div>
    <nav
      v-if="list.loaded.value"
      class="system-list-pagination"
      :aria-label="paginationLabel || `${label}分页`"
    >
      <span class="form-note">第 {{ list.index.value + 1 }} 页</span>
      <div class="form-actions">
        <UiButton
          :disabled="disabled || list.pending.value || list.index.value === 0"
          @click="navigate(list.previous)"
          >上一页</UiButton
        >
        <UiButton
          :disabled="disabled || list.pending.value || !list.nextCursor.value"
          @click="navigate(list.next)"
          >下一页</UiButton
        >
      </div>
    </nav>
  </div>
</template>

<style scoped>
.system-list {
  display: grid;
  gap: 16px;
  min-width: 0;
  width: 100%;
}
.system-list-status,
.system-list-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px 16px;
}
.system-list-status .form-note {
  margin: 0;
  min-height: 20px;
}
.system-list-status :deep(.page-size-select) {
  margin-left: auto;
}
.system-list-results,
.system-list-error {
  min-width: 0;
  scroll-margin-block: 12px;
}
.system-list-results:focus-visible,
.system-list-error:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 5px;
}
.system-list-empty {
  padding: 32px 0;
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.7;
}
.system-list-rows {
  list-style: none;
  padding: 0;
  margin: 0;
}
.system-list-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.system-list-row:first-child {
  border-top: 1px solid var(--border-subtle);
}
.system-list-object {
  display: grid;
  gap: 5px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.system-list-object :deep(strong) {
  font-size: 14px;
  font-weight: 550;
}
.system-list-actions {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
}
@media (max-width: 480px) {
  .system-list-row {
    gap: 12px;
  }
}
</style>
