# Jina rerank 有界调研结果（2026-10-08）

本轮未取得任何可用于冻结 Jina wire 字段的一手正文。已验 C0 与 wire 基础可供准备工作引用；实际字段证据仍缺，不能冻结实施卡、开工实现或宣称 conformance。[来源清单](sources-manifest.json)保存精确 URL、UTC 时刻、命令退出码和各小原件 SHA；[结果](result.json)明确未验证边界。

## 固定依赖与缺口

| 层次 | 本轮实际证据 | 结论 |
| --- | --- | --- |
| C0 非 Chat / rerank | `nonchat.go`、`types.go` 两文件与已验 `e6e94c4` 的保存核对相符，当前 SHA 再核一致 | Query/Candidates/TopN、原索引和有限 score 的平台契约已存在；不证明 Jina JSON 字段 |
| 共用 wire 基础 | `budget.go`、`transport.go`、OpenAI embeddings 入口/transport/parser/manifest 六文件逐 SHA 等于已接受 `2debfdde` 清单 | 可准备复用 D04 transport、安全错误及统一 64 全局/8 Project exact-handle Budget；未修改旧协议 |
| D04 及传递基础 | 正式 D09/embeddings 卡与已接受 wire 验证档证明其受控出站、Policy/SecretMaterial、实际 I/O 收尾边界已有验收 | 仅复用相应已有结论；本轮未重新核完整 Go/fixture 闭包，下一实际实施/验证阶段仍须固定其真实输入 |
| Jina 官方协议 | 三次 CONNECT 403；候选 README 404；候选仓库 HEAD 失败 | 零字段事实；有效官方 repo、commit、许可均未建立 |
| 后续业务 Nonchat.Rerank | C0 结果需要 InvocationID；当前生产 Model Authorizations 未设置 Resolution，Usage Authorizations 的 Invocations 为 nil | wire 准备不填补 Runtime/Invocation/Consumer/生产 root；不能伪造授权、调用事实或 InvocationID |
| 三项已停止工作 | 文本 rerank wire 准备不消费 Object 内容、未验 tools 或 SPA 发布 | 本次不恢复、替代或执行任何停止任务 |

固定输入与八项源码核对分别见 [repository-inputs](evidence/repository-inputs.json)、[补充输入](evidence/repository-inputs-addendum.json)、[C0 核对](evidence/c0-accepted-match.json)、[wire 核对](evidence/accepted-dependency-match.json)。源码核对是只读字节检查，不是重跑上游动态验收。

C0 具体边界：query 与每个候选非空、合法 UTF-8、无 NUL、各≤16MiB；TopN 在 1..候选数。返回 index 唯一、非负且小于原候选数，score 必须有限；`RankedItems.ValidateFor` 只要求数量≤TopN，并未规定必须恰 TopN、不得为空、score 位于[0,1]或排序/tie 规则。现有测试明确接受 12.5 与 -7。以上仅为本地契约，不能转写成 Jina 服务行为或上限。

## 五次来源请求的真实结果

1. `https://api.jina.ai/openapi.json`：curl exit56，CONNECT proxy 403，未取得 origin 文档。
2. `https://jina.ai/reranker/`：同样 exit56/CONNECT proxy 403。
3. `https://api.github.com/search/repositories?q=sdk+org%3Ajina-ai&per_page=10`：同样 exit56/CONNECT proxy 403，未发现有效 SDK。
4. `https://raw.githubusercontent.com/jina-ai/jina-reranker-v3/main/README.md`：curl exit22，HTTP404；14字节错误体不是官方字段正文。该名称只是候选，不证明官方仓库有效。
5. `git ls-remote https://github.com/jina-ai/jina-reranker-v3.git HEAD`：子命令 exit128，Repository not found，未取得 commit；该 Python 记录包装器本身曾退出0，不混写两层退出码。

CONNECT403只证明本环境代理拒绝请求，不证明 Jina 产品/账号/API 失败。404与远程查询失败也不证明该候选是有效公开官方仓库。未使用第三方兼容材料或记忆补齐协议；未修改/关闭/绕过 proxy。root 已确认在此停止取证，本轮不再请求。

## 授权与当前边界

新任务明确授权了公开只读证取，因此没有把旧 D09 §6「本轮不继续联网」的过去阶段限制误当本次禁止。随后依据实际失败和 root 明确指示停止。未提供认证材料、未调用真实 rerank/model 服务、未安装或运行 SDK、未启动测试资源，未更改生产/共享契约/锁。

当前没有需要用户代决的新产品含义；缺口是官方字段证据。若未来一手资料与已定 C0 范围产生冲突，应由 root 组织明确规格修订，而不是本次猜测默认值或缩范围后声称通过。最小后续方案见[准备稿](preparation.md)，它不是已冻结工程规格。
