# 前端开发基础

前端位于 `web/`，使用 Vue 3、TypeScript、Vite、Vue Router 和自定义组件；旋转验证使用精确版本 `go-captcha-vue 2.0.7`。npm 锁文件固定依赖，Node 版本要求见 `web/package.json`。正式 Account 客户端已连接真实 Go 服务，提供登录、旋转挑战、Session 恢复、注销、受保护的空首页，以及本人资料/头像、主题和修改密码、邀请兑换与找回/重置密码。管理员用户目录与邀请管理也已实现，验收状态见下节。范围分别见 [D26 认证工作项](../work-items/d26-account-authentication.md)、[个人设置工作项](../work-items/d26-personal-settings.md)、[公开入口工作项](../work-items/d26-public-account-entry.md)、[目录工作项](../work-items/d27-system-user-directory-ui.md)及[邀请管理工作项](../work-items/d27-system-invitation-ui.md)；其余业务页面及完整 D26/D27 仍待后续交付。

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
| `web/src/api/` | 六项认证、八项个人设置、五项公开入口 Account 调用、System 用户目录 GET，以及邀请列表/创建/重发/撤销与人工投递重试；运行时 DTO、头像字节与安全 Problem 解析 |
| `web/src/composables/` | 同一 Cookie 请求协调者、公开入口 owner、页面期用户目录状态与 App 生命周期内的本人设置/邀请草稿，以及主题、按钮反馈、浮层与键盘工具 |
| `web/src/styles/` | 唯一共享 token、基础规则与公共组件样式 |
| `web/src/router/` | 路由和导航元数据 |
| `web/src/views/auth/` 与 `HomeView.vue` | 正式登录/挑战、邀请/找回/重置页面与受保护空首页 |
| `web/src/views/settings/` | 本人资料与头像、外观、修改密码三个真实设置页面 |
| `web/src/views/system/` | 管理员系统设置壳、用户与待注册邀请两个叶子，含非管理员及权限拒绝状态 |
| `web/src/views/debug/` | 开发环境组件展示、演示数据与展示布局 |
| `web/src/tests/` | Vitest + Vue Test Utils 交互与基线检查 |
| `tests/account/` 与 `tests/account-captcha-web/` | 正式构建、完整真实后端与浏览器的认证、个人设置、公开入口、系统用户目录及邀请管理组合验收 |

公共组件不能导入 `views/debug/`，不能包含演示数据、业务 API 或业务状态规则。正式页面直接引用相同公共组件；Debug 不是组件定义的位置。展示网格、目录和示例编排不约束正式业务布局。

## 骨架与新页面

`/login` 使用独立认证布局。根路径 `/` 先检查真实 Session，再显示 AppShell、当前身份、退出操作、“首页”标题和空 Dashboard 容器。右上本人名称直达资料页，首页初始密码建议直达修改密码页，可继续使用系统。检查失败提供恢复入口，不显示旧的受保护内容。AppShell 内容区独立滚动，认证顶部区域在窄宽度或放大时可换行，保留品牌及退出操作。项目级导航以后放在项目工作区内部；当前没有项目、Inbox、搜索或其它尚未实现的入口。

在 `router/index.ts` 的 `routes` 注册正式页面，使用懒加载 `component`，为需要导航的路由声明 `meta.navigation: { label, order }`。SystemNav 从路由元数据读取入口，不需要复制导航数组或改写骨架。认证路由通过 `router/auth.ts` 及单一 `useSession` 协调，登录返回目标仅接受六个精确路径：`/`、`/settings/profile`、`/settings/appearance`、`/settings/password`、`/system/users`、`/system/invitations`。`/settings`、`/system`、query/hash、数组、外部 URL 与未知系统叶子均不是返回目标；未知路径显示未找到提示。系统导航只向当前已确认且未被系统权限拒绝的 admin 展示“系统设置”。

客户端使用固定同源相对 Account/System 路径，写操作分别传递匿名或 Session CSRF，不持久化密码、challenge pass 或 token。登录成功后还需 GET Session 确认身份及 Session CSRF；注销确认后才退出。认证、本人设置、公开入口、系统目录与邀请操作复用同一个请求协调者，逻辑超时不会提前释放尚未结束的实际请求。具体状态、取消和迟到结果规则见[D26 认证工作项](../work-items/d26-account-authentication.md)、[个人设置工作项](../work-items/d26-personal-settings.md)及[正式 Account API](../../../api/openapi/account.json)。

采用 HTML5 History。开发服务器和 Vite preview 支持回退；生产资源托管属于 D28，Central 当前未托管 SPA。非 API 的 History 页面才能回退到 `index.html`，API、缺失资产和服务端错误不能直接回退。认证、个人设置、公开入口、系统目录与邀请管理的实际浏览器验收使用自有测试服务器托管冻结候选的生产 dist 并反代完整 Central，不把该测试服务器或 `vite preview` 当作生产部署；开发代理另有静态/类型检查，未单独进行真实 dev-server 浏览器验收。

## 本人设置

`/settings` 进入资料页；三个叶子均要求当前本人 Session。普通用户和管理员使用相同本人页面，不提供管理员代改入口。

| 路由 | 实际操作 |
| --- | --- |
| `/settings/profile` | 修改用户名、显示名；独立选择、预览、上传或移除 JPG/PNG/WebP 头像，读取服务端确认的头像 |
| `/settings/appearance` | 预览浅色、深色或跟随系统；明确保存后持久化，取消恢复已保存偏好 |
| `/settings/password` | 输入当前密码、新密码和确认；成功后清空密码，并确认新 Session/CSRF 后再允许后续身份操作 |

资料、头像、主题和密码分别提交，不存在跨分区保存。版本冲突保留草稿并提示重新检查；未保存内容在菜单、返回和退出时有离页确认。同一 Session 重验保留草稿和主题预览，真实身份失效清除旧草稿、候选头像及其 Blob URL。

写结果未确认时保留原 key、完整输入及必要的 File 引用，供用户检查当前会话后明确重试或放弃，不自动更换 key。GET Session 不作为原写命令的成功回执。改密严格成功的反馈与随后 Session/资料读取失败分别保留，避免提示用户重复修改；密码与候选材料只在内存中持有。

个人设置源码已提交为 `c54f73f3324caa11608d84e5d207141985eb6074`，验证范围及原失败见[个人设置验收记录](../agent-team/personal-settings-verification.md)。本节只覆盖本人设置；管理员用户目录见下节，不提供代改他人资料的操作。

## 公开账号入口

[公开入口验收记录](../agent-team/public-account-entry-verification.md)对应25源提交 `787a5c7eeadf5e5f37bab97cbf030b4a745b72af`；三个页面使用独立居中布局，不展示已登录导航。

| 路由 | 实际操作 |
| --- | --- |
| `/invite` | 校验真实邀请链接、填写账号并兑换；成功后由用户明确登录，不自动创建Session |
| `/forgot-password` | 提交找回申请并显示统一渠道说明；202不证明账号存在或已经投递 |
| `/reset-password` | 校验链接、设置密码；确认后清输入并核对当前Session，不猜目标身份或自动改绑账号 |

链接材料在history建立前移除，仅由私有页面owner持有；刷新后须重新打开原链接。有效Session可与匿名CSRF共存，三个公开写入仍使用匿名authority。`/login?switch=1`只提供显式身份选择，必须由用户点击注销；不额外扩展上述登录返回闭集。原写结果未知时保持原key/context/body供明确重试，不自动重发。

本卡151项pure/格式/类型/构建通过，四个新顶层和原认证四个、设置四个顶层按固定输入及未变语义分轮闭合；首红、CDP根因未知与观察修订保留。独立真实跨账号及已验management/ledger00018组合通过。测试clone只证明实际响应JSON形状，不证明原stream尾部；CSS `zoom=2`不是原生浏览器缩放。实际投递渠道为受限backend_log，没有新增真实SMTP、生产SPA托管或Vite开发代理浏览器验收，完整D26/D27仍未完成。

## 系统设置与用户目录

[目录工作项 rev1](../work-items/d27-system-user-directory-ui.md)的 `input04` 基于已接受的[注册时间读口](../agent-team/system-user-directory-read-verification.md) `3affc01` 与[共享遮罩焦点修复](../agent-team/dialog-outside-focus-repair-verification.md) `b53895f`。目录 UI 已获独立最终 PASS 并由主线程采纳：作者完整检查为 16 文件、244 项测试及格式/类型/构建通过；三个新顶层、三个必要旧顶层和一个独立补充组的真实浏览器检查通过。验收限于本节只读目录与设置壳，不代表完整 D27。

| 路由 | 实际行为 |
| --- | --- |
| `/system` | 固定重定向 `/system/users`，不记忆上次系统栏目 |
| `/system/users` | “用户与邀请 → 用户”叶子，读取当前管理员可见的真实用户目录 |

非管理员直接链接保留原 URL，显示“无权访问系统设置”、返回首页与重新检查权限入口，不挂载目录或发列表请求。系统实例复用 `SettingsShell`，只含这一组菜单，不显示侧栏退出；顶部本人名称和退出仍沿 App 的原流程，个人草稿须先确认继续编辑或放弃再离页。760px 以下复用 `UiDrawer`，保留焦点限制、Escape/遮罩关闭和触发焦点恢复。

目录分开显示邮箱、用户名、显示名、角色和 UTC 注册时间；显示名为空时回落邮箱。桌面五列可换行，窄屏逐字段堆叠；长邮箱和名称正常换行，五个字段均可阅读。`time` 保留完整六位微秒 canonical 值，可见值到秒。只有刷新、上一页和下一页；固定每页 25 项，上页重新读取，成功后才改变页位置，刷新、离页及身份变化清除旧分页材料。加载与失败隐藏旧行，空结果、读取错误和无权限分别显示；失效 cursor 由用户明确返回首页重新加载。

`system-account.ts` 只使用同源 `GET /api/v1/system/users?limit=25[&cursor=...]`，不携带 body、CSRF 或命令 key；沿原 transport 的 600,000B JSON 上限和实际 body/cancel 结束规则。专用 DTO 恰为原 User 八字段加 `created_at`，完整校验日历、微秒排序、UUID/版本、重复 ID 和 cursor 字符串边界，成功才发布冻结页面。目录行不进入当前 Session、主题或个人草稿，目录/cursor 不写入页面 URL、日志或持久存储。

目录 GET 共用 `useSession` 的唯一 Cookie 请求 owner：30 秒可见等待结束不提前释放仍在进行的 fetch/body/cancel，离页只放弃本页 system 操作。真实 401 清除失效身份；当前身份的 403 `FORBIDDEN` 清目录、隐藏入口并显示无权限，不自动注销或改写 User.role，后续成功 Session 检查或真实身份变化才清除拒绝。迟到结果不能回写新账号、新 Session 或已离开的页面。

纯检查与定点目录测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-account-client.spec.ts src/tests/system-user-directory-state.spec.ts src/tests/system-user-directory.spec.ts
```

真实检查需先按锁安装 `tests/account-captcha-web` 依赖、构建当前 `web/dist`，并取得任务自有独占资源窗口。浏览器使用 `/usr/bin/chromium`；`AGENTEAM_AUTH_WEB_RUNTIME` 必须是预建、绝对路径且至多 45 字符的私有目录。Go1.27.1、MinIO、PG17.x（最低17.8）等准备沿[后端指南](../backend/README.md)，只选择本卡新三组及共享 owner/router/设置壳涉及的旧三组：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-objects.sh -run '^TestAccount(SystemUserDirectoryWeb(ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation)$'
```

单浏览器 45 秒、Go 顶层 2 分钟、workers=1/retries=0 及脚本原 race/count1/每包 6 分钟预算不变。管理员和普通账号经正式流程准备，其余 28 行仅是明确标记的分页/长文本样本，不作为邀请注册证据。观察器的响应 JSON 或原 reader 旁路不能证明 Cookie owner 尾部结束，该边界由受控原 transport 延迟屏障单独验证。本节目录不提供用户详情/修改或 Model 管理；邀请管理见下节。目录卡未扩大生产 SPA 托管、真实 Vite 代理、SMTP、Provider/tools 等既有边界，也不代表完整 D27。

## 系统邀请管理

[邀请管理工作项 rev4](../work-items/d27-system-invitation-ui.md)的最终 `input07` 已通过独立验收，基于已接受的[邀请投递读口](../agent-team/system-invitation-delivery-read-verification.md) `9b32015` 与[共享 modal 焦点恢复](../agent-team/modal-focus-restoration-verification.md) `79f922e`。作者完整 `npm run check` 为 20 文件、453 项测试及格式/类型/构建通过；新增六组、旧五组和独立补充组的真实浏览器检查按固定输入与未变语义分轮闭合。

`/system/invitations` 是“用户与邀请 → 待注册邀请”叶子，与用户目录共用系统壳；`/system` 仍固定进入用户目录。页面只列当前有效的待注册邀请，显示邮箱、UTC 创建/到期时间和最近一次投递任务的接受时间、阶段、尝试次数、渠道、尝试结果及固定原因说明，不展示邀请链接或 token。列表固定 25 项，cursor 只在内存中持有，上页重新读取、刷新返回首页；加载、错误、空结果和失权分别显示。完整 DTO、微秒日历/排序和 cursor 边界经校验后才发布，桌面五列换行、窄屏逐字段堆叠。

创建严格确认 201，重发和人工重试投递严格确认 202，撤销严格确认 204 及原 body 实际空结束。前端不 trim 或规范化输入邮箱；重发沿用原有效链接，不续期。人工重试使用原观察的 job/version，先提示可能重复投递；终态与渠道不代表收件箱送达，也不提供后台日志中的私有链接。版本冲突保留原观察并提示重新加载；重新读取后可打开新预览，不静默替换 version 重发。

邀请命令沿同一 `useSession` Cookie owner 私有持有原 key、完整 body 与 CSRF；30 秒可见截止不提前释放实际 fetch/body/cancel 尾部。结果未确认时，检查 Session/列表不能代替原写回执，只有显式重试原请求或放弃；放弃后须成功重读再开始新操作。严格写确认与随后列表 GET 失败分别显示，新表单不会继承上一笔操作的完成反馈。App 保留草稿及离页确认 Promise，同 Session checking/失败临时卸载 Dialog，恢复后按固定顺序重挂；真正离页、身份/Session/CSRF 变化、401 或当前管理员 403 清除旧材料。未保存输入与未确认写入在导航、返回和注销前均需确认，选择继续编辑后焦点仍留在可操作的业务层。

纯检查与定点邀请测试：

```sh
npm run check --prefix web
npm run test:unit --prefix web -- src/tests/system-invitations-client.spec.ts src/tests/system-invitations-state.spec.ts src/tests/system-invitations.spec.ts src/tests/settings-shell.spec.ts
```

真实检查沿上节锁依赖、生产 dist、Go1.27.1、MinIO、PG17.8+ 与独占短私有 runtime 准备；以下选择覆盖新六组和旧五组，可按精确名称分轮执行：

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_AUTH_WEB_RUNTIME=/task-owned/private \
sh scripts/test-security.sh -run '^TestAccount(SystemInvitationsWeb(Lifecycle|DeliveryRetry|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)|AuthenticationWebSessionLifecycle|PersonalSettingsWebThemeAndNavigation|PublicEntryWebIdentityNavigation|SystemUserDirectoryWeb(AuthorityAndIdentity|NavigationAndLayouts))$'
```

原 45 秒浏览器、2 分钟顶层、workers=1/retries=0、race/count1/每包 6 分钟预算不变。实际创建/兑换、受限日志与自有私网 SMTP 失败后人工重试、真实 401/403、原请求重放和投递版本竞争均已验收；响应截断、一次 GET 503、Session 503 和限流时钟准备均是明确的自有 fixture 控制，合成 `pageshow` 不称真实浏览器恢复。reader 旁路只观察同一原请求的闭合响应，不作为 owner 尾部结束证明。浅深两主题、1440/1024/834/390 四宽度及抽屉/模态焦点通过；截图等待遮罩物理离场。未增加生产 SPA 托管、真实 Vite 代理或原生浏览器缩放验收，不交付用户代改、Model 管理或完整 D27。

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
