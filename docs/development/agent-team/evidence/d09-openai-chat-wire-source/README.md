# OpenAI Chat wire 固定来源证据

对应[恢复卡](../../../work-items/recovery-d09-openai-chat-wire.md)；本次审核输入为[修订 2 原字节副本](reviewed-card.md.txt)（SHA256 `650cf36506951abc54bf44e7c84738e074238c28c65db3a770bfdc0a8157c508`）。官方来源为 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`，版本 3.24.0；本目录仅为静态取证，不是 SDK 运行依赖或 wire conformance 通过记录。

- [source-manifest.json](source-manifest.json)：17 个采用原件的完整 commit、官方 URL、Git blob、SHA256 与字节数。
- [field-manifest.json](field-manifest.json)：固定字段/类型/原行号及行为定位，保留恢复时 rev1 卡指纹和发现，不改写历史结论。
- [excerpt-manifest.json](excerpt-manifest.json)：本目录 17 份 `.txt` 摘录的原行范围、分段/存储 SHA 及许可证来源。原件共 433,714 bytes、10,227 行；选定范围共约 50 KiB，不包含完整 SDK、bare 库或缓存。摘录新增的证据标题明确标识，所有原文范围逐字节保留。
- [LICENSE.openai](LICENSE.openai)：同一 commit 的官方 Apache 2.0 许可证，Copyright 2026 OpenAI。原根目录无 NOTICE；原许可文本未改。
- [commit-object.base64](commit-object.base64)：原 `commit-object.txt` 的无损编码；保留签名中的空白及无末尾换行，不为 Markdown/差异检查改写 Git 原对象。解码后按 source manifest 的原 SHA256 和完整 commit SHA1 校验。
- [recovery-report.md](recovery-report.md)、[initial-fetch.json](initial-fetch.json)、[git-fetch-commands.json](git-fetch-commands.json) 与 `git-0.*`/`git-1.*`：原直接 HTTPS DNS 失败、随后公开 Git exact fetch 成功的实际元数据与输出。绝对私有路径仅为当时命令证据，不是现存环境保证。
- [review-rev1.md](review-rev1.md)、[independent-sdk-identity-checks.json](independent-sdk-identity-checks.json)：独立复核 17 源 SHA/Git blob/commit 身份的原始记录。报告要求的 refusal 终态与默认 obfuscation 窄修已归位修订 2，尚待差量审查；不把原 rev1 结论写成通过。

源码确认 `completion_usage.py:43` 有 `cache_write_tokens`，D09 原映射保持。固定 SDK 的默认 SSE `obfuscation` 在修订 2 按 absent/null/string 有界验证后忽略，不关闭供应商默认；非空 refusal 为安全不可重试 content_filter 错误。其他 SDK Optional 字段不由此自动加入支持闭集。SDK 源码不证明真实账号、型号或 Model Runtime/Secret/Invocation/Usage 可用。

精确重建使用标准库脚本 [restore-sources.py](restore-sources.py)，只提取和核验源字节，不导入/执行 SDK、不安装依赖、不调用 Provider。目标必须是仓库外尚不存在的任务私有目录。已有固定 Git 对象时只读重建，无联网：

```sh
python3 restore-sources.py /task-owned/new-source-copy --git-dir /task-owned/exact-sdk-objects.git
```

对象已丢失时，仅在取得公开源码网络恢复授权后省略 `--git-dir`；脚本在新私有目标建立 bare 对象库并仅 fetch 上述完整 commit，禁用 credential helper/extra header/交互，不查询凭据。仍不执行 SDK。该 bare 库不得放入本证据目录或提交。

原始文件、Git blob、commit 对象、许可证、每个摘录范围和存储字节全部核对后，脚本才返回成功；实际离线重建命令与结果见 [archive-checks.json](archive-checks.json)。`SHA256SUMS.json` 的 card 路径绑定目录内 `reviewed-card.md.txt`，完整文件表也包含该冻结副本；不校验仓库中的可变活动卡，避免其后续页首状态更新使历史证据失效。索引避免循环给自身作 hash，原活动卡路径仅作为来源定位保存。文档与来源检查不替代 Go、真实 D04/PG/MinIO 或 Provider 验收。
