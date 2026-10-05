# Secret Model usage 验收证据

[正式报告](../../d09-secret-model-usage-verification.md)记录本卡 20 源通过及未绑定边界。最终代码已提交 `8ad6759dbb499ae1cfec1d47bcab75f1abb2f56d`；本目录不重复保存最终源码树、cache 或 binary。文档阶段仅整理冻结证据，没有运行 Go/Docker/网络或产品测试。

| 入口 | 内容 |
| --- | --- |
| [完整索引](SHA256SUMS.json) / [原件映射](source-map.json) | 持久文件 SHA/大小、原绝对路径及作者原索引的精简选取边界 |
| [独立原报告](verification/review.md.txt) / [V 原索引](verification/evidence-index.json) | 全部 23 个冻结 payload，包括私有 probe、实际 argv/env/exit、原日志和清零证据 |
| [作者报告](author/author-report.md.txt) / [作者原索引](author/delivery-index.json) | 原失败、最终纯检查、三组真实 20/85、网络补证；原索引未因只保存必要材料而改写 |
| [已采纳输入](accepted-input.json) / [最终候选清单](author/candidate-freeze-02/manifest.json) | 已验 6 生产 + 14 测试逐项匹配提交；实际测试其余输入沿业务基线 `4cc4726` |
| [历史差量](history/source-deltas.json) | nonce 编译失败、API 漏 copy、两轮 S1 原红的精确输入；缺失文件也单列记录 |
| [精确空白例外](whitespace-exceptions.json) / [归档检查](archive-checks.json) | 仅对实际发现警告的原文件逐路径记录，不改原日志/patch 字节；未列文件正常检查 |

`.go/.md` 原件仅追加 `.txt` 归档后缀，内容不改。原 JSON、原索引、命令中的临时路径保留历史值，通过 `source-map.json` 映射持久文件。最终 20 源来自 accepted commit；作者多份 `.go` 副本、冗余 `baseline.patch` 和卡全文不重复复制。四份失败输入以相对已提交源的行差量恢复，原 metadata 与 SHA 保留。S2 修前只有静态确定问题，未添加不存在的动态红。

从仓库根离线核验或生成独立源 overlay（不运行测试）：

```sh
python3 docs/development/agent-team/evidence/d09-secret-model-usage-verification/rebuild.py --repo . --check
python3 docs/development/agent-team/evidence/d09-secret-model-usage-verification/rebuild.py --repo . --out /tmp/secret-model-accepted-overlay --with-probe
```

输出目录必须不存在。原独立测试树等于业务基线 `4cc4726b5707127f4b50c7da7023a3cd1f0b7f2d` 加 accepted commit 中的 20 源和原 probe；没有把当前活动树作为验证输入。`--variant` 可选 `compile-integration-01`、`pure-final-01`、`legacy-priority-red-01`、`legacy-availability-red-01`；恢复时须遵守 `reconstruction.json` 的 absent_paths，不能补齐原来缺失的文件后再声称复现原红。

后续任何 Go/fixture 重跑须另获原范围和资源授权；本脚本只作 SHA/只读 Git 对象检查与源恢复。Unknown 是正式 Store 的结果装饰，原 AttemptID/Cause 与物理 backend attempt 明确分开。作者 `new-01` 原 network_count=0 与实际三网 live-label/两次 absence 补证并存；独立 Human 缺锁的 Session gate 边界和 Service checker 证明不互相替代。
