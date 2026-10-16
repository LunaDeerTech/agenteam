<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog, UiInput } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import {
  authenticatedRequest,
  updateSessionUser,
  useSession,
} from "../../account/session";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import { isProfile } from "../../account/types";
import { AccountError } from "../../account/transport";
import {
  confirmEmailRecovery,
  mailCodeError,
  mailRequestErrorMessage,
  passkeyErrorMessage,
  requestEmailRecovery,
  resendEmailRecovery,
  type EmailChangeReceipt,
  type MailCodeRequest,
} from "../../account/passkeys";

const props = defineProps<{ open: boolean; initialization: boolean }>();
const emit = defineEmits<{ "update:open": [value: boolean]; changed: [] }>();
const session = useSession();
const authorization = ref("");
const password = ref("");
const code = ref("");
const issue = ref("");
const request = shallowRef<MailCodeRequest>();
const reading = ref(false);
const readError = shallowRef<unknown>();
const codeForm = ref<HTMLElement>();
const now = ref(Date.now());
const send = useCommand<MailCodeRequest>();
const confirm = useCommand<EmailChangeReceipt>();
let timer: ReturnType<typeof setInterval> | undefined;
let revision = 0;
let alive = true;
const locked = computed(
  () =>
    reading.value ||
    send.busy.value ||
    send.uncertain.value ||
    confirm.busy.value ||
    confirm.uncertain.value,
);
const conflict = computed(
  () =>
    confirm.error.value instanceof AccountError &&
    confirm.error.value.code === "VERSION_CONFLICT",
);
const seconds = computed(() =>
  request.value
    ? Math.max(
        0,
        Math.ceil((Date.parse(request.value.resend_after) - now.value) / 1000),
      )
    : 0,
);
function clear() {
  revision++;
  authorization.value = "";
  password.value = "";
  code.value = "";
  issue.value = "";
  request.value = undefined;
  reading.value = false;
  readError.value = undefined;
  send.abandon();
  confirm.abandon();
}
function close() {
  if (locked.value) return;
  clear();
  emit("update:open", false);
}
async function sendCode() {
  if (locked.value || request.value) return;
  issue.value = !authorization.value.trim()
    ? "请输入部署者提供的一次性授权。"
    : !password.value
      ? "请输入当前密码。"
      : "";
  if (issue.value) return;
  await send.submit(
    () => requestEmailRecovery(authorization.value.trim(), password.value),
    (value) => {
      request.value = value;
    },
    () => {
      authorization.value = "";
      password.value = "";
    },
  );
}
async function resend() {
  if (locked.value || !request.value || seconds.value || conflict.value) return;
  const id = request.value.request_id;
  await send.submit(
    () => resendEmailRecovery(id),
    (value) => {
      request.value = value;
      code.value = "";
    },
  );
}
async function readProfile() {
  if (reading.value) return;
  const owner = ++revision;
  const identity = session.generation.value;
  const current = () =>
    alive &&
    props.open &&
    owner === revision &&
    identity === session.generation.value;
  reading.value = true;
  readError.value = undefined;
  try {
    const profile = await authenticatedRequest("/me", { current }, isProfile);
    if (!current()) return;
    updateSessionUser(profile.user);
    emit("changed");
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
    () => confirmEmailRecovery(id, code.value),
    readProfile,
    () => {
      code.value = "";
    },
  );
}
watch(
  () => !!request.value && !locked.value,
  async (ready) => {
    if (ready) {
      await nextTick();
      codeForm.value?.querySelector<HTMLInputElement>("input")?.focus();
    }
  },
);
watch(
  () => props.open,
  (open) => {
    clear();
    if (timer) clearInterval(timer);
    timer = undefined;
    if (open) {
      now.value = Date.now();
      timer = setInterval(() => {
        now.value = Date.now();
      }, 1000);
    }
  },
  { immediate: true },
);
watch(session.generation, () => {
  clear();
  emit("update:open", false);
});
useLeaveProtection(
  () =>
    props.open &&
    (locked.value ||
      !!authorization.value ||
      !!password.value ||
      !!request.value),
);
onBeforeUnmount(() => {
  alive = false;
  clear();
  if (timer) clearInterval(timer);
});
</script>

<template>
  <UiDialog
    :open="open"
    :title="initialization ? '初始化邮箱' : '恢复邮箱'"
    :close-on-escape="!locked"
    :close-on-outside="!locked"
    @update:open="
      (value) => {
        if (!value) close();
      }
    "
  >
    <p
      v-if="confirm.confirmed.value"
      class="form-feedback form-success"
      role="status"
    >
      邮箱已验证。添加 Passkey 时，还需使用当前密码和邮箱验证码验证。
    </p>
    <div v-else-if="!request" class="account-form">
      <p class="form-note">
        请向部署者获取绑定当前账号和邮箱的一次性授权。验证码将发送到部署者授权的邮箱。
      </p>
      <p v-if="!initialization" class="form-note">
        恢复成功后，已有 Passkey 和其他登录会话将被撤销。
      </p>
      <UiInput
        v-model="authorization"
        label="一次性授权"
        type="password"
        autocomplete="off"
        :disabled="locked"
      />
      <UiInput
        v-model="password"
        label="当前密码"
        type="password"
        autocomplete="current-password"
        :disabled="locked"
        :error="issue"
        @keydown.enter.prevent="sendCode"
      />
      <UiButton
        variant="primary"
        :disabled="locked"
        :loading="send.busy.value"
        @click="sendCode"
        >发送验证码</UiButton
      >
    </div>
    <div v-else ref="codeForm" class="account-form">
      <p class="form-note">验证请求已受理，请检查部署者授权的邮箱。</p>
      <UiInput
        v-model="code"
        label="邮箱验证码"
        inputmode="numeric"
        autocomplete="one-time-code"
        maxlength="6"
        :disabled="locked || conflict"
        :error="issue"
        @keydown.enter.prevent="submitCode"
      />
      <UiButton
        variant="primary"
        :disabled="locked || conflict"
        :loading="confirm.busy.value"
        @click="submitCode"
        >验证邮箱</UiButton
      >
      <UiButton :disabled="locked || conflict || seconds > 0" @click="resend">{{
        seconds ? `${seconds} 秒后可重发` : "重新发送验证码"
      }}</UiButton>
    </div>
    <CommandFeedback
      :error="send.error.value"
      :message="
        send.error.value ? mailRequestErrorMessage(send.error.value) : undefined
      "
      :uncertain="send.uncertain.value"
      :replayable="send.replayable.value"
      :busy="send.busy.value"
      @retry="send.retry"
      @abandon="clear"
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
      @abandon="clear"
    />
    <p v-if="conflict" class="form-feedback" role="alert">
      账号资料已变化，请重新获取授权后开始验证。
    </p>
    <div v-if="readError" class="form-feedback" role="alert">
      <p>邮箱验证已确认，资料尚未读回。</p>
      <UiButton :loading="reading" @click="readProfile">重新读取资料</UiButton>
    </div>
    <template #footer
      ><UiButton :disabled="locked" @click="close">{{
        confirm.confirmed.value ? "完成" : "取消"
      }}</UiButton></template
    >
  </UiDialog>
</template>
