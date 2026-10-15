# F0 隔离样板模块契约

纯浏览器 ES modules；Python 3 静态服务，不安装产品依赖、不调用 API。合成数据只在当前 tab 内变化。入口 `index.html?page=projects&theme=light&scenario=normal&accent=indigo`。`page` 支持 dashboard/projects/project/tasks/providers/knowledge/meetings/inbox/mcp/oauth/memory；`scenario` 支持 normal/long/empty/loading/error/readonly/unknown，long 等同 normal+ctx.long。页面允许用额外 query 保存非敏感选择；禁止原型凭据/正文进 URL。

每个模块 `export const ...Pages = { page: { title, project: true|false, render(ctx), bind(ctx) } }`；render 返回 main 内 HTML；bind 在 render 后执行，使用 document.querySelector('#main') 范围事件。ctx 为 `{ page, scenario, long, theme, accent, navigate(page, extra), render(), setScenario(value) }`。当前真实 URL 用 ctx.navigate；浏览器前进后退会重建。`data-action=retry` 全局恢复 normal。状态都要真实呈现；不可执行的正式后端动作在页面内反馈“样板演示”，不得宣真实成功。

core.js 提供 esc/icon/status/banner/action/stateGate/readonlyNotice/showDialog/closeDialog/dialogHeader/dialogFooter/inlineResult。showDialog 使用原生 dialog 管理模态及焦点；callback 接 dialog，关闭 `[data-dialog-close]`；options.beforeClose 可拦截草稿丢弃；按需 options.wide。共用类：button/primary/quiet/danger/icon-button；page-heading/eyebrow/muted/small；notice；field/field-row/section-heading；collection-row；workspace/rail/workspace-main；empty-state；status。CSS 语义 token 来自 shared.css；页面 CSS 全部限定页面前缀，避免重定义共享规则。

写者：designer 管 index.html/app.js/core.js/shared.css/base-pages.js 及正式文档；content designer 管 content-pages.js/content.css；collaboration designer 管 collaboration-pages.js/collaboration.css。测试者仅 test/check/playwright 命名的 harness，独审材料另由 root 指定 owner。共享变更向 designer 提出。Git 操作仅 root。
