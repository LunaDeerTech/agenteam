# M1 App 集成与自有环境

- 基线 `c75e1bb8`；分支 `ai/m1-app-integration-20261014`；工作树 `/workspace/agenteam-m1-app-integration`；owner `delivery_path`，root 负责全部 Git。
- 唯一写域：`internal/central/app/**`、必要 `internal/central/config/**` 与 `cmd/agenteam` 接线、M1 新 harness、[本卡](../docs/development/work-items/m1-app-integration.md)。不得修改其他领域或全局迁移；00091 归 core_readiness。
- 已实现显式执行 profile `tool-loop-v1` / `ExecutionRuntimeOptions.ToolLoop()`，保留 `direct-text-v1`。目前 App 对新 profile 明确拒绝 DependencyUnbound，等待真实多轮/Runner 接线；尚未跑产品测试，无 M1 PASS。
- 候选接缝：W1 原 JSONCaller/Runtime 与新 ToolCallingExecutionCapture；W2 ProjectCallDependencies.MultiRound 选配由原 Ownership 构造；W3 RunnerExecutor/RunnerResultAuthority。App 保持同 Store、Authority、Registry 与原 ProcessGuard，不复制状态机。
- 场景：自有目录读取 input.json，生成统计程序/结果文件并运行真实校验；同 Task/Execution 至少两轮工具交互，内容/stdout/exit 可核，Owner 沿已有入口进入 in_review。先自有确定性 Provider 真实 HTTP，再单独核外部 Provider 可用条件。
- 环境：Go `/workspace/toolchains/go1.27.1/bin/go`；共享 `/workspace/go/pkg/mod` 和 `/workspace/.cache/go-build`。固定 MinIO 已按原步骤构建于 `/workspace/shared/agenteam-m1-tools/minio/minio`，release 与 SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 匹配。PG17 `99a149…` 与 nginx `30f1c0…` 镜像已拉取。Playwright 1.56.1 安装完成于 `/workspace/shared/agenteam-m1-tools/node-browser/node_modules`；web 同锁安装位于 `node-web`，写本记录时尚在运行。
- 全树初期最多两路 Go 构建/测试，每路 `GOMAXPROCS=2` / `-p=2`；一个 native 栈由 W5 协调。未创建业务测试容器，未接已有 onboarding 数据库，未读取/输出真实 Provider 凭据或发送外部模型请求。
- 原 live-chat 分支 `e50e913a` 只读核查：保存单轮显式 live harness，四轮失败，最后 DNS 阶段未发送 HTTP；当前会话 owner 未能从陈旧 current 确定，不改原分支或恢复旧驱动。原 STOP/FAIL 保留。
- 首次 checkpoint 为 WIP：源码/卡/current 停写交 root 保存；环境安装输出仍可写忽略目录。独立验证由 macro_milestones 负责，当前未开始产品验收。
