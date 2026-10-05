# Artifact 本域有限验证证据

结论见[正式报告](../../artifact-project-stop-verification.md)。本目录保留原始输入、失败和分版本复用依据；共享 Object guard 与更强恢复门槛仍 BLOCKED，候选 16 源仅归档，尚未提交为业务实现。本次归档没有运行 Go、Docker、网络或产品测试。

| 入口 | 内容 |
| --- | --- |
| [SHA256SUMS.json](SHA256SUMS.json) / [source-map.json](source-map.json) | 所有归档文件的 SHA/大小；原件绝对路径与持久路径对应 |
| [V 原报告](verification/report.md.txt) / [最小定位清单](verification/archive-locator.md.txt) | 已采纳的原字节结论及归档范围；原 V 索引也保留 |
| [逐项覆盖](verification/evidence/case-evidence.json) / [日志核对](verification/evidence/raw-checks.json) / [复用检查](verification/evidence/reuse-function-checks.json) | 30 新 + 37 旧的实际 run、输入、日志、粒度和复用依据 |
| [最终清单](candidate/manifest.json) / `candidate/files/` | 固定 `6658a6cb1f29299521773bc8dc86b2f607b8c809` 上的单份 review09：8 生产 + 8 测试原字节 |
| [历史索引](author/evidence-index.json) / [review09 增补](author/evidence-addendum-review09.json) | 52 个历史原日志、已有的 37 份 exit、12 个原清理记录，以及增补清单全部 76 项 |
| [历史源码差量](history/source-deltas.json) | 相对唯一 final16 的 7 个关键输入差量；每份恢复结果按其原 manifest SHA 验证，不复制旧完整树 |
| [F1](independent/f1/report.md.txt) / [foreign](independent/foreign/real/report.md.txt) | 独立 probe、原命令/env/输入/结果、原 setup 失败、资源与源码重建材料 |
| [原 patch 路径](raw-patch-paths.json) / [精确空白例外](whitespace-exceptions.json) / [归档检查](archive-checks.json) | 15 个原 patch 与 1 个原日志保留原字节；当前空白检查仅排除这 16 个精确路径 |

补充检查事实：主线程首次检查已暂存的 315 路径时，在 15 个原 patch 之外发现 `author/logs/join-pending-01.log:4` 的 `space before tab` 警告（原字节为四个空格后接 Tab）。原日志 SHA `e103146598e81a3f518742a4c46eeaad83dc66f54a0cf7e10e6a4d26241a95ea` 不变，该文件按 `raw_log` 单独列入精确例外，不能称为 patch。此前归档检查只对全证据检查尾随空白，并对三份 tracked 文档及两份新增入口运行限定 Git 检查；没有验证全部 315 路径的缩进空格后接 Tab。[原检查记录](history/archive-checks-before-whitespace-correction.json)保持原字节，不改称此前全新增检查通过。此次补充仅用 Python 重核原件与精确空白规则，没有运行 Git；主线程刷新暂存后的检查另行执行。

原 `.go`、`.md` 文件仅追加 `.txt` 后缀，避免把证据作为活动 Go 包或现行文档；内容和原 SHA 不变。原 JSON、SHA256SUMS、命令与 overlay 中的临时绝对路径保留历史值，由 `source-map.json` 定位持久副本，不能把已失效临时路径当作当前运行入口。缺失的历史 argv/input/exit 没有补造；早期错误与修后输出并存。

仅验证字节与重建 16 源（不运行产品）：

```sh
python3 docs/development/agent-team/evidence/artifact-project-stop-verification/rebuild.py --check
python3 docs/development/agent-team/evidence/artifact-project-stop-verification/rebuild.py --out /tmp/artifact-review09-overlay
```

输出目录必须尚不存在。若将来获得正式重跑授权，先从固定 base `6658a6cb1f29299521773bc8dc86b2f607b8c809` 建立独立树，再覆盖该 overlay；`go.mod/go.sum` 沿用该 base。`--variant` 接受 `production-review-01`、`rejected-close-red-01`、`red-input-join-identity-01`、`source-planning-red-input-01`、`selector-red-input-01`、`selector-fixture-input-02`、`candidate-final`，只恢复原清单覆盖的源；早期生产清单不伪称完整测试输入。

foreign 使用 `--with-foreign-probe` 将最终 16 源和原 probe 写到新 overlay。原 overlay 四份来源字节均等于 final16，精确替换关系见[路径映射](independent/foreign/overlay-path-map.json)；保留原 overlay，不复制四份重复源。F1 使用其[固定输入](independent/f1/evidence/inputs.json)、原 `production.patch` 和原 probe；不将其旧纯测试快照改称 review09 重新执行。

实际历史命令/预算来自各 `input-*.json`、`command.json` 及原索引；保留 Go 1.27.1、offline/readonly 环境、原 race/count/timeout 与实际退出值。脚本只用于解释原执行，不因入库而取得新 fixture 权限。没有缓存、构建 binary、数据库、fixture runtime/Docker 配置、活动源树或已停止的 deferred protocol 草稿。
