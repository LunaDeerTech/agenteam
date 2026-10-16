<script setup lang="ts">
import { ref } from "vue";
import { UiButton } from "../../components/ui";
import { accountEnabled } from "../../account/transport";
import { listUsers, formatInstant } from "../../system/directory";
import { useSystemAccess } from "../../system/requests";
import { useCursorList } from "../../system/useCursorList";
import SettingsPageActions from "../../layouts/SettingsPageActions.vue";
import SystemList from "../../components/system/SystemList.vue";
const access = useSystemAccess();
const list = useCursorList(listUsers);
const listView = ref<{ refresh: () => Promise<void> }>();
</script>

<template>
  <div class="system-directory">
    <SettingsPageActions>
      <UiButton
        :disabled="!accountEnabled || !access.allowed.value"
        :loading="list.pending.value"
        @click="listView?.refresh()"
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
    <SystemList
      ref="listView"
      :list="list"
      :row-key="(user) => user.id"
      label="用户目录"
      results-label="用户目录结果"
      pagination-label="用户分页"
      empty-text="暂无已注册用户。"
      :disabled="!accountEnabled || !access.allowed.value"
    >
      <template #item="{ item: user }">
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
      </template>
      <template #actions="{ item: user }">
        <span class="directory-role">{{
          user.role === "admin" ? "管理员" : "普通用户"
        }}</span>
      </template>
    </SystemList>
  </div>
</template>

<style scoped>
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
  color: var(--text-secondary);
  padding-block: 2px;
}
</style>
