# F0 隔离样板模块契约

纯浏览器 ES modules；Python 3 静态服务，不安装产品依赖、不调用 API。合成数据只在当前 tab 内变化。入口 `index.html?page=projects&theme=light&scenario=normal&accent=indigo`。`page` 支持 dashboard/projects/project/tasks/providers/knowledge/meetings/inbox/mcp/oauth/memory/agents；`scenario` 支持 normal/long/empty/loading/error/readonly/unknown，long 等同 normal+ctx.long。页面允许用额外 query 保存非敏感选择；禁止原型凭据/正文进 URL。

每个模块 `export const ...Pages = { page: { title, project: true|false, render(ctx), bind(ctx) } }`；render 返回 main 内 HTML；bind 在 render 后执行，使用 document.querySelector('#main') 范围事件。ctx 为 `{ page, scenario, long, theme, accent, params, navigate(page, extra), render(), setScenario(value) }`。当前真实 URL 用 ctx.navigate；浏览器前进后退会重建。`data-action=retry` 全局恢复 normal。状态都要真实呈现；不可执行的正式后端动作在页面内反馈“样板演示”，不得宣真实成功。

core.js 提供 esc/icon/status/banner/action/stateGate/readonlyNotice/showDialog/closeDialog/dialogHeader/dialogFooter/inlineResult。showDialog 使用原生 dialog 管理模态及焦点；callback 接 dialog，关闭 `[data-dialog-close]`；options.beforeClose 可拦截草稿丢弃；按需 options.wide。共用类：button/primary/quiet/danger/icon-button；page-heading/eyebrow/muted/small；notice；field/field-row/section-heading；collection-row；workspace/rail/workspace-main；empty-state；status。CSS 语义 token 来自 shared.css；页面 CSS 全部限定页面前缀，避免重定义共享规则。

原11类模板的历史分工：designer 管 index.html/app.js/core.js/shared.css/base-pages.js 及正式文档；content designer 管 content-pages.js/content.css；collaboration designer 管 collaboration-pages.js/collaboration.css。测试者仅 test/check/playwright 命名的 harness，独审材料另由 root 指定 owner。共享变更向 designer 提出。Git 操作仅 root。

## 当前 Agent 模块与保存契约

`page=agents` 默认仅列表，`agent=ID` 为独立详情；默认基本配置，`view=resources / memory / audit` 为同对象的其余三个 Tab，未知 ID 不替换成首项。`agent=new` 只用基本配置与资源汇集一次创建。旧 `page=memory` 兼容至 Architecture 的记忆 Tab。完整运行、目录与故障演示参数见[样板说明](README.md#启动与参数)。

`agent-pages.js` / `agent.css` 负责列表、创建、基本配置、资源与四 Tab 接入，`agent-catalog.js` 提供合成候选；`agent-memory-audit.js` / `agent-memory-audit.css` 负责独立记忆和可复用授权域。基本配置独立手动保存，资源按完整操作逐项提交，记忆删除与审计撤销各自逐条提交；同对象切 Tab 保留基本草稿，其他域的成功不被基本配置取消或保存回滚。各域保留自身原请求、版本与 Unknown 查询/回读门禁，迟到结果不能跨对象或 scope 发布。审计只查看与撤销可复用授权，不是设置变更日志，不手工新建或改 Scope。

本轮唯一写者：`/root/agent_completion_designer` 负责基础/资源与集成，`/root/f0_structure_audit` 负责 Memory/审计两模块文件；`/root/agent_completion_test` 负责指定测试文件，`/root/f0_refinement_review` 独立审查并维护自己的材料；`/root/f0_docs` 维护获授权正式文档与本契约。共享文件变更仍由 root 协调唯一写者，全部 Git 操作由 root 执行。此前分工不作为本轮写入授权。
