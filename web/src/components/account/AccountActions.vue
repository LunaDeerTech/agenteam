<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { UiButton, UiMenu, UiDialog, UiIcon } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import { logoutCommand, useSession } from "../../account/session";
import { useCommand } from "../../account/useCommand";
import { confirmAccountLeave } from "../../account/leave";
const router = useRouter();
const session = useSession();
const open = ref(false);
const operation = useCommand<void>();
const { busy, error, uncertain, replayable } = operation;
async function select(id: string) {
  if (id === "profile") await router.push("/settings/profile");
  if (id === "logout" && (await confirmAccountLeave())) open.value = true;
}
async function logout() {
  await operation.submit(logoutCommand, async () => {
    open.value = false;
    await router.replace("/login");
  });
}
</script>
<template>
  <UiMenu
    label="账号"
    :items="[
      {
        id: 'profile',
        label:
          session.user.value?.display_name ||
          session.user.value?.email ||
          '个人设置',
      },
      { id: 'logout', label: '退出登录' },
    ]"
    @select="select"
    ><template #trigger><UiIcon name="user" :size="16" /></template
  ></UiMenu>
  <UiDialog
    :open="open"
    title="退出登录"
    :close-on-outside="!busy && !uncertain"
    :close-on-escape="!busy && !uncertain"
    @update:open="!busy && !uncertain && (open = $event)"
  >
    <p class="form-note">退出当前账号后，需要重新登录才能访问应用。</p>
    <CommandFeedback
      :error="error"
      :busy="busy"
      :uncertain="uncertain"
      :replayable="replayable"
      @retry="operation.retry"
      @abandon="
        operation.abandon();
        open = false;
      "
    />
    <template #footer
      ><UiButton :disabled="busy || uncertain" @click="open = false"
        >取消</UiButton
      ><UiButton
        variant="primary"
        :loading="busy"
        :disabled="uncertain"
        @click="logout"
        >退出登录</UiButton
      ></template
    >
  </UiDialog>
</template>
