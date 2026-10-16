<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import { useLeaveProtection } from "../account/leave";
import {
  passwordCommand,
  refreshSession,
  useSession,
} from "../account/session";
import { AccountError, accountEnabled } from "../account/transport";
import { useCommand } from "../account/useCommand";
import { passwordError } from "../account/validation";

const session = useSession();
const currentPassword = ref("");
const newPassword = ref("");
const confirmation = ref("");
const version = ref(session.user.value?.version ?? "");
const submitted = ref(false);
const abandoned = ref(false);
const refreshing = ref(false);
const save = useCommand<unknown>();
const dirty = computed(
  () => !!(currentPassword.value || newPassword.value || confirmation.value),
);
const locked = computed(
  () =>
    !accountEnabled ||
    save.busy.value ||
    save.uncertain.value ||
    save.confirmed.value ||
    !version.value,
);
const conflict = computed(
  () =>
    save.error.value instanceof AccountError &&
    save.error.value.code === "VERSION_CONFLICT",
);
function fieldError(paths: string[], local: string) {
  if (submitted.value && local) return local;
  return save.error.value instanceof AccountError &&
    save.error.value.fields.some((field) => paths.includes(field.path))
    ? "请检查此项内容后再试。"
    : "";
}
const currentIssue = computed(() =>
  fieldError(
    ["/current_password"],
    currentPassword.value ? "" : "请输入当前密码。",
  ),
);
const newIssue = computed(() =>
  fieldError(["/new_password", "/password"], passwordError(newPassword.value)),
);
const confirmationIssue = computed(() =>
  fieldError(
    ["/confirmation"],
    confirmation.value === newPassword.value ? "" : "两次输入的密码不一致。",
  ),
);
function clearPasswords() {
  currentPassword.value = "";
  newPassword.value = "";
  confirmation.value = "";
  submitted.value = false;
}
function cancel() {
  clearPasswords();
  save.abandon();
  version.value = session.user.value?.version ?? "";
}
async function submit() {
  if (locked.value || conflict.value) return;
  submitted.value = true;
  abandoned.value = false;
  if (
    !currentPassword.value ||
    passwordError(newPassword.value) ||
    confirmation.value !== newPassword.value
  )
    return;
  await save.submit(
    () =>
      passwordCommand({
        version: version.value,
        current_password: currentPassword.value,
        new_password: newPassword.value,
        confirmation: confirmation.value,
      }),
    undefined,
    clearPasswords,
  );
}
async function confirmSession() {
  if (refreshing.value) return;
  refreshing.value = true;
  try {
    await refreshSession();
  } finally {
    refreshing.value = false;
  }
}
async function readLatestVersion() {
  await confirmSession();
  if (session.user.value) {
    version.value = session.user.value.version;
    save.abandon();
  }
}
function abandonSave() {
  cancel();
  abandoned.value = true;
}
watch(session.user, (user) => {
  if (user && !dirty.value) version.value = user.version;
});
watch(session.generation, clearPasswords);
useLeaveProtection(
  () => dirty.value || save.busy.value || save.uncertain.value,
);
onBeforeUnmount(clearPasswords);
</script>

<template>
  <form class="account-form" @submit.prevent="submit">
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览。账号服务连接后可修改密码。
    </p>
    <template v-if="save.confirmed.value">
      <div class="form-feedback form-success" role="status">
        <p>密码已修改，其他登录会话已退出。</p>
        <p v-if="save.busy.value">正在确认本次登录状态…</p>
        <p v-else-if="session.status.value === 'authenticated'">
          本次登录状态已确认，可以继续使用。
        </p>
        <template v-else>
          <p>密码已修改，请重新确认登录状态或使用新密码登录。</p>
          <div class="form-actions">
            <UiButton :loading="refreshing" @click="confirmSession"
              >重新确认登录状态</UiButton
            >
            <RouterLink class="form-link" to="/login">重新登录</RouterLink>
          </div>
        </template>
      </div>
    </template>
    <template v-else>
      <fieldset :disabled="locked">
        <UiInput
          v-model="currentPassword"
          label="当前密码"
          type="password"
          autocomplete="current-password"
          :error="currentIssue"
        />
        <UiInput
          v-model="newPassword"
          label="新密码"
          type="password"
          autocomplete="new-password"
          :error="newIssue"
          hint="使用 15–128 个字符，避免常见或容易猜到的密码。"
        />
        <UiInput
          v-model="confirmation"
          label="确认新密码"
          type="password"
          autocomplete="new-password"
          :error="confirmationIssue"
        />
      </fieldset>
      <p class="form-note">
        修改成功后，其他设备上的登录会话将退出。本次登录会话会重新确认。
      </p>
      <CommandFeedback
        :error="save.error.value"
        :uncertain="save.uncertain.value"
        :replayable="save.replayable.value"
        :busy="save.busy.value"
        @retry="save.retry"
        @abandon="abandonSave"
      />
      <p v-if="save.uncertain.value || abandoned" class="form-note">
        原改密结果尚未确认，旧登录可能已失效。重新登录不会确认原操作结果；如需登录，请使用实际生效的密码。
      </p>
      <div v-if="conflict" class="form-feedback">
        <p>资料已发生变化，已保留密码输入。重新读取当前版本后再提交。</p>
        <UiButton :loading="refreshing" @click="readLatestVersion"
          >读取并采用当前版本</UiButton
        >
      </div>
      <div class="form-actions">
        <UiButton
          variant="primary"
          type="submit"
          :loading="save.busy.value"
          :disabled="locked || !dirty || conflict"
          >修改密码</UiButton
        >
        <UiButton :disabled="locked || !dirty" @click="cancel">取消</UiButton>
      </div>
    </template>
  </form>
</template>
