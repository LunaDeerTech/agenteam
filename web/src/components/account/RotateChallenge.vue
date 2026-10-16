<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef } from "vue";
import { UiButton } from "../ui";
import { anonymousRequest } from "../../account/session";
import { accountErrorMessage } from "../../account/transport";
import { isRecord } from "../../account/types";
const props = defineProps<{ email: string; loginKey: string }>();
const emit = defineEmits<{ verified: [pass: string] }>();
type Challenge = {
  id: string;
  mode: "rotate";
  master: string;
  thumb: string;
  expires_at: string;
};
const challenge = shallowRef<Challenge>();
const angle = ref(0);
const busy = ref(false);
const error = shallowRef<unknown>();
let alive = true;
function isChallenge(value: unknown): value is Challenge {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    value.mode === "rotate" &&
    typeof value.expires_at === "string" &&
    [value.master, value.thumb].every(
      (image) =>
        typeof image === "string" &&
        image.length < 262144 &&
        /^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(image),
    )
  );
}
async function load() {
  if (busy.value) return;
  busy.value = true;
  error.value = undefined;
  challenge.value = undefined;
  angle.value = 0;
  try {
    const value = await anonymousRequest(
      "/auth/challenges",
      {
        method: "POST",
        status: 201,
        body: JSON.stringify({
          mode: "rotate",
          email: props.email,
          login_key: props.loginKey,
        }),
        current: () => alive,
      },
      isChallenge,
    );
    if (alive) challenge.value = value;
  } catch (failure) {
    if (alive) error.value = failure;
  } finally {
    if (alive) busy.value = false;
  }
}
async function verify() {
  if (!challenge.value || busy.value) return;
  busy.value = true;
  error.value = undefined;
  try {
    const value = await anonymousRequest(
      "/auth/challenges/verify",
      {
        method: "POST",
        body: JSON.stringify({
          email: props.email,
          login_key: props.loginKey,
          challenge_id: challenge.value.id,
          proof: { angle: Number(angle.value) },
        }),
        current: () => alive,
      },
      (value): value is { pass: string } =>
        isRecord(value) && typeof value.pass === "string" && !!value.pass,
    );
    if (alive) emit("verified", value.pass);
  } catch (failure) {
    if (alive) error.value = failure;
  } finally {
    if (alive) busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  challenge.value = undefined;
});
</script>
<template>
  <section class="rotate-challenge" aria-label="登录验证">
    <h2>完成验证</h2>
    <p class="form-note">
      拖动滑块或使用方向键旋转图片，使图片与参考方向一致。
    </p>
    <div v-if="challenge" class="challenge-images">
      <img :src="challenge.master" alt="参考图片" />
      <img
        :src="challenge.thumb"
        alt="需要旋转的图片"
        :style="{ transform: `rotate(${angle}deg)` }"
      />
    </div>
    <label v-if="challenge" class="angle-control"
      >旋转角度：{{ angle }}°<input
        v-model="angle"
        type="range"
        min="0"
        max="360"
        step="1"
        :disabled="busy"
    /></label>
    <p v-if="error" role="alert" class="form-note">
      {{ accountErrorMessage(error) }}
    </p>
    <div class="form-actions">
      <UiButton :loading="busy" :disabled="!challenge" @click="verify"
        >验证并登录</UiButton
      ><UiButton :disabled="busy" variant="ghost" @click="load"
        >换一张</UiButton
      >
    </div>
  </section>
</template>
<style scoped>
.rotate-challenge {
  display: grid;
  gap: 14px;
  padding: 18px;
  background: var(--surface-content);
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
}
.challenge-images {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 20px;
  overflow: hidden;
  padding: 12px;
}
.challenge-images img {
  width: min(40%, 120px);
  height: auto;
}
.angle-control {
  display: grid;
  gap: 8px;
  font-size: 12px;
}
.angle-control input {
  width: 100%;
}
</style>
