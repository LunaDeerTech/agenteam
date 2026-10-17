<script setup lang="ts">
import { UiButton } from "../ui";
import { catalogError } from "../../system/model-catalog";
defineProps<{
  error?: unknown;
  uncertain: boolean;
  replayable: boolean;
  busy: boolean;
  observing: boolean;
  observation?: "found" | "not_found";
  observationError?: unknown;
}>();
defineEmits<{ observe: []; retry: [] }>();
</script>
<template>
  <div v-if="uncertain || error" class="form-feedback" role="alert">
    <template v-if="uncertain">
      <p>原操作结果尚未确认，已保留原请求。请查询结果或继续确认原请求。</p>
      <p v-if="observation === 'found'">
        已找到历史回执；确认原请求后才能完成本次操作。
      </p>
      <p v-else-if="observation === 'not_found'">
        暂未观察到结果，不能据此判断操作未发生。
      </p>
      <p v-if="observationError">{{ catalogError(observationError) }}</p>
      <div class="form-actions">
        <UiButton
          :disabled="busy || !replayable"
          :loading="observing"
          @click="$emit('observe')"
          >查询原操作</UiButton
        >
        <UiButton
          :disabled="observing || !replayable"
          :loading="busy"
          @click="$emit('retry')"
          >确认原请求</UiButton
        >
      </div>
    </template>
    <p v-else>{{ catalogError(error) }}</p>
  </div>
</template>
