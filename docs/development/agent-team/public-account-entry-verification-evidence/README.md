# 公开 Account 入口证据

当前结论见[验证记录](../public-account-entry-verification.md)与 `status.json`：25 源范围独立验收通过，主线程已接受并以 `787a5c7` 提交推送。12 个原顶层按固定输入分轮通过，并非 input13 单轮全量重跑；原阶段记录逐字保留，不用新版说明改写旧退出值。

`original-map.json` 按逻辑文件名保存出处、SHA-256、字节数及实际归档路径。重复的源/依赖指纹、日志空文件、卡前后原件和源码按内容去重，原字节只保存一次，通常位于 `objects/<sha256>.txt`；关键报告位于 `reports/`。`.txt` 避免原Go/Node/Python测试或驱动成为归档中的可运行入口。私有绝对路径仅作原始出处，校验不依赖原scratch。

`candidate-files.json` 对应冻结25源，`history-reconstruction.json` 将各历史输入的每条源指向原件，包含原缺陷、修复及测试差量。候选其余依赖按 `c54f73f3324caa11608d84e5d207141985eb6074` Git基线恢复；既有后端组合另由独立manifest固定已接受management13/ledger20，不包含tools14。`dist-hashes.json` 仅保留冻结 input13 的15资产指纹，均与冻结 input08 相同；工作区较早生成的 `web/dist` 未用于验收，未声称其匹配。归档不包含dist、node_modules、缓存、二进制或完整源码树。

可在仓库根运行只读归档检查：

```sh
python3 docs/development/agent-team/public-account-entry-verification-evidence/verify_archive.py
```

可选 `--candidate-root /path/to/existing-candidate` 只比对已有25个文件，不创建工作树或写回源码。脚本只读取本地文件和哈希，不导入项目代码，不调用Go/npm/Docker/浏览器/SQL/网络，不执行任何归档脚本。其PASS仅表示归档一致，不替代业务验收。

`SHA256SUMS.json` 覆盖除自身外全部归档文件及上级验证页。原件的空白差异保留在 `whitespace-exceptions.json`，包括Git diff上下文及原日志；不为格式检查改变证据字节。最终结论只使用已固定输入、实际结果和明确复用关系。
