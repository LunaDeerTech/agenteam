# Task 历史事件筛选与时间顺序

本片基于既有[任务详情历史](d11-work-owner-planning-ui.md#14-任务详情只读历史)与正式 [Work Timeline HTTP](../../../api/openapi/work-timeline.json)，仅补当前 Owner 的只读历史控件，不修改任务、评论或任何命令恢复状态。

## 行为与边界

- 事件类型可选“全部”或七种正式事件之一；全部省略 `types`，单选发送原类型，不以本地过滤代替服务端读取。
- 时间顺序可选由新到旧（默认 `desc`）或由旧到新（`asc`），服务端查询与列表顺序一致。默认仍取最近 50 条；此前将最近页反转为正序的展示在本片改为明确的新到旧。
- 类型或顺序改变即清除原列表、错误和 cursor，从第一页读取；相同条件不重复请求。后续页保留原条件与原 cursor，严格检查跨页重复、顺序、范围和进展，整页成功后才追加；失败保留此前完整页并重试原页。筛选空态提供清除筛选。
- 原 Session 串行 owner、当前 Project/Task 身份及页面代际不变。忙时禁用条件变更，旧请求实际退出前不启动新请求。离开 Task/Project、真实身份退出清空历史和筛选；同 Task 显式刷新保留条件。历史读取不确认、重发或清除 Unknown 写意图。
- 控件复用 UiField/UiSelect/UiButton，适配窄宽、主题、键盘与请求后焦点。仅保留当前页面内存状态，不写 URL、存储或私有命令。

## 写域与验证

产品仅修改 `web/src/composables/useProjectTasks.ts`、`web/src/components/work/TaskTimeline.vue`、`TaskReviewDetail.vue`；专属状态/组件测试扩展相邻 `work-task-timeline-*.spec.ts`。浏览器复用 `tests/account-captcha-web/e2e/task-timeline.spec.ts`、`task-timeline.helpers.ts` 与 `tests/projectvariable/task_timeline_web_test.go`。共享 API client、Session、router、Ui 控件、后端产品、原 profile/预算/退出门不变。

冻结候选 `206996e3` 已获 `recovery_lead` 的独立静态、方法、真实业务/资源及有限图审接受，随本次变更交付；作者为 `variables_review`，不以作者自测代独立验收。

作者离线 `unit02` 为 5 文件 27 项通过，包含 Timeline state/component/client 与既有 Work state/page；类型 `type01`、正式构建 `build01` 退出 0。真实 Session/API 受控检查覆盖 types/order/cursor、双向连续分页、空态/重试、忙时拒绝变更、跨 Task/Project 迟到响应、撤权及 Unknown 原意图保持。原 `unit01` 11 PASS / 1 FAIL 保留：新增 Transfer Lookup 夹具误写到 Task 内容命令路径，修正为正式 `task-transition-commands/lookup` 后通过；不修改产品端点，也不将两轮计数累加。原件与精确命令在 `output/ai/task-history-filters/checks/`。

## 真实只读链与边界

`TestTaskTimelineWeb/recent-older-and-refresh` 的 `native01` 全轮通过：复用正式 Work 服务产生的原 51 条事件，按用户动作逐次读取最近 desc 50 → 较早 1 → 刷新 50 → 评论单型 1 → 添加阻塞空态 → 清除筛选 50 → asc 50 → 较新 1。八次查询均校验原 types/order/cursor、服务端完整原响应与独立 SQL 持久事实，浏览器消费、实际响应及服务记录联合闭合。原只读 DB 后验、Task/command/history/Outbox 不变及零 Dispatch/Execution 保持；浏览器零写入、零 Lookup。

原 25 个请求为 22 finished 与 3 个 GET 200/ERR_ABORTED（Project、agents、空筛选 events），保留各自真实终态；10 份联合证明覆盖本链核心请求，不称全部 API 均 finished 或全部均有三方证明。Playwright 一项 12.6s，Go 顶层及唯一子例 31.07s。作者亲见原工具 35491 exit0，supervisor 141878 actual0/135.596s；原 Go/Node/driver 与 4 个 Chromium Wait 全 0，七资源 14 次 absence、private/runtime/descendants/TCP 双尾齐，2482 个运行输入不变。独立审阅者核原业务与退出记录，outer 工具终态来源明确为作者亲见。

1440 浅色图实际显示新到旧的评论/负责人/状态，390 深色图显示旧到新的任务创建与两个筛选控件；实际键盘选项、回焦、减少动效与横向溢出检查通过。两图均已独立查看，仅接受实际可见范围，不外推全部 51 行、七类型浏览器/视觉矩阵或全面无障碍。

## 原件与重建入口

执行树 `/workspace/agenteam-task-history-filters`；基础检查见上文目录，Go 与真实链原件为 `output/ai/task-timeline-web/{compile01,native01,dist-filters01}`，图和原业务 JSON 在 `native01/evidence/`，原监督日志 `/tmp/thf01/ui-5681701cb0c84b0f.log`。精确安全环境、命令及绑定见 `compile01/invocation.json`、`native01/invocation.json`；结果以各自 `result.json` / `terminal.json` 和原日志为准，启动 recipe 中的待运行状态不回填。

Go race 编译实际退出 0（75.134s），selector list 只枚举原 top，不计行为通过。编译依据是冻结 `206996e3` 与期间无源码改动声明，**没有编译前后源码 manifest**；编译后的 runtime closure 不冒作编译前后记录。正式 `build01` 的 74 个资产物理复制到 `dist-filters01`，`dist-copy01.json` 记录字节一致；运行绑定 2482 输入含同包 67 个 Go 文件，原 supervisor 首尾门证明运行期间输入不变。

重建使用现有 [supervisor](../../../.agent-state/task-planning-recovery/pg_only_supervisor.py) 和 [Task profile driver](../../../.agent-state/work-owner-http/root_chain_driver.py)，Go 1.27.1 离线 `go test -c -race -tags=integration` 编译 `./tests/projectvariable`，运行 selector `^TestTaskTimelineWeb$`。设置 `AGENTEAM_TASK_TIMELINE_WEB_DIST` 为本候选独占正式构建目录、`AGENTEAM_TASK_TIMELINE_WEB_EVIDENCE` 为新 evidence 目录，以 `--root-chain`、独占短 runtime 路径和原预算/资源门运行；不覆盖旧结果或复用在途资源。本轮只使用既有测试服务组合，不宣称生产 initializer/root fullApp 已接通。

旧 §14 和本轮原失败均保留。本片不提供评论写入/编辑、Execution/Model 日志、实时订阅或完整 Task/D11/D27；正常只读浏览器链不代 Unknown/撤权全矩阵。生产 Project initializer、Central SPA 发布、ready=false/503 及既有 STOP 不变。
