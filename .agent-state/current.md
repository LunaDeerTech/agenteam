# Work Owner 任务规划界面检查点

- 活动分支：`ai/work-owner-planning-ui`；当前隔离工作树 `/workspace/agenteam-work-ui`，正式基线 `f1c94ee520e8153e935fea7e7ed269e7e8b9adca`。所有 Git 写入、分支与工作树操作归 root。
- 目标：已有初始化 Project 当前 Human Owner 的显式 `/tasks/explore` 规划界面，消费已正式交付的 21 项 Work HTTP 能力。普通 Task Kanban、状态流转、指派、Project 创建和生产 SPA 发布不在本卡范围。
- 正式规格：`docs/development/work-items/d11-work-owner-planning-ui.md`。rev1 已经独立 SPEC 审查接受并从原树冻结版本单向同步；后续规格只维护本树。产品实施即将分两域启动，当前无构建或动态 PASS。
- 唯一负责人：`/root/service_delivery`，维护本分支恢复记录、卡片及页面实施；`blocker_implementation` 负责 API/client/Session 与相应两测试；`blocker_spec_review` 未参与产品实现，负责独立审查与本人动态验收。
- 共享产品仅 `web/src/api/client.ts`、`web/src/composables/useSession.ts`、`web/src/router/auth.ts`、`web/src/router/index.ts`、`web/src/App.vue`、`web/src/components/layout/ProjectNav.vue`。新专属源码/测试及 README 精确范围见卡；已授权测试共享接缝仅 `tests/account/project_owner_web_fixture_test.go` 的可选任务观察/config/IPC，默认行为及预算/资源退出保持。无新迁移/依赖/通用监督器。
- 实施只写本隔离树；原树同名产品与 Model 固定输入不得改写。SPEC 接受后按互不重叠文件域并行，所有文件同时仅一个 writer；检查点先暂停精确保存范围。
- 全局状态与资源调度以 `ai/product-continuation` 分支的 `.agent-state/current.md` 和 `docs/development/agent-team/tasks.md` 为准，目前原树在 `/workspace/agenteam`。这里不复制 Model 验收流水；Model 资源窗仍统一由 root 分配，不因隔离工作树而并发真实 PG/browser/socket 或操作其 dist。
- 前置依赖：本树两套 `package.json`/`package-lock.json` 已与现有安装来源核实相同，`web/node_modules` 与 `tests/account-captcha-web/node_modules` 仅离线复制成独占目录，排除 `.vite/.vite-temp/.cache`。这是环境准备，不是产品测试；以后依锁重建，不提交 node_modules/output。
- 输出位置：`output/ai/work-owner-planning-ui/`；需要跨设备的测试源/harness 放正式测试路径。真实验收沿原 fixture 的 no-tag `app.Run`、45s case/120s top/6m 包与原资源链，实际 Wait、资源和 I/O 尾分别收齐。未取得真实结果不得写通过。
- 下一步：API/Session 与 controller/views/导航两域并行实施，保存可构建闭合片段。既有停止项不变。最终正式交付排除本恢复文件，不将 WIP 当成 main 结果。
