# System 出站规则管理 UI 规格审查记录

状态：2026-10-06，**冻结 rev3 获独立 STATIC PASS，主线程采纳为[正式卡 rev1](../work-items/d27-system-outbound-policy-ui.md)，提交推送 `f843506d9ec991be1c334a81b88d5cbacc277467` 并核远端一致。** 本档只记录规格、原静态审查和实施授权，不是 UI 产品或动态验收。

固定产品 `213cf5c3f552e6b05b541ce02afc1dd65ce9db93` 已接受 [SMTP 投递 UI](system-smtp-delivery-ui-verification.md)；后端 [Outbound HTTP](system-outbound-policy-http-verification.md) `a942779` 及其档案 `63de0ac` 已包含在该基线。这里只复核固定来源，不扩大旧动态证据。

## 1. 固定版本与审查链

| 输入 | 原件及结论 |
| --- | --- |
| 原私稿 rev1 | [全文](system-outbound-policy-ui-spec-verification-evidence/objects/7fa69924facaceaa9944364a68fdf53cdb26655eddf2bf89b48c94e19feb1a71) SHA `7fa69924facaceaa9944364a68fdf53cdb26655eddf2bf89b48c94e19feb1a71`，保留原依赖待验时态。 |
| rev2 | [全文](system-outbound-policy-ui-spec-verification-evidence/objects/a6b698b72f4e213c8decb360c171e3c27444f39e9df0540aa431d56597080d2a) SHA `a6b698b72f4e213c8decb360c171e3c27444f39e9df0540aa431d56597080d2a`；[34 路径与固定接缝](system-outbound-policy-ui-spec-verification-evidence/objects/5f5911aecda2ed756fb9cf0b3bb2cd1fc06be8e3dd2fde2f7c00bf86ded06ba0)及[原精确差量](system-outbound-policy-ui-spec-verification-evidence/objects/85681f0c2024b9a2704ff0d7a066eef21e3c3997630c1127a80bf6c50761baaf)。 |
| rev2 独立静审 | [原报告](system-outbound-policy-ui-spec-verification-evidence/objects/629917c04eea41426d53a5edd964625d322430f811c65cbad4d2fcfd38fcfc5d)与[原 JSON](system-outbound-policy-ui-spec-verification-evidence/objects/7315a9ee63955dfbaa327c0e15fc7ec2975d12a9f481de665e0108f12fc9d555)：STATIC BLOCKED，唯一 OUI-SPEC-01；没有产品动态 RED。 |
| rev3 | [全文](system-outbound-policy-ui-spec-verification-evidence/objects/efc6c8cb8c87c058c5e3f7db0d68bd3c33d6eef13a42fdcbc56fe0196d0699b0) SHA `efc6c8cb8c87c058c5e3f7db0d68bd3c33d6eef13a42fdcbc56fe0196d0699b0`；[窄修差量](system-outbound-policy-ui-spec-verification-evidence/objects/a4b7a7a8b5ba9e257ec7d2c6bdb9fc6f6e884364ab3bb25fc766ec30a9f0e5b4)只改页首与 §4／5／7，§1／2／3／6 原字节。 |
| rev3 独立复审 | [STATIC PASS 原报告](system-outbound-policy-ui-spec-verification-evidence/objects/525832e8a7a08ecddf1b09a0ae641e19003c1aa1fe69c60f54886c00a95ae011) SHA `525832e8a7a08ecddf1b09a0ae641e19003c1aa1fe69c60f54886c00a95ae011`；[原 JSON](system-outbound-policy-ui-spec-verification-evidence/objects/1bdd46db4f684b3d835eb13b2e28db3f0bca69f32353f8a4eacbf07d95c2bece)关闭唯一阻断，34 路径、预算与四新／三旧真实选择不变。 |
| 正式 rev1 | Git `f843506d9ec991be1c334a81b88d5cbacc277467` 的原卡全文 SHA `2f7f3dbed8cc4d263f1c1db3576404589c0cedb7c2aec217e84a223d9880579b`；[正式页首差量](system-outbound-policy-ui-spec-verification-evidence/objects/cd541cb0348ce7ed8139554a15f4d3d9f011a9cf5358489842218b4b819fa6be)与[原检查](system-outbound-policy-ui-spec-verification-evidence/objects/607b2f6ee3f39967f5e429a9d49d599577fd5dc8191667b08af8e3f5b3e3db8b)保留。技术 §1–7 SHA `343f09b6facd95c000df2f2b35bceeb73f3a7e93d2974ce18304703266d0ab6b` 与被审 rev3 相同。 |

本次仅在正式卡页首替换永久审查链接及登记最新授权，技术正文不改。原正式卡保存在固定 Git；此前 SMTP 独立报告[复用现有原件](system-smtp-delivery-ui-verification-evidence/objects/703600fccf13d2ce6d752f620eb8a40126b53a93b277e9ee9498f4c5c34dec05)，不复制产品树。

## 2. 唯一静态阻断与关闭范围

固定 `PolicyService` 可能先提交事务、再 publish 失败；错误可投影为 `503 / DEPENDENCY_UNAVAILABLE / not_started`。因此 `commit_state` 本身不能证明未提交。rev2 缺少首次明确拒绝的 status/code 闭集；这是固定代码与规格间的静态问题，本档没有执行该故障。

rev3 仅把合法 Problem、匹配 HTTP、`not_started/not_committed`、此前从未 unknown 与四对码共同作为首次明确拒绝：`400/INVALID_ARGUMENT`、`404/NOT_FOUND`、`409/VERSION_CONFLICT`、`409/INVALID_STATE`。已派发的其他失败保持未确认，明确覆盖提交后上述 503、未知码、状态错配、截断与未知提交状态；后续拒绝不洗掉历史 unknown，key 冲突不生成新 key。当前 GET 与历史 receipt 分离，无 lookup，只能显式恢复原 PUT 材料。

合法当前 `401/UNAUTHENTICATED|SESSION_REVOKED`、`403/FORBIDDEN` 及仅新 Outbound 写域的 `403/CSRF_FAILED`，先按原完整 identity／generation／current 守卫清理失效资格，即使 commit_state 为 committed/unknown 也不反推旧写未提交。错配的 503／400 + CSRF_FAILED 不触发清理；旧域 CSRF 行为不改。

clean 新导航允许 inactive、清理后的 anonymous、稳定 current 的非管理员或 system.denied；checking／failed 不追授旧 pending。inactive clean 离页不释放 Cookie owner，全局派发与退出仍等实际 finally。第十独立域、前九参数顺序、既有 smtpSections、同 View 确认宿主及实际 join 均保持。

## 3. 范围、原静态记录与未执行项

34 候选为十九核心路径及十五旧测试窄适配，22 既有／12 新增。旧七个 browser 文件仅十二处全系统叶数 7→8 等已定窄变化；八叶／三组／十二 return 不改旧 SMTP 协调器或退出协议。GET／PUT 两 API、Rules 规范编码、1 MiB 请求／512 KiB rules 字段与 600000 B 响应预算不变。原静态 JSON 形状上界 **414763 B** 不是合法最大规则 fixture；正式最大合法 PUT／GET 与实际 read/cancel/join 仍须后续实施验证。

[原静态检查](system-outbound-policy-ui-spec-verification-evidence/objects/77c5a3b6a0857e13cd4207dc85abb006ede6cbe49e1ad3b31f16d676c3aca160)保存同步只读 Git／SHA／形状算术及 `git diff --no-index --check` 的实际 exit1、空 stdout/stderr；差异退出不是空白错误。[原准备错误](system-outbound-policy-ui-spec-verification-evidence/objects/6cdc103a8e24fb38850f1367ca999f34699886036bf45260dda376f4ecdb69ba)保留 Python actual exit1 与原 traceback：曾误要求 no-index 返回 0，仅纠正解释，未改产品或启动资源。rev3／正式页首各自原 diff-check 记录亦保留。没有另存的完整 Python argv／env／独立 raw 或时长文件，不事后合成历史命令。

独立复审只是差量复核并复用原完整静审；没有 npm／Go 产品测试、构建、浏览器、监听器、数据库、Docker或网络探针。四新／三旧真实组是未来验收要求，未在本档执行。

## 4. 实际授权与后续边界

主线程已授权唯一 frontend 作者 **33 源路径私有实施与适用离线检查，README 第 19 路径最后完成**。作者已在 `/workspace/scratch/agenteam-outbound-policy-frontend-imqcje3i` 实际启动阶段 A 的 #1／2／3／11／12 五源；这里只登记主线程收到的 ACK，不读取活动实现形成结论。

独立验证已实际启动私有准备；[冻结计划](system-outbound-policy-ui-spec-verification-evidence/objects/ef6233fc772596821aab14815a32c1b9b7a61b461fd4fc22cf6c8c1a80940f26)及[原输入记录](system-outbound-policy-ui-spec-verification-evidence/objects/1175f171311baf29aaafc908aba889554c725c4dc328b2494b4fec1c5500221d)列 API／owner／App 各两个风险组合和最终真实 A/B。计划原时点尚无冻结候选、无 probe、无实际测试或资源；后续每阶段消费固定输入，真实 A/B 另授。**实施与离线自查有授权，真实资源未授权，UI 产品尚未验收。**

当前已接受 SMTP 投递结果不受本规格阻断影响。SPA scope controlled/native 的通过只按[既有交接 §26](recovery-2026-10-06-continuation.md#26-smtp-测试与投递任务-ui-接受及监督中断恢复)引用；后续 concurrent-publication 被平台内容安全机制终止、无执行目录，stage02 竞态仍未关闭，脚本修复／发布暂停，不重试、改写或转派。Object/tools 原停止同样保持。Summary 待决、生产未绑定／ready503 不变；完整 D08–D28/E01 未完成，E01 未开始。

## 5. 最小归档与离线复核

本档 21 逻辑原件以 **19 个 SHA 对象／190,454 字节**及两个 Git 原件引用保存；57 项固定 Git 来源引用去重为 57 个 blob。包括原稿、审查、差量、准备错误及计划，不复制源码闭包、依赖、dist、缓存或二进制。精确来源、字节和固定提交见[索引](system-outbound-policy-ui-spec-verification-evidence/index.json)与[档案说明](system-outbound-policy-ui-spec-verification-evidence/README.md)。

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-outbound-policy-ui-spec-verification-evidence/verify_archive.py
```

离线核验只读取原字节、固定 Git、34 路径存在性、三段精确差量、技术正文与行政追加边界；同时检查新增链接／fragment和限定格式。它不执行归档代码或产品测试，不把规格采纳等同动态通过。旧历史、原 objects 与旧验收报告保持。
