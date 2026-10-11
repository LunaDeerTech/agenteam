<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { UiButton, UiCard, UiInput } from '../../components/ui'
import { useSystemRunners } from '../../composables/useSystemRunners'
const page = useSystemRunners(),
  { material } = page
const input = ref<InstanceType<typeof UiInput> | null>(null),
  copied = ref(false),
  copyFailed = ref(false)
function clearDOM() {
  const field = input.value?.$el as HTMLInputElement | undefined
  if (field) field.value = ''
  copied.value = false
  copyFailed.value = false
}
watch(() => material.revision, clearDOM, { flush: 'sync' })
onBeforeUnmount(clearDOM)
async function copy() {
  if (!material.token || !page.visible.value) return
  const revision = material.revision
  try {
    await navigator.clipboard.writeText(material.token)
    if (material.revision === revision) copied.value = true
  } catch {
    if (material.revision === revision) copyFailed.value = true
  }
}
</script>
<template>
  <UiCard
    v-if="material.token && page.visible.value"
    class="runner-enrollment"
    aria-label="一次性登记材料"
  >
    <h2>一次性登记材料</h2>
    <p>仅本次显示，过期或离开页面后无法取回。重新签发会使旧身份、旧登记材料和连接失效。</p>
    <p>
      有效期至 <time :datetime="material.expires">{{ material.expires }}</time>
    </p>
    <label
      >登记 token<UiInput
        ref="input"
        :model-value="material.token"
        type="password"
        readonly
        autocomplete="off"
        spellcheck="false"
        aria-label="登记 token"
    /></label>
    <div class="runner-actions">
      <UiButton @click="copy">复制登记材料</UiButton
      ><UiButton variant="ghost" @click="page.clearMaterial">关闭登记材料</UiButton>
    </div>
    <p v-if="copied" role="status">已复制；请只交给对应 Runner 的登记输入。</p>
    <p v-if="copyFailed" role="alert">复制不可用，可在输入框中选择并复制。</p>
    <p>
      在目标设备配置私有身份文件、Central HTTPS 地址、Runner ID 和相同根目录，然后运行
      <code>agenteam-runner --enroll</code>，从标准输入提供
      token。不要把它写入命令参数、环境变量或日志。
    </p>
    <p>登记完成后重新读取设备状态。配置和在线状态不表示已开放执行能力。</p>
  </UiCard>
</template>
<style scoped>
.runner-enrollment {
  display: grid;
  gap: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.runner-enrollment label {
  display: grid;
  gap: 8px;
}
.runner-enrollment input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.runner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
</style>
