# Project preparation gate

`project.Authority.RequirePreparingProjectInTx` 只在本 Store 原 Tx、已持有 Project SH 下返回当前 initialized/active ProjectRef。Execution 必须先验证自身原 preparing claim/fence；此返回值不是执行授权，不借启动 Human Session，也不反调 Execution/Agent。产品两源已独立有限源码审接受，尚未作为真实 preparation 链验收。

2026-10-10，Go 1.27.1、原私有 telemetry-off/offline 环境及协调热缓存，以下三个新 top 的 race 全部通过（Go871482 Wait0，8.746s）：

- `TestExecutionPreparationProjectCurrentFacts`
- `TestExecutionPreparationProjectRequiresOriginalTransactionAndLock`
- `TestExecutionPreparationProjectCancellationJoinsOriginalRead`

命令为 `go test -mod=readonly -p=2 -race -count=1 -timeout=90s -json -run '^(TestExecutionPreparationProjectCurrentFacts|TestExecutionPreparationProjectRequiresOriginalTransactionAndLock|TestExecutionPreparationProjectCancellationJoinsOriginalRead)$' ./internal/central/project`。原 `pure-01` 的 vet 阶段 fresh 为 5,367,742,464 B，未达到 5 GiB，零 vet、outer871474 Wait1；这次 wrapper FAIL 保留，不是产品测试失败。

释放下述已确认无人依赖的可重建私有依赖后，仅补 `go vet -mod=readonly -p=2 ./internal/central/project`，Go873879/outer873863 Wait0，4.003s。两轮原 group/runtime 双空、无 adopted 子进程，窗口已归还。原 launcher/log/result 均在 `output/ai/project-execution-preparation/`，其中 `pure-01/`、`vet-02/` 保存各自原终态，不复制日志。

经 root 授权及 cleanup/content 依赖确认，以下三个目录正常逐项删除并确认 absent，释放 410,910,720 B；锁文件、dist、候选、失败材料与活跃缓存保留：

- `/workspace/agenteam-work-ui/web/node_modules`
- `/workspace/agenteam-work-ui/tests/account-captcha-web/node_modules`
- `/workspace/agenteam-skills-owner-ui/web/node_modules`

有限清理结果在同输出目录 `dependency-retirement.json`。这些旧任务若恢复前端检查，须先按各自原锁恢复私有依赖；本轮没有再次执行旧 UI 或其它域测试。
