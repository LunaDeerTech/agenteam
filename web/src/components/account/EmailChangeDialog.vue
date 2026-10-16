<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { useRouter } from "vue-router";
import { UiButton, UiDialog, UiInput } from "../ui";
import PasskeyVerification from "./PasskeyVerification.vue";
import CommandFeedback from "./CommandFeedback.vue";
import { AccountError } from "../../account/transport";
import {
  authenticatedRequest,
  updateSessionUser,
  useSession,
} from "../../account/session";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import { isProfile, type ProfileResponse } from "../../account/types";
import { emailError } from "../../account/validation";
import { browserSupportsPasskeys } from "../../account/webauthn";
import {
  confirmEmailChange,
  getPasskeyCapabilities,
  getPasskeys,
  mailCodeError,
  mailRequestErrorMessage,
  passkeyErrorMessage,
  passkeyUnavailableMessage,
  requestEmailChange,
  resendEmailChange,
  type EmailChangeReceipt,
  type MailCodeRequest,
  type PasskeyCapabilities,
  type PasskeyGrant,
  type PasskeyList,
} from "../../account/passkeys";

const props = defineProps<{ open: boolean; version: string }>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  changed: [profile: ProfileResponse];
}>();
const router = useRouter();
const session = useSession();
const list = shallowRef<PasskeyList>();
const capability = shallowRef<PasskeyCapabilities>();
const grant = shallowRef<PasskeyGrant>();
const request = shallowRef<MailCodeRequest>();
const email = ref("");
const pendingEmail = ref("");
const code = ref("");
const issue = ref("");
const expectedVersion = ref("");
const latestVersion = ref("");
const reading = ref(false);
const verificationBusy = ref(false);
const readError = shallowRef<unknown>();
const now = ref(Date.now());
const send = useCommand<MailCodeRequest>();
const confirm = useCommand<EmailChangeReceipt>();
const grantForm = ref<HTMLElement>();
const codeForm = ref<HTMLElement>();
let timer: ReturnType<typeof setInterval> | undefined;
let revision = 0;
let alive = true;
const locked = computed(
  () =>
    reading.value ||
    verificationBusy.value ||
    send.busy.value ||
    send.uncertain.value ||
    confirm.busy.value ||
    confirm.uncertain.value,
);
const available = computed(
  () => capability.value?.available === true && browserSupportsPasskeys(),
);
const conflict = computed(() =>
  [send.error.value, confirm.error.value].some(
    (error) =>
      error instanceof AccountError && error.code === "VERSION_CONFLICT",
  ),
);
const seconds = computed(() =>
  request.value
    ? Math.max(
        0,
        Math.ceil((Date.parse(request.value.resend_after) - now.value) / 1000),
      )
    : 0,
);
watch(
  () => !locked.value && (request.value ? "code" : grant.value ? "email" : ""),
  async (step) => {
    if (!step) return;
    await nextTick();
    (step === "code" ? codeForm.value : grantForm.value)
      ?.querySelector<HTMLInputElement>("input")
      ?.focus();
  },
);
function clearProof() {
  grant.value = undefined;
  request.value = undefined;
  code.value = "";
  pendingEmail.value = "";
  issue.value = "";
  send.abandon();
  confirm.abandon();
}
async function load() {
  if (!props.open || reading.value) return;
  const owner = ++revision;
  const generation = session.generation.value;
  const current = () =>
    alive &&
    props.open &&
    owner === revision &&
    generation === session.generation.value;
  reading.value = true;
  readError.value = undefined;
  try {
    const available = await getPasskeyCapabilities({ current });
    const value = await getPasskeys({ current });
    if (!current()) return;
    capability.value = available;
    list.value = value;
  } catch (error) {
    if (current()) readError.value = error;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
function close() {
  if (locked.value) return;
  revision++;
  clearProof();
  emit("update:open", false);
}
async function goToSecurity() {
  close();
  await nextTick();
  await router.push("/settings/password");
}
async function sendCode() {
  if (locked.value || !grant.value || conflict.value) return;
  issue.value = emailError(email.value);
  if (issue.value) return;
  const address = email.value;
  const proof = grant.value.grant_id;
  await send.submit(
    () => requestEmailChange(proof, address, expectedVersion.value),
    (value) => {
      request.value = value;
      pendingEmail.value = address;
      code.value = "";
      grant.value = undefined;
    },
  );
  if (!send.uncertain.value && !send.confirmed.value) grant.value = undefined;
}
async function resend() {
  if (locked.value || !request.value || seconds.value || conflict.value) return;
  const id = request.value.request_id;
  await send.submit(
    () => resendEmailChange(id),
    (value) => {
      request.value = value;
      code.value = "";
    },
  );
}
async function readProfile() {
  if (reading.value) return;
  const owner = ++revision;
  const generation = session.generation.value;
  const current = () =>
    alive &&
    props.open &&
    owner === revision &&
    generation === session.generation.value;
  reading.value = true;
  readError.value = undefined;
  try {
    const profile = await authenticatedRequest("/me", { current }, isProfile);
    if (!current()) return;
    if (confirm.confirmed.value) {
      updateSessionUser(profile.user);
      emit("changed", profile);
    } else latestVersion.value = profile.user.version;
  } catch (error) {
    if (current()) readError.value = error;
  } finally {
    if (owner === revision) reading.value = false;
  }
}
async function submitCode() {
  if (locked.value || !request.value || conflict.value) return;
  issue.value = mailCodeError(code.value);
  if (issue.value) return;
  const id = request.value.request_id;
  await confirm.submit(
    () => confirmEmailChange(id, code.value),
    readProfile,
    () => {
      code.value = "";
    },
  );
}
function adoptVersion() {
  if (locked.value || !latestVersion.value) return;
  expectedVersion.value = latestVersion.value;
  latestVersion.value = "";
  clearProof();
}
function restart() {
  if (locked.value) return;
  clearProof();
}
function abandonSend() {
  send.abandon();
  grant.value = undefined;
}
function abandonConfirm() {
  confirm.abandon();
  clearProof();
  readError.value = new AccountError("NETWORK_ERROR");
}
watch(
  () => props.open,
  (open) => {
    if (timer) clearInterval(timer);
    timer = undefined;
    if (open) {
      clearProof();
      list.value = undefined;
      capability.value = undefined;
      expectedVersion.value = props.version;
      latestVersion.value = "";
      now.value = Date.now();
      timer = setInterval(() => {
        now.value = Date.now();
      }, 1000);
      void load();
    } else {
      revision++;
      reading.value = false;
      clearProof();
    }
  },
  { immediate: true },
);
watch(session.generation, () => {
  revision++;
  clearProof();
  email.value = "";
  list.value = undefined;
  capability.value = undefined;
  emit("update:open", false);
});
useLeaveProtection(
  () => props.open && (locked.value || !!email.value || !!request.value),
);
onBeforeUnmount(() => {
  alive = false;
  revision++;
  clearProof();
  email.value = "";
  if (timer) clearInterval(timer);
});
</script>

<template>
  <UiDialog
    :open="open"
    title="修改邮箱"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="
      (value) => {
        if (!value) close();
      }
    "
  >
    <p v-if="reading && !list" class="form-note" role="status">
      正在读取验证方式…
    </p>
    <div v-if="readError" class="form-feedback" role="alert">
      <p>{{ passkeyErrorMessage(readError) }}</p>
      <UiButton
        :loading="reading"
        @click="confirm.confirmed.value || conflict ? readProfile() : load()"
        >重新读取</UiButton
      >
    </div>
    <p
      v-if="confirm.confirmed.value"
      class="form-feedback form-success"
      role="status"
    >
      邮箱已修改。
    </p>
    <template v-else-if="list">
      <p v-if="!available" class="form-note">
        {{ passkeyUnavailableMessage(capability) }}
      </p>
      <div v-else-if="!list.items.length" class="account-form">
        <p class="form-note">修改邮箱前，请先添加 Passkey。</p>
        <UiButton @click="goToSecurity">前往登录与安全</UiButton>
      </div>
      <template v-else-if="!conflict">
        <PasskeyVerification
          v-if="!grant && !request && !send.uncertain.value"
          :intent="{ purpose: 'change_email' }"
          :has-passkeys="true"
          :email-initialization-required="list.email_initialization_required"
          @verified="(value) => (grant = value)"
          @busy="(value) => (verificationBusy = value)"
        />
        <div v-if="grant" ref="grantForm" class="account-form">
          <UiInput
            v-model="email"
            label="新邮箱"
            type="email"
            autocomplete="email"
            :disabled="locked"
            :error="issue"
            @keydown.enter.prevent="sendCode"
          /><UiButton
            variant="primary"
            :disabled="locked"
            :loading="send.busy.value"
            @click="sendCode"
            >发送验证码</UiButton
          >
        </div>
        <div v-if="request" ref="codeForm" class="account-form">
          <p class="form-note">
            验证请求已受理，请检查 {{ pendingEmail }}。验证成功后才会修改邮箱。
          </p>
          <UiInput
            v-model="code"
            label="新邮箱验证码"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            :disabled="locked"
            :error="issue"
            @keydown.enter.prevent="submitCode"
          /><UiButton
            variant="primary"
            :disabled="locked"
            :loading="confirm.busy.value"
            @click="submitCode"
            >验证并修改邮箱</UiButton
          >
          <div class="form-actions">
            <UiButton :disabled="locked || seconds > 0" @click="resend">{{
              seconds ? `${seconds} 秒后可重发` : "重新发送验证码"
            }}</UiButton
            ><UiButton :disabled="locked" @click="restart">更换新邮箱</UiButton>
          </div>
        </div>
      </template>
    </template>
    <CommandFeedback
      :error="send.error.value"
      :message="
        send.error.value ? mailRequestErrorMessage(send.error.value) : undefined
      "
      :uncertain="send.uncertain.value"
      :replayable="send.replayable.value"
      :busy="send.busy.value"
      @retry="send.retry"
      @abandon="abandonSend"
    />
    <CommandFeedback
      :error="confirm.error.value"
      :message="
        confirm.error.value
          ? passkeyErrorMessage(confirm.error.value)
          : undefined
      "
      :uncertain="confirm.uncertain.value"
      :replayable="confirm.replayable.value"
      :busy="confirm.busy.value"
      @retry="confirm.retry"
      @abandon="abandonConfirm"
    />
    <div v-if="conflict" class="form-feedback">
      <p>账号资料已变化，已保留新邮箱。读取当前版本后重新验证。</p>
      <UiButton :loading="reading" @click="readProfile">读取当前资料</UiButton
      ><UiButton v-if="latestVersion" :disabled="locked" @click="adoptVersion"
        >采用当前版本并重新验证</UiButton
      >
    </div>
    <template #footer
      ><UiButton :disabled="locked" @click="close">{{
        confirm.confirmed.value ? "完成" : "取消"
      }}</UiButton></template
    >
  </UiDialog>
</template>
