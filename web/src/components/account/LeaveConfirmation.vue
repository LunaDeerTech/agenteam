<script setup lang="ts">
import { UiButton, UiDialog } from "../ui";
import { leaveDialogOpen, finishAccountLeave } from "../../account/leave";
</script>
<template>
  <UiDialog
    :open="leaveDialogOpen"
    title="放弃当前编辑？"
    @update:open="!$event && finishAccountLeave(false)"
  >
    <p>
      离开会清除未保存的内容。已发出的操作仍可能完成，放弃不会取消服务端操作。
    </p>
    <template #footer>
      <UiButton @click="finishAccountLeave(false)">继续编辑</UiButton>
      <UiButton variant="danger" @click="finishAccountLeave(true)"
        >放弃并离开</UiButton
      >
    </template>
  </UiDialog>
</template>
