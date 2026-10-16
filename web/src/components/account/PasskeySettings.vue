<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog, UiInput } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import PasskeyVerification from "./PasskeyVerification.vue";
import EmailRecoveryDialog from "./EmailRecoveryDialog.vue";
import { AccountError, accountEnabled } from "../../account/transport";
import { useSession } from "../../account/session";
import { useCommand } from "../../account/useCommand";
import { useLeaveProtection } from "../../account/leave";
import {
  changePasskey,
  completeRegistration,
  getPasskeyCapabilities,
  getPasskeys,
  getRegistrationOptions,
  passkeyErrorMessage,
  passkeyLabelError,
  passkeyUnavailableMessage,
  type PasskeyCapabilities,
  type PasskeyGrant,
  type PasskeyItem,
  type PasskeyList,
  type PasskeyReceipt,
  type VerificationIntent,
} from "../../account/passkeys";
import {
  browserSupportsPasskeys,
  createPasskey,
  passkeyCancelled,
} from "../../account/webauthn";

const emit = defineEmits<{ busy: [value: boolean] }>();
const session = useSession();
const list = shallowRef<PasskeyList>();
const capability = shallowRef<PasskeyCapabilities>();
const reading = ref(false);
const error = shallowRef<unknown>();
const action = ref<"register" | "rename" | "delete">();
const recoveryOnly = ref(false);
const emailRecoveryOpen = ref(false);
const emailInitialization = ref(false);
const target = shallowRef<PasskeyItem>();
const label = ref("");
const labelIssue = ref("");
const grant = shallowRef<PasskeyGrant>();
const operationVersion = ref("");
const verificationBusy = ref(false);
const prompt = ref(false);
const localError = shallowRef<unknown>();
const save = useCommand<PasskeyReceipt>();
const section = ref<HTMLElement>();
const grantForm = ref<HTMLElement>();
let alive = true;
let revision = 0;
let ceremony = 0;
let controller: AbortController | undefined;
const available = computed(
  () => capability.value?.available === true && browserSupportsPasskeys(),
);
const locked = computed(
  () =>
    verificationBusy.value ||
    prompt.value ||
    save.busy.value ||
    save.uncertain.value,
);
const intent = computed<VerificationIntent>(() =>
  action.value === "rename" || action.value === "delete"
    ? {
        purpose: "manage_passkeys",
        action: action.value,
        passkey_id: target.value!.id,
      }
    : { purpose: "manage_passkeys", action: "register" },
);
const title = computed(() =>
  action.value === "register"
    ? recoveryOnly.value
      ? "恢复 Passkey"
      : "添加 Passkey"
    : action.value === "rename"
      ? "重命名 Passkey"
      : "删除 Passkey",
);
const conflict = computed(() =>
  [save.error.value, localError.value].some(
    (value) =>
      value instanceof AccountError && value.code === "VERSION_CONFLICT",
  ),
);
watch(
  () => !!action.value || emailRecoveryOpen.value || locked.value,
  (value) => emit("busy", value),
  { immediate: true },
);
async function load() {
  if (
    !accountEnabled ||
    session.status.value !== "authenticated" ||
    reading.value
  )
    return;
  const captured = ++revision;
  const generation = session.generation.value;
  const current = () =>
    alive && captured === revision && generation === session.generation.value;
  reading.value = true;
  error.value = undefined;
  try {
    const available = await getPasskeyCapabilities({ current });
    const value = await getPasskeys({ current });
    if (!current()) return;
    capability.value = available;
    list.value = value;
  } catch (failure) {
    if (current()) error.value = failure;
  } finally {
    if (captured === revision) reading.value = false;
  }
}
function open(
  kind: NonNullable<typeof action.value>,
  item?: PasskeyItem,
  recover = false,
) {
  if (
    !list.value ||
    !available.value ||
    locked.value ||
    action.value ||
    emailRecoveryOpen.value
  )
    return;
  recoveryOnly.value = recover;
  target.value = item;
  label.value = item?.label ?? "";
  labelIssue.value = "";
  grant.value = undefined;
  operationVersion.value = list.value.version;
  localError.value = undefined;
  save.abandon();
  action.value = kind;
}
function openEmailRecovery() {
  if (!list.value || locked.value || action.value || emailRecoveryOpen.value)
    return;
  emailInitialization.value = list.value.email_initialization_required;
  emailRecoveryOpen.value = true;
}
function cancelPrompt() {
  ceremony++;
  controller?.abort();
  controller = undefined;
  prompt.value = false;
  grant.value = undefined;
}
function close() {
  if (locked.value || reading.value) return;
  const deleted = action.value === "delete" && save.confirmed.value;
  cancelPrompt();
  save.abandon();
  action.value = undefined;
  emailRecoveryOpen.value = false;
  target.value = undefined;
  if (deleted) {
    void nextTick(async () => {
      await nextTick();
      section.value
        ?.querySelector<HTMLButtonElement>("[data-passkey-add]")
        ?.focus();
    });
  }
}
watch(
  () => !!grant.value && !locked.value,
  async (ready) => {
    if (!ready) return;
    await nextTick();
    grantForm.value?.querySelector<HTMLElement>("input, button")?.focus();
  },
);
function abandonSave() {
  save.abandon();
  grant.value = undefined;
  void load();
}
function adoptVersion() {
  if (!list.value || locked.value || reading.value) return;
  operationVersion.value = list.value.version;
  grant.value = undefined;
  save.abandon();
  localError.value = undefined;
}
async function submit() {
  if (locked.value || !action.value || !grant.value || conflict.value) return;
  labelIssue.value =
    action.value === "delete" ? "" : passkeyLabelError(label.value);
  if (labelIssue.value) return;
  localError.value = undefined;
  const proof = grant.value.grant_id;
  const version = operationVersion.value;
  if (action.value !== "register") {
    const id = target.value!.id;
    const name = action.value === "rename" ? label.value : undefined;
    await save.submit(
      () => changePasskey(id, version, proof, name),
      async () => {
        grant.value = undefined;
        await load();
      },
    );
    if (!save.uncertain.value && !save.confirmed.value) grant.value = undefined;
    return;
  }
  const owner = ++ceremony;
  const identity = session.generation.value;
  const current = () =>
    alive && owner === ceremony && identity === session.generation.value;
  const abort = new AbortController();
  controller = abort;
  prompt.value = true;
  try {
    const options = await getRegistrationOptions(proof, label.value, version, {
      current,
    });
    if (!current()) return;
    const credential = await createPasskey(options.public_key, abort.signal);
    if (!current()) return;
    controller = undefined;
    await save.submit(
      () => completeRegistration(options.operation_id, credential),
      async () => {
        grant.value = undefined;
        await load();
      },
    );
  } catch (failure) {
    if (current() && !passkeyCancelled(failure)) localError.value = failure;
  } finally {
    if (owner === ceremony) {
      controller = undefined;
      prompt.value = false;
      if (!save.uncertain.value) grant.value = undefined;
    }
  }
}
function reset() {
  revision++;
  cancelPrompt();
  list.value = undefined;
  capability.value = undefined;
  error.value = undefined;
  reading.value = false;
  action.value = undefined;
  emailRecoveryOpen.value = false;
  target.value = undefined;
  label.value = "";
  localError.value = undefined;
  save.abandon();
}
watch(session.generation, reset);
watch(session.status, (status) => {
  if (status === "authenticated" && !list.value) void load();
});
useLeaveProtection(
  () => !!action.value || emailRecoveryOpen.value || locked.value,
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  reset();
  emit("busy", false);
});
</script>

<template>
  <section
    ref="section"
    class="passkey-settings form-section"
    aria-labelledby="passkey-heading"
  >
    <div class="passkey-heading">
      <h2 id="passkey-heading">Passkey</h2>
      <div class="form-actions">
        <UiButton
          v-if="accountEnabled"
          :loading="reading"
          :disabled="!!action || locked"
          @click="load"
          >刷新</UiButton
        ><UiButton
          v-if="accountEnabled"
          data-passkey-add
          :disabled="
            !available ||
            !list ||
            list.email_initialization_required ||
            list.items.length >= 20 ||
            reading
          "
          @click="open('register')"
          >添加 Passkey</UiButton
        >
      </div>
    </div>
    <p v-if="!accountEnabled" class="form-note">
      账号服务连接后可管理 Passkey。
    </p>
    <p v-else-if="reading && !list" class="form-note" role="status">
      正在读取 Passkey…
    </p>
    <div v-if="error" class="form-feedback" role="alert">
      <p>{{ passkeyErrorMessage(error) }}</p>
      <UiButton :loading="reading" :disabled="locked" @click="load"
        >重新读取</UiButton
      >
    </div>
    <template v-if="list">
      <p v-if="capability && !available" class="form-note">
        {{ passkeyUnavailableMessage(capability) }}
      </p>
      <p v-if="list.email_initialization_required" class="form-note">
        请先使用部署者的一次性授权初始化邮箱，再添加 Passkey。
      </p>
      <p v-if="!list.items.length" class="form-note">尚未添加 Passkey。</p>
      <ul v-else class="passkey-list" aria-label="我的 Passkey">
        <li v-for="item in list.items" :key="item.id">
          <div class="passkey-name">
            <strong>{{ item.label }}</strong
            ><span class="form-note">{{
              item.last_used_at
                ? `最近使用 ${new Date(item.last_used_at).toLocaleString()}`
                : "尚未使用"
            }}</span>
          </div>
          <div class="form-actions">
            <UiButton
              :aria-label="`重命名 ${item.label}`"
              :disabled="!available || reading"
              @click="open('rename', item)"
              >重命名</UiButton
            ><UiButton
              :aria-label="`删除 ${item.label}`"
              :disabled="!available || reading"
              @click="open('delete', item)"
              >删除</UiButton
            >
          </div>
        </li>
      </ul>
      <div class="form-actions">
        <UiButton
          v-if="list.items.length >= 20"
          :disabled="
            !available || reading || list.email_initialization_required
          "
          @click="open('register', undefined, true)"
          >恢复 Passkey</UiButton
        >
        <UiButton :disabled="reading" @click="openEmailRecovery">{{
          list.email_initialization_required ? "初始化邮箱" : "恢复邮箱"
        }}</UiButton>
      </div>
    </template>
    <UiDialog
      :open="!!action"
      :title="title"
      :close-on-escape="!locked"
      :close-on-outside="!locked"
      @update:open="
        (value) => {
          if (!value) close();
        }
      "
    >
      <p
        v-if="save.confirmed.value"
        class="form-feedback form-success"
        role="status"
      >
        Passkey
        {{
          action === "delete"
            ? "已删除"
            : action === "rename"
              ? "名称已更新"
              : "已添加"
        }}。
      </p>
      <template v-else>
        <p v-if="target" class="form-note">{{ target.label }}</p>
        <PasskeyVerification
          v-if="action && list && !grant && !save.uncertain.value && !conflict"
          :intent="intent"
          :has-passkeys="list.items.length > 0"
          :email-initialization-required="list.email_initialization_required"
          :recovery-only="recoveryOnly"
          @verified="(value) => (grant = value)"
          @busy="(value) => (verificationBusy = value)"
        />
        <div v-if="grant" ref="grantForm" class="account-form">
          <UiInput
            v-if="action !== 'delete'"
            v-model="label"
            label="Passkey 名称"
            placeholder="例如：个人电脑"
            :error="labelIssue"
            :disabled="locked"
            @keydown.enter.prevent="submit"
          />
          <p v-else class="form-note">
            删除后，此 Passkey 将无法用于登录和身份验证。
          </p>
          <UiButton
            :variant="action === 'delete' ? 'danger' : 'primary'"
            :disabled="locked || conflict"
            :loading="save.busy.value || prompt"
            @click="submit"
            >{{
              action === "delete"
                ? "确认删除"
                : action === "rename"
                  ? "保存名称"
                  : "创建 Passkey"
            }}</UiButton
          >
        </div>
        <UiButton v-if="prompt && !save.busy.value" @click="cancelPrompt"
          >取消验证</UiButton
        >
      </template>
      <p v-if="localError" class="form-feedback" role="alert">
        {{ passkeyErrorMessage(localError) }}
      </p>
      <CommandFeedback
        :error="save.error.value"
        :uncertain="save.uncertain.value"
        :replayable="save.replayable.value"
        :busy="save.busy.value"
        @retry="save.retry"
        @abandon="abandonSave"
      />
      <div v-if="conflict" class="form-feedback">
        <p>Passkey 列表已变化，已保留名称。读取最新列表后重新验证。</p>
        <UiButton :loading="reading" @click="load">读取最新列表</UiButton
        ><UiButton
          v-if="list && list.version !== operationVersion"
          :disabled="
            reading ||
            (target && !list.items.some((item) => item.id === target!.id))
          "
          @click="adoptVersion"
          >采用当前版本并重新验证</UiButton
        >
      </div>
      <div v-if="save.confirmed.value && error" class="form-feedback">
        <p>操作已确认，列表尚未读回。</p>
        <UiButton :loading="reading" @click="load">重新读取列表</UiButton>
      </div>
      <template #footer
        ><UiButton :disabled="locked || reading" @click="close">{{
          save.confirmed.value ? "完成" : "取消"
        }}</UiButton></template
      >
    </UiDialog>
    <EmailRecoveryDialog
      v-model:open="emailRecoveryOpen"
      :initialization="emailInitialization"
      @changed="load"
    />
  </section>
</template>

<style scoped>
.passkey-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.passkey-list {
  list-style: none;
  padding: 0;
  margin: 0;
}
.passkey-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.passkey-name {
  display: grid;
  gap: 4px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.passkey-name strong {
  font-size: 14px;
  font-weight: 500;
}
</style>
