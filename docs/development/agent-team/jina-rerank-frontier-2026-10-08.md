# Jina rerank 前沿准备（2026-10-08）

本轮没有取得可冻结 Jina wire 字段的一手正文。五次有界来源请求全部失败并已停止联网；有效官方仓库、commit、许可均未建立。已验平台契约和 wire 基础可供继续准备，但官方字段前置未满足，**不冻结实施卡、不接受产品、不声称 conformance**。[结果](jina-rerank-frontier-2026-10-08-evidence/result.json)、[完整只读发现](jina-rerank-frontier-2026-10-08-evidence/findings.md)与[未冻结准备稿](jina-rerank-frontier-2026-10-08-evidence/preparation.md)保留这一范围。

root 新任务明确授权公开只读取证，因此没有将[D09设计§6](../work-items/d09-model-system-token-usage-design.md#6-五种协议-profile固定源码事实与-conformance-边界)中早期阶段「本轮不继续联网」误解为本任务的禁令。随后 root 根据本轮失败明确确认停止取证；未改动或绕过 proxy，未安装/运行SDK、未调用真实 rerank/model 服务、未启动资源或停止任务 probe。只读研究不能解除 Object runtime join、OpenAI tools 独立验证、SPA 并发发布的三项停止。

## 固定依赖及未满足前置

| 层次 | 固定依据与实际核对 | 结论 |
| --- | --- | --- |
| C0 rerank | [两文件原核对](jina-rerank-frontier-2026-10-08-evidence/evidence/c0-accepted-match.json)：`nonchat.go`、`types.go` 当前 SHA 与已验 `e6e94c4` 相同 | 本地 Query/Candidates/TopN、原索引及有限score契约存在；不是Jina JSON字段证明 |
| 已接受 wire 基础 | [六文件逐SHA核对](jina-rerank-frontier-2026-10-08-evidence/evidence/accepted-dependency-match.json)，对应[已接受 embeddings 产品](openai-embeddings-wire-verification.md) `2debfdde347d8c9262ab83d3fe5e18e947a983e1` 的保存清单 | Transport、统一64/8 exact-handle Budget及embeddings参考源可复用；未改旧协议 |
| D04/传递基础 | [正式 embeddings 卡](../work-items/d09-openai-embeddings-wire.md)与[已接受 text wire 报告](d09-openai-chat-wire-verification.md)已有受控出站/真实收尾验收 | 复用已接受边界；本轮未重新枚举、编译或执行完整后继Go/fixture闭包，实际阶段仍须另固定 |
| Jina协议字段 | 下表五次请求均未提供有效官方字段正文 | query/文本documents/top_n/return_documents/results/index/score/usage/error/caps/endpoint/auth全部保持待证候选 |
| 业务 Nonchat/生产根 | [固定补充输入](jina-rerank-frontier-2026-10-08-evidence/evidence/repository-inputs-addendum.json)覆盖真实app装配：Model Resolution未设置，Usage Invocations仍nil；C0结果要求InvocationID | 本研究及未来wire库不制造Runtime/Invocation/consumer授权或生产绑定，完整业务端口继续缺前置 |

其余输入见[首次指纹](jina-rerank-frontier-2026-10-08-evidence/evidence/repository-inputs.json)。C0只约束返回数≤TopN、原index合法唯一、score有限；现有测试接受12.5与-7，不得发明[0,1]范围、恰TopN、排序/tie或非空结果的供应商保证。新规格所需字段语义必须以有效官方来源另证，不能靠第三方兼容、记忆或缩小范围跳过。

## 五次实际来源失败

[来源 manifest](jina-rerank-frontier-2026-10-08-evidence/sources-manifest.json)逐项保存精确URL、UTC日期、argv/退出码和headers/stderr/error-body指纹。没有官方正文，commit和许可记为未知；14字节404错误体不计字段来源。

| 来源请求 | 实际结果 | 原件 |
| --- | --- | --- |
| `https://api.jina.ai/openapi.json` | curl exit56；CONNECT proxy 403，未到达可读取的origin文档 | [meta](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-openapi.meta.json)、[headers](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-openapi.headers)、[stderr](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-openapi.stderr) |
| `https://jina.ai/reranker/` | curl exit56；CONNECT proxy 403 | [meta](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-page.meta.json)、[headers](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-page.headers)、[stderr](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-page.stderr) |
| `https://api.github.com/search/repositories?q=sdk+org%3Ajina-ai&per_page=10` | curl exit56；CONNECT proxy 403，未建立有效SDK来源 | [meta](jina-rerank-frontier-2026-10-08-evidence/evidence/github-jina-sdk-search.meta.json)、[headers](jina-rerank-frontier-2026-10-08-evidence/evidence/github-jina-sdk-search.headers)、[stderr](jina-rerank-frontier-2026-10-08-evidence/evidence/github-jina-sdk-search.stderr) |
| 候选 `https://raw.githubusercontent.com/jina-ai/jina-reranker-v3/main/README.md` | curl exit22；HTTP404；该候选不证明官方repo有效 | [meta](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-readme-main.meta.json)、[headers](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-readme-main.headers)、[错误体](jina-rerank-frontier-2026-10-08-evidence/sources/jina-reranker-readme-main.raw) |
| 候选 `git ls-remote https://github.com/jina-ai/jina-reranker-v3.git HEAD` | Git子命令exit128，Repository not found；没有commit。记录包装器本身exit0，不能混写 | [meta](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-head.meta.json)、[raw](jina-rerank-frontier-2026-10-08-evidence/evidence/jina-reranker-head.raw) |

CONNECT403只是本环境代理拒绝，不是Jina产品或账号失败；候选404/remote错误也不能证明该名称是有效官方仓库。原只读Git来源/本地blob查询记录保持，未执行Git写；明确续派后的禁Git阶段未重新执行这些查询。短暂环境状态变化后只读取原记录并观测其PID当前均不在，不补历史actual-wait、机器重启或清理结论。

## 后续准备与归档

当前缺口是证据，不是新的用户产品选择。下一步须在未来明确授权取证时补齐精确字段/默认与null、索引/数量/score、usage单位、error、安全endpoint/auth、型号/容量边界及来源版本/许可；再由root组织最小完整规格与独审。完整实施/受控conformance/实际资源窗口另授，不能因已有依赖就绪自行启动。[准备稿](jina-rerank-frontier-2026-10-08-evidence/preparation.md)仅保留上述前置及后继正反验收要求。

归档复制 25 个小原件/私有材料、38105 bytes，另有[归档manifest](jina-rerank-frontier-2026-10-08-evidence/archive-manifest.json)；逐文件核对字节与SHA，不复制仓库、SDK、依赖树或生成大型索引。自查只解析JSON、核来源/输入指纹、链接及非原始HTTP文本格式，不执行保存的取证脚本或产品测试。HTTP headers保留原CRLF，不为格式检查改写原件。

本报告及同名证据目录是唯一新增仓库范围；主设计、台账、生产源码和锁均未改，原固定依赖恢复 `20d45a72` 不重跑。研究只交付有界失败与准备材料，UI工作可独立继续。
