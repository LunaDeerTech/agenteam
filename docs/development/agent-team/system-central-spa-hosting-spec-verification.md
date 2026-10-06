# Central 嵌入 SPA 规格接受与独立静审记录

2026-10-06：主线程已采纳[正式卡 rev1](../work-items/d28-central-spa-hosting.md)，提交推送 `7a490ac7d5b09c1fff564fc50af93bfe236c1d71`并核远端一致。本报告只归档规格与行政状态，不是实现、构建、native HTTP或浏览器验收。主线程已另授16路径私有实施，作者与独立计划均已实际启动；没有动态资源授权。

## 1. 固定输入与范围

设计／后端依据为 `97f45518f31c8b7b835b02c8624eb7aa321a1894`，前端为已接受 `628612cdfc730cc1d89cad4e24a5d20f36cc6812`；固定两提交的web无差量。正式卡全文 SHA-256 为 `88b95723ad1c8a122f181123afceaaf72c375e7c55840cb3c009e414ee1e9e0c`，技术§1–7为 `f5399abe8b3137d4a8d8e8bbb3d2ddf6186013ae1ed092ecfa473b19a5967220`，与被审rev0.2原字节相同。原页首保留当时“待提交／另授”的历史文字，当前状态由本报告与接续§24说明。

16个候选路径固定，设计基线只有 `internal/central/app/app.go` 已存在，其余15为新文件；准确名单在卡§6及[归档索引](system-central-spa-hosting-spec-verification-evidence/index.json)。范围覆盖bundle／handler、窄root装配、显式发布脚本、纯/native与两个真实top的测试入口，以及最后的后端托管说明。候选名单不等于已实现文件，不消费SMTP17活动输入或后继Outbound UI。

## 2. 三次独立结论与原件

| 输入 | 原独立结论 | 接缝和复用边界 |
| --- | --- | --- |
| rev0，全文931a4b…865efd | [需三处窄澄清](system-central-spa-hosting-spec-verification-evidence/objects/99a3fe9215d4de2fea7d193a95b575ef2ef110688ad3fac4ae5b158ac42894c2) | S-SPA-01要求纯构造先于初始化；02要求SPA外层错误不反射path；03区分网络资产与既有精确data favicon，并核真实embed集合。均为静态接缝，不称产品RED。 |
| rev0.1，全文ffca4c…842ea | [前三项关闭，仍需尾部澄清](system-central-spa-hosting-spec-verification-evidence/objects/79373ac9b4c2d2af1430f3a7f294eb5e8e0be1af1a5dd73863c142cf723d969e) | S-SPA-04：原gate先finish后外层Recover写500，不能同时证明SPA失败回包仍在原期限／准入实际尾部内。未扩路径。 |
| rev0.2，全文845290…1ab5a8 | [STATIC PASS，有界规格结论](system-central-spa-hosting-spec-verification-evidence/objects/a30945d1cab277971ccba26912a76e82b8cb47451a34776f88c497bf477dbee8) | 四项关闭；局部Recover复用原tracked state，统一scope包住写／Flush／stop＋join，已准入仅结束后恰一次finish；拒绝503无槽位但有同一有界scope。未发现剩余规格阻断。 |

[被审rev0.2原文](system-central-spa-hosting-spec-verification-evidence/objects/84529007e9c71258d3a5fb84334c412d0d92b8a0ce6d7e8c09ba8352f61ab5a8)、rev0／0.1、两次技术diff、原rev1及页首两次diff、三次md／json均保留原字节。rev1-v2全文直接绑定接受提交，技术未改；离线checker逐次在内存应用原diff核对结果，不重建缺失历史文件。

## 3. 已定门槛与尚未验证

显式Web发布要求真实production Vite及完整imports／dynamicImports／css／assets闭包，严格校验后嵌入同一Central二进制；普通无tag Go构建不依赖Node／bundle。发布脚本、缺包失败、原子发布和清理均尚待实际实现验证，旧dist不作为本次新构建产物。

HTTP先按原路径判定API／诊断／资产／页面，不能见404再回退HTML；仅合法HTML导航可得入口，带点Project名只证明语法、不证明项目存在或授权。SPA安全副本保留原请求身份与取消链，错误instance和日志常量；统一30s或更早parent预算覆盖局部500／拒绝503及实际尾部。取消或写失败只abort，不追加错误后缀；物理TCP关闭不冒称先于finish。原API与no-tag链、body限制及CSRF不改。

这些门槛后续须取得纯／受控屏障／native及两个正式二进制真实代表的独立证据：空CWD、不依赖运行时dist／Node、真实PG／MinIO／Account根、正确PUBLIC_ORIGIN、登录／Session／懒页刷新／退出及API不被页面吞并。当前没有执行这些检查，不能把静态可实现性、测试发现或受控替身视为通过。

## 4. 证据、离线核对与记录限制

[索引](system-central-spa-hosting-spec-verification-evidence/index.json)包含18逻辑原件：16个新内容寻址对象共182789字节、正式卡Git原件1项、既有SMTP配置input04清单复用1项；另以33项必要固定Git引用保存源／文档与行政基线，未复制完整产品树、依赖、dist、缓存或二进制。

原独审目录只保留三次md／json，没有独立command／raw／完整env／退出码／时长文件；本归档不补造。原报告确实记载只读固定源码及旧36个dist／547593字节的SHA形状检查，未运行构建、测试或资源；复用的[永久input04清单](system-smtp-settings-ui-verification-evidence/objects/3aebb2e2116c890989128d529b8d14afbb4c61126753b40998897344580e9e37)支持定位，但本轮不重复读取dist。Go1.27.1标准库依据保留为原审查引用，不复制工具链。

[离线checker](system-central-spa-hosting-spec-verification-evidence/verify_archive.py)只核原字节／固定Git、差量闭合、技术与16路径、行政插入保留原文及新增链接／格式。本次实际命令／原输出／退出记录随私有交付提供；它们是本次归档检查，不是原独审执行记录或业务结果。归位后可在仓库运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-central-spa-hosting-spec-verification-evidence/verify_archive.py --repo /workspace/agenteam
```

## 5. 当前派工与未完成范围

主线程确认后端作者已在 `/workspace/scratch/agenteam-central-spa-author-gfjizhr4` 实际启动，固定97f4551／web628612c并读取7a490ac卡；先做bundle／handler与build脚本纯检查。[独立私有计划](system-central-spa-hosting-spec-verification-evidence/objects/b906fed039f28452e3388b98375436cbf2b99e5b6f47825c41c6c3f2614e179a)与basis已冻结，未消费活动实现。计划者参与过规格设计、未参与产品或作者测试；此前规格独审来自另一验证者，计划本身不是实现审查或动态结果。这里记录授权和启动，不记录尚未交付的检查为PASS；本归档没有读取该活动根。

SMTP投递UI authority01原失败已经独立静态产品归因、私有修复中，尚无产品交付；本报告不改SMTP卡或旧验收。D28仅此规格已接受，完整D08–D28/E01未完成、E01未开始。Summary初值待决、Object/tools原任务停止、Artifact/Project与生产未绑定、ready503等边界保持；不启动停止任务，也不以SPA规格解除它们。

## 6. rev1.1 发布脚本边界修订与原失败

2026-10-06追加：§1–5及原证据保持当时原字节。主线程已审查采纳[正式卡rev1.1](../work-items/d28-central-spa-hosting.md)，提交推送 `272c6c178462b4da72ac0eda69278e8dbde61f30`并核远端一致；全文 SHA `2cd71ed46779cbf382d0c507624839129a912ffa26a14056fe70678ae117fbd8`，技术§1–7 `cfa6d8f31c6f3159499bac4c1258c493299c4bee3c63825232a1841cc74056f8`。本次是已确认工程边界的规格修订，**两项原产品RED仍保留，返修尚未验收**。原rev1技术f5399abe…967220绑定7a490ac；不能套在当前rev1.1卡上。

仅页首与§2两处技术精度变化：显式 `node scripts/build-central-web.mjs` 只在Linux执行，启动任何工具前须证明 `/proc` 可观察本轮自有进程组PID/starttime，否则安全失败且不启动工具。另明确commit-last：完整候选先放dist下本轮独占临时文件，内容SHA定址摘要先就绪；所有工具实际结束、生成物/staging清理、锁归属核验及释放等可失败I/O全部在发布前完成，最后只有一次candidate→dist/agenteam原子rename，之后无可失败I/O。失败保留旧成功与匹配摘要，不能吞锁错、删除当前匹配摘要或在锁释放后回滚后继发布。普通无tag Go/Runner行为保持，不新增跨平台保证；16路径、API、HTTP预算及其它技术正文不变。

[首阶段独立原报告](system-central-spa-hosting-spec-verification-evidence/revisions/rev1.1/objects/fae76080028076bd5bfcf3dc1cc6083fb25cb12fa1b040281e545f113ec1df66)消费冻结bundle-stage01；其manifest SHA `6b247f9f2b7563aba333e7edf411866dc9088759b2e842e8243f36f8c57541ae`。验证者参与过规格设计，未参与产品实现或作者测试。下列是受控进程/文件系统实际失败，区别于§2原三次纯规格审查：

| 原轮 | 实际结果与到达边界 | 终局及不外推范围 |
| --- | --- | --- |
| child-tail01 / A-SCRIPT-01 | exit1／2.838s；inherit分支direct已停止、后代仍运行且Promise仍pending，ignore分支Promise已拒绝但后代仍运行。 | 取证后探针精确KILL自有后代并await原Promise/direct close，外部driver实际wait两个adopted后代；5个owned PID/starttime双扫空。补清理不是产品正确收尾。 |
| publication01 / A-SCRIPT-02 | exit1／1.179s；正常受控对照匹配发布，替换自有lock inode后实际命中WEB_BUILD_OWNERSHIP_CHANGED，binary已NEXT而匹配summary被删除，旧summary仍在。 | 1个owned PID/starttime实际wait及双扫空；合成资源与注入工具回调，不是真实Vite/type-check/Go embed或发布二进制。 |

两轮各45s外限，原command/env/raw/result/前后输入/observations/cleanup及两probe/driver均保存；原输入未变，无超时、driver或gate错误。总计6个自有PID，2个adopted wait另列，两个探针直接child由driver实际wait；子级direct close与非直属wait不能混为一谈，Z/X只说明停止。没有监听、网络、PG/MinIO/Docker或浏览器，本归档不重新执行任何探针。Node不能wait非直属子进程，修后仍须持续TERM→KILL并等待自有组无运行成员和direct close，外部driver的收养/实际wait独立举证。

[追加索引](system-central-spa-hosting-spec-verification-evidence/revisions/rev1.1/index.json)保存52个逻辑原件，用45个新SHA对象共211489字节与2个Git原件引用去重。初rev1.1、v2、v3及原diff/checks/delivery保留；v3逐字绑定272c6c1，四条差量在内存核闭合。两原候选脚本源码分别为53f2ffc…260e4、c2b0689…884cf4，与作者manifest及两轮输入原件相同。26个既有依赖逐项绑定97f4551 Git，未复制9+26树、App闭包、工具链、缓存或生成物。

这份最小包只持久化受影响的两脚本；其余7个bundle/renderer候选、原9源candidate.diff、作者七组检查全文以及Node/Python工具实体仅留原清单/报告指纹，未收入本次对象。不能声称仅靠此追加包可直接重放原全输入gate或重建旧9源完整阶段。独立报告中的作者race/vet/32纯项复用仍是原审查记录；本追加不把它们转成新动态或发布通过。受控probe自建合成材料的代码与关键原observations已保留，不复制其假binary/临时树。

旧index、16对象和旧checker保持原字节。新[版本定位checker](system-central-spa-hosting-spec-verification-evidence/revisions/rev1.1/verify_archive.py)先从6267717取6份必要历史文档到临时目录，调用原checker复核rev1历史；再核本次272c6c1、修订差量、原失败输入与终局、追加保留原文及链接。旧checker的直接当前工作树命令不再适合新增行政段；下面命令明确区分历史和当前，结果只表示离线归档完整：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-central-spa-hosting-spec-verification-evidence/revisions/rev1.1/verify_archive.py --repo /workspace/agenteam
```

以下仅据主线程最新调度通知，不是本档新增验收：SPA stage02两脚本修复已收到，原两probe的作者重跑已PASS，尚待独立验收；正式发布、native、两真实binary代表及核心16产品仍未交付。SMTP oldauthority02已actual0／59.566s并释放窗口，新五组、旧五组按版本通过；独立A/B仅六项离线检查进行中，仍未产品接受。本档未读取这些新增或活动原件，不以调度状态替代原证据或独立结论。完整D08–D28/E01未完成、E01未开始；Summary待决、Object/tools停止、Artifact/Project及生产未绑定、ready503保持。
