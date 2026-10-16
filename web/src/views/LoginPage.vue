<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import RotateChallenge from "../components/account/RotateChallenge.vue";
import { accountEnabled, AccountError } from "../account/transport";
import {
  loginCommand,
  passkeyLoginCommand,
  useSession,
  refreshSession,
} from "../account/session";
import {
  getPasskeyCapabilities,
  getPasskeyLoginOptions,
  passkeyErrorMessage,
  passkeyUnavailableMessage,
  type PasskeyCapabilities,
} from "../account/passkeys";
import {
  authenticatePasskey,
  browserSupportsPasskeys,
  passkeyCancelled,
} from "../account/webauthn";
import { useCommand } from "../account/useCommand";
import { useLeaveProtection } from "../account/leave";
import { emailError } from "../account/validation";
import { safeReturnPath } from "../router/returnPath";
const router = useRouter();
const route = useRoute();
const { status, confirmedAction, generation } = useSession();
const capability = shallowRef<PasskeyCapabilities>();
const capabilityLoading = ref(false);
const capabilityError = shallowRef<unknown>();
const passkeyError = shallowRef<unknown>();
const passkeyPrompt = ref(false);
const loginMethod = ref<"password" | "passkey">("password");
let passkeyController: AbortController | undefined;
let alive = true;
let passkeyRevision = 0;
const passkeyAvailable = computed(
  () => capability.value?.available === true && browserSupportsPasskeys(),
);
const email = ref("");
const password = ref("");
const errors = ref({ email: "", password: "" });
const operation = useCommand<unknown>();
const { busy, error, uncertain, confirmed, replayable } = operation;
const challenge = shallowRef<{ email: string; key: string }>();
let challengePassword = "";
const frozen = computed(
  () =>
    busy.value ||
    uncertain.value ||
    confirmed.value ||
    !!challenge.value ||
    passkeyPrompt.value,
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
function fieldIssue(name: string, local: string) {
  return (
    local ||
    (error.value instanceof AccountError &&
    error.value.fields.some((field) => field.path === `/${name}`)
      ? "请检查此项内容后再试。"
      : "")
  );
}
async function goToApplication() {
  await router.replace(safeReturnPath(route.query.return));
}
async function submit() {
  if (frozen.value || !accountEnabled) return;
  loginMethod.value = "password";
  passkeyError.value = undefined;
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
async function readPasskeyCapability() {
  if (!accountEnabled || capabilityLoading.value) return;
  capabilityLoading.value = true;
  capabilityError.value = undefined;
  try {
    const value = await getPasskeyCapabilities({ current: () => alive });
    if (alive) capability.value = value;
  } catch (error) {
    if (alive) capabilityError.value = error;
  } finally {
    if (alive) capabilityLoading.value = false;
  }
}
function cancelPasskey() {
  passkeyRevision++;
  passkeyController?.abort();
  passkeyController = undefined;
  passkeyPrompt.value = false;
}
async function loginWithPasskey() {
  if (!accountEnabled || frozen.value || !passkeyAvailable.value) return;
  loginMethod.value = "passkey";
  passkeyError.value = undefined;
  operation.abandon();
  const revision = ++passkeyRevision;
  const identity = generation.value;
  const current = () =>
    alive && revision === passkeyRevision && identity === generation.value;
  const controller = new AbortController();
  passkeyController = controller;
  passkeyPrompt.value = true;
  try {
    const options = await getPasskeyLoginOptions({ current });
    if (!current()) return;
    const credential = await authenticatePasskey(
      options.public_key,
      controller.signal,
    );
    if (!current()) return;
    passkeyController = undefined;
    await operation.submit(
      () =>
        passkeyLoginCommand({ operation_id: options.operation_id, credential }),
      goToApplication,
      clearPasswords,
    );
  } catch (error) {
    if (current() && !passkeyCancelled(error)) passkeyError.value = error;
  } finally {
    if (revision === passkeyRevision) {
      passkeyController = undefined;
      passkeyPrompt.value = false;
    }
  }
}
watch(generation, () => {
  cancelPasskey();
  passkeyError.value = undefined;
});
onMounted(readPasskeyCapability);
onBeforeUnmount(() => {
  alive = false;
  cancelPasskey();
});
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
    <template v-if="accountEnabled && status === 'authenticated'">
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
      <template v-if="accountEnabled">
        <UiButton
          class="account-submit"
          :disabled="frozen || !passkeyAvailable"
          :loading="passkeyPrompt || capabilityLoading"
          @click="loginWithPasskey"
          >使用 Passkey 登录</UiButton
        >
        <UiButton
          v-if="passkeyPrompt && !busy"
          variant="ghost"
          @click="cancelPasskey"
          >取消验证</UiButton
        >
        <p v-if="capability && !passkeyAvailable" class="form-note">
          {{ passkeyUnavailableMessage(capability) }}
        </p>
        <div v-if="capabilityError" class="form-feedback" role="alert">
          <p>暂时无法读取 Passkey 可用状态，仍可使用密码。</p>
          <UiButton :loading="capabilityLoading" @click="readPasskeyCapability"
            >重新读取</UiButton
          >
        </div>
        <p v-if="passkeyError" class="form-feedback" role="alert">
          {{ passkeyErrorMessage(passkeyError) }}
        </p>
      </template>
      <fieldset :disabled="!accountEnabled || frozen">
        <UiInput
          v-model="email"
          label="邮箱"
          type="email"
          autocomplete="username"
          inputmode="email"
          :error="fieldIssue('email', errors.email)"
          required
        />
        <UiInput
          v-model="password"
          label="密码"
          type="password"
          autocomplete="current-password"
          :error="fieldIssue('password', errors.password)"
          required
        />
      </fieldset>
      <CommandFeedback
        :error="error"
        :message="
          loginMethod === 'password' &&
          error instanceof AccountError &&
          error.status === 401
            ? '邮箱或密码不正确，请检查后重试。'
            : undefined
        "
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
