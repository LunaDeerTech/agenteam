<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { UiButton, UiMenu, UiDialog, UiIcon } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import { logoutCommand } from "../../account/session";
import {
  refreshAccountAvatar,
  rejectAvatarImage,
  useAccountAvatar,
} from "../../account/avatar";
import { useCommand } from "../../account/useCommand";
import { confirmAccountLeave } from "../../account/leave";
const router = useRouter();
const avatar = useAccountAvatar();
const items = computed(() => [
  { id: "profile", label: "个人设置" },
  ...(avatar.error.value
    ? [
        {
          id: "reload-avatar",
          label: "重新读取头像",
          disabled: avatar.loading.value,
        },
      ]
    : []),
  { id: "logout", label: "退出登录" },
]);
const open = ref(false);
const operation = useCommand<void>();
const { busy, error, uncertain, replayable } = operation;
async function select(id: string) {
  if (id === "profile") await router.push("/settings/profile");
  if (id === "reload-avatar") await refreshAccountAvatar();
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
  <UiMenu label="账号" :items="items" @select="select"
    ><template #trigger>
      <span class="account-avatar" aria-hidden="true">
        <img
          v-if="avatar.url.value"
          :src="avatar.url.value"
          alt=""
          @error="rejectAvatarImage(($event.target as HTMLImageElement).src)"
        />
        <UiIcon v-else name="user" :size="16" />
      </span> </template
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

<style scoped>
.account-avatar {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  overflow: hidden;
  border-radius: 50%;
  background: var(--surface-subtle);
}
.account-avatar img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
</style>
