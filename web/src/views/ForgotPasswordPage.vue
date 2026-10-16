<script setup lang="ts">
import { ref } from "vue";
import { RouterLink } from "vue-router";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import { browserCommand } from "../account/session";
import { accountEnabled } from "../account/transport";
import { isRecord } from "../account/types";
import { useCommand } from "../account/useCommand";
import { useLeaveProtection } from "../account/leave";
import { emailError } from "../account/validation";
type Accepted = { accepted: true; delivery_channel: "smtp" | "backend_log" };
const email = ref("");
const fieldError = ref("");
const channel = ref<Accepted["delivery_channel"]>();
const operation = useCommand<Accepted>();
const { busy, error, uncertain, replayable, confirmed } = operation;
useLeaveProtection(() => uncertain.value || busy.value);
async function submit() {
  fieldError.value = emailError(email.value);
  if (fieldError.value || !accountEnabled) return;
  await operation.submit(
    () =>
      browserCommand(
        "/password-resets/request",
        {
          method: "POST",
          status: 202,
          body: JSON.stringify({ email: email.value }),
        },
        (value): value is Accepted =>
          isRecord(value) &&
          value.accepted === true &&
          (value.delivery_channel === "smtp" ||
            value.delivery_channel === "backend_log"),
      ),
    (value) => {
      channel.value = value.delivery_channel;
    },
    () => {
      email.value = "";
    },
  );
}
</script>
<template>
  <form class="account-form" novalidate @submit.prevent="submit">
    <p class="form-note">输入账号邮箱，获取重置密码的方式。</p>
    <p v-if="!accountEnabled" class="form-feedback">
      这是界面预览。安全账号入口就绪后可提交请求。
    </p>
    <p v-if="confirmed" role="status" class="form-feedback">
      {{
        channel === "smtp"
          ? "若该邮箱可用，将通过邮件提供恢复方式，请查看邮箱。"
          : "恢复请求已受理；当前由有权限的管理员通过受限后端日志协助提供恢复方式。"
      }}
    </p>
    <template v-else>
      <UiInput
        v-model="email"
        label="邮箱"
        type="email"
        autocomplete="email"
        inputmode="email"
        :error="fieldError"
        :disabled="!accountEnabled || busy || uncertain"
        required
      />
      <CommandFeedback
        :error="error"
        :busy="busy"
        :uncertain="uncertain"
        :replayable="replayable"
        @retry="operation.retry"
        @abandon="operation.abandon"
      />
      <UiButton
        class="account-submit"
        type="submit"
        variant="primary"
        :loading="busy"
        :disabled="!accountEnabled || uncertain"
        >发送重置请求</UiButton
      >
    </template>
    <RouterLink class="form-link" to="/login">返回登录</RouterLink>
  </form>
</template>
