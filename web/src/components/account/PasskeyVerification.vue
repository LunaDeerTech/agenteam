<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { UiButton, UiInput } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import { useCommand } from "../../account/useCommand";
import { useSession } from "../../account/session";
import {
  completeVerification,
  confirmRecoveryCode,
  getVerificationOptions,
  mailCodeError,
  mailRequestErrorMessage,
  passkeyErrorMessage,
  requestRecoveryCode,
  resendRecoveryCode,
  type MailCodeRequest,
  type PasskeyGrant,
  type VerificationIntent,
} from "../../account/passkeys";
import { authenticatePasskey, passkeyCancelled } from "../../account/webauthn";

const props = defineProps<{
  intent: VerificationIntent;
  hasPasskeys: boolean;
  emailInitializationRequired: boolean;
  recoveryOnly?: boolean;
}>();
const emit = defineEmits<{
  verified: [grant: PasskeyGrant];
  busy: [value: boolean];
}>();
const session = useSession();
const recovery = ref(props.recoveryOnly || !props.hasPasskeys);
const password = ref("");
const code = ref("");
const request = shallowRef<MailCodeRequest>();
const localError = shallowRef<unknown>();
const fieldError = ref("");
const prompt = ref(false);
const now = ref(Date.now());
const timer = setInterval(() => {
  now.value = Date.now();
}, 1000);
const send = useCommand<MailCodeRequest>();
const verify = useCommand<PasskeyGrant>();
let controller: AbortController | undefined;
let revision = 0;
let alive = true;
const recoveryAllowed = computed(
  () =>
    props.intent.purpose === "manage_passkeys" &&
    props.intent.action === "register",
);
const locked = computed(
  () =>
    prompt.value ||
    send.busy.value ||
    send.uncertain.value ||
    verify.busy.value ||
    verify.uncertain.value,
);
const seconds = computed(() =>
  request.value
    ? Math.max(
        0,
        Math.ceil((Date.parse(request.value.resend_after) - now.value) / 1000),
      )
    : 0,
);
watch(locked, (value) => emit("busy", value), { immediate: true });
function cancelPrompt() {
  revision++;
  controller?.abort();
  controller = undefined;
  prompt.value = false;
}
function clear() {
  cancelPrompt();
  password.value = "";
  code.value = "";
  request.value = undefined;
  localError.value = undefined;
  fieldError.value = "";
  send.abandon();
  verify.abandon();
}
async function authenticate() {
  if (locked.value) return;
  localError.value = undefined;
  const owner = ++revision;
  const generation = session.generation.value;
  const current = () =>
    alive && revision === owner && generation === session.generation.value;
  const abort = new AbortController();
  controller = abort;
  prompt.value = true;
  try {
    const options = await getVerificationOptions(props.intent, { current });
    if (!current()) return;
    const credential = await authenticatePasskey(
      options.public_key,
      abort.signal,
    );
    if (!current()) return;
    controller = undefined;
    await verify.submit(
      () =>
        completeVerification(
          options.operation_id,
          credential,
          props.intent.purpose,
        ),
      (grant) => emit("verified", grant),
    );
  } catch (error) {
    if (current() && !passkeyCancelled(error)) localError.value = error;
  } finally {
    if (revision === owner) {
      prompt.value = false;
      controller = undefined;
    }
  }
}
async function sendCode() {
  if (
    locked.value ||
    !recoveryAllowed.value ||
    props.emailInitializationRequired
  )
    return;
  fieldError.value = password.value ? "" : "请输入当前密码。";
  if (fieldError.value) return;
  await send.submit(
    () => requestRecoveryCode(password.value),
    (value) => {
      request.value = value;
      code.value = "";
    },
    () => {
      password.value = "";
    },
  );
}
async function confirmCode() {
  if (locked.value || !request.value || !recoveryAllowed.value) return;
  fieldError.value = mailCodeError(code.value);
  if (fieldError.value) return;
  const id = request.value.request_id;
  await verify.submit(
    () => confirmRecoveryCode(id, code.value),
    (grant) => emit("verified", grant),
    () => {
      code.value = "";
    },
  );
}
function changeMethod() {
  if (locked.value) return;
  clear();
  recovery.value = !recovery.value;
}
async function resend() {
  if (locked.value || !request.value || seconds.value) return;
  const id = request.value.request_id;
  await send.submit(
    () => resendRecoveryCode(id),
    (value) => {
      request.value = value;
      code.value = "";
    },
  );
}
watch(session.generation, clear);
onBeforeUnmount(() => {
  alive = false;
  clearInterval(timer);
  clear();
  emit("busy", false);
});
</script>

<template>
  <div class="account-form">
    <template v-if="!recovery">
      <p class="form-note">使用已有 Passkey 验证身份。</p>
      <UiButton
        variant="primary"
        :loading="prompt || verify.busy.value"
        :disabled="locked"
        @click="authenticate"
        >使用 Passkey 验证</UiButton
      >
      <UiButton v-if="prompt && !verify.busy.value" @click="cancelPrompt"
        >取消验证</UiButton
      >
      <UiButton
        v-if="recoveryAllowed"
        variant="ghost"
        :disabled="locked"
        @click="changeMethod"
        >无法使用已有 Passkey？</UiButton
      >
    </template>
    <template v-else-if="recoveryAllowed">
      <p v-if="emailInitializationRequired" class="form-note">
        当前邮箱尚未验证，请联系管理员完成邮箱初始化后再添加 Passkey。
      </p>
      <template v-else-if="!request">
        <p class="form-note">验证当前密码，并接收原邮箱验证码。</p>
        <p v-if="hasPasskeys" class="form-note">
          恢复完成后，已有 Passkey 将被撤销。
        </p>
        <UiInput
          v-model="password"
          label="当前密码"
          type="password"
          autocomplete="current-password"
          :disabled="locked"
          :error="fieldError"
          @keydown.enter.prevent="sendCode"
        />
        <UiButton
          variant="primary"
          :loading="send.busy.value"
          :disabled="locked"
          @click="sendCode"
          >发送验证码</UiButton
        >
      </template>
      <template v-else>
        <p class="form-note">验证请求已受理，请检查原邮箱。</p>
        <UiInput
          v-model="code"
          label="邮箱验证码"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          :disabled="locked"
          :error="fieldError"
          @keydown.enter.prevent="confirmCode"
        />
        <UiButton
          variant="primary"
          :loading="verify.busy.value"
          :disabled="locked"
          @click="confirmCode"
          >验证并继续</UiButton
        >
        <UiButton :disabled="locked || seconds > 0" @click="resend">{{
          seconds ? `${seconds} 秒后可重发` : "重新发送验证码"
        }}</UiButton>
      </template>
      <UiButton
        v-if="hasPasskeys && !recoveryOnly"
        variant="ghost"
        :disabled="locked"
        @click="changeMethod"
        >使用 Passkey 验证</UiButton
      >
    </template>
    <p v-else class="form-note">请先在“登录与安全”中添加 Passkey。</p>
    <p v-if="localError" class="form-feedback" role="alert">
      {{ passkeyErrorMessage(localError) }}
    </p>
    <CommandFeedback
      :error="send.error.value"
      :message="
        send.error.value ? mailRequestErrorMessage(send.error.value) : undefined
      "
      :uncertain="send.uncertain.value"
      :replayable="send.replayable.value"
      :busy="send.busy.value"
      @retry="send.retry"
      @abandon="send.abandon"
    />
    <CommandFeedback
      :error="verify.error.value"
      :message="
        verify.error.value ? passkeyErrorMessage(verify.error.value) : undefined
      "
      :uncertain="verify.uncertain.value"
      :replayable="verify.replayable.value"
      :busy="verify.busy.value"
      @retry="verify.retry"
      @abandon="verify.abandon"
    />
  </div>
</template>
