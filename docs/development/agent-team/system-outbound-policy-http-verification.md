# System 出站规则管理 HTTP 验证与交接

状态：rev1 十二路径完整结果已获作者检查、独立风险验收及主线程采纳，提交推送 `a94277982620f01dc15488b09ae6ea9064977b5a`，主线程核远端一致。本报告归位已发生的结果；归档过程只读取原件与固定 Git，没有重跑产品或启动资源。[工作卡](../work-items/d04-system-outbound-policy-http.md)技术§1–7 SHA `44515e212a996d1a1413022a38ae2b56297ad7e5cd9d195d732225aab818f8dc`保持原字节。固定前置为 `819aba1b8f764328f1e2e67b53c274fad0db877d`。

## 接受范围与输入

正式管理员 GET/PUT 出站规则管理已接入原 Account HTTPBoundary、同事务当前授权和唯一 PolicyService；管理写入更新既有 root 客户端实际使用的策略。完整替换、稳定用户命令身份、typed Rules/Audit/receipt/Unknown、原分类器与发送门禁保持。GET 是当前观察，原 key/body 重放返回历史回执；不新增 lookup、Reload、探测或代理 HTTP。原 3s GET／30s PUT 从预认证前计时，继承更早 parent，实际 body、服务和取消回调尾部完成后才离场。

[作者 API/root 阶段报告](system-outbound-policy-http-verification-evidence/objects/5a8bc24a38e800d2f37177b1445633f662f37c9cec1ecdf4aa731dcc85a26fb1)、[最终十一源独立报告](system-outbound-policy-http-verification-evidence/objects/415d43230a1cd0e1ee45d521dcca5634b7cb919255c2b2e2d6b5d43078251c38)及[第十二项交付清单](system-outbound-policy-http-verification-evidence/objects/058cf730b101e8ae69456df448f2b2b444712d611df1c596f42b2f7f114a8299)分别保留当时范围。最终十一源为 harness-stage02 `de32ede50b0c8d45c70c98ce480829c7630a568f07a554847484f3f9eb99938b`；作者执行输入 `13b8597403d77faaac567e0a99d7ec94d82c897138b1a7cae6b238607a7e88b5`，独立执行输入 `6c429eb693790085670fbfddb79aa3e68690d1bcf451eed22eda0b2f836d2414`。末件[后端说明](../backend/outbound.md) SHA `809de939bedcd7625342b9507921f0f0b7a0be4be62a6aa377edcebf1b501015`，只追加已验证契约和证据边界；其原“报告待冻结／待归档”字段是历史阶段事实，最终状态以本报告为准。

原 API 阶段407个固定 Git 文件；harness-stage01 有431个有效源码文件，11候选外420个固定依赖。事务 fixture 修复仅增加固定 Git 的 b02_fixture_test.go，最终432文件／421依赖。作者真实轮绑定3503个运行时文件，独立真实轮绑定3506个；这些是原指纹与工具链记录，不复制运行时实体。Go1.27.1、原锁、race/count1/package6m、新顶层2m及内部调用预算保持。独立八源与最终产品八源逐SHA相同，旧 Boundary/OpenAPI 回归按未变输入复用。

## 原失败与分阶段证据

| 阶段 | 实际结果与保留边界 |
| --- | --- |
| 作者 API api01／api02 | actual1/0.309s 缺缓存依赖；actual1/57.256s 替身 QueryRow 签名编译失败，均未执行测试。 |
| 作者 API api03 → api04／api05 | api03 actual1/4.185s 为真正产品 RED：原生 keepalive `unexpected EOF`。正常尾部提前 cancel 触发过期写期限；修为 Close body、stop/join 回调、清期限后再 cancel。api04 actual0/4.796s，最终 api05 actual0/5.378s，12 HTTP 顶层通过，补实际 Read/Close 尾部，没有放宽原生断言。 |
| 独立 API/root | 三个风险组首轮实际0/58.397s（含冷编译），包4.165s：strict wire／Unknown安全投影、取消写回调实际join、原生复用及自然3s／更早parent预算。237所属PID终局空；无PG或真实Account授权替身外推。 |
| harness／真实启动静审 | 原 O-HARNESS-01 保留：120s离线检查器不是资源监督器。execution-stage01 补独立真实driver、固定输入门禁和资源/PID终局后静审通过；原离线120s TERM/KILL不进入真实执行链。 |
| 作者 real01 | 总体actual1；四项PASS保留。事务测试以 newAccount 子集构造 HTTPBoundary，缺 Challenges/DeliveryRequested，初始化 DEPENDENCY_UNBOUND，未进入预认证／撤权／预算子例。 |
| 作者 transaction-real02 | 仅 `newAccount(t)` → `newB02Account(t).fixture` 一行，其他十候选和全部断言／预算不变；只复验事务一顶层，七子例PASS。不是五项最终整轮重跑。 |
| 独立 compile-account01 | actual1/94.010s 为私有 string 误传 typed IdempotencyKey 的编译前提。原 probe01／raw 保留；仅正式 ParseIdempotencyKey 后原 key 继续使用，后续两包race编译／vet／发现／门禁通过，未因此改产品。 |

原件定位见[独立 API 原审查](system-outbound-policy-http-verification-evidence/objects/3afaa3235170490b58c946e3e14bef64e3422ba24961bc89e968b7207ef569f4)、[作者 real01 原报告](system-outbound-policy-http-verification-evidence/objects/4571f1065dfd06f7720664549886e8216601f900a5558092bc00147446821afe)、[事务单组复验报告](system-outbound-policy-http-verification-evidence/objects/25efc658c5032246652438b590071290fd9992de7e316a26859430a139f296a4)、[独立编译前提失败记录](system-outbound-policy-http-verification-evidence/objects/609000c60e8f15b5b066f5304d6ba0fdd4bd2acd6b0d4ba0f3c3c6c84cd40b79)。35条原检查各自的 command/env、输入、raw、退出及收尾记录在[索引](system-outbound-policy-http-verification-evidence/index.json)；未执行的发现/编译不计动态PASS。

## 三轮真实结果与实际收尾

| 原轮 | actual exit／秒 | 顶层事实 | exact资源／所属PID／adopted wait |
| --- | --- | --- | --- |
| 作者 outbound-http-real01 | 1／32.295 | RootSharedInstance2.57s、HTTP3.88s、旧Durability1.04s、旧Rollback0.91s均PASS；Transaction初始化1.37s FAIL。 | 7／39／0 |
| 作者 outbound-http-transaction-real02 | 0／23.728 | 事务一顶层5.37s、七子例PASS。 | 7／30／0 |
| 独立 independent01 | 0／27.539 | HistoricalReceipt A2.58s、RootInflightPolicy B2.49s，首轮两项PASS。 | 7／36／0 |

每轮均原 command 实际 wait、两次 exact ID absent、所属 PID/starttime 空、runtime空、monitor0，原2容器4网络基线不变，输入前后一致。原历史非自有 PPID1 Z 未触碰，不计作本任务已wait或已回收。[独立原raw](system-outbound-policy-http-verification-evidence/objects/bfb5bb81e4015ec230566d09ac891ea783de809585e7b96ccaafea35f3c21afb) SHA `bfb5bb81e4015ec230566d09ac891ea783de809585e7b96ccaafea35f3c21afb`，[独立实际终局](system-outbound-policy-http-verification-evidence/objects/a444d9af69172d6c70900ba4a15ad360fe1cfe5421fe7a60c21b3c0e0174f8cb) SHA `a444d9af69172d6c70900ba4a15ad360fe1cfe5421fe7a60c21b3c0e0174f8cb`，[独立双清原件](system-outbound-policy-http-verification-evidence/objects/9921a9ae6d75e09cee5ed027fa25335bb9ee4539b3c3eaae9322c9db661aedfa) SHA `9921a9ae6d75e09cee5ed027fa25335bb9ee4539b3c3eaae9322c9db661aedfa`。

作者事务组真实证明预认证后正式撤销、User SH 对 revoke EX 排队，以及更早parent约256ms和PG LOCK_TIMEOUT=1s的实际锁等待、typed错误、零候选与实际join。GET／PUT数据库超时分别约1.006／1.010s，未要求所有底层错误都带同一SQLSTATE；最终policy v1、receipt0、audit0。这不冒充HTTP3s／30s自然到期，后者由API受控与原生证据另行支持。

独立A在真实HTTP已完整提交后截断下游响应；另一同用户Session推进当前v3，再显式原body/key和合法CSRF重放逐字节v2回执。按Outbound自身canonical command与Audit AppendKey精确关联，原receipt与Audit均恰1，actor/session/time匹配；正式注销后旧Cookie重放401并清Cookie。独立B以同root的policy/client保持已经获准的真实响应体，HTTP清规则后新请求PrivateNotAllowed/Sent=false且目标仍1请求；显式Release后原body EOF／reader join，root退出后socket与handler闭合。

## 限制与离线复核

响应截断不是DB COMMIT Unknown注入；未变D04双向Unknown与核实失败证据按[原D04完成记录](../work-items/d04-security-foundation.md)复用，HTTP Unknown安全投影有固定纯组证据。B隔离库总数不代替原command精确唯一性，后者由A覆盖。生产ready503／ready=false、Runtime和其他未绑定域、SMTP UI、未来出站页面、完整D08–D28/E01均不在此次完成声明中；E01未开始，Summary待决，Object/tools停止边界保持。

历史唯一有界缺件：harness-compile01 的 input 引用旧附加闭包清单 SHA `73ca61720757dae8393b0170de10446538adeb8e92cd8d22fb65e32e8a995884`，已选冻结来源中未定位该清单全文。其11候选源码、command/raw/result及退出保留，后续最终421依赖清单完整；不反推或补造旧清单，也不宣称所有历史闭包均可完整重建。被审规格原全文8b3770…a9a74与接受页首全文dfcf82…b453c均有原件，技术原字节可核。

[证据说明](system-outbound-policy-http-verification-evidence/README.md)保留581个逻辑原件，用213个SHA对象与286个Git引用去重；只复制必要原件，不复制完整源码树、缓存、依赖、dist或二进制。归档离线校验实际退出0：原字节、固定12个Git交付、源版本、421依赖、3503／3506指纹及三轮实际终局匹配。复核入口：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-outbound-policy-http-verification-evidence/verify_archive.py
```

该命令只读档案与固定Git，不执行档案脚本、业务、Docker或网络请求。原日志与diff按字节保存，原空白不为文档格式检查而清理。
