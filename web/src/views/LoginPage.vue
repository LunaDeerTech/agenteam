<script setup lang="ts">
import { computed, ref, shallowRef } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import RotateChallenge from "../components/account/RotateChallenge.vue";
import { accountEnabled, AccountError } from "../account/transport";
import { loginCommand, useSession, refreshSession } from "../account/session";
import { useCommand } from "../account/useCommand";
import { useLeaveProtection } from "../account/leave";
import { emailError } from "../account/validation";
import { safeReturnPath } from "../router/returnPath";
const router = useRouter();
const route = useRoute();
const { status, confirmedAction } = useSession();
const email = ref("");
const password = ref("");
const errors = ref({ email: "", password: "" });
const operation = useCommand<unknown>();
const { busy, error, uncertain, confirmed, replayable } = operation;
const challenge = shallowRef<{ email: string; key: string }>();
let challengePassword = "";
const frozen = computed(
  () => busy.value || uncertain.value || confirmed.value || !!challenge.value,
);
useLeaveProtection(
  () =>
    !!password.value ||
    !!challenge.value ||
    uncertain.value ||
    (busy.value && !confirmed.value),
);
function clearPasswords() {
  password.value = "";
  challengePassword = "";
  challenge.value = undefined;
}
async function goToApplication() {
  await router.replace(safeReturnPath(route.query.return));
}
async function submit() {
  if (frozen.value || !accountEnabled) return;
  errors.value = {
    email: emailError(email.value),
    password: password.value ? "" : "请输入密码。",
  };
  if (Object.values(errors.value).some(Boolean)) return;
  const key = crypto.randomUUID();
  const submittedEmail = email.value;
  const submittedPassword = password.value;
  await operation.submit(
    () => loginCommand(submittedEmail, submittedPassword, { key }),
    goToApplication,
    clearPasswords,
  );
  if (
    error.value instanceof AccountError &&
    error.value.code === "CHALLENGE_REQUIRED" &&
    !uncertain.value
  ) {
    challengePassword = submittedPassword;
    challenge.value = { email: submittedEmail, key };
  }
}
async function verified(pass: string) {
  if (!challenge.value) return;
  const context = challenge.value;
  await operation.submit(
    () =>
      loginCommand(context.email, challengePassword, {
        key: context.key,
        challengePass: pass,
      }),
    goToApplication,
    clearPasswords,
  );
}
function abandon() {
  operation.abandon();
  clearPasswords();
}
async function recheck() {
  await refreshSession();
  if (status.value === "authenticated") await goToApplication();
}
</script>
<template>
  <div class="account-form">
    <p
      v-if="confirmedAction === 'password'"
      class="form-feedback"
      role="status"
    >
      密码已修改，其他登录会话已退出。请使用新密码重新登录。
    </p>
    <p v-if="!accountEnabled" class="form-feedback">
      这是界面预览。安全账号入口就绪后可登录。
    </p>
    <template v-else-if="status === 'authenticated'">
      <p class="form-note">你已经登录，可以继续使用当前账号。</p>
      <UiButton variant="primary" @click="goToApplication">进入应用</UiButton>
    </template>
    <template v-else-if="confirmed">
      <p role="status" class="form-feedback">
        登录已完成，正在确认当前登录状态。若连接中断，请继续检查，无需重复提交密码。
      </p>
      <UiButton :loading="status === 'checking'" @click="recheck"
        >重新检查登录状态</UiButton
      >
    </template>
    <form v-else class="account-form" novalidate @submit.prevent="submit">
      <fieldset :disabled="!accountEnabled || frozen">
        <UiInput
          v-model="email"
          label="邮箱"
          type="email"
          autocomplete="username"
          inputmode="email"
          :error="errors.email"
          required
        />
        <UiInput
          v-model="password"
          label="密码"
          type="password"
          autocomplete="current-password"
          :error="errors.password"
          required
        />
      </fieldset>
      <CommandFeedback
        :error="error"
        :uncertain="uncertain"
        :replayable="replayable"
        :busy="busy"
        @retry="operation.retry"
        @abandon="abandon"
      />
      <RotateChallenge
        v-if="challenge"
        :email="challenge.email"
        :login-key="challenge.key"
        @verified="verified"
      />
      <UiButton
        v-if="challenge"
        variant="ghost"
        :disabled="busy"
        @click="abandon"
        >重新填写登录信息</UiButton
      >
      <UiButton
        v-else
        class="account-submit"
        type="submit"
        variant="primary"
        :loading="busy"
        :disabled="!accountEnabled || frozen"
        >登录</UiButton
      >
      <RouterLink class="form-link" to="/forgot-password"
        >忘记密码？</RouterLink
      >
    </form>
  </div>
</template>
