<script setup lang="ts">
import { UiButton } from "../ui";
import { accountErrorMessage } from "../../account/transport";
defineProps<{
  error?: unknown;
  uncertain?: boolean;
  replayable?: boolean;
  busy?: boolean;
}>();
defineEmits<{ retry: []; abandon: [] }>();
</script>
<template>
  <div v-if="error || uncertain" class="form-feedback" role="alert">
    <p>
      {{
        uncertain
          ? "原操作结果尚未确认。即使后续登录状态改变，也不能据此判断原操作未发生。"
          : accountErrorMessage(error)
      }}
    </p>
    <div v-if="uncertain" class="form-actions">
      <UiButton v-if="replayable" :loading="busy" @click="$emit('retry')"
        >重试原请求</UiButton
      >
      <UiButton :disabled="busy" @click="$emit('abandon')"
        >放弃本次操作</UiButton
      >
    </div>
  </div>
</template>
