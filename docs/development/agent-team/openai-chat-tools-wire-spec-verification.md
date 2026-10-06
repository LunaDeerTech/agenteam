# OpenAI Chat tools wire 规格采纳

状态：**rev1 已独立静审 PASS 并获主线程采纳**，规格提交推送 `b2be334cd8a78b904878bdbc388a43805390f7b7`，主线程确认远端一致。当前进入限定库实现，尚无产品验收；完整 D09 未完成。

[正式卡](../work-items/recovery-d09-openai-chat-tools-wire.md)固定业务基线 `ecd733711caff5df46e423cadab52b32c34f785e`。独立审查者 `recovery_documentation` 未参与设计或实现，审过最终技术稿 SHA-256 `5c9656cb8fa3f9a4785b701cee433f34f44fb244e0e4580c90920913ff33addb`；采纳稿 SHA-256 为 `bd20d74ce497df08fe3276387e5d143b7dc5f8deeb2ceef89905cb6e2f186e37`，仅首段行政状态及证据定位不同，技术 §1–9 原字节一致。

## 来源与独立结论

[轻量证据入口](evidence/openai-chat-tools-wire-spec-verification/README.md)保存固定官方 SDK `becc1d20eed83c1b8d85e15dc131a372d9dc7813` 的 21 个完整选定文件、原许可证、公开获取命令、指纹与离线复原方式。来源 manifest SHA-256 为 `4f28ba7834759fd87d0c595cce1b1aaee6b7b0d96fa985ffbcd15aaf0f14537d`；未安装或执行 SDK，不把字段声明当成型号或供应商账号通过。

[独立原报告](evidence/openai-chat-tools-wire-spec-verification/independent/review.md.txt) SHA-256 `af779a8c0aacd88f9692cdff605c73d46084ba1bd7bfa534203df9cb20164450`。[66 项静态核对](evidence/openai-chat-tools-wire-spec-verification/independent/static-checks.json)实际通过，覆盖官方 SHA/Git blob/字段行、原 commit 对象一致、14 个已验依赖指纹和候选 4 旧 + 10 新实施路径。审查确认：新增 Result.ToolCalls 与有界 ToolDelta/ToolComplete 接缝可在授权路径内实施；SSE 同 index 重复 entry 按原生顺序追加；参数累积与队列分别限额；finish/完成标记不替代 DONE、actual join 或业务终态，不赋予工具执行权限。

原 rev1 有两处静态问题，均保留原输入与窄修差量：

| 原问题 | 最终约束 |
| --- | --- |
| null/省略 content 的非空 refusal 与成功正文形状冲突 | 合法拒绝先保可靠 usage，再返回原 content_filter；新增 HTTP 验收场景。 |
| wire JSON 检查比 C0 ToolCallPart.Validate 更宽 | 普通工具与全部 SSE 完成均须满足原 C0，明确根 1 且含标量的 32 层、数字有限性、NUL ID 拒绝；保留有限 >2^53 数字原词法，增加双模式正反例。C0 不改。 |

[最终差量检查](evidence/openai-chat-tools-wire-spec-verification/independent/final-checks.json)确认未扩路径或改旧修订规则。这是规格问题关闭，没有运行动态红例或产品测试。

## 实施分工与未验门槛

主线程已授权 `management_reads` 以 backend_worker 身份唯一实施卡 §8 的 14 条路径；`recovery_documentation` 承担后续独立验收，`verification_recovery` 协调独占 Docker 窗口。所有子 agent 不再委派，停止写入并固定输入后才能形成实际验收。卡内“待主线程另行授权”的原行政措辞保留在接受提交，当前派工以本段及台账为准。

作者须完成 adapter unit/race/vet、相关编译/build、3 个新真实顶层和原 text/structured 各 3 顶层；独立者补合法 envelope 内参数/选择失败和已交付工具输出后取消/缺 DONE。原 512 KiB fixture 可容纳实际 320 KiB 参数场景，独立静态算例总 SSE payload 为 328108 B；该算例只证明可构造，尚未运行。1 MiB 参数、16 MiB 累积等极限由纯 parser/transport 实测，仍须分别报告，不能降低为 256 KiB 队列上限。

当前 Resolver 仍拒绝 tools 能力；生产 consumer/Runtime、Secret usage grant、Invocation/Usage 终态、Tool 授权/执行、真实 Provider 账号及 HTTP/root 接通不属于本结果。Summary 待决，Object 原中断任务停止，Artifact/Project 阻塞不变。归档只核原字节、Git locator、链接与格式，无 Go/Docker/SQL 或 SDK 执行，检查结果见[归档记录](evidence/openai-chat-tools-wire-spec-verification/archive-checks.json)。
