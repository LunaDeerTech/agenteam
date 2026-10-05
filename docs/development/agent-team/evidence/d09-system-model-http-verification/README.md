# System Model HTTP 验收证据

[正式报告](../../d09-system-model-http-verification.md)仅认定库级 HTTP 与被动 credential 查证通过；生产 root 尚未挂载。最终 17 源从已采纳提交 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7` 复建，不在本目录复制完整源码树、cache 或 binary。

| 入口 | 内容 |
| --- | --- |
| [SHA256SUMS.json](SHA256SUMS.json) / [原件映射](source-map.json) | 全部持久文件 SHA/大小与原临时路径；原字节保持 |
| [作者矩阵](author/coverage-matrix.json) / [作者报告](author/author-report.md.txt) | 精确 79 个引用全保留，含原失败、实际 argv/env/exit、四组覆盖和资源双清理 |
| [V 原报告](verification/final-report.md.txt) / [原 core 索引](verification/final-core-index.json) / [locator](verification/archive-locator.json) | 2 顶层/4 子例、源固定、启动语法拒绝及实际资源边界 |
| [accepted 输入](accepted-input.json) / [历史源差量](history/source-deltas.json) | 最终 17、业务基线 `9110686` 与已验 Secret20；unit01/unit02、静态 production01、真实首红 fixture01 的小型差量 |
| [原探针](verification/probe/independent_system_http_test.go.txt) / [实际独立结果](verification/evidence/dynamic01/result.json) | 原 probe、两个候选版本的 compile 元数据、实际 selector/环境和唯一真实一轮 |
| [精确空白例外](whitespace-exceptions.json) / [归档检查](archive-checks.json) | 逐文件实际检查，原 patch/log 的警告按精确路径保留，不能修改原字节消警告 |

原 `.go/.md` 名称仅追加 `.txt` 后缀；原索引、JSON、命令/env 和绝对路径不改，由 `source-map.json` 定位持久副本。V 旧/新 fixture 的重复源由[版本映射](history/omitted-duplicate-source-map.json)恢复。unit02 的 `secret/write_lookup_test.go` 原旧副本不在作者目录，V 逆转 F1 新增测试后逐 SHA 命中原 `00814055…`；其恢复原件与来源说明保留，不改称原物理副本一直存在。F1 修前只有静态发现，没有动态原红。

从仓库根核验或生成独立 overlay（不运行测试）：

```sh
python3 docs/development/agent-team/evidence/d09-system-model-http-verification/rebuild.py --repo . --check
python3 docs/development/agent-team/evidence/d09-system-model-http-verification/rebuild.py --repo . --out /tmp/system-http-accepted-overlay --with-probe
```

输出目录必须不存在。原独立输入为基线 `9110686a507716d8b4e042a5562a22507658adcb` 加 Secret20 `8ad6759`、HTTP17 `ac5b4c6` 和 probe；脚本从 Git 对象读取 20+17 源，不消费活动工作树。`--variant unit01|unit02|production01|fixture01` 按原清单恢复 14/14/9/17 项；原 pure 时三份 integration 源尚未存在，static production01 仅声明生产范围。后续任何 Go/fixture 执行须另行获得授权。

配置 5/26 来自 input01 的限定复用；其余有效真实组来自 input02，未声称最终版本一次全跑。原 Secret Unknown 首红未命中 receipt，修复仅两测试；F1 静态、V 包装器 Python 启动错误和真实测试失败分别保存。Unknown 只证明正式 Store 结果处理，不冒称网络丢 ACK、存活 writer 或 ROLLBACK 干预。
