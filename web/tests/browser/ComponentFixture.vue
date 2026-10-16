<script setup lang="ts">
import { ref } from "vue";
import {
  UiButton,
  UiDialog,
  UiInput,
  UiMenu,
  UiSelect,
  UiTabs,
} from "../../src/components/ui";

const name = ref("");
const selection = ref("alpha");
const currentTab = ref("first");
const dialogOpen = ref(false);
const nestedOpen = ref(false);
const closeReason = ref("");
const action = ref("");
const options = [
  { value: "alpha", label: "Alpha" },
  { value: "disabled", label: "不可用选项", disabled: true },
  { value: "charlie", label: "Charlie" },
  {
    value: "long",
    label: "较长的选项 Long option for wrapping and viewport containment",
  },
];
const menuItems = [
  { id: "first", label: "First action" },
  { id: "disabled", label: "Disabled action", disabled: true },
  { id: "last", label: "Last action" },
];
const tabs = [
  { value: "first", label: "第一项" },
  { value: "disabled", label: "不可用分组", disabled: true },
  { value: "last", label: "最后一项" },
];
</script>

<template>
  <main class="fixture">
    <h1>共享 UI 测试夹具</h1>
    <p>此入口仅供组件测试，不属于产品路由。</p>
    <UiInput
      v-model="name"
      label="名称"
      hint="填写测试名称"
      :error="name ? undefined : '名称不能为空'"
      autocomplete="off"
    />
    <output aria-label="输入结果">{{ name }}</output>
    <UiInput label="禁用输入" disabled />
    <UiSelect
      v-model="selection"
      label="选择项"
      :options="options"
      hint="用于验证选择交互"
    />
    <output aria-label="选择结果">{{ selection }}</output>
    <UiSelect label="禁用选择" :options="options" disabled />
    <UiTabs v-model="currentTab" label="内容分组" :items="tabs">
      <template #default="{ value }"
        ><p>
          {{ value === "first" ? "第一项内容" : "最后一项内容" }}
        </p></template
      >
    </UiTabs>
    <div class="fixture-actions">
      <UiMenu label="操作菜单" :items="menuItems" @select="action = $event" />
      <UiButton @click="dialogOpen = true">打开测试弹窗</UiButton>
      <UiButton>外部按钮</UiButton>
    </div>
    <output aria-label="菜单结果">{{ action }}</output>
    <output aria-label="关闭原因">{{ closeReason }}</output>
    <UiDialog
      v-model:open="dialogOpen"
      title="测试弹窗"
      @close="closeReason = $event"
    >
      <UiInput label="弹窗输入" />
      <UiSelect v-model="selection" label="弹窗选择" :options="options" />
      <UiMenu label="弹窗菜单" :items="menuItems" @select="action = $event" />
      <UiButton @click="nestedOpen = true">打开第二层</UiButton>
      <template #footer="{ close }"
        ><UiButton @click="close()">完成</UiButton></template
      >
    </UiDialog>
    <UiDialog v-model:open="nestedOpen" title="第二层弹窗">
      <UiInput label="第二层输入" />
    </UiDialog>
  </main>
</template>

<style scoped>
.fixture {
  display: grid;
  gap: 18px;
  max-width: 620px;
  margin: 0 auto;
  padding: 28px 20px 48px;
}
.fixture-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
</style>
