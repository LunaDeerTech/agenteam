<script setup lang="ts">
import { nextTick, ref } from "vue";
import { UiButton } from "../../components/ui";
import { accountEnabled, accountErrorMessage } from "../../account/transport";
import { listUsers, formatInstant } from "../../system/directory";
import { useSystemAccess } from "../../system/requests";
import { useCursorList } from "../../system/useCursorList";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
const access = useSystemAccess();
const list = useCursorList(listUsers);
const results = ref<HTMLElement>();
const loadFailure = ref<HTMLElement>();
async function navigate(action: () => Promise<void>) {
  await action();
  await nextTick();
  const target = list.error.value ? loadFailure.value : results.value;
  target?.focus();
  target?.scrollIntoView({ block: "start" });
}
</script>

<template>
  <div class="system-directory">
    <SettingsPageActions>
      <UiButton
        :disabled="!accountEnabled || !access.allowed.value"
        :loading="list.loading.value"
        @click="navigate(list.first)"
        >刷新</UiButton
      >
    </SettingsPageActions>
    <p v-if="!accountEnabled" class="form-feedback">
      当前为界面预览。连接账号服务后可查看用户。
    </p>
    <div v-else-if="access.denied.value" class="form-feedback" role="alert">
      <p>当前没有系统管理权限，用户数据已清除。</p>
      <UiButton @click="access.recheck">重新检查权限</UiButton>
    </div>
    <div
      v-else-if="list.error.value"
      ref="loadFailure"
      tabindex="-1"
      class="form-feedback"
      role="alert"
    >
      <p>{{ accountErrorMessage(list.error.value) }}</p>
      <UiButton :loading="list.loading.value" @click="navigate(list.reload)"
        >重新读取</UiButton
      >
      <UiButton
        v-if="list.index.value > 0"
        :disabled="list.loading.value"
        @click="navigate(list.first)"
        >返回第一页</UiButton
      >
    </div>
    <p class="directory-status form-note" role="status" aria-live="polite">
      {{
        list.loading.value
          ? "正在读取用户…"
          : list.loaded.value
            ? `第 ${list.index.value + 1} 页，已读取 ${list.items.value.length} 位用户。`
            : ""
      }}
    </p>
    <div
      ref="results"
      tabindex="-1"
      class="directory-results"
      aria-label="用户目录结果"
    >
      <p
        v-if="list.loaded.value && !list.items.value.length"
        class="directory-empty"
      >
        暂无已注册用户。
      </p>
      <ul
        v-if="list.items.value.length"
        class="directory-list"
        aria-label="用户目录"
        :aria-busy="list.loading.value"
      >
        <li
          v-for="user in list.items.value"
          :key="user.id"
          class="directory-row"
        >
          <div class="directory-object">
            <strong>{{ user.display_name || user.email }}</strong>
            <span v-if="user.display_name" class="directory-secondary">{{
              user.email
            }}</span>
            <span class="directory-meta"
              >@{{ user.username }} <span aria-hidden="true">·</span> 注册于
              <time
                :datetime="user.created_at"
                :title="user.created_at"
                :aria-label="user.created_at"
                >{{ formatInstant(user.created_at) }}</time
              >
            </span>
          </div>
          <span class="directory-role">{{
            user.role === "admin" ? "管理员" : "普通用户"
          }}</span>
        </li>
      </ul>
    </div>
    <nav
      v-if="list.loaded.value && access.allowed.value"
      class="directory-pagination"
      aria-label="用户分页"
    >
      <span class="form-note">第 {{ list.index.value + 1 }} 页</span>
      <div class="form-actions">
        <UiButton
          :disabled="list.loading.value || list.index.value === 0"
          @click="navigate(list.previous)"
          >上一页</UiButton
        >
        <UiButton
          :disabled="list.loading.value || !list.nextCursor.value"
          @click="navigate(list.next)"
          >下一页</UiButton
        >
      </div>
    </nav>
  </div>
</template>

<style scoped>
.directory-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.directory-list {
  list-style: none;
  padding: 0;
  margin: 24px 0;
}
.directory-status {
  min-height: 20px;
  margin-bottom: 0;
}
.directory-results {
  scroll-margin-block: 12px;
}
.directory-results:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 5px;
}
.directory-row {
  display: flex;
  gap: 24px;
  justify-content: space-between;
  align-items: flex-start;
  padding: 18px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.directory-row:first-child {
  border-top: 1px solid var(--border-subtle);
}
.directory-object {
  display: grid;
  gap: 5px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.directory-object strong {
  font-size: 14px;
  font-weight: 550;
}
.directory-secondary,
.directory-role {
  font-size: 13px;
}
.directory-meta {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.6;
}
.directory-role {
  flex: none;
  color: var(--text-secondary);
  padding-block: 2px;
}
.directory-empty {
  padding: 40px 0;
  color: var(--text-secondary);
}
@media (max-width: 480px) {
  .directory-row {
    gap: 12px;
  }
}
</style>
