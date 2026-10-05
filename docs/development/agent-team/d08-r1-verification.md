# D08 B03-R1 独立验收

日期：2026-10-05。结论：**R1 不可变参与者注册与原版本恢复解析通过，可采纳两文件候选，无未决阻断**。验收者未参与实现。主线程已将相同两源提交推送为 `98262b49aa87c52cfb0c09586dd1b76f9aa1fd52`；后续 R2 写入不属于本报告输入。

## 固定范围

规格为[恢复首卡](../work-items/recovery-d08-b03.md) rev1，SHA-256 `8f0297714cf6104784db55c8bc8c2adfe87449b6febe3b058bb761d7a64892e4`。冻结初检 HEAD 为 `08bc5c5874d29d34855323119a7f7e52240caad1`，非文档已跟踪输入与 `a9cf0de222ce846f40784472e3d3dbce43a0d3e5` 一致，另加下列冻结候选。运行期间主线程的纯文档提交未改变验证代码；源码边界以冻结指纹及运行前后末检为准，不将初检 HEAD 当作整个运行期间未变化的分支指针。

| 文件 | SHA-256 |
| --- | --- |
| `internal/central/project/participants.go` | `b38ebb0b2cfa0ae42e969bd993932808fdb20f7a5f4297375f27d5f6a74455ed` |
| `internal/central/project/participants_test.go` | `f3386b187e4c751976ba8d147eb43d5b0c717bf19394e53a822aaf7ee0968b57` |

[冻结记录](evidence/d08-r1/freeze-input.json)含源码、module 锁文件、脚本及独立探针指纹。作者 `compile-input.json` SHA 为 `445b7fc069dd0208b08088beb42cde23931a79f294b445608abbeb2a7cf98dc2`，其 162 项本地编译输入在运行前和运行结束后逐项匹配；不另复制大型依赖索引。该末检发生在 R2 开始写入之前，仅证明本次冻结输入，见[末检记录](evidence/d08-r1/final-checks.json)。

## 审查与实际运行

静审确认 RequiredManifest 仍是完整清单、图合法性与摘要的唯一实现。构造先拒绝 typed nil，再核 Name、精确 tuple 和集合；解析仅消费原清单，逐项比较 OwnerModule、ReferenceKinds、CleanupAfter，缺版本与不兼容均返回零计划。输入/返回切片复制，计划保留原 digest，清理顺序从原图每轮选最小就绪名称。注册/解析没有参与者业务调用、SQL、外部 I/O 或运行时可变注册入口。

所有运行位于 `/workspace/agenteam`，使用 Go 1.27.1、`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off`、恢复后的 `GOMODCACHE=/workspace/agenteam-dependency-cache/modcache`。独立执行设置 `GOFLAGS='-mod=readonly -p=2'`，按授权复用作者已停写的编译 cache；源码输入先核后用，测试临时目录归本次独占。

| 检查 | 实际命令与结果 |
| --- | --- |
| 作者证据复用 | `go test -count=1 ./internal/central/project/...`、`go test -race -count=1 ./internal/central/project/...`、`go vet ./internal/central/project/...` 均 exit 0。环境见[作者输入](evidence/d08-r1/author-input.json)；原 [unit](evidence/d08-r1/author-unit.log)、[race](evidence/d08-r1/author-race.log)、[vet](evidence/d08-r1/author-vet.log) 日志保留。这三项明确为作者执行。 |
| 独立工程检查 | 一次 `AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh`，133.445s，exit 0。完整执行脚本既有普通全树 test、vet、integration-tag vet、race 与 Central/Runner build；未运行 integration 测试。[完整命令](evidence/d08-r1/check-go-command.json)、[原日志](evidence/d08-r1/check-go.log)，日志 SHA `e0c5b20fa8ef8e91426cb398db9446214e8a27738d66653a1f5ac74ef1afbe8e`。 |
| 独立风险探针 | 作者冻结前按公开卡片 API 编写的外部消费测试，经 Go overlay 注入虚拟测试路径。`go test -race -count=1 -timeout=2m -v -overlay <owned-overlay> -run '^TestIndependentR1(FrozenManifestCompatibility\|PlanIsolation)$' ./internal/central/project`，exit 0，包运行 1.053s；两顶层及五子例通过。[完整命令](evidence/d08-r1/independent-probes-command.json)、[原日志](evidence/d08-r1/independent-probes.log)，日志 SHA `17055cf239559127d141e9581ed4f9119c28cbeef5a73b30289be3ed2fe91bec`。 |
| 格式与停止 | 两源 gofmt 与限定 diff 空白检查通过；新文件另作 `--no-index --check`。两个 owned 进程组均无剩余进程，临时工作目录为空。没有创建 Docker 资源。构建产物大小及 SHA 记录在末检文件，未启动产物服务。 |

独立探针实际观察原 v1/当前 v2 选择不同指定实例、旧计划不吸入 Skills、缺版本无回退、合法原图的三类元数据漂移拒绝、typed nil 拒绝，以及调用方和返回值修改不污染计划。八个并发读取者各进行 40 轮解析和读取，race 无报告；全部参与者业务调用计数为零。作者已有更完整单项非法、集合换序和拓扑场景证据复用，未重复扩张全量测试。

[探针原文](evidence/d08-r1/r1_independent_probe.go.txt) SHA 为 `46930cc87f3eb8f8638a03a50db06040eb242b2fc370d92b90e8873bed19604b`。在 `98262b49aa87c52cfb0c09586dd1b76f9aa1fd52` 对应冻结产品输入上可运行 `python3 docs/development/agent-team/evidence/d08-r1/replay-probes.py` 重建 overlay；[重放助手](evidence/d08-r1/replay-probes.py)仅做语法检查，正式通过证据来自上述实际原命令。探针以 `.go.txt` 保存，避免被普通 `go test ./...` 意外发现。

## 限制与交接

本次通过仅覆盖 R1 注册/恢复解析和工程可构建性。没有验证 B03-P 的持久生命周期、当前 cause/phase 授权、D05 真停止/清理、Secret/Object Audit fact checker、真实 PG/MinIO/ProcessGuard/Outbox 组合、HTTP/app 或 D10 初始化；没有把 typed 测试 adapter 当成生产绑定，也不宣称 D08 模块通过。

所有验收命令已结束，生产文件未被验收者修改。实际新增仅本报告和 `evidence/d08-r1/` 的轻量输入、日志与探针；`bin/` 产物及临时缓存不纳入提交。原始工作目录 `/tmp/agenteam-d08-r1-independent-vuogsrq8` 保留用于故障追溯，后续任务按自己的冻结输入另行验收。
