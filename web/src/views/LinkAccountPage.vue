<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import {
  anonymousRequest,
  browserCommand,
  refreshSession,
  useSession,
} from "../account/session";
import { accountEnabled, accountErrorMessage } from "../account/transport";
import { isRecord } from "../account/types";
import { takeAccountLink, ownAccountLink } from "../account/link";
import { useCommand } from "../account/useCommand";
import { confirmAccountLeave, useLeaveProtection } from "../account/leave";
import {
  usernameError,
  displayNameError,
  passwordError,
} from "../account/validation";
const invitation = useRoute().name === "invite";
const kind = invitation ? "invite" : "reset-password";
const session = useSession();
let token = takeAccountLink(kind);
let pageGeneration = 0;
let alive = true;
const inspecting = ref(false);
const valid = ref(false);
const invalid = ref(false);
const inspectError = shallowRef<unknown>();
const email = ref("");
const username = ref("");
const displayName = ref("");
const password = ref("");
const confirmation = ref("");
const fields = ref({
  username: "",
  display_name: "",
  password: "",
  confirmation: "",
});
const operation = useCommand<unknown>();
const { busy, error, uncertain, replayable, confirmed } = operation;
const dirty = computed(
  () =>
    !!username.value ||
    !!displayName.value ||
    !!password.value ||
    !!confirmation.value ||
    uncertain.value ||
    busy.value,
);
useLeaveProtection(() => dirty.value);
function clearSensitive() {
  token = undefined;
  password.value = "";
  confirmation.value = "";
}
function clearDraft() {
  clearSensitive();
  username.value = "";
  displayName.value = "";
  email.value = "";
  operation.abandon();
  valid.value = false;
}
async function inspect() {
  if (!token || !accountEnabled) {
    invalid.value = !token;
    return;
  }
  const generation = ++pageGeneration;
  inspecting.value = true;
  inspectError.value = undefined;
  invalid.value = false;
  valid.value = false;
  try {
    const value = await anonymousRequest(
      invitation ? "/invitations/inspect" : "/password-resets/inspect",
      {
        method: "POST",
        body: JSON.stringify({ token }),
        current: () => alive && generation === pageGeneration,
      },
      (value): value is { email?: string; expires_at: string; valid?: true } =>
        isRecord(value) &&
        typeof value.expires_at === "string" &&
        (invitation ? typeof value.email === "string" : value.valid === true),
    );
    if (alive && generation === pageGeneration) {
      valid.value = true;
      email.value = value.email ?? "";
    }
  } catch (failure) {
    if (alive && generation === pageGeneration) inspectError.value = failure;
  } finally {
    if (alive && generation === pageGeneration) inspecting.value = false;
  }
}
const release = ownAccountLink(kind, (nextToken) => {
  void (async () => {
    const accepted = await confirmAccountLeave();
    if (!alive || !accepted) return;
    pageGeneration++;
    clearDraft();
    token = nextToken;
    await inspect();
  })();
});
async function submit() {
  if (
    !valid.value ||
    !token ||
    !accountEnabled ||
    uncertain.value ||
    busy.value
  )
    return;
  fields.value = {
    username: invitation ? usernameError(username.value) : "",
    display_name: invitation ? displayNameError(displayName.value) : "",
    password: passwordError(password.value),
    confirmation:
      password.value === confirmation.value ? "" : "两次输入的密码不一致。",
  };
  if (Object.values(fields.value).some(Boolean)) return;
  const body = invitation
    ? {
        token,
        username: username.value,
        display_name: displayName.value,
        password: password.value,
        confirmation: confirmation.value,
      }
    : { token, new_password: password.value, confirmation: confirmation.value };
  await operation.submit(
    () =>
      browserCommand(
        invitation ? "/invitations/redeem" : "/password-resets/complete",
        {
          method: "POST",
          status: invitation ? 201 : 204,
          body: JSON.stringify(body),
        },
        invitation
          ? (value): value is unknown =>
              isRecord(value) &&
              value.completed === true &&
              value.login_required === true
          : undefined,
      ),
    async () => {
      username.value = "";
      displayName.value = "";
      if (!invitation) await refreshSession();
    },
    clearSensitive,
  );
}
function abandon() {
  pageGeneration++;
  clearDraft();
  invalid.value = true;
}
onMounted(inspect);
onBeforeUnmount(() => {
  alive = false;
  pageGeneration++;
  clearSensitive();
  release();
});
</script>
<template>
  <div class="account-form">
    <p v-if="!accountEnabled" class="form-feedback">
      这是界面预览。安全账号入口就绪后可使用链接。
    </p>
    <template v-else-if="confirmed">
      <p role="status" class="form-feedback">
        {{
          invitation
            ? "账号已创建，请登录后继续。"
            : "密码已重置，该账号的旧登录会话已失效。请使用新密码登录。"
        }}
      </p>
      <RouterLink class="form-link" to="/login">前往登录</RouterLink>
    </template>
    <template v-else-if="!valid">
      <p v-if="inspecting" role="status" class="form-note">正在检查链接…</p>
      <p v-else-if="invalid" class="form-feedback">
        {{
          invitation
            ? "链接无效或已不在此页面。请重新打开邀请链接，或联系管理员。"
            : "链接无效或已不在此页面。请重新打开重置链接，或申请新的链接。"
        }}
      </p>
      <p v-else-if="inspectError" role="alert" class="form-feedback">
        {{ accountErrorMessage(inspectError) }}
      </p>
      <UiButton v-if="inspectError" :loading="inspecting" @click="inspect"
        >重新检查链接</UiButton
      >
      <RouterLink v-if="!invitation" class="form-link" to="/forgot-password"
        >重新找回密码</RouterLink
      >
      <RouterLink
        class="form-link"
        :to="session.status.value === 'authenticated' ? '/' : '/login'"
        >{{
          session.status.value === "authenticated" ? "返回当前账号" : "返回登录"
        }}</RouterLink
      >
    </template>
    <form v-else class="account-form" novalidate @submit.prevent="submit">
      <fieldset :disabled="busy || uncertain">
        <template v-if="invitation">
          <UiInput
            v-model="email"
            label="邀请邮箱"
            readonly
            autocomplete="email"
          />
          <UiInput
            v-model="username"
            label="用户名"
            autocomplete="username"
            :error="fields.username"
            hint="3–32 位字母、数字或中间连字符。"
            required
          />
          <UiInput
            v-model="displayName"
            label="显示名"
            autocomplete="nickname"
            :error="fields.display_name"
            hint="可选；留空时显示邮箱。"
          />
        </template>
        <UiInput
          v-model="password"
          :label="invitation ? '密码' : '新密码'"
          type="password"
          autocomplete="new-password"
          :error="fields.password"
          hint="使用 15–128 个字符。"
          required
        />
        <UiInput
          v-model="confirmation"
          label="确认密码"
          type="password"
          autocomplete="new-password"
          :error="fields.confirmation"
          required
        />
      </fieldset>
      <CommandFeedback
        :error="error"
        :busy="busy"
        :uncertain="uncertain"
        :replayable="replayable"
        @retry="operation.retry"
        @abandon="abandon"
      />
      <UiButton
        class="account-submit"
        type="submit"
        variant="primary"
        :loading="busy"
        :disabled="uncertain"
        >{{ invitation ? "接受邀请" : "重置密码" }}</UiButton
      >
      <RouterLink class="form-link" to="/login">返回登录</RouterLink>
    </form>
  </div>
</template>
