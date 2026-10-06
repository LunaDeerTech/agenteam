# Invocation/Usage 账本验收证据

本目录支持[正式验收记录](../invocation-usage-ledger-verification.md)。产品已提交推送 `36e5ff1a124c957d200888ad2e40bdc4ecb01305`；这里只归档已执行检查，没有因文档归档重新运行产品。

## 校验与固定输入

在含相关 Git 历史的仓库执行：

```sh
python3 docs/development/agent-team/invocation-usage-ledger-verification-evidence/verify-evidence.py
```

[校验脚本](verify-evidence.py)核对 [CHECKSUMS](CHECKSUMS.json) 的目录成员、大小和 SHA；核对[来源清单](provenance.json)中的原始字节（gzip 文件解压后核原 SHA）；以只读 `git cat-file --batch` 加 `objects/<sha>.txt` 历史差量重建 16 份固定输入的 owned/closure 内容，并核最终提交 20 路径、组合上游 13 路径及实际 probe SHA。它不依赖旧 `/workspace/scratch`，不执行原 driver、Git 写入或网络操作，也不把哈希校验当成重跑业务。

- [最终作者 input13](inputs/author-input13.json)：基线 `d09e8ef`，20 owned / 871 closure，SHA `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`。
- [最终组合](inputs/combined-input01.json)：input13 的 20 路径不变，叠加已提交 `ecd7337` 管理 13 路径；880 closure，SHA `8d97aef145a9582ac35444c9d7ba64688f0c74baec637f4bcbeba07e1ab30b23`。
- `inputs/author-input01..13.json` 与两份 core-freeze 清单保留原版本和原路径。最终字节由已交付 Git commit 提供；21 个按 SHA 去重的历史差量文本补足原失败/中间修订。不复制 871/880 文件树。
- 原 core-freeze-01 的基线是 `e87c6ed`，不是后续完整输入使用的 `d09e8ef`；[独立 87 文件依赖证明](independent/core01/dependency-proof.json)与 [input](independent/core01/input.json)保留实际口径。

## 执行证据入口

| 证据 | 原始结果及用途 |
| --- | --- |
| [作者交接](author/author-handoff.md)、[最终停止](author/author-final-stop.json) | 20 路径、全部 checks、分版本复用、未绑定边界与最后 SHA / process 记录 |
| `author/logs/*.{json,log}` | 纯/race/vet、完整 integration 编译、cmd build 及修后最小复查；命令、env、退出码、原 stdout 一一对应。零字节 build/vet 日志是原件，非补造 PASS |
| [real01 原日志](author/logs/real-01.log)、[原结果](author/logs/real-01.json) | input07，driver exit1；八新组中七 PASS，仅 QueryBudget 的未 ANALYZE 索引断言失败 |
| [real02 原日志](author/logs/real-02.log)、[原结果](author/logs/real-02.json) | input11，driver exit1；五受影响组中四 PASS，Reader 的删除 Session 错误码断言失败 |
| [独立最终报告](independent/report.md)、[机器结果](independent/verification-results.json) | 结论、命令/输入索引、原失败与修复、风险覆盖、组合与资源归还 |
| [publication01](independent/publication01/raw.log) / [command](independent/publication01/command.json) | 原 input07 四个真实 Committed 后取消的读取发布反例，driver exit1 |
| [perf-lookup01](independent/perf-lookup01/raw.log) / [command](independent/perf-lookup01/command.json) | 原 Lookup 同族反例失败；1201 行完整统计计时组通过；整条 driver exit1 |
| [final01](independent/final01/raw.log) / [command](independent/final01/command.json) / [result](independent/final01/result-summary.json) | 4 独立 + 6 旧回归 + 1 作者 Reader，11 顶层 PASS / 0 skip，driver exit0 |
| [combined01](independent/combined01/raw.log) / [command](independent/combined01/command.json) / [result](independent/combined01/result-summary.json) | 正式 gate-only、UsageWireLedger、SystemManagementMetadata，3 顶层 PASS / 0 skip，driver exit0 |

每个独立真实目录保存原 command、raw、baseline、observed-processes/resources、monitor-errors 与两次 cleanup。相同 `input.json` 按原 SHA 映射到 `inputs/`，映射见 provenance，未修改 command 内原绝对路径。`independent/probes/` 含执行 probe 与原未执行 A/B 错误期望版本；它们以 `.go.txt` 归档，不能误计为产品测试。编译/overlay 文件和独立 driver 也保留原字节。

作者 `real-01.resources.json.gz`、`real-02.resources.json.gz`、`real-02.processes.json.gz` 只是原重复观察 JSON 的无损压缩。原 source SHA 和压缩后 SHA 分别保存在 provenance / CHECKSUMS。real01 当时没有后加的持续进程树监视器；它的原结果、独立后续 `/proc` 核查与 real02 的持续监视严格区分，不反向补证。`author/drivers/run-real.py.txt` 是最后使用版本。

## 性能与限制

[性能报告](independent/performance-report.md)保留准确 API 计时、两条真实 SQL EXPLAIN ANALYZE 与 CPU 归因；[分析命令](independent/perf-lookup01/profile-analysis.json)、[flat 文本](independent/perf-lookup01/profile-flat.txt)、[cumulative 文本](independent/perf-lookup01/profile-cumulative.txt)、原 stderr / binary notes 和约 12 KiB 原 CPU profile 数据均保留。未复制匹配测试 ELF、MinIO 或其他二进制/cache。文本结果来自当时精确 build ID，不能把重编译的新 ELF 冒作原二进制。

作者 real01 的 1.92s 是包含 EXPLAIN 的子例总时长；当时只证明 API 在 2s 断言内完成。精确 API 时长为作者 real02 的 1.361191577s 与独立受控样本的 1.378896329s；单次 1201 行测试不承诺任意规模性能。1 条真实 wire + 1200 条明确统计 fixture 不代表 1201 次 Provider 发送。

[环境记录](environment/environment.json)保留 Go1.27.1、规定 PG17.8/PG16.12 digest、固定 MinIO SHA 与 offline 模块证明。PG16 只是不支持版本反例。未归档随机凭据、证书、fixture 挂载、模块缓存或完整源树；原始日志只保留安全投影。旧会话遗失的 wire03/schema02 原件未补造，本目录仅证明本轮恢复的执行。

此处校验与 PASS 只覆盖账本库/schema 卡。生产 Facts/Runtime、调用编排、lease/Process/lifecycle、HTTP/default root 未绑定；完整 D09、Summary 决定及 Object/Artifact 阻塞不随该卡完成。
