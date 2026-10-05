# System Model 默认根验收证据

[正式报告](../../system-model-root-verification.md)归档已采纳的 8 路径提交 `457b1979c9d6563740543b2011eedc06cce34c71`。作者有效 15 顶层/21 子例，独立有效 2 顶层/4 子例均包含明确分段复用，不代表最终版一次全绿。

| 入口 | 保存内容 |
| --- | --- |
| [原件映射](source-map.json) / [全量指纹](SHA256SUMS.json) | 原临时位置、未变 SHA/大小，以及从精确 Git 对象恢复的重复源码 |
| [最终输入](accepted-input.json) / [最终冻结](author/final-freeze-03/manifest.json) | 6 源和验后两能力文档；六源与 candidate02 完全相同 |
| [作者报告](author/handoff/report.md.txt) / [矩阵](author/handoff/summary.json) | 原纯准备失败、两版真实输入、6 个实际轮和完整资源记录 |
| [作者差量](author/candidate-freeze-02/from-candidate01.patch) / [首红归因](author/failure-review-01/triage.md.txt) | 四处 test-only 修正，原失败两测试按 `.go.txt` 保存 |
| [独立原报告](verification/review.md.txt) / [交付索引](verification/delivery-index.json) | 原 60 payload 与 7 外部冻结定位全部核对；未改原状态和本机路径 |
| [原 probe](verification/probe-freeze-01/model_root_independent_test.go.txt) / [修后 probe](verification/probe-freeze-02/model_root_independent_test.go.txt) | Host/CookieJar 首红、精确取样修正与修后身份组；初始化限定复用 |
| [生产静审](static/production-review01/review.md.txt) / [规格静审](static/spec-review01/review.md.txt) | 独立的固定输入、原 PASS 与边界 |
| [归档检查](archive-checks.json) / [精确空白例外](whitespace-exceptions.json) | 原件、重建、JSON/Python 语法及文档链接/格式核验 |

原 `.md/.go` 文件名仅追加 `.txt`，内容逐字保持。旧报告中的“待独立”“两文档未更新”描述各自冻结时点；最终状态以正式报告和 accepted commit 为准。原索引中的本机路径由 `source-map.json` 定位，不能只保留临时路径当持久证据。所有报告、日志、patch、nonce 资源记录均来自 task-owned fixture；没有复制受限 bootstrap log 或真实账号材料。

在仓库根核验或生成小型 overlay，不运行测试：

```sh
python3 docs/development/agent-team/evidence/system-model-root-verification/rebuild.py --repo . --check
python3 docs/development/agent-team/evidence/system-model-root-verification/rebuild.py --repo . --variant independent02 --out /tmp/system-model-root-probe-overlay
```

输出目录必须不存在。`accepted` 为最终 8 路径；`candidate01`/`candidate02` 包含当时两能力文档，原失败两测试按原字节恢复；`production01` 为 3 源；`pure01` 仅当时存在的 4 源，另两份 integration 源尚未创建；`independent01`/`independent02` 为基线 ac5 上的 3 生产加独立 probe，没有作者测试。工具同时核所有版本 SHA，不创建全树，不运行 Go/Docker、不访问网络或修改 Git。后续任何实际 fixture 执行仍需主线程单独授权。
