# Tools wire 有限证据归档

**BLOCKED：作者检查完成，独立动态 A/B 为 NOT RUN，产品14路径未提交。** 当前结论见[验证记录](../openai-chat-tools-wire-verification.md)。历史原件中的 pending、running、grant 等描述保留当时状态，现行状态见 `status.json` 及窗口关闭原件，不能从编译或授窗推断已经运行。

- `candidate-files.json` 和 `candidate/` 保存最终 input08 的14个逐字源码文本，路径附 `.txt`，不会成为新 Go 包。
- `author/` 保存固定输入、实际 argv/env/exit/raw、原红、差量、作者双清和最终末检；原绝对私有路径仅作为出处。旧索引可能引用未重复复制的 tar；对应内容由下述去重映射保留。
- `history/reconstruction.json` 将 core01/core02、race03、input04–08 逐路径指向最终候选或8个旧版本文本，全部按原SHA验证。早期 compile01/unit02 仅保历史日志/指纹，两个旧源码版本未完整留存；最终证据不依赖这两次早期PASS。
- `independent/` 只保存已有静审、F1纯探针、离线编译以及受阻A/B准备原件；`.go`/`.py`/`.md` 原件加 `.txt`。A/B 没有动态日志和退出值，不执行这些保存的脚本。
- `resources/` 保存协调者对两个作者窗口与一个 NOT STARTED 独立窗口的原始记录。
- `original-map.json` 记录每份原件路径、字节数与SHA；`SHA256SUMS.json` 覆盖除自身以外的全部归档文件和上级验证页指纹。已归档的官方SDK来源直接引用[规格证据](../evidence/openai-chat-tools-wire-spec-verification/README.md)，没有再复制SDK或依赖树。

候选的静态恢复基线是 `ecd733711caff5df46e423cadab52b32c34f785e`。在授权的独立副本中取得该Git提交后，按 `candidate-files.json` 将14份文本字节放回对应 `path`，即可恢复最终候选；余下依赖由 Git 提交重建。这个说明不授权重建或运行当前受阻的独立 A/B，不把产品候选当作已提交能力。

只读检查命令：

```sh
python3 docs/development/agent-team/openai-chat-tools-wire-verification-evidence/verify_archive.py
```

可选 `--candidate-root /path/to/read-only-candidate` 只比对已有14个文件，不创建副本、不改写文件、不执行测试或访问网络。默认检查同时验证九份历史固定输入的每条源码映射。纯哈希检查不代表业务或独立动态验收。原件中保留的6份空白差异由 `whitespace-exceptions.json` 按文件、行和SHA列出；新增说明与脚本本身均通过UTF-8/LF/尾空白检查。
