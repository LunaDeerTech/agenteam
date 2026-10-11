# 已提交 RoundInput 投影的基础检查

产品范围见[本片卡](../../docs/development/work-items/d22-next-round-projection.md)。`run-basic-checks.py` 复用上游 68 的记录方式，仅选择本片 **8 个新 top**：Execution reader 4、Execution contract 1、AgentLoop 3。成功且精确命中后，只 vet 这三个包；不重跑原基础矩阵，不启动 PG、Model、Runner 或 HTTP。子例数量以实际 JSON 输出为准；目前方法仅静态准备，尚未运行 Go。

root 完成上游 68 的正式组合、固定输入并授予唯一 Go/cache 窗口后，在本树运行：

```sh
python3 -B .agent-state/next-round-projection/run-basic-checks.py --output output/ai/next-round-projection/checks/basic01
```

输出目录必须不存在，失败不自动重试。脚本先分别执行 race/plain `go list -deps -test -json`，从实际编译字段收集本仓库输入（含被选中包的测试），强制包含六个本片 Go 源及 66–68 migrations。发现前先绑定候选源，再只保留实际闭包，防止发现期间输入变化；不把无关依赖包的 `TestGoFiles` 冒充编译输入。每一阶段保存 command、PID、实际 `Popen.wait`、原日志、前后输入；任何失败或输入变化立即停止。race 使用 `-p 1 -count=1 -timeout=10m -json`，逐包逐名校验 8 个 top 各运行并通过一次，额外或缺失 top 均拒绝。最后才运行原三包 vet。

固定 Go 为 `/workspace/toolchains/go1.27.1/bin/go`，并绑定其 SHA；`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`-mod=readonly`、`GOMAXPROCS=1`。共享热缓存只用 `agenteam-skill-package-http/output/ai/skill-package-http/go-build`，只读 modules 来自 `agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod`，必须按 root 的实际 writer 交接使用。闭包记录是仓库源、嵌入文件、脚本和 Go 二进制，不声称保存整个外部 toolchain/module tree。

本基础方法不拥有 PG profile、七资源或 TCP 门。真实 PG 由独立方法作者接上游 68 的真实提交后实施；基础通过也不等于下一 Model 调用、当前执行授权或生产默认接入。
