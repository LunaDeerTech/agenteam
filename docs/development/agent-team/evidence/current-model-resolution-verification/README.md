# Current Model Resolution 验收证据

[正式报告](../../current-model-resolution-verification.md)对应已采纳的精确 20 路径提交 `4295df7d51c1f171df78ab3f0d9cef2fd241a505`。测试输入为 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74` 的固定闭包加这 20 路径，不能以整个后续提交替代基线。

| 入口 | 内容 |
| --- | --- |
| [输入绑定](accepted-input.json)、[原件和源码映射](source-map.json)、[指纹](SHA256SUMS.json) | 原临时位置、原字节 SHA/大小，精确 Git 源定位与历史小型覆盖文件 |
| [作者原报告](author/author-final-frozen.md.txt)、[原交付索引](author/final-delivery.json) | 分版本有效 26 顶层/109 子例、纯检查、原失败和每轮资源 |
| [最终 20 输入](author/candidate-freeze-04/manifest.json)、[不变基线闭包](author/inputs/immutable-inputs.json) | 精确输入；693 基线文件只保存原索引，不复制树 |
| [生产原审](static/production/review01.md.txt)、[修后](static/production/review02.md.txt) | P1/P2 静态发现；原候选和修订均保留 |
| [readiness 原审](static/readiness/review.md.txt)、[candidate02](static/readiness/review-candidate02.md.txt)、[candidate03](static/readiness/review-candidate03.md.txt) | T1/T2/T3 和旧 txContextKey 的静态修正链 |
| [Schema 原红](author/fixture-runs/new-01/driver.log)、[归因](static/readiness/new01-schema-diagnosis.md.txt)、[修订](author/candidate-freeze-04/delta.patch)、[复验](author/fixture-runs/schema-01/driver.log) | 首次 Fault 的 SQLSTATE 未观测；后续 42883/相同源 checksum 重试是另一轮实测 |
| [独立原结论](verification/review.md.txt)、[原交付索引](verification/delivery-index.json)、[probe](verification/current_resolution_independent_test.go.txt)、[overlay](verification/overlay.json) | 未参与生产实现的 2 顶层/4 子例，真实权限、竞争、同 Tx witness 与全部事实回滚 |
| [独立命令](verification/fixture-runs/independent-01/command.json)、[raw](verification/fixture-runs/independent-01/driver.log)、[双清理](verification/fixture-runs/independent-01/resource-handoff.json) | 实际 argv/env/exit、原预算与实际资源观察 |
| [归档检查](archive-checks.json)、[原字节空白例外](whitespace-exceptions.json) | 原件映射、局部重建、链接和格式检查；不新增业务运行 |

原 `.md`、`.go` 文件仅加 `.txt` 后缀，内容不变。原件里的本机路径由 `source-map.json` 映射；其中“待动态”等语句保留原冻结时点，不倒写为当前结果。历史源码按 SHA 去重，只保存未被接受提交覆盖的 15 个小文件；最终 20 源从精确提交读取。四次最早编译非零的实际输入、缺失路径、命令、环境和日志均保留。

两份中间成功版本的 fixture `48a751d4…`、selection `ff09c65f…` 原字节未另存，不能逐字重建 integration-compile-03、integration-vet-01 和 final-integration-compile-01 的相应输入；实际 metadata/hash/log 保持原样，见 `source-map.json` 的 `unavailable_intermediate_sources`。review02-vet 和 commands-build 也记录了它们，但各自 argv 未执行该测试包；相应 vet variant 以 `not_selected_by_argv` 显式列出。最终 20、全部关键失败/修复与真实验收输入不受影响，不用后来的源码假替这些历史哈希。

在仓库根验证或重建小型覆盖目录，不运行产品测试：

```sh
python3 docs/development/agent-team/evidence/current-model-resolution-verification/rebuild.py --repo . --check
python3 docs/development/agent-team/evidence/current-model-resolution-verification/rebuild.py --repo . --variant independent --out /tmp/current-resolution-overlay
```

输出目录必须不存在。`candidate-freeze-01` 至 `04`、`production-review-01/02`、四个早期编译失败及五个纯检查输入、`independent` 共 16 种局部覆盖均有 SHA 校验。只有 `independent` 包含私有 probe；原 overlay 保留当时绝对路径，重建目录不假装是可直接运行的完整仓库。需要运行时，另在已授权的固定基线上应用对应覆盖；不得直接运行原 observer 或据归档取得 Docker 权限。

归档阶段只使用 `--accepted-files` 和 `--baseline-root` 读取既有冻结副本完成校验，未执行 Git/Go/Docker 或网络。Git 对象入口留给已授权的后续核验，未把本轮本地副本校验写成新的 Git 读取结果。受控 fixture 的 nonce/资源标签属于验收定位；不保存真实凭据、binary、cache、受限恢复日志或完整源码树。
