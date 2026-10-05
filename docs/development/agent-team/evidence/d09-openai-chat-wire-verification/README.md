# OpenAI Chat wire 验收证据

[正式报告](../../d09-openai-chat-wire-verification.md)记录库级独立验收与采纳；精确 16 源已提交 `9c72190fc1600f27c3607a3315854967ae008138`。本目录保存必要原件，不保存完整 snapshot、缓存、二进制、runtime、TLS 私钥或实际凭据。

## 固定源码与重建

原执行基线为 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`，候选 [manifest](candidate-manifest.json) SHA-256 为 `1ed6b19184fa2e3cec928ed01e3b651eadc1704718b8d0f7547e1949dbf03f80`。重建应保留原基线，再只取已采纳提交的这 16 个文件；完整检出后来的提交不能冒充原动态输入。

1. 将固定 `de00c610` 的 Git archive 导出到自有空目录，保留其 `api/`、`db/`、`scripts/` 和 `go.mod/go.sum`。`api/openapi/account.json` 与 `common.json` 不可遗漏，作者快照缺少两者的首失败原件仍保留。
2. 对 manifest 中精确 16 路径，读取 `git show 9c72190fc1600f27c3607a3315854967ae008138:<路径>` 的原字节覆盖，逐一核候选 SHA。[accepted-source-check.json](accepted-source-check.json)已实际以只读 Git blob 核完 16/16。
3. 重建独立输入时，将 [probe.go.txt](independent/probe.go.txt) 原字节放到 `tests/model/openai_chat_wire_independent_test.go`，核 [verification-input.json](independent/verification-input.json) 的 17 项 SHA 与两个模块锁；作者输入不加该 probe。

[candidate.patch](candidate.patch)保留冻结候选相对原基线的唯一完整差异（14 新路径、2 旧 fixture），可作为第 2 步的替代。它已在内存中实际应用到固定 Git blob 并得到全部 16 个相同 SHA，见 [reconstruction-check.json](reconstruction-check.json)。不重复保存 16 源目录或独立目录中的两份局部 fixture diff。归档核对没有执行 Go 或动态测试；后续运行仍须当时正式任务与资源窗口授权，不能复用历史 fixture 描述符或凭据。

## 原始证据与索引

- `author/` 保留作者报告、15 对日志及 argv/env/cwd/输入 SHA/exit 元数据，含首 unit、首 integration compile、根纯测试失败。两个直接失败测试以 `.go.txt` 后缀保存原字节，原路径和 SHA 不变；未声称全部早期源码变体都保留完整可重建树。
- `static/` 保存独立生产静审原报告、manifest 和依赖/检查指纹。`static/SHA256SUMS.json` 是原静审目录的历史索引，5 源沿已提交 Git/候选差异恢复，不重复复制全树。
- `independent/` 保存独立最终报告、原始索引、17 项执行输入、唯一 probe、编译及真实命令/日志、两轮资源记录和作者复用核对。原准备阶段 `ready.md` 保留当时待验事实，最终结论见 `verification-report.md`。原运行/清理脚本仅按 `.py.txt` 原字节归档，本轮没有执行。
- 独立原 `verification-evidence.json` 的 76 项均已在原冻结目录逐 SHA 核对。其作者/静审重复件映射到本目录已有原件，16 个 fixed 源映射到已提交 Git，两份局部 diff 由完整 candidate patch 替代；[逐项映射](independent-log-resource-review.json)记录这些选择，历史索引不等于本目录完整性索引。
- [作者日志核对](author-log-review.json)保留真实 top/sub 计数、最终与早期输入差异、根首 FAIL 和两包补验组合。空日志不证明成功，相应实际 exit 元数据一并归档。两侧资源核对均只解析原件，没有重新访问 Docker。
- [archive-provenance.json](archive-provenance.json)记录所有原件来源与 SHA。[SHA256SUMS.json](SHA256SUMS.json)为最终完整性索引，覆盖本目录除自身外全部文件及外侧正式报告；不绑定未来会修改的活动状态页。

## SDK 来源与未验范围

官方 SDK `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813` 的 17 采用文件、字段摘录和许可证已随 `9d648575d07ce791dd623eb45eb6604cc6c908b5` 保存在[来源证据](../d09-openai-chat-wire-source/README.md)，本目录引用固定身份，不再复制 SDK。profile manifest 可沿已提交 16 源恢复，来源核对记录保留；来源正确不单独代表运行 conformance。

测试中的合成 canary 是协议输入，不代表 Secret/Project/Invocation 授权。库级通过不包含真实 Provider 账号 smoke、Model consumer/Runtime、Secret Model resolve、Invocation、持久 Usage 或生产 root；Summary 初值/Settings 待决边界不变。
