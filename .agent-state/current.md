# Work Owner 任务规划界面检查点

- 活动分支：`ai/work-owner-planning-ui`；当前隔离工作树 `/workspace/agenteam-work-ui`，正式基线 `f1c94ee520e8153e935fea7e7ed269e7e8b9adca`。所有 Git 写入、分支与工作树操作归 root。
- 目标：已有初始化 Project 当前 Human Owner 的显式 `/tasks/explore` 规划界面，消费已正式交付的 21 项 Work HTTP 能力。普通 Task Kanban、状态流转、指派、Project 创建和生产 SPA 发布不在本卡范围。
- 正式规格：`docs/development/work-items/d11-work-owner-planning-ui.md`。rev1 已经独立 SPEC 审查接受并从原树冻结版本单向同步；后续规格只维护本树。产品已分两域实施；首批 API/transport、路由守卫与两个编辑组件可构建，限定作者纯控已过，尚无真实 UI 动态验收。
- 唯一负责人：`/root/service_delivery`，维护本分支恢复记录、卡片及页面实施；`blocker_implementation` 已交回全部 UI 写权，并转入独立 Project Variables 树写规格；当前 UI 实施、fixture 与作者浏览器矩阵由负责人接管；`blocker_spec_review` 已完成四fixture有限静审并实际转 Runner SPEC 作者；后续独立审查/两动态场景由 root 在稳定边界轮转未参与者，不等待固定实例返场。
- 共享产品仅 `web/src/api/client.ts`、`web/src/composables/useSession.ts`、`web/src/router/auth.ts`、`web/src/router/index.ts`、`web/src/App.vue`、`web/src/components/layout/ProjectNav.vue`。新专属源码/测试及 README 精确范围见卡；已授权测试共享接缝仅 `tests/account/project_owner_web_fixture_test.go` 的可选任务观察/config/IPC，默认行为及预算/资源退出保持。无新迁移/依赖/通用监督器。
- 实施只写本隔离树；原树同名产品与 Model 固定输入不得改写。SPEC 接受后按互不重叠文件域并行，所有文件同时仅一个 writer；检查点先暂停精确保存范围。
- 全局状态与资源调度以 `ai/product-continuation` 分支的 `.agent-state/current.md` 和 `docs/development/agent-team/tasks.md` 为准，目前原树在 `/workspace/agenteam`。这里不复制 Model 验收流水；Model 资源窗仍统一由 root 分配，不因隔离工作树而并发真实 PG/browser/socket 或操作其 dist。
- 前置依赖：本树两套 `package.json`/`package-lock.json` 已与现有安装来源核实相同，`web/node_modules` 与 `tests/account-captcha-web/node_modules` 仅离线复制成独占目录，排除 `.vite/.vite-temp/.cache`。这是环境准备，不是产品测试；以后依锁重建，不提交 node_modules/output。
- 输出位置：`output/ai/work-owner-planning-ui/`；需要跨设备的测试源/harness 放正式测试路径。真实验收沿原 fixture 的 no-tag `app.Run`、45s case/120s top/6m 包与原资源链，实际 Wait、资源和 I/O 尾分别收齐。未取得真实结果不得写通过。
- 已保存并经独立窄验的客户端/状态片段：API 原三类缺陷修后独立 21/21 控制通过；Session 历史回执保留独立 15/15，通过后的正式 Task/Blocker 专码分类漏项又由红例揭示，限定修后独立 16/16 通过；controller 跨 Project 清理与同 ID 规范改名保护先被独审发现，修后独立 14/14 通过。均为离线控制，不冒真实服务或浏览器验收。原产品失败及独验自身刺激问题分别保留。
- 当前页面组合片段已可构建：四个新组合视图、App/正式 router/ProjectNav 接入，controller 和两状态/交互测试更新。作者原状态/路由/Owner/auth 组合 119 项通过；最新 controller/state 与包含真实 App/正式路由的 45 项通过，严格 TS 实际通过。首次 App 模板编译失败因内联多赋值经格式化后缺少表达式分隔，已改局部 handler 后窄重验；初始 TS 路径/typed metadata/toReversed 等错误保留。尚无全 UI 独审、production build 或真实浏览器通过结论。
- Work 真实 fixture 首段已交接：共享 Owner 可选接缝、新 Work fixture、新六作者 Go top 和八闭集 case config，共四源，integration race 编译实际 exit0；config 语法及限定 diff 检查通过。没有运行 TestMain/PG/browser。复用原 no-tag root/Account/Project/proxy/实际清理链，Work 种子经正式 HTTP；观察、成功后截断、原2秒回包 hold 与闭集 IPC 已有可构建代码，默认分支与新注入已有限独立静审：旧Owner路径不变、正式根/完整body后截断/持久同key门槛及原退出链保留；唯一mustfix是Structure/Task Lookup TargetID被记录为lookup，交负责人最小修复。真实验证仍未执行。
- fixture 尚缺六作者 Playwright 源、同体 schema/client 判据与完整业务矩阵；Lookup target 观察、撤销/过期、生命周期负向、51 Blocker/循环及 Unknown 相关前置须逐项闭合，不能用当前可编译 top 或 browser-result 布尔宣称完成。独立两场景源待消费实际 fixture；所有真实窗口仍由 root 分配。
- Current Sprint边界已按正式Structure契约与独审事实纠正：没有正式非null指针生产者，本轮真实无Current/planned typed选择；自动current/用户选择规则保留纯投影控制，非null真实正例待D11生命周期接通，不用SQL伪造、不称已验证。
- 下一步：负责人继续 UI/fixture/六作者浏览器源，root轮转未参与者核完整组合并编写两个独立场景；正式真实验收前先闭合相应输入与原资源监督链，不伪造接口或成功布尔。既有停止项不变。最终正式交付排除本恢复文件，不将 WIP 当成 main 结果。
