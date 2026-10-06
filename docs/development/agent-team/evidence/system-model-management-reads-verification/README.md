# System Model 管理读口持久证据

正式结论见[验收记录](../../system-model-management-reads-verification.md)。已验 13 源通过 Git 提交 `ecd733711caff5df46e423cadab52b32c34f785e` 定位；运行输入为 c54 加 [input05](author/input05.json)。

| 证据 | 定位 |
| --- | --- |
| 作者命令、原日志与输入 | [作者报告](author/author-report.md)、`author/input01.json` 至 `input05.json`、`author/input*-*.json` 和 `author/logs/` |
| 原真实失败与修正复验 | [real01 命令](author/real01/command.json)、[原日志](author/real01/raw.log)、[real02 命令](author/real02/command.json)、[原日志](author/real02/raw.log)、[精确修正差量](author/input04-to05.diff) |
| 独立输入与实际执行 | [原报告字节](independent/review.md.txt)、[probe](independent/management_independent_test.go.txt)、[编译记录](independent/compile01.json)、[真实命令](independent/real01/command.json)、[原日志](independent/real01/raw.log) |
| 实际资源交还 | [作者第一轮](author/real01/cleanup.json)、[作者第二轮](author/real02/cleanup.json)、[独立轮](independent/real01/cleanup.json)；对应目录内保存 baseline、observed-resources、observed-processes 与 monitor-errors |
| 原定位与固定字节 | [original-map.json](original-map.json)、[SHA256SUMS](SHA256SUMS)、[历史源码覆盖](historical-overrides.json) |

`original-map.json` 记录原绝对路径、字节数与 SHA。原报告、JSON、日志和 patch 保持字节；其中运行路径与私有索引属于当时现场，档案定位以本入口为准。probe 与历史 Go 测试使用 `.go.txt` 保存，避免成为仓库可编译包。

原 patch 的空白上下文行保持不变，精确文件、SHA 与诊断见 [whitespace-exceptions.json](whitespace-exceptions.json)；其余新增正文和证据无 whitespace 诊断。

历史输入由已验 Git 源码与四份原测试覆盖文件精确恢复，覆盖 input01 至 input05。校验工具同时检查所有档案字节及各输入的逐文件 SHA：

```sh
python3 docs/development/agent-team/evidence/system-model-management-reads-verification/verify_inputs.py
```

作者 17 顶层采用 real01 的 16 个有效通过与 real02 修复后预算顶层组合；独立 2 顶层/3 子例通过。原 pure/真实失败保留，不用后轮通过改写前轮。归档只核固定字节、Git 重建、链接与格式，不重跑产品测试或 fixture。
