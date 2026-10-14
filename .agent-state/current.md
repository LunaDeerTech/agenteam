# M1 App 集成与自有环境

- 基线 `c75e1bb8`；分支 `ai/m1-app-integration-20261014`；工作树 `/workspace/agenteam-m1-app-integration`；owner `delivery_path`，root 负责全部 Git。
- 唯一写域：`internal/central/app/**`、必要 `internal/central/config/**` 与 `cmd/agenteam` 接线、M1 新 harness、[本卡](../docs/development/work-items/m1-app-integration.md)。不得修改其他领域或全局迁移；00091 归 core_readiness。
- 已实现显式执行 profile `tool-loop-v1` / `ExecutionRuntimeOptions.ToolLoop()`，保留 `direct-text-v1`。App 已增加新 Capture、MultiRound 选配、真实三 Runner Tool 来源/factory 与同路由 DeviceHandler 接线；等待 W2 可编译片及三个辅助文件完成后首编译，尚无 M1 PASS。
- 候选接缝：W1 原 JSONCaller/Runtime 与新 ToolCallingExecutionCapture；W2 ProjectCallDependencies.MultiRound 选配由原 Ownership 构造；W3 RunnerExecutor/RunnerResultAuthority。App 保持同 Store、Authority、Registry 与原 ProcessGuard，不复制状态机。
- 场景：自有目录读取 input.json，生成统计程序/结果文件并运行真实校验；同 Task/Execution 至少两轮工具交互，内容/stdout/exit 可核，Owner 沿已有入口进入 in_review。先自有确定性 Provider 真实 HTTP，再单独核外部 Provider 可用条件。
- 环境：Go `/workspace/toolchains/go1.27.1/bin/go`；共享 `/workspace/go/pkg/mod` 和 `/workspace/.cache/go-build`。固定 MinIO 已按原步骤构建于 `/workspace/shared/agenteam-m1-tools/minio/minio`，release 与 SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 匹配。PG17 `99a149…` 与 nginx `30f1c0…` 镜像已拉取。Playwright 1.56.1 安装完成于 `/workspace/shared/agenteam-m1-tools/node-browser/node_modules`；web 同锁安装位于 `node-web`，原 npm ci 已退出 0。
- 全树初期最多两路 Go 构建/测试，每路 `GOMAXPROCS=2` / `-p=2`；一个 native 栈由 W5 协调。未创建业务测试容器，未接已有 onboarding 数据库，未读取/输出真实 Provider 凭据或发送外部模型请求。
- 原 live-chat 分支 `e50e913a` 只读核查：保存单轮显式 live harness，四轮失败，最后 DNS 阶段未发送 HTTP；当前会话 owner 未能从陈旧 current 确定，不改原分支或恢复旧驱动。原 STOP/FAIL 保留。
- 上游实际采纳：Model `7cac84aa`（前片 `f47d3eb0`）、Tool `0c523752`（前片 `0bcfa47a`）、Task/UI/浏览器 `9a206769`（产品片 `e76873de`）；本树待 root 保存。Execution 尚待其完成快照/Loop 小修后采纳，不宣称此时可编译。
- 已有验证：本树 config `^TestExecutionRuntime` 实际 exit 0（首轮仅缺缓存的 setup FAIL 另保留）；上游 Model 7 个顶层/20 子测试独立 race 0，最终 Capture 隔离负例独验 0；UI 4 场景限定独验 0。不代替本组合验收。
- 新 `TestM1AppToolWorkflow` 主文件已落：真实默认 App 启动、正式 Registry/Runner/Provider/Agent/Task 种子；真实 mount 取得后才生成五响应并正式 PUT Provider，最后释放原空目录返回让 Scheduler 自行启动，无手调 Claim/Advance。正常 outbound WireSequence 扩展 2..16，保留原两响应规则；不接旧故障驱动。
- root 授权三辅助唯一写者：runner_tools 写本树 `m1_tool_runner_test.go`；model_runtime 写 `m1_tool_facts_test.go`；product_recent 写 `m1_tool_browser_test.go`。主 harness/App 接线仍 delivery_path 单写。三文件活动期由 root checkpoint 排除，后续各自冻结保存。
- 第二 checkpoint 为 WIP：本 owner 的 App、主 harness、fixture、card/current 已停写交 root；未整体编译/未起 native 栈。独立验证由 macro_milestones 负责，旧 STOP/FAIL 不回填。

## 首次完整组合运行（2026-10-14）

- 可恢复组合 `d239836e`；收尾原 App Join 修正 `b04b8ac1`。无标签 App 编译和完整 integration test binary 编译实际 exit 0。
- 首官方 owned native 栈 `run.hCX8bF` 实际 outer Wait exit 1：新库迁移（含 00091）、App 启动、实际 Runner enroll/连接已到达；在正式 Agent request 本地校验 `m1-agent-intent INVALID_ARGUMENT` 停止，未发 Model 请求、未启动浏览器。
- 原因是测试 seed 的 Name 空格及 RunnerMounts 同时附非 nil AllowedMountIDs；已改合法名称、原二选一字段，并对 ToolIDs 排序，产品合同不改。
- `output/ai/m1-app-integration/run.hCX8bF/native.log` 与 `owned-resources.json` 保存本轮证据；原 Runner cleanup Wait、PG/outbound/MinIO 精确 nonce 资源清理全部完成。未重跑旧 STOP 驱动。下一轮使用新隔离 DB，不复用失败轮资源。
