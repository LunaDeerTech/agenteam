# 当前状态：接续新 Vue 工程，框架与空路由验证中

2026-10-16，用户要求从已暂停并保存的 Vue 重建继续。当前任务仍未验收；WIP、旧 HTML 和归档 Vue 的检查不证明新工程通过。

## 目标与边界

- 旧 `web/` 已完整归档到 `web-crape/`，仅作历史参照。新 `web/` 从零实现，不复用旧 API 客户端、Session、权限或业务代码；后端接口按具体页面需求另行设计，本轮不做。
- 先共享 UI、AppShell、**唯一 SettingsShell** 与全部已定路由骨架，系统设置优先。系统/个人/项目设置为 11/3/11 个叶子；未讨论正文只保留标题与空区域，不搬完整 Agent 或项目列表正文，不填虚构数据。
- `ai/frontend-f0-refinement-20261015@746ddcb3` 的可运行 HTML 原型是本轮设计参考，目录为 `docs/frontend-design/redesign/prototype/`。实现者与独审须实际运行并观察整页；原型检查不代证新工程。设计入口见[重设计说明](../docs/frontend-design/redesign/README.md)。

## 分支、会话与文件所有权

- 当前工作树：`/workspace/agenteam`；任务分支：`ai/frontend-vue-rebuild-20261016`。用户所指暂停点为 `635c90dc`；root 已保留远端后续 `d05bc8ab`、`ac23d6a9`，本轮从 `ac23d6a9` 接续，不回退覆盖。恢复以任务分支最新提交和实际差异为准，本状态不回填自身提交号。
- `/root` 是当前唯一协调写会话，负责全部 Git、分支、worktree、checkpoint/push 与集成。已核对 `main@b6db5297` 治理与任务分支相同，本轮不合 main。
- 骨架、共享组件与 harness 紧耦合，本轮共用工作树并按文件保持唯一写者；跨 owner 接口变更先沟通。Git 保存持续获用户授权；WIP 不等于正式完成或用户设计接受。

| Owner | 唯一写入范围与职责 |
| --- | --- |
| `/root/vue_integration` | `web/` 工程配置、锁、入口、路由、页面与布局；依赖恢复、工程检查、壳集成、作者浏览器与预览 |
| `/root/vue_shared_ui` | `web/src/components/ui/`、`web/src/styles/`、`web/src/composables/useTheme.ts`；组件合同及受影响检查 |
| `/root/vue_tests` | `web/tests/` 与 `tests/frontend-rebuild/`（含该目录 README）；组件测试与浏览器 harness |
| `/root/vue_visual_review` | 独立只读浏览器审查；可再生材料放忽略的 `output/ai/` |
| `/root/vue_docs` | 本状态、根 AGENTS/README、前端开发文档、`web/README.md` 和授权设计入口状态；只读核对新旧隔离，不代跑产品检查 |

## 当前结果与第一个断点

已保存独立 Vue 工程及锁、集中路由 metadata、AppShell/AccountShell/单一 SettingsShell、标题加空正文、共享控件和初步组件测试。本轮恢复依赖后继续壳集成；八控件已存在于恢复输入。首个浏览器 harness `tests/frontend-rebuild/foundation.spec.cjs` 已落盘、实际通过 1/1 首链路并冻结；完整矩阵与独审仍进行中。

当前断点为本轮完整工程检查、浏览器首链路及新 Vue 独审；具体控件和路由覆盖须由本轮实际检查确认。前端开发文档仍有归档合同，文档 owner 正按新源码归位。

## 本轮已报告检查与资源

- 集成 owner 报告：Node `24.19.0`、npm `11.9.0`；`npm ci --prefix web` 与 `npm ci --prefix tests/account-captcha-web` 均 exit 0；Playwright `1.56.1`、Chromium `151.0.7922.173` 可用。新 `web` 的 type-check 与 build 通过；首轮 format-check 仅发现三个既有 unit 文件格式失败，测试 owner 正修复；完整 `check`、组件测试、作者浏览器和独审结果待各 owner 本轮报告。
- 文档 owner 只读核对：`746ddcb3:web` 与当前 `HEAD:web-crape` 的 569 个文件 blob/执行位完全相同，工作树归档无受跟踪改动。新 lock 为独立 `@agenteam/web-rebuild`、179 个 package；当前新 `web/src` 未检出旧 client、Session、权限/API 或归档导入。精确路由与默认构建入口核对仍在进行；此为有限静态边界结论，不是业务或浏览器验收。
- 集成 owner 已实际启动原型 5241 与新 Vue dev 5244，并报告各自 HTTP 200；npm 父进程 PID 分别为 7451/7452，日志在忽略的 `output/ai/vue-integration/`。frp 公网 18081 已报告控制连接/代理启动成功，public/local index hash 一致；公网浏览器/HMR 仍在检查，尚未完成用户预览交付。
- 测试 owner 实际运行生产 dist 首链路 1/1 exit 0：`/system/users` → Providers → 返回 → 键盘主题切换 → 刷新持久化 → 跟随系统变色，无 pageerror；对应命令见 [harness](../tests/frontend-rebuild/README.md)。5242 preview 已 SIGTERM，报告 `closed=true / exitCode=143` 且无监听；完整矩阵与组件扩展待做。
- 独审 owner 已实际查看 `746ddcb3` 原型的 1440×960 浅色 general、Agent 列表与 Reviewer 基本配置整页；新 Vue 尚未审，无独审通过结论。独审服务 5243 尚未启动，当前使用原型 5241。环境快照不保留进程，恢复时必须重新核实。
- 工具链要求 Node `>=24.12.0`；依赖按新 `web/package-lock.json` 安装，不能链接归档依赖。浏览器命令以[新工程 harness](../tests/frontend-rebuild/README.md)为准；普通工程运行入口由文档 owner 补齐。
- 日志、截图、dist 和缓存放忽略目录；源码、锁和必要 harness 随 checkpoint 保存。用户图片展示先按项目约定上传图床并使用其公网链接；凭据不入文档。用户已授权通过 frp 保留 dev 供体验，只有公网响应及页面功能验证后才提供地址。

## 下一步与保存边界

1. 集成 owner 完成共享壳接线与工程检查；UI owner 按实际浏览器反馈返修控件和样式。
2. 测试 owner 运行独立端口浏览器 harness；独审分别观察 `746ddcb3` 原型与新 Vue 的整页、交互、主题、窄屏、动效和缩放。
3. 文档 owner 按源码与实际检查归位运行入口、组件合同、范围和未验项；保留历史验收，不宣称用户已接受设计。
4. 有意义结果或持续修改约 5 分钟的安全边界，各 owner 报告并停写本批路径，由 root checkpoint/push 后续写；本轮正式范围验收前不合 main。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
