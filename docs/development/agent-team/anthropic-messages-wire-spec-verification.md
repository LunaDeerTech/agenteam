# Anthropic Messages wire 规格采纳

[rev1 工程卡](../work-items/recovery-d09-anthropic-messages-wire.md)已由 `recovery_documentation` 独立 **STATIC PASS**、主线程采纳并提交推送 `f349303bf6aa8b5c796f76cc43a34271e8c186e0`，主线程确认远端一致。**当前仅接受规格；19 条候选路径未获实施授权，OpenAI tools 产品接受及共享源码交权仍未满足。** 完整 D09 未完成。

规格作者为 `backend_recovery`（architecture_worker / research_worker），独立审查者未参与设计或实现。本次作者转 documentation_worker，按[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)归档原字节并检查链接与指纹；没有网络、SDK、Go、Docker、浏览器或产品执行，没有恢复受阻 tools 任务。

## 固定来源与接受输入

[证据入口](anthropic-messages-wire-spec-verification-evidence/README.md)保存官方 SDK `anthropics/anthropic-sdk-python` 的固定 commit `18f25547f20cf5f01da69ac611e700e3bc9ebf21`（1.11.0）：66 个完整选定文件、297,810 bytes，包含完整 MIT LICENSE。原公开获取命令、commit 原件、固定 URL/Git blob/SHA256/长度及字段定位均持久归档；没有安装或执行 SDK。来源 manifest SHA256 为 `28ea317726e14967d13f17197c9f4b1c2cd9980bba41f815dbddcbec5259c3b8`。

固定 Git 输入为接受基线 `0d22c7fd9202a81478804f2b810ebaddc350e856` 的 25 文件，加状态提交 `71f276f374fb1846adcd94bbc2dec2285be63e59` 的 2 文件。全部原字节、Git locator 与必要树对象已归档，可离线核对 commit/path/blob；未消费活动 tools 实现作为已验依赖。官方字段证据共 9 组、75 个精确行定位，SDK 内字段声明与注释差异、项目保守约束均在正式卡解释，不把 SDK 声明等同型号或账号验收。

| 冻结输入 | SHA256 | 相对前版的变化 |
| --- | --- | --- |
| [freeze01](anthropic-messages-wire-spec-verification-evidence/author/spec-freeze01/spec.md.txt) | `bc3830cf43ca262e560ac7668f2cc1547c33aedb264fef432bd9d00c2335d2b7` | 原待审规格，保留原事实 |
| [freeze02](anthropic-messages-wire-spec-verification-evidence/author/spec-freeze02/spec.md.txt) | `31d2ca9247e2b41b7432bf94e5bef06748166c54e6f00ae41111b84443535e00` | F1 stop_sequence 终止关系与 HTTP/SSE 验收反例 |
| [freeze03](anthropic-messages-wire-spec-verification-evidence/author/spec-freeze03/spec.md.txt) | `abbed65ecd40a99336a05f281c33c0efa8992883df2f55f9c44758da426f4d18` | F2 诊断输出禁令的一句范围澄清；独立最终接受输入 |
| [adopted01](anthropic-messages-wire-spec-verification-evidence/author/spec-adopted01/spec.md.txt) | `506f2a11a049f69e14d2a097c99c7594071df248b2244bf33deda06abacb374d` | 仅页首采纳状态/报告定位；与 f349303 交付文件同字节 |

两次窄修和最后采纳页首的完整差量各在对应目录 `delta.patch`，离线脚本重算逐字核对。freeze03 与 adopted01 技术 §1–11 原字节相同，其 SHA256 为 `3c813284980346ec5fbcb517756220f4ca4b02a796b4fb34619a276067c4d9db`。

## 独立静审结论

[独立原报告](anthropic-messages-wire-spec-verification-evidence/independent/review.md.txt) SHA256 `6be56df7a56463d7c5791b87b79d35a0ca8ee2decc69ada006b930b374dccfbf`；[最终检查](anthropic-messages-wire-spec-verification-evidence/independent/final-checks.json) SHA256 `732b10e27cce50763743edb93cdfaf9217e3a6b3e5ab18beff0d3792d9d9f787`，结论为 STATIC PASS，无剩余阻断发现。

| 发现 | 已接受修订 |
| --- | --- |
| F1：stop_sequence 未明确有工具或 required/named 未满足时的关系 | 仅无工具且非强制工具选择可文本成功，其余 protocol_invalid；HTTP/SSE 各列合法准入和可靠 usage 后的三个反例，并保留 none/auto 正例。仅规定未来验收，未执行动态红例 |
| F2：禁止 Request/Result/Event 含正文/参数的措辞误限正常业务字段 | 禁令限于 Format/LogValue/安全 JSON、日志、错误与诊断输出；正常 typed 业务载荷仍按契约承载。没有更改 API 或协议规则 |

独立审查覆盖 C0 有序内容与空块、最小 `Result.Message` / `Event.PartIndex` 载体和旧修订 nil 兼容、原生请求及消息关系、JSON/SSE 状态、累计 nullable usage、停止/安全错误、有界内存与 actual join、19 路径及未来验证闭环。API base 仅追加 `/messages`、不猜补 `/v1`；usage 初值及后续非 null 累计值覆盖、缺失保留、不估算 total 等规则均有固定来源。具体技术规范只在正式卡维护，本报告不另造规则副本。

独立元数据检查首次 exit1，原因是检查器误要求 LICENSE 出现字面标题 “MIT License”；原官方许可使用 MIT 正文。修正检查器后 exit0，最终修订元数据检查亦 exit0。[原失败记录](anthropic-messages-wire-spec-verification-evidence/independent/static-check01.json)、旧检查器、实际 argv/cwd/时间和 stdout/stderr 保留。该失败不是产品测试失败；指纹检查通过也不替代人工语义静审。

## 后续门槛与归档检查

root 须先接受 tools 产品、固定实际提交并交还四个共享 adapter 文件，再核相对本卡基线的差量、授予单一实施者。卡内 19 条候选路径及有界 fixture 扩展均没有当前写权；不复制共享 transport，不改旧闭集或原 512 KiB 场景上限。未来 pure/真实三组、旧 text/structured/tools 及 ledger 兼容按卡 §11 完成后，才能形成产品验收；本次没有这些执行证据。

reasoning/thinking 与 structured 明确留给后继完整卡，当前不支持时拒绝；生产 Resolver/Runtime/consumer、真实 Provider、工具授权执行和 HTTP/root 未绑定。Summary 待决、Object 原停止任务及 Artifact/Project 依赖阻塞保持。受自动筛查中断的 tools 独立任务仍为 [NOT RUN / 停止](recovery-2026-10-06.md#9-tools-独立启动被筛查中断not-run)，本规格不代替、重建或解除该任务的停止条件。

本次归档保存 143 个原件，另有轻量离线脚本、manifest 和 44 个必要树对象；没有 bare SDK、缓存或整树副本。`python3 docs/development/agent-team/anthropic-messages-wire-spec-verification-evidence/verify-evidence.py` 的实际结果见[归档检查记录](anthropic-messages-wire-spec-verification-evidence/archive-checks.json)：核 SHA/长度、66 官方文件、27 Git 输入、9 组/75 行来源、修订差量、独立索引与接受提交身份。[文档检查](anthropic-messages-wire-spec-verification-evidence/document-checks.json)另核新增文档/两状态入口的链接、fragment、UTF-8/LF、结构和限定 whitespace；原 commit、patch 与 fixture 的格式例外保留原字节。归档完整性与 STATIC PASS 均不表示实施已验收。
