<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton, UiDialog, UiInput } from "../../components/ui";
import { useLeaveProtection } from "../../account/leave";
import {
  AccountError,
  accountEnabled,
  accountErrorMessage,
} from "../../account/transport";
import { useSystemAccess } from "../../system/requests";
import {
  accountSettingsIssues,
  accountSettingsValues,
  getAccountSettings,
  saveAccountSettings,
  securityFields,
  useSettingsCommand,
  type AccountSettings,
  type AccountSettingsValues,
} from "../../system/account-settings";

const access = useSystemAccess();
const observation = shallowRef<AccountSettings>();
const baseline = shallowRef<AccountSettings>();
const draft = reactive(accountSettingsValues());
const reading = ref(false);
const readError = shallowRef<unknown>();
const submitted = ref(false);
const confirmation = ref<"discard" | "adopt" | "abandon">();
const save = useSettingsCommand<AccountSettings>();
const checking = ref(false);
const pageElement = ref<HTMLElement | null>(null);
let alive = true;
let readRevision = 0;
const dirty = computed(
  () =>
    !!baseline.value &&
    securityFields.some(
      (field) => draft[field.key] !== baseline.value?.[field.key],
    ),
);
const issues = computed(() => accountSettingsIssues(draft));
const conflict = computed(
  () =>
    save.error.value instanceof AccountError &&
    save.error.value.code === "VERSION_CONFLICT",
);
const locked = computed(
  () =>
    !accountEnabled ||
    !access.allowed.value ||
    !baseline.value ||
    reading.value ||
    save.busy.value ||
    save.uncertain.value ||
    checking.value,
);
const committed = save.committed;
const latestChanged = computed(
  () =>
    !!observation.value &&
    !!baseline.value &&
    observation.value.version !== baseline.value.version,
);

function fieldIssue(key: keyof AccountSettingsValues) {
  if (submitted.value && issues.value[key]) return issues.value[key];
  return save.error.value instanceof AccountError &&
    save.error.value.fields.some((field) => field.path === `/${key}`)
    ? "请检查此项内容后再试。"
    : "";
}
function adopt(value: AccountSettings) {
  baseline.value = value;
  Object.assign(draft, accountSettingsValues(value));
  submitted.value = false;
}
async function load(replace = false) {
  if (reading.value || !access.allowed.value || !accountEnabled) return;
  const revision = ++readRevision;
  const generation = access.generation.value;
  const current = () =>
    alive &&
    revision === readRevision &&
    generation === access.generation.value;
  reading.value = true;
  readError.value = undefined;
  try {
    const value = await getAccountSettings({ current });
    if (!current()) return;
    if (baseline.value && value.id !== baseline.value.id)
      throw new AccountError("INVALID_RESPONSE");
    if (
      !observation.value ||
      BigInt(value.version) >= BigInt(observation.value.version)
    )
      observation.value = value;
    if (!baseline.value || replace) adopt(observation.value!);
  } catch (error) {
    if (current()) readError.value = error;
  } finally {
    if (revision === readRevision) reading.value = false;
  }
}
async function submit() {
  if (locked.value || conflict.value || !dirty.value || !baseline.value) return;
  submitted.value = true;
  if (Object.keys(issues.value).length) {
    await nextTick();
    if (alive && access.allowed.value)
      pageElement.value
        ?.querySelector<HTMLElement>('[aria-invalid="true"]')
        ?.focus();
    return;
  }
  const generation = access.generation.value;
  const base = baseline.value;
  await save.submit(
    () =>
      saveAccountSettings(
        base,
        { ...draft },
        { current: () => alive && generation === access.generation.value },
      ),
    async (value) => {
      // This receipt confirms the command. A separate GET supplies the next edit baseline.
      baseline.value = undefined;
      Object.assign(draft, accountSettingsValues(value));
      await load(true);
    },
  );
}
async function inspect() {
  if (checking.value || reading.value || save.busy.value) return;
  checking.value = true;
  try {
    if (save.uncertain.value) {
      if (!(await save.verify())) return;
    } else await access.recheck();
    if (access.allowed.value) await load();
  } finally {
    checking.value = false;
  }
}
async function confirmAction() {
  const action = confirmation.value;
  confirmation.value = undefined;
  if (!access.allowed.value || save.busy.value || reading.value) return;
  if (action === "abandon") {
    save.abandon();
    baseline.value = undefined;
    Object.assign(draft, accountSettingsValues());
    await load(true);
  } else if (action === "adopt" && observation.value) {
    save.abandon();
    adopt(observation.value);
  } else if (action === "discard" && baseline.value) {
    save.abandon();
    adopt(baseline.value);
  }
}
function reset() {
  readRevision++;
  reading.value = false;
  readError.value = undefined;
  observation.value = undefined;
  baseline.value = undefined;
  Object.assign(draft, accountSettingsValues());
  submitted.value = false;
  confirmation.value = undefined;
}
watch(access.generation, reset, { flush: "sync" });
watch(access.allowed, (allowed) => {
  if (allowed && !baseline.value && !reading.value) void load();
});
onMounted(() => void load());
onBeforeUnmount(() => {
  alive = false;
  readRevision++;
});
useLeaveProtection(
  () => dirty.value || save.busy.value || save.uncertain.value,
);
</script>

<template>
  <div ref="pageElement" class="security-page account-form">
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览，连接账号服务后可读取和修改设置。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>你目前没有系统管理权限。</p>
      <UiButton :loading="checking" @click="inspect">重新检查权限</UiButton>
    </div>
    <template v-else>
      <p class="form-note">
        期限修改只影响之后签发的登录会话与重置链接。已有会话不会因此退出；邀请链接仍固定有效
        24 小时。
      </p>
      <p v-if="reading && !baseline" class="form-note" role="status">
        正在读取账号安全配置…
      </p>
      <div v-if="readError" class="form-feedback" role="alert">
        <p>
          {{
            save.confirmed.value
              ? "保存已确认，当前配置读取失败。"
              : "当前配置读取失败。"
          }}
          {{ accountErrorMessage(readError) }}
        </p>
        <UiButton :loading="reading" :disabled="save.busy.value" @click="load()"
          >重新读取配置</UiButton
        >
      </div>
      <form
        v-if="baseline || save.confirmed.value"
        class="account-form"
        @submit.prevent="submit"
      >
        <fieldset :disabled="locked">
          <section class="form-section" aria-labelledby="session-limits">
            <h2 id="session-limits">登录会话</h2>
            <UiInput
              v-for="field in securityFields.slice(0, 2)"
              :key="field.key"
              v-model="draft[field.key]"
              :label="field.label"
              :hint="field.hint"
              :error="fieldIssue(field.key)"
              inputmode="numeric"
              autocomplete="off"
            />
          </section>
          <section class="form-section" aria-labelledby="account-protection">
            <h2 id="account-protection">重置与登录验证</h2>
            <UiInput
              v-for="field in securityFields.slice(2)"
              :key="field.key"
              v-model="draft[field.key]"
              :label="field.label"
              :hint="field.hint"
              :error="fieldIssue(field.key)"
              inputmode="numeric"
              autocomplete="off"
            />
            <p class="form-note">登录失败挑战阈值会在下一次登录尝试时生效。</p>
          </section>
        </fieldset>
        <p
          v-if="save.confirmed.value && !dirty"
          class="form-note form-success"
          role="status"
        >
          账号安全配置已保存。
        </p>
        <div v-if="save.uncertain.value" class="form-feedback" role="alert">
          <p>
            {{
              committed ? "操作已提交，回执尚未读回。" : "原保存结果尚未确认。"
            }}
            保留当前页面，检查登录状态后可重试原请求。重新读取配置不能确认原保存结果。
          </p>
          <div class="form-actions">
            <UiButton
              :loading="checking"
              :disabled="save.busy.value || reading"
              @click="inspect"
              >检查当前状态</UiButton
            >
            <UiButton
              v-if="save.replayable.value"
              :loading="save.busy.value"
              :disabled="reading || checking"
              @click="save.retry"
              >重试原请求</UiButton
            >
            <UiButton
              :disabled="save.busy.value || reading || checking"
              @click="confirmation = 'abandon'"
              >放弃本次操作</UiButton
            >
          </div>
        </div>
        <div v-else-if="save.error.value" class="form-feedback" role="alert">
          <p>
            {{
              conflict
                ? "配置已被其他管理员修改，你的草稿已保留。请读取最新配置并核对。"
                : accountErrorMessage(save.error.value)
            }}
          </p>
        </div>
        <div
          v-if="latestChanged && !save.uncertain.value"
          class="form-feedback"
        >
          <p>已读取更新的配置。采用后会替换当前草稿。</p>
          <dl class="latest-values">
            <template v-for="field in securityFields" :key="field.key"
              ><dt>{{ field.label }}</dt>
              <dd>{{ observation?.[field.key] }}</dd></template
            >
          </dl>
          <UiButton :disabled="locked" @click="confirmation = 'adopt'"
            >采用最新配置</UiButton
          >
        </div>
        <div class="form-actions">
          <UiButton
            variant="primary"
            type="submit"
            :loading="save.busy.value"
            :disabled="locked || !dirty || conflict"
            >保存配置</UiButton
          >
          <UiButton
            :disabled="locked || !dirty"
            @click="confirmation = 'discard'"
            >取消</UiButton
          >
          <UiButton
            variant="ghost"
            :loading="reading"
            :disabled="save.busy.value || save.uncertain.value || checking"
            @click="load()"
            >读取最新配置</UiButton
          >
        </div>
      </form>
    </template>
    <UiDialog
      :open="!!confirmation && access.allowed.value"
      :title="confirmation === 'abandon' ? '放弃追踪本次操作' : '放弃当前修改'"
      @update:open="!$event && (confirmation = undefined)"
    >
      <p class="confirmation-copy">
        {{
          confirmation === "abandon"
            ? "此前提交仍可能生效。这只停止追踪原请求，不会撤销服务端操作；之后需要重新读取配置。"
            : "当前未保存的草稿会被替换。"
        }}
      </p>
      <template #footer>
        <UiButton @click="confirmation = undefined">继续编辑</UiButton>
        <UiButton variant="primary" @click="confirmAction">{{
          confirmation === "abandon" ? "放弃追踪" : "放弃修改"
        }}</UiButton>
      </template>
    </UiDialog>
  </div>
</template>

<style scoped>
.security-page {
  max-width: 520px;
}
h2 {
  font-size: 14px;
  font-weight: 600;
}
.latest-values {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px 16px;
  margin: 14px 0;
}
.latest-values dt {
  overflow-wrap: anywhere;
}
.latest-values dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}
.confirmation-copy {
  font-size: 13px;
  line-height: 1.7;
  color: var(--text-secondary);
}
</style>
