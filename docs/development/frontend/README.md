# 前端开发基础

前端位于 `web/`，使用 Vue 3、TypeScript、Vite、Vue Router 和自定义组件；旋转验证使用精确版本 `go-captcha-vue 2.0.7`。npm 锁文件固定依赖，Node 版本要求见 `web/package.json`。正式 Account 客户端已连接真实 Go 服务，提供登录、旋转挑战、Session 恢复、注销、受保护的空首页，以及本人资料/头像、主题和修改密码。范围分别见 [D26 认证工作项](../work-items/d26-account-authentication.md)与[个人设置工作项](../work-items/d26-personal-settings.md)；其余业务页面及完整 D26 仍待后续交付。

## 启动与检查

在仓库根目录执行：

```sh
npm ci --prefix web
npm run dev --prefix web
```

正式认证入口为 [/login](http://127.0.0.1:5173/login)，开发组件展示为 [/debug](http://127.0.0.1:5173/debug)。开发服务器只绑定 `127.0.0.1:5173`；冲突时退出，不悄悄切换端口。

连接自有 Central 时，显式提供其 loopback HTTP 端口，例如后端监听 `127.0.0.1:8080` 时：

```sh
AGENTEAM_DEV_API_TARGET=http://127.0.0.1:8080 npm run dev --prefix web
```

代理只转发 `/api/v1`，保留浏览器 Host/Origin；Central 的 `PUBLIC_ORIGIN` 应为浏览器外部地址，本例为 `http://127.0.0.1:5173`。后端数据库、keyring、MinIO 等必需配置沿[后端指南](../backend/README.md)。目标必须是带显式端口的 loopback HTTP origin，不接受凭据、路径、query 或 hash；不添加 CORS、Cookie rewrite 或 CSRF 豁免。未配置目标时仍可运行纯前端展示，认证 API 不可用，不会提供假登录。

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
| `web/src/components/layout/` | AppShell、SystemNav 与 SettingsShell；系统导航、路由内容区和设置侧栏 |
| `web/src/api/` | 六项认证与八项个人设置 Account 调用、运行时 DTO、头像字节与安全 Problem 解析 |
| `web/src/composables/` | 单一 Session 协调者、App 生命周期内的设置草稿，以及主题、按钮反馈、浮层与键盘工具 |
| `web/src/styles/` | 唯一共享 token、基础规则与公共组件样式 |
| `web/src/router/` | 路由和导航元数据 |
| `web/src/views/auth/` 与 `HomeView.vue` | 正式登录/挑战页面与受保护空首页 |
| `web/src/views/settings/` | 本人资料与头像、外观、修改密码三个真实设置页面 |
| `web/src/views/debug/` | 开发环境组件展示、演示数据与展示布局 |
| `web/src/tests/` | Vitest + Vue Test Utils 交互与基线检查 |
| `tests/account/` 与 `tests/account-captcha-web/` | 正式构建、完整真实后端与浏览器的认证及个人设置组合验收 |

公共组件不能导入 `views/debug/`，不能包含演示数据、业务 API 或业务状态规则。正式页面直接引用相同公共组件；Debug 不是组件定义的位置。展示网格、目录和示例编排不约束正式业务布局。

## 骨架与新页面

`/login` 使用独立认证布局。根路径 `/` 先检查真实 Session，再显示 AppShell、当前身份、退出操作、“首页”标题和空 Dashboard 容器。右上本人名称直达资料页，首页初始密码建议直达修改密码页，可继续使用系统。检查失败提供恢复入口，不显示旧的受保护内容。AppShell 内容区独立滚动，认证顶部区域在窄宽度或放大时可换行，保留品牌及退出操作。项目级导航以后放在项目工作区内部；当前没有项目、Inbox、搜索或其它尚未实现的入口。

在 `router/index.ts` 的 `pages` 注册正式页面，使用懒加载 `component`，为需要导航的路由声明 `meta.navigation: { label, order }`。SystemNav 从路由元数据读取入口，不需要复制导航数组或改写骨架。认证路由通过 `router/auth.ts` 及单一 `useSession` 协调，登录返回目标仅接受 `/` 和下列三个设置叶子；未知路径显示未找到提示。

客户端使用固定同源相对 Account 路径，分别传递匿名或 Session CSRF，不持久化密码、challenge pass 或 token。登录成功后还需 GET Session 确认身份及 Session CSRF；注销确认后才退出。认证与设置操作复用同一个请求协调者，逻辑超时不会提前释放尚未结束的实际请求。具体状态、取消和迟到结果规则见[D26 认证工作项](../work-items/d26-account-authentication.md)、[个人设置工作项](../work-items/d26-personal-settings.md)及[正式 Account API](../../../api/openapi/account.json)。

采用 HTML5 History。开发服务器和 Vite preview 支持回退；生产资源托管属于 D28，Central 当前未托管 SPA。非 API 的 History 页面才能回退到 `index.html`，API、缺失资产和服务端错误不能直接回退。认证与个人设置的实际浏览器验收使用自有测试服务器托管正式 `web/dist` 并反代完整 Central，不把该测试服务器或 `vite preview` 当作生产部署；开发代理另有静态/类型检查，未单独进行真实 dev-server 浏览器验收。

## 本人设置

`/settings` 进入资料页；三个叶子均要求当前本人 Session。普通用户和管理员使用相同本人页面，不提供管理员代改入口。

| 路由 | 实际操作 |
| --- | --- |
| `/settings/profile` | 修改用户名、显示名；独立选择、预览、上传或移除 JPG/PNG/WebP 头像，读取服务端确认的头像 |
| `/settings/appearance` | 预览浅色、深色或跟随系统；明确保存后持久化，取消恢复已保存偏好 |
| `/settings/password` | 输入当前密码、新密码和确认；成功后清空密码，并确认新 Session/CSRF 后再允许后续身份操作 |

资料、头像、主题和密码分别提交，不存在跨分区保存。版本冲突保留草稿并提示重新检查；未保存内容在菜单、返回和退出时有离页确认。同一 Session 重验保留草稿和主题预览，真实身份失效清除旧草稿、候选头像及其 Blob URL。

写结果未确认时保留原 key、完整输入及必要的 File 引用，供用户检查当前会话后明确重试或放弃，不自动更换 key。GET Session 不作为原写命令的成功回执。改密严格成功的反馈与随后 Session/资料读取失败分别保留，避免提示用户重复修改；密码与候选材料只在内存中持有。

个人设置源码已提交为 `c54f73f3324caa11608d84e5d207141985eb6074`，验证范围及原失败见[个人设置验收记录](../agent-team/personal-settings-verification.md)。邀请、恢复、管理员设置等未交付页面不显示假入口。

## Debug 与主题

Debug 覆盖按钮、表单、选择、树、内容容器、消息、浮层、异常和导航组件，以及实际 CSS 参数。演示操作只修改本地状态，消息使用文本插值，搜索范围仅为组件目录。

Debug 路由及动态导入使用编译期 `import.meta.env.DEV` 条件。生产构建不包含 Debug 导航、路由、示例代码及数据；`/debug` 在生产预览中进入未找到页面。这里的“开发环境”由 Vite 编译模式决定，不是后端用户权限。

未认证时主题跟随系统，入口在首次绘制前应用系统主题。正式页面在当前 Session 确认后应用服务端用户偏好；外观设置可以即时预览，只有明确保存才写入账号偏好。Debug 可以即时切换浅深主题，选择仅保存在当前会话内存中，不存储账号偏好，也不代表个人设置接口。

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
