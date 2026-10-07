# D09 后继 wire 依赖核对（2026-10-07）

本记录归档主线程已接受的两份只读核对，来源、原件 SHA 与固定输入映射见[证据索引](d09-next-wire-dependencies-2026-10-07-evidence/index.json)。这是后继任务门槛记录，**不是新的产品验收、替代规格或实施授权**。已接受的 Embeddings 产品提交 `2debfdde347d8c9262ab83d3fe5e18e947a983e1` 不解除下列前提，完整 D09 仍未完成。

| 核对 | 固定依据 | 结论 |
| --- | --- | --- |
| Jina Rerank 官方字段补证 | root 指定 `18b6fb5` 的 C0 与[D09 设计 §6/13](../work-items/d09-model-system-token-usage-design.md)；研究未执行 Git，实际输入 SHA 已固定 | 来源获取受阻；没有新增官方 wire 字段证据，不给默认型号或原生参数。 |
| Anthropic Messages 后继可行性 | `2debfdde347d8c9262ab83d3fe5e18e947a983e1` 与已采纳正式卡 `f349303bf6aa8b5c796f76cc43a34271e8c186e0` | 官方旧原件可复用，但正式实施门槛未满足；可授新实施候选为零，不提出 text-only 替代。 |

## Jina：代理拒绝不是官方响应

唯一尝试是公开 schema 候选入口 `GET https://api.jina.ai/openapi.json`，无账号、API key、body 或自动重定向。2026-10-07 08:36:45 UTC，0.014857s 后代理 HTTPS CONNECT 返回 `403 Forbidden`；urllib 原异常为 `Tunnel connection failed: 403 Forbidden`。**官方 HTTP status=null、正文 0B**，不能写成 Jina 官方 HTTP403、schema 不存在或 Provider 拒绝。记录脚本 actual exit0 仅说明失败已保存。1 次尝试后停止联网，未换域、通道或重试。

原 fetch 脚本、command、record、空正文及冻结输入清单均随索引保存。只保留了 urllib 异常文本，没有 CONNECT 原报文/响应头、官方正文或完整网络 trace；原外层 shell/heredoc 等创建 stdin、工具 stdout 未另存，不能事后重建。原 command 未绑定脚本 SHA，本归档保存收到的冻结脚本，不补造执行时字节证明。

C0 已有文本 query/candidates、合法唯一原 index、有限 score 与 top_n 约束，这些是本地契约，不证明远端字段。`query/documents/top_n/return_documents/results[index,relevance_score,document]`、usage tokens、model/id、缺失字段、排序、分数范围、长文本截断及型号差异均未得到新的官方证明；schema/SDK revision 与许可证仍 unknown。不得把 score 猜为 0..1、补零用量、自动裁切或擅选默认型号。原两页研究保留这些未知项，不据此冻结“Jina 兼容”实现。

## Anthropic：已有来源不替代正式依赖

实际复用来源为 `anthropics/anthropic-sdk-python@18f25547f20cf5f01da69ac611e700e3bc9ebf21`（1.11.0）。原核对从固定 Git 读取既有来源 manifest 和 66 份官方原件（含 LICENSE，共 297,810B），逐件核 SHA/长度，未联网或执行 SDK。manifest SHA 为 `28ea317726e14967d13f17197c9f4b1c2cd9980bba41f815dbddcbec5259c3b8`；本归档只引用[既有来源清单](anthropic-messages-wire-spec-verification-evidence/author/source-manifest.json)，不重复复制 66 文件。旧补证报告 `41f04a…515f` 未由本次核对重新取得，不把旧报告引用称为重验。

[已采纳 Anthropic text/tools 正式卡](../work-items/recovery-d09-anthropic-messages-wire.md)仍要求：

- tools **产品接受**及共享四源 `openai_chat.go`、`transport.go`、`sse.go`、`errors.go` 冻结交权后实施；不是只有排班所有权问题。
- 在 tools 的结果/事件载体与有序混排基础上追加 `Result.Message`、`Event.PartIndex`；使用同一 Exchange、worker、D04 Client 和 Budget，不复制这些共享实现。
- 原十九候选重绑 tools 已接受提交，保留混合预算及旧工具 HTTP/Stream/CloseAndBudget 回归门槛。

Embeddings 已接受的 `budgetWork` 与同一 64/8 登记表只证明 Chat/Embeddings 配额和实际 join 接缝；不证明 tools 的结果、SSE 或交付流程已接受。删除工具能力、另建 handle、改名拆卡或跳过回归均不能作为本次获准替代。本记录没有读取未验 tools 候选来填补依赖，也没有新增规格或公开 API。

原件以 SHA 内容寻址去重；固定 Git 的 C0、正式卡和旧官方源码只引用，原 scratch 路径与旧链接保持原字节，由索引提供定位。归档仅作原 hash、JSON、有限链接及 Markdown 自查，不执行网络、Go、SDK、模型或资源测试，不改 AGENTS、任务台账、计划、接续记录或旧卡。Object runtime join、tools、SPA publication 三停止事项均不解除；Summary、真实 consumer/Runtime 绑定、型号 conformance 和完整 D09 门槛保持原状。
