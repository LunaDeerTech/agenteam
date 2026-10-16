<script setup lang="ts">
import { nextTick, watch } from "vue";
import { RouterLink, RouterView, useRoute } from "vue-router";
import AppShell from "./layouts/AppShell.vue";
import AccountShell from "./layouts/AccountShell.vue";
import LeaveConfirmation from "./components/account/LeaveConfirmation.vue";
import { UiButton } from "./components/ui";
import { accountEnabled, accountErrorMessage } from "./account/transport";
import { useSession } from "./account/session";
import { router } from "./router";

const route = useRoute();
const session = useSession();
watch(session.status, (value) => {
  if (
    accountEnabled &&
    route.matched.length > 0 &&
    value === "anonymous" &&
    route.meta.layout !== "account"
  )
    void router.replace({ name: "login", query: { return: route.path } });
});
watch(
  () => route.fullPath,
  async (_next, previous) => {
    if (!previous) return;
    await nextTick();
    requestAnimationFrame(() =>
      document.getElementById("page-title")?.focus({ preventScroll: true }),
    );
  },
);
</script>

<template>
  <component
    v-if="
      !accountEnabled ||
      route.meta.layout === 'account' ||
      session.hasIdentity.value
    "
    v-show="
      !accountEnabled ||
      route.meta.layout === 'account' ||
      session.status.value === 'authenticated'
    "
    :is="route.meta.layout === 'account' ? AccountShell : AppShell"
  >
    <RouterView :key="route.path" />
  </component>
  <main
    v-if="
      accountEnabled &&
      route.meta.layout !== 'account' &&
      session.status.value !== 'authenticated'
    "
    class="session-feedback"
  >
    <h1 v-if="session.confirmedAction.value === 'password'">密码已修改</h1>
    <h1 v-else>
      {{
        session.status.value === "checking"
          ? "正在确认登录状态"
          : "暂时无法确认登录状态"
      }}
    </h1>
    <p v-if="session.confirmedAction.value === 'password'">
      其他登录会话已退出。请重新确认本次登录状态，或使用新密码重新登录。
    </p>
    <p v-if="session.problem.value" role="alert">
      {{ accountErrorMessage(session.problem.value) }}
    </p>
    <UiButton
      :loading="session.status.value === 'checking'"
      @click="session.refresh"
      >重新检查登录状态</UiButton
    >
    <RouterLink
      v-if="
        session.confirmedAction.value === 'password' &&
        session.status.value !== 'checking'
      "
      class="form-link"
      to="/login"
      >使用新密码重新登录</RouterLink
    >
  </main>
  <LeaveConfirmation />
</template>
