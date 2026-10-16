import { computed, effectScope, readonly, ref, shallowRef, watch } from "vue";
import { authenticatedRequest, sessionIdentity, useSession } from "./session";
import { AccountError, accountEnabled, type AccountImage } from "./transport";
import { isProfile, type ProfileResponse } from "./types";
import { boundedBlobURL } from "./image";

type Identity = ReturnType<typeof sessionIdentity>;
const session = useSession();
const url = ref("");
const imageLoading = ref(false);
const profileLoading = ref(false);
const error = shallowRef<unknown>();
let metadata: ProfileResponse["avatar"] | undefined;
let version = 0n;
let removedAtVersion = 0n;
let revision = 0;
let imageGeneration = 0;
let profileRequest: Promise<void> | undefined;
let imageRequest: Promise<void> | undefined;

function sameIdentity(owner: Identity) {
  const current = sessionIdentity();
  return (
    session.hasIdentity.value &&
    owner.generation === current.generation &&
    owner.userId === current.userId &&
    owner.sessionId === current.sessionId
  );
}

function releaseURL() {
  if (url.value) URL.revokeObjectURL(url.value);
  url.value = "";
}

function reset() {
  revision++;
  imageGeneration++;
  releaseURL();
  metadata = undefined;
  version = 0n;
  removedAtVersion = 0n;
  imageRequest = undefined;
  profileRequest = undefined;
  imageLoading.value = false;
  profileLoading.value = false;
  error.value = undefined;
}

function readImage(owner: Identity): Promise<void> {
  if (
    !metadata ||
    !sameIdentity(owner) ||
    session.status.value !== "authenticated"
  )
    return Promise.resolve();
  if (imageRequest) return imageRequest;
  const source = metadata;
  const captured = ++imageGeneration;
  const current = () =>
    sameIdentity(owner) && captured === imageGeneration && metadata === source;
  imageLoading.value = true;
  error.value = undefined;
  const request = (async () => {
    let nextURL = "";
    try {
      const image = await authenticatedRequest<AccountImage>("/me/avatar", {
        binary: true,
        current,
      });
      nextURL = await boundedBlobURL(image, source);
      if (!current()) return;
      releaseURL();
      url.value = nextURL;
      nextURL = "";
    } catch (failure) {
      if (current()) {
        releaseURL();
        error.value = failure;
      }
    } finally {
      if (nextURL) URL.revokeObjectURL(nextURL);
      if (current()) imageLoading.value = false;
    }
  })();
  imageRequest = request;
  void request.finally(() => {
    if (imageRequest === request) imageRequest = undefined;
  });
  return request;
}

// Only publish confirmed server profiles. Local candidates never enter this store.
export function acceptAvatarProfile(
  value: ProfileResponse,
  owner: Identity,
): Promise<void> {
  if (
    !accountEnabled ||
    !sameIdentity(owner) ||
    !isProfile(value) ||
    value.user.id !== owner.userId ||
    BigInt(value.user.version) < version ||
    BigInt(value.user.version) <= removedAtVersion
  )
    return Promise.resolve();
  const next = value.avatar;
  const changed =
    metadata === undefined ||
    metadata?.sha256 !== next?.sha256 ||
    metadata?.media_type !== next?.media_type ||
    metadata?.byte_size !== next?.byte_size;
  revision++;
  version = BigInt(value.user.version);
  removedAtVersion = 0n;
  if (changed) {
    imageGeneration++;
    imageRequest = undefined;
    imageLoading.value = false;
    releaseURL();
    metadata = next ? { ...next } : null;
    error.value = undefined;
  }
  return metadata && !url.value ? readImage(owner) : Promise.resolve();
}

export function confirmAvatarRemoval(removedVersion: string, owner: Identity) {
  if (!sameIdentity(owner)) return;
  const removed = BigInt(removedVersion);
  // A later confirmed profile wins over an older completion callback.
  if (removed < version) return;
  revision++;
  imageGeneration++;
  removedAtVersion = removed;
  version = removed;
  metadata = null;
  imageRequest = undefined;
  imageLoading.value = false;
  error.value = undefined;
  releaseURL();
}

export function rejectAvatarImage(failedURL: string) {
  if (!failedURL || failedURL !== url.value) return;
  releaseURL();
  error.value = new AccountError("INVALID_RESPONSE");
}

export function refreshAccountAvatar(): Promise<void> {
  if (!accountEnabled || session.status.value !== "authenticated")
    return Promise.resolve();
  if (profileRequest) return profileRequest;
  const owner = sessionIdentity();
  const captured = revision;
  const current = () => sameIdentity(owner) && captured === revision;
  profileLoading.value = true;
  error.value = undefined;
  const request = (async () => {
    try {
      const value = await authenticatedRequest("/me", { current }, isProfile);
      if (!current()) return;
      await acceptAvatarProfile(value, owner);
    } catch (failure) {
      if (current()) {
        releaseURL();
        error.value = failure;
      }
    } finally {
      if (sameIdentity(owner)) profileLoading.value = false;
    }
  })();
  profileRequest = request;
  void request.finally(() => {
    if (profileRequest === request) profileRequest = undefined;
  });
  return request;
}

const scope = effectScope(true);
scope.run(() => {
  watch(session.generation, reset, { flush: "sync" });
  watch(
    [session.status, session.generation],
    ([status]) => {
      if (status === "authenticated" && metadata === undefined)
        void refreshAccountAvatar();
      else if (
        status === "authenticated" &&
        metadata &&
        !url.value &&
        !error.value
      )
        void readImage(sessionIdentity());
    },
    { immediate: true },
  );
});

export function useAccountAvatar() {
  return {
    url: readonly(url),
    loading: computed(() => imageLoading.value || profileLoading.value),
    error: readonly(error),
  };
}

if (import.meta.hot)
  import.meta.hot.dispose(() => {
    scope.stop();
    reset();
  });
