<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiInput } from "../components/ui";
import CommandFeedback from "../components/account/CommandFeedback.vue";
import AvatarEditor from "../components/account/AvatarEditor.vue";
import EmailChangeDialog from "../components/account/EmailChangeDialog.vue";
import { useLeaveProtection } from "../account/leave";
import {
  authenticatedRequest,
  sessionCommand,
  updateSessionUser,
  useSession,
} from "../account/session";
import {
  AccountError,
  accountEnabled,
  accountErrorMessage,
} from "../account/transport";
import { isProfile, type ProfileResponse } from "../account/types";
import { useCommand } from "../account/useCommand";
import { displayNameError, usernameError } from "../account/validation";

const session = useSession();
const profile = shallowRef<ProfileResponse>();
const baseline = shallowRef<ProfileResponse>();
const username = ref("");
const displayName = ref("");
const loading = ref(false);
const loadError = shallowRef<unknown>();
const submitted = ref(false);
const saved = ref(false);
const abandoned = ref(false);
const latestRead = ref(false);
const avatarBusy = ref(false);
const emailOpen = ref(false);
const save = useCommand<ProfileResponse>();
let alive = true;
let readGeneration = 0;
const dirty = computed(
  () =>
    !!baseline.value &&
    (username.value !== baseline.value.user.username ||
      displayName.value !== baseline.value.user.display_name),
);
const locked = computed(
  () =>
    !accountEnabled ||
    loading.value ||
    save.busy.value ||
    save.uncertain.value ||
    avatarBusy.value ||
    !baseline.value,
);
const conflict = computed(
  () =>
    save.error.value instanceof AccountError &&
    save.error.value.code === "VERSION_CONFLICT",
);
function fieldError(path: string, local: string) {
  if (submitted.value && local) return local;
  return save.error.value instanceof AccountError &&
    save.error.value.fields.some((field) => field.path === path)
    ? "请检查此项内容后再试。"
    : "";
}
const usernameIssue = computed(() =>
  fieldError(
    "/username",
    username.value === baseline.value?.user.username
      ? ""
      : usernameError(username.value),
  ),
);
const nameIssue = computed(() =>
  fieldError("/display_name", displayNameError(displayName.value)),
);
function resetDraft() {
  if (!profile.value) return;
  baseline.value = profile.value;
  username.value = profile.value.user.username;
  displayName.value = profile.value.user.display_name;
  submitted.value = false;
  saved.value = false;
  latestRead.value = false;
  save.abandon();
}
function acceptProfile(value: ProfileResponse) {
  const preserveDraft = dirty.value;
  profile.value = value;
  updateSessionUser(value.user);
  if (!preserveDraft) resetDraft();
}
async function load() {
  if (!accountEnabled || loading.value || save.uncertain.value) return;
  const owner = ++readGeneration;
  loading.value = true;
  loadError.value = undefined;
  try {
    const value = await authenticatedRequest(
      "/me",
      { current: () => alive && owner === readGeneration },
      isProfile,
    );
    if (!alive || owner !== readGeneration) return;
    acceptProfile(value);
    latestRead.value = conflict.value;
  } catch (error) {
    if (alive && owner === readGeneration) loadError.value = error;
  } finally {
    if (alive && owner === readGeneration) loading.value = false;
  }
}
function useLatestVersion() {
  if (!profile.value || !latestRead.value) return;
  baseline.value = profile.value;
  save.abandon();
  latestRead.value = false;
}
async function submit() {
  if (locked.value || !dirty.value || !baseline.value || conflict.value) return;
  submitted.value = true;
  saved.value = false;
  abandoned.value = false;
  if (
    (username.value !== baseline.value.user.username &&
      usernameError(username.value)) ||
    displayNameError(displayName.value)
  )
    return;
  const body = {
    version: baseline.value.user.version,
    ...(username.value !== baseline.value.user.username
      ? { username: username.value }
      : {}),
    ...(displayName.value !== baseline.value.user.display_name
      ? { display_name: displayName.value }
      : {}),
  };
  await save.submit(
    () =>
      sessionCommand(
        "/me",
        { method: "PATCH", body: JSON.stringify(body) },
        isProfile,
      ),
    async (value) => {
      profile.value = value;
      baseline.value = value;
      username.value = value.user.username;
      displayName.value = value.user.display_name;
      updateSessionUser(value.user);
      submitted.value = false;
      saved.value = true;
      if (
        session.user.value &&
        BigInt(session.user.value.version) > BigInt(value.user.version)
      ) {
        await load();
        saved.value = true;
      }
    },
  );
}
function abandonSave() {
  save.abandon();
  saved.value = false;
  abandoned.value = true;
  loadError.value = undefined;
  void load();
}
watch(
  [username, displayName],
  () => {
    saved.value = false;
  },
  { flush: "sync" },
);
watch(session.generation, () => {
  readGeneration++;
  profile.value = undefined;
  baseline.value = undefined;
  username.value = "";
  displayName.value = "";
  loading.value = false;
  saved.value = false;
});
watch(session.status, (status) => {
  if (status === "authenticated" && !baseline.value) void load();
});
watch(session.user, (user) => {
  if (
    user &&
    profile.value &&
    user.id === profile.value.user.id &&
    user.version !== profile.value.user.version &&
    !save.busy.value &&
    !save.uncertain.value &&
    !avatarBusy.value
  )
    void load();
});
useLeaveProtection(
  () => dirty.value || save.busy.value || save.uncertain.value,
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  readGeneration++;
  username.value = "";
  displayName.value = "";
});
</script>

<template>
  <div class="account-form">
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览。账号服务连接后可编辑个人资料。
    </p>
    <p v-else-if="loading && !profile" class="form-note" role="status">
      正在读取个人资料…
    </p>
    <div v-if="loadError" class="form-feedback" role="alert">
      <p>{{ accountErrorMessage(loadError) }}</p>
      <UiButton :loading="loading" @click="load">重新读取资料</UiButton>
    </div>
    <AvatarEditor
      v-if="profile"
      :profile="profile"
      :disabled="loading || save.busy.value || save.uncertain.value"
      @updated="acceptProfile"
      @busy="avatarBusy = $event"
    />
    <form class="form-section" @submit.prevent="submit">
      <fieldset :disabled="locked">
        <UiInput
          v-model="username"
          label="用户名"
          autocomplete="username"
          :error="usernameIssue"
          hint="3–32 位字母、数字或中间连字符。"
        />
        <p
          v-if="baseline && username !== baseline.user.username"
          class="form-note"
        >
          用户名变更后，项目路径会变化，旧链接失效且没有别名或自动跳转。
        </p>
        <UiInput
          v-model="displayName"
          label="显示名称"
          autocomplete="nickname"
          :error="nameIssue"
          hint="可留空；留空时显示邮箱。"
        />
        <UiInput
          :model-value="profile?.user.email ?? ''"
          label="邮箱"
          type="email"
          readonly
          hint="邮箱用于登录。"
        />
        <UiButton :disabled="locked" @click="emailOpen = true"
          >修改邮箱</UiButton
        >
      </fieldset>
      <CommandFeedback
        :error="save.error.value"
        :uncertain="save.uncertain.value"
        :replayable="save.replayable.value"
        :busy="save.busy.value"
        @retry="save.retry"
        @abandon="abandonSave"
      />
      <p v-if="abandoned" class="form-note">
        已放弃继续确认原操作，原结果仍未确认。可重新读取当前资料后继续编辑。
      </p>
      <div v-if="conflict" class="form-feedback">
        <p>你的修改仍保留。读取最新资料后，可核对并继续编辑。</p>
        <UiButton :loading="loading" @click="load">读取最新资料</UiButton>
        <template v-if="latestRead && profile">
          <p>
            当前用户名：{{ profile.user.username }}；显示名称：{{
              profile.user.display_name || profile.user.email
            }}
          </p>
          <UiButton @click="useLatestVersion">基于最新资料继续编辑</UiButton>
        </template>
      </div>
      <div class="form-actions">
        <UiButton
          type="submit"
          variant="primary"
          :loading="save.busy.value"
          :disabled="locked || !dirty || conflict"
          >保存资料</UiButton
        >
        <UiButton :disabled="locked || !dirty" @click="resetDraft"
          >取消</UiButton
        >
        <span v-if="saved" class="form-note form-success" role="status"
          >个人资料已保存。</span
        >
      </div>
    </form>
    <EmailChangeDialog
      v-model:open="emailOpen"
      :version="profile?.user.version ?? ''"
      @changed="acceptProfile"
    />
  </div>
</template>
