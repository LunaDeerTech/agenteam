# 前端开发基础

前端位于 `web/`，使用 Vue 3、TypeScript、Vite、Vue Router 和自定义组件。npm 锁文件固定依赖；Node 版本要求见 `web/package.json`（当前验证为 Node 24）。Go 服务、认证和业务接口尚未实现。

## 启动与检查

在仓库根目录执行：

```sh
npm ci --prefix web
npm run dev --prefix web
```

访问 [开发入口](http://127.0.0.1:5173/debug)。开发服务器只绑定本机，使用固定端口 5173；冲突时退出，不悄悄切换端口。

```sh
npm run type-check --prefix web
npm run test:unit --prefix web
npm run format:check --prefix web
npm run build --prefix web
npm run preview --prefix web
```

`npm run check --prefix web` 执行格式检查、单元测试和生产构建。`format` 会修改文件，`format:check` 只检查。

## 代码边界

| 目录 | 职责 |
| --- | --- |
| `web/src/components/ui/` | 可复用控件和内容组件；公开导出及接口类型在 `index.ts` 和 `types.ts` |
| `web/src/components/layout/` | AppShell 与 SystemNav；仅系统级顶部导航和路由内容区 |
| `web/src/composables/` | 主题、按钮反馈、浮层与键盘工具 |
| `web/src/styles/` | 唯一共享 token、基础规则与公共组件样式 |
| `web/src/router/` | 路由和导航元数据 |
| `web/src/views/debug/` | 开发环境组件展示、演示数据与展示布局 |
| `web/src/tests/` | Vitest + Vue Test Utils 交互与基线检查 |

公共组件不能导入 `views/debug/`，不能包含演示数据、业务 API 或业务状态规则。正式页面直接引用相同公共组件；Debug 不是组件定义的位置。展示网格、目录和示例编排不约束正式业务布局。

## 骨架与新页面

AppShell 保持系统导航与内容区；内容区独立滚动。项目级导航以后放在项目工作区内部，不在所有页面添加第二层导航。业务入口尚未实现，本轮没有项目、Inbox、用户设置或搜索业务入口。

在 `router/index.ts` 的 `pages` 注册正式页面，使用懒加载 `component`，为需要导航的路由声明 `meta.navigation: { label, order }`。SystemNav 从路由元数据读取入口，不需要复制导航数组或改写骨架。生产根路径暂时只有骨架和空内容区，未知路径显示未找到提示。

采用 HTML5 History。开发服务器和 Vite preview 支持回退；未来 Central 静态服务必须将前端非资源路径回退到 `index.html`，API、资源文件和服务端错误不能直接回退。具体服务端实现留到 Central 开发。

## Debug 与主题

Debug 覆盖按钮、表单、选择、树、内容容器、消息、浮层、异常和导航组件，以及实际 CSS 参数。演示操作只修改本地状态，消息使用文本插值，搜索范围仅为组件目录。

Debug 路由及动态导入使用编译期 `import.meta.env.DEV` 条件。生产构建不包含 Debug 导航、路由、示例代码及数据；`/debug` 在生产预览中进入未找到页面。这里的“开发环境”由 Vite 编译模式决定，不是后端用户权限。

主题默认跟随系统，入口在首次绘制前应用系统主题。Debug 可以即时切换浅深主题；选择仅保存在当前会话内存中，刷新重新跟随系统，不存储账号偏好。未来账号主题加载在主题服务入口接入；不要把 Debug 状态当成个人设置接口。

已确认的[样式规范](../../frontend-design/styles/README.md)保持不变。旧独立 HTML 展示已由 Vue Debug 替代。

## 使用组件

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { UiButton, UiField, UiInput, UiDialog } from '../../../web/src/components/ui'
import { useActionFeedback } from '../../../web/src/composables/useActionFeedback'

const name = ref('')
const open = ref(false)
const { state, error, run } = useActionFeedback()

async function save() {
  await run(async () => {
    // 在正式页面接入实际保存逻辑；完成后才显示成功。
  })
}
</script>

<template>
  <UiField label="名称" :error="error">
    <template #default="field">
      <UiInput :id="field.id" v-model="name" :invalid="field.invalid" :aria-describedby="field.describedby" />
    </template>
  </UiField>
  <UiButton :state="state" success-label="已保存" @click="save">保存修改</UiButton>
  <UiDialog v-model:open="open" title="确认">
    <p>正文与操作区分别组织。</p>
    <template #footer="{ close }">
      <UiButton @click="close">取消</UiButton>
    </template>
  </UiDialog>
</template>
```

示例导入路径仅表示源文件位置；实际页面按所在目录调整相对路径。组件支持的 props / events 以源代码类型为准，使用说明见[公共组件接口](components.md)。

检查范围和结果见[验证记录](verification.md)。
