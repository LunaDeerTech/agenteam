<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
} from "vue";
import { UiButton } from "../ui";
import CommandFeedback from "./CommandFeedback.vue";
import { useLeaveProtection } from "../../account/leave";
import {
  authenticatedRequest,
  sessionCommand,
  useSession,
} from "../../account/session";
import {
  AccountError,
  accountEnabled,
  accountErrorMessage,
  type AccountImage,
} from "../../account/transport";
import { isProfile, type ProfileResponse } from "../../account/types";
import { useCommand } from "../../account/useCommand";

const props = defineProps<{ profile: ProfileResponse; disabled?: boolean }>();
const emit = defineEmits<{
  updated: [value: ProfileResponse];
  busy: [value: boolean];
}>();
const session = useSession();
const currentURL = ref("");
const candidateURL = ref("");
const candidateName = ref("");
const candidateError = ref("");
const reading = ref(false);
const preparing = ref(false);
const refreshingProfile = ref(false);
const readError = shallowRef<unknown>();
const refreshedProfile = shallowRef<ProfileResponse>();
const resultMessage = ref("");
const removed = ref(false);
const mustRefresh = ref(false);
const save = useCommand<ProfileResponse | void>();
const fileInput = ref<HTMLInputElement>();
let candidate: File | undefined;
let candidateVersion = "";
let alive = true;
let imageGeneration = 0;
let candidateGeneration = 0;
let profileGeneration = 0;
let pendingCandidateURL = "";
let cancelDecode: (() => void) | undefined;
let action: "upload" | "remove" = "upload";
class CandidateError extends Error {}
const maximumBytes = 5 * 1024 * 1024;
const busy = computed(
  () =>
    preparing.value ||
    save.busy.value ||
    save.uncertain.value ||
    mustRefresh.value,
);
const locked = computed(
  () => !accountEnabled || props.disabled || busy.value || preparing.value,
);
const conflict = computed(
  () =>
    save.error.value instanceof AccountError &&
    save.error.value.code === "VERSION_CONFLICT",
);
const fingerprint = computed(
  () =>
    `${props.profile.user.id}:${props.profile.avatar?.sha256 ?? "none"}:${props.profile.avatar?.media_type ?? ""}:${props.profile.avatar?.byte_size ?? ""}`,
);

function releaseCurrent() {
  if (currentURL.value) URL.revokeObjectURL(currentURL.value);
  currentURL.value = "";
}
function clearCandidate() {
  candidateGeneration++;
  cancelDecode?.();
  if (pendingCandidateURL) URL.revokeObjectURL(pendingCandidateURL);
  pendingCandidateURL = "";
  if (candidateURL.value) URL.revokeObjectURL(candidateURL.value);
  candidateURL.value = "";
  candidateName.value = "";
  candidate = undefined;
  candidateVersion = "";
  preparing.value = false;
  if (fileInput.value) fileInput.value.value = "";
}
function cancelCandidate() {
  clearCandidate();
  candidateError.value = "";
  resultMessage.value = "";
  save.abandon();
}

// Read bounded containers before decoding so extreme dimensions and animated
// PNG/WebP candidates do not enter the preview decoder.
function inspectImage(bytes: ArrayBuffer, mediaType: string) {
  const view = new DataView(bytes);
  const data = new Uint8Array(bytes);
  const textAt = (start: number, length: number) =>
    String.fromCharCode(...data.subarray(start, start + length));
  const invalid = () => {
    throw new CandidateError("请选择完整的静态 JPEG、PNG 或 WebP 图片。");
  };
  let width = 0;
  let height = 0;
  if (mediaType === "image/png") {
    if (
      data.length < 33 ||
      ![137, 80, 78, 71, 13, 10, 26, 10].every(
        (byte, index) => data[index] === byte,
      ) ||
      textAt(12, 4) !== "IHDR"
    )
      invalid();
    width = view.getUint32(16);
    height = view.getUint32(20);
    let offset = 8;
    while (offset + 12 <= data.length) {
      const size = view.getUint32(offset);
      if (offset + size + 12 > data.length || textAt(offset + 4, 4) === "acTL")
        invalid();
      offset += size + 12;
    }
    if (offset !== data.length) invalid();
  } else if (mediaType === "image/webp") {
    if (
      data.length < 20 ||
      textAt(0, 4) !== "RIFF" ||
      textAt(8, 4) !== "WEBP" ||
      view.getUint32(4, true) + 8 !== data.length
    )
      invalid();
    const uint24 = (at: number) =>
      data[at]! + (data[at + 1]! << 8) + (data[at + 2]! << 16);
    let offset = 12;
    while (offset + 8 <= data.length) {
      const kind = textAt(offset, 4);
      const size = view.getUint32(offset + 4, true);
      const start = offset + 8;
      if (start + size > data.length || kind === "ANIM" || kind === "ANMF")
        invalid();
      if (kind === "VP8X" && size >= 10) {
        if (data[start]! & 2) invalid();
        width = uint24(start + 4) + 1;
        height = uint24(start + 7) + 1;
      } else if (!width && kind === "VP8 " && size >= 10) {
        if (
          data[start + 3] !== 0x9d ||
          data[start + 4] !== 1 ||
          data[start + 5] !== 0x2a
        )
          invalid();
        width = view.getUint16(start + 6, true) & 0x3fff;
        height = view.getUint16(start + 8, true) & 0x3fff;
      } else if (!width && kind === "VP8L" && size >= 5) {
        if (data[start] !== 0x2f) invalid();
        const bits = view.getUint32(start + 1, true);
        width = (bits & 0x3fff) + 1;
        height = ((bits >>> 14) & 0x3fff) + 1;
      }
      offset = start + size + (size % 2);
    }
    if (offset !== data.length) invalid();
  } else if (mediaType === "image/jpeg") {
    if (data.length < 4 || data[0] !== 0xff || data[1] !== 0xd8) invalid();
    let offset = 2;
    while (offset + 4 <= data.length) {
      if (data[offset] !== 0xff) invalid();
      while (data[offset] === 0xff) offset++;
      const marker = data[offset++]!;
      if (marker === 0xd9 || marker === 0xda) break;
      if (marker === 1 || (marker >= 0xd0 && marker <= 0xd7)) continue;
      if (offset + 2 > data.length) invalid();
      const size = view.getUint16(offset);
      if (size < 2 || offset + size > data.length) invalid();
      if ([0xc0, 0xc1, 0xc2].includes(marker)) {
        if (size < 8) invalid();
        height = view.getUint16(offset + 3);
        width = view.getUint16(offset + 5);
      }
      offset += size;
    }
  } else invalid();
  if (!width || !height) invalid();
  if (width > 4096 || height > 4096 || width * height > 16_777_216)
    throw new CandidateError(
      "图片每边不能超过 4096 像素，总像素不能超过 16,777,216。",
    );
  return { width, height };
}

async function chooseFile(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file || locked.value) return;
  const version = props.profile.user.version;
  clearCandidate();
  const owner = candidateGeneration;
  candidateError.value = "";
  resultMessage.value = "";
  preparing.value = true;
  let url = "";
  try {
    if (!file.size || file.size > maximumBytes)
      throw new CandidateError("图片大小需大于 0 且不超过 5 MiB。");
    if (!["image/jpeg", "image/png", "image/webp"].includes(file.type))
      throw new CandidateError("请选择静态 JPEG、PNG 或 WebP 图片。");
    const dimensions = inspectImage(await file.arrayBuffer(), file.type);
    if (!alive || owner !== candidateGeneration) return;
    url = URL.createObjectURL(file);
    pendingCandidateURL = url;
    await new Promise<void>((resolve, reject) => {
      const image = new Image();
      const finish = (success: boolean) => {
        image.onload = null;
        image.onerror = null;
        cancelDecode = undefined;
        if (success) resolve();
        else reject(new CandidateError("无法读取这张图片，请选择其他图片。"));
      };
      cancelDecode = () => {
        finish(false);
        image.src = "";
      };
      image.onload = () =>
        finish(
          (image.naturalWidth === dimensions.width &&
            image.naturalHeight === dimensions.height) ||
            (image.naturalWidth === dimensions.height &&
              image.naturalHeight === dimensions.width),
        );
      image.onerror = () => finish(false);
      image.src = url;
    });
    if (!alive || owner !== candidateGeneration) return;
    candidate = file;
    candidateVersion = version;
    candidateName.value = file.name;
    candidateURL.value = url;
    pendingCandidateURL = "";
    url = "";
  } catch (error) {
    if (alive && owner === candidateGeneration)
      candidateError.value =
        error instanceof CandidateError
          ? error.message
          : "无法读取这张图片，请选择其他图片。";
  } finally {
    if (url) URL.revokeObjectURL(url);
    if (pendingCandidateURL === url) pendingCandidateURL = "";
    if (alive && owner === candidateGeneration) preparing.value = false;
  }
}

async function boundedBlobURL(
  image: AccountImage,
  metadata: NonNullable<ProfileResponse["avatar"]>,
) {
  const expected = BigInt(metadata.byte_size);
  if (
    expected <= 0n ||
    expected > BigInt(maximumBytes) ||
    image.contentLength !== metadata.byte_size ||
    BigInt(image.blob.size) !== expected ||
    image.blob.type !== metadata.media_type ||
    image.etag !== `"${metadata.sha256}"`
  )
    throw new AccountError("INVALID_RESPONSE");
  const bytes = await image.blob.arrayBuffer();
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  const hash = `sha256:${Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
  if (hash !== metadata.sha256) throw new AccountError("INVALID_RESPONSE");
  inspectImage(bytes, image.blob.type);
  return URL.createObjectURL(image.blob);
}
async function loadImage() {
  const metadata = props.profile.avatar;
  const owner = ++imageGeneration;
  releaseCurrent();
  readError.value = undefined;
  if (!accountEnabled || !metadata || removed.value) {
    reading.value = false;
    return;
  }
  const source = fingerprint.value;
  const current = () =>
    alive && owner === imageGeneration && source === fingerprint.value;
  reading.value = true;
  let url = "";
  try {
    const image = await authenticatedRequest<AccountImage>("/me/avatar", {
      binary: true,
      current,
    });
    url = await boundedBlobURL(image, metadata);
    if (!current()) return;
    currentURL.value = url;
    url = "";
  } catch (error) {
    if (current()) readError.value = error;
  } finally {
    if (url) URL.revokeObjectURL(url);
    if (current()) reading.value = false;
  }
}
async function refreshProfile() {
  if (refreshingProfile.value) return;
  const owner = ++profileGeneration;
  refreshingProfile.value = true;
  readError.value = undefined;
  try {
    const value = await authenticatedRequest(
      "/me",
      { current: () => alive && owner === profileGeneration },
      isProfile,
    );
    if (!alive || owner !== profileGeneration) return;
    refreshedProfile.value = value;
    mustRefresh.value = false;
    removed.value = false;
    emit("updated", value);
  } catch (error) {
    if (alive && owner === profileGeneration) readError.value = error;
  } finally {
    if (alive && owner === profileGeneration) refreshingProfile.value = false;
  }
}
async function reloadAvatar() {
  const previous = fingerprint.value;
  await refreshProfile();
  await nextTick();
  if (!readError.value && previous === fingerprint.value) await loadImage();
}
async function upload() {
  if (locked.value || !candidate || conflict.value) return;
  action = "upload";
  refreshedProfile.value = undefined;
  const file = candidate;
  const version = candidateVersion;
  resultMessage.value = "";
  await save.submit(
    () =>
      sessionCommand<ProfileResponse | void>(
        "/me/avatar",
        {
          method: "PUT",
          body: file,
          headers: { "Content-Type": file.type, "If-Match": `"${version}"` },
        },
        isProfile,
      ),
    (value) => {
      if (!value) return;
      clearCandidate();
      emit("updated", value);
      resultMessage.value = "头像已保存。";
    },
  );
}
async function remove() {
  if (locked.value || !props.profile.avatar || conflict.value) return;
  action = "remove";
  refreshedProfile.value = undefined;
  resultMessage.value = "";
  await save.submit(
    () =>
      sessionCommand<ProfileResponse | void>("/me/avatar", {
        method: "DELETE",
        body: JSON.stringify({ version: props.profile.user.version }),
        status: 204,
      }),
    refreshProfile,
    () => {
      removed.value = true;
      mustRefresh.value = true;
      releaseCurrent();
      clearCandidate();
      resultMessage.value = "头像已移除。";
    },
  );
}
function adoptLatestVersion() {
  if (!refreshedProfile.value) return;
  if (action === "upload")
    candidateVersion = refreshedProfile.value.user.version;
  save.abandon();
  refreshedProfile.value = undefined;
}
function abandonSave() {
  save.abandon();
  clearCandidate();
  mustRefresh.value = true;
  resultMessage.value =
    "已放弃继续确认原操作，原结果仍未确认。请读取当前资料后再操作。";
  void refreshProfile();
}
watch(busy, (value) => emit("busy", value), { immediate: true });
watch(
  fingerprint,
  () => {
    removed.value = false;
    void loadImage();
  },
  { immediate: true },
);
watch(session.generation, () => {
  imageGeneration++;
  profileGeneration++;
  clearCandidate();
  releaseCurrent();
  readError.value = undefined;
});
useLeaveProtection(
  () =>
    !!candidate || preparing.value || save.busy.value || save.uncertain.value,
);
onBeforeUnmount(() => {
  alive = false;
  imageGeneration++;
  profileGeneration++;
  clearCandidate();
  releaseCurrent();
});
</script>

<template>
  <section class="form-section avatar-editor" aria-labelledby="avatar-label">
    <div>
      <h2 id="avatar-label" class="avatar-heading">头像</h2>
      <p class="form-note">
        静态 JPEG、PNG 或 WebP，最大 5 MiB，每边最多 4096 像素。
      </p>
    </div>
    <div class="avatar-row">
      <div class="avatar-image">
        <img
          v-if="currentURL"
          :src="currentURL"
          alt="当前头像"
          @error="
            releaseCurrent();
            readError = new AccountError('INVALID_RESPONSE');
          "
        />
        <span v-else-if="reading" class="form-note">读取中…</span>
        <span v-else-if="readError" class="form-note">读取失败</span>
        <span v-else class="avatar-fallback" aria-label="未设置头像">{{
          (profile.user.display_name || profile.user.username)
            .slice(0, 1)
            .toUpperCase()
        }}</span>
      </div>
      <div class="avatar-controls">
        <input
          ref="fileInput"
          class="avatar-file"
          type="file"
          accept="image/jpeg,image/png,image/webp"
          aria-label="选择头像图片"
          :disabled="locked"
          @change="chooseFile"
        />
        <UiButton :disabled="locked" @click="fileInput?.click()">{{
          profile.avatar ? "替换头像" : "选择头像"
        }}</UiButton>
        <UiButton
          v-if="profile.avatar && !removed"
          :disabled="locked || !!candidateURL || conflict"
          @click="remove"
          >移除头像</UiButton
        >
      </div>
    </div>
    <p v-if="preparing" class="form-note" role="status">正在检查图片…</p>
    <p v-if="candidateError" class="form-feedback" role="alert">
      {{ candidateError }}
    </p>
    <div v-if="candidateURL" class="avatar-candidate">
      <img :src="candidateURL" alt="待上传头像预览" />
      <div>
        <p class="form-note avatar-filename">{{ candidateName }}</p>
        <p class="form-note">仅本地预览，尚未上传。</p>
        <div class="form-actions">
          <UiButton
            variant="primary"
            :loading="save.busy.value"
            :disabled="locked || conflict"
            @click="upload"
            >上传头像</UiButton
          >
          <UiButton :disabled="locked" @click="cancelCandidate"
            >取消选择</UiButton
          >
        </div>
      </div>
    </div>
    <CommandFeedback
      :error="save.error.value"
      :uncertain="save.uncertain.value"
      :replayable="save.replayable.value"
      :busy="save.busy.value"
      @retry="save.retry"
      @abandon="abandonSave"
    />
    <div v-if="readError || mustRefresh" class="form-feedback" role="alert">
      <p>
        {{
          removed
            ? "头像已移除，当前资料尚未刷新。"
            : accountErrorMessage(readError)
        }}
      </p>
      <UiButton
        :disabled="save.busy.value"
        :loading="refreshingProfile || reading"
        @click="mustRefresh ? refreshProfile() : reloadAvatar()"
        >{{ mustRefresh ? "重新读取资料" : "重新读取头像" }}</UiButton
      >
    </div>
    <div v-if="conflict" class="form-feedback">
      <p>资料已发生变化，所选图片仍保留。请读取最新资料后再操作。</p>
      <UiButton :loading="refreshingProfile" @click="refreshProfile"
        >读取最新资料</UiButton
      >
      <UiButton v-if="refreshedProfile" @click="adoptLatestVersion"
        >基于最新资料继续操作</UiButton
      >
    </div>
    <p v-if="resultMessage" class="form-note form-success" role="status">
      {{ resultMessage }}
    </p>
  </section>
</template>

<style scoped>
.avatar-heading {
  margin: 0 0 6px;
  font-size: 13px;
  font-weight: 600;
}
.avatar-row,
.avatar-candidate {
  display: flex;
  align-items: center;
  gap: 18px;
}
.avatar-image,
.avatar-candidate > img {
  width: 72px;
  height: 72px;
  flex-shrink: 0;
  border-radius: 50%;
  object-fit: cover;
  background: var(--surface-subtle);
}
.avatar-image {
  display: grid;
  place-items: center;
  overflow: hidden;
}
.avatar-image img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.avatar-fallback {
  font-size: 26px;
  color: var(--text-secondary);
}
.avatar-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.avatar-file {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}
.avatar-file:focus-visible + button {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
}
.avatar-candidate {
  align-items: flex-start;
}
.avatar-candidate > div {
  min-width: 0;
}
.avatar-filename {
  overflow-wrap: anywhere;
}
.avatar-candidate .form-actions {
  margin-top: 10px;
}
</style>
