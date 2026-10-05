# D09 OpenAI Chat structured wire 验收记录

状态：**structured wire 库已独立验收并获主线程采纳。** 精确 10 源已提交推送 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74`，主线程确认远端一致。结果限定 `openai-chat-structured-v1`，完整 D09 尚未完成。

## 固定输入与交付能力

依据[已采纳规格](../work-items/recovery-d09-openai-chat-structured-wire.md)，业务基线为 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`。[最终候选 manifest](evidence/d09-openai-chat-structured-wire-verification/author/candidate-review-02/manifest.json) SHA-256 为 `53d31b380ed5c214cb9e87fbf5450454b0f2e5935e0b92615dd3eb0b0389236c`：3 旧生产、1 新生产、3 新纯测试、1 新来源 manifest、2 新真实测试。接受提交的 10 个 Git blob 与候选逐 SHA 一致，见[源码核对](evidence/d09-openai-chat-structured-wire-verification/accepted-source-check.json)。

同一个 OpenAIChat adapter 新增独立 structured 修订，支持 strict `json_schema` 的有界闭集请求、普通响应及 SSE 完整结果验证，保留原文、usage、安全错误和实际关闭。schema 编译早于 Budget/出站接受；未知或未支持约束拒绝，不静默忽略。新旧修订共享原 64 全局/8 Project 配额；`openai-chat-text-v1` 及新修订 text 分支保持原语义。没有新增 C0 签名、迁移、依赖或运行 SDK。

[归档与重建说明](evidence/d09-openai-chat-structured-wire-verification/README.md)以固定 Git 为最终源码入口，保留必要原 patch、原红前像和独立 probe；不复制完整树、cache、binary 或凭据。两份新增 SDK 原件已包含在接受提交的 testdata manifest，以 base64 无损保存并核 SHA/Git blob；其余来源及许可证复用[原文本 wire 来源](evidence/d09-openai-chat-wire-source/README.md)。这是本地保守 schema 子集，不是全部 JSON Schema 或真实供应商模型能力保证。

## 原失败与修复

[review01 原静审](evidence/d09-openai-chat-structured-wire-verification/author/evidence/independent-production-review-01.md)为 BLOCKED：F1 的 Decoder.Token 会在限额判定前复制长标量，扫描中缺少取消检查；F2 是 ac5 原 consumerJoin 接缝，已 join 的底层可返回 nil，使后来取消未被成功出口复核。该报告仅为静态发现，没有倒写为动态复现。

作者随后在冻结 review01 上实跑[原红日志](evidence/d09-openai-chat-structured-wire-verification/author/evidence/pure-f1-f2-red-01.log)：**2 顶层/6 子例 FAIL**。4 MiB string/number 的验证额外分配分别为 20,972,048/20,972,128 B；JSON/SSE × caller/attempt 四个确定 barrier 在取消后错误成功。改为原字符串上的有界词法游标、增量数值限长/扫描内 ctx 检查，并在真实 waitJoined 后无条件复核 caller/attempt ctx 后，[同测试同预算](evidence/d09-openai-chat-structured-wire-verification/author/evidence/pure-f1-f2-fixed-01.log) **2/6 PASS**，额外分配降至 0/64 B。

[review02 窄静审](evidence/d09-openai-chat-structured-wire-verification/author/evidence/independent-production-review-02.md) PASS，原 D04/slot 所有权不变。F2 纯测试只证明本地已完成 owner 的取消出口与 slot 退休，不能单独证明 D04 body/server join；真实门槛由下列作者和独立组合补足。原 review01、原红输入及修复差量全部保留，未改卡降低要求。

## 作者检查及复用

[作者原报告](evidence/d09-openai-chat-structured-wire-verification/author/author-report.md) SHA-256 `2e9863e157df6fecbe832ed4fc1efc67a48cdd52b57a83d40e3802646579594b`；[原 87 项索引](evidence/d09-openai-chat-structured-wire-verification/author/author-evidence.json) `ebaa38378213af6b5ec5955a94eed69b6520c3128a7312ced530e1bd35789231`。独立者及归档者均核过全部原件 SHA/长度；最终工程检查的输入指纹等于最终 10 源。

| 记录 | 实际结论 |
| --- | --- |
| pure-race-02 | 整条 **exit 1** 保留：adapter **26 顶层/113 子例 PASS**，contract **24/13 PASS**；旧 Model 包因私有快照漏带固定 OpenAPI 资产，1 顶层失败。 |
| model-pure-race-03 | 仅从 ac5 补回精确 `model-system.json` 后，Model **26/20 PASS**；生产与测试未改，已过 adapter/contract 按包复用，不称一次纯整组全绿。 |
| 最终工程门槛 | integration vet、race integration compile、正式 fixture 包闭包编译、Central/Runner 两 cmd build 均实际 exit 0，有 argv/env/exit 原元数据；`-run=^$` 只计编译。 |
| structured-real-01 | 最终 10 源一次真实运行 **6 顶层/48 子例 PASS**，model 包 **13.165s**，driver exit 0，无 FAIL/SKIP；新 structured 为 **3/26**，原 text wire 为 **3/22**。 |

adapter 26/113 包含新纯测试 **11/75** 和原测试 **15/38**，原断言未改。原缺资产失败、恢复来源和按包计数见[原日志](evidence/d09-openai-chat-structured-wire-verification/author/evidence/pure-race-02.log)、[资产补充](evidence/d09-openai-chat-structured-wire-verification/author/evidence/baseline-assets-supplement.json)、[包结果](evidence/d09-openai-chat-structured-wire-verification/author/evidence/pure-package-results-02.json)。早期 review01 的通过记录只保留为历史，不替代最终输入。

[真实 selector](evidence/d09-openai-chat-structured-wire-verification/author/evidence/real-selection-01.json)仅选新 HTTP/Stream/CloseAndBudget 及旧 WireHTTP/Stream/JoinAndBudget。[完整日志](evidence/d09-openai-chat-structured-wire-verification/author/evidence/structured-real-01.log) SHA-256 `a01719ccc27512d99320d33cc338590b9e6cc1595f6d25956ceb43fdf5b0111d`，实际 argv/env/exit 见[原元数据](evidence/d09-openai-chat-structured-wire-verification/author/evidence/structured-real-01.json)。使用固定 Go 1.27.1、离线依赖、原 `-race -count=1 -timeout=6m`、`GOFLAGS=-mod=readonly -v`，未增加预算。

真实行为包括 strict native body 与输入冻结、嵌套/nullable/enum/精确大整数、先验零发送、合法 envelope 内 schema 失败、SSE 前缀/usage/失败终态、DONE 后服务器继续 hold 时的实际客户端关闭、背压取消与材料所有权、混合 revision 同 Budget；旧 text 三组真实兼容全过。16 MiB/1 MiB 等超过 fixture 单场景 512 KiB 的极限由新纯测试覆盖，没有扩大 fixture 上限或称其全为真实网络测试。

## 独立真实增量与资源

[独立最终报告](evidence/d09-openai-chat-structured-wire-verification/independent/final-report.md) SHA-256 `702c30f4da9a1e074205d9d9e10058b995c5ad00dab9cd5b732e9511f0a6ed6b`，[原定位索引](evidence/d09-openai-chat-structured-wire-verification/independent/final-core-index.json) `de1c1cea1c2f87ed3b0d090fc39c08e37800a90a0a71c454fed9ef3dba90e02f`。独立输入为 ac5 + 已审 4 生产 + [probe 原件](evidence/d09-openai-chat-structured-wire-verification/independent/probe.go.txt)，probe SHA-256 `5bd36ca1b831a621fbfc53103c4d1212807dec8fe84bd6c836c4f5105016292f`；没有消费作者新增测试或活动主树。

独立 race integration compile/vet 均 exit 0，随后[正式 driver](evidence/d09-openai-chat-structured-wire-verification/independent/evidence/dynamic01/command.json)一次真实执行 **2 顶层、无子例 PASS**，model **3.795s**、driver exit 0：[原日志](evidence/d09-openai-chat-structured-wire-verification/independent/evidence/dynamic01/raw.log) SHA-256 `9030cec37e62eb174303a80a8948936419daf093b064d4ddb64e129f8a20d27a`。其余包 no-tests-to-run 不计行为覆盖。

- `TestIndependentStructuredMismatchUsage`：实际 SSE 已消费前缀、合法 JSON 在嵌套 enum 处不匹配；断言 nonretryable 协议错误、Dispatched/PartialOutput、完整 usage 7/0/9、无 End、随后 EOF、一次 POST、无 canary 泄露，实际 join 后才销毁借用材料。
- `TestIndependentStructuredCancelActualJoin`：正式 WroteRequest barrier 与受控 server hold；取消和短 Close 超时后仍未 join、材料可用、8 个项目 slot 真占用，第 9 个零发送拒绝。原 callback 实际放行后才归还 slot；StopAdmission/Force 后全部 owner/body/parser 和服务器连接/handler 终局，才销毁材料。

独立准备阶段曾因请求不存在的 `api/openapi.yaml` 而 archive 拒绝，发生在 Go/fixture 前、零资源；[原记录](evidence/d09-openai-chat-structured-wire-verification/independent/evidence/preparation-rejection-01.json)保留，不计测试红例。独立真实轮无行为失败或重跑。

作者[双清理记录](evidence/d09-openai-chat-structured-wire-verification/author/evidence/structured-real-01-cleanup.json)与独立[第一轮清理](evidence/d09-openai-chat-structured-wire-verification/independent/evidence/dynamic01/cleanup01.json)、[第二轮清理](evidence/d09-openai-chat-structured-wire-verification/independent/evidence/dynamic01/cleanup02.json)均绑定实际存活 ID/name/nonce-label：两侧各 4 容器/3 网络双 exact-ID absent，原 2 容器/4 网络基线不变，所属进程/runtime 清零，窗口已交回。归档者仅核原件，不重新连接 Docker、SQL 或网络。

通过限于真实 Account/Audit/PG policy/D04 controlled local service 组合；material/snapshot 为协议 fixture 输入，没有真实 Provider 账号、Secret Resolve grant、Memory canonical schema、生产 consumer、Resolver、Invocation/Usage 或 wire root 绑定。Summary 初值/Settings 待决；Object guard 原阻断及 Artifact/Project 领域绑定依赖不变，不恢复暂停任务。

[完整轻量索引](evidence/d09-openai-chat-structured-wire-verification/SHA256SUMS.json)及[原件去重映射](evidence/d09-openai-chat-structured-wire-verification/archive-provenance.json)覆盖固定输入、实际命令、原失败和资源证据。此次仅文档归位与文件/Git 对象核对，没有新的 Go、Docker、动态验证或 Git 写操作。
