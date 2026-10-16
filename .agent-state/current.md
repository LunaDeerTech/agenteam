# 当前状态：接续新 Vue 工程，框架与空路由实现中

2026-10-16，用户在前次明确暂停、保存并推送后要求接续。当前任务仍是未验收的 Vue 重建，不把前次 WIP、旧 HTML 或旧 Vue 的检查视为新工程通过。

## 目标与边界

- 旧 `web/` 已完整归档到 `web-crape/`，仅作历史参照。新 `web/` 从零实现，不复用旧 API 客户端、Session、权限或业务代码；后端接口按具体页面需求另行设计，本轮未做。
- 先共享 UI、AppShell、**唯一 SettingsShell** 与全部已定路由骨架，系统设置优先。系统/个人/项目设置为 11/3/11 个叶子；未讨论正文只保留标题与空区域，不搬完整 Agent 或项目列表正文，不填虚构数据。
- `746ddcb3` 的可运行 HTML 原型是本轮明确设计参考。须由实际运行并看过整页的 owner 报告观察；原型 PASS 不代证新工程。设计入口见[重设计说明](../docs/frontend-design/redesign/README.md)。

## 分支、会话与文件所有权

- 工作树：`/workspace/agenteam-vue-rebuild`；分支：`ai/frontend-vue-rebuild-20261016`；本次恢复输入为 `635c90dc`，原型参考为 `746ddcb3`。恢复以该远端任务分支最新提交和实际差异为准；本状态不回填自身提交号。
- `/root` 是当前唯一协调写会话，负责全部 Git、分支、worktree、checkpoint/push 与集成。前次暂停已明确交接，本次 owners 如下。
- 骨架、共享组件与 harness 紧耦合，本轮共用工作树并按文件保持唯一写者；跨 owner 接口变更先沟通。Git 保存持续获用户授权；本轮未经最终范围验收，不合 main、不部署。

| Owner | 唯一写入范围与职责 | 当前状态 |
| --- | --- | --- |
| `/root/vue_integration` | `web/` 工程配置、锁、入口、路由、页面与布局；安装依赖、工程检查和壳集成 | 已安装新工程依赖；完整工程检查与作者浏览器结果待报告 |
| `/root/vue_shared_ui` | `web/src/components/ui/`、`web/src/styles/`、`web/src/composables/useTheme.ts` | 已补齐八组件导出、Input/Tabs/Select CSS、五个壳图标、Dialog 类型及菜单首开宽度；限定 Prettier 与 type-check exit 0，浏览器待验 |
| `/root/vue_tests` | `web/tests/` 与 `tests/frontend-rebuild/`（含该目录 README） | 原有 theme/menu/dialog 三文件共 10 项（4/3/3）通过；浏览器 harness 继续实现 |
| `/root/vue_visual_review` | 独立只读审查；可再生材料放忽略的 `output/ai/` | 分别实际查看参考原型与新 Vue，范围和结论待报告 |
| `/root/vue_boundary_audit` | 独立只读核对 fresh 依赖/源码隔离、归档一致性与路由覆盖；无正式文件写权 | 审查结果待报告 |
| `/root/vue_docs` | 本状态、根 AGENTS/README、前端开发文档、新 `web/README.md` 与必要设计范围更正 | 唯一正式文档与状态作者；依据源码及 owners 实际回报更新，不代跑产品检查 |

## 已有内容与首个断点

前次已保存独立 Vue 工程及锁、集中路由 metadata、AppShell/AccountShell/单一 SettingsShell、标题加空正文，以及共享控件和初步组件测试。旧 fixture 已显式改指 `web-crape/`；归档源码保持原版本，默认构建入口仍是新 `web/`。

恢复输入尚未安装依赖，Input/Tabs/Select 未导出且缺专用样式，浏览器 `foundation.spec.cjs` 尚缺。本轮已安装新依赖并补组件接线，当前首断点转为完整工程检查、浏览器 harness 和新 Vue 独审；已有源码不等于这些门槛已通过。

## 本轮检查与资源

- 测试 owner 报告 Node `v24.19.0`，`npm run test:unit --prefix web` 实际 exit 0，三个既有测试文件 10/10 通过（theme 4、menu 3、dialog 3）；5242 当时无监听。Chromium 为 `151.0.7922.173`，集成 owner 已恢复 `tests/account-captcha-web/node_modules` 的 Playwright `1.56.1`。浏览器尚未据此通过。
- UI owner 已停止本批写入；限定组件/样式 Prettier 与 `npm run type-check --prefix web` 实际 exit 0。全工程 format/build/check、完整浏览器与新 Vue 独审待对应 owner 报告；不扩大局部通过范围。
- 边界审查初步核实 `746ddcb3:web` 与恢复输入 `635c90dc:web-crape` 的 569 个文件 blob/执行位相同，工作树归档未改；新 lock 为独立 `@agenteam/web-rebuild`，179 个 package，未发现旧业务导入。精确路由与 fixture 审查仍进行中，未给整体接受结论。
- 原型服务 5241 归 `/root/vue_integration`；测试服务 5242 归 `/root/vue_tests`；独审服务 5243 归 `/root/vue_visual_review`；作者 Vue 服务 5244 归 `/root/vue_integration`。以上是资源分配，启动、占用与实际退出分别由 owner 核实，不能视为已经运行或清理。
- 新工程工具链要求 Node `>=24.12.0`；依赖按新 `web/package-lock.json` 安装，不能链接归档工程的依赖。安装与可复制运行入口将归位到 [web/README](../web/README.md)；浏览器命令以[新工程 harness](../tests/frontend-rebuild/README.md)为准。
- 原始日志、截图、dist 和缓存保留于忽略目录；源码、锁和必要 harness 随 checkpoint 保存。向用户展示截图须先使用项目约定图床上传并展示返回链接；不在仓库文档写入凭据，不将本机路径当作用户已看到图片。

## 下一步与保存边界

1. 集成 owner 安装新依赖并核对实际 Node/npm，UI owner 完成组件合同和样式；先打通 `/system/users` 及全部已定空路由。
2. 运行本轮必要工程/组件检查，测试 owner 完成独立端口浏览器 harness；独审分别看原型与新 Vue 的整页、交互、主题、窄屏、动效和缩放。
3. 文档 owner 按最终源码及检查结果补齐入口、组件合同、未验项与资源退出事实；只更正现行 scope，保留历史验收事实。
4. 有意义结果或约每 5 分钟安全边界，各 owner 报告并停止本次保存路径写入；root checkpoint/push 后再续写。WIP 保存不等于正式完成、用户设计接受或可发布。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
