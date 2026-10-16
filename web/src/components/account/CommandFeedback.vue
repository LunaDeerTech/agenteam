<script setup lang="ts">
import { UiButton } from "../ui";
import { AccountError, accountErrorMessage } from "../../account/transport";
defineProps<{
  error?: unknown;
  message?: string;
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
          ? error instanceof AccountError && error.commitState === "committed"
            ? "操作已提交，回执尚未读回。请保留原请求，继续读取回执；不要重新发起操作。"
            : "原操作结果尚未确认。即使后续登录状态改变，也不能据此判断原操作未发生。"
          : message || accountErrorMessage(error)
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
