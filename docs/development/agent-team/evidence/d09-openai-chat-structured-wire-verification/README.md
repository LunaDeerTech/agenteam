# Structured wire 轻量验收证据

对应[正式验收记录](../../d09-openai-chat-structured-wire-verification.md)，接受提交为 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74`，业务基线为 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`。本目录只归档原始证据和可重建定位，不保存完整树、cache、binary、生成凭据或运行中的 fixture。

[archive-provenance.json](archive-provenance.json)逐项关联作者 87 payload、独立 39 定位及两个原索引：同字节只存一次；与接受提交相同的源码沿精确 `commit:path` 读取。所有保留原件均保持原字节，不修改原报告“当时待验”的状态、路径、argv、退出码或日志。当前采纳状态以正式验收记录为准。历史绝对临时路径是原执行定位，不要求该路径仍存在；去重后的归档地址由此映射确定。

- [作者原索引](author/author-evidence.json)与[独立原索引](independent/final-core-index.json)保留完整 SHA/长度。
- [最终 10 源](author/candidate-review-02/manifest.json)以已提交 Git 对象为主；[原 candidate patch](author/candidate-review-02/source.patch)只保留一份，作为原阶段证据。
- 原红前像、review01/review02 patch 和各自 manifest 保留。少量未提交源码原件使用 `.go.txt` 后缀，字节不变，不在 docs 中形成新 Go 包。
- [独立 probe](independent/probe.go.txt)及原 runner/命令、日志、存活资源与双清理原件保留；runner 是历史执行记录，硬编码当时路径，不是生产接口或默认复验入口。
- SDK 两原文件已在[接受源码的 manifest](../../../../../internal/central/model/adapter/testdata/openai-chat-structured-v1.json)内原字节保存；旧 17 源与许可证复用[已提交来源](../d09-openai-chat-wire-source/README.md)。本次不再复制 SDK，不联网/安装/执行 SDK。

## 只重建输入，不执行产品

[reconstruct.py](reconstruct.py)只读取本目录和本地固定 Git 对象，核对每项长度/SHA；不调用 Go、Docker、数据库、网络或 shell。默认在内存验证，不写文件：

```sh
python3 docs/development/agent-team/evidence/d09-openai-chat-structured-wire-verification/reconstruct.py --mode final
python3 docs/development/agent-team/evidence/d09-openai-chat-structured-wire-verification/reconstruct.py --mode red
python3 docs/development/agent-team/evidence/d09-openai-chat-structured-wire-verification/reconstruct.py --mode independent
```

三个模式分别恢复作者最终 695 项、F1/F2 原红 694 项、独立 probe 的 823 项实际输入。`--repo` 可显式指定含上述 commit 的本地仓库；追加 `--output /path/to/new-private-directory` 时只物化这些字节，目标必须不存在，仍不运行源码。原红不补后来 OpenAPI 资产，避免倒写原执行输入。实际动态复验仍须遵守资源唯一所有权与固定原预算，不能因重建成功称测试通过。

[归档核对](archive-review.json)记录原件/接受提交、原日志计数、两侧双清理和三个重建输入的只读核查。[SHA256SUMS.json](SHA256SUMS.json)是最终归档与正式报告指纹；本 README、重建脚本及派生核对单独标识为新文档，不冒充当时原件。

[原始空白例外](raw-whitespace-exceptions.json)只列原件中实际存在的 patch 上下文空行等及其 SHA。保留它们不等于放宽新增说明文档的格式要求；主线程限定 diff 检查可仅排除所列精确路径。本次归档没有新的 Go/Docker/网络或 Git 写操作。
